package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/sessions"
)

func TestSession_NewSessionInContext(t *testing.T) {
	store := sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough"))

	var captured *sessions.Session
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = SessionFromContext(r.Context())
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	Session(store)(next).ServeHTTP(rec, req)

	if captured == nil {
		t.Fatal("expected a session in context, got nil")
	}
	if !captured.IsNew {
		t.Errorf("expected a fresh session for a request with no cookie")
	}
}

func TestSession_RoundTrip(t *testing.T) {
	store := sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough"))

	setter := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := SessionFromContext(r.Context())
		s.Values["key"] = "value"
		s.Save(r, w)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	Session(store)(setter).ServeHTTP(rec, req)

	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected a session cookie to be set")
	}

	var readBack *sessions.Session
	reader := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		readBack = SessionFromContext(r.Context())
	})

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.AddCookie(cookies[0])
	Session(store)(reader).ServeHTTP(rec2, req2)

	if readBack.IsNew {
		t.Fatal("expected an existing session to be loaded from cookie")
	}
	if readBack.Values["key"] != "value" {
		t.Errorf("expected key=value in round-tripped session, got %v", readBack.Values["key"])
	}
}

func TestSession_TamperedCookieYieldsFreshSession(t *testing.T) {
	store := sessions.NewCookieStore([]byte("test-secret-32-bytes-long-enough"))

	var captured *sessions.Session
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = SessionFromContext(r.Context())
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "tampered-garbage"})
	Session(store)(next).ServeHTTP(rec, req)

	if captured == nil {
		t.Fatal("expected a session in context, got nil")
	}
	if !captured.IsNew {
		t.Errorf("expected a fresh session when cookie is tampered/invalid")
	}
}
