package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootCommandPrivateMaterialRejectsReplaceableAncestor(t *testing.T) {
	parent := t.TempDir()
	if err := os.Chmod(parent, 0777); err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(parent, "private")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(private, "policy.json")
	if err := os.WriteFile(filename, []byte("private-material"), 0600); err != nil {
		t.Fatal(err)
	}
	// Neither this test nor the reader invokes a command or changes identity.
	// Under UID 0 the leaf is genuinely root-owned; the ancestor must still fail.
	if raw, err := readCommandPrivateFile(filename, 1024, true); err == nil {
		clear(raw)
		t.Fatal("root material accepted through a replaceable ancestor")
	}
	if rootCommandAncestors(private) {
		t.Fatal("replaceable ancestor accepted for root command paths")
	}
	// Ordinary private configuration retains its current-user ownership rule.
	if raw, err := readCommandPrivateFile(filename, 1024, false); err != nil || string(raw) != "private-material" {
		t.Fatal("ordinary private material unexpectedly rejected", err)
	}
}

func TestRootCommandAncestorsRejectSymlinkComponent(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "root-link")
	if err := os.Symlink("/", link); err != nil {
		t.Fatal(err)
	}
	if rootCommandAncestors(link) {
		t.Fatal("root command path accepted a symlink component")
	}
}

func TestRootCommandAncestorsRequireCanonicalAbsolutePath(t *testing.T) {
	for _, path := range []string{"/./", "/root/../", "relative"} {
		if rootCommandAncestors(path) {
			t.Fatalf("root command accepted noncanonical path %q", path)
		}
	}
	if !rootCommandAncestors("/") {
		t.Fatal("root-owned immutable filesystem root rejected")
	}
}
