package ai

import (
	"context"
	"time"
)

type GenerationBudgetSQLStatements struct {
	Read func(context.Context, string, ...any) ([]byte, error)
	Exec func(context.Context, string, ...any) (int64, error)
	// Must use THIS SAME DS transaction. Missing audit fails closed.
	AppendAudit func(context.Context, []GenerationBudgetAudit) error
}
type GenerationBudgetSQLBackend struct {
	InTx func(context.Context, func(GenerationBudgetSQLStatements) error) error
}
type SQLGenerationBudgetStore struct{ backend GenerationBudgetSQLBackend }

func NewSQLGenerationBudgetStore(backend GenerationBudgetSQLBackend) (*SQLGenerationBudgetStore, error) {
	if backend.InTx == nil {
		return nil, ErrSessionUnavailable
	}
	return &SQLGenerationBudgetStore{backend}, nil
}
func GenerationBudgetSchemaStatements() []string {
	return []string{
		`CREATE TABLE cx_ai_generation_budget(singleton INTEGER PRIMARY KEY CHECK(singleton=1),payload BLOB NOT NULL CHECK(length(payload)>0))`,
		`INSERT INTO cx_ai_generation_budget VALUES(1,'{"Version":1}')`,
		`CREATE TABLE cx_ai_budget_audit(id INTEGER PRIMARY KEY AUTOINCREMENT,actor_id TEXT NOT NULL,owner_id TEXT NOT NULL,profile_id TEXT NOT NULL,operation_id TEXT NOT NULL,window_id TEXT NOT NULL,action TEXT NOT NULL,plan_digest TEXT NOT NULL,policy_revision INTEGER NOT NULL,input_bytes INTEGER NOT NULL,output_tokens INTEGER NOT NULL,at TEXT NOT NULL)`,
		`CREATE UNIQUE INDEX cx_ai_budget_operation_audit ON cx_ai_budget_audit(operation_id) WHERE operation_id<>''`,
	}
}
func (s *SQLGenerationBudgetStore) Update(ctx context.Context, fn func([]byte) ([]byte, []GenerationBudgetAudit, error)) error {
	if s == nil || ctx == nil || ctx.Err() != nil || fn == nil {
		return ErrSessionUnavailable
	}
	return s.backend.InTx(ctx, func(tx GenerationBudgetSQLStatements) error {
		if tx.Read == nil || tx.Exec == nil || tx.AppendAudit == nil {
			return ErrSessionUnavailable
		}
		raw, err := tx.Read(ctx, `SELECT payload FROM cx_ai_generation_budget WHERE singleton=1 AND length(payload)<=4194304`)
		if err != nil || len(raw) == 0 {
			return ErrSessionUnavailable
		}
		if _, err = decodeBudget(raw); err != nil {
			return err
		}
		next, events, err := fn(raw)
		if err != nil {
			return err
		}
		if len(events) != 1 {
			return ErrSessionUnavailable
		}
		if _, err = decodeBudget(next); err != nil {
			return err
		}
		if err = tx.AppendAudit(ctx, events); err != nil {
			return err
		}
		rows, err := tx.Exec(ctx, `UPDATE cx_ai_generation_budget SET payload=? WHERE singleton=1`, next)
		if err != nil || rows != 1 {
			return ErrSessionUnavailable
		}
		return ctx.Err()
	})
}

// AppendGenerationBudgetAudit is a typed, payload-free same-transaction writer.
func AppendGenerationBudgetAudit(ctx context.Context, exec func(context.Context, string, ...any) (int64, error), events []GenerationBudgetAudit) error {
	if exec == nil || ctx == nil {
		return ErrSessionUnavailable
	}
	for _, e := range events {
		if !exactID(e.ActorID) || !exactID(e.OwnerID) || !exactID(e.ProfileID) || !exactID(e.WindowID) || e.PolicyRevision == 0 || e.At.IsZero() || (e.Action != "ai.budget.configure" && e.Action != "ai.budget.reserve") || e.Action == "ai.budget.reserve" && (!exactID(e.OperationID) || !validBudgetDigest(e.Digest) || e.OutputTokens != ProviderMaxOutputTokens) {
			return ErrSessionUnavailable
		}
		rows, err := exec(ctx, `INSERT INTO cx_ai_budget_audit(actor_id,owner_id,profile_id,operation_id,window_id,action,plan_digest,policy_revision,input_bytes,output_tokens,at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, e.ActorID, e.OwnerID, e.ProfileID, e.OperationID, e.WindowID, e.Action, e.Digest, e.PolicyRevision, e.InputBytes, e.OutputTokens, e.At.UTC().Format(time.RFC3339Nano))
		if err != nil || rows != 1 {
			return ErrSessionUnavailable
		}
	}
	return ctx.Err()
}
