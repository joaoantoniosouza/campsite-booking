//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func newTestDSN(t *testing.T) string {
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
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}
	return dsn
}

func TestMigrate_UpIsIdempotent(t *testing.T) {
	dsn := newTestDSN(t)

	mg, err := NewMigrator(dsn)
	if err != nil {
		t.Fatalf("NewMigrator failed: %v", err)
	}
	defer mg.Close()

	if err := mg.Up(); err != nil {
		t.Fatalf("first Up failed: %v", err)
	}

	version, dirty, err := mg.Version()
	if err != nil {
		t.Fatalf("Version failed: %v", err)
	}
	if version != 1 || dirty {
		t.Fatalf("expected version=1, dirty=false, got version=%d dirty=%v", version, dirty)
	}

	// Second Up must succeed (ErrNoChange treated as success).
	if err := mg.Up(); err != nil {
		t.Fatalf("second Up (no-op) failed: %v", err)
	}

	pool, err := NewPool(context.Background(), Config{DSN: dsn})
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	defer pool.Close()

	var extExists bool
	err = pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'btree_gist')`).Scan(&extExists)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if !extExists {
		t.Error("expected btree_gist extension to be present after Up")
	}
}

func TestMigrate_DownRevertsAndVersionOnEmptyDB(t *testing.T) {
	dsn := newTestDSN(t)

	mg, err := NewMigrator(dsn)
	if err != nil {
		t.Fatalf("NewMigrator failed: %v", err)
	}
	defer mg.Close()

	version, dirty, err := mg.Version()
	if err != nil {
		t.Fatalf("Version on empty DB failed: %v", err)
	}
	if version != 0 || dirty {
		t.Fatalf("expected version=0, dirty=false on empty DB, got version=%d dirty=%v", version, dirty)
	}

	if err := mg.Up(); err != nil {
		t.Fatalf("Up failed: %v", err)
	}
	if err := mg.Down(); err != nil {
		t.Fatalf("Down failed: %v", err)
	}

	version, _, err = mg.Version()
	if err != nil {
		t.Fatalf("Version after Down failed: %v", err)
	}
	if version != 0 {
		t.Errorf("expected version=0 after Down, got %d", version)
	}

	pool, err := NewPool(context.Background(), Config{DSN: dsn})
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	defer pool.Close()

	var extExists bool
	err = pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'btree_gist')`).Scan(&extExists)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if extExists {
		t.Error("expected btree_gist extension to be gone after Down")
	}
}

func TestRunMigrations_SucceedsOnCleanDB(t *testing.T) {
	dsn := newTestDSN(t)

	if err := RunMigrations(context.Background(), dsn); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}
}

func TestRunMigrations_ErrorsOnDirtyState(t *testing.T) {
	dsn := newTestDSN(t)

	mg, err := NewMigrator(dsn)
	if err != nil {
		t.Fatalf("NewMigrator failed: %v", err)
	}
	if err := mg.Up(); err != nil {
		t.Fatalf("Up failed: %v", err)
	}
	mg.Close()

	// Simulate a failed prior migration by marking schema_migrations dirty.
	pool, err := NewPool(context.Background(), Config{DSN: dsn})
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE schema_migrations SET dirty = true`); err != nil {
		t.Fatalf("failed to mark dirty: %v", err)
	}
	pool.Close()

	if err := RunMigrations(context.Background(), dsn); err == nil {
		t.Fatal("expected RunMigrations to error on a dirty schema state")
	}
}
