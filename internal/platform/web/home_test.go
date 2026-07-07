package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHome_FullPage(t *testing.T) {
	r, err := NewRenderer(FS)
	if err != nil {
		t.Fatalf("NewRenderer failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	Home(r).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<title>") {
		t.Errorf("expected <title> in body, got: %s", body)
	}
	if !strings.Contains(body, "htmx.min.js") {
		t.Errorf("expected htmx.min.js script tag in body, got: %s", body)
	}
}

func TestHome_Fragment(t *testing.T) {
	r, err := NewRenderer(FS)
	if err != nil {
		t.Fatalf("NewRenderer failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("HX-Request", "true")
	Home(r).ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "<html") {
		t.Errorf("expected fragment only (no shell), got: %s", body)
	}
}

func TestNotFound_Renders404(t *testing.T) {
	r, err := NewRenderer(FS)
	if err != nil {
		t.Fatalf("NewRenderer failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	NotFound(r).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if rec.Body.Len() == 0 {
		t.Errorf("expected a rendered body, got empty")
	}
}
