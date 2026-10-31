package plugins

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// A deliberately late runtime double tests lifecycle fencing independently of
// the runtime contract. Cleanup for one start must never stop a newer instance.
type lateTestRuntime struct {
	mu               sync.Mutex
	instances        map[string]bool
	started, release chan struct{}
}

func (r *lateTestRuntime) Start(ctx context.Context, i RuntimeInstance, _ PackageContent) error {
	if i.ID == "enable-old" {
		close(r.started)
		select {
		case <-r.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.instances[i.ID] = true
	return nil
}
func (r *lateTestRuntime) Stop(_ context.Context, i RuntimeInstance) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.instances, i.ID)
	return nil
}
func (r *lateTestRuntime) Ready(_ context.Context, i RuntimeInstance) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.instances[i.ID] {
		return errors.New("not running")
	}
	return nil
}
func TestLatePluginStartCannotDisableOrStopNewerRuntimeInstance(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, _, _, raw, sig, payload, digest := lifecycleFixture(t)
	runtime := &lateTestRuntime{instances: map[string]bool{}, started: make(chan struct{}), release: make(chan struct{})}
	m.runtime = runtime
	op := LifecycleOperation{ActorID: "admin", TaskID: "install-fixture", Action: "plugins.install", Digest: digest, ConsentedCapabilities: []string{"xray.read"}}
	installed, err := m.Install(ctx, op, raw, sig, payload, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	old := op
	old.TaskID = "enable-old"
	old.Action = "plugins.enable"
	old.ExpectedRevision = installed.Revision
	done := make(chan error, 1)
	go func() { _, err := m.Enable(ctx, old); done <- err }()
	select {
	case <-runtime.started:
	case <-ctx.Done():
		t.Fatal("start missing")
	}
	starting, err := m.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	disable := op
	disable.TaskID = "disable-old"
	disable.Action = "plugins.disable"
	disable.ExpectedRevision = starting.Revision
	disabled, err := m.Disable(ctx, disable)
	if err != nil {
		t.Fatal(err)
	}
	next := old
	next.TaskID = "enable-new"
	next.ExpectedRevision = disabled.Revision
	enabled, err := m.Enable(ctx, next)
	if err != nil {
		t.Fatal(err)
	}
	close(runtime.release)
	select {
	case err := <-done:
		if err != ErrLifecycleUnknown {
			t.Fatal("late start claimed enabled", err)
		}
	case <-ctx.Done():
		t.Fatal("old start did not finish")
	}
	current, err := m.Get(ctx)
	if err != nil || current != enabled || !current.Enabled {
		t.Fatal("late cleanup overwrote newer installation", current, err)
	}
	if runtime.Ready(ctx, runtimeInstance(current)) != nil {
		t.Fatal("cleanup stopped newer runtime")
	}
	invoke := LifecycleOperation{ActorID: "admin", TaskID: "task-fixture", Action: "plugins.invoke", Digest: digest, ExpectedRevision: current.Revision, Capability: "xray.read", RuntimeID: "enable-old"}
	if _, err = m.CheckStoredInvocation(ctx, invoke, "xray.read"); err != ErrPackageRejected {
		t.Fatal("old runtime borrowed newer lease", err)
	}
	invoke.RuntimeID = current.RuntimeID
	if _, err = m.CheckStoredInvocation(ctx, invoke, "xray.read"); err != nil {
		t.Fatal(err)
	}
}
