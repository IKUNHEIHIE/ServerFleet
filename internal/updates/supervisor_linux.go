package updates

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type OwnedSupervisorConfig struct {
	Directory, ExecutionID string
	Arguments, Environment []string
	// Same current task/approval gate used by InstallHooks; no default grant.
	Gate func(context.Context, ReleaseManifest, string) error
	// Must inspect actual application readiness/version/storage compatibility,
	// bound to this PID/start/nonce. Image identity is independently checked here.
	Ready func(context.Context, ReleaseManifest, OwnedProcessObservation) error
}
type ownedReleaseProcess struct {
	cmd         *exec.Cmd
	manifest    ReleaseManifest
	started     time.Time
	nonce       string
	done        chan struct{}
	reservation string
}
type OwnedProcessSupervisor struct {
	mu      sync.Mutex
	root    *os.Root
	cfg     OwnedSupervisorConfig
	process *ownedReleaseProcess
}

func (*OwnedProcessSupervisor) String() string     { return "updates.OwnedProcessSupervisor{private}" }
func (s *OwnedProcessSupervisor) GoString() string { return s.String() }

// Owns only children started by THIS object, in a dedicated non-root directory.
// It cannot attach to/stop systemd units, arbitrary PIDs or any existing service.
// Application sandbox/cgroup/child-process policy needs separate admission.
func NewOwnedProcessSupervisor(cfg OwnedSupervisorConfig) (*OwnedProcessSupervisor, error) {
	if os.Geteuid() == 0 || !filepath.IsAbs(cfg.Directory) || !installID(cfg.ExecutionID) || cfg.Gate == nil || cfg.Ready == nil || len(cfg.Arguments) > 64 || len(cfg.Environment) > 64 {
		return nil, ErrArtifactRejected
	}
	for _, s := range cfg.Arguments {
		if len(s) > 4096 || strings.ContainsRune(s, 0) {
			return nil, ErrArtifactRejected
		}
	}
	for _, s := range cfg.Environment {
		if len(s) > 4096 || !strings.Contains(s, "=") || strings.ContainsRune(s, 0) || strings.HasPrefix(s, "SF_SUPERVISOR_NONCE=") {
			return nil, ErrArtifactRejected
		}
	}
	info, err := os.Lstat(cfg.Directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return nil, ErrArtifactRejected
	}
	root, err := os.OpenRoot(cfg.Directory)
	if err != nil {
		return nil, ErrArtifactRejected
	}
	cfg.Arguments = append([]string{}, cfg.Arguments...)
	cfg.Environment = append([]string{}, cfg.Environment...)
	return &OwnedProcessSupervisor{root: root, cfg: cfg}, nil
}
func (s *OwnedProcessSupervisor) Start(ctx context.Context, m ReleaseManifest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx == nil || ctx.Err() != nil || s.root == nil || s.process != nil || !installedManifest(m) || s.cfg.Gate(ctx, m, "supervisor-start") != nil {
		return ErrApplyUnknown
	}
	info, err := s.root.Lstat("active")
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0700 {
		return ErrArtifactRejected
	}
	file, err := s.root.Open("active")
	if err != nil {
		return ErrArtifactRejected
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || verifyOwnedProcessImage(ctx, file, m) != nil {
		return ErrArtifactRejected
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return ErrApplyUnknown
	}
	// Execute the verified fd, not a pathname that can be swapped after hashing.
	cmd := exec.Command("/proc/self/fd/3", s.cfg.Arguments...)
	cmd.ExtraFiles = []*os.File{file}
	cmd.Dir = s.cfg.Directory
	text := hex.EncodeToString(nonce[:])
	reservation, _ := json.Marshal(struct {
		Version     int
		ExecutionID string
		Manifest    ReleaseManifest
		NonceSHA256 string
	}{1, s.cfg.ExecutionID, m, func() string { h := sha256.Sum256(nonce[:]); return hex.EncodeToString(h[:]) }()})
	intent, err := s.root.OpenFile("supervisor-reservation.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return ErrUpdateBusy
	}
	_, writeErr := intent.Write(reservation)
	syncErr := intent.Sync()
	closeErr := intent.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil || syncStagingDirectory(s.root, ".") != nil {
		return ErrApplyUnknown
	}
	cmd.Env = append(append([]string{}, s.cfg.Environment...), "SF_SUPERVISOR_NONCE="+text)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	cmd.WaitDelay = time.Second
	if err = cmd.Start(); err != nil {
		return ErrApplyUnknown
	}
	h := sha256.Sum256(reservation)
	p := &ownedReleaseProcess{cmd: cmd, manifest: m, started: time.Now(), nonce: text, done: make(chan struct{}), reservation: hex.EncodeToString(h[:])}
	s.process = p
	go func() { _ = cmd.Wait(); close(p.done) }()
	if ctx.Err() != nil {
		_ = cmd.Process.Kill()
		return ErrApplyUnknown
	}
	return nil
}
func verifyOwnedProcessImage(ctx context.Context, file *os.File, m ReleaseManifest) error {
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != m.Bytes {
		return ErrArtifactRejected
	}
	h := sha256.New()
	n, err := io.Copy(h, contextReader{ctx, io.NewSectionReader(file, 0, m.Bytes+1)})
	if err != nil || n != m.Bytes || hex.EncodeToString(h.Sum(nil)) != m.SHA256 {
		return ErrArtifactRejected
	}
	return nil
}
func (s *OwnedProcessSupervisor) Health(ctx context.Context, m ReleaseManifest) (HealthEvidence, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.process
	if ctx == nil || ctx.Err() != nil || p == nil || p.manifest != m || s.cfg.Gate(ctx, m, "supervisor-health") != nil {
		return HealthEvidence{}, ErrApplyUnknown
	}
	select {
	case <-p.done:
		return HealthEvidence{}, ErrHealthUnhealthy
	default:
	}
	file, err := os.Open("/proc/" + strconv.Itoa(p.cmd.Process.Pid) + "/exe")
	if err != nil {
		return HealthEvidence{}, ErrApplyUnknown
	}
	defer file.Close()
	if verifyOwnedProcessImage(ctx, file, m) != nil {
		return HealthEvidence{}, ErrApplyUnknown
	}
	check, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	readyErr := s.cfg.Ready(check, m, OwnedProcessObservation{p.cmd.Process.Pid, p.nonce, p.started})
	if check.Err() != nil || s.cfg.Gate(ctx, m, "supervisor-health-complete") != nil {
		return HealthEvidence{}, ErrApplyUnknown
	}
	if readyErr != nil {
		if errors.Is(readyErr, ErrHealthUnhealthy) {
			return HealthEvidence{}, ErrHealthUnhealthy
		}
		return HealthEvidence{}, ErrApplyUnknown
	}
	select {
	case <-p.done:
		return HealthEvidence{}, ErrHealthUnhealthy
	default:
	}
	if p.cmd.Process.Signal(syscall.Signal(0)) != nil {
		return HealthEvidence{}, ErrHealthUnhealthy
	}
	return HealthEvidence{ExecutionID: s.cfg.ExecutionID, Component: m.Component, Version: m.Version, SHA256: m.SHA256, ProcessID: strconv.Itoa(p.cmd.Process.Pid), StartedAt: p.started, ObservedAt: time.Now()}, nil
}
func (s *OwnedProcessSupervisor) Stop(ctx context.Context, m ReleaseManifest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopLocked(ctx, m)
}
func (s *OwnedProcessSupervisor) stopLocked(ctx context.Context, m ReleaseManifest) error {
	p := s.process
	if ctx == nil || ctx.Err() != nil || p == nil || p.manifest != m || s.cfg.Gate(ctx, m, "supervisor-stop") != nil {
		return ErrApplyUnknown
	}
	select {
	case <-p.done:
		return s.releaseOwnedReservation(p)
	default:
	}
	if p.cmd.Process.Signal(syscall.SIGTERM) != nil {
		return ErrApplyUnknown
	}
	grace := time.NewTimer(2 * time.Second)
	defer grace.Stop()
	select {
	case <-p.done:
		return s.releaseOwnedReservation(p)
	case <-ctx.Done():
		return ErrApplyUnknown
	case <-grace.C:
	}
	if p.cmd.Process.Kill() != nil {
		return ErrApplyUnknown
	}
	select {
	case <-p.done:
		return s.releaseOwnedReservation(p)
	case <-ctx.Done():
		return ErrApplyUnknown
	case <-time.After(time.Second):
		return ErrApplyUnknown
	}
}

func (s *OwnedProcessSupervisor) releaseOwnedReservation(p *ownedReleaseProcess) error {
	f, err := s.root.Open("supervisor-reservation.json")
	if err != nil {
		return ErrApplyUnknown
	}
	raw, err := io.ReadAll(io.LimitReader(f, 4097))
	f.Close()
	h := sha256.Sum256(raw)
	if err != nil || len(raw) > 4096 || hex.EncodeToString(h[:]) != p.reservation {
		return ErrApplyUnknown
	}
	if s.root.Remove("supervisor-reservation.json") != nil || syncStagingDirectory(s.root, ".") != nil {
		return ErrApplyUnknown
	}
	s.process = nil
	return nil
}

// Close requires fresh stop authority and a live context. On error it retains
// ownership metadata; callers must reconcile/stop this child, never a foreign PID.
func (s *OwnedProcessSupervisor) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.process != nil {
		if err := s.stopLocked(ctx, s.process.manifest); err != nil {
			return err
		}
	}
	if s.root != nil {
		err := s.root.Close()
		s.root = nil
		return err
	}
	return nil
}

// BindExecution changes the receipt binding for the next owned transaction; it
// never attaches to another process or alters its ownership reservation.
func (s *OwnedProcessSupervisor) BindExecution(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !installID(id) || s.root == nil {
		return ErrArtifactRejected
	}
	s.cfg.ExecutionID = id
	return nil
}
