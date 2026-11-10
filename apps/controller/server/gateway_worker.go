package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/gateway"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/publishing"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
)

const (
	gatewayDefaultApprovalWait = 30 * time.Second
	gatewayDefaultExecuteWait  = 2 * time.Minute
	gatewayDefaultPoll         = 25 * time.Millisecond
)

var (
	ErrGatewayAwaitingApproval = errors.New("gateway: publication task is awaiting approval")
	ErrGatewayTaskUncertain    = errors.New("gateway: publication task outcome is unknown")
)

// gatewayPlannedTask is the trusted program-side plan binding one unified task
// to exactly one artifact and one publication target revision. It lives only in
// Controller memory: it is never accepted from HTTP or the Agent.
type gatewayPlannedTask struct {
	TaskID         string
	ActionID       string
	GatewayID      string
	NodeID         string
	SitePath       string
	ArtifactPath   string
	Digest         string
	Content        []byte
	PlanID         string
	TargetRevision uint64
}

func (p gatewayPlannedTask) validate() error {
	if p.TaskID == "" || p.GatewayID == "" || p.NodeID == "" || p.PlanID == "" ||
		p.TargetRevision == 0 || p.SitePath == "" || !validGatewayDigest(p.Digest) {
		return errGatewayPlan
	}
	switch p.ActionID {
	case gateway.NginxActionApply:
		if p.ArtifactPath == "" || len(p.Content) == 0 || len(p.Content) > 64<<10 {
			return errGatewayPlan
		}
		digest := sha256.Sum256(p.Content)
		if hex.EncodeToString(digest[:]) != p.Digest {
			return errGatewayPlan
		}
	case gateway.NginxActionRemove:
		if p.ArtifactPath != "" || len(p.Content) != 0 {
			return errGatewayPlan
		}
	default:
		return errGatewayPlan
	}
	return nil
}

func validGatewayDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

var errGatewayPlan = errors.New("gateway: planned publication task is invalid")

type gatewayArtifactRegistry struct {
	mu      sync.Mutex
	entries map[string]gatewayPlannedTask
}

func newGatewayArtifactRegistry() *gatewayArtifactRegistry {
	return &gatewayArtifactRegistry{entries: map[string]gatewayPlannedTask{}}
}

// register stores the plan before approval is possible. Registering content in
// memory is not an upload: no bytes reach the Agent until the dispatcher has
// rechecked the current approval and target revision.
func (r *gatewayArtifactRegistry) register(planned gatewayPlannedTask) error {
	if r == nil || planned.validate() != nil {
		return errGatewayPlan
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.entries[planned.TaskID]; ok {
		if existing.ActionID != planned.ActionID || existing.NodeID != planned.NodeID ||
			existing.Digest != planned.Digest || existing.SitePath != planned.SitePath ||
			existing.PlanID != planned.PlanID || existing.TargetRevision != planned.TargetRevision {
			return errGatewayPlan
		}
		return nil
	}
	r.entries[planned.TaskID] = planned
	return nil
}

func (r *gatewayArtifactRegistry) take(taskID, nodeID, actionID string) (gatewayPlannedTask, bool) {
	if r == nil {
		return gatewayPlannedTask{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	planned, ok := r.entries[taskID]
	if !ok || planned.NodeID != nodeID || planned.ActionID != actionID {
		return gatewayPlannedTask{}, false
	}
	delete(r.entries, taskID)
	return planned, true
}

func (r *gatewayArtifactRegistry) drop(taskID string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	delete(r.entries, taskID)
	r.mu.Unlock()
}

// GatewayPlanAuthority rechecks the persisted publication target revision that
// produced the frozen task parameters.
type GatewayPlanAuthority interface {
	Current(ctx context.Context, planID string, targetRevision uint64, gatewayID string) error
}

type publicationPlanAuthority struct{ repo *publishing.Repo }

func (a publicationPlanAuthority) Current(ctx context.Context, planID string, targetRevision uint64, gatewayID string) error {
	if a.repo == nil {
		return publishing.ErrProductionUnavailable
	}
	plan, _, err := a.repo.Get(ctx, planID)
	if err != nil || !plan.TargetsPersisted || plan.TargetRevision != targetRevision {
		return publishing.ErrProductionTarget
	}
	matches := 0
	for _, id := range plan.GatewayIDs {
		if id == gatewayID {
			matches++
		}
	}
	if matches != 1 {
		return publishing.ErrProductionTarget
	}
	return nil
}

// gatewayExecutionConfig is trusted program composition for the gateway task
// dispatcher. Next is the Agent link dispatcher; it is never bypassed.
type gatewayExecutionConfig struct {
	Repo        *tasks.Repo
	DB          *storage.DB
	Plans       *publishing.Repo
	Artifacts   GatewayArtifactUploader
	Next        tasks.Dispatcher
	Authorizer  auth.Authorizer
	Nodes       *nodes.Repo
	Registry    *gatewayArtifactRegistry
	Now         func() time.Time
	ApprovalTTL time.Duration
	Authority   func(context.Context, tasks.Task, string) error
	// planAuthority is a test seam; production derives it from Plans.
	planAuthority GatewayPlanAuthority
}

type gatewayTaskDispatcher struct {
	repo        *tasks.Repo
	db          *storage.DB
	authority   GatewayPlanAuthority
	artifacts   GatewayArtifactUploader
	next        tasks.Dispatcher
	authorizer  auth.Authorizer
	nodes       *nodes.Repo
	registry    *gatewayArtifactRegistry
	now         func() time.Time
	approvalTTL time.Duration
	check       func(context.Context, tasks.Task, string) error
}

var _ tasks.Dispatcher = (*gatewayTaskDispatcher)(nil)

// newGatewayTaskDispatcher is exported through the controller assembly seam.
func newGatewayTaskDispatcher(config gatewayExecutionConfig) (*gatewayTaskDispatcher, error) {
	if config.Repo == nil || config.DB == nil ||
		config.Artifacts == nil || config.Next == nil || config.Authorizer == nil ||
		(config.Authority == nil && config.Nodes == nil) || config.Registry == nil {
		return nil, gateway.ErrTaskSubmitterUnavailable
	}
	authority := config.planAuthority
	if authority == nil {
		if config.Plans == nil {
			return nil, gateway.ErrTaskSubmitterUnavailable
		}
		authority = publicationPlanAuthority{repo: config.Plans}
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	ttl := config.ApprovalTTL
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	dispatcher := &gatewayTaskDispatcher{
		repo: config.Repo, db: config.DB,
		authority: authority, artifacts: config.Artifacts,
		next: config.Next, authorizer: config.Authorizer, nodes: config.Nodes,
		registry: config.Registry, now: now, approvalTTL: ttl, check: config.Authority,
	}
	return dispatcher, nil
}

// Dispatch is the sole execution hook for gateway tasks. It rechecks the current
// approval, plan target revision and node authorization before uploading the
// artifact, uploads without a shell, rechecks all three after the upload, and
// only then hands the unchanged offer to the Agent link dispatcher.
func (d *gatewayTaskDispatcher) Dispatch(ctx context.Context, command tasks.Command) (tasks.DispatchResult, error) {
	if command.ActionID != gateway.NginxActionApply && command.ActionID != gateway.NginxActionRemove {
		return d.next.Dispatch(ctx, command)
	}
	planned, ok := d.registry.take(command.TaskID, command.NodeID, command.ActionID)
	if !ok {
		return tasks.DispatchResult{Accepted: false, Reason: "gateway_plan_unavailable"}, nil
	}
	defer d.registry.drop(command.TaskID)
	task, err := d.repo.Get(ctx, command.TaskID)
	if err != nil || !gatewayCommandMatches(task, command, planned) {
		return tasks.DispatchResult{Accepted: false, Reason: "gateway_plan_mismatch"}, nil
	}
	if err := d.currentApproval(ctx, task); err != nil {
		return tasks.DispatchResult{Accepted: false, Reason: "approval_invalid"}, nil
	}
	if err := d.authority.Current(ctx, planned.PlanID, planned.TargetRevision, planned.GatewayID); err != nil {
		return tasks.DispatchResult{Accepted: false, Reason: "target_revision_changed"}, nil
	}
	if err := d.currentNodeAuthority(ctx, task, command.NodeID); err != nil {
		return tasks.DispatchResult{Accepted: false, Reason: "authorization_revoked"}, nil
	}
	if command.ActionID == gateway.NginxActionApply {
		if bound, ok := d.artifacts.(GatewayBoundArtifactUploader); ok {
			err := bound.UploadBound(ctx, GatewayArtifactBinding{
				TaskID: command.TaskID, NodeID: command.NodeID,
				ExecutionID: gatewayArtifactExecutionID(command.TaskID, command.NodeID, task.PlanHash, planned.GatewayID),
				TransferID:  gatewayArtifactTransferID(command.TaskID, command.NodeID, planned.ArtifactPath),
				PlanDigest:  task.PlanHash,
				GatewayID:   planned.GatewayID, PlanID: planned.PlanID,
				TargetRevision: planned.TargetRevision, SitePath: planned.SitePath,
				AbsolutePath: planned.ArtifactPath, Content: planned.Content, SHA256: planned.Digest,
			})
			if err != nil {
				return tasks.DispatchResult{Accepted: false, Reason: "artifact_upload_failed"}, nil
			}
		} else if err := d.artifacts.Upload(ctx, command.TaskID, command.NodeID, planned.ArtifactPath, planned.Content, planned.Digest); err != nil {
			return tasks.DispatchResult{Accepted: false, Reason: "artifact_upload_failed"}, nil
		}
		latest, err := d.repo.Get(ctx, command.TaskID)
		if err != nil || latest.Status != tasks.StatusQueued || latest.CancelRequested {
			return tasks.DispatchResult{Accepted: false, Reason: "task_canceled_after_upload"}, nil
		}
		if err := d.currentApproval(ctx, latest); err != nil {
			return tasks.DispatchResult{Accepted: false, Reason: "approval_revoked_after_upload"}, nil
		}
		if err := d.authority.Current(ctx, planned.PlanID, planned.TargetRevision, planned.GatewayID); err != nil {
			return tasks.DispatchResult{Accepted: false, Reason: "target_changed_after_upload"}, nil
		}
		if err := d.currentNodeAuthority(ctx, latest, command.NodeID); err != nil {
			return tasks.DispatchResult{Accepted: false, Reason: "authorization_revoked_after_upload"}, nil
		}
	}
	return d.next.Dispatch(ctx, command)
}

func gatewayCommandMatches(task tasks.Task, command tasks.Command, planned gatewayPlannedTask) bool {
	if task.ID != planned.TaskID || task.ActionID != planned.ActionID || task.SubmitterID == "" ||
		task.Status != tasks.StatusQueued || task.CancelRequested || task.PlanHash == "" {
		return false
	}
	params, err := gatewayCommandParams(command.Params)
	if err != nil || params["operation"] != operationForAction(planned.ActionID) ||
		params["site_path"] != planned.SitePath || params["config_sha256"] != planned.Digest {
		return false
	}
	if planned.ActionID == gateway.NginxActionApply && params["artifact_path"] != planned.ArtifactPath {
		return false
	}
	return true
}

func gatewayCommandParams(raw json.RawMessage) (map[string]string, error) {
	if len(raw) == 0 || len(raw) > 16<<10 {
		return nil, errGatewayPlan
	}
	var values map[string]string
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&values); err != nil || values == nil {
		return nil, errGatewayPlan
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errGatewayPlan
	}
	return values, nil
}

func operationForAction(action string) string {
	if action == gateway.NginxActionRemove {
		return "remove"
	}
	return "apply"
}

// Cancel forwards cancellation through the same Agent link. The Agent owns the
// typed action and reports its real terminal state; a cancel is never treated
// as success.
func (d *gatewayTaskDispatcher) Cancel(ctx context.Context, command tasks.CancelCommand) error {
	if d == nil || d.next == nil {
		return gateway.ErrTaskSubmitterUnavailable
	}
	return d.next.Cancel(ctx, command)
}

func (d *gatewayTaskDispatcher) currentApproval(ctx context.Context, task tasks.Task) error {
	if task.Status != tasks.StatusQueued || task.CancelRequested || !task.ApprovalRequired || task.PlanHash == "" {
		return tasks.ErrApprovalInvalid
	}
	approval, err := d.repo.Approval(ctx, d.db, task.ID, task.Revision)
	if err != nil || !approval.Approved() || approval.PlanHash != task.PlanHash ||
		approval.ApproverID == task.SubmitterID {
		return tasks.ErrApprovalInvalid
	}
	if d.approvalTTL > 0 && d.now().UTC().After(approval.DecidedAt.Add(d.approvalTTL)) {
		return tasks.ErrApprovalInvalid
	}
	return nil
}

func (d *gatewayTaskDispatcher) currentNodeAuthority(ctx context.Context, task tasks.Task, nodeID string) error {
	if d.check != nil {
		return d.check(ctx, task, nodeID)
	}
	node, err := d.nodes.Get(ctx, nodeID)
	if err != nil || node.Status != nodes.StatusOnline {
		return publishing.ErrProductionTarget
	}
	return d.authorizer.Authorize(ctx, auth.Check{
		SubjectID: task.SubmitterID, Action: task.ActionID, NodeIDs: []string{nodeID},
	})
}

// CheckArtifactStage is the equivalent reviewed gate for the queued upload
// phase. It runs before the first chunk and again before every subsequent chunk
// through the channel authorizer. It never relaxes tasks.CheckExecution, which
// still requires running for Agent execution frames.
func (d *gatewayTaskDispatcher) CheckArtifactStage(ctx context.Context, taskID, nodeID, planID string, targetRevision uint64, gatewayID, planDigest, artifactPath, sitePath, configDigest string) error {
	if d == nil || d.repo == nil {
		return gateway.ErrTaskSubmitterUnavailable
	}
	task, err := d.repo.Get(ctx, taskID)
	if err != nil || task.Status != tasks.StatusQueued || task.CancelRequested || task.PlanHash == "" ||
		task.PlanHash != planDigest {
		return tasks.ErrApprovalInvalid
	}
	params, err := gatewayCommandParams(json.RawMessage(task.Params))
	if err != nil || params["operation"] != "apply" ||
		params["artifact_path"] != artifactPath || params["site_path"] != sitePath ||
		params["config_sha256"] != configDigest {
		return tasks.ErrApprovalInvalid
	}
	pending := false
	for _, target := range task.Targets {
		if target.NodeID == nodeID && target.State == tasks.TargetPending {
			pending = true
			break
		}
	}
	if !pending {
		return tasks.ErrApprovalInvalid
	}
	if err := d.currentApproval(ctx, task); err != nil {
		return err
	}
	if err := d.authority.Current(ctx, planID, targetRevision, gatewayID); err != nil {
		return err
	}
	return d.currentNodeAuthority(ctx, task, nodeID)
}

// CheckArtifactChild is the child-lifecycle gate. It allows the upload to have
// moved the parent task from queued to running, but still requires the same
// frozen plan digest, task parameters, approval and current authority. It never
// records a parent result.
func (d *gatewayTaskDispatcher) CheckArtifactChild(ctx context.Context, taskID, nodeID, planID string, targetRevision uint64, gatewayID, planDigest, artifactPath, sitePath, configDigest string) error {
	if d == nil || d.repo == nil {
		return gateway.ErrTaskSubmitterUnavailable
	}
	task, err := d.repo.Get(ctx, taskID)
	if err != nil || task.CancelRequested || task.PlanHash == "" || task.PlanHash != planDigest ||
		task.Status.Terminal() {
		return tasks.ErrApprovalInvalid
	}
	params, err := gatewayCommandParams(json.RawMessage(task.Params))
	if err != nil || params["operation"] != "apply" ||
		params["artifact_path"] != artifactPath || params["site_path"] != sitePath ||
		params["config_sha256"] != configDigest {
		return tasks.ErrApprovalInvalid
	}
	if !task.ApprovalRequired {
		return tasks.ErrApprovalInvalid
	}
	approval, err := d.repo.Approval(ctx, d.db, task.ID, task.Revision)
	if err != nil || !approval.Approved() || approval.PlanHash != task.PlanHash || approval.ApproverID == task.SubmitterID {
		return tasks.ErrApprovalInvalid
	}
	if d.approvalTTL > 0 && d.now().UTC().After(approval.DecidedAt.Add(d.approvalTTL)) {
		return tasks.ErrApprovalInvalid
	}
	if err := d.authority.Current(ctx, planID, targetRevision, gatewayID); err != nil {
		return err
	}
	return d.currentNodeAuthority(ctx, task, nodeID)
}

func gatewayArtifactTransferID(taskID, nodeID, absolutePath string) string {
	digest := sha256.Sum256([]byte(taskID + "\x00" + nodeID + "\x00" + absolutePath))
	return "gwa-" + hex.EncodeToString(digest[:16])
}

// gatewayTaskHost submits exactly one approved unified task per plan action,
// then waits for its real terminal state. It never treats queued, running or an
// HTTP 202 as publication success.
type gatewayTaskHost struct {
	tasks        *tasks.Service
	registry     *gatewayArtifactRegistry
	authority    GatewayPlanAuthority
	now          func() time.Time
	poll         time.Duration
	approvalWait time.Duration
	executeWait  time.Duration
	onSubmitted  func(string)
}

type gatewayHostConfig struct {
	Registry     *gatewayArtifactRegistry
	Authority    GatewayPlanAuthority
	Poll         time.Duration
	ApprovalWait time.Duration
	ExecuteWait  time.Duration
	OnSubmitted  func(string)
	Now          func() time.Time
}

func newGatewayTaskHost(tasksService *tasks.Service, config gatewayHostConfig) (*gatewayTaskHost, error) {
	if tasksService == nil || config.Registry == nil || config.Authority == nil {
		return nil, gateway.ErrTaskSubmitterUnavailable
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	poll := config.Poll
	if poll <= 0 {
		poll = gatewayDefaultPoll
	}
	approvalWait := config.ApprovalWait
	if approvalWait <= 0 {
		approvalWait = gatewayDefaultApprovalWait
	}
	executeWait := config.ExecuteWait
	if executeWait <= 0 {
		executeWait = gatewayDefaultExecuteWait
	}
	return &gatewayTaskHost{
		tasks: tasksService, registry: config.Registry, authority: config.Authority,
		now: now, poll: poll, approvalWait: approvalWait, executeWait: executeWait,
		onSubmitted: config.OnSubmitted,
	}, nil
}

func (h *gatewayTaskHost) apply(ctx context.Context, plan publishing.Plan, site gateway.NginxSite, input gateway.NginxTaskInput) (string, error) {
	request, err := gateway.BuildNginxApplyTask(site, input)
	if err != nil {
		return "", err
	}
	planned := gatewayPlannedTask{
		ActionID: gateway.NginxActionApply, GatewayID: input.GatewayID, NodeID: input.NodeID,
		SitePath: site.Path, ArtifactPath: site.ArtifactPath, Digest: site.Digest, Content: append([]byte(nil), site.Config...),
		PlanID: plan.ID, TargetRevision: plan.TargetRevision,
	}
	return h.execute(ctx, plan, request, planned, "nginx site apply "+input.GatewayID)
}

func (h *gatewayTaskHost) remove(ctx context.Context, plan publishing.Plan, site gateway.NginxSite, input gateway.NginxTaskInput) (string, error) {
	request, err := gateway.BuildNginxRemoveTask(site, input)
	if err != nil {
		return "", err
	}
	planned := gatewayPlannedTask{
		ActionID: gateway.NginxActionRemove, GatewayID: input.GatewayID, NodeID: input.NodeID,
		SitePath: site.Path, Digest: site.Digest, PlanID: plan.ID, TargetRevision: plan.TargetRevision,
	}
	return h.execute(ctx, plan, request, planned, "nginx site remove "+input.GatewayID)
}

func (h *gatewayTaskHost) execute(ctx context.Context, plan publishing.Plan, request gateway.TaskRequest, planned gatewayPlannedTask, name string) (string, error) {
	if err := h.authority.Current(ctx, planned.PlanID, planned.TargetRevision, planned.GatewayID); err != nil {
		return "", err
	}
	task, err := h.submit(ctx, plan, request, name)
	if err != nil {
		return "", err
	}
	planned.TaskID = task.ID
	// In-memory only: the dispatcher performs the sole upload after it has
	// rechecked the current approval and target revision.
	if task.Status != tasks.StatusCompleted {
		if err := h.registry.register(planned); err != nil {
			return "", err
		}
		if !publishing.NonBlockingTaskObservation(ctx) {
			defer h.registry.drop(task.ID)
		}
	}
	if h.onSubmitted != nil {
		h.onSubmitted(task.ID)
	}
	if task.Status == tasks.StatusCompleted {
		h.registry.drop(task.ID)
		return task.ID, gatewayCompleted(task)
	}
	if publishing.NonBlockingTaskObservation(ctx) && !task.Status.Terminal() {
		return task.ID, publishing.ErrStageAwaitingTask
	}
	return h.wait(ctx, task.ID, task.Status)
}

func (h *gatewayTaskHost) submit(ctx context.Context, plan publishing.Plan, request gateway.TaskRequest, name string) (tasks.Task, error) {
	suffix := "-apply"
	if request.ActionID == gateway.NginxActionRemove {
		suffix = "-remove"
	}
	idempotency := gatewayTaskIdempotencyKey(plan.ID, request.ResourceID, suffix, len(plan.GatewayIDs) > 1)
	task, err := h.tasks.Submit(ctx, tasks.SubmitRequest{
		SubjectID: request.SubjectID, RequestID: request.RequestID, Name: name,
		ActionID: request.ActionID, OperationClass: tasks.OperationClassModifying,
		TargetType: "node_ids", TargetNodeIDs: request.NodeIDs,
		Params: request.Params, IdempotencyKey: idempotency,
		RequireApproval: true,
	})
	if err != nil {
		return tasks.Task{}, err
	}
	if task.ID == "" {
		return tasks.Task{}, gateway.ErrTaskSubmitterUnavailable
	}
	return task, nil
}

func (h *gatewayTaskHost) wait(ctx context.Context, taskID string, initial tasks.TaskStatus) (string, error) {
	deadline := h.now().Add(h.executeWait)
	approvalDeadline := h.now().Add(h.approvalWait)
	status := initial
	for {
		switch status {
		case tasks.StatusCompleted:
			task, err := h.tasks.Get(ctx, taskID)
			if err != nil {
				return "", err
			}
			if err := gatewayCompleted(task); err != nil {
				return "", err
			}
			return taskID, nil
		case tasks.StatusPartialFailure, tasks.StatusFailed, tasks.StatusCanceled, tasks.StatusExpired:
			task, err := h.tasks.Get(ctx, taskID)
			if err != nil {
				return "", err
			}
			return "", fmt.Errorf("%w: %s", publishing.ErrProductionTarget, task.Status)
		case tasks.StatusUnknown:
			return "", fmt.Errorf("%w: task %s is unknown", ErrGatewayTaskUncertain, taskID)
		}
		now := h.now()
		if status == tasks.StatusAwaitingApproval && !now.Before(approvalDeadline) {
			return "", fmt.Errorf("%w: task %s", ErrGatewayAwaitingApproval, taskID)
		}
		if !now.Before(deadline) {
			return "", fmt.Errorf("%w: task %s did not reach a terminal state", ErrGatewayTaskUncertain, taskID)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(h.poll):
		}
		task, err := h.tasks.Get(ctx, taskID)
		if err != nil {
			return "", err
		}
		status = task.Status
	}
}

func gatewayCompleted(task tasks.Task) error {
	if task.Status != tasks.StatusCompleted || len(task.Targets) == 0 {
		return fmt.Errorf("%w: task %s is %s", publishing.ErrProductionTarget, task.ID, task.Status)
	}
	for _, target := range task.Targets {
		if target.State != tasks.TargetSucceeded {
			return fmt.Errorf("%w: task %s target %s is %s",
				publishing.ErrProductionTarget, task.ID, target.NodeID, target.State)
		}
	}
	return nil
}
