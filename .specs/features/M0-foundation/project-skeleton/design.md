# Project Skeleton & Module Boundaries Design

**Spec**: `.specs/features/M0-foundation/project-skeleton/spec.md`
**Status**: Draft

---

## Architecture Overview

This feature builds the **technical shell** of the modular monolith: `main` → composition root →
platform (router + middleware + template renderer + graceful server), plus the empty
module/layer directory tree. It writes **no** domain/app/adapter business logic — the seven
modules ship as boundary-declaring package placeholders. The composition root is the seam through
which later features attach modules and inject cross-cutting dependencies (pgx pool from
data-migration; logger/secret/addr from config-runtime).

```mermaid
graph TD
    Main["cmd/server/main.go<br/>(dev-default seams, signal handling)"]
    Main -->|builds deps + calls| BS["internal/platform/bootstrap<br/>(composition root)"]
    BS -->|builds router+chain| HX["internal/platform/httpx<br/>router, recovery, logging, session, Server"]
    BS -->|builds renderer+static| WEB["internal/platform/web<br/>embed.FS, Renderer, Home, static"]
    BS -->|iterates Module seam<br/>Mount(chi.Router)| MODS["internal/modules/*<br/>(empty domain/app/adapter/public)"]
    HX -->|http.Handler| SRV["httpx.Server.Run(ctx)<br/>graceful shutdown"]
    WEB -->|renders| PAGE["GET / base htmx page (200)"]
    subgraph seams["Injection seams (other M0 features)"]
      LOG["*slog.Logger, session secret,<br/>addr → config-runtime"]
      PG["pgx pool → data-migration"]
    end
    LOG -.injected into.-> BS
    PG -.future param.-> BS
```

**Clean Architecture note:** dependencies point inward. Here only the outermost technical ring
exists; `platform/*` may know `chi`/`net/http`/`embed`. The `Module` seam keeps bootstrap
depending on a tiny interface, not on any module's internals — so mounting a module never violates
ARCHITECTURE §2.

---

## Modules & Clean Architecture Layers Touched

| Area | This feature does | Layer |
| ---- | ----------------- | ----- |
| `internal/platform/bootstrap` | **Implements** the composition root (`App`, `New`, `Module` seam) | platform (technical) |
| `internal/platform/httpx` | **Implements** router, middleware (recovery/logging/session), `Server` | platform (technical) |
| `internal/platform/web` | **Implements** `embed.FS`, `Renderer`, home + static handlers, templates | platform (technical) |
| `internal/shared/{document,id}` | **Placeholder** `doc.go` only (VOs land in later features) | shared kernel |
| `internal/modules/<all 7>/{domain,app,adapter/{repository,http},public}` | **Placeholder** `doc.go` declaring the layer boundary; no logic | all four layers (empty) |
| `cmd/server` | **Implements** thin entrypoint glue | entrypoint |
| `db/{migrations,queries}`, `web/{templates,static}` | Directory placeholders (+ base templates/assets) | infra dirs |

### Module Boundary Rule (design-level statement)

Per ARCHITECTURE §2 (**non-negotiable**): no module imports another module's `domain` or `app`;
cross-module access is only via `public/` interfaces + flat DTOs. This feature enforces the rule
two ways: (1) `doc.go` package comments state each layer's allowed imports; (2) a machine-checked
boundary test (SKEL-16) asserts the import graph. **No cross-module wiring exists yet** because no
module has behavior — the composition root only demonstrates the `Module.Mount` seam.

---

## DDD Building Blocks

**None.** This is an infrastructure feature: there are no entities, value objects, aggregates,
domain services, domain events, or repository interfaces. It establishes *where* those will live
(the `domain`/`app`/`adapter`/`public` folders) and the composition root that will one day wire
their `public` ports together. The shared-kernel VOs (`CPF`, `CNPJ`, `Base62`, `Period`) are named
placeholders only — their behavior is specified by the features that introduce them.

---

## Public Interfaces (exposed / consumed)

This feature **consumes** no other module's `public` package (nothing exists yet) and **exposes**
no module `public` API. It defines one platform-level seam that all later modules implement:

```go
// internal/platform/bootstrap/module.go
// Module is the only surface bootstrap needs to mount a module's HTTP routes.
// Each module's adapter/http provides an implementation; bootstrap wires the
// module's app-layer dependencies (incl. other modules' public impls) into it
// BEFORE calling Mount. Keeps bootstrap free of module internals (ISP).
type Module interface {
    Name() string             // e.g. "reservations" — for logging/route grouping
    Mount(r chi.Router)       // registers the module's routes on the shared router
}
```

Platform packages expose these seams (Go-ish signatures):

```go
// internal/platform/httpx
func NewRouter(deps RouterDeps) chi.Router          // assembles the middleware chain + NotFound
type RouterDeps struct {
    Logger        *slog.Logger                       // seam ← config-runtime (default slog.Default())
    SessionSecret []byte                             // seam ← config-runtime (dev default standalone)
    NotFound      http.HandlerFunc                   // rendered 404 from web
}
func Recoverer(next http.Handler) http.Handler       // panic → 500, server stays up
func RequestLogger(l *slog.Logger) func(http.Handler) http.Handler
func Session(secret []byte) func(http.Handler) http.Handler
func SessionFromContext(ctx context.Context) *sessions.Session
type Server struct{ /* wraps *http.Server + timeout */ }
func NewServer(addr string, h http.Handler, l *slog.Logger) *Server
func (s *Server) Run(ctx context.Context) error      // serve until ctx cancelled, then graceful drain

// internal/platform/web
//go:embed templates/*.html static/*
var assetsFS embed.FS                                 // (embed lives in web pkg root; see Tech Decisions)
type Renderer struct{ /* parsed *template.Template */ }
func NewRenderer() (*Renderer, error)
func (rd *Renderer) Page(w http.ResponseWriter, r *http.Request, name string, data any)  // full vs fragment by HX-Request
func Home(rd *Renderer) http.HandlerFunc              // GET / base page
func NotFound(rd *Renderer) http.HandlerFunc          // rendered 404
func StaticHandler() http.Handler                     // serves embedded /static/*

// internal/platform/bootstrap
type Deps struct {                                    // all fields are seams from other M0 features
    Addr          string
    Logger        *slog.Logger
    SessionSecret []byte
    Modules       []Module                            // empty for the skeleton
}
type App struct{ /* handler + server */ }
func New(deps Deps) (*App, error)                     // composition root; only place with concrete types
func (a *App) Handler() http.Handler                  // for httptest
func (a *App) Run(ctx context.Context) error          // delegates to httpx.Server.Run
```

**Port ownership:** the `Module` seam is a **consumer-owned** port (bootstrap owns the abstraction
it needs to mount routes), matching ARCHITECTURE §2's "prefer declaring the port in the consumer".

---

## Data Models

**None.** No Postgres tables, range types, or constraints are defined here — the entire data layer
(pool, migrations, `sqlc`) is the **data-migration** feature. `db/migrations/` and `db/queries/`
are created as empty placeholders only. Bootstrap reserves a future `*pgxpool.Pool` parameter (a
documented seam) but does not open a connection, so the skeleton boots with no database.

---

## Code Reuse Analysis

### Existing Components to Leverage

| Component | Location | How to Use |
| --------- | -------- | ---------- |
| _(none — greenfield repo)_ | — | First code in the repo; nothing to reuse internally. |

### Integration Points (seams to other features)

| System | Integration Method |
| ------ | ------------------ |
| config-runtime | Injects `*slog.Logger`, `SessionSecret`, `Addr` into `bootstrap.Deps`; skeleton uses dev defaults until then. |
| data-migration | Will add a `*pgxpool.Pool` field to `bootstrap.Deps` and pass repos into module constructors. |
| identity (M1) | Reads/writes the session via `httpx.SessionFromContext`; may replace the `CookieStore` behind the accessor. |
| all future modules | Implement `bootstrap.Module`; bootstrap mounts them and injects their cross-module `public` deps. |

### External dependencies (declared in `go.mod`)

| Dependency | Purpose | Decision |
| ---------- | ------- | -------- |
| `github.com/go-chi/chi/v5` | router + `RequestID` middleware | AD-001 / PROJECT.md §Tech Stack |
| `github.com/gorilla/sessions` | signed cookie session store | Open Decision (spec) |
| stdlib `net/http`, `html/template`, `embed`, `log/slog`, `context`, `os/signal` | server, templates, embedding, logging seam, shutdown | stdlib-first (KISS) |

---

## Components

### bootstrap (composition root)

- **Purpose**: Build platform dependencies, mount routes, return a runnable `App`; the only place
  that knows concrete types and wires modules.
- **Location**: `internal/platform/bootstrap/{bootstrap.go,module.go}`
- **Interfaces**: `New(Deps) (*App, error)`, `(*App).Handler()`, `(*App).Run(ctx)`,
  `Module{ Name(); Mount(chi.Router) }`.
- **Dependencies**: `httpx`, `web`, `chi`.
- **Reuses**: n/a (greenfield).

### httpx (router, middleware, server)

- **Purpose**: Assemble the chi router + middleware chain and own the graceful-shutdown server.
- **Location**: `internal/platform/httpx/{router.go,recovery.go,logging.go,session.go,server.go}`
- **Interfaces**: `NewRouter`, `Recoverer`, `RequestLogger`, `Session`, `SessionFromContext`,
  `NewServer`, `(*Server).Run`.
- **Dependencies**: `chi`, `gorilla/sessions`, `net/http`, `log/slog`, `context`.
- **Reuses**: chi's built-in `middleware.RequestID`.

### web (templates, renderer, static)

- **Purpose**: Embed and render the base htmx layout; serve static assets; distinguish full-page vs
  fragment.
- **Location**: `internal/platform/web/{embed.go,renderer.go,home.go,static.go}` +
  `web/templates/{base.html,home.html,not_found.html}` + `web/static/{app.css,htmx.min.js}`.
- **Interfaces**: `NewRenderer`, `(*Renderer).Page`, `Home`, `NotFound`, `StaticHandler`.
- **Dependencies**: `html/template`, `embed`, `net/http`.
- **Reuses**: n/a.

### module & shared skeleton

- **Purpose**: Establish the boundary-declaring package tree the rest of the roadmap fills in.
- **Location**: `internal/modules/<7 modules>/{domain,app,adapter/{repository,http},public}/doc.go`,
  `internal/shared/{document,id}/doc.go`.
- **Interfaces**: none (package-doc only).
- **Dependencies**: none.
- **Reuses**: n/a.

### cmd/server (entrypoint)

- **Purpose**: Thin glue: assemble dev-default seams, call `bootstrap.New`, run with signal-driven
  graceful shutdown.
- **Location**: `cmd/server/main.go`
- **Interfaces**: `main()`.
- **Dependencies**: `bootstrap`, `os/signal`, `log/slog`.
- **Reuses**: all shutdown logic lives in `httpx.Server` (tested there), so `main` has no branching.

---

## Error Handling Strategy

| Error Scenario | Handling | User Impact |
| -------------- | -------- | ----------- |
| Handler/middleware panic | `httpx.Recoverer` recovers, logs, writes `500`; process survives | Generic 500; server stays up |
| Template parse fails at startup | `web.NewRenderer` returns error → `bootstrap.New` wraps (`fmt.Errorf("...: %w")`) → `main` exits non-zero | App refuses to start (fail fast) |
| Template render fails per-request | `Renderer.Page` writes `500`, logs; never emits a partial body | Generic 500 |
| Unknown route | chi `NotFound` → `web.NotFound` renders 404 | Rendered not-found page |
| Missing/tampered session cookie | `Session` middleware starts a fresh empty session | Transparent; request proceeds |
| Missing static asset | `StaticHandler` returns `404` (no directory listing) | 404 |
| Shutdown exceeds drain timeout | `Server.Run` force-closes remaining conns, returns cleanly | Brief connection resets on restart |

Convention (CONVENTIONS.md): return `error`, wrap with `%w`; no panics for control flow; handlers
map failures to HTTP status. Startup errors bubble to `main`; per-request errors become HTTP 500.

---

## Tech Decisions (only non-obvious ones)

| Decision | Choice | Rationale |
| -------- | ------ | --------- |
| Template + static delivery | `embed.FS` compiled into the binary | Single self-contained deploy (AD-004); no runtime file-path or CDN dependency; CSP-friendly. |
| htmx delivery | Vendored `web/static/htmx.min.js`, served locally | Offline/self-contained; avoids external CDN + CSP issues. |
| Session store | `gorilla/sessions` `CookieStore` behind `httpx` accessors | Cookie-based per PROJECT.md; hidden behind `SessionFromContext` so identity (M1) can swap store without touching callers. |
| Logger & session secret | Injected via `bootstrap.Deps` (seams), dev defaults in `main` | Keeps env/config internals in config-runtime while letting the skeleton boot standalone. |
| Middleware order | `RequestID` → recovery → logging → session (outer→inner) | Recovery outermost of app logic so no panic escapes; logging wraps the handler+session; session innermost (closest to handlers that read it). |
| Full-page vs fragment | Branch on the `HX-Request` header in `Renderer.Page` | Standard htmx pattern (AD-003); one renderer serves both direct nav and partial swaps. |
| `Module` seam | Tiny `Mount(chi.Router)` interface, consumer-owned by bootstrap | ISP + ARCHITECTURE §2: bootstrap depends on an abstraction, never on module internals; cross-module `public` deps are injected at construction before `Mount`. |
| Boundary enforcement | Go test parsing the import graph (`golang.org/x/tools/go/packages` or `go/build`) | Makes the non-negotiable rule fail the build, not a review comment; no extra runtime cost. |
| Where the embed directive lives | `//go:embed` in `internal/platform/web`, pathing up to repo-root `web/` is not allowed | `embed` cannot climb out of its package dir, so the embedded copy of templates/static lives under `internal/platform/web/`; repo-root `web/` per STRUCTURE.md holds the source-of-truth assets that a build step (or symlink) mirrors. **Open note:** simplest is to place the `//go:embed`-adjacent asset dirs inside `internal/platform/web/`; STRUCTURE.md's top-level `web/` is honored as the canonical location and the embed package references a copy. Flagged for the implementer to pick one during Execute. |

---

## Tips honored

- Interfaces-first: `Module`, `RouterDeps`, `Renderer` contracts defined before implementation.
- KISS/YAGNI: stdlib-first; no DB, no config framework, no auth here; one tiny mount seam, not a
  plugin system.
- DRY: shutdown logic centralized in `httpx.Server`; `main` is glue only.
