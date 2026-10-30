// Package recovery prepares quarantined reconciliation; it never signs keys or
// enables scheduling merely because a backup can be decoded.
package recovery

import (
	"encoding/hex"
	"errors"
	"slices"
	"time"
)

var ErrRecoveryRejected = errors.New("recovery: reconciliation rejected")

type TaskRecord struct {
	ID      string
	State   string
	Expires time.Time
}
type Snapshot struct {
	RootSPKISHA256     string
	TrustGeneration    uint64
	RevocationSequence uint64
	RevokedSerials     []string
	Tasks              []TaskRecord
}
type Authority struct {
	// These come from the verified offline recovery procedure, not the backup
	// itself or HTTP flags. Missing current revocation evidence is a hard stop.
	RootSPKISHA256     string
	TrustGeneration    uint64
	RevocationSequence uint64
	RevokedSerials     []string
}
type Reconciliation struct {
	Quarantine             bool
	InvalidateAllSessions  bool
	InvalidateAllApprovals bool
	TrustGeneration        uint64
	RevocationSequence     uint64
	RevokedSerials         []string
	Tasks                  []TaskRecord
}

// Reconcile returns an inert plan for a reviewed storage transaction. It cannot
// clear quarantine; real backup integrity/decryption, old-Controller fencing,
// offline authority verification and new task authorization are still required.
// UNKNOWN results and pending tasks are NEVER retried by this component.
func Reconcile(snapshot Snapshot, current Authority, now time.Time) (*Reconciliation, error) {
	root, err := hex.DecodeString(snapshot.RootSPKISHA256)
	if err != nil || len(root) != 32 {
		return nil, ErrRecoveryRejected
	}
	if now.IsZero() || len(snapshot.RootSPKISHA256) != 64 || snapshot.RootSPKISHA256 != current.RootSPKISHA256 || snapshot.TrustGeneration == 0 || current.TrustGeneration < snapshot.TrustGeneration || current.RevocationSequence < snapshot.RevocationSequence || len(snapshot.Tasks) > 100000 || len(current.RevokedSerials) > 100000 || len(snapshot.RevokedSerials) > 100000 {
		return nil, ErrRecoveryRejected
	}
	plan := &Reconciliation{Quarantine: true, InvalidateAllSessions: true, InvalidateAllApprovals: true, TrustGeneration: current.TrustGeneration, RevocationSequence: current.RevocationSequence}
	serials := map[string]bool{}
	for _, list := range [][]string{snapshot.RevokedSerials, current.RevokedSerials} {
		for _, serial := range list {
			if serial == "" || len(serial) > 128 {
				return nil, ErrRecoveryRejected
			}
			serials[serial] = true
		}
	}
	for serial := range serials {
		plan.RevokedSerials = append(plan.RevokedSerials, serial)
	}
	slices.Sort(plan.RevokedSerials)
	seen := map[string]bool{}
	for _, task := range snapshot.Tasks {
		if task.ID == "" || seen[task.ID] {
			return nil, ErrRecoveryRejected
		}
		seen[task.ID] = true
		switch task.State {
		case "running", "offered", "acknowledged":
			task.State = "unknown"
		case "queued", "waiting_node", "awaiting_approval", "paused":
			if task.Expires.IsZero() {
				return nil, ErrRecoveryRejected
			}
			if !now.Before(task.Expires) {
				task.State = "expired"
			}
		case "succeeded", "failed", "canceled", "timed_out", "unknown", "expired", "completed", "partially_failed":
		default:
			return nil, ErrRecoveryRejected
		}
		plan.Tasks = append(plan.Tasks, task)
	}
	return plan, nil
}
