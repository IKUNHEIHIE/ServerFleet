//go:build linux

package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"serverfleet.local/serverfleet/internal/updates"
	"testing"
)

func checkpointDirectory(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if e := os.Chmod(d, 0700); e != nil {
		t.Fatal(e)
	}
	for _, n := range []string{"enrollment.json", "identity-key.pem", "issued.json"} {
		if e := os.WriteFile(filepath.Join(d, n), []byte("test-state-"+n), 0600); e != nil {
			t.Fatal(e)
		}
	}
	return d
}

// State-integrity tests only; no actual process/key/release is impersonated.
func TestSupervisorFreezesLedgerAfterStopWithoutRewindingCandidateChanges(t *testing.T) {
	ctx := context.Background()
	d := checkpointDirectory(t)
	s := supervisorAgentCheckpoint{directory: d}
	if s.RollbackSafe(ctx) != updates.ErrApplyUnknown {
		t.Fatal("rollback before stop")
	}
	if e := s.BeforeStop(ctx); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(d, "execution-result.json")
	if e := os.WriteFile(p, []byte(`{"result":"UNKNOWN"}`), 0600); e != nil {
		t.Fatal(e)
	}
	if e := s.Stopped(ctx); e != nil {
		t.Fatal("old finalization rejected", e)
	}
	if e := s.RollbackSafe(ctx); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte("candidate-changed-state"), 0600); e != nil {
		t.Fatal(e)
	}
	if s.RollbackSafe(ctx) != updates.ErrApplyUnknown || s.BeforeStop(ctx) != updates.ErrApplyUnknown || s.Stopped(ctx) != updates.ErrApplyUnknown {
		t.Fatal("candidate state change admitted")
	}
}
func TestSupervisorIdentityCannotChangeDuringOldProcessShutdown(t *testing.T) {
	ctx := context.Background()
	d := checkpointDirectory(t)
	s := supervisorAgentCheckpoint{directory: d}
	if e := s.BeforeStop(ctx); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(d, "issued.json"), []byte("changed-trust"), 0600); e != nil {
		t.Fatal(e)
	}
	if s.Stopped(ctx) != updates.ErrApplyUnknown {
		t.Fatal("trust changed during shutdown")
	}
}
func TestSupervisorRejectsChangedPinnedRoots(t *testing.T) {
	d := checkpointDirectory(t)
	p := filepath.Join(d, "root.pem")
	raw := []byte("test-public-root")
	if e := os.WriteFile(p, raw, 0600); e != nil {
		t.Fatal(e)
	}
	c := updates.ResidentSupervisorConfig{RootCertificate: p, RootCertificateSHA256: recoveryDigest(raw)}
	if _, e := pinnedSupervisorRoots(c); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte("different-root"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := pinnedSupervisorRoots(c); e != updates.ErrArtifactRejected {
		t.Fatal("unapproved root accepted", e)
	}
}
