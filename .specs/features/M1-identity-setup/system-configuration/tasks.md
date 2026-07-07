# System Configuration Tasks

**Design**: `.specs/features/M1-identity-setup/system-configuration/design.md`
**Status**: Draft

All tasks follow TDD (RED → GREEN → REFACTOR); tests are co-located in the task that creates the
code (never deferred). Gate commands (TESTING.md): **quick** = `go build ./... && go vet ./... && go test ./...`;
**full** = `go test -tags=integration ./...`; **build** = `go build ./...`.

**Cross-feature notes:** `identity/public` (Administrator accessor / `EnsureAdministrator`) is
authored by the sibling `authentication` feature; T8/T9 consume it via the consumer-owned
`Authorizer` port (or an adapter wired at the composition root). The `config` migration number is
the next free slot in the single gap-free stream after `000001_baseline` — coordinate with sibling
M1 migrations (`identity`, `campsites`) at Execute time; `0000NN` below is a placeholder. `config`
has **no** foreign keys, so its ordering among M1 features is independent.

---

## Execution Plan

### Phase 1: Domain (Sequential — unit)

```
T1 (VOs + errors) ──→ T2 (Configuration aggregate + defaults + repo interface)
```

### Phase 2: App surfaces (Parallel — unit)

```
        ┌─→ T3 (public.Provider + ProviderService) [P]
T2 ─────┤
        └─→ T4 (GetConfiguration + result DTOs)     [P]
T4 ──────→ T5 (UpdateConfiguration + Authorizer/TxRunner ports)
```

### Phase 3: Persistence (Sequential — integration)

```
T2 ──→ T6 (migration + seed) ──→ T7 (pgx repository + sqlc)
```

### Phase 4: Admin HTTP (Sequential — integration)

```
T4,T5,T7 ──→ T8 (admin edit handler + templates, behind identity admin guard)
```

### Phase 5: Wiring (Sequential — integration)

```
T3,T7,T8 ──→ T9 (composition-root wiring + route mount + provider export)
```

- `[P]` only on T3–T4 (unit, depend solely on T2, mutually independent). T5 reuses the result DTO
  from T4. All integration tasks (T6–T9) run serially per TESTING.md (shared Postgres container ⇒
  not parallel-safe).

---

## Task Breakdown

### T1: Configuration value objects + sentinel errors

**What**: `ReservationLimits{PF,PJ}` (`PF>0`, `PJ>=PF`), `SwapRules`/`SwapBracket` (ascending by `UpTo`, non-decreasing `Swaps`, covers 1..`coverUpTo`) with `LimitFor(groupSize)` step function, `BookingWindow{MonthsAhead}` (`>=0`) with `EndFrom(now)`; domain sentinel errors.
**Where**: `internal/modules/config/domain/{reservation_limits.go,swap_rules.go,booking_window.go,errors.go}` (+ `*_test.go`)
**Depends on**: None
**Reuses**: stdlib only (`time`, `errors`)
**Requirement**: CFG-01 (typed settings VOs), CFG-03 (invariants), CFG-04 (`SwapRules.LimitFor`, `BookingWindow.EndFrom`)

**Tools**:

- MCP: NONE
- Skill: `tactical-ddd` (verify self-validating, non-anemic VOs)

**Done when**:

- [ ] `NewReservationLimits`: `(5,15)` ok; `(0,15)`→`ErrInvalidReservationLimits`; `(10,5)`→error (**unit**, table-driven).
- [ ] `NewSwapRules`: default brackets `{UpTo5→1,UpTo10→2,UpTo15→3}` ok; non-ascending / gap / not covering `coverUpTo` → `ErrInvalidSwapRules` (**unit**).
- [ ] `SwapRules.LimitFor`: boundaries 1→1, 5→1, 6→2, 10→2, 11→3, 15→3, 99→3 (clamp), 0→0 (**unit**).
- [ ] `NewBookingWindow(2)` ok; `NewBookingWindow(-1)`→`ErrInvalidBookingWindow`; `EndFrom(2026-07-15)` → `2026-09-30` (last day of month+2) (**unit**).
- [ ] Sentinels defined: `ErrInvalidReservationLimits`, `ErrInvalidSwapRules`, `ErrInvalidBookingWindow`, `ErrInvalidHoldTTL`, `ErrInvalidDeadline`, `ErrInvalidOverbooking`, `ErrConfigurationNotFound`.
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: ≥ 8 unit cases pass (limits 3, swap-rules 3, swap-limit-for boundaries 1 table, booking-window 2), no silent deletions.

**Verify**: `go test ./internal/modules/config/domain/ -run 'TestReservationLimits|TestSwapRules|TestBookingWindow' -v` → PASS.

**Tests**: unit
**Gate**: quick

**Commit**: `feat(config): add reservation-limits, swap-rules and booking-window value objects`

---

### T2: Configuration aggregate + defaults + behavior + repository interface

**What**: `Configuration` aggregate (unexported fields, accessors incl. `Window() BookingWindow` exposing raw `Months()`), `NewConfiguration` (validates `holdTTL>0`, deadlines `>=0`, `overbookingPercent` 0..100, swap covers PJ), `DefaultConfiguration()` (PRD defaults), `ReconstituteConfiguration(...)`, `Apply(ConfigurationChange)`; derived behavior `SwapLimitFor`, `CancellationDeadlineAt`, `ChangeDeadlineAt` (booking-window date math is **not** here — availability owns it); `ConfigurationRepository` interface (`Load`, `Save`).
**Where**: `internal/modules/config/domain/{configuration.go,defaults.go,repository.go}` (+ `configuration_test.go`)
**Depends on**: T1
**Reuses**: T1 VOs; stdlib (`time`, `errors`)
**Requirement**: CFG-01 (aggregate composes settings), CFG-02 (`DefaultConfiguration`), CFG-03 (cross-field invariants, no partial aggregate), CFG-04 (deadline behavior), CFG-07 (repository interface declared)

**Tools**:

- MCP: NONE
- Skill: `tactical-ddd` (verify rich aggregate: behavior lives here, not in consumers)

**Done when**:

- [ ] `DefaultConfiguration()` returns PRD defaults (PF 5, PJ 15, window 2, cancel 24h, change 24h, hold 10m, swap 1/2/3, default overbooking) and never errors (**unit**).
- [ ] `NewConfiguration` rejects each invariant violation with the matching sentinel and returns no partial aggregate: `holdTTL=0`→`ErrInvalidHoldTTL`, `deadline<0`→`ErrInvalidDeadline`, `overbooking=101`→`ErrInvalidOverbooking` (**unit**, table-driven).
- [ ] Behavior: `CancellationDeadlineAt(entry)` = `entry - 24h`; `ChangeDeadlineAt(entry)` = `entry - 24h`; `SwapLimitFor` delegates to the VO; `Window().Months()` returns the raw horizon (no date arithmetic here — availability owns the window end) (**unit**).
- [ ] `Apply` with an invalid change leaves the aggregate unchanged and returns the sentinel (**unit**).
- [ ] `ConfigurationRepository` interface declares `Load(ctx) (*Configuration, error)` and `Save(ctx, *Configuration) error`.
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: ≥ 6 unit cases pass (defaults, 3 invariants, deadline math, apply-rollback), no silent deletions.

**Verify**: `go test ./internal/modules/config/domain/ -run TestConfiguration -v` → PASS.

**Tests**: unit
**Gate**: quick

**Commit**: `feat(config): add Configuration aggregate with defaults and derived-rule behavior`

---

### T3: public.Provider + ProviderService [P]

**What**: `config/public` (`Provider` interface + flat `Limits{PF,PJ}` DTO, stdlib-only) + `app.ProviderService` implementing it by loading the aggregate and delegating to domain behavior, mapping to flat DTOs; fail-closed on load error.
**Where**: `internal/modules/config/public/provider.go`, `internal/modules/config/app/provider_service.go` (+ `provider_service_test.go`)
**Depends on**: T2
**Reuses**: `domain` (aggregate + `ConfigurationRepository`); hand-written fake repo (LSP)
**Requirement**: CFG-08 (public interface + DTOs), CFG-09 (ProviderService, fail-closed), CFG-04 (delegation), CFG-10 (satisfies narrow consumer subsets — ISP)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Over a fake repo seeded with `DefaultConfiguration()`: `ReservationLimits`→`{5,15}`, `HoldTTL`→10m, `CancellationDeadline`/`ChangeDeadline`→24h, `SwapLimitFor(7)`→2, `BookingWindowMonths`→`2`, `DefaultOverbookingPercent`→default (**unit**).
- [ ] When the fake repo returns `ErrConfigurationNotFound`, **every** provider method returns the error (fail closed) and never a fabricated default (**unit**).
- [ ] Compile-time assertions: `var _ public.Provider = (*ProviderService)(nil)`; a sample narrow subset interface (e.g. `interface{ SwapLimitFor(...) }`) is satisfied by `*ProviderService` (ISP) (**unit**, compiles).
- [ ] `public` imports stdlib only (`context`, `time`) — no domain import (compile-enforced).
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: ≥ 3 unit cases pass (all getters incl. `BookingWindowMonths`, fail-closed, boundary swap), no silent deletions.

**Verify**: `go test ./internal/modules/config/app/ -run TestProviderService -v` → PASS.

**Tests**: unit
**Gate**: quick

**Commit**: `feat(config): expose public Provider and fail-closed ProviderService`

---

### T4: GetConfiguration use case + result DTOs [P]

**What**: `GetConfiguration` use case + `ConfigurationView`/`SwapBracketDTO` flat result DTOs; loads the aggregate and maps it to the view.
**Where**: `internal/modules/config/app/get_configuration.go`, `internal/modules/config/app/dto.go` (+ `get_configuration_test.go`)
**Depends on**: T2
**Reuses**: `domain`, fake repo
**Requirement**: CFG-11

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `Execute(ctx)` over a seeded fake repo returns a `ConfigurationView` with every current value (limits, window months, durations, overbooking, swap brackets) (**unit**).
- [ ] Load error is propagated (fail closed), no fabricated view (**unit**).
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: 2 unit cases pass (happy view, load-error), no silent deletions.

**Verify**: `go test ./internal/modules/config/app/ -run TestGetConfiguration -v` → PASS.

**Tests**: unit
**Gate**: quick

**Commit**: `feat(config): add GetConfiguration use case with flat view DTO`

---

### T5: UpdateConfiguration use case + Authorizer & TxRunner ports

**What**: `UpdateConfiguration` use case + `UpdateConfigurationCommand`; consumer-owned `Authorizer` (`EnsureAdministrator(ctx) error`) and `TxRunner` ports. Flow: `authz.EnsureAdministrator` → load aggregate → build/validate settings + `Apply` → `repo.Save` inside one transaction; returns the updated `ConfigurationView`.
**Where**: `internal/modules/config/app/update_configuration.go`, `internal/modules/config/app/ports.go` (+ `update_configuration_test.go`)
**Depends on**: T2, T4 (reuses `ConfigurationView`)
**Reuses**: `domain` validation, fake repo, fake `Authorizer`, fake `TxRunner`
**Requirement**: CFG-12 (validated update in a transaction; reject invalid without persisting), CFG-13 (Administrator-only via `Authorizer`)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Valid command by an authorized admin → `Apply` + `Save` invoked in the tx; returns the updated view (**unit**).
- [ ] Invalid command (e.g. PF>PJ, holdTTL 0) → domain sentinel returned, `Save` NOT called, store unchanged (**unit**).
- [ ] `Authorizer.EnsureAdministrator` error → returned before any load/mutation; `Save` NOT called (**unit**).
- [ ] `TxRunner` failure rolls back (no partial persist), wrapped error returned (**unit**).
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: 4 unit cases pass (happy, invalid, unauthorized, tx-failure), no silent deletions.

**Verify**: `go test ./internal/modules/config/app/ -run TestUpdateConfiguration -v` → PASS.

**Tests**: unit
**Gate**: quick

**Commit**: `feat(config): add UpdateConfiguration use case with authz and tx ports`

---

### T6: system_configuration migration + seed

**What**: `0000NN_system_configuration.up.sql` — single-row table with typed columns, CHECK constraints mirroring scalar invariants (`pf>0`, `pj>=pf`, `months>=0`, `hold_ttl>interval '0'`, deadlines `>=0`, `overbooking BETWEEN 0 AND 100`, `swap_brackets` is a jsonb array), singleton guard (`id boolean PK DEFAULT true CHECK (id)`), and a seed `INSERT` mirroring `DefaultConfiguration()`; `.down.sql` drops the table.
**Where**: `db/migrations/0000NN_system_configuration.up.sql`, `db/migrations/0000NN_system_configuration.down.sql`
**Depends on**: T2 (field/invariant + seed-value alignment)
**Reuses**: golang-migrate stream + `pgtest.Setup` (DATA-04, DATA-09)
**Requirement**: CFG-05 (table + CHECKs + singleton), CFG-06 (seed row)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `up` creates `system_configuration` with columns + CHECKs per design and seeds exactly one row (**integration**).
- [ ] The singleton guard rejects a second row insert; a CHECK rejects an out-of-range value (e.g. `overbooking_percent=200`, `reservation_limit_pj < reservation_limit_pf`) (**integration**).
- [ ] Seeded values equal the PRD defaults (5/15/2, 10m/24h/24h, swap 1/2/3, default overbooking) (**integration**).
- [ ] `down` drops the table; migration is reversible (**integration**).
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 4 integration cases pass (table+seed present, singleton/CHECK rejects, seed values, down reverts), no silent deletions.

**Verify**: `go test -tags=integration ./internal/modules/config/adapter/repository/ -run TestConfigMigration -v` → PASS.

**Tests**: integration
**Gate**: full

**Commit**: `feat(config): add system_configuration table migration with seed`

---

### T7: pgx ConfigurationRepository + sqlc queries

**What**: `db/queries/config.sql` (`GetConfiguration` `SELECT ... WHERE id = true`, `UpdateConfiguration` `UPDATE ... SET ..., updated_at = now() WHERE id = true RETURNING *`) + a `config` block in `sqlc.yaml`; pgx `Repository` implementing `domain.ConfigurationRepository` — `Load` maps `interval`→`time.Duration` and `jsonb`→`[]SwapBracket` then `ReconstituteConfiguration`, no row → `ErrConfigurationNotFound`; `Save` `UPDATE`s the singleton from accessors.
**Where**: `db/queries/config.sql`, `sqlc.yaml` (extend), `internal/modules/config/adapter/repository/configuration_repository.go` (+ `configuration_repository_test.go`, `//go:build integration`)
**Depends on**: T2, T6
**Reuses**: `postgres.WithTx` + pool, generated sqlc, `pgtest.Setup`, `domain.ReconstituteConfiguration`
**Requirement**: CFG-07 (pgx impl, row↔aggregate mapping), CFG-06 (seed/domain parity)

**Tools**:

- MCP: `context7` (sqlc + pgx/v5 interval & jsonb mapping)
- Skill: NONE

**Done when**:

- [ ] Against a migrated+seeded testcontainer, `Load()` returns an aggregate **equal to** `DefaultConfiguration()` (seed/domain parity, drift-guarded) (**integration**).
- [ ] `Save`→`Load` round-trips modified values with `interval`↔`Duration` and `jsonb`↔`SwapRules` intact (**integration**).
- [ ] With the singleton row deleted, `Load` returns `ErrConfigurationNotFound` (**integration**).
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 3 integration cases pass (load-parity, save-load round-trip, not-found), no silent deletions.

**Verify**: `go test -tags=integration ./internal/modules/config/adapter/repository/ -run TestConfigurationRepository -v` → PASS.

**Tests**: integration
**Gate**: full

**Commit**: `feat(config): add pgx ConfigurationRepository with sqlc queries`

---

### T8: Admin edit handler + templates

**What**: thin htmx `adapter/http` handler — `GET /admin/config` renders current values, `POST /admin/config` parses the form → `UpdateConfigurationCommand` → `UpdateConfiguration.Execute`; on validation error render the form fragment naming offending field(s), on success render a success fragment; mounted behind the identity Administrator guard. `Authorizer` satisfied by `identity/public`.
**Where**: `internal/modules/config/adapter/http/configuration_handler.go`, `web/templates/config/{edit.html,_form.html}` (+ `configuration_handler_test.go`, `//go:build integration`)
**Depends on**: T4, T5, T7
**Reuses**: `app` use cases (T4, T5), repo (T7), `identity/public` guard, `internal/platform/web` renderer, `pgtest.Setup`
**Requirement**: CFG-14 (GET/POST admin surface), CFG-13 (admin-only enforced end-to-end)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `GET` as an Administrator renders the current seeded values (full page on direct nav, fragment on `HX-Request`) (**integration**, real DB via harness).
- [ ] `POST` valid values as an Administrator → persisted (verified via repo/provider) + success fragment (**integration**).
- [ ] `POST` invalid values → 422 form fragment naming the field, store unchanged (**integration**).
- [ ] `POST` by a non-admin / absent principal → 403, use case not invoked, store unchanged (**integration**).
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 4 integration cases pass (get, post-valid, post-invalid, non-admin denied), no silent deletions.

**Verify**: `go test -tags=integration ./internal/modules/config/adapter/http/ -run TestConfigurationHandler -v` → PASS.

**Tests**: integration
**Gate**: full

**Commit**: `feat(config): add admin configuration edit htmx handler`

---

### T9: Composition-root wiring + route mount + provider export

**What**: `config` `bootstrap.Module` impl: construct repo → `Get`/`Update` use cases + `ProviderService`, adapt `identity/public` into the `Authorizer` port, mount `/admin/config` behind the Administrator group, and expose `config/public.Provider` for M2 consumers (availability/reservations/checkin).
**Where**: `internal/platform/bootstrap/` (extend; e.g. `config.go` module wiring)
**Depends on**: T3, T7, T8
**Reuses**: `bootstrap.Module` seam (SKEL), `postgres` pool, `identity/public`, all config layers
**Requirement**: CFG-09 (provider wired/exported), CFG-13 (authz wired), CFG-14 (route live)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Bootstrap constructs the module and mounts `/admin/config` behind the Administrator guard (**integration**, full app via `App.Handler()`).
- [ ] End-to-end through the mounted router (admin principal): GET current → POST a valid change → the value is reflected by a subsequent `config/public.Provider` read (**integration**).
- [ ] `config/public.Provider` is retrievable from the composition root for downstream modules (**integration**).
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 2 integration cases pass (route mounted + guarded, end-to-end update reflected by provider), no silent deletions.

**Verify**: `go test -tags=integration ./internal/platform/bootstrap/ -run TestConfigWiring -v` → PASS.

**Tests**: integration
**Gate**: full

**Commit**: `feat(config): wire system-configuration module into the composition root`

---

## Parallel Execution Map

```
Phase 1 (Sequential — unit):
  T1 ──→ T2

Phase 2 (Parallel — unit, T3/T4 depend only on T2):
  T2 ──┬── T3 [P]
       └── T4 [P] ──→ T5   (T5 reuses T4's ConfigurationView)

Phase 3 (Sequential — integration):
  T2 ──→ T6 ──→ T7

Phase 4 (Sequential — integration):
  T4,T5,T7 ──→ T8

Phase 5 (Sequential — integration):
  T3,T7,T8 ──→ T9
```

**Parallelism constraint:** `[P]` requires no unfinished deps, a parallel-safe test type, and no
shared mutable state. Only **T3–T4** qualify (unit, depend solely on T2, mutually independent). T5
depends on T4 (shared DTO). T6–T9 are integration ⇒ serial per TESTING.md (shared Postgres
container is the bottleneck), regardless of code independence.

---

## Task Granularity Check

| Task | Scope | Status |
| ---- | ----- | ------ |
| T1: VOs + errors | 3 cohesive VOs + sentinels (1 concept: domain primitives) | ✅ Granular |
| T2: Configuration aggregate + defaults + repo interface | 1 aggregate + factory + its port (cohesive) | ✅ Granular |
| T3: public Provider + ProviderService | 1 port + its read impl | ✅ Granular |
| T4: GetConfiguration + DTOs | 1 read use case + its view DTO | ✅ Granular |
| T5: UpdateConfiguration + ports | 1 write use case + its two ports | ✅ Granular |
| T6: migration + seed | 1 migration (up+down) | ✅ Granular |
| T7: pgx repo + queries | 1 repository impl + its SQL | ✅ Granular |
| T8: admin handler + templates | 2 cohesive endpoints (one resource, GET+POST) | ✅ Granular |
| T9: wiring | 1 composition-root module | ✅ Granular |

Multi-item tasks (T1 VOs, T8 GET+POST) are cohesive around a single concept and tested together —
within the "2–3 related things if cohesive" allowance.

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| ---- | ---------------------- | ------------- | ------ |
| T1 | None | root (→ T2) | ✅ Match |
| T2 | T1 | `T1 → T2` (→ T3,T4,T6) | ✅ Match |
| T3 | T2 | `T2 → T3 [P]` (→ T9) | ✅ Match |
| T4 | T2 | `T2 → T4 [P]` (→ T5,T8) | ✅ Match |
| T5 | T2, T4 | `T4 → T5` | ✅ Match |
| T6 | T2 | `T2 → T6` (→ T7) | ✅ Match |
| T7 | T2, T6 | `T6 → T7` (T2 upstream) (→ T8,T9) | ✅ Match |
| T8 | T4, T5, T7 | `T4,T5,T7 → T8` (→ T9) | ✅ Match |
| T9 | T3, T7, T8 | `T3,T7,T8 → T9` | ✅ Match |

- Every `Depends on` has a matching arrow; every arrow maps to a `Depends on`.
- `[P]` tasks (T3–T4) depend only on T2, never on each other. ✅
- Integration tasks carry no `[P]` (T6–T9). ✅

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| ---- | --------------------------- | --------------- | --------- | ------ |
| T1 | `domain` (VOs) | unit | unit | ✅ OK |
| T2 | `domain` (aggregate + repo interface) | unit | unit | ✅ OK |
| T3 | `public` (iface) + `app` (ProviderService, fake repo) | unit | unit | ✅ OK |
| T4 | `app` (read use case) | unit | unit | ✅ OK |
| T5 | `app` (write use case, ports faked) | unit | unit | ✅ OK |
| T6 | `db/migrations` | integration | integration | ✅ OK |
| T7 | `adapter/repository` | integration | integration | ✅ OK |
| T8 | `adapter/http` (handler + templates) | integration | integration | ✅ OK |
| T9 | `internal/platform/bootstrap` wiring | integration | integration | ✅ OK |

- No task uses `Tests: none` (every task creates a code layer with a required test type).
- T3 creates `public` (interface only, no test needed) **and** `app.ProviderService` (unit-testable
  with a fake repo, no I/O) — unit is the correct (and highest) required type. ✅
- Every requirement CFG-01…CFG-14 is covered by ≥1 task, and each task cites its requirement IDs. ✅

---

## Requirement Coverage

| Requirement | Task(s) |
| ----------- | ------- |
| CFG-01 | T1, T2 |
| CFG-02 | T2, T7 |
| CFG-03 | T1, T2 |
| CFG-04 | T1, T2, T3 |
| CFG-05 | T6 |
| CFG-06 | T6, T7 |
| CFG-07 | T2, T7 |
| CFG-08 | T3 |
| CFG-09 | T3, T9 |
| CFG-10 | T3 |
| CFG-11 | T4 |
| CFG-12 | T5 |
| CFG-13 | T5, T8, T9 |
| CFG-14 | T8, T9 |

All 14 requirements mapped; 9 tasks, 0 unmapped.
