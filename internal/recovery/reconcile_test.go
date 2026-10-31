package recovery

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRecoveryPreservesRevocationsAndNeverReactivatesWork(t *testing.T) {
	now := time.Now()
	root := strings.Repeat("a", 64)
	snapshot := Snapshot{root, 2, 4, []string{"old-revoked"}, []TaskRecord{{"running", "running", now.Add(time.Minute)}, {"pending-expired", "waiting_node", now}, {"unknown", "unknown", time.Time{}}, {"pending", "queued", now.Add(time.Minute)}}}
	authority := Authority{root, 3, 7, []string{"new-revoked"}}
	plan, e := Reconcile(snapshot, authority, now)
	if e != nil {
		t.Fatal(e)
	}
	if !plan.Quarantine || !plan.InvalidateAllSessions || !plan.InvalidateAllApprovals || !slices.Contains(plan.RevokedSerials, "old-revoked") || !slices.Contains(plan.RevokedSerials, "new-revoked") {
		t.Fatal(plan)
	}
	if plan.Tasks[0].State != "unknown" || plan.Tasks[1].State != "expired" || plan.Tasks[2].State != "unknown" || plan.Tasks[3].State != "queued" {
		t.Fatal(plan.Tasks)
	}
	plan.Tasks[0].ID = "mutated"
	if snapshot.Tasks[0].ID != "running" {
		t.Fatal("aliased restored state")
	}
	for _, change := range []func(*Authority){func(a *Authority) { a.RootSPKISHA256 = strings.Repeat("b", 64) }, func(a *Authority) { a.TrustGeneration = 1 }, func(a *Authority) { a.RevocationSequence = 3 }} {
		bad := authority
		change(&bad)
		if _, e = Reconcile(snapshot, bad, now); e != ErrRecoveryRejected {
			t.Fatal("stale trust accepted", e)
		}
	}
}
