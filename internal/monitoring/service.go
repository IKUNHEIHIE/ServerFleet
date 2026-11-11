package monitoring

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/notifications"
	"serverfleet.local/serverfleet/internal/storage"
)

const (
	ActionRead   = "monitoring.read"
	ActionWrite  = "monitoring.write"
	ActionCreate = "monitoring.create"
	ActionUpdate = "monitoring.update"
	ActionDelete = "monitoring.delete"
	ActionCheck  = "monitoring.check"
)

// ServiceActions returns the audit action catalog this domain emits.
func ServiceActions() []string {
	return []string{ActionCheck, ActionCreate, ActionDelete, ActionRead, ActionUpdate, ActionWrite}
}

// AuditSink is the durable audit port; manual checks and configuration mutations
// append in the same transaction as their data.
type AuditSink interface {
	Append(context.Context, audit.Event) error
	AppendTx(context.Context, storage.Tx, audit.Event) error
}

// Options configures the monitoring service. DB, Repo, Audit and Authorizer are
// mandatory. A nil Prober fails closed for every check; a nil Notifier suppresses
// transition alerts but does not fabricate one.
type Options struct {
	RemoteProber      RemoteProber
	SourceValidator   SourceValidator
	DB                *storage.DB
	Repo              *Repo
	Audit             AuditSink
	Authorizer        auth.Authorizer
	Prober            Prober
	Notifier          Notifier
	AllowedHosts      []string
	Now               func() time.Time
	AutomationCurrent AutomationAuthority
}

type Service struct {
	remoteProber      RemoteProber
	sourceValidator   SourceValidator
	db                *storage.DB
	repo              *Repo
	audit             AuditSink
	authorizer        auth.Authorizer
	prober            Prober
	notifier          Notifier
	policy            TargetPolicy
	now               func() time.Time
	automationCurrent AutomationAuthority
	cursor            automationCursor
}

func NewService(options Options) (*Service, error) {
	switch {
	case options.DB == nil:
		return nil, errors.New("monitoring: database is required")
	case options.Repo == nil:
		return nil, errors.New("monitoring: repository is required")
	case options.Audit == nil:
		return nil, errors.New("monitoring: audit sink is required")
	case options.Authorizer == nil:
		return nil, ErrMissingAuthorizer
	}
	policy, err := NewTargetPolicy(options.AllowedHosts)
	if err != nil {
		return nil, err
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		db: options.DB, repo: options.Repo, audit: options.Audit, remoteProber: options.RemoteProber, sourceValidator: options.SourceValidator,
		authorizer: options.Authorizer, prober: options.Prober,
		notifier: options.Notifier, policy: policy, now: now, automationCurrent: options.AutomationCurrent,
	}, nil
}

// List returns every monitor visible to the subject. The read grant is checked
// before any inventory is loaded.
func (s *Service) List(ctx context.Context, subjectID string) ([]Monitor, error) {
	if err := s.authorize(ctx, subjectID, ActionRead, "", "monitor_list"); err != nil {
		return nil, err
	}
	return s.repo.List(ctx)
}

func (s *Service) Get(ctx context.Context, subjectID, monitorID string) (Monitor, error) {
	if err := s.authorize(ctx, subjectID, ActionRead, "", "monitor_get"); err != nil {
		return Monitor{}, err
	}
	return s.repo.Get(ctx, monitorID)
}

func (s *Service) ListChecks(ctx context.Context, subjectID, monitorID string, limit int) ([]Check, error) {
	if err := s.authorize(ctx, subjectID, ActionRead, "", "monitor_checks"); err != nil {
		return nil, err
	}
	if _, err := s.repo.Get(ctx, monitorID); err != nil {
		return nil, err
	}
	return s.repo.ListChecks(ctx, monitorID, limit)
}

// Create persists one validated monitor. Target validation includes the SSRF
// boundary and any explicit allowed-host override.
func (s *Service) Create(ctx context.Context, subjectID, requestID string, input MonitorInput) (Monitor, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Monitor{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionWrite, requestID, "monitor_create"); err != nil {
		return Monitor{}, err
	}
	if err := s.validateSource(ctx, subjectID, input.SourceNodeID); err != nil {
		return Monitor{}, err
	}
	if err := validateMonitorInput(input, s.policy); err != nil {
		return Monitor{}, err
	}
	now := s.now()
	monitor := Monitor{SourceNodeID: input.SourceNodeID, FailureThreshold: normalizeThreshold(input.FailureThreshold), RecoveryThreshold: normalizeThreshold(input.RecoveryThreshold), NotifyChannels: input.NotifyChannels,
		ID: input.ID, Name: input.Name, Kind: input.Kind, Target: input.Target,
		IntervalSeconds: input.IntervalSeconds, TimeoutSeconds: input.TimeoutSeconds,
		Enabled: input.Enabled, MaintenanceUntil: input.MaintenanceUntil,
		State: StateUnknown, CreatedAt: now, UpdatedAt: now,
	}
	err := s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.Create(txCtx, tx, monitor); err != nil {
			return err
		}
		rev, e := s.bindAutomationTx(txCtx, tx, monitor.ID, subjectID)
		if e != nil {
			return e
		}
		monitor.AutomationOwnerID = subjectID
		monitor.AutomationRevision = rev
		if e = s.bindMonitorOptions(txCtx, tx, monitor); e != nil {
			return e
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionCreate, "success", requestID, "monitor_created"))
	})
	if err != nil {
		return Monitor{}, err
	}
	return monitor, nil
}

// Update replaces mutable monitor fields. State, failure count and the last sample
// are preserved so a configuration edit cannot fabricate a transition.
func (s *Service) Update(ctx context.Context, subjectID, requestID, monitorID string, input MonitorInput) (Monitor, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Monitor{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionWrite, requestID, "monitor_update"); err != nil {
		return Monitor{}, err
	}
	if input.ID == "" {
		input.ID = monitorID
	}
	if input.ID != monitorID {
		return Monitor{}, ErrInvalidMonitor
	}
	current, err := s.repo.Get(ctx, monitorID)
	if err != nil {
		return Monitor{}, err
	}
	if err := s.validateSource(ctx, subjectID, input.SourceNodeID); err != nil {
		return Monitor{}, err
	}
	if err := validateMonitorInput(input, s.policy); err != nil {
		return Monitor{}, err
	}
	updated := current
	updated.FailureThreshold = normalizeThreshold(input.FailureThreshold)
	updated.RecoveryThreshold = normalizeThreshold(input.RecoveryThreshold)
	updated.NotifyChannels = input.NotifyChannels
	updated.ConsecutiveFailures = 0
	updated.ConsecutiveRecoveries = 0
	if input.Target != current.Target || input.Kind != current.Kind || input.SourceNodeID != current.SourceNodeID {
		updated.State = StateUnknown
	}
	updated.SourceNodeID = input.SourceNodeID
	updated.LastProbeUnknown = false
	updated.LastProbeTaskID = ""
	updated.Name = input.Name
	updated.Kind = input.Kind
	updated.Target = input.Target
	updated.IntervalSeconds = input.IntervalSeconds
	updated.TimeoutSeconds = input.TimeoutSeconds
	updated.Enabled = input.Enabled
	updated.MaintenanceUntil = input.MaintenanceUntil
	updated.UpdatedAt = s.now()
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.Update(txCtx, tx, updated); err != nil {
			return err
		}
		rev, e := s.bindAutomationTx(txCtx, tx, updated.ID, subjectID)
		if e != nil {
			return e
		}
		updated.AutomationOwnerID = subjectID
		updated.AutomationRevision = rev
		if e = s.bindMonitorOptions(txCtx, tx, updated); e != nil {
			return e
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionUpdate, "success", requestID, "monitor_updated"))
	})
	if err != nil {
		return Monitor{}, err
	}
	return updated, nil
}

func (s *Service) Delete(ctx context.Context, subjectID, requestID, monitorID string) error {
	if err := s.requireRequest(requestID); err != nil {
		return err
	}
	if err := s.authorize(ctx, subjectID, ActionWrite, requestID, "monitor_delete"); err != nil {
		return err
	}
	return s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.Delete(txCtx, tx, monitorID); err != nil {
			return err
		}
		if _, e := tx.Exec(txCtx, `DELETE FROM monitor_automation WHERE monitor_id=?`, monitorID); e != nil {
			return e
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionDelete, "success", requestID, "monitor_deleted"))
	})
}

// RunCheckNow performs one authorized immediate probe, persists the sample and
// updates state. A nil Prober returns ErrProberUnavailable without mutation.
func (s *Service) RunCheckNow(ctx context.Context, subjectID, requestID, monitorID string) (Check, Monitor, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Check{}, Monitor{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionWrite, requestID, "monitor_check"); err != nil {
		return Check{}, Monitor{}, err
	}
	if s.prober == nil && s.remoteProber == nil {
		return Check{}, Monitor{}, ErrProberUnavailable
	}
	monitor, err := s.repo.Get(ctx, monitorID)
	if err != nil {
		return Check{}, Monitor{}, err
	}
	if e := s.validateSource(ctx, subjectID, monitor.SourceNodeID); e != nil {
		return Check{}, Monitor{}, e
	}
	check, updated, alert, _, err := s.runCheck(ctx, monitor, s.now(), subjectID, true)
	if err != nil {
		return Check{}, Monitor{}, err
	}
	auditEvent := s.event(subjectID, ActionCheck, "success", requestID, string(updated.State))
	if err := s.recordCheck(ctx, check, updated, &auditEvent, alert); err != nil {
		return Check{}, Monitor{}, err
	}
	if alert != nil {
		_ = s.FlushAlerts(ctx, 50)
	}
	return check, updated, nil
}

// RunDue checks every enabled monitor whose interval has elapsed. Maintenance
// windows record checks but keep state and suppress alerts.
func (s *Service) RunDue(ctx context.Context, now time.Time) ([]RunResult, error) {
	if s.prober == nil && s.remoteProber == nil {
		return nil, ErrProberUnavailable
	}
	if now.IsZero() {
		return nil, ErrInvalidNow
	}
	if s.notifier != nil {
		_ = s.FlushAlerts(ctx, 50)
	}
	monitors, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	results := make([]RunResult, 0, len(monitors))
	var firstErr error
	for n := 0; n < len(monitors) && n < 50; n++ {
		s.cursor.Lock()
		index := s.cursor.next % len(monitors)
		s.cursor.next = (index + 1) % len(monitors)
		s.cursor.Unlock()
		monitor := monitors[index]
		if err := ctx.Err(); err != nil {
			return results, err
		}
		if s.automationCurrent != nil && s.automationCurrent(ctx, monitor) != nil {
			continue
		}
		if !monitor.Enabled || !due(monitor, now) {
			continue
		}
		check, updated, alert, maintenance, err := s.runCheck(ctx, monitor, now, monitor.AutomationOwnerID, false)
		if err != nil {
			if ctx.Err() != nil {
				return results, ctx.Err()
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := s.recordCheck(ctx, check, updated, nil, alert); err != nil {
			return results, err
		}
		resultState := updated.State
		if updated.LastProbeUnknown {
			resultState = StateUnknown
		}
		result := RunResult{MonitorID: monitor.ID, Check: check, State: resultState, Maintenance: maintenance}
		if alert != nil {
			if err := s.FlushAlerts(ctx, 50); err != nil {
				if firstErr == nil {
					firstErr = err
				}
			} else {
				result.Alerted = s.transitionAccepted(ctx, updated, alert.DedupeKey)
			}
		}
		results = append(results, result)
	}
	return results, firstErr
}

func (s *Service) runCheck(ctx context.Context, monitor Monitor, now time.Time, actor string, manual bool) (Check, Monitor, *notifications.Alert, bool, error) {
	if s.automationCurrent != nil && s.automationCurrent(ctx, monitor) != nil {
		return Check{}, Monitor{}, nil, false, ErrObservationChanged
	}
	if monitor.SourceNodeID == "" && s.prober == nil {
		return Check{}, Monitor{}, nil, false, ErrProberUnavailable
	}
	if err := s.policy.Validate(monitor.Kind, monitor.Target); err != nil {
		return Check{}, Monitor{}, nil, false, err
	}
	spec := TargetSpec{
		Kind: monitor.Kind, Target: monitor.Target,
		Timeout: time.Duration(monitor.TimeoutSeconds) * time.Second,
	}
	budget := spec.Timeout
	if monitor.SourceNodeID != "" {
		budget += 8 * time.Second
	}
	probeCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	var (
		result   ProbeResult
		probeErr error
	)
	if monitor.SourceNodeID != "" {
		if s.remoteProber == nil {
			result = ProbeResult{Unknown: true, Reason: "probe_source_unavailable"}
		} else {
			result, probeErr = s.remoteProber.Probe(probeCtx, monitor, actor, manual)
			if probeErr != nil {
				result = ProbeResult{Unknown: true, TaskID: result.TaskID, Reason: "probe_source_unavailable"}
				probeErr = nil
			}
		}
	} else {
		switch monitor.Kind {
		case KindHTTP:
			result, probeErr = s.prober.HTTP(probeCtx, spec)
		case KindTCP:
			result, probeErr = s.prober.TCP(probeCtx, spec)
		case KindTLS:
			result, probeErr = s.prober.TLS(probeCtx, spec)
		default:
			return Check{}, Monitor{}, nil, false, ErrInvalidMonitor
		}
	}
	if err := ctx.Err(); err != nil {
		return Check{}, Monitor{}, nil, false, err
	}
	if probeErr != nil {
		result = ProbeResult{Healthy: false, Reason: sanitizeProbeText(probeErr.Error())}
	}
	if err := validateProbeResult(result); err != nil {
		return Check{}, Monitor{}, nil, false, err
	}
	healthy := result.Healthy && probeErr == nil
	status := StateDown
	reason := ""
	if healthy {
		status = StateUp
	} else {
		if probeErr != nil {
			reason = sanitizeProbeText(probeErr.Error())
		} else {
			reason = sanitizeProbeText(result.Reason)
		}
	}
	check := Check{
		MonitorID: monitor.ID, Status: status, Error: reason, CheckedAt: now, SourceNodeID: monitor.SourceNodeID, TaskID: result.TaskID,
	}
	if result.Latency != nil {
		latency := *result.Latency
		check.Latency = &latency
	}
	if result.StatusCode != nil {
		code := *result.StatusCode
		check.StatusCode = &code
	}
	updated := monitor
	updated.LastCheckedAt = now
	updated.UpdatedAt = now
	updated.LastError = reason
	updated.LastProbeUnknown = result.Unknown
	updated.LastProbeTaskID = result.TaskID
	if result.Unknown {
		check.Status = StateUnknown
		check.Error = sanitizeProbeText(result.Reason)
		updated.LastError = check.Error
		updated.ConsecutiveFailures = 0
		updated.ConsecutiveRecoveries = 0
		return check, updated, nil, false, nil
	}
	inMaintenance := !monitor.MaintenanceUntil.IsZero() && now.Before(monitor.MaintenanceUntil)
	if inMaintenance {
		return check, updated, nil, true, nil
	}
	previous := monitor.State
	var alert *notifications.Alert
	if healthy {
		updated.ConsecutiveFailures = 0
		if updated.ConsecutiveRecoveries < 100 {
			updated.ConsecutiveRecoveries++
		}
		if updated.ConsecutiveRecoveries >= normalizeThreshold(monitor.RecoveryThreshold) {
			updated.State = StateUp
			if previous == StateDown {
				alert = s.transitionAlert(updated, previous, StateUp, check, now)
			}
		}
	} else {
		updated.ConsecutiveRecoveries = 0
		if updated.ConsecutiveFailures < math.MaxInt {
			updated.ConsecutiveFailures++
		}
		if updated.ConsecutiveFailures >= normalizeThreshold(monitor.FailureThreshold) {
			updated.State = StateDown
			if previous != StateDown {
				alert = s.transitionAlert(updated, previous, StateDown, check, now)
			}
		}
	}
	return check, updated, alert, false, nil
}

func (s *Service) transitionAlert(monitor Monitor, from, to State, check Check, now time.Time) *notifications.Alert {
	state := "down"
	if to == StateUp {
		state = "recovered"
	}
	body := fmt.Sprintf("Monitor %s (%s) changed from %s to %s. Target: %s.", monitor.Name, monitor.ID, from, to, monitor.Target)
	if to == StateDown && check.Error != "" {
		body += " Probe: " + check.Error
	}
	return &notifications.Alert{
		DedupeKey: fmt.Sprintf("monitoring:%s:r%d:o%d:%s", monitor.ID, monitor.AutomationRevision, monitor.ObservationRevision+1, to),
		Subject:   fmt.Sprintf("Monitor %s is %s", monitor.Name, state),
		Body:      body,
	}
}

func (s *Service) recordCheck(ctx context.Context, check Check, m Monitor, event *audit.Event, alert *notifications.Alert) error {
	var writer func(context.Context, storage.Tx) error
	if event != nil {
		writer = func(c context.Context, tx storage.Tx) error { return s.audit.AppendTx(c, tx, *event) }
	}
	return s.saveObservation(ctx, check, m, alert, writer)
}

func due(monitor Monitor, now time.Time) bool {
	if monitor.IntervalSeconds < 1 {
		return false
	}
	if monitor.LastCheckedAt.IsZero() {
		return true
	}
	return !now.Before(monitor.LastCheckedAt.Add(time.Duration(monitor.IntervalSeconds) * time.Second))
}

func (s *Service) requireRequest(requestID string) error {
	if s == nil {
		return ErrServiceNotConfigured
	}
	if !validText(requestID, audit.DefaultLimits().MaxRequestIDBytes) {
		return ErrRequestIDRequired
	}
	return nil
}

func (s *Service) authorize(ctx context.Context, subjectID, action, requestID, reason string) error {
	if s == nil {
		return ErrServiceNotConfigured
	}
	check := auth.Check{SubjectID: subjectID, Action: action}
	err := s.authorizer.Authorize(ctx, check)
	if err == nil {
		return nil
	}
	if isPureDenial(err) && requestID != "" {
		denial := s.event(subjectID, action, "denied", requestID, reason)
		auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if auditErr := s.audit.Append(auditCtx, denial); auditErr != nil {
			return fmt.Errorf("monitoring: authorization denied (denial audit failed: %w)", auditErr)
		}
	}
	return err
}

func (s *Service) event(subjectID, action, result, requestID, reason string) audit.Event {
	return audit.Event{
		SubjectID: subjectID, Action: action, Result: result, RequestID: requestID,
		Source: audit.SourceWeb, ReasonCode: reason, At: s.now(),
	}
}

func isPureDenial(err error) bool {
	for depth := 0; err != nil; depth++ {
		if err == auth.ErrDenied {
			return true
		}
		if depth >= 64 {
			return false
		}
		if _, multi := err.(interface{ Unwrap() []error }); multi {
			return false
		}
		single, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = single.Unwrap()
	}
	return false
}
