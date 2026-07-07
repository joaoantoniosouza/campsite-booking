package log

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
)

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

func fieldsOf(r slog.Record) map[string]any {
	fields := map[string]any{}
	r.Attrs(func(a slog.Attr) bool {
		fields[a.Key] = a.Value.Any()
		return true
	})
	return fields
}

func TestMiddleware_EmitsOneRecordWithFields(t *testing.T) {
	h := &capturingHandler{}
	logger := slog.New(h)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello"))
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	Middleware(logger)(next).ServeHTTP(rec, req)

	if len(h.records) != 1 {
		t.Fatalf("expected exactly 1 log record, got %d", len(h.records))
	}
	fields := fieldsOf(h.records[0])
	if fields["method"] != http.MethodGet {
		t.Errorf("expected method GET, got %v", fields["method"])
	}
	if fields["path"] != "/foo" {
		t.Errorf("expected path /foo, got %v", fields["path"])
	}
	if fmt.Sprint(fields["status"]) != fmt.Sprint(http.StatusOK) {
		t.Errorf("expected status 200, got %v", fields["status"])
	}
	if fmt.Sprint(fields["bytes"]) != fmt.Sprint(len("hello")) {
		t.Errorf("expected bytes=5, got %v", fields["bytes"])
	}
	if _, ok := fields["duration_ms"]; !ok {
		t.Error("expected duration_ms field to be present")
	}
}

func TestMiddleware_IncludesRequestIDWhenPresent(t *testing.T) {
	h := &capturingHandler{}
	logger := slog.New(h)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.RequestIDKey, "req-123"))

	Middleware(logger)(next).ServeHTTP(rec, req)

	fields := fieldsOf(h.records[0])
	if fields["request_id"] != "req-123" {
		t.Errorf("expected request_id=req-123, got %v", fields["request_id"])
	}
}

func TestMiddleware_OmitsRequestIDWhenAbsent(t *testing.T) {
	h := &capturingHandler{}
	logger := slog.New(h)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	Middleware(logger)(next).ServeHTTP(rec, req)

	fields := fieldsOf(h.records[0])
	if _, ok := fields["request_id"]; ok {
		t.Errorf("expected no request_id field when absent, got %v", fields["request_id"])
	}
}

func TestFromContext_FallsBackToDefault(t *testing.T) {
	if FromContext(context.Background()) != slog.Default() {
		t.Error("expected FromContext to fall back to slog.Default() when none injected")
	}

	logger := slog.New(&capturingHandler{})
	ctx := IntoContext(context.Background(), logger)
	if FromContext(ctx) != logger {
		t.Error("expected FromContext to return the injected logger")
	}
}
