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
}
type IdentitySQLBackend struct {
	Read func(context.Context, string, ...any) ([]byte, bool, error)
	// InTx must use one DS BEGIN IMMEDIATE transaction, invoke fn exactly once,
	// roll back on callback/cancellation/commit errors, and never retry fn.
	InTx func(context.Context, func(IdentitySQLStatements) error) error
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
	if backend.Read == nil || backend.InTx == nil {
		return nil, ErrIdentityUnavailable
	}
	return &SQLIdentityStore{backend}, nil
}

const readIdentitySQL = `SELECT payload,ever_initialized FROM cx_auth_identity WHERE singleton=1 AND (payload IS NULL OR length(payload)<=16777216)`

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
	if ctx == nil || s == nil || s.backend.InTx == nil || fn == nil {
		return ErrIdentityUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return identityStoreError(ctx, s.backend.InTx(ctx, func(tx IdentitySQLStatements) error {
		if tx.Read == nil || tx.Exec == nil {
			return ErrIdentityUnavailable
		}
		data, initialized, err := tx.Read(ctx, readIdentitySQL)
		if err != nil {
			return err
		}
		if err = checkIdentityRecord(data, initialized); err != nil {
			return err
		}
		next, err := fn(bytes.Clone(data))
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
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
		if tx.AppendAudit != nil && len(state.Outbox) > 0 {
			events, err := identityAuditEvents(state.Outbox)
			if err != nil {
				return err
			}
			if err = tx.AppendAudit(ctx, events); err != nil {
				return err
			}
			state.Outbox = nil
			next, err = json.Marshal(state)
			if err != nil || len(next) > 16<<20 {
				return ErrIdentityUnavailable
			}
		}
		rows, err := tx.Exec(ctx, `UPDATE cx_auth_identity SET payload=?,ever_initialized=? WHERE singleton=1 AND ever_initialized=?`, bytes.Clone(next), state.Initialized, initialized)
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrIdentityUnavailable
		}
		return ctx.Err()
	}))
}

var _ IdentityStore = (*SQLIdentityStore)(nil)
