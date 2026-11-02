package updates

import (
	"context"

	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
)

// ProductDeploymentOptions is trusted program configuration. No paths, trust
// keys, argv, provenance or node binding are accepted from management HTTP.
type ProductDeploymentOptions struct {
	DB                                             *storage.DB
	Audit                                          InstallAuditSink
	Directory, Inbox, StageDir, KeyID, NodeID      string
	IdentityDir, ControllerOrigin, RootCertificate string
	Policy                                         ReleasePolicy
	CandidateVersion                               string
	Initial                                        ReleaseManifest
	SchemaVersion                                  int
	// SchemaFingerprint is the reviewed sqlite_master definition hash that MUST
	// match the live Controller database; a bare user_version is not enough.
	SchemaFingerprint string
	Current           func(context.Context, string, string) (tasks.Task, error)
	Administrator     func(context.Context, string) error
	RuntimeCurrent    func(context.Context) error
	NodeCurrent       func(context.Context, string, string) error
}

type ProductDeployment interface {
	WorkerOptions() *TaskInstallWorkerOptions
	ResolvePlan(context.Context, ApplyRequest) (FrozenInstallPlan, error)
	Check(context.Context, string) (CheckResult, error)
	Plan(context.Context, string) (Plan, error)
	Start(context.Context) error
	Close() error
}
