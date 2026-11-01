package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestFileCapacityChild(t *testing.T) {
	root := os.Getenv("SF_TEST_FILE_CAPACITY_ROOT")
	if root == "" {
		t.Skip("private resource-limit subprocess only")
	}
	signal.Ignore(syscall.SIGXFSZ)
	if err := unix.Setrlimit(unix.RLIMIT_FSIZE, &unix.Rlimit{Cur: 1024, Max: 1024}); err != nil {
		t.Fatal(err)
	}
	f, err := openAgentFileCapability(root, func(context.Context, agentFileBinding, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	data := bytes.Repeat([]byte("x"), 4096)
	binding := fileBinding(data, "write")
	if f.Write(context.Background(), binding, 0, data, true) == nil {
		t.Fatal("capacity-limited write falsely succeeded")
	}
	if _, err = os.Stat(filepath.Join(root, binding.RelativePath)); !os.IsNotExist(err) {
		t.Fatal("failed write published destination")
	}
}
func TestActualLinuxCapacityFailureDoesNotPublishPartialFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("non-root private file capability")
	}
	dir := privateAgentDirectory(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestFileCapacityChild$")
	cmd.Env = append(os.Environ(), "SF_TEST_FILE_CAPACITY_ROOT="+dir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatal("private capacity fixture", err, string(output))
	}
	partial, err := os.Stat(filepath.Join(dir, ".sf-file-transfer", "partial"))
	if err != nil || partial.Size() != 1024 {
		t.Fatal("no actual OS-limited partial write observed", err)
	}
}
