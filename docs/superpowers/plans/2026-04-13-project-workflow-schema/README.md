# Project & Workflow Schema — Phase Index

> **Initiative:** v0.10 / Project & Workflow Schema
> **Source spec:** `ROADMAP.md` §v0.10, `PRODUCT.md` §Projects, §Workflows
> **Drafted:** 2026-04-13

## Goal

Move projects and workflows from config-driven in-memory entities into the workspace SQLite database as first-class, version-controlled rows. Land DB-level foreign key integrity from `tasks` to `projects`, and re-type `domain.Task.ProjectID` as `uuid.UUID` so every entity has typed identity.

## Scope Boundaries

- **In scope:** new `workflows` and `projects` tables with optimistic locking, `sqlite.WorkflowRepo` / `sqlite.ProjectRepo` full-CRUD implementations, domain-type extensions (ID / Version / timestamps), `domain.Task.ProjectID` typing change, `tasks.project_id` FK migration, name→UUID plumbing through service / filter / TUI, and removal of the temporary `domain.Project.Workflow` compat field introduced in the middle of the sequence.
- **Out of scope (next initiative — "Service Layer Migration"):** deleting `inmem/project.go` and `inmem/workflow.go`, switching `ProjectService` / `WorkflowService` from `inmem` to `sqlite` repositories, removing `[projects.*]` / `[workflows.*]` from config, config-loader rejection of deprecated sections, CLI/MCP rewiring. None of these are touched here.
- **Never in scope:** backwards-compatible data migrations — per user directive, no existing deployments to preserve.

## Architecture

Three storage schemas (`workflows`, `projects`, `tasks`) mutate in lockstep. Workflows come first (projects FK-depends on them), projects second (tasks FK-depends on them), and the tasks table rebuild lands third along with the `domain.Task.ProjectID` typing change. A fourth phase cleans up the one piece of bridge code this plan introduces: the temporary `domain.Project.Workflow` string field used to keep `service/task.go` compiling during phase 2. Each phase ships compile-clean and test-green on its own, with the existing `inmem` repositories still wired into services so the running binary keeps using the config-seeded in-memory views while the SQLite side of the house is built underneath.

## Phase Summary

| # | Phase | Tasks | Ships |
|---|-------|-------|-------|
| 1 | Workflows Schema & SQLite Repo | 5 | New `workflows` table, extended `domain.Workflow`, `sqlite.WorkflowRepo` full CRUD |
| 2 | Projects Schema & SQLite Repo | 6 | New `projects` table, extended `domain.Project`, `sqlite.ProjectRepo` full CRUD, interface rename `GetByID`→`GetByName`, temporary `project.Workflow` compat field |
| 3 | Tasks FK & `ProjectID` Typing | 6 | `tasks` table rebuild with FK, `domain.Task.ProjectID` as `uuid.UUID`, service/filter/TUI resolution of names→UUIDs |
| 4 | Remove `project.Workflow` Compat Field | 5 | `WorkflowRepository.GetByID(uuid.UUID)`, `WorkflowService.GetByID`, `workflowName` helper in `service/task.go`, deletion of the compat field |

## Phase Documents

1. [Phase 1 — Workflows Schema & SQLite Repo](./phase-1-workflows-schema.md)
2. [Phase 2 — Projects Schema & SQLite Repo](./phase-2-projects-schema.md)
3. [Phase 3 — Tasks FK & `ProjectID` Typing](./phase-3-tasks-project-fk.md)
4. [Phase 4 — Remove `project.Workflow` Compat Field](./phase-4-remove-workflow-compat-field.md)

## Sequencing

Phases run strictly in order. Each depends on every prior phase and nothing else.

- Phase 1 has no prerequisites beyond `main` at drafting time (commit `f50bb24`).
- Phase 2 requires phase 1 merged — its migration seeds `_default` with `workflow_id` pointing at the kanban row inserted by phase 1's migration.
- Phase 3 requires phases 1 and 2 merged — its migration rebuilds `tasks` with FK to `projects.id`, which must already contain the seeded `_default` row.
- Phase 4 requires phases 1, 2, and 3 merged — it removes the compat field introduced in phase 2, and assumes `service/task.go` has already been re-typed by phase 3.

No phase may run in parallel with another.

## Global Invariants (hold at the end of every phase)

- `make build` and `make test` pass.
- `make test-race` passes.
- The `tusk` binary starts, runs `tusk task list`, creates/starts/completes a task, lists projects, lists workflows, and produces the same human-visible output it did before the initiative started.
- Existing E2E scenarios in `tests/e2e/` pass unchanged unless explicitly updated by the phase.
- `inmem.ProjectRepository` and `inmem.WorkflowRepository` continue to satisfy `repository.ProjectRepository` and `repository.WorkflowRepository` and remain the repos wired into `ProjectService` / `WorkflowService`.

## Implementer Agent Expectations

Each phase doc is the single authoritative directive for that phase's implementer. Implementers will see this index and the other phase docs in the repo for cross-reference only — they must not wait on or coordinate with another implementer. If a phase doc says something a reference doc does not, the phase doc wins.

When a phase is complete, the implementer commits per the plan's commit checkpoints and stops. The planning agent picks up the next phase.
