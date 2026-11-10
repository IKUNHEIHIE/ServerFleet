package publishing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"serverfleet.local/serverfleet/internal/storage"
)

// Repo persists publication plans and stage rows. It never authorizes callers and
// never invokes a publisher.
type Repo struct {
	db         *storage.DB
	hasTargets bool
}

func NewRepo(db *storage.DB) (*Repo, error) {
	if db == nil {
		return nil, errors.New("publishing: database is required")
	}
	return &Repo{db: db, hasTargets: detectTargetColumns(db)}, nil
}

// detectTargetColumns reports whether the candidate ds/0081 columns were
// accepted by the integration owner. The target fields are never silently
// written into a schema that cannot store them.
func detectTargetColumns(db *storage.DB) bool {
	rows, err := db.Query(context.Background(), `PRAGMA table_info(publishing_plans)`)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid, notNull, primaryKey int
			name, columnType         string
			defaultValue             any
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false
		}
		if name == "gateway_ids" {
			return true
		}
	}
	return false
}

// TargetsPersisted reports whether the accepted schema has the candidate target
// columns. Callers must expose false rather than silently pretending targets were
// stored.
func (r *Repo) TargetsPersisted() bool { return r != nil && r.hasTargets }

const planSelectLegacy = `SELECT id, name, domain, service_group_id, expected_group_revision,
	status, current_stage, created_by, idempotency_key, created_at, updated_at
	FROM publishing_plans`

const planSelectTargets = `SELECT id, name, domain, service_group_id, expected_group_revision,
	gateway_ids, zone_id, certificate_id, https, target_revision,
	status, current_stage, created_by, idempotency_key, created_at, updated_at
	FROM publishing_plans`

func (r *Repo) planSelect() string {
	if r != nil && r.hasTargets {
		return planSelectTargets
	}
	return planSelectLegacy
}

const stageSelect = `SELECT plan_id, stage, status, attempts, result_summary,
	error_code, started_at, finished_at FROM publishing_stages`

func (r *Repo) Create(ctx context.Context, q storage.Queryer, plan Plan, stages []Stage) error {
	if r == nil || q == nil {
		return errors.New("publishing: repository is not configured")
	}
	if err := validatePlanRecord(plan); err != nil {
		return err
	}
	if err := validateInitialStages(plan, stages); err != nil {
		return err
	}
	var err error
	if r.hasTargets {
		gatewayJSON, marshalErr := json.Marshal(plan.GatewayIDs)
		if marshalErr != nil {
			return fmt.Errorf("publishing: encode gateway targets: %w", marshalErr)
		}
		_, err = q.Exec(ctx, `INSERT INTO publishing_plans
			(id, name, domain, service_group_id, expected_group_revision, gateway_ids, zone_id,
			 certificate_id, https, target_revision, status, current_stage, created_by,
			 idempotency_key, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			plan.ID, plan.Name, plan.Domain, plan.ServiceGroupID, int64(plan.ExpectedGroupRevision),
			string(gatewayJSON), plan.ZoneID, plan.CertificateID, boolInt(plan.HTTPS), int64(plan.TargetRevision),
			string(plan.Status), string(plan.CurrentStage), plan.CreatedBy, plan.IdempotencyKey,
			storage.EncodeTime(plan.CreatedAt), storage.EncodeTime(plan.UpdatedAt))
	} else {
		_, err = q.Exec(ctx, `INSERT INTO publishing_plans
			(id, name, domain, service_group_id, expected_group_revision, status,
			 current_stage, created_by, idempotency_key, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			plan.ID, plan.Name, plan.Domain, plan.ServiceGroupID, int64(plan.ExpectedGroupRevision),
			string(plan.Status), string(plan.CurrentStage), plan.CreatedBy, plan.IdempotencyKey,
			storage.EncodeTime(plan.CreatedAt), storage.EncodeTime(plan.UpdatedAt))
	}
	if err != nil {
		if storage.IsUniqueViolation(err) {
			var existing int
			if queryErr := q.QueryRow(ctx, "SELECT COUNT(*) FROM publishing_plans WHERE id = ?", plan.ID).Scan(&existing); queryErr == nil && existing > 0 {
				return ErrPlanExists
			}
			return ErrDuplicatePlan
		}
		return fmt.Errorf("publishing: insert plan: %w", err)
	}
	for _, stage := range stages {
		if _, err := q.Exec(ctx, `INSERT INTO publishing_stages
			(plan_id, stage, status, attempts, result_summary, error_code, started_at, finished_at)
			VALUES (?, ?, ?, ?, '', '', NULL, NULL)`,
			stage.PlanID, string(stage.Kind), string(stage.Status), stage.Attempts); err != nil {
			return fmt.Errorf("publishing: insert stage: %w", err)
		}
	}
	return nil
}

// List returns every plan ordered newest first. Callers still authorize each
// plan against its service group.
func (r *Repo) List(ctx context.Context) ([]Plan, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("publishing: repository is not configured")
	}
	rows, err := r.db.Query(ctx, r.planSelect()+" ORDER BY created_at DESC, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	plans := make([]Plan, 0)
	for rows.Next() {
		plan, err := scanPlan(rows, r.hasTargets)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, rows.Err()
}

func (r *Repo) Get(ctx context.Context, planID string) (Plan, []Stage, error) {
	if r == nil || r.db == nil {
		return Plan{}, nil, errors.New("publishing: repository is not configured")
	}
	plan, err := scanPlan(r.db.QueryRow(ctx, r.planSelect()+" WHERE id = ?", planID), r.hasTargets)
	if storage.IsNotFound(err) {
		return Plan{}, nil, ErrPlanNotFound
	}
	if err != nil {
		return Plan{}, nil, err
	}
	stages, err := r.stages(ctx, planID)
	if err != nil {
		return Plan{}, nil, err
	}
	return plan, stages, nil
}

func (r *Repo) GetByIdempotencyKey(ctx context.Context, createdBy, key string) (Plan, []Stage, bool, error) {
	if r == nil || r.db == nil {
		return Plan{}, nil, false, errors.New("publishing: repository is not configured")
	}
	if key == "" {
		return Plan{}, nil, false, nil
	}
	plan, err := scanPlan(r.db.QueryRow(ctx, r.planSelect()+" WHERE created_by = ? AND idempotency_key = ?", createdBy, key), r.hasTargets)
	if storage.IsNotFound(err) {
		return Plan{}, nil, false, nil
	}
	if err != nil {
		return Plan{}, nil, false, err
	}
	stages, err := r.stages(ctx, plan.ID)
	if err != nil {
		return Plan{}, nil, false, err
	}
	return plan, stages, true, nil
}

func (r *Repo) UpdatePlan(ctx context.Context, q storage.Queryer, plan Plan) error {
	if r == nil || q == nil {
		return errors.New("publishing: repository is not configured")
	}
	if err := validatePlanRecord(plan); err != nil {
		return err
	}
	outcome, err := q.Exec(ctx, `UPDATE publishing_plans SET status = ?, current_stage = ?,
		updated_at = ? WHERE id = ?`,
		string(plan.Status), string(plan.CurrentStage), storage.EncodeTime(plan.UpdatedAt), plan.ID)
	if err != nil {
		return err
	}
	affected, err := outcome.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrPlanNotFound
	}
	return nil
}

func (r *Repo) UpdateStage(ctx context.Context, q storage.Queryer, stage Stage) error {
	if r == nil || q == nil {
		return errors.New("publishing: repository is not configured")
	}
	if err := validateStageRecord(stage); err != nil {
		return err
	}
	outcome, err := q.Exec(ctx, `UPDATE publishing_stages SET status = ?, attempts = ?,
		result_summary = ?, error_code = ?, started_at = ?, finished_at = ?
		WHERE plan_id = ? AND stage = ?`,
		string(stage.Status), stage.Attempts, stage.ResultSummary, stage.ErrorCode,
		optionalTime(stage.StartedAt), optionalTime(stage.FinishedAt),
		stage.PlanID, string(stage.Kind))
	if err != nil {
		return err
	}
	affected, err := outcome.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrStageNotFound
	}
	return nil
}

func (r *Repo) stages(ctx context.Context, planID string) ([]Stage, error) {
	rows, err := r.db.Query(ctx, stageSelect+` WHERE plan_id = ?
		ORDER BY CASE stage WHEN 'dns' THEN 0 WHEN 'certificate' THEN 1 WHEN 'gateway' THEN 2 ELSE 9 END`,
		planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stages := make([]Stage, 0, 3)
	seen := make(map[StageKind]struct{}, 3)
	for rows.Next() {
		stage, err := scanStage(rows)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[stage.Kind]; duplicate {
			return nil, fmt.Errorf("publishing: duplicate stage %q", stage.Kind)
		}
		seen[stage.Kind] = struct{}{}
		stages = append(stages, stage)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, kind := range StageKinds() {
		if _, ok := seen[kind]; !ok {
			return nil, fmt.Errorf("publishing: plan %q is missing stage %q", planID, kind)
		}
	}
	return stages, nil
}

func scanPlan(row scanner, hasTargets bool) (Plan, error) {
	var (
		plan             Plan
		revision         int64
		gatewayJSON      string
		https            int
		targetRevision   int64
		status, current  string
		created, updated string
	)
	if hasTargets {
		if err := row.Scan(&plan.ID, &plan.Name, &plan.Domain, &plan.ServiceGroupID, &revision,
			&gatewayJSON, &plan.ZoneID, &plan.CertificateID, &https, &targetRevision,
			&status, &current, &plan.CreatedBy, &plan.IdempotencyKey, &created, &updated); err != nil {
			return Plan{}, err
		}
		if err := json.Unmarshal([]byte(gatewayJSON), &plan.GatewayIDs); err != nil {
			return Plan{}, fmt.Errorf("publishing: stored plan %q has invalid gateway targets", plan.ID)
		}
		plan.HTTPS = https != 0
		if targetRevision < 1 {
			return Plan{}, fmt.Errorf("publishing: stored plan %q has invalid target revision", plan.ID)
		}
		plan.TargetRevision = uint64(targetRevision)
		plan.TargetsPersisted = true
	} else {
		if err := row.Scan(&plan.ID, &plan.Name, &plan.Domain, &plan.ServiceGroupID, &revision,
			&status, &current, &plan.CreatedBy, &plan.IdempotencyKey, &created, &updated); err != nil {
			return Plan{}, err
		}
		plan.TargetRevision = 1
		plan.TargetsPersisted = false
	}
	plan.Status = PlanStatus(status)
	if !plan.Status.Valid() {
		return Plan{}, fmt.Errorf("publishing: stored plan %q has invalid status", plan.ID)
	}
	plan.CurrentStage = StageKind(current)
	if !plan.CurrentStage.Valid() {
		return Plan{}, fmt.Errorf("publishing: stored plan %q has invalid current stage", plan.ID)
	}
	if revision < 1 {
		return Plan{}, fmt.Errorf("publishing: stored plan %q has invalid revision", plan.ID)
	}
	plan.ExpectedGroupRevision = uint64(revision)
	var err error
	if plan.CreatedAt, err = storage.DecodeTime(created); err != nil {
		return Plan{}, err
	}
	if plan.UpdatedAt, err = storage.DecodeTime(updated); err != nil {
		return Plan{}, err
	}
	if err := validatePlanRecord(plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func scanStage(row scanner) (Stage, error) {
	var (
		stage        Stage
		kind, status string
		started      storage.NullTime
		finished     storage.NullTime
	)
	if err := row.Scan(&stage.PlanID, &kind, &status, &stage.Attempts,
		&stage.ResultSummary, &stage.ErrorCode, &started, &finished); err != nil {
		return Stage{}, err
	}
	stage.Kind = StageKind(kind)
	stage.Status = StageStatus(status)
	if !stage.Kind.Valid() {
		return Stage{}, fmt.Errorf("publishing: stored stage has invalid kind %q", kind)
	}
	if !stage.Status.Valid() {
		return Stage{}, fmt.Errorf("publishing: stored stage %q has invalid status", kind)
	}
	if stage.Attempts < 0 {
		return Stage{}, fmt.Errorf("publishing: stored stage %q has negative attempts", kind)
	}
	if started.Valid {
		stage.StartedAt = started.Time
	}
	if finished.Valid {
		stage.FinishedAt = finished.Time
	}
	if err := validateStageRecord(stage); err != nil {
		return Stage{}, err
	}
	return stage, nil
}

func validatePlanRecord(plan Plan) error {
	if !validID(plan.ID) {
		return fmt.Errorf("%w: id", ErrInvalidPlan)
	}
	if !validText(plan.Name, maxPlanNameBytes) {
		return fmt.Errorf("%w: name", ErrInvalidPlan)
	}
	domain, err := normalizeDomain(plan.Domain)
	if err != nil {
		return err
	}
	if domain != plan.Domain {
		return fmt.Errorf("%w: domain must be canonical", ErrInvalidPlan)
	}
	if !validID(plan.ServiceGroupID) {
		return fmt.Errorf("%w: service group id", ErrInvalidPlan)
	}
	if plan.ExpectedGroupRevision == 0 {
		return fmt.Errorf("%w: expected group revision", ErrInvalidPlan)
	}
	if err := validateTargets(plan.GatewayIDs, plan.ZoneID, plan.CertificateID, plan.TargetRevision); err != nil {
		return err
	}
	if !plan.Status.Valid() {
		return fmt.Errorf("%w: status", ErrInvalidPlan)
	}
	if !plan.CurrentStage.Valid() {
		return fmt.Errorf("%w: current stage", ErrInvalidPlan)
	}
	if !validToken(plan.CreatedBy, 128) {
		return fmt.Errorf("%w: created by", ErrInvalidPlan)
	}
	if plan.IdempotencyKey != "" && !validID(plan.IdempotencyKey) {
		return fmt.Errorf("%w: idempotency key", ErrInvalidPlan)
	}
	if plan.CreatedAt.IsZero() || plan.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: timestamps", ErrInvalidPlan)
	}
	return nil
}

func validateInitialStages(plan Plan, stages []Stage) error {
	if len(stages) != len(StageKinds()) {
		return fmt.Errorf("%w: exactly three stages are required", ErrInvalidPlan)
	}
	seen := make(map[StageKind]struct{}, len(stages))
	for _, stage := range stages {
		if stage.PlanID != plan.ID {
			return fmt.Errorf("%w: stage plan id", ErrInvalidPlan)
		}
		if !stage.Kind.Valid() {
			return fmt.Errorf("%w: stage kind", ErrInvalidPlan)
		}
		if _, duplicate := seen[stage.Kind]; duplicate {
			return fmt.Errorf("%w: duplicate stage", ErrInvalidPlan)
		}
		seen[stage.Kind] = struct{}{}
		if stage.Status != StagePending || stage.Attempts != 0 || stage.ResultSummary != "" ||
			stage.ErrorCode != "" || !stage.StartedAt.IsZero() || !stage.FinishedAt.IsZero() {
			return fmt.Errorf("%w: initial stage state", ErrInvalidPlan)
		}
	}
	for _, kind := range StageKinds() {
		if _, ok := seen[kind]; !ok {
			return fmt.Errorf("%w: missing stage %q", ErrInvalidPlan, kind)
		}
	}
	return nil
}

func validateStageRecord(stage Stage) error {
	if !validID(stage.PlanID) || !stage.Kind.Valid() {
		return fmt.Errorf("%w: stage identity", ErrInvalidStage)
	}
	if !stage.Status.Valid() {
		return fmt.Errorf("%w: stage status", ErrInvalidStage)
	}
	if stage.Attempts < 0 {
		return fmt.Errorf("%w: attempts", ErrInvalidStage)
	}
	if stage.ResultSummary != "" && sanitizeReceipt(stage.ResultSummary) != stage.ResultSummary {
		return fmt.Errorf("%w: result summary is not redacted", ErrInvalidStage)
	}
	if stage.ErrorCode != "" && !validErrorCode(stage.ErrorCode) {
		return fmt.Errorf("%w: error code", ErrInvalidStage)
	}
	if stage.Status == StageSucceeded && stage.ResultSummary == "" {
		return fmt.Errorf("%w: succeeded stage needs a validated result", ErrInvalidStage)
	}
	if stage.Status == StageFailed && stage.ErrorCode == "" {
		return fmt.Errorf("%w: failed stage needs an error code", ErrInvalidStage)
	}
	if !stage.StartedAt.IsZero() && !stage.FinishedAt.IsZero() && stage.FinishedAt.Before(stage.StartedAt) {
		return fmt.Errorf("%w: stage timestamps", ErrInvalidStage)
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func optionalTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return storage.EncodeTime(value)
}
