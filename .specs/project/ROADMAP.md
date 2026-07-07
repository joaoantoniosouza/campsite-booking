# Roadmap

**Current Milestone:** M0 — Foundation & Infrastructure
**Status:** Specified — M0 + M1 + M2 feature specs authored (spec + design + tasks); implementation not started.

Milestones are dependency-ordered: each is a shippable increment. RF IDs trace back to
[PRD.md](PRD.md) §11 (Requisitos Funcionais) — the detailed source-of-truth requirements.

**Feature status legend:** `PLANNED` (not yet specified) → `SPECIFIED` (spec + design + tasks
complete) → `IN PROGRESS` → `DONE`. M0/M1/M2 features below are `SPECIFIED` except where noted.

---

## M0 — Foundation & Infrastructure

**Goal:** A runnable modular-monolith skeleton: HTTP server, base htmx layout, Postgres wired with migrations, config loading, and a test harness. Nothing user-facing, but everything below builds on it.
**Target:** Skeleton boots, migrates a clean DB, serves a base page, and CI runs tests against a Postgres testcontainer.

### Features

**Project skeleton & module boundaries** — SPECIFIED

- Go modules layout for a modular monolith (identity, campsites, availability, reservations, check-in, admin, config)
- `chi` router, middleware (logging, recovery, session), graceful shutdown
- Base `html/template` layout + htmx wiring + static assets

**Data & migration layer** — SPECIFIED

- `pgx` pool + `sqlc` config; `golang-migrate` setup
- Initial schema baseline + migration workflow
- `testcontainers-go` integration-test harness

**Config & runtime** — SPECIFIED

- Env-based configuration, `.env` for dev
- Health check + structured logging

---

## M1 — Identity & Setup

**Goal:** Users can register and log in; admins can register campsites and set system rules. Establishes the data every reservation depends on.
**Target:** PF/PJ can register and authenticate; an admin can create an Acampamento and edit configuration values.

### Features

**User & company registration** — SPECIFIED *(RF02, RF03)*

- PF registration (optional): name, CPF, birth date, email, phone, password
- PJ registration (required): company (razão social, CNPJ, email, password) + legal responsible
- CPF/CNPJ validation; unique constraints; LGPD-aware storage

**Authentication** — SPECIFIED

- Login by email + password (bcrypt), cookie-based sessions
- Role model: visitor, Porteiro, Administrator
- Logout, session expiry

**Campsite management** — SPECIFIED *(RF01)*

- CRUD for Acampamento: name, location, description, max capacity, overbooking %, status, notes
- Effective capacity = capacity + overbooking %

**System configuration** — SPECIFIED *(RF11)*

- Config store + defaults: per-reservation limit (5 PF / 15 PJ), booking window, cancellation deadline (24h), change deadline (24h), temporary-hold TTL (10 min), swap rules, overbooking
- Values consumed by reservation rules downstream

---

## M2 — Availability & Reservation Core

**Goal:** The heart of the system — compute availability and create a valid reservation without ever overselling. Highest-risk milestone (concurrency).
**Target:** A visitor can see availability and complete a booking; concurrent bookings for the last vacancy resolve to exactly one winner.

### Features

**Availability engine** — SPECIFIED *(RF08, RF09)*

- Occupancy per person, per Diária, per campsite (09:00→09:00; checkout day not counted)
- Effective-capacity check including overbooking; booking window enforcement (2 months ahead)
- Availability calendar view; < 200 ms query target

**Reservation creation** — SPECIFIED *(RF04)*

- Temporary hold: status Pendente, vacancies blocked, configurable TTL (default 10 min) → auto-expire → release
- Participants (CPF + name required; responsible included), emergency contact (name, phone, kinship), limits by PF/PJ
- Overlap prevention: same CPF (responsible or participant) cannot hold two overlapping reservations across **any** campsite in the park
- Unique Base62 code generation
- **Concurrency control:** last-vacancy race safety (row locks / advisory locks + transactional occupancy)

---

## M3 — Reservation Lifecycle & Self-Service

**Goal:** Reservation holders can find, adjust, hand off, and cancel their reservations remotely.
**Target:** A holder can look up a reservation three ways, swap participants within limits, transfer responsibility, and cancel within policy.

### Features

**Reservation lookup** — PLANNED *(RF07)*

- Query by Base62 code, by responsible CPF, by any participant CPF, and authenticated user history

**Participant changes** — PLANNED *(RF06)*

- Allowed until change deadline (default 24h); swap limits (1/2/3 by group size); removals allowed; adds only within swap budget

**Responsibility transfer** — PLANNED *(RF13)*

- Transfer only to an existing participant; same code/history retained; old responsible loses management rights unless still a participant

**Remote cancellation** — PLANNED *(RF05)*

- By responsible, up to deadline (default 24h before entry) → status Cancelada, vacancies released immediately

---

## M4 — On-Site Operations (Porteiro)

**Goal:** The gatekeeper can run the gate: check people in, create walk-ins, cancel on-site, and mark no-shows.
**Target:** A Porteiro can locate a reservation, register presence per participant, create a walk-in that is already checked in, and cancel on-site after identity confirmation.

### Features

**Porteiro panel & check-in** — PLANNED *(RF10)*

- Locate reservation; responsible must be present; per-participant presence; absent participants stay recorded
- Divergence rule: if participant divergence exceeds the allowed swap limit, access is denied
- State flow: Pendente → Check-in realizado → Finalizada; alt: No-show / Expirada

**Walk-in reservation** — PLANNED *(RF12)*

- Created by Porteiro for visitors without a prior reservation; requires available vacancies (effective capacity)
- Skips the Pendente/expiration state; born as Check-in realizado
- Same participant/overlap/emergency-contact rules apply

**On-site cancellation** — PLANNED *(RF05)*

- By Porteiro with responsible present; requires CPF **or** reservation-code confirmation; no 24h deadline; vacancies released immediately

---

## M5 — Admin Panel & Dashboard

**Goal:** Administrators get full visibility and management surfaces.
**Target:** Dashboard shows occupancy (per campsite + aggregated) and reservation/cancellation/no-show metrics; management screens cover all entities.

### Features

**Admin dashboard** — PLANNED

- Occupancy per campsite and aggregated park total; reservations, cancellations, no-shows; campsites overview

**Management surfaces** — PLANNED

- Manage campsites, reservations, users, companies, Porteiros, and configuration (UI over M1 config store)

---

## Future Considerations (Post-MVP)

- Payments
- Waitlist (lista de espera)
- Notifications
- Detailed audit trail
- Advanced reports
- External integrations
