# Project Skeleton & Module Boundaries Tasks

**Design**: `.specs/features/M0-foundation/project-skeleton/design.md`
**Status**: Done

> **Test note (read first):** This feature wires only `internal/platform/*` and the module
> skeleton. Per TESTING.md the platform row is *"integration (unit where pure)"* — every test here
> uses `httptest`/in-memory listeners with **no database and no external I/O**, so they qualify as
> **unit** under the `quick` gate. There are **no** integration/DB tests in this feature (Postgres
> is the data-migration feature). Because all tests are unit, all are parallel-safe per the
> Parallelism Assessment; `[P]` is therefore gated only by code dependencies.

---

## Execution Plan

### Phase 1: Foundation (Sequential)

```
T1 → T2
```

### Phase 2: Platform building blocks (Parallel OK — all depend only on T2)

```
        ┌→ T3 [P] ┐
        ├→ T4 [P] │
T2 ─────┼→ T5 [P] │
        ├→ T7 [P] │
        └→ T8 [P] ┘
```

### Phase 3: Composed platform pieces (Parallel OK)

```
T3,T4,T5 ──→ T6 [P]
T8 ────────→ T9 [P]
```

### Phase 4: Base handlers (Sequential)

```
T9 → T10
```

### Phase 5: Composition root (Sequential)

```
T6,T7,T9,T10 → T11
```

### Phase 6: Entrypoint + boundary guard (Parallel OK — both depend only on T11)

```
        ┌→ T12 [P]
T11 ────┤
        └→ T13 [P]
```

### Phase 7: Dev tooling (Sequential)

```
T12 → T14
```

---

## Task Breakdown

### T1: Initialize Go module and dependencies

**What**: Create `go.mod` (module `github.com/campsite-booking/campsite-booking`, Go 1.22+) and add
`go-chi/chi/v5` + `gorilla/sessions`.
**Where**: `go.mod`, `go.sum`
**Depends on**: None
**Reuses**: n/a (greenfield)
**Requirement**: SKEL-01

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `go.mod` declares the module path and `go 1.22`
- [x] `go get github.com/go-chi/chi/v5` and `github.com/gorilla/sessions` recorded
- [x] Gate check passes: `go build ./...`

**Tests**: none
**Gate**: build

**Verify**: `go build ./...` exits 0; `go.mod` contains both deps.

**Commit**: `chore(skeleton): init go module and deps`

---

### T2: Scaffold module & platform directory tree with boundary docs

**What**: Create the full STRUCTURE.md tree with `doc.go` placeholders declaring each layer's
import rule (ARCHITECTURE §3) and directory placeholders for assets/db.
**Where**: `internal/modules/{identity,campsites,config,availability,reservations,checkin,admin}/{domain,app,adapter/repository,adapter/http,public}/doc.go`;
`internal/platform/{bootstrap,httpx,web}/doc.go`; `internal/shared/{document,id}/doc.go`;
`cmd/server/`; `web/{templates,static}/.gitkeep`; `db/{migrations,queries}/.gitkeep`
**Depends on**: T1
**Reuses**: n/a
**Requirement**: SKEL-02, SKEL-03

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] All seven modules exist with `domain/app/adapter/{repository,http}/public` `doc.go` files
- [x] Each `doc.go` package name equals its leaf dir; comment states the allowed/forbidden imports
- [x] `internal/platform/*` and `internal/shared/*` placeholder packages exist
- [x] Gate check passes: `go build ./...`

**Tests**: none (scaffolding, no logic)
**Gate**: build

**Verify**: `go build ./...` exits 0; `find internal/modules -name doc.go | wc -l` == 35.

**Commit**: `chore(skeleton): scaffold module tree and layer boundary docs`

---

### T3: httpx recovery middleware [P]

**What**: Middleware that recovers panics, logs, and writes `500` without crashing the process.
**Where**: `internal/platform/httpx/recovery.go` (+ `recovery_test.go`)
**Depends on**: T2
**Reuses**: `log/slog`
**Requirement**: SKEL-05

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] Handler that panics returns `500`; test asserts server goroutine survives (no re-panic)
- [x] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [x] Test count: 2 tests pass (panic→500, normal pass-through) (no silent deletions)

**Tests**: unit
**Gate**: quick

**Verify**: `go test ./internal/platform/httpx/ -run Recover` → PASS.

**Commit**: `feat(httpx): add panic recovery middleware`

---

### T4: httpx request-logging middleware [P]

**What**: Middleware that emits one structured line (method, path, status, duration) via an
injected `*slog.Logger`.
**Where**: `internal/platform/httpx/logging.go` (+ `logging_test.go`)
**Depends on**: T2
**Reuses**: `log/slog` (seam ← config-runtime; default `slog.Default()`)
**Requirement**: SKEL-06

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] Test injects a `slog.Handler` capturing records; asserts method/path/status/duration logged
- [x] Status code correctly captured via a wrapped `ResponseWriter`
- [x] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [x] Test count: 2 tests pass (logs fields, captures non-200 status) (no silent deletions)

**Tests**: unit
**Gate**: quick

**Verify**: `go test ./internal/platform/httpx/ -run Logg` → PASS.

**Commit**: `feat(httpx): add structured request-logging middleware`

---

### T5: httpx session middleware and context accessor [P]

**What**: Middleware loading/creating a signed cookie session and `SessionFromContext(ctx)`.
**Where**: `internal/platform/httpx/session.go` (+ `session_test.go`)
**Depends on**: T2
**Reuses**: `gorilla/sessions` `CookieStore` (secret seam ← config-runtime)
**Requirement**: SKEL-07

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] Middleware puts a `*sessions.Session` in context; `SessionFromContext` returns it
- [x] Missing/tampered cookie yields a fresh empty session (no request error)
- [x] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [x] Test count: 3 tests pass (new session, round-trip, tampered→fresh) (no silent deletions)

**Tests**: unit
**Gate**: quick

**Verify**: `go test ./internal/platform/httpx/ -run Session` → PASS.

**Commit**: `feat(httpx): add cookie session middleware and context accessor`

---

### T6: httpx router builder (middleware chain + NotFound) [P]

**What**: `NewRouter(RouterDeps) chi.Router` assembling `RequestID → Recovery → Logging → Session`
and wiring the injected NotFound handler.
**Where**: `internal/platform/httpx/router.go` (+ `router_test.go`)
**Depends on**: T3, T4, T5
**Reuses**: chi `middleware.RequestID`, T3/T4/T5 middleware
**Requirement**: SKEL-04, SKEL-12

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] Chain applied in the documented outer→inner order (assert via request through a probe handler)
- [x] Unknown route invokes the injected NotFound handler (test uses a stub NotFound)
- [x] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [x] Test count: 2 tests pass (chain order/all-applied, NotFound wired) (no silent deletions)

**Tests**: unit
**Gate**: quick

**Verify**: `go test ./internal/platform/httpx/ -run Router` → PASS.

**Commit**: `feat(httpx): assemble chi router with middleware chain`

---

### T7: httpx graceful-shutdown server [P]

**What**: `Server` wrapping `*http.Server` with `Run(ctx)` that serves until ctx cancel, then
drains within a timeout.
**Where**: `internal/platform/httpx/server.go` (+ `server_test.go`)
**Depends on**: T2
**Reuses**: `net/http`, `context`
**Requirement**: SKEL-15

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `Run(ctx)` bound to `:0` returns cleanly when ctx is cancelled (graceful path)
- [x] In-flight request completes during drain; drain timeout force-closes and still returns
- [x] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [x] Test count: 2 tests pass (graceful drain, timeout force-close) (no silent deletions)

**Tests**: unit
**Gate**: quick

**Verify**: `go test ./internal/platform/httpx/ -run Server` → PASS.

**Commit**: `feat(httpx): add server with graceful shutdown`

---

### T8: Base templates and static assets [P]

**What**: Author the base layout, home + not-found templates, base stylesheet, and vendor
`htmx.min.js`.
**Where**: `internal/platform/web/templates/{base.html,home.html,not_found.html}`;
`internal/platform/web/static/{app.css,htmx.min.js}`
**Depends on**: T2
**Reuses**: n/a (htmx.min.js vendored from upstream release)
**Requirement**: SKEL-08, SKEL-09

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `base.html` shell loads `/static/app.css` + `/static/htmx.min.js` and defines a content block
- [x] `home.html` and `not_found.html` define the content block
- [x] `htmx.min.js` present under `static/`
- [x] Gate check passes: `go build ./...`

**Tests**: none (assets; exercised by T9/T10 renderer tests)
**Gate**: build

**Verify**: files exist; `base.html` references both static paths.

**Commit**: `feat(web): add base htmx layout and static assets`

---

### T9: web renderer, embed FS, and static handler [P]

**What**: `embed.FS` of templates+static, `Renderer.Page` (full-page vs `HX-Request` fragment),
and `StaticHandler`.
**Where**: `internal/platform/web/{embed.go,renderer.go,static.go}` (+ `renderer_test.go`)
**Depends on**: T8
**Reuses**: `html/template`, `embed`, `net/http`, T8 templates/assets
**Requirement**: SKEL-08, SKEL-09, SKEL-10

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `NewRenderer` parses embedded templates; parse failure returns error
- [x] `Page` renders full shell for a plain request; renders fragment only when `HX-Request: true`
- [x] `StaticHandler` serves an embedded asset (200) and returns 404 for a missing one
- [x] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [x] Test count: 4 tests pass (full page, fragment, static hit, static 404) (no silent deletions)

**Tests**: unit
**Gate**: quick

**Verify**: `go test ./internal/platform/web/ -run 'Render|Static'` → PASS.

**Commit**: `feat(web): add template renderer, embed FS, and static handler`

---

### T10: web home and not-found handlers

**What**: `Home(rd)` rendering the base page at `GET /`, and `NotFound(rd)` rendering a 404 page.
**Where**: `internal/platform/web/home.go` (+ `home_test.go`)
**Depends on**: T9
**Reuses**: T9 `Renderer`
**Requirement**: SKEL-11, SKEL-10, SKEL-12

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `Home` returns 200; body contains the app `<title>` and the `htmx.min.js` script tag
- [x] `Home` with `HX-Request: true` returns the fragment only (no `<html>` shell)
- [x] `NotFound` returns 404 with a rendered body
- [x] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [x] Test count: 3 tests pass (full home, home fragment, 404) (no silent deletions)

**Tests**: unit
**Gate**: quick

**Verify**: `go test ./internal/platform/web/ -run 'Home|NotFound'` → PASS.

**Commit**: `feat(web): add home and not-found handlers`

---

### T11: bootstrap composition root and Module seam

**What**: `Module` interface + `App`/`New(Deps)` building router+renderer+static, mounting `/`,
`/static/*`, NotFound, iterating `Deps.Modules`; end-to-end wiring test.
**Where**: `internal/platform/bootstrap/{module.go,bootstrap.go}` (+ `bootstrap_test.go`)
**Depends on**: T6, T7, T9, T10
**Reuses**: `httpx.NewRouter`, `httpx.Server`, `web.NewRenderer`, `web.Home`, `web.NotFound`,
`web.StaticHandler`
**Requirement**: SKEL-13, SKEL-14, SKEL-11, SKEL-12

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `New` returns `(*App, error)`; renderer build failure is wrapped (`%w`), no panic
- [x] `App.Handler()` end-to-end: `GET /` → 200, `GET /static/app.css` → 200, unknown → 404
- [x] A fake `Module` in `Deps.Modules` has its `Mount` called and its route reachable
- [x] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [x] Test count: 4 tests pass (root 200, static 200, 404, module-mounted) (no silent deletions)

**Tests**: unit
**Gate**: quick

**Verify**: `go test ./internal/platform/bootstrap/` → PASS.

**Commit**: `feat(bootstrap): add composition root and module mount seam`

---

### T12: cmd/server entrypoint with signal-driven shutdown [P]

**What**: `main()` assembling dev-default seams, calling `bootstrap.New`, running `App.Run(ctx)`
with `signal.NotifyContext` (SIGINT/SIGTERM); exits non-zero on build error.
**Where**: `cmd/server/main.go`
**Depends on**: T11
**Reuses**: `bootstrap.New`, `httpx.Server` (all shutdown logic already tested in T7)
**Requirement**: SKEL-13, SKEL-15

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `main` wires `signal.NotifyContext` for SIGINT/SIGTERM and passes ctx to `App.Run`
- [x] Build error from `New` → log + `os.Exit(1)`; no branching logic beyond glue
- [x] Gate check passes: `go build ./...`

**Tests**: none (thin glue; branching-free; shutdown covered by T7, wiring by T11)
**Gate**: build

**Verify**: `go run ./cmd/server &` then `curl -s localhost:<addr>/` → base page; SIGINT → clean exit.

**Commit**: `feat(server): add entrypoint with graceful shutdown`

---

### T13: module-boundary enforcement test [P]

**What**: A Go test that parses the import graph and asserts the boundary rule.
**Where**: `internal/architecture_test.go`
**Depends on**: T11
**Reuses**: `golang.org/x/tools/go/packages` (or `go/build`)
**Requirement**: SKEL-16

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] Asserts no `internal/modules/<A>` imports `internal/modules/<B>/{domain,app}` (A≠B)
- [x] Asserts no `internal/modules/*/domain` imports `chi`, `pgx`, `net/http`, sessions, or another module
- [x] Passes on the current skeleton (no violations)
- [x] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [x] Test count: 2 tests pass (cross-module rule, domain-purity rule) (no silent deletions)

**Tests**: unit
**Gate**: quick

**Verify**: `go test ./internal/ -run Boundary` → PASS; adding a forbidden import makes it FAIL.

**Commit**: `test(arch): enforce module import boundaries`

---

### T14: developer Makefile

**What**: `Makefile` with `build`, `run`, `test` targets wrapping the `go` commands.
**Where**: `Makefile`
**Depends on**: T12
**Reuses**: gate commands from TESTING.md
**Requirement**: SKEL-17

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `build`→`go build ./...`, `run`→`go run ./cmd/server`, `test`→`go test ./...`
- [x] Gate check passes: `go build ./...`

**Tests**: none
**Gate**: build

**Verify**: `make build` exits 0; `make test` runs the unit suite.

**Commit**: `chore(skeleton): add developer Makefile`

---

## Parallel Execution Map

```
Phase 1 (Sequential):
  T1 ──→ T2

Phase 2 (Parallel — depend only on T2):
  ├── T3 [P]
  ├── T4 [P]
  ├── T5 [P]
  ├── T7 [P]
  └── T8 [P]

Phase 3 (Parallel):
  T3,T4,T5 ──→ T6 [P]
  T8 ────────→ T9 [P]

Phase 4 (Sequential):
  T9 ──→ T10

Phase 5 (Sequential):
  T6,T7,T9,T10 ──→ T11

Phase 6 (Parallel — depend only on T11):
  ├── T12 [P]
  └── T13 [P]

Phase 7 (Sequential):
  T12 ──→ T14
```

**Parallelism constraint check:** every `[P]` task's required test type is **unit** (or none),
which TESTING.md marks parallel-safe; no `[P]` task shares mutable state (each owns distinct files
and, for T7, an isolated `:0` listener). No integration/DB tests exist in this feature, so no `[P]`
stripping is required.

---

## Task Granularity Check

| Task | Scope | Status |
| ---- | ----- | ------ |
| T1: init go module | 1 file (`go.mod`) | ✅ Granular |
| T2: scaffold tree + boundary docs | 1 concept (the skeleton) — bulk placeholders, cohesive | ✅ Granular (scaffold) |
| T3: recovery middleware | 1 middleware | ✅ Granular |
| T4: logging middleware | 1 middleware | ✅ Granular |
| T5: session middleware | 1 middleware + accessor (same file, cohesive) | ✅ Granular |
| T6: router builder | 1 function | ✅ Granular |
| T7: graceful server | 1 component | ✅ Granular |
| T8: templates + assets | 1 concept (base UI assets) | ✅ Granular |
| T9: renderer + embed + static | 1 component (rendering) + its FS/static, cohesive | ✅ Granular |
| T10: home + not-found handlers | 2 cohesive handlers, 1 file | ✅ OK (cohesive) |
| T11: bootstrap + Module seam | 1 composition root | ✅ Granular |
| T12: cmd/server entrypoint | 1 file | ✅ Granular |
| T13: boundary test | 1 test file | ✅ Granular |
| T14: Makefile | 1 file | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| ---- | ---------------------- | ------------- | ------ |
| T1 | None | (root) | ✅ Match |
| T2 | T1 | T1→T2 | ✅ Match |
| T3 | T2 | T2→T3 | ✅ Match |
| T4 | T2 | T2→T4 | ✅ Match |
| T5 | T2 | T2→T5 | ✅ Match |
| T6 | T3, T4, T5 | T3,T4,T5→T6 | ✅ Match |
| T7 | T2 | T2→T7 | ✅ Match |
| T8 | T2 | T2→T8 | ✅ Match |
| T9 | T8 | T8→T9 | ✅ Match |
| T10 | T9 | T9→T10 | ✅ Match |
| T11 | T6, T7, T9, T10 | T6,T7,T9,T10→T11 | ✅ Match |
| T12 | T11 | T11→T12 | ✅ Match |
| T13 | T11 | T11→T13 | ✅ Match |
| T14 | T12 | T12→T14 | ✅ Match |

Parallel-set independence: {T3,T4,T5,T7,T8} mutually independent ✅; {T6,T9} independent ✅;
{T12,T13} independent ✅.

---

## Test Co-location Validation

Coverage matrix (TESTING.md): `internal/platform/*` wiring/middleware → **integration (unit where
pure)**. All tests here are `httptest`/in-memory with no DB or external I/O → the "unit where pure"
clause applies (unit + quick gate). Scaffolding/glue tasks with no branching logic → **none** +
build gate.

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| ---- | --------------------------- | --------------- | --------- | ------ |
| T1 | `go.mod` (no logic) | none | none | ✅ OK |
| T2 | `doc.go` placeholders (no logic) | none | none | ✅ OK |
| T3 | platform/httpx middleware (pure) | unit (pure) | unit | ✅ OK |
| T4 | platform/httpx middleware (pure) | unit (pure) | unit | ✅ OK |
| T5 | platform/httpx middleware (pure) | unit (pure) | unit | ✅ OK |
| T6 | platform/httpx router (pure) | unit (pure) | unit | ✅ OK |
| T7 | platform/httpx server (pure) | unit (pure) | unit | ✅ OK |
| T8 | static assets/templates (no Go logic) | none | none | ✅ OK |
| T9 | platform/web renderer/static (pure) | unit (pure) | unit | ✅ OK |
| T10 | platform/web handlers (pure) | unit (pure) | unit | ✅ OK |
| T11 | platform/bootstrap wiring (pure) | unit (pure) | unit | ✅ OK |
| T12 | cmd/server glue (no branching logic) | none | none | ✅ OK |
| T13 | architecture test (pure) | unit (pure) | unit | ✅ OK |
| T14 | Makefile (no logic) | none | none | ✅ OK |

**Notes:** No `Tests: none` is used to defer a testable layer — the `none` rows are pure
scaffolding/assets/glue. `main` (T12) delegates all shutdown behavior to `httpx.Server` (tested in
T7) and all wiring to `bootstrap` (tested in T11), so it carries no unverified logic.

---

## Requirement Coverage

| Requirement | Task(s) |
| ----------- | ------- |
| SKEL-01 | T1 |
| SKEL-02 | T2 |
| SKEL-03 | T2 |
| SKEL-04 | T6 |
| SKEL-05 | T3 |
| SKEL-06 | T4 |
| SKEL-07 | T5 |
| SKEL-08 | T8, T9 |
| SKEL-09 | T8, T9 |
| SKEL-10 | T9, T10 |
| SKEL-11 | T10, T11 |
| SKEL-12 | T6, T10, T11 |
| SKEL-13 | T11, T12 |
| SKEL-14 | T11 |
| SKEL-15 | T7, T12 |
| SKEL-16 | T13 |
| SKEL-17 | T14 |

All 17 requirements mapped; 14 tasks, 0 unmapped.
