# Reservation Creation Specification

**Milestone:** M2 — Availability & Reservation Core
**Module:** `reservations` (`internal/modules/reservations/{domain,app,adapter}`)
**Requirement prefix:** `RSV`
**Implements:** RF04 (Criar reservas). PRD §6 (Reserva, Reserva temporária, Participantes, Limite de participantes, Sobreposição, Contato de emergência, Ocupação — context), §8 (state flow: Pendente), §11 (RF04), §13 (duas reservas simultâneas para a última vaga / expiração de reserva temporária / sobreposição).

## Problem Statement

A visitor (Pessoa Física, with or without cadastro) or a company (Pessoa Jurídica) needs to book an **Acampamento** for a period without any manual park approval — a created hold is already valid, subject only to real availability. Creation must place a **temporary hold** (status **Pendente**) that blocks vacancies for a configurable window (default 10 min), auto-expiring to **Expirada** and releasing the vacancies if not confirmed. The system must never oversell the last vacancy under concurrent load and must never let one CPF hold two overlapping reservations anywhere in the parque. This feature owns reservation **creation + auto-expiry only**; occupancy math, lookup, changes, transfer, cancellation and check-in belong to sibling features.

## Goals

- [ ] A visitor can create a **Reserva** for a campsite + período and receive a unique **Base62** code and a **Pendente** hold with a visible expiry.
- [ ] The hold is created **atomically** with the availability vacancy increment (one transaction), so a last-vacancy race resolves to **exactly one** winner (0 oversell).
- [ ] A **Reservation** aggregate enforces every creation invariant (CPF present, Responsável ∈ participants, participant limit, ≥1 emergency contact) — a rich model, never an anemic struct + service.
- [ ] The same CPF (Responsável or any Participante) cannot hold two overlapping reservations across **any** campsite — guaranteed by a Postgres exclusion constraint plus a fast app-level pre-check.
- [ ] An expiry **sweeper** transitions overdue Pendente holds to Expirada and releases their vacancies.
- [ ] Cross-module capability (availability, campsites, config, identity) is consumed **only** through public ports / consumer-owned ports — no internals imported.

## Out of Scope

Explicitly excluded — prevents scope creep. Each is a sibling feature or post-MVP.

| Feature | Reason |
| ------- | ------ |
| Occupancy / effective-capacity / booking-window computation | Owned by `availability` (M2, AVL, RF08). This feature *consumes* the `availability/public.Reserver` seam; it does not compute vacancies. |
| Reservation lookup (by code / CPF / user history) | `reservation-lookup` (M3, LKP, RF07). |
| Participant changes / swaps | `participant-changes` (M3, PRT, RF06). |
| Responsibility transfer | `responsibility-transfer` (M3, XFR, RF13). |
| Remote cancellation | `remote-cancellation` (M3, CNL, RF05). |
| On-site cancellation, per-participant check-in, walk-in | M4 (OSC / CHK / WLK). Walk-in skips the Pendente state (born Check-in realizado) — not created here. |
| Reservation states beyond Pendente/Expirada (Cancelada, No-show, Check-in realizado, Finalizada) | Introduced by their owning M3/M4 features; this feature only creates Pendente and expires it to Expirada. |
| Monthly booking-window liberation (next month auto-release) | Availability owns the window; here we only surface `ErrOutsideBookingWindow`. |
| PF/PJ registration & authentication | `user-company-registration` (REG) / `authentication` (AUTH). We only *read* the authenticated actor. |
| Payments, waitlist, notifications, audit trail | Post-MVP (PROJECT.md §Scope). |
| A `reservations/public` surface (lookup/cancel/checkin ports) | YAGNI — nothing in this feature consumes it; M3/M4 add it when they need it (ARCHITECTURE §2). |

---

## User Stories

### P1: Visitor creates a temporary hold (Pendente) ⭐ MVP

**User Story**: As a visitor, I want to book a campsite for a período and get a confirmation code so that my vacancies are held while I finish, without waiting for park approval.

**Why P1**: RF04 is the core of the whole system; every downstream flow (lookup, cancel, check-in) presupposes a created hold.

**Acceptance Criteria**:

1. WHEN a visitor submits a valid booking (campsite, entry, exit, participants incl. Responsável, ≥1 emergency contact) THEN the system SHALL create a Reserva with status **Pendente**, a unique Base62 code, and `expiresAt = now + TTL`, call `availability.Reserve` to block the vacancies, persist participants + emergency contact(s), and render a confirmation showing the code and expiry — all in one transaction.
2. WHEN the target campsite does not exist or is **Inativo** THEN the system SHALL reject the request (404 / 422), persist nothing, and block no vacancies.
3. WHEN the período is invalid (`Exit ≤ Entry`) or falls outside the booking window THEN the system SHALL reject it (422) with a clear message (window rejection surfaced from `availability.ErrOutsideBookingWindow`) and persist nothing.

**Independent Test**: POST a valid booking against an Ativo campsite with free vacancies → row persisted with status Pendente, code returned, `availability.Reserve` invoked with `headcount = len(participants)`; POST against an Inativo/unknown campsite → rejected, no row, `Reserve` not called; POST with `Exit ≤ Entry` → 422.

---

### P1: Participants require CPF and respect the per-reservation limit ⭐ MVP

**User Story**: As the system, I want every Participante to carry a CPF and the Responsável to be one of them, within the allowed group size, so that occupancy and overlap are attributable and bounded.

**Why P1**: PRD §6 (Participantes, Limite de participantes) — CPF is the identity every occupancy/overlap rule keys on; the limit bounds group size.

**Acceptance Criteria**:

1. WHEN a booking is submitted THEN the system SHALL require each Participante to have a non-blank name and a valid CPF, SHALL include the Responsável in the participant list, and SHALL require at least one participant; a violation SHALL reject the booking and persist nothing.
2. WHEN the participant count exceeds the applicable limit (default **5** for PF, **15** for PJ) THEN the system SHALL reject the booking with a limit error and persist nothing.
3. WHEN the same CPF appears twice in one booking THEN the system SHALL reject it (a person cannot be listed twice in one Reserva).

**Independent Test**: Table cases on the aggregate factory — Responsável absent from participants → error; 6 participants for a PF actor → limit error; duplicate CPF → error; 1 valid Responsável-only booking → ok.

---

### P1: Emergency contact is mandatory ⭐ MVP

**User Story**: As park operations, I want at least one external emergency contact per reservation so that the group is reachable 24h in an incident.

**Why P1**: PRD §6 (Contato de emergência) — a hard reservation invariant.

**Acceptance Criteria**:

1. WHEN a booking is submitted with zero emergency contacts THEN the system SHALL reject it and persist nothing.
2. WHEN an emergency contact is provided THEN it SHALL carry a non-blank name, a telefone, and a grau de parentesco; a contact missing any of these SHALL reject the booking.

**Independent Test**: Factory cases — zero contacts → `ErrNoEmergencyContact`; a contact missing telefone → `ErrInvalidPhone`/validation error; one complete contact → ok.

---

### P1: Overlap prevention across the whole parque ⭐ MVP

**User Story**: As the system, I want to forbid a CPF from holding two overlapping reservations in any campsite so that one person cannot double-occupy.

**Why P1**: PRD §6 (Sobreposição) + §13 — a core correctness rule; the DB is the final guarantee (ARCHITECTURE §7).

**Acceptance Criteria**:

1. WHEN a booking includes a CPF (Responsável or any Participante) that is already in an **active** reservation whose período overlaps the requested one — in **any** campsite — THEN the system SHALL reject the booking with `ErrOverlappingReservation` and persist nothing.
2. WHEN two reservations are merely adjacent (one's checkout day equals the other's entry day) THEN they SHALL NOT be treated as overlapping (the checkout Diária is not occupied — `[entry, exit)` semantics).
3. WHEN the app-level pre-check misses a concurrent insert THEN a Postgres **exclusion constraint (range type + `btree_gist`)** SHALL reject the second write as the final guarantee.

**Independent Test**: Seed an active hold for CPF X over days 2–4 in campsite A; book CPF X over days 3–5 in campsite B → rejected; book CPF X over days 4–6 (adjacent to the 2–4 hold, exit=4) → allowed; concurrent insert path covered by P1: Concurrency.

---

### P1: Unique Base62 reservation code ⭐ MVP

**User Story**: As a visitor, I want a short unique code for my reservation so that I (and the Porteiro) can reference it.

**Why P1**: PRD §6 (Código Base62 único) — the reservation's public handle.

**Acceptance Criteria**:

1. WHEN a reservation is created THEN the system SHALL assign a code composed only of `A–Z`, `a–z`, `0–9`, unique across all reservations (enforced by a DB `UNIQUE` constraint).
2. WHEN a generated code collides with an existing one THEN the system SHALL regenerate and retry (bounded), so creation still succeeds.

**Independent Test**: Create two reservations → two distinct codes, both matching `^[0-9A-Za-z]+$`; a forced-collision generator (returns a taken code once) → creation still succeeds with a fresh code.

---

### P1: Concurrency & atomicity (last-vacancy + CPF overlap) ⭐ MVP

**User Story**: As park operations, I want concurrent bookings for the last vacancy (or the same CPF) to resolve to exactly one winner so that we never oversell or double-book.

**Why P1**: PROJECT.md goal (0 oversell) + PRD §13 (duas reservas simultâneas para a última vaga). The highest-risk area (ARCHITECTURE §7).

**Acceptance Criteria**:

1. WHEN two creates race for the last vacancy THEN exactly one SHALL commit and the other SHALL receive `ErrNoVacancy`; effective capacity SHALL never be exceeded (the vacancy increment happens under the availability row lock, inside the same transaction as the reservation insert).
2. WHEN any step of a create fails (no vacancy, overlap, persistence error) THEN the whole transaction SHALL roll back — no orphaned occupancy increment and no partially-written reservation.
3. WHEN two creates race with a shared CPF over overlapping períodos THEN exactly one SHALL commit and the other SHALL receive `ErrOverlappingReservation` (resolved by the exclusion constraint).

**Independent Test**: Integration — N goroutines create against a campsite with effective capacity 1 → exactly 1 success, N−1 `ErrNoVacancy`, occupancy row = capacity; force a Save failure after `Reserve` → occupancy unchanged after rollback; N goroutines create with the same CPF + overlapping período → exactly 1 success, N−1 `ErrOverlappingReservation`.

---

### P1: Hold auto-expiry sweeper ⭐ MVP

**User Story**: As park operations, I want unconfirmed holds to expire and free their vacancies automatically so that abandoned bookings don't block real demand.

**Why P1**: PRD §6 (Reserva temporária) + §13 (expiração de reserva temporária) — the temporary-hold rule is incomplete without release.

**Acceptance Criteria**:

1. WHEN a Pendente hold's `expiresAt` has passed THEN the sweeper SHALL transition it to **Expirada** and call `availability.Release` to free its vacancies, atomically (one transaction per reservation), and free the CPFs for rebooking.
2. WHEN a reservation is not Pendente (or not yet expired) THEN the sweeper SHALL leave it untouched; expiry SHALL be idempotent (a hold expired once is never double-released).
3. WHEN the sweeper runs concurrently with another transition on the same hold THEN row locking (`FOR UPDATE`) SHALL ensure the hold is transitioned at most once.

**Independent Test**: Integration — create a hold with TTL, advance the injected clock past `expiresAt`, run the sweeper → status Expirada, `Release` called with the right headcount/período, the CPF is now re-bookable; run the sweeper again → no-op; a non-expired / non-Pendente hold is skipped.

---

### P2: Configurable, actor-aware policy (TTL + PF/PJ limit)

**User Story**: As an Administrator, I want the hold TTL and the PF/PJ participant limits to come from system configuration and the correct limit to be chosen by who is booking, so that policy is tunable without code changes.

**Why P2**: Layers cross-module policy (`config/public`, `identity/public`) onto the core P1 flow — separated because it depends on sibling modules being wired, exactly as campsite-management separated authorization. P1 enforces the limit given a number; P2 sources the number and selects PF-vs-PJ.

**Acceptance Criteria**:

1. WHEN a create runs THEN the hold TTL SHALL be read from `config/public` (default **10 min**) rather than hard-coded.
2. WHEN the actor is a company (PJ, authenticated) THEN the applicable participant limit SHALL be the PJ limit (default **15**); WHEN the actor is a person (PF, registered **or anonymous**) THEN it SHALL be the PF limit (default **5**) — both sourced from `config/public`, with the actor type resolved from `identity/public`.

**Independent Test**: With a fake config returning TTL=1 min and limits (PF=2, PJ=4) and a fake actor resolver: an anonymous booking with 3 participants → rejected (PF limit 2); a PJ booking with 3 participants → ok; the created hold's `expiresAt` reflects the 1-min TTL.

---

## Edge Cases

- WHEN a create succeeds at `availability.Reserve` but the reservation `Save` fails THEN the transaction SHALL roll back so occupancy is not left incremented (no orphaned vacancy). (RSV-11)
- WHEN the checkout day of one reservation equals the entry day of another for the same CPF THEN they SHALL be allowed (adjacent, non-overlapping — `[entry, exit)`). (RSV-08)
- WHEN a Base62 code collides on insert (`UNIQUE` violation) THEN the system SHALL regenerate and retry up to a bounded number of times before surfacing an error. (RSV-09)
- WHEN the participant list is empty or contains only non-Responsável entries THEN the booking SHALL be rejected (≥1 participant; Responsável ∈ list). (RSV-04)
- WHEN a booking is submitted with an already-elapsed or malformed período THEN it SHALL be rejected before any lock is taken. (RSV-03)
- WHEN the sweeper finds a hold that another transaction is mid-transition on THEN it SHALL wait for / skip the locked row and never double-release. (RSV-14)
- WHEN an anonymous PF books THEN the booking SHALL be allowed (PRD §7 cadastro é opcional) and the PF limit SHALL apply. (RSV-15)
- WHEN `availability.Reserve` returns `ErrNoVacancy` or `ErrOutsideBookingWindow` THEN the handler SHALL map it to a friendly 409/422 fragment and persist nothing. (RSV-01, RSV-10)

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| -------------- | ----- | ----- | ------ |
| RSV-01 | P1: Create hold (Pendente + code + TTL + persist + `Reserve` + confirm) | Design | In Tasks |
| RSV-02 | P1: Create hold (campsite exists + Ativo; else reject) | Design | In Tasks |
| RSV-03 | P1: Create hold (período valid; outside-window rejected) | Design | In Tasks |
| RSV-04 | P1: Participants (CPF + name required; Responsável ∈ list; ≥1) | Design | In Tasks |
| RSV-05 | P1: Participants (per-reservation limit enforced) | Design | In Tasks |
| RSV-06 | P1: Participants (no duplicate CPF in one reservation) | Design | In Tasks |
| RSV-07 | P1: Emergency contact (≥1 with name/telefone/parentesco) | Design | In Tasks |
| RSV-08 | P1: Overlap prevention (any campsite; `[entry,exit)`; pre-check + exclusion) | Design | In Tasks |
| RSV-09 | P1: Unique Base62 code (charset + UNIQUE + retry) | Design | In Tasks |
| RSV-10 | P1: Concurrency (last-vacancy → exactly one winner; no oversell) | Design | In Tasks |
| RSV-11 | P1: Atomicity (Reserve + persist in one tx; rollback on failure) | Design | In Tasks |
| RSV-12 | P1: Concurrency (shared CPF overlap → exactly one winner via exclusion) | Design | In Tasks |
| RSV-13 | P1: Sweeper (Pendente past TTL → Expirada + `Release`, atomic) | Design | In Tasks |
| RSV-14 | P1: Sweeper (only Pendente; idempotent; concurrency-safe) | Design | In Tasks |
| RSV-15 | P2: Policy (TTL + PF/PJ limit from config; actor from identity) | Design | In Tasks |

**ID format:** `RSV-NN`. **Status values:** Pending → In Design → In Tasks → Implementing → Verified.
**Coverage:** 15 total, 15 mapped to tasks (see tasks.md), 0 unmapped.

### PRD / RF Traceability

- **RF04** (Criar reservas) — the whole feature (RSV-01…RSV-15).
- **PRD §6 Reserva** — code, Responsável, participants, entry/exit, emergency phones, status (RSV-01, 04, 07, 09).
- **PRD §6 Reserva temporária** — Pendente hold, default 10-min TTL configurable, expiry → Expirada + release (RSV-01, 13, 14, 15).
- **PRD §6 Participantes / Limite** — CPF+name, Responsável ∈ list, 5 PF / 15 PJ configurable (RSV-04, 05, 15).
- **PRD §6 Sobreposição** — same CPF, all diárias, all campsites (RSV-08, 12).
- **PRD §6 Contato de emergência** — ≥1 external contact name/telefone/parentesco (RSV-07).
- **PRD §6 Ocupação** (context) — Diária `09:00→09:00`, checkout not counted → `[entry, exit)` range + `headcount` per Diária passed to availability (RSV-01, 08).
- **PRD §8** — state flow entry point Pendente (→ Expirada) (RSV-01, 13).
- **PRD §13** — última vaga (RSV-10, 11), expiração de reserva temporária (RSV-13, 14), sobreposição (RSV-08, 12).

---

## Success Criteria

- [ ] `go test ./internal/modules/reservations/...` passes (domain + app unit) — factory invariants, VO validation, use-case orchestration with fakes.
- [ ] `go test -tags=integration ./internal/modules/reservations/...` passes — repository, exclusion constraint, sweeper, htmx handlers, and concurrency tests against a real Postgres 16 testcontainer.
- [ ] A visitor can complete new → POST → Pendente confirmation (code + expiry) end-to-end through htmx.
- [ ] Under N concurrent creates for the last vacancy: exactly one commits, no oversell (occupancy = effective capacity), verified by a concurrency integration test.
- [ ] An expired hold is swept to Expirada with its vacancies released and its CPFs re-bookable.
- [ ] No `reservations` code imports another module's `domain`/`app`; cross-module use is via `availability/public`, `campsites/public`, `config/public`, `identity/public` (or consumer-owned ports) only.

---

## Open Decisions

- **Period VO is reused from the shared kernel `internal/shared/booking`** — the same primitive availability uses at the seam — validated by `booking.NewPeriod` (`Exit > Entry`, `[entry, exit)` Diária semantics, `Nights()`). This follows the brief's "reuse shared kernel (CPF/CNPJ, Base62, **Period**) — don't reinvent" and keeps M2 DRY (one date-range VO across availability + reservations; whichever feature is implemented first seeds it). The reservation-specific mappings — → flat `availability/public.Period` DTO at the boundary, → `[entry, exit)` `daterange` for the exclusion constraint — stay in this module; the Diária-expansion business rule lives in `availability/domain`, not the shared kernel.
- **Overlap "active" set = Pendente (and future confirmed states).** Expirada/Cancelada/No-show do **not** block. Modeled as an `active` flag on the participant rows carrying the constraint; creation sets it true, the sweeper sets it false on expiry. M3/M4 features flip it on their transitions.
- **CPF-overlap exclusion constraint is denormalized onto `reservation_participants`** (each participant row carries the reservation's `during daterange` + `active`), because a Postgres exclusion constraint cannot span tables. The período is written both on `reservations` (source of truth for display/release) and on each participant row (constraint carrier), atomically in the same Save. Requires the `btree_gist` extension.
- **Base62 code length ≈ 8 chars** (Open — final length is a small constant / config value; ~8 Base62 chars ≈ 47 bits gives ample headroom for MVP volume). Uniqueness is guaranteed by the DB `UNIQUE` constraint + bounded regenerate-on-collision regardless of length.
- **Responsible telefone/e-mail are optional** on the hold (nullable, pre-filled from `identity/public` when authenticated). PRD §6 mentions them, but the brief's required-field set is CPF + name (participants) and the emergency contact carries the mandatory phone; enforcing responsible telefone is deferred to avoid widening MVP scope. Revisit if operations require it.
- **"External to the park, 24h-reachable" emergency contact is not machine-validated** (not decidable from the data). Captured as form guidance; only name/telefone/parentesco presence is enforced.
- **Actor ↔ Responsável identity is not cross-checked.** An authenticated user may book on behalf of others, and anonymous PF booking is allowed (PRD §7); the actor type only selects the participant limit. No rule ties the Responsável's CPF to the logged-in principal.
- **Account linkage captured but not enforced.** The reservation stores a nullable `booked_by_user_id` (the authenticated principal from `identity/public`; NULL for anonymous PF). This is written now — even though M2 has no read use for it — so RF07 "authenticated user history" (M3, LKP) can key on it without a schema migration. It matters most for **PJ**, whose company account has no CPF to match against `responsible_cpf`. M3 decides the exact history query (by `booked_by_user_id` and/or by the user's own CPF); M2 only persists the column.
- **`reservations/public` exposes nothing** in M2 (YAGNI). Lookup/cancel/check-in ports are added by their M3/M4 owners when first consumed.
- **LGPD posture (explicit gap, MVP stance).** This module is the store of record for personal data denormalized across rows — each `reservation_participants` row carries a CPF, and `responsible_cpf` is duplicated on `reservations`. LGPD is a stated legal constraint (PROJECT.md §Constraints, PRD §12), but a full retention/erasure design is **out of MVP scope** alongside the audit trail. MVP stance, recorded so it is a conscious decision and not an oversight: (1) CPF/CNPJ stored as plain `TEXT` (no field-level encryption in MVP); (2) no PII in logs (M0 RUN); (3) erasure is **non-trivial by construction** — a person's CPF appears on every reservation they took part in, so a future "right to be forgotten" flow must anonymize the denormalized CPF on *all* their participant rows (not just delete an account), and must preserve the exclusion-constraint invariant while doing so. Flag for a dedicated post-MVP LGPD feature (retention window, anonymization job, data-subject export). Do **not** silently assume `ON DELETE CASCADE` on a user account satisfies erasure — reservations are keyed by CPF, not by account.
