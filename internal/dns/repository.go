package dns

import (
	"context"
	"errors"
	"fmt"
	"time"

	"serverfleet.local/serverfleet/internal/storage"
)

// Repo persists DNS metadata. It never authorizes callers and never stores
// provider credentials.
type Repo struct {
	db *storage.DB
}

func NewRepo(db *storage.DB) (*Repo, error) {
	if db == nil {
		return nil, errors.New("dns: database is required")
	}
	return &Repo{db: db}, nil
}

const accountSelect = `SELECT id, provider_code, display_name, note, enabled, is_default,
	credential_ref, credential_status, created_at, updated_at FROM dns_accounts`

func (r *Repo) createAccount(ctx context.Context, q storage.Queryer, record accountRecord) error {
	_, err := q.Exec(ctx, `INSERT INTO dns_accounts
		(id, provider_code, display_name, note, enabled, is_default, credential_ref,
		 credential_status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.ProviderCode, record.DisplayName, record.Note, boolInt(record.Enabled),
		boolInt(record.IsDefault), string(record.CredentialRef), string(record.CredentialStatus),
		storage.EncodeTime(record.CreatedAt), storage.EncodeTime(record.UpdatedAt))
	if storage.IsUniqueViolation(err) {
		return ErrAccountExists
	}
	return err
}

func (r *Repo) getAccount(ctx context.Context, q storage.Queryer, id string) (accountRecord, error) {
	record, err := scanAccount(q.QueryRow(ctx, accountSelect+" WHERE id = ?", id))
	if storage.IsNotFound(err) {
		return accountRecord{}, ErrAccountNotFound
	}
	return record, err
}

func (r *Repo) listAccounts(ctx context.Context) ([]accountRecord, error) {
	rows, err := r.db.Query(ctx, accountSelect+" ORDER BY display_name, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]accountRecord, 0)
	for rows.Next() {
		record, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (r *Repo) updateAccount(ctx context.Context, q storage.Queryer, record accountRecord) error {
	outcome, err := q.Exec(ctx, `UPDATE dns_accounts SET display_name = ?, note = ?, enabled = ?,
		credential_ref = ?, credential_status = ?, updated_at = ? WHERE id = ?`,
		record.DisplayName, record.Note, boolInt(record.Enabled), string(record.CredentialRef),
		string(record.CredentialStatus), storage.EncodeTime(record.UpdatedAt), record.ID)
	if err != nil {
		return err
	}
	return requireOne(outcome, ErrAccountNotFound)
}

func (r *Repo) setDefault(ctx context.Context, q storage.Queryer, id string, now time.Time) (accountRecord, error) {
	record, err := r.getAccount(ctx, q, id)
	if err != nil {
		return accountRecord{}, err
	}
	if _, err := q.Exec(ctx, `UPDATE dns_accounts SET is_default = 0, updated_at = ?
		WHERE provider_code = ? AND is_default = 1`, storage.EncodeTime(now), record.ProviderCode); err != nil {
		return accountRecord{}, err
	}
	outcome, err := q.Exec(ctx, "UPDATE dns_accounts SET is_default = 1, updated_at = ? WHERE id = ?",
		storage.EncodeTime(now), id)
	if err != nil {
		return accountRecord{}, err
	}
	if err := requireOne(outcome, ErrAccountNotFound); err != nil {
		return accountRecord{}, err
	}
	record.IsDefault = true
	record.UpdatedAt = now
	return record, nil
}

func (r *Repo) updateCredentialStatus(ctx context.Context, q storage.Queryer, id string, status CredentialStatus, now time.Time) (accountRecord, error) {
	outcome, err := q.Exec(ctx, "UPDATE dns_accounts SET credential_status = ?, updated_at = ? WHERE id = ?",
		string(status), storage.EncodeTime(now), id)
	if err != nil {
		return accountRecord{}, err
	}
	if err := requireOne(outcome, ErrAccountNotFound); err != nil {
		return accountRecord{}, err
	}
	return r.getAccount(ctx, q, id)
}

const zoneSelect = `SELECT id, account_id, provider_zone_id, name, status, synced_at FROM dns_zones`

func (r *Repo) createZone(ctx context.Context, q storage.Queryer, zone Zone) error {
	_, err := q.Exec(ctx, `INSERT INTO dns_zones
		(id, account_id, provider_zone_id, name, status, synced_at) VALUES (?, ?, ?, ?, ?, ?)`,
		zone.ID, zone.AccountID, zone.ProviderZoneID, zone.Name, string(zone.Status),
		storage.EncodeTime(zone.SyncedAt))
	if storage.IsUniqueViolation(err) {
		var exists int
		if scanErr := q.QueryRow(ctx, `SELECT COUNT(*) FROM dns_zones
			WHERE account_id = ? AND name = ?`, zone.AccountID, zone.Name).Scan(&exists); scanErr == nil && exists > 0 {
			return ErrZoneNameTaken
		}
		return ErrZoneExists
	}
	return err
}

func (r *Repo) getZone(ctx context.Context, q storage.Queryer, id string) (Zone, error) {
	zone, err := scanZone(q.QueryRow(ctx, zoneSelect+" WHERE id = ?", id))
	if storage.IsNotFound(err) {
		return Zone{}, ErrZoneNotFound
	}
	return zone, err
}

func (r *Repo) listZones(ctx context.Context, accountID string) ([]Zone, error) {
	query := zoneSelect
	args := []any{}
	if accountID != "" {
		query += " WHERE account_id = ?"
		args = append(args, accountID)
	}
	query += " ORDER BY name, id"
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	zones := make([]Zone, 0)
	for rows.Next() {
		zone, err := scanZone(rows)
		if err != nil {
			return nil, err
		}
		zones = append(zones, zone)
	}
	return zones, rows.Err()
}

const recordSelect = `SELECT id, zone_id, type, name, content, ttl, proxied, priority, modified_at
	FROM dns_records`

func (r *Repo) createRecord(ctx context.Context, q storage.Queryer, record Record) error {
	_, err := q.Exec(ctx, `INSERT INTO dns_records
		(id, zone_id, type, name, content, ttl, proxied, priority, modified_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.ZoneID, string(record.Type), record.Name, record.Content, record.TTL,
		boolInt(record.Proxied), record.Priority, storage.EncodeTime(record.ModifiedAt))
	if storage.IsUniqueViolation(err) {
		return ErrRecordConflict
	}
	return err
}

func (r *Repo) getRecord(ctx context.Context, q storage.Queryer, id string) (Record, error) {
	record, err := scanRecord(q.QueryRow(ctx, recordSelect+" WHERE id = ?", id))
	if storage.IsNotFound(err) {
		return Record{}, ErrRecordNotFound
	}
	return record, err
}

func (r *Repo) listRecords(ctx context.Context, zoneID string) ([]Record, error) {
	query := recordSelect
	args := []any{}
	if zoneID != "" {
		query += " WHERE zone_id = ?"
		args = append(args, zoneID)
	}
	query += " ORDER BY type, name, id"
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]Record, 0)
	for rows.Next() {
		record, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (r *Repo) updateRecord(ctx context.Context, q storage.Queryer, record Record) error {
	outcome, err := q.Exec(ctx, `UPDATE dns_records SET type = ?, name = ?, content = ?, ttl = ?,
		proxied = ?, priority = ?, modified_at = ? WHERE id = ?`,
		string(record.Type), record.Name, record.Content, record.TTL, boolInt(record.Proxied),
		record.Priority, storage.EncodeTime(record.ModifiedAt), record.ID)
	if err != nil {
		if storage.IsUniqueViolation(err) {
			return ErrRecordConflict
		}
		return err
	}
	return requireOne(outcome, ErrRecordNotFound)
}

func (r *Repo) deleteRecord(ctx context.Context, q storage.Queryer, id string) error {
	outcome, err := q.Exec(ctx, "DELETE FROM dns_records WHERE id = ?", id)
	if err != nil {
		return err
	}
	return requireOne(outcome, ErrRecordNotFound)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanAccount(row scanner) (accountRecord, error) {
	var (
		record                accountRecord
		enabled, isDefault    int
		credentialRef, status string
		createdAt, updatedAt  string
	)
	if err := row.Scan(&record.ID, &record.ProviderCode, &record.DisplayName, &record.Note,
		&enabled, &isDefault, &credentialRef, &status, &createdAt, &updatedAt); err != nil {
		return accountRecord{}, err
	}
	if record.ProviderCode != ProviderCloudflare {
		return accountRecord{}, fmt.Errorf("dns: stored account %q has invalid provider", record.ID)
	}
	record.CredentialStatus = CredentialStatus(status)
	if !record.CredentialStatus.valid() {
		return accountRecord{}, fmt.Errorf("dns: stored account %q has invalid credential status", record.ID)
	}
	record.CredentialRef = CredentialRef(credentialRef)
	record.Enabled = enabled != 0
	record.IsDefault = isDefault != 0
	var err error
	if record.CreatedAt, err = storage.DecodeTime(createdAt); err != nil {
		return accountRecord{}, err
	}
	if record.UpdatedAt, err = storage.DecodeTime(updatedAt); err != nil {
		return accountRecord{}, err
	}
	return record, nil
}

func scanZone(row scanner) (Zone, error) {
	var (
		zone   Zone
		status string
		synced string
	)
	if err := row.Scan(&zone.ID, &zone.AccountID, &zone.ProviderZoneID, &zone.Name, &status, &synced); err != nil {
		return Zone{}, err
	}
	zone.Status = ZoneStatus(status)
	if !zone.Status.valid() {
		return Zone{}, fmt.Errorf("dns: stored zone %q has invalid status", zone.ID)
	}
	parsed, err := storage.DecodeTime(synced)
	if err != nil {
		return Zone{}, err
	}
	zone.SyncedAt = parsed
	return zone, nil
}

func scanRecord(row scanner) (Record, error) {
	var (
		record   Record
		kind     string
		ttl      int64
		proxied  int
		priority int64
		modified string
	)
	if err := row.Scan(&record.ID, &record.ZoneID, &kind, &record.Name, &record.Content,
		&ttl, &proxied, &priority, &modified); err != nil {
		return Record{}, err
	}
	record.Type = RecordType(kind)
	if !record.Type.valid() {
		return Record{}, fmt.Errorf("dns: stored record %q has invalid type", record.ID)
	}
	if ttl < 0 || ttl > 86400 || priority < 0 || priority > 65535 {
		return Record{}, fmt.Errorf("dns: stored record %q has invalid numbers", record.ID)
	}
	record.TTL = int(ttl)
	record.Proxied = proxied != 0
	record.Priority = int(priority)
	parsed, err := storage.DecodeTime(modified)
	if err != nil {
		return Record{}, err
	}
	record.ModifiedAt = parsed
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

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
