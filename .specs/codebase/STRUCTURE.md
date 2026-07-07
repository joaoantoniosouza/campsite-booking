# Structure — Target Directory Layout

Concrete layout every design/tasks doc must target. Paths below are the contract; designs
reference exact file paths under these roots.

```
campsite-booking/
├── cmd/
│   └── server/
│       └── main.go                     # entrypoint: calls bootstrap, starts HTTP server
├── internal/
│   ├── platform/                       # technical cross-cutting (no business rules)
│   │   ├── bootstrap/                  # composition root: build + wire modules
│   │   ├── config/                     # env-based runtime config loading (M0)
│   │   ├── postgres/                   # pgx pool, tx helpers
│   │   ├── httpx/                      # chi router, middleware (logging, recovery, session)
│   │   ├── web/                        # base html/template layout, htmx helpers, static assets
│   │   └── log/                        # slog structured logging
│   ├── shared/                         # small shared kernel (domain primitives only)
│   │   ├── document/                   # CPF, CNPJ value objects
│   │   └── id/                         # Base62 code gen, UUID
│   └── modules/
│       └── <module>/                   # identity | campsites | config | availability | reservations | checkin | admin
│           ├── domain/                 # entities, VOs, aggregates, domain services, events, repo interfaces
│           ├── app/                    # use cases, application services, port interfaces, command/result DTOs
│           ├── adapter/
│           │   ├── repository/         # pgx/sqlc repo implementations (implements domain interfaces)
│           │   └── http/               # chi handlers + htmx template rendering
│           └── public/                 # interfaces + DTOs exposed to OTHER modules (the only importable surface)
├── db/
│   ├── migrations/                     # golang-migrate SQL files (NNNNNN_name.up.sql / .down.sql)
│   └── queries/                        # sqlc .sql sources (per module or shared)
├── web/
│   ├── templates/                      # html/template files (base layout + module partials)
│   └── static/                         # css, htmx.min.js, assets
├── sqlc.yaml
├── go.mod
└── .env.example
```

## Per-module package naming

- Go package name = leaf dir name. Import path root: `github.com/<org>/campsite-booking/internal/modules/<module>/<layer>`.
- Test files live **next to** the code they test (`foo.go` → `foo_test.go`), same package for
  white-box unit tests, `_test` package for black-box where it reads better.
- Integration tests: same directory, guarded with `//go:build integration`.

## Where a new feature's code goes

Most features extend an existing module. A feature design must state, per component, the exact
target path. Examples:

- Availability engine → `internal/modules/availability/{domain,app,adapter,public}`
- Reservation creation → `internal/modules/reservations/...` + consumes `availability/public`,
  `config/public`, `campsites/public`.
- Admin dashboard → `internal/modules/admin/...`, consuming other modules' `public` read APIs only.

## Migrations

- One migration per schema change, ordered, reversible. Owned conceptually by the module whose
  tables they create, but all live under `db/migrations/` (single migration stream for one DB).
- `sqlc` generates typed query code from `db/queries/` into each module's `adapter/repository`
  (or a generated package the repository wraps). Concurrency/locking SQL is written by hand in
  these query files.
