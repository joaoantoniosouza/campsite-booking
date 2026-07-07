//go:build integration

package bootstrap

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/campsite-booking/campsite-booking/internal/platform/postgres"
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

func validGetenv(dsn string) func(string) string {
	env := map[string]string{
		"DATABASE_URL":   dsn,
		"SESSION_SECRET": strings.Repeat("s", 32),
	}
	return func(key string) string { return env[key] }
}

func TestBootstrap_AbortsOnMissingConfig(t *testing.T) {
	app, err := Bootstrap(func(string) string { return "" }, nil)
	if err == nil {
		t.Fatal("expected an error for missing required config")
	}
	if app != nil {
		t.Error("expected no App to be built when config is invalid")
	}
}

func TestBootstrap_HealthzOKAgainstLiveDB(t *testing.T) {
	dsn := newTestDSN(t)

	app, err := Bootstrap(validGetenv(dsn), nil)
	if err != nil {
		t.Fatalf("Bootstrap failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	app.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from /healthz against a live DB, got %d", rec.Code)
	}
}

func TestBootstrap_HealthzServiceUnavailableWhenDBDown(t *testing.T) {
	dsn := newTestDSN(t)

	app, err := Bootstrap(validGetenv(dsn), nil)
	if err != nil {
		t.Fatalf("Bootstrap failed: %v", err)
	}

	pool, err := postgres.NewPool(context.Background(), postgres.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	pool.Close() // simulate DB being unreachable via a separately closed pool

	appDown, err := New(Deps{
		Addr:          ":0",
		Logger:        slog.Default(),
		SessionSecret: []byte(strings.Repeat("s", 32)),
		Pool:          pool,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	appDown.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 from /healthz against a closed pool, got %d", rec.Code)
	}

	_ = app // built to prove the OK path is independently reachable too
}

type capturingHandler struct {
	records []slog.Record
}

func (h *capturingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}
func (h *capturingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *capturingHandler) WithGroup(_ string) slog.Handler      { return h }

func TestBootstrap_RequestEmitsStructuredLogRecord(t *testing.T) {
	dsn := newTestDSN(t)

	pool, err := postgres.NewPool(context.Background(), postgres.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	defer pool.Close()

	captured := &capturingHandler{}
	logger := slog.New(captured)

	app, err := New(Deps{
		Addr:          ":0",
		Logger:        logger,
		SessionSecret: []byte(strings.Repeat("s", 32)),
		Pool:          pool,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	app.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from /, got %d", rec.Code)
	}
	if len(captured.records) == 0 {
		t.Fatal("expected at least one structured log record for the request")
	}
}
