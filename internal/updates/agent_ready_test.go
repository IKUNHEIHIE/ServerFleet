//go:build linux

package updates

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadSizedRegularFileRejectsSymlinkOversizeAndDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("private current-UID readiness is not meaningful as root")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "health.json")
	if err := os.WriteFile(path, []byte(`{"pid":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	raw, err := readSizedRegularFile(path, agentHealthMaxBytes)
	if err != nil || string(raw) != `{"pid":1}` {
		t.Fatal("valid readiness report rejected", err, string(raw))
	}
	if _, err := readSizedRegularFile(path, 3); err == nil {
		t.Fatal("oversize report accepted")
	}
	link := filepath.Join(dir, "health-link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readSizedRegularFile(link, agentHealthMaxBytes); err == nil {
		t.Fatal("symlinked report accepted")
	}
	if _, err := readSizedRegularFile(dir, agentHealthMaxBytes); err == nil {
		t.Fatal("directory accepted as a readiness report")
	}
	if !healthDirectoryPrivate(dir) {
		t.Fatal("private current-UID directory rejected")
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if healthDirectoryPrivate(dir) {
		t.Fatal("group/world-accessible directory accepted")
	}
}
