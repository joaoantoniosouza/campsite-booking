# Campsite Booking — National Parks Reservation System

**Vision:** A platform to manage campsite reservations across national-park campgrounds — self-service booking for visitors, on-site operations for gatekeepers, and full administrative control — with concurrency-safe, per-night capacity and overbooking enforcement.
**For:** Park visitors (Pessoa Física), companies (Pessoa Jurídica), gatekeepers (Porteiro), and administrators.
**Solves:** Manual, error-prone reservation handling and invalid overbooking. Replaces it with an automated, self-service booking flow that needs no manual park approval — a confirmed reservation is already valid, subject only to real availability.

## Goals

- **No invalid overbooking:** never exceed effective capacity (`capacity + overbooking %`) per campsite per night — 0 oversell incidents under concurrent load (last-vacancy races resolved correctly).
- **Fast availability:** availability query response time < 200 ms (NFR).
- **Fully automated booking:** reservation created + confirmed by the system is valid, with no manual approval step.
- **Multi-campsite parks:** independent occupancy control per campsite + correct aggregated park totals.
- **Traceable operations:** every reservation state transition (create, hold, expire, cancel, check-in, transfer, no-show) is recorded.

## Tech Stack

**Core:**

- Language: Go (1.22+)
- Web/router: `net/http` + `chi` (lightweight, stdlib-compatible)
- Frontend: htmx + Go server-rendered templates (`html/template`) — no separate SPA
- Database: PostgreSQL (16+)
- Data access: `pgx` driver + `sqlc` (type-safe SQL, so locking/overlap SQL is written directly)
- Migrations: `golang-migrate` (or `goose`)

**Key dependencies / patterns:**

- Auth: cookie-based sessions, `bcrypt` password hashing
- Concurrency: Postgres row-level locks (`SELECT … FOR UPDATE`) / advisory locks for last-vacancy; exclusion constraints + range types for period-overlap rules
- Testing: stdlib `testing` + `testcontainers-go` (integration tests against real Postgres)
- Architecture: **modular monolith** — single deployable, clear internal module boundaries (identity, campsites, availability, reservations, check-in, admin, config)

## Scope

**v1 (MVP) includes:**

- User registration PF & PJ; login by email + password
- Campsite registration (Acampamento) + system configuration
- Availability calendar per night, per campsite (overbooking-aware)
- Reservation by period, by CPF or CNPJ; temporary (Pendente) hold with expiration
- Unique Base62 reservation code
- Query by code, by CPF (responsible or any participant), and authenticated user history
- Limited participant changes (swap limits) + responsibility transfer
- Cancellation: remote (by responsible) and on-site (by Porteiro)
- Per-participant check-in; Porteiro walk-in reservation
- Admin panel + Porteiro panel + system configuration

**Explicitly out of scope (post-MVP):**

- Payments
- Waitlist (lista de espera)
- Notifications
- Detailed audit trail
- External integrations
- Advanced reports

## Constraints

- **Legal:** LGPD compliance (personal data: CPF, CNPJ, contacts).
- **Technical:** transactional consistency + concurrency control are mandatory; availability query < 200 ms; booking window limited to 2 months ahead of the current month (next month auto-released monthly).
- **Domain language:** PT-BR ubiquitous language preserved (Acampamento, Diária, Responsável, Participante, Porteiro, Reserva Walk-in, No-show) even though spec prose is in English.
