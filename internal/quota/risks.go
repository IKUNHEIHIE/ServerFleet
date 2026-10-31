package quota

import (
	"context"
	"errors"
	"sort"
	"time"
)

// TrafficRisk is the quota-owned risk view for visible nodes. Composition maps it
// to the HTTP transport type; quota does not import the monitoring package.
type TrafficRisk struct {
	NodeID    string
	Kind      string
	Severity  string
	Estimated bool
}

// ListTrafficRisks evaluates stored policies for the subject's readable nodes.
// Nodes without usage produce no threshold risk; expiry risk comes from the
// policy itself. A policy lookup or evaluation failure aborts the list rather
// than returning partial risk data.
func (s *Service) ListTrafficRisks(ctx context.Context, subjectID string) ([]TrafficRisk, error) {
	if s == nil {
		return nil, ErrServiceNotConfigured
	}
	policies, err := s.ListPolicies(ctx, subjectID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	risks := make([]TrafficRisk, 0, len(policies))
	for _, policy := range policies {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var usage *Usage
		latest, err := s.repo.LatestUsage(ctx, policy.NodeID)
		switch {
		case err == nil:
			value := latest
			usage = &value
		case errors.Is(err, ErrUsageNotFound):
		default:
			return nil, err
		}
		periodStart := monthStart
		if usage != nil {
			periodStart = usage.PeriodStart
		}
		evaluation := evaluate(policy, usage, periodStart, now)
		estimated := policy.Estimated || (usage != nil && usage.Source == SourceAgent)
		switch evaluation.Level {
		case LevelWarning:
			risks = append(risks, TrafficRisk{NodeID: policy.NodeID, Kind: "quota_near_limit", Severity: "warning", Estimated: estimated})
		case LevelExceeded:
			risks = append(risks, TrafficRisk{NodeID: policy.NodeID, Kind: "quota_exceeded", Severity: "critical", Estimated: estimated})
		}
		if evaluation.Expiring {
			kind, severity := "quota_expiring", "warning"
			if evaluation.Expired {
				kind, severity = "quota_expired", "critical"
			}
			risks = append(risks, TrafficRisk{NodeID: policy.NodeID, Kind: kind, Severity: severity, Estimated: estimated})
		}
	}
	sort.Slice(risks, func(i, j int) bool {
		if risks[i].NodeID == risks[j].NodeID {
			return risks[i].Kind < risks[j].Kind
		}
		return risks[i].NodeID < risks[j].NodeID
	})
	return risks, nil
}
