# Campsite Management Specification

**Milestone:** M1 — Identity & Setup
**Module:** `campsites` (`internal/modules/campsites/{domain,app,adapter,public}`)
**Requirement prefix:** `CAMP`
**Implements:** RF01 (Cadastrar acampamentos), RF09 (per-campsite overbooking %). PRD §6 (Acampamentos), §9 (management surface), §13 (capacity/overbooking change edge cases).

## Problem Statement

Every reservation and availability computation depends on knowing *where* people can camp and *how many* fit. An administrator needs to register and maintain each **Acampamento** (name, localização, descrição, capacidade máxima, percentual de overbooking, status, observações) and the system must derive each site's **effective capacity** (`capacidade + percentual de overbooking`). A parque may hold several independent acampamentos; there is no stored "park total" — it is always the aggregate sum of effective capacities. This feature owns that catalogue and exposes the minimal read surface downstream modules (availability M2, admin M5) need.

## Goals

- [ ] An Administrator can create, view, list, update, and deactivate an Acampamento through admin htmx screens.
- [ ] **Effective capacity is a domain calculation** on the `Campsite` aggregate/VO — never an anemic external service.
- [ ] Invariants are unrepresentable-if-invalid: capacity ≥ 0, overbooking % ≥ 0, status ∈ {Ativo, Inativo}, name non-empty — enforced in constructors.
- [ ] A `campsites/public` port exposes effective capacity + active-campsite list as flat DTOs, so no consumer ever touches the domain.
- [ ] Management is Administrator-only, authorized via `identity/public`.

## Out of Scope

Explicitly excluded — prevents scope creep.

| Feature | Reason |
| ------- | ------ |
| Availability / occupancy computation | Owned by `availability` (M2, RF08). This feature only *supplies* effective capacity + status. |
| Retroactive invalidation of confirmed reservations after a capacity/overbooking change | PRD §13 seam: availability recomputes forward; existing confirmed reservations are never voided here. |
| Park-total capacity as stored config | PRD §6: the park total is always an aggregate (sum of effective capacities), not a persisted value. Aggregation is the admin dashboard's job (M5). |
| System-wide default overbooking % / other config values | Owned by `system-configuration` (CFG, RF11). Here overbooking % is a *per-campsite* field only. |
| Campsite-name uniqueness, geo/coordinates, photos, pricing | Not required by PRD §6; YAGNI for MVP. |
| Reactivation UI as a distinct flow | Deactivate is the named MVP transition; symmetric reactivation is noted in Open Decisions. |
| Optimistic-concurrency (version) on admin edits | Single-admin surface; last-write-wins. Not required by PRD (YAGNI). |

---

## User Stories

### P1: Administrator registers an Acampamento ⭐ MVP

**User Story**: As an Administrator, I want to register an Acampamento with its name, localização, descrição, capacidade máxima, percentual de overbooking and observações so that visitors can later book it.

**Why P1**: RF01 is the root data every reservation depends on; nothing downstream exists without a campsite.

**Acceptance Criteria**:

1. WHEN an Administrator submits a create form with name, localização, descrição, capacidade, overbooking % and observações THEN the system SHALL persist a new Acampamento with a unique ID and status defaulting to **Ativo**, and render it in the list.
2. WHEN the submitted name is empty/blank, capacidade < 0, or overbooking % < 0 THEN the system SHALL reject the request with a validation message and SHALL NOT persist anything.
3. WHEN capacidade and overbooking % are valid THEN the created Acampamento SHALL report the correct effective capacity (see P1: Effective capacity).

**Independent Test**: POST the create form with valid data → row persisted, status Ativo, appears in `GET` list; POST with blank name → 422 with error fragment, no row.

---

### P1: Effective capacity is a domain calculation ⭐ MVP

**User Story**: As the system, I want each Acampamento to compute its own effective capacity so that availability and overbooking checks have one authoritative source.

**Why P1**: PRD §6 / RF09 — effective capacity is the number every occupancy check compares against; it must live on the aggregate, not be recomputed ad-hoc by callers.

**Acceptance Criteria**:

1. WHEN a `Campsite` has capacidade `C` and overbooking `P%` THEN `EffectiveCapacity()` SHALL return `C + floor(C × P / 100)`.
2. WHEN overbooking % is 0 THEN `EffectiveCapacity()` SHALL equal capacidade.
3. WHEN capacidade is 0 THEN `EffectiveCapacity()` SHALL be 0 regardless of overbooking %.

**Independent Test**: Table-driven unit test on the aggregate: (100, 10%)→110, (50, 0%)→50, (0, 25%)→0, (33, 10%)→36 (floor of 3.3).

---

### P1: List and view campsites (admin) ⭐ MVP

**User Story**: As an Administrator, I want to list all acampamentos and open one so that I can review and manage the catalogue.

**Why P1**: PRD §9 management surface; required to reach the update/deactivate flows.

**Acceptance Criteria**:

1. WHEN an Administrator opens the campsites screen THEN the system SHALL list every Acampamento with name, localização, capacidade, overbooking %, effective capacity and status.
2. WHEN an Administrator requests a campsite by ID THEN the system SHALL return its full detail.
3. WHEN the requested ID does not exist THEN the system SHALL respond not-found (404) without a stack trace.

**Independent Test**: Seed two campsites → list renders both with correct effective capacities; GET unknown ID → 404.

---

### P1: Update an Acampamento ⭐ MVP

**User Story**: As an Administrator, I want to edit an Acampamento's mutable fields (including capacidade and overbooking %) so that the catalogue reflects reality.

**Why P1**: RF01; capacity/overbooking values change over time and drive availability.

**Acceptance Criteria**:

1. WHEN an Administrator submits an edit with valid values THEN the system SHALL update name, localização, descrição, capacidade, overbooking % and observações, re-enforcing all invariants.
2. WHEN the edit violates an invariant (blank name, capacidade < 0, overbooking % < 0) THEN the system SHALL reject it and leave the stored Acampamento unchanged.
3. WHEN capacidade or overbooking % changes THEN the new effective capacity SHALL apply to **future** availability computations only; already-confirmed reservations SHALL NOT be retroactively invalidated by this feature (PRD §13 seam).

**Independent Test**: Update a campsite's capacity 50→80 → detail shows 80 and new effective capacity; downstream `public.Provider.EffectiveCapacity` returns the new value on next call; a pre-existing confirmed reservation record is untouched.

---

### P1: Deactivate an Acampamento ⭐ MVP

**User Story**: As an Administrator, I want to deactivate an Acampamento so that it stops accepting new bookings while its history is preserved.

**Why P1**: PRD §6 status Ativo/Inativo; the lever that removes a site from the bookable set.

**Acceptance Criteria**:

1. WHEN an Administrator deactivates an Acampamento THEN its status SHALL become **Inativo** and it SHALL remain persisted (not deleted).
2. WHEN an Acampamento is Inativo THEN `public.Provider.ActiveCampsites` SHALL exclude it, so downstream availability/booking treats it as unavailable for new reservations.
3. WHEN an Administrator deactivates an already-Inativo Acampamento THEN the operation SHALL be idempotent (remains Inativo, no error).

**Independent Test**: Deactivate an Ativo campsite → status Inativo, absent from `ActiveCampsites`; deactivate again → still Inativo, 200.

---

### P1: Public interface for downstream modules ⭐ MVP

**User Story**: As the availability (M2) and admin (M5) modules, I want a stable `campsites/public` port returning flat DTOs so that I can read effective capacity and the active-campsite list without importing campsites internals.

**Why P1**: Module-boundary rule (ARCHITECTURE §2); M2 cannot compute availability without effective capacity, and it must not reach into `campsites/domain`.

**Acceptance Criteria**:

1. WHEN a consumer calls `Provider.EffectiveCapacity(ctx, campsiteID)` for an existing campsite THEN the system SHALL return its effective capacity as an `int`.
2. WHEN the campsiteID is unknown THEN `EffectiveCapacity` SHALL return `ErrNotFound` (sentinel in `public`), not a domain error.
3. WHEN a consumer calls `Provider.ActiveCampsites(ctx)` THEN the system SHALL return only Ativo campsites as flat `CampsiteDTO` values (primitives only) — **no** domain entity or value object crosses the boundary.

**Independent Test**: Through the `public.Provider` only (no domain import): create an Ativo + an Inativo campsite via the module; `ActiveCampsites` returns one DTO; `EffectiveCapacity(id)` returns the computed int; unknown ID → `ErrNotFound`.

---

### P2: Administrator-only authorization

**User Story**: As the system, I want every campsite-management operation restricted to Administrators so that non-admins cannot alter the catalogue.

**Why P2**: PRD §4/§9 — management is admin-only. Separated from P1 because it layers on top of the CRUD once `identity/public` is available (sibling M1 feature).

**Acceptance Criteria**:

1. WHEN a request to any campsite-management route lacks an authenticated Administrator principal THEN the system SHALL deny it (403 / redirect to login) and SHALL NOT execute the use case.
2. WHEN the principal is an authenticated Administrator THEN the request SHALL proceed.

**Independent Test**: Hit a management route with a non-admin/absent principal → denied, use case not invoked; with an Administrator principal → 200.

---

## Edge Cases

- WHEN capacidade × overbooking % does not divide evenly (e.g. 33 × 10%) THEN `EffectiveCapacity` SHALL **floor** the added allowance (33 → 36, never 37) so the system never oversells beyond the exact overbooking share.
- WHEN overbooking % is 0 or capacidade is 0 THEN effective capacity SHALL degrade gracefully (= capacidade, and 0, respectively).
- WHEN an Administrator updates capacidade/overbooking % of a campsite that has active occupancy THEN this feature SHALL only change the campsite record; recomputation and any over-capacity situation is availability's concern (M2) and confirmed reservations are never voided here (PRD §13).
- WHEN a get/update/deactivate targets a non-existent ID THEN the system SHALL return not-found, not a 500.
- WHEN two admins edit the same campsite concurrently THEN last-write-wins (no version guard; documented Open Decision) — no data corruption, since each write is a single-aggregate transaction.
- WHEN the create/edit form omits localização, descrição or observações THEN the system SHALL accept them as empty (only name, capacidade, overbooking % are constrained).

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| -------------- | ----- | ----- | ------ |
| CAMP-01 | P1: Register (create + defaults + persist) | Design | In Tasks |
| CAMP-02 | P1: Register (validation on create) | Design | In Tasks |
| CAMP-03 | P1: Effective capacity (`C + floor(C×P/100)`, on aggregate) | Design | In Tasks |
| CAMP-04 | P1: Effective capacity (boundaries: P=0, C=0) | Design | In Tasks |
| CAMP-05 | P1: List/view (list with effective capacity + status) | Design | In Tasks |
| CAMP-06 | P1: List/view (get by ID; 404 on unknown) | Design | In Tasks |
| CAMP-07 | P1: Update (mutable fields, invariants re-enforced) | Design | In Tasks |
| CAMP-08 | P1: Update (capacity/overbooking change = forward-only; no retro invalidation — PRD §13 seam) | Design | In Tasks |
| CAMP-09 | P1: Deactivate (status → Inativo, preserved) | Design | In Tasks |
| CAMP-10 | P1: Deactivate (excluded from ActiveCampsites; idempotent) | Design | In Tasks |
| CAMP-11 | P1: Public (EffectiveCapacity; ErrNotFound on unknown) | Design | In Tasks |
| CAMP-12 | P1: Public (ActiveCampsites flat DTOs; domain never leaks) | Design | In Tasks |
| CAMP-13 | P2: Authorization (Administrator-only; deny otherwise) | Design | In Tasks |

**ID format:** `CAMP-NN`. **Status values:** Pending → In Design → In Tasks → Implementing → Verified.
**Coverage:** 13 total, 13 mapped to tasks (see tasks.md), 0 unmapped.

### PRD / RF Traceability

- **RF01** (Cadastrar acampamentos) — the whole CRUD (CAMP-01, 02, 05–10).
- **RF09** (Controlar overbooking) — the *per-campsite* overbooking % field and its role in effective capacity (CAMP-03, 04, 11). System-wide overbooking defaults live in CFG (RF11); the availability *enforcement* is AVL (RF08).
- **PRD §6** — Acampamento fields, `capacidade efetiva = capacidade + percentual de overbooking`, multiple independent acampamentos, park total as aggregate (CAMP-01…12).
- **PRD §9** — management surface for Acampamentos (CAMP-05, 07, 09, 13).
- **PRD §13** — "Mudança de capacidade" / "Mudança do percentual de overbooking" edge cases (CAMP-08 seam).

---

## Success Criteria

- [ ] `go test ./internal/modules/campsites/...` passes (domain + app unit) — effective-capacity table cases green.
- [ ] `go test -tags=integration ./internal/modules/campsites/...` passes — repository, public Provider, and admin htmx handlers against a real Postgres.
- [ ] An Administrator can complete create → list → edit → deactivate end-to-end through htmx screens.
- [ ] `campsites/public.Provider` returns effective capacity + active DTOs with no domain type crossing the boundary (verified by a public-only integration test).
- [ ] A non-admin cannot reach any management route.

---

## Open Decisions

- **Effective-capacity formula = `capacidade + floor(capacidade × overbooking% / 100)`.** PRD §6's shorthand "capacidade + percentual de overbooking" is read as the standard overbooking semantics (allowance = a percentage *of* capacity), floored so the system never oversells a fractional vacancy. Overbooking % is stored as a whole integer percent (no fractional %). Revisit if the business wants an absolute additive allowance or fractional percents.
- **Reactivation:** MVP ships only `DeactivateCampsite` (the named transition). A symmetric `ActivateCampsite` (Inativo → Ativo) would mirror it exactly and can be added trivially; excluded now to honor scope. Status is still fully represented (Ativo/Inativo) so no data change is needed to add it.
- **Authorization source:** management routes consume `identity/public` (Administrator role) — a *provider-owned* port, since role semantics belong to identity. The exact principal/role accessor is owned by the sibling `authentication` feature (in progress); if its shape is not final at implementation time, campsites declares a minimal consumer-owned `AdminGuard` adapted from `identity/public` at the composition root. Either way no identity internals are imported.
- **No name uniqueness / no optimistic concurrency** for MVP (YAGNI); admin edits are last-write-wins within single-aggregate transactions.
