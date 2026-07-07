# Availability Engine Design

**Spec**: `.specs/features/M2-availability-reservation-core/availability-engine/spec.md`
**Status**: Draft

---

## Architecture Overview

The `availability` module is a full bounded context across all four Clean Architecture layers. A rich occupancy model lives in `domain`: a `Diaria` VO, a `DailyOccupancy` entity that owns the per-Diária capacity check, a transient `PeriodOccupancy` aggregate that applies **all-or-nothing** reserve/release across a período's Diárias, and a `BookingWindow` VO that owns the sliding-window rule. Single-purpose use cases orchestrate them on the **ambient transaction** resolved from `ctx`; a pgx repository persists the occupancy ledger and takes a per-Acampamento **advisory lock** for the last-vacancy guarantee; a thin htmx handler renders the calendar; and a stdlib-only `public` package exposes the `Reserver` write port + an `AvailabilityReader` read port with flat DTOs. Effective capacity and the window-months value are consumed **live** through small consumer-shaped ports satisfied by `campsites/public` and `config/public`. Dependencies point inward; the only importable surface for other modules is `availability/public`.

```mermaid
graph TD
    subgraph http["adapter/http (chi + htmx)"]
        CH["calendar_handler.go<br/>GET /campsites/{id}/availability"]
    end
    subgraph app["app (use cases + public impl + ports)"]
        UC1["ReserveOccupancy"]
        UC2["ReleaseOccupancy"]
        UC3["GetAvailabilityCalendar"]
        PROV["provider.go<br/>implements public.Reserver + AvailabilityReader"]
        PORTS["ports.go<br/>CampsiteCapacity, WindowConfig, Clock (consumer-owned)"]
    end
    subgraph domain["domain (pure)"]
        DIA["Diaria VO + Diarias(period) expander"]
        DOCC["DailyOccupancy entity<br/>CanAccommodate / Reserve / Release"]
        PAGG["PeriodOccupancy aggregate<br/>all-or-nothing Reserve/Release"]
        BW["BookingWindow VO<br/>Contains / Validate(now)"]
        REPOI["OccupancyRepository (interface)"]
        ERR["sentinel errors"]
    end
    subgraph adapter_repo["adapter/repository"]
        REPO["occupancy_repository.go<br/>pgx + sqlc; advisory lock; executor-from-ctx"]
    end
    PUB["public (stdlib only)<br/>Reserver, AvailabilityReader,<br/>Period, DayAvailability,<br/>ErrNoVacancy, ErrOutsideBookingWindow, ErrNotFound"]
    SK["internal/shared/booking.Period"]

    CH --> UC3
    UC1 & UC2 --> PAGG
    UC3 --> DOCC
    PAGG --> DOCC --> DIA
    UC1 --> BW
    DIA --> SK
    UC1 & UC2 & UC3 --> REPOI
    UC1 & UC3 --> PORTS
    UC1 --> ERR
    PROV --> UC1 & UC2 & UC3
    PROV -.satisfies.-> PUB
    REPO -.implements.-> REPOI
    REPO --> PGX["platform/postgres<br/>WithTx + Executor(ctx)"]
    PGX --> PG[("Postgres: daily_occupancy")]

    PORTS -. wired at root .-> CAMP["campsites/public.Provider<br/>(consumed)"]
    PORTS -. wired at root .-> CFG["config/public<br/>(consumed)"]
    RSV["reservation-creation (RSV) / checkin (M4) / admin (M5)"] -->|import only| PUB

    BOOT["platform/bootstrap<br/>composition root"] -.wires repo→uc→provider→handler,<br/>injects campsites+config ports,<br/>exposes public.Reserver.-> PROV
```

---

## Modules & Clean Architecture Layers Touched

| Layer | This feature adds | Path root |
| ----- | ----------------- | --------- |
| `domain` | `Diaria` VO + `Diarias(period)` expander, `DailyOccupancy` entity, `PeriodOccupancy` aggregate, `BookingWindow` VO, `OccupancyRepository` interface, sentinel errors | `internal/modules/availability/domain/` |
| `app` | Use cases (`ReserveOccupancy`, `ReleaseOccupancy`, `GetAvailabilityCalendar`), consumer-owned ports (`CampsiteCapacity`, `WindowConfig`, `Clock`), command/view DTOs, `provider.go` impl of the public ports | `internal/modules/availability/app/` |
| `adapter/repository` | pgx/sqlc `OccupancyRepository` (advisory lock, load/adjust/range), executor-from-ctx | `internal/modules/availability/adapter/repository/` |
| `adapter/http` | Availability calendar htmx handler | `internal/modules/availability/adapter/http/` |
| `public` | `Reserver`, `AvailabilityReader`, `Period`, `DayAvailability` DTOs, `ErrNoVacancy`, `ErrOutsideBookingWindow`, `ErrNotFound` | `internal/modules/availability/public/` |
| `db` | `daily_occupancy` table migration + sqlc queries (incl. hand-written advisory-lock / upsert SQL) | `db/migrations/`, `db/queries/availability.sql` |
| shared kernel | reuse (and, if absent, seed) `booking.Period` date-range VO | `internal/shared/booking/` |
| templates | availability calendar htmx partials | `web/templates/availability/` |
| composition root | construct + wire (repo → use cases → provider → handler), inject `campsites/public` + `config/public` + clock, mount route, expose `public.Reserver` for RSV | `internal/platform/bootstrap/` |

### Module Boundary Rule (design-level statement)

Per ARCHITECTURE §2/§3 (**non-negotiable**):

- **Exposes** `availability/public` (`Reserver`, `AvailabilityReader`, flat DTOs, sentinels) — the *only* surface `reservation-creation` (RSV), `checkin` (M4), and `admin` (M5) may import. `availability/public` imports **stdlib only** (ARCHITECTURE §3 dependency table: `public` may import stdlib + own DTOs). No pgx/infra type and **no** availability domain entity or VO ever crosses; `app/provider.go` maps domain → DTO inside the module.
- **Consumes** `campsites/public.Provider` for effective capacity — via a **consumer-owned** narrow port `CampsiteCapacity` (ISP: availability needs only `EffectiveCapacity`, not `ActiveCampsites`/`GetCampsite`). Because `campsites/public.Provider`'s method set is a superset, it satisfies `CampsiteCapacity` structurally — no adapter needed. Availability never imports `campsites/domain` or `campsites/app`.
- **Consumes** `config/public` for the booking-window months — via a **consumer-owned** port `WindowConfig`, adapted at the composition root. If config's exact accessor is not final at implementation time, `WindowConfig` keeps availability unblocked (the same pattern campsite-management used with `identity/public`); the upstream provider is `config` (CFG, RF11 — PRD §10 "janela de reservas").
- Consumes `internal/platform/postgres` (pool + `WithTx` + `Executor(ctx)`) and `internal/shared/booking` (Period) — allowed cross-cutting infra / shared kernel.

---

## DDD Building Blocks

- **Value Object — `Diaria` (Diária).** Immutable; wraps a single **calendar date** (park-local, normalized to UTC midnight so it is a valid `map`/comparison key). Constructor `NewDiaria(date time.Time) Diaria` strips the time-of-day. Behavior: `Date() time.Time`, `Next() Diaria`, `Before/Equal`. The "09:00→09:00" boundary is ubiquitous-language documentation; the occupancy key is the date.
- **Domain function — `Diarias(p booking.Period) []Diaria`.** Expands a Período into its ordered Diárias `[Entry, Exit)` — Entry inclusive, **Exit (checkout) excluded**. This encodes PRD §6's "checkout day not counted"; it is a business rule, so it lives in `availability/domain`, not the shared kernel. Free function (KISS — no service struct for a pure map).
- **Entity — `DailyOccupancy`.** Identity = `(campsiteID, Diaria)`; state = `occupied int` (+ injected `effectiveCapacity int`, read live, never persisted). The **rich** per-Diária capacity behavior lives here: `Available() int` (= `max(0, effective − occupied)`), `CanAccommodate(headcount int) bool` (= `occupied + headcount ≤ effective`), `reserve(headcount int)` / `release(headcount int)` (unexported mutators floored at 0, driven only through the aggregate). Invariant `occupied ≥ 0`; a reduced capacity may leave `occupied > effective` transiently (PRD §13) — that blocks new bookings without voiding existing occupancy.
- **Aggregate — `PeriodOccupancy`.** Transient consistency boundary assembled per request from the locked occupancy of one `(campsiteID, período)`: an ordered slice of `*DailyOccupancy` (one per Diária). Root methods enforce **all-or-nothing**:
  - `Reserve(headcount int) error` — if **every** day `CanAccommodate(headcount)`, mutate all; else return `ErrNoVacancy` with **no** mutation (last-vacancy correctness at the domain level).
  - `Release(headcount int)` — decrement all days (floored at 0).
  One transaction covers one aggregate (ARCHITECTURE §4); the per-Acampamento advisory lock is the persistence-level mechanism that makes the assemble→check→persist sequence atomic across concurrent reservers.
- **Value Object — `BookingWindow`.** Immutable; wraps `windowMonths int` (from config). Behavior: `Contains(d Diaria, now time.Time) bool` and `Validate(p booking.Period, now time.Time) error` (→ `ErrOutsideBookingWindow`). Upper bound = last day of `(month(now) + windowMonths)`; lower bound = `date(now)` (today). Because it is evaluated against the injected `now`, the window **slides** automatically — no scheduled release job (PRD §6 "auto-released monthly" is emergent).
- **Repository — `OccupancyRepository`** (interface in `domain`, one per aggregate root). Expresses the persistence contract (advisory lock, load, adjust, range) without naming SQL; the pgx impl in `adapter/repository` performs the actual locking/upsert on the **ambient tx**.
- **Domain view — `DayAvailability`** (domain VO): `{Diaria, Occupied, EffectiveCapacity, Available}`, produced by `GetAvailabilityCalendar` for the read side and mapped to `public.DayAvailability`.
- **No domain events** for MVP — occupancy is read live via the public port on each query; push invalidation is YAGNI (PRD §13 handled by forward live-read). Add later only if a consumer needs to react.

**Ubiquitous language:** `Diaria` = Diária; `DailyOccupancy`/`PeriodOccupancy` = ocupação por diária / por período; `BookingWindow` = janela de reservas; "effective capacity" = capacidade efetiva; overbooking kept verbatim.

---

## Public Interfaces (exposed / consumed)

### Exposed — `internal/modules/availability/public/availability.go` (stdlib only)

```go
package public

import (
	"context"
	"errors"
	"time"
)

// Period is a flat, primitives-only booking window for cross-module calls.
// Entry is the 09:00 start of the first occupied Diária; Exit is the 09:00
// checkout day, which is NOT an occupied Diária. Occupied Diárias = [Entry, Exit).
type Period struct {
	Entry time.Time
	Exit  time.Time
}

// DayAvailability is a flat per-Diária availability snapshot.
type DayAvailability struct {
	Date              time.Time
	Occupied          int
	EffectiveCapacity int
	Available         int // max(0, EffectiveCapacity-Occupied)
}

var (
	// ErrNoVacancy: at least one Diária in the Period cannot admit the headcount.
	ErrNoVacancy = errors.New("availability: no vacancy")
	// ErrOutsideBookingWindow: some Diária is before today or beyond the horizon.
	ErrOutsideBookingWindow = errors.New("availability: outside booking window")
	// ErrNotFound: the campsite does not exist (mapped from campsites/public.ErrNotFound).
	ErrNotFound = errors.New("availability: campsite not found")
)

// Reserver is the transactional occupancy write port consumed by
// reservation-creation (RSV), walk-in (M4), and cancellation flows.
type Reserver interface {
	// Reserve locks the occupancy for EVERY Diária in p (per-Acampamento advisory
	// lock), enforces the booking window and the effective-capacity ceiling per
	// Diária, then increments occupancy by headcount — ALL on the AMBIENT
	// TRANSACTION carried in ctx (opened by the caller via postgres.WithTx).
	// Returns ErrNoVacancy / ErrOutsideBookingWindow / ErrNotFound WITHOUT
	// mutating on failure. Atomic all-or-nothing across the Period's Diárias.
	Reserve(ctx context.Context, campsiteID string, p Period, headcount int) error

	// Release decrements occupancy by headcount for each Diária in p, on the
	// ambient tx (hold expiry / cancel). Floors at 0; never fails on capacity.
	Release(ctx context.Context, campsiteID string, p Period, headcount int) error
}

// AvailabilityReader is the read port for the availability calendar. Backed by
// the same app read use case as the in-module htmx view (DRY). Read-only, no lock.
type AvailabilityReader interface {
	// Availability returns one DayAvailability per Diária in [from, to)
	// (checkout-excluded semantics). Unknown campsite → ErrNotFound.
	Availability(ctx context.Context, campsiteID string, from, to time.Time) ([]DayAvailability, error)
}
```

Implemented by `app/provider.go` (`type provider struct { reserve *ReserveOccupancy; release *ReleaseOccupancy; calendar *GetAvailabilityCalendar }`), which delegates to the use cases and maps domain sentinels → public sentinels. Wired at the composition root; the concrete type stays private. **The tx handle never appears in any signature** — atomicity flows through `ctx` (§ Ambient-Transaction Seam).

### Consumed — `campsites/public` (effective capacity) and `config/public` (window)

Declared as small consumer-owned ports in `internal/modules/availability/app/ports.go`:

```go
package app

import "context"

// CampsiteCapacity is availability's ISP-narrow view of campsites/public.Provider
// (which satisfies it structurally — no adapter). Returns campsites/public.ErrNotFound
// for an unknown campsite, mapped to availability's ErrNotFound at the boundary.
type CampsiteCapacity interface {
	EffectiveCapacity(ctx context.Context, campsiteID string) (int, error)
}

// WindowConfig yields the configured booking-window horizon in months (PRD §10,
// default owned by config). Satisfied by config/public, adapted at the root; a
// consumer-owned port keeps availability unblocked if config's accessor is not final.
type WindowConfig interface {
	BookingWindowMonths(ctx context.Context) (int, error)
}

// Clock supplies "now" for booking-window evaluation (CONVENTIONS: inject a clock
// into use cases that read time). A func() time.Time is injected; fixed in tests.
type Clock func() time.Time
```

### Domain repository port — `internal/modules/availability/domain/repository.go`

```go
package domain

import "context"

// OccupancyRepository persists the sparse (campsite, Diaria) → occupied ledger.
// Every method resolves its executor from ctx (ambient tx for Reserve/Release,
// pool for the read path) via internal/platform/postgres.Executor(ctx).
type OccupancyRepository interface {
	// AdvisoryLock takes a per-Acampamento pg_advisory_xact_lock on the ambient
	// tx, serializing reservers of the same campsite. Released at tx end.
	AdvisoryLock(ctx context.Context, campsiteID string) error
	// LoadOccupancy returns occupied per requested Diaria (0 when no row exists).
	LoadOccupancy(ctx context.Context, campsiteID string, ds []Diaria) (map[Diaria]int, error)
	// AddOccupancy upserts occupied += delta for each Diaria (delta may be negative;
	// floored at 0). Single statement per call set.
	AddOccupancy(ctx context.Context, campsiteID string, ds []Diaria, delta int) error
	// LoadRange returns occupied per Diaria in [from, to) for the calendar (no lock).
	LoadRange(ctx context.Context, campsiteID string, from, to Diaria) (map[Diaria]int, error)
}
```

---

## Ambient-Transaction Seam (ARCHITECTURE §7 — the last-vacancy guarantee)

`Reserve`/`Release` must run on the transaction **opened by the caller** (reservation-creation) so the occupancy increment and the reservation insert commit atomically. The tx handle is never passed explicitly — it travels in `ctx`:

- `internal/platform/postgres.WithTx(ctx, fn)` opens a pgx transaction and stores it in the `ctx` passed to `fn`.
- `internal/platform/postgres.Executor(ctx)` resolves the **ambient executor**: the tx stored by `WithTx` if present, else the pool. Repositories run every query through it.

The availability `OccupancyRepository` impl obtains its executor via `Executor(ctx)` on **every** method — so when reservation-creation does `WithTx(ctx, func(txctx) { reserver.Reserve(txctx, …); reservationRepo.Insert(txctx, …) })`, both writes hit the **same** tx and commit/rollback together (concurrent last-vacancy bookings → exactly one winner, because the advisory lock is held for the tx's duration).

> **Required platform seam (infra, not domain).** M0 provides this seam under **DATA-03**: `WithTx(ctx, pool, fn func(ctx) error)` stores the `pgx.Tx` in `ctx`, and `Executor(ctx, pool)` returns the ambient querier (`Query`/`QueryRow`/`Exec`) — the tx if inside `WithTx`, else the pool. It is shared M2 infrastructure — `reservation-creation` opens the tx and availability's repository joins it via `Executor(ctx)`. No new platform work is needed here; if DATA-03 ships a narrower shape at Execute time, reconcile against it.

### Reserve control flow (one transaction, held by the caller)

```
reservation-creation:  postgres.WithTx(ctx, func(txctx) error {
    reserver.Reserve(txctx, campsiteID, period, headcount)   // ── availability, this tx
    reservationRepo.Insert(txctx, reservation)               // ── reservations, same tx
    return nil                                               // commit both atomically
})

availability ReserveOccupancy.Handle(txctx, cmd):
  1. validate headcount ≥ 1
  2. ds := domain.Diarias(period)                         // checkout-excluded expansion
  3. months := windowCfg.BookingWindowMonths(txctx)       // config/public
     win := domain.NewBookingWindow(months)
     if err := win.Validate(period, clock()); err != nil  // → ErrOutsideBookingWindow
  4. cap := campsiteCap.EffectiveCapacity(txctx, id)      // campsites/public (live); ErrNotFound
  5. repo.AdvisoryLock(txctx, id)                         // serialize same-campsite reservers
  6. occ := repo.LoadOccupancy(txctx, id, ds)            // current counts (0 if absent)
  7. agg := domain.NewPeriodOccupancy(id, ds, occ, cap)
     if err := agg.Reserve(headcount); err != nil        // all-or-nothing → ErrNoVacancy
  8. repo.AddOccupancy(txctx, id, ds, +headcount)         // upsert increment
  → nil  (caller commits; on any error above, caller rolls back → nothing persisted)
```

`Release` is steps 1–2 + `AdvisoryLock` + `AddOccupancy(-headcount)` (no window/capacity check).

---

## Data Models

### Table `daily_occupancy`

Migration `db/migrations/0000NN_create_daily_occupancy.up.sql` (sequence assigned at implementation within the single gap-free stream — after M1 migrations; coordinate with the sibling `reservations` migration). `.down.sql` drops the table.

```sql
-- up
CREATE TABLE daily_occupancy (
    campsite_id UUID        NOT NULL,          -- campsites.id (no cross-module FK; see Tech Decisions)
    diaria      DATE        NOT NULL,          -- the 09:00 start date of the occupied Diária
    occupied    INTEGER     NOT NULL DEFAULT 0,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (campsite_id, diaria),         -- btree covers the calendar range scan
    CONSTRAINT daily_occupancy_occupied_nonneg CHECK (occupied >= 0)
);
```

```sql
-- down
DROP TABLE IF EXISTS daily_occupancy;
```

- The composite **PK `(campsite_id, diaria)`** is the only index needed: it serves point upserts *and* the calendar range scan `WHERE campsite_id = $1 AND diaria >= $2 AND diaria < $3` (leftmost prefix + range) — satisfying the <200 ms NFR without an extra index.
- Storage is **sparse**: a Diária row exists only once it has occupancy. Absent rows = `occupied 0`. No rows are pre-seeded.
- Effective capacity is **not** a column — it is read live from `campsites/public` (PRD §6: derived, not stored; and this makes PRD §13 capacity/overbooking changes take effect with zero migration).
- The `occupied >= 0` CHECK is the in-table backstop for the domain floor. The capacity **ceiling** cannot be a static CHECK (capacity is dynamic/live) — it is enforced by the locked read-check-increment under the advisory lock.

### sqlc queries — `db/queries/availability.sql`

- `AdvisoryLockCampsite` (`:exec`) — `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0));`
- `LoadOccupancy` (`:many`) — `SELECT diaria, occupied FROM daily_occupancy WHERE campsite_id = $1 AND diaria = ANY($2::date[]);`
- `AddOccupancy` (`:exec`) — per-Diária upsert increment:
  ```sql
  INSERT INTO daily_occupancy (campsite_id, diaria, occupied)
  SELECT $1, d, $3 FROM unnest($2::date[]) AS d
  ON CONFLICT (campsite_id, diaria)
  DO UPDATE SET occupied   = GREATEST(0, daily_occupancy.occupied + $3),
                updated_at = now();
  ```
  (`$3` is the signed delta; `GREATEST(0, …)` floors on release. The capacity ceiling was already enforced in the domain under the advisory lock, so no `WHERE` guard is needed here.)
- `LoadRange` (`:many`) — `SELECT diaria, occupied FROM daily_occupancy WHERE campsite_id = $1 AND diaria >= $2 AND diaria < $3 ORDER BY diaria;`

sqlc generates typed code into `internal/modules/availability/adapter/repository` per `sqlc.yaml`'s per-module convention (DATA-12). The advisory-lock and upsert SQL are **hand-written** in this file (concurrency SQL is authored directly, per PROJECT.md / STRUCTURE.md).

### App-layer DTOs (not crossing modules)

- `ReserveCommand{ CampsiteID string; Period booking.Period; Headcount int }` → `error`.
- `ReleaseCommand{ CampsiteID string; Period booking.Period; Headcount int }` → `error`.
- `CalendarQuery{ CampsiteID string; From, To time.Time }` → `[]DayAvailabilityView`.
- `DayAvailabilityView{ Date time.Time; Occupied, EffectiveCapacity, Available int }` — rendered by the htmx handler and mapped to `public.DayAvailability` by `provider.go`.

### Shared kernel — `internal/shared/booking.Period`

```go
package booking // internal/shared/booking — stdlib only, no business rules

type Period struct{ /* entry, exit time.Time (unexported) */ }

func NewPeriod(entry, exit time.Time) (Period, error) // invariant: exit.After(entry)
func (p Period) Entry() time.Time
func (p Period) Exit() time.Time
func (p Period) Nights() int // whole days in [Entry, Exit)
```

A stable date-range primitive reused by availability, reservations, checkin (CONVENTIONS §DRY). It holds **no** occupancy rule — the Diária-expansion (checkout-excluded) lives in `availability/domain.Diarias`. If M0/foundation has not shipped it, availability seeds this minimal VO and coordinates ownership (Open Decisions).

---

## Components

### domain.Diaria + Diarias expander

- **Purpose**: The atomic occupied-night VO and the checkout-excluded Período→Diárias expansion.
- **Location**: `internal/modules/availability/domain/{diaria.go}`
- **Interfaces**: `NewDiaria(time.Time) Diaria`, `(Diaria).Date()/Next()/Before()/Equal()`; `Diarias(booking.Period) []Diaria`.
- **Dependencies**: stdlib, `internal/shared/booking`.
- **Reuses**: `booking.Period`.

### domain.DailyOccupancy + PeriodOccupancy

- **Purpose**: Rich per-Diária capacity behavior + all-or-nothing aggregate.
- **Location**: `internal/modules/availability/domain/{occupancy.go}`
- **Interfaces**: `NewDailyOccupancy(id string, d Diaria, occupied, effective int) *DailyOccupancy`, `Available()/CanAccommodate(int)/reserve(int)/release(int)`; `NewPeriodOccupancy(id string, ds []Diaria, occ map[Diaria]int, effective int) *PeriodOccupancy`, `Reserve(int) error`, `Release(int)`, `Days() []*DailyOccupancy`.
- **Dependencies**: own domain (Diaria), stdlib.
- **Reuses**: —

### domain.BookingWindow

- **Purpose**: The sliding booking-window rule as a VO.
- **Location**: `internal/modules/availability/domain/{booking_window.go}`
- **Interfaces**: `NewBookingWindow(months int) BookingWindow`, `Contains(Diaria, now time.Time) bool`, `Validate(booking.Period, now time.Time) error`.
- **Dependencies**: own domain, `internal/shared/booking`, stdlib.
- **Reuses**: `booking.Period`, `Diarias`.

### app use cases

- **Purpose**: Single-purpose orchestration on the ambient tx (SRP).
- **Location**: `internal/modules/availability/app/{reserve_occupancy.go,release_occupancy.go,calendar.go,ports.go,dto.go}`
- **Interfaces**:
  - `ReserveOccupancy.Handle(ctx, ReserveCommand) error` — window + all-or-nothing capacity + increment (flow above).
  - `ReleaseOccupancy.Handle(ctx, ReleaseCommand) error` — advisory lock + decrement.
  - `GetAvailabilityCalendar.Handle(ctx, CalendarQuery) ([]DayAvailabilityView, error)` — capacity lookup + `LoadRange` + build views (Available floored).
- **Dependencies**: `domain` (aggregate, VOs, repo interface), consumer-owned ports (`CampsiteCapacity`, `WindowConfig`, `Clock`). No adapter, no other module's internals.
- **Reuses**: `internal/shared/booking`.

### app.provider (public impl)

- **Purpose**: Satisfy `public.Reserver` + `public.AvailabilityReader`; map domain sentinels → public sentinels and views → DTOs.
- **Location**: `internal/modules/availability/app/provider.go`
- **Interfaces**: implements the two public ports; `ErrNoVacancy`/`ErrOutsideBookingWindow` pass through; `campsites` not-found → `public.ErrNotFound`.
- **Dependencies**: own `app` use cases, `availability/public` (own DTOs only — no cycle).

### adapter/repository.OccupancyRepository (pgx)

- **Purpose**: Implement the domain port over Postgres via sqlc + `Executor(ctx)`; advisory lock + sparse upsert.
- **Location**: `internal/modules/availability/adapter/repository/occupancy_repository.go`
- **Interfaces**: implements `domain.OccupancyRepository`; every method runs through `postgres.Executor(ctx)` (ambient tx or pool); `AddOccupancy` via the `unnest` upsert; `AdvisoryLock` via `pg_advisory_xact_lock`.
- **Dependencies**: `internal/platform/postgres` (Executor/WithTx), generated sqlc, `domain`.
- **Reuses**: `postgres.Executor`, `pgtest` harness in tests.

### adapter/http.calendar_handler

- **Purpose**: Thin htmx availability calendar (in-module view; does **not** route through `public`).
- **Location**: `internal/modules/availability/adapter/http/calendar_handler.go` + `web/templates/availability/{calendar.html,day_cell.html}`
- **Interfaces**: `Routes(r chi.Router)` mounting `GET /campsites/{id}/availability?from=&to=`; decode/validate range → `GetAvailabilityCalendar.Handle` → render fragment (`HX-Request`) or full page; unknown campsite → 404; bad range → 422.
- **Dependencies**: `app.GetAvailabilityCalendar`, `internal/platform/web` renderer.
- **Reuses**: `web.Renderer` full/fragment pattern (as campsite-management handlers).

### composition-root wiring

- **Purpose**: Construct repo → use cases → provider → handler; inject `campsites/public.Provider` (as `CampsiteCapacity`), `config/public` (adapted to `WindowConfig`), and the clock; mount the calendar route; expose `public.Reserver` + `public.AvailabilityReader` for RSV/M4/M5 wiring.
- **Location**: `internal/platform/bootstrap/` (extend; e.g. `availability.go` module wiring implementing the `bootstrap.Module` seam).
- **Reuses**: `bootstrap.Module.Mount(chi.Router)` seam (SKEL), `postgres` pool/WithTx, `campsites/public`, `config/public`.

---

## Code Reuse Analysis

### Existing Components to Leverage

| Component | Location | How to Use |
| --------- | -------- | ---------- |
| `postgres.WithTx` + `Executor(ctx)` | `internal/platform/postgres` | Ambient-tx resolution in the repository; caller (RSV) opens the tx (add the `Executor(ctx)` accessor if absent — § Ambient-Transaction Seam) |
| `pgtest.Setup` | `internal/platform/postgres/pgtest` | Migrated Postgres 16 + isolation for integration + concurrency tests |
| `booking.Period` | `internal/shared/booking` | Reused period VO (reuse or seed per Open Decisions) |
| `campsites/public.Provider` | `internal/modules/campsites/public` | Live effective capacity; satisfies the narrow `CampsiteCapacity` port structurally |
| `config/public` | `internal/modules/config/public` | Booking-window months; adapted to `WindowConfig` at the root |
| `web.Renderer` | `internal/platform/web` | Render the calendar htmx full page / fragment |
| `bootstrap.Module` seam | `internal/platform/bootstrap` | Mount routes + expose the public ports without leaking internals |
| `sqlc.yaml` per-module convention | repo root (DATA-12) | Generate typed queries into `adapter/repository` |
| golang-migrate stream | `db/migrations` (DATA-04) | Add `0000NN_create_daily_occupancy` following the reversible naming contract |

### Integration Points

| System | Integration Method |
| ------ | ------------------ |
| `campsites` (M1, CAMP) | Consumes `campsites/public.Provider.EffectiveCapacity` live (PRD §13 forward-recompute is emergent from live reads) |
| `config` (M1, CFG) | Consumes the booking-window months via `WindowConfig` (adapted from `config/public`) |
| `reservation-creation` (M2, RSV) | **Imports `availability/public.Reserver`**; opens the tx via `postgres.WithTx` and calls `Reserve`/`Release` on it (this feature owns and implements the contract) |
| `checkin`/walk-in (M4), `admin` (M5) | Import `availability/public` — walk-in calls `Reserve`; admin reads via `AvailabilityReader` |
| `internal/platform/postgres` | Ambient-tx seam (`WithTx` + `Executor(ctx)`); advisory lock via pgx |

---

## Error Handling Strategy

| Error Scenario | Handling | User / Caller Impact |
| -------------- | -------- | ----- |
| Some Diária lacks room in `Reserve` | `PeriodOccupancy.Reserve` returns `domain.ErrNoVacancy`; use case propagates; provider maps → `public.ErrNoVacancy`; **no** mutation | Caller aborts its tx → nothing persisted; visitor sees "sem vagas" |
| Período outside the window | `BookingWindow.Validate` → `domain.ErrOutsideBookingWindow` → `public.ErrOutsideBookingWindow`; no mutation | Caller aborts; message "fora da janela de reservas" |
| Unknown campsite | `campsites/public.ErrNotFound` from `EffectiveCapacity` → mapped to `public.ErrNotFound`; calendar handler → 404 | 404 / `ErrNotFound` to caller |
| `headcount < 1` | Use case guard returns a validation error before any I/O | Caller sees invalid-request error |
| Invalid Período (`Exit ≤ Entry`) | `booking.NewPeriod` returns error at construction | Rejected before occupancy work |
| Capacity reduced below occupancy (PRD §13) | Live read yields smaller `effective`; `CanAccommodate` false for new reserves; calendar `Available = max(0, …) = 0`; existing `occupied` untouched | New bookings blocked; confirmed occupancy preserved |
| DB failure in lock/load/adjust | Wrapped `fmt.Errorf("...: %w", err)`; caller's `WithTx` rolls back the whole tx | Generic 500; no partial write |
| Concurrent last-vacancy `Reserve` | Advisory lock serializes; losers see current counts and get `ErrNoVacancy` | Exactly one winner; 0 oversell |
| Bad calendar range (`to ≤ from`, unparseable dates) | Handler validation → 422 fragment | Inline message |

Convention (CONVENTIONS.md): return `error`, wrap with `%w`; domain sentinels in `domain/errors.go` (`ErrNoVacancy`, `ErrOutsideBookingWindow`, `ErrInvalidHeadcount`); public sentinels in `public` (`ErrNoVacancy`, `ErrOutsideBookingWindow`, `ErrNotFound`); handlers map errors → HTTP; `errors.Is` for comparisons; no panics for control flow. Public and domain sentinels are **distinct values** (domain never leaks); the provider translates.

---

## Tech Decisions (only non-obvious ones)

| Decision | Choice | Rationale |
| -------- | ------ | --------- |
| Concurrency primitive | Per-Acampamento `pg_advisory_xact_lock(hashtextextended(campsite_id,0))` inside the caller's tx, not row `FOR UPDATE` | Occupancy rows are sparse — `FOR UPDATE` can't lock a not-yet-existing Diária row (phantom inserts). The advisory lock serializes same-site reservers, makes assemble→check→increment atomic, and auto-releases at tx end. ARCHITECTURE §7 permits advisory locks. Different sites stay concurrent. |
| Ambient tx via ctx | `Reserve`/`Release` run on `postgres.Executor(ctx)`; tx opened by the caller's `WithTx`; handle never in the signature | ARCHITECTURE §7 last-vacancy: occupancy increment + reservation insert must be one atomic unit. Keeps `public` stdlib-only (no pgx in the signature). |
| Effective capacity read live, never stored | `CampsiteCapacity.EffectiveCapacity` on every check | PRD §6 derived-not-stored; makes PRD §13 capacity/overbooking changes effective immediately with zero migration and no confirmed-reservation voiding. |
| Occupancy stored sparsely | Row created lazily on first `Reserve`; absent = 0 | No need to pre-seed a row per (site, day); the `unnest` upsert + advisory lock handle first-insert races. KISS. |
| Booking window from the clock | `BookingWindow.Validate(period, clock())` | The "auto-release next month at rollover" (PRD §6) is emergent — no cron/scheduled job. Deterministically testable with a fixed clock. |
| `Diaria` identity = calendar `DATE` (UTC-midnight key) | Normalize time-of-day away | The 09:00 boundary is documentation; the occupancy key is the date. Makes `Diaria` a valid map/compare key. Multi-tz out of scope. |
| Rich aggregate `PeriodOccupancy` (not a service) | All-or-nothing check lives on the aggregate root | Avoids the anemic "struct + service"; the period is the true consistency boundary (one tx = one aggregate, ARCHITECTURE §4). |
| Narrow consumer-owned `CampsiteCapacity` port | 1 method, satisfied structurally by `campsites/public.Provider` | ISP — availability needs only effective capacity; no adapter (Go structural typing). |
| Public read port `AvailabilityReader` exposed | Alongside `Reserver`; reuses the calendar use case | The reference contract ships `DayAvailability` in `public`; natural consumers (RSV date picker, admin). Carries no extra logic; removable if strict YAGNI preferred (Open Decisions). |
| No cross-module FK on `campsite_id` | Plain UUID column, existence via `campsites/public` | Keeps the two module schemas independent (module boundary in spirit); `occupied >= 0` CHECK stays as backstop. |
| Migration sequence number | **`daily_occupancy` is the FIRST M2 migration** (immediately after the last M1 migration), `reservations` is the second — deterministic intra-M2 order so two parallel authors never claim the same number. Absolute number = last-M1 + 1, fixed at Execute | The two M2 tables have no FK between them (order is free), so pinning availability-before-reservations removes the collision ambiguity a bare `0000NN` placeholder leaves. |
