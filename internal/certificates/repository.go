package certificates

import (
	"context"
	"errors"
	"fmt"
	"time"

	"serverfleet.local/serverfleet/internal/storage"
)

// Repo persists certificate metadata and internal distribution rows. It never
// stores or returns private key bytes.
type Repo struct {
	db *storage.DB
}

func NewRepo(db *storage.DB) (*Repo, error) {
	if db == nil {
		return nil, errors.New("certificates: database is required")
	}
	return &Repo{db: db}, nil
}

const certificateSelect = `SELECT id, kind, common_name, status, issuer_ref, key_ref, serial,
	not_before, not_after, created_at, updated_at FROM certificates`

func (r *Repo) createCertificate(ctx context.Context, q storage.Queryer, record certificateRecord) error {
	_, err := q.Exec(ctx, `INSERT INTO certificates
		(id, kind, common_name, status, issuer_ref, key_ref, serial, not_before, not_after,
		 created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, string(record.Kind), record.CommonName, string(record.Status),
		record.IssuerRef, string(record.KeyRef), record.Serial,
		storage.EncodeTime(record.NotBefore), storage.EncodeTime(record.NotAfter),
		storage.EncodeTime(record.CreatedAt), storage.EncodeTime(record.UpdatedAt))
	if storage.IsUniqueViolation(err) {
		return ErrCertificateExists
	}
	return err
}

func (r *Repo) getCertificate(ctx context.Context, q storage.Queryer, id string) (certificateRecord, error) {
	record, err := scanCertificate(q.QueryRow(ctx, certificateSelect+" WHERE id = ?", id))
	if storage.IsNotFound(err) {
		return certificateRecord{}, ErrCertificateNotFound
	}
	return record, err
}

func (r *Repo) listCertificates(ctx context.Context) ([]certificateRecord, error) {
	rows, err := r.db.Query(ctx, certificateSelect+" ORDER BY common_name, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]certificateRecord, 0)
	for rows.Next() {
		record, err := scanCertificate(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (r *Repo) updateCertificate(ctx context.Context, q storage.Queryer, record certificateRecord) error {
	outcome, err := q.Exec(ctx, `UPDATE certificates SET kind = ?, common_name = ?, status = ?,
		issuer_ref = ?, key_ref = ?, serial = ?, not_before = ?, not_after = ?, updated_at = ?
		WHERE id = ?`,
		string(record.Kind), record.CommonName, string(record.Status), record.IssuerRef,
		string(record.KeyRef), record.Serial, storage.EncodeTime(record.NotBefore),
		storage.EncodeTime(record.NotAfter), storage.EncodeTime(record.UpdatedAt), record.ID)
	if err != nil {
		return err
	}
	return requireOne(outcome, ErrCertificateNotFound)
}

const distributionSelect = `SELECT id, certificate_id, service_group_id, node_id, status,
	task_id, error, created_at, updated_at FROM certificate_distributions`

func (r *Repo) createDistribution(ctx context.Context, q storage.Queryer, record DistributionView) error {
	_, err := q.Exec(ctx, `INSERT INTO certificate_distributions
		(id, certificate_id, service_group_id, node_id, status, task_id, error, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.CertificateID, record.ServiceGroupID, record.NodeID,
		string(record.Status), record.TaskID, record.Error,
		storage.EncodeTime(record.CreatedAt), storage.EncodeTime(record.UpdatedAt))
	if storage.IsUniqueViolation(err) {
		return ErrDistributionExists
	}
	return err
}

func (r *Repo) listDistributions(ctx context.Context, q storage.Queryer, certificateID string) ([]DistributionView, error) {
	rows, err := q.Query(ctx, distributionSelect+
		" WHERE certificate_id = ? ORDER BY node_id", certificateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]DistributionView, 0)
	for rows.Next() {
		record, err := scanDistribution(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (r *Repo) revokeDistributions(ctx context.Context, q storage.Queryer, certificateID string, now time.Time) error {
	_, err := q.Exec(ctx, `UPDATE certificate_distributions SET status = ?, updated_at = ?
		WHERE certificate_id = ? AND status <> ?`,
		string(DistributionRevoked), storage.EncodeTime(now), certificateID, string(DistributionRevoked))
	return err
}

func (r *Repo) nodeIDs(ctx context.Context, certificateID string) ([]string, error) {
	rows, err := r.db.Query(ctx, `SELECT node_id FROM certificate_distributions
		WHERE certificate_id = ? ORDER BY node_id`, certificateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func scanCertificate(row interface{ Scan(...any) error }) (certificateRecord, error) {
	var (
		record                                certificateRecord
		kind, status                          string
		keyRef                                string
		notBefore, notAfter, created, updated string
	)
	if err := row.Scan(&record.ID, &kind, &record.CommonName, &status, &record.IssuerRef,
		&keyRef, &record.Serial, &notBefore, &notAfter, &created, &updated); err != nil {
		return certificateRecord{}, err
	}
	record.Kind = Kind(kind)
	if !record.Kind.valid() {
		return certificateRecord{}, fmt.Errorf("certificates: stored certificate %q has invalid kind", record.ID)
	}
	record.Status = CertificateStatus(status)
	if !record.Status.valid() {
		return certificateRecord{}, fmt.Errorf("certificates: stored certificate %q has invalid status", record.ID)
	}
	record.KeyRef = KeyRef(keyRef)
	if record.KeyRef == "" || containsPrivateKeyMaterial(keyRef) {
		return certificateRecord{}, fmt.Errorf("certificates: stored certificate %q has invalid key reference", record.ID)
	}
	var err error
	if record.NotBefore, err = storage.DecodeTime(notBefore); err != nil {
		return certificateRecord{}, err
	}
	if record.NotAfter, err = storage.DecodeTime(notAfter); err != nil {
		return certificateRecord{}, err
	}
	if record.CreatedAt, err = storage.DecodeTime(created); err != nil {
		return certificateRecord{}, err
	}
	if record.UpdatedAt, err = storage.DecodeTime(updated); err != nil {
		return certificateRecord{}, err
	}
	return record, nil
}

func scanDistribution(row interface{ Scan(...any) error }) (DistributionView, error) {
	var (
		record           DistributionView
		status           string
		created, updated string
	)
	if err := row.Scan(&record.ID, &record.CertificateID, &record.ServiceGroupID, &record.NodeID,
		&status, &record.TaskID, &record.Error, &created, &updated); err != nil {
		return DistributionView{}, err
	}
	record.Status = DistributionStatus(status)
	if !record.Status.valid() {
		return DistributionView{}, fmt.Errorf("certificates: stored distribution %q has invalid status", record.ID)
	}
	var err error
	if record.CreatedAt, err = storage.DecodeTime(created); err != nil {
		return DistributionView{}, err
	}
	if record.UpdatedAt, err = storage.DecodeTime(updated); err != nil {
		return DistributionView{}, err
	}
	return record, nil
}

func requireOne(result storage.Result, missing error) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return missing
	}
	return nil
}
