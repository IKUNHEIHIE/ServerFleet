package updates

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/transport"
)

// The executable is a real private child, signed and hashed as bytes. Its tiny
// readiness protocol tests the supervisor's lifecycle, not Agent enrollment or
// the product's Controller HTTP readiness adapter.
func TestResidentLifecycleChild(t *testing.T) {
	if os.Getenv("SF_TEST_RESIDENT_CHILD") != "1" {
		t.Skip("private lifecycle subprocess only")
	}
	body, err := os.ReadFile("/proc/self/exe")
	if err != nil {
		os.Exit(71)
	}
	marker := "\nSF_TEST_RELEASE="
	index := strings.LastIndex(string(body), marker)
	if index < 0 {
		os.Exit(72)
	}
	version := strings.TrimSpace(string(body[index+len(marker):]))
	raw, _ := json.Marshal(struct {
		PID            int
		Nonce, Version string
	}{os.Getpid(), os.Getenv("SF_SUPERVISOR_NONCE"), version})
	if os.WriteFile(os.Getenv("SF_TEST_RESIDENT_READY"), raw, 0600) != nil {
		os.Exit(73)
	}
	for {
		time.Sleep(time.Second)
	}
}

type residentLifecycleFixture struct {
	c       ResidentSupervisorConfig
	h       ResidentSupervisorHooks
	request transport.ResidentUpdateIntent
	commits chan InstallReceipt
	ready   chan OwnedProcessObservation
	fences  chan ResidentFenceControl
}

func newResidentLifecycle(t *testing.T, component, reportedCandidateVersion string) *residentLifecycleFixture {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("requires a genuine non-root process owner")
	}
	base := t.TempDir()
	if err := os.Chmod(base, 0700); err != nil {
		t.Fatal(err)
	}
	private := func(name string) string {
		p := filepath.Join(base, name)
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
		return p
	}
	dir, mailbox, inbox, stage, health := private("slot"), private("mailbox"), private("inbox"), private("stage"), private("health")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	initialBody := append(append([]byte{}, body...), []byte("\nSF_TEST_RELEASE=1.0.0\n")...)
	nextBody := append(append([]byte{}, body...), []byte("\nSF_TEST_RELEASE="+reportedCandidateVersion+"\n")...)
	manifest := func(version string, seq uint64, b []byte) ReleaseManifest {
		return ReleaseManifest{Format: 1, Component: component, Version: version, Sequence: seq, OS: "linux", Architecture: runtime.GOARCH, SHA256: releaseDigest(b), Bytes: int64(len(b))}
	}
	previous, next := manifest("1.0.0", 1, initialBody), manifest("2.0.0", 2, nextBody)
	if err = os.WriteFile(filepath.Join(dir, "active"), initialBody, 0700); err != nil {
		t.Fatal(err)
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	versionDir := filepath.Join(inbox, next.Version)
	if err = os.Mkdir(versionDir, 0700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(next)
	for name, data := range map[string][]byte{"manifest.json": raw, "signature.bin": ed25519.Sign(key, append([]byte("serverfleet.release.v1\x00"), raw...)), "artifact": nextBody} {
		if err = os.WriteFile(filepath.Join(versionDir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	f := &residentLifecycleFixture{commits: make(chan InstallReceipt, 4), ready: make(chan OwnedProcessObservation, 8), fences: make(chan ResidentFenceControl, 2)}
	f.c = ResidentSupervisorConfig{Format: 1, Component: component, NodeID: "owned-node", Directory: dir, Mailbox: mailbox, Inbox: inbox, StageDir: stage, HealthFile: filepath.Join(health, "ready.json"), Initial: previous, Policy: ReleasePolicy{Component: component, Architecture: runtime.GOARCH, MinimumSequence: 1, MaxBytes: 256 << 20, TrustedKeys: map[string]ed25519.PublicKey{"test-release": pub}}, Arguments: []string{"-test.run=^TestResidentLifecycleChild$"}, Environment: []string{"SF_TEST_RESIDENT_CHILD=1", "SF_TEST_RESIDENT_READY=" + filepath.Join(health, "ready.json")}}
	params, _ := json.Marshal(map[string]string{"version": next.Version, "keyId": "test-release", "releaseSha256": next.SHA256, "policySha256": ResidentPolicySHA(f.c)})
	f.request = transport.ResidentUpdateIntent{Format: 1, TaskID: "approved-task", ExecutionID: "approved-execution", PlanSHA256: strings.Repeat("a", 64), NodeID: f.c.NodeID, Parameters: string(params), ExpiresAt: time.Now().Add(time.Minute)}
	f.h = ResidentSupervisorHooks{
		Current:      func(ctx context.Context, _ transport.ResidentUpdateIntent, _ string) error { return ctx.Err() },
		BeforeStop:   func(ctx context.Context, _ transport.ResidentUpdateIntent) error { return ctx.Err() },
		Stopped:      func(ctx context.Context, _ transport.ResidentUpdateIntent) error { return ctx.Err() },
		RollbackSafe: func(ctx context.Context) error { return ctx.Err() },
		Commit: func(_ context.Context, _ transport.ResidentUpdateIntent, r InstallReceipt) (string, error) {
			f.commits <- r
			return "test-atomic-owner-audit", nil
		},
		Ready: func(ctx context.Context, m ReleaseManifest, p OwnedProcessObservation) error {
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				b, e := os.ReadFile(f.c.HealthFile)
				var r struct {
					PID            int
					Nonce, Version string
				}
				if e == nil && json.Unmarshal(b, &r) == nil && r.PID == p.PID && r.Nonce == p.Nonce {
					if r.Version != m.Version {
						return ErrHealthUnhealthy
					}
					f.ready <- p
					return nil
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-ticker.C:
				}
			}
		},
	}
	return f
}

func startResidentLifecycle(t *testing.T, f *residentLifecycleFixture) (context.Context, func(), <-chan error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	done := make(chan error, 1)
	go func() { done <- RunResidentSupervisor(ctx, f.c, f.h) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case e := <-done:
				if e != nil && !errors.Is(e, context.Canceled) {
					t.Errorf("resident shutdown: %v", e)
				}
			case <-time.After(8 * time.Second):
				t.Error("owned parent failed to stop its child")
			}
		})
	}
	t.Cleanup(stop)
	return ctx, stop, done
}

func residentReady(t *testing.T, ctx context.Context, f *residentLifecycleFixture) OwnedProcessObservation {
	t.Helper()
	select {
	case p := <-f.ready:
		return p
	case <-ctx.Done():
		t.Fatal("actual child never became ready", ctx.Err())
		return OwnedProcessObservation{}
	}
}

func TestResidentSupervisorStartupWindowSignedUpdateAndRestart(t *testing.T) {
	for _, component := range []string{"agent", "controller"} {
		t.Run(component, func(t *testing.T) {
			f := newResidentLifecycle(t, component, "2.0.0")
			ready := f.h.Ready
			var initial sync.Once
			f.h.Ready = func(ctx context.Context, m ReleaseManifest, p OwnedProcessObservation) error {
				var e error
				// Submit while the initial child is still inside readiness, before the
				// old implementation took its startup mailbox snapshot.
				initial.Do(func() { e = WriteResidentIntent(f.c.Mailbox, f.request) })
				if e != nil {
					return e
				}
				return ready(ctx, m, p)
			}
			ctx, stop, _ := startResidentLifecycle(t, f)
			first := residentReady(t, ctx, f)
			var receipt InstallReceipt
			select {
			case receipt = <-f.commits:
			case <-ctx.Done():
				t.Fatal("request submitted during startup readiness was lost", ctx.Err())
			}
			if receipt.State != "installed" || receipt.Current.Sequence != 2 || receipt.Health.ProcessID == "" {
				t.Fatalf("update result: %+v", receipt)
			}
			second := residentReady(t, ctx, f)
			if first.PID == second.PID || first.Nonce == second.Nonce {
				t.Fatal("update did not start a distinct owned process")
			}
			waitResidentCommit(t, ctx, f)
			stop()
			if syscall.Kill(second.PID, 0) == nil {
				t.Fatal("owned child survived parent shutdown")
			}
			ctx, stop, _ = startResidentLifecycle(t, f)
			restarted := residentReady(t, ctx, f)
			if restarted.Nonce == second.Nonce {
				t.Fatal("restart reused prior health")
			}
			select {
			case <-f.commits:
				t.Fatal("startup replayed an old completed request")
			case <-time.After(300 * time.Millisecond):
			}
			stop()
		})
	}
}

func waitResidentCommit(t *testing.T, ctx context.Context, f *residentLifecycleFixture) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		slot, e := NewSlotInstaller(f.c.Directory)
		if e == nil {
			_, e = slot.Receipt(f.request.ExecutionID)
			slot.Close()
		}
		_, active := os.Lstat(filepath.Join(f.c.Mailbox, "update-active.json"))
		if e == nil && errors.Is(active, os.ErrNotExist) {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("actual receipt not durably finalized", ctx.Err())
		case <-ticker.C:
		}
	}
}

func TestResidentSupervisorUnhealthyRollbackAndPersistentFloor(t *testing.T) {
	f := newResidentLifecycle(t, "agent", "1.0.0")
	ctx, stop, _ := startResidentLifecycle(t, f)
	first := residentReady(t, ctx, f)
	if e := WriteResidentIntent(f.c.Mailbox, f.request); e != nil {
		t.Fatal(e)
	}
	var receipt InstallReceipt
	select {
	case receipt = <-f.commits:
	case <-ctx.Done():
		t.Fatal("rollback did not finish", ctx.Err())
	}
	if receipt.State != "rolled_back" || receipt.Current != f.c.Initial || receipt.Attempted.Sequence != 2 {
		t.Fatalf("rollback result: %+v", receipt)
	}
	previous := residentReady(t, ctx, f)
	if previous.PID == first.PID {
		t.Fatal("rollback did not restart the owned previous image")
	}
	waitResidentCommit(t, ctx, f)
	raw, e := readSizedRegularFile(filepath.Join(f.c.Directory, "resident-state.json"), 16<<10)
	var state struct {
		Manifest ReleaseManifest
		Floor    uint64
	}
	if e != nil || json.Unmarshal(raw, &state) != nil || state.Floor != 2 || state.Manifest != f.c.Initial {
		t.Fatal("rollback lowered high water", string(raw), e)
	}
	stop()
	// The callback publishes readiness BEFORE Health's final authority check.
	// Cancel immediately after this restart observation deliberately races that
	// check: UNKNOWN remains correct, but shutdown must still stop/wait the owned
	// child and clear its reservation. Do not mistake callback delivery for a
	// completed parent health check or relax the production cancellation gate.
	restartCtx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	restartDone := make(chan error, 1)
	go func() { restartDone <- RunResidentSupervisor(restartCtx, f.c, f.h) }()
	restarted := residentReady(t, restartCtx, f)
	cancel()
	select {
	case e := <-restartDone:
		if !errors.Is(e, context.Canceled) && !errors.Is(e, ErrApplyUnknown) {
			t.Fatal("restart cancellation outcome", e)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("restarted parent failed to stop its child")
	}
	if e := syscall.Kill(restarted.PID, 0); !errors.Is(e, syscall.ESRCH) {
		t.Fatal("restarted child did not exit after cancellation", e)
	}
	for _, p := range []string{filepath.Join(f.c.Directory, "supervisor-reservation.json"), filepath.Join(f.c.Directory, "update-lock.json"), filepath.Join(f.c.Mailbox, "update-active.json")} {
		if _, e := os.Lstat(p); !errors.Is(e, os.ErrNotExist) {
			t.Fatal("restart cancellation left unresolved ownership or update", p, e)
		}
	}
}

func TestResidentSupervisorBlocksUnresolvedMailboxBeforeStartingChild(t *testing.T) {
	f := newResidentLifecycle(t, "agent", "2.0.0")
	if e := os.WriteFile(filepath.Join(f.c.Mailbox, "update-active.json"), []byte("unresolved-claim"), 0600); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e := RunResidentSupervisor(ctx, f.c, f.h); !errors.Is(e, ErrUpdateBusy) {
		t.Fatal("parent admitted unresolved mailbox", e)
	}
	if _, e := os.Stat(f.c.HealthFile); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("unresolved restart started a child", e)
	}
}

func TestResidentSupervisorAuthorityLostAfterStopStaysUnknown(t *testing.T) {
	f := newResidentLifecycle(t, "controller", "2.0.0")
	withdrawn := false
	f.h.Current = func(ctx context.Context, _ transport.ResidentUpdateIntent, _ string) error {
		if withdrawn {
			return ErrApplyUnknown
		}
		return ctx.Err()
	}
	f.h.Stopped = func(context.Context, transport.ResidentUpdateIntent) error { withdrawn = true; return nil }
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunResidentSupervisor(ctx, f.c, f.h) }()
	first := residentReady(t, ctx, f)
	if e := WriteResidentIntent(f.c.Mailbox, f.request); e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-done:
		if !errors.Is(e, ErrApplyUnknown) {
			t.Fatal("withdrawn authority outcome", e)
		}
	case <-ctx.Done():
		t.Fatal("parent did not stop after authority loss")
	}
	if syscall.Kill(first.PID, 0) == nil {
		t.Fatal("old process remained running after stop")
	}
	for _, p := range []string{filepath.Join(f.c.Directory, "update-lock.json"), filepath.Join(f.c.Mailbox, "update-active.json")} {
		if _, e := os.Stat(p); e != nil {
			t.Fatal("lost UNKNOWN marker", p, e)
		}
	}
	select {
	case <-f.commits:
		t.Fatal("unknown update committed success")
	default:
	}
	select {
	case <-f.ready:
		t.Fatal("revoked update started candidate or rollback")
	default:
	}
	if e := RunResidentSupervisor(ctx, f.c, f.h); !errors.Is(e, ErrUpdateBusy) {
		t.Fatal("unknown update was restarted", e)
	}
}

func TestResidentSupervisorRejectsTrailingMailboxJSONBeforeUpdateEffects(t *testing.T) {
	f := newResidentLifecycle(t, "agent", "2.0.0")
	ready := f.h.Ready
	f.h.Ready = func(ctx context.Context, m ReleaseManifest, p OwnedProcessObservation) error {
		raw, _ := json.Marshal(f.request)
		raw = append(raw, []byte("{}")...)
		if e := os.WriteFile(filepath.Join(f.c.Mailbox, f.request.ExecutionID+".request.json"), raw, 0600); e != nil {
			return e
		}
		return ready(ctx, m, p)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunResidentSupervisor(ctx, f.c, f.h) }()
	first := residentReady(t, ctx, f)
	select {
	case e := <-done:
		if !errors.Is(e, ErrArtifactRejected) {
			t.Fatal("trailing mailbox document accepted", e)
		}
	case <-ctx.Done():
		t.Fatal("invalid request was ignored instead of rejected")
	}
	if syscall.Kill(first.PID, 0) == nil {
		t.Fatal("failed parent left owned child running")
	}
	if _, e := os.Stat(filepath.Join(f.c.Directory, "update-lock.json")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("invalid payload reached installer", e)
	}
}

func TestResidentSupervisorActualFencePersistsAcrossParentRestart(t *testing.T) {
	f := newResidentLifecycle(t, "controller", "2.0.0")
	f.h.ServeFence = func(_ context.Context, c ResidentFenceControl) (func() error, error) {
		f.fences <- c
		return func() error { return nil }, nil
	}
	ctx, stop, _ := startResidentLifecycle(t, f)
	child := residentReady(t, ctx, f)
	var control ResidentFenceControl
	select {
	case control = <-f.fences:
	case <-ctx.Done():
		t.Fatal("fence control unavailable")
	}
	request := ResidentFenceRequest{AuthorizationSHA256: strings.Repeat("b", 64), OldAuthorityID: f.c.NodeID, NewAuthorityID: "replacement-controller", TrustGeneration: 2}
	record, e := control.Fence(ctx, request)
	if e != nil || record.ImageSHA256 != f.c.Initial.SHA256 || record.Request != request || syscall.Kill(child.PID, 0) == nil {
		t.Fatal("fence failed to stop/wait actual owned process", record, e)
	}
	if again, e := control.Current(ctx, request); e != nil || again != record {
		t.Fatal("fence observation changed", e)
	}
	wrong := request
	wrong.NewAuthorityID = "different-controller"
	if _, e = control.Current(ctx, wrong); e == nil {
		t.Fatal("fence evidence rebound to another authority")
	}
	stop()
	ctx, stop, _ = startResidentLifecycle(t, f)
	select {
	case control = <-f.fences:
	case <-ctx.Done():
		t.Fatal("fenced parent failed to restart in observation-only mode")
	}
	if again, e := control.Current(ctx, request); e != nil || again != record {
		t.Fatal("fence did not survive parent restart", e)
	}
	select {
	case <-f.ready:
		t.Fatal("fenced parent resurrected old Controller")
	case <-time.After(300 * time.Millisecond):
	}
	stop()
}
