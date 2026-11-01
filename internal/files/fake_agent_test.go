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

// fakeAgent is an in-process agent-side file capability used to exercise the
// controller transport at the exact frame boundary. It deliberately implements
// resume, conflict, fault injection and error codes so tests do not need a real
// remote node.
type fakeAgent struct {
	mu             sync.Mutex
	files          map[string]map[string][]byte
	partial        map[string]map[string][]byte
	rejects        map[string]string
	failUpload     map[string]int
	failDownload   map[string]int
	refuseDials    int
	dials          int
	uploads        int
	downloads      int
	corruptDigest  bool
	lastTransferID string
	lastExecID     string
}

func newFakeAgent() *fakeAgent {
	return &fakeAgent{
		files:        map[string]map[string][]byte{},
		partial:      map[string]map[string][]byte{},
		rejects:      map[string]string{},
		failUpload:   map[string]int{},
		failDownload: map[string]int{},
	}
}

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

func (a *fakeAgent) OpenFileChannel(_ context.Context, nodeID, transferID, executionID string) (files.FileChannelSession, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dials++
	a.lastTransferID = transferID
	a.lastExecID = executionID
	if a.dials <= a.refuseDials {
		return nil, errors.New("connection refused")
	}
	return &fakeSession{agent: a, nodeID: nodeID, transferID: transferID, executionID: executionID}, nil
}

type fakeSession struct {
	agent       *fakeAgent
	nodeID      string
	transferID  string
	executionID string

	responses []files.ChannelFrame
	dead      bool
	direction files.ChannelDirection

	uploadPath   string
	uploadSize   int64
	uploadSHA    string
	uploadPolicy files.ConflictPolicy
	uploadBuffer []byte

	downloadData   []byte
	downloadOffset int64
	downloadSize   int64
}

func (s *fakeSession) Close() error { return nil }

func (s *fakeSession) Send(ctx context.Context, frame files.ChannelFrame) error {
	if s.dead && len(s.responses) == 0 {
		return errors.New("channel closed")
	}
	if frame.TransferID != s.transferID || frame.ExecutionID != s.executionID {
		return errors.New("identity mismatch")
	}
	switch frame.Type {
	case files.FrameOpen:
		return s.open(frame)
	case files.FrameChunk:
		return s.chunk(frame)
	case files.FrameComplete:
		return s.complete(frame)
	case files.FrameAck:
		if s.dead {
			return errors.New("channel closed")
		}
		return nil
	case files.FrameCompleteAck:
		return nil
	case files.FrameCancel:
		s.dead = true
		return nil
	default:
		return fmt.Errorf("unexpected frame %s", frame.Type)
	}
}

func (s *fakeSession) Receive(ctx context.Context) (files.ChannelFrame, error) {
	if len(s.responses) > 0 {
		frame := s.responses[0]
		s.responses = s.responses[1:]
		return frame, nil
	}
	if s.dead {
		return files.ChannelFrame{}, errors.New("channel closed")
	}
	return files.ChannelFrame{}, errors.New("no response")
}

func (s *fakeSession) queueError(code string) {
	s.responses = append(s.responses, files.ChannelFrame{
		Version: files.ChannelProtocolVersion, Type: files.FrameError,
		TransferID: s.transferID, ExecutionID: s.executionID, ErrorCode: code,
	})
	s.dead = true
}

func (s *fakeSession) open(frame files.ChannelFrame) error {
	if code, rejected := s.agent.rejects[s.nodeID+"|"+frame.Path]; rejected {
		s.queueError(code)
		return nil
	}
	if frame.Direction == files.ChannelUpload {
		s.direction = files.ChannelUpload
		s.uploadPath = frame.Path
		s.uploadSize = frame.Size
		s.uploadSHA = frame.SHA256
		s.uploadPolicy = frame.ConflictPolicy
		if final, exists := s.agent.files[s.nodeID][frame.Path]; exists {
			switch frame.ConflictPolicy {
			case files.ConflictSkip:
				s.responses = append(s.responses, files.ChannelFrame{
					Version: files.ChannelProtocolVersion, Type: files.FrameOpenAck,
					TransferID: s.transferID, ExecutionID: s.executionID, Skipped: true,
				})
				return nil
			case files.ConflictRename:
				if final != nil {
					s.uploadPath = frame.Path + ".1"
				}
			}
		}
		partial := s.agent.partial[s.nodeID][s.uploadPath]
		s.uploadBuffer = append([]byte(nil), partial...)
		s.responses = append(s.responses, files.ChannelFrame{
			Version: files.ChannelProtocolVersion, Type: files.FrameOpenAck,
			TransferID: s.transferID, ExecutionID: s.executionID,
			ResumeOffset: int64(len(s.uploadBuffer)), ResolvedPath: s.uploadPath,
		})
		return nil
	}
	s.direction = files.ChannelDownload
	final, exists := s.agent.files[s.nodeID][frame.Path]
	if !exists {
		s.queueError("io_error")
		return nil
	}
	if frame.Offset > int64(len(final)) {
		s.queueError("protocol_error")
		return nil
	}
	digest := sha256.Sum256(final)
	s.downloadData = final
	s.downloadOffset = frame.Offset
	s.downloadSize = int64(len(final))
	s.agent.downloads++
	s.responses = append(s.responses, files.ChannelFrame{
		Version: files.ChannelProtocolVersion, Type: files.FrameOpenAck,
		TransferID: s.transferID, ExecutionID: s.executionID,
		Size: int64(len(final)), SHA256: hex.EncodeToString(digest[:]), ResumeOffset: frame.Offset,
	})
	remaining := final[frame.Offset:]
	limit := len(remaining)
	if configured := s.agent.failDownload[s.nodeID]; configured > 0 && limit > configured {
		limit = configured
		s.dead = true
		s.agent.failDownload[s.nodeID] = 0
	}
	const chunk = 1024
	for sent := 0; sent < limit; sent += chunk {
		end := sent + chunk
		if end > limit {
			end = limit
		}
		s.responses = append(s.responses, files.ChannelFrame{
			Version: files.ChannelProtocolVersion, Type: files.FrameChunk,
			TransferID: s.transferID, ExecutionID: s.executionID,
			Offset: int64(sent) + frame.Offset, Data: remaining[sent:end],
		})
	}
	if !s.dead {
		s.responses = append(s.responses, files.ChannelFrame{
			Version: files.ChannelProtocolVersion, Type: files.FrameComplete,
			TransferID: s.transferID, ExecutionID: s.executionID,
			Size: int64(len(final)), SHA256: hex.EncodeToString(digest[:]),
		})
	}
	return nil
}

func (s *fakeSession) chunk(frame files.ChannelFrame) error {
	if s.direction != files.ChannelUpload || s.uploadPath == "" {
		s.queueError("protocol_error")
		return nil
	}
	expected := int64(len(s.uploadBuffer))
	if frame.Offset != expected {
		s.queueError("protocol_error")
		return nil
	}
	next := append(s.uploadBuffer, frame.Data...)
	if int64(len(next)) > s.uploadSize {
		s.queueError("protocol_error")
		return nil
	}
	s.uploadBuffer = next
	if configured := s.agent.failUpload[s.nodeID]; configured > 0 && len(s.uploadBuffer) >= configured {
		if s.agent.partial[s.nodeID] == nil {
			s.agent.partial[s.nodeID] = map[string][]byte{}
		}
		s.agent.partial[s.nodeID][s.uploadPath] = append([]byte(nil), s.uploadBuffer...)
		s.agent.failUpload[s.nodeID] = 0
		s.dead = true
		return nil
	}
	s.responses = append(s.responses, files.ChannelFrame{
		Version: files.ChannelProtocolVersion, Type: files.FrameAck,
		TransferID: s.transferID, ExecutionID: s.executionID,
		Offset: frame.Offset + int64(len(frame.Data)),
	})
	return nil
}

func (s *fakeSession) complete(frame files.ChannelFrame) error {
	if s.direction != files.ChannelUpload {
		s.queueError("protocol_error")
		return nil
	}
	if s.agent.corruptDigest {
		s.queueError("checksum_mismatch")
		return nil
	}
	if frame.Size != s.uploadSize || frame.SHA256 != s.uploadSHA || int64(len(s.uploadBuffer)) != s.uploadSize {
		s.queueError("checksum_mismatch")
		return nil
	}
	digest := sha256.Sum256(s.uploadBuffer)
	if hex.EncodeToString(digest[:]) != s.uploadSHA {
		s.queueError("checksum_mismatch")
		return nil
	}
	if s.agent.files[s.nodeID] == nil {
		s.agent.files[s.nodeID] = map[string][]byte{}
	}
	s.agent.files[s.nodeID][s.uploadPath] = append([]byte(nil), s.uploadBuffer...)
	delete(s.agent.partial[s.nodeID], s.uploadPath)
	s.agent.uploads++
	s.responses = append(s.responses, files.ChannelFrame{
		Version: files.ChannelProtocolVersion, Type: files.FrameCompleteAck,
		TransferID: s.transferID, ExecutionID: s.executionID,
		Size: frame.Size, SHA256: frame.SHA256,
	})
	return nil
}
