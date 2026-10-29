package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"time"
)

type SessionAuditEvent struct {
	SubjectID, Action, SessionID string
	ScopeRevision                uint64
	At                           time.Time
	NodeIDs                      []string
}
type SessionSQLStatements struct {
	Read        func(context.Context, string, ...any) ([]byte, error)
	Exec        func(context.Context, string, ...any) (int64, error)
	AppendAudit func(context.Context, []SessionAuditEvent) error
}
type SessionSQLBackend struct {
	Read func(context.Context, string, ...any) ([]byte, error)
	InTx func(context.Context, func(SessionSQLStatements) error) error
}
type SQLSessionStore struct{ backend SessionSQLBackend }

func SessionSchemaStatements() []string {
	return []string{`CREATE TABLE cx_ai_sessions(singleton INTEGER PRIMARY KEY CHECK(singleton=1),payload BLOB NOT NULL CHECK(length(payload)>0))`, `INSERT INTO cx_ai_sessions(singleton,payload) VALUES(1,'{"Version":1}')`}
}
func NewSQLSessionStore(backend SessionSQLBackend) (*SQLSessionStore, error) {
	if backend.Read == nil || backend.InTx == nil {
		return nil, ErrSessionUnavailable
	}
	return &SQLSessionStore{backend}, nil
}

const readSessionSQL = `SELECT payload FROM cx_ai_sessions WHERE singleton=1 AND length(payload)>0 AND length(payload)<=8388608`

func (s *SQLSessionStore) View(ctx context.Context, fn func([]byte) error) error {
	if s == nil || ctx == nil || fn == nil || ctx.Err() != nil {
		return ErrSessionUnavailable
	}
	raw, err := s.backend.Read(ctx, readSessionSQL)
	if err != nil || len(raw) == 0 {
		return ErrSessionUnavailable
	}
	if _, err = decodeSessions(raw); err != nil {
		return err
	}
	err = fn(bytes.Clone(raw))
	if ctx.Err() != nil {
		return ErrSessionUnavailable
	}
	return err
}
func (s *SQLSessionStore) Update(ctx context.Context, fn func([]byte) ([]byte, error)) error {
	if s == nil || ctx == nil || fn == nil || ctx.Err() != nil {
		return ErrSessionUnavailable
	}
	return s.backend.InTx(ctx, func(tx SessionSQLStatements) error {
		if tx.Read == nil || tx.Exec == nil {
			return ErrSessionUnavailable
		}
		raw, err := tx.Read(ctx, readSessionSQL)
		if err != nil || len(raw) == 0 {
			return ErrSessionUnavailable
		}
		if _, err = decodeSessions(raw); err != nil {
			return err
		}
		next, err := fn(bytes.Clone(raw))
		if err != nil {
			return err
		}
		st, err := decodeSessions(next)
		if err != nil || len(next) == 0 {
			return ErrSessionUnavailable
		}
		// A nil audit sink preserves the durable outbox, never drops it or claims it
		// reached the audit domain. Reviewed composition must bind SAME DS Tx.
		if tx.AppendAudit != nil && len(st.Outbox) > 0 {
			events := make([]SessionAuditEvent, 0, len(st.Outbox))
			for _, event := range st.Outbox {
				events = append(events, SessionAuditEvent{event.SubjectID, event.Action, event.SessionID, event.ScopeRevision, event.At, append([]string{}, event.NodeIDs...)})
			}
			if err = tx.AppendAudit(ctx, events); err != nil {
				return err
			}
			st.Outbox = nil
			next, err = json.Marshal(st)
			if err != nil || len(next) > 8<<20 {
				return ErrSessionUnavailable
			}
		}
		if ctx.Err() != nil {
			return ErrSessionUnavailable
		}
		rows, err := tx.Exec(ctx, `UPDATE cx_ai_sessions SET payload=? WHERE singleton=1`, bytes.Clone(next))
		if err != nil || rows != 1 {
			return ErrSessionUnavailable
		}
		if ctx.Err() != nil {
			return ErrSessionUnavailable
		}
		return nil
	})
}

var _ SessionStore = (*SQLSessionStore)(nil)
