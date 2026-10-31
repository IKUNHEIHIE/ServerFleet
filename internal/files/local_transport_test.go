package files_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"serverfleet.local/serverfleet/internal/files"
)

func localDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func newLocalTransport(t *testing.T) (*files.LocalStagingTransport, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "node-staging")
	transport, err := files.NewLocalStagingTransport(root)
	if err != nil {
		t.Fatalf("NewLocalStagingTransport: %v", err)
	}
	return transport, root
}

func TestLocalStagingPushPullRoundTrip(t *testing.T) {
	transport, root := newLocalTransport(t)
	ctx := context.Background()
	content := []byte("server-fleet staged content\n")
	remotePath := "/srv/app/config.yaml"

	if err := transport.PushFile(ctx, "node-1", remotePath, bytes.NewReader(content), int64(len(content)), localDigest(content)); err != nil {
		t.Fatalf("push: %v", err)
	}
	onDisk, err := os.ReadFile(filepath.Join(root, "node-1", "srv", "app", "config.yaml"))
	if err != nil {
		t.Fatalf("read staged file: %v", err)
	}
	if !bytes.Equal(onDisk, content) {
		t.Fatalf("staged content = %q", onDisk)
	}

	var pulled bytes.Buffer
	written, err := transport.PullFile(ctx, "node-1", remotePath, &pulled)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if written != int64(len(content)) || !bytes.Equal(pulled.Bytes(), content) {
		t.Fatalf("pulled written=%d content=%q", written, pulled.Bytes())
	}

	updated := []byte("replaced content")
	if err := transport.PushFile(ctx, "node-1", remotePath, bytes.NewReader(updated), int64(len(updated)), localDigest(updated)); err != nil {
		t.Fatalf("overwrite push: %v", err)
	}
	pulled.Reset()
	if _, err := transport.PullFile(ctx, "node-1", remotePath, &pulled); err != nil {
		t.Fatalf("overwrite pull: %v", err)
	}
	if !bytes.Equal(pulled.Bytes(), updated) {
		t.Fatalf("overwritten content = %q", pulled.Bytes())
	}
}

func TestLocalStagingRejectsInvalidInputs(t *testing.T) {
	transport, root := newLocalTransport(t)
	ctx := context.Background()
	content := []byte("abc")
	digest := localDigest(content)

	nodeCases := []string{"", ".", "..", "../escape", "a/b", `a\b`, "a b", "C:"}
	for _, nodeID := range nodeCases {
		err := transport.PushFile(ctx, nodeID, "/srv/app/x", bytes.NewReader(content), int64(len(content)), digest)
		if err == nil {
			t.Fatalf("node %q accepted", nodeID)
		}
	}
	pathCases := []string{"", "/", "relative/x", "/../etc/passwd", "/srv/../../etc/passwd", "/srv/app/", "/srv//app/x", "/C:/x"}
	for _, remotePath := range pathCases {
		err := transport.PushFile(ctx, "node-1", remotePath, bytes.NewReader(content), int64(len(content)), digest)
		if err == nil {
			t.Fatalf("path %q accepted", remotePath)
		}
	}
	if err := transport.PushFile(ctx, "node-1", "/srv/app/x", nil, int64(len(content)), digest); err == nil {
		t.Fatal("nil content accepted")
	}
	if err := transport.PushFile(ctx, "node-1", "/srv/app/x", bytes.NewReader(content), -1, digest); err == nil {
		t.Fatal("negative size accepted")
	}
	if err := transport.PushFile(ctx, "node-1", "/srv/app/x", bytes.NewReader(content), int64(len(content)), "ABC"); err == nil {
		t.Fatal("uppercase digest accepted")
	}
	if err := transport.PushFile(ctx, "node-1", "/srv/app/x", bytes.NewReader(content), int64(len(content)+1), digest); !errors.Is(err, files.ErrInvalidTransfer) {
		t.Fatalf("size mismatch = %v", err)
	}
	if err := transport.PushFile(ctx, "node-1", "/srv/app/x", bytes.NewReader(content), int64(len(content)), localDigest([]byte("other"))); !errors.Is(err, files.ErrChecksumMismatch) {
		t.Fatalf("digest mismatch = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "node-1", "srv", "app", "x")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected push left a file: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "node-1", "srv", "app"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("staging directory has leftovers: %v", entries)
	}
	if _, err := os.Stat(filepath.Join(root, "escape")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path escaped staging root: %v", err)
	}
}

func TestLocalStagingRejectsSymlinkedDirectories(t *testing.T) {
	transport, root := newLocalTransport(t)
	ctx := context.Background()
	nodeDir := filepath.Join(root, "node-1")
	if err := os.MkdirAll(nodeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(nodeDir, "srv")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	err := transport.PushFile(ctx, "node-1", "/srv/app/x", bytes.NewReader([]byte("abc")), 3, localDigest([]byte("abc")))
	if !errors.Is(err, files.ErrSymlinkForbidden) {
		t.Fatalf("push through symlink = %v", err)
	}
	entries, readErr := os.ReadDir(outside)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("symlinked directory was written: %v", entries)
	}
}

func TestLocalStagingRejectsSymlinkedTargetFile(t *testing.T) {
	transport, root := newLocalTransport(t)
	ctx := context.Background()
	appDir := filepath.Join(root, "node-1", "srv", "app")
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(appDir, "target")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	err := transport.PushFile(ctx, "node-1", "/srv/app/target", bytes.NewReader([]byte("abc")), 3, localDigest([]byte("abc")))
	if !errors.Is(err, files.ErrSymlinkForbidden) {
		t.Fatalf("push onto symlink = %v", err)
	}
	untouched, readErr := os.ReadFile(outsideFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(untouched) != "outside" {
		t.Fatalf("symlink target modified: %q", untouched)
	}
}

func TestLocalStagingPullRejections(t *testing.T) {
	transport, root := newLocalTransport(t)
	ctx := context.Background()
	appDir := filepath.Join(root, "node-1", "srv", "app")
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "file.txt"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	var sink bytes.Buffer
	if _, err := transport.PullFile(ctx, "node-1", "/srv/app/missing.txt", &sink); err == nil {
		t.Fatal("missing file accepted")
	}
	if _, err := transport.PullFile(ctx, "node-1", "/srv/app", &sink); err == nil {
		t.Fatal("directory accepted as a file")
	}
	if _, err := transport.PullFile(ctx, "node-1", "/srv/app/file.txt", nil); err == nil {
		t.Fatal("nil sink accepted")
	}
	if _, err := transport.PullFile(ctx, "../node-1", "/srv/app/file.txt", &sink); err == nil {
		t.Fatal("invalid node accepted")
	}
	if err := os.Symlink(filepath.Join(appDir, "file.txt"), filepath.Join(appDir, "link.txt")); err == nil {
		if _, err := transport.PullFile(ctx, "node-1", "/srv/app/link.txt", &sink); !errors.Is(err, files.ErrSymlinkForbidden) {
			t.Fatalf("symlink pull = %v", err)
		}
	}
}

func TestLocalStagingHonorsCancellation(t *testing.T) {
	transport, root := newLocalTransport(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	content := []byte("abc")
	if err := transport.PushFile(ctx, "node-1", "/srv/app/x", bytes.NewReader(content), int64(len(content)), localDigest(content)); !errors.Is(err, context.Canceled) {
		t.Fatalf("push cancel = %v", err)
	}
	if _, err := transport.PullFile(ctx, "node-1", "/srv/app/x", io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatalf("pull cancel = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "node-1", "srv", "app", "x")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled push left a file: %v", err)
	}
}

func TestNewLocalStagingTransportRejectsBadRoot(t *testing.T) {
	if _, err := files.NewLocalStagingTransport(""); err == nil {
		t.Fatal("empty root accepted")
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := files.NewLocalStagingTransport(file); err == nil {
		t.Fatal("file root accepted")
	}
}

func TestLocalStagingWindowsReservedNames(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only path rule")
	}
	transport, _ := newLocalTransport(t)
	content := []byte("abc")
	for _, target := range []string{"CON", "nul.txt", "COM1"} {
		err := transport.PushFile(context.Background(), "node-1", "/srv/"+target, bytes.NewReader(content), int64(len(content)), localDigest(content))
		if err == nil {
			t.Fatalf("reserved name %q accepted", target)
		}
	}
}
