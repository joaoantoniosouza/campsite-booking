package bootstrap

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

type fakeModule struct {
	mounted bool
}

func (f *fakeModule) Name() string { return "fake" }
func (f *fakeModule) Mount(r chi.Router) {
	f.mounted = true
	r.Get("/fake", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fake module route"))
	})
}

func TestNew_RootReturns200(t *testing.T) {
	app, err := New(Deps{Addr: ":0", SessionSecret: []byte("test-secret-32-bytes-long-enough")})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	app.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestNew_StaticReturns200(t *testing.T) {
	app, err := New(Deps{Addr: ":0", SessionSecret: []byte("test-secret-32-bytes-long-enough")})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/static/app.css", nil)
	app.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestNew_UnknownRouteReturns404(t *testing.T) {
	app, err := New(Deps{Addr: ":0", SessionSecret: []byte("test-secret-32-bytes-long-enough")})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
	app.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestNew_ModuleMounted(t *testing.T) {
	fake := &fakeModule{}
	app, err := New(Deps{
		Addr:          ":0",
		SessionSecret: []byte("test-secret-32-bytes-long-enough"),
		Modules:       []Module{fake},
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if !fake.mounted {
		t.Fatal("expected fake module's Mount to have been called")
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/fake", nil)
	app.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from module-mounted route, got %d", rec.Code)
	}
}
