package recovery

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
)

const ActionBackupUpload = "backups.upload"
const ActionBackupRemoteVerify = "backups.remote.verify"
const ActionBackupPolicy = "backups.policy.configure"
const ActionBackupProtect = "backups.protect"

type BackupPolicy struct {
	Revision              uint64             `json:"revision"`
	OwnerID               string             `json:"ownerId"`
	ScheduledBackups      bool               `json:"scheduledBackups"`
	IntervalHours         int                `json:"intervalHours"`
	AutoUpload            bool               `json:"autoUpload"`
	ScheduleCleanup       bool               `json:"scheduleCleanup"`
	LocalDays             int                `json:"localDays"`
	LocalKeep             int                `json:"localKeep"`
	RemoteDays            int                `json:"remoteDays"`
	RemoteKeep            int                `json:"remoteKeep"`
	RequireVerifiedRemote bool               `json:"requireVerifiedRemote"`
	Destination           *BackupDestination `json:"destination,omitempty"`
	UpdatedAt             time.Time          `json:"updatedAt"`
}
type BackupLocalState struct {
	BackupID string `json:"backupId"`
	State    string `json:"state"`
	Pinned   bool   `json:"pinned"`
	Note     string `json:"note"`
	Revision uint64 `json:"revision"`
	TaskID   string `json:"taskId,omitempty"`
}
type BackupRemoteRecord struct {
	ID             string            `json:"id"`
	BackupID       string            `json:"backupId"`
	TaskID         string            `json:"taskId"`
	VerifiedTaskID string            `json:"verifiedTaskId,omitempty"`
	State          string            `json:"state"`
	Destination    BackupDestination `json:"destination"`
	Object         RemoteObject      `json:"object"`
	UpdatedAt      time.Time         `json:"updatedAt"`
	ErrorCode      string            `json:"errorCode,omitempty"`
}
type BackupMaintenanceOptions struct {
	Manager           *BackupManager
	DefaultOwnerID    string
	CredentialCurrent func(context.Context, string, BackupDestination) error
	Borrow            func(context.Context, tasks.Task, BackupDestination, func(context.Context, []byte) error) error
	ReadTask          func(context.Context, string) (tasks.Task, error)
}
type BackupMaintenance struct {
	o       BackupMaintenanceOptions
	manager *BackupManager
}

func OffsiteMigrations() []storage.Migration {
	return []storage.Migration{{Version: "cx/0021_backup_maintenance", Name: "owner backup policies immutable offsite operations and retention plans", Statements: []string{
		`CREATE TABLE cx_backup_policy(id TEXT PRIMARY KEY CHECK(id='current'),revision INTEGER NOT NULL,payload BLOB NOT NULL CHECK(length(payload)<=16384))`,
		`CREATE TABLE cx_backup_local_state(backup_id TEXT PRIMARY KEY,revision INTEGER NOT NULL,state TEXT NOT NULL,payload BLOB NOT NULL CHECK(length(payload)<=4096))`,
		`CREATE TABLE cx_backup_remote_objects(id TEXT PRIMARY KEY,backup_id TEXT NOT NULL,destination_sha256 TEXT NOT NULL,state TEXT NOT NULL,payload BLOB NOT NULL CHECK(length(payload)<=16384),UNIQUE(backup_id,destination_sha256))`,
		`CREATE TABLE cx_backup_cleanup_plans(digest TEXT PRIMARY KEY,owner_id TEXT NOT NULL,payload BLOB NOT NULL CHECK(length(payload)<=131072))`,
		`CREATE TABLE cx_backup_cleanup_receipts(task_id TEXT PRIMARY KEY,payload BLOB NOT NULL CHECK(length(payload)<=131072))`,
	}}}
}
func NewBackupMaintenance(o BackupMaintenanceOptions) (*BackupMaintenance, error) {
	if o.Manager == nil || o.CredentialCurrent == nil || o.Borrow == nil || o.ReadTask == nil {
		return nil, ErrBackupUnavailable
	}
	m := &BackupMaintenance{o: o, manager: o.Manager}
	m.manager.maintenance = m
	// An interrupted network mutation is a durable UNKNOWN, never an upload retry.
	rows, e := m.manager.o.DB.Query(context.Background(), `SELECT payload FROM cx_backup_remote_objects WHERE state IN ('uploading','deleting')`)
	if e != nil {
		return nil, e
	}
	records := []BackupRemoteRecord{}
	for rows.Next() {
		var raw []byte
		if e = rows.Scan(&raw); e != nil {
			rows.Close()
			return nil, e
		}
		var r BackupRemoteRecord
		if json.Unmarshal(raw, &r) != nil {
			rows.Close()
			return nil, ErrBackupUnavailable
		}
		r.State = "unknown"
		r.ErrorCode = "controller_restarted_no_replay"
		records = append(records, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	for _, r := range records {
		if e = m.saveRemote(context.Background(), r); e != nil {
			return nil, e
		}
	}
	return m, nil
}
func (m *BackupMaintenance) policy(ctx context.Context) (BackupPolicy, error) {
	var raw []byte
	e := m.manager.o.DB.QueryRow(ctx, `SELECT payload FROM cx_backup_policy WHERE id='current'`).Scan(&raw)
	if storage.IsNotFound(e) {
		return BackupPolicy{OwnerID: m.o.DefaultOwnerID, ScheduledBackups: m.o.DefaultOwnerID != "", IntervalHours: 24, LocalDays: 7, LocalKeep: 3, RemoteDays: 30, RemoteKeep: 7, RequireVerifiedRemote: true}, nil
	}
	var p BackupPolicy
	if e != nil || len(raw) > 16<<10 || json.Unmarshal(raw, &p) != nil || p.Revision == 0 {
		return p, ErrBackupUnavailable
	}
	return p, nil
}
func (m *BackupMaintenance) Policy(ctx context.Context, subject string) (BackupPolicy, error) {
	if e := m.manager.authorizeRead(ctx, subject); e != nil {
		return BackupPolicy{}, e
	}
	return m.policy(ctx)
}
func (m *BackupMaintenance) Configure(ctx context.Context, subject, request string, expected uint64, p BackupPolicy) (BackupPolicy, error) {
	if m.manager.authorizeRead(ctx, subject) != nil || !validReference(request) || p.IntervalHours < 1 || p.IntervalHours > 168 || p.LocalDays < 1 || p.LocalDays > 3650 || p.RemoteDays < 1 || p.RemoteDays > 3650 || p.LocalKeep < 1 || p.LocalKeep > 1000 || p.RemoteKeep < 1 || p.RemoteKeep > 1000 {
		return BackupPolicy{}, ErrBackupUnavailable
	}
	if p.Destination != nil && (ValidateBackupDestination(*p.Destination) != nil || m.o.CredentialCurrent(ctx, subject, *p.Destination) != nil) {
		return BackupPolicy{}, ErrRemoteRejected
	}
	if p.AutoUpload && p.Destination == nil {
		return BackupPolicy{}, ErrRemoteRejected
	}
	p.OwnerID = subject
	p.Revision = expected + 1
	p.UpdatedAt = time.Now().UTC()
	raw, e := json.Marshal(p)
	if e != nil || len(raw) > 16<<10 {
		return p, ErrBackupUnavailable
	}
	e = m.manager.o.DB.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		var revision uint64
		e := tx.QueryRow(txCtx, `SELECT revision FROM cx_backup_policy WHERE id='current'`).Scan(&revision)
		if e != nil && !storage.IsNotFound(e) {
			return e
		}
		if revision != expected {
			return ErrBackupPolicyConflict
		}
		if _, e = tx.Exec(txCtx, `INSERT INTO cx_backup_policy VALUES('current',?,?) ON CONFLICT(id) DO UPDATE SET revision=excluded.revision,payload=excluded.payload`, p.Revision, raw); e != nil {
			return e
		}
		return m.manager.o.Audit.AppendTx(txCtx, tx, audit.Event{SubjectID: subject, Action: ActionBackupPolicy, Result: "success", RequestID: request, Source: audit.SourceWeb, ReasonCode: "bounded_backup_policy_configured", At: p.UpdatedAt})
	})
	return p, e
}

var ErrBackupPolicyConflict = errors.New("recovery: backup policy or plan conflict")

func (m *BackupMaintenance) LocalState(ctx context.Context, id string) (BackupLocalState, error) {
	v := BackupLocalState{BackupID: id, State: "present", Revision: 0}
	var raw []byte
	e := m.manager.o.DB.QueryRow(ctx, `SELECT payload FROM cx_backup_local_state WHERE backup_id=?`, id).Scan(&raw)
	if storage.IsNotFound(e) {
		return v, nil
	}
	if e != nil || json.Unmarshal(raw, &v) != nil || v.BackupID != id {
		return v, ErrBackupUnavailable
	}
	return v, nil
}
func (m *BackupMaintenance) Protect(ctx context.Context, subject, request, id string, expected uint64, pin bool, note string) (BackupLocalState, error) {
	if m.manager.authorizeRead(ctx, subject) != nil || !validReference(request) || len(note) > 512 || strings.TrimSpace(note) == "" {
		return BackupLocalState{}, ErrBackupUnavailable
	}
	if _, e := m.manager.Get(ctx, subject, id); e != nil {
		return BackupLocalState{}, e
	}
	var out BackupLocalState
	e := m.manager.o.DB.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		var raw []byte
		v := BackupLocalState{BackupID: id, State: "present"}
		e := tx.QueryRow(txCtx, `SELECT payload FROM cx_backup_local_state WHERE backup_id=?`, id).Scan(&raw)
		if e != nil && !storage.IsNotFound(e) {
			return e
		}
		if e == nil && json.Unmarshal(raw, &v) != nil {
			return ErrBackupUnavailable
		}
		if v.Revision != expected || v.State == "deleting" {
			return ErrBackupPolicyConflict
		}
		v.Pinned = pin
		v.Note = note
		v.Revision++
		raw, _ = json.Marshal(v)
		if _, e = tx.Exec(txCtx, `INSERT INTO cx_backup_local_state VALUES(?,?,?,?) ON CONFLICT(backup_id) DO UPDATE SET revision=excluded.revision,state=excluded.state,payload=excluded.payload`, id, v.Revision, v.State, raw); e != nil {
			return e
		}
		out = v
		return m.manager.o.Audit.AppendTx(txCtx, tx, audit.Event{SubjectID: subject, Action: ActionBackupProtect, Result: "success", RequestID: request, Source: audit.SourceWeb, ReasonCode: "backup_protection_changed", At: time.Now().UTC()})
	})
	return out, e
}
func (m *BackupMaintenance) RemoteRecords(ctx context.Context, subject string) ([]BackupRemoteRecord, error) {
	if m.manager.authorizeRead(ctx, subject) != nil {
		return nil, ErrBackupUnavailable
	}
	rows, e := m.manager.o.DB.Query(ctx, `SELECT payload FROM cx_backup_remote_objects WHERE state<>'deleted' ORDER BY id LIMIT 10000`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []BackupRemoteRecord{}
	for rows.Next() {
		var raw []byte
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		var r BackupRemoteRecord
		if len(raw) > 16<<10 || json.Unmarshal(raw, &r) != nil {
			return nil, ErrBackupUnavailable
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (m *BackupMaintenance) remote(ctx context.Context, id string) (BackupRemoteRecord, error) {
	var raw []byte
	e := m.manager.o.DB.QueryRow(ctx, `SELECT payload FROM cx_backup_remote_objects WHERE id=?`, id).Scan(&raw)
	var r BackupRemoteRecord
	if e != nil {
		return r, e
	}
	if json.Unmarshal(raw, &r) != nil || r.ID != id {
		return r, ErrBackupUnavailable
	}
	return r, nil
}
func (m *BackupMaintenance) saveRemote(ctx context.Context, r BackupRemoteRecord) error {
	r.UpdatedAt = time.Now().UTC()
	raw, e := json.Marshal(r)
	if e != nil || len(raw) > 16<<10 {
		return ErrBackupUnavailable
	}
	result, e := m.manager.o.DB.Exec(ctx, `UPDATE cx_backup_remote_objects SET state=?,payload=? WHERE id=?`, r.State, raw, r.ID)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil || n != 1 {
		return ErrBackupUnavailable
	}
	return nil
}
func (m *BackupMaintenance) current(ctx context.Context, t tasks.Task, revision uint64) error {
	if m.manager.authorizeRead(ctx, t.SubmitterID) != nil {
		return ErrBackupUnavailable
	}
	current, e := m.manager.o.TaskCurrent(ctx, t.ID, t.PlanHash)
	if e != nil || current.ActionID != t.ActionID || current.TargetType != tasks.TargetTypeController || current.TargetResourceID != m.manager.o.AuthorityID || current.Params != t.Params || current.Source != t.Source {
		return ErrBackupUnavailable
	}
	p, e := m.policy(ctx)
	if e != nil || p.Revision != revision || p.OwnerID != t.SubmitterID {
		return ErrBackupPolicyConflict
	}
	if p.Destination != nil && m.o.CredentialCurrent(ctx, t.SubmitterID, *p.Destination) != nil {
		return ErrBackupUnavailable
	}
	if t.Source == "system" {
		if t.ActionID == ActionBackupUpload && !p.AutoUpload || t.ActionID == ActionBackupRetention && !p.ScheduleCleanup {
			return ErrBackupPolicyConflict
		}
	}
	return nil
}
func (m *BackupMaintenance) CredentialOperationCurrent(ctx context.Context, t tasks.Task, d BackupDestination) error {
	var params map[string]string
	if json.Unmarshal([]byte(t.Params), &params) != nil {
		return ErrBackupUnavailable
	}
	revision, e := strconv.ParseUint(params["policyRevision"], 10, 64)
	if e != nil || m.current(ctx, t, revision) != nil {
		return ErrBackupPolicyConflict
	}
	p, e := m.policy(ctx)
	if e != nil || p.Destination == nil || *p.Destination != d || m.o.CredentialCurrent(ctx, t.SubmitterID, d) != nil {
		return ErrBackupUnavailable
	}
	return nil
}
func (m *BackupMaintenance) ScheduledCurrent(ctx context.Context, t tasks.Task) error {
	var params map[string]string
	if json.Unmarshal([]byte(t.Params), &params) != nil {
		return ErrBackupUnavailable
	}
	p, e := m.policy(ctx)
	if e != nil || !p.ScheduledBackups || p.OwnerID != t.SubmitterID || params["policyRevision"] != offsiteNumber(p.Revision) {
		return ErrBackupPolicyConflict
	}
	return nil
}
func (m *BackupMaintenance) wait(ctx context.Context) error {
	for !m.manager.mu.TryLock() {
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}
func (m *BackupMaintenance) TaskParameters(ctx context.Context, subject, action, object string) (map[string]string, error) {
	p, e := m.Policy(ctx, subject)
	if e != nil || p.Revision == 0 || p.OwnerID != subject {
		return nil, ErrBackupPolicyConflict
	}
	params := map[string]string{"policyRevision": offsiteNumber(p.Revision)}
	switch action {
	case ActionBackupUpload:
		b, e := m.manager.Get(ctx, subject, object)
		if e != nil || b.Status != "ready" || b.Kind != "full" || p.Destination == nil {
			return nil, ErrBackupUnavailable
		}
		local, e := m.LocalState(ctx, b.ID)
		if e != nil || local.State != "present" {
			return nil, ErrBackupUnavailable
		}
		params["backupId"] = b.ID
		params["cipherSha256"] = b.SHA256
		params["destinationSha256"] = destinationHash(*p.Destination)
	case ActionBackupRemoteVerify:
		r, e := m.remote(ctx, object)
		if e != nil || p.Destination == nil || destinationHash(r.Destination) != destinationHash(*p.Destination) || r.State == "deleted" || r.State == "uploading" || r.State == "deleting" {
			return nil, ErrBackupUnavailable
		}
		params["remoteId"] = r.ID
		params["cipherSha256"] = r.Object.SHA256
		params["destinationSha256"] = destinationHash(r.Destination)
	case ActionBackupRetention:
		plan, e := m.CleanupPlan(ctx, subject, object)
		if e != nil || plan.PolicyRevision != p.Revision || !time.Now().Before(plan.ExpiresAt) {
			return nil, ErrBackupPolicyConflict
		}
		params["planDigest"] = plan.Digest
	default:
		return nil, ErrBackupUnavailable
	}
	return params, nil
}
func (m *BackupMaintenance) Execute(ctx context.Context, t tasks.Task) (map[string]tasks.WorkerResult, error) {
	result := func(code tasks.NodeResultCode, message string) (map[string]tasks.WorkerResult, error) {
		return map[string]tasks.WorkerResult{t.TargetResourceID: {Code: code, Message: message}}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	var params map[string]string
	if json.Unmarshal([]byte(t.Params), &params) != nil {
		return result(tasks.NodeResultFailed, "invalid maintenance parameters")
	}
	revision, e := strconv.ParseUint(params["policyRevision"], 10, 64)
	if e != nil || m.current(ctx, t, revision) != nil {
		return result(tasks.NodeResultFailed, "backup maintenance policy revoked or stale")
	}
	expected, e := m.TaskParameters(ctx, t.SubmitterID, t.ActionID, params["backupId"]+params["remoteId"]+params["planDigest"])
	if e != nil || !sameStringMap(params, expected) {
		return result(tasks.NodeResultFailed, "backup maintenance binding rejected")
	}
	if e = m.wait(ctx); e != nil {
		return result(tasks.NodeResultFailed, "backup maintenance not started")
	}
	defer m.manager.mu.Unlock()
	if m.current(ctx, t, revision) != nil {
		return result(tasks.NodeResultFailed, "backup maintenance authority unavailable")
	}
	if t.ActionID == ActionBackupRetention {
		return m.executeCleanup(ctx, t, params, revision)
	}
	p, e := m.policy(ctx)
	if e != nil || p.Destination == nil {
		return result(tasks.NodeResultFailed, "backup destination unavailable")
	}
	var record BackupRemoteRecord
	if t.ActionID == ActionBackupUpload {
		b, e := m.manager.Get(ctx, t.SubmitterID, params["backupId"])
		if e != nil {
			return result(tasks.NodeResultFailed, "backup metadata unavailable")
		}
		source, err := m.o.ReadTask(ctx, b.TaskID)
		if err != nil || source.Status != tasks.StatusCompleted || b.Status != "ready" || b.Kind != "full" {
			return result(tasks.NodeResultFailed, "source backup result requires human verification")
		}
		record = BackupRemoteRecord{ID: "remote_" + offsiteKey(b.ID, destinationHash(*p.Destination))[:40], BackupID: b.ID, TaskID: t.ID, State: "uploading", Destination: *p.Destination, Object: RemoteObject{Key: remoteObjectKey(*p.Destination, m.manager.o.AuthorityID, b), SHA256: b.SHA256, Bytes: b.Bytes}, UpdatedAt: time.Now().UTC()}
		raw, _ := json.Marshal(record)
		_, e = m.manager.o.DB.Exec(ctx, `INSERT INTO cx_backup_remote_objects VALUES(?,?,?,?,?)`, record.ID, b.ID, destinationHash(record.Destination), record.State, raw)
		if e != nil {
			return result(tasks.NodeResultFailed, "existing remote intent requires observation, not another upload")
		}
	} else {
		record, e = m.remote(ctx, params["remoteId"])
		if e != nil {
			return result(tasks.NodeResultFailed, "remote receipt unavailable")
		}
	}
	gate := func() error { return m.current(ctx, t, revision) }
	var observed RemoteObject
	e = m.o.Borrow(ctx, t, *p.Destination, func(bctx context.Context, secret []byte) error {
		store, e := newRemoteStore(*p.Destination, secret, gate)
		if e != nil {
			return e
		}
		defer store.client.CloseIdleConnections()
		if t.ActionID == ActionBackupUpload {
			file, _, e := m.manager.OpenCipher(bctx, t.SubmitterID, t.RequestID, record.BackupID)
			if e != nil {
				return e
			}
			defer file.Close()
			observed, e = store.put(bctx, record.Object, file)
			return e
		}
		observed, e = store.verify(bctx, record.Object)
		return e
	})
	if e != nil || gate() != nil {
		record.State = "unknown"
		record.ErrorCode = "remote_operation_not_confirmed"
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		_ = m.saveRemote(cleanup, record)
		return result(tasks.NodeResultUnknown, "remote outcome unknown; use read-only verification before further action")
	}
	record.State = "verified"
	record.VerifiedTaskID = t.ID
	record.Object = observed
	record.ErrorCode = ""
	if e = m.saveRemote(ctx, record); e != nil {
		return result(tasks.NodeResultUnknown, "remote bytes verified but receipt commit unknown")
	}
	_, e = m.manager.o.DB.Exec(ctx, `UPDATE cx_recovery_backups SET remote_status='verified',updated_at=? WHERE id=?`, storage.EncodeTime(time.Now().UTC()), record.BackupID)
	if e != nil {
		return result(tasks.NodeResultUnknown, "remote metadata update unknown")
	}
	if e = m.manager.o.Audit.Append(ctx, audit.Event{SubjectID: t.SubmitterID, Action: t.ActionID, Result: "success", RequestID: t.RequestID, TaskID: t.ID, Source: audit.Source(t.Source), ReasonCode: "remote_ciphertext_readback_sha256_verified", At: time.Now().UTC()}); e != nil {
		return result(tasks.NodeResultUnknown, "remote audit commit unknown")
	}
	return result(tasks.NodeResultSucceeded, "encrypted remote object verified: "+record.ID)
}
func sameStringMap(a, b map[string]string) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
func (m *BackupMaintenance) AutomaticWork(ctx context.Context) (BackupPolicy, []map[string]string, error) {
	p, e := m.policy(ctx)
	if e != nil || p.OwnerID == "" || m.manager.authorizeRead(ctx, p.OwnerID) != nil {
		return p, nil, ErrBackupUnavailable
	}
	work := []map[string]string{}
	if p.AutoUpload && p.Destination != nil {
		backups, e := m.manager.List(ctx, p.OwnerID)
		if e != nil {
			return p, nil, e
		}
		remote, e := m.RemoteRecords(ctx, p.OwnerID)
		if e != nil {
			return p, nil, e
		}
		seen := map[string]bool{}
		for _, r := range remote {
			seen[r.BackupID+destinationHash(r.Destination)] = true
		}
		for _, b := range backups {
			if len(work) >= 5 {
				break
			}
			local, e := m.LocalState(ctx, b.ID)
			if e != nil {
				return p, nil, e
			}
			source, err := m.o.ReadTask(ctx, b.TaskID)
			if err != nil {
				return p, nil, err
			}
			if b.Kind == "full" && b.Status == "ready" && source.Status == tasks.StatusCompleted && local.State == "present" && !seen[b.ID+destinationHash(*p.Destination)] {
				params, e := m.TaskParameters(ctx, p.OwnerID, ActionBackupUpload, b.ID)
				if e == nil {
					params["_action"] = ActionBackupUpload
					work = append(work, params)
				}
			}
		}
	}
	if p.ScheduleCleanup {
		plan, e := m.PreviewCleanup(ctx, p.OwnerID)
		if e != nil {
			return p, nil, e
		}
		if len(plan.Candidates) > 0 {
			work = append(work, map[string]string{"_action": ActionBackupRetention, "policyRevision": offsiteNumber(p.Revision), "planDigest": plan.Digest})
		}
	}
	return p, work, nil
}
func InvalidateRestoredBackupPolicyTx(ctx context.Context, tx storage.Tx) error {
	var raw []byte
	e := tx.QueryRow(ctx, `SELECT payload FROM cx_backup_policy WHERE id='current'`).Scan(&raw)
	if storage.IsNotFound(e) {
		return nil
	}
	if e != nil {
		return e
	}
	var p BackupPolicy
	if json.Unmarshal(raw, &p) != nil {
		return ErrRecoveryRejected
	}
	p.ScheduledBackups = false
	p.AutoUpload = false
	p.ScheduleCleanup = false
	p.Revision++
	p.UpdatedAt = time.Now().UTC()
	raw, _ = json.Marshal(p)
	_, e = tx.Exec(ctx, `UPDATE cx_backup_policy SET revision=?,payload=? WHERE id='current'`, p.Revision, raw)
	return e
}

// Keep original encrypted provenance after deleting a local copy; its remote
// copy can independently remain available under a longer retention policy.
func (m *BackupMaintenance) enrich(ctx context.Context, b *BackupMetadata) error {
	local, e := m.LocalState(ctx, b.ID)
	if e != nil {
		return e
	}
	b.LocalStatus = local.State
	b.Pinned = local.Pinned
	b.LocalRevision = local.Revision
	return nil
}
func maintenanceResult(t tasks.Task, code tasks.NodeResultCode, message string) (map[string]tasks.WorkerResult, error) {
	return map[string]tasks.WorkerResult{t.TargetResourceID: {Code: code, Message: message}}, nil
}

var _ tasks.Worker = (*BackupMaintenance)(nil)
