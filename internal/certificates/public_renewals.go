package certificates

import (
	"context"
	"encoding/json"
	"time"

	"serverfleet.local/serverfleet/internal/storage"
)

// PublicRenewal only schedules an approval-required issuance. Adoption requires
// an independently observed successful installation of the replacement chain.
// A terminal failed/unknown cycle is never retried by advancing a clock bucket.
type PublicRenewal struct {
	ID              string    `json:"id"`
	OwnerID         string    `json:"ownerId"`
	Revision        uint64    `json:"revision"`
	Enabled         bool      `json:"enabled"`
	OrderTaskID     string    `json:"orderTaskId"`
	InstallTaskID   string    `json:"installTaskId"`
	CSRTaskID       string    `json:"csrTaskId"`
	NodeID          string    `json:"nodeId"`
	Domain          string    `json:"domain"`
	KeyRef          string    `json:"keyRef"`
	ZoneID          string    `json:"zoneId"`
	ProfileSHA256   string    `json:"profileSha256"`
	NotAfter        time.Time `json:"notAfter"`
	RenewBeforeDays int       `json:"renewBeforeDays"`
	TaskID          string    `json:"taskId,omitempty"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

func PublicRenewalMigrations() []storage.Migration {
	return []storage.Migration{{Version: "cx/0020_public_certificate_renewals", Name: "owner-scoped approval-required public certificate renewal cycles", Statements: []string{
		`CREATE TABLE cx_public_certificate_renewals(id TEXT PRIMARY KEY,owner_id TEXT NOT NULL,node_id TEXT NOT NULL,key_ref TEXT NOT NULL,payload BLOB NOT NULL CHECK(length(payload)<=8192),UNIQUE(node_id,key_ref))`,
	}}}
}

func validPublicRenewal(p PublicRenewal) bool {
	for _, id := range []string{p.ID, p.OwnerID, p.OrderTaskID, p.InstallTaskID, p.CSRTaskID, p.NodeID, p.ZoneID} {
		if !validInputToken(id, 128) {
			return false
		}
	}
	return p.Revision > 0 && p.Revision <= 1<<53-1 && p.RenewBeforeDays >= 1 && p.RenewBeforeDays <= 60 && !p.NotAfter.IsZero() && !p.UpdatedAt.IsZero() && KeyRef(p.KeyRef).Validate() == nil && validText(p.Domain, 255) && len(p.ProfileSHA256) == 64 && (p.TaskID == "" || validInputToken(p.TaskID, 128))
}

func ReadPublicRenewal(ctx context.Context, db storage.Queryer, id string) (PublicRenewal, error) {
	var p PublicRenewal
	var raw []byte
	var owner, node, key string
	if err := db.QueryRow(ctx, `SELECT owner_id,node_id,key_ref,payload FROM cx_public_certificate_renewals WHERE id=?`, id).Scan(&owner, &node, &key, &raw); err != nil {
		return p, err
	}
	if len(raw) > 8192 || json.Unmarshal(raw, &p) != nil || p.ID != id || p.OwnerID != owner || p.NodeID != node || p.KeyRef != key || !validPublicRenewal(p) {
		return PublicRenewal{}, ErrInvalidInput
	}
	return p, nil
}

// ListPublicRenewals has a deployment-wide cap. Reads cannot allocate an
// unbounded scheduling queue, including when a restored database is corrupt.
func ListPublicRenewals(ctx context.Context, db storage.Queryer) ([]PublicRenewal, error) {
	rows, err := db.Query(ctx, `SELECT id FROM cx_public_certificate_renewals ORDER BY id LIMIT 1001`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if len(ids) > 1000 {
		return nil, ErrInvalidInput
	}
	result := make([]PublicRenewal, 0, len(ids))
	for _, id := range ids {
		p, e := ReadPublicRenewal(ctx, db, id)
		if e != nil {
			return nil, e
		}
		result = append(result, p)
	}
	return result, nil
}

// SavePublicRenewalTx requires the previous revision and keeps the owner/node/
// key slot immutable. Callers append the corresponding audit in this same Tx.
func SavePublicRenewalTx(ctx context.Context, tx storage.Tx, p PublicRenewal, expected uint64) error {
	if !validPublicRenewal(p) || p.Revision != expected+1 {
		return ErrInvalidRevision
	}
	raw, err := json.Marshal(p)
	if err != nil || len(raw) > 8192 {
		return ErrInvalidInput
	}
	if expected == 0 {
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM cx_public_certificate_renewals`).Scan(&count); err != nil {
			return err
		}
		if count >= 1000 {
			return ErrInvalidInput
		}
		_, err = tx.Exec(ctx, `INSERT INTO cx_public_certificate_renewals VALUES(?,?,?,?,?)`, p.ID, p.OwnerID, p.NodeID, p.KeyRef, raw)
		return err
	}
	old, err := ReadPublicRenewal(ctx, tx, p.ID)
	if err != nil || old.Revision != expected || old.OwnerID != p.OwnerID || old.NodeID != p.NodeID || old.KeyRef != p.KeyRef {
		return ErrInvalidRevision
	}
	_, err = tx.Exec(ctx, `UPDATE cx_public_certificate_renewals SET payload=? WHERE id=?`, raw, p.ID)
	return err
}

// BindPublicRenewalTaskTx does not advance the policy revision: that revision
// is part of the frozen task plan. Repeated binding of the same ID is harmless.
func BindPublicRenewalTaskTx(ctx context.Context, tx storage.Tx, id string, revision uint64, taskID string) error {
	p, err := ReadPublicRenewal(ctx, tx, id)
	if err != nil || !p.Enabled || p.Revision != revision || !validInputToken(taskID, 128) || p.TaskID != "" && p.TaskID != taskID {
		return ErrInvalidRevision
	}
	p.TaskID = taskID
	p.UpdatedAt = time.Now().UTC()
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE cx_public_certificate_renewals SET payload=? WHERE id=?`, raw, id)
	return err
}

func InvalidateRestoredRenewalsTx(ctx context.Context, tx storage.Tx, now time.Time) error {
	policies, err := ListPublicRenewals(ctx, tx)
	if err != nil {
		return err
	}
	for _, p := range policies {
		previous := p.Revision
		p.Revision++
		p.Enabled = false
		p.UpdatedAt = now.UTC()
		if err = SavePublicRenewalTx(ctx, tx, p, previous); err != nil {
			return err
		}
	}
	return nil
}
