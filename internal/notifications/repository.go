package notifications

import (
	"context"
	"errors"
	"fmt"
	"time"

	"serverfleet.local/serverfleet/internal/storage"
)

// Repo persists channels, settings and delivery attempts. It performs no
// authorization and never stores a credential value.
type Repo struct {
	db *storage.DB
}

func NewRepo(db *storage.DB) (*Repo, error) {
	if db == nil {
		return nil, errors.New("notifications: database is required")
	}
	return &Repo{db: db}, nil
}

const channelSelect = `SELECT channel, enabled, smtp_host, smtp_port, smtp_security,
	smtp_sender, smtp_recipient, qq_app_id, qq_target_type, qq_target_id,
	secret_ref, credential_status, updated_at FROM notification_channels`

func (r *Repo) GetChannel(ctx context.Context, channel Channel) (ChannelRecord, error) {
	if r == nil || r.db == nil {
		return ChannelRecord{}, ErrServiceNotConfigured
	}
	return r.getChannel(ctx, r.db, channel)
}

func (r *Repo) getChannel(ctx context.Context, q storage.Queryer, channel Channel) (ChannelRecord, error) {
	if q == nil {
		return ChannelRecord{}, ErrServiceNotConfigured
	}
	record, err := scanChannel(q.QueryRow(ctx, channelSelect+" WHERE channel = ?", string(channel)))
	if storage.IsNotFound(err) {
		return ChannelRecord{}, ErrChannelNotFound
	}
	return record, err
}

func (r *Repo) ListChannels(ctx context.Context) ([]ChannelRecord, error) {
	if r == nil || r.db == nil {
		return nil, ErrServiceNotConfigured
	}
	rows, err := r.db.Query(ctx, channelSelect+" ORDER BY channel")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]ChannelRecord, 0, 2)
	for rows.Next() {
		record, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

// putChannel writes the complete channel row inside the caller transaction.
func (r *Repo) putChannel(ctx context.Context, q storage.Queryer, record ChannelRecord) error {
	if q == nil {
		return ErrServiceNotConfigured
	}
	var (
		host, security, sender, recipient string
		port                              int
		appID, targetType, targetID       string
	)
	if record.SMTP != nil {
		host = record.SMTP.Host
		port = record.SMTP.Port
		security = string(record.SMTP.Security)
		sender = record.SMTP.Sender
		recipient = record.SMTP.Recipient
	}
	if record.QQ != nil {
		appID = record.QQ.AppID
		targetType = string(record.QQ.TargetType)
		targetID = record.QQ.TargetID
	}
	_, err := q.Exec(ctx, `INSERT INTO notification_channels
		(channel, enabled, smtp_host, smtp_port, smtp_security, smtp_sender,
		 smtp_recipient, qq_app_id, qq_target_type, qq_target_id, secret_ref,
		 credential_status, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(channel) DO UPDATE SET
			enabled = excluded.enabled,
			smtp_host = excluded.smtp_host,
			smtp_port = excluded.smtp_port,
			smtp_security = excluded.smtp_security,
			smtp_sender = excluded.smtp_sender,
			smtp_recipient = excluded.smtp_recipient,
			qq_app_id = excluded.qq_app_id,
			qq_target_type = excluded.qq_target_type,
			qq_target_id = excluded.qq_target_id,
			secret_ref = excluded.secret_ref,
			credential_status = excluded.credential_status,
			updated_at = excluded.updated_at`,
		string(record.Channel), boolInt(record.Enabled), host, port, security, sender,
		recipient, appID, targetType, targetID, record.SecretRef,
		string(record.CredentialStatus), storage.EncodeTime(record.UpdatedAt))
	return err
}

func (r *Repo) updateCredential(ctx context.Context, q storage.Queryer, channel Channel, secretRef string, status CredentialStatus, at time.Time) error {
	if q == nil {
		return ErrServiceNotConfigured
	}
	outcome, err := q.Exec(ctx, `UPDATE notification_channels
		SET secret_ref = ?, credential_status = ?, updated_at = ? WHERE channel = ?`,
		secretRef, string(status), storage.EncodeTime(at), string(channel))
	if err != nil {
		return err
	}
	affected, err := outcome.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrChannelNotFound
	}
	return nil
}

func scanChannel(row storage.Row) (ChannelRecord, error) {
	var (
		channel, security, status string
		host, sender, recipient   string
		appID, targetType         string
		targetID, secretRef       string
		enabled, port             int
		updatedAt                 string
	)
	if err := row.Scan(&channel, &enabled, &host, &port, &security, &sender, &recipient,
		&appID, &targetType, &targetID, &secretRef, &status, &updatedAt); err != nil {
		return ChannelRecord{}, err
	}
	record := ChannelRecord{
		Channel:          Channel(channel),
		Enabled:          enabled == 1,
		SecretRef:        secretRef,
		CredentialStatus: CredentialStatus(status),
	}
	if !record.Channel.Valid() || !record.CredentialStatus.Valid() {
		return ChannelRecord{}, fmt.Errorf("notifications: stored channel %q has invalid metadata", channel)
	}
	switch record.Channel {
	case ChannelSMTP:
		record.SMTP = &SMTPSettings{
			Host: host, Port: port, Security: SMTPSecurity(security),
			Sender: sender, Recipient: recipient,
		}
		if validateSMTPSettings(*record.SMTP) != nil {
			return ChannelRecord{}, fmt.Errorf("notifications: stored smtp channel is invalid")
		}
	case ChannelQQBot:
		record.QQ = &QQSettings{
			AppID: appID, TargetType: QQTargetType(targetType), TargetID: targetID,
		}
		if validateQQSettings(*record.QQ) != nil {
			return ChannelRecord{}, fmt.Errorf("notifications: stored qqbot channel is invalid")
		}
	}
	parsed, err := storage.DecodeTime(updatedAt)
	if err != nil {
		return ChannelRecord{}, err
	}
	record.UpdatedAt = parsed
	return record, nil
}

func (r *Repo) GetSettings(ctx context.Context) (Settings, error) {
	if r == nil || r.db == nil {
		return Settings{}, ErrServiceNotConfigured
	}
	return r.getSettings(ctx, r.db)
}

func (r *Repo) getSettings(ctx context.Context, q storage.Queryer) (Settings, error) {
	if q == nil {
		return Settings{}, ErrServiceNotConfigured
	}
	var (
		settings  Settings
		fallback  int
		updatedAt string
	)
	err := q.QueryRow(ctx, `SELECT smtp_fallback_on_qq_failure, max_retry_attempts,
		deduplication_window_seconds, updated_at FROM notification_settings WHERE id = 1`).
		Scan(&fallback, &settings.MaxRetryAttempts, &settings.DeduplicationWindowSeconds, &updatedAt)
	if err != nil {
		return Settings{}, err
	}
	settings.SMTPFallbackOnQQFailure = fallback == 1
	if err := settings.Validate(); err != nil {
		return Settings{}, fmt.Errorf("notifications: stored settings are invalid")
	}
	return settings, nil
}

func (r *Repo) putSettings(ctx context.Context, q storage.Queryer, settings Settings, at time.Time) error {
	if q == nil {
		return ErrServiceNotConfigured
	}
	_, err := q.Exec(ctx, `INSERT INTO notification_settings
		(id, smtp_fallback_on_qq_failure, max_retry_attempts,
		 deduplication_window_seconds, updated_at)
		VALUES (1, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			smtp_fallback_on_qq_failure = excluded.smtp_fallback_on_qq_failure,
			max_retry_attempts = excluded.max_retry_attempts,
			deduplication_window_seconds = excluded.deduplication_window_seconds,
			updated_at = excluded.updated_at`,
		boolInt(settings.SMTPFallbackOnQQFailure), settings.MaxRetryAttempts,
		settings.DeduplicationWindowSeconds, storage.EncodeTime(at))
	return err
}

const deliverySelect = `SELECT id, channel, status, provider_code, message, dedupe_key,
	attempts, created_at, updated_at FROM notification_deliveries`

func (r *Repo) findDelivery(ctx context.Context, q storage.Queryer, dedupeKey string, since time.Time) (Delivery, error) {
	if q == nil || dedupeKey == "" {
		return Delivery{}, ErrDeliveryNotFound
	}
	delivery, err := scanDelivery(q.QueryRow(ctx,
		deliverySelect+" WHERE dedupe_key = ? AND created_at >= ? ORDER BY id DESC LIMIT 1",
		dedupeKey, storage.EncodeTime(since)))
	if storage.IsNotFound(err) {
		return Delivery{}, ErrDeliveryNotFound
	}
	return delivery, err
}

func (r *Repo) insertDelivery(ctx context.Context, q storage.Queryer, delivery Delivery) (int64, error) {
	if q == nil {
		return 0, ErrServiceNotConfigured
	}
	outcome, err := q.Exec(ctx, `INSERT INTO notification_deliveries
		(channel, status, provider_code, message, dedupe_key, attempts, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		string(delivery.Channel), string(delivery.Status), delivery.ProviderCode,
		delivery.Message, delivery.DedupeKey, delivery.Attempts,
		storage.EncodeTime(delivery.CreatedAt), storage.EncodeTime(delivery.UpdatedAt))
	if err != nil {
		return 0, err
	}
	return outcome.LastInsertId()
}

func (r *Repo) updateDelivery(ctx context.Context, q storage.Queryer, delivery Delivery) error {
	if q == nil {
		return ErrServiceNotConfigured
	}
	outcome, err := q.Exec(ctx, `UPDATE notification_deliveries SET
		status = ?, provider_code = ?, attempts = ?, updated_at = ? WHERE id = ?`,
		string(delivery.Status), delivery.ProviderCode, delivery.Attempts,
		storage.EncodeTime(delivery.UpdatedAt), delivery.ID)
	if err != nil {
		return err
	}
	affected, err := outcome.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrDeliveryNotFound
	}
	return nil
}

func (r *Repo) GetDelivery(ctx context.Context, id int64) (Delivery, error) {
	if r == nil || r.db == nil {
		return Delivery{}, ErrServiceNotConfigured
	}
	delivery, err := scanDelivery(r.db.QueryRow(ctx, deliverySelect+" WHERE id = ?", id))
	if storage.IsNotFound(err) {
		return Delivery{}, ErrDeliveryNotFound
	}
	return delivery, err
}

// hasAcceptedSibling prevents retrying one failed channel after another channel
// with the same dedupe key already delivered the same alert.
func (r *Repo) hasAcceptedSibling(ctx context.Context, dedupeKey string, excludeID int64) (bool, error) {
	if r == nil || r.db == nil {
		return false, ErrServiceNotConfigured
	}
	if dedupeKey == "" {
		return false, nil
	}
	var count int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM notification_deliveries
		WHERE dedupe_key = ? AND id <> ? AND status IN ('provider_accepted', 'submitted')`,
		dedupeKey, excludeID).Scan(&count)
	return count > 0, err
}

func (r *Repo) ListDeliveries(ctx context.Context, filter DeliveryFilter) ([]Delivery, error) {
	if r == nil || r.db == nil {
		return nil, ErrServiceNotConfigured
	}
	normalized, err := normalizedFilter(filter)
	if err != nil {
		return nil, err
	}
	query := deliverySelect
	args := make([]any, 0, 4)
	clauses := make([]string, 0, 2)
	if normalized.Channel != "" {
		clauses = append(clauses, "channel = ?")
		args = append(args, string(normalized.Channel))
	}
	if normalized.Status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, string(normalized.Status))
	}
	if len(clauses) > 0 {
		query += " WHERE "
		for index, clause := range clauses {
			if index > 0 {
				query += " AND "
			}
			query += clause
		}
	}
	query += " ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?"
	args = append(args, normalized.Limit, normalized.Offset)
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	deliveries := make([]Delivery, 0, normalized.Limit)
	for rows.Next() {
		delivery, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, delivery)
	}
	return deliveries, rows.Err()
}

func scanDelivery(row storage.Row) (Delivery, error) {
	var (
		delivery  Delivery
		channel   string
		status    string
		createdAt string
		updatedAt string
	)
	if err := row.Scan(&delivery.ID, &channel, &status, &delivery.ProviderCode,
		&delivery.Message, &delivery.DedupeKey, &delivery.Attempts, &createdAt, &updatedAt); err != nil {
		return Delivery{}, err
	}
	delivery.Channel = Channel(channel)
	delivery.Status = DeliveryStatus(status)
	if !delivery.Channel.Valid() || !delivery.Status.Valid() {
		return Delivery{}, fmt.Errorf("notifications: stored delivery %d has invalid metadata", delivery.ID)
	}
	parsedCreated, err := storage.DecodeTime(createdAt)
	if err != nil {
		return Delivery{}, err
	}
	parsedUpdated, err := storage.DecodeTime(updatedAt)
	if err != nil {
		return Delivery{}, err
	}
	delivery.CreatedAt = parsedCreated
	delivery.UpdatedAt = parsedUpdated
	return delivery, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
