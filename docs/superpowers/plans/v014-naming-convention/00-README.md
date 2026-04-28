# v0.14 — Naming and Spacing Convention: Plan Index

> Milestone: v0.14
> Spec: [`../../specs/2026-04-28-v0.14-naming-convention-design.md`](../../specs/2026-04-28-v0.14-naming-convention-design.md)
> Status: plan (pending review)

This directory contains the eight phase plan docs that implement the
v0.14 naming and spacing convention. Each phase doc is the
authoritative directive for the implementer agent assigned to that
phase. Plan docs are temporary — they will be removed from the
repository after the post-implementation review verifies the milestone.

## Phase Sequence and Dependencies

| Phase | Title | Prerequisites | Parallelizable |
|-------|-------|---------------|----------------|
| [1](01-convention-and-scaffold.md) | Convention doc + linter scaffold + rule 1 | Base codebase | No |
| [2](02-custom-analyzers.md) | Custom analyzers (rules 2, 3, 4) | Phase 1 | No |
| [3](03-sweep-service.md) | Sweep `service/` | Phase 2 | With 4–7 |
| [4](04-sweep-tui.md) | Sweep `internal/tui/` | Phase 2 | With 3, 5–7 |
| [5](05-sweep-mcp.md) | Sweep `internal/mcp/` + `internal/portability/` | Phase 2 | With 3, 4, 6, 7 |
| [6](06-sweep-filter-domain.md) | Sweep `filter/` + `domain/` + `syntax/` | Phase 2 | With 3–5, 7 |
| [7](07-sweep-rest.md) | Sweep `repository/` + `sqlite/` + `cmd/` + `tests/e2e/` + root | Phase 2 | With 3–6 |
| [8](08-lock-in.md) | Lock-in (regression guards) | Phases 1–7 | No |

```
Phase 1 ──▶ Phase 2 ──▶ Phases 3..7 ──▶ Phase 8
                       (any order, parallelizable)
```

## Compilation safety

Every phase compiles and tests pass at phase boundary. Phase 1 ships
a multichecker shell with no analyzers (bridge code; replaced in
Phase 2). Phases 1 and 2 ship full-codebase path exclusions (bridge
code; shrunk progressively in Phases 3–7, fully removed in Phase 8).
No phase leaves the codebase in a half-migrated or non-compiling state.

## Scope reminder

These plans implement the v0.14 milestone only — naming and spacing
convention plus the linter that enforces it. Behavior changes,
service-layer decomposition, parser hardening, and other items from
the v0.13 architecture audit are deferred to v0.15+ and are out of
scope for every phase below.
