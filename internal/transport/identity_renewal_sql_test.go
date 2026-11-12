package transport

import (
	"context"
	"path/filepath"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/storage"
	"testing"
	"time"
)

func TestIdentityRenewalSQLiteAuditAndRestartReplay(t *testing.T) {
	service, memory, _, state, input := renewalFixture(t)
	ctx := context.Background()
	db, e := storage.Open(ctx, storage.Config{Path: filepath.Join(t.TempDir(), "renew.db")})
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	m, e := storage.NewMigrator([]storage.Migration{{Version: "renewal/test", Name: "test-only enrollment schema", Statements: EnrollmentSchemaStatements()}}, audit.Migrations())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Apply(ctx, db); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, "UPDATE cx_agent_enrollment SET payload=? WHERE singleton=1", memory.data); e != nil {
		t.Fatal(e)
	}
	validator, e := audit.NewValidator([]string{"enrollment.token.issue", "enrollment.request", "enrollment.approved", "agent.identity.renew", "agent.identity.activate"}, audit.DefaultLimits())
	if e != nil {
		t.Fatal(e)
	}
	recorder, e := audit.NewSQLRecorder(db, validator, service.now)
	if e != nil {
		t.Fatal(e)
	}
	read := func(ctx context.Context, q storage.Queryer, query string, args ...any) ([]byte, error) {
		var raw []byte
		e := q.QueryRow(ctx, query, args...).Scan(&raw)
		return raw, e
	}
	backend := EnrollmentSQLBackend{Read: func(ctx context.Context, query string, args ...any) ([]byte, error) {
		return read(ctx, db, query, args...)
	}}
	backend.InTx = func(ctx context.Context, fn func(context.Context, EnrollmentSQLStatements) error) error {
		return db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
			return fn(ctx, EnrollmentSQLStatements{
				Read: func(ctx context.Context, query string, args ...any) ([]byte, error) {
					return read(ctx, tx, query, args...)
				},
				Exec: func(ctx context.Context, query string, args ...any) (int64, error) {
					result, e := tx.Exec(ctx, query, args...)
					if e != nil {
						return 0, e
					}
					return result.RowsAffected()
				},
				AppendAudit: func(ctx context.Context, events []EnrollmentAuditEvent) error {
					for _, event := range events {
						request := event.EnrollmentID
						if request == "" {
							request = "fixture-request"
						}
						nodes := []string{}
						if event.NodeID != "" {
							nodes = []string{event.NodeID}
						}
						if e := recorder.AppendTx(ctx, tx, audit.Event{SubjectID: event.ActorID, Action: event.Action, Result: "success", RequestID: request, NodeIDs: nodes, Source: audit.SourceAgent, At: time.UnixMilli(event.AtUnixMilli)}); e != nil {
							return e
						}
					}
					return nil
				},
			})
		})
	}
	persistent, e := NewSQLEnrollmentStore(backend)
	if e != nil {
		t.Fatal(e)
	}
	service.store = persistent
	current := func(context.Context, string) error { return nil }
	issued, e := service.RenewIdentity(ctx, state, input, current)
	if e != nil {
		t.Fatal(e)
	}
	restarted, e := NewEnrollmentService(persistent, service.issuer, service.gate, service.commit)
	if e != nil {
		t.Fatal(e)
	}
	restarted.now = service.now
	replay, e := restarted.RenewIdentity(ctx, state, input, current)
	if e != nil || replay != issued {
		t.Fatal("SQLite restart lost issuance receipt", e)
	}
	var count int
	if e = db.QueryRow(ctx, "SELECT count(*) FROM audit_events WHERE action='agent.identity.renew'").Scan(&count); e != nil || count != 1 {
		t.Fatal("renewal audit not atomic or duplicated", count, e)
	}
}
