# State

**Last Updated:** 2026-07-07
**Current Work:** M0 (Foundation & Infrastructure) fully implemented — all 25 tasks across
`project-skeleton` (14), `data-migration` (5), and `config-runtime` (6) complete, gated, and
committed. Repo boots: `cmd/server` serves `GET /` (200), `GET /healthz` (200 against live
Postgres), graceful shutdown on SIGINT/SIGTERM, migrations run via `cmd/migrate`. Full gate green:
`go build ./... && go vet ./... && go test ./...` (46 unit tests) and
`go test -tags=integration ./...` (real Postgres via testcontainers-go/Docker) both pass. M1 + M2
feature specs remain authored (spec + design + tasks) but unimplemented — next up per Todos.

---

## Recent Decisions (Last 60 days)

### AD-008: Reservation account linkage + LGPD posture recorded (2026-07-07)

**Decision:** `reservations` stores a nullable `booked_by_user_id` (authenticated principal; NULL for anonymous PF) from M2 onward, even though M2 has no read use for it. LGPD retention/erasure remains post-MVP but is now an explicit, documented gap (erasure must anonymize denormalized CPFs across all participant rows — an account `CASCADE` does **not** satisfy it).
**Reason:** RF07 "authenticated user history" (M3) needs a reliable linkage — especially for PJ, whose company account has no CPF matching `responsible_cpf`. Capturing the column now avoids a later migration. LGPD is a stated legal constraint that was under-specified.
**Impact:** reservation-creation design (schema) + spec (Open Decisions). M3 LKP decides the exact history query.

### AD-007: Single owner for the booking-window computation — `availability` (2026-07-07)

**Decision:** `config/public` exposes only the raw horizon via `BookingWindowMonths(ctx) int`; `availability` owns the sliding-end/within-window math (its `BookingWindow` VO). `availability` does **not** consume `DefaultOverbookingPercent` — effective capacity (overbooking baked in) comes from `campsites/public`; config's overbooking default is a new-campsite-form seed only.
**Reason:** Review found config and availability *both* claimed to own the window math (incompatible port shapes: `BookingWindowEnd(now)` vs `BookingWindowMonths`), and config's dependency table wrongly listed availability consuming overbooking → double-count risk.
**Impact:** system-configuration design/spec/tasks updated (removed `BookingWindowEnd`/`IsWithinBookingWindow`); availability unchanged (already used `BookingWindowMonths`).

### AD-006: Ambient (ctx-carried) transaction seam in `platform/postgres` (2026-07-07)

**Decision:** M0 `WithTx(ctx, pool, fn func(ctx) error)` stores the `pgx.Tx` in `ctx`; add `Executor(ctx, pool)` resolving tx-or-pool. Repositories run every query through `Executor(ctx)`.
**Reason:** Review found M0 had specified `WithTx(..., fn func(pgx.Tx) error)` (explicit handle), which **cannot** deliver M2's cross-module last-vacancy atomicity: `reservation-creation` opens the tx and calls `availability/public.Reserver.Reserve` inside it, but availability's `public` is stdlib-only — the `pgx.Tx` must travel in `ctx`, not a signature.
**Impact:** M0 data-migration design/spec/tasks (DATA-03) updated; the M2 "add if absent" caveat resolved. This unblocks the highest-risk milestone before implementation.

### AD-001: Backend language — Go (2026-07-06)

**Decision:** Build the backend in Go (1.22+) with `net/http` + `chi` router.
**Reason:** Excellent performance/concurrency to meet the < 200 ms availability NFR; simple, single-binary deploy for a modular monolith.
**Trade-off:** More manual data-layer work; smaller domain-framework ecosystem than JVM/Spring.
**Impact:** Concurrency control (overbooking last-vacancy) handled explicitly at the DB layer rather than via an ORM's transaction abstractions.

### AD-002: Database — PostgreSQL (2026-07-06)

**Decision:** PostgreSQL 16+ as the single datastore.
**Reason:** Strong transactional guarantees; row-level/advisory locking for last-vacancy races; range types + exclusion constraints for period-overlap rules.
**Trade-off:** None significant for MVP.
**Impact:** Overlap prevention and occupancy integrity can be enforced in-schema, not only in app code.

### AD-003: Frontend — htmx + Go templates (2026-07-06)

**Decision:** Server-rendered `html/template` enhanced with htmx; no separate SPA.
**Reason:** Simplest deploy, cohesive with the Go backend, fast enough for the booking/admin/Porteiro panels.
**Trade-off:** Less rich client-side interactivity than a React/Vue SPA.
**Impact:** UI logic lives in Go handlers + templates; partial updates via htmx fragments.

### AD-004: Architecture — modular monolith (2026-07-06)

**Decision:** Single deployable with clear internal module boundaries (identity, campsites, availability, reservations, check-in, admin, config).
**Reason:** Fastest path to a shippable MVP; boundaries keep future extraction cheap.
**Trade-off:** Shared process/runtime; discipline needed to keep module boundaries from leaking.
**Impact:** Roadmap milestones map roughly to modules.

### AD-005: Supporting data/runtime defaults (2026-07-06)

**Decision:** `pgx` driver + `sqlc` for type-safe SQL; `golang-migrate` for migrations; cookie-based sessions + `bcrypt`; `testcontainers-go` for integration tests.
**Reason:** Idiomatic Go stack that lets us write the locking/overlap SQL directly and test against real Postgres.
**Trade-off:** `sqlc` codegen step in the build.
**Impact:** Data-access patterns standardized before M0 skeleton work begins.

---

## Active Blockers

_None._

---

## Lessons Learned

- **Cross-feature seams need one named owner.** Two features authored in parallel each independently "owned" the same concept twice — the transaction seam (M0 vs M2) and the booking-window math (config vs availability) — producing incompatible interface shapes. When a capability spans a module boundary, pin the owner and the exact port shape in one doc and have the other *cite* it, rather than re-deriving it. (Surfaced by the 2026-07-07 design review; fixed in AD-006/AD-007.)
- **`Release`-style decrements are not self-protecting.** Unlike a capacity-ceilinged increment, a decrement that only floors at 0 relies entirely on callers for exactly-once; documented as a caller contract in availability rather than assumed.
- **Toolchain drift on greenfield `go mod init`.** The dev box only had no Go installed; installed
  1.22.10 user-locally, but `chi/v5` and later `golang.org/x/tools` (for the M0 boundary test)
  each required a newer Go, auto-bumping `go.mod`'s `go` directive to 1.25.0 via the toolchain
  mechanism. Not a problem (Go handles it transparently), but don't assume the spec's stated
  "Go 1.22+" pins the actual toolchain version in `go.mod` after `go get`.
- **`sqlc compile` errors on zero query files**, even with a valid config and schema ("no queries
  contained in paths db/queries"). This is expected in M0 (no module has queries yet) — the real
  gate for the `sqlc.yaml` task is `go build ./...` (config-only, no Go code layer); `sqlc compile`
  only becomes meaningful once the first module feature adds `db/queries/*.sql`.
- **Integration tests need Docker.** All `//go:build integration` suites in M0
  (`postgres`, `pgtest`, `cmd/migrate`, `bootstrap`) spin real `postgres:16-alpine` containers via
  testcontainers-go — verified working against the local Docker daemon. Future sessions on a
  different machine should confirm Docker is running before trusting a red integration gate.

---

## Quick Tasks Completed

| #   | Description | Date | Commit | Status |
| --- | ----------- | ---- | ------ | ------ |
| —   | —           | —    | —      | —      |

---

## Deferred Ideas

- [ ] Confirm whether spec docs should be authored in PT-BR instead of English (domain terms already preserved) — Captured during: project init
- [ ] Evaluate `templ` (typed templates) vs stdlib `html/template` when M0 UI work starts — Captured during: project init

---

## Todos

- [x] Begin implementation with M0 `project-skeleton` → `data-migration` → `config-runtime` (dependency order) — done 2026-07-07
- [ ] Begin M1 (Identity & Setup): `user-company-registration` → `authentication` → `campsite management` → `system-configuration`

---

## Preferences

**Model Guidance Shown:** 2026-07-06
