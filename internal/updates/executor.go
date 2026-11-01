package updates

import "context"

// PreparedInstall is resolved from a durable, actor-bound task plan, never the
// browser's confirmation. Resolver MUST consume current task/approval admission
// and match actor + plan digest + backup evidence. Each InstallHooks phase still
// rechecks authority, actual process identity/health and storage compatibility.
type PreparedInstall struct {
	PlanID, SubjectID string
	Staged            *StagedArtifact
	Policy            ReleasePolicy
	Intent            InstallIntent
	Hooks             InstallHooks
}
type InstallPlanResolver func(context.Context, ApplyRequest) (PreparedInstall, error)

func NewTaskInstallExecutor(installer *SlotInstaller, resolve InstallPlanResolver) (func(context.Context, ApplyRequest) (ApplyResult, error), error) {
	if installer == nil || resolve == nil {
		return nil, ErrArtifactRejected
	}
	return func(ctx context.Context, r ApplyRequest) (out ApplyResult, err error) {
		defer func() {
			if recover() != nil {
				out = ApplyResult{}
				err = ErrApplyUnknown
			}
		}()
		if ctx == nil || ctx.Err() != nil || !r.Principal.Administrator || !installID(r.Principal.SubjectID) || !installID(r.PlanID) || r.Confirmation == "" || r.BackupMode != "required" {
			return out, ErrArtifactRejected
		}
		prepared, err := resolve(ctx, r)
		if err != nil || prepared.PlanID != r.PlanID || prepared.SubjectID != r.Principal.SubjectID || prepared.Staged == nil {
			return out, ErrArtifactRejected
		}
		receipt, err := installer.Apply(ctx, prepared.Staged, prepared.Policy, prepared.Intent, prepared.Hooks)
		if err != nil {
			return out, err
		}
		return ApplyResult{ID: receipt.Intent.ExecutionID, TaskID: receipt.Intent.TaskID, Status: receipt.State, TargetVersion: receipt.Attempted.Version}, nil
	}, nil
}
