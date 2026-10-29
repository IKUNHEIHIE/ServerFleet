package updates

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"
)

func stagingFixture(t *testing.T) (*ArtifactStager, []byte, []byte, []byte, ReleasePolicy) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	stager, err := NewArtifactStager(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stager.Close() })
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	body := bytes.Repeat([]byte("synthetic artifact bytes"), 4000)
	hash := sha256.Sum256(body)
	manifest := ReleaseManifest{1, "agent", "0.9.0", 2, "linux", "amd64", hex.EncodeToString(hash[:]), int64(len(body))}
	raw, _ := json.Marshal(manifest)
	sig := ed25519.Sign(private, append([]byte("serverfleet.release.v1\x00"), raw...))
	return stager, raw, sig, body, ReleasePolicy{"agent", "amd64", 1, 1 << 20, map[string]ed25519.PublicKey{"test-key": public}}
}
func TestStagingRealFilesRecheckOneShotAndCleanup(t *testing.T) {
	stager, raw, sig, body, policy := stagingFixture(t)
	ctx := context.Background()
	staged, err := stager.Stage(ctx, raw, sig, "test-key", bytes.NewReader(body), policy)
	if err != nil {
		t.Fatal(err)
	}
	metadata := staged.Manifest()
	if metadata.Version != "0.9.0" {
		t.Fatal(metadata)
	}
	calls := 0
	gate := func(context.Context, ReleaseManifest) error { return nil }
	if err = staged.WithVerified(ctx, policy, gate, func(_ context.Context, m ReleaseManifest, r io.Reader) error {
		calls++
		read, err := io.ReadAll(r)
		if err != nil || !bytes.Equal(read, body) || m != metadata {
			t.Error("wrong artifact loan", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err = staged.WithVerified(ctx, policy, gate, func(context.Context, ReleaseManifest, io.Reader) error { calls++; return nil }); err != ErrArtifactConsumed || calls != 1 {
		t.Fatal("artifact executed twice", err, calls)
	}
	name := staged.directory
	if err = staged.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = stager.root.Stat(name); !os.IsNotExist(err) {
		t.Fatal("private staging not cleaned", err)
	}
	if err = staged.WithVerified(ctx, policy, gate, func(context.Context, ReleaseManifest, io.Reader) error { t.Fatal("closed artifact loan"); return nil }); err != ErrArtifactRejected {
		t.Fatal(err)
	}
}
func TestStagingRejectsTamperingPartialOrNewInstalledSequence(t *testing.T) {
	stager, raw, sig, body, policy := stagingFixture(t)
	ctx := context.Background()
	for _, bad := range [][]byte{body[:len(body)-1], append(bytes.Clone(body), byte('x'))} {
		if staged, err := stager.Stage(ctx, raw, sig, "test-key", bytes.NewReader(bad), policy); err != ErrArtifactRejected || staged != nil {
			t.Fatal("incomplete download accepted", err)
		}
	}
	staged, err := stager.Stage(ctx, raw, sig, "test-key", bytes.NewReader(body), policy)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Close()
	newer := policy
	newer.MinimumSequence = 2
	if err = staged.WithVerified(ctx, newer, func(context.Context, ReleaseManifest) error { return nil }, func(context.Context, ReleaseManifest, io.Reader) error { t.Fatal("rollback sequence used"); return nil }); err != ErrArtifactRejected {
		t.Fatal(err)
	}
	if _, err = staged.file.WriteAt([]byte("tampered"), 0); err != nil {
		t.Fatal(err)
	}
	if err = staged.WithVerified(ctx, policy, func(context.Context, ReleaseManifest) error { return nil }, func(context.Context, ReleaseManifest, io.Reader) error { t.Fatal("tampered artifact used"); return nil }); err != ErrArtifactRejected {
		t.Fatal(err)
	}
}
func TestStagingUnknownUseNeverAutomaticallyRetried(t *testing.T) {
	stager, raw, sig, body, policy := stagingFixture(t)
	ctx := context.Background()
	staged, err := stager.Stage(ctx, raw, sig, "test-key", bytes.NewReader(body), policy)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Close()
	if err = staged.WithVerified(ctx, policy, nil, func(context.Context, ReleaseManifest, io.Reader) error { return nil }); err != ErrArtifactRejected {
		t.Fatal("nil task gate", err)
	}
	if err = staged.WithVerified(ctx, policy, func(context.Context, ReleaseManifest) error { return nil }, func(context.Context, ReleaseManifest, io.Reader) error {
		return errors.New("failed after possible install")
	}); err != ErrApplyUnknown {
		t.Fatal(err)
	}
	if err = staged.WithVerified(ctx, policy, func(context.Context, ReleaseManifest) error { return nil }, func(context.Context, ReleaseManifest, io.Reader) error { t.Fatal("unknown update retried"); return nil }); err != ErrArtifactConsumed {
		t.Fatal(err)
	}
}
