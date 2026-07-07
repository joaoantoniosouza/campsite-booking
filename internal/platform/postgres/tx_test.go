//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("testdb"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}
	t.Cleanup(func() {
		_ = container.Terminate(ctx)
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	pool, err := NewPool(ctx, Config{DSN: dsn})
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS tx_test (id INT PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatalf("failed to create temp table: %v", err)
	}

	return pool
}

func TestWithTx_CommitsOnNilReturn(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)

	err := WithTx(ctx, pool, func(txCtx context.Context) error {
		_, execErr := Executor(txCtx, pool).Exec(txCtx, `INSERT INTO tx_test (id, value) VALUES (1, 'committed')`)
		return execErr
	})
	if err != nil {
		t.Fatalf("WithTx returned error: %v", err)
	}

	var value string
	if err := pool.QueryRow(ctx, `SELECT value FROM tx_test WHERE id = 1`).Scan(&value); err != nil {
		t.Fatalf("expected committed row to be visible: %v", err)
	}
	if value != "committed" {
		t.Errorf("expected value 'committed', got %q", value)
	}
}

func TestWithTx_RollsBackOnError(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)

	wantErr := errors.New("intentional failure")
	err := WithTx(ctx, pool, func(txCtx context.Context) error {
		_, execErr := Executor(txCtx, pool).Exec(txCtx, `INSERT INTO tx_test (id, value) VALUES (2, 'rolled-back')`)
		if execErr != nil {
			return execErr
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected WithTx to return the wrapped error, got: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tx_test WHERE id = 2`).Scan(&count); err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected rollback to leave no row, found %d", count)
	}
}

func TestWithTx_RollsBackAndRePanicsOnPanic(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected WithTx to re-panic")
		}

		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM tx_test WHERE id = 3`).Scan(&count); err != nil {
			t.Fatalf("query failed: %v", err)
		}
		if count != 0 {
			t.Errorf("expected rollback after panic to leave no row, found %d", count)
		}
	}()

	_ = WithTx(ctx, pool, func(txCtx context.Context) error {
		_, _ = Executor(txCtx, pool).Exec(txCtx, `INSERT INTO tx_test (id, value) VALUES (3, 'panicked')`)
		panic("boom")
	})
}

func TestExecutor_ResolvesTxInsideWithTxAndPoolOutside(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)

	outside := Executor(ctx, pool)
	if outside != Querier(pool) {
		t.Errorf("expected Executor outside WithTx to return the pool")
	}

	_ = WithTx(ctx, pool, func(txCtx context.Context) error {
		inside := Executor(txCtx, pool)
		if inside == Querier(pool) {
			t.Errorf("expected Executor inside WithTx to return the tx, not the pool")
		}
		return nil
	})
}
