package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/transport"
)

// agentAllowedActions is the local structural allowlist. Task authorization and
// approvals still come from the sole task service before any offer is sent.
func agentAllowedActions() map[string]string {
	return map[string]string{
		"tasks.command": "1",
		"host.patch":    "1",
		"host.root":     "1",
	}
}

func agentValidationLimits() transport.ValidationLimits {
	return transport.ValidationLimits{
		MaxMessageBytes:    64 << 10,
		MaxTextBytes:       256,
		MaxParametersBytes: 16 << 10,
		MaxOutputBytes:     32 << 10,
		MaxMetrics:         128,
		MaxSyncExecutions:  128,
		MaxCapabilities:    32,
		MaxTimeoutSeconds:  600,
		MaxOfferLifetime:   10 * time.Minute,
		AllowedActions:     agentAllowedActions(),
	}
}

func agentFrameLimits() transport.FrameDecodeLimits {
	return transport.FrameDecodeLimits{MaxFrameBytes: 64 << 10, MaxFields: 64, MaxDepth: 16}
}

type agentLinkBridge struct {
	nodesRepo    *nodes.Repo
	nodesService *nodes.Service
	tasksRepo    *tasks.Repo
	tasks        *tasks.Service
	links        *transport.ControllerLinks
	now          func() time.Time

	seqMu     sync.Mutex
	sequences map[string]uint64
}

func newAgentLinkBridge(nodesRepo *nodes.Repo, nodesService *nodes.Service, tasksRepo *tasks.Repo, now func() time.Time) *agentLinkBridge {
	return &agentLinkBridge{nodesRepo: nodesRepo, nodesService: nodesService, tasksRepo: tasksRepo, now: now, sequences: map[string]uint64{}}
}

func (b *agentLinkBridge) handshake(ctx context.Context, observation transport.LinkObservation, hello *agentv1.AgentHello) ([]string, error) {
	if b == nil || hello == nil || hello.Platform != "linux" || len(hello.CapabilityIds) == 0 {
		return nil, transport.ErrLinkRejected
	}
	node, err := b.nodesRepo.Get(ctx, observation.Identity.NodeID)
	if err != nil || node.Status == nodes.StatusRevoked || node.Status == nodes.StatusSuspended {
		return nil, transport.ErrLinkRejected
	}
	allowed := agentAllowedActions()
	granted := make([]string, 0, len(hello.CapabilityIds))
	for _, capability := range hello.CapabilityIds {
		if allowed[capability] != "" {
			granted = append(granted, capability)
		}
	}
	sort.Strings(granted)
	if len(granted) == 0 {
		return nil, transport.ErrLinkRejected
	}
	return granted, nil
}

func (b *agentLinkBridge) admit(ctx context.Context, observation transport.LinkObservation, envelope *agentv1.Envelope) error {
	offer := envelope.GetTaskOffer()
	if offer == nil {
		return nil
	}
	task, err := b.tasksRepo.Get(ctx, offer.GetTaskId())
	if err != nil || task.Status.Terminal() || task.Status == tasks.StatusAwaitingApproval || task.CancelRequested {
		return transport.ErrLinkRejected
	}
	if task.PlanHash != offer.GetPlanDigestSha256() || task.ActionID != offer.GetActionId() || task.PluginID != offer.GetPluginId() {
		return transport.ErrLinkRejected
	}
	if agentAllowedActions()[task.ActionID] != offer.GetActionSchemaVersion() {
		return transport.ErrLinkRejected
	}
	found := false
	for _, target := range task.Targets {
		if target.NodeID != observation.Identity.NodeID {
			continue
		}
		if target.State != tasks.TargetPending {
			return transport.ErrLinkRejected
		}
		found = true
		break
	}
	if !found {
		return transport.ErrLinkRejected
	}
	node, err := b.nodesRepo.Get(ctx, observation.Identity.NodeID)
	if err != nil || node.Status == nodes.StatusRevoked || node.Status == nodes.StatusSuspended {
		return transport.ErrLinkRejected
	}
	return nil
}

func (b *agentLinkBridge) event(ctx context.Context, observation transport.LinkObservation, envelope *agentv1.Envelope) error {
	if b == nil || envelope == nil || envelope.GetNodeId() != observation.Identity.NodeID {
		return transport.ErrLinkRejected
	}
	requestID := envelope.GetMessageId()
	switch {
	case envelope.GetAgentHello() != nil:
		return nil
	case envelope.GetHeartbeat() != nil:
		return b.applyAgentState(ctx, observation, requestID, nil, nodes.StatusOnline)
	case envelope.GetMetrics() != nil:
		metrics := agentMetrics(envelope.GetMetrics(), b.now().UTC())
		return b.applyAgentState(ctx, observation, requestID, metrics, nodes.StatusOnline)
	case envelope.GetTaskAck() != nil:
		return b.taskAck(ctx, observation, envelope.GetTaskAck())
	case envelope.GetTaskStarted() != nil:
		started := envelope.GetTaskStarted()
		return b.taskLog(ctx, observation, started.GetTaskId(), "info", "agent reported task started")
	case envelope.GetTaskOutput() != nil:
		output := envelope.GetTaskOutput()
		message := sanitizeAgentOutput(output.GetChunk())
		if output.GetTruncated() {
			message += " [output truncated]"
		}
		return b.taskLog(ctx, observation, output.GetTaskId(), "info", message)
	case envelope.GetTaskFinished() != nil:
		return b.taskFinished(ctx, observation, envelope.GetTaskFinished())
	case envelope.GetStateSyncRequest() != nil:
		// The Controller owns state sync requests; an Agent-initiated one is ignored
		// rather than answered with fabricated execution state.
		return nil
	case envelope.GetControlError() != nil:
		return nil
	default:
		return transport.ErrLinkRejected
	}
}

func (b *agentLinkBridge) disconnected(ctx context.Context, observation transport.LinkObservation) error {
	err := b.applyAgentState(ctx, observation, "agent-disconnect", nil, nodes.StatusOffline)
	if errors.Is(err, nodes.ErrNodeRevoked) || errors.Is(err, nodes.ErrNodeNotFound) {
		return nil
	}
	return err
}

func (b *agentLinkBridge) applyAgentState(ctx context.Context, observation transport.LinkObservation, requestID string, metrics *nodes.Metrics, status nodes.Status) error {
	if !validAgentRequestID(requestID) {
		requestID = "agent-state"
	}
	if b.nodesService == nil {
		return transport.ErrLinkRejected
	}
	update := nodes.AgentUpdate{
		NodeID:       observation.Identity.NodeID,
		Status:       status,
		Sequence:     b.nextSequence(observation.Identity.NodeID),
		ObservedAt:   b.now().UTC(),
		Metrics:      metrics,
		SampleSource: nodes.SampleSourceAgent,
	}
	_, err := b.nodesService.ApplyAgentUpdate(ctx, observation.Identity.NodeID, requestID, update)
	return err
}

func validAgentRequestID(value string) bool {
	return value != "" && len(value) <= 128 && utf8.ValidString(value) && !strings.ContainsAny(value, " \t\r\n\x00")
}

func (b *agentLinkBridge) nextSequence(nodeID string) uint64 {
	b.seqMu.Lock()
	defer b.seqMu.Unlock()
	next := uint64(b.now().UnixMilli())
	if next <= b.sequences[nodeID] {
		next = b.sequences[nodeID] + 1
	}
	b.sequences[nodeID] = next
	return next
}

func agentMetrics(batch *agentv1.MetricsBatch, observed time.Time) *nodes.Metrics {
	if batch == nil || len(batch.GetPoints()) == 0 || len(batch.GetPoints()) > 128 {
		return nil
	}
	var cpu, memory *float64
	for _, point := range batch.GetPoints() {
		switch point.GetName() {
		case "cpu", "cpu.percent", "cpu_percent":
			value := point.GetValue()
			cpu = &value
		case "memory", "memory.percent", "memory_percent":
			value := point.GetValue()
			memory = &value
		}
	}
	if cpu == nil || memory == nil {
		return nil
	}
	return &nodes.Metrics{CPUPercent: *cpu, MemoryPercent: *memory, ObservedAt: observed}
}

func (b *agentLinkBridge) taskAck(ctx context.Context, observation transport.LinkObservation, ack *agentv1.TaskAck) error {
	if ack.GetTaskId() == "" || ack.GetExecutionId() == "" {
		return transport.ErrLinkRejected
	}
	if ack.GetAccepted() {
		message := "agent accepted task offer"
		if ack.GetPreviouslySeen() {
			message = "agent acknowledged an already seen execution"
		}
		return b.taskLog(ctx, observation, ack.GetTaskId(), "info", message)
	}
	reason := sanitizeAgentCode(ack.GetReasonCode())
	if reason == "" {
		reason = "agent_rejected"
	}
	_, err := b.tasks.RecordResult(ctx, ack.GetTaskId(), observation.Identity.NodeID, tasks.NodeResultFailed, "agent rejected offer: "+reason)
	if ignorableTaskError(err) {
		return nil
	}
	return err
}

func (b *agentLinkBridge) taskLog(ctx context.Context, observation transport.LinkObservation, taskID, level, message string) error {
	if taskID == "" {
		return nil
	}
	task, err := b.tasksRepo.Get(ctx, taskID)
	if err != nil {
		return nil
	}
	owned := false
	for _, target := range task.Targets {
		if target.NodeID == observation.Identity.NodeID {
			owned = true
			break
		}
	}
	if !owned {
		return nil
	}
	err = b.tasks.AppendLog(ctx, taskID, observation.Identity.NodeID, level, message)
	if ignorableTaskError(err) {
		return nil
	}
	return err
}

func (b *agentLinkBridge) taskFinished(ctx context.Context, observation transport.LinkObservation, finished *agentv1.TaskFinished) error {
	if finished.GetTaskId() == "" {
		return transport.ErrLinkRejected
	}
	code := tasks.NodeResultUnknown
	switch finished.GetResult() {
	case agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED:
		code = tasks.NodeResultSucceeded
	case agentv1.ExecutionResult_EXECUTION_RESULT_FAILED:
		code = tasks.NodeResultFailed
	case agentv1.ExecutionResult_EXECUTION_RESULT_CANCELED:
		code = tasks.NodeResultCanceled
	case agentv1.ExecutionResult_EXECUTION_RESULT_TIMED_OUT:
		code = tasks.NodeResultTimedOut
	}
	message := sanitizeAgentCode(finished.GetErrorCode())
	if message == "" {
		message = "agent reported " + string(code)
	}
	_, err := b.tasks.RecordResult(ctx, finished.GetTaskId(), observation.Identity.NodeID, code, message)
	if ignorableTaskError(err) {
		return nil
	}
	return err
}

func ignorableTaskError(err error) bool {
	return err == nil || errors.Is(err, tasks.ErrTaskTerminal) || errors.Is(err, tasks.ErrDuplicateResult) ||
		errors.Is(err, tasks.ErrTaskNotFound) || errors.Is(err, tasks.ErrTargetNotFound) || errors.Is(err, tasks.ErrInvalidStatus)
}

func sanitizeAgentCode(code string) string {
	if code == "" || len(code) > 128 {
		return ""
	}
	out := make([]rune, 0, len(code))
	for _, r := range strings.ToLower(code) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' {
			out = append(out, r)
		}
	}
	if len(out) == 0 || len(out) > 128 {
		return ""
	}
	return string(out)
}

func sanitizeAgentOutput(chunk []byte) string {
	if len(chunk) == 0 {
		return "[empty output]"
	}
	if !utf8.Valid(chunk) {
		return "[non-UTF-8 output omitted]"
	}
	text := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, string(chunk))
	text = strings.TrimSpace(text)
	if text == "" {
		return "[empty output]"
	}
	if len(text) > 4000 {
		text = text[:4000] + "...[truncated]"
	}
	return text
}

func executionIDFor(taskID, nodeID, planHash string) string {
	digest := sha256.Sum256([]byte(taskID + "\x00" + nodeID + "\x00" + planHash))
	return "exec-" + hex.EncodeToString(digest[:16])
}

// Dispatch implements tasks.Dispatcher over the current Agent link.
func (b *agentLinkBridge) Dispatch(ctx context.Context, command tasks.Command) (tasks.DispatchResult, error) {
	if b == nil || b.links == nil || b.tasksRepo == nil {
		return tasks.DispatchResult{Accepted: false, Reason: "agent_link_unavailable"}, nil
	}
	task, err := b.tasksRepo.Get(ctx, command.TaskID)
	if err != nil || task.PlanHash == "" {
		return tasks.DispatchResult{Accepted: false, Reason: "task_unavailable"}, nil
	}
	found := false
	for _, target := range task.Targets {
		if target.NodeID == command.NodeID && target.State == tasks.TargetPending {
			found = true
			break
		}
	}
	if !found {
		return tasks.DispatchResult{Accepted: false, Reason: "target_unavailable"}, nil
	}
	offer := &agentv1.TaskOffer{
		TaskId:                  task.ID,
		ExecutionId:             executionIDFor(task.ID, command.NodeID, task.PlanHash),
		PlanDigestSha256:        task.PlanHash,
		ActionId:                command.ActionID,
		CanonicalParametersJson: append([]byte(nil), command.Params...),
		Privilege:               agentv1.Privilege_PRIVILEGE_NORMAL,
		ExpiresAtUnixMs:         b.now().Add(10 * time.Minute).UnixMilli(),
		TimeoutSeconds:          300,
		ScopeRevision:           task.ScopeRevision,
		PluginId:                task.PluginID,
		ActionSchemaVersion:     agentAllowedActions()[command.ActionID],
	}
	envelope := &agentv1.Envelope{Message: &agentv1.Envelope_TaskOffer{TaskOffer: offer}}
	err = b.links.Send(ctx, command.NodeID, envelope)
	switch {
	case err == nil:
		return tasks.DispatchResult{Accepted: true}, nil
	case errors.Is(err, transport.ErrDeliveryUnknown):
		return tasks.DispatchResult{Unknown: true}, nil
	default:
		return tasks.DispatchResult{Accepted: false, Reason: "agent_unavailable"}, nil
	}
}

// Cancel implements tasks.Dispatcher over the same link.
func (b *agentLinkBridge) Cancel(ctx context.Context, command tasks.CancelCommand) error {
	if b == nil || b.links == nil || b.tasksRepo == nil {
		return transport.ErrLinkRejected
	}
	task, err := b.tasksRepo.Get(ctx, command.TaskID)
	if err != nil {
		return err
	}
	envelope := &agentv1.Envelope{Message: &agentv1.Envelope_TaskCancel{TaskCancel: &agentv1.TaskCancel{
		TaskId:            task.ID,
		ExecutionId:       executionIDFor(task.ID, command.NodeID, task.PlanHash),
		ReasonCode:        "operator_cancel",
		RequestedAtUnixMs: b.now().UnixMilli(),
	}}}
	return b.links.Send(ctx, command.NodeID, envelope)
}

var _ tasks.Dispatcher = (*agentLinkBridge)(nil)
