package publishing

import (
	"context"
	"errors"

	"serverfleet.local/serverfleet/internal/storage"
)

// A replacement retains the route identities and DNS of one completed plan.
// Unique base IDs serialize successive replacements; existing sites are never
// removed as automatic compensation for a certificate rotation.
func CertificateReplacementMigrations() []storage.Migration {
	return []storage.Migration{{Version: "ds/0083_certificate_replacements", Name: "immutable certificate-only publication successors", Statements: []string{
		`CREATE TABLE publishing_certificate_replacements(plan_id TEXT PRIMARY KEY,base_plan_id TEXT NOT NULL UNIQUE,route_plan_id TEXT NOT NULL,previous_certificate_id TEXT NOT NULL)`,
	}}}
}
func CertificateRoutePlan(ctx context.Context, q storage.Queryer, id string) (string, error) {
	var route string
	e := q.QueryRow(ctx, `SELECT route_plan_id FROM publishing_certificate_replacements WHERE plan_id=?`, id).Scan(&route)
	if storage.IsNotFound(e) {
		return id, nil
	}
	if e != nil || !validID(route) {
		return "", ErrProductionTarget
	}
	return route, nil
}

// ReplaceCertificate freezes a new publication without reapplying public DNS.
// The certificate inspection and gateway stages still use the normal approval,
// artifact, Agent and TLS/HTTP proof pipeline.
func (s *Service) ReplaceCertificate(ctx context.Context, subject, request, id, baseID, expectedCertificate, replacement string) (Plan, error) {
	if s.requireRequest(request) != nil || !validID(id) || !validID(baseID) || !validID(replacement) || replacement == expectedCertificate {
		return Plan{}, ErrInvalidPlan
	}
	unlock := s.lockPlan(baseID)
	defer unlock()
	base, _, e := s.Get(ctx, subject, baseID)
	if e != nil {
		return Plan{}, e
	}
	group, e := s.lookupGroup(ctx, base.ServiceGroupID)
	if e != nil {
		return Plan{}, e
	}
	if e = s.authorize(ctx, subject, ActionWrite, request, group.NodeIDs, "certificate_publication_successor"); e != nil {
		return Plan{}, e
	}
	if existing, _, e := s.repo.Get(ctx, id); e == nil {
		var storedBase string
		if s.db.QueryRow(ctx, `SELECT base_plan_id FROM publishing_certificate_replacements WHERE plan_id=?`, id).Scan(&storedBase) != nil || storedBase != baseID || existing.CreatedBy != subject || existing.CertificateID != replacement {
			return Plan{}, ErrRevisionConflict
		}
		return existing, nil
	} else if !errors.Is(e, ErrPlanNotFound) {
		return Plan{}, e
	}
	if !base.TargetsPersisted || !base.HTTPS || base.Status != PlanCompleted || base.CertificateID != expectedCertificate || group.Revision != base.ExpectedGroupRevision || base.TargetRevision >= 1<<53-1 {
		return Plan{}, ErrRevisionConflict
	}
	route, e := CertificateRoutePlan(ctx, s.db, baseID)
	if e != nil {
		return Plan{}, e
	}
	next := base
	next.ID = id
	next.CreatedBy = subject
	next.CertificateID = replacement
	next.TargetRevision++
	next.Status = PlanQueued
	next.CurrentStage = StageCertificate
	next.IdempotencyKey = "certificate-successor-" + id
	next.CreatedAt = s.now().UTC()
	next.UpdatedAt = next.CreatedAt
	stages := newPlanStages(id)
	e = s.db.InTx(ctx, func(c context.Context, tx storage.Tx) error {
		if e := s.repo.Create(c, tx, next, stages); e != nil {
			return e
		}
		if _, e := tx.Exec(c, `INSERT INTO publishing_certificate_replacements VALUES(?,?,?,?)`, id, baseID, route, expectedCertificate); e != nil {
			return e
		}
		stages[0].Status = StageSucceeded
		stages[0].Attempts = 1
		stages[0].StartedAt = next.CreatedAt
		stages[0].FinishedAt = next.CreatedAt
		stages[0].ResultSummary = "Existing DNS retained from completed publication " + baseID
		if e := s.repo.UpdateStage(c, tx, stages[0]); e != nil {
			return e
		}
		return s.audit.AppendTx(c, tx, s.event(subject, ActionCreate, "success", request, group.NodeIDs, "certificate_only_successor_dns_retained"))
	})
	return next, e
}

// DeferredGatewayPublisher only resumes the same deterministic child task IDs.
type DeferredGatewayPublisher interface {
	GatewayPublisher
	CanResumeGatewayStage()
}

func (*productionGateway) CanResumeGatewayStage() {}

type taskObservationKey struct{}

func ObserveTasksWithoutWaiting(ctx context.Context) context.Context {
	return context.WithValue(ctx, taskObservationKey{}, true)
}
func NonBlockingTaskObservation(ctx context.Context) bool {
	v, _ := ctx.Value(taskObservationKey{}).(bool)
	return v
}

// Retaining DNS and an existing site means generic delete compensation is not a
// safe inverse. Rotation failures require inspection or a new approved rotation.
func (s *Service) isCertificateReplacement(ctx context.Context, id string) bool {
	var v string
	e := s.db.QueryRow(ctx, `SELECT plan_id FROM publishing_certificate_replacements WHERE plan_id=? OR base_plan_id=? LIMIT 1`, id, id).Scan(&v)
	if e == nil {
		return true
	}
	if storage.IsNotFound(e) {
		return false
	}
	var table string
	if err := s.db.QueryRow(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name='publishing_certificate_replacements'`).Scan(&table); storage.IsNotFound(err) {
		var count int
		if s.db.QueryRow(ctx, `SELECT count(*) FROM schema_migrations WHERE version=?`, "ds/0083_certificate_replacements").Scan(&count) != nil {
			return true
		}
		return count > 0
	}
	return true
}

// Follow completed successors; node renewals wait for a pending rotation.
func (s *Service) LatestCertificatePlan(ctx context.Context, subject, id string) (Plan, error) {
	for n := 0; n < 256; n++ {
		p, _, e := s.Get(ctx, subject, id)
		if e != nil {
			return Plan{}, e
		}
		var next string
		e = s.db.QueryRow(ctx, `SELECT plan_id FROM publishing_certificate_replacements WHERE base_plan_id=?`, id).Scan(&next)
		if storage.IsNotFound(e) {
			return p, nil
		}
		if e != nil {
			return Plan{}, e
		}
		p, _, e = s.Get(ctx, subject, next)
		if e != nil {
			return Plan{}, e
		}
		if p.Status == PlanQueued || p.Status == PlanRunning {
			return Plan{}, ErrStageAwaitingTask
		}
		if p.Status != PlanCompleted {
			return Plan{}, ErrProductionTarget
		}
		id = next
	}
	return Plan{}, ErrProductionTarget
}
