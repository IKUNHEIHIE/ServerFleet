package updates

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

var ErrUpdateBusy = errors.New("updates: installation reserved; reconcile before another attempt")
var ErrHealthUnhealthy = errors.New("updates: observed unhealthy service")

// InstallIntent is frozen by the sole task service. Previous is trusted installed
// provenance, not browser-supplied metadata. Rollback is limited to this exact
// verified backup, and only a positively observed health failure permits it.
type InstallIntent struct {
	TaskID, ExecutionID string
	Previous            ReleaseManifest
	AllowRollback       bool
}

type HealthEvidence struct {
	ExecutionID, Component, Version, SHA256, ProcessID string
	StartedAt, ObservedAt                              time.Time
}

type InstallReceipt struct {
	Intent        InstallIntent
	Attempted     ReleaseManifest
	Current       ReleaseManifest
	State         string // installed or rolled_back; neither means current live health
	Health        HealthEvidence
	AuditRecordID string
}

// All hooks are program-registered service adapters, never request callbacks.
// Gate must enforce current task/approval/trust and phase-specific authority.
// Stop/Start must observe actual service state and honor ctx. Health must verify
// process identity, build, readiness AND storage/migration compatibility. Return
// ErrHealthUnhealthy only for a KNOWN failure, not timeout or missing evidence.
// Commit durably updates installed provenance, audit and the maximum attempted
// release-sequence floor in one transaction, including on rollback. A failed or
// ambiguous commit must NOT be retried by this component.
type InstallHooks struct {
	Gate   func(context.Context, InstallIntent, ReleaseManifest, string) error
	Stop   func(context.Context, ReleaseManifest) error
	Start  func(context.Context, ReleaseManifest) error
	Health func(context.Context, ReleaseManifest) (HealthEvidence, error)
	// Required when rollback is allowed. Check actual storage compatibility /
	// migration state BEFORE an old binary is restored or started. An operator's
	// AllowRollback flag alone never proves that rollback is safe.
	RollbackSafe func(context.Context, ReleaseManifest, ReleaseManifest) error
	Commit       func(context.Context, InstallReceipt) (string, error)
}

// SlotInstaller owns ONE private, dedicated directory with a regular "active"
// executable. No arbitrary target pathname, shell, HTTP endpoint or auto-resume.
// The owning supervisor must serialize other writers and keep state outside DB
// restores. Linux rename is atomic; Windows tests prove logical behavior only.
type SlotInstaller struct {
	mu   sync.Mutex
	root *os.Root
}

type installRecord struct {
	Format    int
	Intent    InstallIntent
	Attempted ReleaseManifest
}

func installID(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func installedManifest(m ReleaseManifest) bool {
	h, err := hex.DecodeString(m.SHA256)
	return err == nil && len(h) == 32 && hex.EncodeToString(h) == m.SHA256 && m.Format == 1 && (m.Component == "controller" || m.Component == "agent") && versionPattern.MatchString(m.Version) && m.Sequence > 0 && m.OS == "linux" && (m.Architecture == "amd64" || m.Architecture == "arm64") && m.Bytes > 0 && m.Bytes <= 256<<20
}

func NewSlotInstaller(directory string) (*SlotInstaller, error) {
	if !filepath.IsAbs(directory) {
		return nil, ErrArtifactRejected
	}
	r, err := os.OpenRoot(directory)
	if err != nil {
		return nil, ErrArtifactRejected
	}
	info, err := r.Stat(".")
	if err != nil || !info.IsDir() || runtime.GOOS == "linux" && info.Mode().Perm()&0077 != 0 {
		_ = r.Close()
		return nil, ErrArtifactRejected
	}
	return &SlotInstaller{root: r}, nil
}

func (s *SlotInstaller) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.root == nil {
		return nil
	}
	err := s.root.Close()
	s.root = nil
	return err
}

// Regular files only: os.Root itself permits in-root symlinks. Private directory
// ownership is still required; this is not isolation from a hostile same-UID writer.
func (s *SlotInstaller) openRegular(name string, max int64) (*os.File, error) {
	before, err := s.root.Lstat(name)
	if err != nil || !before.Mode().IsRegular() || before.Size() > max {
		return nil, ErrArtifactRejected
	}
	f, err := s.root.Open(name)
	if err != nil {
		return nil, ErrArtifactRejected
	}
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() || after.Size() != before.Size() {
		_ = f.Close()
		return nil, ErrArtifactRejected
	}
	return f, nil
}

func (s *SlotInstaller) verifyFile(ctx context.Context, name string, m ReleaseManifest) error {
	f, err := s.openRegular(name, m.Bytes)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(contextReader{ctx, f}, m.Bytes+1))
	if err != nil || n != m.Bytes || hex.EncodeToString(h.Sum(nil)) != m.SHA256 {
		return ErrArtifactRejected
	}
	return nil
}

func (s *SlotInstaller) writeRecord(name string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil || len(raw) > 16<<10 {
		return ErrArtifactRejected
	}
	f, err := s.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(raw)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil || syncStagingDirectory(s.root, filepath.ToSlash(filepath.Dir(name))) != nil {
		return ErrArtifactRejected
	}
	return nil
}

func (s *SlotInstaller) copyFile(ctx context.Context, name string, source io.Reader, m ReleaseManifest) error {
	f, err := s.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if err != nil {
		return ErrArtifactRejected
	}
	h := sha256.New()
	n, writeErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(contextReader{ctx, source}, m.Bytes+1))
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || n != m.Bytes || hex.EncodeToString(h.Sum(nil)) != m.SHA256 || syncErr != nil || closeErr != nil || syncStagingDirectory(s.root, filepath.ToSlash(filepath.Dir(name))) != nil {
		return ErrArtifactRejected
	}
	return s.verifyFile(ctx, name, m)
}

func validHealth(h HealthEvidence, m ReleaseManifest, execution string, since time.Time) bool {
	now := time.Now()
	return h.ExecutionID == execution && h.Component == m.Component && h.Version == m.Version && h.SHA256 == m.SHA256 && installID(h.ProcessID) && !h.StartedAt.Before(since) && !h.ObservedAt.Before(h.StartedAt) && !h.ObservedAt.After(now) && now.Sub(h.ObservedAt) <= 30*time.Second
}

// Apply permanently reserves executionId BEFORE consuming the staged object or
// stopping a service. Any incomplete reservation remains UNKNOWN across reopen;
// no cleanup, repeated start, automatic rollback or second apply is inferred.
func (s *SlotInstaller) Apply(ctx context.Context, staged *StagedArtifact, policy ReleasePolicy, intent InstallIntent, hooks InstallHooks) (receipt InstallReceipt, result error) {
	if s == nil || ctx == nil || ctx.Err() != nil || staged == nil || !installID(intent.TaskID) || !installID(intent.ExecutionID) || !installedManifest(intent.Previous) || hooks.Gate == nil || hooks.Stop == nil || hooks.Start == nil || hooks.Health == nil || hooks.Commit == nil || intent.AllowRollback && hooks.RollbackSafe == nil {
		return receipt, ErrArtifactRejected
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() {
		if recover() != nil {
			receipt = InstallReceipt{}
			result = ErrApplyUnknown
		}
	}()
	if s.root == nil {
		return receipt, ErrArtifactRejected
	}
	m := staged.Manifest()
	if !installedManifest(m) || m.Component != intent.Previous.Component || m.Architecture != intent.Previous.Architecture || m.Sequence <= intent.Previous.Sequence || intent.Previous.Sequence > policy.MinimumSequence || policy.MinimumSequence >= m.Sequence || s.verifyFile(ctx, "active", intent.Previous) != nil || hooks.Gate(ctx, intent, m, "admit") != nil {
		return receipt, ErrArtifactRejected
	}
	record := installRecord{1, intent, m}
	dir := "update-" + intent.ExecutionID
	if _, err := s.root.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		return receipt, ErrArtifactConsumed
	}
	// A lock is never automatically removed after an error or a crash.
	if err := s.writeRecord("update-lock.json", record); err != nil {
		if errors.Is(err, os.ErrExist) {
			return receipt, ErrUpdateBusy
		}
		return receipt, ErrApplyUnknown
	}
	if s.root.Mkdir(dir, 0700) != nil || syncStagingDirectory(s.root, ".") != nil || s.writeRecord(dir+"/intent.json", record) != nil {
		return receipt, ErrApplyUnknown
	}
	gate := func(phase string) error {
		if ctx.Err() != nil || hooks.Gate(ctx, intent, m, phase) != nil {
			return ErrApplyUnknown
		}
		return nil
	}
	mark := func(phase string) error {
		if gate(phase) != nil {
			return ErrApplyUnknown
		}
		return s.writeRecord(dir+"/"+phase+".json", record)
	}
	if staged.WithVerified(ctx, policy, func(context.Context, ReleaseManifest) error { return gate("stage") }, func(ctx context.Context, _ ReleaseManifest, reader io.Reader) error {
		return s.copyFile(ctx, dir+"/candidate", reader, m)
	}) != nil {
		return receipt, ErrApplyUnknown
	}
	old, err := s.openRegular("active", intent.Previous.Bytes)
	if err != nil {
		return receipt, ErrApplyUnknown
	}
	err = s.copyFile(ctx, dir+"/previous", old, intent.Previous)
	closeErr := old.Close()
	if err != nil || closeErr != nil || mark("prepared") != nil || s.verifyFile(ctx, "active", intent.Previous) != nil || mark("stopping") != nil {
		return receipt, ErrApplyUnknown
	}
	if hooks.Stop(ctx, intent.Previous) != nil || mark("stopped") != nil || s.verifyFile(ctx, dir+"/candidate", m) != nil || s.verifyFile(ctx, "active", intent.Previous) != nil || mark("switching") != nil {
		return receipt, ErrApplyUnknown
	}
	if s.root.Rename(dir+"/candidate", "active") != nil || syncStagingDirectory(s.root, ".") != nil || syncStagingDirectory(s.root, dir) != nil || s.verifyFile(ctx, "active", m) != nil || mark("switched") != nil {
		return receipt, ErrApplyUnknown
	}
	started := time.Now()
	if mark("starting") != nil || hooks.Start(ctx, m) != nil || gate("health") != nil {
		return receipt, ErrApplyUnknown
	}
	health, err := hooks.Health(ctx, m)
	current, state := m, "installed"
	if err != nil {
		// A missing/timeout/ambiguous health result cannot justify more effects.
		if !errors.Is(err, ErrHealthUnhealthy) || !intent.AllowRollback || gate("rollback-compatibility") != nil || hooks.RollbackSafe(ctx, intent.Previous, m) != nil || mark("rollback-stopping") != nil || hooks.Stop(ctx, m) != nil || s.verifyFile(ctx, "active", m) != nil || s.verifyFile(ctx, dir+"/previous", intent.Previous) != nil || mark("rollback-switching") != nil {
			return InstallReceipt{}, ErrApplyUnknown
		}
		if s.root.Rename(dir+"/previous", "active") != nil || syncStagingDirectory(s.root, ".") != nil || syncStagingDirectory(s.root, dir) != nil || s.verifyFile(ctx, "active", intent.Previous) != nil {
			return InstallReceipt{}, ErrApplyUnknown
		}
		started = time.Now()
		if mark("rollback-starting") != nil || hooks.Start(ctx, intent.Previous) != nil || gate("rollback-health") != nil {
			return InstallReceipt{}, ErrApplyUnknown
		}
		health, err = hooks.Health(ctx, intent.Previous)
		current, state = intent.Previous, "rolled_back"
	}
	if err != nil || !validHealth(health, current, intent.ExecutionID, started) || s.verifyFile(ctx, "active", current) != nil || mark("committing") != nil {
		return InstallReceipt{}, ErrApplyUnknown
	}
	receipt = InstallReceipt{Intent: intent, Attempted: m, Current: current, State: state, Health: health}
	auditID, err := hooks.Commit(ctx, receipt)
	if err != nil || !installID(auditID) || ctx.Err() != nil || s.verifyFile(ctx, "active", current) != nil {
		return InstallReceipt{}, ErrApplyUnknown
	}
	receipt.AuditRecordID = auditID
	if s.writeRecord(dir+"/receipt.json", receipt) != nil || s.root.Remove("update-lock.json") != nil || syncStagingDirectory(s.root, ".") != nil {
		return InstallReceipt{}, ErrApplyUnknown
	}
	return receipt, nil
}

// Receipt is a historical commit receipt, NEVER a live health assertion. Missing
// or damaged records remain unknown. Reopening does not resume an operation.
func (s *SlotInstaller) Receipt(executionID string) (InstallReceipt, error) {
	var r InstallReceipt
	if s == nil || !installID(executionID) {
		return r, ErrArtifactRejected
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.root == nil {
		return r, ErrArtifactRejected
	}
	read := func(name string, v any) error {
		f, err := s.openRegular(name, 16<<10)
		if err != nil {
			return err
		}
		defer f.Close()
		raw, err := io.ReadAll(io.LimitReader(f, (16<<10)+1))
		if err != nil || len(raw) > 16<<10 || json.Unmarshal(raw, v) != nil {
			return ErrApplyUnknown
		}
		canonical, err := json.Marshal(v)
		if err != nil || !bytes.Equal(canonical, raw) {
			return ErrApplyUnknown
		}
		return nil
	}
	var record installRecord
	dir := "update-" + executionID
	if read(dir+"/intent.json", &record) != nil || record.Format != 1 || record.Intent.ExecutionID != executionID || !installID(record.Intent.TaskID) || !installedManifest(record.Intent.Previous) || !installedManifest(record.Attempted) || read(dir+"/receipt.json", &r) != nil || r.Intent != record.Intent || r.Attempted != record.Attempted || !installedManifest(r.Current) || !installID(r.AuditRecordID) || r.State != "installed" && r.State != "rolled_back" || r.State == "installed" && r.Current != r.Attempted || r.State == "rolled_back" && (!r.Intent.AllowRollback || r.Current != r.Intent.Previous) || r.Health.ExecutionID != executionID || r.Health.Component != r.Current.Component || r.Health.Version != r.Current.Version || r.Health.SHA256 != r.Current.SHA256 || !installID(r.Health.ProcessID) || r.Health.StartedAt.IsZero() || r.Health.ObservedAt.Before(r.Health.StartedAt) {
		return InstallReceipt{}, ErrApplyUnknown
	}
	return r, nil
}
