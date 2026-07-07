# Testing — Strategy, Coverage Matrix & Gates

Greenfield Go project. **TDD is mandatory**: RED (write a failing test) → GREEN (minimal code to
pass) → REFACTOR. Tests are the spec; implementation conforms to tests. Never weaken/skip/delete
a test to go green (see coding-principles.md §Test Integrity).

## Test types

| Type | Tooling | Scope | Speed |
| ---- | ------- | ----- | ----- |
| **unit** | stdlib `testing`, table-driven | domain (entities/VOs/domain services), app (use cases with mocked ports) | fast, no I/O |
| **integration** | stdlib `testing` + `testcontainers-go` (real Postgres 16) | adapter/repository, adapter/http handlers, migrations, public-impl wiring, concurrency/locking | slower, real DB |

No separate e2e tier for MVP; handler-level integration tests (spin real Postgres, exercise the
HTTP route through the use case + repo) cover end-to-end behavior.

## Test Coverage Matrix (code layer → REQUIRED test type)

Every task that creates/modifies a layer below MUST include its tests in the same task
(co-located — tests are never a separate deferred task).

| Code layer | Required test type |
| ---------- | ------------------ |
| `domain` — entities, value objects, aggregates, domain services | **unit** |
| `app` — use cases / application services (ports mocked) | **unit** |
| `adapter/repository` — pgx/sqlc implementations, locking/overlap SQL | **integration** |
| `adapter/http` — chi handlers, htmx rendering | **integration** |
| `public` — cross-module interface implementation | **integration** |
| `db/migrations` — schema, exclusion constraints, range types | **integration** |
| `internal/platform/*` wiring, middleware | **integration** (unit where pure) |
| Concurrency (last-vacancy race, hold expiry) | **integration** (concurrent goroutines against real Postgres) |

If a task creates multiple layers, use the **highest** required test type.

## Gate Check Commands

| Gate | Command | When |
| ---- | ------- | ---- |
| **quick** | `go build ./... && go vet ./... && go test ./...` | after any task touching domain/app (unit) |
| **full** | `go test -tags=integration ./...` | after any task touching adapter/repository/http/public/migrations/concurrency |
| **build** | `go build ./...` | scaffolding-only tasks with no testable logic yet |

`go test ./...` runs unit tests (no build tag). Integration tests use `//go:build integration`
and run only under the `full` gate. A task's **Done when** must name the exact gate command and
the expected pass count (prevents silent test deletion).

## Parallelism Assessment (drives `[P]` flags in tasks.md)

| Test type | Parallel-safe? | Reason |
| --------- | -------------- | ------ |
| **unit** | **Yes** | pure, no shared state; `t.Parallel()` fine |
| **integration** | **No** (default) | shared Postgres testcontainer / schema state; run serially unless the task isolates state per test (own schema or wrapping tx rolled back) |
| **concurrency tests** | **No** | intentionally contend on the same rows |

Rule: a task whose required test type is **integration** must NOT carry `[P]` unless its design
explicitly isolates DB state per test. Unit-only tasks may be `[P]` when they have no code deps.

## TDD flow per task (Execute phase)

1. Write the failing test from the acceptance criteria / design contract (RED).
2. Implement the minimal domain/app/adapter code to pass (GREEN).
3. Refactor under green (REFACTOR) — keep KISS/DRY.
4. Run the task's gate command; confirm expected pass count.
5. One atomic commit per task (`feat(<module>): ...`).

## Test data & fakes

- Domain/app unit tests use **hand-written fakes** implementing the port interfaces (no mocking
  framework needed — KISS). Fakes honor the interface contract incl. error cases (LSP).
- Integration tests get a fresh migrated schema via a testcontainers helper in
  `internal/platform/postgres` (or a `testutil` package). Truncate/rollback between tests for isolation.
