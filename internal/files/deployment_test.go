package files

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDeploymentRegistryRejectsAmbiguousAndUntrustedConfig(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	spool := filepath.Join(dir, "spool")
	valid, _ := json.Marshal(rootRegistry{Version: 1, SpoolDir: spool, NodeRoots: []rootRegistration{{"node-source", "/source"}, {"node-target", "/target"}}})
	mapper, got, err := parseDeployment(valid)
	if err != nil || got != spool {
		t.Fatal(err)
	}
	if value, e := mapper.RelativePath(context.Background(), "node-target", "/target/out/a"); e != nil || value != "out/a" {
		t.Fatal(value, e)
	}
	for _, raw := range []string{`{}`, `{"version":1,"version":1}`, `{"version":1} {}`, `[]`, string(valid[:len(valid)-1]) + `,"httpPath":"/"}`, string(valid[:len(valid)-1]) + `,"nodeRoots":[]}`} {
		if _, _, err := parseDeployment([]byte(raw)); err == nil {
			t.Fatal("accepted ambiguous/unknown config", raw)
		}
	}
	cfg := rootRegistry{Version: 1, SpoolDir: spool, NodeRoots: []rootRegistration{{"node-source", "/source"}, {"node-source", "/other"}}}
	raw, _ := json.Marshal(cfg)
	if _, _, err := parseDeployment(raw); err == nil {
		t.Fatal("duplicate node registration")
	}
	name := filepath.Join(dir, "files.json")
	os.WriteFile(name, valid, 0600)
	_, _, err = LoadDeployment(name)
	if runtime.GOOS != "linux" {
		if err == nil {
			t.Fatal("platform lacks ACL proof")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if os.Chmod(name, 0644) != nil {
		t.Fatal("chmod")
	}
	if _, _, err := LoadDeployment(name); err == nil {
		t.Fatal("world-readable config")
	}
	os.Chmod(name, 0600)
	link := filepath.Join(dir, "link.json")
	if os.Symlink(name, link) != nil {
		t.Fatal("symlink")
	}
	if _, _, err := LoadDeployment(link); err == nil {
		t.Fatal("config symlink")
	}
	if os.Chmod(dir, 0755) != nil {
		t.Fatal("chmod")
	}
	if _, _, err := LoadDeployment(name); err == nil {
		t.Fatal("shared parent")
	}
	os.Chmod(dir, 0700)
	os.Chmod(spool, 0755)
	if _, _, err := LoadDeployment(name); err == nil {
		t.Fatal("shared spool")
	}
}
