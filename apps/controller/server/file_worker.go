package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"google.golang.org/protobuf/proto"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/files"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/transport"
)

// FileExecutionConfig is trusted PROGRAM configuration, never an HTTP DTO.
// Paths map logical remote paths to each Agent's explicit private file-root.
// AI/plugin tasks require an additional current scope gate; absent means deny.
type FileExecutionConfig struct {
	Paths     files.PathMapper
	SpoolDir  string
	ScopeGate func(context.Context, tasks.Task) error
}
type FileExecutionWorker struct {
	server     *Server
	repo       *files.Repo
	authorizer auth.Authorizer
	config     FileExecutionConfig
}
type filePlanItem struct{ Source, Destination, ReadPath, WritePath, ExpectedSHA256 string }
type fileTaskPlan struct {
	TransferID, SourceNode, DestinationNode string
	Items                                   []filePlanItem
}

func newFileExecutionWorker(s *Server, repo *files.Repo, authorizer auth.Authorizer, cfg FileExecutionConfig) (*FileExecutionWorker, error) {
	if cfg.Paths == nil || cfg.SpoolDir == "" {
		return nil, files.ErrPathMappingUnavailable
	}
	if err := files.CheckExecutionSpool(cfg.SpoolDir); err != nil {
		return nil, err
	}
	return &FileExecutionWorker{s, repo, authorizer, cfg}, nil
}
func (w *FileExecutionWorker) plan(ctx context.Context, c files.TransferCommand) (fileTaskPlan, map[string]string, error) {
	p := fileTaskPlan{TransferID: c.TransferID, SourceNode: c.SourceNodeID, DestinationNode: c.DestinationNodeID}
	if c.ConflictPolicy != files.ConflictSkip || len(c.SourcePaths) == 0 || len(c.SourcePaths) > 16 || !transport.FileID(c.TransferID) {
		return p, nil, files.ErrInvalidTransfer
	}
	params := map[string]string{"transfer_id": c.TransferID, "source_node": c.SourceNodeID, "dest_node": c.DestinationNodeID, "conflict_policy": "skip"}
	seen := map[string]bool{}
	for i, source := range c.SourcePaths {
		name := source
		for n := len(source) - 1; n >= 0; n-- {
			if source[n] == '/' {
				name = source[n+1:]
				break
			}
		}
		dest, err := files.JoinRemotePath(c.DestinationDir, name)
		if err != nil || seen[dest] {
			return p, nil, files.ErrInvalidTransfer
		}
		seen[dest] = true
		read, err := w.config.Paths.RelativePath(ctx, c.SourceNodeID, source)
		if err != nil || !transport.FilePath(read) {
			return p, nil, files.ErrInvalidTransfer
		}
		write, err := w.config.Paths.RelativePath(ctx, c.DestinationNodeID, dest)
		if err != nil || !transport.FilePath(write) {
			return p, nil, files.ErrInvalidTransfer
		}
		item := filePlanItem{source, dest, read, write, c.Checksums[source]}
		raw, _ := json.Marshal(item)
		if len(raw) > 1024 {
			return p, nil, files.ErrInvalidTransfer
		}
		params["item_"+strconv.Itoa(i)] = string(raw)
		p.Items = append(p.Items, item)
	}
	return p, params, nil
}
func (w *FileExecutionWorker) ValidateTransfer(ctx context.Context, c files.TransferCommand) error {
	_, _, err := w.plan(ctx, c)
	return err
}
func (w *FileExecutionWorker) SubmitTransfer(ctx context.Context, c files.TransferCommand) (string, error) {
	_, params, err := w.plan(ctx, c)
	if err != nil {
		return "", err
	}
	t, err := w.server.Tasks.Submit(ctx, tasks.SubmitRequest{SubjectID: c.SubjectID, RequestID: c.RequestID, Name: "file transfer", ActionID: files.ActionTransfer, OperationClass: tasks.OperationClassStandard, TargetType: "node_ids", TargetNodeIDs: []string{c.SourceNodeID, c.DestinationNodeID}, Params: params, IdempotencyKey: c.TransferID})
	return t.ID, err
}
func decodeFileTask(t tasks.Task) (fileTaskPlan, error) {
	var params map[string]string
	var p fileTaskPlan
	if json.Unmarshal([]byte(t.Params), &params) != nil || params["conflict_policy"] != "skip" || t.ActionID != files.ActionTransfer || t.OperationClass != tasks.OperationClassStandard || len(t.Targets) != 2 {
		return p, files.ErrTransferIdentity
	}
	p.TransferID, p.SourceNode, p.DestinationNode = params["transfer_id"], params["source_node"], params["dest_node"]
	if !transport.FileID(p.TransferID) || p.SourceNode == p.DestinationNode {
		return p, files.ErrTransferIdentity
	}
	target := map[string]bool{}
	for _, n := range t.Targets {
		target[n.NodeID] = true
	}
	if !target[p.SourceNode] || !target[p.DestinationNode] {
		return p, files.ErrTransferIdentity
	}
	for i := 0; i < 16; i++ {
		raw, ok := params["item_"+strconv.Itoa(i)]
		if !ok {
			break
		}
		var item filePlanItem
		if json.Unmarshal([]byte(raw), &item) != nil || !transport.FilePath(item.ReadPath) || !transport.FilePath(item.WritePath) || files.ValidateRemotePath(item.Source) != nil || files.ValidateRemotePath(item.Destination) != nil || item.ExpectedSHA256 != "" && !transport.FileDigest(item.ExpectedSHA256) {
			return p, files.ErrTransferIdentity
		}
		p.Items = append(p.Items, item)
	}
	if len(p.Items) == 0 || len(params) != 4+len(p.Items) {
		return p, files.ErrTransferIdentity
	}
	return p, nil
}
func (w *FileExecutionWorker) live(ctx context.Context, t tasks.Task, p fileTaskPlan) error {
	current, err := w.server.Tasks.CheckExecution(ctx, t.ID, t.PlanHash)
	if err != nil || current.Params != t.Params || current.SubmitterID != t.SubmitterID {
		return files.ErrTransferExecution
	}
	if current.Source != string(audit.SourceWeb) && w.config.ScopeGate == nil {
		return files.ErrTransferExecution
	}
	if w.config.ScopeGate != nil && w.config.ScopeGate(ctx, current) != nil {
		return files.ErrTransferExecution
	}
	for _, check := range []auth.Check{{SubjectID: t.SubmitterID, Action: files.ActionRead, NodeIDs: []string{p.SourceNode}}, {SubjectID: t.SubmitterID, Action: files.ActionWrite, NodeIDs: []string{p.DestinationNode}}} {
		node, err := w.server.NodesRepo.Get(ctx, check.NodeIDs[0])
		if err != nil || node.Status != nodes.StatusOnline {
			return files.ErrAgentUnauthorized
		}
		if err := w.authorizer.Authorize(ctx, check); err != nil {
			return err
		}
	}
	transfer, err := w.repo.GetTransfer(ctx, p.TransferID)
	if err != nil || transfer.TaskID != t.ID || transfer.SubmitterID != t.SubmitterID || transfer.Status.Terminal() || transfer.Status == files.TransferCancelling || transfer.SourceNodeID != p.SourceNode || transfer.DestinationNodeID != p.DestinationNode || len(transfer.Items) != len(p.Items) || transfer.ConflictPolicy != files.ConflictSkip {
		return files.ErrTransferIdentity
	}
	for i, item := range p.Items {
		if transfer.Items[i].SourcePath != item.Source || transfer.Items[i].DestinationPath != item.Destination {
			return files.ErrTransferIdentity
		}
		r, e := w.config.Paths.RelativePath(ctx, p.SourceNode, item.Source)
		if e != nil || r != item.ReadPath {
			return files.ErrPathMappingUnavailable
		}
		r, e = w.config.Paths.RelativePath(ctx, p.DestinationNode, item.Destination)
		if e != nil || r != item.WritePath {
			return files.ErrPathMappingUnavailable
		}
	}
	return nil
}
func observationKey(o transport.LinkObservation) string {
	raw, _ := json.Marshal(o)
	return string(raw)
}
func childExecutionID(t tasks.Task, node string, i int) string {
	h := sha256.Sum256([]byte(t.ID + "\x00" + node + "\x00" + strconv.Itoa(i) + "\x00" + t.PlanHash))
	return "file-" + hex.EncodeToString(h[:20])
}

// Gate persists the real durable Agent TaskAck BEFORE FileClient can emit Open.
// Every request AND reply repeats current identity/task/cancel/approval/mapping/
// both endpoint permissions. No once-only or default-allow authorization.
func (w *FileExecutionWorker) Gate(ctx context.Context, o transport.LinkObservation, offer *agentv1.TaskOffer, e *agentv1.Envelope) error {
	b, err := w.repo.Execution(ctx, offer.ExecutionId)
	if err != nil {
		return err
	}
	stored := new(agentv1.TaskOffer)
	if proto.Unmarshal(b.Offer, stored) != nil || !proto.Equal(stored, offer) || b.NodeID != o.Identity.NodeID || b.Observation != observationKey(o) {
		return files.ErrTransferIdentity
	}
	t, err := w.server.TasksRepo.Get(ctx, b.TaskID)
	if err != nil {
		return err
	}
	p, err := decodeFileTask(t)
	if err != nil || w.live(ctx, t, p) != nil || b.ParentPlan != t.PlanHash || b.TransferID != p.TransferID {
		return files.ErrTransferExecution
	}
	return w.repo.ObserveExecution(ctx, b.ExecutionID, "frame", w.server.Audit, func(b *files.ExecutionBinding) error {
		parameters, err := transport.FileOffer(offer)
		if err != nil {
			return err
		}
		if request := e.GetTaskOffer(); request != nil {
			if b.State != "reserved" && b.State != "offered" {
				return files.ErrTransferExecution
			}
			b.State = "offered"
			return nil
		}
		if ack := e.GetTaskAck(); ack != nil {
			if b.State != "offered" && b.State != "accepted" {
				return files.ErrTransferExecution
			}
			if !ack.Accepted || ack.PreviouslySeen {
				return files.ErrAgentUnknown
			}
			b.State = "accepted"
			return nil
		}
		if r := e.GetFileOpenRequest(); r != nil {
			if b.State != "accepted" {
				return files.ErrTransferExecution
			}
			if r.RelativePath != parameters.RelativePath || r.Direction != parameters.Direction || r.PlanDigestSha256 != offer.PlanDigestSha256 || r.Size != parameters.Size || r.Sha256 != parameters.SHA256 || r.ResumeOffset != 0 || r.CreateParents || r.ConflictPolicy != "skip" {
				return files.ErrTransferIdentity
			}
			return nil
		}
		if r := e.GetFileOpenReply(); r != nil {
			if b.State != "accepted" && b.State != "opened" && b.State != "done" {
				return files.ErrTransferExecution
			}
			if b.State != "accepted" && (b.Size != r.Size || b.SHA256 != r.Sha256) {
				return files.ErrTransferIdentity
			}
			b.Size = r.Size
			b.SHA256 = r.Sha256
			b.Complete = r.Complete
			if b.State != "done" {
				b.State = "opened"
			}
			return nil
		}
		if r := e.GetFileChunkRequest(); r != nil {
			if b.State != "opened" || b.Result != "" {
				return files.ErrTransferExecution
			}
			if r.Offset != b.Offset || r.Cancel {
				return files.ErrTransferExecution
			}
			next := r.Offset + uint64(len(r.Data))
			if b.Direction == "read" {
				next = r.Offset + uint64(r.ReadBytes)
				if r.ReadBytes == 0 || len(r.Data) > 0 || r.Finish {
					return files.ErrTransferIdentity
				}
			} else if r.ReadBytes != 0 || r.Finish != (next == b.Size) {
				return files.ErrTransferIdentity
			}
			if next > b.Size {
				return files.ErrTransferIdentity
			}
			return nil
		}
		if r := e.GetFileChunkReply(); r != nil {
			if b.State != "opened" && b.State != "done" {
				return files.ErrTransferExecution
			}
			if r.NextOffset > b.Size || r.Offset > r.NextOffset || r.Offset != b.Offset && r.NextOffset != b.Offset {
				return files.ErrTransferIdentity
			}
			b.Offset = r.NextOffset
			if r.Complete {
				b.Complete = true
			}
			return nil
		}
		return files.ErrTransferExecution
	})
}

// Observe consumes child lifecycle events; only the worker returns parent results.
func (w *FileExecutionWorker) Observe(ctx context.Context, o transport.LinkObservation, e *agentv1.Envelope) (bool, error) {
	task, execution := "", ""
	if a := e.GetTaskAck(); a != nil {
		task, execution = a.TaskId, a.ExecutionId
	}
	if a := e.GetTaskStarted(); a != nil {
		task, execution = a.TaskId, a.ExecutionId
	}
	if a := e.GetTaskFinished(); a != nil {
		task, execution = a.TaskId, a.ExecutionId
	}
	if task == "" {
		return false, nil
	}
	t, err := w.server.TasksRepo.Get(ctx, task)
	if err != nil || t.ActionID != files.ActionTransfer {
		return false, nil
	}
	b, err := w.repo.Execution(ctx, execution)
	if err != nil || b.TaskID != task || b.NodeID != o.Identity.NodeID || b.Observation != observationKey(o) {
		return true, files.ErrTransferIdentity
	}
	offer := new(agentv1.TaskOffer)
	if proto.Unmarshal(b.Offer, offer) != nil {
		return true, files.ErrTransferIdentity
	}
	if e.GetTaskAck() != nil {
		return true, w.Gate(ctx, o, offer, e)
	}
	p, err := decodeFileTask(t)
	if err != nil || w.live(ctx, t, p) != nil {
		return true, files.ErrTransferExecution
	}
	if f := e.GetTaskFinished(); f != nil {
		return true, w.repo.ObserveExecution(ctx, execution, "terminal", w.server.Audit, func(b *files.ExecutionBinding) error {
			result := f.Result.String()
			if b.Result != "" && b.Result != result {
				return files.ErrTransferIdentity
			}
			if f.Result == agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED && !b.Complete {
				return files.ErrAgentUnknown
			}
			b.State = "done"
			b.Result = result
			return nil
		})
	}
	return true, nil
}
func (w *FileExecutionWorker) waitDone(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		b, err := w.repo.Execution(ctx, id)
		if err != nil {
			return err
		}
		if b.Result != "" {
			if b.Result == agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED.String() {
				return nil
			}
			return files.ErrAgentUnknown
		}
		select {
		case <-ctx.Done():
			return files.ErrAgentUnknown
		case <-ticker.C:
		}
	}
}
func (w *FileExecutionWorker) open(ctx context.Context, t tasks.Task, p fileTaskPlan, i int, write bool, size uint64, digest string) (*transport.FileSession, *agentv1.FileOpenReply, error) {
	item := p.Items[i]
	node, relative, direction := p.SourceNode, item.ReadPath, "read"
	if write {
		node, relative, direction = p.DestinationNode, item.WritePath, "write"
	}
	execution := childExecutionID(t, node, i)
	parameters := transport.FileParameters{TransferID: execution, RelativePath: relative, Direction: direction, Size: size, SHA256: digest, ConflictPolicy: "skip"}
	raw, _ := json.Marshal(parameters)
	offer := &agentv1.TaskOffer{TaskId: t.ID, ExecutionId: execution, PlanDigestSha256: t.PlanHash, ActionId: transport.FileCapability, CanonicalParametersJson: raw, Privilege: agentv1.Privilege_PRIVILEGE_NORMAL, ExpiresAtUnixMs: time.Now().Add(5 * time.Minute).UnixMilli(), TimeoutSeconds: 300, ScopeRevision: t.ScopeRevision, ActionSchemaVersion: "1"}
	observation, err := w.server.Links.FileConnection(ctx, node)
	if err != nil {
		return nil, nil, err
	}
	offerRaw, _ := proto.Marshal(offer)
	if err = w.repo.ReserveExecution(ctx, files.ExecutionBinding{ExecutionID: execution, TaskID: t.ID, TransferID: p.TransferID, SubjectID: t.SubmitterID, NodeID: node, Direction: direction, ParentPlan: t.PlanHash, Observation: observationKey(observation), Sequence: i, Offer: offerRaw, State: "reserved"}, w.server.Audit); err != nil {
		return nil, nil, err
	}
	session, err := w.server.AgentFiles.NewSession(ctx, node, offer)
	if err != nil {
		return nil, nil, err
	}
	reply, err := session.Open(ctx)
	if err != nil {
		session.Close()
		w.cancelChild(t.ID, node, execution)
		return nil, nil, err
	}
	return session, reply, nil
}
func (w *FileExecutionWorker) cancelChild(task, node, execution string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = w.server.Links.Send(ctx, node, &agentv1.Envelope{Message: &agentv1.Envelope_TaskCancel{TaskCancel: &agentv1.TaskCancel{TaskId: task, ExecutionId: execution, ReasonCode: "file_worker_stopped", RequestedAtUnixMs: time.Now().UnixMilli()}}})
}
func (w *FileExecutionWorker) copyItem(ctx context.Context, t tasks.Task, p fileTaskPlan, i int) (copied int64, skipped bool, err error) {
	spool, err := files.NewExecutionSpool(w.config.SpoolDir)
	if err != nil {
		return 0, false, err
	}
	defer func() { name := spool.Name(); spool.Close(); os.Remove(name) }()
	source, meta, err := w.open(ctx, t, p, i, false, 0, "")
	if err != nil {
		return 0, false, err
	}
	defer source.Close()
	defer func() {
		if err != nil {
			w.cancelChild(t.ID, p.SourceNode, childExecutionID(t, p.SourceNode, i))
		}
	}()
	if expected := p.Items[i].ExpectedSHA256; expected != "" && meta.Sha256 != expected {
		return 0, false, files.ErrAgentChecksum
	}
	h := sha256.New()
	for offset := uint64(0); offset < meta.Size; {
		n := min(uint64(transport.FileChunkBytes), meta.Size-offset)
		r, e := source.Chunk(ctx, &agentv1.FileChunkRequest{TaskId: t.ID, ExecutionId: childExecutionID(t, p.SourceNode, i), TransferId: childExecutionID(t, p.SourceNode, i), Offset: offset, ReadBytes: uint32(n)})
		if e != nil {
			return 0, false, e
		}
		if _, e = spool.Write(r.Data); e != nil {
			return 0, false, e
		}
		h.Write(r.Data)
		offset = r.NextOffset
	}
	if hex.EncodeToString(h.Sum(nil)) != meta.Sha256 || spool.Sync() != nil {
		return 0, false, files.ErrAgentChecksum
	}
	if err = w.waitDone(ctx, childExecutionID(t, p.SourceNode, i)); err != nil {
		return 0, false, err
	}
	if err = w.live(ctx, t, p); err != nil {
		return 0, false, err
	}
	if _, err = spool.Seek(0, io.SeekStart); err != nil {
		return 0, false, err
	}
	destination, opened, err := w.open(ctx, t, p, i, true, meta.Size, meta.Sha256)
	if err != nil {
		return 0, false, err
	}
	defer destination.Close()
	defer func() {
		if err != nil {
			w.cancelChild(t.ID, p.DestinationNode, childExecutionID(t, p.DestinationNode, i))
		}
	}()
	if !opened.Skipped {
		buffer := make([]byte, transport.FileChunkBytes)
		defer clear(buffer)
		for offset := uint64(0); offset < meta.Size || meta.Size == 0; {
			n := min(uint64(len(buffer)), meta.Size-offset)
			if _, err = io.ReadFull(spool, buffer[:n]); err != nil {
				return 0, false, err
			}
			r, e := destination.Chunk(ctx, &agentv1.FileChunkRequest{TaskId: t.ID, ExecutionId: childExecutionID(t, p.DestinationNode, i), TransferId: childExecutionID(t, p.DestinationNode, i), Offset: offset, Data: buffer[:n], Finish: offset+n == meta.Size})
			if e != nil {
				return 0, false, e
			}
			offset = r.NextOffset
			if r.Complete {
				break
			}
		}
	}
	if err = w.waitDone(ctx, childExecutionID(t, p.DestinationNode, i)); err != nil {
		return 0, false, err
	}
	if err = w.live(ctx, t, p); err != nil {
		return 0, false, err
	}
	return int64(meta.Size), opened.Skipped, nil
}
func (w *FileExecutionWorker) Execute(ctx context.Context, t tasks.Task) (map[string]tasks.WorkerResult, error) {
	p, err := decodeFileTask(t)
	if err != nil {
		return nil, err
	}
	started := time.Now().UTC()
	// AttachTask follows submission in a separate durable transaction. No child
	// effects before that exact linkage exists; a missing linkage never falls back.
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		tr, e := w.repo.GetTransfer(ctx, p.TransferID)
		if e != nil {
			return nil, e
		}
		if tr.TaskID == t.ID {
			break
		}
		if tr.TaskID != "" {
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
	if err = w.live(ctx, t, p); err != nil {
		return nil, err
	}
	if _, won, e := w.repo.ClaimTransfer(ctx, t.ID, started); e != nil || !won {
		return nil, files.ErrExecutionInProgress
	}
	updates := make([]files.ItemUpdate, 0, len(p.Items))
	var total int64
	code := tasks.NodeResultSucceeded
	status := files.TransferCompleted
	for i, item := range p.Items {
		n, skip, e := w.copyItem(ctx, t, p, i)
		state := files.ItemCopied
		if skip {
			state = files.ItemSkipped
			n = 0
		}
		errorCode, message := "", ""
		if e != nil {
			state = files.ItemFailed
			code = tasks.NodeResultUnknown
			status = files.TransferFailed
			errorCode = "agent_unknown"
			message = "file execution uncertain; reconcile without replay"
			// No destination reservation means this item never issued a write.
			// Only positive known failures/cancellation use known terminal results.
			var bindings int
			checkCtx, done := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			checkErr := w.server.DB.QueryRow(checkCtx, `SELECT count(*) FROM cx_file_execution_bindings WHERE execution_id=?`, childExecutionID(t, p.DestinationNode, i)).Scan(&bindings)
			current, taskErr := w.server.TasksRepo.Get(checkCtx, t.ID)
			if checkErr == nil && bindings == 0 {
				if taskErr == nil && current.CancelRequested {
					state, code, status, errorCode, message = files.ItemCancelled, tasks.NodeResultCanceled, files.TransferCanceled, "cancelled_before_write", "cancelled before destination write"
				} else if e == files.ErrAgentChecksum || w.authorizer.Authorize(checkCtx, auth.Check{SubjectID: t.SubmitterID, Action: files.ActionRead, NodeIDs: []string{p.SourceNode}}) != nil || w.authorizer.Authorize(checkCtx, auth.Check{SubjectID: t.SubmitterID, Action: files.ActionWrite, NodeIDs: []string{p.DestinationNode}}) != nil {
					code, errorCode, message = tasks.NodeResultFailed, "denied_before_write", "checksum or current permission rejected before destination write"
				}
			}
			done()
		}
		updates = append(updates, files.ItemUpdate{SourcePath: item.Source, DestinationPath: item.Destination, Status: state, BytesCopied: n, ErrorCode: errorCode, ErrorMessage: message})
		total += n
		if e != nil {
			for _, rest := range p.Items[i+1:] {
				updates = append(updates, files.ItemUpdate{SourcePath: rest.Source, DestinationPath: rest.Destination, Status: files.ItemCancelled, ErrorCode: "not_started", ErrorMessage: "not attempted after uncertain execution"})
			}
			break
		}
		progressCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		_, e = w.server.Files.ApplyTaskUpdate(progressCtx, t.ID, files.TaskUpdate{Status: files.TransferRunning, Items: updates, CopiedBytes: &total, StartedAt: started})
		stop()
		if e != nil {
			return nil, e
		}
	}
	finishCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	_, err = w.server.Files.ApplyTaskUpdate(finishCtx, t.ID, files.TaskUpdate{Status: status, Progress: 100, Items: updates, TotalBytes: &total, CopiedBytes: &total, StartedAt: started, CompletedAt: time.Now().UTC()})
	if err != nil {
		return nil, err
	}
	result := tasks.WorkerResult{Code: code, Message: fmt.Sprintf("file transfer %s; inspect item results", status)}
	return map[string]tasks.WorkerResult{p.SourceNode: result, p.DestinationNode: result}, nil
}
