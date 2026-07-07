package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakePinger struct {
	err   error
	delay time.Duration
}

func (f *fakePinger) Ping(ctx context.Context) error {
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
			return f.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return f.err
}

func TestHandler_OKOnSuccessfulPing(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	Handler(&fakePinger{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if body["status"] != "ok" || body["db"] != "ok" {
		t.Errorf("expected {status:ok,db:ok}, got %v", body)
	}
}

func TestHandler_ServiceUnavailableOnPingError(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	Handler(&fakePinger{err: errors.New("db down")}).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if body["status"] != "degraded" || body["db"] != "down" {
		t.Errorf("expected {status:degraded,db:down}, got %v", body)
	}
}

func TestHandler_ServiceUnavailableOnTimeout(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	Handler(&fakePinger{delay: pingTimeout + time.Second}).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 on timeout, got %d", rec.Code)
	}
}
