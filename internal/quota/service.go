package quota

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/notifications"
	"serverfleet.local/serverfleet/internal/storage"
)

const (
	ActionRead     = "quota.read"
	ActionWrite    = "quota.write"
	ActionPolicy   = "quota.policy"
	ActionReminder = "quota.reminder"
)

// ServiceActions returns the audit action catalog this domain emits.
func ServiceActions() []string {
	return []string{ActionPolicy, ActionRead, ActionReminder, ActionWrite}
}

// AuditSink is the durable audit port; policy writes append in the same
// transaction as the stored policy.
type AuditSink interface {
	Append(context.Context, audit.Event) error
	AppendTx(context.Context, storage.Tx, audit.Event) error
}

// Options configures the quota service. DB, Repo, Nodes, Audit and Authorizer are
// mandatory. A nil Notifier makes RunReminders fail closed instead of pretending
// an alert was sent.
type Options struct {
	DB                *storage.DB
	Repo              *Repo
	Nodes             *nodes.Repo
	Audit             AuditSink
	Authorizer        auth.Authorizer
	Notifier          Notifier
	Now               func() time.Time
	AutomationCurrent AutomationAuthority
}

type Service struct {
	db                *storage.DB
	repo              *Repo
	nodes             *nodes.Repo
	audit             AuditSink
	authorizer        auth.Authorizer
	notifier          Notifier
	now               func() time.Time
	automationCurrent AutomationAuthority
}

func NewService(options Options) (*Service, error) {
	switch {
	case options.DB == nil:
		return nil, errors.New("quota: database is required")
	case options.Repo == nil:
		return nil, errors.New("quota: repository is required")
	case options.Nodes == nil:
		return nil, errors.New("quota: node repository is required")
	case options.Audit == nil:
		return nil, errors.New("quota: audit sink is required")
	case options.Authorizer == nil:
		return nil, ErrMissingAuthorizer
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		db: options.DB, repo: options.Repo, nodes: options.Nodes, audit: options.Audit,
		authorizer: options.Authorizer, notifier: options.Notifier, now: now, automationCurrent: options.AutomationCurrent,
	}, nil
}

// RecordUsage accepts one trusted traffic observation. It verifies that the node
// exists, rejects negative or non-finite values, and rejects a decrease from an
// agent estimate. Billing and explicit manual corrections may reduce the stored
// period. There is deliberately no auto-disable or block path.
func (s *Service) RecordUsage(ctx context.Context, sample UsageSample) (Usage, error) {
	if s == nil {
		return Usage{}, ErrServiceNotConfigured
	}
	now := s.now()
	if sample.ObservedAt.IsZero() {
		sample.ObservedAt = now
	}
	if sample.ObservedAt.After(now.Add(5 * time.Minute)) {
		return Usage{}, ErrInvalidUsage
	}
	sample.PeriodStart = sample.PeriodStart.UTC()
	sample.PeriodEnd = sample.PeriodEnd.UTC()
	sample.ObservedAt = sample.ObservedAt.UTC()
	if err := validateUsageSample(sample); err != nil {
		return Usage{}, err
	}
	if _, err := s.nodes.Get(ctx, sample.NodeID); err != nil {
		if errors.Is(err, nodes.ErrNodeNotFound) {
			return Usage{}, ErrUnknownNode
		}
		return Usage{}, err
	}
	usage := Usage{
		NodeID: sample.NodeID, PeriodStart: sample.PeriodStart, PeriodEnd: sample.PeriodEnd,
		BytesIn: sample.BytesIn, BytesOut: sample.BytesOut, Source: sample.Source, UpdatedAt: now,
	}
	err := s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		current, err := s.repo.GetPeriod(txCtx, tx, sample.NodeID, sample.PeriodStart)
		switch {
		case err == nil:
			if !sample.Source.allowsDecrease() &&
				(sample.BytesIn < current.BytesIn || sample.BytesOut < current.BytesOut) {
				return ErrNonMonotonic
			}
		case errors.Is(err, ErrUsageNotFound):
		default:
			return err
		}
		if err := s.repo.UpsertUsage(txCtx, tx, usage); err != nil {
			return err
		}
		return s.repo.PutSample(txCtx, tx, sample, now)
	})
	if err != nil {
		return Usage{}, err
	}
	return usage, nil
}

// GetUsage returns found=false when no sample exists. The zero Usage in that case
// is not a measurement and must never be rendered as zero usage.
func (s *Service) GetUsage(ctx context.Context, subjectID, nodeID string, periodStart time.Time) (Usage, bool, error) {
	if err := s.validateLookup(nodeID, periodStart); err != nil {
		return Usage{}, false, err
	}
	if err := s.authorizeRead(ctx, subjectID, nodeID, "usage_read"); err != nil {
		return Usage{}, false, err
	}
	usage, err := s.repo.GetUsage(ctx, nodeID, periodStart.UTC())
	if errors.Is(err, ErrUsageNotFound) {
		return Usage{}, false, nil
	}
	if err != nil {
		return Usage{}, false, err
	}
	return usage, true, nil
}

// LatestUsage returns the most recent stored period for one node. found=false
// means no sample exists and must never be rendered as zero usage.
func (s *Service) LatestUsage(ctx context.Context, subjectID, nodeID string) (Usage, bool, error) {
	if !validToken(nodeID, 128) {
		return Usage{}, false, ErrInvalidUsage
	}
	if err := s.authorizeRead(ctx, subjectID, nodeID, "usage_latest"); err != nil {
		return Usage{}, false, err
	}
	usage, err := s.repo.LatestUsage(ctx, nodeID)
	if errors.Is(err, ErrUsageNotFound) {
		return Usage{}, false, nil
	}
	if err != nil {
		return Usage{}, false, err
	}
	return usage, true, nil
}

func (s *Service) GetPolicy(ctx context.Context, subjectID, nodeID string) (Policy, bool, error) {
	if !validToken(nodeID, 128) {
		return Policy{}, false, ErrInvalidPolicy
	}
	if err := s.authorizeRead(ctx, subjectID, nodeID, "policy_read"); err != nil {
		return Policy{}, false, err
	}
	policy, err := s.repo.GetPolicy(ctx, nodeID)
	if errors.Is(err, ErrPolicyNotFound) {
		return Policy{}, false, nil
	}
	if err != nil {
		return Policy{}, false, err
	}
	return policy, true, nil
}

// ListPolicies filters stored policies by the subject's per-node read grants. A
// pure denial hides that node; a policy or evaluation failure aborts the list.
func (s *Service) ListPolicies(ctx context.Context, subjectID string) ([]Policy, error) {
	policies, err := s.repo.ListPolicies(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Policy, 0, len(policies))
	for _, policy := range policies {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := s.authorizeRead(ctx, subjectID, policy.NodeID, "policy_list"); err != nil {
			if isPureDenial(err) {
				continue
			}
			return nil, err
		}
		out = append(out, policy)
	}
	return out, nil
}

// UpsertPolicy writes per-node quota policy. Exceeding it later only produces an
// alert recommendation; this package never changes node status.
func (s *Service) UpsertPolicy(ctx context.Context, subjectID, requestID string, input PolicyInput) (Policy, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Policy{}, err
	}
	if err := validatePolicyInput(input); err != nil {
		return Policy{}, err
	}
	if err := s.authorizeWrite(ctx, subjectID, requestID, input.NodeID, "quota_policy_write"); err != nil {
		return Policy{}, err
	}
	if _, err := s.nodes.Get(ctx, input.NodeID); err != nil {
		if errors.Is(err, nodes.ErrNodeNotFound) {
			return Policy{}, ErrUnknownNode
		}
		return Policy{}, err
	}
	if input.ResetDay == 0 {
		input.ResetDay = 1
	}
	if input.Timezone == "" {
		input.Timezone = "UTC"
	}
	if input.Direction == "" {
		input.Direction = "combined"
	}
	now := s.now()
	policy := Policy{
		Direction: input.Direction, ThresholdsPct: input.ThresholdsPct, RemindDays: input.RemindDays, ExpiryType: input.ExpiryType,
		NodeID: input.NodeID, MonthlyLimitBytes: input.MonthlyLimitBytes, ResetDay: input.ResetDay, Timezone: input.Timezone,
		WarningPercent: input.WarningPercent, ExpiryAt: input.ExpiryAt.UTC(),
		Estimated: input.Estimated, UpdatedAt: now,
	}
	err := s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.UpsertPolicy(txCtx, tx, policy); err != nil {
			return err
		}
		var err error
		policy, err = s.bindPolicyTx(txCtx, tx, policy, subjectID)
		if err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionPolicy, "success", requestID, input.NodeID, "policy_updated"))
	})
	if err != nil {
		return Policy{}, err
	}
	return policy, nil
}

// Evaluate returns the warning/exceeded/expiring view for one period. Missing
// usage yields Usage=nil rather than a zero-filled sample.
func (s *Service) Evaluate(ctx context.Context, subjectID, nodeID string, periodStart, now time.Time) (Evaluation, error) {
	if err := s.validateLookup(nodeID, periodStart); err != nil {
		return Evaluation{}, err
	}
	if now.IsZero() {
		return Evaluation{}, ErrInvalidNow
	}
	if err := s.authorizeRead(ctx, subjectID, nodeID, "quota_evaluate"); err != nil {
		return Evaluation{}, err
	}
	policy, err := s.repo.GetPolicy(ctx, nodeID)
	if errors.Is(err, ErrPolicyNotFound) {
		return Evaluation{}, ErrPolicyNotFound
	}
	if err != nil {
		return Evaluation{}, err
	}
	usage, err := s.repo.GetUsage(ctx, nodeID, periodStart.UTC())
	if errors.Is(err, ErrUsageNotFound) {
		return evaluate(policy, nil, periodStart, now), nil
	}
	if err != nil {
		return Evaluation{}, err
	}
	return evaluate(policy, &usage, periodStart, now), nil
}

// EstimateExpiry predicts quota exhaustion only when at least two observations
// span at least MinEstimateSpan and show a positive burn rate. Otherwise found is
// false and no date is invented.
func (s *Service) EstimateExpiry(ctx context.Context, subjectID, nodeID string, now time.Time) (ExpiryEstimate, bool, error) {
	if !validToken(nodeID, 128) {
		return ExpiryEstimate{}, false, ErrInvalidPolicy
	}
	if now.IsZero() {
		return ExpiryEstimate{}, false, ErrInvalidNow
	}
	if err := s.authorizeRead(ctx, subjectID, nodeID, "quota_estimate"); err != nil {
		return ExpiryEstimate{}, false, err
	}
	policy, err := s.repo.GetPolicy(ctx, nodeID)
	if errors.Is(err, ErrPolicyNotFound) {
		return ExpiryEstimate{}, false, nil
	}
	if err != nil {
		return ExpiryEstimate{}, false, err
	}
	usage, err := s.repo.LatestUsage(ctx, nodeID)
	if errors.Is(err, ErrUsageNotFound) {
		return ExpiryEstimate{}, false, nil
	}
	if err != nil {
		return ExpiryEstimate{}, false, err
	}
	count, err := s.repo.CountSamples(ctx, s.db, nodeID, usage.PeriodStart)
	if err != nil {
		return ExpiryEstimate{}, false, err
	}
	if count < MinEstimateSamples {
		return ExpiryEstimate{}, false, nil
	}
	points, err := s.repo.ListRecentSamples(ctx, s.db, nodeID, usage.PeriodStart, 1)
	if err != nil {
		return ExpiryEstimate{}, false, err
	}
	if len(points) < 1 {
		return ExpiryEstimate{}, false, nil
	}
	latest := points[0]
	earlier, err := s.repo.FirstSample(ctx, s.db, nodeID, usage.PeriodStart)
	if errors.Is(err, ErrUsageNotFound) {
		return ExpiryEstimate{}, false, nil
	}
	if err != nil {
		return ExpiryEstimate{}, false, err
	}
	span := latest.Observed.Sub(earlier.Observed)
	if span < MinEstimateSpan {
		return ExpiryEstimate{}, false, nil
	}
	latestTotal := policy.used(Usage{BytesIn: latest.BytesIn, BytesOut: latest.BytesOut})
	earlierTotal := policy.used(Usage{BytesIn: earlier.BytesIn, BytesOut: earlier.BytesOut})
	if latestTotal <= earlierTotal {
		return ExpiryEstimate{}, false, nil
	}
	ratePerSecond := (latestTotal - earlierTotal) / span.Seconds()
	bytesPerDay := ratePerSecond * 86400
	if math.IsNaN(bytesPerDay) || math.IsInf(bytesPerDay, 0) || bytesPerDay <= 0 {
		return ExpiryEstimate{}, false, nil
	}
	remaining := float64(policy.MonthlyLimitBytes) - latestTotal
	exhaustedAt := now
	if remaining > 0 {
		seconds := remaining / ratePerSecond
		// Bound the prediction to a useful horizon and avoid Duration overflow.
		if seconds > float64(10*366*24*3600) {
			return ExpiryEstimate{}, false, nil
		}
		exhaustedAt = now.Add(time.Duration(seconds * float64(time.Second)))
	}
	return ExpiryEstimate{
		NodeID: nodeID, PeriodStart: usage.PeriodStart, ExhaustedAt: exhaustedAt,
		BytesPerDay: bytesPerDay, Samples: count, From: earlier.Observed, To: latest.Observed,
		Estimated: policy.Estimated,
	}, true, nil
}

// RunReminders emits warning/exceeded/expiring alerts for nodes with real usage.
// Each dedupe key is node+period+threshold; missing usage produces no alert and no
// zero-filled value. Delivering reminders never blocks or disables networking.
func (s *Service) RunReminders(ctx context.Context, now time.Time) ([]Reminder, error) {
	if s == nil {
		return nil, ErrServiceNotConfigured
	}
	if s.notifier == nil {
		return nil, ErrNotifierUnavailable
	}
	if now.IsZero() {
		return nil, ErrInvalidNow
	}
	policies, err := s.repo.ListPolicies(ctx)
	if err != nil {
		return nil, err
	}
	reminders := make([]Reminder, 0)
	var firstErr error
	for _, policy := range policies {
		if err := ctx.Err(); err != nil {
			return reminders, err
		}
		if s.automationCurrent != nil && s.automationCurrent(ctx, policy) != nil {
			continue
		}
		period, _, periodErr := PolicyPeriod(now, policy)
		if periodErr != nil {
			return reminders, periodErr
		}
		usage, err := s.repo.LatestUsage(ctx, policy.NodeID)
		if errors.Is(err, ErrUsageNotFound) {
			usage = Usage{}
		} else if err != nil {
			return reminders, err
		}
		var measured *Usage
		if !usage.PeriodStart.IsZero() && !now.Before(usage.PeriodStart) && now.Before(usage.PeriodEnd) {
			measured = &usage
			period = usage.PeriodStart
		}
		evaluation := evaluate(policy, measured, period, now)
		type candidate struct {
			level  Level
			suffix string
		}
		candidates := []candidate{}
		if measured != nil {
			if len(policy.ThresholdsPct) > 0 {
				// At a large jump, send only the highest crossed threshold; lower
				// thresholds cannot become newly relevant as usage increases.
				highest := 0
				for _, pct := range policy.ThresholdsPct {
					if evaluation.PercentUsed >= float64(pct) {
						highest = pct
					}
				}
				if highest > 0 {
					level := LevelWarning
					if highest == 100 {
						level = LevelExceeded
					}
					candidates = append(candidates, candidate{level, fmt.Sprintf(":pct-%d", highest)})
				}
			} else if evaluation.Level == LevelExceeded || evaluation.Level == LevelWarning {
				candidates = append(candidates, candidate{level: evaluation.Level})
			}
		}
		if evaluation.Expiring {
			suffix := ""
			if len(policy.RemindDays) > 0 {
				day := -1
				for _, d := range policy.RemindDays {
					if !now.Before(policy.ExpiryAt.Add(-time.Duration(d) * 24 * time.Hour)) {
						day = d
					}
				}
				if day >= 0 {
					suffix = fmt.Sprintf(":days-%d", day)
				}
			}
			candidates = append(candidates, candidate{LevelExpiring, suffix})
		}
		for _, candidate := range candidates {
			threshold := candidate.level
			alert := reminderAlert(evaluation, threshold)
			alert.DedupeKey += candidate.suffix
			key := alert.DedupeKey
			exists, err := s.repo.HasReminder(ctx, s.db, key)
			if err != nil {
				return reminders, err
			}
			if exists {
				continue
			}
			delivery, sendErr := s.notifier.SendAlert(ctx, alert)
			if sendErr != nil {
				if firstErr == nil {
					firstErr = ErrNotifyFailed
				}
				continue
			}
			if delivery.Status != notifications.DeliveryProviderAccepted && !(delivery.Status == notifications.DeliverySubmitted && delivery.ProviderCode != "attempt_pending") && !(s.automationCurrent == nil && delivery.Status == "") {
				continue
			}
			reminder := Reminder{
				NodeID: policy.NodeID, PeriodStart: period,
				DedupeKey: key, Threshold: threshold, SentAt: now,
			}
			auditEvent := audit.Event{
				SubjectID: "quota-reminders", Action: ActionReminder, Result: "success",
				RequestID: fmt.Sprintf("reminder-%d", now.Unix()), NodeIDs: []string{policy.NodeID},
				Source: audit.SourceSystem, ReasonCode: string(threshold), At: s.now(),
			}
			if err := s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
				if err := s.repo.MarkReminder(txCtx, tx, reminder); err != nil {
					return err
				}
				return s.audit.AppendTx(txCtx, tx, auditEvent)
			}); err != nil {
				return reminders, err
			}
			reminders = append(reminders, reminder)
		}
	}
	return reminders, firstErr
}

func evaluate(policy Policy, usage *Usage, periodStart, now time.Time) Evaluation {
	evaluation := Evaluation{
		NodeID: policy.NodeID, PeriodStart: periodStart.UTC(), Usage: usage, Policy: &policy,
		ExpiryAt: policy.ExpiryAt,
	}
	if usage != nil {
		evaluation.PeriodStart = usage.PeriodStart
		evaluation.TotalBytes = policy.used(*usage)
		if policy.MonthlyLimitBytes > 0 {
			evaluation.PercentUsed = evaluation.TotalBytes / float64(policy.MonthlyLimitBytes) * 100
		}
		switch {
		case evaluation.PercentUsed >= 100:
			evaluation.Level = LevelExceeded
		case evaluation.PercentUsed >= float64(policy.WarningPercent):
			evaluation.Level = LevelWarning
		default:
			evaluation.Level = LevelOK
		}
	} else {
		evaluation.Level = LevelOK
	}
	if !policy.ExpiryAt.IsZero() {
		window := ExpiryWarningWindow
		if len(policy.RemindDays) > 0 {
			window = time.Duration(policy.RemindDays[0]) * 24 * time.Hour
		}
		evaluation.Expiring = !now.Before(policy.ExpiryAt.Add(-window))
		evaluation.Expired = !now.Before(policy.ExpiryAt)
		if usage != nil && evaluation.Level == LevelOK && evaluation.Expiring {
			evaluation.Level = LevelExpiring
		}
	}
	evaluation.AlertRecommended = evaluation.Level != LevelOK || evaluation.Expiring
	return evaluation
}

func reminderKey(nodeID string, periodStart time.Time, threshold Level) string {
	return fmt.Sprintf("quota:%s:%s:%s", nodeID,
		periodStart.UTC().Format("20060102T150405Z"), threshold)
}

func reminderAlert(evaluation Evaluation, threshold Level) notifications.Alert {
	subject := fmt.Sprintf("Traffic quota %s on %s", threshold, evaluation.NodeID)
	body := fmt.Sprintf("Node %s used %.0f of %d bytes (%.1f%%) in the period starting %s.",
		evaluation.NodeID, evaluation.TotalBytes, evaluation.Policy.MonthlyLimitBytes,
		evaluation.PercentUsed, evaluation.PeriodStart.UTC().Format(time.RFC3339))
	if evaluation.Usage == nil {
		body = fmt.Sprintf("Node %s has no current traffic measurement; no zero usage is implied.", evaluation.NodeID)
	}
	if threshold == LevelExpiring && !evaluation.ExpiryAt.IsZero() {
		body += fmt.Sprintf(" Quota expiry: %s.", evaluation.ExpiryAt.UTC().Format(time.RFC3339))
	}
	return notifications.Alert{
		DedupeKey: quotaAlertKey(evaluation, threshold),
		Source:    notifications.AlertSource{Kind: "quota", ID: evaluation.NodeID, OwnerID: evaluation.Policy.AutomationOwnerID, Revision: evaluation.Policy.AutomationRevision},
		Subject:   subject,
		Body:      body,
	}
}

func (s *Service) validateLookup(nodeID string, periodStart time.Time) error {
	if !validToken(nodeID, 128) {
		return ErrInvalidUsage
	}
	if periodStart.IsZero() {
		return ErrInvalidPeriod
	}
	return nil
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

func (s *Service) authorizeRead(ctx context.Context, subjectID, nodeID, reason string) error {
	return s.authorize(ctx, subjectID, auth.NodeReadAction, nodeID, "", reason)
}

func (s *Service) authorizeWrite(ctx context.Context, subjectID, requestID, nodeID, reason string) error {
	return s.authorize(ctx, subjectID, ActionWrite, nodeID, requestID, reason)
}

func (s *Service) authorize(ctx context.Context, subjectID, action, nodeID, requestID, reason string) error {
	if s == nil {
		return ErrServiceNotConfigured
	}
	check := auth.Check{SubjectID: subjectID, Action: action, NodeIDs: []string{nodeID}}
	err := s.authorizer.Authorize(ctx, check)
	if err == nil {
		return nil
	}
	if isPureDenial(err) && requestID != "" {
		denial := s.event(subjectID, action, "denied", requestID, nodeID, reason)
		auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if auditErr := s.audit.Append(auditCtx, denial); auditErr != nil {
			return fmt.Errorf("quota: authorization denied (denial audit failed: %w)", auditErr)
		}
	}
	return err
}

func (s *Service) event(subjectID, action, result, requestID, nodeID, reason string) audit.Event {
	nodeIDs := []string{}
	if nodeID != "" {
		nodeIDs = append(nodeIDs, nodeID)
	}
	return audit.Event{
		SubjectID: subjectID, Action: action, Result: result, RequestID: requestID,
		NodeIDs: nodeIDs, Source: audit.SourceWeb, ReasonCode: reason, At: s.now(),
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

func quotaAlertKey(e Evaluation, t Level) string {
	if t == LevelExpiring {
		return "quota:" + e.NodeID + ":expiring:" + e.ExpiryAt.UTC().Format("20060102T150405Z")
	}
	return reminderKey(e.NodeID, e.PeriodStart, t)
}
