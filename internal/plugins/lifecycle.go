package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"
)

var ErrLifecycleUnknown = errors.New("plugins: runtime outcome unknown; reconcile before retry")
var ErrLifecycleReplay = errors.New("plugins: lifecycle operation already admitted")

type LifecycleStore interface {
	View(context.Context, func([]byte) error) error
	Update(context.Context, func([]byte) ([]byte, error)) error
}
type LifecycleOperation struct {
	ActorID, TaskID, Action, Digest string
	RuntimeID                       string
	Capability                      string
	ExpectedRevision                uint64
	ConsentedCapabilities           []string
}
type LifecycleGate func(context.Context, LifecycleOperation) error
type PackageTrustReader func(context.Context) (PackageTrust, error)

// Runtime must be supplied ONLY after profile/ABI/isolation admission. Nil
// allows verified disabled storage, never enables executable plugin capability.
// Start/Stop/Ready must honor ctx and expose no generic shell/SSH/DB/raw network.
type AdmittedRuntime interface {
	Start(context.Context, RuntimeInstance, PackageContent) error
	Stop(context.Context, RuntimeInstance) error
	Ready(context.Context, RuntimeInstance) error
}
type RuntimeInstance struct {
	Descriptor Descriptor
	ID         string
}
type Installation struct {
	ID        string `json:"id"`
	Version   string `json:"version"`
	Source    string `json:"source"`
	Digest    string `json:"digest"`
	Installed bool   `json:"installed"`
	Enabled   bool   `json:"enabled"`
	Phase     string `json:"phase"`
	Revision  uint64 `json:"revision"`
	RuntimeID string `json:"runtimeId,omitempty"`
}
type lifecycleAudit struct {
	ActorID, TaskID, Action, Result string
	At                              time.Time
}
type lifecycleState struct {
	Version      int
	Sequence     uint64
	Installation Installation
	Operations   map[string]string
	Outbox       []lifecycleAudit
}
type LifecycleManager struct {
	store   LifecycleStore
	vault   *PackageVault
	trust   PackageTrustReader
	gate    LifecycleGate
	runtime AdmittedRuntime
}

func NewLifecycleManager(store LifecycleStore, vault *PackageVault, trust PackageTrustReader, gate LifecycleGate, runtime AdmittedRuntime) (*LifecycleManager, error) {
	if store == nil || vault == nil || trust == nil || gate == nil {
		return nil, ErrPackageRejected
	}
	return &LifecycleManager{store, vault, trust, gate, runtime}, nil
}
func lifecycleID(id string) bool {
	return id != "" && len(id) <= 128 && !strings.ContainsAny(id, "*?[] \t\r\n\x00")
}
func decodeLifecycle(raw []byte) (*lifecycleState, error) {
	st := &lifecycleState{}
	if len(raw) > 4<<20 || len(raw) > 0 && (json.Unmarshal(raw, st) != nil || st.Version != 1) {
		return nil, ErrPackageRejected
	}
	st.Version = 1
	if st.Operations == nil {
		st.Operations = map[string]string{}
	}
	if len(st.Operations) > 10000 || len(st.Outbox) > 10000 {
		return nil, ErrPackageRejected
	}
	i := st.Installation
	if i.ID != "" && (i.ID != "xray-manager" || i.Revision == 0 || i.Revision > st.Sequence || i.Enabled && !i.Installed) {
		return nil, ErrPackageRejected
	}
	if i.ID != "" {
		if !pluginVersion.MatchString(i.Version) || len(i.Digest) != 64 {
			return nil, ErrPackageRejected
		}
		switch i.Phase {
		case "installed_disabled", "disabled", "uninstalled", "starting", "stopping", "runtime_unknown", "stop_unknown", "enabled":
		default:
			return nil, ErrPackageRejected
		}
		if i.Enabled && (i.Phase != "enabled" || !lifecycleID(i.RuntimeID)) {
			return nil, ErrPackageRejected
		}
	}
	for id, state := range st.Operations {
		if !lifecycleID(id) || state != "unknown" && state != "succeeded" {
			return nil, ErrPackageRejected
		}
	}
	return st, nil
}
func (m *LifecycleManager) mutate(ctx context.Context, fn func(*lifecycleState) error) error {
	if m == nil || m.store == nil || ctx == nil || ctx.Err() != nil {
		return ErrPackageRejected
	}
	err := m.store.Update(ctx, func(raw []byte) ([]byte, error) {
		st, err := decodeLifecycle(raw)
		if err != nil {
			return nil, err
		}
		if err = fn(st); err != nil {
			return nil, err
		}
		raw, err = json.Marshal(st)
		if err != nil || len(raw) > 4<<20 || ctx.Err() != nil {
			return nil, ErrPackageRejected
		}
		return raw, nil
	})
	if errors.Is(err, ErrLifecycleReplay) {
		return ErrLifecycleReplay
	}
	if err != nil || ctx.Err() != nil {
		return ErrPackageRejected
	}
	return nil
}
func (m *LifecycleManager) admit(ctx context.Context, op LifecycleOperation, action string) error {
	if m == nil || m.gate == nil || ctx == nil || ctx.Err() != nil || op.Action != action || !lifecycleID(op.ActorID) || !lifecycleID(op.TaskID) || m.gate(ctx, op) != nil {
		return ErrPackageRejected
	}
	return nil
}
func auditLifecycle(st *lifecycleState, op LifecycleOperation, result string) error {
	if len(st.Outbox) >= 10000 {
		return ErrPackageRejected
	}
	action := op.Action
	if result == "prepared" {
		action += ".intent"
		result = "success"
	}
	st.Outbox = append(st.Outbox, lifecycleAudit{op.ActorID, op.TaskID, action, result, time.Now().UTC()})
	return nil
}
func advanceInstallation(st *lifecycleState) error {
	st.Sequence++
	if st.Sequence == 0 {
		return ErrPackageRejected
	}
	st.Installation.Revision = st.Sequence
	return nil
}
func claimLifecycle(st *lifecycleState, op LifecycleOperation) error {
	if _, ok := st.Operations[op.TaskID]; ok {
		return ErrLifecycleReplay
	}
	if len(st.Operations) >= 10000 {
		return ErrPackageRejected
	}
	if st.Installation.Revision != op.ExpectedRevision {
		return ErrPackageRejected
	}
	st.Operations[op.TaskID] = "unknown"
	return nil
}
func newerPlugin(next, old string) bool {
	a, b := strings.Split(next, "."), strings.Split(old, ".")
	if len(a) != 3 || len(b) != 3 {
		return false
	}
	for i := range a {
		x, e := strconv.ParseUint(a[i], 10, 32)
		y, f := strconv.ParseUint(b[i], 10, 32)
		if e != nil || f != nil {
			return false
		}
		if x != y {
			return x > y
		}
	}
	return false
}
func (m *LifecycleManager) Install(ctx context.Context, op LifecycleOperation, raw, signature, payload []byte, keyID string) (Installation, error) {
	if op.Action != "plugins.install" && op.Action != "plugins.upgrade" {
		return Installation{}, ErrPackageRejected
	}
	if err := m.admit(ctx, op, op.Action); err != nil {
		return Installation{}, err
	}
	trust, err := m.trust(ctx)
	if err != nil {
		return Installation{}, ErrPackageRejected
	}
	manifest, err := VerifyPackage(raw, signature, payload, keyID, trust)
	if err != nil {
		return Installation{}, err
	}
	consent, caps := slices.Clone(op.ConsentedCapabilities), slices.Clone(manifest.Capabilities)
	slices.Sort(consent)
	slices.Sort(caps)
	if !slices.Equal(consent, caps) {
		return Installation{}, ErrPackageRejected
	}
	id, err := m.vault.Put(ctx, raw, signature, payload, keyID, trust)
	if err != nil || id != op.Digest {
		return Installation{}, ErrPackageRejected
	}
	verified, err := m.vault.Load(ctx, id, trust)
	if err != nil {
		return Installation{}, err
	}
	manifest = verified.Manifest
	caps = slices.Clone(manifest.Capabilities)
	slices.Sort(caps)
	if !slices.Equal(consent, caps) {
		return Installation{}, ErrPackageRejected
	}
	var out Installation
	err = m.mutate(ctx, func(st *lifecycleState) error {
		if err := claimLifecycle(st, op); err != nil {
			return err
		}
		if st.Installation.Enabled || st.Installation.Phase == "starting" {
			return ErrPackageRejected
		}
		if st.Installation.Installed && st.Installation.Phase != "disabled" && st.Installation.Phase != "installed_disabled" {
			return ErrPackageRejected
		}
		if op.Action == "plugins.install" && st.Installation.Installed || op.Action == "plugins.upgrade" && (!st.Installation.Installed || !newerPlugin(manifest.Version, st.Installation.Version)) {
			return ErrPackageRejected
		}
		if m.admit(ctx, op, op.Action) != nil {
			return ErrPackageRejected
		}
		st.Installation = Installation{ID: manifest.ID, Version: manifest.Version, Source: manifest.Source, Digest: id, Installed: true, Enabled: false, Phase: "installed_disabled"}
		if advanceInstallation(st) != nil {
			return ErrPackageRejected
		}
		st.Operations[op.TaskID] = "succeeded"
		out = st.Installation
		return auditLifecycle(st, op, "success")
	})
	if err != nil {
		return Installation{}, err
	}
	return out, nil
}
func (m *LifecycleManager) Get(ctx context.Context) (Installation, error) {
	var out Installation
	if m == nil || m.store == nil || ctx == nil {
		return out, ErrPackageRejected
	}
	err := m.store.View(ctx, func(raw []byte) error {
		st, err := decodeLifecycle(raw)
		if err != nil {
			return err
		}
		out = st.Installation
		return nil
	})
	if err != nil || ctx.Err() != nil {
		return Installation{}, ErrPackageRejected
	}
	return out, nil
}
func runtimeInstance(i Installation) RuntimeInstance {
	return RuntimeInstance{Descriptor{i.ID, i.Version, i.Digest}, i.RuntimeID}
}
func (m *LifecycleManager) load(ctx context.Context, i Installation) (PackageContent, error) {
	trust, err := m.trust(ctx)
	if err != nil {
		return PackageContent{}, ErrPackageRejected
	}
	content, err := m.vault.Load(ctx, i.Digest, trust)
	if err != nil || content.Manifest.ID != i.ID || content.Manifest.Version != i.Version || content.Manifest.Source != i.Source {
		return PackageContent{}, ErrPackageRejected
	}
	return content, nil
}
func (m *LifecycleManager) Enable(ctx context.Context, op LifecycleOperation) (Installation, error) {
	if m == nil || m.runtime == nil {
		return Installation{}, ErrPackageRejected
	}
	if err := m.admit(ctx, op, "plugins.enable"); err != nil {
		return Installation{}, err
	}
	var starting Installation
	err := m.mutate(ctx, func(st *lifecycleState) error {
		if err := claimLifecycle(st, op); err != nil {
			return err
		}
		i := st.Installation
		if !i.Installed || i.Enabled || (i.Phase != "disabled" && i.Phase != "installed_disabled") || i.Digest != op.Digest {
			return ErrPackageRejected
		}
		st.Installation.Enabled = false
		st.Installation.Phase = "starting"
		st.Installation.RuntimeID = op.TaskID
		if advanceInstallation(st) != nil {
			return ErrPackageRejected
		}
		starting = st.Installation
		return auditLifecycle(st, op, "prepared")
	})
	if err != nil {
		return Installation{}, err
	}
	content, err := m.load(ctx, starting)
	if err != nil {
		m.unknown(ctx, op, "runtime_unknown", starting.Revision)
		return Installation{}, ErrLifecycleUnknown
	}
	if runtimeCall(ctx, func(ctx context.Context) error { return m.runtime.Start(ctx, runtimeInstance(starting), content) }) != nil || runtimeCall(ctx, func(ctx context.Context) error { return m.runtime.Ready(ctx, runtimeInstance(starting)) }) != nil || m.admit(ctx, op, "plugins.enable") != nil {
		m.stop(ctx, starting)
		m.unknown(ctx, op, "runtime_unknown", starting.Revision)
		return Installation{}, ErrLifecycleUnknown
	}
	var enabled Installation
	err = m.mutate(ctx, func(st *lifecycleState) error {
		if st.Installation.Revision != starting.Revision || st.Installation.Digest != starting.Digest || st.Installation.Phase != "starting" {
			return ErrPackageRejected
		}
		if m.admit(ctx, op, "plugins.enable") != nil {
			return ErrPackageRejected
		}
		st.Installation.Enabled = true
		st.Installation.Phase = "enabled"
		if advanceInstallation(st) != nil {
			return ErrPackageRejected
		}
		st.Operations[op.TaskID] = "succeeded"
		enabled = st.Installation
		return auditLifecycle(st, op, "success")
	})
	if err != nil {
		m.stop(ctx, starting)
		m.unknown(ctx, op, "runtime_unknown", starting.Revision)
		return Installation{}, ErrLifecycleUnknown
	}
	return enabled, nil
}
func (m *LifecycleManager) stop(ctx context.Context, i Installation) {
	if m.runtime == nil {
		return
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = runtimeCall(cleanup, func(ctx context.Context) error { return m.runtime.Stop(ctx, runtimeInstance(i)) })
}
func runtimeCall(ctx context.Context, fn func(context.Context) error) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrLifecycleUnknown
		}
	}()
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if fn(bounded) != nil || bounded.Err() != nil {
		return ErrLifecycleUnknown
	}
	return nil
}
func (m *LifecycleManager) unknown(ctx context.Context, op LifecycleOperation, phase string, expected uint64) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	_ = m.mutate(cleanup, func(st *lifecycleState) error {
		if st.Operations[op.TaskID] != "unknown" || st.Installation.Digest != op.Digest || st.Installation.Revision != expected {
			return ErrPackageRejected
		}
		st.Installation.Enabled = false
		st.Installation.Phase = phase
		if advanceInstallation(st) != nil {
			return ErrPackageRejected
		}
		return auditLifecycle(st, op, "failure")
	})
}
func (m *LifecycleManager) Disable(ctx context.Context, op LifecycleOperation) (Installation, error) {
	if err := m.admit(ctx, op, "plugins.disable"); err != nil {
		return Installation{}, err
	}
	var disabled Installation
	err := m.mutate(ctx, func(st *lifecycleState) error {
		if err := claimLifecycle(st, op); err != nil {
			return err
		}
		if !st.Installation.Installed || st.Installation.Digest != op.Digest {
			return ErrPackageRejected
		}
		st.Installation.Enabled = false
		st.Installation.Phase = "stopping"
		if advanceInstallation(st) != nil {
			return ErrPackageRejected
		}
		disabled = st.Installation
		return auditLifecycle(st, op, "prepared")
	})
	if err != nil {
		return Installation{}, err
	}
	if m.runtime == nil || runtimeCall(ctx, func(ctx context.Context) error { return m.runtime.Stop(ctx, runtimeInstance(disabled)) }) != nil {
		m.unknown(ctx, op, "stop_unknown", disabled.Revision)
		return disabled, ErrLifecycleUnknown
	}
	err = m.mutate(ctx, func(st *lifecycleState) error {
		if st.Installation.Revision != disabled.Revision {
			return ErrPackageRejected
		}
		st.Installation.Phase = "disabled"
		if advanceInstallation(st) != nil {
			return ErrPackageRejected
		}
		st.Operations[op.TaskID] = "succeeded"
		disabled = st.Installation
		return auditLifecycle(st, op, "success")
	})
	if err != nil {
		m.unknown(ctx, op, "stop_unknown", disabled.Revision)
		return disabled, ErrLifecycleUnknown
	}
	return disabled, nil
}
func (m *LifecycleManager) Uninstall(ctx context.Context, op LifecycleOperation) (Installation, error) {
	if err := m.admit(ctx, op, "plugins.uninstall"); err != nil {
		return Installation{}, err
	}
	var out Installation
	err := m.mutate(ctx, func(st *lifecycleState) error {
		if err := claimLifecycle(st, op); err != nil {
			return err
		}
		if !st.Installation.Installed || st.Installation.Enabled || st.Installation.Phase != "disabled" && st.Installation.Phase != "installed_disabled" || st.Installation.Digest != op.Digest {
			return ErrPackageRejected
		}
		st.Installation.Installed = false
		st.Installation.Enabled = false
		st.Installation.Phase = "uninstalled"
		if advanceInstallation(st) != nil {
			return ErrPackageRejected
		}
		st.Operations[op.TaskID] = "succeeded"
		out = st.Installation
		return auditLifecycle(st, op, "success")
	})
	if err != nil {
		return Installation{}, err
	}
	return out, nil
}

// CheckStoredInvocation is admission only. The Host still submits through sole
// tasks with current user/AI scope/approval and typed schema; it cannot execute.
func (m *LifecycleManager) CheckStoredInvocation(ctx context.Context, op LifecycleOperation, action string) (PackageManifest, error) {
	if op.Capability != action || !lifecycleID(action) {
		return PackageManifest{}, ErrPackageRejected
	}
	if err := m.admit(ctx, op, "plugins.invoke"); err != nil {
		return PackageManifest{}, err
	}
	i, err := m.Get(ctx)
	if err != nil || !i.Enabled || i.Revision != op.ExpectedRevision || i.Digest != op.Digest || op.RuntimeID != i.RuntimeID || !lifecycleID(op.RuntimeID) || m.runtime == nil {
		return PackageManifest{}, ErrPackageRejected
	}
	content, err := m.load(ctx, i)
	if err != nil || CheckInvocation(i.Enabled, i.Version, i.Revision, op.ExpectedRevision, content.Manifest, action) != nil || runtimeCall(ctx, func(ctx context.Context) error { return m.runtime.Ready(ctx, runtimeInstance(i)) }) != nil {
		return PackageManifest{}, ErrPackageRejected
	}
	latest, err := m.Get(ctx)
	if err != nil || latest != i || m.admit(ctx, op, "plugins.invoke") != nil {
		return PackageManifest{}, ErrPackageRejected
	}
	return content.Manifest, nil
}
