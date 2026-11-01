package recovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"serverfleet.local/serverfleet/internal/storage"
)

// RestoreJob is created by offline task admission, not decoded from HTTP Plan.
// The source supplies only the exact backup bound to Intent.SnapshotSHA256.
type RestoreJob struct {
	ID, TaskID, BackupID string
	Intent               RestoreIntent
}
type RestoreSource func(context.Context, RestoreJob) (io.ReadCloser, Snapshot, error)
type RestoreHooks struct {
	Admit     func(context.Context, RestoreJob, string) error
	Authority func(context.Context, RestoreJob) (Authority, error)
	// Commit must invalidate sessions/approvals, preserve UNKNOWN results and
	// merge revocations in ONE DS transaction on this new isolated database.
	Commit   func(context.Context, string, Reconciliation) error
	Verifier QuarantineVerifier
}
type RestoreReceipt struct {
	ID, State string
	Hold      QuarantineHold
}

// RestoreIsolated never replaces or opens the running Controller database. It
// reserves a NEW private job directory, creates quarantine before reading bytes,
// copies/hash-checks a standalone SQLite snapshot, then reconciles via the domain
// transaction adapter. No retry, dispatch or release follows an unknown result.
// Even success remains quarantined; explicit offline latch.Release plus live
// fencing/current-authority checks are required before a supervisor can use it.
func RestoreIsolated(ctx context.Context, directory string, job RestoreJob, source RestoreSource, hooks RestoreHooks) (out RestoreReceipt, err error) {
	defer func() {
		if recover() != nil {
			out = RestoreReceipt{}
			err = ErrQuarantined
		}
	}()
	if runtime.GOOS != "linux" || os.Geteuid() == 0 || ctx == nil || ctx.Err() != nil || !filepath.IsAbs(directory) || !validReference(job.ID) || !validReference(job.TaskID) || !validReference(job.BackupID) || !validIntent(job.Intent) || source == nil || hooks.Admit == nil || hooks.Authority == nil || hooks.Commit == nil || hooks.Verifier.Prepare == nil || hooks.Verifier.Current == nil {
		return out, ErrRecoveryRejected
	}
	if hooks.Admit(ctx, job, "reserve") != nil {
		return out, ErrRecoveryRejected
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return out, ErrRecoveryRejected
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return out, ErrRecoveryRejected
	}
	defer root.Close()
	info, err = root.Stat(".")
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return out, ErrRecoveryRejected
	}
	name := "restore-" + job.ID
	if root.Mkdir(name, 0700) != nil {
		return out, ErrQuarantined
	}
	path := filepath.Join(directory, name)
	latch, err := BeginQuarantine(ctx, path, job.Intent, hooks.Verifier)
	if err != nil {
		return out, ErrQuarantined
	}
	defer latch.Close()
	if latch.Check(ctx) != ErrQuarantined || hooks.Admit(ctx, job, "copy") != nil {
		return out, ErrQuarantined
	}
	reader, snapshot, err := source(ctx, job)
	if err != nil || reader == nil {
		return out, ErrQuarantined
	}
	defer reader.Close()
	file, err := root.OpenFile(name+"/restored.db", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return out, ErrQuarantined
	}
	hash := sha256.New()
	buffer := make([]byte, 32<<10)
	defer clear(buffer)
	var total int64
	for {
		if ctx.Err() != nil {
			file.Close()
			return out, ErrQuarantined
		}
		n, e := reader.Read(buffer)
		total += int64(n)
		if total > 256<<20 {
			file.Close()
			return out, ErrQuarantined
		}
		if n > 0 {
			hash.Write(buffer[:n])
			if written, we := file.Write(buffer[:n]); we != nil || written != n {
				file.Close()
				return out, ErrQuarantined
			}
		}
		if e == io.EOF {
			break
		}
		if e != nil || n == 0 {
			file.Close()
			return out, ErrQuarantined
		}
	}
	synced := file.Sync()
	closed := file.Close()
	if synced != nil || closed != nil || hex.EncodeToString(hash.Sum(nil)) != job.Intent.SnapshotSHA256 || storage.VerifyFile(ctx, filepath.Join(path, "restored.db"), true) != nil || hooks.Admit(ctx, job, "reconcile") != nil {
		return out, ErrQuarantined
	}
	current, err := hooks.Authority(ctx, job)
	if err != nil {
		return out, ErrQuarantined
	}
	if current.RootSPKISHA256 != job.Intent.RootSPKISHA256 || current.TrustGeneration < job.Intent.MinTrustGeneration || current.RevocationSequence < job.Intent.MinRevocationSequence {
		return out, ErrQuarantined
	}
	plan, err := Reconcile(snapshot, current, time.Now().UTC())
	if err != nil || !plan.Quarantine {
		return out, ErrQuarantined
	}
	if hooks.Commit(ctx, filepath.Join(path, "restored.db"), *plan) != nil || ctx.Err() != nil || hooks.Admit(ctx, job, "committed") != nil {
		return out, ErrQuarantined
	}
	return RestoreReceipt{job.ID, "restored_quarantined", latch.Hold()}, nil
}
