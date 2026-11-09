package updates

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"serverfleet.local/serverfleet/internal/transport"
	"strings"
	"sync"
	"time"
)

// RunResidentSupervisor is the actual parent process. It does not attach to
// existing daemons, assume process ownership from a PID file, or replay a lock.
func RunResidentSupervisor(ctx context.Context, c ResidentSupervisorConfig, h ResidentSupervisorHooks) error {
	if ctx == nil || ctx.Err() != nil || c.Format != 1 || c.Component != c.Initial.Component || !installedManifest(c.Initial) || c.Initial.Architecture != runtime.GOARCH || c.Policy.Component != c.Component || c.Policy.Architecture != runtime.GOARCH || c.Policy.MinimumSequence < c.Initial.Sequence || !installID(c.NodeID) || h.Current == nil || h.Ready == nil || h.BeforeStop == nil || h.Stopped == nil || h.RollbackSafe == nil || h.Commit == nil {
		return ErrArtifactRejected
	}
	for _, p := range []string{c.Directory, c.Mailbox, c.Inbox, c.StageDir, filepath.Dir(c.HealthFile)} {
		if !productPrivate(p) {
			return ErrArtifactRejected
		}
	}
	if !filepath.IsAbs(c.HealthFile) {
		return ErrArtifactRejected
	}
	root, e := os.OpenRoot(c.Directory)
	if e != nil {
		return e
	}
	defer root.Close()
	var operationMu sync.Mutex
	if _, e := root.Lstat("fenced.json"); e == nil {
		if h.ServeFence == nil || c.Component != "controller" {
			return ErrUpdateBusy
		}
		// A completed fence may serve fresh observations after parent restart, but
		// must never start its former child. Incomplete fences stay blocked.
		for _, name := range []string{"supervisor-reservation.json", "update-lock.json"} {
			if _, e := root.Lstat(name); !errors.Is(e, os.ErrNotExist) {
				return ErrUpdateBusy
			}
		}
		closeFence, e := h.ServeFence(ctx, newResidentFenceControl(root, c, nil, &operationMu))
		if e != nil {
			return e
		}
		defer closeFence()
		<-ctx.Done()
		return ctx.Err()
	}
	for _, name := range []string{"update-lock.json", "supervisor-reservation.json", "fenced-intent.json"} {
		if _, e = root.Lstat(name); !errors.Is(e, os.ErrNotExist) {
			return ErrUpdateBusy
		}
	}
	installed := c.Initial
	floor := c.Policy.MinimumSequence
	// High water is outside the application DB and never restored from backups.
	if _, e := os.Lstat(filepath.Join(c.Directory, "resident-state.json")); e == nil {
		raw, e := readSizedRegularFile(filepath.Join(c.Directory, "resident-state.json"), 16<<10)
		if e != nil {
			return ErrArtifactRejected
		}
		var v struct {
			Manifest ReleaseManifest
			Floor    uint64
		}
		if json.Unmarshal(raw, &v) != nil || !installedManifest(v.Manifest) || v.Manifest.Component != c.Component || v.Manifest.Architecture != runtime.GOARCH || v.Floor < floor || v.Floor < v.Manifest.Sequence {
			return ErrArtifactRejected
		}
		installed, floor = v.Manifest, v.Floor
	} else if !errors.Is(e, os.ErrNotExist) {
		return ErrArtifactRejected
	}
	stager, e := NewArtifactStager(c.StageDir)
	if e != nil {
		return e
	}
	defer stager.Close()
	source, e := NewDirectoryReleaseSource(c.Inbox, stager)
	if e != nil {
		return e
	}
	defer source.Close()
	slot, e := NewSlotInstaller(c.Directory)
	if e != nil {
		return e
	}
	defer slot.Close()
	var intent *transport.ResidentUpdateIntent
	closing := false
	gate := func(gctx context.Context, _ ReleaseManifest, _ string) error {
		if closing {
			return nil
		} // only OS signal shutdown of this exact owned child
		if _, e := root.Lstat("fenced-intent.json"); !errors.Is(e, os.ErrNotExist) {
			return ErrApplyUnknown
		}
		if intent == nil {
			return nil
		} // explicit operator initial start; no update effects
		return h.Current(gctx, *intent, "current")
	}
	environment := append([]string{}, c.Environment...)
	healthEnv := "SF_AGENT_HEALTH_FILE="
	if c.Component == "controller" {
		healthEnv = "SF_CONTROLLER_HEALTH_FILE="
	}
	for _, v := range environment {
		if strings.HasPrefix(v, "SF_AGENT_HEALTH_FILE=") || strings.HasPrefix(v, "SF_CONTROLLER_HEALTH_FILE=") || strings.HasPrefix(v, "SF_RESIDENT_SUPERVISOR_POLICY=") {
			return ErrArtifactRejected
		}
	}
	environment = append(environment, healthEnv+c.HealthFile, "SF_RESIDENT_SUPERVISOR_POLICY="+ResidentPolicySHA(c))
	process, e := NewOwnedProcessSupervisor(OwnedSupervisorConfig{Directory: c.Directory, ExecutionID: "initial-owned-start", Arguments: c.Arguments, Environment: environment, Gate: gate, Ready: h.Ready})
	if e != nil {
		return e
	}
	defer func() {
		closing = true
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = process.Close(stop)
	}()
	if e = process.Start(ctx, installed); e != nil {
		return e
	}
	if _, e = process.Health(ctx, installed); e != nil {
		return e
	}
	if h.ServeFence != nil {
		if c.Component != "controller" {
			return ErrArtifactRejected
		}
		closeFence, e := h.ServeFence(ctx, newResidentFenceControl(root, c, process, &operationMu))
		if e != nil {
			return e
		}
		defer closeFence()
	}
	mailbox, e := os.OpenRoot(c.Mailbox)
	if e != nil {
		return e
	}
	defer mailbox.Close()
	operationLocked := false
	defer func() {
		if operationLocked {
			operationMu.Unlock()
		}
	}()
	seen := map[string]bool{}
	// Existing requests on supervisor startup are ambiguous: never replay them.
	initial, e := os.ReadDir(c.Mailbox)
	if e != nil {
		return e
	}
	for _, f := range initial {
		if strings.HasSuffix(f.Name(), ".request.json") {
			seen[f.Name()] = true
		}
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		if _, e := root.Lstat("fenced-intent.json"); !errors.Is(e, os.ErrNotExist) {
			continue
		} // permanently serve observations only
		entries, e := os.ReadDir(c.Mailbox)
		if e != nil || len(entries) > 1024 {
			return ErrUpdateBusy
		}
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasSuffix(name, ".request.json") || seen[name] {
				continue
			}
			seen[name] = true
			raw, e := readSizedRegularFile(filepath.Join(c.Mailbox, name), 32<<10)
			if e != nil {
				return ErrApplyUnknown
			}
			var request transport.ResidentUpdateIntent
			d := json.NewDecoder(bytesReader(raw))
			d.DisallowUnknownFields()
			if d.Decode(&request) != nil || !request.Valid(time.Now()) || request.NodeID != c.NodeID || name != request.ExecutionID+".request.json" {
				return ErrArtifactRejected
			}
			p, e := transport.ParseResidentUpdateParameters([]byte(request.Parameters))
			if e != nil || p.PolicySHA256 != ResidentPolicySHA(c) {
				return ErrArtifactRejected
			}
			if writeResidentRecord(mailbox, "update-active.json", request) != nil {
				return ErrUpdateBusy
			}
			if !operationMu.TryLock() {
				return ErrUpdateBusy
			}
			operationLocked = true
			intent = &request
			bounded, cancel := context.WithDeadline(ctx, request.ExpiresAt)
			current := func(phase string) error { return h.Current(bounded, request, phase) }
			if current("admit") != nil {
				cancel()
				return ErrApplyUnknown
			}
			policy := cloneReleasePolicy(c.Policy)
			policy.MinimumSequence = floor
			if c.Feed != nil {
				if e = AcquireResidentRelease(bounded, *c.Feed, c.Inbox, p.Version, p.KeyID, policy, func(gctx context.Context) error { return h.Current(gctx, request, "acquire") }); e != nil {
					cancel()
					return ErrApplyUnknown
				}
			}
			staged, e := source.Stage(bounded, p.Version, p.KeyID, policy)
			if e != nil {
				cancel()
				return e
			}
			if staged.Manifest().SHA256 != p.ReleaseSHA256 || current("verified") != nil || process.BindExecution(request.ExecutionID) != nil {
				staged.Close()
				cancel()
				return ErrArtifactRejected
			}
			receipt, e := slot.Apply(bounded, staged, policy, InstallIntent{TaskID: request.TaskID, ExecutionID: request.ExecutionID, Previous: installed, AllowRollback: true}, InstallHooks{
				Gate: func(gctx context.Context, _ InstallIntent, _ ReleaseManifest, phase string) error {
					return h.Current(gctx, request, phase)
				},
				Stop: func(gctx context.Context, m ReleaseManifest) error {
					if h.BeforeStop(gctx, request) != nil {
						return ErrApplyUnknown
					}
					if process.Stop(gctx, m) != nil {
						return ErrApplyUnknown
					}
					return h.Stopped(gctx, request)
				},
				Start: process.Start, Health: process.Health,
				RollbackSafe: func(gctx context.Context, _, _ ReleaseManifest) error { return h.RollbackSafe(gctx) },
				Commit: func(gctx context.Context, r InstallReceipt) (string, error) {
					if h.Current(gctx, request, "commit") != nil {
						return "", ErrApplyUnknown
					}
					state, _ := json.Marshal(struct {
						Manifest ReleaseManifest
						Floor    uint64
					}{r.Current, max(floor, r.Attempted.Sequence)})
					f, se := root.OpenFile("resident-state.next", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
					if se != nil {
						return "", ErrApplyUnknown
					}
					n, we := f.Write(state)
					sy := f.Sync()
					ce := f.Close()
					if we != nil || n != len(state) || sy != nil || ce != nil || root.Rename("resident-state.next", "resident-state.json") != nil || syncStagingDirectory(root, ".") != nil {
						return "", ErrApplyUnknown
					}
					return h.Commit(gctx, request, r)
				},
			})
			staged.Close()
			cancel()
			if e != nil {
				return ErrApplyUnknown
			}
			if mailbox.Remove("update-active.json") != nil || syncStagingDirectory(mailbox, ".") != nil {
				return ErrApplyUnknown
			}
			installed, floor = receipt.Current, max(floor, receipt.Attempted.Sequence)
			intent = nil
			operationLocked = false
			operationMu.Unlock()
		}
	}
}
