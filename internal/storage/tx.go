package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Tx is a write transaction started with BEGIN IMMEDIATE. All statements must go
// through the Tx value; using the pool inside fn would deadlock on the in-process
// writer lock. A transaction is always finished: Commit on success, Rollback on
// error, cancellation or panic.
type Tx interface {
	Queryer
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

type sqlTx struct {
	conn     *sql.Conn
	finished bool
}

// InTx runs fn inside one immediate write transaction. Writers are serialized per
// handle: concurrent callers wait for the current transaction (honoring ctx)
// instead of racing into SQLITE_BUSY. Reads from the pool remain concurrent.
//
// Cancellation semantics: if ctx is canceled before BEGIN the transaction never
// starts; if it is canceled during fn the callback may observe ctx itself, and
// InTx rolls back and returns the context error even when fn returned nil. A
// rolled-back transaction never reports success.
func (db *DB) InTx(ctx context.Context, fn func(context.Context, Tx) error) (err error) {
	if db == nil || db.db == nil {
		return ErrClosed
	}
	if ctx == nil {
		return fmt.Errorf("%w: context is required", ErrInvalidConfig)
	}
	if fn == nil {
		return fmt.Errorf("%w: transaction callback is required", ErrInvalidConfig)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case db.writer <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-db.writer }()

	conn, err := db.db.Conn(ctx)
	if err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		_ = conn.Close()
		return fmt.Errorf("storage: begin immediate: %w", err)
	}
	tx := &sqlTx{conn: conn}
	closeConn := true
	defer func() {
		if recovered := recover(); recovered != nil {
			rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_ = tx.rollback(rollbackCtx)
			cancel()
			if closeConn {
				_ = conn.Close()
			}
			panic(recovered)
		}
		if !tx.finished {
			rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_ = tx.rollback(rollbackCtx)
			cancel()
		}
		if closeConn {
			_ = conn.Close()
		}
	}()

	if err := fn(ctx, tx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	_ = conn.Close()
	closeConn = false
	return nil
}

func (tx *sqlTx) Exec(ctx context.Context, query string, args ...any) (Result, error) {
	if tx == nil || tx.finished {
		return nil, ErrClosed
	}
	return tx.conn.ExecContext(ctx, query, args...)
}

func (tx *sqlTx) Query(ctx context.Context, query string, args ...any) (Rows, error) {
	if tx == nil || tx.finished {
		return nil, ErrClosed
	}
	rows, err := tx.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (tx *sqlTx) QueryRow(ctx context.Context, query string, args ...any) Row {
	if tx == nil || tx.finished {
		return errorRow{ErrClosed}
	}
	return tx.conn.QueryRowContext(ctx, query, args...)
}

func (tx *sqlTx) Commit(ctx context.Context) error {
	if tx == nil || tx.finished {
		return ErrClosed
	}
	tx.finished = true
	_, err := tx.conn.ExecContext(ctx, "COMMIT")
	return err
}

func (tx *sqlTx) Rollback(ctx context.Context) error {
	if tx == nil || tx.finished {
		return ErrClosed
	}
	return tx.rollback(ctx)
}

func (tx *sqlTx) rollback(ctx context.Context) error {
	if tx == nil || tx.finished {
		return ErrClosed
	}
	tx.finished = true
	_, err := tx.conn.ExecContext(ctx, "ROLLBACK")
	return err
}

var _ Tx = (*sqlTx)(nil)

// IsCanceled reports whether err is a context cancellation or deadline.
func IsCanceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
