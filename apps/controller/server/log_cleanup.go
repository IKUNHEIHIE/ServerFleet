package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/storage"
)

const (
	defaultLogRetentionDays = 15
	maxLogRetentionDays     = 3650
	logCleanupInterval      = 24 * time.Hour
	logCleanupBatch         = 500
)

var errLogCleanupConflict = errors.New("log cleanup settings changed or cleanup is running")

func logCleanupMigration() storage.Migration {
	return storage.Migration{
		Version: "ds/0092_log_cleanup",
		Name:    "persistent settings and progress for log cleanup",
		Statements: []string{
			`CREATE TABLE cx_log_cleanup (id INTEGER PRIMARY KEY CHECK (id = 1), payload TEXT NOT NULL)`,
			`INSERT INTO cx_log_cleanup (id, payload) VALUES (1, '{}')`,
			`CREATE INDEX task_logs_retention_at ON task_logs (at, id)`,
		},
	}
}

type logCleanupSettings struct {
	Revision      int            `json:"revision"`
	RetentionDays int            `json:"retentionDays"`
	Scheduled     bool           `json:"scheduled"`
	NextRunAt     *time.Time     `json:"nextRunAt,omitempty"`
	LastRun       *logCleanupRun `json:"lastRun,omitempty"`
}

type logCleanupRun struct {
	StartedAt   time.Time  `json:"startedAt"`
	Cutoff      time.Time  `json:"cutoff"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
	AuditEvents int64      `json:"auditEvents"`
	TaskLogs    int64      `json:"taskLogs"`
	Scheduled   bool       `json:"scheduled"`
}

func (settings logCleanupSettings) running() bool {
	return settings.LastRun != nil && settings.LastRun.FinishedAt == nil
}

func (settings *logCleanupSettings) start(now time.Time, scheduled bool) {
	settings.LastRun = &logCleanupRun{
		StartedAt: now,
		Cutoff:    now.Add(-time.Duration(settings.RetentionDays) * 24 * time.Hour).Truncate(time.Second),
		Scheduled: scheduled,
	}
	if scheduled {
		next := now.Add(logCleanupInterval)
		settings.NextRunAt = &next
	}
}

func loadLogCleanup(ctx context.Context, q storage.Queryer) (logCleanupSettings, error) {
	settings := logCleanupSettings{Revision: 1, RetentionDays: defaultLogRetentionDays}
	var payload string
	if err := q.QueryRow(ctx, `SELECT payload FROM cx_log_cleanup WHERE id = 1`).Scan(&payload); err != nil {
		return settings, err
	}
	if err := json.Unmarshal([]byte(payload), &settings); err != nil {
		return settings, err
	}
	if settings.Revision < 1 || settings.RetentionDays < 1 || settings.RetentionDays > maxLogRetentionDays {
		return settings, errors.New("invalid stored log cleanup settings")
	}
	return settings, nil
}

func saveLogCleanup(ctx context.Context, tx storage.Tx, settings logCleanupSettings) error {
	payload, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE cx_log_cleanup SET payload = ? WHERE id = 1`, string(payload))
	return err
}

func (s *Server) changeLogCleanup(ctx context.Context, revision int, subject, request, reason string, change func(*logCleanupSettings)) (logCleanupSettings, error) {
	var settings logCleanupSettings
	err := s.DB.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		var err error
		settings, err = loadLogCleanup(ctx, tx)
		if err != nil {
			return err
		}
		if settings.Revision != revision || settings.running() {
			return errLogCleanupConflict
		}
		change(&settings)
		if err := s.Audit.AppendTx(ctx, tx, audit.Event{
			SubjectID: subject, Action: audit.ActionRetention, Result: "success",
			RequestID: request, Source: audit.SourceWeb, ReasonCode: reason, At: s.now().UTC(),
		}); err != nil {
			return err
		}
		return saveLogCleanup(ctx, tx, settings)
	})
	return settings, err
}

func (s *Server) registerLogCleanupRoutes() {
	s.mux.HandleFunc("GET /api/v1/settings/log-cleanup", func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := s.operatorPrincipal(w, r); !ok {
			return
		}
		if r.URL.RawQuery != "" || r.ContentLength != 0 {
			releaseSourceJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		settings, err := loadLogCleanup(r.Context(), s.DB)
		writeLogCleanup(w, http.StatusOK, settings, err)
	})
	s.mux.HandleFunc("PUT /api/v1/settings/log-cleanup", func(w http.ResponseWriter, r *http.Request) {
		subject, request, ok := s.operatorPrincipal(w, r)
		if !ok {
			return
		}
		var input struct {
			Revision      int   `json:"expectedRevision"`
			RetentionDays int   `json:"retentionDays"`
			Scheduled     *bool `json:"scheduled"`
		}
		if !decodeOperationsBody(w, r, &input) {
			return
		}
		if input.Revision < 1 || input.RetentionDays < 1 || input.RetentionDays > maxLogRetentionDays || input.Scheduled == nil {
			releaseSourceJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		settings, err := s.changeLogCleanup(r.Context(), input.Revision, subject, request, "log_cleanup_configured", func(settings *logCleanupSettings) {
			if *input.Scheduled && !settings.Scheduled {
				next := s.now().UTC().Add(logCleanupInterval)
				settings.NextRunAt = &next
			} else if !*input.Scheduled {
				settings.NextRunAt = nil
			}
			settings.Revision++
			settings.RetentionDays = input.RetentionDays
			settings.Scheduled = *input.Scheduled
		})
		writeLogCleanup(w, http.StatusOK, settings, err)
	})
	s.mux.HandleFunc("POST /api/v1/settings/log-cleanup/run", func(w http.ResponseWriter, r *http.Request) {
		subject, request, ok := s.operatorPrincipal(w, r)
		if !ok {
			return
		}
		var input struct {
			Revision int `json:"expectedRevision"`
		}
		if !decodeOperationsBody(w, r, &input) {
			return
		}
		if input.Revision < 1 {
			releaseSourceJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		settings, err := s.changeLogCleanup(r.Context(), input.Revision, subject, request, "log_cleanup_requested", func(settings *logCleanupSettings) {
			settings.start(s.now().UTC(), false)
		})
		if err == nil {
			select {
			case s.logCleanupWake <- struct{}{}:
			default:
			}
		}
		writeLogCleanup(w, http.StatusAccepted, settings, err)
	})
}

func writeLogCleanup(w http.ResponseWriter, status int, settings logCleanupSettings, err error) {
	if errors.Is(err, errLogCleanupConflict) {
		releaseSourceJSON(w, http.StatusConflict, map[string]string{"code": "LOG_CLEANUP_CONFLICT"})
		return
	}
	if err != nil {
		releaseSourceJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "LOG_CLEANUP_UNAVAILABLE"})
		return
	}
	releaseSourceJSON(w, status, settings)
}

func (s *Server) runLogCleanup(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		batch, cancel := context.WithTimeout(ctx, 8*time.Second)
		more, err := s.runLogCleanupBatch(batch)
		cancel()
		if err != nil && ctx.Err() == nil {
			log.Print("controller: log cleanup batch failed, will retry")
		}
		if more {
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				continue
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.logCleanupWake:
		}
	}
}

func (s *Server) runLogCleanupBatch(ctx context.Context) (bool, error) {
	now := s.now().UTC()
	settings, err := loadLogCleanup(ctx, s.DB)
	if err != nil {
		return false, err
	}
	due := func(settings logCleanupSettings) bool {
		return settings.Scheduled && settings.NextRunAt != nil && !now.Before(*settings.NextRunAt)
	}
	if (!settings.running() && !due(settings)) || s.runtimeGate(ctx) != nil {
		return false, nil
	}
	more := false
	err = s.DB.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		settings, err := loadLogCleanup(ctx, tx)
		if err != nil {
			return err
		}
		if !settings.running() {
			if !due(settings) {
				return nil
			}
			settings.start(now, true)
			if err := s.Audit.AppendTx(ctx, tx, audit.Event{
				SubjectID: "system-maintenance", Action: audit.ActionRetention, Result: "success",
				RequestID: newRequestID(), Source: audit.SourceSystem, ReasonCode: "log_cleanup_scheduled", At: now,
			}); err != nil {
				return err
			}
		}
		run := settings.LastRun
		auditRemoved, err := s.Audit.PurgeExpiredBatchTx(ctx, tx, run.Cutoff, logCleanupBatch)
		if err != nil {
			return err
		}
		taskRemoved, err := s.TasksRepo.PurgeTerminalLogsBatchTx(ctx, tx, run.Cutoff, logCleanupBatch)
		if err != nil {
			return err
		}
		run.AuditEvents += auditRemoved
		run.TaskLogs += taskRemoved
		more = auditRemoved == logCleanupBatch || taskRemoved == logCleanupBatch
		if !more {
			finished := s.now().UTC()
			run.FinishedAt = &finished
		}
		return saveLogCleanup(ctx, tx, settings)
	})
	return err == nil && more, err
}
