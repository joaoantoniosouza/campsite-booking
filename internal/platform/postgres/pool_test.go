package postgres

import (
	"context"
	"testing"
	"time"
)

func TestNewPool_InvalidDSNReturnsError(t *testing.T) {
	_, err := NewPool(context.Background(), Config{DSN: "not-a-valid-dsn://???"})
	if err == nil {
		t.Fatal("expected an error for an invalid DSN, got nil")
	}
}

func TestNewPool_UnreachableHostReturnsErrorWithinTimeout(t *testing.T) {
	start := time.Now()
	_, err := NewPool(context.Background(), Config{
		DSN:            "postgres://user:pass@127.0.0.1:1/nonexistent",
		ConnectTimeout: 500 * time.Millisecond,
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error for an unreachable host, got nil")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("expected NewPool to fail within a bounded time, took %s", elapsed)
	}
}
