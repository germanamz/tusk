# Phase 1 — Repository Interface Expansion

## Prerequisites

Base codebase at `main` (commit `925306e` or later). No prior phase required.

## Objective

Expand `ProjectRepository` and `WorkflowRepository` to include write methods plus `CountProjectsByWorkflow`, without changing any caller. Both implementations (`sqlite/` and `inmem/`) must satisfy the expanded interfaces. `sqlite/` already implements most of these as concrete methods — this phase promotes them to the interface. `inmem/` gets bridge stubs.

The phase is a pure plumbing change: after it applies, `go vet ./...`, `go test ./...`, and the e2e suite all pass, and user-visible behavior is identical to `main`.

## Tasks

### Task 1 — Expand `ProjectRepository` interface

Edit `repository/project.go`. Add the following methods to the `ProjectRepository` interface (keep the existing `GetByID`, `GetByName`, `List`):

```go
Create(ctx context.Context, p *domain.Project) error
Update(ctx context.Context, p *domain.Project) error
Delete(ctx context.Context, id uuid.UUID, expectedVersion int64) error
CountProjectsByWorkflow(ctx context.Context, workflowID uuid.UUID) (int, error)
```

Signatures must match `sqlite/project.go`:
- `Create` mirrors `(*ProjectRepo).Create(ctx, *domain.Project) error` at `sqlite/project.go:29`.
- `Update` mirrors `(*ProjectRepo).Update(ctx, *domain.Project) error` at `sqlite/project.go:129`. The concrete method performs optimistic locking via `WHERE id = ? AND version = ?`; the interface just carries the method.
- `Delete` accepts the expected version for optimistic locking. If the concrete method at `sqlite/project.go:182` currently takes only `id`, extend it in this task to `(ctx, id, expectedVersion)` and update its single SQLite test.
- `CountProjectsByWorkflow` mirrors `(*ProjectRepo).CountByWorkflow(ctx, uuid.UUID) (int, error)` at `sqlite/project.go:167`. **Rename** the concrete method to `CountProjectsByWorkflow` so it matches the interface name exactly. Update any internal callers (expected: none outside the same file at this stage).

Remove any `// Phase 2: add Create/Update/Delete` TODO comments on the interface (currently around `repository/project.go:11`).

### Task 2 — Expand `WorkflowRepository` interface

Edit `repository/workflow.go`. Add to the `WorkflowRepository` interface:

```go
Create(ctx context.Context, w *domain.Workflow) error
Update(ctx context.Context, w *domain.Workflow) error
Delete(ctx context.Context, id uuid.UUID, expectedVersion int64) error
```

Signatures must match the existing concrete methods at `sqlite/workflow.go:29`, `:135`, and `:177`. Adjust `Delete` to take `expectedVersion` the same way as in Task 1.

Do **not** add `CountProjectsByWorkflow` to the workflow repo — it lives on `ProjectRepository` by design (project rows hold the FK).

### Task 3 — Verify SQLite implementations satisfy the interfaces

Add a compile-time assertion at the bottom of each SQLite file:

```go
// sqlite/project.go
var _ repository.ProjectRepository = (*ProjectRepo)(nil)

// sqlite/workflow.go
var _ repository.WorkflowRepository = (*WorkflowRepo)(nil)
```

Run `go build ./...` and fix any signature drift surfaced by the assertions (most likely the `Delete` version parameter change from Task 1 / Task 2).

### Task 4 — Add `inmem` bridge stubs

Edit `inmem/project.go` and `inmem/workflow.go`. The `inmem` repositories are read-only today, so after Task 1 and Task 2 they no longer satisfy the interface. Add stub implementations that return a sentinel error:

```go
// inmem/project.go
func (r *ProjectRepository) Create(context.Context, *domain.Project) error {
    return domain.ErrReadOnlyRepository
}
func (r *ProjectRepository) Update(context.Context, *domain.Project) error {
    return domain.ErrReadOnlyRepository
}
func (r *ProjectRepository) Delete(context.Context, uuid.UUID, int64) error {
    return domain.ErrReadOnlyRepository
}
func (r *ProjectRepository) CountProjectsByWorkflow(context.Context, uuid.UUID) (int, error) {
    return 0, domain.ErrReadOnlyRepository
}
```

Mirror the same stubs in `inmem/workflow.go` for `Create` / `Update` / `Delete`.

Add `ErrReadOnlyRepository = errors.New("repository is read-only")` to `domain/errors.go` if it does not exist. Include the same `var _ repository.ProjectRepository = (*ProjectRepository)(nil)` assertion at the bottom of each inmem file to prevent regression.

**Bridge tag:** These stubs are bridge code. They exist solely so `inmem` compiles through Phases 2–4 until it is deleted in **Phase 5**. Do not extend them to do real work.

### Task 5 — Tests

- Run `make test` and `make test-race`. No test should need to change — existing service tests only call read methods, and the stubs are never called in the read path.
- Add one SQLite-level test in `sqlite/project_test.go` that calls `CountProjectsByWorkflow` and asserts it returns the expected count for a freshly created workflow with N projects attached, if no such test exists.
- Add one SQLite-level test for the extended `Delete(id, expectedVersion)` signature on both project and workflow repos — a version mismatch must return `domain.ErrConflict`.

## User-Visible Behavior (Acceptance Criteria)

After this phase:

- Every existing `tusk project ...`, `tusk workflow ...`, `tusk task ...` CLI invocation still works identically.
- Every MCP tool call still works identically.
- `config show`, `config get`, `config set` are unchanged.
- `go build`, `go vet`, `make test`, `make test-race`, `make test-e2e` all pass.

## Changes Introduced

**New interface methods:**
- `ProjectRepository.Create`, `.Update`, `.Delete(id, expectedVersion)`, `.CountProjectsByWorkflow`
- `WorkflowRepository.Create`, `.Update`, `.Delete(id, expectedVersion)`

**Modified concrete methods:**
- `sqlite.(*ProjectRepo).CountByWorkflow` renamed to `CountProjectsByWorkflow`.
- `sqlite.(*ProjectRepo).Delete` and `sqlite.(*WorkflowRepo).Delete` now take `expectedVersion int64`.

**New files:** none.

**Bridge code introduced:**
- `inmem.(*ProjectRepository).{Create,Update,Delete,CountProjectsByWorkflow}` returning `domain.ErrReadOnlyRepository`. **Removed in Phase 5** (whole file deleted).
- `inmem.(*WorkflowRepository).{Create,Update,Delete}` returning `domain.ErrReadOnlyRepository`. **Removed in Phase 5**.

**New sentinel error:** `domain.ErrReadOnlyRepository` (if not already defined).

**Schema migrations:** none.

**New dependencies:** none.

**New environment variables:** none.
