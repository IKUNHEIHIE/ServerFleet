package publishing

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/storage"
)

const (
	ActionRead        = "publishing.read"
	ActionWrite       = "publishing.write"
	ActionCreate      = "publishing.plan_create"
	ActionStage       = "publishing.stage"
	ActionRun         = "publishing.run"
	ActionRollback    = "publishing.rollback"
	ActionRollbackAll = "publishing.rollback_all"
	ActionCancel      = "publishing.cancel"
)

// ServiceActions returns the audit action catalog this domain emits.
func ServiceActions() []string {
	return []string{ActionCancel, ActionCreate, ActionRead, ActionRollback, ActionRollbackAll, ActionRun, ActionStage, ActionWrite}
}

// AuditSink is the durable audit port. Final mutations append in the same
// transaction.
type AuditSink interface {
	Append(context.Context, audit.Event) error
	AppendTx(context.Context, storage.Tx, audit.Event) error
}

// Options configures the publication service. Every dependency is required; a
// nil port fails construction instead of being treated as a successful no-op.
type Options struct {
	DB           *storage.DB
	Repo         *Repo
	Groups       GroupLookup
	DNS          DNSPublisher
	Certificates CertificatePublisher
	Gateway      GatewayPublisher
	Audit        AuditSink
	Authorizer   auth.Authorizer
	Now          func() time.Time
}

type Service struct {
	db           *storage.DB
	repo         *Repo
	groups       GroupLookup
	dns          DNSPublisher
	certificates CertificatePublisher
	gateway      GatewayPublisher
	audit        AuditSink
	authorizer   auth.Authorizer
	now          func() time.Time

	planLocks sync.Map
}

func NewService(options Options) (*Service, error) {
	switch {
	case options.DB == nil:
		return nil, errors.New("publishing: database is required")
	case options.Repo == nil:
		return nil, errors.New("publishing: repository is required")
	case options.Groups == nil:
		return nil, errors.New("publishing: group lookup is required")
	case options.DNS == nil:
		return nil, errors.New("publishing: dns publisher is required")
	case options.Certificates == nil:
		return nil, errors.New("publishing: certificate publisher is required")
	case options.Gateway == nil:
		return nil, errors.New("publishing: gateway publisher is required")
	case options.Audit == nil:
		return nil, errors.New("publishing: audit sink is required")
	case options.Authorizer == nil:
		return nil, ErrMissingAuthorizer
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		db: options.DB, repo: options.Repo, groups: options.Groups,
		dns: options.DNS, certificates: options.Certificates, gateway: options.Gateway,
		audit: options.Audit, authorizer: options.Authorizer, now: now,
	}, nil
}

// Create validates the service group revision, persists the plan and its three
// pending stages in one transaction. A repeated idempotency key returns the
// original plan.
func (s *Service) Create(ctx context.Context, subjectID, requestID string, input CreateInput) (Plan, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Plan{}, err
	}
	normalized, err := normalizeCreateInput(input)
	if err != nil {
		return Plan{}, err
	}
	group, err := s.lookupGroup(ctx, normalized.ServiceGroupID)
	if err != nil {
		return Plan{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionWrite, requestID, group.NodeIDs, "publishing_plan_create"); err != nil {
		return Plan{}, err
	}
	if normalized.IdempotencyKey != "" {
		existing, _, found, err := s.repo.GetByIdempotencyKey(ctx, subjectID, normalized.IdempotencyKey)
		if err != nil {
			return Plan{}, err
		}
		if found {
			return existing, nil
		}
	}
	if group.Revision != normalized.ExpectedGroupRevision {
		return Plan{}, ErrRevisionConflict
	}
	now := s.now().UTC()
	plan := Plan{
		TargetsPersisted: s.repo.TargetsPersisted(),
		ID:               normalized.ID, Name: normalized.Name, Domain: normalized.Domain,
		ServiceGroupID: normalized.ServiceGroupID, ExpectedGroupRevision: normalized.ExpectedGroupRevision,
		GatewayIDs: append([]string(nil), normalized.GatewayIDs...), ZoneID: normalized.ZoneID,
		CertificateID: normalized.CertificateID, HTTPS: normalized.HTTPS, TargetRevision: normalized.TargetRevision,
		Status: PlanQueued, CurrentStage: StageDNS, CreatedBy: subjectID,
		IdempotencyKey: normalized.IdempotencyKey, CreatedAt: now, UpdatedAt: now,
	}
	stages := newPlanStages(plan.ID)
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.Create(txCtx, tx, plan, stages); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionCreate, "success", requestID, group.NodeIDs, ""))
	})
	if err != nil {
		if errors.Is(err, ErrDuplicatePlan) && normalized.IdempotencyKey != "" {
			existing, _, found, lookupErr := s.repo.GetByIdempotencyKey(ctx, subjectID, normalized.IdempotencyKey)
			if lookupErr != nil {
				return Plan{}, lookupErr
			}
			if found {
				return existing, nil
			}
		}
		return Plan{}, err
	}
	return plan, nil
}

// Cancel stops a plan that has not produced any external stage effect. A plan
// with executed stages must be compensated explicitly; Cancel never claims a
// rollback and never interrupts a provider call in another process.
func (s *Service) Cancel(ctx context.Context, subjectID, requestID, planID string) (Plan, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Plan{}, err
	}
	if !validID(planID) {
		return Plan{}, ErrPlanNotFound
	}
	unlock := s.lockPlan(planID)
	defer unlock()
	plan, stages, err := s.repo.Get(ctx, planID)
	if err != nil {
		return Plan{}, err
	}
	group, err := s.lookupGroup(ctx, plan.ServiceGroupID)
	if err != nil {
		return Plan{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionWrite, requestID, group.NodeIDs, "publishing_cancel"); err != nil {
		return Plan{}, err
	}
	if plan.Status == PlanCanceled {
		return plan, nil
	}
	if plan.Status == PlanCompleted {
		return plan, ErrPlanCompleted
	}
	for _, stage := range stages {
		if stage.Status == StageRunning {
			return plan, ErrStageAlreadyRunning
		}
		if stage.Status.Executed() {
			return plan, ErrPlanNotRunnable
		}
	}
	plan.Status = PlanCanceled
	plan.UpdatedAt = s.now().UTC()
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.UpdatePlan(txCtx, tx, plan); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionCancel, "success", requestID, group.NodeIDs, "plan_canceled"))
	})
	if err != nil {
		return Plan{}, err
	}
	return plan, nil
}

// Get returns one plan and its stages after a read authorization against the
// current service group members.
func (s *Service) Get(ctx context.Context, subjectID, planID string) (Plan, []Stage, error) {
	plan, stages, err := s.repo.Get(ctx, planID)
	if err != nil {
		return Plan{}, nil, err
	}
	group, err := s.lookupGroup(ctx, plan.ServiceGroupID)
	if err != nil {
		return Plan{}, nil, err
	}
	if err := s.authorize(ctx, subjectID, ActionRead, "", group.NodeIDs, "publishing_read"); err != nil {
		return Plan{}, nil, err
	}
	return plan, stages, nil
}

// Preview evaluates the local preconditions a publication draft can verify:
// group existence, member snapshot and revision match. Provider health and a
// configuration diff are not fabricated here.
func (s *Service) Preview(ctx context.Context, subjectID string, input PreviewInput) (Preview, error) {
	if !validID(input.ServiceGroupID) {
		return Preview{}, ErrServiceGroupNotFound
	}
	if _, err := normalizeDomain(input.Domain); err != nil {
		return Preview{}, err
	}
	if input.TargetRevision == 0 {
		input.TargetRevision = 1
	}
	if err := validateTargets(input.GatewayIDs, input.ZoneID, input.CertificateID, input.TargetRevision); err != nil {
		return Preview{}, err
	}
	group, err := s.lookupGroup(ctx, input.ServiceGroupID)
	if err != nil {
		return Preview{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionRead, "", group.NodeIDs, "publishing_preview"); err != nil {
		return Preview{}, err
	}
	membersPresent := len(group.NodeIDs) > 0
	checks := []PreviewCheck{
		{Name: "service_group_exists", Passed: true},
		{Name: "members_present", Passed: membersPresent},
	}
	eligible := membersPresent
	warnings := []string{"DNS, certificate and gateway provider health is not evaluated by this preview"}
	if input.ExpectedGroupRevision > 0 {
		matched := group.Revision == input.ExpectedGroupRevision
		check := PreviewCheck{Name: "revision_matches", Passed: matched}
		if !matched {
			check.Reason = "service group revision changed"
		}
		checks = append(checks, check)
		eligible = eligible && matched
	}
	return Preview{
		Eligible: eligible, GroupRevision: group.Revision,
		TargetNodeIDs: append([]string(nil), group.NodeIDs...), Checks: checks, Warnings: warnings,
	}, nil
}

// ListDetails returns plans the subject may read with their stage rows. A pure
// per-plan denial is filtered; any other authorization or lookup failure aborts
// the list instead of returning partial data.
func (s *Service) ListDetails(ctx context.Context, subjectID string) ([]PlanDetail, error) {
	plans, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	if len(plans) == 0 {
		if err := s.authorize(ctx, subjectID, ActionRead, "", nil, "publishing_read"); err != nil {
			return nil, err
		}
		return []PlanDetail{}, nil
	}
	details := make([]PlanDetail, 0, len(plans))
	for _, plan := range plans {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		group, err := s.lookupGroup(ctx, plan.ServiceGroupID)
		if err != nil {
			if errors.Is(err, ErrServiceGroupNotFound) {
				continue
			}
			return nil, err
		}
		if err := s.authorize(ctx, subjectID, ActionRead, "", group.NodeIDs, "publishing_read"); err != nil {
			if isPureDenial(err) {
				continue
			}
			return nil, err
		}
		stages, err := s.repo.stages(ctx, plan.ID)
		if err != nil {
			return nil, err
		}
		details = append(details, PlanDetail{Plan: plan, Stages: stages})
	}
	return details, nil
}

// Run executes pending or previously failed stages in order. Every stage result is
// persisted before the next stage starts. Succeeded stages are never re-run, and
// a failure stops the plan as partial_failure with later stages still pending.
func (s *Service) Run(ctx context.Context, subjectID, requestID, planID string) (Plan, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Plan{}, err
	}
	if !validID(planID) {
		return Plan{}, ErrPlanNotFound
	}
	unlock := s.lockPlan(planID)
	defer unlock()

	plan, stages, err := s.repo.Get(ctx, planID)
	if err != nil {
		return Plan{}, err
	}
	group, err := s.lookupGroup(ctx, plan.ServiceGroupID)
	if err != nil {
		return Plan{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionWrite, requestID, group.NodeIDs, "publishing_run"); err != nil {
		return Plan{}, err
	}
	if plan.Status == PlanCompleted {
		return plan, ErrPlanCompleted
	}
	if plan.Status == PlanCanceled {
		return plan, ErrPlanCanceled
	}
	if group.Revision != plan.ExpectedGroupRevision {
		return plan, ErrRevisionConflict
	}
	for _, stage := range stages {
		switch stage.Status {
		case StageRunning:
			_, dnsResume := s.dns.(DeferredDNSPublisher)
			_, gatewayResume := s.gateway.(DeferredGatewayPublisher)
			if !(dnsResume && stage.Kind == StageDNS || gatewayResume && stage.Kind == StageGateway) {
				return plan, ErrStageAlreadyRunning
			}
		case StageRolledBack:
			return plan, ErrPlanNotRunnable
		}
	}
	runnable := make([]int, 0, len(stages))
	for index, stage := range stages {
		if stage.Status == StagePending || stage.Status == StageFailed || stage.Status == StageRunning && (stage.Kind == StageDNS || stage.Kind == StageGateway) {
			runnable = append(runnable, index)
		}
	}
	if len(runnable) == 0 {
		if allSucceeded(stages) {
			return s.complete(ctx, subjectID, requestID, plan, group.NodeIDs)
		}
		return plan, ErrPlanNotRunnable
	}

	plan.Status = PlanRunning
	plan.CurrentStage = stages[runnable[0]].Kind
	plan.UpdatedAt = s.now().UTC()
	if err := s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.UpdatePlan(txCtx, tx, plan); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionStage, "success", requestID, group.NodeIDs, "run_started"))
	}); err != nil {
		return Plan{}, err
	}

	for _, index := range runnable {
		if err := ctx.Err(); err != nil {
			return plan, err
		}
		stageGroup, authErr := s.refreshStageAuthority(ctx, subjectID, requestID, plan, "publishing_stage")
		if authErr != nil {
			code := ErrorCodeAuthorizationDenied
			if errors.Is(authErr, ErrRevisionConflict) {
				code = ErrorCodeRevisionConflict
			} else if !isPureDenial(authErr) {
				code = ErrorCodeGroupLookupFailed
			}
			return s.fail(ctx, subjectID, requestID, plan, stages, index, code, "", authErr, group.NodeIDs)
		}
		group = stageGroup
		stage := &stages[index]
		now := s.now().UTC()
		if stage.Status != StageRunning {
			stage.Status = StageRunning
			stage.Attempts++
			stage.ResultSummary = ""
			stage.ErrorCode = ""
			stage.StartedAt = now
			stage.FinishedAt = time.Time{}
		}
		plan.Status = PlanRunning
		plan.CurrentStage = stage.Kind
		plan.UpdatedAt = now
		if err := s.persistProgress(ctx, subjectID, requestID, plan, *stage, group.NodeIDs, "stage_running"); err != nil {
			return Plan{}, err
		}

		result, publishErr := s.publish(ctx, subjectID, requestID, plan, stage.Kind)
		if errors.Is(publishErr, ErrStageAwaitingTask) {
			stage.ResultSummary = sanitizeReceipt(result.Summary)
			plan.UpdatedAt = s.now().UTC()
			if err := s.persistProgress(ctx, subjectID, requestID, plan, *stage, group.NodeIDs, "stage_awaiting_unified_task"); err != nil {
				return plan, err
			}
			return plan, ErrStageAwaitingTask
		}
		if publishErr != nil {
			if ctx.Err() != nil {
				return plan, ctx.Err()
			}
			return s.fail(ctx, subjectID, requestID, plan, stages, index, ErrorCodePublishError,
				sanitizeReceipt(result.Summary), nil, group.NodeIDs)
		}
		summary, resultErr := normalizeStageResult(result)
		if resultErr != nil {
			return s.fail(ctx, subjectID, requestID, plan, stages, index, ErrorCodeInvalidResult,
				"", ErrInvalidStageResult, group.NodeIDs)
		}
		now = s.now().UTC()
		stage.Status = StageSucceeded
		stage.ResultSummary = summary
		stage.ErrorCode = ""
		stage.FinishedAt = now
		plan.CurrentStage = stage.Kind
		plan.UpdatedAt = now
		if err := s.persistProgress(ctx, subjectID, requestID, plan, *stage, group.NodeIDs, "stage_succeeded"); err != nil {
			return Plan{}, err
		}
	}
	if !allSucceeded(stages) {
		return s.incomplete(ctx, subjectID, requestID, plan, stages, group.NodeIDs)
	}
	return s.complete(ctx, subjectID, requestID, plan, group.NodeIDs)
}

// RunDetail executes Run and returns the persisted stage rows from the same
// authorized operation. The returned detail is populated whenever the plan could
// be loaded, including when a stage failed.
func (s *Service) RunDetail(ctx context.Context, subjectID, requestID, planID string) (PlanDetail, error) {
	plan, err := s.Run(ctx, subjectID, requestID, planID)
	if plan.ID == "" {
		return PlanDetail{}, err
	}
	stages, stageErr := s.repo.stages(ctx, planID)
	if stageErr != nil {
		return PlanDetail{Plan: plan}, stageErr
	}
	return PlanDetail{Plan: plan, Stages: stages}, err
}

// RollbackDetail is RunDetail for one stage compensation.
func (s *Service) RollbackDetail(ctx context.Context, subjectID, requestID, planID string, kind StageKind) (PlanDetail, error) {
	plan, err := s.Rollback(ctx, subjectID, requestID, planID, kind)
	if plan.ID == "" {
		return PlanDetail{}, err
	}
	stages, stageErr := s.repo.stages(ctx, planID)
	if stageErr != nil {
		return PlanDetail{Plan: plan}, stageErr
	}
	return PlanDetail{Plan: plan, Stages: stages}, err
}

// Rollback invokes the explicit compensation port for one executed stage. The
// plan becomes canceled only after every executed stage is rolled_back; a partial
// rollback leaves it partial_failure. No stage summary is rewritten, so a DNS TTL
// or upstream cache is never represented as instantly reverted.
func (s *Service) Rollback(ctx context.Context, subjectID, requestID, planID string, kind StageKind) (Plan, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Plan{}, err
	}
	if s.isCertificateReplacement(ctx, planID) {
		return Plan{}, ErrStageNotRollable
	}
	normalized, err := normalizeStageKind(kind)
	if err != nil {
		return Plan{}, err
	}
	if !validID(planID) {
		return Plan{}, ErrPlanNotFound
	}
	unlock := s.lockPlan(planID)
	defer unlock()

	plan, stages, err := s.repo.Get(ctx, planID)
	if err != nil {
		return Plan{}, err
	}
	group, err := s.lookupGroup(ctx, plan.ServiceGroupID)
	if err != nil {
		return Plan{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionWrite, requestID, group.NodeIDs, "publishing_rollback"); err != nil {
		return Plan{}, err
	}
	index := -1
	for stageIndex := range stages {
		if stages[stageIndex].Kind == normalized {
			index = stageIndex
			break
		}
	}
	if index < 0 {
		return plan, ErrStageNotFound
	}
	stage := &stages[index]
	if plan.Status == PlanCanceled {
		if stage.Status == StageRolledBack {
			return plan, nil
		}
		return plan, ErrPlanCanceled
	}
	for _, candidate := range stages {
		if candidate.Status == StageRunning {
			return plan, ErrStageAlreadyRunning
		}
	}
	if stage.Status == StageRolledBack {
		return plan, nil
	}
	if !stage.Status.Executed() {
		return plan, ErrStageNotRollable
	}
	if _, err := s.refreshStageAuthority(ctx, subjectID, requestID, plan, "publishing_rollback_stage"); err != nil {
		reason := "rollback_authorization_denied"
		if errors.Is(err, ErrRevisionConflict) {
			reason = "rollback_revision_conflict"
		}
		if auditErr := s.recordFailure(ctx, subjectID, ActionRollback, requestID, group.NodeIDs, reason); auditErr != nil {
			return plan, fmt.Errorf("%w (rollback authority audit failed)", err)
		}
		return plan, err
	}
	if err := s.rollback(context.WithValue(ctx, rollbackActorKey{}, rollbackActor{subjectID, requestID}), plan, normalized, StageResult{Summary: stage.ResultSummary}); err != nil {
		if errors.Is(err, ErrStageAwaitingTask) {
			return plan, err
		}
		auditErr := s.recordFailure(ctx, subjectID, ActionRollback, requestID, group.NodeIDs, "rollback_failed")
		if auditErr != nil {
			return plan, fmt.Errorf("%w (rollback audit failed)", ErrRollbackFailed)
		}
		return plan, ErrRollbackFailed
	}
	now := s.now().UTC()
	stage.Status = StageRolledBack
	plan.CurrentStage = normalized
	plan.UpdatedAt = now
	if allExecutedRolledBack(stages) {
		plan.Status = PlanCanceled
	} else {
		plan.Status = PlanPartialFailure
	}
	resultName := "partial"
	reason := "rollback_partial"
	if plan.Status == PlanCanceled {
		resultName = "success"
		reason = "rollback_complete"
	}
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.UpdateStage(txCtx, tx, *stage); err != nil {
			return err
		}
		if err := s.repo.UpdatePlan(txCtx, tx, plan); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionRollback, resultName, requestID, group.NodeIDs, reason))
	})
	if err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func (s *Service) complete(ctx context.Context, subjectID, requestID string, plan Plan, nodeIDs []string) (Plan, error) {
	plan.Status = PlanCompleted
	plan.CurrentStage = StageGateway
	plan.UpdatedAt = s.now().UTC()
	err := s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.UpdatePlan(txCtx, tx, plan); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionRun, "success", requestID, nodeIDs, "plan_completed"))
	})
	if err != nil {
		return Plan{}, err
	}
	return plan, nil
}

// fail records a failed stage. A non-empty partialSummary preserves the exact
// provider identities a failed publish created, so explicit compensation can
// still remove only those resources instead of guessing.
func (s *Service) fail(ctx context.Context, subjectID, requestID string, plan Plan, stages []Stage, index int, errorCode, partialSummary string, cause error, nodeIDs []string) (Plan, error) {
	stage := &stages[index]
	now := s.now().UTC()
	stage.Status = StageFailed
	stage.ErrorCode = errorCode
	stage.ResultSummary = sanitizeReceipt(partialSummary)
	stage.FinishedAt = now
	plan.Status = PlanPartialFailure
	plan.CurrentStage = stage.Kind
	plan.UpdatedAt = now
	err := s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.UpdateStage(txCtx, tx, *stage); err != nil {
			return err
		}
		if err := s.repo.UpdatePlan(txCtx, tx, plan); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionRun, "failure", requestID, nodeIDs, errorCode))
	})
	if err != nil {
		return Plan{}, err
	}
	if cause != nil {
		return plan, fmt.Errorf("%w: %s: %w", ErrStageFailed, stage.Kind, cause)
	}
	return plan, fmt.Errorf("%w: %s", ErrStageFailed, stage.Kind)
}

// incomplete records a plan whose executed stages did not all succeed but no
// single newly attempted stage failed. It never rewrites a successful stage row.
func (s *Service) incomplete(ctx context.Context, subjectID, requestID string, plan Plan, stages []Stage, nodeIDs []string) (Plan, error) {
	plan.Status = PlanPartialFailure
	plan.UpdatedAt = s.now().UTC()
	for _, stage := range stages {
		if stage.Status != StageSucceeded {
			plan.CurrentStage = stage.Kind
			break
		}
	}
	err := s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.UpdatePlan(txCtx, tx, plan); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionRun, "failure", requestID, nodeIDs, "stages_incomplete"))
	})
	if err != nil {
		return Plan{}, err
	}
	return plan, ErrPlanNotRunnable
}

func (s *Service) persistProgress(ctx context.Context, subjectID, requestID string, plan Plan, stage Stage, nodeIDs []string, reason string) error {
	return s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.UpdateStage(txCtx, tx, stage); err != nil {
			return err
		}
		if err := s.repo.UpdatePlan(txCtx, tx, plan); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionStage, "success", requestID, nodeIDs, reason))
	})
}

func (s *Service) publish(ctx context.Context, subjectID, requestID string, plan Plan, kind StageKind) (StageResult, error) {
	switch kind {
	case StageDNS:
		if s.dns == nil {
			return StageResult{}, ErrPublisherUnavailable
		}
		return s.dns.Publish(ctx, DNSPublishRequest{
			PlanID: plan.ID, SubjectID: subjectID, RequestID: requestID, Domain: plan.Domain, ServiceGroupID: plan.ServiceGroupID,
			ExpectedGroupRevision: plan.ExpectedGroupRevision, GatewayIDs: append([]string(nil), plan.GatewayIDs...),
			ZoneID: plan.ZoneID, CertificateID: plan.CertificateID, HTTPS: plan.HTTPS, TargetRevision: plan.TargetRevision,
		})
	case StageCertificate:
		if s.certificates == nil {
			return StageResult{}, ErrPublisherUnavailable
		}
		return s.certificates.Publish(ctx, CertificateRequest{
			PlanID: plan.ID, SubjectID: subjectID, RequestID: requestID, Domain: plan.Domain, ServiceGroupID: plan.ServiceGroupID,
			ExpectedGroupRevision: plan.ExpectedGroupRevision, GatewayIDs: append([]string(nil), plan.GatewayIDs...),
			ZoneID: plan.ZoneID, CertificateID: plan.CertificateID, HTTPS: plan.HTTPS, TargetRevision: plan.TargetRevision,
		})
	case StageGateway:
		if s.gateway == nil {
			return StageResult{}, ErrPublisherUnavailable
		}
		return s.gateway.Publish(ctx, GatewayRequest{
			PlanID: plan.ID, SubjectID: subjectID, RequestID: requestID, Domain: plan.Domain, ServiceGroupID: plan.ServiceGroupID,
			ExpectedGroupRevision: plan.ExpectedGroupRevision, GatewayIDs: append([]string(nil), plan.GatewayIDs...),
			ZoneID: plan.ZoneID, CertificateID: plan.CertificateID, HTTPS: plan.HTTPS, TargetRevision: plan.TargetRevision,
		})
	default:
		return StageResult{}, ErrInvalidStage
	}
}

func (s *Service) rollback(ctx context.Context, plan Plan, kind StageKind, result StageResult) error {
	switch kind {
	case StageDNS:
		if s.dns == nil {
			return ErrPublisherUnavailable
		}
		return s.dns.Rollback(ctx, plan.ID, result)
	case StageCertificate:
		if s.certificates == nil {
			return ErrPublisherUnavailable
		}
		return s.certificates.Rollback(ctx, plan.ID, result)
	case StageGateway:
		if s.gateway == nil {
			return ErrPublisherUnavailable
		}
		return s.gateway.Rollback(ctx, plan.ID, result)
	default:
		return ErrInvalidStage
	}
}

// refreshStageAuthority re-reads the service group immediately before a stage or
// compensation. A changed membership revision or revoked write permission stops
// the plan instead of relying on the authorization captured at Run start.
func (s *Service) refreshStageAuthority(ctx context.Context, subjectID, requestID string, plan Plan, reason string) (GroupRef, error) {
	group, err := s.lookupGroup(ctx, plan.ServiceGroupID)
	if err != nil {
		return GroupRef{}, err
	}
	if group.Revision != plan.ExpectedGroupRevision {
		return group, ErrRevisionConflict
	}
	if err := s.authorize(ctx, subjectID, ActionWrite, requestID, group.NodeIDs, reason); err != nil {
		return group, err
	}
	return group, nil
}

func (s *Service) lookupGroup(ctx context.Context, serviceGroupID string) (GroupRef, error) {
	if !validID(serviceGroupID) {
		return GroupRef{}, ErrServiceGroupNotFound
	}
	group, exists, err := s.groups.LookupGroup(ctx, serviceGroupID)
	if err != nil {
		if ctx.Err() != nil {
			return GroupRef{}, ctx.Err()
		}
		return GroupRef{}, ErrGroupLookupFailed
	}
	if !exists {
		return GroupRef{}, ErrServiceGroupNotFound
	}
	return normalizeGroupRef(group, serviceGroupID)
}

func (s *Service) recordFailure(ctx context.Context, subjectID, action, requestID string, nodeIDs []string, reason string) error {
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.db.InTx(auditCtx, func(txCtx context.Context, tx storage.Tx) error {
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, action, "failure", requestID, nodeIDs, reason))
	})
}

func (s *Service) lockPlan(planID string) func() {
	value, _ := s.planLocks.LoadOrStore(planID, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	mutex.Lock()
	return mutex.Unlock
}

func (s *Service) requireRequest(requestID string) error {
	if s == nil {
		return errors.New("publishing: service is not configured")
	}
	if !validText(requestID, 128) {
		return ErrRequestIDRequired
	}
	return nil
}

func (s *Service) authorize(ctx context.Context, subjectID, action, requestID string, nodeIDs []string, reason string) error {
	check := auth.Check{SubjectID: subjectID, Action: action, NodeIDs: append([]string(nil), nodeIDs...)}
	err := s.authorizer.Authorize(ctx, check)
	if err == nil {
		return nil
	}
	if isPureDenial(err) {
		auditRequestID := requestID
		if auditRequestID == "" {
			// Read paths have no transport request id; keep the denial durable.
			auditRequestID = "internal-read"
		}
		auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if auditErr := s.audit.Append(auditCtx, s.event(subjectID, action, "denied", auditRequestID, nodeIDs, reason)); auditErr != nil {
			return fmt.Errorf("publishing: authorization denied (denial audit failed: %w)", auditErr)
		}
	}
	return err
}

func (s *Service) event(subjectID, action, result, requestID string, nodeIDs []string, reason string) audit.Event {
	return audit.Event{
		SubjectID: subjectID, Action: action, Result: result, RequestID: requestID,
		NodeIDs: append([]string(nil), nodeIDs...), Source: audit.SourceWeb,
		ReasonCode: reason, At: s.now(),
	}
}

func newPlanStages(planID string) []Stage {
	kinds := StageKinds()
	stages := make([]Stage, 0, len(kinds))
	for _, kind := range kinds {
		stages = append(stages, Stage{PlanID: planID, Kind: kind, Status: StagePending})
	}
	return stages
}

func allSucceeded(stages []Stage) bool {
	for _, stage := range stages {
		if stage.Status != StageSucceeded {
			return false
		}
	}
	return len(stages) == len(StageKinds())
}

func allExecutedRolledBack(stages []Stage) bool {
	for _, stage := range stages {
		switch stage.Status {
		case StageSucceeded, StageFailed, StageRunning:
			return false
		}
	}
	return true
}

// isPureDenial only treats a single denial chain as a filterable denial.
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
