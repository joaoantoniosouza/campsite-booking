# Authentication Tasks

**Design**: `.specs/features/M1-identity-setup/authentication/design.md`
**Status**: Draft

> **Test note (read first):** Per TESTING.md, `domain`/`app` layers are **unit** (quick gate);
> `adapter/repository`, `adapter/http`, `public` cross-module impl are **integration** (full gate,
> real Postgres via `pgtest.Setup`). Two pure exceptions are justified below and in the Test
> Co-location table: the `bcrypt` hasher (T5) is CPU-only with no I/O → **unit**; the pure
> `public` DTO/middleware (T2, T6) are stdlib-only → **unit**, and the *cross-module `public`
> contract's integration requirement is satisfied by the end-to-end wiring test in T12*
> (merge-forward, not deferral). All integration tasks share the Postgres testcontainer, so per
> the Parallelism Assessment they carry **no `[P]`**; only unit tasks may be `[P]`.
>
> **Cross-feature dependency:** T8/T10/T12 integration tests read the credential source that
> **user-company-registration (REG)** creates. REG's credential migration must be applied by the
> shared migration stream before those tasks run; tests seed a credential row directly (raw
> INSERT), not via REG's registration use case, to keep auth independently testable.

---

## Execution Plan

### Phase 1: Domain & contract roots (Parallel OK — unit, no deps)

```
T1 [P]   (Role VO)
T2 [P]   (public Principal DTO + context accessors)
```

### Phase 2: Domain entities & authz middleware (Parallel OK — unit)

```
T1 ──┬──→ T3 [P]   (Session entity)
     └──→ T4 [P]   (Credential + ports + errors)
T2 ─────→ T6 [P]   (public RequireRole)
```

### Phase 3: Password adapter & use case (Parallel OK — unit)

```
T4 ─────→ T5 [P]   (bcrypt hasher)
T3,T4 ──→ T7 [P]   (Authenticate use case)
```

### Phase 4: Adapters & wiring (Sequential — integration, shared Postgres)

```
T4 ───────────→ T8   (CredentialRepository pgx)
T2,T3 ────────→ T9   (AuthMiddleware)
(root) ────────→ T11 (Logout handler)
T7,T8 ────────→ T10  (Login handlers + template)
T6,T9,T10,T11 → T12  (Module mount + bootstrap wiring + e2e authz/expiry contract)
```

---

## Task Breakdown

### T1: Role value object [P]

**What**: `Role` VO/enum with `ParseRole`, `String`, value equality; accepts only
`visitor`/`Porteiro`/`Administrator`, else `ErrInvalidRole`.
**Where**: `internal/modules/identity/domain/role.go` (+ `role_test.go`)
**Depends on**: None
**Reuses**: n/a
**Requirement**: AUTH-10

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `ParseRole` accepts the three roles (exact PT-BR casing for `Porteiro`), rejects others with `ErrInvalidRole`
- [ ] `String()` round-trips; VO compared by value
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: 3 tests pass (valid roles table, invalid→err, round-trip) (no silent deletions)

**Tests**: unit
**Gate**: quick

**Verify**: `go test ./internal/modules/identity/domain/ -run Role` → PASS.

**Commit**: `feat(identity): add Role value object`

---

### T2: public Principal DTO + context accessors [P]

**What**: Flat `Principal{UserID,Role,Authenticated}`, role string constants, `Anonymous()`,
`ContextWithPrincipal`, `PrincipalFromContext`.
**Where**: `internal/modules/identity/public/principal.go` (+ `principal_test.go`)
**Depends on**: None
**Reuses**: stdlib `context`
**Requirement**: AUTH-07, AUTH-10

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `PrincipalFromContext` returns the stored principal, or `Anonymous()` when unset
- [ ] `Anonymous()` == `{Role: RoleVisitor, Authenticated:false}`; constants defined
- [ ] Package imports stdlib only (no domain/app/infra) — keeps the boundary clean
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: 3 tests pass (round-trip, unset→anonymous, anonymous shape) (no silent deletions)

**Tests**: unit (pure; cross-module contract integration-proven in T12)
**Gate**: quick

**Verify**: `go test ./internal/modules/identity/public/ -run Principal` → PASS.

**Commit**: `feat(identity): add public Principal DTO and context accessors`

---

### T3: Session entity [P]

**What**: `Session{UserID,Role,IssuedAt,ExpiresAt}` with `NewSession(uid,role,now,ttl)` (invariant
`ExpiresAt = IssuedAt + ttl`) and `IsExpired(now)`.
**Where**: `internal/modules/identity/domain/session.go` (+ `session_test.go`)
**Depends on**: T1
**Reuses**: `Role` (T1), stdlib `time`
**Requirement**: AUTH-12, AUTH-13

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `NewSession` sets `ExpiresAt = IssuedAt + ttl`; rejects empty UserID / zero ttl
- [ ] `IsExpired(now)` true when `now >= ExpiresAt`, false before (boundary tested with a fixed clock)
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: 4 tests pass (expiry set, not-expired, expired-at-boundary, invalid input) (no silent deletions)

**Tests**: unit
**Gate**: quick

**Verify**: `go test ./internal/modules/identity/domain/ -run Session` → PASS.

**Commit**: `feat(identity): add Session entity with expiry invariant`

---

### T4: Credential entity + ports + sentinel errors [P]

**What**: `Credential{UserID,Email,PasswordHash,Role}` with `VerifyPassword(plain,hasher)`;
`PasswordHasher` + `CredentialRepository` port interfaces; `ErrInvalidCredentials`,
`ErrCredentialNotFound` (and reuse `ErrInvalidRole`).
**Where**: `internal/modules/identity/domain/{credential.go,errors.go}` (+ `credential_test.go`)
**Depends on**: T1
**Reuses**: `Role` (T1), `Email` VO (REG, intra-module)
**Requirement**: AUTH-02, AUTH-04

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `VerifyPassword` delegates to a `PasswordHasher` fake; returns `ErrInvalidCredentials` on mismatch, nil on match
- [ ] Port interfaces + sentinel errors defined; `errors.Is` works
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: 3 tests pass (match, mismatch→err, empty-hash→err) (no silent deletions)

**Tests**: unit
**Gate**: quick

**Verify**: `go test ./internal/modules/identity/domain/ -run Credential` → PASS.

**Commit**: `feat(identity): add Credential entity and auth ports`

---

### T5: bcrypt PasswordHasher implementation [P]

**What**: `bcryptHasher` implementing `PasswordHasher.Compare` via
`bcrypt.CompareHashAndPassword`; maps mismatch → `ErrInvalidCredentials`; exposes a fixed dummy
compare for timing mitigation.
**Where**: `internal/modules/identity/adapter/security/bcrypt.go` (+ `bcrypt_test.go`)
**Depends on**: T4
**Reuses**: `golang.org/x/crypto/bcrypt`, `identity/domain`
**Requirement**: AUTH-04

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `Compare` returns nil for a hash of the given plaintext, `ErrInvalidCredentials` for a wrong one
- [ ] Malformed hash → `ErrInvalidCredentials` (no panic)
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: 3 tests pass (match, mismatch, malformed) (no silent deletions)

**Tests**: unit (pure CPU, no I/O)
**Gate**: quick

**Verify**: `go test ./internal/modules/identity/adapter/security/ -run Bcrypt` → PASS.

**Commit**: `feat(identity): add bcrypt password hasher`

---

### T6: public RequireRole / RequireAuthenticated middleware [P]

**What**: `RequireRole(roles...)` and `RequireAuthenticated()` reading `PrincipalFromContext`:
allowed role → next; authenticated wrong role → 403; anonymous → 302 `/login` (401 + `HX-Redirect`
for `HX-Request`).
**Where**: `internal/modules/identity/public/authz.go` (+ `authz_test.go`)
**Depends on**: T2
**Reuses**: `Principal`/`PrincipalFromContext` (T2), stdlib `net/http`
**Requirement**: AUTH-08, AUTH-09

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Allowed-role principal reaches the wrapped handler (200 probe)
- [ ] Authenticated wrong role → 403, handler not invoked
- [ ] Anonymous → 302 `/login` (plain) and 401 + `HX-Redirect` (with `HX-Request` header)
- [ ] Package still imports stdlib only
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: 4 tests pass (allowed, forbidden, anon-redirect, anon-htmx) (no silent deletions)

**Tests**: unit (pure httptest; contract integration-proven in T12)
**Gate**: quick

**Verify**: `go test ./internal/modules/identity/public/ -run RequireRole` → PASS.

**Commit**: `feat(identity): add RequireRole authorization middleware`

---

### T7: Authenticate use case [P]

**What**: `Authenticate.Execute(ctx, LoginCommand) (AuthResult, error)` — parse `Email`, load via
`CredentialRepository`, verify via `PasswordHasher`, mint `Session` with injected clock + TTL;
not-found *and* mismatch both → `ErrInvalidCredentials` (+ dummy compare on not-found).
**Where**: `internal/modules/identity/app/authenticate.go` (+ `authenticate_test.go`)
**Depends on**: T3, T4
**Reuses**: `Session` (T3), `Credential`/ports (T4), hand-written fakes for repo + hasher
**Requirement**: AUTH-01, AUTH-02, AUTH-12

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Valid creds → `AuthResult{UserID,Role,ExpiresAt}` with `ExpiresAt = now()+ttl`
- [ ] Unknown email and wrong password both return `ErrInvalidCredentials` (fake asserts dummy compare ran on not-found)
- [ ] Empty email/password → validation error before repo/hasher calls
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: 5 tests pass (success, unknown-email, wrong-password, empty-input, expiry-set) (no silent deletions)

**Tests**: unit (fakes honor port contracts — LSP)
**Gate**: quick

**Verify**: `go test ./internal/modules/identity/app/ -run Authenticate` → PASS.

**Commit**: `feat(identity): add Authenticate login use case`

---

### T8: CredentialRepository pgx implementation + read query

**What**: `sqlc` `FindCredentialByEmail :one` query + `CredentialRepository.FindByEmail` pgx impl
over the REG-owned credential source; "no rows" → `ErrCredentialNotFound`.
**Where**: `internal/modules/identity/adapter/repository/credential_repository.go` (+
`credential_repository_test.go`, `//go:build integration`); `db/queries/identity_credentials.sql`
**Depends on**: T4
**Reuses**: `internal/platform/postgres` pool, `pgtest.Setup`, `identity/domain`
**Requirement**: AUTH-02, AUTH-04

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Test seeds a credential row (raw INSERT) then `FindByEmail` returns the mapped `Credential`
- [ ] Unknown email → `ErrCredentialNotFound`; other DB errors wrapped (not conflated)
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 3 tests pass (found, not-found, mapping fields incl. role) (no silent deletions)

**Tests**: integration
**Gate**: full

**Verify**: `go test -tags=integration ./internal/modules/identity/adapter/repository/` → PASS.

**Commit**: `feat(identity): add pgx credential repository`

---

### T9: AuthMiddleware (session → principal, expiry)

**What**: `AuthMiddleware(now)` reading the signed cookie via `httpx.SessionFromContext`, rebuilding
`domain.Session`, dropping it when `IsExpired(now)`, and writing `public.Principal` (authenticated
or `Anonymous()`) into context.
**Where**: `internal/modules/identity/adapter/http/auth_middleware.go` (+
`auth_middleware_test.go`, `//go:build integration`)
**Depends on**: T2, T3
**Reuses**: `httpx.Session`/`SessionFromContext` (SKEL), `Session` (T3), `public` (T2)
**Requirement**: AUTH-05, AUTH-06, AUTH-13

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Valid non-expired cookie → downstream handler sees `Authenticated:true` + correct UserID/Role
- [ ] Missing / tampered / expired cookie → `Anonymous()` Visitor, request proceeds (no error)
- [ ] Expired branch driven with a clock past `ExpiresAt`
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 4 tests pass (valid, missing, tampered, expired) (no silent deletions)

**Tests**: integration
**Gate**: full

**Verify**: `go test -tags=integration ./internal/modules/identity/adapter/http/ -run AuthMiddleware` → PASS.

**Commit**: `feat(identity): add session auth middleware`

---

### T10: Login handlers + login template

**What**: `GET /login` (render form full/fragment; redirect authed users), `POST /login` (validate
→ `Authenticate` → write signed cookie with `Max-Age`=TTL, `HttpOnly` → redirect to safe `next`;
on `ErrInvalidCredentials` re-render 200 with generic message).
**Where**: `internal/modules/identity/adapter/http/login_handler.go` (+ `login_handler_test.go`,
`//go:build integration`); `web/templates/identity/login.html`
**Depends on**: T7, T8
**Reuses**: `Authenticate` (T7), `CredentialRepository` (T8), `web.Renderer`, `httpx` session,
`pgtest.Setup`, `net/url` (safe redirect)
**Requirement**: AUTH-01, AUTH-02, AUTH-03, AUTH-14, AUTH-16

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `POST /login` with a seeded valid credential → 302 + `Set-Cookie` session (`HttpOnly`, `Max-Age`>0)
- [ ] Wrong password → 200 re-render, generic message, no cookie; empty fields → validation, no DB/bcrypt call
- [ ] `GET /login` → form (full page; fragment on `HX-Request`); already-authed → redirect `/`
- [ ] Safe `next` param honored; off-origin/absolute `next` ignored
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 6 tests pass (success+cookie, wrong-pw, empty, GET full, GET fragment, safe-next) (no silent deletions)

**Tests**: integration
**Gate**: full

**Verify**: `go test -tags=integration ./internal/modules/identity/adapter/http/ -run Login` → PASS.

**Commit**: `feat(identity): add login handlers and template`

---

### T11: Logout handler

**What**: `POST /logout` clearing the session cookie (`Options.MaxAge = -1`, save) and redirecting
to `/`.
**Where**: `internal/modules/identity/adapter/http/logout_handler.go` (+ `logout_handler_test.go`,
`//go:build integration`)
**Depends on**: None
**Reuses**: `httpx.Session`/`SessionFromContext` (SKEL)
**Requirement**: AUTH-11

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `POST /logout` responds with a redirect and a `Set-Cookie` expiring the session (MaxAge < 0)
- [ ] Test drives it behind the `httpx.Session` middleware (no DB needed)
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 2 tests pass (clears cookie, redirect target) (no silent deletions)

**Tests**: integration (adapter/http)
**Gate**: full

**Verify**: `go test -tags=integration ./internal/modules/identity/adapter/http/ -run Logout` → PASS.

**Commit**: `feat(identity): add logout handler`

---

### T12: identity module mount + bootstrap wiring + end-to-end authz/expiry contract

**What**: `Module` implementing `bootstrap.Module` (mount `AuthMiddleware` globally, `/login`,
`/logout`); composition-root wiring (repo→bcrypt→`Authenticate`→handlers→middleware, session store
with `HttpOnly`/`SameSite`/`Secure` from `SessionSecret`); end-to-end integration test proving the
cross-module `public` contract.
**Where**: `internal/modules/identity/adapter/http/module.go` (+ `module_test.go`,
`//go:build integration`); `internal/platform/bootstrap/bootstrap.go` (additive wiring)
**Depends on**: T6, T9, T10, T11
**Reuses**: `bootstrap.Module` seam (SKEL), `public.RequireRole` (T6), all prior identity pieces,
`pgtest.Setup`, `Config.SessionSecret`
**Requirement**: AUTH-05, AUTH-07, AUTH-08, AUTH-09, AUTH-11, AUTH-14, AUTH-15, AUTH-16

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] End-to-end: seed admin + Porteiro creds; login → cookie; `GET` a probe route wrapped in `public.RequireRole(RoleAdministrator)` → 200 for admin, 403 for Porteiro, redirect/401 anonymous
- [ ] After `POST /logout`, the same route is anonymous again (redirect/401)
- [ ] Store cookies are `HttpOnly` + `SameSite=Lax`; a handler reads `PrincipalFromContext` to render signed-in state; `next` round-trip returns to the originally requested path
- [ ] A consumer package importing only `identity/public` (no `domain`/`app`) compiles — boundary honored
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 5 tests pass (admin-200, porteiro-403, anon-redirect, logout→anon, next-roundtrip) (no silent deletions)

**Tests**: integration
**Gate**: full

**Verify**: `go test -tags=integration ./internal/modules/identity/adapter/http/ -run Module` and
`go test -tags=integration ./internal/platform/bootstrap/` → PASS.

**Commit**: `feat(identity): wire auth module and prove public authz contract`

---

## Parallel Execution Map

```
Phase 1 (Parallel — unit, no deps):
  ├── T1 [P]
  └── T2 [P]

Phase 2 (Parallel — unit):
  T1 ──┬── T3 [P]
       └── T4 [P]
  T2 ───── T6 [P]

Phase 3 (Parallel — unit):
  T4 ────── T5 [P]
  T3,T4 ─── T7 [P]

Phase 4 (Sequential — integration, shared Postgres container):
  T8  (dep T4)
  T9  (dep T2,T3)
  T11 (no dep)
  T10 (dep T7,T8)
  T12 (dep T6,T9,T10,T11)
```

**Parallelism constraint check:** every `[P]` task (T1–T7) is **unit** — parallel-safe per the
TESTING.md Parallelism Assessment — and shares no mutable state (each owns distinct files; fakes
are local). All integration tasks (T8–T12) share the Postgres testcontainer, so they carry **no
`[P]`** and run serially, regardless of code independence (T8/T9/T11 have no code deps but still
run one at a time).

---

## Task Granularity Check

| Task | Scope | Status |
| ---- | ----- | ------ |
| T1: Role VO | 1 value object | ✅ Granular |
| T2: public Principal + accessors | 1 DTO + its context transport (cohesive) | ✅ Granular |
| T3: Session entity | 1 entity | ✅ Granular |
| T4: Credential + ports + errors | 1 entity + its ports/errors (cohesive) | ✅ Granular |
| T5: bcrypt hasher | 1 adapter (1 port impl) | ✅ Granular |
| T6: RequireRole middleware | 1 middleware (2 cohesive constructors) | ✅ Granular |
| T7: Authenticate use case | 1 use case | ✅ Granular |
| T8: credential repository | 1 repo impl + 1 query | ✅ Granular |
| T9: AuthMiddleware | 1 middleware | ✅ Granular |
| T10: login handlers + template | 1 handler pair + its template (cohesive) | ✅ OK (cohesive) |
| T11: logout handler | 1 handler | ✅ Granular |
| T12: module mount + wiring + e2e | 1 composition seam + its contract test | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (body) | Diagram Shows | Status |
| ---- | ----------------- | ------------- | ------ |
| T1 | None | (root) | ✅ Match |
| T2 | None | (root) | ✅ Match |
| T3 | T1 | T1→T3 | ✅ Match |
| T4 | T1 | T1→T4 | ✅ Match |
| T5 | T4 | T4→T5 | ✅ Match |
| T6 | T2 | T2→T6 | ✅ Match |
| T7 | T3, T4 | T3,T4→T7 | ✅ Match |
| T8 | T4 | T4→T8 | ✅ Match |
| T9 | T2, T3 | T2,T3→T9 | ✅ Match |
| T10 | T7, T8 | T7,T8→T10 | ✅ Match |
| T11 | None | (root of integration phase) | ✅ Match |
| T12 | T6, T9, T10, T11 | T6,T9,T10,T11→T12 | ✅ Match |

Parallel-set independence: {T1,T2} independent ✅; {T3,T4,T6} independent ✅; {T5,T7} independent
✅. No `[P]` task depends on another task in its own phase.

---

## Test Co-location Validation

Coverage matrix (TESTING.md): `domain`/`app` → **unit**; `adapter/repository`, `adapter/http` →
**integration**; `public` cross-module impl → **integration**. Pure exceptions justified inline.

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| ---- | -------------------------- | --------------- | --------- | ------ |
| T1 | domain (Role VO) | unit | unit | ✅ OK |
| T2 | public (pure DTO/context) | integration* | unit | ✅ OK — *pure stdlib; contract integration-proven in T12 (merge-forward) |
| T3 | domain (Session) | unit | unit | ✅ OK |
| T4 | domain (Credential + ports) | unit | unit | ✅ OK |
| T5 | adapter/security (bcrypt) | integration† | unit | ✅ OK — †pure CPU, no I/O; unit is the honest type |
| T6 | public (pure middleware) | integration* | unit | ✅ OK — *pure httptest; contract integration-proven in T12 |
| T7 | app (use case) | unit | unit | ✅ OK |
| T8 | adapter/repository (pgx) | integration | integration | ✅ OK |
| T9 | adapter/http (middleware) | integration | integration | ✅ OK |
| T10 | adapter/http (handlers) | integration | integration | ✅ OK |
| T11 | adapter/http (handler) | integration | integration | ✅ OK |
| T12 | adapter/http + bootstrap wiring + public contract | integration | integration | ✅ OK |

**Notes:** No `Tests: none` is used anywhere — every task carries its own tests. The `public`
package's integration requirement (matrix) is satisfied by T12's end-to-end test that exercises
`Principal` + `RequireRole` through real module wiring against Postgres — the cross-module contract
is validated in the task where it becomes runnable, not deferred (per tasks.md "Resolving
compilation dependencies" guidance). T5's bcrypt hasher is deterministic CPU work with no DB/HTTP,
so a unit test fully covers it.

---

## Requirement Coverage

| Requirement | Task(s) |
| ----------- | ------- |
| AUTH-01 | T7, T10 |
| AUTH-02 | T4, T7, T8, T10 |
| AUTH-03 | T10 |
| AUTH-04 | T4, T5, T8 |
| AUTH-05 | T9, T12 |
| AUTH-06 | T9 |
| AUTH-07 | T2, T12 |
| AUTH-08 | T6, T12 |
| AUTH-09 | T6, T12 |
| AUTH-10 | T1, T2 |
| AUTH-11 | T11, T12 |
| AUTH-12 | T3, T7 |
| AUTH-13 | T3, T9 |
| AUTH-14 | T10, T12 |
| AUTH-15 | T9, T12 |
| AUTH-16 | T10, T12 |

All 16 requirements mapped; 12 tasks, 0 unmapped.
