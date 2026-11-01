package server

import (
	"context"

	"serverfleet.local/serverfleet/internal/ai"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/storage"
)

// Policies remain explicit administrator operations. No policy means no model
// I/O. The budget fence, typed event and common audit use one durable DS Tx.
func newAIBudgetStore(db *storage.DB, recorder *audit.SQLRecorder) (*ai.SQLGenerationBudgetStore, error) {
	return ai.NewSQLGenerationBudgetStore(ai.GenerationBudgetSQLBackend{InTx: func(ctx context.Context, fn func(ai.GenerationBudgetSQLStatements) error) error {
		return db.InTx(ctx, func(txctx context.Context, tx storage.Tx) error {
			exec := func(ctx context.Context, query string, args ...any) (int64, error) {
				result, err := tx.Exec(ctx, query, args...)
				if err != nil {
					return 0, err
				}
				return result.RowsAffected()
			}
			return fn(ai.GenerationBudgetSQLStatements{
				Read: func(ctx context.Context, query string, args ...any) ([]byte, error) {
					var raw []byte
					err := tx.QueryRow(ctx, query, args...).Scan(&raw)
					return raw, err
				},
				Exec: exec,
				AppendAudit: func(ctx context.Context, events []ai.GenerationBudgetAudit) error {
					if err := ai.AppendGenerationBudgetAudit(ctx, exec, events); err != nil {
						return err
					}
					for _, event := range events {
						requestID, source := event.OperationID, audit.SourceAI
						if requestID == "" {
							requestID, source = newRequestID(), audit.SourceSystem
						}
						if err := recorder.AppendTx(ctx, tx, audit.Event{SubjectID: event.ActorID, Action: event.Action, Result: "success", RequestID: requestID, NodeIDs: []string{}, Source: source, At: event.At}); err != nil {
							return err
						}
					}
					return nil
				},
			})
		})
	}})
}
