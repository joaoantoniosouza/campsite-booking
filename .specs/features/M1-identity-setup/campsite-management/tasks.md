# Campsite Management Tasks

**Design**: `.specs/features/M1-identity-setup/campsite-management/design.md`
**Status**: Draft

All tasks follow TDD (RED → GREEN → REFACTOR); tests are co-located in the task that creates the
code (never deferred). Gate commands (TESTING.md): **quick** = `go build ./... && go vet ./... && go test ./...`;
**full** = `go test -tags=integration ./...`; **build** = `go build ./...`.

**Cross-feature notes:** `identity/public` (Administrator accessor) is authored by the sibling
`authentication` feature; T10 consumes it (or a consumer-owned `AdminGuard` adapted at the
composition root). The `campsites` migration number is the next free slot in the single gap-free
stream after `000001_baseline` — coordinate with sibling M1 migrations (`identity`, `config`) at
Execute time; `0000NN` below is a placeholder.

---

## Execution Plan

### Phase 1: Domain (Sequential — unit)

```
T1 (VOs + errors) ──→ T2 (Campsite aggregate + repo interface)
```

### Phase 2: App use cases (Parallel — unit)

```
        ┌─→ T3 (CreateCampsite)   [P]
T2 ─────┼─→ T4 (UpdateCampsite)   [P]
        ├─→ T5 (DeactivateCampsite)[P]
        └─→ T6 (Get/List queries) [P]
```

### Phase 3: Persistence & public (Sequential — integration)

```
T2 ──→ T7 (migration) ──→ T8 (pgx repo) ──→ T9 (public Provider)
T10 (admin authz guard)   [independent; integration ⇒ serial]
```

### Phase 4: Admin HTTP (Sequential — integration)

```
T6,T8,T10 ──────────────→ T11 (read handlers + templates)
T3,T4,T5,T8,T10 ────────→ T12 (write handlers + form templates)
```

### Phase 5: Wiring (Sequential — integration)

```
T9,T11,T12 ──→ T13 (composition-root wiring + routes)
```

- `[P]` only on T3–T6 (unit, depend solely on T2, mutually independent). All integration tasks
  (T7–T13) run serially per TESTING.md (shared Postgres container ⇒ not parallel-safe).

---

## Task Breakdown

### T1: Campsite value objects + sentinel errors

**What**: `Capacity` (`>=0`), `OverbookingPercent` (`>=0`, whole percent), `CampsiteStatus` (`Ativo`/`Inativo`) VOs with validating constructors; domain sentinel errors.
**Where**: `internal/modules/campsites/domain/{capacity.go,overbooking.go,status.go,errors.go}` (+ `*_test.go`)
**Depends on**: None
**Reuses**: stdlib only
**Requirement**: CAMP-02 (invariants), CAMP-04 (boundaries feed effective capacity)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `NewCapacity(-1)` / `NewOverbookingPercent(-1)` return wrapped errors; `>=0` succeed (**unit**, table-driven).
- [ ] `ParseCampsiteStatus("Ativo"|"Inativo")` succeed; any other string errors; `IsActive()` correct.
- [ ] Sentinels defined: `ErrEmptyName`, `ErrInvalidCapacity`, `ErrInvalidOverbooking`, `ErrInvalidStatus`, `ErrCampsiteNotFound`.
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: 6 unit cases pass (2 capacity, 2 overbooking, 2 status), no silent deletions.

**Verify**: `go test ./internal/modules/campsites/domain/ -run 'TestCapacity|TestOverbooking|TestStatus' -v` → PASS.

**Tests**: unit
**Gate**: quick

**Commit**: `feat(campsites): add capacity, overbooking and status value objects`

---

### T2: Campsite aggregate + factory + repository interface

**What**: `Campsite` aggregate (unexported fields, getters), `NewCampsite` factory (defaults status Ativo, non-empty name), `Reconstitute`, `EffectiveCapacity()`, `UpdateDetails`, `ChangeCapacity`, `ChangeOverbooking`, `Activate`, `Deactivate`; `CampsiteRepository` interface.
**Where**: `internal/modules/campsites/domain/{campsite.go,repository.go}` (+ `campsite_test.go`)
**Depends on**: T1
**Reuses**: T1 VOs, `internal/shared/id` (UUID)
**Requirement**: CAMP-01 (construct + defaults), CAMP-02 (name invariant), CAMP-03 (effective capacity), CAMP-04 (boundaries), CAMP-09 (Deactivate transition, idempotent)

**Tools**:

- MCP: NONE
- Skill: `tactical-ddd` (verify rich, non-anemic aggregate)

**Done when**:

- [ ] `NewCampsite` with blank name → `ErrEmptyName`; valid → status Ativo, ID set (**unit**).
- [ ] `EffectiveCapacity()` table cases: (100,10%)→110, (50,0%)→50, (0,25%)→0, (33,10%)→36 (**unit**).
- [ ] `Deactivate()` sets Inativo and is idempotent; `Activate()` sets Ativo; `ChangeCapacity`/`ChangeOverbooking`/`UpdateDetails` re-enforce invariants (**unit**).
- [ ] `CampsiteRepository` interface declares `Save`, `FindByID`, `List(onlyActive bool)`.
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: 7 unit cases pass (1 factory ok, 1 blank-name, 4 effective-capacity, 1 deactivate-idempotent), no silent deletions.

**Verify**: `go test ./internal/modules/campsites/domain/ -run TestCampsite -v` → PASS.

**Tests**: unit
**Gate**: quick

**Commit**: `feat(campsites): add Campsite aggregate with effective-capacity behavior`

---

### T3: CreateCampsite use case [P]

**What**: `CreateCampsite` use case + `CreateCampsiteCommand`/`CreateCampsiteResult`; builds VOs + aggregate (generate ID), `repo.Save`.
**Where**: `internal/modules/campsites/app/create_campsite.go`, `internal/modules/campsites/app/dto.go` (+ `create_campsite_test.go`)
**Depends on**: T2
**Reuses**: `domain` aggregate + repo interface; hand-written fake repo (LSP); `internal/shared/id`
**Requirement**: CAMP-01, CAMP-02

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Valid command → aggregate persisted via fake repo, result carries the new ID, status Ativo (**unit**).
- [ ] Invalid command (blank name / capacity < 0 / overbooking < 0) → error, `repo.Save` NOT called (**unit**).
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: 3 unit cases pass (happy, blank-name, negative-capacity), no silent deletions.

**Verify**: `go test ./internal/modules/campsites/app/ -run TestCreateCampsite -v` → PASS.

**Tests**: unit
**Gate**: quick

**Commit**: `feat(campsites): add CreateCampsite use case`

---

### T4: UpdateCampsite use case [P]

**What**: `UpdateCampsite` use case + `UpdateCampsiteCommand`; `FindByID`, mutate via aggregate methods, `Save`.
**Where**: `internal/modules/campsites/app/update_campsite.go` (+ `update_campsite_test.go`)
**Depends on**: T2
**Reuses**: `domain`, fake repo
**Requirement**: CAMP-07, CAMP-08

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Valid update mutates fields (incl. capacity/overbooking) and persists; new `EffectiveCapacity` reflected (**unit**).
- [ ] Update with invalid value → error, stored aggregate unchanged (fake asserts no bad Save) (**unit**).
- [ ] Unknown ID → `ErrCampsiteNotFound` propagated (**unit**).
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: 3 unit cases pass (happy, invalid-value, not-found), no silent deletions.

**Verify**: `go test ./internal/modules/campsites/app/ -run TestUpdateCampsite -v` → PASS.

**Tests**: unit
**Gate**: quick

**Commit**: `feat(campsites): add UpdateCampsite use case`

---

### T5: DeactivateCampsite use case [P]

**What**: `DeactivateCampsite` use case + `DeactivateCampsiteCommand`; `FindByID`, `Deactivate()`, `Save`.
**Where**: `internal/modules/campsites/app/deactivate_campsite.go` (+ `deactivate_campsite_test.go`)
**Depends on**: T2
**Reuses**: `domain`, fake repo
**Requirement**: CAMP-09, CAMP-10

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Ativo campsite → status Inativo persisted (**unit**).
- [ ] Deactivating already-Inativo → idempotent success, still Inativo (**unit**).
- [ ] Unknown ID → `ErrCampsiteNotFound` (**unit**).
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: 3 unit cases pass (deactivate, idempotent, not-found), no silent deletions.

**Verify**: `go test ./internal/modules/campsites/app/ -run TestDeactivateCampsite -v` → PASS.

**Tests**: unit
**Gate**: quick

**Commit**: `feat(campsites): add DeactivateCampsite use case`

---

### T6: GetCampsite + ListCampsites read use cases [P]

**What**: `GetCampsite` and `ListCampsites` read use cases + `CampsiteView` DTO (includes computed effective capacity); `ListCampsites(onlyActive bool)`.
**Where**: `internal/modules/campsites/app/queries.go` (+ `queries_test.go`); `CampsiteView` in `app/dto.go`
**Depends on**: T2
**Reuses**: `domain`, fake repo
**Requirement**: CAMP-05, CAMP-06

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `ListCampsites` returns a `CampsiteView` per campsite with correct effective capacity + status (**unit**).
- [ ] `GetCampsite` returns the view; unknown ID → `ErrCampsiteNotFound` (**unit**).
- [ ] `ListCampsites(onlyActive=true)` filters out Inativo (**unit**).
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: 3 unit cases pass (list-all, get + not-found, list-active-only), no silent deletions.

**Verify**: `go test ./internal/modules/campsites/app/ -run 'TestGetCampsite|TestListCampsites' -v` → PASS.

**Tests**: unit
**Gate**: quick

**Commit**: `feat(campsites): add GetCampsite and ListCampsites read use cases`

---

### T7: create_campsites migration

**What**: `0000NN_create_campsites.up.sql` (table + CHECK constraints mirroring invariants + `status` index) and `.down.sql` (drop table).
**Where**: `db/migrations/0000NN_create_campsites.up.sql`, `db/migrations/0000NN_create_campsites.down.sql`
**Depends on**: T2 (field/invariant alignment)
**Reuses**: golang-migrate stream + `pgtest.Setup` (DATA-04, DATA-09)
**Requirement**: CAMP-01, CAMP-02

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `up` creates `campsites` with columns per design + CHECKs (name not blank, capacity/overbooking ≥ 0, status ∈ {Ativo,Inativo}) + `campsites_status_idx` (**integration**).
- [ ] Inserting capacity `-1` / blank name / status `X` is rejected by the DB (**integration**).
- [ ] `down` drops the table; migration is reversible (**integration**).
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 3 integration cases pass (table+constraints present, constraint rejects, down reverts), no silent deletions.

**Verify**: `go test -tags=integration ./internal/modules/campsites/adapter/repository/ -run TestCampsitesMigration -v` → PASS.

**Tests**: integration
**Gate**: full

**Commit**: `feat(campsites): add campsites table migration`

---

### T8: pgx CampsiteRepository + sqlc queries

**What**: `db/queries/campsites.sql` (Insert/Update as upsert, GetByID, List, ListByStatus) + pgx `CampsiteRepository` implementing `domain.CampsiteRepository` (`Save` upsert via `ON CONFLICT`, `FindByID`→`Reconstitute`, `List(onlyActive)`), `updated_at` set on save.
**Where**: `db/queries/campsites.sql`, `internal/modules/campsites/adapter/repository/campsite_repository.go` (+ `campsite_repository_test.go`, `//go:build integration`)
**Depends on**: T2, T7
**Reuses**: `postgres.WithTx` + pool, generated sqlc, `pgtest.Setup`, `domain.Reconstitute`
**Requirement**: CAMP-01, CAMP-05, CAMP-06, CAMP-07, CAMP-09

**Tools**:

- MCP: `context7` (sqlc + pgx/v5 query API)
- Skill: NONE

**Done when**:

- [ ] `Save` inserts a new campsite; a second `Save` on the same ID updates it (upsert) and bumps `updated_at` (**integration**).
- [ ] `FindByID` reconstitutes the aggregate with correct VOs; unknown ID → `ErrCampsiteNotFound` (**integration**).
- [ ] `List(false)` returns all; `List(true)` returns only Ativo (**integration**).
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 4 integration cases pass (insert, upsert-update, find/not-found, list-filter), no silent deletions.

**Verify**: `go test -tags=integration ./internal/modules/campsites/adapter/repository/ -run TestCampsiteRepository -v` → PASS.

**Tests**: integration
**Gate**: full

**Commit**: `feat(campsites): add pgx CampsiteRepository with sqlc queries`

---

### T9: public.Provider interface + DTOs + app implementation

**What**: `campsites/public` (`Provider` interface, `CampsiteDTO`, `ErrNotFound`) + `app/provider.go` implementing it (`EffectiveCapacity`, `ActiveCampsites`, `GetCampsite`), mapping domain → DTO and `domain.ErrCampsiteNotFound` → `public.ErrNotFound`.
**Where**: `internal/modules/campsites/public/provider.go`, `internal/modules/campsites/app/provider.go` (+ `provider_test.go`, `//go:build integration`)
**Depends on**: T2, T8
**Reuses**: `domain`, real repo (T8), `pgtest.Setup`
**Requirement**: CAMP-11, CAMP-12

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Through the `public.Provider` only (no domain import in the test): `EffectiveCapacity(id)` returns the computed int; unknown ID → `public.ErrNotFound` (**integration**).
- [ ] `ActiveCampsites` returns only Ativo campsites as `CampsiteDTO` (primitives), Inativo excluded (**integration**).
- [ ] `GetCampsite` returns a DTO for any status; unknown → `ErrNotFound` (**integration**).
- [ ] No domain type appears in any `public` signature (compile-enforced: `public` imports stdlib only).
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 3 integration cases pass (effective-capacity + not-found, active-filter, get), no silent deletions.

**Verify**: `go test -tags=integration ./internal/modules/campsites/app/ -run TestProvider -v` → PASS.

**Tests**: integration
**Gate**: full

**Commit**: `feat(campsites): expose public Provider (effective capacity + active campsites)`

---

### T10: Administrator authorization guard

**What**: `authz.go` chi middleware reading the authenticated principal from `identity/public` (or consumer-owned `AdminGuard`) and allowing only Administrators; renders 403 / redirect otherwise.
**Where**: `internal/modules/campsites/adapter/http/authz.go` (+ `authz_test.go`, `//go:build integration`)
**Depends on**: None (consumes external `identity/public`)
**Reuses**: `identity/public` principal accessor; `internal/platform/web` renderer
**Requirement**: CAMP-13

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Request with an Administrator principal → next handler runs (**integration**, httptest with a stub handler).
- [ ] Request with a non-admin / absent principal → 403 (or redirect), next handler NOT run (**integration**).
- [ ] Guard imports only `identity/public` (no identity internals).
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 2 integration cases pass (admin allowed, non-admin denied), no silent deletions.

**Verify**: `go test -tags=integration ./internal/modules/campsites/adapter/http/ -run TestAdminGuard -v` → PASS.

**Tests**: integration
**Gate**: full

**Commit**: `feat(campsites): add Administrator authorization guard`

---

### T11: Admin read handlers + templates (list, detail)

**What**: `GET /admin/campsites` (list) and `GET /admin/campsites/{id}` (detail) handlers rendering htmx full-page/fragment via `web.Renderer`; `list.html`, `detail.html`, `row.html` templates. 404 on unknown ID.
**Where**: `internal/modules/campsites/adapter/http/handler.go`, `web/templates/campsites/{list.html,detail.html,row.html}` (+ `handler_read_test.go`, `//go:build integration`)
**Depends on**: T6, T8, T10
**Reuses**: `app` read use cases (T6), repo (T8), guard (T10), `web.Renderer`
**Requirement**: CAMP-05, CAMP-06, CAMP-13

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Seeded campsites render in the list with name, capacity, overbooking %, effective capacity, status (**integration**, real DB via harness, admin principal).
- [ ] Detail renders one campsite; unknown ID → 404 (**integration**).
- [ ] Routes are behind the Administrator guard (non-admin → denied) (**integration**).
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 3 integration cases pass (list, detail, not-found), no silent deletions.

**Verify**: `go test -tags=integration ./internal/modules/campsites/adapter/http/ -run TestCampsiteReadHandlers -v` → PASS.

**Tests**: integration
**Gate**: full

**Commit**: `feat(campsites): add admin list and detail htmx handlers`

---

### T12: Admin write handlers + form templates (create, update, deactivate)

**What**: `GET /admin/campsites/new`, `POST /admin/campsites` (create), `GET /admin/campsites/{id}/edit`, `PUT /admin/campsites/{id}` (update), `POST /admin/campsites/{id}/deactivate` handlers; `form.html`. Decode → validate → use case → fragment/redirect; validation errors → 422 fragment.
**Where**: `internal/modules/campsites/adapter/http/handler.go` (extend), `web/templates/campsites/form.html` (+ `handler_write_test.go`, `//go:build integration`)
**Depends on**: T3, T4, T5, T8, T10
**Reuses**: `app` write use cases (T3–T5), repo (T8), guard (T10), `web.Renderer`
**Requirement**: CAMP-01, CAMP-02, CAMP-07, CAMP-08, CAMP-09, CAMP-10, CAMP-13

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `POST` valid form → campsite persisted (verified via repo), appears in list; status Ativo (**integration**).
- [ ] `POST`/`PUT` with blank name or negative capacity → 422 fragment, nothing persisted/changed (**integration**).
- [ ] `PUT` updates fields incl. capacity/overbooking; `POST .../deactivate` sets Inativo (**integration**).
- [ ] Routes behind the Administrator guard (non-admin → denied) (**integration**).
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 4 integration cases pass (create-ok, create-invalid, update, deactivate), no silent deletions.

**Verify**: `go test -tags=integration ./internal/modules/campsites/adapter/http/ -run TestCampsiteWriteHandlers -v` → PASS.

**Tests**: integration
**Gate**: full

**Commit**: `feat(campsites): add admin create, update and deactivate htmx handlers`

---

### T13: Composition-root wiring + route mounting

**What**: `campsites` `bootstrap.Module` impl: construct repo → use cases → `public.Provider` → handlers, adapt `identity/public` into the admin guard, mount routes; expose `public.Provider` for M2/M5 wiring.
**Where**: `internal/platform/bootstrap/` (extend; e.g. `campsites.go` module wiring)
**Depends on**: T9, T11, T12
**Reuses**: `bootstrap.Module` seam (SKEL), `postgres` pool, `identity/public`, all campsites layers
**Requirement**: CAMP-13 (guard wired), CAMP-11/CAMP-12 (provider exposed), CAMP-01/05–10 (routes live)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Bootstrap constructs the module and mounts `/admin/campsites*` behind the guard (**integration**, full app via `App.Handler()`).
- [ ] End-to-end through the mounted router (admin principal): create → list → edit → deactivate succeeds (**integration**).
- [ ] `campsites/public.Provider` is retrievable from the composition root for downstream modules (**integration**).
- [ ] Gate check passes: `go test -tags=integration ./...`
- [ ] Test count: 2 integration cases pass (routes mounted + guarded, end-to-end CRUD), no silent deletions.

**Verify**: `go test -tags=integration ./internal/platform/bootstrap/ -run TestCampsitesWiring -v` → PASS.

**Tests**: integration
**Gate**: full

**Commit**: `feat(campsites): wire campsites module into the composition root`

---

## Parallel Execution Map

```
Phase 1 (Sequential — unit):
  T1 ──→ T2

Phase 2 (Parallel — unit, all depend only on T2):
  T2 ──┬── T3 [P]
       ├── T4 [P]
       ├── T5 [P]
       └── T6 [P]

Phase 3 (Sequential — integration):
  T2 ──→ T7 ──→ T8 ──→ T9
  T10  (independent; integration ⇒ runs serially in this window)

Phase 4 (Sequential — integration):
  T6,T8,T10 ──→ T11
  T3,T4,T5,T8,T10 ──→ T12

Phase 5 (Sequential — integration):
  T9,T11,T12 ──→ T13
```

**Parallelism constraint:** `[P]` requires no unfinished deps, a parallel-safe test type, and no
shared mutable state. Only **T3–T6** qualify (unit, depend solely on T2, mutually independent).
T7–T13 are integration ⇒ serial per TESTING.md (shared Postgres container / host is the bottleneck),
regardless of code independence (e.g. T10 has no code dep but still runs serially).

---

## Task Granularity Check

| Task | Scope | Status |
| ---- | ----- | ------ |
| T1: VOs + errors | 3 cohesive VOs + sentinels (1 concept: domain primitives) | ✅ Granular |
| T2: Campsite aggregate + repo interface | 1 aggregate + its port (cohesive) | ✅ Granular |
| T3: CreateCampsite | 1 use case | ✅ Granular |
| T4: UpdateCampsite | 1 use case | ✅ Granular |
| T5: DeactivateCampsite | 1 use case | ✅ Granular |
| T6: Get/List queries | 2 cohesive read use cases + shared view DTO | ✅ Granular |
| T7: migration | 1 migration (up+down) | ✅ Granular |
| T8: pgx repo + queries | 1 repository impl + its SQL | ✅ Granular |
| T9: public Provider | 1 port + its impl | ✅ Granular |
| T10: admin guard | 1 middleware | ✅ Granular |
| T11: read handlers | 2 cohesive GET endpoints (one resource, read side) | ✅ Granular |
| T12: write handlers | 3 cohesive write endpoints (one resource, write side) | ✅ Granular |
| T13: wiring | 1 composition-root module | ✅ Granular |

Multi-item tasks (T1 VOs, T6 read queries, T11/T12 handler groups) are cohesive around a single
concept and tested together — within the "2–3 related things if cohesive" allowance. HTTP handlers
are split read/write so no task spans the whole controller.

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| ---- | ---------------------- | ------------- | ------ |
| T1 | None | root (→ T2) | ✅ Match |
| T2 | T1 | `T1 → T2` (→ T3–T7) | ✅ Match |
| T3 | T2 | `T2 → T3 [P]` | ✅ Match |
| T4 | T2 | `T2 → T4 [P]` | ✅ Match |
| T5 | T2 | `T2 → T5 [P]` | ✅ Match |
| T6 | T2 | `T2 → T6 [P]` (→ T11) | ✅ Match |
| T7 | T2 | `T2 → T7` (→ T8) | ✅ Match |
| T8 | T2, T7 | `T7 → T8` (T2 upstream) (→ T9,T11,T12) | ✅ Match |
| T9 | T2, T8 | `T8 → T9` (→ T13) | ✅ Match |
| T10 | None (external `identity/public`) | independent (→ T11, T12) | ✅ Match |
| T11 | T6, T8, T10 | `T6,T8,T10 → T11` | ✅ Match |
| T12 | T3, T4, T5, T8, T10 | `T3,T4,T5,T8,T10 → T12` | ✅ Match |
| T13 | T9, T11, T12 | `T9,T11,T12 → T13` | ✅ Match |

- Every `Depends on` has a matching arrow; every arrow maps to a `Depends on`.
- `[P]` tasks (T3–T6) depend only on T2, never on each other. ✅
- Integration tasks carry no `[P]` even where code-independent (T10). ✅

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| ---- | --------------------------- | --------------- | --------- | ------ |
| T1 | `domain` (VOs) | unit | unit | ✅ OK |
| T2 | `domain` (aggregate + repo interface) | unit | unit | ✅ OK |
| T3 | `app` (use case) | unit | unit | ✅ OK |
| T4 | `app` (use case) | unit | unit | ✅ OK |
| T5 | `app` (use case) | unit | unit | ✅ OK |
| T6 | `app` (read use cases) | unit | unit | ✅ OK |
| T7 | `db/migrations` | integration | integration | ✅ OK |
| T8 | `adapter/repository` | integration | integration | ✅ OK |
| T9 | `public` impl (+ `app`) | integration | integration | ✅ OK |
| T10 | `adapter/http` (middleware) | integration | integration | ✅ OK |
| T11 | `adapter/http` (handlers) | integration | integration | ✅ OK |
| T12 | `adapter/http` (handlers) | integration | integration | ✅ OK |
| T13 | `internal/platform/bootstrap` wiring | integration | integration | ✅ OK |

- No task uses `Tests: none` (every task creates a code layer with a required test type).
- Tasks creating multiple layers (T9 = `public` + `app`; T11/T12 = handlers + templates) use the
  **highest** required test type (integration). ✅
- Every requirement CAMP-01…CAMP-13 is covered by ≥1 task, and each task cites its requirement IDs. ✅
