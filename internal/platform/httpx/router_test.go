package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/sessions"
)

func TestNewRouter_ChainAppliedInOrder(t *testing.T) {
	store := sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough"))

	var sawRequestID, sawSession, recovered bool

	r := NewRouter(RouterDeps{SessionStore: store})
	r.Get("/probe", func(w http.ResponseWriter, req *http.Request) {
		if middleware.GetReqID(req.Context()) != "" {
			sawRequestID = true
		}
		if SessionFromContext(req.Context()) != nil {
			sawSession = true
		}
		w.WriteHeader(http.StatusOK)
	})
	r.Get("/panics", func(w http.ResponseWriter, req *http.Request) {
		panic("boom")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from probe, got %d", rec.Code)
	}
	if !sawRequestID {
		t.Errorf("expected RequestID middleware applied (request id present in context)")
	}
	if !sawSession {
		t.Errorf("expected Session middleware applied (session present in context)")
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/panics", nil)
	// If Recovery isn't in the chain, this call re-panics and fails the test.
	r.ServeHTTP(rec2, req2)
	recovered = rec2.Code == http.StatusInternalServerError
	if !recovered {
		t.Errorf("expected Recovery middleware applied (500 on panic), got %d", rec2.Code)
	}
}

func TestNewRouter_NotFoundWired(t *testing.T) {
	store := sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough"))
	called := false

	r := NewRouter(RouterDeps{
		SessionStore: store,
		NotFound: func(w http.ResponseWriter, req *http.Request) {
			called = true
			w.WriteHeader(http.StatusNotFound)
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
	r.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected injected NotFound handler to be invoked")
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
