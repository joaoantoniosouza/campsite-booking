//go:build integration

package main

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

func TestRun_UpThenVersionHappyPath(t *testing.T) {
	dsn := newTestDSN(t)

	if err := run([]string{"up"}, dsn); err != nil {
		t.Fatalf("run up failed: %v", err)
	}
	if err := run([]string{"version"}, dsn); err != nil {
		t.Fatalf("run version failed: %v", err)
	}
}

func TestRun_UnknownSubcommandOrMissingDSNErrors(t *testing.T) {
	dsn := newTestDSN(t)

	if err := run([]string{"bogus"}, dsn); err == nil {
		t.Error("expected an error for an unknown subcommand")
	}
	if err := run([]string{"up"}, ""); err == nil {
		t.Error("expected an error for a missing DSN")
	}
}
