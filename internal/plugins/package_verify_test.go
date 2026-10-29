package plugins

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestPackageSignatureSourceUIAndDisable(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("test-only-package-not-wasm")
	hash := sha256.Sum256(payload)
	manifest := PackageManifest{1, "xray-manager", "1.0.0", "https://example.test/approved-source", hex.EncodeToString(hash[:]), len(payload), []string{"xray.read"}, []UIElement{{"view", "action", "Read", "xray.read"}}}
	trust := PackageTrust{map[string]ed25519.PublicKey{"test-key": public}, map[string]bool{manifest.Source: true}, map[string]bool{"xray.read": true}}
	verify := func(m PackageManifest) error {
		raw, _ := json.Marshal(m)
		signature := ed25519.Sign(private, append([]byte("serverfleet.plugin.v1\x00"), raw...))
		_, err := VerifyPackage(raw, signature, payload, "test-key", trust)
		return err
	}
	if err = verify(manifest); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*PackageManifest){func(m *PackageManifest) { m.Source = "https://untrusted.test/fork" }, func(m *PackageManifest) { m.ID = "removed-demo-plugin" }, func(m *PackageManifest) { m.Version = "latest" }, func(m *PackageManifest) { m.PayloadSHA256 = "bad" }, func(m *PackageManifest) { m.Capabilities = []string{"shell.root"} }, func(m *PackageManifest) { m.UI = []UIElement{{"script", "script", "Run", ""}} }, func(m *PackageManifest) { m.UI = []UIElement{{"button", "action", "Run", "shell.root"}} }} {
		m := manifest
		change(&m)
		if err = verify(m); err == nil {
			t.Fatal("unsafe signed manifest accepted")
		}
	}
	raw, _ := json.Marshal(manifest)
	signature := ed25519.Sign(private, append([]byte("serverfleet.plugin.v1\x00"), raw...))
	payload[0] ^= 1
	if _, err = VerifyPackage(raw, signature, payload, "test-key", trust); err == nil {
		t.Fatal("tampered payload")
	}
	payload[0] ^= 1
	signature[0] ^= 1
	if _, err = VerifyPackage(raw, signature, payload, "test-key", trust); err == nil {
		t.Fatal("tampered signature")
	}
	if err = CheckInvocation(true, "1.0.0", 2, 2, manifest, "xray.read"); err != nil {
		t.Fatal(err)
	}
	if err = CheckInvocation(false, "1.0.0", 2, 2, manifest, "xray.read"); err == nil {
		t.Fatal("disabled plugin accepted")
	}
	if err = CheckInvocation(true, "1.0.0", 3, 2, manifest, "xray.read"); err == nil {
		t.Fatal("old lifecycle revision accepted")
	}
}
