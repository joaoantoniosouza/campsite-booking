# Authentication Specification

**Milestone:** M1 — Identity & Setup
**Module:** `identity` (`internal/modules/identity/{domain,app,adapter,public}`) — sibling of REG
**Requirement prefix:** `AUTH`
**Implements:** No single RF — the access-control substrate underpinning RF04–RF13 (see Requirement
Traceability). Realizes AD-005 (cookie sessions + `bcrypt`); serves PRD §4, §8, §12.

## Problem Statement

Every self-service and operational surface downstream (reservations, lookup, check-in, admin)
must know *who* is acting and *with what authority*. The registration feature (REG) persists PF
users and PJ companies with an email + `bcrypt` password, but nothing lets those credentials log
in, hold a session, or gate access by role. This feature adds login, cookie-based sessions,
logout, session expiry, and the **authenticated-principal contract** other modules consume to
authorize requests.

## Goals

- [ ] A registered PF/PJ can log in with email + password (`bcrypt` verify) and receive a signed
      cookie session; wrong credentials are rejected without leaking which field was wrong.
- [ ] Every request carries an authenticated `Principal` (UserID + Role) — or an anonymous
      Visitor — available to this module *and* to other modules via `identity/public`.
- [ ] Role-based authorization (`visitor`, `Porteiro`, `Administrator`) is enforceable by any
      module through a single small `identity/public.RequireRole` middleware.
- [ ] A user can log out; sessions expire after a bounded lifetime and expired sessions are
      treated as anonymous.

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
| ------- | ------ |
| PF/PJ registration, credential/user persistence schema, CPF/CNPJ validation | Owned by M1 **user-company-registration** (REG). Auth *reads* the credential; it does not create it. |
| Password reset / change / "forgot password" e-mail flows | Post-MVP (needs notifications, which are out per PROJECT.md). Not in roadmap M1 auth scope. |
| Role assignment / provisioning of Porteiro & Administrator accounts | Done by registration defaults (`visitor`) and admin **management-surfaces** (M5). Auth only *reads* and *enforces* the role. |
| Server-side session store / revocation list, "remember me", refresh tokens | YAGNI for MVP — signed cookie sessions with expiry (AD-005) are sufficient. |
| Business-config-driven session TTL sourced from the CFG store | Auth uses an injected TTL (default). Wiring it to M1 **system-configuration** is a later, trivial swap. |
| Anonymous reservation flow ("reservar sem cadastro", PRD §4) | A reservations-module concern; auth only guarantees an anonymous Visitor principal exists. |

---

## User Stories

### P1: Login by email + password ⭐ MVP

**User Story**: As a registered visitor or company, I want to log in with my email and password so
that I can access my authenticated reservation history and self-service actions.

**Why P1**: Login is the entry point for every authenticated capability in the system; nothing
role-gated works without it.

**Acceptance Criteria**:

1. WHEN a user submits a valid, registered email and the correct password THEN the system SHALL
   verify the password against the stored `bcrypt` hash, create a `Session` for that user's
   UserID + Role, write it into a signed cookie, and redirect to the post-login destination.
   (AUTH-01)
2. WHEN a user submits an unknown email OR an incorrect password THEN the system SHALL reject the
   attempt with a single generic message ("invalid email or password"), set no session cookie,
   and NOT reveal which field was wrong (no user enumeration). (AUTH-02)
3. WHEN `GET /login` is requested THEN the system SHALL render the login form (full page on direct
   navigation, fragment on an `HX-Request`). (AUTH-03)
4. WHEN a password is verified THEN verification SHALL use a `bcrypt` comparison behind a domain
   `PasswordHasher` port, and the `Email` and `Role` SHALL be represented as validated value
   objects (invalid values unrepresentable). (AUTH-04)

**Independent Test**: Seed a credential row (email + `bcrypt` hash + role) directly in the test DB;
`POST /login` with correct creds → 302 + `Set-Cookie` session; with wrong password → re-rendered
form + generic error + no cookie.

---

### P1: Authenticated principal & session middleware ⭐ MVP

**User Story**: As any module in the system, I want each request to carry the authenticated
principal (UserID + Role) so that I can make authorization decisions without importing identity
internals.

**Why P1**: This is the cross-module contract every later milestone (reservations, check-in,
admin) depends on. It must exist before any role gating is possible.

**Acceptance Criteria**:

1. WHEN a request arrives with a valid, non-expired session cookie THEN the auth middleware SHALL
   populate `identity/public.Principal{UserID, Role, Authenticated:true}` into the request
   context. (AUTH-05)
2. WHEN a request arrives with no session, a tampered/forged cookie, or an expired session THEN
   the middleware SHALL populate an anonymous `Principal{Role: RoleVisitor, Authenticated:false}`
   and let the request proceed (no error). (AUTH-06)
3. WHEN another module needs the current actor THEN it SHALL read it only via
   `identity/public.PrincipalFromContext(ctx)` — never by importing `identity/domain` or
   `identity/app`. (AUTH-07)

**Independent Test**: Drive a probe handler behind the middleware: request with a freshly issued
cookie → handler sees `Authenticated:true` + correct role; request with no/garbled/expired cookie
→ handler sees anonymous Visitor.

---

### P1: Role-based authorization ⭐ MVP

**User Story**: As a module owner, I want to guard a route by required role(s) so that only a
Porteiro or an Administrator can reach operational/admin surfaces.

**Why P1**: Authorization is the point of having roles; PRD §8 makes Porteiro-only check-in and
§9 admin-only management hard requirements that ride on this middleware.

**Acceptance Criteria**:

1. WHEN a request whose principal holds one of the allowed roles reaches a route wrapped in
   `identity/public.RequireRole(roles...)` THEN the middleware SHALL allow it through. (AUTH-08)
2. WHEN the principal is authenticated but lacks every allowed role THEN `RequireRole` SHALL
   respond `403 Forbidden` and NOT invoke the protected handler. (AUTH-09)
3. WHEN the principal is anonymous (unauthenticated) on a `RequireRole` route THEN the middleware
   SHALL respond `401`/redirect to `/login` (browser navigation → redirect; `HX-Request` → 401
   with an `HX-Redirect` to `/login`). (AUTH-09)
4. WHEN a role is parsed THEN the domain SHALL accept only `visitor`, `Porteiro`, `Administrator`
   and reject any other value; the cross-module DTO SHALL expose these as flat string constants
   (`RoleVisitor`, `RolePorteiro`, `RoleAdministrator`). (AUTH-10)

**Independent Test**: Wrap a probe route in `RequireRole(RoleAdministrator)`; hit it as admin →
200; as Porteiro → 403; anonymous → redirect/401.

---

### P1: Logout ⭐ MVP

**User Story**: As a logged-in user, I want to log out so that my session no longer authenticates
requests from this browser.

**Why P1**: Basic session hygiene / LGPD expectation; trivial but required to close the login
loop.

**Acceptance Criteria**:

1. WHEN an authenticated user submits `POST /logout` THEN the system SHALL clear the session
   cookie (expire it) and redirect to `/` (or `/login`). (AUTH-11)
2. WHEN a subsequent request is made after logout THEN the auth middleware SHALL see an anonymous
   Visitor principal. (AUTH-11)

**Independent Test**: Log in → confirm authenticated principal; `POST /logout` → `Set-Cookie`
with a past expiry (MaxAge < 0); next request → anonymous.

---

### P2: Session lifetime & expiry

**User Story**: As a security-conscious operator, I want sessions to expire after a bounded
lifetime so that a stolen or abandoned cookie stops working.

**Why P2**: Login works without it, but a non-expiring session is a security defect; expiry is
part of PRD §12 (Segurança). Bundled just after the P1 slice.

**Acceptance Criteria**:

1. WHEN a `Session` is created THEN it SHALL record `IssuedAt` and `ExpiresAt = IssuedAt + TTL`,
   where TTL is an injected duration (default 24h). (AUTH-12)
2. WHEN the auth middleware reads a session whose `ExpiresAt <= now` THEN it SHALL treat the
   request as anonymous (per AUTH-06) and not surface an authenticated principal. (AUTH-13)
3. WHEN the session cookie is written at login THEN its `Max-Age` SHALL equal the TTL so the
   browser drops it on expiry, and it SHALL be marked `HttpOnly`, `SameSite=Lax`, and `Secure`
   (outside development). (AUTH-14)

**Independent Test**: Issue a session with a clock set in the past (or TTL≈0); a request one tick
later → middleware yields anonymous. Inspect the login `Set-Cookie` for `HttpOnly`/`Max-Age`.

---

### P3: Authenticated identity surfaced to the UI

**User Story**: As a logged-in user, I want the page to reflect that I am signed in (and, on a
protected page I could not reach, be returned there after login) so that the app feels coherent.

**Why P3**: Pure UX polish on top of the P1 contract; the system is fully functional without it.

**Acceptance Criteria**:

1. WHEN a page is rendered THEN the handler SHALL be able to read the current `Principal` from
   context so the base layout can show signed-in state (e.g. a logout control). (AUTH-15)
2. WHEN an anonymous user is redirected to `/login` from a protected route THEN, after a
   successful login, the system SHALL redirect back to the originally requested path (safe,
   same-origin paths only). (AUTH-16)

**Independent Test**: Visit a `RequireRole` page anonymously → redirected to `/login?next=…`;
log in → land on the originally requested path.

---

## Edge Cases

- WHEN the login form is submitted with an empty email or empty password THEN the system SHALL
  return a validation error WITHOUT performing a DB lookup or `bcrypt` compare. (AUTH-02)
- WHEN the submitted email is not found THEN the system SHALL still perform a constant-cost dummy
  `bcrypt` comparison before returning the generic error, to blunt timing-based user enumeration.
  (AUTH-02)
- WHEN the session cookie is present but its signature fails (tampered/forged) THEN the request
  SHALL proceed as anonymous, never as the impersonated user. (AUTH-06)
- WHEN an already-authenticated user requests `GET /login` THEN the system SHALL redirect to `/`
  instead of showing the form. (AUTH-03)
- WHEN the stored password hash is malformed/empty for a credential THEN verification SHALL fail
  as `ErrInvalidCredentials` (never panic, never authenticate). (AUTH-02)
- WHEN `RequireRole` receives an `HX-Request` for an unauthorized principal THEN it SHALL emit an
  htmx-friendly response (`HX-Redirect` / 403) rather than a full-page redirect. (AUTH-09)

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| -------------- | ----- | ----- | ------ |
| AUTH-01 | P1: Login | Design | In Tasks |
| AUTH-02 | P1: Login | Design | In Tasks |
| AUTH-03 | P1: Login | Design | In Tasks |
| AUTH-04 | P1: Login | Design | In Tasks |
| AUTH-05 | P1: Principal & middleware | Design | In Tasks |
| AUTH-06 | P1: Principal & middleware | Design | In Tasks |
| AUTH-07 | P1: Principal & middleware | Design | In Tasks |
| AUTH-08 | P1: Role authorization | Design | In Tasks |
| AUTH-09 | P1: Role authorization | Design | In Tasks |
| AUTH-10 | P1: Role authorization | Design | In Tasks |
| AUTH-11 | P1: Logout | Design | In Tasks |
| AUTH-12 | P2: Session lifetime & expiry | Design | In Tasks |
| AUTH-13 | P2: Session lifetime & expiry | Design | In Tasks |
| AUTH-14 | P2: Session lifetime & expiry | Design | In Tasks |
| AUTH-15 | P3: Identity in UI | Design | In Tasks |
| AUTH-16 | P3: Identity in UI | Design | In Tasks |

**ID format:** `AUTH-NN` (per CONVENTIONS.md prefix table).

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 16 total, 16 mapped to tasks (see tasks.md), 0 unmapped.

**PRD / RF traceability:** Authentication implements **no single functional requirement** — no RF
is dedicated to it. It is the **access-control substrate underpinning RF04–RF13**: every
authenticated or role-restricted operation (reservations RF04–RF07/RF13, check-in RF10, walk-in
RF12, config/management RF11) rides on the `Principal` + `RequireRole` contract defined here. It
directly realizes **AD-005** (cookie-based sessions + `bcrypt`, STATE.md) and serves PRD **§4**
(Personas: Porteiro has no admin rights; Administrator manages everything), **§8** (check-in is a
Porteiro-only permission set — the concrete driver for the Porteiro role), and **§12**
(Segurança / LGPD: password hashing, session expiry, no credential leakage). It reuses the M0
session seam (`internal/platform/httpx.SessionFromContext`, SKEL-07) and `Config.SessionSecret`
(RUN).

---

## Success Criteria

- [ ] A seeded PF and PJ account can log in and receive a working session cookie; wrong password
      is rejected with a generic message and no cookie.
- [ ] A protected `RequireRole(RoleAdministrator)` route returns 200 for an admin, 403 for a
      Porteiro, and redirect/401 for anonymous — driven entirely through `identity/public`.
- [ ] Another module reads the actor solely via `identity/public.PrincipalFromContext` with zero
      import of `identity/domain` or `identity/app` (boundary test stays green).
- [ ] Logout clears the cookie and the next request is anonymous.
- [ ] An expired session is treated as anonymous; login cookies are `HttpOnly` with a bounded
      `Max-Age`.

---

## Open Decisions

- **Credential source schema is REG's contract — resolved (no longer TBD).** Auth needs
  `(account_id, email, password_hash, role)` retrievable by email. **Decision:** for MVP there is
  **no** unified `credentials`/`accounts` table; Auth's `CredentialRepository.FindByEmail` reads a
  **UNION view over REG's `users` and `companies` tables** (each already stores email +
  `password_hash` + role), owned and migrated by user-company-registration. This matches REG's MVP
  storage (two typed tables + app-level cross-type email pre-check) with zero duplicated schema.
  The deferred `identity_credentials(email UNIQUE, account_id, kind)` projection (see REG Open
  Decisions) remains the agreed future hardening that would make global email uniqueness DB-final
  and replace the UNION with a single indexed read — to be adopted jointly by REG + AUTH if/when
  that race is closed. No further coordination is outstanding for the MVP slice.
- **Role provisioning is not auth's job.** Registration creates accounts as `visitor`; Porteiro
  and Administrator are promoted by admin management (M5) or a bootstrap seed. Auth only *reads*
  and *enforces* the role. If no admin exists at first boot, a seeded Administrator may be needed
  — flagged for the M5/ops track, not built here.
- **Logout is handled in the HTTP adapter, not an app use case.** With signed cookie sessions
  there is no server-side session state to revoke, so "logout" is cookie expiry — modeling it as
  a use case with a repository would be YAGNI. Revisit if a server-side session store is ever
  introduced.
- **Session TTL is an injected constant (default 24h), not yet CFG-backed.** Kept out of the M1
  system-configuration store to avoid a cross-feature dependency in this slice; swapping the
  injected value for a CFG lookup later is a one-line composition-root change.
