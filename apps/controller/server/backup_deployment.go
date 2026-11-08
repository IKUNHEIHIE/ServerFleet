package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/url"
	"path/filepath"
	"time"

	"serverfleet.local/serverfleet/apps/controller/localpki"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/recovery"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/transport"
)

// Paths and public recipient are startup-only. The offline age identity and
// Root private key are deliberately absent from this online configuration.
type BackupDeployment struct {
	Directory        string `json:"directory"`
	StageDir         string `json:"stageDir"`
	AgeBinary        string `json:"ageBinary"`
	AgeSHA256        string `json:"ageSha256"`
	Recipient        string `json:"recipient"`
	AutomaticOwnerID string `json:"automaticOwnerId,omitempty"`
	// Fixed by the real management bootstrap, never accepted from deployment JSON.
	PKIDirectory, RootPin, TLSCertificate, TLSKey, CredentialDirectory, AgentEndpoint, ReleaseVersion string `json:"-"`
}

func LoadBackupDeployment(filename string) (*BackupDeployment, error) {
	raw, err := readPrivateDeployment(filename)
	if err != nil {
		return nil, recovery.ErrBackupUnavailable
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if gatewayDeploymentUniqueJSON(d) != nil {
		return nil, recovery.ErrBackupUnavailable
	}
	var trailing any
	if d.Decode(&trailing) != io.EOF {
		return nil, recovery.ErrBackupUnavailable
	}
	var doc struct {
		Version int `json:"version"`
		BackupDeployment
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&doc) != nil || doc.Version != 1 {
		return nil, recovery.ErrBackupUnavailable
	}
	for _, path := range []string{doc.Directory, doc.StageDir, doc.AgeBinary} {
		if !validGatewayDeploymentPath(path) {
			return nil, recovery.ErrBackupUnavailable
		}
	}
	if doc.Directory == doc.StageDir || doc.AutomaticOwnerID != "" && !deploymentID.MatchString(doc.AutomaticOwnerID) {
		return nil, recovery.ErrBackupUnavailable
	}
	return &doc.BackupDeployment, nil
}
func (s *Server) setupEncryptedBackups(ctx context.Context, cfg Config, workers map[string]tasks.Worker) (*tasks.ControllerExecution, error) {
	if cfg.BackupDeployment == nil {
		return nil, nil
	}
	d := *cfg.BackupDeployment
	material, err := localpki.LoadProvisioned(d.PKIDirectory, d.RootPin)
	if err != nil || !material.Provisioned {
		return nil, recovery.ErrBackupUnavailable
	}
	cipher, err := recovery.NewAgeCipher(d.AgeBinary, d.AgeSHA256)
	if err != nil {
		return nil, err
	}
	keyDir := d.CredentialDirectory
	if keyDir == "" {
		keyDir = cfg.CredentialKeyDir
	}
	if keyDir == "" {
		absolute, e := filepath.Abs(cfg.DBPath)
		if e != nil {
			return nil, e
		}
		keyDir = filepath.Join(filepath.Dir(absolute), "credential-keys")
	}
	sources := []recovery.BackupFileSource{}
	for name, path := range map[string]string{
		"pki/root.pem":                     filepath.Join(d.PKIDirectory, localpki.ProvisionedRootName),
		"pki/issuer.pem":                   filepath.Join(d.PKIDirectory, localpki.ProvisionedIssuerName),
		"pki/issuer-key.pem":               filepath.Join(d.PKIDirectory, localpki.ProvisionedKeyName),
		"pki/pki.json":                     filepath.Join(d.PKIDirectory, localpki.ProvisionedManifestName),
		"credentials/credential-keys.json": filepath.Join(keyDir, "credential-keys.json"),
		"tls/controller.pem":               d.TLSCertificate, "tls/controller-key.pem": d.TLSKey,
	} {
		source, e := recovery.FreezeBackupFile(name, path)
		if e != nil {
			return nil, e
		}
		sources = append(sources, source)
	}
	validate := func(current context.Context) error {
		if current.Err() != nil {
			return current.Err()
		}
		m, e := localpki.LoadProvisioned(d.PKIDirectory, d.RootPin)
		if e != nil || m.AuthorityID != material.AuthorityID || m.TrustGeneration != material.TrustGeneration {
			return recovery.ErrBackupUnavailable
		}
		for _, file := range sources {
			if recovery.CheckBackupFile(file) != nil {
				return recovery.ErrBackupUnavailable
			}
		}
		certPEM, e := recovery.ReadPrivateFile(d.TLSCertificate, 128<<10)
		if e != nil {
			return e
		}
		defer clear(certPEM)
		keyPEM, e := recovery.ReadPrivateFile(d.TLSKey, 128<<10)
		if e != nil {
			return e
		}
		defer clear(keyPEM)
		pair, e := tls.X509KeyPair(certPEM, keyPEM)
		if e != nil || len(pair.Certificate) == 0 {
			return recovery.ErrBackupUnavailable
		}
		leaf, e := x509.ParseCertificate(pair.Certificate[0])
		if e != nil || leaf.IsCA || time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) {
			return recovery.ErrBackupUnavailable
		}
		origin, e := url.Parse(cfg.Origin)
		if e != nil || leaf.VerifyHostname(origin.Hostname()) != nil {
			return recovery.ErrBackupUnavailable
		}
		hash := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
		if hex.EncodeToString(hash[:]) == d.RootPin || hex.EncodeToString(hash[:]) == material.IssuerSPKI {
			return recovery.ErrBackupUnavailable
		}
		return nil
	}
	if err = validate(ctx); err != nil {
		return nil, err
	}
	if _, _, err = net.SplitHostPort(d.AgentEndpoint); err != nil {
		return nil, recovery.ErrBackupUnavailable
	}
	manager, err := recovery.NewBackupManager(recovery.BackupDeploymentOptions{
		DB: s.DB, Audit: s.Audit, Directory: d.Directory, StageDir: d.StageDir, Recipient: d.Recipient, Cipher: cipher, Files: sources,
		AuthorityID: material.AuthorityID, RootSPKI: material.RootSPKI, TrustGeneration: material.TrustGeneration, AgentEndpoint: d.AgentEndpoint, Version: d.ReleaseVersion,
		Authorize: s.Identity.AuthorizeAdministrator, RuntimeCurrent: s.runtimeGate, ValidateConfiguration: validate,
		TaskCurrent: func(ctx context.Context, id, plan string) (tasks.Task, error) {
			if s.Tasks == nil {
				return tasks.Task{}, tasks.ErrInvalidTask
			}
			return s.Tasks.CheckExecution(ctx, id, plan)
		},
		Capture: func(ctx context.Context, path string) (recovery.Snapshot, []string, error) {
			snapshot, e := storage.Open(ctx, storage.Config{Path: path, ReadOnly: true})
			if e != nil {
				return recovery.Snapshot{}, nil, e
			}
			defer snapshot.Close()
			enrollment, e := transport.ReadRecoveryEnrollment(ctx, snapshot)
			if e != nil {
				return recovery.Snapshot{}, nil, e
			}
			records, e := tasks.ReadRecoveryTasks(ctx, snapshot)
			if e != nil {
				return recovery.Snapshot{}, nil, e
			}
			state := recovery.Snapshot{RootSPKISHA256: material.RootSPKI, TrustGeneration: material.TrustGeneration, RevocationSequence: enrollment.RevocationSequence, RevokedSerials: enrollment.RevokedSerials, Tasks: []recovery.TaskRecord{}}
			for _, record := range records {
				state.Tasks = append(state.Tasks, recovery.TaskRecord{ID: record.ID, State: record.State, Expires: record.Expires})
			}
			return state, enrollment.NodeIDs, nil
		},
	})
	if err != nil {
		return nil, err
	}
	s.Backups = manager
	s.backupAutomaticOwner = d.AutomaticOwnerID
	workers[recovery.ActionCreateBackup] = manager
	workers[recovery.ActionRecoveryKeyChallenge] = manager
	return &tasks.ControllerExecution{AuthorityID: material.AuthorityID, Actions: map[string]tasks.ControllerAction{recovery.ActionCreateBackup: {AllowSystem: d.AutomaticOwnerID != ""}, recovery.ActionRecoveryKeyChallenge: {}}, Authorize: func(ctx context.Context, subject, action, operation string) error {
		if action != recovery.ActionCreateBackup && action != recovery.ActionRecoveryKeyChallenge {
			return auth.ErrDenied
		}
		return s.Identity.AuthorizeAdministrator(ctx, subject)
	}}, nil
}
func (s *Server) submitBackup(ctx context.Context, p recovery.Principal, kind, key string, source audit.Source) (tasks.Task, error) {
	if s.Backups == nil || s.Tasks == nil || s.runtimeGate(ctx) != nil || s.Identity.AuthorizeAdministrator(ctx, p.SubjectID) != nil {
		return tasks.Task{}, auth.ErrDenied
	}
	action, name := recovery.ActionCreateBackup, "Encrypted Controller backup"
	if kind == "key_challenge" {
		action, name = recovery.ActionRecoveryKeyChallenge, "Offline recovery-key confirmation"
	} else if kind != "full" {
		return tasks.Task{}, tasks.ErrInvalidTask
	}
	return s.Tasks.SubmitController(ctx, tasks.SubmitRequest{SubjectID: p.SubjectID, RequestID: p.RequestID, Name: name, ActionID: action, OperationClass: tasks.OperationClassStandard, Params: map[string]string{"kind": kind, "recipientFingerprint": s.Backups.RecipientFingerprint()}, IdempotencyKey: backupTaskKey(kind, key), Source: source})
}

// This loop only produces durable unified tasks. It cannot read plaintext,
// encrypt, upload, or retry an UNKNOWN task outside the normal worker gates.
func (s *Server) runBackupSchedule(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.Backups == nil || s.backupAutomaticOwner == "" || s.runtimeGate(ctx) != nil {
				continue
			}
			confirmed, err := s.Backups.KeyConfirmed(ctx)
			if err != nil || !confirmed {
				continue
			}
			day := time.Now().UTC().Format("2006-01-02")
			_, _ = s.submitBackup(ctx, recovery.Principal{SubjectID: s.backupAutomaticOwner, RequestID: "backup-daily-" + day}, "full", "daily-"+day, audit.SourceSystem)
		}
	}
}

func backupTaskKey(kind, key string) string {
	sum := sha256.Sum256([]byte(key))
	return "backup-" + kind + "-" + hex.EncodeToString(sum[:])
}
