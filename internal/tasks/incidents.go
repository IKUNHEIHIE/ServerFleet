package tasks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/storage"
)

var ErrIncidentConflict = errors.New("tasks: incident revision or source conflict")

const ActionIncidentManage = "operations.incidents.manage"

func IncidentMigrations() []storage.Migration {
	return []storage.Migration{{Version: "cx/0022_operator_incidents", Name: "immutable anomaly sources and audited human dispositions", Statements: []string{
		`CREATE TABLE task_incidents(id TEXT PRIMARY KEY,source_key TEXT NOT NULL UNIQUE,revision INTEGER NOT NULL,payload BLOB NOT NULL CHECK(length(payload)<=131072),created_at TEXT NOT NULL,source_kind TEXT NOT NULL,source_id TEXT NOT NULL,source_revision INTEGER NOT NULL)`,
		`CREATE INDEX task_incidents_created ON task_incidents(created_at,id)`,
		`CREATE INDEX task_incidents_source ON task_incidents(source_kind,source_id,source_revision)`,
	}}}
}

type IncidentSource struct {
	Kind       string   `json:"kind"`
	ID         string   `json:"id"`
	OwnerID    string   `json:"ownerId"`
	Revision   uint64   `json:"revision"`
	State      string   `json:"state"`
	TaskID     string   `json:"taskId,omitempty"`
	SessionID  string   `json:"sessionId,omitempty"`
	Action     string   `json:"action,omitempty"`
	PlanDigest string   `json:"planDigest,omitempty"`
	NodeIDs    []string `json:"nodeIds"`
	Reason     string   `json:"reason,omitempty"`
}
type IncidentEvent struct {
	Sequence int       `json:"sequence"`
	Action   string    `json:"action"`
	Actor    string    `json:"actor"`
	Note     string    `json:"note,omitempty"`
	At       time.Time `json:"at"`
}
type IncidentProposal struct {
	ObservationDigest string          `json:"observationDigest"`
	Checks            *IncidentChecks `json:"checks,omitempty"`
	Outcome           string          `json:"outcome"`
	Evidence          string          `json:"evidence"`
	ProposedBy        string          `json:"proposedBy"`
	SourceDigest      string          `json:"sourceDigest"`
	At                time.Time       `json:"at"`
}
type Incident struct {
	Cycle             uint64              `json:"cycle"`
	ObservationDigest string              `json:"observationDigest,omitempty"`
	ResolvedAt        *time.Time          `json:"resolvedAt,omitempty"`
	Assessment        *IncidentAssessment `json:"assessment,omitempty"`
	ID                string              `json:"id"`
	Revision          uint64              `json:"revision"`
	Source            IncidentSource      `json:"source"`
	SourceDigest      string              `json:"sourceDigest"`
	State             string              `json:"state"`
	AssigneeID        string              `json:"assigneeId,omitempty"`
	CreatedAt         time.Time           `json:"createdAt"`
	UpdatedAt         time.Time           `json:"updatedAt"`
	Proposal          *IncidentProposal   `json:"proposal,omitempty"`
	ResolvedBy        string              `json:"resolvedBy,omitempty"`
	FollowupTaskIDs   []string            `json:"followupTaskIds"`
	Events            []IncidentEvent     `json:"events"`
	RestoreCount      uint64              `json:"restoreCount,omitempty"`
	LastRestoredAt    *time.Time          `json:"lastRestoredAt,omitempty"`
}

// request bindings are persisted separately from the public JSON view.
type incidentRecord struct {
	View     Incident
	Requests map[string]string
}
type IncidentOptions struct {
	InspectSource    func(context.Context, string, IncidentSource) (IncidentSource, error)
	InspectTask      func(context.Context, string, string) (IncidentTaskReceipt, error)
	ValidateFollowup func(context.Context, string, IncidentSource, string) error
	Now              func() time.Time
	DB               *storage.DB
	Audit            AuditSink
	Administrator    func(context.Context, string) error
	ReadSource       func(context.Context, string, IncidentSource) error
	ReadFollowup     func(context.Context, string, string, time.Time) error
	RuntimeCurrent   func(context.Context) error
	Filter           func(string) (string, error)
}
type IncidentService struct {
	o           IncidentOptions
	sweepMu     sync.Mutex
	sweepCursor string
}

func NewIncidentService(o IncidentOptions) (*IncidentService, error) {
	if o.DB == nil || o.Audit == nil || o.Administrator == nil || o.ReadSource == nil || o.ReadFollowup == nil || o.RuntimeCurrent == nil || o.Filter == nil || o.InspectSource == nil || o.InspectTask == nil || o.ValidateFollowup == nil {
		return nil, ErrInvalidTask
	}
	return &IncidentService{o: o}, nil
}
func incidentDigest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func incidentAnomaly(state string) bool {
	return slices.Contains([]string{"unknown", "failed", "partial_failure", "partially_failed", "expired", "limit_reached"}, state)
}
func validIncidentSource(s IncidentSource) bool {
	return (s.Kind == "task" || s.Kind == "ai_run") && validToken(s.ID, 128) && validToken(s.OwnerID, 128) && s.Revision > 0 && incidentAnomaly(s.State) && len(s.NodeIDs) <= 1000 && len(s.Reason) <= 256
}

// Observe is an inert program-owned monitor port. It records facts, never
// authorizes/retries tasks, assigns a human decision or changes original results.
func (s *IncidentService) Observe(ctx context.Context, source IncidentSource) error {
	if !validIncidentSource(source) || s.o.RuntimeCurrent(ctx) != nil {
		return ErrInvalidTask
	}
	digest := incidentDigest(source)
	id := "inc_" + digest[:40]
	now := s.now()
	return s.o.DB.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		var existing string
		e := tx.QueryRow(txCtx, `SELECT id FROM task_incidents WHERE source_kind=? AND source_id=? LIMIT 1`, source.Kind, source.ID).Scan(&existing)
		if e == nil {
			return nil
		}
		if !storage.IsNotFound(e) {
			return e
		}
		v := Incident{Cycle: 1, ID: id, Revision: 1, Source: source, SourceDigest: digest, State: "open", CreatedAt: now, UpdatedAt: now, FollowupTaskIDs: []string{}, Events: []IncidentEvent{{1, "observed", "system", "", now}}}
		raw, _ := json.Marshal(incidentRecord{View: v, Requests: map[string]string{}})
		if _, e = tx.Exec(txCtx, `INSERT INTO task_incidents VALUES(?,?,?,?,?,?,?,?)`, id, digest, 1, raw, storage.EncodeTime(now), source.Kind, source.ID, source.Revision); e != nil {
			return e
		}
		return s.o.Audit.AppendTx(txCtx, tx, audit.Event{SubjectID: source.OwnerID, Action: ActionIncidentManage, Result: "success", RequestID: id, TaskID: source.TaskID, Source: audit.SourceSystem, ReasonCode: "anomaly_observed_no_execution", At: now})
	})
}
func (s *IncidentService) gate(ctx context.Context, subject string, source *IncidentSource) error {
	if ctx == nil || ctx.Err() != nil || s.o.RuntimeCurrent(ctx) != nil || s.o.Administrator(ctx, subject) != nil {
		return ErrInvalidTask
	}
	if source != nil && s.o.ReadSource(ctx, subject, *source) != nil {
		return ErrInvalidTask
	}
	return nil
}
func decodeIncident(raw []byte) (incidentRecord, error) {
	var r incidentRecord
	if len(raw) > 128<<10 || json.Unmarshal(raw, &r) != nil || !validToken(r.View.ID, 128) || r.View.Revision == 0 || !validIncidentSource(r.View.Source) || r.View.SourceDigest != incidentDigest(r.View.Source) || len(r.View.Events) > 200 || len(r.Requests) > 200 {
		return r, ErrInvalidTask
	}
	return r, nil
}
func (s *IncidentService) getRecord(ctx context.Context, q storage.Queryer, id string) (incidentRecord, error) {
	if !validToken(id, 128) {
		return incidentRecord{}, ErrInvalidTask
	}
	var raw []byte
	if e := q.QueryRow(ctx, `SELECT payload FROM task_incidents WHERE id=?`, id).Scan(&raw); e != nil {
		return incidentRecord{}, e
	}
	return decodeIncident(raw)
}
func (s *IncidentService) Get(ctx context.Context, subject, id string) (Incident, error) {
	if e := s.gate(ctx, subject, nil); e != nil {
		return Incident{}, e
	}
	r, e := s.getRecord(ctx, s.o.DB, id)
	if e != nil {
		return Incident{}, e
	}
	if e = s.gate(ctx, subject, &r.View.Source); e != nil {
		return Incident{}, e
	}
	v := r.View
	for i := range v.Events {
		if v.Events[i].Note != "" {
			v.Events[i].Note, e = s.o.Filter(v.Events[i].Note)
			if e != nil {
				return Incident{}, e
			}
		}
	}
	if v.Proposal != nil {
		v.Proposal.Evidence, e = s.o.Filter(v.Proposal.Evidence)
		if e != nil {
			return Incident{}, e
		}
		if v.Proposal.Checks != nil {
			checks := *v.Proposal.Checks
			checks.ResidualRisk, e = s.o.Filter(checks.ResidualRisk)
			if e != nil {
				return Incident{}, e
			}
			v.Proposal.Checks = &checks
		}
	}
	a, e := s.assessment(ctx, subject, v, s.o.DB)
	if e != nil {
		return Incident{}, e
	}
	v.Assessment = &a
	if s.gate(ctx, subject, &v.Source) != nil {
		return Incident{}, ErrInvalidTask
	}
	return v, nil
}
func (s *IncidentService) List(ctx context.Context, subject string) ([]Incident, error) {
	if e := s.gate(ctx, subject, nil); e != nil {
		return nil, e
	}
	rows, e := s.o.DB.Query(ctx, `SELECT id FROM task_incidents ORDER BY (json_extract(CAST(payload AS TEXT),'$.View.state')='resolved') ASC,created_at DESC,id DESC LIMIT 1000`)
	if e != nil {
		return nil, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []Incident{}
	for _, id := range ids {
		v, e := s.Get(ctx, subject, id)
		if e == nil {
			out = append(out, v)
		} else if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return out, nil
}

type IncidentDecision struct {
	ReviewAcknowledged bool            `json:"reviewAcknowledged,omitempty"`
	ObservationDigest  string          `json:"observationDigest,omitempty"`
	Checks             *IncidentChecks `json:"checks,omitempty"`
	EvidenceCategory   string          `json:"evidenceCategory,omitempty"`
	EvidenceReference  string          `json:"evidenceReference,omitempty"`
	ObservedAt         *time.Time      `json:"observedAt,omitempty"`
	ExpectedRevision   uint64          `json:"expectedRevision"`
	RequestID          string          `json:"requestId"`
	Action             string          `json:"action"`
	Note               string          `json:"note"`
	Outcome            string          `json:"outcome,omitempty"`
	FollowupTaskID     string          `json:"followupTaskId,omitempty"`
}

func (s *IncidentService) Decide(ctx context.Context, subject, id string, in IncidentDecision) (Incident, error) {
	if !validToken(in.RequestID, 128) || in.ExpectedRevision == 0 || len(in.Note) > 4096 || !utf8.ValidString(in.Note) || strings.TrimSpace(in.Note) == "" {
		return Incident{}, ErrInvalidTask
	}
	note, e := s.o.Filter(in.Note)
	if e != nil {
		return Incident{}, e
	}
	in.Note = note
	if in.Action != "record_evidence" && (in.EvidenceCategory != "" || in.EvidenceReference != "" || in.ObservedAt != nil) {
		return Incident{}, ErrInvalidTask
	}
	if in.Action != "approve" && in.ReviewAcknowledged {
		return Incident{}, ErrInvalidTask
	}
	if in.Action != "propose" && in.Outcome != "" || in.Action != "link_task" && in.FollowupTaskID != "" {
		return Incident{}, ErrInvalidTask
	}
	if in.Action != "propose" && in.Checks != nil {
		return Incident{}, ErrInvalidTask
	}
	if in.Action != "propose" && in.Action != "approve" && in.ObservationDigest != "" {
		return Incident{}, ErrInvalidTask
	}
	if in.Checks != nil {
		checks := *in.Checks
		if len(checks.ResidualRisk) > 2048 || !utf8.ValidString(checks.ResidualRisk) {
			return Incident{}, ErrInvalidTask
		}
		checks.ResidualRisk, e = s.o.Filter(checks.ResidualRisk)
		if e != nil {
			return Incident{}, e
		}
		in.Checks = &checks
	}
	before, e := s.Get(ctx, subject, id)
	if e != nil {
		return Incident{}, e
	}
	if e = s.gate(ctx, subject, &before.Source); e != nil {
		return Incident{}, e
	}
	if in.Action == "link_task" && (s.o.ReadFollowup(ctx, subject, in.FollowupTaskID, before.CreatedAt) != nil || s.o.ValidateFollowup(ctx, subject, before.Source, in.FollowupTaskID) != nil) {
		return Incident{}, ErrInvalidTask
	}
	requestDigest := incidentDigest(struct {
		Actor string
		Input IncidentDecision
	}{subject, in})
	var out Incident
	e = s.o.DB.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		r, e := s.getRecord(txCtx, tx, id)
		if e != nil {
			return e
		}
		v := r.View
		if r.Requests == nil {
			r.Requests = map[string]string{}
		}
		if s.gate(txCtx, subject, &v.Source) != nil {
			return ErrInvalidTask
		}
		if old := r.Requests[in.RequestID]; old != "" {
			if old != requestDigest {
				return ErrIncidentConflict
			}
			out = v
			return nil
		}
		if v.Revision != in.ExpectedRevision || v.SourceDigest != before.SourceDigest || len(v.Events) >= 200 || len(r.Requests) >= 200 {
			return ErrIncidentConflict
		}
		switch in.Action {
		case "record_evidence", "reconcile":
			if e = s.recordClosureEvidence(txCtx, tx, subject, &v, in); e != nil {
				return e
			}
		case "claim":
			if v.State != "open" {
				return ErrIncidentConflict
			}
			v.State = "investigating"
			v.AssigneeID = subject
		case "release":
			if v.State != "investigating" || v.AssigneeID != subject {
				return ErrIncidentConflict
			}
			v.State = "open"
			v.AssigneeID = ""
			v.ObservationDigest = ""
			v.Cycle++
		case "take_over":
			if v.State != "investigating" && v.State != "review_required" {
				return ErrIncidentConflict
			}
			v.State = "investigating"
			v.AssigneeID = subject
			v.Proposal = nil
			v.ObservationDigest = ""
			v.Cycle++
		case "note":
			if v.State == "resolved" {
				return ErrIncidentConflict
			}
		case "propose":
			if v.State != "investigating" || v.AssigneeID != subject || !slices.Contains([]string{"confirmed_succeeded", "confirmed_failed", "confirmed_no_effect", "accepted_unresolved"}, in.Outcome) {
				return ErrIncidentConflict
			}
			if e = s.checkClosure(txCtx, subject, v, tx, in.Outcome, in.ObservationDigest, in.Checks); e != nil {
				return e
			}
			v.State = "review_required"
			v.Proposal = &IncidentProposal{Outcome: in.Outcome, Evidence: in.Note, ProposedBy: subject, SourceDigest: v.SourceDigest, At: s.now(), ObservationDigest: in.ObservationDigest, Checks: in.Checks}
		case "approve":
			if v.State != "review_required" || v.Proposal == nil || v.Proposal.ProposedBy == subject || v.Proposal.SourceDigest != v.SourceDigest {
				return ErrIncidentConflict
			}
			if s.o.Administrator(txCtx, v.Proposal.ProposedBy) != nil {
				return ErrIncidentConflict
			}
			if !in.ReviewAcknowledged || in.ObservationDigest != v.Proposal.ObservationDigest {
				return ErrIncidentClosureBlocked
			}
			if e = s.checkClosure(txCtx, subject, v, tx, v.Proposal.Outcome, v.Proposal.ObservationDigest, v.Proposal.Checks); e != nil {
				return e
			}
			v.State = "resolved"
			v.ResolvedBy = subject
			now := s.now()
			v.ResolvedAt = &now
		case "reject":
			if v.State != "review_required" || v.Proposal == nil || v.Proposal.ProposedBy == subject {
				return ErrIncidentConflict
			}
			v.State = "investigating"
			v.Proposal = nil
		case "reopen":
			if v.State != "resolved" {
				return ErrIncidentConflict
			}
			v.State = "open"
			v.ResolvedBy = ""
			v.AssigneeID = ""
			v.ResolvedAt = nil
			v.ObservationDigest = ""
			v.Cycle++
			v.Proposal = nil
		case "link_task":
			if v.State != "investigating" || v.AssigneeID != subject || !validToken(in.FollowupTaskID, 128) || in.FollowupTaskID == v.Source.TaskID || in.FollowupTaskID == v.Source.ID || len(v.FollowupTaskIDs) >= 20 || slices.Contains(v.FollowupTaskIDs, in.FollowupTaskID) {
				return ErrIncidentConflict
			}
			if s.o.ReadFollowup(txCtx, subject, in.FollowupTaskID, v.CreatedAt) != nil || s.o.ValidateFollowup(txCtx, subject, v.Source, in.FollowupTaskID) != nil {
				return ErrInvalidTask
			}
			v.FollowupTaskIDs = append(v.FollowupTaskIDs, in.FollowupTaskID)
			v.ObservationDigest = ""
		default:
			return ErrInvalidTask
		}
		v.Revision++
		v.UpdatedAt = s.now()
		v.Events = append(v.Events, IncidentEvent{len(v.Events) + 1, in.Action, subject, in.Note, v.UpdatedAt})
		r.View = v
		r.Requests[in.RequestID] = requestDigest
		raw, e := json.Marshal(r)
		if e != nil || len(raw) > 120<<10 {
			return ErrInvalidTask
		}
		result, e := tx.Exec(txCtx, `UPDATE task_incidents SET revision=?,payload=? WHERE id=? AND revision=?`, v.Revision, raw, id, in.ExpectedRevision)
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e != nil || n != 1 {
			return ErrIncidentConflict
		}
		if e = s.o.Audit.AppendTx(txCtx, tx, audit.Event{SubjectID: subject, Action: ActionIncidentManage, Result: "success", RequestID: in.RequestID, TaskID: v.Source.TaskID, NodeIDs: append([]string{}, v.Source.NodeIDs...), Source: audit.SourceWeb, ReasonCode: "manual_" + in.Action + "_original_result_preserved", At: v.UpdatedAt}); e != nil {
			return e
		}
		out = v
		return nil
	})
	if e != nil {
		return Incident{}, e
	}
	if e = s.gate(ctx, subject, &out.Source); e != nil {
		return Incident{}, e
	}
	return s.Get(ctx, subject, id)
}

func (r *Repo) IncidentSources(ctx context.Context) ([]IncidentSource, error) {
	rows, e := r.db.Query(ctx, `SELECT t.id FROM tasks t WHERE t.status IN ('unknown','failed','partial_failure','expired') AND NOT EXISTS(SELECT 1 FROM task_incidents i WHERE i.source_kind='task' AND i.source_id=t.id ) ORDER BY t.updated_at DESC,t.id DESC LIMIT 1000`)
	if e != nil {
		return nil, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []IncidentSource{}
	for _, id := range ids {
		t, e := r.Get(ctx, id)
		if e != nil {
			return nil, e
		}
		if incidentAnomaly(string(t.Status)) {
			out = append(out, IncidentSource{Kind: "task", ID: t.ID, OwnerID: t.SubmitterID, Revision: t.Revision, State: string(t.Status), TaskID: t.ID, Action: t.ActionID, PlanDigest: t.PlanHash, NodeIDs: taskTargetIDs(t), Reason: t.FailureReason})
		}
	}
	return out, nil
}
func InvalidateRestoredIncidentsTx(ctx context.Context, tx storage.Tx, now time.Time) error {
	rows, e := tx.Query(ctx, `SELECT id,payload FROM task_incidents`)
	if e != nil {
		return e
	}
	records := []incidentRecord{}
	for rows.Next() {
		var id string
		var raw []byte
		if e = rows.Scan(&id, &raw); e != nil {
			rows.Close()
			return e
		}
		r, e := decodeIncident(raw)
		if e != nil {
			rows.Close()
			return e
		}
		records = append(records, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, r := range records {
		v := &r.View
		v.State = "open"
		v.AssigneeID = ""
		v.Proposal = nil
		v.ResolvedBy = ""
		v.Revision++
		v.UpdatedAt = now
		v.FollowupTaskIDs = []string{}
		v.ResolvedAt = nil
		v.ObservationDigest = ""
		v.Cycle++
		// A full human journal must never make disaster recovery impossible.
		// Preserve all evidence; the dedicated monotonic restore marker and the
		// existing reconciliation journal record every invalidation regardless.
		v.RestoreCount++
		v.LastRestoredAt = &now
		if len(v.Events) < 200 {
			v.Events = append(v.Events, IncidentEvent{len(v.Events) + 1, "restore_invalidated", "system", "Old human decisions are historical; fresh review required.", now})
		}
		r.Requests = map[string]string{}
		raw, e := json.Marshal(r)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE task_incidents SET revision=?,payload=? WHERE id=?`, v.Revision, raw, v.ID); e != nil {
			return e
		}
	}
	return nil
}
