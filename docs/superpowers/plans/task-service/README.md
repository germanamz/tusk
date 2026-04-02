# TaskService Implementation Plan — Index

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `TaskService` with CRUD operations, workflow validation, optimistic locking, and annotation management.

**Architecture:** TaskService sits in the service layer (`internal/service/`), depends on repository interfaces and a thin WorkflowService. Integration tests use real SQLite repos with in-memory databases, matching the existing test patterns in `internal/sqlite/`.

**Tech Stack:** Go 1.26, SQLite via `github.com/mattn/go-sqlite3`, UUIDs via `github.com/google/uuid`

**Spec:** `docs/superpowers/specs/2026-04-01-task-service-design.md`

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `internal/domain/task.go` | Modify | Add `TaskUpdate` struct |
| `internal/service/workflow.go` | Replace stub | `WorkflowService` with `IsTransitionAllowed`, `GetStatuses` |
| `internal/service/workflow_test.go` | Create | Integration tests for WorkflowService |
| `internal/service/task.go` | Replace stub | Full `TaskService` implementation |
| `internal/service/task_test.go` | Create | Integration tests for TaskService |

---

## Phases

Execute these phases **in order**. Each phase has its own document with detailed steps.

| Phase | Document | What it builds | Depends on |
|---|---|---|---|
| 1 | [01-domain-type.md](01-domain-type.md) | `TaskUpdate` struct in domain layer | Nothing |
| 2 | [02-workflow-service.md](02-workflow-service.md) | `WorkflowService` (TDD) | Nothing |
| 3 | [03-task-service-create.md](03-task-service-create.md) | `TaskService` struct, constructor, `Create` method (TDD) | Phase 1, Phase 2 |
| 4 | [04-task-service-read.md](04-task-service-read.md) | Read operations: `GetByShortID`, `GetByID`, `List`, `GetChildren`, `GetDescendants` (TDD) | Phase 3 |
| 5 | [05-task-service-update.md](05-task-service-update.md) | `Update` with partial updates, validation, workflow enforcement (TDD) | Phase 4 |
| 6 | [06-task-service-transitions.md](06-task-service-transitions.md) | `Start`, `Complete`, `Delete` convenience methods (TDD) | Phase 5 |
| 7 | [07-task-service-annotations.md](07-task-service-annotations.md) | `Annotate`, `GetAnnotations`, `DeleteAnnotation` (TDD) | Phase 6 |
| 8 | [08-full-suite-verification.md](08-full-suite-verification.md) | Run full test suite, verify no regressions | Phase 7 |

---

## Key Concepts for Implementers

### Test pattern

All tests in this project are **integration tests** using a real in-memory SQLite database. You do **not** mock repositories. The pattern is:

1. Create an in-memory SQLite store via `sqlite.New(":memory:", migrations.FS)`
2. Build real repo instances from the store's DB connection
3. Wire them into the service under test
4. Run operations through the service, assert results

### Optimistic locking

Every task has a `Version` field (starts at 1). When you update a task, the repository checks `WHERE version = ?`. If someone else wrote first, the version won't match and `ErrConflict` is returned. The service also does an early version check before doing validation work.

### Double-pointer pattern

For nullable fields that need "don't change / set null / set value" semantics:

```go
ParentID **uuid.UUID
```

- `nil` (outer pointer is nil) → don't change this field
- `&nilPtr` (outer non-nil, inner nil) → set to NULL
- `&&someUUID` (both non-nil) → set to this value

### Workflow validation

Status changes are validated against the project's workflow. The default workflow allows:
`pending→active`, `pending→deleted`, `active→completed`, `active→pending`, `active→deleted`, `completed→pending`.

Any transition not in this list is rejected with `ErrInvalidTransition`.
