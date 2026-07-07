# User & Company Registration Tasks

**Design**: `.specs/features/M1-identity-setup/user-company-registration/design.md`
**Status**: Draft

All tasks follow TDD (RED → GREEN → REFACTOR); tests are co-located in the task that creates the
code (never deferred). Gate commands from TESTING.md: **quick** = `go build ./... && go vet ./... && go test ./...`;
**full** = `go test -tags=integration ./...`; **build** = `go build ./...`.

**Parallelism (TESTING.md):** unit-only tasks are parallel-safe (`[P]` when no code deps).
adapter/repository, adapter/http, public, and migration tasks are **integration** ⇒ share the
Postgres testcontainer ⇒ **serial, no `[P]`**.

**Cross-feature note:** `internal/platform/postgres` (pool/`WithTx`) and `pgtest` come from DATA;
the `bootstrap.Module` seam + chi router + base htmx layout come from SKEL (both M0). External deps
(`golang.org/x/crypto/bcrypt`, `github.com/google/uuid`) added via `go get` in the tasks that use them.

---

## Execution Plan

### Phase 1: Domain foundation — VOs & hasher (unit, parallel)

```
T1 [P]  shared/document CPF+CNPJ VOs
T2 [P]  identity Email+Phone VOs
T3 [P]  identity Password VO + policy
T4 [P]  PasswordHasher port + bcrypt impl
```

### Phase 2: Aggregates (unit, parallel — after Phase 1)

```
T1,T2,T3 ─┬─→ T5 [P]  User aggregate + UserRepository iface + sentinels
          └─→ T6 [P]  Company + LegalResponsible + CompanyRepository iface
```

### Phase 3: Use cases (unit, parallel)

```
T5,T4 ─→ T7 [P]  RegisterUser use case + ports + DTOs
T6,T4 ─→ T8 [P]  RegisterCompany use case + DTOs
```

### Phase 4: Persistence (integration, serial)

```
T9 (migration 000002) ─┬─→ T10 (pgx UserRepository)
                       └─→ T11 (pgx CompanyRepository)
   (T10 also needs T5; T11 also needs T6)
```

### Phase 5: HTTP + public surface (integration, serial)

```
T7,T10 ─→ T12 (PF handlers + templates)
T8,T11 ─→ T13 (PJ handlers + templates)
T10,T11 ─→ T14 (public Directory + impl)
```

### Phase 6: Module wiring (integration, serial)

```
T12,T13,T14 ─→ T15 (identity Module assembly + Mount + Directory export)
```

---

## Task Breakdown

### T1: shared-kernel CPF + CNPJ value objects [P]

**What**: `CPF` and `CNPJ` VOs — normalize to digits, check-digit validation, `String()`; sentinel `ErrInvalidCPF`/`ErrInvalidCNPJ`.
**Where**: `internal/shared/document/cpf.go`, `internal/shared/document/cnpj.go` (+ `*_test.go`)
**Depends on**: None
**Reuses**: stdlib only
**Requirement**: REG-02, REG-07

**Tools**: MCP: NONE — Skill: NONE

**Done when**:

- [ ] `NewCPF`/`NewCNPJ` accept punctuated input, normalize to 11/14 digits, reject bad length + bad check digits (table-driven **unit**).
- [ ] Known-valid and known-invalid fixtures covered; `String()` returns digits-only.
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: ~12 unit cases pass (valid, invalid check digit, wrong length, punctuation, empty), no silent deletions.

**Verify**: `go test ./internal/shared/document/ -v` → PASS.

**Tests**: unit
**Gate**: quick
**Commit**: `feat(document): add CPF and CNPJ value objects to shared kernel`

---

### T2: identity Email + Phone value objects [P]

**What**: `Email` (trim/lower-case + format) and `Phone` (digits, BR length 10–11) VOs + `ErrInvalidEmail`/`ErrInvalidPhone`.
**Where**: `internal/modules/identity/domain/email.go`, `.../domain/phone.go` (+ `*_test.go`)
**Depends on**: None
**Reuses**: stdlib (`net/mail` or regex), `strings`
**Requirement**: REG-03, REG-09

**Tools**: MCP: NONE — Skill: NONE

**Done when**:

- [ ] `NewEmail` normalizes + rejects malformed input; `NewPhone` normalizes to digits + enforces length (**unit**, table-driven).
- [ ] `String()` returns the normalized value.
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: ~10 unit cases pass, no silent deletions.

**Verify**: `go test ./internal/modules/identity/domain/ -run 'TestEmail|TestPhone' -v` → PASS.

**Tests**: unit
**Gate**: quick
**Commit**: `feat(identity): add Email and Phone value objects`

---

### T3: identity Password VO + password policy [P]

**What**: `Password` VO holding a bcrypt hash (`NewPassword(hash)`, `Hash()`) + pure `ValidatePasswordPolicy(plain)` (len 8–72 bytes) + `ErrWeakPassword`.
**Where**: `internal/modules/identity/domain/password.go` (+ `password_test.go`)
**Depends on**: None
**Reuses**: stdlib only (no bcrypt here — hashing is in adapter)
**Requirement**: REG-05, REG-12

**Tools**: MCP: NONE — Skill: NONE

**Done when**:

- [ ] `NewPassword` rejects empty / non-`$2` hash; `Hash()` returns the stored hash.
- [ ] `ValidatePasswordPolicy` rejects < 8 and > 72 bytes, accepts valid (**unit**).
- [ ] No plaintext retained on the VO (holds hash only).
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: ~8 unit cases pass, no silent deletions.

**Verify**: `go test ./internal/modules/identity/domain/ -run TestPassword -v` → PASS.

**Tests**: unit
**Gate**: quick
**Commit**: `feat(identity): add Password value object and password policy`

---

### T4: PasswordHasher port + bcrypt implementation [P]

**What**: `PasswordHasher` port (`app`) + bcrypt impl (`adapter/security`) at a configurable cost (default 12).
**Where**: `internal/modules/identity/app/ports.go` (port), `internal/modules/identity/adapter/security/bcrypt.go` (+ `bcrypt_test.go`)
**Depends on**: None
**Reuses**: `golang.org/x/crypto/bcrypt` (`go get`)
**Requirement**: REG-05, REG-12

**Tools**: MCP: `context7` (bcrypt API) — Skill: NONE

**Done when**:

- [ ] `Hash(ctx, plain)` returns a `$2`-prefixed hash; `bcrypt.CompareHashAndPassword(hash, plain)` succeeds (roundtrip **unit**, no I/O).
- [ ] Different plaintexts produce different hashes; cost is applied.
- [ ] `go get golang.org/x/crypto/bcrypt` recorded in `go.mod`.
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: ~3 unit cases pass, no silent deletions.

**Verify**: `go test ./internal/modules/identity/adapter/security/ -v` → PASS.

**Tests**: unit
**Gate**: quick
**Commit**: `feat(identity): add bcrypt PasswordHasher behind an app port`

---

### T5: User aggregate + UserRepository interface + sentinels [P]

**What**: `User` aggregate + `NewUser` factory (invariants: non-empty name, past birth date), `UserRepository` interface, and shared identity sentinels (`ErrEmailAlreadyRegistered`, `ErrCPFAlreadyRegistered`, `ErrInvalidBirthDate`).
**Where**: `internal/modules/identity/domain/user.go`, `.../domain/errors.go`, `.../domain/repository.go` (+ `user_test.go`)
**Depends on**: T1, T2, T3
**Reuses**: `document.CPF` (T1), `Email`/`Phone` (T2), `Password` (T3)
**Requirement**: REG-01, REG-03, REG-04

**Tools**: MCP: NONE — Skill: NONE

**Done when**:

- [ ] `NewUser` builds a valid aggregate; rejects empty name and non-past birth date (`ErrInvalidBirthDate`) (**unit**).
- [ ] No exported field setters (mutation only via constructor).
- [ ] `UserRepository` interface compiles with `Save`/`ExistsByEmail`/`ExistsByCPF`/`FindByID`.
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: ~6 unit cases pass, no silent deletions.

**Verify**: `go test ./internal/modules/identity/domain/ -run TestNewUser -v` → PASS.

**Tests**: unit
**Gate**: quick
**Commit**: `feat(identity): add User aggregate, repository interface and sentinels`

---

### T6: Company aggregate + LegalResponsible entity + CompanyRepository interface [P]

**What**: `Company` aggregate root + `LegalResponsible` entity (inside it) + `NewCompany`/`NewLegalResponsible` factories + `CompanyRepository` interface + `ErrCNPJAlreadyRegistered`/`ErrMissingLegalResponsible`.
**Where**: `internal/modules/identity/domain/company.go`, `.../domain/legal_responsible.go` (+ `company_test.go`); extends `.../domain/repository.go`, `.../domain/errors.go`
**Depends on**: T1, T2, T3
**Reuses**: `document.CNPJ`/`CPF` (T1), `Email`/`Phone` (T2), `Password` (T3)
**Requirement**: REG-06, REG-08, REG-09, REG-10

**Tools**: MCP: NONE — Skill: NONE

**Done when**:

- [ ] `NewCompany` requires a valid `LegalResponsible` (`ErrMissingLegalResponsible` when nil); `NewLegalResponsible` validates nome/CPF/email/telefone (**unit**).
- [ ] `LegalResponsible` mutated only through `Company` (no external setters).
- [ ] `CompanyRepository` interface compiles with `Save`/`ExistsByEmail`/`ExistsByCNPJ`/`FindByID`.
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: ~7 unit cases pass, no silent deletions.

**Verify**: `go test ./internal/modules/identity/domain/ -run TestNewCompany -v` → PASS.

**Tests**: unit
**Gate**: quick
**Commit**: `feat(identity): add Company aggregate with LegalResponsible entity`

---

### T7: RegisterUser use case + ports + DTOs [P]

**What**: `RegisterUser.Handle` (validate VOs → policy → uniqueness cross-check → hash → `NewUser` → `Save`, translate unique-violation) + `RegisterUserCommand`/`Result` + `IDGenerator`/`Clock` ports.
**Where**: `internal/modules/identity/app/register_user.go`, `.../app/ports.go` (extend) (+ `register_user_test.go`)
**Depends on**: T5, T4
**Reuses**: `UserRepository` (T5), `PasswordHasher` (T4); hand-written fakes per TESTING.md
**Requirement**: REG-01, REG-04, REG-05, REG-12

**Tools**: MCP: NONE — Skill: NONE

**Done when**:

- [ ] Happy path returns `{ID}`, calls `Save` once with a hashed password (fake hasher), never retains plaintext (**unit**, fakes).
- [ ] Invalid CPF/email/phone, weak password, duplicate email/CPF each abort with the right sentinel and no `Save` (**unit**).
- [ ] Deterministic id/clock via injected fakes; birth-date "now" comparison uses `Clock`.
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: ~8 unit cases pass, no silent deletions.

**Verify**: `go test ./internal/modules/identity/app/ -run TestRegisterUser -v` → PASS.

**Tests**: unit
**Gate**: quick
**Commit**: `feat(identity): add RegisterUser use case`

---

### T8: RegisterCompany use case + DTOs [P]

**What**: `RegisterCompany.Handle` (build responsible + company, hash, atomic `Save`, translate unique-violation) + `RegisterCompanyCommand`/`Result`.
**Where**: `internal/modules/identity/app/register_company.go` (+ `register_company_test.go`)
**Depends on**: T6, T4
**Reuses**: `CompanyRepository` (T6), `PasswordHasher`/`IDGenerator`/`Clock` ports (T4/T7); fakes
**Requirement**: REG-06, REG-08, REG-10, REG-05

**Tools**: MCP: NONE — Skill: NONE

**Done when**:

- [ ] Happy path returns `{ID}`, builds `Company` with a `LegalResponsible`, hashes the password, one `Save` (**unit**, fakes).
- [ ] Invalid CNPJ, invalid/missing responsible, duplicate company email/CNPJ abort with the right sentinel and no `Save` (**unit**).
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: ~8 unit cases pass, no silent deletions.

**Verify**: `go test ./internal/modules/identity/app/ -run TestRegisterCompany -v` → PASS.

**Tests**: unit
**Gate**: quick
**Commit**: `feat(identity): add RegisterCompany use case`

---

### T9: Identity schema migration `000002_identity_accounts`

**What**: Migration creating `users`, `companies`, `legal_responsibles` with unique indexes (cpf, email, cnpj, company_id) + FK cascade; reversible `.down.sql`.
**Where**: `db/migrations/000002_identity_accounts.up.sql`, `db/migrations/000002_identity_accounts.down.sql` (+ `internal/modules/identity/adapter/repository/migration_test.go`, `//go:build integration`)
**Depends on**: None (extends DATA's `000001` stream)
**Reuses**: `pgtest.Setup` (DATA), migration conventions (DATA design §Migration Conventions)
**Requirement**: REG-04, REG-08, REG-10, REG-12, REG-13

**Tools**: MCP: `context7` (Postgres DDL) — Skill: NONE

**Done when**:

- [ ] `up` creates all three tables + unique indexes (`users_cpf_key`, `users_email_key`, `companies_cnpj_key`, `companies_email_key`, `legal_responsibles_company_id_key`) + FK `ON DELETE CASCADE` (**integration**, via `pgtest`).
- [ ] `down` drops them in reverse; migration is idempotent under `RunMigrations`.
- [ ] A direct duplicate-`email` insert raises `23505` on the expected constraint (asserts the guarantee) (**integration**).
- [ ] Gate check passes: `go test -tags=integration ./internal/modules/identity/...`
- [ ] Test count: ~3 integration tests pass (schema present, reversible, unique-violation), no silent deletions.

**Verify**: `go test -tags=integration ./internal/modules/identity/adapter/repository/ -run TestMigration -v` → PASS.

**Tests**: integration
**Gate**: full
**Commit**: `feat(identity): add users/companies/legal_responsibles migration`

---

### T10: pgx UserRepository + sqlc queries

**What**: pgx `UserRepository` impl (`Save`, `ExistsByEmail`, `ExistsByCPF`, `FindByID`) using `WithTx`; translate `23505` → `ErrEmailAlreadyRegistered`/`ErrCPFAlreadyRegistered`; user sqlc queries.
**Where**: `internal/modules/identity/adapter/repository/user_repository.go`, `db/queries/identity.sql` (user queries) (+ `user_repository_test.go`, `//go:build integration`)
**Depends on**: T5, T9
**Reuses**: `postgres.WithTx`/pool (DATA), `pgtest.Setup`, `document`/VO mapping, sqlc
**Requirement**: REG-01, REG-04, REG-13

**Tools**: MCP: `context7` (pgx/sqlc, pgconn error codes) — Skill: NONE

**Done when**:

- [ ] `Save` then `FindByID` round-trips a `User` (VOs reconstructed; `password_hash` stored) (**integration**).
- [ ] `ExistsByEmail`/`ExistsByCPF` reflect inserted rows.
- [ ] Duplicate email/CPF `Save` returns the mapped domain sentinel (via `23505` translation).
- [ ] **Concurrency (REG-13):** N goroutines `Save` the same email → exactly one success, others get `ErrEmailAlreadyRegistered` (**integration**, real commits).
- [ ] Gate check passes: `go test -tags=integration ./internal/modules/identity/...`
- [ ] Test count: ~5 integration tests pass, no silent deletions.

**Verify**: `go test -tags=integration ./internal/modules/identity/adapter/repository/ -run TestUserRepository -v` → PASS.

**Tests**: integration
**Gate**: full
**Commit**: `feat(identity): add pgx UserRepository with unique-violation mapping`

---

### T11: pgx CompanyRepository + sqlc queries

**What**: pgx `CompanyRepository` impl — `Save` inserts company + legal responsible in **one** `WithTx` transaction (rollback on partial failure); `ExistsByEmail`/`ExistsByCNPJ`/`FindByID`; `23505` translation; company sqlc queries.
**Where**: `internal/modules/identity/adapter/repository/company_repository.go`, `db/queries/identity.sql` (company queries) (+ `company_repository_test.go`, `//go:build integration`)
**Depends on**: T6, T9
**Reuses**: `postgres.WithTx`/pool, `pgtest.Setup`, sqlc
**Requirement**: REG-06, REG-08, REG-10, REG-13

**Tools**: MCP: `context7` (pgx tx, pgconn error codes) — Skill: NONE

**Done when**:

- [ ] `Save` persists company + one linked `legal_responsibles` row atomically; `FindByID` returns both (**integration**).
- [ ] **Atomicity (REG-10):** a forced responsible-insert failure rolls back the company (no orphan row) (**integration**).
- [ ] Duplicate company email/CNPJ returns the mapped domain sentinel; concurrent duplicate → exactly one winner (**integration**).
- [ ] Gate check passes: `go test -tags=integration ./internal/modules/identity/...`
- [ ] Test count: ~5 integration tests pass, no silent deletions.

**Verify**: `go test -tags=integration ./internal/modules/identity/adapter/repository/ -run TestCompanyRepository -v` → PASS.

**Tests**: integration
**Gate**: full
**Commit**: `feat(identity): add pgx CompanyRepository with atomic responsible persistence`

---

### T12: PF registration handlers + htmx templates

**What**: `GET /register/pf` (form) + `POST /register/pf` (decode → `RegisterUser` → success/redirect or field-error fragment); htmx templates.
**Where**: `internal/modules/identity/adapter/http/pf_handler.go`, `web/templates/identity/register_pf.tmpl`, `.../register_form_errors.tmpl` (+ `pf_handler_test.go`, `//go:build integration`)
**Depends on**: T7, T10
**Reuses**: `RegisterUser` (T7), pgx repo (T10), `httpx`/`web` base layout (SKEL), `pgtest`
**Requirement**: REG-01, REG-03, REG-14

**Tools**: MCP: NONE — Skill: NONE

**Done when**:

- [ ] `GET` renders the PF form; `POST` valid data → 200/redirect + `users` row (route → use case → real repo) (**integration**).
- [ ] `POST` invalid CPF / duplicate email → field-error fragment; **password never echoed** in the response (REG-12/REG-14) (**integration**).
- [ ] Handler stays thin (no business rules); domain errors mapped to PT-BR messages.
- [ ] Gate check passes: `go test -tags=integration ./internal/modules/identity/...`
- [ ] Test count: ~4 integration tests pass, no silent deletions.

**Verify**: `go test -tags=integration ./internal/modules/identity/adapter/http/ -run TestPFRegister -v` → PASS.

**Tests**: integration
**Gate**: full
**Commit**: `feat(identity): add PF registration htmx handlers`

---

### T13: PJ registration handlers + htmx templates

**What**: `GET /register/pj` + `POST /register/pj` (decode company + responsible → `RegisterCompany` → success or field-error fragment); htmx templates.
**Where**: `internal/modules/identity/adapter/http/pj_handler.go`, `web/templates/identity/register_pj.tmpl` (+ `pj_handler_test.go`, `//go:build integration`)
**Depends on**: T8, T11
**Reuses**: `RegisterCompany` (T8), pgx repo (T11), base layout, error-fragment template (T12), `pgtest`
**Requirement**: REG-06, REG-09, REG-14

**Tools**: MCP: NONE — Skill: NONE

**Done when**:

- [ ] `GET` renders the PJ form (company + responsible fields); `POST` valid → 200/redirect + `companies` + `legal_responsibles` rows (**integration**).
- [ ] `POST` invalid CNPJ / missing responsible / duplicate email → field-error fragment; password never echoed (**integration**).
- [ ] Gate check passes: `go test -tags=integration ./internal/modules/identity/...`
- [ ] Test count: ~4 integration tests pass, no silent deletions.

**Verify**: `go test -tags=integration ./internal/modules/identity/adapter/http/ -run TestPJRegister -v` → PASS.

**Tests**: integration
**Gate**: full
**Commit**: `feat(identity): add PJ registration htmx handlers`

---

### T14: public Account DTO + Directory port + impl

**What**: `identity/public.Account` DTO + `Directory` port + `ErrAccountNotFound`; app-layer `Directory` impl mapping `User`/`Company` → `Account` via `FindByID`.
**Where**: `internal/modules/identity/public/account.go`, `internal/modules/identity/app/directory.go` (+ `directory_test.go`, `//go:build integration`)
**Depends on**: T10, T11
**Reuses**: `UserRepository`/`CompanyRepository` (T10/T11), `pgtest`
**Requirement**: REG-11

**Tools**: MCP: NONE — Skill: NONE

**Done when**:

- [ ] `ByID` for a registered PF returns `Account{Kind: PF, DisplayName: name}`; for a PJ `{Kind: PJ, DisplayName: razaoSocial}` (**integration**, real repos).
- [ ] Unknown id → `public.ErrAccountNotFound`; no domain types cross the boundary (DTO is flat primitives).
- [ ] Gate check passes: `go test -tags=integration ./internal/modules/identity/...`
- [ ] Test count: ~3 integration tests pass, no silent deletions.

**Verify**: `go test -tags=integration ./internal/modules/identity/app/ -run TestDirectory -v` → PASS.

**Tests**: integration
**Gate**: full
**Commit**: `feat(identity): expose public Account DTO and Directory port`

---

### T15: identity Module assembly + Mount + Directory export

**What**: An identity module constructor that builds repos + use cases + handlers + `Directory`, implements the SKEL `bootstrap.Module` seam (mount `/register/pf`, `/register/pj`), and returns the `Directory` for the composition root to wire cross-module.
**Where**: `internal/modules/identity/adapter/http/module.go` (+ `module_test.go`, `//go:build integration`)
**Depends on**: T12, T13, T14
**Reuses**: all identity components; `bootstrap.Module` seam (SKEL), chi router, `pgtest`
**Requirement**: REG-11, REG-14

**Tools**: MCP: NONE — Skill: NONE

**Done when**:

- [ ] Constructor wires pool → repos → use cases → handlers + bcrypt/uuid/clock impls; `Mount(router)` registers both routes.
- [ ] End-to-end via the mounted chi router: register a PF then a PJ (200 + persisted); the exported `Directory.ByID` resolves both (**integration**).
- [ ] Import-boundary respected: identity imports no other module's `domain`/`app` (ARCHITECTURE §2) — asserted by the SKEL boundary test / package-import check.
- [ ] Gate check passes: `go test -tags=integration ./internal/modules/identity/...`
- [ ] Test count: ~2 integration tests pass, no silent deletions.

**Verify**: `go test -tags=integration ./internal/modules/identity/adapter/http/ -run TestModule -v` → PASS.

**Tests**: integration
**Gate**: full
**Commit**: `feat(identity): wire identity module (mount routes, export Directory)`

---

## Parallel Execution Map

```
Phase 1 (unit, parallel):
    ├── T1 [P]  document CPF/CNPJ
    ├── T2 [P]  Email/Phone
    ├── T3 [P]  Password + policy
    └── T4 [P]  PasswordHasher + bcrypt

Phase 2 (unit, parallel — after T1,T2,T3):
    ├── T5 [P]  User aggregate
    └── T6 [P]  Company aggregate

Phase 3 (unit, parallel):
    ├── T7 [P]  RegisterUser        (after T5,T4)
    └── T8 [P]  RegisterCompany     (after T6,T4)

Phase 4 (integration, SERIAL — shared testcontainer):
    T9  migration
     ├── T10 pgx UserRepository     (after T5,T9)
     └── T11 pgx CompanyRepository  (after T6,T9)

Phase 5 (integration, SERIAL):
    T12 PF handlers   (after T7,T10)
    T13 PJ handlers   (after T8,T11)
    T14 public Directory (after T10,T11)

Phase 6 (integration, SERIAL):
    T15 module wiring  (after T12,T13,T14)
```

**Parallelism constraint:** `[P]` requires no unfinished deps, a parallel-safe (unit) test type, and
no shared mutable state. T1–T8 are unit ⇒ `[P]` within their phase. T9–T15 are integration ⇒ serial
per TESTING.md regardless of code independence (shared Postgres container is the bottleneck).

---

## Task Granularity Check

| Task | Scope | Status |
| ---- | ----- | ------ |
| T1 | 2 cohesive VOs (CPF, CNPJ) in one kernel package | ✅ Granular |
| T2 | 2 cohesive VOs (Email, Phone) | ✅ Granular |
| T3 | 1 VO + 1 policy func | ✅ Granular |
| T4 | 1 port + 1 impl | ✅ Granular |
| T5 | 1 aggregate + its repo iface + sentinels | ✅ Granular |
| T6 | 1 aggregate root + inner entity + repo iface | ✅ Granular |
| T7 | 1 use case | ✅ Granular |
| T8 | 1 use case | ✅ Granular |
| T9 | 1 migration | ✅ Granular |
| T10 | 1 repository impl | ✅ Granular |
| T11 | 1 repository impl | ✅ Granular |
| T12 | 1 handler pair (PF) + templates | ✅ Granular |
| T13 | 1 handler pair (PJ) + templates | ✅ Granular |
| T14 | 1 DTO + 1 port + 1 impl | ✅ Granular |
| T15 | 1 module assembly | ✅ Granular |

Multi-file tasks (T1/T2/T5/T6) stay within the "2–3 cohesive things in one concept" allowance and
are tested as a unit. No task spans unrelated concepts.

---

## Diagram-Definition Cross-Check

| Task | Depends On (body) | Diagram Shows | Status |
| ---- | ----------------- | ------------- | ------ |
| T1 | None | root (feeds T5,T6) | ✅ Match |
| T2 | None | root (feeds T5,T6) | ✅ Match |
| T3 | None | root (feeds T5,T6) | ✅ Match |
| T4 | None | root (feeds T7,T8) | ✅ Match |
| T5 | T1,T2,T3 | `T1,T2,T3 → T5` | ✅ Match |
| T6 | T1,T2,T3 | `T1,T2,T3 → T6` | ✅ Match |
| T7 | T5,T4 | `T5,T4 → T7` | ✅ Match |
| T8 | T6,T4 | `T6,T4 → T8` | ✅ Match |
| T9 | None | root of Phase 4 (feeds T10,T11) | ✅ Match |
| T10 | T5,T9 | `T5,T9 → T10` | ✅ Match |
| T11 | T6,T9 | `T6,T9 → T11` | ✅ Match |
| T12 | T7,T10 | `T7,T10 → T12` | ✅ Match |
| T13 | T8,T11 | `T8,T11 → T13` | ✅ Match |
| T14 | T10,T11 | `T10,T11 → T14` | ✅ Match |
| T15 | T12,T13,T14 | `T12,T13,T14 → T15` | ✅ Match |

- Every `Depends on` has a matching arrow; every arrow maps to a `Depends on`.
- No `[P]` task depends on another `[P]` task in the same phase (T5/T6 independent; T7/T8 independent). ✅
- All integration tasks (T9–T15) carry no `[P]`. ✅

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| ---- | --------------------------- | --------------- | --------- | ------ |
| T1 | domain (shared kernel VOs) | unit | unit | ✅ OK |
| T2 | domain (VOs) | unit | unit | ✅ OK |
| T3 | domain (VO + policy) | unit | unit | ✅ OK |
| T4 | app port + adapter/security (bcrypt, no I/O) | unit | unit | ✅ OK |
| T5 | domain (aggregate + interface) | unit | unit | ✅ OK |
| T6 | domain (aggregate + entity + interface) | unit | unit | ✅ OK |
| T7 | app (use case, ports mocked) | unit | unit | ✅ OK |
| T8 | app (use case, ports mocked) | unit | unit | ✅ OK |
| T9 | db/migrations | integration | integration | ✅ OK |
| T10 | adapter/repository | integration | integration | ✅ OK |
| T11 | adapter/repository | integration | integration | ✅ OK |
| T12 | adapter/http | integration | integration | ✅ OK |
| T13 | adapter/http | integration | integration | ✅ OK |
| T14 | public (cross-module impl) | integration | integration | ✅ OK |
| T15 | adapter/http + platform wiring | integration | integration | ✅ OK |

- No task uses `Tests: none` (every task creates a code layer with a required test type).
- T4 bcrypt adapter has no I/O ⇒ unit is the correct (and highest) required type for that layer.
- Every requirement REG-01…REG-14 is covered by ≥1 task; each task cites its requirement IDs.
