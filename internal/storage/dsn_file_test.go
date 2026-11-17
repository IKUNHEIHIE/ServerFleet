package storage_test

import (
	"os"
	"path/filepath"
	"serverfleet.local/serverfleet/internal/storage"
	"testing"
)

func TestReadDSNFileRejectsPublicFilesAndSymlinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dsn")
	if err := os.WriteFile(path, []byte("host=localhost dbname=isolated"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.ReadDSNFile(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.ReadDSNFile(path); err == nil {
		t.Fatal("public DSN accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	link := path + ".link"
	if err := os.Symlink(path, link); err != nil {
		t.Skip(err)
	}
	if _, err := storage.ReadDSNFile(link); err == nil {
		t.Fatal("symlink DSN accepted")
	}
}
