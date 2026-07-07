# Architecture — Target (Greenfield)

**Status:** Authoritative reference for all feature specs/designs. This is the *target*
architecture; the M0 skeleton establishes it in code.

This project is a **modular monolith** built with **Domain-Driven Design (DDD)** tactical
patterns inside a **Clean Architecture** dependency structure. Single deployable binary,
strong internal boundaries so future extraction stays cheap.

---

## 1. Modules (Bounded Contexts)

Seven modules. Each is a bounded context with its own ubiquitous language.

| Module         | Responsibility (bounded context) |
| -------------- | -------------------------------- |
| `identity`     | PF users, PJ companies + legal responsible, authentication, sessions, roles |
| `campsites`    | Acampamento CRUD, effective-capacity calculation |
| `config`       | System configuration store + typed defaults (limits, windows, deadlines, TTL, overbooking) |
| `availability` | Occupancy per person/Diária/campsite, availability query, booking-window enforcement |
| `reservations` | Reservation aggregate lifecycle: holds, participants, overlap, transfer, remote cancellation |
| `checkin`      | Porteiro operations: per-participant check-in, walk-in, on-site cancellation, no-show |
| `admin`        | Dashboard read models + management surfaces (orchestrates other modules via their public APIs) |

Modules map roughly to milestones/features but a feature may add code to an existing module.

---

## 2. The Module Boundary Rule (NON-NEGOTIABLE)

> **No module may import another module's `domain` or `app` (use-case) packages.**

Cross-module communication happens **only** through a module's **public interface package**
(`public/`). The `public/` package exposes:

- **Interfaces (ports)** other modules depend on (e.g. `availability/public.Checker`).
- **Plain DTOs** — flat structs of primitives/std types. **Domain entities and value objects
  never cross a module boundary.** Map domain → DTO inside the owning module.

The **composition root** (`cmd/server` + `internal/platform/bootstrap`) is the only place that
knows concrete types: it constructs each module's use cases and wires one module's public
implementation into another module's use case as a dependency.

```
reservations/app  ──depends on──▶  availability/public.Checker  (interface)
                                          ▲
composition root ──wires──▶  availability/app.Service (concrete impl of the interface)
```

`reservations` never imports `availability/domain` or `availability/app`. It only knows the
interface declared in (or consumed from) `availability/public`.

**Consuming vs. owning a port.** Prefer declaring the port interface in the *consumer* module
(dependency inversion — the consumer owns the abstraction it needs) OR in the provider's
`public/` package when the contract is naturally provider-defined. Pick one per relationship
and state it in the design doc. Either way, the concrete implementation stays private to the
provider and is injected at the composition root.

---

## 3. Clean Architecture Layers (inside each module)

Dependencies point **inward only**. Inner layers know nothing about outer layers.

```
        ┌─────────────────────────────────────────────┐
        │  adapter/  (http handlers, repositories,      │  ← knows app + domain
        │            sqlc queries, templates)           │
        │   ┌───────────────────────────────────────┐  │
        │   │  app/  (use cases, application         │  │  ← knows domain + ports
        │   │        services, port interfaces, DTOs)│  │
        │   │   ┌────────────────────────────────┐   │  │
        │   │   │  domain/  (entities, value      │   │  │  ← knows NOTHING outside itself
        │   │   │  objects, aggregates, domain    │   │  │
        │   │   │  services, domain events,       │   │  │
        │   │   │  repository interfaces)         │   │  │
        │   │   └────────────────────────────────┘   │  │
        │   └───────────────────────────────────────┘  │
        └─────────────────────────────────────────────┘
                    public/  (interfaces + DTOs for OTHER modules)
```

**Dependency rule per layer:**

| Layer      | May import                                   | May NOT import |
| ---------- | -------------------------------------------- | -------------- |
| `domain`   | stdlib, shared kernel VOs only               | app, adapter, any other module, pgx/chi/http |
| `app`      | own `domain`, own ports, other modules' `public` | own `adapter`, other modules' `domain`/`app` |
| `adapter`  | own `app`, own `domain`, infra libs (pgx, chi) | other modules' internals |
| `public`   | stdlib + own DTOs only                        | own `domain` (DTOs are standalone) |

The domain layer is **pure**: no SQL, no HTTP, no framework types. Persistence is expressed as
**repository interfaces defined in `domain`** and implemented in `adapter/repository` (Ports &
Adapters / Hexagonal). Use cases in `app` depend on those interfaces, never on concrete repos.

---

## 4. DDD Tactical Building Blocks

Apply these deliberately — they are how we avoid an anemic model (behavior lives with data).

- **Entity** — identity + lifecycle (e.g. `Reservation`, `User`, `Campsite`). Mutations happen
  through methods that enforce invariants, not by external field-setting.
- **Value Object (VO)** — immutable, compared by value, self-validating in its constructor.
  Examples: `CPF`, `CNPJ`, `Email`, `Base62Code`, `Period` (entry/exit dates), `Capacity`,
  `OverbookingPercent`. A VO that can't be invalid removes a whole class of guard code downstream.
- **Aggregate** — consistency boundary with a single root. `Reservation` is the primary
  aggregate root; `Participant` and `EmergencyContact` are inside it and only mutated through
  the root. One transaction = one aggregate. Reference other aggregates **by ID**, not by pointer.
- **Domain Service** — domain logic that doesn't belong to a single entity (e.g. overlap
  checking across reservations, occupancy computation). Stateless, lives in `domain`.
- **Domain Event** — a fact that happened (`ReservationConfirmed`, `ReservationCancelled`,
  `ReservationExpired`). Used for cross-module reactions (e.g. availability releasing vacancies)
  without importing internals. For MVP, events may be delivered in-process synchronously through
  the composition root; keep the event type in `public/`.
- **Repository** — collection-like interface for an aggregate, defined in `domain`, implemented
  in `adapter`. One repository per aggregate root.
- **Factory** — constructors/builders that produce valid aggregates (e.g. `NewReservation(...)`
  returning `(*Reservation, error)`), centralizing invariant checks.

**Ubiquitous language:** keep PT-BR domain terms in type/method names where they are the real
term — `Acampamento`, `Diaria`, `Reserva`? Prefer English type names with PT-BR concepts noted
(`Reservation` = Reserva, `Campsite` = Acampamento, `Daily`/`Night` = Diária, `Responsible` =
Responsável, `Participant` = Participante, `Porteiro` gatekeeper, `NoShow`, `WalkIn`). Be
consistent across modules; the design doc names the exact types.

---

## 5. Cross-Cutting / Shared Kernel

`internal/platform/` (technical) and `internal/shared/` (a *small* shared kernel of domain
primitives reused across contexts). Keep the shared kernel minimal (YAGNI).

| Package | Contents | Notes |
| ------- | -------- | ----- |
| `internal/platform/postgres` | pgx pool construction, tx helpers | infra |
| `internal/platform/config`   | env loading, typed runtime config | infra (M0) |
| `internal/platform/httpx`    | chi router setup, middleware (logging, recovery, session) | infra |
| `internal/platform/web`      | `html/template` base layout, htmx helpers, static assets | infra |
| `internal/platform/log`      | structured logging (`slog`) | infra |
| `internal/shared/document`   | `CPF`, `CNPJ` value objects (validation) | domain primitive reused by identity/reservations/checkin |
| `internal/shared/id`         | Base62 code generation, UUIDs | domain primitive |

Shared-kernel VOs are the **one** exception to "no shared domain": they are stable, ubiquitous
primitives, not context-specific behavior. Do not put business rules there.

---

## 6. Composition Root & Request Flow

```
cmd/server/main.go
  └─ internal/platform/bootstrap
       ├─ build platform (config, pgx pool, logger, router)
       ├─ for each module: construct repositories(adapter) → use cases(app) → handlers(adapter)
       ├─ wire cross-module ports (module A's public impl → module B's use-case dependency)
       └─ mount module HTTP routes onto the chi router
```

**HTTP request flow (htmx):** chi route → module handler (`adapter/http`) → parse/validate input
into a command DTO → call use case (`app`) → use case loads aggregate via repository, invokes
domain behavior, persists in a transaction → returns result DTO → handler renders an
`html/template` fragment (htmx swap) or full page.

---

## 7. Concurrency & Consistency (availability / reservations)

The highest-risk area. Rules that the relevant designs MUST honor:

- Last-vacancy races resolve to **exactly one** winner. Use Postgres `SELECT … FOR UPDATE`
  row locks on the occupancy rows for the (campsite, Diária) set, or advisory locks, inside the
  reservation-creation transaction. Never compute availability then insert without holding the lock.
- Occupancy is per person, per Diária (09:00→09:00, checkout day **not** counted), per campsite.
- Effective capacity = `capacity + overbooking%`, evaluated per Diária.
- Overlap prevention (same CPF across any campsite) enforced with an **exclusion constraint +
  range type** in-schema, in addition to app-level checks — the DB is the final guarantee.
- Temporary holds (status `Pendente`) block vacancies and auto-expire (default 10 min) →
  `Expirada` → vacancies released. Walk-ins skip this state (born `Check-in realizado`).

---

## 8. Reservation State Machine (shared vocabulary)

```
                        ┌──────────► Expirada        (hold TTL elapsed)
Pendente ───────────────┼──────────► Cancelada       (remote/on-site cancel)
   │                    └──────────► No-show         (group absent)
   └──► Check-in realizado ────────► Finalizada
Walk-in: (created) ──► Check-in realizado ──► Finalizada   (skips Pendente)
```

Transitions are enforced by the `Reservation` aggregate (guard methods), never by setting the
status field directly from a handler.
