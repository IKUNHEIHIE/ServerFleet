package quota

import (
	"context"
	"encoding/json"
	"serverfleet.local/serverfleet/internal/storage"
	"time"
)

type AutomationAuthority func(context.Context, Policy) error
type policyAutomation struct {
	Direction     string
	ThresholdsPct []int
	RemindDays    []int
	ExpiryType    string
	OwnerID       string
	Revision      uint64
	ResetDay      int
	Timezone      string
}

func (s *Service) bindPolicyTx(ctx context.Context, tx storage.Tx, policy Policy, owner string) (Policy, error) {
	var raw []byte
	var previous policyAutomation
	e := tx.QueryRow(ctx, `SELECT payload FROM quota_policy_automation WHERE node_id=?`, policy.NodeID).Scan(&raw)
	if e != nil && !storage.IsNotFound(e) {
		return policy, e
	}
	if e == nil && json.Unmarshal(raw, &previous) != nil {
		return policy, ErrInvalidPolicy
	}
	next := policyAutomation{Direction: policy.Direction, ThresholdsPct: policy.ThresholdsPct, RemindDays: policy.RemindDays, ExpiryType: policy.ExpiryType, OwnerID: owner, Revision: previous.Revision + 1, ResetDay: policy.ResetDay, Timezone: policy.Timezone}
	if next.Revision > 1<<53-1 {
		return policy, ErrInvalidPolicy
	}
	raw, _ = json.Marshal(next)
	_, e = tx.Exec(ctx, `INSERT INTO quota_policy_automation VALUES(?,?) ON CONFLICT(node_id) DO UPDATE SET payload=excluded.payload`, policy.NodeID, raw)
	policy.AutomationOwnerID = owner
	policy.AutomationRevision = next.Revision
	return policy, e
}
func (r *Repo) hydratePolicy(ctx context.Context, policy *Policy) error {
	policy.Direction = "combined"
	policy.ResetDay = 1
	policy.Timezone = "UTC"
	var raw []byte
	e := r.db.QueryRow(ctx, `SELECT payload FROM quota_policy_automation WHERE node_id=?`, policy.NodeID).Scan(&raw)
	if storage.IsNotFound(e) {
		return nil
	}
	if e != nil {
		return e
	}
	var a policyAutomation
	if json.Unmarshal(raw, &a) != nil || a.Revision == 0 || a.ResetDay < 1 || a.ResetDay > 28 {
		return ErrInvalidPolicy
	}
	if _, e = time.LoadLocation(a.Timezone); e != nil {
		return ErrInvalidPolicy
	}
	policy.Direction = a.Direction
	if policy.Direction == "" {
		policy.Direction = "combined"
	}
	policy.ThresholdsPct = a.ThresholdsPct
	policy.RemindDays = a.RemindDays
	policy.ExpiryType = a.ExpiryType
	policy.AutomationOwnerID = a.OwnerID
	policy.AutomationRevision = a.Revision
	policy.ResetDay = a.ResetDay
	policy.Timezone = a.Timezone
	return nil
}
func (s *Service) AutomationCurrent(ctx context.Context, id, owner string, revision uint64) error {
	p, e := s.repo.GetPolicy(ctx, id)
	if e != nil {
		return e
	}
	if p.AutomationOwnerID != owner || revision == 0 || p.AutomationRevision != revision {
		return ErrInvalidPolicy
	}
	if s.automationCurrent != nil {
		return s.automationCurrent(ctx, p)
	}
	return nil
}
func PolicyPeriod(now time.Time, p Policy) (time.Time, time.Time, error) {
	day := p.ResetDay
	if day == 0 {
		day = 1
	}
	zone := p.Timezone
	if zone == "" {
		zone = "UTC"
	}
	if day < 1 || day > 28 || len(zone) > 64 {
		return time.Time{}, time.Time{}, ErrInvalidPolicy
	}
	loc, e := time.LoadLocation(zone)
	if e != nil {
		return time.Time{}, time.Time{}, ErrInvalidPolicy
	}
	local := now.In(loc)
	start := time.Date(local.Year(), local.Month(), day, 0, 0, 0, 0, loc)
	if local.Before(start) {
		start = start.AddDate(0, -1, 0)
	}
	return start.UTC(), start.AddDate(0, 1, 0).UTC(), nil
}
