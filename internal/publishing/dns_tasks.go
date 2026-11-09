package publishing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/storage"
)

var ErrStageAwaitingTask = errors.New("publishing: stage is awaiting its unified task")

// DeferredDNSPublisher resumes only its own persisted DNS child, never an
// arbitrary running stage or an unobserved provider effect.
type DeferredDNSPublisher interface {
	DNSPublisher
	CanResumeDNSStage()
}

type rollbackActorKey struct{}
type rollbackActor struct{ subject, request string }

func DNSRollbackActor(ctx context.Context) (string, string, error) {
	actor, ok := ctx.Value(rollbackActorKey{}).(rollbackActor)
	if !ok || actor.subject == "" || actor.request == "" {
		return "", "", ErrProductionTarget
	}
	return actor.subject, actor.request, nil
}

type DNSBatchRecord struct {
	Type           string `json:"type"`
	Name           string `json:"name"`
	Content        string `json:"content"`
	TTL            int    `json:"ttl"`
	ExistingID     string `json:"existingId,omitempty"`
	ExistingSHA256 string `json:"existingSha256,omitempty"`
}
type DNSBatchDescriptor struct {
	PlanID         string           `json:"planId"`
	PlanSHA256     string           `json:"planSha256"`
	TargetRevision uint64           `json:"targetRevision"`
	ZoneID         string           `json:"zoneId"`
	ProfileSHA256  string           `json:"profileSha256"`
	Records        []DNSBatchRecord `json:"records"`
}
type DNSBatchObservation struct {
	State        string `json:"state"`
	RecordID     string `json:"recordId,omitempty"`
	RecordSHA256 string `json:"recordSha256,omitempty"`
}
type DNSBatch struct {
	ID                 string                `json:"id"`
	Descriptor         DNSBatchDescriptor    `json:"descriptor"`
	ApplyTaskID        string                `json:"applyTaskId,omitempty"`
	ApplyPlanSHA256    string                `json:"applyPlanSha256,omitempty"`
	RollbackTaskID     string                `json:"rollbackTaskId,omitempty"`
	RollbackPlanSHA256 string                `json:"rollbackPlanSha256,omitempty"`
	State              string                `json:"state"`
	Observations       []DNSBatchObservation `json:"observations"`
	UpdatedAt          time.Time             `json:"updatedAt"`
}

func DNSBatchMigrations() []storage.Migration {
	return []storage.Migration{{Version: "ds/0082_publishing_dns_tasks", Name: "frozen publication DNS child tasks and provider observations", Statements: []string{`CREATE TABLE publishing_dns_batches(id TEXT PRIMARY KEY,plan_id TEXT NOT NULL UNIQUE REFERENCES publishing_plans(id),payload BLOB NOT NULL CHECK(length(payload)<=65536))`}}}
}
func PublicationPlanSHA256(plan Plan) string {
	// Status/times change during observation; all actual execution targets and
	// the original creator are immutable members of this frozen identity.
	raw, _ := json.Marshal(struct {
		ID, Domain, Group, Zone, Certificate, Creator string
		GroupRevision, TargetRevision                 uint64
		HTTPS                                         bool
		Gateways                                      []string
	}{plan.ID, plan.Domain, plan.ServiceGroupID, plan.ZoneID, plan.CertificateID, plan.CreatedBy, plan.ExpectedGroupRevision, plan.TargetRevision, plan.HTTPS, plan.GatewayIDs})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
func DNSDescriptorSHA256(d DNSBatchDescriptor) string {
	raw, _ := json.Marshal(d)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func ReadDNSBatch(ctx context.Context, q storage.Queryer, id string) (DNSBatch, error) {
	var batch DNSBatch
	var raw []byte
	if err := q.QueryRow(ctx, `SELECT payload FROM publishing_dns_batches WHERE id=? AND length(payload)<=65536`, id).Scan(&raw); err != nil {
		return batch, err
	}
	if json.Unmarshal(raw, &batch) != nil || batch.ID != id || DNSDescriptorSHA256(batch.Descriptor) != id || len(batch.Descriptor.Records) < 1 || len(batch.Descriptor.Records) > 8 || len(batch.Observations) != len(batch.Descriptor.Records) {
		return batch, ErrProductionTarget
	}
	return batch, nil
}
func FindDNSBatch(ctx context.Context, q storage.Queryer, plan string) (DNSBatch, error) {
	var id string
	if err := q.QueryRow(ctx, `SELECT id FROM publishing_dns_batches WHERE plan_id=?`, plan).Scan(&id); err != nil {
		return DNSBatch{}, err
	}
	return ReadDNSBatch(ctx, q, id)
}
func ReserveDNSBatchTx(ctx context.Context, tx storage.Tx, batch DNSBatch) error {
	if batch.ID != DNSDescriptorSHA256(batch.Descriptor) || batch.State != "reserved" || len(batch.Descriptor.Records) < 1 || len(batch.Descriptor.Records) > 8 || len(batch.Observations) != len(batch.Descriptor.Records) || batch.ApplyTaskID != "" || batch.RollbackTaskID != "" {
		return ErrProductionTarget
	}
	for i, record := range batch.Descriptor.Records {
		if record.Name == "" || record.Content == "" || record.TTL != 60 || record.Type != "A" && record.Type != "AAAA" || batch.Observations[i].State != "pending" {
			return ErrProductionTarget
		}
	}
	raw, err := json.Marshal(batch)
	if err != nil || len(raw) > 64<<10 {
		return ErrProductionTarget
	}
	_, err = tx.Exec(ctx, `INSERT INTO publishing_dns_batches VALUES(?,?,?)`, batch.ID, batch.Descriptor.PlanID, raw)
	return err
}

// UpdateDNSBatchTx requires an exact previous snapshot. State transitions and
// audit are written in one owner transaction; a lost commit never permits replay.
func UpdateDNSBatchTx(ctx context.Context, tx storage.Tx, before, after DNSBatch) error {
	current, err := ReadDNSBatch(ctx, tx, before.ID)
	if err != nil {
		return err
	}
	left, _ := json.Marshal(current)
	right, _ := json.Marshal(before)
	if string(left) != string(right) || after.ID != before.ID || DNSDescriptorSHA256(after.Descriptor) != before.ID || len(after.Observations) != len(before.Observations) {
		return ErrProductionTarget
	}
	if before.ApplyTaskID != "" && (before.ApplyTaskID != after.ApplyTaskID || before.ApplyPlanSHA256 != after.ApplyPlanSHA256) || before.RollbackTaskID != "" && (before.RollbackTaskID != after.RollbackTaskID || before.RollbackPlanSHA256 != after.RollbackPlanSHA256) {
		return ErrProductionTarget
	}
	switch after.State {
	case "reserved", "running", "applied", "partial", "unknown", "rolling_back", "rolled_back":
	default:
		return ErrProductionTarget
	}
	for _, r := range after.Observations {
		switch r.State {
		case "pending", "creating", "reused", "created", "deleting", "deleted", "unknown":
		default:
			return ErrProductionTarget
		}
	}
	raw, err := json.Marshal(after)
	if err != nil || len(raw) > 64<<10 {
		return ErrProductionTarget
	}
	_, err = tx.Exec(ctx, `UPDATE publishing_dns_batches SET payload=? WHERE id=?`, raw, after.ID)
	return err
}
func (s *Service) CheckDNSBatch(ctx context.Context, subject, request string, batch DNSBatch, rollback bool) error {
	if s == nil {
		return ErrProductionUnavailable
	}
	plan, stages, err := s.repo.Get(ctx, batch.Descriptor.PlanID)
	if err != nil || !plan.TargetsPersisted || plan.Status == PlanCanceled || PublicationPlanSHA256(plan) != batch.Descriptor.PlanSHA256 || plan.TargetRevision != batch.Descriptor.TargetRevision || plan.ZoneID != batch.Descriptor.ZoneID {
		return ErrProductionTarget
	}
	if _, err = s.refreshStageAuthority(ctx, subject, request, plan, "publication_dns_child_current"); err != nil {
		return err
	}
	stage := findStage(stages, StageDNS)
	if stage == nil {
		return ErrStageNotFound
	}
	if !rollback {
		if plan.Status != PlanRunning || plan.CurrentStage != StageDNS || stage.Status != StageRunning {
			return ErrProductionTarget
		}
		return nil
	}
	if !stage.Status.Executed() {
		return ErrStageNotRollable
	}
	for _, later := range stages {
		if later.Kind != StageDNS && (later.Status == StageRunning || later.Status.Executed()) {
			return ErrStageNotRollable
		}
	}
	return nil
}

// RecoverDNSBatches retains intents/IDs but never calls a provider after restart.
func RecoverDNSBatches(ctx context.Context, db *storage.DB, sink AuditSink) error {
	if sink == nil {
		return ErrProductionUnavailable
	}
	rows, err := db.Query(ctx, `SELECT id FROM publishing_dns_batches`)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err = db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
			before, e := ReadDNSBatch(ctx, tx, id)
			if e != nil {
				return e
			}
			if before.State != "running" && before.State != "rolling_back" {
				return nil
			}
			after := before
			after.Observations = append([]DNSBatchObservation{}, before.Observations...)
			after.State = "unknown"
			after.UpdatedAt = time.Now().UTC()
			for i, r := range after.Observations {
				if r.State == "creating" || r.State == "deleting" {
					after.Observations[i].State = "unknown"
				}
			}
			if err := UpdateDNSBatchTx(ctx, tx, before, after); err != nil {
				return err
			}
			taskID := before.ApplyTaskID
			if before.State == "rolling_back" {
				taskID = before.RollbackTaskID
			}
			return sink.AppendTx(ctx, tx, audit.Event{SubjectID: "controller-recovery", Action: ActionStage, Result: "failure", RequestID: "recover-dns-" + before.ID, TaskID: taskID, Source: audit.SourceSystem, ReasonCode: "restarted_dns_effect_unknown", At: after.UpdatedAt})
		}); err != nil {
			return err
		}
	}
	return nil
}
