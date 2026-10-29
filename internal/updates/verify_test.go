package updates

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestReleaseSignaturePlatformDigestRollbackAndCancel(t *testing.T) {
	public, private, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	artifact := []byte("test-only-not-executable")
	hash := sha256.Sum256(artifact)
	m := ReleaseManifest{1, "agent", "0.9.0", 9, "linux", "amd64", hex.EncodeToString(hash[:]), int64(len(artifact))}
	policy := ReleasePolicy{"agent", "amd64", 8, 1 << 20, map[string]ed25519.PublicKey{"test-key": public}}
	verify := func(m ReleaseManifest, body []byte, ctx context.Context) error {
		raw, _ := json.Marshal(m)
		signature := ed25519.Sign(private, append([]byte("serverfleet.release.v1\x00"), raw...))
		_, err := VerifyRelease(ctx, raw, signature, "test-key", bytes.NewReader(body), policy)
		return err
	}
	if e = verify(m, artifact, context.Background()); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*ReleaseManifest){func(m *ReleaseManifest) { m.Sequence = 8 }, func(m *ReleaseManifest) { m.Architecture = "arm64" }, func(m *ReleaseManifest) { m.Component = "plugin" }, func(m *ReleaseManifest) { m.Version = "latest" }, func(m *ReleaseManifest) { m.SHA256 = "wrong" }} {
		bad := m
		change(&bad)
		if e = verify(bad, artifact, context.Background()); e == nil {
			t.Fatal("unsafe release accepted")
		}
	}
	if e = verify(m, append(bytes.Clone(artifact), 'x'), context.Background()); e == nil {
		t.Fatal("trailing bytes accepted")
	}
	if e = verify(m, artifact[:len(artifact)-1], context.Background()); e == nil {
		t.Fatal("partial download accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e = verify(m, artifact, ctx); e == nil {
		t.Fatal("canceled verification accepted")
	}
	raw, _ := json.Marshal(m)
	sig := ed25519.Sign(private, append([]byte("serverfleet.release.v1\x00"), raw...))
	sig[0] ^= 1
	if _, e = VerifyRelease(context.Background(), raw, sig, "test-key", bytes.NewReader(artifact), policy); e == nil {
		t.Fatal("bad signature accepted")
	}
}
