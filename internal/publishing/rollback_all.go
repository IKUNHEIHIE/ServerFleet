package publishing

import (
	"context"
	"errors"
	"fmt"

	"serverfleet.local/serverfleet/internal/storage"
)

// RollbackAllDetail returns the same detail shape as the single-stage operations.
func (s *Service) RollbackAllDetail(ctx context.Context, subjectID, requestID, planID string) (PlanDetail, error) {
	plan, err := s.RollbackAll(ctx, subjectID, requestID, planID)
	stages, stageErr := s.repo.stages(ctx, planID)
	if stageErr != nil {
		return PlanDetail{Plan: plan}, stageErr
	}
	return PlanDetail{Plan: plan, Stages: stages}, err
}

// RollbackAll compensates every executed stage in reverse publication order
// (gateway, certificate, DNS). A failed compensation stops the sequence so an
// earlier upstream stage is never removed while a still-live downstream stage
// depends on it. Successfully compensated stages are persisted immediately and a
// later call resumes from the first remaining stage. It never rewrites a stage
// summary, so a DNS TTL or resolver cache is not claimed to be instantly reverted.
func (s *Service) RollbackAll(ctx context.Context, subjectID, requestID, planID string) (Plan, error) {
	if s.isCertificateReplacement(ctx, planID) {
		return Plan{}, ErrStageNotRollable
	}
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
	if err := s.authorize(ctx, subjectID, ActionWrite, requestID, group.NodeIDs, "publishing_rollback_all"); err != nil {
		return Plan{}, err
	}
	for _, stage := range stages {
		if stage.Status == StageRunning {
			return plan, ErrStageAlreadyRunning
		}
	}
	order := StageKinds()
	candidates := make([]Stage, 0, len(order))
	for index := len(order) - 1; index >= 0; index-- {
		stage := findStage(stages, order[index])
		if stage == nil || stage.Status == StageRolledBack || !stage.Status.Executed() {
			continue
		}
		candidates = append(candidates, *stage)
	}
	if len(candidates) == 0 {
		if hasRolledBack(stages) && allExecutedRolledBack(stages) {
			return plan, nil
		}
		return plan, ErrStageNotRollable
	}

	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return plan, err
		}
		stage := findStage(stages, candidate.Kind)
		if stage == nil {
			return plan, ErrStageNotFound
		}
		currentGroup, authErr := s.refreshStageAuthority(ctx, subjectID, requestID, plan, "publishing_rollback_all_stage")
		if authErr != nil {
			reason := "rollback_all_authorization_denied"
			if errors.Is(authErr, ErrRevisionConflict) {
				reason = "rollback_all_revision_conflict"
			}
			plan.Status = PlanPartialFailure
			plan.CurrentStage = stage.Kind
			plan.UpdatedAt = s.now().UTC()
			if persistErr := s.persistRollbackAll(ctx, subjectID, requestID, plan, nil, group.NodeIDs, "failure", reason); persistErr != nil {
				return Plan{}, persistErr
			}
			return plan, authErr
		}
		group = currentGroup
		if err := s.rollback(context.WithValue(ctx, rollbackActorKey{}, rollbackActor{subjectID, requestID}), plan, stage.Kind, StageResult{Summary: stage.ResultSummary}); err != nil {
			if errors.Is(err, ErrStageAwaitingTask) {
				return plan, err
			}
			plan.Status = PlanPartialFailure
			plan.CurrentStage = stage.Kind
			plan.UpdatedAt = s.now().UTC()
			persistErr := s.persistRollbackAll(ctx, subjectID, requestID, plan, nil, group.NodeIDs, "failure", "rollback_all_failed_"+string(stage.Kind))
			if persistErr != nil {
				return Plan{}, persistErr
			}
			return plan, fmt.Errorf("%w: %s", ErrRollbackFailed, stage.Kind)
		}
		now := s.now().UTC()
		stage.Status = StageRolledBack
		plan.CurrentStage = stage.Kind
		plan.UpdatedAt = now
		result := "partial"
		reason := "rollback_all_partial"
		if allExecutedRolledBack(stages) {
			plan.Status = PlanCanceled
			result = "success"
			reason = "rollback_all_complete"
		} else {
			plan.Status = PlanPartialFailure
		}
		if err := s.persistRollbackAll(ctx, subjectID, requestID, plan, stage, group.NodeIDs, result, reason); err != nil {
			return Plan{}, err
		}
	}
	return plan, nil
}

func hasRolledBack(stages []Stage) bool {
	for _, stage := range stages {
		if stage.Status == StageRolledBack {
			return true
		}
	}
	return false
}

func findStage(stages []Stage, kind StageKind) *Stage {
	for index := range stages {
		if stages[index].Kind == kind {
			return &stages[index]
		}
	}
	return nil
}

func (s *Service) persistRollbackAll(ctx context.Context, subjectID, requestID string, plan Plan, stage *Stage, nodeIDs []string, result, reason string) error {
	return s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if stage != nil {
			if err := s.repo.UpdateStage(txCtx, tx, *stage); err != nil {
				return err
			}
		}
		if err := s.repo.UpdatePlan(txCtx, tx, plan); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionRollbackAll, result, requestID, nodeIDs, reason))
	})
}
