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
	// GapBefore prevents accounting across observations lost from the in-memory queue.
	GapBefore bool `json:"-"`
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

// CounterSampleMaxAge bounds both link validation and deferred settlement.
const CounterSampleMaxAge = 5 * time.Minute

const counterHistoryInterval = 5 * time.Minute

func ValidateCounterSample(sample CounterSample, now time.Time) error {
	decoded, err := hex.DecodeString(sample.Interfaces)
	if err != nil || len(decoded) != 32 || sample.Interfaces != strings.ToLower(sample.Interfaces) {
		return ErrInvalidUsage
	}
	if !validToken(sample.NodeID, 128) || sample.BootID == "" || len(sample.BootID) > 256 || sample.ObservedAt.IsZero() ||
		sample.ObservedAt.After(now.Add(CounterSampleMaxAge)) || sample.ObservedAt.Before(now.Add(-CounterSampleMaxAge)) ||
		sample.BytesIn > 1<<53-1 || sample.BytesOut > 1<<53-1 {
		return ErrInvalidUsage
	}
	return nil
}

func (s *Service) RecordCounter(ctx context.Context, sample CounterSample) error {
	return s.RecordCounterBatch(ctx, []CounterSample{sample})
}

// RecordCounterBatch settles samples for a single node in arrival order. It
// evaluates every observation but writes the baseline and each affected billing
// period once. The transaction makes a retry safe after an ambiguous commit.
func (s *Service) RecordCounterBatch(ctx context.Context, samples []CounterSample) error {
	if len(samples) == 0 {
		return nil
	}
	now := s.now().UTC()
	nodeID := samples[0].NodeID
	for _, sample := range samples {
		if sample.NodeID != nodeID || ValidateCounterSample(sample, now) != nil {
			return ErrInvalidUsage
		}
	}
	if _, err := s.nodes.Get(ctx, nodeID); err != nil {
		return err
	}

	return s.db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		policy, err := s.repo.getPolicy(ctx, tx, nodeID)
		if err != nil && !errors.Is(err, ErrPolicyNotFound) {
			return err
		}
		var old counterState
		var raw []byte
		err = tx.QueryRow(ctx, `SELECT payload FROM quota_counter_states WHERE node_id=?`, nodeID).Scan(&raw)
		found := err == nil
		if err != nil && !storage.IsNotFound(err) {
			return err
		}
		if found && (len(raw) > 8192 || json.Unmarshal(raw, &old) != nil || old.NodeID != nodeID) {
			return ErrInvalidUsage
		}

		var period counterBatchPeriod
		for _, sample := range samples {
			sample.ObservedAt = sample.ObservedAt.UTC()
			if found && !sample.ObservedAt.After(old.ObservedAt) {
				continue
			}
			if period.start.IsZero() || !sample.ObservedAt.Before(period.end) {
				if err := s.flushCounterPeriod(ctx, tx, period, old.Coverage); err != nil {
					return err
				}
				start, end, err := PolicyPeriod(sample.ObservedAt, policy)
				if err != nil {
					return err
				}
				period = counterBatchPeriod{start: start, end: end}
			}
			coverage := old.Coverage
			if !found || !coverage.PeriodStart.Equal(period.start) {
				coverage = CounterCoverage{NodeID: nodeID, PeriodStart: period.start, ObservedFrom: sample.ObservedAt, Estimated: true}
			}
			coverage.Samples++
			coverage.ObservedUntil = sample.ObservedAt
			continuous := found && !sample.GapBefore && old.Coverage.PeriodStart.Equal(period.start) &&
				old.BootID == sample.BootID && old.Interfaces == sample.Interfaces &&
				sample.BytesIn >= old.BytesIn && sample.BytesOut >= old.BytesOut &&
				sample.ObservedAt.Sub(old.ObservedAt) <= CounterSampleMaxAge
			if continuous {
				if err := s.addCounterDelta(ctx, tx, &period, sample, &old, now); err != nil {
					return err
				}
			} else if found || sample.GapBefore {
				coverage.Gaps++
			}
			old = counterState{CounterSample: sample, Coverage: coverage, LastHistoryAt: old.LastHistoryAt}
			found = true
		}
		if period.start.IsZero() {
			return nil
		}
		if err := s.flushCounterPeriod(ctx, tx, period, old.Coverage); err != nil {
			return err
		}
		raw, err = json.Marshal(old)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO quota_counter_states VALUES(?,?) ON CONFLICT(node_id) DO UPDATE SET payload=excluded.payload`, nodeID, raw)
		return err
	})
}

type counterBatchPeriod struct {
	start, end time.Time
	usage      Usage
	dirty      bool
}

func (s *Service) addCounterDelta(ctx context.Context, tx storage.Tx, period *counterBatchPeriod, sample CounterSample, old *counterState, now time.Time) error {
	if period.usage.NodeID == "" {
		usage, err := s.repo.GetPeriod(ctx, tx, sample.NodeID, period.start)
		if errors.Is(err, ErrUsageNotFound) {
			usage = Usage{NodeID: sample.NodeID, PeriodStart: period.start, PeriodEnd: period.end, Source: SourceAgent}
		} else if err != nil {
			return err
		}
		period.usage = usage
	}
	usage := &period.usage
	// Billing/manual corrections remain authoritative.
	if usage.Source != SourceAgent {
		return nil
	}
	usage.BytesIn += float64(sample.BytesIn - old.BytesIn)
	usage.BytesOut += float64(sample.BytesOut - old.BytesOut)
	usage.UpdatedAt = now
	if math.IsNaN(usage.TotalBytes()) || math.IsInf(usage.TotalBytes(), 0) || usage.BytesIn > 1<<53-1 || usage.BytesOut > 1<<53-1 {
		return ErrInvalidUsage
	}
	period.dirty = true
	if old.LastHistoryAt.IsZero() || sample.ObservedAt.Sub(old.LastHistoryAt) >= counterHistoryInterval {
		if err := s.repo.PutSample(ctx, tx, UsageSample{NodeID: sample.NodeID, PeriodStart: period.start, PeriodEnd: usage.PeriodEnd, BytesIn: usage.BytesIn, BytesOut: usage.BytesOut, Source: SourceAgent, ObservedAt: sample.ObservedAt}, now); err != nil {
			return err
		}
		old.LastHistoryAt = sample.ObservedAt
		if _, err := tx.Exec(ctx, `DELETE FROM traffic_samples WHERE node_id=? AND observed_at<?`, sample.NodeID, storage.EncodeTime(now.Add(-35*24*time.Hour))); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) flushCounterPeriod(ctx context.Context, tx storage.Tx, period counterBatchPeriod, coverage CounterCoverage) error {
	if period.start.IsZero() {
		return nil
	}
	if period.dirty {
		if err := s.repo.UpsertUsage(ctx, tx, period.usage); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(coverage)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO quota_counter_coverage VALUES(?,?,?) ON CONFLICT(node_id,period_start) DO UPDATE SET payload=excluded.payload`, coverage.NodeID, storage.EncodeTime(period.start), raw)
	return err
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
