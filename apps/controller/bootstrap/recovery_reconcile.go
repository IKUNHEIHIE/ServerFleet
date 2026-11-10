package bootstrap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"path/filepath"
	"time"

	"serverfleet.local/serverfleet/apps/controller/localpki"
	"serverfleet.local/serverfleet/apps/controller/server"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/recovery"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/transport"
)

func readOfflineRecord(path string, value any, limit int64) ([]byte, error) {
	raw, err := recovery.ReadPrivateFile(path, limit)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return nil, recovery.ErrRecoveryRejected
	}
	canonical, err := json.Marshal(value)
	if err != nil || !bytes.Equal(bytes.TrimSpace(raw), canonical) {
		return nil, recovery.ErrRecoveryRejected
	}
	return raw, nil
}
func reconcileStagedBackup(ctx context.Context, directory, authorityPath, newPKIDirectory, rootPin string) (recovery.ReconciliationReceipt, error) {
	if ctx == nil || ctx.Err() != nil {
		return recovery.ReconciliationReceipt{}, recovery.ErrRecoveryRejected
	}
	var staged recovery.StagedBundle
	var manifest recovery.BackupManifest
	if _, err := readOfflineRecord(filepath.Join(directory, "staged.json"), &staged, 16<<10); err != nil {
		return recovery.ReconciliationReceipt{}, err
	}
	manifestRaw, err := readOfflineRecord(filepath.Join(directory, "manifest.json"), &manifest, 8<<20)
	if err != nil {
		return recovery.ReconciliationReceipt{}, err
	}
	hash := sha256.Sum256(manifestRaw)
	if hex.EncodeToString(hash[:]) != staged.ManifestSHA256 || staged.State != "quarantined_authenticated_bundle" {
		return recovery.ReconciliationReceipt{}, recovery.ErrRecoveryRejected
	}
	authority, err := localpki.VerifyRecoveryAuthority(filepath.Join(directory, "pki/root.pem"), authorityPath, rootPin)
	if err != nil {
		return recovery.ReconciliationReceipt{}, err
	}
	p := authority.Payload()
	if p.CipherSHA256 != staged.CipherSHA256 || p.ManifestSHA256 != staged.ManifestSHA256 || p.OldAuthorityID != manifest.AuthorityID || p.RootSPKI != manifest.RootSPKI || p.NewAuthorityID != staged.Hold.ControllerID || p.TrustGeneration < staged.Hold.MinTrustGeneration || p.RevocationSequence < staged.Hold.MinRevocationSequence || p.TrustGeneration <= manifest.TrustGeneration || p.RevocationSequence < manifest.Snapshot.RevocationSequence {
		return recovery.ReconciliationReceipt{}, recovery.ErrRecoveryRejected
	}
	latch, err := recovery.OpenQuarantine(directory, staged.Hold, recovery.QuarantineVerifier{})
	if err != nil {
		return recovery.ReconciliationReceipt{}, err
	}
	defer latch.Close()
	newPKI, err := localpki.LoadProvisioned(newPKIDirectory, rootPin)
	if err != nil || !newPKI.Provisioned || newPKI.AuthorityID != p.NewAuthorityID || newPKI.TrustGeneration != p.TrustGeneration || newPKI.IssuerSHA256 != p.IssuerSHA256 {
		return recovery.ReconciliationReceipt{}, recovery.ErrRecoveryRejected
	}
	archived, err := localpki.VerifyArchivedProvisioning(filepath.Join(directory, "pki"), rootPin, manifest.CreatedAt)
	if err != nil || archived.IssuerSHA256 == newPKI.IssuerSHA256 {
		return recovery.ReconciliationReceipt{}, recovery.ErrRecoveryRejected
	}
	if recovery.VerifyStagedFiles(ctx, directory, manifest) != nil || validateStagedBundle(ctx, directory, manifest) != nil {
		return recovery.ReconciliationReceipt{}, recovery.ErrRecoveryRejected
	}
	now := time.Now().UTC()
	if authority.Current(now) != nil {
		return recovery.ReconciliationReceipt{}, recovery.ErrRecoveryRejected
	}
	receipt := recovery.ReconciliationReceipt{HoldID: staged.Hold.ID, AuthoritySHA256: authority.SHA256(), CipherSHA256: p.CipherSHA256, ManifestSHA256: p.ManifestSHA256, OldAuthorityID: p.OldAuthorityID, NewAuthorityID: p.NewAuthorityID, RootSPKI: p.RootSPKI, IssuerSHA256: p.IssuerSHA256, TrustGeneration: p.TrustGeneration, RevocationSequence: p.RevocationSequence + 1, CreatedAt: now}
	// An interrupted attempt is never automatically replayed, even if the SQL
	// transaction rolled back. The private target and hold remain available.
	if recovery.WriteOfflineRecord(directory, "reconcile-attempt.json", receipt) != nil {
		return recovery.ReconciliationReceipt{}, recovery.ErrRecoveryRejected
	}
	if auth.RotateCredentialMasterKey(filepath.Join(directory, "credentials")) != nil {
		return recovery.ReconciliationReceipt{}, recovery.ErrRecoveryRejected
	}
	db, err := storage.Open(ctx, storage.Config{Path: filepath.Join(directory, "restored.db")})
	if err != nil {
		return recovery.ReconciliationReceipt{}, err
	}
	defer db.Close()
	migrator, err := storage.NewMigrator(server.Migrations()...)
	if err != nil {
		return recovery.ReconciliationReceipt{}, err
	}
	if _, err = migrator.Apply(ctx, db); err != nil {
		return recovery.ReconciliationReceipt{}, err
	}
	limits := audit.DefaultLimits()
	limits.RequireSource = true
	validator, err := audit.NewValidator(server.AuditActions(), limits)
	if err != nil {
		return recovery.ReconciliationReceipt{}, err
	}
	recorder, err := audit.NewSQLRecorder(db, validator, time.Now)
	if err != nil {
		return recovery.ReconciliationReceipt{}, err
	}
	err = db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if authority.Current(time.Now()) != nil {
			return recovery.ErrRecoveryRejected
		}
		if e := auth.ApplyRestoredIdentityTx(txCtx, tx, auth.RestoredIdentityPolicy{ActorID: p.ID, At: now, RevokedCredentialIDs: p.RevokedCredentialIDs, DisabledSubjectIDs: p.DisabledSubjectIDs}); e != nil {
			return e
		}
		if e := tasks.InvalidateRestoredTasksTx(txCtx, tx, now); e != nil {
			return e
		}
		if e := tasks.InvalidateRestoredIncidentsTx(txCtx, tx, now); e != nil {
			return e
		}
		if e := recovery.InvalidateRestoredBackupPolicyTx(txCtx, tx); e != nil {
			return e
		}
		// Revoke union, including all identities in the old snapshot. No node can
		// be resurrected using an old certificate or a restored enrollment token.
		serials := append(append([]string{}, manifest.Snapshot.RevokedSerials...), p.RevokedSerials...)
		if e := transport.ReconcileRestoredEnrollmentTx(txCtx, tx, transport.RestoreEnrollmentPolicy{Generation: p.TrustGeneration, RevocationSequence: p.RevocationSequence, RevokedSerials: serials, At: now}); e != nil {
			return e
		}
		if e := certificates.InvalidateRestoredCleanupTx(txCtx, tx, now); e != nil {
			return e
		}
		if e := recovery.SuspendRestoredResidentAuthorityTx(txCtx, tx, p.OldAuthorityID, p.NewAuthorityID); e != nil {
			return e
		}
		if e := certificates.InvalidateRestoredRenewalsTx(txCtx, tx, now); e != nil {
			return e
		}
		if e := recovery.RecordReconciliationTx(txCtx, tx, receipt); e != nil {
			return e
		}
		return recorder.AppendTx(txCtx, tx, audit.Event{SubjectID: p.ID, Action: "recovery.reconcile", Result: "success", RequestID: p.ID, Source: audit.SourceSystem, ReasonCode: "quarantined_no_authority_release", At: now})
	})
	if err != nil {
		return recovery.ReconciliationReceipt{}, err
	}
	if err = recovery.WriteOfflineRecord(directory, "reconciled.json", receipt); err != nil {
		return recovery.ReconciliationReceipt{}, err
	}
	// Neither this transaction nor a Root signature proves the old Controller
	// is fenced. There is intentionally no latch.Release or online startup here.
	return receipt, nil
}
