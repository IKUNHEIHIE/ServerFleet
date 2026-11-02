package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"

	"serverfleet.local/serverfleet/internal/files"
)

var (
	// ErrGatewayArtifactTransport means no trusted file transport was supplied.
	ErrGatewayArtifactTransport = errors.New("gateway: artifact transport is unavailable")
	// ErrGatewayArtifactInvalid means the planned bytes/path/digest disagree.
	ErrGatewayArtifactInvalid = errors.New("gateway: artifact content is invalid")
)

// gatewayArtifactMaxBytes matches the Agent capability staging ceiling; a larger
// artifact is rejected before any transport call.
const gatewayArtifactMaxBytes = 64 << 10

// gatewayFileArtifactUploader streams trusted program-side bytes over the
// existing unified file transport. It never opens SSH, a second queue or a
// general secret channel. Task/approval/target-revision gating stays in
// gatewayTaskDispatcher, which rechecks before and after this call.
type gatewayFileArtifactUploader struct {
	transport files.UploadTransport
}

var _ GatewayArtifactUploader = (*gatewayFileArtifactUploader)(nil)

// NewGatewayArtifactUploader is the controller composition seam for the real
// gateway artifact uploader. A nil transport keeps the capability closed.
func NewGatewayArtifactUploader(transport files.UploadTransport) (GatewayArtifactUploader, error) {
	if transport == nil {
		return nil, ErrGatewayArtifactTransport
	}
	return &gatewayFileArtifactUploader{transport: transport}, nil
}

// Upload validates the exact planned content before handing it to the transport.
// The digest is recomputed here rather than trusted from the caller.
func (u *gatewayFileArtifactUploader) Upload(ctx context.Context, taskID, nodeID, absolutePath string, content []byte, sha256Hex string) error {
	if u == nil || u.transport == nil || ctx == nil {
		return ErrGatewayArtifactTransport
	}
	if taskID == "" || nodeID == "" || len(content) == 0 || len(content) > gatewayArtifactMaxBytes {
		return ErrGatewayArtifactInvalid
	}
	if !filepath.IsAbs(absolutePath) || filepath.Clean(absolutePath) != absolutePath ||
		strings.ContainsRune(absolutePath, '\x00') || strings.ContainsRune(absolutePath, '\\') {
		return ErrGatewayArtifactInvalid
	}
	if !validGatewayDigest(sha256Hex) {
		return ErrGatewayArtifactInvalid
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != sha256Hex {
		return ErrGatewayArtifactInvalid
	}
	if err := u.transport.PushFile(ctx, nodeID, absolutePath, bytes.NewReader(content), int64(len(content)), sha256Hex); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return errors.Join(ErrGatewayArtifactTransport, err)
	}
	return nil
}
