//go:build integration

// Package pgtest provides a shared, migrated Postgres 16 testcontainer for
// integration tests, with per-test isolation via container snapshot/restore.
package pgtest

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver for WithSQLDriver
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	ourpostgres "github.com/campsite-booking/campsite-booking/internal/platform/postgres"
)

var (
	once      sync.Once
	container *tcpostgres.PostgresContainer
	dsn       string
	setupErr  error
)

// ensureContainer starts the shared container exactly once: it's reused
// across every Setup call in the package. It is not explicitly terminated —
// testcontainers-go's Ryuk reaper cleans it up when the test binary exits.
func ensureContainer(t testing.TB) {
	once.Do(func() {
		ctx := context.Background()

		c, err := tcpostgres.Run(ctx, "postgres:16-alpine",
			tcpostgres.WithDatabase("campsite_test"),
			tcpostgres.WithUsername("test"),
			tcpostgres.WithPassword("test"),
			tcpostgres.WithSQLDriver("pgx"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second),
			),
		)
		if err != nil {
			setupErr = err
			return
		}
		container = c

		connStr, err := c.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			setupErr = err
			return
		}
		dsn = connStr

		if err := ourpostgres.RunMigrations(ctx, dsn); err != nil {
			setupErr = err
			return
		}

		if err := c.Snapshot(ctx); err != nil {
			setupErr = err
			return
		}
	})

	if setupErr != nil {
		t.Fatalf("pgtest: container setup failed: %v", setupErr)
	}
}

// Setup returns a pool to a migrated Postgres 16 database. Each call
// restores the container to its post-migration snapshot on test cleanup,
// isolating state between tests/subtests.
func Setup(t testing.TB) *pgxpool.Pool {
	t.Helper()
	ensureContainer(t)

	ctx := context.Background()
	t.Cleanup(func() {
		if err := container.Restore(ctx); err != nil {
			t.Errorf("pgtest: restore failed: %v", err)
		}
	})

	pool, err := ourpostgres.NewPool(ctx, ourpostgres.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("pgtest: failed to build pool: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}
