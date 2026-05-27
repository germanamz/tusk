# Concurrent Tusk Instances — Implementation Plan

**Status:** Draft **Date:** 2026-05-26 **Spec:**
`docs/superpowers/specs/2026-05-26-tusk-concurrent-instances.md`

## Purpose

This plan organizes the implementation of the concurrent-instances design into
independently shippable phases, each composed of independently shippable tasks.
It is the entry point for the work stream. Per-phase docs live in
`phase-N-*.md`; per-task docs live in `phase-N/task-N.M-*.md`.

## Execution Rules

These rules apply to every phase and every task in this plan. Each phase doc and
each task doc must restate them prominently so implementer agents cannot miss
them.

1. **One task = one PR.** Every task in every phase produces a single,
   self-contained pull request. Tasks do not stack changes; they ship.
2. **Every task is independently shippable.** After a task lands, `main` is
   deployable. Tests pass, lint passes, lefthook pre-commit (which runs the full
Go test suite) passes. No task may leave the tree red.
3. **Every task is compilation-safe in isolation.** If a task adds an interface
   that later tasks will consume, that task includes whatever bridge code
(stubs, no-op implementations, feature flags) is required to keep the build
green.
4. **Bridge code carries a removal target.** Any bridge introduced names the
   exact later task that removes it. The removing task lists the removal as an
explicit step.
5. **No "the next task will fix this."** Each task stands on its own. User-
   visible behavior carries forward unless the task explicitly deprecates it.
6. **Plan docs are reasoning, not copy-paste.** Code snippets in these documents
   exist only to clarify a data structure or invariant. Implementer agents read
the spec and the codebase for ground truth; they consult plan docs for the *why*
and the boundaries of each step.

## Sequencing

The phases must be executed in order. Tasks within a phase may be parallelized
only when the phase doc explicitly says so; the default is sequential.

| # | Phase                                    | Tasks | Prereqs |
|---|------------------------------------------|-------|---------|
| 1 | Precondition — drop `body` from `node_modify` | 1 | none |
| 2 | Schema and lease primitives              | 3 | Phase 1 |
| 3 | Embed queue lease-based draining         | 2 | Phase 2 |
| 4 | WRITE-BOTH handlers via `file_state` leases | 5 | Phase 2 + Phase 3 |
| 5 | Remove workspace flock from runtime      | 2 | Phase 3 + Phase 4 |
| 6 | Reindex parallelization                  | 4 | Phase 4 + Phase 5 |
| 7 | Worker opt-out and watch disable         | 3 | Phase 3 + Phase 6 |

Phase 4 depends on Phase 3 because the file_state lease in `WriteWithLease`
needs a TTL value, and `internal/leaseconfig` (env + manifest resolution)
ships in T3.2. Without it, Phase 4 would need its own hardcoded TTL bridge.
Phase 4 can begin as soon as Phase 3 is complete; the two phases touch
different subsystems and do not need to run truly in parallel.

Phase 7 needs Phase 3 (the embed worker pool exists to be governed) and
Phase 6 (T7.1 removes a bridge constant introduced by T6.4 for the
reindex worker pool — the bridge must exist for T7.1 to remove it).
Phase 7 does not need Phase 4 or Phase 5.

**Within-phase sequencing notes:**

- Phase 4: T4.2-T4.5 are declared parallelizable after T4.1, but T4.2/T4.3
  both edit `internal/node/service.go` and T4.4/T4.5 both edit
  `internal/node/rename.go`. To minimize rebase churn, dispatch
  T4.2+T4.4 in one batch and T4.3+T4.5 in a second batch. Truly
  parallel dispatch across all four is permitted but the second to
  merge will need to rebase.
- Phase 6 **must** land before Phase 7. T7.1 explicitly removes a
  bridge constant introduced by T6.4 (the hardcoded reindex worker
  count); the bridge must exist for the removal to be a no-op-vs-fix
  decision rather than a forward reference. Phase 7's prereq is
  declared as `Phase 3 + Phase 6` to reflect this.

## Phase Summaries

### Phase 1 — Precondition: drop `body` from `node_modify`

Single task. Removes the `body` parameter from `tusk_node_modify` (MCP) and the
`Body` field from `node.ModifyInput`. The CLI command never exposed body so it
needs no change. This narrows `node_modify` to frontmatter mutations only, which
is the invariant the concurrency design relies on (see spec § *Precondition:
Drop `body` from `node_modify`*).

### Phase 2 — Schema and lease primitives

Lands the schema groundwork for the work stream. Because each task is its own
PR (per the execution rules), users who pull mid-phase pay multiple rebuilds
during Phase 2 rollout — each schema-bumping task triggers a drop-and-rebuild
on first open after upgrade. This is accepted as the cost of keeping tasks
independently shippable.

The index schema version mechanism already exists
(`internal/index/schema_version.go` + `internal/workspace/indexopen.OpenOrRebuild`);
this phase reuses it. Each schema-changing task bumps the `SchemaVersion`
string constant; the existing `OpenOrRebuild` flow drops `.tusk/` and
reindexes on mismatch.

Introduces:

- The new `file_state` table and its repository.
- Lease columns on the existing `embed_queue` table, plus a `kind`
discriminator (`'embed'` | `'reindex'`) so the same queue carries both job
types — preempts the schema delta Phase 6 would otherwise need.
- The lease primitive (`Claim` / `Release` / stale-temp cleanup) and worker
identity (UUID generated at MCP startup).

After this phase, no handlers consume the new infrastructure yet — the
`file_state` table starts empty (it will be populated by reindex in Phase 6,
or lazily by handlers in Phase 4) and the lease helpers exist but are unused.
Build stays green.

### Phase 3 — Embed queue lease-based draining

Converts `EmbedQueueRepo.Drain` from its current select-then-delete shape to an
atomic lease claim returning rows. Adds release-on-success and
re-lease-on-failure paths, expired-lease reclamation, and TTL configuration via
env (`TUSK_LEASE_TTL_SECONDS`) and manifest (`lease.ttl_seconds`).

After this phase, embedding work can be claimed across processes. The workspace
flock still exists so concurrent MCP instances aren't yet possible, but the
underlying mechanism is ready.

### Phase 4 — WRITE-BOTH handlers via `file_state` leases

Introduces `node.WriteWithLease`, the shared helper that wraps claim → read →
mutate → stage in `.tusk/staging/<uuid>` → atomic rename → commit (`file_state`
update + lease release). Then converts each of the four WRITE-BOTH handler
families to use it: `node_create`, `node_modify`, `node_move`, `node_delete`.
Each handler conversion is one task / one PR.

Handlers continue to acquire the workspace flock through this phase; the flock
is the safety net that lets each conversion ship without coupling to flock
removal.

### Phase 5 — Remove workspace flock from runtime

Removes the runtime-lifetime flock from MCP startup
(`internal/mcp/runtime.go:96-107`) and the per-command flock from CLI mutation
paths (`cmd_node*.go`, `cmd_edge*.go`, `cmd_watch.go`, `cmd_doctor.go`). The
`internal/lock` package is retained for schema migrations, which is documented
in its package comment.

After this phase, concurrent MCP instances are functional. Reads, mutations, and
embed work all run cooperatively across processes.

### Phase 6 — Reindex parallelization

Replaces the single-process reindex flow with a generation-based, queue-driven
model:

- A `reindex_gen` counter in `meta` is bumped at walk start; per-file
`last_seen_gen` is updated as the walk encounters each file.
- The in-memory `seenPaths` reap (`internal/reindex/reindex.go:106,417-439`) is
replaced by generation-based reap with `os.Stat` confirmation.
- Per-file reindex jobs are enqueued during the walk.
- Workers drain those jobs in parallel, using the same lease primitives that
protect node mutations.

Two concurrent reindex walks become safe; reindex stops being a single critical
section.

### Phase 7 — Worker opt-out and watch disable

Adds the embed-worker pool configuration (env > manifest > default), wires
`workers=0` to also disable the watcher in that instance (so a non-draining
process does not produce work), and emits a startup `WARN` log explaining the
consequence and the operator's responsibility.

## Files in this Plan

```
docs/superpowers/plans/2026-05-26-concurrent-instances/
  PLAN.md                                     ← this file
  phase-1-precondition.md                     ← phase 1 overview
  phase-1/
    task-1.1-drop-node-modify-body.md
  phase-2-foundation.md
  phase-2/
    task-2.1-file-state-table.md
    task-2.2-embed-queue-lease-and-kind-columns.md
    task-2.3-lease-primitives-and-identity.md
  phase-3-embed-queue-leases.md
  phase-3/
    task-3.1-drain-lease-claim.md
    task-3.2-ttl-configuration.md
  phase-4-write-handlers.md
  phase-4/
    task-4.1-write-with-lease-helper.md
    task-4.2-node-create.md
    task-4.3-node-modify.md
    task-4.4-node-move.md
    task-4.5-node-delete.md
  phase-5-flock-removal.md
  phase-5/
    task-5.1-mcp-startup-flock.md
    task-5.2-cli-mutation-flock.md
  phase-6-reindex-parallel.md
  phase-6/
    task-6.1-generation-tracking.md
    task-6.2-generation-based-reap.md
    task-6.3-per-file-reindex-jobs.md
    task-6.4-parallel-drain.md
  phase-7-worker-opt-out.md
  phase-7/
    task-7.1-worker-config.md
    task-7.2-watch-disable-when-no-workers.md
    task-7.3-startup-warning.md
```

## Lifecycle

These plan docs are committed to the repository for the duration of the work
stream so implementer agents can reference them. After the final post-
implementation review confirms all phases shipped correctly, the
`docs/superpowers/plans/2026-05-26-concurrent-instances/` directory is removed
in a cleanup commit.

The spec under `docs/superpowers/specs/` is permanent and stays.
