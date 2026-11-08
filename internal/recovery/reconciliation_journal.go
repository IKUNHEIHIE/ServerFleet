package recovery

import (
	"context"
	"serverfleet.local/serverfleet/internal/storage"
	"time"
)

func ReconciliationMigrations() []storage.Migration {
	return []storage.Migration{{Version: "cx/0013_recovery_reconciliation", Name: "immutable offline reconciliation authority binding", Statements: []string{`CREATE TABLE cx_recovery_reconciliation(id TEXT PRIMARY KEY CHECK(id='current'),hold_id TEXT NOT NULL,authority_sha256 TEXT NOT NULL,cipher_sha256 TEXT NOT NULL,manifest_sha256 TEXT NOT NULL,old_authority_id TEXT NOT NULL,new_authority_id TEXT NOT NULL,root_spki TEXT NOT NULL,issuer_sha256 TEXT NOT NULL,trust_generation INTEGER NOT NULL,revocation_sequence INTEGER NOT NULL,created_at TEXT NOT NULL)`}}}
}

type ReconciliationReceipt struct {
	HoldID             string    `json:"holdId"`
	AuthoritySHA256    string    `json:"authoritySha256"`
	CipherSHA256       string    `json:"cipherSha256"`
	ManifestSHA256     string    `json:"manifestSha256"`
	OldAuthorityID     string    `json:"oldAuthorityId"`
	NewAuthorityID     string    `json:"newAuthorityId"`
	RootSPKI           string    `json:"rootSpkiSha256"`
	IssuerSHA256       string    `json:"issuerSha256"`
	TrustGeneration    uint64    `json:"trustGeneration"`
	RevocationSequence uint64    `json:"revocationSequence"`
	CreatedAt          time.Time `json:"createdAt"`
}

func RecordReconciliationTx(ctx context.Context, tx storage.Tx, r ReconciliationReceipt) error {
	for _, digest := range []string{r.HoldID, r.AuthoritySHA256, r.CipherSHA256, r.ManifestSHA256, r.RootSPKI, r.IssuerSHA256} {
		if !validDigest(digest) {
			return ErrRecoveryRejected
		}
	}
	if !validReference(r.OldAuthorityID) || !validReference(r.NewAuthorityID) || r.NewAuthorityID == r.OldAuthorityID || r.TrustGeneration == 0 || r.CreatedAt.IsZero() {
		return ErrRecoveryRejected
	}
	if _, err := tx.Exec(ctx, `INSERT INTO cx_recovery_reconciliation VALUES('current',?,?,?,?,?,?,?,?,?,?,?)`, r.HoldID, r.AuthoritySHA256, r.CipherSHA256, r.ManifestSHA256, r.OldAuthorityID, r.NewAuthorityID, r.RootSPKI, r.IssuerSHA256, r.TrustGeneration, r.RevocationSequence, storage.EncodeTime(r.CreatedAt)); err != nil {
		return err
	}
	// Backup metadata in an old database does not prove corresponding ciphertext
	// was restored, and an old recipient challenge is not a new-key confirmation.
	if _, err := tx.Exec(ctx, `UPDATE cx_recovery_backups SET status='unknown',error_code='offline_restore_ciphertext_not_materialized',updated_at=? WHERE status IN ('preparing','ready')`, storage.EncodeTime(r.CreatedAt)); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE cx_recovery_recipient_checks SET confirmed=0,confirmed_by='',confirmed_at=''`)
	return err
}
