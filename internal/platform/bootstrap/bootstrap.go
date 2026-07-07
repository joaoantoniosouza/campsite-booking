package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gorilla/sessions"

	"github.com/campsite-booking/campsite-booking/internal/platform/config"
	"github.com/campsite-booking/campsite-booking/internal/platform/health"
	"github.com/campsite-booking/campsite-booking/internal/platform/httpx"
	applog "github.com/campsite-booking/campsite-booking/internal/platform/log"
	"github.com/campsite-booking/campsite-booking/internal/platform/postgres"
	"github.com/campsite-booking/campsite-booking/internal/platform/web"
)

// Deps are the seams other M0 features (config-runtime, data-migration)
// inject into the composition root.
type Deps struct {
	Addr          string
	Logger        *slog.Logger
	SessionSecret []byte
	Modules       []Module      // empty for the skeleton
	Pool          health.Pinger // seam ← data-migration; nil skips /healthz
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
	router.Use(applog.Middleware(deps.Logger))

	router.Get("/", web.Home(renderer))
	router.Handle("/static/*", web.StaticHandler(web.FS))

	if deps.Pool != nil {
		router.Get("/healthz", health.Handler(deps.Pool))
	}

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

// Bootstrap is the env-driven composition root entrypoint: it loads Config
// first (aborting with no listener opened on any missing/invalid var),
// builds the logger and Postgres pool, then delegates to New.
func Bootstrap(getenv func(string) string, modules []Module) (*App, error) {
	cfg, err := config.LoadFrom(getenv)
	if err != nil {
		return nil, err
	}

	logger := applog.New(cfg.Log)
	slog.SetDefault(logger)

	pool, err := postgres.NewPool(context.Background(), postgres.Config{DSN: cfg.DatabaseURL})
	if err != nil {
		return nil, fmt.Errorf("build postgres pool: %w", err)
	}

	return New(Deps{
		Addr:          fmt.Sprintf(":%d", cfg.HTTP.Port),
		Logger:        logger,
		SessionSecret: []byte(cfg.SessionSecret),
		Modules:       modules,
		Pool:          pool,
	})
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
