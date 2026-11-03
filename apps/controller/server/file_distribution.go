package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"time"

	"google.golang.org/protobuf/proto"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/files"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/transport"
)

// distributionPlan freezes one multi-target staging push: every target's
// identity, per-node mapped write path, size and digest. The staging bytes
// themselves never enter the task parameters.
func (w *FileExecutionWorker) distributionPlan(ctx context.Context, c files.DistributionCommand) (fileTaskPlan, map[string]string, error) {
	distribution, err := w.repo.GetDistribution(ctx, c.DistributionID)
	if err != nil {
		return fileTaskPlan{}, nil, err
	}
	if distribution.SubjectID != c.SubjectID || distribution.Status != files.DistributionOpen {
		return fileTaskPlan{}, nil, files.ErrTransferIdentity
	}
	if len(c.TargetNodes) == 0 || len(c.TargetNodes) > files.MaxDistributionTargets {
		return fileTaskPlan{}, nil, files.ErrTransferIdentity
	}
	if len(distribution.Targets) != len(c.TargetNodes) {
		return fileTaskPlan{}, nil, files.ErrTransferIdentity
	}
	frozen := map[string]files.DistributionTarget{}
	for _, target := range distribution.Targets {
		frozen[target.NodeID] = target
	}
	seen := map[string]bool{}
	items := make([]filePlanItem, 0, len(c.TargetNodes))
	p := fileTaskPlan{Kind: "distribute", TransferID: c.DistributionID, ConflictPolicy: files.ConflictOverwrite, UploadSize: c.Size}
	for _, node := range c.TargetNodes {
		if _, ok := frozen[node]; !ok || seen[node] {
			return fileTaskPlan{}, nil, files.ErrTransferIdentity
		}
		seen[node] = true
		if _, err := w.server.NodesRepo.Get(ctx, node); err != nil {
			return fileTaskPlan{}, nil, err
		}
		write, err := w.config.Paths.RelativePath(ctx, node, c.DestinationPath)
		if err != nil || !transport.FilePath(write) {
			return fileTaskPlan{}, nil, files.ErrPathMappingUnavailable
		}
		items = append(items, filePlanItem{Source: c.DestinationPath, Destination: c.DestinationPath, WritePath: write, ExpectedSHA256: c.SHA256Expected, Node: node})
	}
	p.Items = items
	params := map[string]string{
		"kind": "distribute", "upload_id": c.DistributionID, "destination": c.DestinationPath,
		"sha256": c.SHA256Expected, "size": strconv.FormatInt(c.Size, 10),
		"conflict_policy": string(files.ConflictOverwrite), "targets": strconv.Itoa(len(c.TargetNodes)),
	}
	for i, item := range items {
		raw, err := json.Marshal(item)
		if err != nil || len(raw) > 1024 {
			return fileTaskPlan{}, nil, files.ErrTransferIdentity
		}
		params["item_"+strconv.Itoa(i)] = string(raw)
	}
	return p, params, nil
}

// ValidateDistribution validates the frozen distribution plan without submitting it.
func (w *FileExecutionWorker) ValidateDistribution(ctx context.Context, c files.DistributionCommand) error {
	_, _, err := w.distributionPlan(ctx, c)
	return err
}

// SubmitDistribution moves a verified staging distribution into the sole
// unified task queue under the files.transfer action with one target per
// frozen destination node. The task freezes the target identities, mapped
// write paths, size and digest; the staging bytes never enter the parameters.
func (w *FileExecutionWorker) SubmitDistribution(ctx context.Context, c files.DistributionCommand) (string, error) {
	p, params, err := w.distributionPlan(ctx, c)
	if err != nil {
		return "", err
	}
	// The worker-mapped per-node write paths freeze into the target rows
	// BEFORE submission so every frame validates against one durable plan.
	paths := make(map[string]string, len(p.Items))
	for _, item := range p.Items {
		paths[item.Node] = item.WritePath
	}
	if err := w.repo.FreezeDistributionTargetPaths(ctx, c.DistributionID, paths); err != nil {
		return "", err
	}
	t, err := w.server.Tasks.Submit(ctx, tasks.SubmitRequest{
		SubjectID: c.SubjectID, RequestID: c.RequestID, Name: "file distribution", ActionID: files.ActionTransfer,
		OperationClass: tasks.OperationClassStandard, TargetType: "node_ids", TargetNodeIDs: c.TargetNodes,
		Params: params, IdempotencyKey: c.DistributionID,
	})
	return t.ID, err
}

// decodeDistributeTask validates the frozen multi-target plan against the
// persisted task: exact target set, per-target mapped write paths, size and
// digest. Params must be exactly the frozen keys.
func decodeDistributeTask(t tasks.Task, params map[string]string) (fileTaskPlan, error) {
	var p fileTaskPlan
	count, err := strconv.Atoi(params["targets"])
	if err != nil || count <= 0 || count > files.MaxDistributionTargets || len(t.Targets) != count || len(params) != 7+count {
		return p, files.ErrTransferIdentity
	}
	size, err := strconv.ParseInt(params["size"], 10, 63)
	if err != nil || size < 0 || size > int64(transport.FileMaxBytes) {
		return p, files.ErrTransferIdentity
	}
	p = fileTaskPlan{Kind: "distribute", TransferID: params["upload_id"], ConflictPolicy: files.ConflictPolicy(params["conflict_policy"]), UploadSize: size}
	if !transport.FileID(p.TransferID) || p.ConflictPolicy != files.ConflictOverwrite || files.ValidateRemotePath(params["destination"]) != nil || params["sha256"] != "" && !transport.FileDigest(params["sha256"]) {
		return p, files.ErrTransferIdentity
	}
	targetSet := map[string]bool{}
	for _, n := range t.Targets {
		targetSet[n.NodeID] = true
	}
	itemNodes := map[string]bool{}
	for i := 0; i < count; i++ {
		raw, ok := params["item_"+strconv.Itoa(i)]
		if !ok {
			return p, files.ErrTransferIdentity
		}
		var item filePlanItem
		if json.Unmarshal([]byte(raw), &item) != nil || !transport.FilePath(item.WritePath) || files.ValidateRemotePath(item.Source) != nil || item.Source != params["destination"] || item.ExpectedSHA256 != params["sha256"] || item.Destination != params["destination"] || item.ReadPath != "" || !transport.FileID(item.Node) || !targetSet[item.Node] || itemNodes[item.Node] {
			return p, files.ErrTransferIdentity
		}
		itemNodes[item.Node] = true
		p.Items = append(p.Items, item)
	}
	if len(itemNodes) != count {
		return p, files.ErrTransferIdentity
	}
	for _, n := range t.Targets {
		if !itemNodes[n.NodeID] {
			return p, files.ErrTransferIdentity
		}
	}
	return p, nil
}

// liveDistribute re-checks the shared frozen plan invariants plus, for the
// given frame node, that node's CURRENT online state and write permission.
// A revoked or offline sibling target never fails another target's frame.
func (w *FileExecutionWorker) liveDistribute(ctx context.Context, t tasks.Task, p fileTaskPlan, frameNode string) error {
	if frameNode != "" {
		node, err := w.server.NodesRepo.Get(ctx, frameNode)
		if err != nil || node.Status != nodes.StatusOnline {
			return files.ErrAgentOffline
		}
		if err := w.authorizer.Authorize(ctx, auth.Check{SubjectID: t.SubmitterID, Action: files.ActionWrite, NodeIDs: []string{frameNode}}); err != nil {
			return files.ErrAgentUnauthorized
		}
	}
	distribution, err := w.repo.GetDistribution(ctx, p.TransferID)
	if err != nil {
		return err
	}
	if distribution.Status != files.DistributionDispatching || distribution.TaskID != t.ID || distribution.SubjectID != t.SubmitterID {
		return files.ErrTransferIdentity
	}
	if distribution.SHA256Actual != "" && distribution.SHA256Actual != p.Items[0].ExpectedSHA256 {
		return files.ErrTransferIdentity
	}
	if distribution.SHA256Expected != "" && distribution.SHA256Expected != p.Items[0].ExpectedSHA256 {
		return files.ErrTransferIdentity
	}
	if distribution.BytesReceived != p.UploadSize {
		return files.ErrTransferIdentity
	}
	if len(distribution.Targets) != len(p.Items) {
		return files.ErrTransferIdentity
	}
	byNode := map[string]files.DistributionTarget{}
	for _, target := range distribution.Targets {
		byNode[target.NodeID] = target
	}
	for _, item := range p.Items {
		target, ok := byNode[item.Node]
		if !ok || target.WritePath != item.WritePath {
			return files.ErrPathMappingUnavailable
		}
		// Frames never re-push a target whose honest terminal result is already
		// persisted: the per-frame fence for this exact node.
		if frameNode == item.Node && target.Status == files.TargetStateCompleted {
			return files.ErrTransferIdentity
		}
		relative, err := w.config.Paths.RelativePath(ctx, item.Node, item.Source)
		if err != nil || relative != item.WritePath {
			return files.ErrPathMappingUnavailable
		}
	}
	return nil
}

// executeDistribute streams the verified staging file to every frozen target
// through the controlled agent channel. One target failing, being offline,
// revoked or cancelled never overwrites or stops the other targets: each
// target converges to its own honest terminal row, and already-succeeded
// targets are never replayed.
func (w *FileExecutionWorker) executeDistribute(ctx context.Context, t tasks.Task, p fileTaskPlan) (map[string]tasks.WorkerResult, error) {

	// BindDistributionTask binds the distribution to this task in a separate
	// durable transaction. No child effects before that exact linkage exists.
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		d, e := w.repo.GetDistribution(ctx, p.TransferID)
		if e != nil {
			return nil, e
		}
		if d.TaskID == t.ID {
			break
		}
		if d.TaskID != "" {
			return nil, files.ErrTransferIdentity
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, files.ErrTransferIdentity
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := w.liveDistribute(ctx, t, p, ""); err != nil {
		return nil, err
	}
	staging, _, err := w.server.Files.DistributionStaging(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	defer staging.Close()
	digest := p.Items[0].ExpectedSHA256
	results := make(map[string]tasks.WorkerResult, len(p.Items))
	for i, item := range p.Items {
		// An already-succeeded target keeps its result and is never replayed,
		// even when a restarted execution re-runs the frozen plan.
		targets, e := w.repo.ListDistributionTargets(ctx, p.TransferID)
		if e != nil {
			return nil, e
		}
		done := false
		for _, row := range targets {
			if row.NodeID == item.Node && row.Status == files.TargetStateCompleted {
				results[item.Node] = tasks.WorkerResult{Code: tasks.NodeResultSucceeded, Message: "file distribution target already completed; not replayed"}
				done = true
				break
			}
		}
		if done {
			if i < len(p.Items)-1 {
				if _, err := staging.Seek(0, io.SeekStart); err != nil {
					return nil, err
				}
			}
			continue
		}
		code, targetStatus, errorCode, message, bytes := w.distributeTarget(ctx, t, p, item, i, staging, digest)
		results[item.Node] = tasks.WorkerResult{Code: code, Message: message}
		finishCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		_, updateErr := w.server.Files.ApplyDistributionTaskUpdate(finishCtx, t.ID, files.DistributionTaskUpdate{
			TargetNode: item.Node, Status: targetStatus, Bytes: bytes, SHA256: digest,
			ErrorCode: errorCode, ErrorMessage: message, RequestID: t.RequestID,
		})
		stop()
		if updateErr != nil {
			return nil, updateErr
		}
		// Re-open the staging file for the next target: each target streams
		// the same frozen bytes from the beginning.
		if i < len(p.Items)-1 {
			if _, err := staging.Seek(0, io.SeekStart); err != nil {
				return nil, err
			}
		}
	}
	return results, nil
}

// distributeTarget streams the staging bytes to one target and returns the
// honest per-target outcome.
func (w *FileExecutionWorker) distributeTarget(ctx context.Context, t tasks.Task, p fileTaskPlan, item filePlanItem, i int, staging io.Reader, digest string) (code tasks.NodeResultCode, targetStatus, errorCode, message string, bytes int64) {
	var (
		e error
	)
	code, targetStatus, errorCode, message, bytes = tasks.NodeResultSucceeded, files.TargetStateCompleted, "", "file distribution target succeeded", p.UploadSize
	_, _ = w.repo.SetDistributionTargetState(ctx, p.TransferID, item.Node, files.TargetStateDispatching)
	execution := childExecutionID(t, item.Node, i)
	var session *transport.FileSession
	var opened *agentv1.FileOpenReply
	defer func() {
		if session != nil {
			session.Close()
		}
	}()
	defer func() {
		if e != nil {
			w.cancelChild(t.ID, item.Node, execution)
		}
	}()
	if e = w.liveDistribute(ctx, t, p, item.Node); e != nil {
		bytes = 0
		if errors.Is(e, files.ErrAgentOffline) {
			code, targetStatus, errorCode, message = tasks.NodeResultFailed, files.TargetStateFailed, "agent_offline", "target node is offline"
			return
		}
		if errors.Is(e, files.ErrAgentUnauthorized) {
			code, targetStatus, errorCode, message = tasks.NodeResultFailed, files.TargetStateFailed, "denied_now", "current permission denied before destination write"
			return
		}
		code, targetStatus, errorCode, message, bytes = tasks.NodeResultUnknown, files.TargetStateUnknown, files.UploadErrorAgentUnknown, "file execution uncertain; reconcile without replay", 0
		return
	}
	if e == nil {
		session, opened, e = w.openDistributionSession(ctx, t, p, item, i, execution, digest)
	}
	if e == nil && opened.RelativePath != item.WritePath {
		e = files.ErrTransferIdentity
	}
	if e == nil && !opened.Skipped {
		buffer := make([]byte, transport.FileChunkBytes)
		defer clear(buffer)
		for offset := uint64(0); offset < uint64(p.UploadSize) || p.UploadSize == 0; {
			limit := min(uint64(len(buffer)), uint64(p.UploadSize)-offset)
			if _, e = io.ReadFull(staging, buffer[:limit]); e != nil {
				break
			}
			var reply *agentv1.FileChunkReply
			reply, e = session.Chunk(ctx, &agentv1.FileChunkRequest{TaskId: t.ID, ExecutionId: execution, TransferId: execution, Offset: offset, Data: buffer[:limit], Finish: offset+limit == uint64(p.UploadSize)})
			if e != nil {
				break
			}
			offset = reply.NextOffset
			if reply.Complete {
				break
			}
		}
	}
	if e == nil {
		if e = w.waitDone(ctx, execution); e == nil {
			if e = w.liveDistribute(ctx, t, p, item.Node); e != nil {
				if errors.Is(e, files.ErrAgentUnauthorized) {
					code, targetStatus, errorCode, message = tasks.NodeResultFailed, files.TargetStateFailed, "denied_now", "current permission denied during destination write"
					return
				}
			}
		}
	}
	if e != nil {
		// Unknown stays the honest default: a binding in the opened/done
		// states, a binding query failure, or an unreadable task leaves the
		// outcome uncertain and reconciles without replay.
		code, targetStatus, errorCode, message, bytes = tasks.NodeResultUnknown, files.TargetStateUnknown, files.UploadErrorAgentUnknown, "file execution uncertain; reconcile without replay", 0
		binding, stateErr := w.repo.Execution(ctx, execution)
		beforeWrite := false
		if stateErr == nil {
			beforeWrite = binding.State == "" || binding.State == "reserved" || binding.State == "offered" || binding.State == "accepted"
		}
		current, taskErr := w.server.TasksRepo.Get(ctx, t.ID)
		switch {
		case taskErr == nil && current.CancelRequested && beforeWrite:
			code, targetStatus, errorCode, message = tasks.NodeResultCanceled, files.TargetStateCancelled, "cancelled_before_write", "cancelled before destination write"
		case errors.Is(e, files.ErrAgentChecksum) && beforeWrite:
			code, targetStatus, errorCode, message = tasks.NodeResultFailed, files.TargetStateFailed, "denied_before_write", "checksum rejected before destination write"
		}
	}
	return
}

// openDistributionSession opens one write session for a single target. The
// Gate re-checks current identity, permissions and mapping for every frame.
func (w *FileExecutionWorker) openDistributionSession(ctx context.Context, t tasks.Task, p fileTaskPlan, item filePlanItem, i int, execution, digest string) (*transport.FileSession, *agentv1.FileOpenReply, error) {
	parameters := transport.FileParameters{TransferID: execution, RelativePath: item.WritePath, Direction: "write", Size: uint64(p.UploadSize), SHA256: digest, ConflictPolicy: string(p.ConflictPolicy)}
	raw, _ := json.Marshal(parameters)
	offer := &agentv1.TaskOffer{TaskId: t.ID, ExecutionId: execution, PlanDigestSha256: t.PlanHash, ActionId: transport.FileCapability, CanonicalParametersJson: raw, Privilege: agentv1.Privilege_PRIVILEGE_NORMAL, ExpiresAtUnixMs: time.Now().Add(5 * time.Minute).UnixMilli(), TimeoutSeconds: 300, ScopeRevision: t.ScopeRevision, ActionSchemaVersion: "1"}
	observation, err := w.server.Links.FileConnection(ctx, item.Node)
	if err != nil {
		return nil, nil, err
	}
	offerRaw, _ := proto.Marshal(offer)
	if err = w.repo.ReserveExecution(ctx, files.ExecutionBinding{ExecutionID: execution, TaskID: t.ID, TransferID: p.TransferID, SubjectID: t.SubmitterID, NodeID: item.Node, Direction: "write", ParentPlan: t.PlanHash, Observation: observationKey(observation), Sequence: i, Offer: offerRaw, State: "reserved"}, w.server.Audit); err != nil {
		return nil, nil, err
	}
	session, err := w.server.AgentFiles.NewSession(ctx, item.Node, offer)
	if err != nil {
		return nil, nil, err
	}
	reply, err := session.Open(ctx)
	if err != nil {
		session.Close()
		w.cancelChild(t.ID, item.Node, execution)
		return nil, nil, err
	}
	return session, reply, nil
}
