package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRenderer_FullPage(t *testing.T) {
	r, err := NewRenderer(FS)
	if err != nil {
		t.Fatalf("NewRenderer failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if err := r.Page(rec, req, "home", nil); err != nil {
		t.Fatalf("Page failed: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "<html") {
		t.Errorf("expected full page shell, got: %s", body)
	}
	if !strings.Contains(body, "Welcome") {
		t.Errorf("expected home content in body, got: %s", body)
	}
}

func TestRenderer_FragmentOnHXRequest(t *testing.T) {
	r, err := NewRenderer(FS)
	if err != nil {
		t.Fatalf("NewRenderer failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("HX-Request", "true")
	if err := r.Page(rec, req, "home", nil); err != nil {
		t.Fatalf("Page failed: %v", err)
	}

	body := rec.Body.String()
	if strings.Contains(body, "<html") {
		t.Errorf("expected fragment only (no shell), got: %s", body)
	}
	if !strings.Contains(body, "Welcome") {
		t.Errorf("expected home content in fragment, got: %s", body)
	}
}

func TestStaticHandler_ServesEmbeddedAsset(t *testing.T) {
	handler := StaticHandler(FS)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/static/app.css", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestStaticHandler_MissingAsset404s(t *testing.T) {
	handler := StaticHandler(FS)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/static/does-not-exist.css", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
