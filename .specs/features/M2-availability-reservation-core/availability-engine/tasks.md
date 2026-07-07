# Availability Engine Tasks

**Design**: `.specs/features/M2-availability-reservation-core/availability-engine/design.md`
**Status**: Draft

All tasks follow TDD (RED → GREEN → REFACTOR); tests are co-located in the task that creates the
code (never deferred). Gate commands (TESTING.md): **quick** = `go build ./... && go vet ./... && go test ./...`;
**full** = `go test -tags=integration ./...`; **build** = `go build ./...`.

**Cross-feature / platform notes:**

- `internal/shared/booking.Period` (shared-kernel date-range VO) is reused, not re-implemented (CONVENTIONS §DRY). If M0/foundation has not shipped it, T1 seeds the minimal VO and coordinates ownership (spec Open Decisions). It carries **no** occupancy rule.
- The **ambient-tx seam** (`postgres.WithTx` storing the tx in `ctx` + `postgres.Executor(ctx)` returning the ambient pgx querier) is required by T8/T9/T10. `WithTx` exists (used by campsite-management); if `Executor(ctx)` is absent it is a small platform (infra) shim coordinated with SKEL/DATA at Execute time — `reservation-creation` needs the same seam.
- Consumed ports: `campsites/public.Provider` (satisfies the narrow `CampsiteCapacity` structurally) and `config/public` (adapted to `WindowConfig`) — wired at the composition root in T12; mocked in unit tasks.
- The `daily_occupancy` migration number is the next free slot in the single gap-free stream after the M1 migrations — coordinate with the sibling M2 `reservations` migration at Execute time; `0000NN` below is a placeholder.

---

## Execution Plan

### Phase 1: Domain (unit)

```
        ┌─→ T2 (DailyOccupancy + PeriodOccupancy + repo interface) [P]
T1 ─────┤
(Diaria+expander)
        └─→ T3 (BookingWindow VO) [P]
```

### Phase 2: App use cases (unit)

```
              ┌─→ T5 (ReleaseOccupancy)        [P]
T2,T3 ─→ T4 ──┤
(Reserve+ports)
              └─→ T6 (GetAvailabilityCalendar) [P]
```

### Phase 3: Persistence, public & concurrency (integration)

```
T2 ─→ T7 (migration) ─→ T8 (pgx OccupancyRepository) ─┐
                                                      ├─→ T9 (public Reserver/Reader + provider) ─→ T10 (concurrency)
T4,T5,T6 ─────────────────────────────────────────────┘
```

### Phase 4: HTTP (integration)

```
T6,T8 ─→ T11 (calendar htmx handler + templates)
```

### Phase 5: Wiring (integration)

```
T9,T11 ─→ T12 (composition-root wiring + route + expose public ports)
```

- `[P]` only on T2/T3 (unit, depend solely on T1, mutually independent) and T5/T6 (unit, depend solely on T4, mutually independent). All integration tasks (T7–T12) run serially per TESTING.md (shared Postgres container ⇒ not parallel-safe); the concurrency task (T10) is intentionally contended ⇒ never `[P]`.

---

## Task Breakdown

### T1: Diaria VO + Período→Diárias expander + domain errors

**What**: `Diaria` VO (calendar-date-keyed, `NewDiaria`, `Date/Next/Before/Equal`), the `Diarias(booking.Period) []Diaria` expander (checkout-excluded `[Entry, Exit)`), and domain sentinel errors.
**Where**: `internal/modules/availability/domain/{diaria.go,errors.go}` (+ `diaria_test.go`)
**Depends on**: None
**Reuses**: `internal/shared/booking.Period` (reuse or seed per Open Decisions), stdlib
**Requirement**: AVL-01

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `Diarias` on `2026-07-10 → 2026-07-13` returns `{07-10,07-11,07-12}` (3 Diárias; checkout excluded) (**unit**).
- [ ] `Diarias` on a one-night período (`Exit == Entry+1d`) returns exactly 1 Diária (**unit**).
- [ ] `Diaria` normalizes time-of-day (two same-date instants compare/`Equal` and are the same map key); `Next()` advances one day (**unit**).
- [ ] Sentinels defined: `ErrNoVacancy`, `ErrOutsideBookingWindow`, `ErrInvalidHeadcount`.
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: 4 unit cases pass (3-night, 1-night, normalization/Next, expander ordering), no silent deletions.

**Verify**: `go test ./internal/modules/availability/domain/ -run 'TestDiaria|TestDiarias' -v` → PASS.

**Tests**: unit
**Gate**: quick

**Commit**: `feat(availability): add Diaria value object and Período→Diárias expander`

---

### T2: DailyOccupancy entity + PeriodOccupancy aggregate + repository interface [P]

**What**: `DailyOccupancy` entity (`Available`, `CanAccommodate`, floored `reserve`/`release`), the `PeriodOccupancy` aggregate (all-or-nothing `Reserve(headcount) error` → `ErrNoVacancy` with no mutation; `Release(headcount)`), and the `OccupancyRepository` interface.
**Where**: `internal/modules/availability/domain/{occupancy.go,repository.go}` (+ `occupancy_test.go`)
**Depends on**: T1
**Reuses**: T1 `Diaria`; sentinel errors
**Requirement**: AVL-02, AVL-03

**Tools**:

- MCP: NONE
- Skill: `tactical-ddd` (verify rich, non-anemic aggregate — behavior on the root, not a service)

**Done when**:

- [ ] `DailyOccupancy.CanAccommodate` true at/below the ceiling, false above; `Available()` = `max(0, effective−occupied)` (floors when over-capacity) (**unit**).
- [ ] `PeriodOccupancy.Reserve(h)` increments every day when all fit; if **any** day is full it returns `ErrNoVacancy` and mutates **nothing** (all-or-nothing) (**unit**).
- [ ] `PeriodOccupancy.Release(h)` decrements every day, floored at 0 (**unit**).
- [ ] `OccupancyRepository` interface declares `AdvisoryLock`, `LoadOccupancy`, `AddOccupancy`, `LoadRange`.
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: 6 unit cases pass (2 CanAccommodate, 1 Available-floor, 1 reserve-all-ok, 1 reserve-one-full-no-mutation, 1 release-floor), no silent deletions.

**Verify**: `go test ./internal/modules/availability/domain/ -run 'TestDailyOccupancy|TestPeriodOccupancy' -v` → PASS.

**Tests**: unit
**Gate**: quick

**Commit**: `feat(availability): add DailyOccupancy and all-or-nothing PeriodOccupancy aggregate`

---

### T3: BookingWindow value object [P]

**What**: `BookingWindow` VO (`NewBookingWindow(months)`, `Contains(Diaria, now) bool`, `Validate(booking.Period, now) error` → `ErrOutsideBookingWindow`); upper bound = last day of `(month(now)+months)`, lower bound = today.
**Where**: `internal/modules/availability/domain/booking_window.go` (+ `booking_window_test.go`)
**Depends on**: T1
**Reuses**: T1 `Diarias`, `internal/shared/booking.Period`, sentinel `ErrOutsideBookingWindow`
**Requirement**: AVL-06, AVL-07

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] With `now = 2026-07-07`, months 2: a período fully within `[2026-07-07 .. 2026-09-30]` passes; a Diária `2026-10-01` → `ErrOutsideBookingWindow` (**unit**).
- [ ] A período with any Diária **before** `now`'s date → `ErrOutsideBookingWindow` (**unit**).
- [ ] Advancing `now` to `2026-08-01` makes a `2026-10-*` período pass (window **slides**, no job) (**unit**).
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: 4 unit cases pass (inside, beyond-upper, past-date, rollover-slide), no silent deletions.

**Verify**: `go test ./internal/modules/availability/domain/ -run TestBookingWindow -v` → PASS.

**Tests**: unit
**Gate**: quick

**Commit**: `feat(availability): add sliding BookingWindow value object`

---

### T4: ReserveOccupancy use case + consumer-owned ports + DTOs

**What**: `ReserveOccupancy` use case (validate headcount ≥ 1 → expand → window check → live effective-capacity lookup → advisory lock → load → aggregate all-or-nothing → increment); consumer-owned ports `CampsiteCapacity`, `WindowConfig`, `Clock`; command/result DTOs.
**Where**: `internal/modules/availability/app/{reserve_occupancy.go,ports.go,dto.go}` (+ `reserve_occupancy_test.go`)
**Depends on**: T2, T3
**Reuses**: `domain` (aggregate, `Diarias`, `BookingWindow`, repo interface); hand-written fakes (repo, capacity, window-config) honoring the contract (LSP); fixed `Clock`
**Requirement**: AVL-03, AVL-06, AVL-13

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Happy path: all days fit + inside window → `AddOccupancy(+headcount)` called for the período's Diárias; returns `nil` (**unit**, fakes).
- [ ] One full day → `ErrNoVacancy`, `AddOccupancy` **not** called (no mutation) (**unit**).
- [ ] Any day outside window → `ErrOutsideBookingWindow`, no lock/increment (**unit**).
- [ ] Unknown campsite (capacity fake → `ErrNotFound`) → propagated; no mutation (**unit**).
- [ ] `headcount < 1` → validation error before any port call (**unit**).
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: 5 unit cases pass (happy, no-vacancy, outside-window, unknown-campsite, bad-headcount), no silent deletions.

**Verify**: `go test ./internal/modules/availability/app/ -run TestReserveOccupancy -v` → PASS.

**Tests**: unit
**Gate**: quick

**Commit**: `feat(availability): add ReserveOccupancy use case with window + capacity enforcement`

---

### T5: ReleaseOccupancy use case [P]

**What**: `ReleaseOccupancy` use case (validate headcount ≥ 1 → expand → advisory lock → `AddOccupancy(−headcount)`), reusing the T4 ports/DTOs.
**Where**: `internal/modules/availability/app/release_occupancy.go` (+ `release_occupancy_test.go`)
**Depends on**: T4
**Reuses**: `domain`, T4 ports/DTOs, fake repo
**Requirement**: AVL-05

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Release calls `AddOccupancy` with the negative delta for every Diária in the período (**unit**).
- [ ] `headcount < 1` → validation error, no mutation (**unit**).
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: 2 unit cases pass (decrement-per-day, bad-headcount), no silent deletions.

**Verify**: `go test ./internal/modules/availability/app/ -run TestReleaseOccupancy -v` → PASS.

**Tests**: unit
**Gate**: quick

**Commit**: `feat(availability): add ReleaseOccupancy use case`

---

### T6: GetAvailabilityCalendar use case + view DTO [P]

**What**: `GetAvailabilityCalendar` use case (live effective-capacity lookup → `LoadRange` → build one `DayAvailabilityView` per Diária in `[from,to)` with `Available = max(0, effective−occupied)`); `DayAvailabilityView` DTO.
**Where**: `internal/modules/availability/app/calendar.go`, `internal/modules/availability/app/dto.go` (extend) (+ `calendar_test.go`)
**Depends on**: T4
**Reuses**: `domain` (`Diarias`/`DayAvailability`), T4 `CampsiteCapacity` port, fake repo + capacity
**Requirement**: AVL-08, AVL-02, AVL-10

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Mixed range (some days occupied, some absent) → correct `Occupied`/`Available` per Diária; absent days report `Occupied 0`, `Available = effective` (**unit**).
- [ ] Over-capacity day (occupied > effective, e.g. after a capacity drop — AVL-10) → `Available = 0` (floored, never negative) (**unit**).
- [ ] Unknown campsite (capacity fake → `ErrNotFound`) → propagated (**unit**).
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: 3 unit cases pass (mixed-range, over-capacity-floor, unknown-campsite), no silent deletions.

**Verify**: `go test ./internal/modules/availability/app/ -run TestGetAvailabilityCalendar -v` → PASS.

**Tests**: unit
**Gate**: quick

**Commit**: `feat(availability): add GetAvailabilityCalendar read use case`

---

### T7: create_daily_occupancy migration

**What**: `0000NN_create_daily_occupancy.up.sql` (`daily_occupancy` table, PK `(campsite_id, diaria)`, `occupied >= 0` CHECK) and `.down.sql` (drop table).
**Where**: `db/migrations/0000NN_create_daily_occupancy.up.sql`, `db/migrations/0000NN_create_daily_occupancy.down.sql` (+ `migration_test.go`, `//go:build integration`)
**Depends on**: T2 (field/invariant alignment)
**Reuses**: golang-migrate stream + `pgtest.Setup` (DATA-04, DATA-09)
**Requirement**: AVL-01, AVL-02

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `up` creates `daily_occupancy` with the composite PK and the `occupied >= 0` CHECK (**integration**).
- [ ] Inserting `occupied = -1` is rejected by the DB CHECK (**integration**).
- [ ] `down` drops the table; migration is reversible (**integration**).
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 3 integration cases pass (table+PK+CHECK present, CHECK rejects negative, down reverts), no silent deletions.

**Verify**: `go test -tags=integration ./internal/modules/availability/adapter/repository/ -run TestDailyOccupancyMigration -v` → PASS.

**Tests**: integration
**Gate**: full

**Commit**: `feat(availability): add daily_occupancy table migration`

---

### T8: pgx OccupancyRepository + sqlc queries (advisory lock, load, upsert, range)

**What**: `db/queries/availability.sql` (`AdvisoryLockCampsite`, `LoadOccupancy`, `AddOccupancy` unnest-upsert with `GREATEST(0,…)`, `LoadRange`) + pgx `OccupancyRepository` implementing `domain.OccupancyRepository`, every method resolving its executor via `postgres.Executor(ctx)` (ambient tx or pool).
**Where**: `db/queries/availability.sql`, `internal/modules/availability/adapter/repository/occupancy_repository.go` (+ `occupancy_repository_test.go`, `//go:build integration`)
**Depends on**: T2, T7
**Reuses**: `internal/platform/postgres` (`Executor(ctx)`/`WithTx` — add the accessor if absent, see notes), generated sqlc, `pgtest.Setup`
**Requirement**: AVL-03, AVL-05, AVL-08, AVL-11

**Tools**:

- MCP: `context7` (sqlc + pgx/v5 query API; `pg_advisory_xact_lock`, `unnest` array binding)
- Skill: NONE

**Done when**:

- [ ] `AddOccupancy(+n)` on absent Diárias inserts rows; a second `AddOccupancy(+n)` upserts-increments; `AddOccupancy(−n)` decrements and floors at 0 (**integration**).
- [ ] `LoadOccupancy` returns `0` for Diárias with no row and the stored count otherwise (**integration**).
- [ ] `LoadRange(from,to)` returns occupied per Diária in `[from,to)`, ordered, over a single query (**integration**).
- [ ] `AdvisoryLock` succeeds within a `WithTx` scope; all methods run on the **ambient tx** (writing inside `WithTx` then rolling back leaves no rows) (**integration**, proves `Executor(ctx)` uses the tx — AVL-11).
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 4 integration cases pass (upsert increment/decrement/floor, load-absent-zero, load-range, ambient-tx-rollback), no silent deletions.

**Verify**: `go test -tags=integration ./internal/modules/availability/adapter/repository/ -run TestOccupancyRepository -v` → PASS.

**Tests**: integration
**Gate**: full

**Commit**: `feat(availability): add pgx OccupancyRepository with advisory lock and sqlc queries`

---

### T9: public Reserver/AvailabilityReader + app provider impl

**What**: `availability/public` (`Reserver`, `AvailabilityReader`, `Period`, `DayAvailability`, `ErrNoVacancy`, `ErrOutsideBookingWindow`, `ErrNotFound`) + `app/provider.go` implementing both ports (delegates to T4/T5/T6, maps domain sentinels → public, `campsites` not-found → `public.ErrNotFound`).
**Where**: `internal/modules/availability/public/availability.go`, `internal/modules/availability/app/provider.go` (+ `provider_test.go`, `//go:build integration`)
**Depends on**: T4, T5, T6, T8
**Reuses**: `domain`, real repo (T8), real `campsites/public.Provider` + a stub `WindowConfig`, `postgres.WithTx`, `pgtest.Setup`
**Requirement**: AVL-11, AVL-12, AVL-13, AVL-10

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Through `public.Reserver` only: `Reserve` inside a `WithTx` increments occupancy (visible after commit); a sibling write in the **same** tx + a forced rollback leaves occupancy unchanged (atomicity — AVL-11) (**integration**).
- [ ] `Release` via the port decrements; a full Diária then `Reserve` → `public.ErrNoVacancy`; outside-window período → `public.ErrOutsideBookingWindow` (**integration**).
- [ ] Effective-capacity reduced below occupancy (real campsites provider returns a lower value) → `Reserve` → `public.ErrNoVacancy`, existing occupancy intact, `AvailabilityReader` reports `Available 0` (AVL-10) (**integration**).
- [ ] `AvailabilityReader.Availability` returns flat `DayAvailability` DTOs; unknown campsite → `public.ErrNotFound`; no domain type appears in any `public` signature (compile-enforced: `public` imports stdlib only) (**integration**).
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 5 integration cases pass (reserve-commit + rollback-atomicity, release + no-vacancy + outside-window, capacity-reduction, reader-DTO + not-found, stdlib-only compile), no silent deletions.

**Verify**: `go test -tags=integration ./internal/modules/availability/app/ -run TestAvailabilityProvider -v` → PASS.

**Tests**: integration
**Gate**: full

**Commit**: `feat(availability): expose public Reserver and AvailabilityReader ports`

---

### T10: Concurrency verification — last-vacancy race + release/reserve race

**What**: Concurrency integration suite (per TESTING.md concurrency category) proving the last-vacancy guarantee against a real Postgres: a helper running `K` concurrent `Reserve` calls each in its own `WithTx`; the release/reserve interleaving; and the distinct-campsites non-contention/no-deadlock case.
**Where**: `internal/modules/availability/app/concurrency_test.go` (`//go:build integration`)
**Depends on**: T8, T9
**Reuses**: `public.Reserver` (T9), real repo (T8), `postgres.WithTx`, `pgtest.Setup`, `sync`/goroutines
**Requirement**: AVL-04, AVL-05, AVL-11

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Diária seeded to `effective−1`; `K` goroutines each `Reserve(headcount=1)` in its own tx → **exactly one** returns `nil`, `K−1` return `ErrNoVacancy`; final `occupied == effective` (0 oversell — AVL-04) (**integration**).
- [ ] Diária at full; concurrent `Release(1)` and `Reserve(1)` → serialized by the advisory lock so the `Reserve` succeeds after the `Release`; final `occupied == effective` (AVL-05) (**integration**).
- [ ] Concurrent `Reserve` on **two different** campsites proceed without serializing on each other and without deadlock (**integration**).
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 3 integration cases pass (last-vacancy exactly-one-winner, release/reserve race, distinct-campsites concurrency), no silent deletions.

**Verify**: `go test -tags=integration ./internal/modules/availability/app/ -run TestOccupancyConcurrency -v -race` → PASS.

**Tests**: integration
**Gate**: full

**Commit**: `test(availability): prove last-vacancy race resolves to exactly one winner`

---

### T11: Availability calendar htmx handler + templates

**What**: `GET /campsites/{id}/availability?from=&to=` handler rendering the per-Diária calendar via `web.Renderer` (fragment on `HX-Request`, full page otherwise); `calendar.html`, `day_cell.html` templates; unknown campsite → 404, bad range → 422.
**Where**: `internal/modules/availability/adapter/http/calendar_handler.go`, `web/templates/availability/{calendar.html,day_cell.html}` (+ `calendar_handler_test.go`, `//go:build integration`)
**Depends on**: T6, T8
**Reuses**: `app.GetAvailabilityCalendar` (T6), real repo (T8), `internal/platform/web` renderer, `pgtest.Setup`
**Requirement**: AVL-09, AVL-08, AVL-10

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Seeded range renders one cell per Diária with correct occupied/available; an over-capacity day shows `Available 0` (AVL-10) (**integration**, real DB).
- [ ] Unknown campsite → 404; `to ≤ from` / unparseable dates → 422 (**integration**).
- [ ] The read issues a single range query (no per-Diária round trip / N+1) over the PK index (**integration**, asserted via query count or seeded-range correctness — AVL-09).
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 4 integration cases pass (render-range, over-capacity-cell, not-found, bad-range), no silent deletions.

**Verify**: `go test -tags=integration ./internal/modules/availability/adapter/http/ -run TestAvailabilityCalendarHandler -v` → PASS.

**Tests**: integration
**Gate**: full

**Commit**: `feat(availability): add availability calendar htmx handler`

---

### T12: Composition-root wiring + route + expose public ports

**What**: `availability` `bootstrap.Module` impl: construct repo → use cases → `provider` → calendar handler; inject `campsites/public.Provider` (as `CampsiteCapacity`), adapt `config/public` → `WindowConfig`, inject the clock; mount the calendar route; expose `public.Reserver` + `public.AvailabilityReader` for RSV/M4/M5 wiring.
**Where**: `internal/platform/bootstrap/` (extend; e.g. `availability.go` module wiring)
**Depends on**: T9, T11
**Reuses**: `bootstrap.Module` seam (SKEL), `postgres` pool/`WithTx`, `campsites/public`, `config/public`, all availability layers
**Requirement**: AVL-12, AVL-13, AVL-09

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Bootstrap constructs the module and mounts `/campsites/{id}/availability`; the calendar renders end-to-end via `App.Handler()` against a real DB (**integration**).
- [ ] `availability/public.Reserver` is retrievable from the composition root and a `Reserve` inside a `WithTx` succeeds end-to-end with capacity from the real `campsites/public.Provider` and the window from the adapted `config/public` (AVL-13) (**integration**).
- [ ] The wired `WindowConfig`/`CampsiteCapacity` come from `config/public` / `campsites/public` only (no internals imported) (**integration**).
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 3 integration cases pass (calendar route mounted + rendered, public Reserver end-to-end, ports wired from public), no silent deletions.

**Verify**: `go test -tags=integration ./internal/platform/bootstrap/ -run TestAvailabilityWiring -v` → PASS.

**Tests**: integration
**Gate**: full

**Commit**: `feat(availability): wire availability module into the composition root`

---

## Parallel Execution Map

```
Phase 1 (unit):
  T1 ──┬── T2 [P]
       └── T3 [P]

Phase 2 (unit, depend only on T4):
  T2,T3 ──→ T4 ──┬── T5 [P]
                 └── T6 [P]

Phase 3 (integration — serial):
  T2 ──→ T7 ──→ T8 ──→ T9 ──→ T10
  (T9 also needs T4,T5,T6)

Phase 4 (integration — serial):
  T6,T8 ──→ T11

Phase 5 (integration — serial):
  T9,T11 ──→ T12
```

**Parallelism constraint:** `[P]` requires no unfinished deps, a parallel-safe test type, and no
shared mutable state. Only **T2/T3** (unit, depend solely on T1, mutually independent) and **T5/T6**
(unit, depend solely on T4, mutually independent) qualify. T7–T12 are integration ⇒ serial per
TESTING.md (shared Postgres container is the bottleneck); T10 is intentionally contended ⇒ never `[P]`.

---

## Task Granularity Check

| Task | Scope | Status |
| ---- | ----- | ------ |
| T1: Diaria VO + expander + errors | 1 VO + its pure expander + sentinels (1 concept: Diária primitives) | ✅ Granular |
| T2: DailyOccupancy + PeriodOccupancy + repo interface | 1 entity + its aggregate root + the port (cohesive occupancy model) | ✅ Granular |
| T3: BookingWindow | 1 VO | ✅ Granular |
| T4: ReserveOccupancy + ports/DTOs | 1 use case + its port/DTO scaffolding | ✅ Granular |
| T5: ReleaseOccupancy | 1 use case | ✅ Granular |
| T6: GetAvailabilityCalendar | 1 read use case + view DTO | ✅ Granular |
| T7: migration | 1 migration (up+down) | ✅ Granular |
| T8: pgx repo + queries | 1 repository impl + its SQL | ✅ Granular |
| T9: public ports + provider impl | 2 cohesive ports (one public surface) + 1 impl | ✅ Granular |
| T10: concurrency verification | 1 concurrency suite for the reserve/release seam (TESTING.md concurrency category) | ✅ Granular |
| T11: calendar handler | 1 GET endpoint + its templates | ✅ Granular |
| T12: wiring | 1 composition-root module | ✅ Granular |

Multi-item tasks (T1 VO+expander, T2 entity+aggregate+port, T4 use case+ports, T9 two ports+impl) are
cohesive around a single concept and tested together — within the "2–3 related things if cohesive"
allowance. T10 adds **no** production code: it is the dedicated concurrency verification of code already
created and unit/functionally tested in T8/T9 (TESTING.md lists concurrency as its own coverage row),
so it is additive, not deferral.

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| ---- | ---------------------- | ------------- | ------ |
| T1 | None | root (→ T2, T3) | ✅ Match |
| T2 | T1 | `T1 → T2 [P]` (→ T4, T7) | ✅ Match |
| T3 | T1 | `T1 → T3 [P]` (→ T4) | ✅ Match |
| T4 | T2, T3 | `T2,T3 → T4` (→ T5, T6, T9) | ✅ Match |
| T5 | T4 | `T4 → T5 [P]` (→ T9) | ✅ Match |
| T6 | T4 | `T4 → T6 [P]` (→ T9, T11) | ✅ Match |
| T7 | T2 | `T2 → T7` (→ T8) | ✅ Match |
| T8 | T2, T7 | `T7 → T8` (T2 upstream) (→ T9, T11) | ✅ Match |
| T9 | T4, T5, T6, T8 | `T4,T5,T6,T8 → T9` (→ T10, T12) | ✅ Match |
| T10 | T8, T9 | `T9 → T10` (T8 upstream) | ✅ Match |
| T11 | T6, T8 | `T6,T8 → T11` (→ T12) | ✅ Match |
| T12 | T9, T11 | `T9,T11 → T12` | ✅ Match |

- Every `Depends on` has a matching arrow; every arrow maps to a `Depends on`.
- `[P]` tasks depend only on their single parent (T2/T3 on T1; T5/T6 on T4), never on each other. ✅
- Integration tasks carry no `[P]` even where code-independent; T10 (contended) is never `[P]`. ✅

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| ---- | --------------------------- | --------------- | --------- | ------ |
| T1 | `domain` (VO + expander) | unit | unit | ✅ OK |
| T2 | `domain` (entity + aggregate + repo interface) | unit | unit | ✅ OK |
| T3 | `domain` (VO) | unit | unit | ✅ OK |
| T4 | `app` (use case + ports) | unit | unit | ✅ OK |
| T5 | `app` (use case) | unit | unit | ✅ OK |
| T6 | `app` (read use case) | unit | unit | ✅ OK |
| T7 | `db/migrations` | integration | integration | ✅ OK |
| T8 | `adapter/repository` (+ locking SQL) | integration | integration | ✅ OK |
| T9 | `public` impl (+ `app`) | integration | integration | ✅ OK |
| T10 | Concurrency (last-vacancy race, hold-expiry release) | integration | integration | ✅ OK |
| T11 | `adapter/http` (handler + templates) | integration | integration | ✅ OK |
| T12 | `internal/platform/bootstrap` wiring | integration | integration | ✅ OK |

- No task uses `Tests: none` (every task creates a code layer with a required test type; T10 maps to the matrix **Concurrency** row).
- Tasks creating multiple layers (T9 = `public` + `app`; T11 = handler + templates) use the **highest** required test type (integration). ✅
- Every requirement AVL-01…AVL-13 is covered by ≥1 task, and each task cites its requirement IDs. ✅
- Requirement coverage: AVL-01 (T1,T7), AVL-02 (T2,T6,T7), AVL-03 (T2,T4,T8), AVL-04 (T10), AVL-05 (T5,T8,T10), AVL-06 (T3,T4), AVL-07 (T3), AVL-08 (T6,T8,T11), AVL-09 (T11,T12), AVL-10 (T6,T9,T11), AVL-11 (T8,T9,T10), AVL-12 (T9,T12), AVL-13 (T4,T9,T12).
