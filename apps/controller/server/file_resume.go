package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
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

func (w *FileExecutionWorker) cacheSource(ctx context.Context, t tasks.Task, p fileTaskPlan, i int, source *os.File, size uint64, digest string) error {
	item := p.Items[i]
	meta := files.ExecutionCacheManifest{Version: 1, ExecutionID: childExecutionID(t, p.DestinationNode, i), TaskID: t.ID, PlanSHA256: t.PlanHash, SubjectID: t.SubmitterID, NodeID: p.DestinationNode, SourceNodeID: p.SourceNode, RelativePath: item.WritePath, Destination: item.Destination, ConflictPolicy: string(p.ConflictPolicy), SHA256: digest, Size: size}
	return files.CreateExecutionCache(ctx, w.config.SpoolDir, meta, source)
}
func (w *FileExecutionWorker) executionPlan(ctx context.Context, t tasks.Task) (fileTaskPlan, error) {
	if t.ActionID != files.ActionResume {
		return decodeFileTask(t)
	}
	var values map[string]string
	if t.Source != "web" || t.OperationClass != tasks.OperationClassModifying || !t.ApprovalRequired || t.ScopeRevision != 0 || t.PluginID != "" || json.Unmarshal([]byte(t.Params), &values) != nil || len(values) != 5 {
		return fileTaskPlan{}, files.ErrTransferIdentity
	}
	meta, err := files.ReadExecutionCacheManifest(w.config.SpoolDir, values["previousExecutionId"])
	if err != nil {
		return fileTaskPlan{}, err
	}
	if files.ExecutionCacheDigest(meta) != values["cacheSha256"] || meta.SubjectID != t.SubmitterID {
		return fileTaskPlan{}, files.ErrTransferIdentity
	}
	offset, err := strconv.ParseUint(values["offset"], 10, 64)
	if err != nil || offset == 0 || offset >= meta.Size || !transport.FileDigest(values["prefixSha256"]) || !transport.FileID(values["transfer_id"]) {
		return fileTaskPlan{}, files.ErrTransferIdentity
	}
	old, err := w.repo.Execution(ctx, meta.ExecutionID)
	if err != nil || old.TaskID != meta.TaskID || old.ParentPlan != meta.PlanSHA256 || old.NodeID != meta.NodeID || old.Offset != offset || old.Complete {
		return fileTaskPlan{}, files.ErrTransferIdentity
	}
	previous, err := w.server.TasksRepo.Get(ctx, old.TaskID)
	if err != nil || !previous.Status.Terminal() || previous.PlanHash != meta.PlanSHA256 {
		return fileTaskPlan{}, files.ErrTransferIdentity
	}
	offer := new(agentv1.TaskOffer)
	if proto.Unmarshal(old.Offer, offer) != nil {
		return fileTaskPlan{}, files.ErrTransferIdentity
	}
	parameters, err := transport.FileOffer(offer)
	if err != nil || parameters.Direction != "write" || parameters.SHA256 != meta.SHA256 || parameters.Size != meta.Size || parameters.RelativePath != meta.RelativePath || parameters.ConflictPolicy != meta.ConflictPolicy || offer.TaskId != old.TaskID || offer.ExecutionId != old.ExecutionID || offer.PlanDigestSha256 != old.ParentPlan {
		return fileTaskPlan{}, files.ErrTransferIdentity
	}
	resume, _ := json.Marshal(transport.FileResumeSource{TaskID: old.TaskID, ExecutionID: old.ExecutionID, TransferID: parameters.TransferID, PlanSHA256: old.ParentPlan, ConfirmedOffset: offset, PrefixSHA256: values["prefixSha256"]})
	return fileTaskPlan{Kind: "resume", ResumeFrom: string(resume), TransferID: values["transfer_id"], SourceNode: meta.SourceNodeID, DestinationNode: meta.NodeID, ConflictPolicy: files.ConflictPolicy(meta.ConflictPolicy), UploadSize: int64(meta.Size), Items: []filePlanItem{{Destination: meta.Destination, WritePath: meta.RelativePath, ExpectedSHA256: meta.SHA256}}}, nil
}
func (w *FileExecutionWorker) liveResume(ctx context.Context, t tasks.Task, p fileTaskPlan) error {
	current, err := w.server.Tasks.CheckExecution(ctx, t.ID, t.PlanHash)
	if err != nil || current.Params != t.Params || current.Source != "web" || !current.ApprovalRequired || current.OperationClass != tasks.OperationClassModifying {
		return files.ErrTransferExecution
	}
	node, err := w.server.NodesRepo.Get(ctx, p.DestinationNode)
	if err != nil || node.Status != nodes.StatusOnline {
		return files.ErrAgentUnauthorized
	}
	if err = w.authorizer.Authorize(ctx, auth.Check{SubjectID: t.SubmitterID, Action: files.ActionWrite, NodeIDs: []string{p.DestinationNode}}); err != nil {
		return err
	}
	if p.SourceNode != "" {
		if err = w.authorizer.Authorize(ctx, auth.Check{SubjectID: t.SubmitterID, Action: files.ActionRead, NodeIDs: []string{p.SourceNode}}); err != nil {
			return err
		}
	}
	path, err := w.config.Paths.RelativePath(ctx, p.DestinationNode, p.Items[0].Destination)
	if err != nil || path != p.Items[0].WritePath {
		return files.ErrPathMappingUnavailable
	}
	return nil
}
func (w *FileExecutionWorker) executeResume(ctx context.Context, t tasks.Task) (map[string]tasks.WorkerResult, error) {
	p, err := w.executionPlan(ctx, t)
	if err != nil {
		return nil, err
	}
	result := func(code tasks.NodeResultCode, message string) (map[string]tasks.WorkerResult, error) {
		return map[string]tasks.WorkerResult{p.DestinationNode: {Code: code, Message: message}}, nil
	}
	if w.liveResume(ctx, t, p) != nil {
		return result(tasks.NodeResultFailed, "file resume current permission rejected")
	}
	var values map[string]string
	_ = json.Unmarshal([]byte(t.Params), &values)
	meta, source, err := files.OpenExecutionCache(ctx, w.config.SpoolDir, values["previousExecutionId"])
	if err != nil {
		return result(tasks.NodeResultUnknown, "file resume source unavailable")
	}
	defer source.Close()
	resume, _ := transport.ParseFileResumeSource(p.ResumeFrom, meta.Size)
	prefix := sha256.New()
	if n, e := io.Copy(prefix, io.NewSectionReader(source, 0, int64(resume.ConfirmedOffset))); e != nil || uint64(n) != resume.ConfirmedOffset || hex.EncodeToString(prefix.Sum(nil)) != resume.PrefixSHA256 {
		return result(tasks.NodeResultFailed, "confirmed prefix mismatch")
	}
	if err = w.cacheSource(ctx, t, p, 0, source, meta.Size, meta.SHA256); err != nil {
		return result(tasks.NodeResultUnknown, "new resume source reservation unavailable")
	}
	execution := childExecutionID(t, p.DestinationNode, 0)
	parameters := transport.FileParameters{TransferID: execution, RelativePath: meta.RelativePath, Direction: "write", Size: meta.Size, SHA256: meta.SHA256, ConflictPolicy: meta.ConflictPolicy, ResumeFrom: p.ResumeFrom}
	raw, _ := json.Marshal(parameters)
	offer := &agentv1.TaskOffer{TaskId: t.ID, ExecutionId: execution, PlanDigestSha256: t.PlanHash, ActionId: transport.FileResumeCapability, CanonicalParametersJson: raw, Privilege: agentv1.Privilege_PRIVILEGE_NORMAL, ExpiresAtUnixMs: time.Now().Add(5 * time.Minute).UnixMilli(), TimeoutSeconds: 300, ActionSchemaVersion: "1"}
	observation, err := w.server.Links.FileConnection(ctx, p.DestinationNode)
	if err != nil {
		return result(tasks.NodeResultUnknown, "resume agent unavailable")
	}
	offerRaw, _ := proto.Marshal(offer)
	if err = w.repo.ReserveExecution(ctx, files.ExecutionBinding{ExecutionID: execution, TaskID: t.ID, TransferID: p.TransferID, SubjectID: t.SubmitterID, NodeID: p.DestinationNode, Direction: "write", ParentPlan: t.PlanHash, Observation: observationKey(observation), Sequence: 0, Offer: offerRaw, State: "reserved"}, w.server.Audit); err != nil {
		return result(tasks.NodeResultUnknown, "resume execution reservation unknown")
	}
	session, err := w.server.AgentFiles.NewSession(ctx, p.DestinationNode, offer)
	if err != nil {
		return result(tasks.NodeResultUnknown, "resume session unavailable")
	}
	defer session.Close()
	opened, err := session.Open(ctx)
	if err != nil {
		return result(tasks.NodeResultUnknown, "agent prefix/publish state did not permit resume")
	}
	if !opened.Skipped {
		buffer := make([]byte, transport.FileChunkBytes)
		defer clear(buffer)
		for offset := resume.ConfirmedOffset; offset < meta.Size; {
			n := min(uint64(len(buffer)), meta.Size-offset)
			if w.liveResume(ctx, t, p) != nil {
				return result(tasks.NodeResultUnknown, "resume authority changed")
			}
			if read, e := source.ReadAt(buffer[:n], int64(offset)); e != nil || uint64(read) != n {
				return result(tasks.NodeResultUnknown, "resume source changed")
			}
			reply, e := session.Chunk(ctx, &agentv1.FileChunkRequest{TaskId: t.ID, ExecutionId: execution, TransferId: execution, Offset: offset, Data: buffer[:n], Finish: offset+n == meta.Size})
			if e != nil {
				return result(tasks.NodeResultUnknown, "resume destination outcome unobserved")
			}
			offset = reply.NextOffset
		}
	}
	if err = w.waitDone(ctx, execution); err != nil {
		return result(tasks.NodeResultUnknown, "resume terminal receipt not observed")
	}
	_ = files.ReleaseExecutionCache(w.config.SpoolDir, meta.ExecutionID, meta.TaskID, meta.PlanSHA256)
	w.releaseSourceCache(ctx, t, execution)
	if opened.Skipped {
		return result(tasks.NodeResultSucceeded, "resume skipped an existing destination under the approved skip policy")
	}
	return result(tasks.NodeResultSucceeded, "new approved resume completed; original attempt remains historical and is not replayed")
}
func (s *Server) registerFileResumeRoutes() {
	s.mux.HandleFunc("GET /api/v1/files/resume-candidates/{taskId}", func(w http.ResponseWriter, r *http.Request) {
		identity, _, err := s.authenticateIdentity(r)
		if r.TLS == nil || err != nil || s.FileWorker == nil || r.URL.RawQuery != "" || r.ContentLength != 0 {
			releaseSourceJSON(w, 403, map[string]string{"code": "RESUME_NOT_AUTHORIZED"})
			return
		}
		task, err := s.TasksRepo.Get(r.Context(), r.PathValue("taskId"))
		if err != nil || task.SubmitterID != identity.SubjectID || !task.Status.Terminal() || (&composedTaskReadGate{identity: s.Identity, tasks: s.TasksRepo}).AuthorizeTaskRead(r.Context(), identity.SubjectID, task.ID) != nil {
			releaseSourceJSON(w, 403, map[string]string{"code": "RESUME_OWNER_OR_STATE_DENIED"})
			return
		}
		bindings, err := s.FileWorker.repo.ExecutionsForTask(r.Context(), task.ID)
		if err != nil {
			releaseSourceJSON(w, 409, map[string]string{"code": "RESUME_METADATA_UNAVAILABLE"})
			return
		}
		items := []map[string]any{}
		for _, binding := range bindings {
			if binding.Direction != "write" || binding.Complete || binding.Offset == 0 || binding.Offset >= binding.Size || binding.ParentPlan != task.PlanHash {
				continue
			}
			meta, e := files.ReadExecutionCacheManifest(s.FileWorker.config.SpoolDir, binding.ExecutionID)
			if e != nil || meta.TaskID != task.ID || meta.PlanSHA256 != task.PlanHash || meta.SubjectID != identity.SubjectID || meta.NodeID != binding.NodeID {
				continue
			}
			items = append(items, map[string]any{"executionId": binding.ExecutionID, "nodeId": binding.NodeID, "destination": meta.Destination, "confirmedOffset": binding.Offset, "size": binding.Size, "sha256": binding.SHA256, "requiresAgentPrefixProof": true})
		}
		releaseSourceJSON(w, 200, map[string]any{"taskId": task.ID, "items": items})
	})

	s.mux.HandleFunc("POST /api/v1/files/resume-tasks", func(w http.ResponseWriter, r *http.Request) {
		identity, request, err := s.authenticateIdentity(r)
		if r.TLS == nil || err != nil || s.FileWorker == nil {
			releaseSourceJSON(w, 403, map[string]string{"code": "RESUME_NOT_AUTHORIZED"})
			return
		}
		body, ok := publicCertificateBody(w, r, 4096)
		if !ok {
			return
		}
		if len(body) != 2 || !transport.FileID(body["executionId"]) || !transport.FileID(body["idempotencyKey"]) {
			releaseSourceJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		worker := s.FileWorker
		binding, err := worker.repo.Execution(r.Context(), body["executionId"])
		if err != nil || binding.Direction != "write" || binding.Complete || binding.Offset == 0 || binding.Offset >= binding.Size || (&composedTaskReadGate{identity: s.Identity, tasks: s.TasksRepo}).AuthorizeTaskRead(r.Context(), identity.SubjectID, binding.TaskID) != nil {
			releaseSourceJSON(w, 409, map[string]string{"code": "NO_CONFIRMED_PARTIAL"})
			return
		}
		old, err := s.TasksRepo.Get(r.Context(), binding.TaskID)
		if err != nil || !old.Status.Terminal() || old.SubmitterID != identity.SubjectID {
			releaseSourceJSON(w, 403, map[string]string{"code": "RESUME_OWNER_OR_STATE_DENIED"})
			return
		}
		meta, source, err := files.OpenExecutionCache(r.Context(), worker.config.SpoolDir, binding.ExecutionID)
		if err != nil {
			releaseSourceJSON(w, 409, map[string]string{"code": "RESUME_SOURCE_NOT_RETAINED"})
			return
		}
		defer source.Close()
		hash := sha256.New()
		if _, err = io.Copy(hash, io.NewSectionReader(source, 0, int64(binding.Offset))); err != nil {
			releaseSourceJSON(w, 409, map[string]string{"code": "RESUME_PREFIX_UNAVAILABLE"})
			return
		}
		params := map[string]string{"previousExecutionId": binding.ExecutionID, "cacheSha256": files.ExecutionCacheDigest(meta), "offset": strconv.FormatUint(binding.Offset, 10), "prefixSha256": hex.EncodeToString(hash.Sum(nil)), "transfer_id": "resume-" + backupTaskKey("file-resume", identity.SubjectID+body["idempotencyKey"])[:40]}
		task, err := s.Tasks.Submit(r.Context(), tasks.SubmitRequest{SubjectID: identity.SubjectID, RequestID: request, Name: "Resume verified unpublished file prefix", ActionID: files.ActionResume, OperationClass: tasks.OperationClassModifying, RequireApproval: true, Source: audit.SourceWeb, TargetNodeIDs: []string{meta.NodeID}, Params: params, IdempotencyKey: "file-resume-" + backupTaskKey("resume", identity.SubjectID+body["idempotencyKey"])})
		if err != nil {
			releaseSourceJSON(w, 409, map[string]string{"code": "RESUME_TASK_REJECTED"})
			return
		}
		releaseSourceJSON(w, 202, map[string]any{"taskId": task.ID, "status": task.Status, "planHash": task.PlanHash, "approvalRequired": task.ApprovalRequired})
	})
}

func (w *FileExecutionWorker) releaseSourceCache(ctx context.Context, t tasks.Task, id string) {
	b, err := w.repo.Execution(ctx, id)
	if err == nil && b.TaskID == t.ID && b.ParentPlan == t.PlanHash && b.Complete && b.Result == agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED.String() {
		_ = files.ReleaseExecutionCache(w.config.SpoolDir, id, t.ID, t.PlanHash)
	}
}
