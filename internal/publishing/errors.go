// Package publishing owns the staged DNS/certificate/gateway publication plan. It
// never talks to providers, DNS or gateways directly: every stage runs through an
// injected publisher port and every transition is persisted before the next stage.
package publishing

import "errors"

var (
	ErrMissingAuthorizer = errors.New("publishing: authorizer is required")
	ErrRequestIDRequired = errors.New("publishing: request id is required")

	ErrInvalidPlan     = errors.New("publishing: invalid plan")
	ErrPlanNotFound    = errors.New("publishing: plan not found")
	ErrPlanExists      = errors.New("publishing: plan already exists")
	ErrDuplicatePlan   = errors.New("publishing: plan idempotency key already exists")
	ErrPlanCompleted   = errors.New("publishing: plan is already completed")
	ErrPlanCanceled    = errors.New("publishing: plan is canceled")
	ErrPlanNotRunnable = errors.New("publishing: plan is not runnable")

	ErrRevisionConflict     = errors.New("publishing: service group revision conflict")
	ErrServiceGroupNotFound = errors.New("publishing: service group not found")
	ErrGroupLookupFailed    = errors.New("publishing: service group lookup failed")

	ErrInvalidStage         = errors.New("publishing: invalid stage")
	ErrStageNotFound        = errors.New("publishing: stage not found")
	ErrStageAlreadyRunning  = errors.New("publishing: a stage is already running")
	ErrStageNotRollable     = errors.New("publishing: stage has not executed and cannot be rolled back")
	ErrStageFailed          = errors.New("publishing: stage failed")
	ErrInvalidStageResult   = errors.New("publishing: stage publisher returned an invalid result")
	ErrPublisherUnavailable = errors.New("publishing: stage publisher is not configured")
	ErrRollbackFailed       = errors.New("publishing: stage rollback failed")
)
