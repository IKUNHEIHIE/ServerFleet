package tasks

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/storage"
)

const IncidentObservationTTL = 15 * time.Minute
const IncidentManualEvidenceTTL = 24 * time.Hour

var ErrIncidentClosureBlocked = errors.New("tasks: current evidence or follow-up prevents incident closure")

type IncidentChecks struct {
	ActualStateChecked   bool   `json:"actualStateChecked"`
	ExecutionLogsChecked bool   `json:"executionLogsChecked"`
	ImpactChecked        bool   `json:"impactChecked"`
	UnknownAccepted      bool   `json:"unknownAccepted"`
	ResidualRisk         string `json:"residualRisk,omitempty"`
}
type IncidentTaskReceipt struct {
	TaskID           string         `json:"taskId"`
	Revision         uint64         `json:"revision"`
	State            string         `json:"state"`
	Action           string         `json:"action,omitempty"`
	PlanDigest       string         `json:"planDigest,omitempty"`
	TargetType       string         `json:"targetType,omitempty"`
	TargetResourceID string         `json:"targetResourceId,omitempty"`
	TargetDigest     string         `json:"targetDigest,omitempty"`
	TargetCount      int            `json:"targetCount"`
	Results          map[string]int `json:"results"`
	CreatedAt        time.Time      `json:"createdAt"`
	UpdatedAt        time.Time      `json:"updatedAt"`
	Terminal         bool           `json:"terminal"`
}

// Program-owned minimal facts, excluding names, params, output and model text.
func OperatorTaskReceipt(t Task) IncidentTaskReceipt {
	type target struct {
		ID    string
		State TargetState
		Code  NodeResultCode
	}
	targets := []target{}
	results := map[string]int{}
	for _, v := range t.Targets {
		targets = append(targets, target{v.NodeID, v.State, v.Code})
		results[string(v.State)]++
		if v.Code == NodeResultUnknown && v.State != TargetUnknown {
			results["unknown_result_code"]++
		}
	}
	return IncidentTaskReceipt{t.ID, t.Revision, string(t.Status), t.ActionID, t.PlanHash, t.TargetType, t.TargetResourceID, incidentDigest(targets), len(targets), results, t.CreatedAt, t.UpdatedAt, t.Status.Terminal()}
}

type IncidentFacts struct {
	Source             IncidentSource        `json:"source"`
	SourceTask         *IncidentTaskReceipt  `json:"sourceTask,omitempty"`
	Followups          []IncidentTaskReceipt `json:"followups"`
	ManualEvidenceIDs  []string              `json:"manualEvidenceIds"`
	EvidenceCategories []string              `json:"evidenceCategories"`
	EvidenceTimes      map[string]time.Time  `json:"evidenceTimes"`
	Cycle              uint64                `json:"cycle"`
	RestoreCount       uint64                `json:"restoreCount"`
	Digest             string                `json:"digest"`
}
type IncidentEvidence struct {
	ID           string         `json:"id"`
	IncidentID   string         `json:"incidentId"`
	Sequence     int64          `json:"sequence"`
	Kind         string         `json:"kind"`
	Category     string         `json:"category,omitempty"`
	Reference    string         `json:"reference,omitempty"`
	Note         string         `json:"note"`
	Actor        string         `json:"actor"`
	ObservedAt   time.Time      `json:"observedAt"`
	CreatedAt    time.Time      `json:"createdAt"`
	Cycle        uint64         `json:"cycle"`
	RestoreCount uint64         `json:"restoreCount"`
	Snapshot     *IncidentFacts `json:"snapshot,omitempty"`
}
type IncidentAssessment struct {
	Current           IncidentFacts `json:"current"`
	SourceChanged     bool          `json:"sourceChanged"`
	FactsChanged      bool          `json:"factsChanged"`
	ObservationDigest string        `json:"observationDigest,omitempty"`
	ObservedAt        *time.Time    `json:"observedAt,omitempty"`
	ObservationFresh  bool          `json:"observationFresh"`
	NeedsRecheck      bool          `json:"needsRecheck"`
	Blockers          []string      `json:"blockers"`
}

func IncidentEvidenceMigrations() []storage.Migration {
	return []storage.Migration{{Version: "cx/0023_incident_closure_evidence", Name: "append-only operator evidence and live reconciliation snapshots", Statements: []string{
		`CREATE TABLE task_incident_evidence(incident_id TEXT NOT NULL,sequence INTEGER NOT NULL,digest TEXT NOT NULL UNIQUE,kind TEXT NOT NULL,cycle INTEGER NOT NULL,payload BLOB NOT NULL CHECK(length(payload)<=65536),PRIMARY KEY(incident_id,sequence))`,
		`CREATE INDEX task_incident_evidence_cycle ON task_incident_evidence(incident_id,cycle,kind,sequence)`,
	}}}
}
func (s *IncidentService) now() time.Time {
	if s.o.Now != nil {
		return s.o.Now().UTC()
	}
	return time.Now().UTC()
}
func decodeIncidentEvidence(raw []byte) (IncidentEvidence, error) {
	var v IncidentEvidence
	if len(raw) > 64<<10 || json.Unmarshal(raw, &v) != nil || v.ID == "" || !validToken(v.IncidentID, 128) || v.Sequence < 1 {
		return v, ErrInvalidTask
	}
	digest := v.ID
	v.ID = ""
	if incidentDigest(v) != digest {
		return v, ErrInvalidTask
	}
	v.ID = digest
	if v.Snapshot != nil {
		facts := *v.Snapshot
		digest := facts.Digest
		facts.Digest = ""
		if digest != incidentDigest(facts) {
			return v, ErrInvalidTask
		}
	}
	return v, nil
}
func (s *IncidentService) evidence(ctx context.Context, q storage.Queryer, id, digest string) (IncidentEvidence, error) {
	var raw []byte
	e := q.QueryRow(ctx, `SELECT payload FROM task_incident_evidence WHERE incident_id=? AND digest=?`, id, digest).Scan(&raw)
	if e != nil {
		return IncidentEvidence{}, e
	}
	v, e := decodeIncidentEvidence(raw)
	if e != nil || v.ID != digest || v.IncidentID != id {
		return v, ErrInvalidTask
	}
	return v, nil
}
func (s *IncidentService) addEvidenceTx(ctx context.Context, tx storage.Tx, v IncidentEvidence) (IncidentEvidence, error) {
	if e := tx.QueryRow(ctx, `SELECT COALESCE(MAX(sequence),0)+1 FROM task_incident_evidence WHERE incident_id=?`, v.IncidentID).Scan(&v.Sequence); e != nil {
		return v, e
	}
	v.ID = ""
	v.ID = incidentDigest(v)
	raw, e := json.Marshal(v)
	if e != nil || len(raw) > 64<<10 {
		return v, ErrInvalidTask
	}
	_, e = tx.Exec(ctx, `INSERT INTO task_incident_evidence VALUES(?,?,?,?,?,?)`, v.IncidentID, v.Sequence, v.ID, v.Kind, v.Cycle, raw)
	return v, e
}
func (s *IncidentService) inspect(ctx context.Context, subject string, v Incident, q storage.Queryer) (IncidentFacts, error) {
	current, e := s.o.InspectSource(ctx, subject, v.Source)
	if e != nil {
		return IncidentFacts{}, e
	}
	facts := IncidentFacts{Source: current, Followups: []IncidentTaskReceipt{}, ManualEvidenceIDs: []string{}, EvidenceCategories: []string{}, EvidenceTimes: map[string]time.Time{}, Cycle: v.Cycle, RestoreCount: v.RestoreCount}
	if current.TaskID != "" {
		receipt, e := s.o.InspectTask(ctx, subject, current.TaskID)
		if e != nil {
			return facts, e
		}
		facts.SourceTask = &receipt
	}
	for _, id := range v.FollowupTaskIDs {
		receipt, e := s.o.InspectTask(ctx, subject, id)
		if e != nil {
			receipt = IncidentTaskReceipt{TaskID: id, State: "unavailable", Results: map[string]int{}}
		}
		facts.Followups = append(facts.Followups, receipt)
	}
	rows, e := q.Query(ctx, `SELECT payload FROM task_incident_evidence WHERE incident_id=? AND cycle=? AND kind='manual' ORDER BY sequence`, v.ID, v.Cycle)
	if e != nil {
		return facts, e
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if e = rows.Scan(&raw); e != nil {
			return facts, e
		}
		entry, e := decodeIncidentEvidence(raw)
		if e != nil {
			return facts, e
		}
		if entry.RestoreCount != v.RestoreCount || entry.ObservedAt.After(s.now()) {
			continue
		}
		facts.ManualEvidenceIDs = append(facts.ManualEvidenceIDs, entry.ID)
		if !slices.Contains(facts.EvidenceCategories, entry.Category) {
			facts.EvidenceCategories = append(facts.EvidenceCategories, entry.Category)
		}
		if entry.ObservedAt.After(facts.EvidenceTimes[entry.Category]) {
			facts.EvidenceTimes[entry.Category] = entry.ObservedAt
		}
	}
	if e = rows.Err(); e != nil {
		return facts, e
	}
	facts.Digest = incidentDigest(facts)
	return facts, nil
}
func (s *IncidentService) assessment(ctx context.Context, subject string, v Incident, q storage.Queryer) (IncidentAssessment, error) {
	facts, e := s.inspect(ctx, subject, v, q)
	if e != nil {
		return IncidentAssessment{}, e
	}
	a := IncidentAssessment{Current: facts, SourceChanged: incidentDigest(facts.Source) != v.SourceDigest, Blockers: []string{}}
	if v.ObservationDigest != "" {
		entry, e := s.evidence(ctx, q, v.ID, v.ObservationDigest)
		if e != nil {
			return a, e
		}
		if entry.Snapshot == nil {
			return a, ErrInvalidTask
		}
		a.ObservationDigest = entry.ID
		a.ObservedAt = &entry.CreatedAt
		a.ObservationFresh = entry.Cycle == v.Cycle && entry.RestoreCount == v.RestoreCount && !entry.CreatedAt.After(s.now()) && s.now().Sub(entry.CreatedAt) <= IncidentObservationTTL
		a.FactsChanged = entry.Snapshot.Digest != facts.Digest
	} else {
		a.FactsChanged = true
	}
	a.NeedsRecheck = (v.State == "resolved" || v.State == "review_required") && (a.FactsChanged || a.ObservationDigest == "")
	if !a.ObservationFresh {
		a.Blockers = append(a.Blockers, "fresh_reconciliation_required")
	}
	if a.FactsChanged {
		a.Blockers = append(a.Blockers, "facts_changed_reconcile_again")
	}
	for _, category := range []string{"actual_state", "execution_log", "impact_check"} {
		if !slices.Contains(facts.EvidenceCategories, category) || s.now().Sub(facts.EvidenceTimes[category]) > IncidentManualEvidenceTTL {
			a.Blockers = append(a.Blockers, "evidence_required_"+category)
		}
	}
	for _, task := range facts.Followups {
		if !task.Terminal {
			a.Blockers = append(a.Blockers, "followup_pending_or_unavailable:"+task.TaskID)
		}
	}
	return a, nil
}
func (s *IncidentService) checkClosure(ctx context.Context, subject string, v Incident, q storage.Queryer, outcome, digest string, checks *IncidentChecks) error {
	if !slices.Contains([]string{"confirmed_succeeded", "confirmed_failed", "confirmed_no_effect", "accepted_unresolved"}, outcome) {
		return ErrInvalidTask
	}
	a, e := s.assessment(ctx, subject, v, q)
	if e != nil {
		return e
	}
	if len(a.Blockers) > 0 || digest == "" || digest != a.ObservationDigest || checks == nil || !checks.ActualStateChecked || !checks.ExecutionLogsChecked || !checks.ImpactChecked {
		return ErrIncidentClosureBlocked
	}
	uncertain := false
	unsuccessful := false
	for _, task := range a.Current.Followups {
		unclear := !task.Terminal || task.State == "unknown" || task.Results["unknown"] > 0 || task.Results["pending"] > 0 || task.Results["dispatched"] > 0 || task.Results["unknown_result_code"] > 0
		uncertain = uncertain || unclear
		unsuccessful = unsuccessful || task.State != "completed" || unclear || task.Results["failed"] > 0 || task.Results["timed_out"] > 0
	}
	if outcome == "accepted_unresolved" {
		if !checks.UnknownAccepted || strings.TrimSpace(checks.ResidualRisk) == "" || !slices.Contains(a.Current.EvidenceCategories, "containment") || s.now().Sub(a.Current.EvidenceTimes["containment"]) > IncidentManualEvidenceTTL {
			return ErrIncidentClosureBlocked
		}
	} else if (outcome == "confirmed_failed" && uncertain) || (outcome != "confirmed_failed" && unsuccessful) || checks.UnknownAccepted || checks.ResidualRisk != "" {
		return ErrIncidentClosureBlocked
	}
	return nil
}
func (s *IncidentService) recordClosureEvidence(ctx context.Context, tx storage.Tx, subject string, v *Incident, in IncidentDecision) error {
	if v.AssigneeID != subject || v.State != "investigating" && v.State != "review_required" {
		return ErrIncidentConflict
	}
	entry := IncidentEvidence{IncidentID: v.ID, Actor: subject, CreatedAt: s.now(), ObservedAt: s.now(), Cycle: v.Cycle, RestoreCount: v.RestoreCount, Note: in.Note}
	if in.Action == "record_evidence" {
		if !slices.Contains([]string{"actual_state", "execution_log", "impact_check", "containment"}, in.EvidenceCategory) || in.ObservedAt == nil || in.ObservedAt.After(s.now()) || s.now().Sub(*in.ObservedAt) > IncidentManualEvidenceTTL || strings.TrimSpace(in.EvidenceReference) == "" || len(in.EvidenceReference) > 512 {
			return ErrInvalidTask
		}
		reference, e := s.o.Filter(in.EvidenceReference)
		if e != nil {
			return e
		}
		entry.Kind = "manual"
		entry.Category = in.EvidenceCategory
		entry.Reference = reference
		entry.ObservedAt = in.ObservedAt.UTC()
	} else if in.Action == "reconcile" {
		facts, e := s.inspect(ctx, subject, *v, tx)
		if e != nil {
			return e
		}
		entry.Kind = "snapshot"
		entry.Snapshot = &facts
	} else {
		return ErrInvalidTask
	}
	saved, e := s.addEvidenceTx(ctx, tx, entry)
	if e != nil {
		return e
	}
	v.Proposal = nil
	v.State = "investigating"
	v.ObservationDigest = ""
	if in.Action == "reconcile" {
		v.ObservationDigest = saved.ID
	}
	return nil
}
func (s *IncidentService) EvidencePage(ctx context.Context, subject, id string, after int64) ([]IncidentEvidence, int64, error) {
	v, e := s.Get(ctx, subject, id)
	if e != nil {
		return nil, 0, e
	}
	if after < 0 {
		return nil, 0, ErrInvalidTask
	}
	rows, e := s.o.DB.Query(ctx, `SELECT payload FROM task_incident_evidence WHERE incident_id=? AND sequence>? ORDER BY sequence LIMIT 51`, id, after)
	if e != nil {
		return nil, 0, e
	}
	out := []IncidentEvidence{}
	for rows.Next() {
		var raw []byte
		if e = rows.Scan(&raw); e != nil {
			rows.Close()
			return nil, 0, e
		}
		entry, e := decodeIncidentEvidence(raw)
		if e != nil {
			rows.Close()
			return nil, 0, e
		}
		out = append(out, entry)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, 0, e
	}
	next := int64(0)
	if len(out) > 50 {
		out = out[:50]
		next = out[len(out)-1].Sequence
	}
	visible := map[string]bool{}
	checked := map[string]bool{}
	sanitize := func(receipt IncidentTaskReceipt) IncidentTaskReceipt {
		if !checked[receipt.TaskID] {
			_, err := s.o.InspectTask(ctx, subject, receipt.TaskID)
			visible[receipt.TaskID] = err == nil
			checked[receipt.TaskID] = true
		}
		if !visible[receipt.TaskID] {
			return IncidentTaskReceipt{TaskID: receipt.TaskID, State: "unavailable", Results: map[string]int{}}
		}
		return receipt
	}
	for i := range out {
		out[i].Note, e = s.o.Filter(out[i].Note)
		if e != nil {
			return nil, 0, e
		}
		out[i].Reference, e = s.o.Filter(out[i].Reference)
		if e != nil {
			return nil, 0, e
		}
		if snapshot := out[i].Snapshot; snapshot != nil {
			if snapshot.SourceTask != nil {
				receipt := sanitize(*snapshot.SourceTask)
				snapshot.SourceTask = &receipt
			}
			for j, r := range snapshot.Followups {
				snapshot.Followups[j] = sanitize(r)
			}
		}
	}
	if s.gate(ctx, subject, &v.Source) != nil {
		return nil, 0, ErrInvalidTask
	}
	return out, next, nil
}

// Rotating bounded scan: no task submission, retry, approval or external I/O.
func (s *IncidentService) RecheckClosures(ctx context.Context) error {
	if !s.sweepMu.TryLock() {
		return nil
	}
	defer s.sweepMu.Unlock()
	if s.o.RuntimeCurrent(ctx) != nil {
		return ErrInvalidTask
	}
	rows, e := s.o.DB.Query(ctx, `SELECT id FROM task_incidents WHERE id>? AND json_extract(CAST(payload AS TEXT),'$.View.state') IN ('resolved','review_required') ORDER BY id LIMIT 100`, s.sweepCursor)
	if e != nil {
		return e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if len(ids) == 0 {
		s.sweepCursor = ""
		return nil
	}
	for _, id := range ids {
		s.sweepCursor = id
		r, e := s.getRecord(ctx, s.o.DB, id)
		if e != nil {
			return e
		}
		v := r.View
		actor := ""
		for _, candidate := range []string{v.ResolvedBy, v.AssigneeID, v.Source.OwnerID} {
			if s.gate(ctx, candidate, &v.Source) == nil {
				actor = candidate
				break
			}
		}
		if actor == "" {
			continue
		}
		a, e := s.assessment(ctx, actor, v, s.o.DB)
		if e != nil || !a.NeedsRecheck {
			continue
		}
		e = s.o.DB.InTx(ctx, func(c context.Context, tx storage.Tx) error {
			record, e := s.getRecord(c, tx, id)
			if e != nil {
				return e
			}
			if record.View.Revision != v.Revision {
				return nil
			}
			current, e := s.assessment(c, actor, record.View, tx)
			if e != nil || !current.NeedsRecheck {
				return e
			}
			updated := record.View
			if len(updated.Events) >= 200 {
				return nil
			}
			updated.State = "investigating"
			if s.o.Administrator(c, updated.AssigneeID) != nil {
				updated.AssigneeID = ""
				updated.State = "open"
			}
			updated.Proposal = nil
			updated.ResolvedBy = ""
			updated.ResolvedAt = nil
			updated.ObservationDigest = ""
			updated.Cycle++
			updated.Revision++
			updated.UpdatedAt = s.now()
			updated.Events = append(updated.Events, IncidentEvent{len(updated.Events) + 1, "recheck_required", "system", "Source or follow-up facts changed; previous closure is historical.", s.now()})
			record.View = updated
			raw, e := json.Marshal(record)
			if e != nil || len(raw) > 120<<10 {
				return ErrInvalidTask
			}
			if _, e = tx.Exec(c, `UPDATE task_incidents SET revision=?,payload=? WHERE id=? AND revision=?`, updated.Revision, raw, id, v.Revision); e != nil {
				return e
			}
			return s.o.Audit.AppendTx(c, tx, audit.Event{SubjectID: actor, Action: ActionIncidentManage, Result: "success", RequestID: "incident-recheck-" + incidentDigest([]any{id, updated.Revision})[:32], TaskID: v.Source.TaskID, Source: audit.SourceSystem, ReasonCode: "incident_facts_changed_no_execution", At: s.now()})
		})
		if e != nil {
			return e
		}
	}
	return nil
}

type IncidentListOptions struct {
	State, Kind, TaskID, Cursor string
	Limit                       int
}
type IncidentPage struct {
	Items      []Incident `json:"items"`
	NextCursor string     `json:"nextCursor,omitempty"`
}
type incidentCursor struct{ At, ID, Binding string }

func (s *IncidentService) ListPage(ctx context.Context, subject string, o IncidentListOptions) (IncidentPage, error) {
	out := IncidentPage{Items: []Incident{}}
	if s.gate(ctx, subject, nil) != nil {
		return out, ErrInvalidTask
	}
	if o.Limit == 0 {
		o.Limit = 50
	}
	if o.State == "" {
		o.State = "active"
	}
	if o.Limit < 1 || o.Limit > 100 || !slices.Contains([]string{"active", "all", "open", "investigating", "review_required", "resolved"}, o.State) || o.Kind != "" && o.Kind != "task" && o.Kind != "ai_run" || o.TaskID != "" && !validToken(o.TaskID, 128) {
		return out, ErrInvalidTask
	}
	binding := incidentDigest([]string{subject, o.State, o.Kind, o.TaskID})
	cursor := incidentCursor{At: "9999-12-31T23:59:59.999999999Z", ID: strings.Repeat("z", 128), Binding: binding}
	if len(o.Cursor) > 2048 {
		return out, ErrInvalidTask
	}
	if o.Cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(o.Cursor)
		if e != nil || len(raw) > 1024 || json.Unmarshal(raw, &cursor) != nil || cursor.Binding != binding || !validToken(cursor.ID, 128) {
			return out, ErrInvalidTask
		}
		if _, e = time.Parse(time.RFC3339Nano, cursor.At); e != nil {
			return out, ErrInvalidTask
		}
	}
	where := ""
	args := []any{cursor.At, cursor.At, cursor.ID}
	if o.State == "active" {
		where += " AND json_extract(CAST(payload AS TEXT),'$.View.state')<>'resolved'"
	} else if o.State != "all" {
		where += " AND json_extract(CAST(payload AS TEXT),'$.View.state')=?"
		args = append(args, o.State)
	}
	if o.Kind != "" {
		where += " AND source_kind=?"
		args = append(args, o.Kind)
	}
	if o.TaskID != "" {
		where += " AND json_extract(CAST(payload AS TEXT),'$.View.source.taskId')=?"
		args = append(args, o.TaskID)
	}
	rows, e := s.o.DB.Query(ctx, `SELECT id,created_at FROM task_incidents WHERE (created_at<? OR(created_at=? AND id<?))`+where+` ORDER BY created_at DESC,id DESC LIMIT 1001`, args...)
	if e != nil {
		return out, e
	}
	candidates := []incidentCursor{}
	for rows.Next() {
		var c incidentCursor
		if e = rows.Scan(&c.ID, &c.At); e != nil {
			rows.Close()
			return out, e
		}
		c.Binding = binding
		candidates = append(candidates, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	for i, c := range candidates {
		if i >= 1000 {
			break
		}
		v, e := s.Get(ctx, subject, c.ID)
		if e == nil {
			out.Items = append(out.Items, v)
		} else if ctx.Err() != nil {
			return out, ctx.Err()
		}
		if len(out.Items) == o.Limit || i == 999 {
			if i+1 < len(candidates) {
				raw, _ := json.Marshal(c)
				out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
			}
			break
		}
	}
	if s.gate(ctx, subject, nil) != nil {
		return IncidentPage{}, ErrInvalidTask
	}
	return out, nil
}
