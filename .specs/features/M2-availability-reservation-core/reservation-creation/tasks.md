# Reservation Creation Tasks

**Design**: `.specs/features/M2-availability-reservation-core/reservation-creation/design.md`
**Status**: Draft

All tasks follow TDD (RED → GREEN → REFACTOR); tests are co-located in the task that creates the
code (never deferred). Gate commands (TESTING.md): **quick** = `go build ./... && go vet ./... && go test ./...`;
**full** = `go test -tags=integration ./...`; **build** = `go build ./...`.

**Cross-feature notes:** the `availability/public.Reserver` seam is authored by the sibling
`availability-engine` feature; T4/T5 consume it by contract and T8/T9/T10 use a contract-conformant
test double, while T11 wires the **real** availability implementation for the authoritative
last-vacancy + window checks. `campsites/public.Provider`, `config/public` and `identity/public`
(M1) are adapted at the composition root into this module's consumer-owned ports (`CampsiteChecker`,
`PolicyProvider`, `ActorResolver`); if a shape is not final at Execute time, the consumer-owned port
keeps this feature unblocked. The migration number `0000NN` is the next free slot in the single
gap-free stream (after M1 `campsites`/`identity`/`config`) — assigned at Execute time.

---

## Execution Plan

### Phase 1: Domain (Sequential — unit)

```
T1 (reservation VOs + errors) ──→ T2 (participant/contact VOs + members) ──→ T3 (Reservation aggregate + repo interface)
```

### Phase 2: App use cases (Parallel — unit)

```
        ┌─→ T4 (CreateReservation) [P]
T3 ─────┤
        └─→ T5 (ExpireHolds)       [P]
```

### Phase 3: Persistence (Sequential — integration)

```
T3 ──→ T6 (migration + exclusion constraint) ──→ T7 (pgx repository + sqlc)
```

### Phase 4: Concurrency, sweeper & HTTP (Sequential — integration)

```
T4,T7 ──→ T8  (create: atomicity + last-vacancy + CPF-overlap concurrency)
T5,T7 ──→ T9  (sweeper integration + runner)
T4,T7 ──→ T10 (htmx handlers + templates)
```

### Phase 5: Wiring (Sequential — integration)

```
T8,T9,T10 ──→ T11 (composition-root wiring + real availability end-to-end)
```

- `[P]` only on T4, T5 (unit, depend solely on T3, mutually independent). All integration tasks
  (T6–T11) run serially per TESTING.md (shared Postgres container ⇒ not parallel-safe).

---

## Task Breakdown

### T1: Reservation value objects + sentinel errors

**What**: reuse `internal/shared/booking.Period` (seed the minimal date-range VO there if foundation/availability has not — `NewPeriod` invariant `Exit>Entry`, `Nights()`, `[entry,exit)`); define the reservation-local `Base62Code` (`^[0-9A-Za-z]+$`) and `ReservationStatus` (`Pendente`/`Expirada`, `IsPending()`) VOs with validating constructors; `domain/errors.go` declaring all domain sentinels.
**Where**: `internal/modules/reservations/domain/{code.go,status.go,errors.go}` + `internal/shared/booking/period.go` (if seeding) (+ `*_test.go`)
**Depends on**: None
**Reuses**: `internal/shared/booking` (Period; stdlib-only shared kernel)
**Requirement**: RSV-03 (Period), RSV-09 (Base62Code charset), RSV-01/RSV-13 (status Pendente/Expirada)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `booking.NewPeriod` rejects `Exit ≤ Entry`; accepts a valid range; `Nights()` counts Diárias `[entry,exit)` (**unit**, table-driven; in `internal/shared/booking`).
- [ ] `NewBase62Code` accepts `[0-9A-Za-z]+`, rejects other chars/empty (**unit**).
- [ ] `ParseReservationStatus("Pendente"|"Expirada")` succeed, any other errors; `IsPending()` correct (**unit**).
- [ ] `domain/errors.go` declares the sentinels named in design (`ErrInvalidPeriod`, `ErrInvalidCode`, `ErrEmptyName`, `ErrNoParticipants`, `ErrResponsibleNotParticipant`, `ErrParticipantLimitExceeded`, `ErrDuplicateParticipantCPF`, `ErrNoEmergencyContact`, `ErrInvalidPhone`, `ErrInvalidKinship`, `ErrNotExpirable`, `ErrOverlappingReservation`, `ErrDuplicateCode`).
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: 7 unit cases pass (3 period, 2 code, 2 status), no silent deletions.

**Verify**: `go test ./internal/modules/reservations/domain/ ./internal/shared/booking/ -run 'TestPeriod|TestBase62Code|TestReservationStatus' -v` → PASS.

**Tests**: unit
**Gate**: quick

**Commit**: `feat(reservations): add period, code and status value objects`

---

### T2: Participant/contact value objects + aggregate members

**What**: `Kinship` and `Phone` non-blank VOs; `Participant` (`CPF`+name+responsible flag) and `EmergencyContact` (name+`Phone`+`Kinship`) members with validating constructors.
**Where**: `internal/modules/reservations/domain/{kinship.go,phone.go,participant.go,emergency_contact.go}` (+ `*_test.go`)
**Depends on**: T1
**Reuses**: T1 sentinels; `internal/shared/document` (CPF)
**Requirement**: RSV-04 (participant CPF+name), RSV-07 (emergency contact fields)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `NewKinship`/`NewPhone` reject blank (and `NewPhone` a no-digit string); accept valid (**unit**).
- [ ] `NewParticipant` rejects blank name, accepts a valid CPF+name (uses `shared/document.CPF`) (**unit**).
- [ ] `NewEmergencyContact` rejects blank name, requires `Phone`+`Kinship` (**unit**).
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: 8 unit cases pass (2 kinship, 2 phone, 2 participant, 2 emergency contact), no silent deletions.

**Verify**: `go test ./internal/modules/reservations/domain/ -run 'TestKinship|TestPhone|TestParticipant|TestEmergencyContact' -v` → PASS.

**Tests**: unit
**Gate**: quick

**Commit**: `feat(reservations): add participant and emergency-contact building blocks`

---

### T3: Reservation aggregate + factory + repository interface

**What**: `Reservation` aggregate (unexported fields, getters); `NewReservation` factory enforcing all creation invariants (non-empty campsiteID; `1 ≤ len(participants) ≤ limit`; exactly one Responsável ∈ participants; no duplicate CPF; ≥1 emergency contact; status Pendente; `expiresAt = now+ttl`); `Reconstitute`; `Expire(now)` guard; `Headcount()`, `CPFs()`, `Period()`, `CampsiteID()`; `ReservationRepository` interface.
**Where**: `internal/modules/reservations/domain/{reservation.go,repository.go}` (+ `reservation_test.go`)
**Depends on**: T2
**Reuses**: T1/T2 VOs + members, `internal/shared/id` (UUID), `shared/document.CPF`
**Requirement**: RSV-01 (born Pendente + expiresAt), RSV-04 (Responsável ∈ list, ≥1), RSV-05 (limit), RSV-06 (no dup CPF), RSV-07 (≥1 contact), RSV-13/RSV-14 (Expire guard, idempotent)

**Tools**:

- MCP: NONE
- Skill: `tactical-ddd` (verify rich, non-anemic aggregate; invariants centralized in the factory)

**Done when**:

- [ ] `NewReservation` happy path → status Pendente, `expiresAt = now+ttl`, Responsável flagged (**unit**).
- [ ] Factory rejects: empty participants (`ErrNoParticipants`), Responsável ∉ list (`ErrResponsibleNotParticipant`), duplicate CPF (`ErrDuplicateParticipantCPF`), `len > limit` (`ErrParticipantLimitExceeded`), zero contacts (`ErrNoEmergencyContact`) (**unit**, table-driven).
- [ ] `Expire(now≥expiresAt)` on a Pendente → Expirada; `Expire` on non-Pendente or `now<expiresAt` → `ErrNotExpirable` (idempotent) (**unit**).
- [ ] `Headcount()==len(participants)`, `CPFs()` returns all participant CPFs incl. Responsável (**unit**).
- [ ] `ReservationRepository` interface declares `Insert`, `Update`, `LockByID`, `HasOverlappingCPF`, `FindExpiredPending`.
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: 9 unit cases pass (1 happy, 5 invariant rejections, 2 Expire, 1 headcount/CPFs), no silent deletions.

**Verify**: `go test ./internal/modules/reservations/domain/ -run TestReservation -v` → PASS.

**Tests**: unit
**Gate**: quick

**Commit**: `feat(reservations): add Reservation aggregate with creation invariants and Expire guard`

---

### T4: CreateReservation use case + consumer-owned ports [P]

**What**: `app/ports.go` (`PolicyProvider`, `ActorResolver`, `CampsiteChecker`, `CodeGenerator`, `Clock`, `TxRunner`, `ActorType`); `CreateReservation` use case + `CreateReservationCommand`/`CreateReservationResult`; app errors (`ErrCampsiteNotFound`, `ErrCampsiteInactive`). Orchestrates: resolve actor → policy (limit, TTL) → `CampsiteChecker` → build aggregate → bounded code-retry loop around `TxRunner.Run{ HasOverlappingCPF pre-check → Reserver.Reserve → repo.Insert }`.
**Where**: `internal/modules/reservations/app/{ports.go,create_reservation.go,dto.go,errors.go}` (+ `create_reservation_test.go`)
**Depends on**: T3
**Reuses**: `domain`, `availability/public.Reserver`; hand-written fakes (fake reserver, fake repo, fake policy/actor/campsite, fake `TxRunner` running `fn(ctx)` inline, fixed clock, scripted code generator) — LSP-honoring
**Requirement**: RSV-01, RSV-02, RSV-03, RSV-05, RSV-08 (pre-check), RSV-09 (retry), RSV-10, RSV-11 (single tx)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Valid command → aggregate built, `Reserve` called with `headcount=len(participants)` then `repo.Insert`, result carries ID/code/`expiresAt`/status Pendente (**unit**).
- [ ] `CampsiteChecker` → `ErrCampsiteNotFound`/`ErrCampsiteInactive` short-circuits before `Reserve` (not called) (**unit**).
- [ ] `HasOverlappingCPF==true` → `ErrOverlappingReservation`, `Reserve`/`Insert` NOT called (**unit**).
- [ ] `Reserve` returning `ErrNoVacancy`/`ErrOutsideBookingWindow` is propagated; `Insert` NOT called (**unit**).
- [ ] `repo.Insert` returning `ErrDuplicateCode` → use case regenerates code and retries; succeeds within the bound (**unit**).
- [ ] `TxRunner` returning the callback error (e.g. `Insert` failure) is propagated (rollback owned by the runner) (**unit**).
- [ ] Actor+policy select the limit (PF vs PJ) and TTL used by the factory (**unit**).
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: 7 unit cases pass (happy, campsite-invalid, overlap-precheck, no-vacancy, code-retry, insert-failure, actor-limit), no silent deletions.

**Verify**: `go test ./internal/modules/reservations/app/ -run TestCreateReservation -v` → PASS.

**Tests**: unit
**Gate**: quick

**Commit**: `feat(reservations): add CreateReservation use case and consumer-owned ports`

---

### T5: ExpireHolds sweeper use case [P]

**What**: `ExpireHolds` use case: `FindExpiredPending` → per-id `TxRunner.Run{ LockByID → Expire(now) (skip on ErrNotExpirable) → Update → Reserver.Release }`; returns count expired.
**Where**: `internal/modules/reservations/app/expire_holds.go` (+ `expire_holds_test.go`)
**Depends on**: T3
**Reuses**: `domain`, `availability/public.Reserver`, fakes (repo, reserver, `TxRunner`, fixed clock)
**Requirement**: RSV-13 (expire + release), RSV-14 (only Pendente, idempotent)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] A due Pendente hold → `Update` persists Expirada and `Release` is called with the hold's campsite/período/headcount (**unit**).
- [ ] `Expire` guard skips a non-expired or non-Pendente hold (`ErrNotExpirable`) → no `Update`/`Release`, not counted (**unit**).
- [ ] Re-running over an already-expired hold is a no-op (idempotent) (**unit**).
- [ ] `Handle` returns the number actually expired (**unit**).
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: 4 unit cases pass (expire+release, skip-not-expirable, idempotent, count), no silent deletions.

**Verify**: `go test ./internal/modules/reservations/app/ -run TestExpireHolds -v` → PASS.

**Tests**: unit
**Gate**: quick

**Commit**: `feat(reservations): add ExpireHolds sweeper use case`

---

### T6: create_reservations migration + exclusion constraint

**What**: `0000NN_create_reservations.up.sql` — `CREATE EXTENSION btree_gist`; `reservations` (UNIQUE code, CHECKs, sweeper index), `reservation_participants` (`during daterange`, `active`, `UNIQUE(reservation_id,cpf)`, `EXCLUDE USING gist (cpf =, during &&) WHERE active`, partial cpf index), `reservation_emergency_contacts`; `.down.sql` drops the three tables.
**Where**: `db/migrations/0000NN_create_reservations.up.sql`, `db/migrations/0000NN_create_reservations.down.sql`
**Depends on**: T3 (schema mirrors aggregate fields/invariants)
**Reuses**: golang-migrate stream + `pgtest.Setup` (DATA-04, DATA-09)
**Requirement**: RSV-06 (`UNIQUE(reservation_id,cpf)`), RSV-08/RSV-12 (exclusion constraint), RSV-09 (`UNIQUE` code)

**Tools**:

- MCP: `context7` (Postgres `btree_gist` / `EXCLUDE` / `daterange` syntax)
- Skill: NONE

**Done when**:

- [ ] `up` creates the three tables + `btree_gist` + all constraints/indexes; `down` drops them and is reversible (**integration**).
- [ ] Two active participant rows with the **same cpf** and overlapping `during` are rejected by `rp_no_overlap_per_cpf`; setting one row's `active=false` then allows the insert (**integration**).
- [ ] Adjacent ranges for the same cpf (`[d1,d3)` and `[d3,d5)`) are **accepted** (non-overlapping) (**integration**).
- [ ] Duplicate `code` and duplicate `(reservation_id,cpf)` are rejected (**integration**).
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 5 integration cases pass (schema present, exclusion rejects, adjacent ok, unique code, down reverts), no silent deletions.

**Verify**: `go test -tags=integration ./internal/modules/reservations/adapter/repository/ -run TestReservationsMigration -v` → PASS.

**Tests**: integration
**Gate**: full

**Commit**: `feat(reservations): add reservations schema with CPF-overlap exclusion constraint`

---

### T7: pgx ReservationRepository + sqlc queries

**What**: `db/queries/reservations.sql` (InsertReservation/Participant/EmergencyContact, HasOverlappingCPF, FindExpiredPending, LockReservationByID + child selects, UpdateReservationStatus, SetParticipantsActive) + pgx `ReservationRepository` implementing `domain.ReservationRepository`, resolving the ambient executor from `ctx`; maps `23505`(code)→`ErrDuplicateCode`, `23P01`→`ErrOverlappingReservation`; `Reconstitute` from rows.
**Where**: `db/queries/reservations.sql`, `internal/modules/reservations/adapter/repository/reservation_repository.go` (+ `reservation_repository_test.go`, `//go:build integration`)
**Depends on**: T3, T6
**Reuses**: `postgres.WithTx` + executor-from-ctx, generated sqlc, `pgtest.Setup`, `domain.Reconstitute`
**Requirement**: RSV-01 (persist aggregate+children), RSV-06, RSV-08 (HasOverlappingCPF + exclusion mapping), RSV-09 (dup-code mapping), RSV-13/RSV-14 (FindExpiredPending, LockByID, Update)

**Tools**:

- MCP: `context7` (sqlc + pgx/v5 query API)
- Skill: NONE

**Done when**:

- [ ] `Insert` persists reservation + participant rows (`during`,`active=true`) + contacts; readback matches (**integration**).
- [ ] Inserting an overlapping active same-CPF reservation surfaces `ErrOverlappingReservation` (mapped `23P01`); a colliding `code` surfaces `ErrDuplicateCode` (mapped `23505`) (**integration**).
- [ ] `HasOverlappingCPF` returns true for an overlapping active CPF, false otherwise (incl. adjacent) (**integration**).
- [ ] `FindExpiredPending(now)` returns due Pendente ids; `LockByID`+`Update` set status Expirada and participants `active=false` (**integration**).
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 6 integration cases pass (insert/readback, exclusion-map, dup-code-map, overlap-query, find-expired, lock+update), no silent deletions.

**Verify**: `go test -tags=integration ./internal/modules/reservations/adapter/repository/ -run TestReservationRepository -v` → PASS.

**Tests**: integration
**Gate**: full

**Commit**: `feat(reservations): add pgx ReservationRepository with sqlc queries`

---

### T8: CreateReservation integration — atomicity, last-vacancy & CPF-overlap concurrency

**What**: Integration tests wiring the real `CreateReservation` (T4) + real repo (T7) + real `postgres.WithTx` against Postgres, with a **contract-conformant test-double `Reserver`** (a `testutil` fixture enforcing effective capacity via a one-row occupancy table with `SELECT … FOR UPDATE`, honoring `ErrNoVacancy`) — proving reservations funnels concurrent creates through one transaction. Covers atomic rollback and the two concurrency races this module is responsible for.
**Where**: `internal/modules/reservations/adapter/repository/create_reservation_integration_test.go`, `internal/modules/reservations/testutil/reserver.go` (`//go:build integration`)
**Depends on**: T4, T7
**Reuses**: `CreateReservation` (T4), repo (T7), `postgres.WithTx`, `pgtest.Setup`
**Requirement**: RSV-10 (last-vacancy → one winner), RSV-11 (atomic rollback), RSV-12 (CPF-overlap → one winner)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] N goroutines create against a campsite with effective capacity 1 (test-double Reserver) → exactly 1 success, N−1 `ErrNoVacancy`; occupancy row == capacity (**integration**, concurrent).
- [ ] Forcing a `repo.Insert` failure after a successful `Reserve` leaves occupancy unchanged after rollback (no orphan) (**integration**).
- [ ] N goroutines create with the **same CPF** over overlapping períodos → exactly 1 success, N−1 `ErrOverlappingReservation` (exclusion constraint) (**integration**, concurrent).
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 3 integration cases pass (last-vacancy race, atomic rollback, CPF-overlap race), no silent deletions.

**Verify**: `go test -tags=integration ./internal/modules/reservations/adapter/repository/ -run TestCreateReservationConcurrency -v` → PASS.

**Tests**: integration
**Gate**: full

**Commit**: `feat(reservations): add create atomicity and concurrency integration tests`

---

### T9: ExpireHolds integration + sweeper runner

**What**: `adapter/sweeper/runner.go` (ticker loop calling `ExpireHolds.Handle`, exits on `ctx.Done()`); integration test driving one `ExpireHolds` cycle against real Postgres + a call-recording stub `Reserver` (asserts `Release`), with an injected clock.
**Where**: `internal/modules/reservations/adapter/sweeper/runner.go`, `internal/modules/reservations/adapter/sweeper/expire_holds_integration_test.go` (`//go:build integration`)
**Depends on**: T5, T7
**Reuses**: `ExpireHolds` (T5), repo (T7), `postgres.WithTx`, `pgtest.Setup`, `platform/log`
**Requirement**: RSV-13 (expire + release), RSV-14 (idempotent, concurrency-safe)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Create a Pendente hold, advance the clock past `expiresAt`, run `ExpireHolds.Handle` → status Expirada, `Release` recorded, participant rows `active=false`, and the CPF is re-bookable (a fresh overlapping create for that CPF now succeeds) (**integration**).
- [ ] Re-running `Handle` is a no-op (idempotent); a non-expired / non-Pendente hold is skipped (**integration**).
- [ ] The runner ticks `Handle` and stops on `ctx` cancel (**integration**, short interval).
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 3 integration cases pass (expire+release+re-bookable, idempotent/skip, runner lifecycle), no silent deletions.

**Verify**: `go test -tags=integration ./internal/modules/reservations/adapter/sweeper/ -run TestExpireHolds -v` → PASS.

**Tests**: integration
**Gate**: full

**Commit**: `feat(reservations): add hold-expiry sweeper runner and integration tests`

---

### T10: Booking htmx handlers + templates

**What**: `GET /reservations/new` (form; campsite options via a `campsites/public.ActiveCampsites` adapter) and `POST /reservations` (decode form → `CreateReservation.Handle` → confirmation fragment with code + `expiresAt`, or mapped error fragment); `web/templates/reservations/{new.html,form.html,confirmation.html,error.html}`. Domain/availability errors → HTTP (422 validation, 404 unknown campsite, 409 no-vacancy/overlap).
**Where**: `internal/modules/reservations/adapter/http/handler.go`, `web/templates/reservations/*.html` (+ `handler_test.go`, `//go:build integration`)
**Depends on**: T4, T7
**Reuses**: `CreateReservation` (T4), repo (T7), `web.Renderer`, stub `Reserver` + fake policy/actor/campsite, `pgtest.Setup`
**Requirement**: RSV-01 (confirmation render), RSV-02 (404/422), RSV-03/RSV-05/RSV-07 (422 validation), RSV-10 (409 no-vacancy)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `POST` a valid booking → 200/201 confirmation fragment showing the Base62 code + expiry; row persisted Pendente (verified via repo) (**integration**).
- [ ] `POST` to an unknown/Inativo campsite → 404/422, nothing persisted (**integration**).
- [ ] `POST` with `Exit ≤ Entry` / missing participant / zero emergency contacts / over-limit → 422 fragment, nothing persisted (**integration**).
- [ ] `POST` when the stub `Reserver` returns `ErrNoVacancy` → 409 fragment, nothing persisted (**integration**).
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 4 integration cases pass (create-ok, campsite-invalid, validation-422, no-vacancy-409), no silent deletions.

**Verify**: `go test -tags=integration ./internal/modules/reservations/adapter/http/ -run TestBookingHandlers -v` → PASS.

**Tests**: integration
**Gate**: full

**Commit**: `feat(reservations): add booking htmx handlers and templates`

---

### T11: Composition-root wiring + real-availability end-to-end

**What**: `reservations` `bootstrap.Module` impl: construct repo → `CreateReservation`/`ExpireHolds` → handlers, adapt `campsites/public.Provider`→`CampsiteChecker`, `config/public`→`PolicyProvider`, `identity/public`→`ActorResolver`, `shared/id`→`CodeGenerator`, `postgres.WithTx`→`TxRunner`, inject the **real** `availability/public.Reserver`; mount `/reservations*`; start the sweeper goroutine.
**Where**: `internal/platform/bootstrap/` (extend; e.g. `reservations.go`) (+ `reservations_wiring_test.go`, `//go:build integration`)
**Depends on**: T8, T9, T10
**Reuses**: `bootstrap.Module` seam (SKEL), `postgres` pool, `availability/public`, `campsites/public`, `config/public`, `identity/public`, `shared/id`, all reservations layers
**Requirement**: RSV-01 (routes live), RSV-10 (real last-vacancy), RSV-13/RSV-15 (sweeper started; config TTL + actor-aware limit wired)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Bootstrap mounts `/reservations/new` + `POST /reservations` and an end-to-end create against an Ativo campsite (real availability) returns a Pendente confirmation (**integration**, full app via `App.Handler()`).
- [ ] With the **real** availability Reserver and effective capacity 1, N concurrent creates → exactly 1 success, no oversell (occupancy == capacity) (**integration**, concurrent).
- [ ] TTL comes from `config/public` and the PF/PJ limit is selected by `identity/public` actor type (a PJ actor may exceed the PF limit up to the PJ limit) (**integration**).
- [ ] The sweeper goroutine is started and expires a due hold (releasing vacancies) without manual invocation (**integration**, short interval + clock control).
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 4 integration cases pass (end-to-end create, real last-vacancy race, config/actor policy, sweeper started), no silent deletions.

**Verify**: `go test -tags=integration ./internal/platform/bootstrap/ -run TestReservationsWiring -v` → PASS.

**Tests**: integration
**Gate**: full

**Commit**: `feat(reservations): wire reservations module into the composition root`

---

## Parallel Execution Map

```
Phase 1 (Sequential — unit):
  T1 ──→ T2 ──→ T3

Phase 2 (Parallel — unit, depend only on T3):
  T3 ──┬── T4 [P]
       └── T5 [P]

Phase 3 (Sequential — integration):
  T3 ──→ T6 ──→ T7

Phase 4 (Sequential — integration):
  T4,T7 ──→ T8
  T5,T7 ──→ T9
  T4,T7 ──→ T10

Phase 5 (Sequential — integration):
  T8,T9,T10 ──→ T11
```

**Parallelism constraint:** a `[P]` task must have no unfinished deps, a parallel-safe test type,
and no shared mutable state. Only **T4–T5** qualify (unit, depend solely on T3, mutually independent).
T6–T11 are integration ⇒ serial per TESTING.md (shared Postgres container is the bottleneck),
regardless of code independence.

---

## Task Granularity Check

| Task | Scope | Status |
| ---- | ----- | ------ |
| T1: reservation VOs + errors | 3 cohesive VOs + sentinels (1 concept: domain primitives) | ✅ Granular |
| T2: participant/contact VOs + members | 2 VOs + 2 members (1 concept: aggregate building blocks) | ✅ Granular |
| T3: Reservation aggregate + repo interface | 1 aggregate + its port (cohesive) | ✅ Granular |
| T4: CreateReservation + ports | 1 use case + its port declarations | ✅ Granular |
| T5: ExpireHolds | 1 use case | ✅ Granular |
| T6: migration | 1 migration (up+down) | ✅ Granular |
| T7: pgx repo + queries | 1 repository impl + its SQL | ✅ Granular |
| T8: create concurrency tests | 1 test suite (create atomicity + races) + 1 test double | ✅ Granular |
| T9: sweeper runner + tests | 1 runner + its integration suite | ✅ Granular |
| T10: booking handlers | 2 cohesive endpoints (one resource, create side) | ✅ Granular |
| T11: wiring | 1 composition-root module | ✅ Granular |

Multi-item tasks (T1/T2 VOs, T10 form+create endpoints) are cohesive around one concept and tested
together — within the "2–3 related things if cohesive" allowance. T4 co-locates the small `ports.go`
with its sole consumer (the use case) rather than as a separate untestable file.

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| ---- | ---------------------- | ------------- | ------ |
| T1 | None | root (→ T2) | ✅ Match |
| T2 | T1 | `T1 → T2` (→ T3) | ✅ Match |
| T3 | T2 | `T2 → T3` (→ T4,T5,T6) | ✅ Match |
| T4 | T3 | `T3 → T4 [P]` (→ T8,T10) | ✅ Match |
| T5 | T3 | `T3 → T5 [P]` (→ T9) | ✅ Match |
| T6 | T3 | `T3 → T6` (→ T7) | ✅ Match |
| T7 | T3, T6 | `T6 → T7` (T3 upstream) (→ T8,T9,T10) | ✅ Match |
| T8 | T4, T7 | `T4,T7 → T8` (→ T11) | ✅ Match |
| T9 | T5, T7 | `T5,T7 → T9` (→ T11) | ✅ Match |
| T10 | T4, T7 | `T4,T7 → T10` (→ T11) | ✅ Match |
| T11 | T8, T9, T10 | `T8,T9,T10 → T11` | ✅ Match |

- Every `Depends on` has a matching arrow; every arrow maps to a `Depends on`.
- `[P]` tasks (T4, T5) depend only on T3, never on each other. ✅
- Integration tasks carry no `[P]`. ✅

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| ---- | --------------------------- | --------------- | --------- | ------ |
| T1 | `domain` (VOs) | unit | unit | ✅ OK |
| T2 | `domain` (VOs + members) | unit | unit | ✅ OK |
| T3 | `domain` (aggregate + repo interface) | unit | unit | ✅ OK |
| T4 | `app` (use case + ports) | unit | unit | ✅ OK |
| T5 | `app` (use case) | unit | unit | ✅ OK |
| T6 | `db/migrations` (schema + exclusion constraint) | integration | integration | ✅ OK |
| T7 | `adapter/repository` | integration | integration | ✅ OK |
| T8 | concurrency (last-vacancy/overlap) + `adapter/repository` | integration | integration | ✅ OK |
| T9 | `adapter/sweeper` + concurrency (hold expiry) | integration | integration | ✅ OK |
| T10 | `adapter/http` (handlers + templates) | integration | integration | ✅ OK |
| T11 | `internal/platform/bootstrap` wiring | integration | integration | ✅ OK |

- No task uses `Tests: none` (every task creates a code layer with a required test type).
- Concurrency tasks (T8 last-vacancy/CPF-overlap; T9 hold-expiry) use **integration** with concurrent
  goroutines against real Postgres, per the TESTING.md matrix. ✅
- Every requirement RSV-01…RSV-15 is covered by ≥1 task, and each task cites its requirement IDs. ✅
