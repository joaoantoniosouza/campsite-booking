# Availability Engine Specification

**Milestone:** M2 — Availability & Reservation Core
**Module:** `availability` (`internal/modules/availability/{domain,app,adapter,public}`)
**Requirement prefix:** `AVL`
**Implements:** RF08 (Calcular disponibilidade por diária e por acampamento), RF09 (Controlar overbooking). PRD §6 (Ocupação, Janela de reservas, Acampamentos — capacidade efetiva), §12 (NFR <200 ms na consulta de disponibilidade), §13 (last-vacancy race, hold expiry, cancel release, capacity/overbooking change).

## Problem Statement

Every booking decision hinges on one question: *does this Acampamento still have room for N people on each Diária of the requested período?* Occupancy is counted **per person, per Diária (09:00→09:00, checkout day excluded), per Acampamento**, and must never exceed the site's **effective capacity** (`capacidade + overbooking %`). Under concurrent load the last vacancy must resolve to **exactly one winner** — no invalid overbooking. This feature owns the occupancy ledger, the per-Diária capacity/overbooking check, the booking-window rule, the availability calendar (htmx), and the **transactional occupancy port** (`Reserver`) that reservation-creation drives on its own ambient transaction so the occupancy increment and the reservation insert commit atomically.

## Goals

- [ ] Occupancy is tracked **per pessoa, per Diária, per Acampamento**; a Período expands to its Diárias `[Entry, Exit)` with the **checkout day never counted** — as a domain calculation, not an anemic helper.
- [ ] **No invalid overbooking:** a Diária admits a headcount only while `occupied + headcount ≤ effective capacity`, evaluated per Diária against the site's *live* effective capacity.
- [ ] **Exactly one winner** for the last vacancy under concurrent `Reserve` calls (0 oversell incidents), guaranteed by a per-Acampamento lock inside the caller's transaction.
- [ ] `Reserve`/`Release` execute on the **ambient transaction** carried in `ctx`, so occupancy mutations commit atomically with the caller's reservation write; the tx handle never appears in the public signature.
- [ ] Booking window enforced: reservations only within `[today .. end of (current month + window months)]`, the window **sliding with the clock** (next month auto-released at rollover, no scheduled job).
- [ ] An availability **calendar** (htmx) renders per-Diária occupancy for an Acampamento over a date range in **< 200 ms** (single indexed range scan, lock-free read path).
- [ ] A **stdlib-only** `availability/public` exposes the `Reserver` write port + a read port with flat DTOs; availability consumes `campsites/public` and `config/public` **by interface only**.

## Out of Scope

Explicitly excluded — prevents scope creep.

| Feature | Reason |
| ------- | ------ |
| Reservation aggregate, participants, emergency contacts, holds/TTL timer, Base62 code | Owned by `reservation-creation` (M2, RSV). This feature only supplies the occupancy `Reserve`/`Release` port it calls. |
| Same-CPF period-overlap prevention (exclusion constraint + range type) | A *reservation* invariant (PRD §Sobreposição), owned by `reservation-creation`. Availability counts headcount per Diária; it does not know participant identities. |
| Hold-expiry timer / who calls `Release` and when | Availability exposes `Release`; the expiry/cancel trigger lives in `reservation-creation` (RSV) and `remote-cancellation` (M3, CNL). |
| Admin dashboard, park-total aggregation across Acampamentos | Owned by `admin` (M5, DSH) — reads occupancy via availability's public read port; aggregation is not this feature's job. |
| Check-in / walk-in / no-show | M4 (CHK/WLK/OSC); they call `Reserve`/`Release` like any other caller. |
| Storing the booking-window months / overbooking defaults | Owned by `system-configuration` (CFG, RF11) and per-campsite `campsites` (CAMP). Availability *reads* effective capacity and the window value; it stores neither. |
| Active/Inativo gating of the target Acampamento | Reservation flow's responsibility via `campsites/public.ActiveCampsites`; availability enforces capacity + window only (needs just effective capacity). |
| Multi-timezone Diária boundaries | Single-park MVP uses one calendar date per Diária (Open Decisions). |

---

## User Stories

### P1: Per-Diária, per-Acampamento occupancy model ⭐ MVP

**User Story**: As the system, I want occupancy counted per pessoa, per Diária, per Acampamento — with a Período expanded to its Diárias (09:00→09:00, checkout excluded) and a per-Diária effective-capacity ceiling — so that overbooking is never exceeded.

**Why P1**: RF08/RF09 + PRD §6 (Ocupação). Every reserve/availability decision is built on this model; nothing downstream is correct without it.

**Acceptance Criteria**:

1. WHEN a Período `{Entry, Exit}` is expanded THEN the system SHALL yield one Diária per date in `[Entry, Exit)` — Entry inclusive, **Exit (checkout) excluded** — e.g. `2026-07-10 → 2026-07-13` yields Diárias `{07-10, 07-11, 07-12}` (3 Diárias).
2. WHEN a Período has `Exit == Entry + 1 day` THEN it SHALL yield exactly **one** Diária.
3. WHEN a Período has `Exit ≤ Entry` THEN the system SHALL reject it as an invalid Período (no Diárias).
4. WHEN a Diária has `occupied` people and the site's effective capacity is `E` THEN the Diária SHALL admit a headcount `h` **iff** `occupied + h ≤ E`; otherwise the Diária has no vacancy.
5. WHEN effective capacity is requested THEN it SHALL be read **live** from `campsites/public` (`capacidade + overbooking %`), never stored in the occupancy ledger.

**Independent Test**: Table-driven domain unit tests: period-expansion cases (3-night, 1-night, invalid); `DailyOccupancy.CanAccommodate` at, below, and above the ceiling.

---

### P1: Transactional Reserve — last-vacancy guarantee ⭐ MVP

**User Story**: As `reservation-creation`, I want a `Reserve(ctx, campsiteID, período, headcount)` port that atomically validates the booking window, checks every Diária's ceiling, and increments occupancy **on my ambient transaction**, so concurrent bookings for the last vacancy produce exactly one winner and my reservation insert commits atomically with the occupancy change.

**Why P1**: The core seam of M2 (ARCHITECTURE §7). Without atomic, race-safe occupancy the whole "no invalid overbooking" goal fails.

**Acceptance Criteria**:

1. WHEN `Reserve` is called with a período whose every Diária has room THEN the system SHALL increment `occupied` by `headcount` for **each** Diária and return `nil`.
2. WHEN **any** Diária in the período lacks room THEN `Reserve` SHALL return `ErrNoVacancy` and SHALL leave occupancy **unchanged** (all-or-nothing — no partial increment).
3. WHEN two `Reserve` calls contend for the last vacancy of a Diária THEN **exactly one** SHALL succeed and the other SHALL return `ErrNoVacancy`; final `occupied` SHALL never exceed effective capacity.
4. WHEN `Reserve` runs THEN it SHALL execute against the transaction carried in `ctx` (opened by the caller via `postgres.WithTx`), so that if the caller's surrounding transaction rolls back, the occupancy increment is rolled back too (and vice-versa) — atomic all-or-nothing.
5. WHEN `Reserve` mutates THEN it SHALL hold a per-Acampamento lock so the read-check-increment sequence is serialized against concurrent reservers of the same Acampamento; different Acampamentos SHALL proceed concurrently.
6. WHEN `headcount < 1` THEN `Reserve` SHALL reject the request without mutating.

**Independent Test**: Integration — seed a Diária to `effective-1`; launch `K` goroutines each `Reserve(headcount=1)` inside its own `WithTx`; assert exactly 1 `nil` and `K-1` `ErrNoVacancy`, final `occupied == effective`. Atomicity: within one `WithTx`, call `Reserve` then force the tx to roll back; assert occupancy unchanged.

---

### P1: Release occupancy ⭐ MVP

**User Story**: As `reservation-creation` (hold expiry) and cancellation flows, I want a `Release(ctx, campsiteID, período, headcount)` port that decrements occupancy on my ambient transaction, so freed vacancies become immediately bookable.

**Why P1**: PRD §13 (hold expiry / cancel release vacancies immediately). Symmetric counterpart to `Reserve`; the freed capacity must be consistent with concurrent reservers.

**Acceptance Criteria**:

1. WHEN `Release` is called for a período THEN the system SHALL decrement `occupied` by `headcount` for each Diária in `[Entry, Exit)`, on the ambient transaction.
2. WHEN a decrement would drive `occupied` below 0 THEN the result SHALL be floored at 0 (never negative).
3. WHEN a `Release` and a `Reserve` for the same Acampamento run concurrently THEN they SHALL be serialized (same per-Acampamento lock) so counts stay consistent and a vacancy freed by `Release` becomes available to a waiting `Reserve`.

**Independent Test**: Integration — reserve a Diária to full; concurrently `Release(1)` and `Reserve(1)`; assert the reserve succeeds after the release and final `occupied == effective`.

---

### P1: Booking-window enforcement ⭐ MVP

**User Story**: As the system, I want reservations limited to the current month plus a configured number of months ahead (default 2), with the window sliding automatically as the calendar advances, so bookings stay within the allowed horizon without a scheduled release job.

**Why P1**: PRD §6 (Janela de reservas) — a hard gate on every `Reserve`.

**Acceptance Criteria**:

1. WHEN every Diária of a período falls within `[today .. last day of (current month + window months)]` THEN `Reserve` SHALL pass the window check.
2. WHEN any Diária falls **after** the last day of `(current month + window months)` THEN `Reserve` SHALL return `ErrOutsideBookingWindow` and mutate nothing (e.g. today `2026-07-07`, window 2 → bookable through `2026-09-30`; a Diária `2026-10-01` is rejected).
3. WHEN any Diária falls **before** today THEN `Reserve` SHALL return `ErrOutsideBookingWindow` (no past-date occupancy).
4. WHEN the calendar advances to a new month THEN the newly-eligible month SHALL become bookable automatically because the window is computed live from the injected clock — no cron/job (e.g. on `2026-08-01`, `2026-10-*` becomes bookable).
5. WHEN the window-months value is read THEN it SHALL come from `config/public` (configurable per PRD §10); availability stores no default of its own.

**Independent Test**: Domain unit — `BookingWindow` with a fixed injected `now`: Diária inside window → allowed; one day past the upper bound → rejected; a past date → rejected; advance `now` by one month → previously-rejected month now allowed.

---

### P1: Availability calendar (query + htmx view) ⭐ MVP

**User Story**: As a visitor (and admin), I want a per-Diária availability calendar for an Acampamento over a date range so that I can see, for each Diária, how many vagas remain — fast.

**Why P1**: RF08 + NFR §12 (<200 ms). The user-facing read side of availability and the entry point to booking.

**Acceptance Criteria**:

1. WHEN the calendar is requested for `(campsiteID, from, to)` THEN the system SHALL return, per Diária in the range, `{Date, Occupied, EffectiveCapacity, Available}` where `Available = max(0, EffectiveCapacity − Occupied)`.
2. WHEN a Diária has no occupancy row THEN it SHALL report `Occupied = 0` and `Available = EffectiveCapacity` (sparse storage; gaps filled as empty).
3. WHEN the calendar handler is hit via htmx THEN it SHALL render an HTML fragment (partial swap) and a full page on direct navigation.
4. WHEN `campsiteID` is unknown THEN the handler SHALL respond not-found (404) without a stack trace.
5. WHEN the range is queried THEN it SHALL use a **single indexed range scan** on `daily_occupancy (campsite_id, diaria)` plus one effective-capacity lookup — **no** per-Diária round trip and **no** lock on the read path (meets the <200 ms NFR).

**Independent Test**: Integration — seed occupancy across a range (some days full, some empty); GET the calendar → each Diária cell shows correct occupied/available; unknown campsite → 404; assert the read issues one range query (no N+1).

---

### P1: Public boundary + cross-module consumption ⭐ MVP

**User Story**: As `reservation-creation`, `checkin`, and `admin`, I want a stable, stdlib-only `availability/public` (the `Reserver` write port + a read port + flat DTOs) so that I can drive occupancy and read availability without importing availability internals; and I want availability to consume `campsites`/`config` only through their public interfaces.

**Why P1**: Module-boundary rule (ARCHITECTURE §2/§3, non-negotiable). This is the seam reservation-creation is built against in parallel.

**Acceptance Criteria**:

1. WHEN another module imports `availability/public` THEN it SHALL see only the `Reserver` interface, the read port, flat DTOs (`Period`, `DayAvailability`), and sentinel errors (`ErrNoVacancy`, `ErrOutsideBookingWindow`, `ErrNotFound`) — **no** pgx/infra type and **no** availability domain/value-object type crosses the boundary (`public` imports **stdlib only**).
2. WHEN availability needs effective capacity THEN it SHALL obtain it through a small consumer-shaped port satisfied by `campsites/public.Provider` (`EffectiveCapacity(ctx, id)`); it SHALL NOT import `campsites/domain` or `campsites/app`.
3. WHEN availability needs the booking-window months THEN it SHALL obtain it through a consumer-owned port satisfied by `config/public`; it SHALL NOT import config internals.
4. WHEN `Reserve`/`Release`/read is called for an unknown Acampamento THEN the system SHALL surface `availability/public.ErrNotFound` (mapped from `campsites/public.ErrNotFound`), so consumers never import campsites just for the error.

**Independent Test**: Integration — exercise `Reserver` and the read port through `availability/public` only (no domain import in the test); a compile-time check that `public` imports stdlib only; unknown campsite → `public.ErrNotFound`.

---

### P2: Robustness to capacity / overbooking change

**User Story**: As an Administrator changing an Acampamento's capacidade or overbooking %, I want the change to apply to future availability immediately without voiding any existing occupancy, so the system degrades gracefully (PRD §13).

**Why P2**: PRD §13 edge cases ("Mudança de capacidade", "Mudança do percentual de overbooking"). Layers on the P1 occupancy model as an emergent guarantee of reading effective capacity live; separated so it is independently testable.

**Acceptance Criteria**:

1. WHEN effective capacity **increases** THEN the next `Reserve`/calendar SHALL immediately reflect the larger ceiling (effective capacity read live, no data migration).
2. WHEN effective capacity is **reduced below** a Diária's current `occupied` THEN existing occupancy SHALL remain unchanged (no confirmed occupancy voided), new `Reserve` for that Diária SHALL return `ErrNoVacancy`, and the calendar SHALL report `Available = 0` (floored, never negative).
3. WHEN overbooking % changes THEN the next check SHALL use the newly-computed effective capacity with no other effect.

**Independent Test**: Integration — seed a Diária at `occupied = 100`; drop effective capacity from 120 → 80 (campsites provider returns 80); assert calendar `Available = 0`, `occupied` still 100, and a new `Reserve` returns `ErrNoVacancy`; raise to 130 → a `Reserve(1)` now succeeds.

---

## Edge Cases

- WHEN two reservations contend for the **last vaga** THEN exactly one wins; the other gets `ErrNoVacancy` — enforced by the per-Acampamento advisory lock inside the caller's tx (PRD §13). 
- WHEN a **hold expires** or a reservation is **cancelled** THEN `Release` frees the vagas on the caller's tx; a concurrent `Reserve` serializes behind the same lock and may then succeed (PRD §13: "Reserva walk-in feita quando a vaga se libera por outro cancelamento simultâneo").
- WHEN a período **straddles the window boundary** (one Diária inside, one past the horizon) THEN the whole `Reserve` is rejected with `ErrOutsideBookingWindow` — no partial mutation.
- WHEN `capacidade` is reduced below current occupancy THEN new bookings are blocked while existing occupancy is preserved and `Available` is floored at 0 (PRD §13, AVL-10).
- WHEN a período's `Exit ≤ Entry` THEN it is an invalid Período (rejected by the shared `Period` constructor before any occupancy work).
- WHEN the checkout day is queried in the calendar THEN it is **not** counted as an occupied Diária of the departing reservation (09:00→09:00 rule).
- WHEN a `Reserve`/`Release`/calendar targets an **unknown** `campsiteID` THEN the system returns `ErrNotFound` (not a 500).
- WHEN `headcount < 1` THEN `Reserve`/`Release` reject the request without mutating.
- WHEN a Diária range in the calendar has **no** occupancy rows THEN every Diária reports full availability (sparse ledger; no rows pre-created).
- WHEN `Release` is asked to free more than is occupied (double-release / bug) THEN `occupied` floors at 0 and the DB `occupied >= 0` CHECK is the backstop.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| -------------- | ----- | ----- | ------ |
| AVL-01 | P1: Occupancy model (Período→Diárias, 09:00→09:00, checkout excluded; per pessoa/Diária/Acampamento) | Design | In Tasks |
| AVL-02 | P1: Occupancy model (per-Diária effective-capacity ceiling; `occupied + h ≤ E`; capacity read live) | Design | In Tasks |
| AVL-03 | P1: Reserve (window + all-or-nothing capacity check + increment; `ErrNoVacancy`/`ErrOutsideBookingWindow`; no partial mutation; headcount ≥ 1) | Design | In Tasks |
| AVL-04 | P1: Reserve (last-vacancy race → exactly one winner; per-Acampamento lock; distinct sites concurrent) | Design | In Tasks |
| AVL-05 | P1: Release (per-Diária decrement, floor 0, serialized with Reserve) | Design | In Tasks |
| AVL-06 | P1: Booking window (`[today .. end of current month + window months]`; outside → `ErrOutsideBookingWindow`; window from config) | Design | In Tasks |
| AVL-07 | P1: Booking window slides with the clock (next month auto-released at rollover; no job) | Design | In Tasks |
| AVL-08 | P1: Availability calendar computation (`{Date,Occupied,EffectiveCapacity,Available}`, `Available = max(0, E−occ)`, sparse gaps) | Design | In Tasks |
| AVL-09 | P1: Calendar htmx view (fragment/full page; unknown → 404; single indexed range scan, lock-free, <200 ms) | Design | In Tasks |
| AVL-10 | P2: Capacity/overbooking change forward-only (live read; existing occupancy untouched; over-capacity blocks new, floors Available at 0) | Design | In Tasks |
| AVL-11 | P1: Ambient-tx atomicity (Reserve/Release on tx from `ctx`; commits atomically with caller's write; tx handle never in public signature) | Design | In Tasks |
| AVL-12 | P1: Public boundary (`Reserver` + read port + flat DTOs + sentinels; stdlib-only `public`; no domain/infra type crosses) | Design | In Tasks |
| AVL-13 | P1: Cross-module consumption (consume `campsites/public` + `config/public` by interface only; unknown campsite → `ErrNotFound`) | Design | In Tasks |

**ID format:** `AVL-NN`. **Status values:** Pending → In Design → In Tasks → Implementing → Verified.
**Coverage:** 13 total, 13 mapped to tasks (see tasks.md), 0 unmapped.

### PRD / RF Traceability

- **RF08** (Calcular disponibilidade por diária e por acampamento) — the occupancy model and calendar (AVL-01, 02, 08, 09).
- **RF09** (Controlar overbooking) — the per-Diária effective-capacity ceiling and its enforcement in `Reserve` (AVL-02, 03, 10). The per-campsite overbooking % itself is stored by `campsites` (CAMP); availability *enforces* it.
- **PRD §6 (Ocupação)** — per pessoa/Diária/Acampamento, Diária 09:00→09:00, checkout excluded (AVL-01, 02).
- **PRD §6 (Janela de reservas)** — 2-months-ahead sliding window, auto-release at rollover (AVL-06, 07).
- **PRD §6 (Acampamentos)** — `capacidade efetiva = capacidade + overbooking %`, consumed live from `campsites/public` (AVL-02, 13).
- **PRD §12 (NFR)** — <200 ms availability query (AVL-09).
- **PRD §13** — last-vacancy race (AVL-04), hold expiry / cancel release (AVL-05), capacity change / overbooking change (AVL-10).
- **ARCHITECTURE §7** — atomic last-vacancy on the ambient tx (AVL-03, 04, 11); **§2/§3** — module boundary + stdlib-only public (AVL-12, 13).

---

## Success Criteria

- [ ] `go test ./internal/modules/availability/...` passes (domain + app unit): period-expansion, capacity-ceiling, booking-window, and use-case cases green.
- [ ] `go test -tags=integration ./internal/modules/availability/...` passes: migration, `OccupancyRepository`, `Reserver`/read public impl, calendar htmx handler, and the **concurrency** last-vacancy + release-race tests against a real Postgres 16 testcontainer.
- [ ] Under `K` concurrent `Reserve` calls for the last vaga, **exactly one** succeeds and `occupied` never exceeds effective capacity (0 oversell) — proven by the concurrency test.
- [ ] `Reserve`/`Release` invoked inside a caller `postgres.WithTx` commit/rollback atomically with a sibling write in the same tx (atomicity test).
- [ ] The availability calendar renders a date range through a single indexed range scan (no N+1); the read path takes no lock.
- [ ] `availability/public` compiles importing **stdlib only**; a public-only integration test drives `Reserver` + read port with no availability/campsites domain import.

---

## Open Decisions

- **`Period` is a shared-kernel VO.** Per CONVENTIONS §DRY ("CPF/CNPJ, Base62, **Period** live once in the shared kernel") and ARCHITECTURE §4, the `{Entry, Exit}` date-range primitive (invariant `Exit > Entry`, `Nights()`) lives at `internal/shared/booking.Period` and is reused by availability, reservations, and checkin. Availability adds the **Diária-expansion** rule (checkout-excluded) in its own domain — that is a business rule, which the shared kernel must not hold. If M0/foundation has not shipped the shared `Period` yet, it is a trivial primitive addition coordinated across the M2 features; availability reuses it, never re-implements it.
- **Public read port is exposed alongside `Reserver`.** The shared-seam reference ships `DayAvailability` in `availability/public`, signalling availability data is meant to cross the boundary. We expose a minimal read port (`Availability(ctx, campsiteID, from, to) []DayAvailability`) reusing the same app read use case that backs the in-module htmx calendar (DRY). Natural consumers: reservation-creation's date picker and admin's occupancy views. It carries no logic of its own and is trivially removable if strict YAGNI is preferred; included to honor the provided contract and avoid a later public-API change.
- **Concurrency primitive = per-Acampamento advisory transaction lock** (`pg_advisory_xact_lock(hashtextextended(campsite_id, 0))`) rather than row `SELECT … FOR UPDATE`. Both are sanctioned by ARCHITECTURE §7; the advisory lock is chosen because occupancy rows are **sparse** (a Diária may have no row yet) and `FOR UPDATE` cannot lock a not-yet-existing row (phantom inserts). The lock serializes reservers of the *same* Acampamento only; different Acampamentos proceed concurrently. Contention is naturally low (one site's timeline). A hash collision merely over-serializes two sites occasionally — still correct.
- **Booking-window lower bound = today (no past-date occupancy).** PRD states only the upper bound ("dois meses à frente"). We add "not before today" as the lower bound and fold past-date rejection into `ErrOutsideBookingWindow`. Same-day booking (Entry == today) is allowed.
- **No cross-module DB foreign key** from `daily_occupancy.campsite_id` to `campsites.id`. Keeps the two module schemas independent (module boundary in spirit); campsite existence is enforced in-app via `campsites/public`. The `occupied >= 0` CHECK stays as an in-table backstop.
- **Effective capacity is not gated on Ativo/Inativo here.** Availability only needs the number; active-status gating of the target Acampamento is the reservation flow's job (`campsites/public.ActiveCampsites`). Keeps availability's consumed port to a single method (ISP).
- **Diária identity = one calendar date (park-local), stored as `DATE`.** The "09:00" boundary is ubiquitous-language documentation; the occupancy key is the date. Multi-timezone Diária math is out of scope for the single-park MVP; revisit if parks span timezones.
- **`Release` exactly-once is a caller contract, not an availability-layer guarantee.** Unlike `Reserve` (whose capacity ceiling + advisory lock make over-increment impossible), `Release` only floors at 0 — a *double*-`Release` of the same reservation would wrongly decrement occupancy that belongs to *other* reservations, silently under-counting and risking a later oversell. Availability cannot detect this because it counts headcount, not reservation identity. Exactly-once is therefore owned by the callers' state machines: `reservation-creation`'s sweeper (`FOR UPDATE` + idempotent `Expire` guard) and M3/M4 cancellation transitions each fire `Release` at most once per reservation. Documented here so the asymmetry with `Reserve` is a conscious boundary, not an oversight; if a future caller cannot guarantee it, add an idempotency key (e.g. per-reservation release ledger) rather than relying on the floor.
