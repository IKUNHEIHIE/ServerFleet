package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/storage"
)

func TestLogCleanupManualAndScheduled(t *testing.T) {
	ctx := context.Background()
	tls := httptest.NewUnstartedServer(nil)
	t.Cleanup(tls.Close)
	origin := "https://" + tls.Listener.Addr().String()
	now := time.Now().UTC().Truncate(time.Second)
	cfg := Config{
		DBPath: filepath.Join(t.TempDir(), "cleanup.db"), Origin: origin,
		Now: func() time.Time { return now },
	}
	if dsn := os.Getenv("SF_TEST_LOG_CLEANUP_POSTGRES_DSN"); dsn != "" {
		cfg.DBDriver, cfg.DBDSN = "postgres", dsn
	}
	s, err := New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	s.runCancel()
	<-s.runDone
	tls.Config.Handler = s
	tls.StartTLS()
	client := tls.Client()
	client.Jar, _ = cookiejar.New(nil)

	const base = "/api/v1/settings/log-cleanup"
	csrf := ""
	request := func(method, path, body string, status int, output any) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, origin+path, bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", origin)
		req.Header.Set("X-Requested-With", "ServerFleetWeb")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrf)
		if path == "/api/v1/auth/bootstrap" {
			req.Header.Set("Idempotency-Key", "bootstrap-log-cleanup-fixture")
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, err := io.ReadAll(res.Body)
		if err != nil || res.StatusCode != status {
			t.Fatalf("%s %s: status %d, want %d, body %s, error %v", method, path, res.StatusCode, status, raw, err)
		}
		if output != nil && json.Unmarshal(raw, output) != nil {
			t.Fatalf("invalid response: %s", raw)
		}
	}
	request("GET", base, "", http.StatusForbidden, nil)
	request("POST", base+"/run", `{"expectedRevision":1}`, http.StatusForbidden, nil)
	token, err := s.Identity.PrepareSetup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	credentials := map[string]string{"setupToken": token, "username": "admin", "password": "synthetic-log-cleanup-admin"}
	body, _ := json.Marshal(credentials)
	request("POST", "/api/v1/auth/bootstrap", string(body), http.StatusNoContent, nil)
	delete(credentials, "setupToken")
	body, _ = json.Marshal(credentials)
	var login struct {
		CSRF string `json:"csrfToken"`
	}
	request("POST", "/api/v1/auth/login", string(body), http.StatusOK, &login)
	csrf = login.CSRF
	var settings logCleanupSettings
	request("GET", base, "", http.StatusOK, &settings)
	if settings.RetentionDays != 15 || settings.Scheduled || settings.NextRunAt != nil {
		t.Fatalf("unexpected default settings: %+v", settings)
	}
	request("PUT", base, `{"expectedRevision":1,"retentionDays":0,"scheduled":true}`, http.StatusBadRequest, nil)
	request("PUT", base, `{"expectedRevision":8,"retentionDays":15,"scheduled":true}`, http.StatusConflict, nil)

	cutoff := now.Add(-15 * 24 * time.Hour)
	old := cutoff.Add(-time.Second)
	err = s.DB.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		for i := 0; i <= logCleanupBatch; i++ {
			if err := s.Audit.AppendTx(ctx, tx, audit.Event{
				SubjectID: "fixture", Action: audit.ActionRetention, Result: "success",
				RequestID: "expired-test-log", Source: audit.SourceSystem, At: old,
			}); err != nil {
				return err
			}
		}
		if err := s.Audit.AppendTx(ctx, tx, audit.Event{
			SubjectID: "fixture", Action: audit.ActionRetention, Result: "success",
			RequestID: "cutoff-test-log", Source: audit.SourceSystem, At: cutoff.Add(time.Millisecond),
		}); err != nil {
			return err
		}
		for _, row := range [][2]string{{"finished", "completed"}, {"in-flight", "running"}} {
			if _, err := tx.Exec(ctx, `INSERT INTO tasks (
				id, name, submitter_id, action_id, operation_class, revision, status, plan_hash, unstarted_deadline,
				request_id, created_at, updated_at) VALUES (?, 'fixture', 'fixture', 'tasks.command', 'standard', 1, ?,
				'fixture', '', 'fixture', '', '')`, row[0], row[1]); err != nil {
				return err
			}
		}
		for _, row := range [][3]string{
			{"finished", "expired-output", old.Format(time.RFC3339Nano)},
			{"finished", "boundary-output", cutoff.Add(time.Millisecond).Format(time.RFC3339Nano)},
			{"in-flight", "in-flight-output", old.Format(time.RFC3339Nano)},
		} {
			if _, err := tx.Exec(ctx, `INSERT INTO task_logs (task_id, level, message, at) VALUES (?, 'info', ?, ?)`, row[0], row[1], row[2]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	runBatch := func(wantMore bool) {
		t.Helper()
		more, err := s.runLogCleanupBatch(ctx)
		if err != nil || more != wantMore {
			t.Fatalf("run cleanup batch: more=%t err=%v", more, err)
		}
	}
	count := func(q string, want int) {
		t.Helper()
		var got int
		if err := s.DB.QueryRow(ctx, q).Scan(&got); err != nil || got != want {
			t.Fatalf("%s: count %d want %d, err=%v", q, got, want, err)
		}
	}
	request("POST", base+"/run", `{"expectedRevision":1}`, http.StatusAccepted, nil)
	request("POST", base+"/run", `{"expectedRevision":1}`, http.StatusConflict, nil)
	runBatch(true)
	runBatch(false)
	request("GET", base, "", http.StatusOK, &settings)
	if run := settings.LastRun; run == nil || run.FinishedAt == nil || run.AuditEvents != logCleanupBatch+1 || run.TaskLogs != 1 || run.Scheduled {
		t.Fatalf("unexpected manual cleanup receipt: %+v", run)
	}
	count(`SELECT COUNT(*) FROM audit_events WHERE request_id = 'expired-test-log'`, 0)
	count(`SELECT COUNT(*) FROM audit_events WHERE request_id = 'cutoff-test-log'`, 1)
	count(`SELECT COUNT(*) FROM task_logs WHERE message = 'boundary-output' OR message = 'in-flight-output'`, 2)
	count(`SELECT COUNT(*) FROM tasks WHERE id IN ('finished', 'in-flight')`, 2)

	request("PUT", base, `{"expectedRevision":1,"retentionDays":15,"scheduled":true}`, http.StatusOK, nil)
	runBatch(false)
	count(`SELECT COUNT(*) FROM audit_events WHERE reason_code = 'log_cleanup_scheduled'`, 0)
	now = now.Add(24 * time.Hour)
	runBatch(false)
	settings, err = loadLogCleanup(ctx, s.DB)
	if err != nil {
		t.Fatal(err)
	}
	if run := settings.LastRun; run == nil || !run.Scheduled || run.AuditEvents != 1 || run.TaskLogs != 1 ||
		run.FinishedAt == nil || settings.NextRunAt == nil || !settings.NextRunAt.Equal(now.Add(24*time.Hour)) {
		t.Fatalf("unexpected scheduled receipt: %+v, next run %v", run, settings.NextRunAt)
	}
	runBatch(false)
	count(`SELECT COUNT(*) FROM audit_events WHERE reason_code = 'log_cleanup_scheduled'`, 1)
	count(`SELECT COUNT(*) FROM task_logs WHERE message = 'in-flight-output'`, 1)
}
