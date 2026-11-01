package files

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
)

const (
	DefaultChannelChunkBytes = 16 << 10
	MaxChannelChunkBytes     = 32 << 10
	DefaultChannelFileBytes  = 64 << 20 // matches the CX agent file capability ceiling
	DefaultChannelAttempts   = 3
)

// ChannelDirection is the immutable binding direction. The generated CX frames
// use the same lowercase vocabulary.
type ChannelDirection string

const (
	ChannelRead  ChannelDirection = "read"
	ChannelWrite ChannelDirection = "write"
)

func (d ChannelDirection) valid() bool {
	return d == ChannelRead || d == ChannelWrite
}

var channelIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// FileBinding is the immutable per-file reservation. It corresponds to the
// generated FileOpenRequest fields plus the authenticated node identity. Paths
// are relative to the operator-configured agent file root; absolute paths never
// cross the wire.
type FileBinding struct {
	TaskID           string
	ExecutionID      string
	TransferID       string
	NodeID           string
	RelativePath     string
	Direction        ChannelDirection
	Size             int64
	SHA256           string
	ConflictPolicy   ConflictPolicy
	ResumeOffset     int64
	CreateParents    bool
	PlanDigestSHA256 string
}

// FileOpenResult is the validated FileOpenReply.
type FileOpenResult struct {
	TaskID           string
	ExecutionID      string
	TransferID       string
	Size             int64
	SHA256           string
	RelativePath     string
	ResumeOffset     int64
	Skipped          bool
	Complete         bool
	ResultCode       string
	PlanDigestSHA256 string
}

// FileChunkOp mirrors FileChunkRequest: read requests use ReadBytes, write
// requests use Data/Finish. Cancel has no data/read bytes/finish.
type FileChunkOp struct {
	Offset    uint64
	ReadBytes uint32
	Data      []byte
	Finish    bool
	Cancel    bool
}

// FileChunkResult is the validated FileChunkReply.
type FileChunkResult struct {
	TaskID      string
	ExecutionID string
	TransferID  string
	Offset      uint64
	NextOffset  uint64
	Data        []byte
	Complete    bool
	ResultCode  string
}

// AgentFileSession is one duplex file session bound to an authenticated node and
// an immutable FileBinding. The controller adapter maps these calls to generated
// FileChunkRequest/FileChunkReply envelopes.
type AgentFileSession interface {
	ReadChunk(context.Context, FileChunkOp) (FileChunkResult, error)
	WriteChunk(context.Context, FileChunkOp) (FileChunkResult, error)
	Cancel(context.Context, string) error
	Close() error
}

// AgentFileChannel opens the open handshake and returns a session bound to the
// result. Implementations live in the controller composition and must bind the
// node to the mTLS-authenticated link, never to a frame-supplied node id.
type AgentFileChannel interface {
	OpenFileChannel(ctx context.Context, binding FileBinding) (AgentFileSession, FileOpenResult, error)
}

// PathMapper is the explicit absolute-to-relative mapping. A missing mapper makes
// every channel operation fail closed; there is no silent slash stripping.
type PathMapper interface {
	RelativePath(ctx context.Context, nodeID, absolutePath string) (string, error)
	AbsolutePath(ctx context.Context, nodeID, relativePath string) (string, error)
}

// ChannelAuthorizer rechecks current source-read or target-write authority before
// every chunk and before commit. It must not cache a previous decision.
type ChannelAuthorizer interface {
	AuthorizeChannel(ctx context.Context, direction ChannelDirection) error
}

var (
	ErrChannelUnavailable     = errors.New("files: agent file channel is unavailable")
	ErrChannelProtocol        = errors.New("files: agent file channel protocol violation")
	ErrChannelDisconnected    = errors.New("files: agent file channel disconnected")
	ErrChannelCanceled        = errors.New("files: file transfer was canceled")
	ErrAgentPathRejected      = errors.New("files: agent rejected the remote path")
	ErrAgentSymlink           = errors.New("files: agent rejected a symbolic link")
	ErrInsufficientSpace      = errors.New("files: destination has insufficient space")
	ErrAgentChecksum          = errors.New("files: agent checksum mismatch")
	ErrAgentConflict          = errors.New("files: destination conflict")
	ErrAgentIO                = errors.New("files: agent file operation failed")
	ErrAgentUnauthorized      = errors.New("files: agent rejected the file capability")
	ErrAgentUnknown           = errors.New("files: agent file commit result is unknown")
	ErrResumeUnsupported      = errors.New("files: resume requires a seekable source")
	ErrFileTooLarge           = errors.New("files: file exceeds the channel size limit")
	ErrFileSizeMismatch       = errors.New("files: transferred size does not match the declared size")
	ErrTransferExecution      = errors.New("files: transfer execution is not allowed")
	ErrPathMappingUnavailable = errors.New("files: explicit remote path mapping is not configured")
	ErrTaskBindingRequired    = errors.New("files: channel task/execution/plan binding is required")
	ErrAuthorizationRequired  = errors.New("files: per-chunk authorizer is required")
	ErrCoordinatorUnavailable = errors.New("files: transfer coordinator is not configured")
)

// ChannelError is a stable, payload-free agent error.
type ChannelError struct {
	Code    string
	Message string
}

func (e *ChannelError) Error() string {
	if e == nil {
		return "files: channel error"
	}
	if e.Message == "" {
		return "files: channel error " + e.Code
	}
	return "files: channel error " + e.Code + ": " + e.Message
}

func channelError(code string) error {
	switch strings.ToLower(code) {
	case "", "ok", "complete":
		return nil
	case "skipped", "skip":
		return ErrAgentConflict
	case "path_rejected", "invalid_path":
		return ErrAgentPathRejected
	case "symlink_forbidden":
		return ErrAgentSymlink
	case "insufficient_space":
		return ErrInsufficientSpace
	case "checksum_mismatch":
		return ErrAgentChecksum
	case "conflict":
		return ErrAgentConflict
	case "canceled", "cancelled":
		return ErrChannelCanceled
	case "unauthorized", "capability_denied":
		return ErrAgentUnauthorized
	case "io_error", "open_failed", "write_failed", "read_failed":
		return ErrAgentIO
	case "protocol_error", "unsupported", "frame_too_large":
		return ErrChannelProtocol
	case "unknown", "commit_unknown":
		return ErrAgentUnknown
	default:
		return fmt.Errorf("%w: %s", ErrAgentIO, boundedCode(code))
	}
}

func boundedCode(code string) string {
	if code == "" {
		return "unknown"
	}
	if len(code) > 64 {
		code = code[:64]
	}
	for _, r := range code {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '.' || r == '-' {
			continue
		}
		return "invalid"
	}
	return code
}

func validChannelID(value string) bool {
	return channelIDPattern.MatchString(value)
}

func validRelativePath(value string) bool {
	if value == "" || len(value) > 1024 || !fs.ValidPath(value) || value == "." ||
		strings.Contains(value, "\\") || strings.Contains(value, ":") ||
		strings.HasPrefix(value, ".sf-") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." || len(segment) > 255 {
			return false
		}
	}
	return true
}

func validateBinding(binding FileBinding, maxFile int64) error {
	if !validChannelID(binding.TaskID) || !validChannelID(binding.ExecutionID) ||
		!validChannelID(binding.TransferID) || !validChannelID(binding.NodeID) {
		return ErrChannelProtocol
	}
	if !validRelativePath(binding.RelativePath) || !binding.Direction.valid() {
		return ErrChannelProtocol
	}
	if binding.Size < 0 || binding.Size > maxFile || binding.ResumeOffset < 0 {
		return ErrChannelProtocol
	}
	if binding.Direction == ChannelWrite {
		if binding.ResumeOffset > binding.Size || !sha256Pattern.MatchString(binding.SHA256) || !binding.ConflictPolicy.Valid() {
			return ErrChannelProtocol
		}
	} else if binding.SHA256 != "" && !sha256Pattern.MatchString(binding.SHA256) {
		return ErrChannelProtocol
	}
	if !sha256Pattern.MatchString(binding.PlanDigestSHA256) {
		return ErrTaskBindingRequired
	}
	return nil
}

func validateOpenResult(binding FileBinding, result FileOpenResult, maxFile int64) error {
	if result.PlanDigestSHA256 != binding.PlanDigestSHA256 {
		return fmt.Errorf("%w: plan digest echo", ErrChannelProtocol)
	}
	if result.Size < 0 || result.Size > maxFile {
		return ErrFileTooLarge
	}
	if result.Skipped {
		return nil
	}
	if !sha256Pattern.MatchString(result.SHA256) {
		return fmt.Errorf("%w: open digest", ErrChannelProtocol)
	}
	if result.ResumeOffset < 0 || result.ResumeOffset > result.Size {
		return fmt.Errorf("%w: open resume offset", ErrChannelProtocol)
	}
	if result.Complete && result.ResumeOffset != result.Size {
		return fmt.Errorf("%w: complete open has a partial offset", ErrChannelProtocol)
	}
	return nil
}

type channelScopeKey struct{}

type channelScope struct {
	taskID      string
	transferID  string
	executionID string
	planDigest  string
}

// WithChannelScope binds the accepted unified task, execution, transfer and plan
// digest to one transport call. All IDs must use [A-Za-z0-9_-]{1,128}; the plan
// digest is a 64-char lowercase SHA-256. Invalid input yields no usable scope.
func WithChannelScope(ctx context.Context, taskID, transferID, executionID, planDigest string) context.Context {
	scope := channelScope{taskID: taskID, transferID: transferID, executionID: executionID, planDigest: planDigest}
	if !validChannelID(taskID) || !validChannelID(transferID) || !validChannelID(executionID) ||
		!sha256Pattern.MatchString(planDigest) {
		scope = channelScope{}
	}
	return context.WithValue(ctx, channelScopeKey{}, scope)
}

func channelScopeFrom(ctx context.Context) channelScope {
	if ctx == nil {
		return channelScope{}
	}
	scope, _ := ctx.Value(channelScopeKey{}).(channelScope)
	return scope
}

func (s channelScope) valid() bool {
	return validChannelID(s.taskID) && validChannelID(s.transferID) && validChannelID(s.executionID) &&
		sha256Pattern.MatchString(s.planDigest)
}

type channelAuthorizerKey struct{}

// WithChannelAuthorizer binds a per-chunk authority check to the context.
func WithChannelAuthorizer(ctx context.Context, authorizer ChannelAuthorizer) context.Context {
	return context.WithValue(ctx, channelAuthorizerKey{}, authorizer)
}

func channelAuthorizerFrom(ctx context.Context) ChannelAuthorizer {
	if ctx == nil {
		return nil
	}
	authorizer, _ := ctx.Value(channelAuthorizerKey{}).(ChannelAuthorizer)
	return authorizer
}
