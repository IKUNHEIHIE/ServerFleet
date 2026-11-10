package certificates

import (
	"context"
	"encoding/json"
	"serverfleet.local/serverfleet/internal/storage"
	"time"
)

type RenewalDelivery struct {
	PolicyID       string                `json:"policyId"`
	OwnerID        string                `json:"ownerId"`
	Revision       uint64                `json:"revision"`
	Enabled        bool                  `json:"enabled"`
	PublicationIDs []string              `json:"publicationIds"`
	Cycle          *RenewalDeliveryCycle `json:"cycle,omitempty"`
	UpdatedAt      time.Time             `json:"updatedAt"`
}
type RenewalPublication struct {
	BaseID                string `json:"baseId"`
	ID                    string `json:"id"`
	PreviousCertificateID string `json:"previousCertificateId"`
	CertificateID         string `json:"certificateId,omitempty"`
}
type RenewalDeliveryCycle struct {
	ID             string               `json:"id"`
	PolicyRevision uint64               `json:"policyRevision"`
	IssueTaskID    string               `json:"issueTaskId"`
	InstallTaskID  string               `json:"installTaskId,omitempty"`
	State          string               `json:"state"`
	ErrorCode      string               `json:"errorCode,omitempty"`
	Publications   []RenewalPublication `json:"publications"`
	UpdatedAt      time.Time            `json:"updatedAt"`
}

func RenewalDeliveryMigrations() []storage.Migration {
	return []storage.Migration{{Version: "cx/0024_renewal_delivery", Name: "durable certificate renewal installation and publication orchestration", Statements: []string{
		`CREATE TABLE cx_renewal_deliveries(policy_id TEXT PRIMARY KEY,owner_id TEXT NOT NULL,revision INTEGER NOT NULL,payload BLOB NOT NULL CHECK(length(payload)<=32768))`,
		`CREATE TABLE cx_renewal_delivery_cycles(id TEXT PRIMARY KEY,policy_id TEXT NOT NULL,payload BLOB NOT NULL CHECK(length(payload)<=32768))`,
	}}}
}
func ReadRenewalDelivery(ctx context.Context, q storage.Queryer, id string) (RenewalDelivery, error) {
	var d RenewalDelivery
	var raw []byte
	var owner string
	var revision uint64
	e := q.QueryRow(ctx, `SELECT owner_id,revision,payload FROM cx_renewal_deliveries WHERE policy_id=?`, id).Scan(&owner, &revision, &raw)
	if e != nil {
		return d, e
	}
	if len(raw) > 32768 || json.Unmarshal(raw, &d) != nil || d.PolicyID != id || d.OwnerID != owner || d.Revision != revision || revision == 0 || len(d.PublicationIDs) > 8 {
		return RenewalDelivery{}, ErrInvalidInput
	}
	return d, nil
}
func SaveRenewalDeliveryTx(ctx context.Context, tx storage.Tx, d RenewalDelivery, expected uint64) error {
	if !validInputToken(d.PolicyID, 128) || !validInputToken(d.OwnerID, 128) || d.Revision != expected+1 || len(d.PublicationIDs) > 8 || d.UpdatedAt.IsZero() {
		return ErrInvalidInput
	}
	for _, id := range d.PublicationIDs {
		if !validInputToken(id, 128) {
			return ErrInvalidInput
		}
	}
	raw, e := json.Marshal(d)
	if e != nil || len(raw) > 32768 {
		return ErrInvalidInput
	}
	if expected == 0 {
		_, e = tx.Exec(ctx, `INSERT INTO cx_renewal_deliveries VALUES(?,?,?,?)`, d.PolicyID, d.OwnerID, d.Revision, raw)
	} else {
		old, err := ReadRenewalDelivery(ctx, tx, d.PolicyID)
		if err != nil || old.Revision != expected || old.OwnerID != d.OwnerID {
			return ErrInvalidRevision
		}
		_, e = tx.Exec(ctx, `UPDATE cx_renewal_deliveries SET revision=?,payload=? WHERE policy_id=?`, d.Revision, raw, d.PolicyID)
	}
	if e != nil {
		return e
	}
	if d.Cycle != nil {
		if !validInputToken(d.Cycle.ID, 128) || d.Cycle.PolicyRevision == 0 || !validInputToken(d.Cycle.IssueTaskID, 128) || len(d.Cycle.Publications) > 8 {
			return ErrInvalidInput
		}
		cycle, _ := json.Marshal(d.Cycle)
		_, e = tx.Exec(ctx, `INSERT INTO cx_renewal_delivery_cycles VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload WHERE policy_id=excluded.policy_id`, d.Cycle.ID, d.PolicyID, cycle)
	}
	return e
}
func RenewalDeliveryHistory(ctx context.Context, q storage.Queryer, policyID string) ([]RenewalDeliveryCycle, error) {
	rows, e := q.Query(ctx, `SELECT payload FROM cx_renewal_delivery_cycles WHERE policy_id=? ORDER BY rowid DESC LIMIT 50`, policyID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []RenewalDeliveryCycle{}
	for rows.Next() {
		var raw []byte
		var c RenewalDeliveryCycle
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		if json.Unmarshal(raw, &c) != nil {
			return nil, ErrInvalidInput
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
