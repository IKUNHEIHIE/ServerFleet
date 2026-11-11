package notifications

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"serverfleet.local/serverfleet/internal/storage"
	"strconv"
)

type ChannelBinding struct {
	OwnerID             string `json:"ownerId"`
	CredentialRevision  uint64 `json:"credentialRevision"`
	ConfigurationSHA256 string `json:"configurationSha256"`
}
type DeliveryIntent struct {
	SourceReference  AlertSource    `json:"sourceReference,omitempty"`
	DeliveryID       int64          `json:"deliveryId"`
	Attempt          int            `json:"attempt"`
	Channel          ChannelRecord  `json:"channel"`
	Binding          ChannelBinding `json:"binding"`
	Message          Message        `json:"message"`
	RequestID        string         `json:"requestId"`
	Source           string         `json:"source"`
	TaskID           string         `json:"taskId,omitempty"`
	ParentDeliveryID int64          `json:"parentDeliveryId,omitempty"`
	Fallback         bool           `json:"fallback"`
}
type DeliveryDispatcher func(context.Context, DeliveryIntent) (string, error)
type CredentialBindingPreparer func(context.Context, string, Channel, string) (uint64, error)
type DeliveryAuthority func(context.Context, DeliveryIntent) error

func TaskDeliveryMigrations() []storage.Migration {
	return []storage.Migration{{Version: "ds/0084_notification_task_delivery", Name: "owner bound notification intent, task linkage and durable alert deduplication", Statements: []string{
		`CREATE TABLE notification_channel_bindings(channel TEXT PRIMARY KEY,payload BLOB NOT NULL CHECK(length(payload)<=4096))`,
		`CREATE TABLE notification_delivery_intents(delivery_id INTEGER NOT NULL,attempt INTEGER NOT NULL,task_id TEXT NOT NULL DEFAULT '',payload BLOB NOT NULL CHECK(length(payload)<=32768),PRIMARY KEY(delivery_id,attempt))`,
		`CREATE UNIQUE INDEX notification_delivery_task ON notification_delivery_intents(task_id) WHERE task_id<>''`,
		`CREATE TABLE notification_alert_claims(dedupe_key TEXT PRIMARY KEY,delivery_id INTEGER NOT NULL)`,
		`CREATE TABLE notification_fallback_claims(parent_id INTEGER PRIMARY KEY,delivery_id INTEGER NOT NULL UNIQUE)`,
		// Before this release a crash could leave queued after provider I/O. Do not
		// interpret those historical rows as safe unsent work.
		`UPDATE notification_deliveries SET status='delivery_unknown',provider_code='legacy_unfinished' WHERE status='queued'`,
	}}}
}
func ChannelConfigurationSHA256(r ChannelRecord) string {
	raw, _ := json.Marshal(struct {
		Channel   Channel
		Enabled   bool
		SMTP      *SMTPSettings
		QQ        *QQSettings
		SecretRef string
	}{r.Channel, r.Enabled, r.SMTP, r.QQ, r.SecretRef})
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
func (s *Service) ReadBinding(ctx context.Context, ch Channel) (ChannelBinding, error) {
	var raw []byte
	e := s.db.QueryRow(ctx, `SELECT payload FROM notification_channel_bindings WHERE channel=?`, ch).Scan(&raw)
	if e != nil {
		return ChannelBinding{}, e
	}
	var b ChannelBinding
	if json.Unmarshal(raw, &b) != nil || b.OwnerID == "" || b.CredentialRevision == 0 || len(b.ConfigurationSHA256) != 64 {
		return b, ErrSecretUnavailable
	}
	return b, nil
}
func (s *Service) bindCredentialTx(ctx context.Context, tx storage.Tx, owner string, record ChannelRecord, revision uint64) error {
	if revision == 0 {
		return ErrSecretUnavailable
	}
	raw, _ := json.Marshal(ChannelBinding{OwnerID: owner, CredentialRevision: revision, ConfigurationSHA256: ChannelConfigurationSHA256(record)})
	_, e := tx.Exec(ctx, `INSERT INTO notification_channel_bindings VALUES(?,?) ON CONFLICT(channel) DO UPDATE SET payload=excluded.payload`, record.Channel, raw)
	return e
}
func (s *Service) ReadIntent(ctx context.Context, id int64, attempt int) (DeliveryIntent, error) {
	var raw []byte
	var task string
	e := s.db.QueryRow(ctx, `SELECT task_id,payload FROM notification_delivery_intents WHERE delivery_id=? AND attempt=?`, id, attempt).Scan(&task, &raw)
	if e != nil {
		return DeliveryIntent{}, e
	}
	var in DeliveryIntent
	if len(raw) > 32768 || json.Unmarshal(raw, &in) != nil || in.DeliveryID != id || in.Attempt != attempt || in.TaskID != task || in.Attempt < 1 || in.Attempt > 6 {
		return in, ErrInvalidConfig
	}
	return in, nil
}
func (s *Service) persistIntentTx(ctx context.Context, tx storage.Tx, in DeliveryIntent) error {
	raw, e := json.Marshal(in)
	if e != nil || len(raw) > 32768 {
		return ErrInvalidMessage
	}
	_, e = tx.Exec(ctx, `INSERT INTO notification_delivery_intents VALUES(?,?,?,?)`, in.DeliveryID, in.Attempt, in.TaskID, raw)
	return e
}
func (s *Service) queueDelivery(ctx context.Context, record ChannelRecord, message Message, d Delivery, source string) (out Delivery, err error) {
	defer func() {
		if err == nil || d.ID <= 0 {
			return
		}
		// A rejected preflight must not leave a permanently queued orphan. An
		// existing intent (possibly being dispatched concurrently) is untouched.
		cleanup := s.db.InTx(ctx, func(c context.Context, tx storage.Tx) error {
			changed, e := tx.Exec(c, `UPDATE notification_deliveries SET status='failed',provider_code='not_queued',updated_at=? WHERE id=? AND attempts=? AND status='queued' AND NOT EXISTS(SELECT 1 FROM notification_delivery_intents WHERE delivery_id=?)`, storage.EncodeTime(s.now()), d.ID, d.Attempts, d.ID)
			if e != nil {
				return e
			}
			n, e := changed.RowsAffected()
			if e != nil {
				return e
			}
			if n == 1 {
				return s.audit.AppendTx(c, tx, s.event(systemAuditSubject, ActionSend, "failure", "notification-preflight", "not_queued"))
			}
			return nil
		})
		if cleanup == nil {
			if current, e := s.Delivery(ctx, d.ID); e == nil {
				out = current
			}
		}
	}()
	if s.dispatch == nil {
		return Delivery{}, ErrProviderUnavailable
	}
	binding, e := s.ReadBinding(ctx, record.Channel)
	if e != nil || binding.ConfigurationSHA256 != ChannelConfigurationSHA256(record) {
		return d, ErrSecretUnavailable
	}
	// Manual tests/retries may only borrow the acting owner's own credential.
	scope := secretScopeFrom(ctx)
	if scope.SubjectID != "" && scope.SubjectID != systemAuditSubject && scope.SubjectID != binding.OwnerID {
		return d, ErrSecretUnavailable
	}
	settings, e := s.repo.GetSettings(ctx)
	if e != nil {
		return d, e
	}
	in := DeliveryIntent{DeliveryID: d.ID, Attempt: d.Attempts + 1, Channel: record, Binding: binding, Message: message, Source: source, RequestID: scope.RequestID, Fallback: settings.SMTPFallbackOnQQFailure && record.Channel == ChannelQQBot}
	if d.Attempts > 0 {
		previous, err := s.ReadIntent(ctx, d.ID, d.Attempts)
		if err != nil {
			return d, err
		}
		in.SourceReference = previous.SourceReference
		in.Message = previous.Message
	}
	if in.RequestID == "" {
		in.RequestID = "notification-" + strconv.FormatInt(d.ID, 10)
	}
	if s.current != nil {
		if e = s.current(ctx, in); e != nil {
			return d, e
		}
	}
	e = s.db.InTx(ctx, func(c context.Context, tx storage.Tx) error {
		var count int
		if err := tx.QueryRow(c, `SELECT count(*) FROM notification_delivery_intents WHERE delivery_id=? AND attempt=?`, in.DeliveryID, in.Attempt).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return nil
		}
		return s.persistIntentTx(c, tx, in)
	})
	if e != nil {
		return d, e
	}
	return s.DispatchIntent(ctx, in.DeliveryID, in.Attempt)
}
func (s *Service) DispatchIntent(ctx context.Context, id int64, attempt int) (Delivery, error) {
	in, e := s.ReadIntent(ctx, id, attempt)
	if e != nil {
		return Delivery{}, e
	}
	d, e := s.repo.GetDelivery(ctx, id)
	if e != nil {
		return d, e
	}
	if in.TaskID != "" {
		if in.Attempt > d.Attempts && d.Status == DeliveryFailed {
			d.Status = DeliveryQueued
		}
		d.TaskID = in.TaskID
		return d, nil
	}
	if d.Attempts >= attempt {
		return d, ErrNotRetryable
	}
	if s.dispatch == nil {
		return d, ErrProviderUnavailable
	}
	if s.current != nil {
		if e = s.current(ctx, in); e != nil {
			// No task has been submitted here. Retire this frozen intent so
			// revoked work cannot silently reactivate or starve crash recovery.
			if stopped := s.ReconcileTask(ctx, id, attempt, "failed"); stopped != nil {
				return d, stopped
			}
			if current, readErr := s.Delivery(ctx, id); readErr == nil {
				d = current
			}
			return d, e
		}
	}
	task, e := s.dispatch(ctx, in)
	if e != nil {
		return d, e
	}
	e = s.BindTask(ctx, id, attempt, task)
	d.TaskID = task
	if in.Attempt > d.Attempts && d.Status == DeliveryFailed {
		d.Status = DeliveryQueued
	}
	return d, e
}
func (s *Service) PendingIntents(ctx context.Context, limit int) ([]DeliveryIntent, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, e := s.db.Query(ctx, `SELECT i.delivery_id,i.attempt FROM notification_delivery_intents i JOIN notification_deliveries d ON d.id=i.delivery_id WHERE i.task_id='' AND d.attempts<i.attempt AND d.status IN ('queued','failed') ORDER BY i.delivery_id,i.attempt LIMIT ?`, limit)
	if e != nil {
		return nil, e
	}
	var ids [][2]int64
	for rows.Next() {
		var id, a int64
		if e = rows.Scan(&id, &a); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, [2]int64{id, a})
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []DeliveryIntent{}
	for _, id := range ids {
		in, err := s.ReadIntent(ctx, id[0], int(id[1]))
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, nil
}

// ExecuteIntent is reachable only from the Controller's registered task worker.
// The submitted fence commits before any external I/O. A lost final commit or
// restart never makes this attempt eligible for another send.
func (s *Service) ExecuteIntent(ctx context.Context, id int64, attempt int, task string) (Delivery, error) {
	in, e := s.ReadIntent(ctx, id, attempt)
	if e != nil || in.TaskID != task || task == "" {
		return Delivery{}, ErrNotRetryable
	}
	if s.current == nil {
		return Delivery{}, ErrProviderUnavailable
	}
	if e = s.current(ctx, in); e != nil {
		return Delivery{}, e
	}
	d, e := s.repo.GetDelivery(ctx, id)
	if e != nil {
		return d, e
	}
	if d.Attempts >= attempt || d.Status != DeliveryQueued && d.Status != DeliveryFailed {
		return d, ErrNotRetryable
	}
	e = s.db.InTx(ctx, func(c context.Context, tx storage.Tx) error {
		changed, err := tx.Exec(c, `UPDATE notification_deliveries SET status='submitted',provider_code='attempt_pending',attempts=?,updated_at=? WHERE id=? AND attempts=? AND status=?`, attempt, storage.EncodeTime(s.now()), id, d.Attempts, d.Status)
		if err != nil {
			return err
		}
		n, err := changed.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrNotRetryable
		}
		return s.audit.AppendTx(c, tx, s.event(in.Binding.OwnerID, ActionSend, "success", in.RequestID, "delivery_attempt_fenced"))
	})
	if e != nil {
		return d, e
	}
	// attempt increments from the previously persisted count; that exact count
	// is already reserved by the submitted fence above.
	attemptCtx := WithSecretScope(ctx, SecretScope{SubjectID: in.Binding.OwnerID, RequestID: in.RequestID})
	result, sendErr := s.attempt(attemptCtx, in.Channel, in.Message, d)
	if s.current(ctx, in) != nil {
		result.Status = DeliveryUnknown
		result.ProviderCode = "authority_changed_after_attempt"
		result.ProviderCredentialInvalid = false
		sendErr = ErrDeliveryUnknown
	}
	final, finalErr := s.finalizeDelivery(ctx, result, ActionSend, in.Binding.OwnerID, in.RequestID, "task_delivery_result", func(c context.Context, tx storage.Tx) error {
		if !result.ProviderCredentialInvalid {
			return nil
		}
		current, e := s.repo.getChannel(c, tx, in.Channel.Channel)
		if e != nil {
			return e
		}
		var raw []byte
		e = tx.QueryRow(c, `SELECT payload FROM notification_channel_bindings WHERE channel=?`, in.Channel.Channel).Scan(&raw)
		if e != nil {
			return e
		}
		var binding ChannelBinding
		if json.Unmarshal(raw, &binding) != nil {
			return ErrInvalidConfig
		}
		if binding != in.Binding || ChannelConfigurationSHA256(current) != in.Binding.ConfigurationSHA256 {
			return nil
		}
		return s.repo.updateCredential(c, tx, in.Channel.Channel, in.Channel.SecretRef, CredentialInvalid, s.now())
	})
	if finalErr != nil {
		return result.Delivery, ErrDeliveryUnknown
	}
	final.TaskID = task
	if in.Fallback && shouldFallback(final.Status) {
		_, _ = s.QueueFallback(ctx, in, final)
	}
	return final, sendErr
}
func (s *Service) Delivery(ctx context.Context, id int64) (Delivery, error) {
	d, e := s.repo.GetDelivery(ctx, id)
	if e != nil {
		return d, e
	}
	var task string
	var attempt int
	e = s.db.QueryRow(ctx, `SELECT task_id,attempt FROM notification_delivery_intents WHERE delivery_id=? ORDER BY attempt DESC LIMIT 1`, id).Scan(&task, &attempt)
	if e == nil {
		if attempt > d.Attempts && d.Status == DeliveryFailed {
			d.Status = DeliveryQueued
		}
		d.TaskID = task
	} else if !storage.IsNotFound(e) {
		return d, e
	}
	return d, nil
}
func (s *Service) QueueFallback(ctx context.Context, parent DeliveryIntent, d Delivery) (Delivery, error) {
	smtp, e := s.repo.GetChannel(ctx, ChannelSMTP)
	if e != nil || !usableChannel(smtp) {
		return Delivery{}, ErrChannelNotConfigured
	}
	binding, e := s.ReadBinding(ctx, ChannelSMTP)
	if e != nil || binding.ConfigurationSHA256 != ChannelConfigurationSHA256(smtp) {
		return Delivery{}, ErrSecretUnavailable
	}
	var child int64
	e = s.db.QueryRow(ctx, `SELECT delivery_id FROM notification_fallback_claims WHERE parent_id=?`, d.ID).Scan(&child)
	if e == nil {
		return s.Delivery(ctx, child)
	}
	if !storage.IsNotFound(e) {
		return Delivery{}, e
	}
	fallback := Delivery{Channel: ChannelSMTP, Status: DeliveryQueued, Message: d.Message, DedupeKey: d.DedupeKey, CreatedAt: s.now(), UpdatedAt: s.now()}
	settings, e := s.repo.GetSettings(ctx)
	if e != nil || !settings.SMTPFallbackOnQQFailure {
		return Delivery{}, ErrChannelNotConfigured
	}
	e = s.db.InTx(ctx, func(c context.Context, tx storage.Tx) error {
		id, err := s.repo.insertDelivery(c, tx, fallback)
		if err != nil {
			return err
		}
		fallback.ID = id
		if _, err = tx.Exec(c, `INSERT INTO notification_fallback_claims VALUES(?,?)`, d.ID, id); err != nil {
			return err
		}
		in := DeliveryIntent{SourceReference: parent.SourceReference, DeliveryID: id, Attempt: 1, Channel: smtp, Binding: binding, Message: parent.Message, RequestID: "fallback-" + strconv.FormatInt(d.ID, 10), Source: "system", ParentDeliveryID: d.ID}
		return s.persistIntentTx(c, tx, in)
	})
	if e != nil {
		return Delivery{}, e
	}
	return s.DispatchIntent(ctx, fallback.ID, 1)
}
func (s *Service) EffectiveDelivery(ctx context.Context, id int64) (Delivery, error) {
	var child int64
	e := s.db.QueryRow(ctx, `SELECT delivery_id FROM notification_fallback_claims WHERE parent_id=?`, id).Scan(&child)
	if e == nil {
		return s.Delivery(ctx, child)
	}
	if !storage.IsNotFound(e) {
		return Delivery{}, e
	}
	return s.Delivery(ctx, id)
}

// Durable alerts are used by background producers. Their episode/threshold keys
// remain claimed after UNKNOWN, failure or restart, independently of the normal
// manual-test deduplication window.
func (s *Service) SendDurableAlert(ctx context.Context, alert Alert) (Delivery, error) {
	if alert.DedupeKey == "" || validateDedupeKey(alert.DedupeKey) != nil {
		return Delivery{}, ErrInvalidDedupeKey
	}
	var id int64
	e := s.db.QueryRow(ctx, `SELECT delivery_id FROM notification_alert_claims WHERE dedupe_key=?`, alert.DedupeKey).Scan(&id)
	if e == nil {
		return s.EffectiveDelivery(ctx, id)
	}
	if !storage.IsNotFound(e) {
		return Delivery{}, e
	}
	record, _, e := s.resolveChannels(ctx, alert.Channel)
	if e != nil {
		return Delivery{}, e
	}
	message := redactedMessage(Message{Subject: alert.Subject, Body: alert.Body})
	if validateMessage(message) != nil {
		return Delivery{}, ErrInvalidMessage
	}
	binding, e := s.ReadBinding(ctx, record.Channel)
	if e != nil || binding.ConfigurationSHA256 != ChannelConfigurationSHA256(record) {
		return Delivery{}, ErrSecretUnavailable
	}
	settings, e := s.repo.GetSettings(ctx)
	if e != nil {
		return Delivery{}, e
	}
	in := DeliveryIntent{Attempt: 1, SourceReference: alert.Source, Channel: record, Binding: binding, Message: message, Source: "system", RequestID: "alert-" + shortAlertKey(alert.DedupeKey), Fallback: settings.SMTPFallbackOnQQFailure && record.Channel == ChannelQQBot && alert.Channel == ""}
	if s.current != nil {
		if e = s.current(ctx, in); e != nil {
			return Delivery{}, e
		}
	}
	d := Delivery{Channel: record.Channel, Status: DeliveryQueued, Message: message.Body, DedupeKey: alert.DedupeKey, CreatedAt: s.now(), UpdatedAt: s.now()}
	e = s.db.InTx(ctx, func(c context.Context, tx storage.Tx) error {
		id, err := s.repo.insertDelivery(c, tx, d)
		if err != nil {
			return err
		}
		in.DeliveryID = id
		d.ID = id
		if _, err = tx.Exec(c, `INSERT INTO notification_alert_claims VALUES(?,?)`, alert.DedupeKey, id); err != nil {
			return err
		}
		return s.persistIntentTx(c, tx, in)
	})
	if e != nil {
		if storage.IsUniqueViolation(e) {
			return s.SendDurableAlert(ctx, alert)
		}
		return Delivery{}, e
	}
	return s.DispatchIntent(ctx, d.ID, 1)
}
func shortAlertKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:16])
}

func IntentSHA256(in DeliveryIntent) string {
	in.TaskID = ""
	raw, _ := json.Marshal(in)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
func (s *Service) BindTask(ctx context.Context, id int64, attempt int, task string) error {
	if task == "" {
		return ErrInvalidConfig
	}
	in, e := s.ReadIntent(ctx, id, attempt)
	if e != nil {
		return e
	}
	if in.TaskID != "" && in.TaskID != task {
		return ErrNotRetryable
	}
	in.TaskID = task
	raw, _ := json.Marshal(in)
	result, e := s.db.Exec(ctx, `UPDATE notification_delivery_intents SET task_id=?,payload=? WHERE delivery_id=? AND attempt=? AND (task_id='' OR task_id=?)`, task, raw, id, attempt, task)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return ErrNotRetryable
	}
	return nil
}
func (s *Service) RecentTaskIntents(ctx context.Context, limit int) ([]DeliveryIntent, error) {
	if limit < 1 || limit > 100 {
		limit = 100
	}
	rows, e := s.db.Query(ctx, `SELECT i.delivery_id,i.attempt FROM notification_delivery_intents i JOIN notification_deliveries d ON d.id=i.delivery_id WHERE i.task_id<>'' AND ((d.status IN ('queued','submitted') AND d.provider_code IN ('','attempt_pending')) OR (d.status='failed' AND i.attempt=d.attempts+1)) ORDER BY i.delivery_id,i.attempt LIMIT ?`, limit)
	if e != nil {
		return nil, e
	}
	var ids [][2]int64
	for rows.Next() {
		var id, a int64
		if e = rows.Scan(&id, &a); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, [2]int64{id, a})
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []DeliveryIntent{}
	for _, id := range ids {
		in, err := s.ReadIntent(ctx, id[0], int(id[1]))
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, nil
}

// Reserved for Controller reconciliation of a terminal unified task: an
// unconfirmed send is never converted back to queued/failed.
func (s *Service) ReconcileTask(ctx context.Context, id int64, attempt int, status string) error {
	if status != "unknown" && status != "canceled" && status != "expired" && status != "failed" && status != "partial_failure" {
		return nil
	}
	_, e := s.ReadIntent(ctx, id, attempt)
	if e != nil {
		return e
	}
	d, e := s.repo.GetDelivery(ctx, id)
	if e != nil {
		return e
	}
	if d.Status == DeliveryUnknown || d.Attempts >= attempt && d.Status != DeliveryQueued && !(d.Status == DeliverySubmitted && d.ProviderCode == "attempt_pending") {
		return nil
	}
	previous := d
	outcome := DeliveryUnknown
	code := "task_" + status
	if status != "unknown" && (d.Status == DeliveryQueued || d.Status == DeliveryFailed && d.Attempts < attempt) {
		outcome = DeliveryFailed
		code += "_not_sent"
	}
	if d.Attempts < attempt {
		d.Attempts = attempt
	}
	d.Status = outcome
	d.ProviderCode = code
	d.UpdatedAt = s.now()
	return s.db.InTx(ctx, func(c context.Context, tx storage.Tx) error {
		moved, e := tx.Exec(c, `UPDATE notification_deliveries SET status=?,provider_code=?,attempts=?,updated_at=? WHERE id=? AND status=? AND provider_code=? AND attempts=?`, d.Status, d.ProviderCode, d.Attempts, storage.EncodeTime(d.UpdatedAt), id, previous.Status, previous.ProviderCode, previous.Attempts)
		if e != nil {
			return e
		}
		n, e := moved.RowsAffected()
		if e != nil {
			return e
		}
		if n == 0 {
			return nil
		}
		return s.audit.AppendTx(c, tx, s.event(systemAuditSubject, ActionSend, "failure", "notification-reconcile-"+strconv.FormatInt(id, 10), "task_terminal_"+status))
	})
}
