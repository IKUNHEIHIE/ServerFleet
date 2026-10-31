package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"time"
)

type LifecycleAuditEvent struct {
	ActorID, TaskID, Action, Result string
	At                              time.Time
}
type LifecycleSQLStatements struct {
	Read        func(context.Context, string, ...any) ([]byte, error)
	Exec        func(context.Context, string, ...any) (int64, error)
	AppendAudit func(context.Context, []LifecycleAuditEvent) error
}
type LifecycleSQLBackend struct {
	Read func(context.Context, string, ...any) ([]byte, error)
	InTx func(context.Context, func(LifecycleSQLStatements) error) error
}
type SQLLifecycleStore struct{ backend LifecycleSQLBackend }

func LifecycleSchemaStatements() []string {
	return []string{`CREATE TABLE cx_plugin_lifecycle(singleton INTEGER PRIMARY KEY CHECK(singleton=1),payload BLOB NOT NULL CHECK(length(payload)>0))`, `INSERT INTO cx_plugin_lifecycle(singleton,payload) VALUES(1,'{"Version":1}')`}
}
func NewSQLLifecycleStore(b LifecycleSQLBackend) (*SQLLifecycleStore, error) {
	if b.Read == nil || b.InTx == nil {
		return nil, ErrPackageRejected
	}
	return &SQLLifecycleStore{b}, nil
}

const lifecycleReadSQL = `SELECT payload FROM cx_plugin_lifecycle WHERE singleton=1 AND length(payload)>0 AND length(payload)<=4194304`

func (s *SQLLifecycleStore) View(ctx context.Context, fn func([]byte) error) error {
	if s == nil || ctx == nil || ctx.Err() != nil || fn == nil {
		return ErrPackageRejected
	}
	raw, err := s.backend.Read(ctx, lifecycleReadSQL)
	if err != nil || len(raw) == 0 {
		return ErrPackageRejected
	}
	if _, err = decodeLifecycle(raw); err != nil {
		return err
	}
	err = fn(bytes.Clone(raw))
	if ctx.Err() != nil {
		return ErrPackageRejected
	}
	return err
}
func (s *SQLLifecycleStore) Update(ctx context.Context, fn func([]byte) ([]byte, error)) error {
	if s == nil || ctx == nil || ctx.Err() != nil || fn == nil {
		return ErrPackageRejected
	}
	return s.backend.InTx(ctx, func(tx LifecycleSQLStatements) error {
		if tx.Read == nil || tx.Exec == nil {
			return ErrPackageRejected
		}
		raw, err := tx.Read(ctx, lifecycleReadSQL)
		if err != nil || len(raw) == 0 {
			return ErrPackageRejected
		}
		if _, err = decodeLifecycle(raw); err != nil {
			return err
		}
		next, err := fn(bytes.Clone(raw))
		if err != nil {
			return err
		}
		st, err := decodeLifecycle(next)
		if err != nil || len(next) == 0 {
			return ErrPackageRejected
		}
		if tx.AppendAudit != nil && len(st.Outbox) > 0 {
			events := make([]LifecycleAuditEvent, 0, len(st.Outbox))
			for _, e := range st.Outbox {
				events = append(events, LifecycleAuditEvent(e))
			}
			if err = tx.AppendAudit(ctx, events); err != nil {
				return err
			}
			st.Outbox = nil
			next, err = json.Marshal(st)
			if err != nil || len(next) > 4<<20 {
				return ErrPackageRejected
			}
		}
		if ctx.Err() != nil {
			return ErrPackageRejected
		}
		rows, err := tx.Exec(ctx, `UPDATE cx_plugin_lifecycle SET payload=? WHERE singleton=1`, bytes.Clone(next))
		if err != nil || rows != 1 {
			return ErrPackageRejected
		}
		if ctx.Err() != nil {
			return ErrPackageRejected
		}
		return nil
	})
}

var _ LifecycleStore = (*SQLLifecycleStore)(nil)
