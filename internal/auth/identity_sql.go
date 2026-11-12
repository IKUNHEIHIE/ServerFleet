package auth

import (
	"bytes"
	"context"
	"encoding/json"
)

// IdentitySQLStatements is an auth-owned adapter surface, not a storage KV API.
// The composition adapter maps Scan(payload, ever_initialized) and RowsAffected
// onto the DS Queryer/Tx proposal. No database/sql dependency escapes storage.
// These new integration ports remain candidates until owner review.
type IdentitySQLStatements struct {
	Read func(context.Context, string, ...any) (payload []byte, everInitialized bool, err error)
	Exec func(context.Context, string, ...any) (rowsAffected int64, err error)
	// AppendAudit must use this SAME underlying DS Tx (e.g. SQLRecorder.AppendTx).
	// A nil sink preserves the durable outbox for later adoption; it never drops
	// events or pretends they reached the audit domain. Production review decides
	// when that deferred mode is admissible. No callback opens a nested writer.
	AppendAudit func(context.Context, []IdentityAuditEvent) error
	// Complete typed Xray material events, never folded into a legacy event
	// that loses instance/user/reference/plan. Mandatory for material mutation.
	AppendXrayMaterialAudit func(context.Context, []XrayMaterialAuditEvent) error
}
type IdentitySQLBackend struct {
	Read func(context.Context, string, ...any) ([]byte, bool, error)
	// InTx must use one DS BEGIN IMMEDIATE transaction, invoke fn exactly once,
	// roll back on callback/cancellation/commit errors, and never retry fn.
	InTx func(context.Context, func(IdentitySQLStatements) error) error
	// Optional migration seam: binds the actual transaction's read-only policy
	// reader into txCtx. Required by UpdateWithContext; no legacy fallback there.
	InTxContext func(context.Context, func(context.Context, IdentitySQLStatements) error) error
}
type SQLIdentityStore struct{ backend IdentitySQLBackend }

// IdentitySchemaStatements defines the auth-owned aggregate and its permanent
// initialization marker. It assigns NO public migration ID and runs no schema
// changes automatically. The coordinator must register an immutable migration.
func IdentitySchemaStatements() []string {
	return []string{
		`CREATE TABLE cx_auth_identity (singleton INTEGER PRIMARY KEY CHECK(singleton=1), payload BLOB, ever_initialized INTEGER NOT NULL DEFAULT 0 CHECK(ever_initialized IN (0,1)), CHECK(payload IS NOT NULL OR ever_initialized=0))`,
		`INSERT INTO cx_auth_identity(singleton,payload,ever_initialized) VALUES(1,NULL,0)`,
	}
}
func NewSQLIdentityStore(backend IdentitySQLBackend) (*SQLIdentityStore, error) {
	if backend.Read == nil || backend.InTx == nil && backend.InTxContext == nil {
		return nil, ErrIdentityUnavailable
	}
	return &SQLIdentityStore{backend}, nil
}

const readIdentitySQL = `SELECT payload,ever_initialized FROM cx_auth_identity WHERE singleton=1 AND (payload IS NULL OR length(payload)<=16777216)`

// Operator preflight reads before ordinary normalization so fixed field/version
// diagnostics can distinguish a bad payload from an initialization-marker error.
func (s *SQLIdentityStore) readUpgrade(ctx context.Context, fn func([]byte, bool) error) error {
	if s == nil || s.backend.Read == nil || ctx == nil || ctx.Err() != nil {
		return ErrIdentityUnavailable
	}
	raw, initialized, e := s.backend.Read(ctx, readIdentitySQL)
	if e != nil {
		return e
	}
	return fn(bytes.Clone(raw), initialized)
}

func checkIdentityRecord(data []byte, initialized bool) error {
	st, err := decodeIdentity(data)
	if err != nil {
		return err
	}
	if initialized != st.Initialized {
		return ErrIdentityUnavailable
	}
	return nil
}
func (s *SQLIdentityStore) View(ctx context.Context, fn func([]byte) error) error {
	if ctx == nil || s == nil || s.backend.Read == nil || fn == nil {
		return ErrIdentityUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	data, initialized, err := s.backend.Read(ctx, readIdentitySQL)
	if err != nil {
		return identityStoreError(ctx, err)
	}
	if err = checkIdentityRecord(data, initialized); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	err = fn(bytes.Clone(data))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
func (s *SQLIdentityStore) Update(ctx context.Context, fn func([]byte) ([]byte, error)) error {
	if fn == nil {
		return ErrIdentityUnavailable
	}
	return s.updateSQL(ctx, func(_ context.Context, data []byte) ([]byte, error) { return fn(data) }, false)
}
func (s *SQLIdentityStore) UpdateWithContext(ctx context.Context, fn func(context.Context, []byte) ([]byte, error)) error {
	return s.updateSQL(ctx, fn, true)
}
func (s *SQLIdentityStore) updateSQL(ctx context.Context, fn func(context.Context, []byte) ([]byte, error), requireTransaction bool) error {
	if ctx == nil || s == nil || fn == nil {
		return ErrIdentityUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	run := s.backend.InTxContext
	if run == nil {
		if requireTransaction || s.backend.InTx == nil {
			return ErrIdentityUnavailable
		}
		run = func(ctx context.Context, fn func(context.Context, IdentitySQLStatements) error) error {
			return s.backend.InTx(ctx, func(tx IdentitySQLStatements) error { return fn(ctx, tx) })
		}
	}
	called := false
	err := run(ctx, func(txCtx context.Context, tx IdentitySQLStatements) error {
		if called || txCtx == nil || txCtx.Err() != nil || ctx.Err() != nil || tx.Read == nil || tx.Exec == nil {
			return ErrIdentityUnavailable
		}
		called = true
		// Derive this marker afresh from THIS transaction's actual sink. Never
		// inherit an earlier transaction's authority through a retained ctx.
		txCtx = context.WithValue(txCtx, xrayMaterialAuditContextKey{}, tx.AppendXrayMaterialAudit != nil)
		data, initialized, err := tx.Read(txCtx, readIdentitySQL)
		if err != nil {
			return err
		}
		if err = checkIdentityRecord(data, initialized); err != nil {
			return err
		}
		next, err := fn(txCtx, bytes.Clone(data))
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if txCtx.Err() != nil {
			return txCtx.Err()
		}
		state, err := decodeIdentity(next)
		if err != nil {
			return err
		}
		if initialized && !state.Initialized {
			return ErrIdentityUnavailable
		}
		if len(next) == 0 {
			return ErrIdentityUnavailable
		}
		if tx.AppendXrayMaterialAudit != nil && len(state.XrayMaterialOutbox) > 0 {
			events := append([]XrayMaterialAuditEvent(nil), state.XrayMaterialOutbox...)
			if err = tx.AppendXrayMaterialAudit(txCtx, events); err != nil {
				return err
			}
			state.XrayMaterialOutbox = nil
			next, err = json.Marshal(state)
			if err != nil || len(next) > 16<<20 {
				return ErrIdentityUnavailable
			}
		}
		if tx.AppendAudit != nil && len(state.Outbox) > 0 {
			events, err := identityAuditEvents(state.Outbox)
			if err != nil {
				return err
			}
			if err = tx.AppendAudit(txCtx, events); err != nil {
				return err
			}
			state.Outbox = nil
			next, err = json.Marshal(state)
			if err != nil || len(next) > 16<<20 {
				return ErrIdentityUnavailable
			}
		}
		rows, err := tx.Exec(txCtx, `UPDATE cx_auth_identity SET payload=?,ever_initialized=? WHERE singleton=1 AND ever_initialized=?`, bytes.Clone(next), state.Initialized, initialized)
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrIdentityUnavailable
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return txCtx.Err()
	})
	if !called && err == nil {
		return ErrIdentityUnavailable
	}
	return identityStoreError(ctx, err)
}

var _ IdentityStore = (*SQLIdentityStore)(nil)
var _ IdentityTransactionStore = (*SQLIdentityStore)(nil)
