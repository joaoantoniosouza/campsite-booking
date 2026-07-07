package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type txContextKey struct{}

// Querier is the common surface satisfied by both *pgxpool.Pool and pgx.Tx,
// letting repositories run queries without knowing whether they're inside a
// transaction.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// WithTx begins a transaction on pool and stores it in ctx (ambient, not
// passed as an argument) before invoking fn. It commits when fn returns nil,
// rolls back when fn returns an error, and rolls back then re-panics if fn
// panics.
func WithTx(ctx context.Context, pool *pgxpool.Pool, fn func(ctx context.Context) error) (err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	txCtx := context.WithValue(ctx, txContextKey{}, tx)

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return
		}
		err = tx.Commit(ctx)
	}()

	err = fn(txCtx)
	return err
}

// Executor resolves the ambient pgx.Tx stored in ctx by WithTx, falling
// back to pool when no transaction is in flight.
func Executor(ctx context.Context, pool *pgxpool.Pool) Querier {
	if tx, ok := ctx.Value(txContextKey{}).(pgx.Tx); ok {
		return tx
	}
	return pool
}
