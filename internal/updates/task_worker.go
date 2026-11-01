package updates

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/tasks"
)

const ActionInstall = "system.updates.apply"

// FrozenInstallPlan contains provenance, not executable paths or commands. It
// is persisted in the sole task service and included in its approval digest.
type FrozenInstallPlan struct {
	ID, SubjectID, NodeID, BackupSHA256, PolicySHA256 string
	Previous, Attempted                               ReleaseManifest
	AllowRollback                                     bool
}

func ReleasePolicyDigest(p ReleasePolicy) string {
	raw, _ := json.Marshal(p)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (p FrozenInstallPlan) valid() bool {
	return installID(p.ID) && installID(p.SubjectID) && installID(p.NodeID) && validPlanDigest(p.BackupSHA256) && validPlanDigest(p.PolicySHA256) && installedManifest(p.Previous) && installedManifest(p.Attempted) && p.Previous.Component == p.Attempted.Component && p.Previous.Architecture == p.Attempted.Architecture && p.Attempted.Sequence > p.Previous.Sequence
}
func validPlanDigest(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && hex.EncodeToString(b) == s
}
func (p FrozenInstallPlan) TaskRequest(requestID string) (tasks.SubmitRequest, error) {
	if !p.valid() {
		return tasks.SubmitRequest{}, ErrArtifactRejected
	}
	raw, _ := json.Marshal(p)
	if len(raw) > 1024 {
		return tasks.SubmitRequest{}, ErrArtifactRejected
	}
	return tasks.SubmitRequest{SubjectID: p.SubjectID, RequestID: requestID, Name: "verified release installation", ActionID: ActionInstall, OperationClass: tasks.OperationClassModifying, RequireApproval: true, TargetType: "node_ids", TargetNodeIDs: []string{p.NodeID}, Params: map[string]string{"install_plan": string(raw)}, IdempotencyKey: p.ID, Source: audit.SourceWeb}, nil
}
func frozenTaskPlan(t tasks.Task) (FrozenInstallPlan, error) {
	var p FrozenInstallPlan
	var params map[string]string
	if t.ActionID != ActionInstall || t.OperationClass != tasks.OperationClassModifying || !t.ApprovalRequired || t.Source != string(audit.SourceWeb) || len(t.Targets) != 1 || json.Unmarshal([]byte(t.Params), &params) != nil || len(params) != 1 {
		return p, ErrArtifactRejected
	}
	raw := []byte(params["install_plan"])
	if len(raw) > 1024 || json.Unmarshal(raw, &p) != nil || !p.valid() || p.SubjectID != t.SubmitterID || p.NodeID != t.Targets[0].NodeID {
		return p, ErrArtifactRejected
	}
	canonical, _ := json.Marshal(p)
	if !bytes.Equal(raw, canonical) {
		return p, ErrArtifactRejected
	}
	return p, nil
}

type TaskInstallWorkerOptions struct {
	Installer *SlotInstaller
	// Current is tasks.Service.CheckExecution, including current approval.
	Current       func(context.Context, string, string) (tasks.Task, error)
	Administrator func(context.Context, string) error
	// Resolve opens ONLY the frozen signed artifact and dedicated owned slot.
	Resolve func(context.Context, FrozenInstallPlan, InstallIntent) (PreparedInstall, error)
	// Independently inspect the exact actual backup and storage state every phase.
	BackupCurrent func(context.Context, FrozenInstallPlan) error
}
type TaskInstallWorker struct{ options TaskInstallWorkerOptions }

func NewTaskInstallWorker(o TaskInstallWorkerOptions) (*TaskInstallWorker, error) {
	if o.Installer == nil || o.Current == nil || o.Administrator == nil || o.Resolve == nil || o.BackupCurrent == nil {
		return nil, ErrArtifactRejected
	}
	return &TaskInstallWorker{o}, nil
}
func (w *TaskInstallWorker) Execute(ctx context.Context, t tasks.Task) (map[string]tasks.WorkerResult, error) {
	p, err := frozenTaskPlan(t)
	if err != nil {
		return nil, err
	}
	live := func(ctx context.Context) error {
		current, e := w.options.Current(ctx, t.ID, t.PlanHash)
		if e != nil || current.ID != t.ID || current.PlanHash != t.PlanHash || current.Status != tasks.StatusRunning || current.CancelRequested || current.Params != t.Params || current.SubmitterID != t.SubmitterID || current.ActionID != ActionInstall || !current.ApprovalRequired || current.OperationClass != tasks.OperationClassModifying || w.options.Administrator(ctx, p.SubjectID) != nil || w.options.BackupCurrent(ctx, p) != nil {
			return ErrArtifactRejected
		}
		return nil
	}
	if live(ctx) != nil {
		return nil, ErrArtifactRejected
	}
	digest := sha256.Sum256([]byte(t.ID + "\x00" + t.PlanHash))
	intent := InstallIntent{TaskID: t.ID, ExecutionID: "update-" + hex.EncodeToString(digest[:20]), Previous: p.Previous, AllowRollback: p.AllowRollback}
	prepared, err := w.options.Resolve(ctx, p, intent)
	if prepared.Staged != nil {
		defer prepared.Staged.Close()
	}
	if err != nil || prepared.PlanID != p.ID || prepared.SubjectID != p.SubjectID || prepared.Intent != intent || prepared.Staged == nil {
		return nil, ErrArtifactRejected
	}
	if prepared.Staged.Manifest() != p.Attempted || ReleasePolicyDigest(prepared.Policy) != p.PolicySHA256 || prepared.Hooks.Gate == nil || prepared.Hooks.Commit == nil || live(ctx) != nil {
		return nil, ErrArtifactRejected
	}
	hooks := prepared.Hooks
	gate, commit := hooks.Gate, hooks.Commit
	hooks.Gate = func(ctx context.Context, i InstallIntent, m ReleaseManifest, phase string) error {
		if i != intent || m != p.Attempted || live(ctx) != nil {
			return ErrArtifactRejected
		}
		return gate(ctx, i, m, phase)
	}
	hooks.Commit = func(ctx context.Context, r InstallReceipt) (string, error) {
		if live(ctx) != nil || r.Intent != intent || r.Attempted != p.Attempted {
			return "", ErrApplyUnknown
		}
		return commit(ctx, r)
	}
	receipt, err := w.options.Installer.Apply(ctx, prepared.Staged, prepared.Policy, intent, hooks)
	code, message := tasks.NodeResultUnknown, "update outcome unknown; reconcile without replay"
	if err == nil {
		if receipt.State == "installed" {
			code, message = tasks.NodeResultSucceeded, "verified release installed"
		} else if receipt.State == "rolled_back" {
			code, message = tasks.NodeResultFailed, "release unhealthy; verified previous release restored"
		}
	}
	if errors.Is(err, ErrArtifactRejected) {
		code, message = tasks.NodeResultFailed, "release rejected before installation"
	}
	return map[string]tasks.WorkerResult{p.NodeID: {Code: code, Message: message}}, nil
}
