package httpx

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/sessions"
)

// RouterDeps are the dependencies needed to assemble the base router.
type RouterDeps struct {
	Logger       *slog.Logger
	SessionStore sessions.Store
	NotFound     http.HandlerFunc
}

// NewRouter assembles a chi.Router with the middleware chain applied in
// order: RequestID → Recovery → Logging → Session. The injected NotFound
// handler is wired for unmatched routes.
func NewRouter(deps RouterDeps) chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(Recovery(deps.Logger))
	r.Use(Logging(deps.Logger))
	r.Use(Session(deps.SessionStore))

	if deps.NotFound != nil {
		r.NotFound(deps.NotFound)
	}

	return r
}
