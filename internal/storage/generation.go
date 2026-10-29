package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	ErrGenerationConflict = errors.New("storage: generation conflict")
	ErrInvalidScope       = errors.New("storage: invalid generation scope")
)

// Generation is one durable monotonic counter. Authentication and authorization
// owners use it for credential/session generations: a revocation advances the
// counter, and stale writes compare against the expected value.
type Generation struct {
	Scope     string
	Value     uint64
	UpdatedAt string
}

// Generations stores monotonic counters keyed by an opaque scope string. It is
// infrastructure only; auth owns the meaning of each scope and its tables.
type Generations struct{ db *DB }

func NewGenerations(db *DB) *Generations { return &Generations{db: db} }

const generationsTable = `CREATE TABLE IF NOT EXISTS storage_generations (
	scope TEXT PRIMARY KEY,
	value INTEGER NOT NULL CHECK (value > 0),
	updated_at TEXT NOT NULL
)`

// EnsureSchema creates the shared generation table. It is idempotent and safe to
// call after migrations; the ledger still records domain migrations separately.
func (g *Generations) EnsureSchema(ctx context.Context) error {
	if g == nil || g.db == nil {
		return ErrClosed
	}
	_, err := g.db.Exec(ctx, generationsTable)
	return err
}

// Current returns the current generation, or zero when the scope has never been
// advanced. A zero generation is absence, not an authorization value.
func (g *Generations) Current(ctx context.Context, scope string) (Generation, error) {
	if g == nil || g.db == nil {
		return Generation{}, ErrClosed
	}
	if err := validateScope(scope); err != nil {
		return Generation{}, err
	}
	var value int64
	var updated string
	err := g.db.QueryRow(ctx, "SELECT value, updated_at FROM storage_generations WHERE scope = ?", scope).Scan(&value, &updated)
	if IsNotFound(err) {
		return Generation{Scope: scope}, nil
	}
	if err != nil {
		return Generation{}, err
	}
	if value <= 0 {
		return Generation{}, fmt.Errorf("storage: generation %q has invalid value %d", scope, value)
	}
	return Generation{Scope: scope, Value: uint64(value), UpdatedAt: updated}, nil
}

// Advance atomically moves scope from expected to expected+1. Passing expected 0
// creates the first generation only when the scope is absent, so two concurrent
// initializers cannot both succeed. A stale expected value returns
// ErrGenerationConflict without changing state.
func (g *Generations) Advance(ctx context.Context, scope string, expected uint64) (Generation, error) {
	if g == nil || g.db == nil {
		return Generation{}, ErrClosed
	}
	if err := validateScope(scope); err != nil {
		return Generation{}, err
	}
	var result Generation
	err := g.db.InTx(ctx, func(txCtx context.Context, tx Tx) error {
		now := nowUTC()
		if expected == 0 {
			_, err := tx.Exec(txCtx,
				"INSERT INTO storage_generations (scope, value, updated_at) VALUES (?, 1, ?)", scope, now)
			if err != nil {
				if IsUniqueViolation(err) {
					return ErrGenerationConflict
				}
				return err
			}
			result = Generation{Scope: scope, Value: 1, UpdatedAt: now}
			return nil
		}
		outcome, err := tx.Exec(txCtx,
			"UPDATE storage_generations SET value = value + 1, updated_at = ? WHERE scope = ? AND value = ?",
			now, scope, expected)
		if err != nil {
			return err
		}
		affected, err := outcome.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return ErrGenerationConflict
		}
		result = Generation{Scope: scope, Value: expected + 1, UpdatedAt: now}
		return nil
	})
	if err != nil {
		return Generation{}, err
	}
	return result, nil
}

func validateScope(scope string) error {
	if scope == "" || len(scope) > 200 || !utf8.ValidString(scope) || strings.TrimSpace(scope) != scope {
		return ErrInvalidScope
	}
	if strings.ContainsFunc(scope, unicode.IsControl) {
		return ErrInvalidScope
	}
	return nil
}
