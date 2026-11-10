package recovery

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
)

const (
	ActionCreateBackup               = "backups.create"
	ActionRecoveryKeyChallenge       = "backups.key.challenge"
	ActionRecoveryKeyConfirm         = "backups.key.confirm"
	ActionBackupDownload             = "backups.download"
	ActionBackupVerify               = "backups.verify"
	ActionBackupRetention            = "backups.retention"
	MaxBackupDatabaseBytes     int64 = 256 << 20
	MaxBackupBundleBytes       int64 = 272 << 20
)

var ErrBackupUnavailable = errors.New("recovery: encrypted backup deployment unavailable")

func BackupAuditActions() []string {
	return []string{ActionCreateBackup, ActionRecoveryKeyChallenge, ActionRecoveryKeyConfirm, ActionBackupDownload, ActionBackupVerify, ActionBackupRetention, "backups.upload"}
}

type BackupAuditSink interface {
	Append(context.Context, audit.Event) error
	AppendTx(context.Context, storage.Tx, audit.Event) error
}
type BackupFileSource struct {
	Name, Path, SHA256 string
	MaxBytes           int64
	Inline             []byte
}
type BackupDeploymentOptions struct {
	DB                                                                            *storage.DB
	Audit                                                                         BackupAuditSink
	Directory, StageDir, Recipient, AuthorityID, RootSPKI, AgentEndpoint, Version string
	TrustGeneration                                                               uint64
	AutomaticConfigured                                                           bool
	Cipher                                                                        *AgeCipher
	Files                                                                         []BackupFileSource
	Authorize                                                                     func(context.Context, string) error
	RuntimeCurrent                                                                func(context.Context) error
	TaskCurrent                                                                   func(context.Context, string, string) (tasks.Task, error)
	Capture                                                                       func(context.Context, string) (Snapshot, []string, error)
	ValidateConfiguration                                                         func(context.Context) error
}

type BackupManager struct {
	o           BackupDeploymentOptions
	mu          sync.Mutex
	root        *os.Root
	stage       *os.Root
	maintenance *BackupMaintenance
	readersMu   sync.Mutex
	readers     map[*os.File]string
}
type BackupMetadata struct {
	LocalStatus          string    `json:"localStatus,omitempty"`
	Pinned               bool      `json:"pinned"`
	LocalRevision        uint64    `json:"localRevision"`
	ID                   string    `json:"id"`
	TaskID               string    `json:"taskId"`
	Kind                 string    `json:"kind"`
	Status               string    `json:"status"`
	SHA256               string    `json:"sha256,omitempty"`
	Bytes                int64     `json:"bytes"`
	CreatedAt            time.Time `json:"createdAt"`
	UpdatedAt            time.Time `json:"updatedAt"`
	RecipientFingerprint string    `json:"recipientFingerprint"`
	RemoteStatus         string    `json:"remoteStatus"`
	ErrorCode            string    `json:"errorCode,omitempty"`
}
type BundleFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}
type BackupManifest struct {
	Format          int          `json:"format"`
	SnapshotID      string       `json:"snapshotId"`
	CreatedAt       time.Time    `json:"createdAt"`
	ReleaseVersion  string       `json:"releaseVersion"`
	Encryption      string       `json:"encryption"`
	AgentEndpoint   string       `json:"agentEndpoint"`
	AuthorityID     string       `json:"authorityId"`
	RootSPKI        string       `json:"rootSpkiSha256"`
	TrustGeneration uint64       `json:"trustGeneration"`
	SchemaVersions  []string     `json:"schemaVersions"`
	NodeIDs         []string     `json:"nodeIds"`
	Snapshot        Snapshot     `json:"snapshot"`
	Files           []BundleFile `json:"files"`
}

func allowedBundleName(name string) bool {
	switch name {
	case "credentials/acme-account-key.pem", "configuration/acme-deployment.json", "configuration/acme-root.pem", "configuration/dns-root.pem", "configuration/controller.json", "configuration/file-deployment.json", "configuration/gateway-deployment.json", "configuration/update-deployment.json", "configuration/backup-deployment.json", "configuration/release-source.json", "configuration/release-root.pem", "controller.db", "pki/root.pem", "pki/issuer.pem", "pki/issuer-key.pem", "pki/pki.json", "credentials/credential-keys.json", "tls/controller.pem", "tls/controller-key.pem":
		return true
	}
	return false
}
func NewBackupManager(o BackupDeploymentOptions) (*BackupManager, error) {
	if os.Geteuid() == 0 || o.DB == nil || o.Audit == nil || o.Cipher == nil || !validAgeRecipient(o.Recipient) || !validReference(o.AuthorityID) || !validDigest(o.RootSPKI) || o.TrustGeneration == 0 || o.Authorize == nil || o.RuntimeCurrent == nil || o.TaskCurrent == nil || o.Capture == nil || o.ValidateConfiguration == nil || o.Directory == o.StageDir || len(o.Files) < 7 || len(o.Files) > 18 {
		return nil, ErrBackupUnavailable
	}
	root, err := openRecoveryDirectory(o.Directory)
	if err != nil {
		return nil, err
	}
	stage, err := openRecoveryDirectory(o.StageDir)
	if err != nil {
		root.Close()
		return nil, err
	}
	seen := map[string]bool{}
	for _, file := range o.Files {
		if file.Name == "controller.db" || !allowedBundleName(file.Name) || seen[file.Name] || (len(file.Inline) == 0 && !filepath.IsAbs(file.Path) || len(file.Inline) > 0 && (file.Path != "" || file.Name != "configuration/controller.json" || !json.Valid(file.Inline))) || !validDigest(file.SHA256) || file.MaxBytes <= 0 || file.MaxBytes > 128<<10 {
			root.Close()
			stage.Close()
			return nil, ErrBackupUnavailable
		}
		seen[file.Name] = true
	}
	for _, name := range []string{"pki/root.pem", "pki/issuer.pem", "pki/issuer-key.pem", "pki/pki.json", "credentials/credential-keys.json", "tls/controller.pem", "tls/controller-key.pem"} {
		if !seen[name] {
			root.Close()
			stage.Close()
			return nil, ErrBackupUnavailable
		}
	}
	o.Files = append([]BackupFileSource(nil), o.Files...)
	for i := range o.Files {
		o.Files[i].Inline = bytes.Clone(o.Files[i].Inline)
	}
	sort.Slice(o.Files, func(i, j int) bool { return o.Files[i].Name < o.Files[j].Name })
	manager := &BackupManager{o: o, root: root, stage: stage}
	// An interrupted encryption/upload is not re-executed from a filename or
	// partial receipt after restart. The unified task recovery stays UNKNOWN.
	if _, err = o.DB.Exec(context.Background(), `UPDATE cx_recovery_backups SET status='unknown',error_code='controller_restarted',updated_at=? WHERE status='preparing'`, storage.EncodeTime(time.Now().UTC())); err != nil {
		manager.Close()
		return nil, err
	}
	return manager, nil
}
func (m *BackupManager) Close() error {
	if m == nil {
		return nil
	}
	if m.root != nil {
		m.root.Close()
	}
	if m.stage != nil {
		m.stage.Close()
	}
	return nil
}
func (m *BackupManager) RecipientFingerprint() string {
	sum := sha256.Sum256([]byte(m.o.Recipient))
	return hex.EncodeToString(sum[:])
}
func (m *BackupManager) gate(ctx context.Context, t tasks.Task) error {
	if m == nil || ctx == nil || ctx.Err() != nil || m.o.RuntimeCurrent(ctx) != nil || m.o.Authorize(ctx, t.SubmitterID) != nil || m.o.ValidateConfiguration(ctx) != nil {
		return ErrBackupUnavailable
	}
	if t.Source == "system" && m.maintenance != nil && m.maintenance.ScheduledCurrent(ctx, t) != nil {
		return ErrBackupUnavailable
	}
	current, err := m.o.TaskCurrent(ctx, t.ID, t.PlanHash)
	if err != nil || current.TargetType != tasks.TargetTypeController || current.TargetResourceID != m.o.AuthorityID || current.ActionID != t.ActionID {
		return ErrBackupUnavailable
	}
	return nil
}
func (m *BackupManager) KeyConfirmed(ctx context.Context) (bool, error) {
	var confirmed int
	err := m.o.DB.QueryRow(ctx, `SELECT confirmed FROM cx_recovery_recipient_checks WHERE recipient_fingerprint=?`, m.RecipientFingerprint()).Scan(&confirmed)
	if storage.IsNotFound(err) {
		return false, nil
	}
	return confirmed == 1, err
}
func (m *BackupManager) Execute(ctx context.Context, t tasks.Task) (map[string]tasks.WorkerResult, error) {
	result := func(code tasks.NodeResultCode, message string) map[string]tasks.WorkerResult {
		return map[string]tasks.WorkerResult{t.TargetResourceID: {Code: code, Message: message}}
	}
	if err := m.gate(ctx, t); err != nil {
		return result(tasks.NodeResultFailed, "backup authorization unavailable"), nil
	}
	var params map[string]string
	expectedFields := 2
	if t.Source == "system" && m.maintenance != nil {
		expectedFields = 3
	}
	if json.Unmarshal([]byte(t.Params), &params) != nil || len(params) != expectedFields || params["recipientFingerprint"] != m.RecipientFingerprint() || params["kind"] != "full" && params["kind"] != "key_challenge" {
		return result(tasks.NodeResultFailed, "backup plan binding rejected"), nil
	}
	kind := params["kind"]
	if kind == "full" && t.ActionID != ActionCreateBackup || kind == "key_challenge" && t.ActionID != ActionRecoveryKeyChallenge {
		return result(tasks.NodeResultFailed, "backup action binding rejected"), nil
	}
	if kind == "full" {
		ok, err := m.KeyConfirmed(ctx)
		if err != nil || !ok {
			return result(tasks.NodeResultFailed, "offline recovery key confirmation required"), nil
		}
	}
	if !m.mu.TryLock() {
		return result(tasks.NodeResultFailed, "another encrypted backup is active"), nil
	}
	defer m.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	id := "backup-" + t.ID
	if !validReference(id) {
		return result(tasks.NodeResultFailed, "backup id rejected"), nil
	}
	now := time.Now().UTC()
	name := id + ".age"
	err := m.o.DB.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if _, err := tx.Exec(txCtx, `INSERT INTO cx_recovery_backups(id,task_id,kind,status,cipher_name,created_at,updated_at,subject_id,recipient_fingerprint,authority_id,root_spki,trust_generation) VALUES(?,?,?,'preparing',?,?,?,?,?,?,?,?)`, id, t.ID, kind, name, storage.EncodeTime(now), storage.EncodeTime(now), t.SubmitterID, m.RecipientFingerprint(), m.o.AuthorityID, m.o.RootSPKI, m.o.TrustGeneration); err != nil {
			return err
		}
		return m.o.Audit.AppendTx(txCtx, tx, audit.Event{SubjectID: t.SubmitterID, Action: t.ActionID, Result: "success", RequestID: t.RequestID, TaskID: t.ID, Source: audit.Source(t.Source), ReasonCode: "backup_reserved", At: now})
	})
	if err != nil {
		return result(tasks.NodeResultFailed, "backup reservation failed"), nil
	}
	if err = m.stage.Mkdir(id, 0700); err != nil {
		m.fail(t, id, "stage_reservation_unknown")
		return result(tasks.NodeResultUnknown, "backup staging outcome unknown"), nil
	}
	work := filepath.Join(m.o.StageDir, id)
	output, err := m.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		m.fail(t, id, "cipher_output_unknown")
		return result(tasks.NodeResultUnknown, "backup output reservation unknown"), nil
	}
	hash := sha256.New()
	counter := &boundedBackupWriter{writer: io.MultiWriter(output, hash), max: MaxBackupBundleBytes, gate: func() error { return m.gate(ctx, t) }}
	manifestSHA := ""
	nonceDigest := ""
	if kind == "key_challenge" {
		nonce := make([]byte, 32)
		if _, err = rand.Read(nonce); err == nil {
			sum := sha256.Sum256(nonce)
			nonceDigest = hex.EncodeToString(sum[:])
			err = m.o.Cipher.Encrypt(ctx, m.o.Recipient, bytes.NewReader(nonce), counter)
		}
		clear(nonce)
	} else {
		manifestSHA, err = m.encryptBundle(ctx, t, id, work, counter)
	}
	syncErr := output.Sync()
	if syncErr == nil {
		syncErr = syncRecoveryRoot(m.root)
	}
	closeErr := output.Close()
	if err != nil || syncErr != nil || closeErr != nil || m.gate(ctx, t) != nil {
		m.fail(t, id, "encryption_outcome_unknown")
		return result(tasks.NodeResultUnknown, "encrypted backup outcome unknown; no automatic replay"), nil
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	receiptTime := time.Now().UTC()
	err = m.o.DB.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		update, err := tx.Exec(txCtx, `UPDATE cx_recovery_backups SET status='ready',sha256=?,bytes=?,manifest_sha256=?,updated_at=? WHERE id=? AND task_id=? AND status='preparing'`, digest, counter.bytes, manifestSHA, storage.EncodeTime(receiptTime), id, t.ID)
		if err != nil {
			return err
		}
		affected, err := update.RowsAffected()
		if err != nil || affected != 1 {
			return ErrBackupUnavailable
		}
		if kind == "key_challenge" {
			if _, err := tx.Exec(txCtx, `INSERT INTO cx_recovery_recipient_checks(recipient_fingerprint,challenge_id,nonce_sha256,created_at) VALUES(?,?,?,?) ON CONFLICT(recipient_fingerprint) DO UPDATE SET challenge_id=excluded.challenge_id,nonce_sha256=excluded.nonce_sha256,confirmed=0,confirmed_by='',created_at=excluded.created_at,confirmed_at=''`, m.RecipientFingerprint(), id, nonceDigest, storage.EncodeTime(receiptTime)); err != nil {
				return err
			}
		}
		return m.o.Audit.AppendTx(txCtx, tx, audit.Event{SubjectID: t.SubmitterID, Action: t.ActionID, Result: "success", RequestID: t.RequestID, TaskID: t.ID, Source: audit.Source(t.Source), ReasonCode: "encrypted_backup_created", At: receiptTime})
	})
	if err != nil {
		m.fail(t, id, "receipt_commit_unknown")
		return result(tasks.NodeResultUnknown, "backup exists but receipt commit unknown"), nil
	}
	// Only our private task directory is removed, after the encrypted receipt is
	// committed. Interrupted private plaintext remains quarantined for operators.
	_ = m.stage.RemoveAll(id)
	return result(tasks.NodeResultSucceeded, "encrypted backup ready: "+id), nil
}

type boundedBackupWriter struct {
	writer     io.Writer
	bytes, max int64
	gate       func() error
}

func (w *boundedBackupWriter) Write(raw []byte) (int, error) {
	if w.gate != nil {
		if err := w.gate(); err != nil {
			return 0, err
		}
	}
	if int64(len(raw)) > w.max-w.bytes {
		return 0, ErrBackupUnavailable
	}
	n, err := w.writer.Write(raw)
	w.bytes += int64(n)
	return n, err
}
func (m *BackupManager) fail(t tasks.Task, id, code string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = m.o.DB.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		_, err := tx.Exec(txCtx, `UPDATE cx_recovery_backups SET status='unknown',error_code=?,updated_at=? WHERE id=? AND status='preparing'`, code, storage.EncodeTime(time.Now().UTC()), id)
		if err != nil {
			return err
		}
		return m.o.Audit.AppendTx(txCtx, tx, audit.Event{SubjectID: t.SubmitterID, Action: t.ActionID, Result: "failure", RequestID: t.RequestID, TaskID: t.ID, Source: audit.Source(t.Source), ReasonCode: code, At: time.Now().UTC()})
	})
}
func (m *BackupManager) encryptBundle(ctx context.Context, t tasks.Task, id, work string, output io.Writer) (string, error) {
	snapshotPath := filepath.Join(work, "controller.db")
	if err := m.o.DB.BackupTo(ctx, snapshotPath); err != nil {
		return "", err
	}
	if err := os.Chmod(snapshotPath, 0600); err != nil {
		return "", err
	}
	info, err := os.Lstat(snapshotPath)
	if err != nil || info.Size() > MaxBackupDatabaseBytes || !privateRecoveryInfo(info, false) {
		return "", ErrBackupUnavailable
	}
	dbHash, err := storage.ChecksumFile(snapshotPath)
	if err != nil {
		return "", err
	}
	state, nodeIDs, err := m.o.Capture(ctx, snapshotPath)
	if err != nil || state.RootSPKISHA256 != m.o.RootSPKI || state.TrustGeneration != m.o.TrustGeneration {
		return "", ErrBackupUnavailable
	}
	snapshotDB, err := storage.Open(ctx, storage.Config{Path: snapshotPath, ReadOnly: true})
	if err != nil {
		return "", err
	}
	versions, err := storage.AppliedVersions(ctx, snapshotDB)
	snapshotDB.Close()
	if err != nil {
		return "", err
	}
	manifest := BackupManifest{Format: 1, SnapshotID: id, CreatedAt: time.Now().UTC(), ReleaseVersion: m.o.Version, Encryption: "age/X25519/v1", AgentEndpoint: m.o.AgentEndpoint, AuthorityID: m.o.AuthorityID, RootSPKI: m.o.RootSPKI, TrustGeneration: m.o.TrustGeneration, SchemaVersions: versions, NodeIDs: nodeIDs, Snapshot: state, Files: []BundleFile{{"controller.db", dbHash, info.Size()}}}
	contents := map[string][]byte{}
	defer func() {
		for _, raw := range contents {
			clear(raw)
		}
	}()
	for _, source := range m.o.Files {
		if err := m.gate(ctx, t); err != nil {
			return "", err
		}
		var raw []byte
		var err error
		if len(source.Inline) > 0 {
			raw = bytes.Clone(source.Inline)
		} else {
			root, openErr := openRecoveryDirectory(filepath.Dir(source.Path))
			if openErr != nil {
				return "", openErr
			}
			raw, err = readRecoveryFile(root, filepath.Base(source.Path), source.MaxBytes)
			root.Close()
		}
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != source.SHA256 {
			clear(raw)
			return "", ErrBackupUnavailable
		}
		contents[source.Name] = raw
		manifest.Files = append(manifest.Files, BundleFile{source.Name, source.SHA256, int64(len(raw))})
	}
	rawManifest, err := json.Marshal(manifest)
	if err != nil || len(rawManifest) > 8<<20 {
		return "", ErrBackupUnavailable
	}
	manifestHash := sha256.Sum256(rawManifest)
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		archive := tar.NewWriter(writer)
		write := func(name string, size int64, input io.Reader, expectedSHA string) error {
			if err := m.gate(ctx, t); err != nil {
				return err
			}
			if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: size, Typeflag: tar.TypeReg, ModTime: manifest.CreatedAt}); err != nil {
				return err
			}
			hash := sha256.New()
			n, err := io.CopyN(archive, io.TeeReader(input, hash), size)
			if err != nil || n != size || hex.EncodeToString(hash.Sum(nil)) != expectedSHA {
				return ErrBackupUnavailable
			}
			return nil
		}
		err := write("manifest.json", int64(len(rawManifest)), bytes.NewReader(rawManifest), hex.EncodeToString(manifestHash[:]))
		for _, entry := range manifest.Files {
			if err != nil {
				break
			}
			if entry.Name == "controller.db" {
				file, e := os.Open(snapshotPath)
				if e != nil {
					err = e
					break
				}
				err = write(entry.Name, entry.Bytes, file, entry.SHA256)
				file.Close()
			} else {
				err = write(entry.Name, entry.Bytes, bytes.NewReader(contents[entry.Name]), entry.SHA256)
			}
		}
		if err == nil {
			err = archive.Close()
		}
		writer.CloseWithError(err)
		done <- err
	}()
	cipherErr := m.o.Cipher.Encrypt(ctx, m.o.Recipient, reader, output)
	reader.CloseWithError(cipherErr)
	archiveErr := <-done
	if cipherErr != nil {
		return "", cipherErr
	}
	if archiveErr != nil {
		return "", archiveErr
	}
	return hex.EncodeToString(manifestHash[:]), nil
}

func (m *BackupManager) authorizeRead(ctx context.Context, subject string) error {
	if m == nil || ctx == nil || ctx.Err() != nil || m.o.RuntimeCurrent(ctx) != nil {
		return ErrBackupUnavailable
	}
	return m.o.Authorize(ctx, subject)
}
func (m *BackupManager) List(ctx context.Context, subject string) ([]BackupMetadata, error) {
	if err := m.authorizeRead(ctx, subject); err != nil {
		return nil, err
	}
	rows, err := m.o.DB.Query(ctx, `SELECT id,task_id,kind,status,sha256,bytes,created_at,updated_at,recipient_fingerprint,remote_status,error_code FROM cx_recovery_backups ORDER BY created_at DESC,id DESC LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BackupMetadata{}
	for rows.Next() {
		var value BackupMetadata
		var created, updated string
		if err = rows.Scan(&value.ID, &value.TaskID, &value.Kind, &value.Status, &value.SHA256, &value.Bytes, &created, &updated, &value.RecipientFingerprint, &value.RemoteStatus, &value.ErrorCode); err != nil {
			return nil, err
		}
		value.CreatedAt, err = storage.DecodeTime(created)
		if err != nil {
			return nil, err
		}
		value.UpdatedAt, err = storage.DecodeTime(updated)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	rows.Close()
	if err = m.authorizeRead(ctx, subject); err != nil {
		return nil, err
	}
	if m.maintenance != nil {
		for i := range out {
			if err = m.maintenance.enrich(ctx, &out[i]); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}
func (m *BackupManager) Get(ctx context.Context, subject, id string) (BackupMetadata, error) {
	if err := m.authorizeRead(ctx, subject); err != nil {
		return BackupMetadata{}, err
	}
	if !validReference(id) {
		return BackupMetadata{}, ErrBackupNotFound
	}
	var value BackupMetadata
	var created, updated string
	err := m.o.DB.QueryRow(ctx, `SELECT id,task_id,kind,status,sha256,bytes,created_at,updated_at,recipient_fingerprint,remote_status,error_code FROM cx_recovery_backups WHERE id=?`, id).Scan(&value.ID, &value.TaskID, &value.Kind, &value.Status, &value.SHA256, &value.Bytes, &created, &updated, &value.RecipientFingerprint, &value.RemoteStatus, &value.ErrorCode)
	if storage.IsNotFound(err) {
		return value, ErrBackupNotFound
	}
	if err != nil {
		return value, err
	}
	value.CreatedAt, err = storage.DecodeTime(created)
	if err != nil {
		return value, err
	}
	value.UpdatedAt, err = storage.DecodeTime(updated)
	if err != nil {
		return value, err
	}
	if m.maintenance != nil {
		if err = m.maintenance.enrich(ctx, &value); err != nil {
			return value, err
		}
	}
	return value, nil
}
func (m *BackupManager) OpenCipher(ctx context.Context, subject, requestID, id string) (*os.File, BackupMetadata, error) {
	value, err := m.Get(ctx, subject, id)
	if err != nil {
		return nil, value, err
	}
	if value.Status != "ready" || !validDigest(value.SHA256) || value.Bytes <= 0 || value.Bytes > MaxBackupBundleBytes {
		return nil, value, ErrBackupUnavailable
	}
	if value.LocalStatus != "" && value.LocalStatus != "present" {
		return nil, value, ErrBackupUnavailable
	}
	var name string
	if err = m.o.DB.QueryRow(ctx, `SELECT cipher_name FROM cx_recovery_backups WHERE id=?`, id).Scan(&name); err != nil || name != id+".age" {
		return nil, value, ErrBackupUnavailable
	}
	info, err := m.root.Lstat(name)
	if err != nil || !privateRecoveryInfo(info, false) || info.Size() != value.Bytes {
		return nil, value, ErrBackupUnavailable
	}
	file, err := m.root.Open(name)
	if err != nil {
		return nil, value, ErrBackupUnavailable
	}
	ok := false
	defer func() {
		if !ok {
			file.Close()
		}
	}()
	after, err := file.Stat()
	if err != nil || !os.SameFile(info, after) || !privateRecoveryInfo(after, false) {
		return nil, value, ErrBackupUnavailable
	}
	header := make([]byte, len("age-encryption.org/v1\n"))
	if _, err = io.ReadFull(file, header); err != nil || string(header) != "age-encryption.org/v1\n" {
		return nil, value, ErrBackupUnavailable
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return nil, value, err
	}
	hash := sha256.New()
	buffer := make([]byte, 64<<10)
	for {
		if err = m.authorizeRead(ctx, subject); err != nil {
			return nil, value, err
		}
		n, e := file.Read(buffer)
		if n > 0 {
			hash.Write(buffer[:n])
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, value, e
		}
	}
	if hex.EncodeToString(hash.Sum(nil)) != value.SHA256 {
		return nil, value, ErrBackupUnavailable
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return nil, value, err
	}
	if err = m.o.Audit.Append(ctx, audit.Event{SubjectID: subject, Action: ActionBackupDownload, Result: "success", RequestID: requestID, TaskID: value.TaskID, Source: audit.SourceWeb, ReasonCode: "ciphertext_download_admitted", At: time.Now().UTC()}); err != nil {
		return nil, value, err
	}
	if err = m.authorizeRead(ctx, subject); err != nil {
		return nil, value, err
	}
	if err = m.trackCipher(ctx, file, id); err != nil {
		return nil, value, err
	}
	ok = true
	return file, value, nil
}

func (m *BackupManager) trackCipher(ctx context.Context, file *os.File, id string) error {
	m.readersMu.Lock()
	defer m.readersMu.Unlock()
	if m.maintenance != nil {
		local, e := m.maintenance.LocalState(ctx, id)
		if e != nil || local.State != "present" {
			return ErrBackupUnavailable
		}
	}
	if m.readers == nil {
		m.readers = map[*os.File]string{}
	}
	for f := range m.readers {
		if _, e := f.Stat(); e != nil {
			delete(m.readers, f)
		}
	}
	m.readers[file] = id
	return nil
}
func (m *BackupManager) activeCipher(id string) bool {
	m.readersMu.Lock()
	defer m.readersMu.Unlock()
	return m.activeCipherLocked(id)
}
func (m *BackupManager) activeCipherLocked(id string) bool {
	active := false
	for f, key := range m.readers {
		if _, e := f.Stat(); e != nil {
			delete(m.readers, f)
		} else if key == id {
			active = true
		}
	}
	return active
}
func (m *BackupManager) ConfirmRecoveryKey(ctx context.Context, subject, requestID, challengeID, proof string) error {
	if err := m.authorizeRead(ctx, subject); err != nil {
		return err
	}
	if !validDigest(proof) || !validReference(challengeID) {
		return ErrBackupUnavailable
	}
	var nonce, kind, status, recipient string
	if err := m.o.DB.QueryRow(ctx, `SELECT c.nonce_sha256,b.kind,b.status,b.recipient_fingerprint FROM cx_recovery_recipient_checks c JOIN cx_recovery_backups b ON b.id=c.challenge_id WHERE c.recipient_fingerprint=? AND c.challenge_id=?`, m.RecipientFingerprint(), challengeID).Scan(&nonce, &kind, &status, &recipient); err != nil || kind != "key_challenge" || status != "ready" || recipient != m.RecipientFingerprint() || nonce != proof {
		return ErrBackupUnavailable
	}
	now := time.Now().UTC()
	return m.o.DB.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		result, err := tx.Exec(txCtx, `UPDATE cx_recovery_recipient_checks SET confirmed=1,confirmed_by=?,confirmed_at=? WHERE recipient_fingerprint=? AND challenge_id=? AND nonce_sha256=?`, subject, storage.EncodeTime(now), recipient, challengeID, proof)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return ErrBackupUnavailable
		}
		return m.o.Audit.AppendTx(txCtx, tx, audit.Event{SubjectID: subject, Action: ActionRecoveryKeyConfirm, Result: "success", RequestID: requestID, Source: audit.SourceWeb, ReasonCode: "offline_recovery_key_confirmed", At: now})
	})
}
func (m *BackupManager) VerifyCipher(ctx context.Context, subject, requestID, id string) (BackupVerification, error) {
	file, _, err := m.OpenCipher(ctx, subject, requestID, id)
	if err != nil {
		return BackupVerification{Valid: false, Reason: "ciphertext_integrity_not_verified"}, err
	}
	file.Close()
	return BackupVerification{Valid: true, Reason: "ciphertext_sha256_verified_restore_not_proven"}, nil
}

// ProveRecoveryKey decrypts ONLY a 32-byte setup nonce on the offline operator
// host. The public proof digest is safe to return; no private key leaves disk.
func ProveRecoveryKey(ctx context.Context, cipher *AgeCipher, identityPath, challengePath string) (string, error) {
	if cipher == nil || !filepath.IsAbs(challengePath) {
		return "", ErrBackupUnavailable
	}
	root, err := openRecoveryDirectory(filepath.Dir(challengePath))
	if err != nil {
		return "", err
	}
	defer root.Close()
	raw, err := readRecoveryFile(root, filepath.Base(challengePath), 16<<10)
	if err != nil {
		return "", err
	}
	var nonce bytes.Buffer
	limited := &boundedBackupWriter{writer: &nonce, max: 32}
	if err = cipher.DecryptOffline(ctx, identityPath, bytes.NewReader(raw), limited); err != nil || nonce.Len() != 32 {
		return "", ErrBackupUnavailable
	}
	defer clear(nonce.Bytes())
	digest := sha256.Sum256(nonce.Bytes())
	return hex.EncodeToString(digest[:]), nil
}
