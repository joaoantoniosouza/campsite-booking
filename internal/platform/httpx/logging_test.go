package httpx

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
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

func TestLogging_LogsFields(t *testing.T) {
	h := &capturingHandler{}
	logger := slog.New(h)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	Logging(logger)(next).ServeHTTP(rec, req)

	if len(h.records) != 1 {
		t.Fatalf("expected 1 log record, got %d", len(h.records))
	}
	fields := fieldsOf(h.records[0])
	if fields["method"] != http.MethodGet {
		t.Errorf("expected method GET, got %v", fields["method"])
	}
	if fields["path"] != "/foo" {
		t.Errorf("expected path /foo, got %v", fields["path"])
	}
	if _, ok := fields["duration"]; !ok {
		t.Errorf("expected duration field to be present")
	}
}

func TestLogging_CapturesNonOKStatus(t *testing.T) {
	h := &capturingHandler{}
	logger := slog.New(h)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	Logging(logger)(next).ServeHTTP(rec, req)

	fields := fieldsOf(h.records[0])
	if fmt.Sprint(fields["status"]) != fmt.Sprint(http.StatusTeapot) {
		t.Errorf("expected status 418, got %v", fields["status"])
	}
}
