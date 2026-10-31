package transport

import (
	"bytes"
	"context"
	"encoding/json"
)

type EnrollmentAuditEvent struct {
	ActorID, Action, NodeID, EnrollmentID string
	AtUnixMilli                           int64
}
type EnrollmentSQLStatements struct {
	Read func(context.Context, string, ...any) ([]byte, error)
	Exec func(context.Context, string, ...any) (int64, error)
	// Must append through DS audit on the SAME transaction; nil retains outbox.
	AppendAudit func(context.Context, []EnrollmentAuditEvent) error
}
type EnrollmentSQLBackend struct {
	Read func(context.Context, string, ...any) ([]byte, error)
	InTx func(context.Context, func(context.Context, EnrollmentSQLStatements) error) error
}
type SQLEnrollmentStore struct{ backend EnrollmentSQLBackend }

// No migration number or automatic schema creation; coordinator registers it.
func EnrollmentSchemaStatements() []string {
	return []string{`CREATE TABLE cx_agent_enrollment(singleton INTEGER PRIMARY KEY CHECK(singleton=1),payload BLOB NOT NULL CHECK(length(payload)>0))`, `INSERT INTO cx_agent_enrollment(singleton,payload) VALUES(1,'{"Version":1}')`}
}
func NewSQLEnrollmentStore(b EnrollmentSQLBackend) (*SQLEnrollmentStore, error) {
	if b.Read == nil || b.InTx == nil {
		return nil, ErrEnrollmentUnavailable
	}
	return &SQLEnrollmentStore{b}, nil
}

const enrollmentReadSQL = `SELECT payload FROM cx_agent_enrollment WHERE singleton=1 AND length(payload)>0 AND length(payload)<=16777216`

func (s *SQLEnrollmentStore) View(ctx context.Context, fn func([]byte) error) error {
	if s == nil || ctx == nil || fn == nil || ctx.Err() != nil {
		return ErrEnrollmentUnavailable
	}
	b, err := s.backend.Read(ctx, enrollmentReadSQL)
	if err != nil || len(b) == 0 {
		return ErrEnrollmentUnavailable
	}
	if _, err = decodeEnrollment(b); err != nil {
		return err
	}
	err = fn(bytes.Clone(b))
	if ctx.Err() != nil {
		return ErrEnrollmentUnavailable
	}
	return err
}
func (s *SQLEnrollmentStore) Update(ctx context.Context, fn func(context.Context, []byte) ([]byte, error)) error {
	if s == nil || ctx == nil || fn == nil || ctx.Err() != nil {
		return ErrEnrollmentUnavailable
	}
	return s.backend.InTx(ctx, func(txCtx context.Context, tx EnrollmentSQLStatements) error {
		if txCtx == nil || txCtx.Err() != nil || ctx.Err() != nil || tx.Read == nil || tx.Exec == nil {
			return ErrEnrollmentUnavailable
		}
		b, err := tx.Read(txCtx, enrollmentReadSQL)
		if err != nil || len(b) == 0 {
			return ErrEnrollmentUnavailable
		}
		if _, err = decodeEnrollment(b); err != nil {
			return err
		}
		next, err := fn(txCtx, bytes.Clone(b))
		if err != nil {
			return err
		}
		st, err := decodeEnrollment(next)
		if err != nil || len(next) == 0 {
			return ErrEnrollmentUnavailable
		}
		if tx.AppendAudit != nil && len(st.Outbox) > 0 {
			events := make([]EnrollmentAuditEvent, 0, len(st.Outbox))
			for _, e := range st.Outbox {
				events = append(events, EnrollmentAuditEvent{e.Actor, e.Action, e.NodeID, e.EnrollmentID, e.At.UnixMilli()})
			}
			if err = tx.AppendAudit(txCtx, events); err != nil {
				return err
			}
			st.Outbox = nil
			next, err = json.Marshal(st)
			if err != nil || len(next) > 16<<20 {
				return ErrEnrollmentUnavailable
			}
		}
		if ctx.Err() != nil || txCtx.Err() != nil {
			return ErrEnrollmentUnavailable
		}
		rows, err := tx.Exec(txCtx, `UPDATE cx_agent_enrollment SET payload=? WHERE singleton=1`, bytes.Clone(next))
		if err != nil || rows != 1 {
			return ErrEnrollmentUnavailable
		}
		if ctx.Err() != nil || txCtx.Err() != nil {
			return ErrEnrollmentUnavailable
		}
		return nil
	})
}

var _ EnrollmentStore = (*SQLEnrollmentStore)(nil)
