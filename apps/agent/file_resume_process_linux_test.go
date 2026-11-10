package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/transport"
)

func processResumeData() []byte { return bytes.Repeat([]byte("r"), agentFileChunkBytes+777) }
func processResumeBinding() agentFileBinding {
	data := processResumeData()
	sum := sha256.Sum256(data)
	return agentFileBinding{PlanSHA256: strings.Repeat("a", 64), TaskID: "seed-task", ExecutionID: "seed-execution", TransferID: "seed-transfer", NodeID: "node", RelativePath: "payload.bin", SHA256: hex.EncodeToString(sum[:]), Direction: "write", ConflictPolicy: "overwrite", Size: int64(len(data))}
}

// Each phase runs in a separate actual OS process. Only the explicitly trusted
// local file port is exercised here; these are not Controller/WSS auth receipts.
func TestAgentResumeProcessChild(t *testing.T) {
	phase := os.Getenv("SF_TEST_RESUME_PHASE")
	if phase == "" {
		t.Skip("private file-resume subprocess only")
	}
	root := os.Getenv("SF_TEST_RESUME_ROOT")
	f, err := openAgentFileCapability(root, func(context.Context, agentFileBinding, string) error {
		if os.Getenv("SF_TEST_RESUME_DENY") == "1" {
			return errFileCapability
		}
		return nil
	})
	if err != nil {
		os.Exit(70)
	}
	defer f.Close()
	b := processResumeBinding()
	data := processResumeData()
	ctx := context.Background()
	if phase == "seed" {
		if f.Write(ctx, b, 0, data[:agentFileChunkBytes], false) != nil {
			os.Exit(71)
		}
		return
	}
	prefix := sha256.Sum256(data[:agentFileChunkBytes])
	raw, _ := json.Marshal(transport.FileResumeSource{TaskID: b.TaskID, ExecutionID: b.ExecutionID, TransferID: b.TransferID, PlanSHA256: b.PlanSHA256, ConfirmedOffset: agentFileChunkBytes, PrefixSHA256: hex.EncodeToString(prefix[:])})
	suffix := os.Getenv("SF_TEST_RESUME_SUFFIX")
	if suffix == "" {
		suffix = "next"
	}
	b.TaskID = "new-task-" + suffix
	b.ExecutionID = "new-execution-" + suffix
	b.TransferID = "new-transfer-" + suffix
	b.PlanSHA256 = strings.Repeat("b", 64)
	b, err = f.adoptResume(ctx, b, string(raw))
	if err != nil {
		os.Exit(73)
	}
	if phase == "finish" && f.Write(ctx, b, agentFileChunkBytes, data[agentFileChunkBytes:], true) != nil {
		os.Exit(74)
	}
}

func resumeProcessCommand(t *testing.T, ctx context.Context, root, phase, suffix string, deny bool) *exec.Cmd {
	t.Helper()
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestAgentResumeProcessChild$")
	cmd.Env = []string{"SF_TEST_RESUME_ROOT=" + root, "SF_TEST_RESUME_PHASE=" + phase, "SF_TEST_RESUME_SUFFIX=" + suffix}
	if deny {
		cmd.Env = append(cmd.Env, "SF_TEST_RESUME_DENY=1")
	}
	return cmd
}

func seedResumeProcess(t *testing.T) (context.Context, string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("requires genuine ordinary file owner")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	root := t.TempDir()
	if e := os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	if out, e := resumeProcessCommand(t, ctx, root, "seed", "", false).CombinedOutput(); e != nil {
		t.Fatal("seed process", e, string(out))
	}
	if _, e := os.Stat(filepath.Join(root, "payload.bin")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("seed published incomplete file", e)
	}
	return ctx, root
}

func TestAgentConfirmedPrefixResumesInNewProcess(t *testing.T) {
	ctx, root := seedResumeProcess(t)
	if out, e := resumeProcessCommand(t, ctx, root, "finish", "", false).CombinedOutput(); e != nil {
		t.Fatal("new process could not adopt confirmed prefix", e, string(out))
	}
	data, e := os.ReadFile(filepath.Join(root, "payload.bin"))
	if e != nil || !bytes.Equal(data, processResumeData()) {
		t.Fatal("resumed publication bytes differ", e)
	}
	f, e := openAgentFileCapability(root, func(context.Context, agentFileBinding, string) error { return nil })
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if f.Write(ctx, processResumeBinding(), agentFileChunkBytes, []byte("x"), false) == nil {
		t.Fatal("old execution remained writable after adoption")
	}
	if e := resumeProcessCommand(t, ctx, root, "finish", "again", false).Run(); e == nil {
		t.Fatal("adopted prefix was reused by another execution")
	}
}

func TestAgentResumeRejectsCorruptPrefixUnknownPublishAndRevocation(t *testing.T) {
	for _, scenario := range []string{"corrupt-prefix", "publish-intent", "revoked"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, root := seedResumeProcess(t)
			switch scenario {
			case "corrupt-prefix":
				f, e := os.OpenFile(filepath.Join(root, ".sf-seed-transfer", "partial"), os.O_WRONLY, 0600)
				if e != nil {
					t.Fatal(e)
				}
				_, e = f.WriteAt([]byte("changed"), 0)
				ce := f.Close()
				if e != nil || ce != nil {
					t.Fatal(e, ce)
				}
			case "publish-intent":
				if e := os.WriteFile(filepath.Join(root, ".sf-seed-transfer", "commit-intent.json"), []byte("unknown-publication"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			if e := resumeProcessCommand(t, ctx, root, "finish", "", scenario == "revoked").Run(); e == nil {
				t.Fatal("unsafe process resume succeeded")
			}
			if _, e := os.Stat(filepath.Join(root, "payload.bin")); !errors.Is(e, os.ErrNotExist) {
				t.Fatal("rejected resume published destination", e)
			}
		})
	}
}

func TestAgentConcurrentProcessesCannotAdoptSamePrefix(t *testing.T) {
	ctx, root := seedResumeProcess(t)
	a, b := resumeProcessCommand(t, ctx, root, "adopt", "a", false), resumeProcessCommand(t, ctx, root, "adopt", "b", false)
	if e := a.Start(); e != nil {
		t.Fatal(e)
	}
	if e := b.Start(); e != nil {
		_ = a.Process.Kill()
		_ = a.Wait()
		t.Fatal(e)
	}
	ea, eb := a.Wait(), b.Wait()
	if (ea == nil) == (eb == nil) {
		t.Fatalf("expected exactly one prefix adopter, got %v and %v", ea, eb)
	}
	if _, e := os.Stat(filepath.Join(root, "payload.bin")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("adoption alone published bytes", e)
	}
}
