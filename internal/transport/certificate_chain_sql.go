package transport

import (
	"context"
	"encoding/json"
	"serverfleet.local/serverfleet/internal/storage"
	"time"
)

type CertificateChainReceipt struct {
	TaskID, CSRTaskID, NodeID, PlanSHA256 string
	Request                               CertificateInstallRequest
	Result                                CertificateInstallResult
	ObservedAt                            time.Time
}

func CertificateChainMigrations() []storage.Migration {
	return []storage.Migration{{Version: "cx/0016_node_certificate_chains", Name: "approved node-local public chain installation receipts", Statements: []string{`CREATE TABLE cx_node_certificate_chains(task_id TEXT PRIMARY KEY,csr_task_id TEXT NOT NULL,node_id TEXT NOT NULL,plan_sha256 TEXT NOT NULL,request_payload BLOB NOT NULL CHECK(length(request_payload)<=16384),result_payload BLOB NOT NULL CHECK(length(result_payload)<=2048),observed_at TEXT NOT NULL)`}}}
}
func RecordCertificateChainTx(ctx context.Context, tx storage.Tx, r CertificateChainReceipt) error {
	if ctx == nil || tx == nil || !validBindingID(r.TaskID) || !validBindingID(r.CSRTaskID) || !canonicalDigest(r.PlanSHA256) || r.ObservedAt.IsZero() {
		return ErrPeerIdentity
	}
	requestRaw, err := json.Marshal(r.Request)
	if err != nil {
		return err
	}
	if _, _, err = ParseCertificateInstallRequest(requestRaw); err != nil {
		return err
	}
	resultRaw, err := json.Marshal(r.Result)
	if err != nil {
		return err
	}
	if _, err = ValidateCertificateInstallResult(resultRaw, r.NodeID, r.Request); err != nil {
		return err
	}
	var priorRequest, priorResult []byte
	var node, plan, csrTask string
	err = tx.QueryRow(ctx, `SELECT request_payload,result_payload,node_id,plan_sha256,csr_task_id FROM cx_node_certificate_chains WHERE task_id=?`, r.TaskID).Scan(&priorRequest, &priorResult, &node, &plan, &csrTask)
	if err == nil {
		if string(priorRequest) != string(requestRaw) || string(priorResult) != string(resultRaw) || node != r.NodeID || plan != r.PlanSHA256 || csrTask != r.CSRTaskID {
			return ErrPeerIdentity
		}
		return nil
	}
	if !storage.IsNotFound(err) {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO cx_node_certificate_chains VALUES(?,?,?,?,?,?,?)`, r.TaskID, r.CSRTaskID, r.NodeID, r.PlanSHA256, requestRaw, resultRaw, storage.EncodeTime(r.ObservedAt))
	return err
}
func ReadCertificateChain(ctx context.Context, db storage.Queryer, taskID string) (CertificateChainReceipt, error) {
	var r CertificateChainReceipt
	var requestRaw, resultRaw []byte
	var observed string
	if ctx == nil || db == nil || !validBindingID(taskID) {
		return r, ErrPeerIdentity
	}
	err := db.QueryRow(ctx, `SELECT task_id,csr_task_id,node_id,plan_sha256,request_payload,result_payload,observed_at FROM cx_node_certificate_chains WHERE task_id=? AND length(request_payload)<=16384 AND length(result_payload)<=2048`, taskID).Scan(&r.TaskID, &r.CSRTaskID, &r.NodeID, &r.PlanSHA256, &requestRaw, &resultRaw, &observed)
	if err != nil {
		return r, err
	}
	r.Request, _, err = ParseCertificateInstallRequest(requestRaw)
	if err != nil {
		return r, err
	}
	r.Result, err = ValidateCertificateInstallResult(resultRaw, r.NodeID, r.Request)
	if err != nil {
		return r, err
	}
	r.ObservedAt, err = storage.DecodeTime(observed)
	return r, err
}
