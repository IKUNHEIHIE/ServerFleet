package quota

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"serverfleet.local/serverfleet/internal/storage"
	"strings"
	"time"
)

type CounterSample struct {
	NodeID     string
	BootID     string
	Interfaces string
	BytesIn    uint64
	BytesOut   uint64
	ObservedAt time.Time
}
type CounterCoverage struct {
	NodeID        string    `json:"nodeId"`
	PeriodStart   time.Time `json:"periodStart"`
	ObservedFrom  time.Time `json:"observedFrom"`
	ObservedUntil time.Time `json:"observedUntil"`
	Samples       uint64    `json:"samples"`
	Gaps          uint64    `json:"gaps"`
	Estimated     bool      `json:"estimated"`
}
type counterState struct {
	LastHistoryAt time.Time
	CounterSample
	Coverage CounterCoverage
}

func AutomationMigrations() []storage.Migration {
	return []storage.Migration{{Version: "ds/0086_traffic_counter_observations", Name: "durable authenticated host counter deltas and coverage", Statements: []string{
		`CREATE TABLE quota_policy_automation(node_id TEXT PRIMARY KEY,payload BLOB NOT NULL CHECK(length(payload)<=4096))`,
		`CREATE TABLE quota_counter_states(node_id TEXT PRIMARY KEY,payload BLOB NOT NULL CHECK(length(payload)<=8192))`,
		`CREATE TABLE quota_counter_coverage(node_id TEXT NOT NULL,period_start TEXT NOT NULL,payload BLOB NOT NULL CHECK(length(payload)<=4096),PRIMARY KEY(node_id,period_start))`,
	}}}
}

// RecordCounter accumulates only observed differences. A first sample, reboot,
// changed interface set, gap over five minutes or month boundary establishes a
// baseline; it never fabricates pre-observation/monthly/provider traffic.
func (s *Service) RecordCounter(ctx context.Context, sample CounterSample) error {
	now := s.now().UTC()
	sample.ObservedAt = sample.ObservedAt.UTC()
	decoded, decodeErr := hex.DecodeString(sample.Interfaces)
	if decodeErr != nil || len(decoded) != 32 || sample.Interfaces != strings.ToLower(sample.Interfaces) {
		return ErrInvalidUsage
	}
	if !validToken(sample.NodeID, 128) || sample.BootID == "" || len(sample.BootID) > 256 || len(sample.Interfaces) != 64 || sample.ObservedAt.IsZero() || sample.ObservedAt.After(now.Add(5*time.Minute)) || sample.ObservedAt.Before(now.Add(-5*time.Minute)) || sample.BytesIn > 1<<53-1 || sample.BytesOut > 1<<53-1 {
		return ErrInvalidUsage
	}
	if _, e := s.nodes.Get(ctx, sample.NodeID); e != nil {
		return e
	}
	policy, e := s.repo.GetPolicy(ctx, sample.NodeID)
	if e != nil && !errors.Is(e, ErrPolicyNotFound) {
		return e
	}
	period, end, e := PolicyPeriod(sample.ObservedAt, policy)
	if e != nil {
		return e
	}
	return s.db.InTx(ctx, func(c context.Context, tx storage.Tx) error {
		old := counterState{}
		var raw []byte
		e := tx.QueryRow(c, `SELECT payload FROM quota_counter_states WHERE node_id=?`, sample.NodeID).Scan(&raw)
		found := e == nil
		if e != nil && !storage.IsNotFound(e) {
			return e
		}
		if found && (len(raw) > 8192 || json.Unmarshal(raw, &old) != nil || old.NodeID != sample.NodeID) {
			return ErrInvalidUsage
		}
		if found && !sample.ObservedAt.After(old.ObservedAt) {
			return nil
		}
		coverage := old.Coverage
		if !found || !coverage.PeriodStart.Equal(period) {
			coverage = CounterCoverage{NodeID: sample.NodeID, PeriodStart: period, ObservedFrom: sample.ObservedAt, Estimated: true}
		}
		coverage.Samples++
		coverage.ObservedUntil = sample.ObservedAt
		continuous := found && old.Coverage.PeriodStart.Equal(period) && old.BootID == sample.BootID && old.Interfaces == sample.Interfaces && sample.BytesIn >= old.BytesIn && sample.BytesOut >= old.BytesOut && sample.ObservedAt.Sub(old.ObservedAt) <= 5*time.Minute
		if !continuous && found {
			coverage.Gaps++
		}
		if continuous {
			usage, err := s.repo.GetPeriod(c, tx, sample.NodeID, period)
			if errors.Is(err, ErrUsageNotFound) {
				usage = Usage{NodeID: sample.NodeID, PeriodStart: period, PeriodEnd: end, Source: SourceAgent}
			} else if err != nil {
				return err
			}
			// Billing/manual corrections remain authoritative. Keep counter baselines
			// current without replacing the authoritative period with an estimate.
			if usage.Source == SourceAgent {
				usage.BytesIn += float64(sample.BytesIn - old.BytesIn)
				usage.BytesOut += float64(sample.BytesOut - old.BytesOut)
				usage.UpdatedAt = now
				if math.IsNaN(usage.TotalBytes()) || math.IsInf(usage.TotalBytes(), 0) || usage.BytesIn > 1<<53-1 || usage.BytesOut > 1<<53-1 {
					return ErrInvalidUsage
				}
				if err = s.repo.UpsertUsage(c, tx, usage); err != nil {
					return err
				}
				if old.LastHistoryAt.IsZero() || sample.ObservedAt.Sub(old.LastHistoryAt) >= 5*time.Minute {
					if err = s.repo.PutSample(c, tx, UsageSample{NodeID: sample.NodeID, PeriodStart: period, PeriodEnd: usage.PeriodEnd, BytesIn: usage.BytesIn, BytesOut: usage.BytesOut, Source: SourceAgent, ObservedAt: sample.ObservedAt}, now); err != nil {
						return err
					}
					old.LastHistoryAt = sample.ObservedAt
					if _, err = tx.Exec(c, `DELETE FROM traffic_samples WHERE node_id=? AND observed_at<?`, sample.NodeID, storage.EncodeTime(now.Add(-35*24*time.Hour))); err != nil {
						return err
					}
				}

			}
		}
		state := counterState{CounterSample: sample, Coverage: coverage, LastHistoryAt: old.LastHistoryAt}
		raw, e = json.Marshal(state)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(c, `INSERT INTO quota_counter_states VALUES(?,?) ON CONFLICT(node_id) DO UPDATE SET payload=excluded.payload`, sample.NodeID, raw); e != nil {
			return e
		}
		raw, e = json.Marshal(coverage)
		if e != nil {
			return e
		}
		_, e = tx.Exec(c, `INSERT INTO quota_counter_coverage VALUES(?,?,?) ON CONFLICT(node_id,period_start) DO UPDATE SET payload=excluded.payload`, sample.NodeID, storage.EncodeTime(period), raw)
		return e
	})
}
func (s *Service) CounterCoverage(ctx context.Context, subject, node string, period time.Time) (CounterCoverage, bool, error) {
	if e := s.authorizeRead(ctx, subject, node, "traffic_counter_coverage"); e != nil {
		return CounterCoverage{}, false, e
	}
	var raw []byte
	e := s.db.QueryRow(ctx, `SELECT payload FROM quota_counter_coverage WHERE node_id=? AND period_start=?`, node, storage.EncodeTime(period.UTC())).Scan(&raw)
	if storage.IsNotFound(e) {
		return CounterCoverage{}, false, nil
	}
	if e != nil {
		return CounterCoverage{}, false, e
	}
	var out CounterCoverage
	if json.Unmarshal(raw, &out) != nil || out.NodeID != node || !out.PeriodStart.Equal(period.UTC()) {
		return out, false, ErrInvalidUsage
	}
	return out, true, nil
}
