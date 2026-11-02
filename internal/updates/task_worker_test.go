package updates

import (
	"context"
	"encoding/json"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
	"serverfleet.local/serverfleet/internal/tasks"
	"strings"
	"testing"
	"time"
)

type installAuthorizer struct{}

func (installAuthorizer) Authorize(context.Context, auth.Check) error { return nil }

type installNodes struct{}

func (installNodes) Online(context.Context, string) (bool, error) { return true, nil }

func TestInstallWorkerRealTaskApprovalAtomicJournalAndUnknownFence(t *testing.T) {
	for _, failAudit := range []bool{false, true} {
		t.Run(map[bool]string{false: "installed", true: "audit_failure_unknown"}[failAudit], func(t *testing.T) {
			ctx := context.Background()
			f := installTestFixture(t)
			// Explicit service/health/admin/availability doubles; staging, filesystem,
			// sole tasks service, approval, SQLite journal/audit and restart are REAL.
			db := testkit.NewDB(t, tasks.Migrations(), audit.Migrations(), []storage.Migration{{Version: "test/9999_update_journal", Name: "test-only unregistered candidate", Statements: InstallJournalSchemaStatements()}})
			validator, _ := audit.NewValidator(append(tasks.ServiceActions(), ActionInstall), audit.DefaultLimits())
			sink, _ := audit.NewSQLRecorder(db, validator, time.Now)
			repo, _ := tasks.NewRepo(db)
			var service *tasks.Service
			var frozen tasks.Task
			journal, _ := NewSQLInstallJournal(db, sink, func(ctx context.Context, id, digest, subject string) error {
				current, e := service.CheckExecution(ctx, id, digest)
				if e != nil || current.SubmitterID != subject {
					return ErrArtifactRejected
				}
				return nil
			})
			calls := 0
			worker, err := NewTaskInstallWorker(TaskInstallWorkerOptions{Installer: f.s, Current: func(ctx context.Context, id, plan string) (tasks.Task, error) {
				return service.CheckExecution(ctx, id, plan)
			}, Administrator: func(context.Context, string) error { return nil }, BackupCurrent: func(context.Context, FrozenInstallPlan) error { return nil }, Resolve: func(_ context.Context, p FrozenInstallPlan, i InstallIntent) (PreparedInstall, error) {
				calls++
				f.intent = i
				hooks := f.hooks
				hooks.Commit = func(ctx context.Context, r InstallReceipt) (string, error) {
					return journal.Commit(ctx, p.SubjectID, frozen.PlanHash, r)
				}
				return PreparedInstall{PlanID: p.ID, SubjectID: p.SubjectID, Staged: f.staged, Policy: f.policy, Intent: i, Hooks: hooks}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			service, err = tasks.NewService(tasks.Options{DB: db, Repo: repo, Audit: sink, Authorizer: installAuthorizer{}, Nodes: installNodes{}, Workers: map[string]tasks.Worker{ActionInstall: worker}})
			if err != nil {
				t.Fatal(err)
			}
			p := FrozenInstallPlan{ID: "reviewed-update", SubjectID: "admin", NodeID: "isolated-node", BackupSHA256: strings.Repeat("a", 64), PolicySHA256: ReleasePolicyDigest(f.policy), SchemaFingerprint: strings.Repeat("d", 64), Previous: f.intent.Previous, Attempted: f.staged.Manifest(), AllowRollback: true}
			request, err := p.TaskRequest("request-update")
			if err != nil {
				t.Fatal(err)
			}
			previous, _ := json.Marshal(p.Previous)
			if _, err = db.Exec(ctx, `INSERT INTO cx_update_installed VALUES(?,?,?)`, p.Previous.Component, previous, p.Previous.Sequence); err != nil {
				t.Fatal(err)
			}
			frozen, err = service.Submit(ctx, request)
			if err != nil || frozen.Status != tasks.StatusAwaitingApproval {
				t.Fatal(frozen.Status, err)
			}
			if err = service.RunOnce(ctx); err != nil || calls != 0 {
				t.Fatal("ran before human approval", err, calls)
			}
			if _, err = service.Decide(ctx, frozen.ID, frozen.Revision, tasks.Decision{ApproverID: "admin", RequestID: "request-self", Approve: true}); err == nil {
				t.Fatal("self approved installation")
			}
			frozen, err = service.Decide(ctx, frozen.ID, frozen.Revision, tasks.Decision{ApproverID: "other-admin", RequestID: "request-approval", Approve: true})
			if err != nil {
				t.Fatal(err)
			}
			if failAudit {
				if _, err = db.Exec(ctx, `CREATE TRIGGER reject_update_audit BEFORE INSERT ON audit_events WHEN NEW.action='system.updates.apply' BEGIN SELECT RAISE(ABORT,'forced install audit failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			if err = service.RunOnce(ctx); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(3 * time.Second)
			var result tasks.Task
			for {
				result, err = service.Get(ctx, frozen.ID)
				if err != nil {
					t.Fatal(err)
				}
				if result.Status.Terminal() {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("worker did not finish", result.Status)
				}
				time.Sleep(time.Millisecond)
			}
			want := tasks.StatusCompleted
			if failAudit {
				want = tasks.StatusUnknown
			}
			if result.Status != want {
				t.Fatal(result.Status, want)
			}
			var floor uint64
			db.QueryRow(ctx, `SELECT sequence_floor FROM cx_update_installed`).Scan(&floor)
			var receipts int
			db.QueryRow(ctx, `SELECT count(*) FROM cx_update_receipts`).Scan(&receipts)
			if failAudit && (floor != 1 || receipts != 0) || !failAudit && (floor != 2 || receipts != 1) {
				t.Fatal("journal/audit not atomic", floor, receipts)
			}
			restarted, e := tasks.NewService(tasks.Options{DB: db, Repo: repo, Audit: sink, Authorizer: installAuthorizer{}, Nodes: installNodes{}, Workers: map[string]tasks.Worker{ActionInstall: worker}})
			if e != nil {
				t.Fatal(e)
			}
			if err = restarted.RunOnce(ctx); err != nil || calls != 1 {
				t.Fatal("restart replayed update", err, calls)
			}
		})
	}
}
