package files

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"
)

// UploadResult is the agent-confirmed outcome of one upload. Skipped is only
// reported for the explicit skip conflict policy; otherwise a completed result
// always carries the agent's verified size and digest.
type UploadResult struct {
	Skipped         bool
	DestinationPath string
	Bytes           int64
	SHA256          string
	Resumed         bool
}

// ResultUploadTransport is the richer upload port. The controller composition may
// still expose the minimal UploadTransport; the coordinator uses this interface
// when available so a skipped or renamed destination is never misreported.
type ResultUploadTransport interface {
	PushFileResult(ctx context.Context, nodeID, destinationPath string, content io.Reader, size int64, sha256Hex string) (UploadResult, error)
}

// PolicyUploadTransport carries the transfer's conflict policy to the agent. The
// minimal UploadTransport has no policy field, so a copy that skips or renames
// must use this interface instead of pretending an overwrite happened.
type PolicyUploadTransport interface {
	PushFileWithPolicy(ctx context.Context, nodeID, destinationPath string, policy ConflictPolicy, content io.Reader, size int64, sha256Hex string) (UploadResult, error)
}

// ChannelTransportConfig binds a ChannelTransport to the controller's agent
// session factory. FrameTimeout is a per-frame I/O bound, not the total transfer
// deadline; the caller's context remains authoritative.
type ChannelTransportConfig struct {
	Channel       AgentFileChannel
	MaxChunkBytes int
	MaxFileBytes  int64
	MaxAttempts   int
	FrameTimeout  time.Duration
}

// ChannelTransport implements UploadTransport and DownloadTransport over the
// bounded agent file channel. It never opens SSH or a second execution queue.
type ChannelTransport struct {
	channel      AgentFileChannel
	maxChunk     int
	maxFile      int64
	maxAttempts  int
	frameTimeout time.Duration
}

var (
	_ UploadTransport       = (*ChannelTransport)(nil)
	_ ResultUploadTransport = (*ChannelTransport)(nil)
	_ PolicyUploadTransport = (*ChannelTransport)(nil)
	_ DownloadTransport     = (*ChannelTransport)(nil)
)

func NewChannelTransport(config ChannelTransportConfig) (*ChannelTransport, error) {
	if config.Channel == nil {
		return nil, ErrChannelUnavailable
	}
	chunk := config.MaxChunkBytes
	if chunk <= 0 {
		chunk = DefaultChannelChunkBytes
	}
	if chunk > MaxChannelChunkBytes {
		return nil, fmt.Errorf("%w: chunk ceiling", ErrChannelProtocol)
	}
	maxFile := config.MaxFileBytes
	if maxFile <= 0 {
		maxFile = DefaultChannelFileBytes
	}
	attempts := config.MaxAttempts
	if attempts <= 0 {
		attempts = DefaultChannelAttempts
	}
	timeout := config.FrameTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &ChannelTransport{channel: config.Channel, maxChunk: chunk, maxFile: maxFile, maxAttempts: attempts, frameTimeout: timeout}, nil
}

func (t *ChannelTransport) PushFile(ctx context.Context, nodeID, destinationPath string, content io.Reader, size int64, sha256Hex string) error {
	_, err := t.PushFileWithPolicy(ctx, nodeID, destinationPath, ConflictOverwrite, content, size, sha256Hex)
	return err
}

func (t *ChannelTransport) PushFileResult(ctx context.Context, nodeID, destinationPath string, content io.Reader, size int64, sha256Hex string) (UploadResult, error) {
	return t.PushFileWithPolicy(ctx, nodeID, destinationPath, ConflictOverwrite, content, size, sha256Hex)
}

func (t *ChannelTransport) PushFileWithPolicy(ctx context.Context, nodeID, destinationPath string, policy ConflictPolicy, content io.Reader, size int64, sha256Hex string) (UploadResult, error) {
	if t == nil || t.channel == nil {
		return UploadResult{}, ErrChannelUnavailable
	}
	if content == nil || size < 0 {
		return UploadResult{}, ErrInvalidTransfer
	}
	if size > t.maxFile {
		return UploadResult{}, ErrFileTooLarge
	}
	if !sha256Pattern.MatchString(sha256Hex) {
		return UploadResult{}, ErrInvalidTransfer
	}
	if !validChannelToken(nodeID, 128) {
		return UploadResult{}, ErrInvalidTransfer
	}
	if !policy.Valid() {
		return UploadResult{}, ErrInvalidTransfer
	}
	if err := ValidateRemotePath(destinationPath); err != nil {
		return UploadResult{}, err
	}
	if destinationPath == "/" {
		return UploadResult{}, ErrInvalidTransfer
	}
	taskID, transferID, executionID := channelIDs(ctx)
	seeker, seekable := content.(io.Seeker)
	var lastErr error
	for attempt := 1; attempt <= t.maxAttempts; attempt++ {
		result, err := t.pushOnce(ctx, nodeID, destinationPath, policy, content, seeker, seekable, size, sha256Hex, taskID, transferID, executionID)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return UploadResult{}, ctx.Err()
		}
		if !retryableChannelError(err) || attempt == t.maxAttempts {
			break
		}
		if !seekable {
			return UploadResult{}, ErrResumeUnsupported
		}
	}
	return UploadResult{}, lastErr
}

func (t *ChannelTransport) pushOnce(ctx context.Context, nodeID, destinationPath string, policy ConflictPolicy, content io.Reader, seeker io.Seeker, seekable bool, size int64, sha256Hex, taskID, transferID, executionID string) (UploadResult, error) {
	session, err := t.channel.OpenFileChannel(ctx, nodeID, transferID, executionID)
	if err != nil {
		return UploadResult{}, fmt.Errorf("%w: %v", ErrChannelDisconnected, err)
	}
	defer session.Close()

	err = t.send(ctx, session, ChannelFrame{
		Version: ChannelProtocolVersion, Type: FrameOpen, Direction: ChannelUpload,
		TaskID: taskID, TransferID: transferID, ExecutionID: executionID, Path: destinationPath,
		Size: size, SHA256: sha256Hex, ConflictPolicy: policy, CreateParents: true,
	})
	if err != nil {
		return UploadResult{}, err
	}
	ack, err := t.receive(ctx, session)
	if err != nil {
		return UploadResult{}, err
	}
	if ack.Type == FrameError {
		return UploadResult{}, channelError(ack.ErrorCode)
	}
	if err := expectFrame(ack, FrameOpenAck, transferID, executionID); err != nil {
		return UploadResult{}, err
	}
	if ack.Skipped {
		return UploadResult{Skipped: true, DestinationPath: destinationPath}, nil
	}
	if ack.ResumeOffset < 0 || ack.ResumeOffset > size {
		return UploadResult{}, fmt.Errorf("%w: resume offset", ErrChannelProtocol)
	}
	resolved := destinationPath
	if ack.ResolvedPath != "" && ack.ResolvedPath != destinationPath {
		if err := ValidateRemotePath(ack.ResolvedPath); err != nil || ack.ResolvedPath == "/" {
			return UploadResult{}, ErrAgentPathRejected
		}
		resolved = ack.ResolvedPath
	}
	offset := ack.ResumeOffset
	resumed := offset > 0
	if !seekable {
		if offset != 0 {
			return UploadResult{}, ErrResumeUnsupported
		}
	} else if _, err := seeker.Seek(offset, io.SeekStart); err != nil {
		return UploadResult{}, fmt.Errorf("%w: %v", ErrResumeUnsupported, err)
	}
	buffer := make([]byte, t.maxChunk)
	for offset < size {
		want := size - offset
		if want > int64(len(buffer)) {
			want = int64(len(buffer))
		}
		if _, err := io.ReadFull(content, buffer[:want]); err != nil {
			if ctx.Err() != nil {
				t.cancel(session, transferID, executionID, "caller_canceled")
				return UploadResult{}, ctx.Err()
			}
			return UploadResult{}, fmt.Errorf("files: read source: %w", err)
		}
		if err := t.send(ctx, session, ChannelFrame{
			Version: ChannelProtocolVersion, Type: FrameChunk, Direction: ChannelUpload,
			TaskID: taskID, TransferID: transferID, ExecutionID: executionID, Offset: offset,
			Data: buffer[:want],
		}); err != nil {
			return UploadResult{}, err
		}
		frame, err := t.receive(ctx, session)
		if err != nil {
			return UploadResult{}, err
		}
		if frame.Type == FrameError {
			return UploadResult{}, channelError(frame.ErrorCode)
		}
		if err := expectFrame(frame, FrameAck, transferID, executionID); err != nil {
			return UploadResult{}, err
		}
		if frame.Offset != offset+want {
			return UploadResult{}, fmt.Errorf("%w: ack offset", ErrChannelProtocol)
		}
		offset += want
	}
	if err := t.send(ctx, session, ChannelFrame{
		Version: ChannelProtocolVersion, Type: FrameComplete, Direction: ChannelUpload,
		TaskID: taskID, TransferID: transferID, ExecutionID: executionID, Size: size, SHA256: sha256Hex,
	}); err != nil {
		return UploadResult{}, err
	}
	frame, err := t.receive(ctx, session)
	if err != nil {
		return UploadResult{}, err
	}
	if frame.Type == FrameError {
		return UploadResult{}, channelError(frame.ErrorCode)
	}
	if err := expectFrame(frame, FrameCompleteAck, transferID, executionID); err != nil {
		return UploadResult{}, err
	}
	if frame.Size != size || frame.SHA256 != sha256Hex {
		return UploadResult{}, ErrAgentChecksum
	}
	return UploadResult{DestinationPath: resolved, Bytes: size, SHA256: sha256Hex, Resumed: resumed}, nil
}

// PullFile streams one remote file into sink. Bytes already written to sink are
// resumed from after a disconnect, so a caller must pass the same sink for the
// duration of the call and must discard it on error.
func (t *ChannelTransport) PullFile(ctx context.Context, nodeID, remotePath string, sink io.Writer) (int64, error) {
	if t == nil || t.channel == nil {
		return 0, ErrChannelUnavailable
	}
	if sink == nil {
		return 0, ErrInvalidTransfer
	}
	if !validChannelToken(nodeID, 128) {
		return 0, ErrInvalidTransfer
	}
	if err := ValidateRemotePath(remotePath); err != nil {
		return 0, err
	}
	if remotePath == "/" {
		return 0, ErrInvalidTransfer
	}
	taskID, transferID, executionID := channelIDs(ctx)
	var (
		written      int64
		expectedSize int64 = -1
		expectedSHA  string
		hasher       = sha256.New()
		lastErr      error
	)
	for attempt := 1; attempt <= t.maxAttempts; attempt++ {
		finished, err := t.pullOnce(ctx, nodeID, remotePath, sink, hasher, &written, &expectedSize, &expectedSHA, taskID, transferID, executionID)
		if err == nil && finished {
			digest := hex.EncodeToString(hasher.Sum(nil))
			if written != expectedSize {
				return written, ErrFileSizeMismatch
			}
			if digest != expectedSHA {
				return written, ErrAgentChecksum
			}
			return written, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return written, ctx.Err()
		}
		if err == nil || !retryableChannelError(err) || attempt == t.maxAttempts {
			break
		}
	}
	return written, lastErr
}

func (t *ChannelTransport) pullOnce(ctx context.Context, nodeID, remotePath string, sink io.Writer, hasher io.Writer, written *int64, expectedSize *int64, expectedSHA *string, taskID, transferID, executionID string) (bool, error) {
	session, err := t.channel.OpenFileChannel(ctx, nodeID, transferID, executionID)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrChannelDisconnected, err)
	}
	defer session.Close()

	if err := t.send(ctx, session, ChannelFrame{
		Version: ChannelProtocolVersion, Type: FrameOpen, Direction: ChannelDownload,
		TaskID: taskID, TransferID: transferID, ExecutionID: executionID, Path: remotePath, Offset: *written,
	}); err != nil {
		return false, err
	}
	ack, err := t.receive(ctx, session)
	if err != nil {
		return false, err
	}
	if ack.Type == FrameError {
		return false, channelError(ack.ErrorCode)
	}
	if err := expectFrame(ack, FrameOpenAck, transferID, executionID); err != nil {
		return false, err
	}
	if ack.Size < 0 || ack.Size > t.maxFile || ack.ResumeOffset != *written {
		return false, fmt.Errorf("%w: open ack", ErrChannelProtocol)
	}
	if !sha256Pattern.MatchString(ack.SHA256) {
		return false, fmt.Errorf("%w: open digest", ErrChannelProtocol)
	}
	if *expectedSize < 0 {
		*expectedSize = ack.Size
		*expectedSHA = ack.SHA256
	} else if *expectedSize != ack.Size || *expectedSHA != ack.SHA256 {
		return false, fmt.Errorf("%w: file changed between attempts", ErrChannelProtocol)
	}
	for {
		frame, err := t.receive(ctx, session)
		if err != nil {
			return false, err
		}
		switch frame.Type {
		case FrameChunk:
			if frame.Offset != *written || len(frame.Data) == 0 || len(frame.Data) > t.maxChunk {
				return false, fmt.Errorf("%w: chunk offset", ErrChannelProtocol)
			}
			if *written+int64(len(frame.Data)) > *expectedSize {
				return false, fmt.Errorf("%w: chunk exceeds declared size", ErrChannelProtocol)
			}
			if _, err := sink.Write(frame.Data); err != nil {
				return false, fmt.Errorf("files: write destination: %w", err)
			}
			if _, err := hasher.Write(frame.Data); err != nil {
				return false, fmt.Errorf("files: hash received bytes: %w", err)
			}
			*written += int64(len(frame.Data))
			if err := t.send(ctx, session, ChannelFrame{
				Version: ChannelProtocolVersion, Type: FrameAck, Direction: ChannelDownload,
				TaskID: taskID, TransferID: transferID, ExecutionID: executionID, Offset: *written,
			}); err != nil {
				return false, err
			}
		case FrameComplete:
			if frame.Size != *expectedSize || frame.SHA256 != *expectedSHA {
				return false, fmt.Errorf("%w: completion metadata", ErrChannelProtocol)
			}
			if err := t.send(ctx, session, ChannelFrame{
				Version: ChannelProtocolVersion, Type: FrameCompleteAck, Direction: ChannelDownload,
				TaskID: taskID, TransferID: transferID, ExecutionID: executionID, Size: *expectedSize, SHA256: *expectedSHA,
			}); err != nil {
				return false, err
			}
			return true, nil
		case FrameError:
			return false, channelError(frame.ErrorCode)
		default:
			return false, fmt.Errorf("%w: unexpected frame %s", ErrChannelProtocol, frame.Type)
		}
	}
}

func (t *ChannelTransport) send(ctx context.Context, session FileChannelSession, frame ChannelFrame) error {
	if err := validateChannelFrame(frame, t.maxChunk, t.maxFile); err != nil && frame.Type != FrameAck && frame.Type != FrameComplete && frame.Type != FrameCompleteAck {
		return err
	}
	sendCtx, cancel := context.WithTimeout(ctx, t.frameTimeout)
	defer cancel()
	if err := session.Send(sendCtx, frame); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: %v", ErrChannelDisconnected, err)
	}
	return nil
}

func (t *ChannelTransport) receive(ctx context.Context, session FileChannelSession) (ChannelFrame, error) {
	receiveCtx, cancel := context.WithTimeout(ctx, t.frameTimeout)
	defer cancel()
	frame, err := session.Receive(receiveCtx)
	if err != nil {
		if ctx.Err() != nil {
			return ChannelFrame{}, ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return ChannelFrame{}, fmt.Errorf("%w: frame timeout", ErrChannelDisconnected)
		}
		return ChannelFrame{}, fmt.Errorf("%w: %v", ErrChannelDisconnected, err)
	}
	return frame, nil
}

func (t *ChannelTransport) cancel(session FileChannelSession, transferID, executionID, reason string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = session.Send(ctx, ChannelFrame{
		Version: ChannelProtocolVersion, Type: FrameCancel, Direction: ChannelUpload,
		TransferID: transferID, ExecutionID: executionID, ErrorCode: reason,
	})
}

func expectFrame(frame ChannelFrame, kind ChannelFrameType, transferID, executionID string) error {
	if frame.Type != kind || frame.TransferID != transferID || frame.ExecutionID != executionID {
		return fmt.Errorf("%w: expected %s", ErrChannelProtocol, kind)
	}
	return nil
}

func retryableChannelError(err error) bool {
	return errors.Is(err, ErrChannelDisconnected)
}

func channelIDs(ctx context.Context) (string, string, string) {
	identity := channelIdentityFrom(ctx)
	if validChannelToken(identity.transferID, 128) && validChannelToken(identity.executionID, 160) {
		taskID := identity.taskID
		if taskID == "" {
			taskID = "task_" + identity.transferID
		}
		return taskID, identity.transferID, identity.executionID
	}
	transferID := newID("xfer")
	return "task_" + transferID, transferID, "exec_" + transferID
}
