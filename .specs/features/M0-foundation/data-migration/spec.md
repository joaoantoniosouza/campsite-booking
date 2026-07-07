# Data & Migration Layer Specification

**Milestone:** M0 — Foundation & Infrastructure
**Module:** `internal/platform/postgres` + `db/` (technical cross-cutting; NOT a bounded-context module)
**Requirement prefix:** `DATA`

## Problem Statement

Every module's `adapter/repository` needs a shared, concurrency-safe way to reach Postgres:
a connection pool, a transaction boundary, an ordered/reversible migration stream, and a real
database to test against. Without this foundation, no domain feature can persist state or prove
its locking/overlap SQL. This feature establishes that infrastructure — pool, migration runner,
baseline schema stream, and a `testcontainers-go` harness — with **no business rules**.

## Goals

- [ ] `NewPool` builds a verified `*pgxpool.Pool` from typed config; failures return errors (no panic).
- [ ] A single ordered migration stream in `db/migrations/` applies cleanly to an empty DB and is reversible.
- [ ] A baseline migration enables the DB-wide prerequisites later features rely on (`btree_gist` for §7 exclusion constraints) — establishing the stream, not the domain schema.
- [ ] A reusable integration-test harness spins Postgres 16, applies the same migrations (prod/test parity), and gives each test isolated state — including support for concurrency (last-vacancy) tests.
- [ ] `sqlc.yaml` + `WithTx`/`Executor(ctx)` conventions in place so downstream repositories write type-safe, hand-written locking SQL that runs on the ambient (ctx-carried) transaction — including one opened by another module across a `public` boundary.

## Out of Scope

Explicitly excluded — prevents scope creep. This is infrastructure only.

| Feature | Reason |
| ------- | ------ |
| Domain tables (users, campsites, reservations, occupancy) | Each domain feature owns its own migration + tables (STRUCTURE §Migrations). We build the stream, not the schema. |
| Exclusion constraints / range-type tables | Reservations/availability features author these; we only enable `btree_gist` so they *can*. |
| `sqlc` code generation output | No queries exist in M0 (no domain tables). We provide `sqlc.yaml` + the per-module convention; codegen runs when a module adds its first query. |
| ORM / query builder | AD-005: hand-written SQL via `sqlc` + `pgx`. |
| Read replicas, sharding, connection proxies (pgbouncer) | YAGNI for MVP single-binary monolith. |
| Config **loading** from env | Owned by `config-runtime` (RUN). We define the `postgres.Config` shape it populates. |
| Bootstrap wiring / HTTP server | Owned by `project-skeleton` (SKEL). We expose the functions its composition root calls. |

---

## User Stories

### P1: Postgres connection pool & transaction boundary ⭐ MVP

**User Story**: As a platform engineer, I want a verified pgx connection pool and a transaction
helper so that every repository shares one concurrency-safe, correctly-configured data access path.

**Why P1**: Nothing persists without it; `WithTx` is the **ambient** (ctx-carried) boundary that
last-vacancy transactions (ARCHITECTURE §7) run inside, and `Executor(ctx, pool)` is how a
repository picks up that tx — including one opened by *another* module — so the cross-module
occupancy increment + reservation insert commit atomically without a `pgx.Tx` crossing a `public` boundary.

**Acceptance Criteria**:

1. WHEN `NewPool(ctx, cfg)` is called with a valid, reachable config THEN system SHALL return a
   `*pgxpool.Pool` with the configured pool sizing applied and connectivity verified (ping) before returning.
2. WHEN the config is invalid or the database is unreachable THEN `NewPool` SHALL return a wrapped
   `error` within a bounded connect timeout and SHALL NOT panic.
3. WHEN `WithTx(ctx, pool, fn)` runs and `fn` returns nil THEN system SHALL commit; WHEN `fn`
   returns an error or panics THEN system SHALL roll back and propagate the error/panic. The tx SHALL
   be stored in the `ctx` passed to `fn` (signature `fn func(ctx context.Context) error`), never handed to `fn` as a `pgx.Tx` argument.
4. WHEN `Executor(ctx, pool)` is called inside a `WithTx` THEN it SHALL return that tx; WHEN called
   outside any `WithTx` THEN it SHALL return the pool — so repositories are transaction-scoped inside `WithTx` and pool-scoped otherwise.

**Independent Test**: Start a bare Postgres container, call `NewPool` → ping succeeds; call
`WithTx` writing a temp row (via `Executor(ctx, pool)`) then returning an error → row absent (rolled
back); returning nil → row present; assert `Executor` returns a tx-backed querier inside `WithTx` and the pool outside it.

---

### P1: Migration stream, baseline & runner ⭐ MVP

**User Story**: As a platform engineer, I want an embedded, ordered, reversible migration stream
and a runner so that a clean database can be brought to the current schema version at startup or on demand.

**Why P1**: The M0 target is "skeleton migrates a clean DB." The stream + conventions gate every
later feature's schema change.

**Acceptance Criteria**:

1. WHEN a migration is added THEN it SHALL follow `NNNNNN_name.up.sql` / `NNNNNN_name.down.sql`
   (6-digit zero-padded sequence, snake_case) under `db/migrations/`, embedded via `embed.FS`.
2. WHEN the baseline migration (`000001_baseline`) applies THEN it SHALL enable `btree_gist` (up)
   and drop it (down), creating NO domain tables — enabling §7 exclusion constraints for later features.
3. WHEN `Up` runs against a clean DB THEN all pending up migrations SHALL apply in order and the
   version SHALL be recorded in `schema_migrations`; a second `Up` SHALL be a no-op (`ErrNoChange`).
4. WHEN `Down`/`Steps(-1)` runs THEN system SHALL revert the last applied migration.
5. WHEN `RunMigrations(ctx, dsn)` is invoked at startup and a migration fails (or state is dirty)
   THEN it SHALL return an error so the composition root can fail fast; on success it SHALL return nil.

**Independent Test**: Against a fresh container, `Up` → `btree_gist` present + version=1; `Up` again
→ no-op; `Down` → extension gone + version nil.

---

### P1: testcontainers-go integration harness ⭐ MVP

**User Story**: As a developer/CI pipeline, I want a reusable helper that spins real Postgres 16,
applies the production migrations, and isolates state per test so integration and concurrency tests
run against a faithful schema.

**Why P1**: TESTING.md mandates `testcontainers-go` for all `adapter`/migration/concurrency tests;
this helper is imported by every module's integration tests.

**Acceptance Criteria**:

1. WHEN a test calls the harness THEN it SHALL receive a ready `*pgxpool.Pool` connected to a
   Postgres 16 database that has the same embedded migrations applied (prod/test parity).
2. WHEN multiple tests in one package run use the harness THEN they SHALL share one container but
   get isolated state (snapshot taken post-migration; each test restored to it on cleanup), and the
   container SHALL be torn down automatically.
3. WHEN a concurrency test drives multiple real connections that commit against the shared database
   THEN the harness SHALL support it (real commits, contended rows) so last-vacancy races can be verified.

**Independent Test**: Two subtests each call the harness; test A inserts into a temp table, test B
sees a clean state (isolation); both observe `btree_gist` present (migrations applied).

---

### P2: sqlc configuration

**User Story**: As a platform engineer, I want `sqlc.yaml` configured for pgx/v5 with the migration
stream as the schema source so that each module can later generate type-safe query code consistently.

**Why P2**: No queries exist in M0; the config + per-module convention must exist before the first
domain feature, but produces no runtime code now.

**Acceptance Criteria**:

1. WHEN `sqlc.yaml` is present THEN it SHALL target engine `postgresql`, `sql_package: "pgx/v5"`,
   `schema: "db/migrations"`, `queries: "db/queries"`, and document the per-module output convention.
2. WHEN a module later adds queries under `db/queries/` THEN `sqlc generate` SHALL emit type-safe Go
   into that module's repository package per the documented convention.

**Independent Test**: `sqlc compile` (or `sqlc vet`) parses `sqlc.yaml` and the migration schema
without error (no queries yet → nothing generated).

---

### P2: migrate CLI (dev/ops)

**User Story**: As a developer/operator, I want a small `cmd/migrate` CLI so I can apply, revert,
inspect, and recover migration state outside the server startup path.

**Why P2**: Startup `RunMigrations` covers the automated path; the CLI is a dev/ops convenience
(down-migrations, version inspection, dirty-state recovery).

**Acceptance Criteria**:

1. WHEN `migrate up|down|version|force <N>` is run with a DSN THEN the CLI SHALL invoke the
   corresponding `Migrator` operation and exit non-zero on error.

**Independent Test**: `migrate up` then `migrate version` against a container prints version `1`.

---

## Edge Cases

- WHEN a migration fails mid-apply THEN golang-migrate SHALL mark the version **dirty**; `RunMigrations`
  at startup SHALL surface it as an error, and `migrate force <N>` SHALL allow manual recovery.
- WHEN `NewPool` cannot reach Postgres within the connect timeout THEN it SHALL return a wrapped error, not block indefinitely.
- WHEN the pool is exhausted (all `MaxConns` in use) THEN acquisition SHALL block on the caller's
  `ctx` and fail on ctx cancellation/deadline (pgxpool default) — no silent leak.
- WHEN two goroutines contend for the last vacancy in a concurrency test THEN the harness SHALL let
  both reach real committed transactions so exactly one winner can be asserted (PRD §13; enabled here, asserted by reservations).
- WHEN Docker is unavailable THEN the harness SHALL fail the test with a clear message (CI provides Docker).
- WHEN `Up` is called with no pending migrations THEN it SHALL return `ErrNoChange`, treated as success by `RunMigrations`.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| -------------- | ----- | ----- | ------ |
| DATA-01 | P1: Pool | Design | In Tasks |
| DATA-02 | P1: Pool | Design | In Tasks |
| DATA-03 | P1: Pool (ambient `WithTx` — tx in `ctx` — + `Executor(ctx, pool)` resolver) | Design | In Tasks |
| DATA-04 | P1: Migration stream | Design | In Tasks |
| DATA-05 | P1: Migration stream (baseline `btree_gist`) | Design | In Tasks |
| DATA-06 | P1: Migration stream (Up/idempotent) | Design | In Tasks |
| DATA-07 | P1: Migration stream (Down) | Design | In Tasks |
| DATA-08 | P1: Migration stream (startup hook, fail-fast) | Design | In Tasks |
| DATA-09 | P1: Harness (migrated pool, parity) | Design | In Tasks |
| DATA-10 | P1: Harness (per-test isolation, teardown) | Design | In Tasks |
| DATA-11 | P1: Harness (concurrency support) | Design | In Tasks |
| DATA-12 | P2: sqlc config | Design | In Tasks |
| DATA-13 | P2: migrate CLI | Design | In Tasks |

**ID format:** `DATA-NN`. **Status values:** Pending → In Design → In Tasks → Implementing → Verified.
**Coverage:** 13 total, 13 mapped to tasks (see tasks.md), 0 unmapped.

### PRD Traceability (foundation feature — no direct RF)

This is an M0 infrastructure feature; it implements **no RF directly** (per the dispatch brief).
It **enables** later requirements and NFRs:

- **ARCHITECTURE §7** (concurrency/consistency): the ambient `WithTx` + `Executor(ctx, pool)` seam
  for `SELECT … FOR UPDATE` / `pg_advisory_xact_lock` row locks that span a module boundary (M2:
  `reservation-creation` opens the tx, `availability` joins it via `ctx`); `btree_gist` baseline for
  exclusion-constraint + range-type overlap prevention; harness for concurrent-goroutine race tests.
  Consumed by RF04 (reservation creation), RF05, RF08/RF09 (availability/overbooking), RF10, RF12.
- **PRD §12** (NFR): transactional consistency + concurrency control (the technical foundation).
- **PRD §13** (casos extremos): last-vacancy race, temporary-hold expiry, cancellation release —
  all require the real-Postgres harness this feature provides; the rules themselves live downstream.
- **AD-002 / AD-005**: PostgreSQL 16, `pgx` + `sqlc` + `golang-migrate` + `testcontainers-go`.

---

## Success Criteria

- [ ] `go build ./...` succeeds; `RunMigrations` brings a clean Postgres 16 to version 1 (`btree_gist` enabled).
- [ ] `go test -tags=integration ./internal/platform/postgres/...` passes against a testcontainer (pool, tx, migrate up/down/idempotency, harness isolation).
- [ ] A second `RunMigrations` is a no-op; `Down` reverts baseline cleanly.
- [ ] Harness usable by an external package (module integration test) to obtain an isolated migrated pool.
- [ ] `sqlc.yaml` parses; `cmd/migrate up|version` works against a container.

---

## Open Decisions

- **Per-test isolation via testcontainers `Snapshot`/`Restore`** (one shared container, snapshot
  after migrations, `Restore` on each test's `t.Cleanup`) rather than a wrapping-transaction-rollback.
  Chosen because concurrency tests need real multi-connection commits, which a single wrapping tx
  cannot provide. Consequence: integration tests run **serially** (matches TESTING.md Parallelism
  Assessment: integration = not parallel-safe). Revisit only if suite runtime becomes a problem.
- **`postgres.Config` defined in this package** (provider-shaped), populated by `config-runtime`
  (RUN) / the composition root from env. Keeps the pool self-contained and unit-testable; RUN owns
  env parsing, we own the pool-tuning shape.
- **`btree_gist` enabled in the M0 baseline** (vs. deferring to the reservations feature). It is a
  DB-wide, cross-feature prerequisite (no domain schema), so it belongs to the foundational stream;
  keeps the exclusion-constraint capability ready without pre-designing domain tables.
- **`db/embed.go` (package `db`)** holds the `//go:embed migrations/*.sql` directive, because Go
  `embed` paths cannot traverse `..` from `internal/platform/postgres`. The runner imports it.
