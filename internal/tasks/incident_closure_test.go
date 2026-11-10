package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
	"strings"
	"testing"
	"time"
)

type closureFixture struct {
	s        *IncidentService
	v        Incident
	source   IncidentSource
	receipts map[string]IncidentTaskReceipt
	denied   map[string]bool
	admins   map[string]bool
	now      time.Time
	sequence int
	db       *storage.DB
}

func newClosureFixture(t *testing.T) *closureFixture {
	t.Helper()
	db := testkit.NewDB(t, IncidentMigrations(), IncidentEvidenceMigrations(), audit.Migrations())
	validator, e := audit.NewValidator([]string{ActionIncidentManage}, audit.DefaultLimits())
	if e != nil {
		t.Fatal(e)
	}
	var f *closureFixture
	recorder, e := audit.NewSQLRecorder(db, validator, func() time.Time {
		if f != nil {
			return f.now
		}
		return time.Now().UTC()
	})
	if e != nil {
		t.Fatal(e)
	}
	f = &closureFixture{db: db, now: time.Now().UTC(), receipts: map[string]IncidentTaskReceipt{}, denied: map[string]bool{}, admins: map[string]bool{"admin-a": true, "admin-b": true}}
	f.source = IncidentSource{Kind: "task", ID: "original", TaskID: "original", OwnerID: "admin-a", Revision: 3, State: "unknown", Action: "host.patch", NodeIDs: []string{"node-a"}, PlanDigest: strings.Repeat("a", 64)}
	f.receipts["original"] = IncidentTaskReceipt{TaskID: "original", Revision: 3, State: "unknown", Terminal: true, Results: map[string]int{"unknown": 1}, CreatedAt: f.now, UpdatedAt: f.now}
	f.s, e = NewIncidentService(IncidentOptions{DB: db, Audit: recorder, Now: func() time.Time { return f.now }, RuntimeCurrent: func(context.Context) error { return nil }, Administrator: func(_ context.Context, id string) error {
		if !f.admins[id] {
			return ErrInvalidTask
		}
		return nil
	}, ReadSource: func(_ context.Context, _ string, source IncidentSource) error {
		if f.denied[source.ID] {
			return ErrInvalidTask
		}
		return nil
	}, InspectSource: func(_ context.Context, _ string, source IncidentSource) (IncidentSource, error) {
		if source.ID == "original" {
			return f.source, nil
		}
		return source, nil
	}, InspectTask: func(_ context.Context, _ string, id string) (IncidentTaskReceipt, error) {
		v, ok := f.receipts[id]
		if !ok || f.denied[id] {
			return v, ErrInvalidTask
		}
		return v, nil
	}, ReadFollowup: func(_ context.Context, _ string, id string, _ time.Time) error {
		if _, ok := f.receipts[id]; !ok {
			return ErrInvalidTask
		}
		return nil
	}, ValidateFollowup: func(_ context.Context, _ string, _ IncidentSource, id string) error {
		if !strings.HasPrefix(id, "follow-") {
			return ErrInvalidTask
		}
		return nil
	}, Filter: func(v string) (string, error) { return v, nil }})
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.Observe(context.Background(), f.source); e != nil {
		t.Fatal(e)
	}
	items, e := f.s.List(context.Background(), "admin-a")
	if e != nil || len(items) != 1 {
		t.Fatal(items, e)
	}
	f.v = items[0]
	return f
}
func (f *closureFixture) input(action string) IncidentDecision {
	f.sequence++
	return IncidentDecision{ExpectedRevision: f.v.Revision, RequestID: fmt.Sprintf("request-%04d", f.sequence), Action: action, Note: "Synthetic operator evidence and independent review"}
}
func (f *closureFixture) apply(t *testing.T, actor string, in IncidentDecision, wantErr bool) {
	t.Helper()
	next, e := f.s.Decide(context.Background(), actor, f.v.ID, in)
	if wantErr {
		if e == nil {
			t.Fatal("unsafe transition accepted", in.Action)
		}
		return
	}
	if e != nil {
		t.Fatal(in.Action, e)
	}
	f.v = next
}
func (f *closureFixture) manual(t *testing.T, category string) {
	t.Helper()
	in := f.input("record_evidence")
	in.EvidenceCategory = category
	in.EvidenceReference = "synthetic-journal:100-120"
	observed := f.now
	in.ObservedAt = &observed
	f.apply(t, "admin-a", in, false)
}
func (f *closureFixture) packet(t *testing.T) {
	t.Helper()
	for _, category := range []string{"actual_state", "execution_log", "impact_check"} {
		f.manual(t, category)
	}
	f.apply(t, "admin-a", f.input("reconcile"), false)
}
func (f *closureFixture) proposal(outcome string) IncidentDecision {
	in := f.input("propose")
	in.Outcome = outcome
	in.ObservationDigest = f.v.ObservationDigest
	in.Checks = &IncidentChecks{ActualStateChecked: true, ExecutionLogsChecked: true, ImpactChecked: true}
	return in
}
func (f *closureFixture) approval() IncidentDecision {
	in := f.input("approve")
	if f.v.Proposal != nil {
		in.ObservationDigest = f.v.Proposal.ObservationDigest
	}
	in.ReviewAcknowledged = true
	return in
}
func (f *closureFixture) sweep(t *testing.T) {
	t.Helper()
	for range 2 {
		if e := f.s.RecheckClosures(context.Background()); e != nil {
			t.Fatal(e)
		}
	}
	v, e := f.s.Get(context.Background(), "admin-a", f.v.ID)
	if e != nil {
		t.Fatal(e)
	}
	f.v = v
}
func TestIncidentClosureRejectsPendingChangedAndUnknownFollowups(t *testing.T) {
	f := newClosureFixture(t)
	f.apply(t, "admin-a", f.input("claim"), false)
	f.apply(t, "admin-a", f.proposal("confirmed_succeeded"), true)
	f.receipts["follow-check"] = IncidentTaskReceipt{TaskID: "follow-check", State: "running", Revision: 1, CreatedAt: f.now.Add(time.Second), UpdatedAt: f.now, Results: map[string]int{"dispatched": 1}}
	link := f.input("link_task")
	link.FollowupTaskID = "follow-check"
	f.apply(t, "admin-a", link, false)
	f.packet(t)
	f.apply(t, "admin-a", f.proposal("confirmed_succeeded"), true)
	receipt := f.receipts["follow-check"]
	receipt.State = "completed"
	receipt.Terminal = true
	receipt.Revision++
	receipt.Results = map[string]int{"succeeded": 1}
	f.receipts[receipt.TaskID] = receipt
	f.apply(t, "admin-a", f.input("reconcile"), false)
	f.apply(t, "admin-a", f.proposal("confirmed_succeeded"), false)
	receipt.State = "unknown"
	receipt.Revision++
	receipt.Results = map[string]int{"unknown": 1}
	f.receipts[receipt.TaskID] = receipt
	f.apply(t, "admin-b", f.approval(), true)
	f.sweep(t)
	if f.v.State != "investigating" || f.v.Proposal != nil || f.v.ObservationDigest != "" {
		t.Fatal(f.v)
	}
	f.packet(t)
	f.manual(t, "containment")
	f.apply(t, "admin-a", f.input("reconcile"), false)
	f.apply(t, "admin-a", f.proposal("confirmed_succeeded"), true)
	f.apply(t, "admin-a", f.proposal("confirmed_failed"), true)
	accepted := f.proposal("accepted_unresolved")
	f.apply(t, "admin-a", accepted, true)
	accepted.Checks.UnknownAccepted = true
	accepted.Checks.ResidualRisk = "Exact remote effect remains unknown; resource fenced pending independent inspection"
	f.apply(t, "admin-a", accepted, false)
	f.apply(t, "admin-a", f.approval(), true)
	missingAck := f.approval()
	missingAck.ReviewAcknowledged = false
	f.apply(t, "admin-b", missingAck, true)
	f.apply(t, "admin-b", f.approval(), false)
	if f.v.State != "resolved" || f.v.ResolvedAt == nil || f.source.State != "unknown" || f.receipts["original"].State != "unknown" || f.receipts["follow-check"].State != "unknown" {
		t.Fatal(f.v)
	}
	f.now = f.now.Add(25 * time.Hour)
	f.sweep(t)
	if f.v.State != "resolved" || f.v.Assessment.NeedsRecheck {
		t.Fatal("mere evidence age reopened a historical closure")
	}
	receipt.State = "completed"
	receipt.Revision++
	receipt.Results = map[string]int{"succeeded": 1}
	f.receipts[receipt.TaskID] = receipt
	f.sweep(t)
	if f.v.State != "investigating" {
		t.Fatal("new follow-up facts did not reopen case", f.v)
	}
}
func TestIncidentClosureExpiryReplayHistoryAndRestore(t *testing.T) {
	f := newClosureFixture(t)
	f.apply(t, "admin-a", f.input("claim"), false)
	in := f.input("record_evidence")
	in.EvidenceCategory = "actual_state"
	in.EvidenceReference = "synthetic-resource-check"
	now := f.now
	in.ObservedAt = &now
	f.apply(t, "admin-a", in, false)
	revision := f.v.Revision
	f.apply(t, "admin-a", in, false)
	if f.v.Revision != revision {
		t.Fatal("evidence replay appended another record")
	}
	f.manual(t, "execution_log")
	f.manual(t, "impact_check")
	f.apply(t, "admin-a", f.input("reconcile"), false)
	f.apply(t, "admin-a", f.proposal("confirmed_no_effect"), false)
	f.now = f.now.Add(16 * time.Minute)
	f.apply(t, "admin-b", f.approval(), true)
	f.apply(t, "admin-a", f.input("reconcile"), false)
	f.apply(t, "admin-a", f.proposal("confirmed_no_effect"), false)
	f.apply(t, "admin-b", f.approval(), false)
	f.source.Revision++
	f.source.State = "failed"
	if e := f.s.Observe(context.Background(), f.source); e != nil {
		t.Fatal(e)
	}
	items, e := f.s.List(context.Background(), "admin-a")
	if e != nil || len(items) != 1 || items[0].Source.State != "unknown" || !items[0].Assessment.SourceChanged {
		t.Fatal("original history lost or duplicated", items, e)
	}
	before, _, e := f.s.EvidencePage(context.Background(), "admin-a", f.v.ID, 0)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.db.InTx(context.Background(), func(c context.Context, tx storage.Tx) error { return InvalidateRestoredIncidentsTx(c, tx, f.now) }); e != nil {
		t.Fatal(e)
	}
	f.v, e = f.s.Get(context.Background(), "admin-a", f.v.ID)
	if e != nil || f.v.ObservationDigest != "" || f.v.Proposal != nil || f.v.State != "open" {
		t.Fatal(f.v, e)
	}
	after, _, e := f.s.EvidencePage(context.Background(), "admin-a", f.v.ID, 0)
	if e != nil || len(after) != len(before) {
		t.Fatal("restoration removed evidence", e)
	}
}
func TestIncidentClosureKnownFailureAndTargetUnknownAreDistinct(t *testing.T) {
	f := newClosureFixture(t)
	f.apply(t, "admin-a", f.input("claim"), false)
	f.receipts["follow-failed"] = IncidentTaskReceipt{TaskID: "follow-failed", Revision: 1, State: "failed", Terminal: true, Results: map[string]int{"failed": 1}, CreatedAt: f.now.Add(time.Second), UpdatedAt: f.now}
	link := f.input("link_task")
	link.FollowupTaskID = "follow-failed"
	f.apply(t, "admin-a", link, false)
	f.packet(t)
	f.apply(t, "admin-a", f.proposal("confirmed_succeeded"), true)
	f.apply(t, "admin-a", f.proposal("confirmed_failed"), false)
	f.apply(t, "admin-b", f.approval(), false)
	receipt := OperatorTaskReceipt(Task{ID: "target-unknown", Name: "synthetic-private-name", Params: `{"private":"synthetic-private-param"}`, Status: StatusCompleted, Targets: []Target{{NodeID: "node-a", State: TargetSucceeded, Code: NodeResultUnknown, Message: "synthetic-private-output"}}})
	raw, _ := json.Marshal(receipt)
	if strings.Contains(string(raw), "synthetic-private") || receipt.Results["unknown_result_code"] != 1 {
		t.Fatal("private task text exposed or unknown result code missed")
	}
}
func TestIncidentClosurePagingAuthorityAndEvidenceIntegrity(t *testing.T) {
	f := newClosureFixture(t)
	ctx := context.Background()
	for i := 0; i < 12; i++ {
		source := IncidentSource{Kind: "ai_run", ID: fmt.Sprintf("run-%03d", i), OwnerID: "admin-a", Revision: 1, State: "unknown", SessionID: "synthetic-session", NodeIDs: []string{}}
		if e := f.s.Observe(ctx, source); e != nil {
			t.Fatal(e)
		}
	}
	seen := map[string]bool{}
	cursor := ""
	for {
		page, e := f.s.ListPage(ctx, "admin-a", IncidentListOptions{Kind: "ai_run", Limit: 3, Cursor: cursor})
		if e != nil {
			t.Fatal(e)
		}
		for _, v := range page.Items {
			if seen[v.ID] {
				t.Fatal("duplicate page item")
			}
			seen[v.ID] = true
		}
		if page.NextCursor == "" {
			break
		}
		if _, e = f.s.ListPage(ctx, "admin-b", IncidentListOptions{Kind: "ai_run", Limit: 3, Cursor: page.NextCursor}); e == nil {
			t.Fatal("cursor crossed identity")
		}
		cursor = page.NextCursor
	}
	if len(seen) != 12 {
		t.Fatal("history truncated", len(seen))
	}
	f.apply(t, "admin-a", f.input("claim"), false)
	f.packet(t)
	entries, _, e := f.s.EvidencePage(ctx, "admin-a", f.v.ID, 0)
	if e != nil || len(entries) != 4 {
		t.Fatal(entries, e)
	}
	entry := entries[3]
	entry.Note = "tampered"
	raw, _ := json.Marshal(entry)
	if _, e = f.db.Exec(ctx, `UPDATE task_incident_evidence SET payload=? WHERE digest=?`, raw, entry.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Get(ctx, "admin-a", f.v.ID); !errors.Is(e, ErrInvalidTask) {
		t.Fatal("corrupted evidence accepted", e)
	}
}
