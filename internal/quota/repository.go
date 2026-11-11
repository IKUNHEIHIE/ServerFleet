package quota

import (
	"context"
	"errors"
	"fmt"
	"time"

	"serverfleet.local/serverfleet/internal/storage"
)

// Repo persists traffic periods, quota policies and reminder keys. It never
// authorizes callers and never invents a usage value.
type Repo struct {
	db *storage.DB
}

func NewRepo(db *storage.DB) (*Repo, error) {
	if db == nil {
		return nil, errors.New("quota: database is required")
	}
	return &Repo{db: db}, nil
}

type samplePoint struct {
	BytesIn  float64
	BytesOut float64
	Observed time.Time
}

func (r *Repo) UpsertPolicy(ctx context.Context, q storage.Queryer, policy Policy) error {
	if r == nil || q == nil {
		return ErrServiceNotConfigured
	}
	_, err := q.Exec(ctx, `INSERT INTO quota_policies
		(node_id, monthly_limit_bytes, warning_percent, expiry_at, estimated, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
			monthly_limit_bytes = excluded.monthly_limit_bytes,
			warning_percent = excluded.warning_percent,
			expiry_at = excluded.expiry_at,
			estimated = excluded.estimated,
			updated_at = excluded.updated_at`,
		policy.NodeID, policy.MonthlyLimitBytes, policy.WarningPercent,
		nullableTime(policy.ExpiryAt), boolInt(policy.Estimated),
		storage.EncodeTime(policy.UpdatedAt))
	return err
}

func (r *Repo) GetPolicy(ctx context.Context, nodeID string) (Policy, error) {
	if r == nil || r.db == nil {
		return Policy{}, ErrServiceNotConfigured
	}
	policy, err := scanPolicy(r.db.QueryRow(ctx, `SELECT node_id, monthly_limit_bytes,
		warning_percent, expiry_at, estimated, updated_at FROM quota_policies
		WHERE node_id = ?`, nodeID))
	if storage.IsNotFound(err) {
		return Policy{}, ErrPolicyNotFound
	}
	if err == nil {
		err = r.hydratePolicy(ctx, &policy)
	}
	return policy, err
}

func (r *Repo) ListPolicies(ctx context.Context) ([]Policy, error) {
	if r == nil || r.db == nil {
		return nil, ErrServiceNotConfigured
	}
	rows, err := r.db.Query(ctx, `SELECT node_id, monthly_limit_bytes, warning_percent,
		expiry_at, estimated, updated_at FROM quota_policies ORDER BY node_id LIMIT 1001`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	policies := make([]Policy, 0)
	for rows.Next() {
		policy, err := scanPolicy(rows)
		if err != nil {
			return nil, err
		}
		policies = append(policies, policy)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if len(policies) > 1000 {
		return nil, ErrInvalidPolicy
	}
	for i := range policies {
		if err = r.hydratePolicy(ctx, &policies[i]); err != nil {
			return nil, err
		}
	}
	return policies, nil
}

const usageSelect = `SELECT node_id, period_start, period_end, bytes_in, bytes_out,
	source, updated_at FROM traffic_periods`

func (r *Repo) GetPeriod(ctx context.Context, q storage.Queryer, nodeID string, periodStart time.Time) (Usage, error) {
	if r == nil || q == nil {
		return Usage{}, ErrServiceNotConfigured
	}
	usage, err := scanUsage(q.QueryRow(ctx, usageSelect+" WHERE node_id = ? AND period_start = ?",
		nodeID, storage.EncodeTime(periodStart)))
	if storage.IsNotFound(err) {
		return Usage{}, ErrUsageNotFound
	}
	return usage, err
}

func (r *Repo) GetUsage(ctx context.Context, nodeID string, periodStart time.Time) (Usage, error) {
	if r == nil || r.db == nil {
		return Usage{}, ErrServiceNotConfigured
	}
	return r.GetPeriod(ctx, r.db, nodeID, periodStart)
}

func (r *Repo) LatestUsage(ctx context.Context, nodeID string) (Usage, error) {
	if r == nil || r.db == nil {
		return Usage{}, ErrServiceNotConfigured
	}
	usage, err := scanUsage(r.db.QueryRow(ctx, usageSelect+
		" WHERE node_id = ? ORDER BY period_start DESC LIMIT 1", nodeID))
	if storage.IsNotFound(err) {
		return Usage{}, ErrUsageNotFound
	}
	return usage, err
}

func (r *Repo) UpsertUsage(ctx context.Context, q storage.Queryer, usage Usage) error {
	if r == nil || q == nil {
		return ErrServiceNotConfigured
	}
	_, err := q.Exec(ctx, `INSERT INTO traffic_periods
		(node_id, period_start, period_end, bytes_in, bytes_out, source, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id, period_start) DO UPDATE SET
			period_end = excluded.period_end,
			bytes_in = excluded.bytes_in,
			bytes_out = excluded.bytes_out,
			source = excluded.source,
			updated_at = excluded.updated_at`,
		usage.NodeID, storage.EncodeTime(usage.PeriodStart), storage.EncodeTime(usage.PeriodEnd),
		usage.BytesIn, usage.BytesOut, string(usage.Source), storage.EncodeTime(usage.UpdatedAt))
	return err
}

func (r *Repo) PutSample(ctx context.Context, q storage.Queryer, sample UsageSample, recordedAt time.Time) error {
	if r == nil || q == nil {
		return ErrServiceNotConfigured
	}
	_, err := q.Exec(ctx, `INSERT INTO traffic_samples
		(node_id, period_start, observed_at, bytes_in, bytes_out, source, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id, period_start, observed_at) DO UPDATE SET
			bytes_in = excluded.bytes_in,
			bytes_out = excluded.bytes_out,
			source = excluded.source,
			recorded_at = excluded.recorded_at`,
		sample.NodeID, storage.EncodeTime(sample.PeriodStart), storage.EncodeTime(sample.ObservedAt),
		sample.BytesIn, sample.BytesOut, string(sample.Source), storage.EncodeTime(recordedAt))
	return err
}

func (r *Repo) CountSamples(ctx context.Context, q storage.Queryer, nodeID string, periodStart time.Time) (int, error) {
	if r == nil || q == nil {
		return 0, ErrServiceNotConfigured
	}
	var count int
	err := q.QueryRow(ctx, `SELECT COUNT(*) FROM traffic_samples
		WHERE node_id = ? AND period_start = ?`, nodeID, storage.EncodeTime(periodStart)).Scan(&count)
	return count, err
}

func (r *Repo) ListRecentSamples(ctx context.Context, q storage.Queryer, nodeID string, periodStart time.Time, limit int) ([]samplePoint, error) {
	if r == nil || q == nil {
		return nil, ErrServiceNotConfigured
	}
	if limit < 1 || limit > 100 {
		return nil, ErrInvalidUsage
	}
	return r.listSamples(ctx, q, nodeID, periodStart, limit, "DESC")
}

// FirstSample returns the oldest observation in a period for burn-rate bounds.
func (r *Repo) FirstSample(ctx context.Context, q storage.Queryer, nodeID string, periodStart time.Time) (samplePoint, error) {
	if r == nil || q == nil {
		return samplePoint{}, ErrServiceNotConfigured
	}
	points, err := r.listSamples(ctx, q, nodeID, periodStart, 1, "ASC")
	if err != nil {
		return samplePoint{}, err
	}
	if len(points) == 0 {
		return samplePoint{}, ErrUsageNotFound
	}
	return points[0], nil
}

func (r *Repo) listSamples(ctx context.Context, q storage.Queryer, nodeID string, periodStart time.Time, limit int, order string) ([]samplePoint, error) {
	rows, err := q.Query(ctx, `SELECT bytes_in, bytes_out, observed_at FROM traffic_samples
		WHERE node_id = ? AND period_start = ? ORDER BY observed_at `+order+` LIMIT ?`,
		nodeID, storage.EncodeTime(periodStart), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	points := make([]samplePoint, 0, limit)
	for rows.Next() {
		var point samplePoint
		var observed string
		if err := rows.Scan(&point.BytesIn, &point.BytesOut, &observed); err != nil {
			return nil, err
		}
		if !finiteNonNegative(point.BytesIn) || !finiteNonNegative(point.BytesOut) {
			return nil, fmt.Errorf("quota: stored sample has invalid bytes")
		}
		parsed, err := storage.DecodeTime(observed)
		if err != nil {
			return nil, err
		}
		point.Observed = parsed
		points = append(points, point)
	}
	return points, rows.Err()
}

func (r *Repo) HasReminder(ctx context.Context, q storage.Queryer, dedupeKey string) (bool, error) {
	if r == nil || q == nil {
		return false, ErrServiceNotConfigured
	}
	var count int
	err := q.QueryRow(ctx, "SELECT COUNT(*) FROM quota_reminders WHERE dedupe_key = ?", dedupeKey).Scan(&count)
	return count > 0, err
}

func (r *Repo) MarkReminder(ctx context.Context, q storage.Queryer, reminder Reminder) error {
	if r == nil || q == nil {
		return ErrServiceNotConfigured
	}
	_, err := q.Exec(ctx, `INSERT INTO quota_reminders
		(dedupe_key, node_id, period_start, threshold, sent_at)
		VALUES (?, ?, ?, ?, ?) ON CONFLICT(dedupe_key) DO NOTHING`,
		reminder.DedupeKey, reminder.NodeID, storage.EncodeTime(reminder.PeriodStart),
		string(reminder.Threshold), storage.EncodeTime(reminder.SentAt))
	return err
}

func scanPolicy(row storage.Row) (Policy, error) {
	var (
		policy    Policy
		expiry    storage.NullString
		estimated int
		updatedAt string
	)
	if err := row.Scan(&policy.NodeID, &policy.MonthlyLimitBytes, &policy.WarningPercent,
		&expiry, &estimated, &updatedAt); err != nil {
		return Policy{}, err
	}
	if policy.MonthlyLimitBytes <= 0 || policy.WarningPercent < 1 || policy.WarningPercent > 100 {
		return Policy{}, fmt.Errorf("quota: stored policy for %q is invalid", policy.NodeID)
	}
	if expiry.Valid {
		parsed, err := storage.DecodeTime(expiry.String)
		if err != nil {
			return Policy{}, err
		}
		policy.ExpiryAt = parsed
	}
	policy.Estimated = estimated == 1
	parsed, err := storage.DecodeTime(updatedAt)
	if err != nil {
		return Policy{}, err
	}
	policy.UpdatedAt = parsed
	return policy, nil
}

func scanUsage(row storage.Row) (Usage, error) {
	var (
		usage      Usage
		start, end string
		source     string
		updatedAt  string
	)
	if err := row.Scan(&usage.NodeID, &start, &end, &usage.BytesIn, &usage.BytesOut,
		&source, &updatedAt); err != nil {
		return Usage{}, err
	}
	if !finiteNonNegative(usage.BytesIn) || !finiteNonNegative(usage.BytesOut) {
		return Usage{}, fmt.Errorf("quota: stored usage for %q has invalid bytes", usage.NodeID)
	}
	usage.Source = Source(source)
	if !usage.Source.Valid() {
		return Usage{}, fmt.Errorf("quota: stored usage for %q has invalid source", usage.NodeID)
	}
	parsedStart, err := storage.DecodeTime(start)
	if err != nil {
		return Usage{}, err
	}
	parsedEnd, err := storage.DecodeTime(end)
	if err != nil {
		return Usage{}, err
	}
	parsedUpdated, err := storage.DecodeTime(updatedAt)
	if err != nil {
		return Usage{}, err
	}
	usage.PeriodStart = parsedStart
	usage.PeriodEnd = parsedEnd
	usage.UpdatedAt = parsedUpdated
	if !usage.PeriodEnd.After(usage.PeriodStart) {
		return Usage{}, fmt.Errorf("quota: stored usage for %q has an invalid period", usage.NodeID)
	}
	return usage, nil
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return storage.EncodeTime(value)
}
