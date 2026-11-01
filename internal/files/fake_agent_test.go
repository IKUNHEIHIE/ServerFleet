package files_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"serverfleet.local/serverfleet/internal/files"
)

// fakeAgent is an in-process CX file-capability double. It exercises the exact
// open/chunk boundary with generated-frame field semantics, including resume,
// conflicts and fault injection.
type fakeAgent struct {
	mu              sync.Mutex
	files           map[string]map[string][]byte
	partial         map[string]map[string][]byte
	rejects         map[string]string
	failUpload      map[string]int
	failDownload    map[string]int
	refuseDials     int
	dials           int
	uploads         int
	downloads       int
	corruptDigest   bool
	unknownCommit   bool
	rejectOverwrite bool
	lastTaskID      string
	lastTransferID  string
	lastExecID      string
	lastPlanDigest  string
	openCalls       []fakeOpenCall
}

type fakeOpenCall struct {
	execution string
	transfer  string
	direction files.ChannelDirection
}

func newFakeAgent() *fakeAgent {
	return &fakeAgent{
		files: map[string]map[string][]byte{}, partial: map[string]map[string][]byte{},
		rejects: map[string]string{}, failUpload: map[string]int{}, failDownload: map[string]int{},
	}
}

func fakeAbsolute(relative string) string { return "/" + relative }

func (a *fakeAgent) putFile(nodeID, path string, data []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.files[nodeID] == nil {
		a.files[nodeID] = map[string][]byte{}
	}
	a.files[nodeID][path] = append([]byte(nil), data...)
}

func (a *fakeAgent) file(nodeID, path string) ([]byte, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	data, ok := a.files[nodeID][path]
	return append([]byte(nil), data...), ok
}

func (a *fakeAgent) reject(nodeID, path, code string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rejects[nodeID+"|"+path] = code
}

func (a *fakeAgent) OpenFileChannel(ctx context.Context, binding files.FileBinding) (files.AgentFileSession, files.FileOpenResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, files.FileOpenResult{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dials++
	a.lastTaskID = binding.TaskID
	a.lastTransferID = binding.TransferID
	a.lastExecID = binding.ExecutionID
	a.lastPlanDigest = binding.PlanDigestSHA256
	a.openCalls = append(a.openCalls, fakeOpenCall{execution: binding.ExecutionID, transfer: binding.TransferID, direction: binding.Direction})
	if a.dials <= a.refuseDials {
		return nil, files.FileOpenResult{}, errors.New("connection refused")
	}
	result := files.FileOpenResult{
		TaskID: binding.TaskID, ExecutionID: binding.ExecutionID, TransferID: binding.TransferID,
		PlanDigestSHA256: binding.PlanDigestSHA256, RelativePath: binding.RelativePath, ResultCode: "ok",
	}
	if code, rejected := a.rejects[binding.NodeID+"|"+fakeAbsolute(binding.RelativePath)]; rejected {
		return nil, files.FileOpenResult{}, fakeCodeError(code)
	}
	if binding.Direction == files.ChannelWrite {
		final, exists := a.files[binding.NodeID][fakeAbsolute(binding.RelativePath)]
		if exists {
			switch binding.ConflictPolicy {
			case files.ConflictSkip:
				result.Skipped = true
				result.ResultCode = "skipped"
				return &fakeSession{agent: a, binding: binding}, result, nil
			case files.ConflictRename:
				result.RelativePath = binding.RelativePath + ".1"
			case files.ConflictOverwrite:
				if a.rejectOverwrite || final != nil && len(final) >= 0 && a.rejectOverwrite {
					return nil, files.FileOpenResult{}, files.ErrAgentConflict
				}
			default:
				return nil, files.FileOpenResult{}, files.ErrAgentConflict
			}
		}
		partial := a.partial[binding.NodeID][fakeAbsolute(result.RelativePath)]
		result.Size = binding.Size
		result.SHA256 = binding.SHA256
		result.ResumeOffset = int64(len(partial))
		return &fakeSession{agent: a, binding: binding, writePath: fakeAbsolute(result.RelativePath), uploadBuffer: append([]byte(nil), partial...)}, result, nil
	}
	final, exists := a.files[binding.NodeID][fakeAbsolute(binding.RelativePath)]
	if !exists {
		return nil, files.FileOpenResult{}, files.ErrAgentIO
	}
	digest := sha256.Sum256(final)
	result.Size = int64(len(final))
	result.SHA256 = hex.EncodeToString(digest[:])
	result.ResumeOffset = binding.ResumeOffset
	result.Complete = int64(len(final)) == binding.ResumeOffset
	if binding.ResumeOffset > int64(len(final)) {
		return nil, files.FileOpenResult{}, files.ErrChannelProtocol
	}
	a.downloads++
	return &fakeSession{agent: a, binding: binding, readData: append([]byte(nil), final...), readSent: int(binding.ResumeOffset)}, result, nil
}

func fakeCodeError(code string) error {
	switch code {
	case "path_rejected", "invalid_path":
		return files.ErrAgentPathRejected
	case "symlink_forbidden":
		return files.ErrAgentSymlink
	case "insufficient_space":
		return files.ErrInsufficientSpace
	case "checksum_mismatch":
		return files.ErrAgentChecksum
	case "conflict":
		return files.ErrAgentConflict
	case "unauthorized", "capability_denied":
		return files.ErrAgentUnauthorized
	case "canceled", "cancelled":
		return files.ErrChannelCanceled
	default:
		return files.ErrAgentIO
	}
}

type fakeSession struct {
	agent   *fakeAgent
	binding files.FileBinding
	dead    bool

	writePath    string
	uploadBuffer []byte

	readData []byte
	readSent int
}

func (s *fakeSession) Close() error { return nil }

func (s *fakeSession) Cancel(context.Context, string) error {
	s.dead = true
	return nil
}

func (s *fakeSession) result(offset, next uint64, complete bool, code string) files.FileChunkResult {
	return files.FileChunkResult{
		TaskID: s.binding.TaskID, ExecutionID: s.binding.ExecutionID, TransferID: s.binding.TransferID,
		Offset: offset, NextOffset: next, Complete: complete, ResultCode: code,
	}
}

func (s *fakeSession) ReadChunk(ctx context.Context, op files.FileChunkOp) (files.FileChunkResult, error) {
	if err := ctx.Err(); err != nil {
		return files.FileChunkResult{}, err
	}
	if s.dead {
		return files.FileChunkResult{}, files.ErrChannelDisconnected
	}
	if op.Cancel || len(op.Data) != 0 || op.Finish || op.ReadBytes == 0 {
		return files.FileChunkResult{}, files.ErrChannelProtocol
	}
	if op.Offset > uint64(len(s.readData)) {
		return files.FileChunkResult{}, files.ErrChannelProtocol
	}
	want := int(op.ReadBytes)
	if want > len(s.readData)-int(op.Offset) {
		want = len(s.readData) - int(op.Offset)
	}
	if want <= 0 {
		return files.FileChunkResult{}, files.ErrChannelProtocol
	}
	if configured := s.agent.failDownload[s.binding.NodeID]; configured > 0 {
		remainingBudget := configured - s.readSent
		if remainingBudget <= 0 {
			s.dead = true
			s.agent.failDownload[s.binding.NodeID] = 0
			return files.FileChunkResult{}, files.ErrChannelDisconnected
		}
		if want > remainingBudget {
			want = remainingBudget
			s.dead = true
			s.agent.failDownload[s.binding.NodeID] = 0
		}
	}
	s.readSent += want
	chunk := append([]byte(nil), s.readData[int(op.Offset):int(op.Offset)+want]...)
	next := op.Offset + uint64(len(chunk))
	complete := next == uint64(len(s.readData))
	result := s.result(op.Offset, next, complete, "ok")
	result.Data = chunk
	if s.dead {
		return result, nil
	}
	return result, nil
}

func (s *fakeSession) WriteChunk(ctx context.Context, op files.FileChunkOp) (files.FileChunkResult, error) {
	if err := ctx.Err(); err != nil {
		return files.FileChunkResult{}, err
	}
	if s.dead {
		return files.FileChunkResult{}, files.ErrChannelDisconnected
	}
	if op.Cancel {
		s.dead = true
		return s.result(op.Offset, op.Offset, false, "canceled"), files.ErrChannelCanceled
	}
	if op.ReadBytes != 0 {
		return files.FileChunkResult{}, files.ErrChannelProtocol
	}
	if op.Offset != uint64(len(s.uploadBuffer)) {
		return files.FileChunkResult{}, files.ErrChannelProtocol
	}
	next := append(s.uploadBuffer, op.Data...)
	if int64(len(next)) > s.binding.Size {
		return files.FileChunkResult{}, files.ErrAgentIO
	}
	if configured := s.agent.failUpload[s.binding.NodeID]; configured > 0 && len(next) >= configured && !op.Finish {
		s.uploadBuffer = next
		if s.agent.partial[s.binding.NodeID] == nil {
			s.agent.partial[s.binding.NodeID] = map[string][]byte{}
		}
		s.agent.partial[s.binding.NodeID][s.writePath] = append([]byte(nil), next...)
		s.agent.failUpload[s.binding.NodeID] = 0
		s.dead = true
		return files.FileChunkResult{}, files.ErrChannelDisconnected
	}
	s.uploadBuffer = next
	if !op.Finish {
		return s.result(op.Offset, op.Offset+uint64(len(op.Data)), false, "ok"), nil
	}
	if s.agent.unknownCommit {
		s.agent.unknownCommit = false
		return files.FileChunkResult{}, files.ErrChannelDisconnected
	}
	if s.agent.corruptDigest || int64(len(s.uploadBuffer)) != s.binding.Size {
		return s.result(op.Offset, op.Offset, false, "checksum_mismatch"), files.ErrAgentChecksum
	}
	digest := sha256.Sum256(s.uploadBuffer)
	if hex.EncodeToString(digest[:]) != s.binding.SHA256 {
		return s.result(op.Offset, op.Offset, false, "checksum_mismatch"), files.ErrAgentChecksum
	}
	if s.agent.files[s.binding.NodeID] == nil {
		s.agent.files[s.binding.NodeID] = map[string][]byte{}
	}
	s.agent.files[s.binding.NodeID][s.writePath] = append([]byte(nil), s.uploadBuffer...)
	delete(s.agent.partial[s.binding.NodeID], s.writePath)
	s.agent.uploads++
	return s.result(op.Offset, uint64(len(s.uploadBuffer)), true, "ok"), nil
}

func (a *fakeAgent) String() string { return fmt.Sprintf("fakeAgent(dials=%d)", a.dials) }
