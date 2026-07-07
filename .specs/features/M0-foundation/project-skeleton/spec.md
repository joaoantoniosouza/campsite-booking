# Project Skeleton & Module Boundaries Specification

## Problem Statement

Every downstream feature (identity, campsites, availability, reservations, check-in, admin,
config) needs a place to live and a boot path to run in. There is no Go code yet — only an empty
repo. We must lay down the modular-monolith skeleton (directory layout, module boundaries, HTTP
server, base htmx page, composition root) so the target architecture in
`.specs/codebase/ARCHITECTURE.md` exists in code and the skeleton boots and serves a base page.

## Goals

- [ ] The binary boots and `GET /` returns a base htmx page (HTTP 200) built from `html/template`.
- [ ] The seven modules exist as `internal/modules/<module>/{domain,app,adapter,public}` with the
      Clean Architecture layer boundaries expressed in code (package-doc placeholders).
- [ ] A `chi` router runs a middleware chain (recovery, request-logging, session) and shuts down
      gracefully on SIGINT/SIGTERM without dropping in-flight requests.
- [ ] The composition root (`internal/platform/bootstrap`) is the single place that constructs
      concrete types and mounts module routes via a stable registration seam.
- [ ] The module-boundary rule is machine-checked: no module imports another module's
      `domain`/`app`, and `domain` imports no infrastructure.

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
| ------- | ------ |
| Postgres pool / `pgx` / migrations / `sqlc` | Owned by M0 **data-migration** feature. Bootstrap only exposes the injection seam. |
| Env config loading, `.env`, health check, `slog` setup internals | Owned by M0 **config-runtime** feature. Skeleton uses safe dev defaults and injects a `*slog.Logger` + session secret as seams. |
| Authentication, login, roles, real session contents | Owned by M1 **authentication**/identity. Session middleware here is plumbing only (signed cookie + context accessor), no auth logic. |
| Any domain/app/adapter business logic in the seven modules | This is infrastructure; modules ship as empty layer packages only. |
| Postgres tables, range types, exclusion constraints | No data model in this feature (see data-migration). |

---

## User Stories

### P1: Repository & module skeleton ⭐ MVP

**User Story**: As a developer, I want the modular-monolith directory layout and module boundaries
scaffolded so that every later feature has an unambiguous, boundary-safe home for its code.

**Why P1**: Nothing else can be built until the layout and boundaries exist. It is the literal
subject of this feature.

**Acceptance Criteria**:

1. WHEN the repo is built THEN the system SHALL provide a `go.mod` with module path
   `github.com/campsite-booking/campsite-booking` and Go `1.22+`, declaring `chi` and the session
   library as dependencies. (SKEL-01)
2. WHEN the layout is created THEN the system SHALL contain `cmd/server/`,
   `internal/platform/{bootstrap,httpx,web}/`, `internal/shared/{document,id}/`,
   `internal/modules/<module>/{domain,app,adapter/{repository,http},public}/` for all seven
   modules (identity, campsites, config, availability, reservations, checkin, admin), and
   `web/{templates,static}/`, `db/{migrations,queries}/` placeholders per STRUCTURE.md. (SKEL-02)
3. WHEN a module layer package is created THEN it SHALL carry a `doc.go` whose package comment
   states the layer and the import rule it obeys (per ARCHITECTURE §3), and its Go package name
   SHALL equal its leaf directory name. (SKEL-03)
4. WHEN `go build ./...` runs on the skeleton THEN it SHALL compile with no errors. (SKEL-02)

**Independent Test**: Run `go build ./...`; assert the directory tree and every `doc.go` exist and
compile.

---

### P1: HTTP server, router, middleware & graceful shutdown ⭐ MVP

**User Story**: As an operator, I want an HTTP server with a sane middleware chain and graceful
shutdown so that the app is observable, panic-resilient, and safe to deploy/restart.

**Why P1**: The base page cannot be served, and later handlers cannot mount, without the router +
server lifecycle.

**Acceptance Criteria**:

1. WHEN the router is built THEN the system SHALL apply middleware in outer→inner order:
   `RequestID` → recovery → request-logging → session. (SKEL-04)
2. WHEN a handler panics THEN the recovery middleware SHALL respond `500` and the server process
   SHALL stay up (no crash). (SKEL-05)
3. WHEN a request completes THEN the request-logging middleware SHALL emit one structured log line
   (method, path, status, duration) via an injected `*slog.Logger` (seam to config-runtime;
   defaults to `slog.Default()`). (SKEL-06)
4. WHEN a request arrives THEN the session middleware SHALL load/create a signed cookie session and
   expose it via `httpx.SessionFromContext(ctx)`; the signing secret is injected (seam to
   config-runtime; dev default used standalone). (SKEL-07)
5. WHEN the process receives SIGINT or SIGTERM THEN the server SHALL stop accepting new connections
   and drain in-flight requests within a shutdown timeout, then exit `0`. (SKEL-15)

**Independent Test**: `httptest` a router whose handler panics → assert 200-stays-up + 500 body;
call `Server.Run(ctx)` bound to `:0`, cancel `ctx`, assert clean return.

---

### P1: Base template layout, htmx wiring & static assets ⭐ MVP

**User Story**: As a visitor, I want the app to serve a base page with htmx and styling loaded so
that later features can render server-side fragments and full pages.

**Why P1**: "Skeleton boots and serves a base page" is the feature's target.

**Acceptance Criteria**:

1. WHEN the app starts THEN it SHALL embed `web/templates/` and `web/static/` via `embed.FS` so the
   binary is self-contained (single-deploy per AD-004). (SKEL-08)
2. WHEN `GET /static/*` is requested THEN the system SHALL serve embedded assets (including a
   vendored `htmx.min.js` and a base stylesheet) with correct content types. (SKEL-08)
3. WHEN templates are loaded THEN a base layout SHALL be parsed from the embedded FS and provide the
   HTML shell (loads the stylesheet + `htmx.min.js`). (SKEL-09)
4. WHEN `GET /` is requested via direct navigation THEN the system SHALL render the full base page
   (HTTP 200) containing the app title and the htmx script tag. (SKEL-11)
5. WHEN a request carries the `HX-Request` header THEN the renderer SHALL render only the content
   fragment (no base shell); otherwise it renders the full page. (SKEL-10)
6. WHEN an unknown route is requested THEN the system SHALL return `404` with a rendered not-found
   response. (SKEL-12)

**Independent Test**: `httptest` `GET /` → 200 body contains `<title>` and `htmx.min.js`; repeat
with `HX-Request: true` → fragment only; `GET /nope` → 404.

---

### P1: Composition root & module registration seam ⭐ MVP

**User Story**: As an architect, I want a single composition root that builds platform dependencies
and wires modules via their `public` interfaces so that the module-boundary rule holds and future
modules plug in without touching each other's internals.

**Why P1**: The composition-root pattern is the mechanism that makes cross-module wiring legal
(ARCHITECTURE §2, §6); establishing it now is the point of the feature.

**Acceptance Criteria**:

1. WHEN `bootstrap.New(deps)` is called THEN it SHALL build the `chi` router (with middleware from
   `httpx`), the template renderer + static handler (from `web`), mount the base routes, and return
   a runnable `App` exposing an `http.Handler`; it SHALL be the only package that constructs
   concrete module/platform types. (SKEL-13)
2. WHEN modules are registered THEN bootstrap SHALL iterate a `Module` seam (`Mount(chi.Router)`)
   and mount each; cross-module dependencies SHALL be injected here by passing one module's `public`
   implementation into another module's constructor — documented as the pattern (zero modules wired
   yet, since none have logic). (SKEL-14)
3. WHEN `bootstrap.New` fails to build a dependency THEN it SHALL return a wrapped `error` (no
   panic) so `main` can exit non-zero. (SKEL-13)
4. WHEN `cmd/server/main.go` runs THEN it SHALL assemble dev-default seams, call `bootstrap.New`,
   and run the server with graceful shutdown. (SKEL-13, SKEL-15)

**Independent Test**: `httptest` the `App.Handler()` end-to-end: `GET /` → 200, `GET /static/...`
→ 200, unknown → 404, all through the real composition root.

---

### P2: Module-boundary enforcement

**User Story**: As an architect, I want the non-negotiable boundary rule machine-checked so that a
future PR that reaches across module internals fails the build instead of silently coupling modules.

**Why P2**: Not required for the skeleton to boot, but it is what keeps "module boundaries" real as
the codebase grows. Cheap to add now, expensive to retrofit later.

**Acceptance Criteria**:

1. WHEN the boundary test runs THEN it SHALL assert that no `internal/modules/<A>` package imports
   any `internal/modules/<B>/domain` or `internal/modules/<B>/app` (A ≠ B). (SKEL-16)
2. WHEN the boundary test runs THEN it SHALL assert that no `internal/modules/*/domain` package
   imports infrastructure (`chi`, `pgx`, `net/http`, the session library) or any other module.
   (SKEL-16)

**Independent Test**: Run the boundary test on the current skeleton → passes (no violations);
manually adding a forbidden import makes it fail.

---

### P3: Developer build/run convenience

**User Story**: As a developer, I want a `Makefile` with build/run/test targets so that boot and
gate commands are one command each.

**Why P3**: Pure ergonomics; the skeleton is fully usable via raw `go` commands without it.

**Acceptance Criteria**:

1. WHEN `make build` / `make run` / `make test` are invoked THEN they SHALL wrap the corresponding
   `go build ./...`, run of `cmd/server`, and `go test ./...` commands respectively. (SKEL-17)

**Independent Test**: `make build` succeeds; `make test` runs the unit suite.

---

## Edge Cases

- WHEN a template fails to render THEN the system SHALL respond `500` and log the error (not write a
  half-rendered body). (SKEL-09)
- WHEN a panic occurs inside a mounted module handler THEN recovery SHALL still yield `500` and keep
  the server up. (SKEL-05)
- WHEN shutdown exceeds the drain timeout THEN the server SHALL force-close remaining connections
  and still exit cleanly. (SKEL-15)
- WHEN the session cookie is missing or tampered THEN the session middleware SHALL start a fresh
  empty session rather than error the request. (SKEL-07)
- WHEN `web/static/` lacks a requested asset THEN the static handler SHALL return `404` (not a
  directory listing). (SKEL-08)

---

## Requirement Traceability

Each requirement gets a unique ID for tracking across design, tasks, and validation.

| Requirement ID | Story | Phase | Status |
| -------------- | ----- | ----- | ------ |
| SKEL-01 | P1: Repo & module skeleton | Design | In Tasks |
| SKEL-02 | P1: Repo & module skeleton | Design | In Tasks |
| SKEL-03 | P1: Repo & module skeleton | Design | In Tasks |
| SKEL-04 | P1: HTTP server & middleware | Design | In Tasks |
| SKEL-05 | P1: HTTP server & middleware | Design | In Tasks |
| SKEL-06 | P1: HTTP server & middleware | Design | In Tasks |
| SKEL-07 | P1: HTTP server & middleware | Design | In Tasks |
| SKEL-08 | P1: Base layout & assets | Design | In Tasks |
| SKEL-09 | P1: Base layout & assets | Design | In Tasks |
| SKEL-10 | P1: Base layout & assets | Design | In Tasks |
| SKEL-11 | P1: Base layout & assets | Design | In Tasks |
| SKEL-12 | P1: Base layout & assets | Design | In Tasks |
| SKEL-13 | P1: Composition root | Design | In Tasks |
| SKEL-14 | P1: Composition root | Design | In Tasks |
| SKEL-15 | P1: HTTP server & middleware | Design | In Tasks |
| SKEL-16 | P2: Boundary enforcement | Design | In Tasks |
| SKEL-17 | P3: Dev convenience | Design | In Tasks |

**ID format:** `SKEL-NN` (per CONVENTIONS.md prefix table).

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 17 total, 17 mapped to tasks (see tasks.md), 0 unmapped.

**PRD / RF traceability:** This is an **infrastructure foundation** feature — it implements **no**
functional requirement (RF01–RF13). It realizes the architecture decisions **AD-001** (Go + chi),
**AD-003** (htmx + `html/template`), **AD-004** (modular monolith) from STATE.md and
PROJECT.md §Tech Stack, and establishes in code the structure described by
`.specs/codebase/ARCHITECTURE.md` (§1–§3, §5, §6) and `.specs/codebase/STRUCTURE.md`. It provides
the injection seams later consumed by the M0 **data-migration** (pgx pool) and **config-runtime**
(logger, session secret, server address, health) features.

---

## Success Criteria

How we know the feature is successful:

- [ ] `go build ./...` compiles the full skeleton with all seven module layer packages present.
- [ ] Running `cmd/server` and hitting `GET /` returns HTTP 200 with the base htmx page.
- [ ] `GET /` with `HX-Request` returns the fragment only; unknown route returns 404.
- [ ] A forced handler panic yields 500 and the process stays up.
- [ ] SIGINT/SIGTERM triggers graceful drain within the timeout, exit 0.
- [ ] The boundary test passes and would fail on a forbidden cross-module import.

---

## Open Decisions

- **Module path** = `github.com/campsite-booking/campsite-booking` (STRUCTURE.md uses
  `github.com/<org>/...`; no org is fixed yet). Replace the `<org>` segment once the real GitHub
  org/repo is chosen — a one-line `go.mod` + import-prefix change.
- **Session library** = `gorilla/sessions` `CookieStore` (mature, cookie-based per PROJECT.md).
  Kept behind `httpx` accessors so identity (M1) can swap the store without touching call sites.
- **htmx delivery** = vendored `web/static/htmx.min.js` (self-contained binary, CSP-friendly)
  rather than a CDN reference.
