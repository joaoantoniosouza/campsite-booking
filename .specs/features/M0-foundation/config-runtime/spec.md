# Config & Runtime Specification

## Problem Statement

The modular monolith needs a single, typed, fail-fast source of runtime configuration
(DB URL, HTTP port, session secret, log settings) instead of scattered `os.Getenv` calls,
plus operational primitives every module relies on: structured request logging and a health
probe. Without these, the M0 skeleton cannot boot deterministically, be observed, or be
health-checked by an orchestrator.

This is **runtime/technical** configuration only (env vars, secrets, ports). It is explicitly
**not** the M1 business configuration store (CFG: reservation limits, booking window,
cancellation/change deadlines, hold TTL, overbooking rules) — that is a DB-backed business
feature. The only seam here: the M1 config store obtains its pgx pool / `DATABASE_URL` from
this runtime config.

## Goals

- [ ] One typed `config.Config` loaded once from the environment; fail-fast validation aborts
      startup (non-zero exit) with a clear message naming every offending variable.
- [ ] `GET /healthz` reports process liveness + DB readiness in < a bounded probe timeout.
- [ ] Structured `slog` logging wired into request middleware: every request emits one
      correlated log line (method, path, status, duration); handlers/use cases log via context.
- [ ] `.env.example` documents every configurable variable for local dev, drift-guarded against
      the `Config` struct.

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
| ------- | ------ |
| Business configuration (limits, booking window, deadlines, hold TTL, overbooking) | M1 `system-configuration` (CFG) — DB-backed business feature, distinct from runtime config |
| pgx pool construction, migrations, testcontainer harness | M0 `data-migration` (DATA); this feature only parses `DATABASE_URL` and pings the pool |
| chi router assembly, recovery/session middleware, graceful shutdown, base layout | M0 `project-skeleton` (SKEL); this feature contributes the logging middleware + `/healthz` route it mounts |
| Metrics / tracing / log aggregation backends | Post-MVP observability; MVP uses stdlib `slog` to stderr |
| `.env` auto-loading (godotenv) | KISS/YAGNI — dev tooling (direnv / compose `env_file`) sources `.env`; app reads real process env only |
| Secrets manager / vault integration | Post-MVP; secrets arrive as env vars |

---

## User Stories

### P1: Typed, fail-fast runtime config ⭐ MVP

**User Story**: As an operator, I want the app to load all runtime settings from the environment
into one validated typed struct so that misconfiguration fails immediately at boot, not at first
request.

**Why P1**: Nothing else in the skeleton can be constructed without a valid, injected config;
fail-fast prevents a half-started server serving errors.

**Acceptance Criteria**:

1. WHEN the process starts THEN the system SHALL parse the environment into a single
   `config.Config` value with no global mutable state.
2. WHEN a required variable (`DATABASE_URL`, `SESSION_SECRET`) is missing or invalid THEN
   `config.Load` SHALL return an error that names **every** offending variable and SHALL NOT
   return a partially populated config.
3. WHEN `SESSION_SECRET` is present but shorter than 32 characters THEN the system SHALL reject
   it as invalid (cookie-signing strength).
4. WHEN `HTTP_PORT` is non-numeric or out of the 1–65535 range, or `LOG_LEVEL`/`LOG_FORMAT` is
   not a recognized value THEN the system SHALL return a validation error naming the variable and
   its accepted values.
5. WHEN an optional variable is unset THEN the system SHALL apply its documented default
   (`HTTP_PORT`=8080, `LOG_LEVEL`=info, `LOG_FORMAT`=text in development else json, `APP_ENV`=development,
   timeouts read/write/idle/shutdown = 15s/15s/60s/10s).
6. WHEN `config.Load` returns an error THEN the composition root SHALL log it and exit non-zero
   without starting the HTTP listener.

**Independent Test**: Call `config.LoadFrom(fakeGetenv)` with a table of env maps and assert the
returned `Config`/error — no server or DB needed.

---

### P1: Structured logging + request middleware ⭐ MVP

**User Story**: As an operator/developer, I want every HTTP request to emit one structured,
correlated log line and to log within handlers with request context so that I can trace behavior
and debug incidents.

**Why P1**: Observability is required before any user-facing feature; the middleware is the
single wiring point all modules inherit.

**Acceptance Criteria**:

1. WHEN the logger is constructed from `config.LogConfig` THEN the system SHALL produce a
   `*slog.Logger` using a JSON or text handler per `LOG_FORMAT` at the configured `LOG_LEVEL`.
2. WHEN an HTTP request completes THEN the middleware SHALL emit exactly one log record with
   method, path, response status, byte count, and duration.
3. WHEN a request carries a request id (chi `middleware.RequestID`) THEN the log record SHALL
   include it; WHEN absent THEN the record SHALL still be emitted without a request id.
4. WHEN a handler or use case calls `log.FromContext(ctx)` THEN the system SHALL return the
   request-scoped logger (or the default logger if none was injected).
5. WHEN logging THEN the system SHALL NEVER write `SESSION_SECRET` or `DATABASE_URL` credentials
   (LGPD / secret hygiene).

**Independent Test**: Drive `httptest` requests through `log.Middleware` with an in-memory slog
handler; assert one record with the expected fields — no DB needed.

---

### P1: Health check endpoint ⭐ MVP

**User Story**: As an orchestrator/operator, I want `GET /healthz` to confirm the process is up
and the database is reachable so that deploys and load balancers can gate traffic.

**Why P1**: The M0 target ("skeleton boots, serves, DB wired") is only verifiable through a
health probe; readiness prevents routing to an instance with a dead DB.

**Acceptance Criteria**:

1. WHEN `GET /healthz` is called and the DB responds to a ping THEN the system SHALL return
   HTTP 200 with a body indicating status ok and db ok.
2. WHEN the DB does not respond within the bounded probe timeout THEN the system SHALL return
   HTTP 503 with a body indicating degraded / db down, and SHALL NOT panic.
3. WHEN pinging the DB THEN the system SHALL use a bounded context timeout so a hung DB cannot
   stall the probe.
4. WHEN readiness is checked THEN the handler SHALL depend only on a small consumer-owned
   `Pinger` port (not on pgx directly).

**Independent Test**: Call `health.Handler(fakePinger)` via `httptest`; fake returns nil → 200,
fake returns error → 503.

---

### P2: `.env.example` for local development

**User Story**: As a new developer, I want a checked-in `.env.example` listing every variable
with placeholders and comments so that I can configure a local run in minutes.

**Why P2**: Not required for the binary to run in prod (real env vars), but essential DX and it
prevents undocumented config drift.

**Acceptance Criteria**:

1. WHEN a developer copies `.env.example` to `.env` and fills placeholders THEN the values SHALL
   satisfy `config.Load` (every required key present).
2. WHEN a new field is added to `Config` without a matching key in `.env.example` THEN a
   drift-guard test SHALL fail.

**Independent Test**: A unit test parses `.env.example` keys and asserts every required env var
name the loader reads is present.

---

## Edge Cases

- WHEN multiple required vars are missing at once THEN the system SHALL report them all in one
  aggregated error (not fail on the first).
- WHEN `DATABASE_URL` is malformed THEN validation SHALL fail fast at load (before any pool
  construction attempt by DATA).
- WHEN `APP_ENV=development` and `LOG_FORMAT` is unset THEN the default SHALL be human-readable
  text; otherwise json.
- WHEN the DB ping times out at `/healthz` THEN the probe SHALL return 503 promptly (bounded),
  logged at warn.
- WHEN `X-Request-ID` / chi request id is absent THEN the request log SHALL omit the field but
  still be emitted.
- WHEN config is read concurrently by many goroutines THEN it SHALL be safe (immutable value
  passed by copy; no shared mutable state).

---

## Requirement Traceability

**PRD / RF mapping:** This is a **foundation (infrastructure) feature — it implements no
functional requirement (RF01–RF13)**. It enables all of them. Sources: PROJECT.md §Tech Stack
+ §Constraints (Go/`chi`/Postgres, LGPD, <200 ms availability), STATE.md **AD-005** (pgx/sqlc,
sessions+bcrypt, testcontainers), ARCHITECTURE.md §5 (platform packages `config`, `log`, plus
`postgres`/`httpx`). NFRs honored: LGPD (no secret/PII in logs), low-overhead logging.

| Requirement ID | Story | Phase | Status |
| -------------- | ----- | ----- | ------ |
| RUN-01 | P1: Typed config | Design | In Tasks |
| RUN-02 | P1: Typed config | Design | In Tasks |
| RUN-03 | P1: Typed config | Design | In Tasks |
| RUN-04 | P1: Typed config | Design | In Tasks |
| RUN-05 | P1: Typed config | Design | In Tasks |
| RUN-06 | P2: `.env.example` | Design | In Tasks |
| RUN-07 | P1: Structured logging | Design | In Tasks |
| RUN-08 | P1: Structured logging | Design | In Tasks |
| RUN-09 | P1: Structured logging | Design | In Tasks |
| RUN-10 | P1: Health check | Design | In Tasks |
| RUN-11 | P1: Health check | Design | In Tasks |
| RUN-12 | P1: Typed config / wiring | Design | In Tasks |

**Requirement definitions:**

- **RUN-01** — Single typed `config.Config` holding env, HTTP settings, `DATABASE_URL`,
  `SessionSecret`, log settings.
- **RUN-02** — `Load()` reads process env (via injectable `getenv`) into `Config`; no global
  mutable state.
- **RUN-03** — Fail-fast aggregated validation of required vars; never returns partial config.
- **RUN-04** — Documented defaults for all optional vars.
- **RUN-05** — `SESSION_SECRET` required, min length 32 (consumed by identity/auth in M1).
- **RUN-06** — `.env.example` template enumerating every var, drift-guarded against `Config`.
- **RUN-07** — `log.New(cfg.Log)` builds a configured `*slog.Logger` (json/text, level).
- **RUN-08** — Request-logging middleware emits one correlated record per request.
- **RUN-09** — `log.FromContext`/`IntoContext` expose the request-scoped logger.
- **RUN-10** — `GET /healthz` → 200 on liveness + DB ping ok.
- **RUN-11** — `/healthz` → 503 on DB unreachable; bounded ping via consumer-owned `Pinger`.
- **RUN-12** — Composition root loads config first, wires logger + middleware + `/healthz`;
  config error aborts startup.

**Coverage:** 12 total, 12 mapped to tasks (see tasks.md), 0 unmapped.

---

## Success Criteria

- [ ] Boot with a valid env → server starts, `slog` emits a startup line; boot with a missing
      required var → process exits non-zero naming the var, listener never opens.
- [ ] `GET /healthz` returns 200 against a live Postgres testcontainer and 503 when the DB is
      unreachable.
- [ ] A single request produces exactly one structured request-log record with method, path,
      status, duration.
- [ ] `quick` gate green for config/log/health unit tests; `full` gate green for the readiness +
      wiring integration test.

---

## Open Decisions

- `.env` is **not** auto-loaded (no godotenv dependency); `.env.example` is a copy-me template,
  dev tooling sources `.env`. Prod uses real env vars.
- `/healthz` folds liveness + DB readiness into one endpoint and returns 503 when the DB is down;
  a split `/readyz` is deferred (trivial to add later).
- Validation is **aggregated fail-fast** (reports all offending vars at once) for better DX.
- `DATABASE_URL` is parsed/validated here (single env-parsing site) and injected into the DATA
  pool constructor; pool tuning params (max conns) stay with DATA unless later surfaced.
- `SESSION_SECRET` is the single source of truth for the cookie-signing secret; its rules
  (required, min length 32) live in **RUN-05** only. project-skeleton (SKEL-07) and authentication
  (AUTH) consume it as an injected seam and MUST NOT restate the constraint.
