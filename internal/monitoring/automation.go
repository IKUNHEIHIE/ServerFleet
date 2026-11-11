package monitoring

import (
	"context"
	"encoding/json"
	"errors"
	"serverfleet.local/serverfleet/internal/notifications"
	"serverfleet.local/serverfleet/internal/storage"
	"sync"
	"time"
)

type AutomationAuthority func(context.Context, Monitor) error
type AlertEvent struct {
	Key        string              `json:"key"`
	MonitorID  string              `json:"monitorId"`
	OwnerID    string              `json:"ownerId"`
	Revision   uint64              `json:"revision"`
	Alert      notifications.Alert `json:"alert"`
	State      string              `json:"state"`
	DeliveryID int64               `json:"deliveryId,omitempty"`
	TaskID     string              `json:"taskId,omitempty"`
	CreatedAt  time.Time           `json:"createdAt"`
	UpdatedAt  time.Time           `json:"updatedAt"`
}

var ErrObservationChanged = errors.New("monitoring: configuration or observation changed while probing")

func AutomationMigrations() []storage.Migration {
	return []storage.Migration{{Version: "ds/0085_monitor_background_alerts", Name: "current owner automation grants and transactional monitor alert outbox", Statements: []string{
		`CREATE TABLE monitor_automation(monitor_id TEXT PRIMARY KEY,owner_id TEXT NOT NULL,revision INTEGER NOT NULL,observation_revision INTEGER NOT NULL,failure_threshold INTEGER NOT NULL DEFAULT 1,recovery_threshold INTEGER NOT NULL DEFAULT 1,consecutive_recoveries INTEGER NOT NULL DEFAULT 0,notify_channels BLOB)`,
		`INSERT INTO monitor_automation(monitor_id,owner_id,revision,observation_revision) SELECT id,'',0,0 FROM monitors`,
		`CREATE TABLE monitor_alert_events(id TEXT PRIMARY KEY,monitor_id TEXT NOT NULL,state TEXT NOT NULL,payload BLOB NOT NULL CHECK(length(payload)<=16384))`,
		`CREATE INDEX monitor_alert_events_pending ON monitor_alert_events(state,id)`,
	}}}
}
func (s *Service) bindAutomationTx(ctx context.Context, tx storage.Tx, id, owner string) (uint64, error) {
	var revision uint64
	e := tx.QueryRow(ctx, `SELECT revision FROM monitor_automation WHERE monitor_id=?`, id).Scan(&revision)
	if e != nil && !storage.IsNotFound(e) {
		return 0, e
	}
	revision++
	if revision > 1<<53-1 {
		return 0, ErrInvalidMonitor
	}
	_, e = tx.Exec(ctx, `INSERT INTO monitor_automation(monitor_id,owner_id,revision,observation_revision) VALUES(?,?,?,0) ON CONFLICT(monitor_id) DO UPDATE SET owner_id=excluded.owner_id,revision=excluded.revision`, id, owner, revision)
	return revision, e
}
func (r *Repo) hydrateAutomation(ctx context.Context, m *Monitor) error {
	var raw []byte
	e := r.db.QueryRow(ctx, `SELECT owner_id,revision,observation_revision,failure_threshold,recovery_threshold,consecutive_recoveries,notify_channels FROM monitor_automation WHERE monitor_id=?`, m.ID).Scan(&m.AutomationOwnerID, &m.AutomationRevision, &m.ObservationRevision, &m.FailureThreshold, &m.RecoveryThreshold, &m.ConsecutiveRecoveries, &raw)
	if storage.IsNotFound(e) {
		return nil
	}
	if e == nil && len(raw) > 0 && json.Unmarshal(raw, &m.NotifyChannels) != nil {
		return ErrInvalidMonitor
	}
	return e
}
func (s *Service) AutomationCurrent(ctx context.Context, id, owner string, revision uint64) error {
	m, e := s.repo.Get(ctx, id)
	if e != nil {
		return e
	}
	if !m.Enabled || m.AutomationOwnerID != owner || revision == 0 || m.AutomationRevision != revision {
		return ErrObservationChanged
	}
	if s.automationCurrent != nil {
		return s.automationCurrent(ctx, m)
	}
	return nil
}

// saveObservation performs the state transition, observation CAS and alert
// enqueue in the same transaction, so a crash cannot silently lose a transition.
func (s *Service) saveObservation(ctx context.Context, check Check, m Monitor, alert *notifications.Alert, eventWriter func(context.Context, storage.Tx) error) error {
	if s.automationCurrent != nil && s.automationCurrent(ctx, m) != nil {
		return ErrObservationChanged
	}
	return s.db.InTx(ctx, func(c context.Context, tx storage.Tx) error {
		moved, e := tx.Exec(c, `UPDATE monitor_automation SET observation_revision=observation_revision+1,consecutive_recoveries=? WHERE monitor_id=? AND revision=? AND observation_revision=?`, m.ConsecutiveRecoveries, m.ID, m.AutomationRevision, m.ObservationRevision)
		if e != nil {
			return e
		}
		n, e := moved.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return ErrObservationChanged
		}
		if e = s.repo.RecordCheck(c, tx, check, m.State, m.ConsecutiveFailures, m.LastError, m.LastCheckedAt); e != nil {
			return e
		}
		if alert != nil {
			channels := m.NotifyChannels
			if channels == nil {
				channels = []notifications.Channel{""}
			}
			for _, ch := range channels {
				next := *alert
				if ch != "" {
					next.DedupeKey += ":" + string(ch)
				}
				next.Channel = ch
				next.Source = notifications.AlertSource{Kind: "monitor", ID: m.ID, OwnerID: m.AutomationOwnerID, Revision: m.AutomationRevision}
				pending := AlertEvent{Key: next.DedupeKey, MonitorID: m.ID, OwnerID: m.AutomationOwnerID, Revision: m.AutomationRevision, Alert: next, State: "pending", CreatedAt: check.CheckedAt, UpdatedAt: check.CheckedAt}
				raw, e := json.Marshal(pending)
				if e != nil || len(raw) > 16384 {
					return ErrInvalidMonitor
				}
				if _, e = tx.Exec(c, `INSERT INTO monitor_alert_events VALUES(?,?,?,?)`, pending.Key, m.ID, pending.State, raw); e != nil {
					return e
				}
			}
		}
		if eventWriter != nil {
			return eventWriter(c, tx)
		}
		return nil
	})
}
func (s *Service) FlushAlerts(ctx context.Context, limit int) error {
	if s.notifier == nil {
		return ErrNotifyFailed
	}
	if limit < 1 || limit > 50 {
		limit = 50
	}
	rows, e := s.db.Query(ctx, `SELECT payload FROM monitor_alert_events WHERE state IN ('pending','queued','submitted') ORDER BY rowid LIMIT ?`, limit)
	if e != nil {
		return e
	}
	var events []AlertEvent
	for rows.Next() {
		var raw []byte
		var event AlertEvent
		if e = rows.Scan(&raw); e != nil {
			rows.Close()
			return e
		}
		if json.Unmarshal(raw, &event) != nil {
			rows.Close()
			return ErrInvalidMonitor
		}
		events = append(events, event)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	var first error
	for _, event := range events {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if s.AutomationCurrent(ctx, event.MonitorID, event.OwnerID, event.Revision) != nil {
			event.State = "blocked"
		} else {
			d, err := s.notifier.SendAlert(ctx, event.Alert)
			if err != nil {
				if first == nil {
					first = ErrNotifyFailed
				}
				continue
			}
			event.DeliveryID = d.ID
			event.TaskID = d.TaskID
			switch d.Status {
			case notifications.DeliveryProviderAccepted:
				event.State = "provider_accepted"
			case notifications.DeliverySubmitted:
				if d.ProviderCode == "attempt_pending" {
					event.State = "submitted"
				} else {
					event.State = "provider_accepted"
				}
			case notifications.DeliveryQueued:
				event.State = "queued"
			case notifications.DeliveryUnknown, notifications.DeliveryRejected, notifications.DeliveryFailed, notifications.DeliveryRateLimited:
				event.State = string(d.Status)
			default:
				// Legacy domain fixtures use a notifier acknowledgment without provider
				// metadata. Production composition always supplies the authority callback.
				if s.automationCurrent == nil {
					event.State = "provider_accepted"
				} else {
					if first == nil {
						first = ErrNotifyFailed
					}
					continue
				}
			}
		}
		event.UpdatedAt = s.now().UTC()
		raw, _ := json.Marshal(event)
		if _, e = s.db.Exec(ctx, `UPDATE monitor_alert_events SET state=?,payload=? WHERE id=?`, event.State, raw, event.Key); e != nil {
			return e
		}
	}
	return first
}
func (s *Service) AlertEvents(ctx context.Context, subject, id string, limit int) ([]AlertEvent, error) {
	if e := s.authorize(ctx, subject, ActionRead, "", "monitor_alert_events"); e != nil {
		return nil, e
	}
	if _, e := s.repo.Get(ctx, id); e != nil {
		return nil, e
	}
	if limit < 1 || limit > 50 {
		limit = 20
	}
	rows, e := s.db.Query(ctx, `SELECT payload FROM monitor_alert_events WHERE monitor_id=? ORDER BY rowid DESC LIMIT ?`, id, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []AlertEvent{}
	for rows.Next() {
		var raw []byte
		var event AlertEvent
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		if json.Unmarshal(raw, &event) != nil {
			return nil, ErrInvalidMonitor
		}

		out = append(out, event)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	if e = rows.Close(); e != nil {
		return nil, e
	}
	// Release the outer read before crossing into the notification repository.
	// This also works with a one-connection pool and concurrent history reads.
	for index := range out {
		event := out[index]
		if reader, ok := s.notifier.(interface {
			DeliveryStatus(context.Context, int64) (notifications.Delivery, error)
		}); ok && event.DeliveryID > 0 {
			if d, e := reader.DeliveryStatus(ctx, event.DeliveryID); e == nil {
				event.State = string(d.Status)
				event.DeliveryID = d.ID
				event.TaskID = d.TaskID
				if d.Status == notifications.DeliverySubmitted && d.ProviderCode != "attempt_pending" {
					event.State = "provider_accepted"
				}
			}
		}
		out[index] = event
	}
	return out, nil
}

type automationCursor struct {
	sync.Mutex
	next int
}

func (s *Service) alertAccepted(ctx context.Context, key string) bool {
	var state string
	return s.db.QueryRow(ctx, `SELECT state FROM monitor_alert_events WHERE id=?`, key).Scan(&state) == nil && state == "provider_accepted"
}

func (s *Service) bindMonitorOptions(ctx context.Context, tx storage.Tx, m Monitor) error {
	raw, e := json.Marshal(m.NotifyChannels)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `UPDATE monitor_automation SET failure_threshold=?,recovery_threshold=?,consecutive_recoveries=?,notify_channels=? WHERE monitor_id=?`, m.FailureThreshold, m.RecoveryThreshold, m.ConsecutiveRecoveries, raw, m.ID)
	return e
}
func (s *Service) transitionAccepted(ctx context.Context, m Monitor, key string) bool {
	if m.NotifyChannels == nil {
		return s.alertAccepted(ctx, key)
	}
	if len(m.NotifyChannels) == 0 {
		return false
	}
	for _, ch := range m.NotifyChannels {
		if !s.alertAccepted(ctx, key+":"+string(ch)) {
			return false
		}
	}
	return true
}
