package files

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// A "/" registration means the console path space IS the Agent file root, so
// browsing, upload and transfer paths agree and the Agent root itself is
// addressable as "/".
func TestRootPathMapperSlashRootIsAgentRoot(t *testing.T) {
	mapper, err := NewRootPathMapper(map[string]string{"node-a": "/"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, tc := range []struct{ absolute, want string }{
		{"/", ""},
		{"/a.txt", "a.txt"},
		{"/dir/sub/b.bin", "dir/sub/b.bin"},
	} {
		got, e := mapper.RelativePath(ctx, "node-a", tc.absolute)
		if e != nil || got != tc.want {
			t.Fatalf("RelativePath(%q) = %q, %v; want %q", tc.absolute, got, e, tc.want)
		}
	}
	for _, tc := range []struct{ relative, want string }{
		{"a.txt", "/a.txt"},
		{"dir/sub/b.bin", "/dir/sub/b.bin"},
	} {
		got, e := mapper.AbsolutePath(ctx, "node-a", tc.relative)
		if e != nil || got != tc.want {
			t.Fatalf("AbsolutePath(%q) = %q, %v; want %q", tc.relative, got, e, tc.want)
		}
	}
	if _, e := mapper.RelativePath(ctx, "node-unregistered", "/a.txt"); e == nil {
		t.Fatal("unregistered node must stay refused")
	}
	for _, bad := range []string{"/../etc/passwd", "/a/../../b", "//x", "/.sf-x", "relative"} {
		if _, e := mapper.RelativePath(ctx, "node-a", bad); e == nil {
			t.Fatalf("escape or invalid path %q must be refused", bad)
		}
	}
}

// A prefix root keeps its documented behaviour: only paths under the prefix are
// addressable, and the Agent root itself is not.
func TestRootPathMapperPrefixRootUnchanged(t *testing.T) {
	mapper, err := NewRootPathMapper(map[string]string{"node-a": "/files"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if got, e := mapper.RelativePath(ctx, "node-a", "/files/a.txt"); e != nil || got != "a.txt" {
		t.Fatalf("prefix root: got %q, %v", got, e)
	}
	if _, e := mapper.RelativePath(ctx, "node-a", "/a.txt"); e == nil {
		t.Fatal("path outside the prefix root must stay refused")
	}
	if _, e := mapper.RelativePath(ctx, "node-a", "/files"); e == nil {
		t.Fatal("the prefix root itself is not a file path")
	}
	if got, e := mapper.AbsolutePath(ctx, "node-a", "a.txt"); e != nil || got != "/files/a.txt" {
		t.Fatalf("prefix root absolute: got %q, %v", got, e)
	}
	if _, e := mapper.AbsolutePath(ctx, "node-unregistered", "a.txt"); e == nil {
		t.Fatal("unregistered node must stay refused")
	}
}

// The deployment document is the only way a node gains a mapping; a "/"
// registration must load and behave as the Agent root.
func TestParseDeploymentAcceptsSlashRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	spool := filepath.Join(dir, "spool")
	raw, _ := json.Marshal(rootRegistry{Version: 1, SpoolDir: spool, NodeRoots: []rootRegistration{{"node-a", "/"}}})
	mapper, got, err := parseDeployment(raw)
	if err != nil || got != spool {
		t.Fatal(err)
	}
	value, e := mapper.RelativePath(context.Background(), "node-a", "/dir/a.bin")
	if e != nil || value != "dir/a.bin" {
		t.Fatalf("slash root mapping: %q, %v", value, e)
	}
}
