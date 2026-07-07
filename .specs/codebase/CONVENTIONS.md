# Conventions

Applies to every feature spec, design, and task. Keep specs terse and high-signal.

## Language

- **Spec/design/task prose: English.** **Domain terms: PT-BR preserved** (Acampamento, Diária,
  Responsável, Participante, Porteiro, Reserva Walk-in, No-show, Pendente, Expirada, Cancelada,
  Finalizada). See PROJECT.md §Constraints.

## SOLID / Clean Code / KISS / YAGNI / DRY (how they bind these specs)

- **SRP** — one reason to change per type. Use cases are single-purpose (`CreateReservation`,
  `CancelReservationRemotely`) — not a fat `ReservationService` with 12 methods.
- **OCP/DIP** — depend on interfaces (ports), inject implementations. Domain/app never `new` an
  adapter. This is what makes the module boundary and testability work.
- **ISP** — small, consumer-shaped interfaces. `availability/public.Checker` exposes only what
  reservations needs, not the whole availability API.
- **LSP** — fakes/mocks used in tests must honor the interface contract (incl. error semantics).
- **KISS/YAGNI** — build only what the roadmap feature asks. No speculative config, no
  abstractions for single-use code, no error handling for impossible states. Post-MVP items
  (payments, waitlist, notifications, audit trail) are OUT — never spec them in.
- **DRY** — shared primitives (CPF/CNPJ, Base62, Period) live once in the shared kernel; do not
  reimplement per module. But do NOT force-share context-specific rules (that would couple modules).

## Go conventions

- Errors: return `error`, wrap with `fmt.Errorf("...: %w", err)`. Define sentinel/domain errors
  in `domain` (e.g. `var ErrOverlappingReservation = errors.New(...)`). Handlers map domain
  errors → HTTP status + htmx-friendly message. No panics for control flow.
- Constructors validate and return `(T, error)`; invalid value objects are unrepresentable.
- Context: first param `ctx context.Context` on every use case and repository method.
- No global mutable state. Dependencies passed explicitly (constructor injection).
- DTOs crossing `public/` and `adapter/http`: flat structs of primitives/std types only.
- Time: inject a clock (`func() time.Time` or a `Clock` port) into use cases that read "now"
  (holds, deadlines, booking window) so they are deterministically testable.

## HTTP / htmx conventions

- `chi` router; routes mounted per module in the composition root.
- Handlers are thin: decode → validate → call use case → render template fragment or redirect.
  No business rules in handlers.
- htmx: return HTML fragments for partial swaps; full-page render on direct navigation. Base
  layout + partials in `web/templates/`.
- Sessions: cookie-based, `bcrypt` password hashing (identity module owns auth; other modules
  read the authenticated principal via middleware-populated context / `identity/public`).

## Requirement-ID prefixes (per feature — use EXACTLY these)

| Milestone | Feature folder | ID prefix |
| --------- | -------------- | --------- |
| M0 | project-skeleton | `SKEL` |
| M0 | data-migration | `DATA` |
| M0 | config-runtime | `RUN` |
| M1 | user-company-registration | `REG` |
| M1 | authentication | `AUTH` |
| M1 | campsite-management | `CAMP` |
| M1 | system-configuration | `CFG` |
| M2 | availability-engine | `AVL` |
| M2 | reservation-creation | `RSV` |
| M3 | reservation-lookup | `LKP` |
| M3 | participant-changes | `PRT` |
| M3 | responsibility-transfer | `XFR` |
| M3 | remote-cancellation | `CNL` |
| M4 | porteiro-checkin | `CHK` |
| M4 | walkin-reservation | `WLK` |
| M4 | onsite-cancellation | `OSC` |
| M5 | admin-dashboard | `DSH` |
| M5 | management-surfaces | `MGT` |

ID format: `PREFIX-NN` (e.g. `RSV-01`). Traceability table in every spec.

## Cross-feature references

When a feature depends on another module's capability, reference the **public interface** by
name (e.g. "consumes `availability/public.Checker`") and cite the providing feature — never
assume access to its domain/app internals. If the needed port does not exist yet, the design
declares the interface it needs (consumer-owned port) and notes which upstream feature implements it.

## Traceability to PRD

Every spec cites the RF IDs (RF01–RF13) and PRD §sections it implements. See ROADMAP.md for the
feature→RF mapping and PRD.md §6 (Regras de Negócio) / §11 (Requisitos Funcionais).
