package files

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ChannelProtocolVersion identifies the DS-owned file frame contract. The
// controller adapter maps these frames onto the generated Protobuf messages;
// this package never imports transport or the generated agent API.
const ChannelProtocolVersion = 1

const (
	DefaultChannelChunkBytes = 16 << 10
	MaxChannelChunkBytes     = 32 << 10
	DefaultChannelFileBytes  = 2 << 30
	DefaultChannelAttempts   = 3
)

// ChannelFrameType is one bounded file-channel message kind.
type ChannelFrameType string

const (
	FrameOpen        ChannelFrameType = "open"
	FrameOpenAck     ChannelFrameType = "open_ack"
	FrameChunk       ChannelFrameType = "chunk"
	FrameAck         ChannelFrameType = "ack"
	FrameComplete    ChannelFrameType = "complete"
	FrameCompleteAck ChannelFrameType = "complete_ack"
	FrameCancel      ChannelFrameType = "cancel"
	FrameError       ChannelFrameType = "error"
)

func (t ChannelFrameType) valid() bool {
	switch t {
	case FrameOpen, FrameOpenAck, FrameChunk, FrameAck, FrameComplete, FrameCompleteAck, FrameCancel, FrameError:
		return true
	default:
		return false
	}
}

// ChannelDirection distinguishes an upload into the node from a download out of
// it. A session is bound to exactly one direction.
type ChannelDirection string

const (
	ChannelUpload   ChannelDirection = "upload"
	ChannelDownload ChannelDirection = "download"
)

func (d ChannelDirection) valid() bool {
	return d == ChannelUpload || d == ChannelDownload
}

// ChannelFrame is the bounded, transport-neutral file frame. Data is only set on
// chunk frames and must never exceed the negotiated chunk ceiling.
type ChannelFrame struct {
	Version        int
	Type           ChannelFrameType
	Direction      ChannelDirection
	TransferID     string
	ExecutionID    string
	Path           string
	Offset         int64
	Size           int64
	SHA256         string
	Data           []byte
	ConflictPolicy ConflictPolicy
	CreateParents  bool
	ResumeOffset   int64
	ResolvedPath   string
	Skipped        bool
	ErrorCode      string
}

// FileChannelSession is one duplex, correlated file session with an agent. The
// controller adapter must bind it to the mTLS-authenticated node connection; the
// node id in a frame is never an independent authorization proof.
type FileChannelSession interface {
	Send(context.Context, ChannelFrame) error
	Receive(context.Context) (ChannelFrame, error)
	Close() error
}

// AgentFileChannel opens a session for one transfer. Implementations are owned by
// the controller composition and backed by the active WSS+mTLS connection.
type AgentFileChannel interface {
	OpenFileChannel(ctx context.Context, nodeID, transferID, executionID string) (FileChannelSession, error)
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
	ErrResumeUnsupported      = errors.New("files: resume requires a seekable source")
	ErrFileTooLarge           = errors.New("files: file exceeds the channel size limit")
	ErrFileSizeMismatch       = errors.New("files: transferred size does not match the declared size")
	ErrTransferExecution      = errors.New("files: transfer execution is not allowed")
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
	switch code {
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
	default:
		return fmt.Errorf("%w: %s", ErrAgentIO, boundedCode(code))
	}
}

func boundedCode(code string) string {
	if code == "" {
		return "unknown"
	}
	if len(code) > 64 {
		return code[:64]
	}
	for _, r := range code {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '.' || r == '-' {
			continue
		}
		return "invalid"
	}
	return code
}

func validateChannelFrame(frame ChannelFrame, maxChunk int, maxFile int64) error {
	if frame.Version != ChannelProtocolVersion {
		return ErrChannelProtocol
	}
	if !frame.Type.valid() || !frame.Direction.valid() {
		return ErrChannelProtocol
	}
	if !validChannelToken(frame.TransferID, 128) || !validChannelToken(frame.ExecutionID, 160) {
		return ErrChannelProtocol
	}
	if frame.Type == FrameOpen {
		if err := ValidateRemotePath(frame.Path); err != nil {
			return ErrAgentPathRejected
		}
		if frame.Path == "/" {
			return ErrAgentPathRejected
		}
		if frame.Size < 0 || frame.Size > maxFile || frame.Offset < 0 {
			return ErrChannelProtocol
		}
		if frame.Direction == ChannelUpload {
			if frame.Offset > frame.Size || !sha256Pattern.MatchString(frame.SHA256) || !frame.ConflictPolicy.Valid() {
				return ErrChannelProtocol
			}
		}
	}
	if frame.Type == FrameChunk {
		if len(frame.Data) == 0 || len(frame.Data) > maxChunk {
			return ErrChannelProtocol
		}
		if frame.Offset < 0 || frame.Offset+int64(len(frame.Data)) > maxFile {
			return ErrChannelProtocol
		}
	}
	if frame.Type == FrameError {
		if frame.ErrorCode == "" || len(frame.ErrorCode) > 64 {
			return ErrChannelProtocol
		}
	}
	return nil
}

func validChannelToken(value string, max int) bool {
	if value == "" || len(value) > max || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if r <= ' ' || r == 0x7f || r == '/' || r == '\\' {
			return false
		}
	}
	return true
}

type channelIdentityKey struct{}

type channelIdentity struct {
	transferID  string
	executionID string
}

// WithChannelIdentity binds a stable transfer and execution id to one transport
// call so retries after a reconnect reuse the agent's durable dedupe key.
func WithChannelIdentity(ctx context.Context, transferID, executionID string) context.Context {
	return context.WithValue(ctx, channelIdentityKey{}, channelIdentity{transferID: transferID, executionID: executionID})
}

func channelIdentityFrom(ctx context.Context) channelIdentity {
	if ctx == nil {
		return channelIdentity{}
	}
	identity, _ := ctx.Value(channelIdentityKey{}).(channelIdentity)
	return identity
}
