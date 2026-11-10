// Package updates verifies artifacts before the controlled update task pipeline.
package updates

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
)

var ErrArtifactRejected = errors.New("updates: artifact rejected")
var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// ReleaseManifest is a proposed signed metadata format; key governance, actual
// installer/health/migration/recovery policy require the missing UPDATE source.
type ReleaseManifest struct {
	Format       int    `json:"format"`
	Component    string `json:"component"`
	Version      string `json:"version"`
	Sequence     uint64 `json:"sequence"`
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	SHA256       string `json:"sha256"`
	Bytes        int64  `json:"bytes"`
}
type ReleasePolicy struct {
	Component       string
	Architecture    string
	MinimumSequence uint64
	MaxBytes        int64
	TrustedKeys     map[string]ed25519.PublicKey
}
type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

// VerifyRelease streams a bounded artifact. Success is verification only and
// must not be projected as installed/healthy. Installer must retain the same
// private staging object and recheck before use to prevent path-swap races.
func VerifyRelease(ctx context.Context, raw, signature []byte, keyID string, artifact io.Reader, policy ReleasePolicy) (ReleaseManifest, error) {
	if artifact == nil {
		return ReleaseManifest{}, ErrArtifactRejected
	}
	m, err := VerifyManifest(ctx, raw, signature, keyID, policy)
	if err != nil {
		return ReleaseManifest{}, err
	}

	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(contextReader{ctx, artifact}, m.Bytes+1))
	if err != nil || ctx.Err() != nil || n != m.Bytes || hex.EncodeToString(h.Sum(nil)) != m.SHA256 {
		return ReleaseManifest{}, ErrArtifactRejected
	}
	return m, nil
}

// VerifyManifest authenticates bounded canonical metadata before fetching bytes.
func VerifyManifest(ctx context.Context, raw, signature []byte, keyID string, policy ReleasePolicy) (ReleaseManifest, error) {
	var m ReleaseManifest
	if ctx == nil || ctx.Err() != nil || len(raw) == 0 || len(raw) > 16<<10 || len(signature) != ed25519.SignatureSize || policy.MaxBytes <= 0 || policy.MaxBytes > 256<<20 {
		return m, ErrArtifactRejected
	}
	key := policy.TrustedKeys[keyID]
	if len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, append([]byte("serverfleet.release.v1\x00"), raw...), signature) {
		return m, ErrArtifactRejected
	}
	if json.Unmarshal(raw, &m) != nil {
		return ReleaseManifest{}, ErrArtifactRejected
	}
	canonical, _ := json.Marshal(m)
	if !bytes.Equal(canonical, raw) || m.Format != 1 || (m.Component != "controller" && m.Component != "agent") || m.Component != policy.Component || !versionPattern.MatchString(m.Version) || m.Sequence <= policy.MinimumSequence || m.OS != "linux" || (m.Architecture != "amd64" && m.Architecture != "arm64") || m.Architecture != policy.Architecture || m.Bytes <= 0 || m.Bytes > policy.MaxBytes {
		return ReleaseManifest{}, ErrArtifactRejected
	}
	return m, nil
}
