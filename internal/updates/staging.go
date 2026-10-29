package updates

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

var ErrArtifactConsumed = errors.New("updates: staged artifact already admitted")
var ErrApplyUnknown = errors.New("updates: apply outcome unknown; do not automatically retry")

type ArtifactStager struct{ root *os.Root }
type StagedArtifact struct {
	mu               sync.Mutex
	root             *os.Root
	file             *os.File
	directory        string
	raw, signature   []byte
	keyID            string
	manifest         ReleaseManifest
	consumed, closed bool
}

func (s *StagedArtifact) String() string   { return "updates.StagedArtifact{private-staging}" }
func (s *StagedArtifact) GoString() string { return s.String() }
func NewArtifactStager(directory string) (*ArtifactStager, error) {
	if !filepath.IsAbs(directory) {
		return nil, ErrArtifactRejected
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, ErrArtifactRejected
	}
	info, err := root.Stat(".")
	if err != nil || !info.IsDir() || runtime.GOOS == "linux" && info.Mode().Perm()&0077 != 0 {
		_ = root.Close()
		return nil, ErrArtifactRejected
	}
	return &ArtifactStager{root}, nil
}

// Close after all borrowed StagedArtifacts have been closed. No recursive delete
// is performed and no artifact is executable merely because staging succeeded.
func (s *ArtifactStager) Close() error {
	if s == nil || s.root == nil {
		return nil
	}
	return s.root.Close()
}
func cloneReleasePolicy(policy ReleasePolicy) ReleasePolicy {
	keys := map[string]ed25519.PublicKey{}
	for id, key := range policy.TrustedKeys {
		keys[id] = bytes.Clone(key)
	}
	policy.TrustedKeys = keys
	return policy
}
func syncStagingDirectory(root *os.Root, name string) error {
	if runtime.GOOS != "linux" {
		return nil
	} // Windows tests do not prove Linux crash durability.
	directory, err := root.Open(name)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
func (s *ArtifactStager) Stage(ctx context.Context, raw, signature []byte, keyID string, source io.Reader, policy ReleasePolicy) (*StagedArtifact, error) {
	if s == nil || s.root == nil || ctx == nil || ctx.Err() != nil || source == nil || len(raw) == 0 || len(raw) > 16<<10 || len(signature) != ed25519.SignatureSize || keyID == "" || len(keyID) > 128 {
		return nil, ErrArtifactRejected
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, ErrArtifactRejected
	}
	name := hex.EncodeToString(random[:])
	if s.root.Mkdir(name, 0700) != nil {
		return nil, ErrArtifactRejected
	}
	file, err := s.root.OpenFile(name+"/artifact", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		_ = s.root.Remove(name)
		return nil, ErrArtifactRejected
	}
	staged := &StagedArtifact{root: s.root, file: file, directory: name, raw: bytes.Clone(raw), signature: bytes.Clone(signature), keyID: keyID}
	accepted := false
	defer func() {
		if !accepted {
			_ = staged.Close()
		}
	}()
	policy = cloneReleasePolicy(policy)
	manifest, err := VerifyRelease(ctx, staged.raw, staged.signature, keyID, io.TeeReader(source, file), policy)
	if err != nil || file.Sync() != nil {
		return nil, ErrArtifactRejected
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return nil, ErrArtifactRejected
	}
	// Re-read the SAME private file descriptor, not a mutable external pathname.
	verified, err := VerifyRelease(ctx, staged.raw, staged.signature, keyID, file, policy)
	if err != nil || verified != manifest {
		return nil, ErrArtifactRejected
	}
	if syncStagingDirectory(s.root, name) != nil || syncStagingDirectory(s.root, ".") != nil {
		return nil, ErrArtifactRejected
	}
	staged.manifest = manifest
	accepted = true
	return staged, nil
}
func (s *StagedArtifact) Manifest() ReleaseManifest {
	if s == nil {
		return ReleaseManifest{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.file == nil || s.root == nil {
		return ReleaseManifest{}
	}
	return s.manifest
}

type artifactReader struct{ reader io.Reader }

func (r artifactReader) Read(b []byte) (int, error) { return r.reader.Read(b) }

// WithVerified is a one-shot loan to a program-registered sole-task installer.
// It exposes no external file path or writer. Gate must bind current task,
// approval, component and sequence; policy must come from current installed state.
// Callback must honor ctx and cannot call Close/WithVerified on the same handle.
func (s *StagedArtifact) WithVerified(ctx context.Context, policy ReleasePolicy, gate func(context.Context, ReleaseManifest) error, consume func(context.Context, ReleaseManifest, io.Reader) error) (err error) {
	if s == nil || ctx == nil || ctx.Err() != nil || gate == nil || consume == nil {
		return ErrArtifactRejected
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.file == nil || s.root == nil {
		return ErrArtifactRejected
	}
	if s.consumed {
		return ErrArtifactConsumed
	}
	policy = cloneReleasePolicy(policy)
	if _, err = s.file.Seek(0, io.SeekStart); err != nil {
		return ErrArtifactRejected
	}
	manifest, err := VerifyRelease(ctx, s.raw, s.signature, s.keyID, s.file, policy)
	if err != nil || manifest != s.manifest || gate(ctx, manifest) != nil || ctx.Err() != nil {
		return ErrArtifactRejected
	}
	if _, err = s.file.Seek(0, io.SeekStart); err != nil {
		return ErrArtifactRejected
	}
	s.consumed = true
	defer func() {
		if recover() != nil {
			err = ErrApplyUnknown
		}
	}()
	limited := &io.LimitedReader{R: contextReader{ctx, s.file}, N: manifest.Bytes}
	if consume(ctx, manifest, artifactReader{limited}) != nil || limited.N != 0 || ctx.Err() != nil {
		return ErrApplyUnknown
	}
	if _, err = s.file.Seek(0, io.SeekStart); err != nil {
		return ErrApplyUnknown
	}
	if _, err = VerifyRelease(ctx, s.raw, s.signature, s.keyID, s.file, policy); err != nil || gate(ctx, manifest) != nil || ctx.Err() != nil {
		return ErrApplyUnknown
	}
	return nil
}
func (s *StagedArtifact) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.file == nil || s.root == nil {
		return nil
	}
	fileErr := s.file.Close()
	removeErr := s.root.Remove(s.directory + "/artifact")
	directoryErr := s.root.Remove(s.directory)
	clear(s.raw)
	clear(s.signature)
	if fileErr != nil || removeErr != nil || directoryErr != nil {
		return ErrArtifactRejected
	}
	return nil
}
