package httpx

import (
	"context"
	"net/http"

	"github.com/gorilla/sessions"
)

type sessionContextKey struct{}

const sessionCookieName = "campsite_session"

// Session loads (or creates, if missing/tampered) a signed cookie session
// using store and places it in the request context for downstream handlers.
func Session(store sessions.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session, _ := store.Get(r, sessionCookieName)
			ctx := context.WithValue(r.Context(), sessionContextKey{}, session)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// SessionFromContext returns the *sessions.Session placed by Session, or nil
// if none is present (e.g. the middleware was not applied).
func SessionFromContext(ctx context.Context) *sessions.Session {
	s, _ := ctx.Value(sessionContextKey{}).(*sessions.Session)
	return s
}
