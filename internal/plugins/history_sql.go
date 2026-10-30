package plugins

import (
	"bytes"
	"context"
	"encoding/json"
)

type HistorySQLStatements struct {
	Read func(context.Context, string, ...any) ([]byte, error)
	Exec func(context.Context, string, ...any) (int64, error)
	// Persist ALL fields on the same transaction. Nil retains the durable outbox.
	AppendAudit func(context.Context, []HistoryAuditEvent) error
}
type HistorySQLBackend struct {
	Read func(context.Context, string, ...any) ([]byte, error)
	InTx func(context.Context, func(context.Context, HistorySQLStatements) error) error
}
type SQLHistoryStore struct {
	backend HistorySQLBackend
	scope   HistoryScope
	key     string
}

func HistorySchemaStatements() []string {
	return []string{`CREATE TABLE cx_plugin_history(scope TEXT PRIMARY KEY,payload BLOB NOT NULL CHECK(length(payload)>0 AND length(payload)<=25165824))`}
}
func historyScopeKey(scope HistoryScope) string {
	raw, _ := json.Marshal(scope)
	return historyDigest(raw)
}

// Explicit provisioning instruction for a NEW trusted installation/instance.
// No automatic INSERT/REPLACE/reset on missing or corrupted state. Coordinator
// owns schema registration; signed installation code owns this one-time insert.
func HistorySeed(scope HistoryScope) (string, []any, error) {
	if !validHistoryScope(scope) {
		return "", nil, ErrHistoryRejected
	}
	raw, err := json.Marshal(historyState{Format: 1, Scope: scope})
	if err != nil {
		return "", nil, ErrHistoryRejected
	}
	return `INSERT INTO cx_plugin_history(scope,payload) VALUES(?,?)`, []any{historyScopeKey(scope), raw}, nil
}
func NewSQLHistoryStore(backend HistorySQLBackend, scope HistoryScope) (*SQLHistoryStore, error) {
	if backend.Read == nil || backend.InTx == nil || !validHistoryScope(scope) {
		return nil, ErrHistoryRejected
	}
	return &SQLHistoryStore{backend, scope, historyScopeKey(scope)}, nil
}

const historyReadSQL = `SELECT payload FROM cx_plugin_history WHERE scope=? AND length(payload)>0 AND length(payload)<=25165824`

func (s *SQLHistoryStore) View(ctx context.Context, fn func([]byte) error) error {
	if s == nil || ctx == nil || ctx.Err() != nil || fn == nil {
		return ErrHistoryRejected
	}
	raw, err := s.backend.Read(ctx, historyReadSQL, s.key)
	if err != nil {
		return ErrHistoryRejected
	}
	if _, err = decodeHistory(raw, s.scope); err != nil {
		return err
	}
	if err = fn(bytes.Clone(raw)); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ErrHistoryRejected
	}
	return nil
}
func (s *SQLHistoryStore) Update(ctx context.Context, fn func(context.Context, []byte) ([]byte, error)) error {
	if s == nil || ctx == nil || ctx.Err() != nil || fn == nil {
		return ErrHistoryRejected
	}
	called := false
	err := s.backend.InTx(ctx, func(txCtx context.Context, tx HistorySQLStatements) error {
		if called || txCtx == nil || txCtx.Err() != nil || ctx.Err() != nil || tx.Read == nil || tx.Exec == nil {
			return ErrHistoryRejected
		}
		called = true
		raw, err := tx.Read(txCtx, historyReadSQL, s.key)
		if err != nil {
			return ErrHistoryRejected
		}
		if _, err = decodeHistory(raw, s.scope); err != nil {
			return err
		}
		next, err := fn(txCtx, bytes.Clone(raw))
		if err != nil {
			return err
		}
		st, err := decodeHistory(next, s.scope)
		if err != nil {
			return err
		}
		if tx.AppendAudit != nil && len(st.Outbox) > 0 {
			events := append([]HistoryAuditEvent(nil), st.Outbox...)
			if err = tx.AppendAudit(txCtx, events); err != nil {
				return ErrHistoryRejected
			}
			st.Outbox = nil
			next, err = json.Marshal(st)
			if err != nil || len(next) > 24<<20 {
				return ErrHistoryRejected
			}
		}
		if ctx.Err() != nil || txCtx.Err() != nil {
			return ErrHistoryRejected
		}
		rows, err := tx.Exec(txCtx, `UPDATE cx_plugin_history SET payload=? WHERE scope=?`, bytes.Clone(next), s.key)
		if err != nil || rows != 1 || ctx.Err() != nil || txCtx.Err() != nil {
			return ErrHistoryRejected
		}
		return nil
	})
	if !called {
		return ErrHistoryRejected
	}
	return err
}

var _ HistoryStore = (*SQLHistoryStore)(nil)
