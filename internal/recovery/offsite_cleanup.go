package recovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
)

type CleanupCandidate struct {
	Scope         string        `json:"scope"`
	BackupID      string        `json:"backupId"`
	RemoteID      string        `json:"remoteId,omitempty"`
	SHA256        string        `json:"sha256"`
	Bytes         int64         `json:"bytes"`
	LocalRevision uint64        `json:"localRevision"`
	Object        *RemoteObject `json:"object,omitempty"`
}
type CleanupProtection struct {
	Scope    string `json:"scope"`
	BackupID string `json:"backupId"`
	Reason   string `json:"reason"`
}
type BackupCleanupPlan struct {
	Digest           string              `json:"digest"`
	OwnerID          string              `json:"ownerId"`
	PolicyRevision   uint64              `json:"policyRevision"`
	CreatedAt        time.Time           `json:"createdAt"`
	ExpiresAt        time.Time           `json:"expiresAt"`
	Candidates       []CleanupCandidate  `json:"candidates"`
	Protected        []CleanupProtection `json:"protected"`
	ApprovalRequired bool                `json:"approvalRequired"`
}
type CleanupItemResult struct {
	Candidate CleanupCandidate `json:"candidate"`
	State     string           `json:"state"`
	Reason    string           `json:"reason,omitempty"`
}
type BackupCleanupReceipt struct {
	TaskID     string              `json:"taskId"`
	PlanDigest string              `json:"planDigest"`
	State      string              `json:"state"`
	Items      []CleanupItemResult `json:"items"`
	At         time.Time           `json:"at"`
}

func cleanupCandidateKey(c CleanupCandidate) string {
	order := map[string]string{"staging": "0", "local": "1", "remote": "2"}
	return order[c.Scope] + ":" + c.BackupID + ":" + c.RemoteID
}
func (m *BackupMaintenance) cleanupCandidates(ctx context.Context, subject string, p BackupPolicy, now time.Time) ([]CleanupCandidate, []CleanupProtection, error) {
	candidates := []CleanupCandidate{}
	protected := []CleanupProtection{}
	backups, e := m.manager.List(ctx, subject)
	if e != nil {
		return nil, nil, e
	}
	remote, e := m.RemoteRecords(ctx, subject)
	if e != nil {
		return nil, nil, e
	}
	verified := map[string]bool{}
	for _, r := range remote {
		if r.State == "verified" && m.remoteCompleted(ctx, r) {
			verified[r.BackupID] = true
		}
	}
	localRanks := map[string]int{}
	for _, b := range backups {
		if b.Kind != "full" {
			continue
		}
		local, e := m.LocalState(ctx, b.ID)
		if e != nil {
			return nil, nil, e
		}
		source, e := m.o.ReadTask(ctx, b.TaskID)
		if e != nil {
			return nil, nil, e
		}
		if b.Status == "ready" && source.Status == tasks.StatusCompleted && !local.Pinned {
			if info, e := m.manager.stage.Lstat(b.ID); e == nil && info.IsDir() && privateRecoveryInfo(info, true) {
				candidates = append(candidates, CleanupCandidate{Scope: "staging", BackupID: b.ID, SHA256: b.SHA256, LocalRevision: local.Revision})
			} else if e != nil && !errors.Is(e, os.ErrNotExist) {
				return nil, nil, e
			}
		}
		if local.State == "deleted" {
			continue
		}
		rank := localRanks[b.RecipientFingerprint]
		if b.Status == "ready" && source.Status == tasks.StatusCompleted && local.State == "present" {
			localRanks[b.RecipientFingerprint]++
		}
		reason := ""
		switch {
		case local.Pinned:
			reason = "pinned"
		case b.Status != "ready" || source.Status != tasks.StatusCompleted:
			reason = "source_result_unresolved"
		case local.State != "present":
			reason = "local_state_unresolved"
		case m.manager.activeCipher(b.ID):
			reason = "in_use"
		case rank < p.LocalKeep:
			reason = "minimum_copy_count"
		case now.Before(b.CreatedAt.Add(time.Duration(p.LocalDays) * 24 * time.Hour)):
			reason = "within_retention_period"
		case p.RequireVerifiedRemote && !verified[b.ID]:
			reason = "verified_remote_copy_required"
		}
		if reason != "" {
			protected = append(protected, CleanupProtection{"local", b.ID, reason})
		} else {
			candidates = append(candidates, CleanupCandidate{Scope: "local", BackupID: b.ID, SHA256: b.SHA256, Bytes: b.Bytes, LocalRevision: local.Revision})
		}

	}
	type remoteCandidate struct {
		record BackupRemoteRecord
		backup BackupMetadata
		local  BackupLocalState
		task   tasks.Task
	}
	remotes := []remoteCandidate{}
	for _, r := range remote {
		b, e := m.manager.Get(ctx, subject, r.BackupID)
		if e != nil {
			return nil, nil, e
		}
		local, e := m.LocalState(ctx, b.ID)
		if e != nil {
			return nil, nil, e
		}
		task, e := m.o.ReadTask(ctx, b.TaskID)
		if e != nil {
			return nil, nil, e
		}
		remotes = append(remotes, remoteCandidate{r, b, local, task})
	}
	sort.Slice(remotes, func(i, j int) bool { return remotes[i].backup.CreatedAt.After(remotes[j].backup.CreatedAt) })
	remoteRanks := map[string]int{}
	for _, item := range remotes {
		r, b, local := item.record, item.backup, item.local
		group := destinationHash(r.Destination) + b.RecipientFingerprint
		rank := remoteRanks[group]
		if r.State == "verified" && b.Status == "ready" && item.task.Status == tasks.StatusCompleted && m.remoteCompleted(ctx, r) {
			remoteRanks[group]++
		}
		reason := ""
		switch {
		case local.Pinned:
			reason = "pinned"
		case b.Status != "ready" || item.task.Status != tasks.StatusCompleted:
			reason = "source_result_unresolved"
		case r.State != "verified" || !m.remoteCompleted(ctx, r):
			reason = "remote_state_unresolved"
		case p.Destination == nil || destinationHash(r.Destination) != destinationHash(*p.Destination):
			reason = "previous_destination_requires_explicit_configuration"
		case rank < p.RemoteKeep:
			reason = "minimum_copy_count"
		case now.Before(b.CreatedAt.Add(time.Duration(p.RemoteDays) * 24 * time.Hour)):
			reason = "within_retention_period"
		case !strongRemoteETag(r.Object.ETag):
			reason = "conditional_delete_etag_required"
		}
		if reason != "" {
			protected = append(protected, CleanupProtection{"remote", b.ID, reason})
		} else {
			o := r.Object
			candidates = append(candidates, CleanupCandidate{Scope: "remote", BackupID: b.ID, RemoteID: r.ID, SHA256: b.SHA256, Bytes: b.Bytes, LocalRevision: local.Revision, Object: &o})
		}
	}
	// Local deletion can require the remote copy. Do not delete that same copy
	// in this plan; otherwise two independently safe-looking deletions could
	// jointly remove the last physical copy.
	localDeleting := map[string]bool{}
	for _, c := range candidates {
		if c.Scope == "local" {
			localDeleting[c.BackupID] = true
		}
	}
	filtered := candidates[:0]
	for _, c := range candidates {
		if c.Scope == "remote" && localDeleting[c.BackupID] && p.RequireVerifiedRemote {
			protected = append(protected, CleanupProtection{"remote", c.BackupID, "required_by_local_deletion"})
			continue
		}
		filtered = append(filtered, c)
	}
	candidates = filtered
	sort.Slice(candidates, func(i, j int) bool { return cleanupCandidateKey(candidates[i]) < cleanupCandidateKey(candidates[j]) })
	if len(candidates) > 100 {
		candidates = candidates[:100]
	}
	if len(protected) > 1000 {
		protected = protected[:1000]
	}
	return candidates, protected, nil
}
func (m *BackupMaintenance) PreviewCleanup(ctx context.Context, subject string) (BackupCleanupPlan, error) {
	p, e := m.Policy(ctx, subject)
	if e != nil || p.Revision == 0 || p.OwnerID != subject {
		return BackupCleanupPlan{}, ErrBackupPolicyConflict
	}
	now := time.Now().UTC()
	candidates, protected, e := m.cleanupCandidates(ctx, subject, p, now)
	if e != nil {
		return BackupCleanupPlan{}, e
	}
	digest := offsiteKey(p.OwnerID, offsiteNumber(p.Revision), now.Format("2006-01-02"), offsiteKey(string(mustOffsiteJSON(candidates))))
	plan := BackupCleanupPlan{Digest: digest, OwnerID: subject, PolicyRevision: p.Revision, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour), Candidates: candidates, Protected: protected, ApprovalRequired: true}
	raw, e := json.Marshal(plan)
	if e != nil || len(raw) > 128<<10 {
		return plan, ErrBackupUnavailable
	}
	if _, e = m.manager.o.DB.Exec(ctx, `INSERT OR IGNORE INTO cx_backup_cleanup_plans VALUES(?,?,?)`, digest, subject, raw); e != nil {
		return plan, e
	}
	return m.CleanupPlan(ctx, subject, digest)
}
func mustOffsiteJSON(v any) []byte { b, _ := json.Marshal(v); return b }
func (m *BackupMaintenance) CleanupPlan(ctx context.Context, subject, digest string) (BackupCleanupPlan, error) {
	if !validDigest(digest) || m.manager.authorizeRead(ctx, subject) != nil {
		return BackupCleanupPlan{}, ErrBackupUnavailable
	}
	var raw []byte
	e := m.manager.o.DB.QueryRow(ctx, `SELECT payload FROM cx_backup_cleanup_plans WHERE digest=? AND owner_id=?`, digest, subject).Scan(&raw)
	var p BackupCleanupPlan
	if e != nil || len(raw) > 128<<10 || json.Unmarshal(raw, &p) != nil || p.Digest != digest || p.OwnerID != subject {
		return p, ErrBackupUnavailable
	}
	return p, nil
}
func (m *BackupMaintenance) CleanupReceipts(ctx context.Context, subject string) ([]BackupCleanupReceipt, error) {
	if m.manager.authorizeRead(ctx, subject) != nil {
		return nil, ErrBackupUnavailable
	}
	rows, e := m.manager.o.DB.Query(ctx, `SELECT payload FROM cx_backup_cleanup_receipts ORDER BY rowid DESC LIMIT 200`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []BackupCleanupReceipt{}
	for rows.Next() {
		var raw []byte
		var r BackupCleanupReceipt
		if e = rows.Scan(&raw); e != nil || json.Unmarshal(raw, &r) != nil {
			return nil, ErrBackupUnavailable
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (m *BackupMaintenance) saveCleanup(ctx context.Context, r BackupCleanupReceipt) error {
	raw, e := json.Marshal(r)
	if e != nil || len(raw) > 128<<10 {
		return ErrBackupUnavailable
	}
	_, e = m.manager.o.DB.Exec(ctx, `INSERT INTO cx_backup_cleanup_receipts VALUES(?,?) ON CONFLICT(task_id) DO UPDATE SET payload=excluded.payload`, r.TaskID, raw)
	return e
}
func (m *BackupMaintenance) candidateCurrent(ctx context.Context, t tasks.Task, p BackupPolicy, c CleanupCandidate) error {
	if m.current(ctx, t, p.Revision) != nil {
		return ErrBackupPolicyConflict
	}
	all, _, e := m.cleanupCandidates(ctx, t.SubmitterID, p, time.Now().UTC())
	if e != nil {
		return e
	}
	for _, item := range all {
		if cleanupCandidateKey(item) == cleanupCandidateKey(c) && string(mustOffsiteJSON(item)) == string(mustOffsiteJSON(c)) {
			return nil
		}
	}
	return ErrBackupPolicyConflict
}
func (m *BackupMaintenance) executeCleanup(ctx context.Context, t tasks.Task, params map[string]string, revision uint64) (map[string]tasks.WorkerResult, error) {
	plan, e := m.CleanupPlan(ctx, t.SubmitterID, params["planDigest"])
	if e != nil || !t.ApprovalRequired || !time.Now().Before(plan.ExpiresAt) {
		return maintenanceResult(t, tasks.NodeResultFailed, "frozen cleanup plan or independent task approval unavailable")
	}
	p, e := m.policy(ctx)
	if e != nil || p.Revision != revision {
		return maintenanceResult(t, tasks.NodeResultFailed, "cleanup policy changed")
	}
	receipt := BackupCleanupReceipt{TaskID: t.ID, PlanDigest: plan.Digest, State: "running", Items: []CleanupItemResult{}, At: time.Now().UTC()}
	var previous []byte
	e = m.manager.o.DB.QueryRow(ctx, `SELECT payload FROM cx_backup_cleanup_receipts WHERE task_id=?`, t.ID).Scan(&previous)
	if e == nil {
		return maintenanceResult(t, tasks.NodeResultUnknown, "cleanup intent already exists; no replay")
	}
	if !storage.IsNotFound(e) || m.saveCleanup(ctx, receipt) != nil {
		return maintenanceResult(t, tasks.NodeResultFailed, "cleanup intent unavailable")
	}
	for _, candidate := range plan.Candidates {
		if e = m.candidateCurrent(ctx, t, p, candidate); e != nil {
			receipt.Items = append(receipt.Items, CleanupItemResult{candidate, "skipped", "protection_or_policy_changed"})
			continue
		}
		// Freeze an individual deleting intent BEFORE irreversible I/O. A crash
		// leaves this item unknown for human observation, never an automatic retry.
		receipt.Items = append(receipt.Items, CleanupItemResult{candidate, "deleting", ""})
		if e = m.saveCleanup(ctx, receipt); e != nil {
			return maintenanceResult(t, tasks.NodeResultUnknown, "cleanup item intent commit unknown")
		}
		switch candidate.Scope {
		case "remote":
			r, readErr := m.remote(ctx, candidate.RemoteID)
			if readErr != nil {
				return maintenanceResult(t, tasks.NodeResultUnknown, "remote intent unavailable; cleanup stopped")
			}
			e = readErr
			if e == nil {
				r.State = "deleting"
				e = m.saveRemote(ctx, r)
			}
			if e == nil && p.Destination != nil {
				gate := func() error {
					if m.current(ctx, t, revision) != nil {
						return ErrBackupPolicyConflict
					}
					local, err := m.LocalState(ctx, candidate.BackupID)
					if err != nil || local.Pinned || local.Revision != candidate.LocalRevision {
						return ErrBackupPolicyConflict
					}
					return nil
				}
				e = m.o.Borrow(ctx, t, *p.Destination, func(bctx context.Context, secret []byte) error {
					store, e := newRemoteStore(*p.Destination, secret, gate)
					if e != nil {
						return e
					}
					defer store.client.CloseIdleConnections()
					return store.remove(bctx, r.Object)
				})
			}
			if e == nil {
				r.State = "deleted"
				r.ErrorCode = ""
			} else {
				r.State = "unknown"
				r.ErrorCode = "remote_delete_not_confirmed"
			}
			cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			persistErr := m.saveRemote(cleanup, r)
			stop()
			if persistErr != nil {
				e = persistErr
			}
		case "local":
			e = m.deleteLocal(ctx, t, p, candidate)
		case "staging":
			if m.current(ctx, t, revision) != nil {
				e = ErrBackupPolicyConflict
			} else {
				e = m.manager.stage.RemoveAll(candidate.BackupID)
			}
		default:
			e = ErrBackupUnavailable
		}
		index := len(receipt.Items) - 1
		if e != nil {
			receipt.Items[index].State = "unknown"
			receipt.Items[index].Reason = "deletion_not_confirmed"
			receipt.State = "unknown"
		} else {
			receipt.Items[index].State = "deleted"
		}
		receipt.At = time.Now().UTC()
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		persistErr := m.saveCleanup(cleanup, receipt)
		stop()
		if e != nil || persistErr != nil {
			return maintenanceResult(t, tasks.NodeResultUnknown, "cleanup outcome unknown; original receipts retained for human disposition")
		}
	}
	receipt.State = "completed"
	receipt.At = time.Now().UTC()
	if m.saveCleanup(ctx, receipt) != nil {
		return maintenanceResult(t, tasks.NodeResultUnknown, "cleanup final receipt unknown")
	}
	if m.manager.o.Audit.Append(ctx, audit.Event{SubjectID: t.SubmitterID, Action: ActionBackupRetention, Result: "success", RequestID: t.RequestID, TaskID: t.ID, Source: audit.Source(t.Source), ReasonCode: "frozen_retention_plan_completed", At: receipt.At}) != nil {
		return maintenanceResult(t, tasks.NodeResultUnknown, "cleanup final audit unknown")
	}
	return maintenanceResult(t, tasks.NodeResultSucceeded, "retention completed; protected or changed items were skipped")
}
func (m *BackupMaintenance) deleteLocal(ctx context.Context, t tasks.Task, p BackupPolicy, c CleanupCandidate) error {
	if p.RequireVerifiedRemote {
		records, e := m.RemoteRecords(ctx, t.SubmitterID)
		if e != nil {
			return e
		}
		verified := false
		for _, r := range records {
			if r.BackupID != c.BackupID || r.State != "verified" || !m.remoteCompleted(ctx, r) || p.Destination == nil || destinationHash(r.Destination) != destinationHash(*p.Destination) {
				continue
			}
			gate := func() error { return m.current(ctx, t, p.Revision) }
			e = m.o.Borrow(ctx, t, *p.Destination, func(bctx context.Context, secret []byte) error {
				store, e := newRemoteStore(*p.Destination, secret, gate)
				if e != nil {
					return e
				}
				defer store.client.CloseIdleConnections()
				_, e = store.verify(bctx, r.Object)
				return e
			})
			if e == nil {
				verified = true
				break
			}
		}
		if !verified {
			return ErrRemoteUnknown
		}
	}
	if m.candidateCurrent(ctx, t, p, c) != nil || m.manager.activeCipher(c.BackupID) {
		return ErrBackupPolicyConflict
	}
	name := c.BackupID + ".age"
	before, e := m.manager.root.Lstat(name)
	if e != nil || !privateRecoveryInfo(before, false) || before.Size() != c.Bytes {
		return ErrBackupUnavailable
	}
	f, e := m.manager.root.Open(name)
	if e != nil {
		return e
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(before, opened) {
		return ErrBackupUnavailable
	}
	hash := sha256.New()
	n, e := io.Copy(hash, &offsiteReader{r: io.LimitReader(f, c.Bytes+1), gate: func() error { return m.current(ctx, t, p.Revision) }})
	if e != nil || n != c.Bytes || hex.EncodeToString(hash.Sum(nil)) != c.SHA256 {
		return ErrBackupUnavailable
	}
	// Serialize final reader admission with the durable deletion intent.
	m.manager.readersMu.Lock()
	defer m.manager.readersMu.Unlock()
	if m.manager.activeCipherLocked(c.BackupID) {
		return ErrBackupPolicyConflict
	}
	local, e := m.LocalState(ctx, c.BackupID)
	if e != nil || local.Pinned || local.Revision != c.LocalRevision {
		return ErrBackupPolicyConflict
	}
	local.State = "deleting"
	local.TaskID = t.ID
	local.Revision++
	raw, _ := json.Marshal(local)
	result, e := m.manager.o.DB.Exec(ctx, `INSERT INTO cx_backup_local_state VALUES(?,?,?,?) ON CONFLICT(backup_id) DO UPDATE SET revision=excluded.revision,state=excluded.state,payload=excluded.payload WHERE revision=?`, c.BackupID, local.Revision, local.State, raw, c.LocalRevision)
	if e != nil {
		return e
	}
	changed, e := result.RowsAffected()
	if e != nil || changed != 1 {
		return ErrBackupPolicyConflict
	}
	tombstone := c.BackupID + "-deleting-" + t.ID + ".age"
	if _, e = m.manager.root.Lstat(tombstone); !errors.Is(e, os.ErrNotExist) {
		return ErrBackupUnavailable
	}
	if m.current(ctx, t, p.Revision) != nil {
		return ErrBackupPolicyConflict
	}
	if e = m.manager.root.Rename(name, tombstone); e != nil {
		return e
	}
	after, e := m.manager.root.Lstat(tombstone)
	if e != nil || !os.SameFile(before, after) {
		return ErrBackupUnavailable
	}
	if e = m.manager.root.Remove(tombstone); e != nil {
		return e
	}
	if e = syncRecoveryRoot(m.manager.root); e != nil {
		return e
	}
	local.State = "deleted"
	raw, _ = json.Marshal(local)
	_, e = m.manager.o.DB.Exec(ctx, `UPDATE cx_backup_local_state SET state='deleted',payload=? WHERE backup_id=? AND revision=?`, raw, c.BackupID, local.Revision)
	return e
}

// A cryptographic receipt from an UNKNOWN upload/verification task remains
// quarantined until a fresh read-only verification task completes normally.
func (m *BackupMaintenance) remoteCompleted(ctx context.Context, r BackupRemoteRecord) bool {
	if r.VerifiedTaskID == "" {
		return false
	}
	t, e := m.o.ReadTask(ctx, r.VerifiedTaskID)
	return e == nil && t.Status == tasks.StatusCompleted && (t.ActionID == ActionBackupUpload || t.ActionID == ActionBackupRemoteVerify)
}
