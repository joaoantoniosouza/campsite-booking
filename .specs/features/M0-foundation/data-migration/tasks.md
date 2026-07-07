# Data & Migration Layer Tasks

**Design**: `.specs/features/M0-foundation/data-migration/design.md`
**Status**: Draft

All tasks follow TDD (RED → GREEN → REFACTOR); tests are co-located in the task that creates the
code (never deferred). Gate commands from TESTING.md: **quick** = `go build ./... && go vet ./... && go test ./...`;
**full** = `go test -tags=integration ./...`; **build** = `go build ./...`.

**Cross-feature note:** `go.mod` and `internal/platform/bootstrap` are owned by `project-skeleton`
(SKEL). Each task below adds its own external deps via `go get`. If `go.mod` does not yet exist,
`go mod init` is assumed done by SKEL; these tasks add `require` directives only.

---

## Execution Plan

### Phase 1: Foundation (Sequential — both integration, not parallel-safe)

Independent by code, but integration tests share Docker/host resources ⇒ run serially.

```
T1 (pool + tx)  ─┐
T2 (migration stream + runner) ─┘   (either order; serial execution)
```

### Phase 2: Consumers of the foundation

```
        ┌─→ T3 (harness)      [integration, serial]
T1,T2 ──┼─→ T4 (migrate CLI)  [integration, serial]
        └─→ T5 (sqlc.yaml)    [P] build-only
```

- `T3` depends on **T1 + T2**; `T4` and `T5` depend on **T2**.
- Only `T5` carries `[P]` (no tests, no shared DB state). `T3` and `T4` are integration ⇒ serial.

---

## Task Breakdown

### T1: pgx connection pool + transaction helper

**What**: `postgres.Config`, `NewPool` (parse DSN, apply tuning, ping within timeout), `Close`, `WithTx` (ambient — stores the `pgx.Tx` in `ctx`; commit/rollback/panic-safe; signature `fn func(ctx context.Context) error`), and `Executor(ctx, pool)` (resolves the tx from `ctx` if present, else the pool).
**Where**: `internal/platform/postgres/pool.go`, `internal/platform/postgres/tx.go` (+ `pool_test.go`, `tx_test.go`)
**Depends on**: None
**Reuses**: `pgx/v5`, `pgx/v5/pgxpool`
**Requirement**: DATA-01, DATA-02, DATA-03

**Tools**:

- MCP: `context7` (pgx pool API), NONE otherwise
- Skill: NONE

**Done when**:

- [ ] `NewPool` returns a pinged `*pgxpool.Pool`; zero-value tuning fields get sane defaults.
- [ ] `NewPool` with an unreachable/invalid DSN returns a wrapped error within `ConnectTimeout`, no panic (**unit**, table-driven).
- [ ] `WithTx` commits on nil, rolls back on error, rolls back + re-panics on panic; `fn` receives the tx via `ctx` (not as an argument) and writes through `Executor(ctx, pool)` (**integration**, bare inline container; test creates its own temp table).
- [ ] `Executor(ctx, pool)` returns a tx-backed querier when called inside `WithTx` and the pool when called outside it (**integration**).
- [ ] `go get github.com/jackc/pgx/v5` recorded in `go.mod`.
- [ ] Gate check passes: `go test -tags=integration ./internal/platform/postgres/...`
- [ ] Test count: 6 tests pass (2 unit error-path cases + 3 WithTx integration cases + 1 Executor resolution case), no silent deletions.

**Verify**: `go test -tags=integration ./internal/platform/postgres/ -run 'TestNewPool|TestWithTx|TestExecutor' -v` → PASS.

**Tests**: integration
**Gate**: full

**Commit**: `feat(postgres): add pgx pool constructor and ambient WithTx/Executor boundary`

---

### T2: Embedded migration stream, baseline & runner

**What**: `db/embed.go` (embed FS), `000001_baseline.up/.down.sql` (`btree_gist`), and `postgres.Migrator` (`NewMigrator`, `Up`, `Down`, `Steps`, `Version`, `Force`, `Close`) + `RunMigrations(ctx, dsn)`.
**Where**: `db/embed.go`, `db/migrations/000001_baseline.up.sql`, `db/migrations/000001_baseline.down.sql`, `internal/platform/postgres/migrate.go` (+ `migrate_test.go`)
**Depends on**: None
**Reuses**: golang-migrate `migrate/v4` + `source/iofs` + `database/pgx/v5`; `pgx/v5/stdlib`
**Requirement**: DATA-04, DATA-05, DATA-06, DATA-07, DATA-08

**Tools**:

- MCP: `context7` (golang-migrate iofs + pgx/v5 driver)
- Skill: NONE

**Done when**:

- [ ] Migration files follow `NNNNNN_name.up.sql`/`.down.sql`; `embed.FS` + `MigrationsDir` exported from package `db`.
- [ ] Baseline up enables `btree_gist`; down drops it; no domain tables created.
- [ ] `Up` on a clean DB → `btree_gist` present, `schema_migrations` version=1; second `Up` → `ErrNoChange` handled as success (**integration**).
- [ ] `Down` reverts baseline (extension gone, version nil); `Version` returns `(0,false,nil)` on empty DB (**integration**).
- [ ] `RunMigrations` returns nil on success, error on dirty/failed state (**integration**).
- [ ] `go get github.com/golang-migrate/migrate/v4` recorded in `go.mod`.
- [ ] Gate check passes: `go test -tags=integration ./internal/platform/postgres/...`
- [ ] Test count: 4 integration tests pass (up+idempotent, down+version, RunMigrations success, dirty/error), no silent deletions.

**Verify**: `go test -tags=integration ./internal/platform/postgres/ -run TestMigrate -v` → PASS; migrated DB has `btree_gist` in `pg_extension`.

**Tests**: integration
**Gate**: full

**Commit**: `feat(postgres): add embedded golang-migrate stream with btree_gist baseline`

---

### T3: testcontainers-go integration harness

**What**: `pgtest.Setup(t) *pgxpool.Pool` — shared Postgres 16 container (once), migrations applied, snapshot taken, per-test `Restore` isolation + pool, auto teardown.
**Where**: `internal/platform/postgres/pgtest/pgtest.go` (`//go:build integration`) (+ `pgtest_test.go`)
**Depends on**: T1, T2
**Reuses**: `postgres.NewPool` (T1), `postgres.RunMigrations` (T2), testcontainers-go `modules/postgres` (`Run`/`Snapshot`/`Restore`/`WithSQLDriver`), `pgx/v5/stdlib`
**Requirement**: DATA-09, DATA-10, DATA-11

**Tools**:

- MCP: `context7` (testcontainers postgres module)
- Skill: NONE

**Done when**:

- [ ] `Setup` returns a pool to a migrated Postgres 16 DB (`btree_gist` present ⇒ parity) (**integration**).
- [ ] Two subtests get isolated state: subtest A writes a temp row, subtest B (after Restore) does not see it (**integration**).
- [ ] Concurrency capability proven: N goroutines run committed `WithTx` inserts against the shared DB and all land (**integration**).
- [ ] Container reused across calls (single `Run`), terminated on package cleanup.
- [ ] `go get github.com/testcontainers/testcontainers-go` + `.../modules/postgres` recorded in `go.mod`.
- [ ] Gate check passes: `go test -tags=integration ./internal/platform/postgres/pgtest/...`
- [ ] Test count: 3 integration tests pass (migrated-pool, isolation, concurrency), no silent deletions.

**Verify**: `go test -tags=integration ./internal/platform/postgres/pgtest/ -v` → PASS; single container started for the package.

**Tests**: integration
**Gate**: full

**Commit**: `feat(postgres): add pgtest testcontainers harness with per-test snapshot isolation`

---

### T4: migrate CLI (dev/ops)

**What**: `cmd/migrate` with `up|down|version|force N`; DSN from `-dsn` flag or `DATABASE_URL`; thin over `Migrator`. Testable `run(args []string, dsn string) error` inner function.
**Where**: `cmd/migrate/main.go` (+ `main_test.go`, `//go:build integration`)
**Depends on**: T2
**Reuses**: `postgres.Migrator`, `postgres.NewMigrator` (T2)
**Requirement**: DATA-13

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `run(["up"], dsn)` then `run(["version"], dsn)` against a container applies baseline and reports version 1 (**integration**).
- [ ] Unknown subcommand / missing DSN → non-nil error (non-zero exit) (**integration**).
- [ ] Gate check passes: `go test -tags=integration ./cmd/migrate/...`
- [ ] Test count: 2 integration tests pass (up+version happy path, arg/DSN error), no silent deletions.

**Verify**: `go test -tags=integration ./cmd/migrate/ -v` → PASS; `go run ./cmd/migrate version -dsn "$DATABASE_URL"` prints `1`.

**Tests**: integration
**Gate**: full

**Commit**: `feat(migrate): add up/down/version/force CLI over the migration stream`

---

### T5: sqlc configuration [P]

**What**: Root `sqlc.yaml` (v2): engine postgresql, `sql_package: pgx/v5`, `schema: db/migrations`, `queries: db/queries`, emit flags, documented per-module `package`/`out` convention; `db/queries/.gitkeep`.
**Where**: `sqlc.yaml`, `db/queries/.gitkeep`
**Depends on**: T2 (schema source = `db/migrations` must exist)
**Reuses**: `sqlc` v2 config schema
**Requirement**: DATA-12

**Tools**:

- MCP: `context7` (sqlc v2 config)
- Skill: NONE

**Done when**:

- [ ] `sqlc.yaml` targets pgx/v5 + `db/migrations` schema + `db/queries` queries; per-module output convention documented in a comment.
- [ ] `sqlc compile` (or `sqlc vet`) parses config + baseline schema with no error (no queries ⇒ nothing generated).
- [ ] No generated Go committed in M0 (deferred to first module feature — documented).
- [ ] Gate check passes: `go build ./...`
- [ ] Test count: N/A (config only; no code layer created — coverage matrix requires no test type).

**Verify**: `sqlc compile` exits 0; `go build ./...` succeeds.

**Tests**: none
**Gate**: build

**Commit**: `chore(sqlc): add sqlc.yaml pgx/v5 config and per-module query convention`

---

## Parallel Execution Map

```
Phase 1 (Sequential — integration, not parallel-safe):
  T1 ──→ T2          (no code dependency; serialized by integration rule)

Phase 2 (after T1,T2):
  T3  (integration, serial)
  T4  (integration, serial)   } T3, T4 run one at a time
  T5 [P] (build-only)         } may run concurrently with T3/T4
```

**Parallelism constraint:** `[P]` requires no unfinished deps, parallel-safe test type, and no
shared mutable state. Only **T5** qualifies (no tests, no DB). T1–T4 are integration ⇒ serial per
TESTING.md, regardless of code independence — the shared container/host is the bottleneck.

---

## Task Granularity Check

| Task | Scope | Status |
| ---- | ----- | ------ |
| T1: pool + WithTx/Executor | 1 package concept (connectivity: 2 cohesive files) | ✅ Granular |
| T2: migration stream + runner | 1 concept (embedded stream + runner: SQL + embed + runner file) | ✅ Granular |
| T3: pgtest harness | 1 package (harness helper) | ✅ Granular |
| T4: migrate CLI | 1 command (main + run) | ✅ Granular |
| T5: sqlc.yaml | 1 config file | ✅ Granular |

Two files in T1/T2 are cohesive around a single concept (connectivity / migration stream) and are
tested as a unit — within the "2–3 related things if cohesive" allowance. No task spans multiple concepts.

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| ---- | ---------------------- | ------------- | ------ |
| T1 | None | root (feeds T3) | ✅ Match |
| T2 | None | root (feeds T3, T4, T5) | ✅ Match |
| T3 | T1, T2 | `T1,T2 → T3` | ✅ Match |
| T4 | T2 | `T2 → T4` | ✅ Match |
| T5 | T2 | `T2 → T5 [P]` | ✅ Match |

- Every `Depends on` has a matching arrow; every arrow maps to a `Depends on`.
- `[P]` task (T5) does not depend on any other `[P]` task (it is the only one). ✅
- T1 and T2 share no dependency edge and are not marked `[P]` (both integration → serial). ✅

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| ---- | --------------------------- | --------------- | --------- | ------ |
| T1 | `internal/platform/postgres` (pool + tx; ambient `WithTx` + `Executor(ctx)` = tx boundary) | integration (platform wiring; unit where pure) | integration (+ unit error-path co-located) | ✅ OK |
| T2 | `db/migrations` + migration runner | integration (migrations; schema) | integration | ✅ OK |
| T3 | `internal/platform/postgres/pgtest` harness | integration | integration | ✅ OK |
| T4 | `cmd/migrate` (platform/CLI wiring) | integration | integration | ✅ OK |
| T5 | `sqlc.yaml` (config; no Go code layer) | none | none | ✅ OK |

- No task uses `Tests: none` as a deferral — T5 is config-only (no code layer with a required test type).
- T1 creates a pure unit-testable path (NewPool error branch) **and** an integration path (WithTx/Executor); it uses the **highest** type (integration) as its gate while co-locating the unit test. ✅
- Every requirement (DATA-01…DATA-13) is covered by a task; each task cites its requirement IDs. ✅
