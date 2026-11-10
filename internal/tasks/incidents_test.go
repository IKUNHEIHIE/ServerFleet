package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
	"testing"
	"time"
)

func TestIncidentIndependentDispositionPreservesUnknownAndRestore(t *testing.T) {
	ctx := context.Background()
	db := testkit.NewDB(t, IncidentMigrations(), IncidentEvidenceMigrations(), audit.Migrations())
	validator, e := audit.NewValidator([]string{ActionIncidentManage}, audit.DefaultLimits())
	if e != nil {
		t.Fatal(e)
	}
	recorder, e := audit.NewSQLRecorder(db, validator, time.Now)
	if e != nil {
		t.Fatal(e)
	}
	admins := map[string]bool{"admin-a": true, "admin-b": true}
	originalState := "unknown"
	visible := true
	linked := 0
	factTime := time.Now().UTC()
	service, e := NewIncidentService(IncidentOptions{InspectSource: func(_ context.Context, _ string, source IncidentSource) (IncidentSource, error) { return source, nil }, InspectTask: func(_ context.Context, _ string, id string) (IncidentTaskReceipt, error) {
		state := "unknown"
		if id == "fresh-task" {
			state = "completed"
		}
		return IncidentTaskReceipt{TaskID: id, State: state, Revision: 1, Terminal: true, CreatedAt: factTime, UpdatedAt: factTime, Results: map[string]int{}}, nil
	}, ValidateFollowup: func(context.Context, string, IncidentSource, string) error { return nil }, DB: db, Audit: recorder, Administrator: func(_ context.Context, id string) error {
		if !admins[id] {
			return ErrInvalidTask
		}
		return nil
	}, RuntimeCurrent: func(context.Context) error { return nil }, Filter: func(v string) (string, error) { return v, nil }, ReadSource: func(_ context.Context, _ string, s IncidentSource) error {
		if !visible || s.State != originalState {
			return ErrInvalidTask
		}
		return nil
	}, ReadFollowup: func(_ context.Context, _ string, id string, _ time.Time) error {
		if id != "fresh-task" {
			return ErrInvalidTask
		}
		linked++
		return nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	source := IncidentSource{Kind: "task", ID: "original-task", OwnerID: "admin-a", Revision: 3, State: originalState, TaskID: "original-task", Action: "backups.upload", NodeIDs: []string{}}
	for range 2 {
		if e = service.Observe(ctx, source); e != nil {
			t.Fatal(e)
		}
	}
	items, e := service.List(ctx, "admin-a")
	if e != nil || len(items) != 1 {
		t.Fatal(items, e)
	}
	v := items[0]
	decide := func(actor, action, request, outcome, followup string, wantErr bool) {
		t.Helper()
		input := IncidentDecision{ExpectedRevision: v.Revision, RequestID: request, Action: action, Note: "Synthetic independent evidence reference", Outcome: outcome, FollowupTaskID: followup}
		if action == "propose" {
			input.ObservationDigest = v.ObservationDigest
			input.Checks = &IncidentChecks{ActualStateChecked: true, ExecutionLogsChecked: true, ImpactChecked: true}
		}
		if action == "approve" && v.Proposal != nil {
			input.ObservationDigest = v.Proposal.ObservationDigest
			input.ReviewAcknowledged = true
		}
		next, err := service.Decide(ctx, actor, v.ID, input)
		if wantErr {
			if err == nil {
				t.Fatal("unsafe action accepted", action)
			}
			return
		}
		if err != nil {
			t.Fatal(action, err)
		}
		v = next
	}
	decide("admin-a", "claim", "claim", "", "", false)
	claimed := v
	decide("admin-a", "link_task", "link", "", "fresh-task", false)
	for _, category := range []string{"actual_state", "execution_log", "impact_check"} {
		now := time.Now().UTC()
		next, err := service.Decide(ctx, "admin-a", v.ID, IncidentDecision{ExpectedRevision: v.Revision, RequestID: "evidence-" + category, Action: "record_evidence", Note: "Synthetic documented observation", EvidenceCategory: category, EvidenceReference: "synthetic-log-reference", ObservedAt: &now})
		if err != nil {
			t.Fatal(err)
		}
		v = next
	}
	decide("admin-a", "reconcile", "reconcile", "", "", false)
	decide("admin-a", "propose", "propose", "confirmed_no_effect", "", false)
	decide("admin-a", "approve", "self-approve", "", "", true)
	admins["admin-a"] = false
	decide("admin-b", "approve", "revoked-proposer", "", "", true)
	admins["admin-a"] = true
	decide("admin-b", "approve", "review", "", "", false)
	if v.State != "resolved" || v.ResolvedBy != "admin-b" || originalState != "unknown" || linked != 2 {
		t.Fatal(v, originalState, linked)
	}
	if _, e = service.Decide(ctx, "admin-a", v.ID, IncidentDecision{ExpectedRevision: claimed.Revision, RequestID: "stale", Action: "note", Note: "stale"}); !errors.Is(e, ErrIncidentConflict) {
		t.Fatal("CAS failed", e)
	}
	visible = false
	if _, e = service.Get(ctx, "admin-b", v.ID); e == nil {
		t.Fatal("read authority ignored")
	}
	visible = true
	if e = db.InTx(ctx, func(c context.Context, tx storage.Tx) error { return InvalidateRestoredIncidentsTx(c, tx, time.Now()) }); e != nil {
		t.Fatal(e)
	}
	restored, e := service.Get(ctx, "admin-a", v.ID)
	if e != nil || restored.State != "open" || restored.Proposal != nil || restored.AssigneeID != "" || len(restored.Events) <= len(v.Events) {
		t.Fatal(restored, e)
	}
	v = restored
	decide("admin-a", "claim", "claim-after-restore", "", "", false)
	decide("admin-b", "take_over", "takeover", "", "", false)
	decide("admin-b", "release", "release", "", "", false)
	if v.State != "open" || originalState != "unknown" {
		t.Fatal(v)
	}
}

func TestIncidentFullJournalCannotBlockOfflineRecovery(t *testing.T) {
	ctx := context.Background()
	db := testkit.NewDB(t, IncidentMigrations())
	source := IncidentSource{Kind: "task", ID: "original", OwnerID: "admin", Revision: 1, State: "unknown", NodeIDs: []string{}}
	record := incidentRecord{View: Incident{ID: "full-case", Revision: 200, Source: source, SourceDigest: incidentDigest(source), State: "resolved", ResolvedBy: "old-admin", Events: []IncidentEvent{}}, Requests: map[string]string{}}
	for i := 1; i <= 200; i++ {
		record.View.Events = append(record.View.Events, IncidentEvent{Sequence: i, Action: "note", Actor: "admin", Note: "Preserved synthetic evidence", At: time.Now().UTC()})
	}
	raw, e := json.Marshal(record)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, `INSERT INTO task_incidents VALUES(?,?,?,?,?,?,?,?)`, record.View.ID, record.View.SourceDigest, record.View.Revision, raw, storage.EncodeTime(time.Now()), source.Kind, source.ID, source.Revision); e != nil {
		t.Fatal(e)
	}
	for range 2 {
		if e = db.InTx(ctx, func(c context.Context, tx storage.Tx) error {
			return InvalidateRestoredIncidentsTx(c, tx, time.Now().UTC())
		}); e != nil {
			t.Fatal(e)
		}
	}
	if e = db.QueryRow(ctx, `SELECT payload FROM task_incidents WHERE id=?`, record.View.ID).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	restored, e := decodeIncident(raw)
	if e != nil || restored.View.State != "open" || restored.View.RestoreCount != 2 || len(restored.View.Events) != 200 || restored.View.Events[199].Note != "Preserved synthetic evidence" {
		t.Fatal(restored.View, e)
	}
}
