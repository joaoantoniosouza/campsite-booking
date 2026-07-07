package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gorilla/sessions"

	"github.com/campsite-booking/campsite-booking/internal/platform/httpx"
	"github.com/campsite-booking/campsite-booking/internal/platform/web"
)

// Deps are the seams other M0 features (config-runtime, data-migration)
// inject into the composition root.
type Deps struct {
	Addr          string
	Logger        *slog.Logger
	SessionSecret []byte
	Modules       []Module // empty for the skeleton
}

// App holds the built HTTP handler and server for the running application.
type App struct {
	handler http.Handler
	server  *httpx.Server
}

// New is the composition root: it builds the renderer, router, and server,
// mounts the base routes plus every injected Module, and wires cross-cutting
// seams. It is the only place in the codebase with concrete platform types.
func New(deps Deps) (*App, error) {
	renderer, err := web.NewRenderer(web.FS)
	if err != nil {
		return nil, fmt.Errorf("build renderer: %w", err)
	}

	store := sessions.NewCookieStore(deps.SessionSecret)

	router := httpx.NewRouter(httpx.RouterDeps{
		Logger:       deps.Logger,
		SessionStore: store,
		NotFound:     web.NotFound(renderer),
	})

	router.Get("/", web.Home(renderer))
	router.Handle("/static/*", web.StaticHandler(web.FS))

	for _, m := range deps.Modules {
		m.Mount(router)
	}

	return &App{
		handler: router,
		server: &httpx.Server{
			Addr:    deps.Addr,
			Handler: router,
		},
	}, nil
}

// Handler exposes the built http.Handler, primarily for httptest.
func (a *App) Handler() http.Handler {
	return a.handler
}

// Run delegates to the underlying httpx.Server, serving until ctx is
// cancelled and then draining gracefully.
func (a *App) Run(ctx context.Context) error {
	return a.server.Run(ctx)
}
