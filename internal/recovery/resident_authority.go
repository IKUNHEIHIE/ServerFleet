package recovery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"serverfleet.local/serverfleet/internal/storage"
)

type ResidentAuthority struct{ AuthorityID, PolicySHA256, Directory, Mode string }

func ResidentAuthorityMigrations() []storage.Migration {
	return []storage.Migration{{Version: "cx/0019_controller_supervision", Name: "persistent authority binding to owned supervisor and external fence", Statements: []string{`CREATE TABLE cx_controller_supervision(id TEXT PRIMARY KEY CHECK(id='current'),authority_id TEXT NOT NULL,policy_sha256 TEXT NOT NULL,directory TEXT NOT NULL,mode TEXT NOT NULL CHECK(mode IN ('owned','restored')))`, `CREATE TABLE cx_recovery_activation(id TEXT PRIMARY KEY CHECK(id='current'),receipt TEXT NOT NULL)`, `CREATE TABLE cx_recovery_reconciliation_history(authority_sha256 TEXT PRIMARY KEY,receipt TEXT NOT NULL)`}}}
}
func ReadResidentAuthority(ctx context.Context, q storage.Queryer) (*ResidentAuthority, error) {
	var r ResidentAuthority
	e := q.QueryRow(ctx, `SELECT authority_id,policy_sha256,directory,mode FROM cx_controller_supervision WHERE id='current'`).Scan(&r.AuthorityID, &r.PolicySHA256, &r.Directory, &r.Mode)
	if storage.IsNotFound(e) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	return &r, nil
}
func BindResidentAuthorityTx(ctx context.Context, tx storage.Tx, r ResidentAuthority, restored bool) error {
	if !validReference(r.AuthorityID) || !validDigest(r.PolicySHA256) || !filepath.IsAbs(r.Directory) || r.Mode != "owned" {
		return ErrRecoveryRejected
	}
	old, e := ReadResidentAuthority(ctx, tx)
	if e != nil {
		return e
	}
	if old != nil {
		if *old == r {
			return nil
		}
		if !restored || old.Mode != "restored" || old.AuthorityID != r.AuthorityID {
			return ErrRecoveryRejected
		}
		_, e = tx.Exec(ctx, `UPDATE cx_controller_supervision SET policy_sha256=?,directory=?,mode='owned' WHERE id='current'`, r.PolicySHA256, r.Directory)
		return e
	}
	if restored {
		return ErrRecoveryRejected
	}
	_, e = tx.Exec(ctx, `INSERT INTO cx_controller_supervision VALUES('current',?,?,?,'owned')`, r.AuthorityID, r.PolicySHA256, r.Directory)
	return e
}
func SuspendRestoredResidentAuthorityTx(ctx context.Context, tx storage.Tx, oldID, newID string) error {
	if !validReference(newID) || newID == oldID {
		return ErrRecoveryRejected
	}
	current, e := ReadResidentAuthority(ctx, tx)
	if e != nil {
		return e
	}
	if current != nil && current.AuthorityID != oldID {
		return ErrRecoveryRejected
	}
	if _, e = tx.Exec(ctx, `DELETE FROM cx_recovery_activation`); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO cx_controller_supervision VALUES('current',?,'','','restored') ON CONFLICT(id) DO UPDATE SET authority_id=excluded.authority_id,policy_sha256='',directory='',mode='restored'`, newID)
	return e
}

// Every live boundary rereads owner metadata and external fence intent. A
// process launched without its bound supervisor fails even when flags omitted.
func ResidentAuthorityGate(db *storage.DB, expected *ResidentAuthority, parentPolicy string, base func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		if base == nil || base(ctx) != nil {
			return ErrQuarantined
		}
		actual, e := ReadResidentAuthority(ctx, db)
		if e != nil {
			return ErrQuarantined
		}
		if actual == nil {
			if expected != nil {
				return ErrQuarantined
			}
			return nil
		}
		if expected == nil || actual.Mode != "owned" || *actual != *expected || parentPolicy != actual.PolicySHA256 {
			return ErrQuarantined
		}
		for _, name := range []string{"fenced-intent.json", "fenced.json"} {
			if _, e := os.Lstat(filepath.Join(actual.Directory, name)); !errors.Is(e, os.ErrNotExist) {
				return ErrQuarantined
			}
		}
		return nil
	}
}
func ReadReconciliation(ctx context.Context, q storage.Queryer) (ReconciliationReceipt, error) {
	var r ReconciliationReceipt
	var at string
	e := q.QueryRow(ctx, `SELECT hold_id,authority_sha256,cipher_sha256,manifest_sha256,old_authority_id,new_authority_id,root_spki,issuer_sha256,trust_generation,revocation_sequence,created_at FROM cx_recovery_reconciliation WHERE id='current'`).Scan(&r.HoldID, &r.AuthoritySHA256, &r.CipherSHA256, &r.ManifestSHA256, &r.OldAuthorityID, &r.NewAuthorityID, &r.RootSPKI, &r.IssuerSHA256, &r.TrustGeneration, &r.RevocationSequence, &at)
	if e != nil {
		return r, e
	}
	r.CreatedAt, e = storage.DecodeTime(at)
	return r, e
}

// Opaque canonical activation receipt owned by recovery. Caller composition
// performs live process fence/Root checks; this port makes its result immutable
// with audit and the authority binding in the SAME transaction.
func RecordResidentActivationTx(ctx context.Context, tx storage.Tx, raw string) error {
	if len(raw) == 0 || len(raw) > 16384 {
		return ErrRecoveryRejected
	}
	_, e := tx.Exec(ctx, `INSERT INTO cx_recovery_activation(id,receipt) VALUES('current',?)`, raw)
	return e
}
func ReadResidentActivation(ctx context.Context, q storage.Queryer) (string, error) {
	var raw string
	e := q.QueryRow(ctx, `SELECT receipt FROM cx_recovery_activation WHERE id='current'`).Scan(&raw)
	return raw, e
}

// During a signed image transaction only private parent readiness may bypass
// maintenance. This gate grants no execution authority and never releases a
// crash/incomplete lock. All task and management boundaries remain blocked.
func ResidentMaintenanceGate(directory string, base func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		if base == nil || base(ctx) != nil {
			return ErrQuarantined
		}
		if _, e := os.Lstat(filepath.Join(directory, "update-lock.json")); !errors.Is(e, os.ErrNotExist) {
			return ErrQuarantined
		}
		return nil
	}
}
