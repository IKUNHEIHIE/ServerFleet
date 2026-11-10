package main

import (
	"context"
	"encoding/json"
	"sync"

	"google.golang.org/protobuf/proto"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/transport"
)

type agentFileTask struct {
	offer            *agentv1.TaskOffer
	parameters       transport.FileParameters
	ctx              context.Context
	cancel           context.CancelFunc
	done             chan agentv1.ExecutionResult
	binding          agentFileBinding
	opened, finished bool
	offset           uint64
}
type agentFileSessions struct {
	mu      sync.Mutex
	tasks   map[string]*agentFileTask
	io      *agentFileCapability
	node    string
	current func(context.Context, *agentv1.TaskOffer) error
}

func newAgentFileSessions(root, node string, current func(context.Context, *agentv1.TaskOffer) error) (*agentFileSessions, error) {
	if !transport.FileID(node) || current == nil {
		return nil, errFileCapability
	}
	f := &agentFileSessions{tasks: map[string]*agentFileTask{}, node: node, current: current}
	fileIO, err := openAgentFileCapability(root, func(ctx context.Context, b agentFileBinding, _ string) error {
		// Handle holds f.mu while performing disk I/O. The captured task is
		// retrieved from a context private to this registered session adapter.
		t, ok := ctx.Value(agentFileTaskKey{}).(*agentFileTask)
		if !ok || t.ctx.Err() != nil || t.finished || t.offer.TaskId != b.TaskID || t.offer.ExecutionId != b.ExecutionID || t.offer.PlanDigestSha256 != b.PlanSHA256 || t.parameters.TransferID != b.TransferID || b.NodeID != node {
			return errFileCapability
		}
		return current(ctx, t.offer)
	})
	if err != nil {
		return nil, err
	}
	f.io = fileIO
	return f, nil
}

type agentFileTaskKey struct{}

func (f *agentFileSessions) Close() {
	f.mu.Lock()
	for _, t := range f.tasks {
		t.cancel()
	}
	f.mu.Unlock()
	f.io.Close()
}
func parseAnyFileParameters(raw []byte) (transport.FileParameters, error) {
	p, err := transport.ParseFileParameters(raw)
	if err != nil {
		return transport.ParseResumedFileParameters(raw)
	}
	return p, nil
}

func (f *agentFileSessions) Action() agentAction {
	return agentAction{Validate: func(raw []byte) error { _, err := parseAnyFileParameters(raw); return err }, Run: func(ctx context.Context, raw []byte) executionResult {
		p, err := parseAnyFileParameters(raw)
		if err != nil {
			return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN}
		}
		f.mu.Lock()
		t := f.tasks[p.TransferID]
		f.mu.Unlock()
		if t == nil || t.parameters != p {
			return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN}
		}
		defer func() {
			t.cancel()
			f.mu.Lock()
			if f.tasks[p.TransferID] == t {
				delete(f.tasks, p.TransferID)
			}
			f.mu.Unlock()
			f.io.mu.Lock()
			delete(f.io.writeHashes, p.TransferID)
			if file := f.io.snapshots[p.TransferID]; file != nil {
				file.Close()
				delete(f.io.snapshots, p.TransferID)
			}
			f.io.mu.Unlock()
		}()
		select {
		case result := <-t.done:
			return executionResult{Result: result}
		case <-ctx.Done():
			return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN}
		}
	}}
}

// Accept is called ONLY after the executor durably reserves the TaskOffer and
// before its ack is written. PreviouslySeen reservations never create sessions.
func (f *agentFileSessions) Accept(ctx context.Context, o *agentv1.TaskOffer, seen bool) error {
	p, err := transport.FileOffer(o)
	if err != nil || seen || f.current(ctx, o) != nil {
		return errFileCapability
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.tasks) >= 5 || f.tasks[p.TransferID] != nil {
		return errFileCapability
	}
	bounded, cancel := context.WithCancel(ctx)
	f.tasks[p.TransferID] = &agentFileTask{offer: proto.Clone(o).(*agentv1.TaskOffer), parameters: p, ctx: bounded, cancel: cancel, done: make(chan agentv1.ExecutionResult, 1)}
	return nil
}
func (f *agentFileSessions) Finish(transfer string, result agentv1.ExecutionResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t := f.tasks[transfer]; t != nil && !t.finished {
		t.finished = true
		t.done <- result
	}
}
func (f *agentFileSessions) Handle(ctx context.Context, e *agentv1.Envelope) (*agentv1.Envelope, func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	task, execution, transfer := transport.FileFrameIDs(e)
	t := f.tasks[transfer]
	if t == nil || t.finished || t.ctx.Err() != nil || task != t.offer.TaskId || execution != t.offer.ExecutionId || f.current(ctx, t.offer) != nil {
		return nil, nil, errFileCapability
	}
	ioctx, cancel := context.WithCancel(context.WithValue(t.ctx, agentFileTaskKey{}, t))
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	complete := func(result agentv1.ExecutionResult) func() { return func() { f.Finish(transfer, result) } }
	if r := e.GetFileOpenRequest(); r != nil {
		p := t.parameters
		if t.opened || r.TaskId != t.offer.TaskId || r.ExecutionId != t.offer.ExecutionId || r.PlanDigestSha256 != t.offer.PlanDigestSha256 || r.TransferId != p.TransferID || r.RelativePath != p.RelativePath || r.Direction != p.Direction || r.Size != p.Size || r.Sha256 != p.SHA256 || r.ConflictPolicy != p.ConflictPolicy || r.CreateParents != p.CreateParents || r.ResumeOffset != transport.FileResumeOffset(p) {
			return nil, nil, errFileCapability
		}
		b := agentFileBinding{PlanSHA256: t.offer.PlanDigestSha256, TaskID: task, ExecutionID: execution, TransferID: transfer, NodeID: f.node, RelativePath: p.RelativePath, SHA256: p.SHA256, Direction: p.Direction, ConflictPolicy: p.ConflictPolicy, Size: int64(p.Size)}
		skipped := false
		if p.Direction == "read" {
			var err error
			b, err = f.io.Snapshot(ioctx, b)
			if err != nil {
				return nil, nil, err
			}
		} else {
			if p.ResumeFrom != "" {
				var err error
				b, err = f.io.adoptResume(ioctx, b, p.ResumeFrom)
				if err != nil {
					return nil, nil, err
				}
			}
			// The trusted plan decides skip/overwrite/rename. Only a non-symlink
			// regular destination is ever replaced; rename reports the actual
			// chosen sibling path back to the Controller.
			var err error
			if p.ResumeFrom == "" {
				b, skipped, err = f.io.resolveWriteDestination(ioctx, b)
			} else if b.ConflictPolicy == "skip" {
				kind, e := agentFileDestination(f.io.root, b.RelativePath)
				err = e
				skipped = kind == agentDestinationRegular
			}
			if err != nil {
				return nil, nil, err
			}
		}
		t.opened = true
		t.binding = b
		t.offset = transport.FileResumeOffset(p)
		reply := &agentv1.FileOpenReply{TaskId: task, ExecutionId: execution, TransferId: transfer, Size: uint64(b.Size), Sha256: b.SHA256, RelativePath: b.RelativePath, Skipped: skipped, Complete: skipped || b.Size == 0 && p.Direction == "read", ResumeOffset: t.offset, ResultCode: "ok", PlanDigestSha256: t.offer.PlanDigestSha256}
		var after func()
		if reply.Complete {
			after = complete(agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED)
		}
		return &agentv1.Envelope{Message: &agentv1.Envelope_FileOpenReply{FileOpenReply: reply}}, after, nil
	}
	r := e.GetFileChunkRequest()
	if r == nil || !t.opened || r.Offset != t.offset {
		return nil, nil, errFileCapability
	}
	reply := &agentv1.FileChunkReply{TaskId: task, ExecutionId: execution, TransferId: transfer, Offset: r.Offset, NextOffset: r.Offset, ResultCode: "ok"}
	if r.Cancel {
		if len(r.Data) > 0 || r.ReadBytes > 0 || r.Finish {
			return nil, nil, errFileCapability
		}
		reply.ResultCode = "canceled"
		return &agentv1.Envelope{Message: &agentv1.Envelope_FileChunkReply{FileChunkReply: reply}}, complete(agentv1.ExecutionResult_EXECUTION_RESULT_CANCELED), nil
	}
	if t.binding.Direction == "read" {
		if r.ReadBytes == 0 || r.ReadBytes > transport.FileChunkBytes || len(r.Data) > 0 || r.Finish || r.Offset+uint64(r.ReadBytes) > uint64(t.binding.Size) {
			return nil, nil, errFileCapability
		}
		data, err := f.io.Read(ioctx, t.binding, int64(r.Offset), int(r.ReadBytes))
		if err != nil {
			return nil, nil, err
		}
		reply.Data = data
		reply.NextOffset += uint64(len(data))
		reply.Complete = reply.NextOffset == uint64(t.binding.Size)
	} else {
		if r.ReadBytes > 0 || len(r.Data) > transport.FileChunkBytes || r.Offset+uint64(len(r.Data)) > uint64(t.binding.Size) {
			return nil, nil, errFileCapability
		}
		if err := f.io.Write(ioctx, t.binding, int64(r.Offset), r.Data, r.Finish); err != nil {
			return nil, nil, err
		}
		reply.NextOffset += uint64(len(r.Data))
		reply.Complete = r.Finish
	}
	if f.current(ioctx, t.offer) != nil {
		return nil, nil, errFileCapability
	}
	t.offset = reply.NextOffset
	var after func()
	if reply.Complete {
		after = complete(agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED)
	}
	return &agentv1.Envelope{Message: &agentv1.Envelope_FileChunkReply{FileChunkReply: reply}}, after, nil
}

// canonicalFileParameters is used only by the CLI/session integration tests.
func canonicalFileParameters(p transport.FileParameters) []byte {
	raw, _ := json.Marshal(p)
	return raw
}
