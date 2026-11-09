package transport

import (
	"context"
	"encoding/json"
	"time"

	"serverfleet.local/serverfleet/internal/storage"
)

type CertificateCSRReceipt struct {
	TaskID, NodeID, PlanSHA256, Domain, KeyRef string
	PublicResult                               CertificateCSRResult
	ObservedAt                                 time.Time
}

func CertificateCSRMigrations() []storage.Migration {
	return []storage.Migration{{Version: "cx/0015_agent_certificate_csr", Name: "public node-local website CSR receipts bound to approved tasks", Statements: []string{`CREATE TABLE cx_agent_certificate_csrs(task_id TEXT PRIMARY KEY,node_id TEXT NOT NULL,plan_sha256 TEXT NOT NULL,domain TEXT NOT NULL,key_ref TEXT NOT NULL,public_result BLOB NOT NULL CHECK(length(public_result)<=8192),observed_at TEXT NOT NULL)`}}}
}
func RecordCertificateCSRTx(ctx context.Context, tx storage.Tx, r CertificateCSRReceipt) error {
	if ctx == nil || tx == nil || !validBindingID(r.TaskID) || len(r.PlanSHA256) != 64 || r.ObservedAt.IsZero() {
		return ErrPeerIdentity
	}
	raw, err := json.Marshal(r.PublicResult)
	if err != nil {
		return ErrPeerIdentity
	}
	if _, err = ValidateCertificateCSRResult(raw, r.NodeID, r.Domain, r.KeyRef); err != nil {
		return err
	}
	var existing []byte
	var node, plan string
	err = tx.QueryRow(ctx, `SELECT public_result,node_id,plan_sha256 FROM cx_agent_certificate_csrs WHERE task_id=?`, r.TaskID).Scan(&existing, &node, &plan)
	if err == nil {
		if string(existing) != string(raw) || node != r.NodeID || plan != r.PlanSHA256 {
			return ErrPeerIdentity
		}
		return nil
	}
	if !storage.IsNotFound(err) {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO cx_agent_certificate_csrs(task_id,node_id,plan_sha256,domain,key_ref,public_result,observed_at) VALUES(?,?,?,?,?,?,?)`, r.TaskID, r.NodeID, r.PlanSHA256, r.Domain, r.KeyRef, raw, storage.EncodeTime(r.ObservedAt))
	return err
}
func ReadCertificateCSR(ctx context.Context, db storage.Queryer, taskID string) (CertificateCSRReceipt, error) {
	var r CertificateCSRReceipt
	var raw []byte
	var observed string
	if db == nil || !validBindingID(taskID) {
		return r, ErrPeerIdentity
	}
	err := db.QueryRow(ctx, `SELECT task_id,node_id,plan_sha256,domain,key_ref,public_result,observed_at FROM cx_agent_certificate_csrs WHERE task_id=? AND length(public_result)<=8192`, taskID).Scan(&r.TaskID, &r.NodeID, &r.PlanSHA256, &r.Domain, &r.KeyRef, &raw, &observed)
	if err != nil {
		return r, err
	}
	r.PublicResult, err = ValidateCertificateCSRResult(raw, r.NodeID, r.Domain, r.KeyRef)
	if err != nil {
		return r, err
	}
	r.ObservedAt, err = storage.DecodeTime(observed)
	return r, err
}
