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

// PolicyUploadTransport carries the transfer's conflict policy to the agent.
type PolicyUploadTransport interface {
	PushFileWithPolicy(ctx context.Context, nodeID, destinationPath string, policy ConflictPolicy, content io.Reader, size int64, sha256Hex string) (UploadResult, error)
}

// ChannelTransportConfig binds a ChannelTransport to the generated CX file
// session adapter and the explicit absolute-to-relative path mapper. FrameTimeout
// is a per-frame I/O bound; the caller's context remains authoritative.
type ChannelTransportConfig struct {
	Channel       AgentFileChannel
	Paths         PathMapper
	MaxChunkBytes int
	MaxFileBytes  int64
	MaxAttempts   int
	FrameTimeout  time.Duration
}

// ChannelTransport implements UploadTransport and DownloadTransport over the
// bounded CX file frames. It never opens SSH or a second execution queue.
type ChannelTransport struct {
	channel      AgentFileChannel
	paths        PathMapper
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
	if config.Paths == nil {
		return nil, ErrPathMappingUnavailable
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
	if maxFile > DefaultChannelFileBytes {
		return nil, fmt.Errorf("%w: file ceiling", ErrChannelProtocol)
	}
	attempts := config.MaxAttempts
	if attempts <= 0 {
		attempts = DefaultChannelAttempts
	}
	timeout := config.FrameTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &ChannelTransport{channel: config.Channel, paths: config.Paths, maxChunk: chunk, maxFile: maxFile, maxAttempts: attempts, frameTimeout: timeout}, nil
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
	if t.paths == nil {
		return UploadResult{}, ErrPathMappingUnavailable
	}
	if content == nil || size < 0 {
		return UploadResult{}, ErrInvalidTransfer
	}
	if size > t.maxFile {
		return UploadResult{}, ErrFileTooLarge
	}
	if !sha256Pattern.MatchString(sha256Hex) || !validChannelID(nodeID) || !policy.Valid() {
		return UploadResult{}, ErrInvalidTransfer
	}
	if err := ValidateRemotePath(destinationPath); err != nil || destinationPath == "/" {
		return UploadResult{}, ErrInvalidTransfer
	}
	scope := channelScopeFrom(ctx)
	if !scope.valid() {
		return UploadResult{}, ErrTaskBindingRequired
	}
	authorizer := channelAuthorizerFrom(ctx)
	if authorizer == nil {
		return UploadResult{}, ErrAuthorizationRequired
	}
	relativePath, err := t.paths.RelativePath(ctx, nodeID, destinationPath)
	if err != nil || !validRelativePath(relativePath) {
		return UploadResult{}, ErrPathMappingUnavailable
	}
	seeker, seekable := content.(io.Seeker)
	confirmed := int64(0)
	var lastErr error
	for attempt := 1; attempt <= t.maxAttempts; attempt++ {
		result, next, err := t.pushOnce(ctx, scope, authorizer, nodeID, relativePath, destinationPath, policy, content, seeker, seekable, confirmed, size, sha256Hex)
		if err == nil {
			return result, nil
		}
		lastErr = err
		confirmed = next
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

func (t *ChannelTransport) pushOnce(ctx context.Context, scope channelScope, authorizer ChannelAuthorizer, nodeID, relativePath, destinationPath string, policy ConflictPolicy, content io.Reader, seeker io.Seeker, seekable bool, confirmed, size int64, sha256Hex string) (UploadResult, int64, error) {
	if err := authorizer.AuthorizeChannel(ctx, ChannelWrite); err != nil {
		return UploadResult{}, confirmed, err
	}
	binding := FileBinding{
		TaskID: scope.taskID, ExecutionID: scope.executionID, TransferID: scope.transferID,
		NodeID: nodeID, RelativePath: relativePath, Direction: ChannelWrite,
		Size: size, SHA256: sha256Hex, ConflictPolicy: policy, ResumeOffset: confirmed,
		CreateParents: true, PlanDigestSHA256: scope.planDigest,
	}
	if err := validateBinding(binding, t.maxFile); err != nil {
		return UploadResult{}, confirmed, err
	}
	session, opened, err := t.open(ctx, binding)
	if err != nil {
		return UploadResult{}, confirmed, err
	}
	defer session.Close()
	if err := validateOpenResult(binding, opened, t.maxFile); err != nil {
		return UploadResult{}, confirmed, err
	}
	codeErr := channelError(opened.ResultCode)
	if opened.Skipped {
		return UploadResult{Skipped: true, DestinationPath: destinationPath}, confirmed, nil
	}
	if codeErr != nil {
		return UploadResult{}, confirmed, codeErr
	}
	if opened.Size != size || opened.SHA256 != sha256Hex {
		return UploadResult{}, confirmed, ErrAgentChecksum
	}
	resolvedPath, err := t.paths.AbsolutePath(ctx, nodeID, opened.RelativePath)
	if err != nil || opened.RelativePath == "" {
		resolvedPath = destinationPath
	}
	if opened.Complete {
		return UploadResult{DestinationPath: resolvedPath, Bytes: size, SHA256: sha256Hex, Resumed: confirmed > 0}, confirmed, nil
	}
	if err := authorizer.AuthorizeChannel(ctx, ChannelWrite); err != nil {
		return UploadResult{}, confirmed, err
	}
	offset := opened.ResumeOffset
	if confirmed > 0 && offset < confirmed {
		return UploadResult{}, confirmed, fmt.Errorf("%w: agent resume moved backwards", ErrChannelProtocol)
	}
	resumed := offset > 0
	if !seekable {
		if offset != 0 {
			return UploadResult{}, confirmed, ErrResumeUnsupported
		}
	} else if _, err := seeker.Seek(offset, io.SeekStart); err != nil {
		return UploadResult{}, confirmed, fmt.Errorf("%w: %v", ErrResumeUnsupported, err)
	}
	buffer := make([]byte, t.maxChunk)
	for offset < size {
		if err := authorizer.AuthorizeChannel(ctx, ChannelWrite); err != nil {
			t.cancel(ctx, session, "write_authority_revoked")
			return UploadResult{}, offset, err
		}
		want := size - offset
		if want > int64(len(buffer)) {
			want = int64(len(buffer))
		}
		if _, err := io.ReadFull(content, buffer[:want]); err != nil {
			if ctx.Err() != nil {
				t.cancel(ctx, session, "caller_canceled")
				return UploadResult{}, offset, ctx.Err()
			}
			return UploadResult{}, offset, fmt.Errorf("files: read source: %w", err)
		}
		finish := offset+want == size
		reply, err := t.writeChunk(ctx, session, binding, FileChunkOp{Offset: uint64(offset), Data: buffer[:want], Finish: finish})
		if err != nil {
			if finish {
				return UploadResult{}, offset, unknownCommitError(err)
			}
			return UploadResult{}, offset, err
		}
		if err := validateChunkReply(binding, reply, uint64(offset), uint32(want), finish, finish); err != nil {
			return UploadResult{}, offset, err
		}
		if reply.NextOffset != uint64(offset)+uint64(want) {
			return UploadResult{}, offset, fmt.Errorf("%w: chunk next offset", ErrChannelProtocol)
		}
		offset = int64(reply.NextOffset)
		if finish {
			if reply.Complete {
				return UploadResult{DestinationPath: resolvedPath, Bytes: size, SHA256: sha256Hex, Resumed: resumed}, offset, nil
			}
			return UploadResult{}, offset, ErrAgentUnknown
		}
	}
	// Empty file: commit with a finish-only request.
	if err := authorizer.AuthorizeChannel(ctx, ChannelWrite); err != nil {
		return UploadResult{}, 0, err
	}
	reply, err := t.writeChunk(ctx, session, binding, FileChunkOp{Offset: 0, Finish: true})
	if err != nil {
		return UploadResult{}, 0, unknownCommitError(err)
	}
	if err := validateChunkReply(binding, reply, 0, 0, true, true); err != nil {
		return UploadResult{}, 0, err
	}
	if !reply.Complete {
		return UploadResult{}, 0, ErrAgentUnknown
	}
	return UploadResult{DestinationPath: resolvedPath, Bytes: 0, SHA256: sha256Hex, Resumed: resumed}, 0, nil
}

// PullFile streams one remote file into sink. Bytes already written to sink are
// resumed after a disconnect, so the caller must pass the same sink for the call.
func (t *ChannelTransport) PullFile(ctx context.Context, nodeID, remotePath string, sink io.Writer) (int64, error) {
	if t == nil || t.channel == nil {
		return 0, ErrChannelUnavailable
	}
	if t.paths == nil {
		return 0, ErrPathMappingUnavailable
	}
	if sink == nil {
		return 0, ErrInvalidTransfer
	}
	if !validChannelID(nodeID) {
		return 0, ErrInvalidTransfer
	}
	if err := ValidateRemotePath(remotePath); err != nil || remotePath == "/" {
		return 0, ErrInvalidTransfer
	}
	scope := channelScopeFrom(ctx)
	if !scope.valid() {
		return 0, ErrTaskBindingRequired
	}
	authorizer := channelAuthorizerFrom(ctx)
	if authorizer == nil {
		return 0, ErrAuthorizationRequired
	}
	relativePath, err := t.paths.RelativePath(ctx, nodeID, remotePath)
	if err != nil || !validRelativePath(relativePath) {
		return 0, ErrPathMappingUnavailable
	}
	var (
		written      int64
		expectedSize int64 = -1
		expectedSHA  string
		hasher       = sha256.New()
		lastErr      error
	)
	for attempt := 1; attempt <= t.maxAttempts; attempt++ {
		complete, err := t.pullOnce(ctx, scope, authorizer, nodeID, relativePath, sink, hasher, &written, &expectedSize, &expectedSHA)
		if err == nil && complete {
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

func (t *ChannelTransport) pullOnce(ctx context.Context, scope channelScope, authorizer ChannelAuthorizer, nodeID, relativePath string, sink io.Writer, hasher io.Writer, written *int64, expectedSize *int64, expectedSHA *string) (bool, error) {
	if err := authorizer.AuthorizeChannel(ctx, ChannelRead); err != nil {
		return false, err
	}
	binding := FileBinding{
		TaskID: scope.taskID, ExecutionID: scope.executionID, TransferID: scope.transferID,
		NodeID: nodeID, RelativePath: relativePath, Direction: ChannelRead,
		ResumeOffset: *written, PlanDigestSHA256: scope.planDigest,
	}
	if err := validateBinding(binding, t.maxFile); err != nil {
		return false, err
	}
	session, opened, err := t.open(ctx, binding)
	if err != nil {
		return false, err
	}
	defer session.Close()
	if err := validateOpenResult(binding, opened, t.maxFile); err != nil {
		return false, err
	}
	if codeErr := channelError(opened.ResultCode); codeErr != nil {
		return false, codeErr
	}
	if opened.ResumeOffset != *written {
		return false, fmt.Errorf("%w: open resume offset", ErrChannelProtocol)
	}
	if *expectedSize < 0 {
		*expectedSize = opened.Size
		*expectedSHA = opened.SHA256
	} else if *expectedSize != opened.Size || *expectedSHA != opened.SHA256 {
		return false, fmt.Errorf("%w: file changed between attempts", ErrChannelProtocol)
	}
	if opened.Complete {
		return *written == *expectedSize, nil
	}
	for {
		if err := authorizer.AuthorizeChannel(ctx, ChannelRead); err != nil {
			t.cancel(ctx, session, "read_authority_revoked")
			return false, err
		}
		remaining := *expectedSize - *written
		request := uint32(t.maxChunk)
		if int64(request) > remaining {
			request = uint32(remaining)
		}
		if request == 0 {
			return false, fmt.Errorf("%w: zero read request", ErrChannelProtocol)
		}
		reply, err := t.readChunk(ctx, session, binding, FileChunkOp{Offset: uint64(*written), ReadBytes: request})
		if err != nil {
			return false, err
		}
		if err := validateChunkReply(binding, reply, uint64(*written), request, false, true); err != nil {
			return false, err
		}
		if len(reply.Data) == 0 {
			return false, fmt.Errorf("%w: empty read chunk", ErrChannelProtocol)
		}
		if reply.NextOffset != uint64(*written)+uint64(len(reply.Data)) {
			return false, fmt.Errorf("%w: read next offset", ErrChannelProtocol)
		}
		if _, err := sink.Write(reply.Data); err != nil {
			return false, fmt.Errorf("files: write destination: %w", err)
		}
		if _, err := hasher.Write(reply.Data); err != nil {
			return false, fmt.Errorf("files: hash received bytes: %w", err)
		}
		*written = int64(reply.NextOffset)
		if reply.Complete {
			return true, nil
		}
	}
}

func (t *ChannelTransport) open(ctx context.Context, binding FileBinding) (AgentFileSession, FileOpenResult, error) {
	callCtx, cancel := context.WithTimeout(ctx, t.frameTimeout)
	defer cancel()
	var session AgentFileSession
	var opened FileOpenResult
	var err error
	if channel, ok := t.channel.(TransferFileChannel); ok {
		session, opened, err = channel.OpenFileChannelForTransfer(ctx, callCtx, binding)
	} else {
		session, opened, err = t.channel.OpenFileChannel(callCtx, binding)
	}
	if err != nil {
		return nil, FileOpenResult{}, t.sessionError(ctx, err)
	}
	if session == nil {
		return nil, FileOpenResult{}, ErrChannelProtocol
	}
	if opened.TaskID != binding.TaskID || opened.ExecutionID != binding.ExecutionID || opened.TransferID != binding.TransferID {
		_ = session.Close()
		return nil, FileOpenResult{}, fmt.Errorf("%w: open identity echo", ErrChannelProtocol)
	}
	return session, opened, nil
}

func (t *ChannelTransport) readChunk(ctx context.Context, session AgentFileSession, binding FileBinding, op FileChunkOp) (FileChunkResult, error) {
	callCtx, cancel := context.WithTimeout(ctx, t.frameTimeout)
	defer cancel()
	result, err := session.ReadChunk(callCtx, op)
	if err != nil {
		return FileChunkResult{}, t.sessionError(ctx, err)
	}
	return result, nil
}

func (t *ChannelTransport) writeChunk(ctx context.Context, session AgentFileSession, binding FileBinding, op FileChunkOp) (FileChunkResult, error) {
	callCtx, cancel := context.WithTimeout(ctx, t.frameTimeout)
	defer cancel()
	result, err := session.WriteChunk(callCtx, op)
	if err != nil {
		return FileChunkResult{}, t.sessionError(ctx, err)
	}
	return result, nil
}

func validateChunkReply(binding FileBinding, reply FileChunkResult, offset uint64, length uint32, finish, allowComplete bool) error {
	if reply.TaskID != binding.TaskID || reply.ExecutionID != binding.ExecutionID || reply.TransferID != binding.TransferID {
		return fmt.Errorf("%w: chunk identity echo", ErrChannelProtocol)
	}
	if reply.Offset != offset {
		return fmt.Errorf("%w: chunk offset echo", ErrChannelProtocol)
	}
	if codeErr := channelError(reply.ResultCode); codeErr != nil {
		return codeErr
	}
	if !allowComplete && reply.Complete {
		return fmt.Errorf("%w: unexpected early completion", ErrChannelProtocol)
	}
	return nil
}

func (t *ChannelTransport) sessionError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err == nil {
		return nil
	}
	for _, sentinel := range []error{ErrChannelCanceled, ErrAgentPathRejected, ErrAgentSymlink, ErrInsufficientSpace,
		ErrAgentChecksum, ErrAgentConflict, ErrAgentIO, ErrAgentUnauthorized, ErrAgentUnknown, ErrChannelProtocol} {
		if errors.Is(err, sentinel) {
			return err
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: frame timeout", ErrChannelDisconnected)
	}
	return fmt.Errorf("%w: %v", ErrChannelDisconnected, err)
}

func unknownCommitError(err error) error {
	if err == nil {
		return ErrAgentUnknown
	}
	for _, sentinel := range []error{ErrChannelCanceled, ErrAgentPathRejected, ErrAgentSymlink, ErrInsufficientSpace,
		ErrAgentChecksum, ErrAgentConflict, ErrAgentIO, ErrAgentUnauthorized, ErrChannelProtocol} {
		if errors.Is(err, sentinel) {
			return err
		}
	}
	return ErrAgentUnknown
}

func (t *ChannelTransport) cancel(ctx context.Context, session AgentFileSession, reason string) {
	cancelCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = session.Cancel(cancelCtx, reason)
}

func retryableChannelError(err error) bool {
	return errors.Is(err, ErrChannelDisconnected)
}
