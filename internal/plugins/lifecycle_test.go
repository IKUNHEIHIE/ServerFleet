package plugins

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
)

type lifecycleTestStore struct {
	mu   sync.Mutex
	data []byte
	fail bool
}

func (s *lifecycleTestStore) View(ctx context.Context, fn func([]byte) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(bytes.Clone(s.data))
}
func (s *lifecycleTestStore) Update(ctx context.Context, fn func([]byte) ([]byte, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next, err := fn(bytes.Clone(s.data))
	if err != nil {
		return err
	}
	if s.fail {
		return errors.New("failed commit")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.data = bytes.Clone(next)
	return nil
}

// Runtime double only; no executable plugin or runtime choice is implied.
type lifecycleTestRuntime struct {
	starts, stops int
	running       bool
	failStop      bool
	onStop        func()
}

func (r *lifecycleTestRuntime) Start(context.Context, RuntimeInstance, PackageContent) error {
	r.starts++
	r.running = true
	return nil
}
func (r *lifecycleTestRuntime) Stop(context.Context, RuntimeInstance) error {
	r.stops++
	if r.onStop != nil {
		r.onStop()
	}
	if r.failStop {
		return errors.New("stop unknown")
	}
	r.running = false
	return nil
}
func (r *lifecycleTestRuntime) Ready(context.Context, RuntimeInstance) error {
	if !r.running {
		return errors.New("not running")
	}
	return nil
}
func pluginFixture(t *testing.T) ([]byte, []byte, []byte, PackageTrust) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("opaque signed test payload; not executable")
	hash := sha256.Sum256(payload)
	manifest := PackageManifest{Format: 1, ID: "xray-manager", Version: "0.9.0", Source: "https://source.invalid/xray/v0.9.0", PayloadSHA256: hex.EncodeToString(hash[:]), PayloadBytes: len(payload), Capabilities: []string{"xray.read"}, UI: []UIElement{{ID: "read", Kind: "action", Label: "Read", Action: "xray.read"}}}
	raw, _ := json.Marshal(manifest)
	sig := ed25519.Sign(key, append([]byte("serverfleet.plugin.v1\x00"), raw...))
	return raw, sig, payload, PackageTrust{Keys: map[string]ed25519.PublicKey{"test-key": pub}, Sources: map[string]bool{manifest.Source: true}, Capabilities: map[string]bool{"xray.read": true}}
}
func lifecycleFixture(t *testing.T) (*LifecycleManager, *lifecycleTestStore, *lifecycleTestRuntime, []byte, []byte, []byte, string) {
	t.Helper()
	dir := t.TempDir()
	_ = os.Chmod(dir, 0700)
	vault, err := NewPackageVault(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = vault.Close() })
	raw, sig, payload, trust := pluginFixture(t)
	store := &lifecycleTestStore{}
	runtime := &lifecycleTestRuntime{}
	manager, err := NewLifecycleManager(store, vault, func(context.Context) (PackageTrust, error) { return trust, nil }, func(context.Context, LifecycleOperation) error { return nil }, runtime)
	if err != nil {
		t.Fatal(err)
	}
	return manager, store, runtime, raw, sig, payload, packageIdentity(raw, sig, "test-key")
}
func TestPluginVerifiedInstallDefaultDisabledLifecycleAndOldLeaseDenial(t *testing.T) {
	ctx := context.Background()
	m, _, runtime, raw, sig, payload, digest := lifecycleFixture(t)
	op := LifecycleOperation{ActorID: "admin-fixture", TaskID: "install-1", Action: "plugins.install", Digest: digest, ConsentedCapabilities: []string{"xray.read"}}
	installed, err := m.Install(ctx, op, raw, sig, payload, "test-key")
	if err != nil || installed.Enabled || !installed.Installed || runtime.starts != 0 {
		t.Fatal(installed, err)
	}
	if _, err = m.Install(ctx, op, raw, sig, payload, "test-key"); err != ErrLifecycleReplay {
		t.Fatal("install replay", err)
	}
	op.TaskID = "enable-1"
	op.Action = "plugins.enable"
	op.ExpectedRevision = installed.Revision
	enabled, err := m.Enable(ctx, op)
	if err != nil || !enabled.Enabled || runtime.starts != 1 {
		t.Fatal(enabled, err)
	}
	invoke := LifecycleOperation{ActorID: op.ActorID, TaskID: "real-task-fixture", Action: "plugins.invoke", ExpectedRevision: enabled.Revision, Digest: digest, Capability: "xray.read", RuntimeID: enabled.RuntimeID}
	if _, err = m.CheckStoredInvocation(ctx, invoke, "xray.read"); err != nil {
		t.Fatal(err)
	}
	runtime.onStop = func() {
		if _, err = m.CheckStoredInvocation(ctx, invoke, "xray.read"); err != ErrPackageRejected {
			t.Error("disable stopped runtime before rejecting Host", err)
		}
	}
	op.TaskID = "disable-1"
	op.Action = "plugins.disable"
	op.ExpectedRevision = enabled.Revision
	disabled, err := m.Disable(ctx, op)
	if err != nil || disabled.Enabled || disabled.Phase != "disabled" {
		t.Fatal(disabled, err)
	}
	if _, err = m.CheckStoredInvocation(ctx, invoke, "xray.read"); err != ErrPackageRejected {
		t.Fatal("old lease after disable", err)
	}
	op.TaskID = "remove-1"
	op.Action = "plugins.uninstall"
	op.ExpectedRevision = disabled.Revision
	removed, err := m.Uninstall(ctx, op)
	if err != nil || removed.Installed {
		t.Fatal(removed, err)
	}
	op.TaskID = "reinstall-1"
	op.Action = "plugins.install"
	op.ExpectedRevision = removed.Revision
	reinstalled, err := m.Install(ctx, op, raw, sig, payload, "test-key")
	if err != nil || reinstalled.Revision <= enabled.Revision {
		t.Fatal("installation revision reused", reinstalled, err)
	}
	if runtime.stops != 1 || runtime.starts != 1 {
		t.Fatal("uninstall/reinstall affected target node runtime", runtime)
	}
}
func TestPluginMissingRuntimeTamperedArtifactConsentAndUnknownStop(t *testing.T) {
	ctx := context.Background()
	m, _, runtime, raw, sig, payload, digest := lifecycleFixture(t)
	op := LifecycleOperation{ActorID: "admin", TaskID: "install-1", Action: "plugins.install", Digest: digest}
	if _, err := m.Install(ctx, op, raw, sig, payload, "test-key"); err != ErrPackageRejected {
		t.Fatal("missing capability consent", err)
	}
	op.ConsentedCapabilities = []string{"xray.read"}
	installed, err := m.Install(ctx, op, raw, sig, payload, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	op.Action = "plugins.enable"
	op.TaskID = "enable-1"
	op.ExpectedRevision = installed.Revision
	m.runtime = nil
	if _, err = m.Enable(ctx, op); err != ErrPackageRejected {
		t.Fatal("missing admitted runtime", err)
	}
	m.runtime = runtime
	enabled, err := m.Enable(ctx, op)
	if err != nil {
		t.Fatal(err)
	}
	runtime.failStop = true
	op.Action = "plugins.disable"
	op.TaskID = "stop-1"
	op.ExpectedRevision = enabled.Revision
	if _, err = m.Disable(ctx, op); err != ErrLifecycleUnknown {
		t.Fatal(err)
	}
	unknown, err := m.Get(ctx)
	if err != nil || unknown.Enabled || unknown.Phase != "stop_unknown" {
		t.Fatal(unknown, err)
	}
	op.Action = "plugins.uninstall"
	op.TaskID = "uninstall-unknown"
	op.ExpectedRevision = unknown.Revision
	if _, err = m.Uninstall(ctx, op); err != ErrPackageRejected {
		t.Fatal("unknown stop claimed uninstalled", err)
	}
	file, err := m.vault.root.OpenFile(digest+"/payload", os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("tampered"))
	_ = file.Close()
	if _, err = m.vault.Load(ctx, digest, mustTrust(t, m, ctx)); err != ErrPackageRejected {
		t.Fatal("tampered file trusted", err)
	}
}
func mustTrust(t *testing.T, m *LifecycleManager, ctx context.Context) PackageTrust {
	t.Helper()
	trust, err := m.trust(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return trust
}
func TestPluginUpgradeRequiresNewerVersionAndExactNewConsent(t *testing.T) {
	ctx := context.Background()
	m, _, _, _, _, _, _ := lifecycleFixture(t)
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	source := "https://source.invalid/reviewed-fork"
	trust := PackageTrust{Keys: map[string]ed25519.PublicKey{"upgrade-key": pub}, Sources: map[string]bool{source: true}, Capabilities: map[string]bool{"xray.read": true, "xray.update": true}}
	m.trust = func(context.Context) (PackageTrust, error) { return trust, nil }
	payload := []byte("signed opaque upgrade fixture")
	hash := sha256.Sum256(payload)
	bundle := func(version string, caps []string) ([]byte, []byte, string) {
		manifest := PackageManifest{Format: 1, ID: "xray-manager", Version: version, Source: source, PayloadBytes: len(payload), PayloadSHA256: hex.EncodeToString(hash[:]), Capabilities: caps}
		raw, _ := json.Marshal(manifest)
		sig := ed25519.Sign(key, append([]byte("serverfleet.plugin.v1\x00"), raw...))
		return raw, sig, packageIdentity(raw, sig, "upgrade-key")
	}
	raw, sig, id := bundle("0.9.0", []string{"xray.read"})
	op := LifecycleOperation{ActorID: "admin", TaskID: "install-base", Action: "plugins.install", Digest: id, ConsentedCapabilities: []string{"xray.read"}}
	first, err := m.Install(ctx, op, raw, sig, payload, "upgrade-key")
	if err != nil {
		t.Fatal(err)
	}
	raw, sig, id = bundle("0.8.9", []string{"xray.read"})
	op.Action = "plugins.upgrade"
	op.TaskID = "older-version"
	op.ExpectedRevision = first.Revision
	op.Digest = id
	if _, err = m.Install(ctx, op, raw, sig, payload, "upgrade-key"); err != ErrPackageRejected {
		t.Fatal("downgrade presented as upgrade", err)
	}
	raw, sig, id = bundle("0.9.1", []string{"xray.read", "xray.update"})
	op.TaskID = "new-capability"
	op.Digest = id
	if _, err = m.Install(ctx, op, raw, sig, payload, "upgrade-key"); err != ErrPackageRejected {
		t.Fatal("new privilege without new consent", err)
	}
	op.ConsentedCapabilities = []string{"xray.read", "xray.update"}
	next, err := m.Install(ctx, op, raw, sig, payload, "upgrade-key")
	if err != nil || next.Version != "0.9.1" || next.Enabled || next.Revision <= first.Revision {
		t.Fatal(next, err)
	}
}
