package recovery

import (
	"bytes"
	"context"
	"crypto/rand"
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

var ErrQuarantined = errors.New("recovery: controller remains quarantined")

// RestoreIntent is prepared by the offline restore procedure, never by a flag
// supplied in a management HTTP request. The directory containing the latch
// MUST be outside the database/snapshot being restored and owned by this target.
type RestoreIntent struct {
	SnapshotSHA256        string
	ControllerID          string
	RootSPKISHA256        string
	MinTrustGeneration    uint64
	MinRevocationSequence uint64
}

type QuarantineHold struct {
	Version int
	ID      string
	RestoreIntent
}

// ReleaseClearance contains references/digests, never recovery keys or secrets.
// A well-formed value does not prove fencing: the mandatory trusted verifier
// must check the actual evidence and current reconciliation on EVERY gate.
type ReleaseClearance struct {
	HoldID, SnapshotSHA256, ControllerID, RootSPKISHA256 string
	TrustGeneration, RevocationSequence                  uint64
	FencingEvidenceSHA256, ReconciledStateSHA256         string
	AuditRecordID                                        string
}

type QuarantineVerifier struct {
	// Prepare may return only after durable reconciliation and audit commit.
	// It must verify offline authority, old-Controller isolation and fresh
	// credential/key epochs. It cannot obtain authority from the old backup.
	Prepare func(context.Context, QuarantineHold) (ReleaseClearance, error)
	// Current checks live authority/fencing and the restored database against
	// the bound receipt. No default verifier, claimed-fenced boolean or cache.
	Current func(context.Context, QuarantineHold, ReleaseClearance) error
}

type QuarantineLatch struct {
	mu       sync.Mutex
	root     *os.Root
	hold     QuarantineHold
	verifier QuarantineVerifier
}

func validDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	b, err := hex.DecodeString(s)
	return err == nil && hex.EncodeToString(b) == s
}
func validReference(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}
func validIntent(i RestoreIntent) bool {
	return validDigest(i.SnapshotSHA256) && validDigest(i.RootSPKISHA256) && validReference(i.ControllerID) && i.MinTrustGeneration > 0
}
func validHold(h QuarantineHold) bool {
	return h.Version == 1 && validDigest(h.ID) && validIntent(h.RestoreIntent)
}
func validClearance(h QuarantineHold, c ReleaseClearance) bool {
	return c.HoldID == h.ID && c.SnapshotSHA256 == h.SnapshotSHA256 && c.ControllerID == h.ControllerID && c.RootSPKISHA256 == h.RootSPKISHA256 && c.TrustGeneration >= h.MinTrustGeneration && c.RevocationSequence >= h.MinRevocationSequence && validDigest(c.FencingEvidenceSHA256) && validDigest(c.ReconciledStateSHA256) && validReference(c.AuditRecordID)
}
func openLatch(directory string) (*QuarantineLatch, error) {
	if !filepath.IsAbs(directory) {
		return nil, ErrRecoveryRejected
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, ErrRecoveryRejected
	}
	info, err := root.Stat(".")
	if err != nil || !info.IsDir() || runtime.GOOS == "linux" && info.Mode().Perm()&0077 != 0 {
		_ = root.Close()
		return nil, ErrRecoveryRejected
	}
	return &QuarantineLatch{root: root}, nil
}

// BeginQuarantine MUST finish successfully before opening/restoring a database.
// It never overwrites a hold or a partial write. Such a write remains blocked
// for offline investigation. Repeating the same intent preserves the hold ID.
func BeginQuarantine(ctx context.Context, directory string, intent RestoreIntent, verifier QuarantineVerifier) (*QuarantineLatch, error) {
	if ctx == nil || ctx.Err() != nil || !validIntent(intent) {
		return nil, ErrRecoveryRejected
	}
	l, err := openLatch(directory)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = l.Close()
		}
	}()
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, ErrRecoveryRejected
	}
	l.hold = QuarantineHold{Version: 1, ID: hex.EncodeToString(nonce[:]), RestoreIntent: intent}
	if err := l.write("hold.json", l.hold); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return nil, ErrRecoveryRejected
		}
		var existing QuarantineHold
		if l.read("hold.json", &existing) != nil || !validHold(existing) || existing.RestoreIntent != intent {
			return nil, ErrRecoveryRejected
		}
		l.hold = existing
	}
	if ctx.Err() != nil || l.checkHold() != nil {
		return nil, ErrRecoveryRejected
	}
	// Each restore episode needs a new target record. Never reuse a previously
	// released hold to open another old database snapshot.
	if _, err := l.root.Lstat("release.json"); !errors.Is(err, os.ErrNotExist) {
		return nil, ErrRecoveryRejected
	}
	l.verifier = verifier
	ok = true
	return l, nil
}

// OpenQuarantine requires the expected hold from the external restore record.
// Missing state is an error, never evidence that a restore target is healthy.
func OpenQuarantine(directory string, expected QuarantineHold, verifier QuarantineVerifier) (*QuarantineLatch, error) {
	if !validHold(expected) {
		return nil, ErrRecoveryRejected
	}
	l, err := openLatch(directory)
	if err != nil {
		return nil, err
	}
	l.hold, l.verifier = expected, verifier
	if l.checkHold() != nil {
		_ = l.Close()
		return nil, ErrRecoveryRejected
	}
	return l, nil
}
func (l *QuarantineLatch) Hold() QuarantineHold {
	if l == nil {
		return QuarantineHold{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.hold
}
func (l *QuarantineLatch) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.root == nil {
		return nil
	}
	err := l.root.Close()
	l.root = nil
	return err
}
func (l *QuarantineLatch) checkHold() error {
	var stored QuarantineHold
	if l.root == nil || l.read("hold.json", &stored) != nil || stored != l.hold {
		return ErrRecoveryRejected
	}
	return nil
}
func (l *QuarantineLatch) read(name string, value any) error {
	info, err := l.root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 4096 || runtime.GOOS == "linux" && info.Mode().Perm()&0077 != 0 {
		return ErrRecoveryRejected
	}
	f, err := l.root.Open(name)
	if err != nil {
		return ErrRecoveryRejected
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return ErrRecoveryRejected
	}
	b, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(b) > 4096 || json.Unmarshal(b, value) != nil {
		return ErrRecoveryRejected
	}
	canonical, err := json.Marshal(value)
	if err != nil || !bytes.Equal(b, canonical) {
		return ErrRecoveryRejected
	}
	return nil
}
func (l *QuarantineLatch) write(name string, value any) error {
	b, err := json.Marshal(value)
	if err != nil || len(b) > 4096 {
		return ErrRecoveryRejected
	}
	f, err := l.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	n, writeErr := f.Write(b)
	syncErr := f.Sync()
	closeErr := f.Close()
	// No cleanup of an ambiguous write: an incomplete record keeps quarantine.
	if writeErr != nil || n != len(b) || syncErr != nil || closeErr != nil {
		return ErrRecoveryRejected
	}
	if runtime.GOOS == "linux" {
		dir, err := l.root.Open(".")
		if err != nil {
			return ErrRecoveryRejected
		}
		syncErr = dir.Sync()
		_ = dir.Close()
		if syncErr != nil {
			return ErrRecoveryRejected
		}
	}
	return nil
}

// Check is a gate for task dispatch, Agent acceptance and sensitive management
// routes in an explicitly selected restore profile. It does not authorize any
// user/resource/action; normal authentication and task gates remain mandatory.
func (l *QuarantineLatch) Check(ctx context.Context) (err error) {
	if l == nil || ctx == nil || ctx.Err() != nil {
		return ErrQuarantined
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	defer func() {
		if recover() != nil {
			err = ErrQuarantined
		}
	}()
	var c ReleaseClearance
	if l.checkHold() != nil || l.verifier.Current == nil || l.read("release.json", &c) != nil || !validClearance(l.hold, c) {
		return ErrQuarantined
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if l.verifier.Current(bounded, l.hold, c) != nil || bounded.Err() != nil || l.checkHold() != nil {
		return ErrQuarantined
	}
	return nil
}

// Release writes an immutable receipt AFTER durable reconciliation/audit. It
// never deletes the hold. A later failed Current check restores denial even
// though this receipt exists. Callbacks must honor cancellation and not reenter.
func (l *QuarantineLatch) Release(ctx context.Context) (err error) {
	if l == nil || ctx == nil || ctx.Err() != nil {
		return ErrQuarantined
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	defer func() {
		if recover() != nil {
			err = ErrQuarantined
		}
	}()
	if l.checkHold() != nil || l.verifier.Prepare == nil || l.verifier.Current == nil {
		return ErrQuarantined
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var c ReleaseClearance
	if l.read("release.json", &c) == nil {
		if !validClearance(l.hold, c) || l.verifier.Current(bounded, l.hold, c) != nil || bounded.Err() != nil || l.checkHold() != nil {
			return ErrQuarantined
		}
		return nil
	}
	// Corrupt/partial existing receipts are never overwritten or repaired here.
	if _, err := l.root.Lstat("release.json"); !errors.Is(err, os.ErrNotExist) {
		return ErrQuarantined
	}
	c, err = l.verifier.Prepare(bounded, l.hold)
	if err != nil || !validClearance(l.hold, c) || bounded.Err() != nil || l.verifier.Current(bounded, l.hold, c) != nil || bounded.Err() != nil || l.checkHold() != nil || l.write("release.json", c) != nil {
		return ErrQuarantined
	}
	if l.verifier.Current(bounded, l.hold, c) != nil || bounded.Err() != nil || l.checkHold() != nil {
		return ErrQuarantined
	}
	return nil
}
