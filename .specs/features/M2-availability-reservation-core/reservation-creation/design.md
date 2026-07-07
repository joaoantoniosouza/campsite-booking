# Reservation Creation Design

**Spec**: `.specs/features/M2-availability-reservation-core/reservation-creation/spec.md`
**Status**: Draft

---

## Architecture Overview

The `reservations` module is a bounded context implemented across three Clean Architecture layers (no `public` surface in M2 — YAGNI). A rich **`Reservation`** aggregate (Reserva) owns the creation invariants and the Pendente→Expirada guard; two single-purpose use cases orchestrate it — **`CreateReservation`** (the temporary hold) and **`ExpireHolds`** (the sweeper). The use case **owns the transaction** (`TxRunner`, backed by `platform/postgres.WithTx`): inside one tx it takes the availability occupancy lock via the provider-owned **`availability/public.Reserver`** seam, then persists the aggregate — so a last-vacancy race resolves to exactly one winner (ARCHITECTURE §7). The CPF-overlap rule is guaranteed by a Postgres **exclusion constraint + `daterange`** on the participant rows (this module's schema) plus a fast app-level pre-check. Cross-module capability (campsites, config, identity) is consumed through small **consumer-owned ports** adapted at the composition root; availability is consumed through its provider-owned port. Dependencies point inward only.

```mermaid
graph TD
    subgraph http["adapter/http (chi + htmx)"]
        HF["handler.go<br/>GET /reservations/new (form)<br/>POST /reservations (create)<br/>+ domain-error → HTTP mapping"]
    end
    subgraph sweep["adapter/sweeper"]
        RUN["runner.go<br/>ticker → ExpireHolds.Handle"]
    end
    subgraph app["app (use cases + ports + DTOs)"]
        UC1["CreateReservation.Handle"]
        UC2["ExpireHolds.Handle"]
        PORTS["ports.go<br/>PolicyProvider · ActorResolver ·<br/>CampsiteChecker · CodeGenerator ·<br/>Clock · TxRunner"]
    end
    subgraph domain["domain (pure)"]
        AGG["Reservation aggregate<br/>NewReservation() factory · Expire() guard<br/>Headcount() · CPFs()"]
        MEM["Participant · EmergencyContact<br/>(inside the aggregate)"]
        VO["VOs: Base62Code · ReservationStatus ·<br/>Kinship · Phone"]
        REPOI["ReservationRepository (interface)"]
        ERR["sentinel errors<br/>(ErrOverlappingReservation, …)"]
    end
    subgraph repo["adapter/repository"]
        REPO["reservation_repository.go<br/>pgx + sqlc · Insert/Update/LockByID/<br/>HasOverlappingCPF/FindExpiredPending"]
    end

    HF --> UC1
    RUN --> UC2
    UC1 --> AGG
    UC2 --> AGG
    AGG --> MEM --> VO
    UC1 --> PORTS
    UC2 --> PORTS
    UC1 & UC2 --> REPOI
    UC1 & UC2 -->|"Reserve / Release (in tx)"| AVL["availability/public.Reserver<br/>(consumed — provider-owned seam)"]
    REPO -.implements.-> REPOI
    REPO --> PG[(Postgres:<br/>reservations · reservation_participants<br/>+ EXCLUDE constraint · reservation_emergency_contacts)]

    PORTS -.adapted at root.-> CAMP["campsites/public.Provider"]
    PORTS -.adapted at root.-> CFG["config/public"]
    PORTS -.adapted at root.-> IDP["identity/public"]
    PORTS -.adapted at root.-> IDGEN["shared/id (Base62)"]
    PORTS -.adapted at root.-> TX["platform/postgres.WithTx"]
    AGG --> DOC["shared/document.CPF"]
    AGG --> BKG["shared/booking.Period<br/>(shared kernel — reused)"]

    BOOT["platform/bootstrap<br/>composition root"] -.wires repo→use cases→handlers+sweeper,<br/>injects availability.Reserver,<br/>adapts campsites/config/identity/id/WithTx→ports.-> HF
```

---

## Modules & Clean Architecture Layers Touched

| Layer | This feature adds | Path root |
| ----- | ----------------- | --------- |
| `domain` | `Reservation` aggregate + factory + `Expire` guard; `Participant`, `EmergencyContact` members; VOs (`Base62Code`, `ReservationStatus`, `Kinship`, `Phone`); `ReservationRepository` interface; sentinel errors | `internal/modules/reservations/domain/` |
| `app` | Use cases (`CreateReservation`, `ExpireHolds`), consumer-owned ports, command/result DTOs, availability period mapping | `internal/modules/reservations/app/` |
| `adapter/repository` | pgx/sqlc `ReservationRepository` (insert aggregate + children, overlap query, sweeper queries, `FOR UPDATE`) | `internal/modules/reservations/adapter/repository/` |
| `adapter/http` | htmx booking form + create handler + domain-error→HTTP mapping | `internal/modules/reservations/adapter/http/` |
| `adapter/sweeper` | background ticker runner invoking `ExpireHolds` | `internal/modules/reservations/adapter/sweeper/` |
| `db` | migration (3 tables + `btree_gist` + exclusion constraint) + sqlc queries | `db/migrations/`, `db/queries/reservations.sql` |
| templates | htmx booking form + confirmation partials | `web/templates/reservations/` |
| composition root | construct + wire (repo → use cases → handlers + sweeper), inject `availability.Reserver`, adapt campsites/config/identity/id/WithTx → ports, mount routes, start sweeper | `internal/platform/bootstrap/` |
| **`public`** | **none** — this feature exposes no `reservations/public` surface (YAGNI; M3/M4 add ports when first consumed) | — |
| shared kernel | reuse `booking.Period` date-range VO (the same primitive availability uses; seed if foundation has not shipped it) | `internal/shared/booking/` |

### Module Boundary Rule (design-level statement)

Per ARCHITECTURE §2 (**non-negotiable**):

- **Consumes** `availability/public.Reserver` (provider-owned seam — occupancy lock + effective-capacity/window enforcement + increment/decrement). `reservations/app` imports `availability/public` only; never `availability/domain`/`app`. The transaction is **owned by reservations** and carried ambiently in `ctx` (`platform/postgres.WithTx`); no tx type crosses the boundary — availability's implementation resolves the executor from `ctx`, exactly as this module's repository does.
- **Consumes** `campsites/public.Provider`, `config/public`, `identity/public` via **consumer-owned ports** (`CampsiteChecker`, `PolicyProvider`, `ActorResolver`) declared in `reservations/app` and adapted to the real publics at the composition root (the pattern campsite-management used with `identity/public`). Small, consumer-shaped (ISP): each exposes only what `CreateReservation` needs. If a provider's exact shape is not final at implementation time, the consumer-owned port keeps this module unblocked.
- **Consumes** shared kernel `internal/shared/document` (`CPF`), `internal/shared/id` (Base62 code + UUID), and `internal/shared/booking` (`Period` date-range VO — the **same** primitive availability uses at the seam) — reused, never reimplemented (DRY).
- **Exposes** nothing. No other module imports `reservations` in M2.
- The **CPF-overlap guarantee is this module's** (exclusion constraint in `reservations` schema); the **occupancy/effective-capacity guarantee is availability's** (behind the `Reserver` seam).

---

## DDD Building Blocks

- **Aggregate root — `Reservation` (Reserva).** Identity `ReservationID` (UUID string). Unexported fields; all state set through the factory or the `Expire` guard (status is **never** field-set from a handler — ARCHITECTURE §8). Holds behavior: `Headcount() int`, `CPFs() []string`, `Expire(now) error`. Born **Pendente** with `expiresAt = now + ttl`. References the campsite **by ID** (`campsiteID string`), never by pointer.
- **Members inside the aggregate** (mutated only through the root):
  - `Participant` — `{ CPF document.CPF, Name string, Responsible bool }`; `NewParticipant(...)` validates non-blank name.
  - `EmergencyContact` — `{ Name string, Phone, Kinship }`; `NewEmergencyContact(...)` validates non-blank name.
- **Value Objects** (immutable, self-validating in constructor):
  - `Period` — **reused from the shared kernel `internal/shared/booking`** (the same VO availability uses), **not** redefined here (DRY, per brief "reuse shared kernel … Period"): `booking.NewPeriod(entry, exit) (Period, error)`, invariant `exit.After(entry)`, `Nights() int`, `[entry, exit)` semantics. This module maps it to the flat `availability/public.Period` DTO at the seam and to a `[entry, exit)` `daterange` for the exclusion constraint (checkout Diária excluded — PRD §6 Ocupação).
  - `Base62Code` — wraps a validated `^[0-9A-Za-z]+$` string; `NewBase62Code(string) (Base62Code, error)`. Generation is delegated to `internal/shared/id`; this VO only validates/holds.
  - `ReservationStatus` — enum; this feature defines/uses `Pendente` (initial) and `Expirada` (post-sweep). The full machine (Cancelada/No-show/Check-in realizado/Finalizada) is shared vocabulary (ARCHITECTURE §8) extended by M3/M4 features. `IsPending()`.
  - `Kinship` (grau de parentesco) — non-blank string VO.
  - `Phone` (telefone) — non-blank string VO with a minimal digit check.
- **Factory** — `NewReservation(id string, code Base62Code, campsiteID string, period Period, participants []Participant, contacts []EmergencyContact, limit int, now time.Time, ttl time.Duration) (*Reservation, error)` centralizes every creation invariant: non-empty campsiteID; `1 ≤ len(participants) ≤ limit`; exactly one Responsável and it is ∈ participants; no duplicate CPF; `len(contacts) ≥ 1`. `Reconstitute(...)` rebuilds a persisted aggregate for the repository without re-running create-only defaults.
- **Repository** — `ReservationRepository` interface in `domain` (one per aggregate root). pgx impl in `adapter/repository`.
- **No domain service.** Overlap detection is a DB query (the exclusion constraint is the real guarantee, ARCHITECTURE §7); a Go "overlap service" would be anemic — deliberately avoided (KISS). The app pre-check calls `repo.HasOverlappingCPF`.
- **No domain events for MVP.** Availability release happens via a direct `Reserver.Release` call from the sweeper (in-process, ARCHITECTURE §4 allows synchronous). Event plumbing is YAGNI here.

**Ubiquitous language:** type `Reservation` = Reserva; `Participant` = Participante; `EmergencyContact` = contato de emergência; status literals PT-BR (`Pendente`, `Expirada`); `Period` = período; Diária = a `[entry, exit)` day.

---

## Public Interfaces (consumed; none exposed)

### Consumed — `availability/public.Reserver` (provider-owned seam; authored by availability-engine)

```go
// availability/public — imports stdlib only; flat DTOs (referenced by contract, not read from its folder).
type Period struct { Entry, Exit time.Time } // Entry @09:00 first Diária; Exit @09:00 checkout, NOT occupied
var ErrNoVacancy            = errors.New("availability: no vacancy")
var ErrOutsideBookingWindow = errors.New("availability: outside booking window")
type Reserver interface {
    // Reserve locks + enforces effective capacity/window + increments occupancy, on the ambient tx (ctx).
    Reserve(ctx context.Context, campsiteID string, p Period, headcount int) error
    // Release decrements occupancy, on the ambient tx (ctx).
    Release(ctx context.Context, campsiteID string, p Period, headcount int) error
}
```

`CreateReservation` calls `Reserve` inside its transaction (before `repo.Insert`); `ExpireHolds` calls `Release` inside the per-reservation expiry transaction. Both propagate `ErrNoVacancy` / `ErrOutsideBookingWindow` outward.

### Consumed — consumer-owned ports (`internal/modules/reservations/app/ports.go`)

```go
package app

type ActorType int
const ( ActorPF ActorType = iota; ActorPJ )

// PolicyProvider — configurable policy (backed by config/public — CFG). Default TTL 10 min; PF 5 / PJ 15.
type PolicyProvider interface {
    HoldTTL(ctx context.Context) (time.Duration, error)
    ParticipantLimit(ctx context.Context, actor ActorType) (int, error)
}

// ActorResolver — booking actor type (backed by identity/public — AUTH).
// Anonymous or registered PF → ActorPF; authenticated company → ActorPJ.
type ActorResolver interface {
    ResolveActor(ctx context.Context) ActorType
}

// CampsiteChecker — target-campsite validation (backed by campsites/public.Provider — CAMP).
// Returns nil if the campsite exists and is Ativo; ErrCampsiteNotFound / ErrCampsiteInactive otherwise.
type CampsiteChecker interface {
    BookableCampsite(ctx context.Context, campsiteID string) error
}

// CodeGenerator — candidate Base62 codes (backed by internal/shared/id).
type CodeGenerator interface { NewCode() string }

// Clock — deterministic "now".
type Clock func() time.Time

// TxRunner — one DB transaction; executor carried ambiently in ctx (backed by platform/postgres.WithTx).
// availability.Reserve/Release and the reservation repository both resolve the executor from ctx.
type TxRunner interface {
    Run(ctx context.Context, fn func(ctx context.Context) error) error
}
```

The composition root adapts `campsites/public.Provider.GetCampsite` → `CampsiteChecker` (Status=="Ativo"; `public.ErrNotFound`→`ErrCampsiteNotFound`), `config/public` → `PolicyProvider`, `identity/public` principal → `ActorResolver`, `shared/id` → `CodeGenerator`, `postgres.WithTx` → `TxRunner`. Concrete adapters stay at the root; `app` depends only on the interfaces (DIP).

---

## Data Models

### Migration `db/migrations/0000NN_create_reservations.up.sql`

Sequence number assigned at Execute time within the single gap-free stream (after M1 migrations `campsites`, `identity`, `config`). `btree_gist` is required for the composite gist exclusion (equality on `cpf` + range on `during`).

```sql
-- up
CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE reservations (
    id              UUID        PRIMARY KEY,              -- generated in Go (shared/id)
    code            TEXT        NOT NULL UNIQUE,          -- Base62 [0-9A-Za-z]
    campsite_id     UUID        NOT NULL,                 -- reference by ID (campsites)
    responsible_cpf TEXT        NOT NULL,                 -- CPF digits (the Responsável)
    booked_by_user_id UUID,                               -- NULL for anonymous PF; the authenticated principal (identity) that created it — enables RF07 "authenticated user history" (esp. PJ) without a later migration
    entry_date      DATE        NOT NULL,                 -- first Diária (09:00)
    exit_date       DATE        NOT NULL,                 -- checkout day (NOT an occupied Diária)
    status          TEXT        NOT NULL DEFAULT 'Pendente',
    expires_at      TIMESTAMPTZ,                          -- hold TTL deadline
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT reservations_status_valid  CHECK (status IN ('Pendente','Expirada')),
    CONSTRAINT reservations_period_valid  CHECK (exit_date > entry_date),
    CONSTRAINT reservations_code_charset  CHECK (code ~ '^[0-9A-Za-z]+$')
);
CREATE INDEX reservations_sweeper_idx ON reservations (status, expires_at); -- sweeper scan

CREATE TABLE reservation_participants (
    id             UUID      PRIMARY KEY,
    reservation_id UUID      NOT NULL REFERENCES reservations(id) ON DELETE CASCADE,
    cpf            TEXT      NOT NULL,
    name           TEXT      NOT NULL,
    is_responsible BOOLEAN   NOT NULL DEFAULT false,
    during         DATERANGE NOT NULL,                    -- [entry_date, exit_date) — checkout excluded
    active         BOOLEAN   NOT NULL DEFAULT true,       -- true while occupying; false once Expirada
    CONSTRAINT rp_name_not_blank CHECK (length(btrim(name)) > 0),
    CONSTRAINT rp_unique_cpf_per_reservation UNIQUE (reservation_id, cpf),
    -- FINAL overlap guarantee: same CPF cannot occupy two overlapping active períodos in ANY campsite.
    CONSTRAINT rp_no_overlap_per_cpf EXCLUDE USING gist (cpf WITH =, during WITH &&) WHERE (active)
);
CREATE INDEX rp_cpf_active_idx ON reservation_participants (cpf) WHERE active; -- pre-check probe

CREATE TABLE reservation_emergency_contacts (
    id             UUID PRIMARY KEY,
    reservation_id UUID NOT NULL REFERENCES reservations(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    phone          TEXT NOT NULL,
    kinship        TEXT NOT NULL,
    CONSTRAINT rec_name_not_blank    CHECK (length(btrim(name)) > 0),
    CONSTRAINT rec_phone_not_blank   CHECK (length(btrim(phone)) > 0),
    CONSTRAINT rec_kinship_not_blank CHECK (length(btrim(kinship)) > 0)
);
```

```sql
-- down
DROP TABLE IF EXISTS reservation_emergency_contacts;
DROP TABLE IF EXISTS reservation_participants;
DROP TABLE IF EXISTS reservations;
-- btree_gist left installed (shared infra); dropping it is out of this migration's scope.
```

**Why denormalize `during` + `active` onto participant rows:** a Postgres exclusion constraint cannot span tables, and the overlap rule keys on `cpf` (per participant) but the período/status live on the reservation. Each participant row (including the Responsável) therefore carries the reservation's `during` range and `active` flag, written atomically with the reservation in one `Insert`. On expiry the sweeper flips `active=false` for the reservation's rows, removing them from the constraint so the CPFs free immediately (PRD §6). The `[entry, exit)` bound makes adjacent reservations (checkout day == next entry day) non-overlapping.

### sqlc queries — `db/queries/reservations.sql`

`InsertReservation`, `InsertParticipant`, `InsertEmergencyContact`, `HasOverlappingCPF` (`SELECT EXISTS(… WHERE active AND cpf = ANY($1) AND during && daterange($2,$3,'[)'))`), `FindExpiredPending` (`WHERE status='Pendente' AND expires_at <= $1 ORDER BY expires_at LIMIT $2`), `LockReservationByID` (`… WHERE id=$1 FOR UPDATE`) + child selects, `UpdateReservationStatus`, `SetParticipantsActive`. Concurrency/locking SQL is hand-written; sqlc generates typed code into `adapter/repository` per `sqlc.yaml`.

### App-layer DTOs (do not cross modules)

- `CreateReservationCommand{ CampsiteID string; Entry, Exit time.Time; ResponsibleCPF string; Participants []ParticipantInput; EmergencyContacts []EmergencyContactInput }`
  - `ParticipantInput{ CPF, Name string }` · `EmergencyContactInput{ Name, Phone, Kinship string }`
- `CreateReservationResult{ ReservationID, Code, Status string; ExpiresAt time.Time }`

---

## Components

### domain VOs + members + errors

- **Purpose**: Self-validating reservation primitives; a booking cannot be built from invalid parts.
- **Location**: `internal/modules/reservations/domain/{code.go,status.go,kinship.go,phone.go,participant.go,emergency_contact.go,errors.go}` (`Period` is reused from `internal/shared/booking`)
- **Interfaces**: `NewBase62Code`, `ParseReservationStatus`/`IsPending`, `NewKinship`, `NewPhone`, `NewParticipant`, `NewEmergencyContact`; sentinel errors. (`Period` comes from `internal/shared/booking` — reused, not defined here.)
- **Dependencies**: stdlib; `internal/shared/document` (CPF) for `Participant`.
- **Reuses**: `shared/document.CPF`.

### domain.Reservation (aggregate + factory)

- **Purpose**: Rich aggregate enforcing every creation invariant and the Pendente→Expirada transition.
- **Location**: `internal/modules/reservations/domain/reservation.go`
- **Interfaces**: `NewReservation(...) (*Reservation, error)`, `Reconstitute(...) *Reservation`, `Expire(now) error`, `Headcount() int`, `CPFs() []string`, `Period() Period`, `CampsiteID() string`, getters (`ID/Code/Status/Participants/Contacts/ExpiresAt/ResponsibleCPF`) for mapping.
- **Dependencies**: own VOs/members; stdlib.
- **Reuses**: `shared/document.CPF`.

### domain.ReservationRepository

- **Purpose**: Persistence port for the aggregate (one repo per root).
- **Location**: `internal/modules/reservations/domain/repository.go`
- **Interfaces**:
  - `Insert(ctx, *Reservation) error` — reservation + participant rows (`during`,`active=true`) + contacts; `ErrDuplicateCode` on code UNIQUE violation, `ErrOverlappingReservation` on exclusion violation.
  - `Update(ctx, *Reservation) error` — persist a transitioned aggregate (status + participant `active`).
  - `LockByID(ctx, id string) (*Reservation, error)` — `SELECT … FOR UPDATE` load.
  - `HasOverlappingCPF(ctx, cpfs []string, p Period) (bool, error)` — pre-check probe.
  - `FindExpiredPending(ctx, now time.Time, limit int) ([]string, error)` — sweeper candidates (ids).
- **Dependencies**: own domain only.

### app.CreateReservation

- **Purpose**: The temporary-hold use case (single responsibility) — owns the transaction and the seam order.
- **Location**: `internal/modules/reservations/app/create_reservation.go` (+ `dto.go`, `ports.go`)
- **Flow**: resolve actor → `PolicyProvider` (limit, TTL) → `CampsiteChecker.BookableCampsite` → build VOs/aggregate (`now` captured once). Then a **bounded code-retry loop** around `TxRunner.Run(ctx, fn)` where `fn` = `HasOverlappingCPF` pre-check → `Reserver.Reserve` → `repo.Insert`; on `ErrDuplicateCode` regenerate the code and retry (≤3), else return.
- **Interfaces**: `Handle(ctx, CreateReservationCommand) (CreateReservationResult, error)`.
- **Dependencies**: `domain`, `availability/public.Reserver`, ports (`PolicyProvider`,`ActorResolver`,`CampsiteChecker`,`CodeGenerator`,`Clock`,`TxRunner`).
- **Reuses**: `shared/id` (via `CodeGenerator`), `postgres.WithTx` (via `TxRunner`).

### app.ExpireHolds

- **Purpose**: The sweeper use case — expire overdue holds and release vacancies.
- **Location**: `internal/modules/reservations/app/expire_holds.go`
- **Flow**: `now := clock()`; `repo.FindExpiredPending(ctx, now, batch)`; for each id `TxRunner.Run`: `repo.LockByID` → `res.Expire(now)` (skip on `ErrNotExpirable`) → `repo.Update` (status Expirada + `active=false`) → `Reserver.Release`. Returns the count expired.
- **Interfaces**: `Handle(ctx) (int, error)`.
- **Dependencies**: `domain`, `availability/public.Reserver`, ports (`Clock`,`TxRunner`).

### adapter/repository.ReservationRepository (pgx)

- **Purpose**: Implement the domain port over Postgres via sqlc, resolving the ambient executor from `ctx`.
- **Location**: `internal/modules/reservations/adapter/repository/reservation_repository.go`
- **Interfaces**: implements `domain.ReservationRepository`; maps Postgres error codes (`23505` on `code`→`ErrDuplicateCode`; `23P01` exclusion→`ErrOverlappingReservation`); `Reconstitute` from rows.
- **Dependencies**: `platform/postgres` (executor-from-ctx + `WithTx`), generated sqlc, `domain`.
- **Reuses**: `postgres` tx seam, `pgtest` harness in tests.

### adapter/http.Handler

- **Purpose**: Thin htmx booking form + create endpoint; map domain/availability errors → HTTP.
- **Location**: `internal/modules/reservations/adapter/http/handler.go` + `web/templates/reservations/{new.html,form.html,confirmation.html,error.html}`
- **Interfaces**: `Routes(r chi.Router)` mounting `GET /reservations/new` (form; campsite options via `campsites/public.ActiveCampsites` adapter) and `POST /reservations` (decode → `CreateReservation.Handle` → confirmation fragment with code + `expiresAt`, or error fragment). No auth guard on create (anonymous PF allowed).
- **Dependencies**: `app.CreateReservation`, `platform/web` renderer.
- **Reuses**: `web.Renderer` (full page vs `HX-Request` fragment).

### adapter/sweeper.Runner

- **Purpose**: Background ticker that invokes `ExpireHolds` on an interval (infra timer — kept out of `app`).
- **Location**: `internal/modules/reservations/adapter/sweeper/runner.go`
- **Interfaces**: `Run(ctx context.Context)` (loop on `time.Ticker`, exits on `ctx.Done()`), started as a goroutine by bootstrap; interval configurable.
- **Dependencies**: `app.ExpireHolds`, `platform/log`.

### composition-root wiring

- **Purpose**: Construct repo → use cases → handlers + sweeper; inject `availability.Reserver`; adapt campsites/config/identity/id/WithTx → ports; mount routes; start the sweeper goroutine.
- **Location**: `internal/platform/bootstrap/` (extends the composition root; `reservations` implements the `bootstrap.Module` seam).
- **Reuses**: `bootstrap.Module.Mount`, `postgres` pool + `WithTx`, `availability/public`, `campsites/public`, `config/public`, `identity/public`, `shared/id`.

---

## Code Reuse Analysis

### Existing Components to Leverage

| Component | Location | How to Use |
| --------- | -------- | ---------- |
| `postgres.WithTx` + executor-from-ctx | `internal/platform/postgres` | The single create/expiry transaction; ambient executor shared with `availability.Reserve/Release` (ARCHITECTURE §7) |
| `pgtest.Setup` | `internal/platform/postgres/pgtest` | Migrated Postgres 16 + isolation for repository/exclusion/concurrency/handler tests |
| `document.CPF` | `internal/shared/document` | Participant + Responsável CPF VO (validation) — do NOT reimplement |
| `id` (Base62 + UUID) | `internal/shared/id` | Reservation code generation + UUIDs (via `CodeGenerator`) — do NOT reimplement |
| `web.Renderer` | `internal/platform/web` | Render htmx form / confirmation / error fragments |
| `bootstrap.Module` seam | `internal/platform/bootstrap` | Mount reservation routes + start sweeper without leaking internals |
| `sqlc.yaml` per-module convention | repo root (DATA-12) | Generate typed queries into `adapter/repository` |
| golang-migrate stream | `db/migrations` (DATA-04) | Add `0000NN_create_reservations` following the reversible naming contract |

### Integration Points

| System | Integration Method |
| ------ | ------------------ |
| `availability` (M2, AVL) | `Reserver.Reserve`/`Release` called inside this module's transaction (provider-owned seam); occupancy lock + effective-capacity/window enforcement is availability's; propagates `ErrNoVacancy`/`ErrOutsideBookingWindow` |
| `campsites` (M1, CAMP) | `campsites/public.Provider.GetCampsite`/`ActiveCampsites` adapted → `CampsiteChecker` (exists + Ativo) and the form's campsite options |
| `config` (M1, CFG) | `config/public` adapted → `PolicyProvider` (hold TTL default 10 min; PF 5 / PJ 15 limits, configurable) |
| `identity` (M1, AUTH) | `identity/public` principal adapted → `ActorResolver` (PF/PJ; anonymous → PF). No auth guard on create |
| shared kernel | `document.CPF`, `id` (Base62/UUID), `booking.Period` (date-range VO shared with availability) |

---

## Error Handling Strategy

| Error Scenario | Handling | User Impact |
| -------------- | -------- | ----------- |
| Invalid VO / factory invariant (blank name, `Exit ≤ Entry`, no participants, Responsável ∉ list, limit exceeded, duplicate CPF, no emergency contact, bad phone/kinship) | Constructor/factory returns wrapped domain sentinel; use case returns it; handler → **422** htmx error fragment | Inline validation message; nothing persisted, no vacancies blocked |
| Campsite unknown / Inativo | `CampsiteChecker` → `ErrCampsiteNotFound` (**404**) / `ErrCampsiteInactive` (**422**) before any lock | "Campsite unavailable"; nothing persisted |
| `availability.ErrNoVacancy` | Propagated from `Reserve`; tx rolls back; handler → **409** fragment | "Sem vagas para o período"; nothing persisted |
| `availability.ErrOutsideBookingWindow` | Propagated from `Reserve`; handler → **422** fragment | "Fora da janela de reservas" |
| `ErrOverlappingReservation` (pre-check or exclusion `23P01`) | Pre-check returns it before `Reserve`; the DB exclusion is the final guarantee on concurrent inserts; handler → **409** fragment | "CPF já possui reserva no período" |
| Base62 code collision (`23505` on `code`) | Repo → `ErrDuplicateCode`; use case regenerates + retries the tx (≤3); only surfaces if all retries collide | Transparent; creation succeeds |
| Persistence failure after `Reserve` | `TxRunner` rolls back the whole tx → occupancy increment reverted (no orphan); handler → **500** | Generic error; no partial write |
| Sweeper hits a non-Pendente / not-yet-expired hold | `res.Expire` returns `ErrNotExpirable`; tx commits no change (idempotent) | None (skipped) |
| Sweeper races another transition on the same hold | `LockByID` (`FOR UPDATE`) serializes; transitioned at most once | None (no double-release) |

Convention (CONVENTIONS.md): return `error`, wrap with `%w`; sentinel domain errors in `domain/errors.go` (`ErrInvalidPeriod`, `ErrInvalidCode`, `ErrEmptyName`, `ErrNoParticipants`, `ErrResponsibleNotParticipant`, `ErrParticipantLimitExceeded`, `ErrDuplicateParticipantCPF`, `ErrNoEmergencyContact`, `ErrInvalidPhone`, `ErrInvalidKinship`, `ErrNotExpirable`, `ErrOverlappingReservation`, `ErrDuplicateCode`) + app errors (`ErrCampsiteNotFound`, `ErrCampsiteInactive`); handlers map via `errors.Is`; no panics for control flow.

---

## Tech Decisions (only non-obvious ones)

| Decision | Choice | Rationale |
| -------- | ------ | --------- |
| Transaction ownership | `CreateReservation` owns one tx (`TxRunner`→`postgres.WithTx`); calls `Reserver.Reserve` then `repo.Insert` inside it | ARCHITECTURE §7: the vacancy lock + increment and the reservation insert must commit atomically so the last-vacancy race has exactly one winner; ambient `ctx` tx keeps the tx type off the public boundary |
| Seam order: `Reserve` before `Insert` | Take the availability lock first, persist after | The lock (inside `Reserve`) is what serializes the last-vacancy race; persisting first would leave a window with no capacity guarantee |
| CPF-overlap final guarantee | Postgres `EXCLUDE USING gist (cpf =, during &&) WHERE active` + app pre-check | ARCHITECTURE §7 — the DB is the final guarantee; the pre-check is a fast, friendly fail. Denormalized onto participant rows because exclusion constraints are single-table |
| Diária range bound | `daterange [entry, exit)` (checkout exclusive) | PRD §6 Ocupação — checkout day is not an occupied Diária; makes adjacent reservations non-overlapping |
| `active` flag drives the constraint | Set `true` on insert, `false` on expiry (and future terminal states) | Only live holds/confirmations block a CPF; Expirada/Cancelada/No-show must free it immediately (PRD §6) without deleting history |
| Code uniqueness | DB `UNIQUE` + regenerate-on-collision (≤3) in the use case | Race-proof (constraint is authoritative); retry keeps creation succeeding; generation reuses `shared/id` (DRY) |
| Sweeper = app use case + thin ticker | `ExpireHolds` (unit-testable with fakes + injected clock) + `adapter/sweeper.Runner` (timer) | Keeps the timer (infra) out of `app`; the transition/release logic is deterministically unit- and integration-testable |
| Sweeper isolation | One tx **per** expired reservation, `LockByID` `FOR UPDATE`, idempotent `Expire` guard | A single failure doesn't block the batch; row lock makes concurrent transitions safe; re-running is a no-op |
| Consumer-owned ports for campsites/config/identity | `CampsiteChecker`/`PolicyProvider`/`ActorResolver` in `app`, adapted at root | ISP + DIP + the campsite-management precedent; keeps this module testable and unblocked if a provider's public shape is still in flight |
| `availability/public.Reserver` consumed directly | Not wrapped in a consumer-owned port | It is the explicitly provider-owned concurrency seam with a final shape; wrapping it would add indirection with no boundary benefit |
| No `reservations/public` in M2 | Expose nothing | YAGNI (ARCHITECTURE §2) — no module consumes reservations yet; M3/M4 add lookup/cancel/check-in ports when first needed |
| No domain events | Direct `Reserver.Release` from the sweeper | In-process synchronous release is sufficient for MVP (ARCHITECTURE §4); event bus is YAGNI |
| Responsible telefone/e-mail optional | Not enforced in the factory | Brief's required set is CPF+name + emergency contact; avoids widening MVP scope (see spec Open Decisions) |
| Migration sequence number | **`reservations` is the SECOND M2 migration** — immediately after `daily_occupancy` (the first M2 migration, availability-engine), i.e. last-M1 + 2. Absolute number fixed at Execute | Deterministic intra-M2 order (availability then reservations) so two parallel authors never claim the same number; the two M2 tables have no FK between them, so the order is a convention, not a dependency. |
