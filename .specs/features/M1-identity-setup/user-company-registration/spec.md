# User & Company Registration Specification

**Milestone:** M1 — Identity & Setup
**Module:** `identity` (`internal/modules/identity/{domain,app,adapter,public}`)
**Requirement prefix:** `REG`
**Implements:** RF02 (Gerenciar usuários), RF03 (Gerenciar empresas); PRD §7 (Cadastros), §6 (participant/CPF context), §12 (NFR: LGPD, segurança).

## Problem Statement

Park visitors (Pessoa Física) may want an account for login, reservation history, faster
cancellation, and pre-filled data; companies (Pessoa Jurídica) **must** have an account to book.
Today no identity exists to hang a reservation, a session, or an admin view on. This feature lets
a PF register (optionally) and a PJ register (required, company + legal responsible), validating
CPF/CNPJ and enforcing unique, LGPD-aware, securely-hashed credential storage — the account data
every later module reads. **Login/sessions are out of scope** (the sibling AUTH feature).

## Goals

- [ ] A PF can self-register with valid data; the account is persisted with a bcrypt-hashed password and a unique id.
- [ ] A PJ can register a company + its legal responsible atomically (one transaction, no orphans).
- [ ] CPF and CNPJ are validated by shared-kernel value objects; email/CPF/CNPJ uniqueness is DB-guaranteed.
- [ ] Personal data is stored LGPD-aware; passwords are never stored, logged, or echoed in plaintext.
- [ ] Other modules can resolve a registered account to a flat principal projection via `identity/public` — without importing identity internals.

## Out of Scope

Explicitly excluded to prevent scope creep.

| Feature | Reason |
| ------- | ------ |
| Login, password verification, sessions, cookies | Sibling AUTH feature (prefix `AUTH`, same module). REG only writes credentials; AUTH reads them. |
| Roles / permissions (visitor, Porteiro, Administrator) | AUTH owns the role model. |
| Profile editing, password reset, account deletion | Post-registration lifecycle; not in RF02/RF03 MVP for this feature. |
| Email verification / confirmation flows | Notifications are post-MVP (PRD §3 "Fora do MVP"). |
| Pre-filling reservation forms from a PF account | Consumed later by reservations (M2) via `identity/public`; that read is downstream, not here. |
| Admin management of users/companies | M5 management surfaces. |
| Detailed audit trail of registrations | Post-MVP (PRD §3, §12). |

---

## User Stories

### P1: Pessoa Física registration ⭐ MVP

**User Story**: As a park visitor, I want to register an optional PF account with my name, CPF,
birth date, email, phone, and password so that I get login, history, easier cancellation, and
pre-filled data (PRD §7).

**Why P1**: M1 target is "PF/PJ can register and authenticate." Without the PF account there is no
authenticated principal and no history — the foundation of self-service.

**Acceptance Criteria**:

1. WHEN a PF submits name, CPF, birth date, email, phone, and password that are all valid and unique THEN system SHALL create a `User`, store the password bcrypt-hashed, assign a unique id, and confirm success.
2. WHEN the submitted CPF is malformed or fails check-digit validation THEN system SHALL reject the registration with a field-level "CPF inválido" error and persist nothing.
3. WHEN the email is malformed THEN system SHALL reject with an email validation error; the email SHALL be normalized (trimmed, lower-cased) before uniqueness checks and storage.
4. WHEN the email or CPF already belongs to an existing account THEN system SHALL reject with "e-mail já cadastrado" / "CPF já cadastrado" and persist nothing.
5. WHEN the password is shorter than the minimum policy (≥ 8 chars, ≤ 72 bytes for bcrypt) THEN system SHALL reject with a password-policy error.
6. WHEN any required field is missing, or the birth date is not a valid past date THEN system SHALL reject with the specific field error and persist nothing.

**Independent Test**: POST the PF form with valid unique data → 200 + a `users` row with a bcrypt
hash (never the plaintext); re-POST the same email/CPF → rejected; POST an invalid CPF → rejected.

---

### P1: Pessoa Jurídica registration ⭐ MVP

**User Story**: As a company, I want to register with our razão social, CNPJ, email, and password
plus a legal responsible (nome, CPF, email, telefone) so that we can book campsites (PRD §7, RF03).

**Why P1**: PJ registration is **required** to book; a reservation by CNPJ presupposes the company
account exists.

**Acceptance Criteria**:

1. WHEN a PJ submits a valid company (razão social, CNPJ, email, password) and a valid legal responsible (nome, CPF, email, telefone) THEN system SHALL persist the `Company` and its `LegalResponsible` in a **single transaction**, hash the company password, assign a unique id, and confirm success.
2. WHEN the CNPJ is malformed or fails check-digit validation THEN system SHALL reject with "CNPJ inválido" and persist nothing.
3. WHEN the company email or CNPJ already exists THEN system SHALL reject with the corresponding uniqueness error and persist nothing.
4. WHEN the legal responsible is missing or has an invalid nome/CPF/email/telefone THEN system SHALL reject with the specific field error and persist nothing.
5. WHEN persistence of the legal responsible fails after the company row is written THEN system SHALL roll back the whole transaction, leaving no orphan company.

**Independent Test**: POST the PJ form with valid company + responsible → a `companies` row and one
linked `legal_responsibles` row exist; inject a responsible-insert failure → neither row persists.

---

### P2: Cross-module principal resolution (`identity/public`)

**User Story**: As another module (AUTH, reservations, admin), I want to resolve a registered
account id to a flat account projection so that I can display or act on a principal without
importing identity's domain/app internals.

**Why P2**: Not needed to demo registration itself, but the module boundary (ARCHITECTURE §2)
requires a `public/` surface before any sibling consumes identity. Kept minimal (YAGNI).

**Acceptance Criteria**:

1. WHEN a consumer calls `Directory.ByID(ctx, id)` for an existing account THEN system SHALL return an `identity/public.Account` flat DTO (`ID`, `Kind` = PF|PJ, `Email`, `DisplayName`) mapped from the domain aggregate — no domain types crossing the boundary.
2. WHEN the id does not resolve to any account THEN system SHALL return `ErrAccountNotFound`.

**Independent Test**: Register a PF and a PJ, then `ByID` each id → correct `Kind`, `Email`, and
`DisplayName` (PF name / company razão social); an unknown id → `ErrAccountNotFound`.

---

## Edge Cases

- WHEN two requests register the **same** email/CPF/CNPJ concurrently THEN exactly one SHALL succeed; the loser SHALL receive a uniqueness error — the DB unique index is the final guarantee, not the app pre-check (mirrors ARCHITECTURE §7 "DB is the final guarantee").
- WHEN a PF email equals an existing PJ company email (cross-type collision) THEN system SHALL reject, because login (AUTH) resolves a principal by a single global email (see Open Decisions).
- WHEN validation fails THEN the error response SHALL NOT echo the submitted password and SHALL NOT log CPF/CNPJ/password in plaintext (LGPD, PRD §12).
- WHEN CPF/CNPJ is submitted with punctuation/whitespace (dots, slash, dashes) THEN the shared VO SHALL normalize to digits before validation and storage.
- WHEN a birth date in the future or an impossible date is submitted THEN system SHALL reject it.
- WHEN the same legal responsible CPF is used by two different companies THEN system SHALL allow it (a person may be responsible for multiple companies — no uniqueness on responsible CPF).

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| -------------- | ----- | ----- | ------ |
| REG-01 | P1 PF: happy-path create (unique id, required fields, past birth date) | Design | In Tasks |
| REG-02 | P1 PF: CPF validated via shared VO (reject invalid) | Design | In Tasks |
| REG-03 | P1 PF: email validated + normalized | Design | In Tasks |
| REG-04 | P1 PF: uniqueness (email, CPF) — DB-guaranteed | Design | In Tasks |
| REG-05 | P1 (PF+PJ): password policy + bcrypt hashing | Design | In Tasks |
| REG-06 | P1 PJ: happy-path create (company + legal responsible atomic) | Design | In Tasks |
| REG-07 | P1 PJ: CNPJ validated via shared VO (reject invalid) | Design | In Tasks |
| REG-08 | P1 PJ: uniqueness (company email, CNPJ) — DB-guaranteed | Design | In Tasks |
| REG-09 | P1 PJ: legal responsible required + valid (nome, CPF, email, telefone) | Design | In Tasks |
| REG-10 | P1 PJ: atomic persistence / rollback (no orphan) | Design | In Tasks |
| REG-11 | P2: `identity/public.Account` + `Directory.ByID` principal resolution | Design | In Tasks |
| REG-12 | Edge/NFR: LGPD-aware storage; no plaintext store/log/echo | Design | In Tasks |
| REG-13 | Edge/NFR: concurrent duplicate → exactly one winner (DB unique final guarantee) | Design | In Tasks |
| REG-14 | P1: htmx forms render + field-level error fragments + success | Design | In Tasks |

**ID format:** `REG-NN`. **Status values:** Pending → In Design → In Tasks → Implementing → Verified.
**Coverage:** 14 total, 14 mapped to tasks (see tasks.md), 0 unmapped.

### PRD / RF Traceability

- **RF02** (Gerenciar usuários) → REG-01…REG-05 (PF registration); PRD §7 Pessoa Física.
- **RF03** (Gerenciar empresas) → REG-06…REG-10 (PJ registration); PRD §7 Pessoa Jurídica.
- **PRD §12** (NFR: LGPD, segurança, consistência transacional) → REG-05, REG-10, REG-12, REG-13.
- **PRD §6** (Participantes: "todos devem possuir CPF") → shares the CPF VO introduced here (REG-02).
- **ARCHITECTURE §2** (module boundary) → REG-11 (`identity/public`).
- **ARCHITECTURE §7 / PRD §13** (DB is the final guarantee, concurrent last-write) → REG-13.

---

## Success Criteria

- [ ] `go build ./... && go vet ./... && go test ./...` green (domain + app unit tests).
- [ ] `go test -tags=integration ./internal/modules/identity/...` green: repository, HTTP, public, and migration tests pass against a Postgres testcontainer.
- [ ] A PF and a PJ register end-to-end through the mounted chi routes; rows exist; passwords are bcrypt hashes.
- [ ] Concurrent duplicate registrations resolve to exactly one success (integration concurrency test).
- [ ] `Directory.ByID` returns correct flat DTOs for both kinds; unknown id → `ErrAccountNotFound`.

---

## Open Decisions

- **Cross-type email uniqueness (chosen: per-table DB unique index + app-level cross-type pre-check).**
  `users.email` and `companies.email` each have their own unique index (hard guarantee within a
  type). Global (PF-vs-PJ) email uniqueness — needed so AUTH resolves a login to exactly one
  principal — is enforced in the use cases by checking the other table before insert, translated to
  `ErrEmailAlreadyRegistered`. This leaves a narrow concurrent PF-vs-PJ race. Deferred hardening: a
  shared `identity_credentials(email UNIQUE, account_id, kind)` projection written in the same
  transaction would make it DB-final; recommended to finalize together with AUTH (which owns
  login-by-email). Acceptable for MVP given app pre-check + low collision likelihood.
  **Downstream contract:** AUTH reads credentials via a UNION over these two tables — REG therefore
  owns exposing `(account_id, email, password_hash, role)` on both `users` and `companies`, and is
  the single owner of the credential schema AUTH depends on (see authentication Open Decisions,
  now resolved for MVP).
- **CPF/CNPJ value objects introduced here** in `internal/shared/document` (CPF, CNPJ). M0 named
  them as placeholders only; REG is the first consumer, so it builds them as reusable shared-kernel
  primitives (reservations/checkin reuse later). No business rules added to the shared kernel.
- **bcrypt cost is a wired constant (default 12), not a config-store value.** No CFG dependency
  (YAGNI); can move to system-configuration later if tuning is needed.
- **Legal responsible modeled as an entity inside the `Company` aggregate** (one-to-one, mutated
  only through the root), not a standalone PF `User`. It is contact data, not a login identity.
