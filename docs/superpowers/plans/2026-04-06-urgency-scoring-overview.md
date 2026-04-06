# Urgency Scoring — Phase Overview

**Design spec:** `docs/superpowers/specs/2026-04-06-urgency-scoring-design.md`

## Phase Summary

| Phase | Name | Tasks | Prerequisites | Key Deliverable |
|-------|------|-------|---------------|-----------------|
| 1 | Engine Core & Config | 4 | None (base codebase) | `UrgencyEngine` with all 10 factors, batch repo methods, config expansion |
| 2 | Wire Engine into Consumers | 4 | Phase 1 | Urgency scoring in `TaskService.List`, Urg column in TUI, E2E tests |
| 3 | Per-Project Overrides & `tusk next` | 5 | Phase 1, Phase 2 | Sparse merge overrides, `tusk next` CLI + MCP, bridge code cleanup |

## Dependency Graph

```
Phase 1 (Engine Core)
    ↓
Phase 2 (Wire into Consumers)
    ↓
Phase 3 (Per-Project Overrides & tusk next)
```

All phases are strictly sequential. No parallel execution is possible.

## Bridge Code Tracking

| Bridge Code | Introduced In | Removed In | Description |
|-------------|---------------|------------|-------------|
| Empty `ProjectWeights` map | Phase 2 (Task 2) | Phase 3 (Task 2) | `TaskService.List` passes empty project weights to `ScoringContext` |

## Spec Coverage Matrix

| Spec Requirement | Phase | Task |
|-----------------|-------|------|
| UrgencyEngine with 10 factors | Phase 1 | Task 4 |
| Sigmoid curve for due date | Phase 1 | Task 4 |
| Config expansion (5 new weights) | Phase 1 | Task 1 |
| Urgency field on domain.Task | Phase 1 | Task 2 |
| Batch count repo methods | Phase 1 | Task 3 |
| Integration into TaskService.List | Phase 2 | Task 2 |
| Urgency column in TUI | Phase 2 | Task 3 |
| Urgency in MCP taskResponse | Phase 1 | Task 2 |
| E2E tests for sort ordering | Phase 2 | Task 4 |
| Per-project urgency overrides | Phase 3 | Tasks 1-2 |
| Sparse merge of weights | Phase 3 | Task 2 |
| `tusk next` CLI command | Phase 3 | Task 4 |
| `tusk_task_next` MCP tool | Phase 3 | Task 5 |
| Configurable urgency weights from config | Phase 2 | Task 1 (DI wiring) |
