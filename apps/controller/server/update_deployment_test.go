package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"serverfleet.local/serverfleet/internal/updates"
)

func updateDocumentForTest(t *testing.T) []byte {
	t.Helper()
	d := updateDeploymentDocument{Version: 1, Directory: "/private/agent", Inbox: "/private/inbox", StageDir: "/private/stage", KeyID: "release-1", NodeID: "node-1", IdentityDir: "/private/identity", ControllerOrigin: "https://controller.example.invalid:8443", RootCertificate: "/private/root.pem", PublicKeys: map[string]string{"release-1": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))}, CandidateVersion: "0.9.2", Initial: updates.ReleaseManifest{Format: 1, Component: "agent", Version: "0.9.1", Sequence: 1, OS: "linux", Architecture: runtime.GOARCH, SHA256: strings.Repeat("a", 64), Bytes: 100}, MaxBytes: 256 << 20, SchemaFingerprint: strings.Repeat("b", 64)}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestUpdateStartupDocumentBindsOnlyTrustedAgentConfiguration(t *testing.T) {
	raw := updateDocumentForTest(t)
	got, err := parseUpdateDeployment(raw)
	if err != nil || got.Policy.Component != "agent" || got.NodeID != "node-1" || len(got.Policy.TrustedKeys["release-1"]) != 32 || got.Current != nil || got.DB != nil || got.Audit != nil {
		t.Fatalf("invalid startup binding: %+v %v", got, err)
	}
	for _, bad := range [][]byte{
		append(append([]byte{}, raw...), []byte(` {}`)...), append(append([]byte{}, raw...), []byte(` garbage`)...),
		bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1),
		bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"signingSecret":"forbidden"`), 1),
		bytes.Replace(raw, []byte(`"component":"agent"`), []byte(`"component":"controller"`), 1),
		bytes.Replace(raw, []byte(`https://controller.example.invalid:8443`), []byte(`http://controller.example.invalid:8443`), 1),
		bytes.Replace(raw, []byte(`/private/stage`), []byte(`/private/../stage`), 1),
		bytes.Replace(raw, []byte(`"keyId":"release-1"`), []byte(`"keyId":"untrusted"`), 1),
		bytes.Replace(raw, []byte(`"maxBytes":268435456`), []byte(`"maxBytes":268435457`), 1),
	} {
		if _, err := parseUpdateDeployment(bad); !errors.Is(err, ErrUpdateDeployment) {
			t.Fatalf("unsafe startup accepted: %s %v", bad, err)
		}
	}
}
func TestUpdateStartupFileRejectsNonPrivateAndSymlink(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("owner-checked Linux startup configuration")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, "update.json")
	if err := os.WriteFile(name, updateDocumentForTest(t), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadUpdateDeployment(name); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(name, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadUpdateDeployment(name); !errors.Is(err, ErrUpdateDeployment) {
		t.Fatal("non-private startup accepted", err)
	}
	if err := os.Chmod(name, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(name, link); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadUpdateDeployment(link); !errors.Is(err, ErrUpdateDeployment) {
		t.Fatal("symlink startup accepted", err)
	}
	if _, err := LoadUpdateDeployment("relative.json"); !errors.Is(err, ErrUpdateDeployment) {
		t.Fatal("relative startup accepted", err)
	}
}
