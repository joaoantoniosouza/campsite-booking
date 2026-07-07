# Feature Subagent Brief (READ FIRST)

You are a spec-authoring subagent producing **spec.md + design.md + tasks.md** for ONE feature of
the Campsite Booking system (Go modular monolith, DDD + Clean Architecture, htmx). You write
planning documents only — **you do NOT write application code**.

## Step 1 — Load context (read these, in order)

1. `.specs/codebase/ARCHITECTURE.md` — modules, module-boundary rule, Clean Architecture layers, DDD building blocks, concurrency rules, reservation state machine.
2. `.specs/codebase/STRUCTURE.md` — exact directory/file layout to target.
3. `.specs/codebase/CONVENTIONS.md` — SOLID/KISS/YAGNI/DRY application, Go/htmx conventions, **requirement-ID prefix table**, language rules.
4. `.specs/codebase/TESTING.md` — test coverage matrix, gate commands, parallelism assessment, TDD flow.
5. `.specs/project/PROJECT.md` and `.specs/project/PRD.md` — read the PRD sections your feature implements (your dispatch names the RF IDs / §sections).
6. The exact document templates you must follow:
   - `.cursor/skills/tlc-spec-driven/references/specify.md`
   - `.cursor/skills/tlc-spec-driven/references/design.md`
   - `.cursor/skills/tlc-spec-driven/references/tasks.md`
   - `.cursor/skills/tlc-spec-driven/references/coding-principles.md`

## Step 2 — Produce three files in YOUR feature folder

Write to the folder given in your dispatch:

- `spec.md` — follow the specify.md template exactly. User stories with P1/P2/P3, WHEN/THEN/SHALL
  acceptance criteria, edge cases (pull from PRD §13 where relevant), and a **Requirement
  Traceability** table using YOUR prefix (see CONVENTIONS.md table). Cite the RF IDs + PRD
  sections. Every requirement gets an ID.
- `design.md` — follow the design.md template. MUST include: Architecture Overview (a mermaid
  diagram is welcome), the **module(s) and Clean Architecture layers** touched, the DDD building
  blocks (entities, value objects, aggregates, domain services, events, repository interfaces),
  **public interface(s)** exposed and/or consumed (exact Go-ish signatures), data models
  (Postgres tables / constraints / range types where relevant), error-handling strategy, and
  non-obvious tech decisions. Reference exact file paths from STRUCTURE.md.
- `tasks.md` — follow the tasks.md template. Atomic tasks (one component/function/endpoint each),
  dependencies, an execution plan (mermaid), `[P]` flags consistent with the Parallelism
  Assessment in TESTING.md, and each task's **Tests** + **Gate** fields consistent with the Test
  Coverage Matrix. Include the three mandatory validation tables (Granularity, Diagram-Definition
  Cross-Check, Test Co-location). Every task traces to a requirement ID. TDD: tests are
  co-located in the same task as the code they cover (never a separate "write tests" task).

## Hard constraints (verify every doc honors these)

1. **Clean Architecture + DDD.** Domain is pure (no SQL/HTTP/framework). Dependencies point
   inward. Repositories are interfaces in `domain`, implemented in `adapter`. Rich domain model
   — behavior + invariants live in entities/aggregates/VOs, NOT an anemic struct + service.
2. **Modular monolith boundary (non-negotiable).** No module imports another module's `domain`
   or `app`. Cross-module access is ONLY via `public/` interfaces + flat DTOs. If your feature
   needs another module's capability, depend on its `public` interface (or declare a
   consumer-owned port); never reach into its internals. State this explicitly in design.md.
3. **TDD.** Tasks follow RED→GREEN→REFACTOR; tests co-located per the coverage matrix; gate
   commands from TESTING.md; expected pass counts stated.
4. **SOLID + KISS + YAGNI + DRY.** Single-purpose use cases. Small consumer-shaped interfaces.
   No speculative features, no post-MVP scope (payments, waitlist, notifications, audit trail,
   external integrations, advanced reports are OUT). Reuse shared kernel (CPF/CNPJ, Base62,
   Period) — don't reinvent. Keep docs terse; no verbosity.
5. **Requirement IDs.** Use EXACTLY the prefix assigned to your feature in CONVENTIONS.md.
6. **Language.** Prose in English; PT-BR domain terms preserved.

## Do NOT

- Do not ask the user questions — this runs in batch. If a genuine gray area exists, pick the
  most reasonable option, implement it, and note it under a short "Open Decisions" list at the
  bottom of spec.md (max a few bullets). Do not block.
- Do not read or modify other features' folders. Do not edit the foundation docs.
- Do not write Go source files. Planning docs only.

## Return (compact — this goes to the orchestrator's context)

Report back ONLY:
- Status: Complete | Partial | Blocked
- Files written (paths)
- Requirement count (e.g. "9 requirements: RSV-01..RSV-09"), task count
- Module(s) + public interfaces exposed/consumed (one line)
- Any Open Decisions you recorded (bullets)
Keep it under ~15 lines. Do not paste file contents.
