package certificates

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/storage"
	"time"
)

type PublicOrder struct {
	TaskID         string            `json:"taskId"`
	NodeID         string            `json:"nodeId"`
	CSRTaskID      string            `json:"csrTaskId"`
	Domain         string            `json:"domain"`
	KeyRef         string            `json:"keyRef"`
	PlanSHA256     string            `json:"planSha256"`
	ProfileSHA256  string            `json:"profileSha256"`
	ZoneID         string            `json:"zoneId"`
	Status         string            `json:"status"`
	ChainPEM       []byte            `json:"certificatePem,omitempty"`
	ChainSHA256    string            `json:"chainSha256,omitempty"`
	Serial         string            `json:"serial,omitempty"`
	NotAfter       time.Time         `json:"notAfter,omitempty"`
	PendingCleanup []ACMEDNS01Record `json:"pendingCleanup,omitempty"`
	ErrorCode      string            `json:"errorCode,omitempty"`
	UpdatedAt      time.Time         `json:"updatedAt"`
}

func PublicOrderMigrations() []storage.Migration {
	return []storage.Migration{{Version: "cx/0018_public_certificate_orders", Name: "approved public certificate orders and durable unknown cleanup evidence", Statements: []string{`CREATE TABLE cx_public_certificate_orders(task_id TEXT PRIMARY KEY,payload BLOB NOT NULL CHECK(length(payload)<=65536))`}}}
}
func ReservePublicOrderTx(ctx context.Context, tx storage.Tx, order PublicOrder) error {
	if order.TaskID == "" || order.Status != "running" || len(order.ChainPEM) != 0 || order.PlanSHA256 == "" || order.ProfileSHA256 == "" {
		return ErrInvalidInput
	}
	raw, err := json.Marshal(order)
	if err != nil || len(raw) > 64<<10 {
		return ErrInvalidInput
	}
	_, err = tx.Exec(ctx, `INSERT INTO cx_public_certificate_orders VALUES(?,?)`, order.TaskID, raw)
	return err
}
func ReadPublicOrder(ctx context.Context, db storage.Queryer, id string) (PublicOrder, error) {
	var order PublicOrder
	var raw []byte
	if err := db.QueryRow(ctx, `SELECT payload FROM cx_public_certificate_orders WHERE task_id=? AND length(payload)<=65536`, id).Scan(&raw); err != nil {
		return order, err
	}
	if json.Unmarshal(raw, &order) != nil || order.TaskID != id {
		return order, ErrInvalidInput
	}
	return order, nil
}
func FinishPublicOrderTx(ctx context.Context, tx storage.Tx, order PublicOrder) error {
	previous, err := ReadPublicOrder(ctx, tx, order.TaskID)
	if err != nil || previous.Status != "running" || previous.NodeID != order.NodeID || previous.CSRTaskID != order.CSRTaskID || previous.PlanSHA256 != order.PlanSHA256 || previous.ProfileSHA256 != order.ProfileSHA256 || previous.ZoneID != order.ZoneID || previous.Domain != order.Domain || previous.KeyRef != order.KeyRef {
		return ErrInvalidInput
	}
	switch order.Status {
	case "issued", "issued_cleanup_pending", "unknown":
	default:
		return ErrInvalidInput
	}
	if len(order.ChainPEM) > 8<<10 || len(order.PendingCleanup) > 16 {
		return ErrInvalidInput
	}
	raw, err := json.Marshal(order)
	if err != nil || len(raw) > 64<<10 {
		return ErrInvalidInput
	}
	_, err = tx.Exec(ctx, `UPDATE cx_public_certificate_orders SET payload=? WHERE task_id=?`, raw, order.TaskID)
	return err
}

// RecoverPublicOrders never replays a CA/DNS call on restart. All public
// evidence remains queryable; a prior running order is durably UNKNOWN.
func RecoverPublicOrders(ctx context.Context, db *storage.DB, sink AuditSink) error {
	if sink == nil {
		return ErrACMEConfig
	}
	rows, err := db.Query(ctx, `SELECT task_id,payload FROM cx_public_certificate_orders`)
	if err != nil {
		return err
	}
	defer rows.Close()
	pending := []PublicOrder{}
	for rows.Next() {
		var id string
		var raw []byte
		if rows.Scan(&id, &raw) != nil {
			return ErrInvalidInput
		}
		var order PublicOrder
		if len(raw) > 64<<10 || json.Unmarshal(raw, &order) != nil || order.TaskID != id {
			return ErrInvalidInput
		}
		if order.Status == "running" {
			order.Status = "unknown"
			order.ErrorCode = "controller_restarted_no_replay"
			order.UpdatedAt = time.Now().UTC()
			pending = append(pending, order)
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	rows.Close()
	for _, order := range pending {
		if err = db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
			if err := FinishPublicOrderTx(ctx, tx, order); err != nil {
				return err
			}
			return sink.AppendTx(ctx, tx, audit.Event{SubjectID: "controller-recovery", Action: "certificates.public.issue", Result: "failure", RequestID: publicOrderRecoveryID(order.TaskID), TaskID: order.TaskID, Source: audit.SourceSystem, ReasonCode: "restarted_public_order_unknown", At: order.UpdatedAt})
		}); err != nil {
			return err
		}
	}
	return nil
}

// ObservePublicOrderChallengesTx records the intended TXT identity before the
// provider call and the returned ID immediately after. An intent with no ID is
// evidence of uncertainty, never permission to search/delete by name alone.
func ObservePublicOrderChallengesTx(ctx context.Context, tx storage.Tx, taskID, plan string, records []ACMEDNS01Record) error {
	order, err := ReadPublicOrder(ctx, tx, taskID)
	if err != nil || order.Status != "running" || order.PlanSHA256 != plan || len(records) > 16 {
		return ErrInvalidInput
	}
	for _, r := range records {
		if r.FQDN == "" || len(r.FQDN) > 255 || r.Value == "" || len(r.Value) > 255 || len(r.RecordID) > 256 {
			return ErrInvalidInput
		}
	}
	order.PendingCleanup = append([]ACMEDNS01Record{}, records...)
	order.UpdatedAt = time.Now().UTC()
	raw, err := json.Marshal(order)
	if err != nil || len(raw) > 64<<10 {
		return ErrInvalidInput
	}
	_, err = tx.Exec(ctx, `UPDATE cx_public_certificate_orders SET payload=? WHERE task_id=?`, raw, taskID)
	return err
}

func publicOrderRecoveryID(taskID string) string {
	sum := sha256.Sum256([]byte(taskID))
	return "recover-public-" + hex.EncodeToString(sum[:])
}
