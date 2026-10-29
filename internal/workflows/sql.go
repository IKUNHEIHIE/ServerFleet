package workflows

import (
	"bytes"
	"context"
)

type SQLWorkflowTx struct {
	Read       func(context.Context, string, ...any) ([]byte, error)
	Exec       func(context.Context, string, ...any) (int64, error)
	Admit      func(context.Context, WorkflowOperation) error
	SubmitTask func(context.Context, TaskIntent, WorkflowBinding) (TaskReceipt, error)
	TaskState  func(context.Context, string) (WorkflowTaskState, error)
	Audit      func(context.Context, WorkflowAudit) error
}
type SQLWorkflowReadTx struct {
	Read  func(context.Context, string, ...any) ([]byte, error)
	Admit func(context.Context, WorkflowOperation) error
}
type SQLWorkflowStore struct {
	InTx func(context.Context, func(SQLWorkflowTx) error) error
	View func(context.Context, func(SQLWorkflowReadTx) error) error
}

func WorkflowSchemaStatements() []string {
	return []string{`CREATE TABLE cx_workflows(singleton INTEGER PRIMARY KEY CHECK(singleton=1),payload BLOB NOT NULL CHECK(length(payload)>0))`, `INSERT INTO cx_workflows(singleton,payload) VALUES(1,'{"Version":1}')`}
}

const readWorkflowSQL = `SELECT payload FROM cx_workflows WHERE singleton=1 AND length(payload)>0 AND length(payload)<=8388608`

func NewSQLWorkflowBackend(sql SQLWorkflowStore) (WorkflowBackend, error) {
	if sql.InTx == nil || sql.View == nil {
		return WorkflowBackend{}, ErrWorkflowUnavailable
	}
	read := func(fn func(context.Context, string, ...any) ([]byte, error)) func(context.Context) ([]byte, error) {
		return func(ctx context.Context) ([]byte, error) {
			if fn == nil {
				return nil, ErrWorkflowUnavailable
			}
			b, err := fn(ctx, readWorkflowSQL)
			if err != nil || len(b) == 0 {
				return nil, ErrWorkflowUnavailable
			}
			if _, err = decodeWorkflow(b); err != nil {
				return nil, err
			}
			return bytes.Clone(b), nil
		}
	}
	return WorkflowBackend{InTx: func(ctx context.Context, fn func(WorkflowTx) error) error {
		return sql.InTx(ctx, func(tx SQLWorkflowTx) error {
			return fn(WorkflowTx{Read: read(tx.Read), Write: func(ctx context.Context, b []byte) error {
				if tx.Exec == nil || len(b) == 0 || len(b) > 8<<20 {
					return ErrWorkflowUnavailable
				}
				if _, err := decodeWorkflow(b); err != nil {
					return err
				}
				n, err := tx.Exec(ctx, `UPDATE cx_workflows SET payload=? WHERE singleton=1`, bytes.Clone(b))
				if err != nil || n != 1 {
					return ErrWorkflowUnavailable
				}
				return nil
			}, Admit: tx.Admit, SubmitTask: tx.SubmitTask, TaskState: tx.TaskState, Audit: tx.Audit})
		})
	}, View: func(ctx context.Context, fn func(WorkflowReadTx) error) error {
		return sql.View(ctx, func(tx SQLWorkflowReadTx) error { return fn(WorkflowReadTx{read(tx.Read), tx.Admit}) })
	}}, nil
}
