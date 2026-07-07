# System Configuration Specification

## Problem Statement

Reservation rules across the system (per-reservation limits, booking window, hold TTL,
cancellation/change deadlines, participant-swap limits, overbooking) are business parameters the
Administrator must be able to change without a code deploy. Today these values are scattered
constants in the PRD; every downstream feature (availability, reservations, checkin) would
otherwise hard-code its own copy — duplicating rules and letting them drift. This feature is the
**single source of truth**: a DB-backed business configuration store with typed defaults, rich
domain behavior for the derived rules, and a minimal typed provider that downstream modules read
through.

This is the **business** configuration store (RF11). It is **distinct** from the M0 `config-runtime`
(RUN) feature — env vars, secrets, ports, health, logging. This module *reads* its pgx pool from
the M0 DATA/RUN seam but *stores* business values in Postgres, editable by the Administrator.

## Goals

- [ ] One typed `Configuration` aggregate with PRD-default values, all invariants enforced in the
      domain (never constructible in an invalid state).
- [ ] A `system_configuration` Postgres store seeded with defaults; values survive restarts and are
      editable by the Administrator.
- [ ] A minimal `config/public.Provider` (typed getters + flat DTOs) that availability,
      reservations, and checkin consume — no consumer hard-codes a rule or reads the config table.
- [ ] Config-owned derived-rule behavior (`SwapLimitFor`, deadline computation) lives once in the
      domain (DRY), not in each consumer. The booking-window horizon is stored here as raw **months**
      (`BookingWindowMonths`); its sliding-end computation is owned by `availability`, not duplicated here.
- [ ] Administrator can view and update configuration values, with validation rejecting inconsistent
      settings before they persist.

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
| ------- | ------ |
| Runtime/env config (DB URL, ports, secrets, health, logging) | M0 `config-runtime` (RUN) — technical infra, not business rules; this module only *reads* the pool it provides |
| Per-campsite overbooking % (authoritative value + effective-capacity math) | M1 `campsite-management` (CAMP, RF01/RF09) owns per-campsite %; config holds only a global default/policy |
| Polished admin management panel (navigation, dashboard integration, rich forms) | M5 `management-surfaces` (UI over this store); M1 ships the store + use cases + a minimal edit endpoint |
| Enforcing the rules (blocking a reservation, expiring a hold, computing availability) | Consumers (AVL/RSV/CHK) enforce; config only *provides* the parameters |
| Audit trail / change history of configuration edits | Post-MVP (PRD §3 "Auditoria detalhada" out of MVP) |
| Per-campsite or per-user configuration overrides | YAGNI — PRD §10 defines one park-wide configuration |

---

## User Stories

### P1: Typed business configuration with seeded defaults ⭐ MVP

**User Story**: As the system, I want a single typed `Configuration` aggregate seeded with the PRD
default values so that every reservation rule reads consistent, validated parameters from one place.

**Why P1**: Nothing downstream (availability, reservations, checkin) can enforce a rule without
these values; a rich, always-valid aggregate removes a whole class of guard code from consumers.

**Acceptance Criteria**:

1. WHEN the `Configuration` aggregate is constructed via `DefaultConfiguration()` THEN it SHALL
   carry the PRD defaults: PF limit 5, PJ limit 15, booking window 2 months ahead, cancellation
   deadline 24h, change deadline 24h, temporary-hold TTL 10 min, swap brackets (1–5→1, 6–10→2,
   11–15→3), and the documented default overbooking percent.
2. WHEN any value violates an invariant (PF ≤ 0, PJ < PF, booking months < 0, hold TTL ≤ 0,
   cancellation/change deadline < 0, overbooking outside 0–100, or swap brackets not covering
   1..PJ in a consistent ascending order) THEN construction SHALL return a typed domain error and
   SHALL NOT return a partially built aggregate.
3. WHEN the store is migrated THEN exactly one `system_configuration` row SHALL exist, seeded with
   the same values `DefaultConfiguration()` produces.
4. WHEN the repository `Load` runs against the seeded store THEN it SHALL return an aggregate equal
   to `DefaultConfiguration()` (seed/domain parity, drift-guarded by test).

**Independent Test**: Unit-test `DefaultConfiguration()` and each invariant with table cases; then
integration-migrate a Postgres testcontainer and assert `Load()` equals `DefaultConfiguration()`.

---

### P1: Downstream typed provider ⭐ MVP

**User Story**: As a downstream module (availability, reservations, checkin), I want a small typed
`config/public.Provider` exposing exactly the values I need so that I never import config internals
or reimplement a rule.

**Why P1**: The module boundary rule forbids reaching into config's domain; the provider is the
only sanctioned cross-module surface and the reason this module exists.

**Acceptance Criteria**:

1. WHEN a consumer calls `ReservationLimits(ctx)` THEN the provider SHALL return a flat
   `Limits{PF, PJ}` DTO from the current configuration.
2. WHEN a consumer calls `HoldTTL`, `CancellationDeadline`, or `ChangeDeadline` THEN the provider
   SHALL return the corresponding `time.Duration`.
3. WHEN a consumer calls `SwapLimitFor(ctx, groupSize)` THEN the provider SHALL return the swap
   limit for that group size per the configured brackets (1–5→1, 6–10→2, 11–15→3 by default).
4. WHEN a consumer calls `BookingWindowMonths(ctx)` THEN the provider SHALL return the configured
   horizon as a raw integer number of months (default 2); the sliding-end date is computed by
   `availability` (single owner), not by config.
5. WHEN a consumer calls `DefaultOverbookingPercent(ctx)` THEN the provider SHALL return the global
   default overbooking percent — a **seed default** for the new-campsite form only; the authoritative
   per-campsite value stays in `campsites/public` and availability never reads this.
6. WHEN the configuration cannot be loaded THEN every provider method SHALL return an error (fail
   closed) rather than a fabricated default.

**Independent Test**: Construct `ProviderService` over the real pgx repository against a seeded
testcontainer DB and assert every getter, `SwapLimitFor` at bracket boundaries, and
`BookingWindowMonths` returning the seeded horizon.

---

### P1: Administrator updates configuration ⭐ MVP

**User Story**: As the Administrator, I want to view the current configuration and change its values
so that I can tune reservation rules (limits, deadlines, hold TTL, window, swap rules, overbooking)
without a deploy.

**Why P1**: ROADMAP M1 target explicitly requires that "an admin can create an Acampamento and edit
configuration values."

**Acceptance Criteria**:

1. WHEN an Administrator requests the current configuration THEN `GetConfiguration` SHALL return all
   current values as a result DTO.
2. WHEN an Administrator submits new values that satisfy every invariant THEN `UpdateConfiguration`
   SHALL persist them in a single transaction and subsequent provider reads SHALL reflect them.
3. WHEN submitted values violate an invariant THEN `UpdateConfiguration` SHALL reject the whole
   update with a typed validation error and SHALL NOT persist any partial change.
4. WHEN the caller is not an Administrator THEN `UpdateConfiguration` SHALL be refused via
   `identity/public` (no mutation) and the store SHALL be unchanged.
5. WHEN the admin edit endpoint is called (GET) THEN it SHALL render the current values; (POST) with
   valid input SHALL update and return a success fragment; with invalid input SHALL return an error
   fragment naming the offending field(s).

**Independent Test**: Integration — an authenticated Administrator GETs the seeded values, POSTs a
valid change (persisted + reflected by the provider), POSTs an invalid change (rejected, store
unchanged), and a non-admin POST is refused (403).

---

## Edge Cases

- WHEN an Administrator sets PF limit above the PJ limit THEN the system SHALL reject the update
  (PF ≤ PJ invariant), unchanged store.
- WHEN hold TTL is set to 0 or negative THEN the system SHALL reject it (TTL must be > 0).
- WHEN swap brackets are submitted that do not cover 1..PJ or are not strictly ascending by upper
  bound THEN the system SHALL reject them.
- WHEN `SwapLimitFor` is called with a group size larger than the largest bracket THEN the system
  SHALL clamp to the largest bracket's swap limit (upstream reservation limits already cap size).
- WHEN `SwapLimitFor` is called with a group size ≤ 0 THEN the system SHALL return 0.
- WHEN `BookingWindowMonths` is read THEN config SHALL return only the raw stored integer; the
  month-boundary / last-day-of-month arithmetic and the auto-release-next-month behavior are
  `availability`'s responsibility (its `BookingWindow` VO, derived from `now`, no scheduled job) —
  config does not compute or test that here.
- WHEN the singleton config row is somehow absent THEN `Load` SHALL return `ErrConfigurationNotFound`
  and provider reads SHALL surface it (fail closed) — the migration seed makes this a system error,
  not a normal path.
- WHEN a configuration value is changed THEN it SHALL affect only future operations that read the
  current config; already-stored reservations SHALL NOT be retroactively invalidated (consumers read
  current config at operation time).
- WHEN two Administrators update the singleton concurrently THEN the row UPDATE SHALL serialize them;
  last committed write wins (single-writer admin surface; optimistic locking deferred, YAGNI).

---

## Requirement Traceability

**PRD / RF mapping:** Implements **RF11** (Gerenciar configurações). RF11 **parameterizes**
RF04 (Criar reservas — hold TTL, limits, overlap window), RF05 (Cancelar — cancellation deadline),
RF06 (Alterar participantes — change deadline, swap brackets), and RF08 (Disponibilidade — booking
window, overbooking policy). Sources: PRD §10 (Configurações), §6 (Janela de reservas, Limite de
participantes, Reserva temporária, Sobreposição context), §Alteração de participantes (swap
brackets), §Cancelamento (24h deadline). ROADMAP M1 → System configuration.

| Requirement ID | Story | Phase | Status |
| -------------- | ----- | ----- | ------ |
| CFG-01 | P1: Config + defaults | Design | In Tasks |
| CFG-02 | P1: Config + defaults | Design | In Tasks |
| CFG-03 | P1: Config + defaults | Design | In Tasks |
| CFG-04 | P1: Config + defaults | Design | In Tasks |
| CFG-05 | P1: Config + defaults | Design | In Tasks |
| CFG-06 | P1: Config + defaults | Design | In Tasks |
| CFG-07 | P1: Config + defaults | Design | In Tasks |
| CFG-08 | P1: Downstream provider | Design | In Tasks |
| CFG-09 | P1: Downstream provider | Design | In Tasks |
| CFG-10 | P1: Downstream provider | Design | In Tasks |
| CFG-11 | P1: Admin update | Design | In Tasks |
| CFG-12 | P1: Admin update | Design | In Tasks |
| CFG-13 | P1: Admin update | Design | In Tasks |
| CFG-14 | P1: Admin update | Design | In Tasks |

**Requirement definitions:**

- **CFG-01** — `Configuration` aggregate holds typed settings: `ReservationLimits{PF,PJ}`,
  `BookingWindow{MonthsAhead}`, cancellation deadline, change deadline, hold TTL, `SwapRules`
  (ordered brackets), global default overbooking percent.
- **CFG-02** — `DefaultConfiguration()` factory returns the PRD defaults (5/15, 2 months, 24h/24h,
  10 min, swap 1/2/3, default overbooking percent) — the single source of default values.
- **CFG-03** — All invariants enforced in VO/aggregate constructors (PF>0, PJ≥PF, months≥0,
  TTL>0, deadlines≥0, overbooking 0–100, swap brackets ascending + covering 1..PJ); invalid input →
  typed domain error, no partial aggregate.
- **CFG-04** — Domain behavior: `SwapRules.LimitFor(groupSize)`, `BookingWindow.EndFrom(now)`,
  aggregate `CancellationDeadlineAt(entry)`/`ChangeDeadlineAt(entry)` — rules live in the domain.
- **CFG-05** — `system_configuration` single-row table: typed columns + CHECK constraints mirroring
  scalar invariants + singleton guard.
- **CFG-06** — Migration seeds the singleton row with `DefaultConfiguration()` values.
- **CFG-07** — `ConfigurationRepository` domain interface (`Load`, `Save`); pgx implementation maps
  row↔aggregate (interval↔`time.Duration`, jsonb↔`SwapRules`).
- **CFG-08** — `config/public.Provider` interface + flat DTOs (`Limits{PF,PJ}`) exposing the typed
  getters consumers need.
- **CFG-09** — `app.ProviderService` implements `public.Provider` by loading the aggregate via the
  repository and delegating to domain behavior; fail-closed on load error.
- **CFG-10** — Consumers depend on narrow, consumer-shaped subsets (ISP): reservations (limits, hold
  TTL, swap, deadlines), availability (booking window, overbooking), checkin (swap limit).
- **CFG-11** — `GetConfiguration` use case returns the current configuration as a result DTO.
- **CFG-12** — `UpdateConfiguration` use case loads the aggregate, applies a validated command,
  persists via the repository in a transaction; rejects invalid input without persisting.
- **CFG-13** — `UpdateConfiguration` is Administrator-only, enforced via a consumer-owned
  `Authorizer` port satisfied by `identity/public`.
- **CFG-14** — Minimal admin edit HTTP surface (htmx): GET current values, POST validated update;
  thin handler mounted behind the identity admin route group.

**Coverage:** 14 total, 14 mapped to tasks (see tasks.md), 0 unmapped.

---

## Success Criteria

- [ ] `DefaultConfiguration()` returns every PRD default; each invariant has a failing-input unit
      test that rejects it.
- [ ] Against a migrated Postgres testcontainer, `Load()` equals `DefaultConfiguration()` and a
      `Save`→`Load` round-trips modified values (interval + jsonb mapping intact).
- [ ] `config/public.Provider` returns correct values, `SwapLimitFor` at every bracket boundary, and
      `BookingWindowMonths` returning the seeded horizon — over the real repository.
- [ ] An Administrator can update values end-to-end (persisted + reflected by the provider); invalid
      input is rejected with the store unchanged; a non-admin is refused.
- [ ] `quick` gate green for domain/app unit tests; `full` gate green for migration, repository,
      provider, and HTTP integration tests.

---

## Open Decisions

- **Overbooking scope.** PRD §6 makes overbooking % *per-campsite* (owned by CAMP/RF09). Config
  exposes only a **global default overbooking percent** (a park-wide policy default / seed for new
  campsites). Availability computes effective capacity from the *campsite's own* % via
  `campsites/public`; the config value is a policy default, not the per-campsite authority.
- **Store shape = single typed row** (not key/value). Justification in design.md: fixed, typed,
  cross-field-validated settings map cleanly to typed columns + CHECK constraints + sqlc codegen;
  key/value would lose type safety and the PF≤PJ style constraints. Enforced as a singleton.
- **Swap brackets stored as JSONB** within the single row (small ordered list, read wholesale); a
  child table is overkill (YAGNI). Bracket structural consistency is validated in the domain.
- **Booking window: config supplies only the raw horizon in months (`BookingWindowMonths`).** All
  date arithmetic — the last date of the window month (inclusive), whether the *entry* or *exit* is
  bounded, and the auto-slide as `now` advances — is owned by `availability` (its `BookingWindow`
  VO). This keeps a single owner for the window math and avoids two modules computing month
  boundaries independently.
- **Concurrent admin edits: last-writer-wins** on the singleton row (admin-only, low-contention);
  optimistic version guard deferred (YAGNI).
