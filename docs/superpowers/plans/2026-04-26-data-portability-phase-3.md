# Data Portability — Phase 3: PortabilityService

**Initiative:** v0.13 — Data Portability
**Spec:** `docs/superpowers/specs/2026-04-26-data-portability-design.md`
**Phase:** 3 of 4
**Prerequisites:** Phase 1 (Foundations) AND Phase 2 (Codec) must both be complete and merged.
**Can run in parallel with:** Nothing — this phase depends on both prior phases' outputs.

---

## Inherits From

After Phases 1 and 2:

- **From Phase 1:**
  - `service.WriteTx` exposes `Tasks()`, `Relations()`, `Events()`, `Projects()`, `Workflows()`, `Players()`, `Tags()`, `Annotations()`, `Notes()`, and `TruncateAll(ctx)`. Every entity kind portability writes is reachable inside one transaction; `TruncateAll` wipes every entity table in reverse-FK order.
  - `*sqlite.Tx` has matching accessors and a `TruncateAll(ctx)` implementation.
  - `client.go`'s `sqliteWriteTx` adapter implements all ten `WriteTx` methods.
  - `domain.EventWorkspaceImported`, `domain.EntityWorkspace`, and `domain.WorkspaceImportedPayload` exist in `domain/event_portability.go`. Payload satisfies the `EventPayload` interface.
- **From Phase 2:**
  - `internal/portability` exposes `PortableWorkspace`, all per-entity DTOs (`PortableTask` etc.), `SchemaVersion`, `Encode`, `Decode`, `ImportIssue`, `ImportError`. The codec is self-contained and unit-tested.

You can rely on all of the above being in place. If anything in this list is missing, stop and verify the prior phases landed cleanly before continuing.

---

## Why this phase

This phase implements `service.PortabilityService` — the orchestration layer that wires the codec (Phase 2) to the database (via Phase 1's `WriteTx`). It owns:

- **Export:** read every entity from the workspace into a `portability.PortableWorkspace` value.
- **Import validation:** schema, FK, taxonomy, blocks-cycle, workflow well-formedness, and collision checks. Collects every issue before returning.
- **Import apply:** FK-ordered INSERT/UPSERT inside a single `WriteTx`, with optional `--truncate` mode and the parent-fixup second pass for tasks. Emits exactly one `workspace_imported` event.
- **`Client.Portability`:** library consumers call `client.Portability.Export(ctx)` and `client.Portability.Import(ctx, ws, opts)` — the same API the CLI will use in Phase 4.

After this phase, the feature is fully functional through the library API. CLI commands land in Phase 4.

---

## Tasks

### Task 1 — Create `service/portability.go` with the type, constructor, and option/report types

**New file:** `service/portability.go`

```go
package service

import (
	"context"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/internal/portability"
	"github.com/google/uuid"
)

// PortabilityService orchestrates workspace-wide Export and Import.
// It reads through the existing per-entity services and the RepoBundle
// for entity kinds that lack a service (annotations, raw events).
// Writes go through WriteTxProvider so the entire import is atomic.
type PortabilityService struct {
	writeTx     WriteTxProvider
	tasks       *TaskService
	projects    *ProjectService
	workflows   *WorkflowService
	relations   *RelationService
	tags        *TagService
	players     *PlayerService
	notes       *NoteService
	bundle      *RepoBundle
	tuskVersion string
}

func NewPortabilityService(
	writeTx WriteTxProvider,
	tasks *TaskService,
	projects *ProjectService,
	workflows *WorkflowService,
	relations *RelationService,
	tags *TagService,
	players *PlayerService,
	notes *NoteService,
	bundle *RepoBundle,
	tuskVersion string,
) *PortabilityService {
	return &PortabilityService{
		writeTx:     writeTx,
		tasks:       tasks,
		projects:    projects,
		workflows:   workflows,
		relations:   relations,
		tags:        tags,
		players:     players,
		notes:       notes,
		bundle:      bundle,
		tuskVersion: tuskVersion,
	}
}

// ImportOptions controls Import behavior. Zero value = strict mode:
// fail on any collision, no truncation, full apply.
type ImportOptions struct {
	Replace  bool // row-level upsert on collision
	Truncate bool // wipe-and-restore mode; requires Replace
	DryRun   bool // run validation pass; report counts; no writes
}

// ImportReport summarizes what an Import did. Counts populate even on
// DryRun. Replaced counts the number of rows updated under --replace
// (not net-new inserts). Truncated reflects whether tables were wiped.
// EventID is the workspace_imported event ID; uuid.Nil on DryRun.
type ImportReport struct {
	Workflows, Projects, Players, Tags, Tasks,
	Relations, Annotations, Notes, Events int

	Replaced  int
	Truncated bool
	EventID   uuid.UUID
}
```

The struct fields and method names match the spec exactly. Do not invent additional helpers or convenience methods this task — keep `service/portability.go` to the type, constructor, and option/report types only. Logic comes in subsequent tasks.

### Task 2 — Implement `Export`

**File:** `service/portability.go` (continue in the same file or split as you prefer).

```go
// Export reads the entire workspace into a PortableWorkspace value.
// It does not wrap the read in a transaction — under SQLite WAL,
// concurrent writers may produce a slightly inconsistent dump.
// Workspaces using portability for backup should pause writers themselves.
// (See spec → "Known limitation".)
func (s *PortabilityService) Export(ctx context.Context) (*portability.PortableWorkspace, error)
```

Implementation:

1. List every entity kind via the existing service / repo accessors (in any order — the result is a snapshot, not a stream):
   - Workflows: `s.workflows.List(ctx)`.
   - Projects: `s.projects.List(ctx)` (or whichever method `ProjectService` exposes that returns every project).
   - Players: `s.players.List(ctx)` or `s.bundle.Players.List(ctx)`.
   - Tags: `s.tags.List(ctx)` (returns `[]TagWithUsage`; extract the `.Tag` field for the wire shape) or use `s.bundle.Tags.List(ctx)` directly to skip the usage count.
   - Tasks: list every task across every project. The `TaskService.List` API takes a `domain.FilterExpr`; build an empty / "match all statuses" filter so terminal tasks (completed, deleted) are included — backups must round-trip terminal tasks. If the simplest path is to call `s.bundle.Tasks.List(ctx, alwaysTrueFilter)` directly, do so; the comment in the file should explain why we bypass the service-level default-status filter (because portability is workspace-wide, not user-facing).
   - Relations: iterate every task and call `s.bundle.Relations.GetByTask(ctx, taskID)`, deduping by `Relation.ID`. The repository does not currently have a `ListAll` method; do not add one — dedupe in memory.
   - Annotations: similar story — iterate tasks and call `s.bundle.Annotations.List(ctx, taskID)` (check the actual signature; if it differs, adapt).
   - Notes: `s.notes.List(ctx, NoteListParams{IncludeArchived: true, AllPlayers: true, ...})` or whichever shape returns every note across every player and project. Verify in `service/note.go` what the signature is.
   - Events: `s.bundle.Events().List(ctx, EventFilter{})` — the empty filter returns everything (subject to `Limit`, which defaults to "no limit" when zero).

2. Convert each `domain.X` to its `portability.PortableX`. Write a per-kind helper (private, in this file): `taskToPortable(*domain.Task) portability.PortableTask`, etc. The conversion is field-for-field; names match. For `tasks[].tags`, look up tags assigned to that task — Tag-task relationships live in the `task_tags` join table, accessible via the existing tag repository. If there's no tag-by-task lookup, add one to `s.bundle.Tags` — but check first; the existing service code likely already does this kind of lookup somewhere (look in `service/task.go` for `GetTags` or similar).

3. Build and return the root `PortableWorkspace`:

```go
return &portability.PortableWorkspace{
    SchemaVersion: portability.SchemaVersion,
    TuskVersion:   s.tuskVersion,
    ExportedAt:    time.Now().UTC(),
    Workflows:     workflowDTOs,
    Projects:      projectDTOs,
    Players:       playerDTOs,
    Tags:          tagDTOs,
    Tasks:         taskDTOs,
    Relations:     relationDTOs,
    Annotations:   annotationDTOs,
    Notes:         noteDTOs,
    Events:        eventDTOs,
}, nil
```

If any `List` call returns an error, propagate it directly — Export does not wrap errors in `ImportError` (that envelope is for Import).

### Task 3 — Implement the validation pass

**File:** `service/portability.go`.

Add a private method `validate(ctx context.Context, ws *portability.PortableWorkspace, opts ImportOptions) *portability.ImportError`. Returns `nil` when the dump is clean. Otherwise returns an `*ImportError` carrying every issue found.

The validation rules are exactly as in the spec → "Validation pass":

1. **Schema check:** redundant with the codec's check (Phase 2 already rejects bad `schema_version` at decode time), but defensive — if the caller hands us a `PortableWorkspace` they built in memory, validate `ws.SchemaVersion == portability.SchemaVersion`. Add `Kind="schema"` issue if not.

2. **Referential integrity:** for every reference in the dump, the target must resolve to either (a) an entity also in the dump, or (b) under `--replace` only, an existing entity in the workspace. Specifically:
   - `task.parent_id` → another task in the dump or workspace.
   - `task.project_id` → a project in the dump or workspace.
   - `relation.source_id` / `relation.target_id` → tasks in the dump or workspace.
   - `annotation.task_id` → a task in the dump or workspace.
   - `note.project_id`, `note.player_id`, `note.task_id` (if non-nil) → matching entities in the dump or workspace.
   - `project.workflow_id` → a workflow in the dump or workspace.
   - `tasks[].tags[]` (each name) → a tag in `dump.Tags` or workspace.
   
   Build a `referenced[entityKind]map[id-or-name]bool` upfront from the dump, then check workspace residency only for refs not in the dump. For workspace residency under `--replace`, query the relevant repo (`s.bundle.Tasks.GetByID`, etc.). For each missing reference, emit `Kind="fk"`, `EntityKind=<the entity that holds the reference>`, `EntityID=<that entity's ID or short_id>`, `Message=<which field points to which missing target>`.

3. **Taxonomy:** for every project with a non-empty effective taxonomy (call `s.projects.EffectiveTaxonomy(project)` against the dump's project — substituting workspace defaults via the existing resolution chain), every task in that project runs through `domain.TaxonomyValidator{}.Check(ctx, ...)`. The validator already accepts a `ValidationContext` with the parent's resolved level — pre-load parent levels by walking the dump's task list once. For each violation, emit `Kind="taxonomy"`, `EntityKind="task"`, `EntityID=<task short_id>`, `Message=<the wrapped TaxonomyError's text>`.

4. **Relation cycles:** for every `blocks` edge in the dump, run a DFS to detect cycles. The existing service (`service/relation.go`) already has cycle-detection logic — extract it to a pure helper (e.g. `func DetectBlocksCycles(rels []domain.Relation) []uuid.UUID`) if it isn't already extracted, and call it from both places. If extraction is invasive, inline the same DFS in this file with a comment pointing at the canonical implementation. For each cycle, emit `Kind="cycle"`, `EntityKind="relation"`, `EntityID=<one of the cycle's relation IDs>`, `Message=<the participants>`.

5. **Workflow well-formedness:** for each `PortableWorkflow`, convert to `domain.Workflow` and run the existing workflow validator (look in `service/workflow.go` or `domain/workflow_validation.go`). For each violation, emit `Kind="workflow"`, `EntityKind="workflow"`, `EntityID=<workflow ID>`, `Message=<validator error>`.

6. **Collisions:** for every entity in the dump, look up by ID via the relevant repo. If present and `opts.Replace == false`, emit `Kind="collision"`, `EntityKind=<kind>`, `EntityID=<ID>`, `Message="entity already exists; use --replace to overwrite"`.

After collecting all issues, return `nil` if empty or `&portability.ImportError{Issues: issues}` otherwise.

### Task 4 — Implement the apply pass and `Import`

**File:** `service/portability.go`.

```go
// Import validates the dump in one pass, then applies it inside a single
// WriteTx. Returns *portability.ImportError on validation failure; the
// error carries every issue detected. On opts.DryRun the validation
// pass runs, the report's counts populate, and no writes happen.
//
// Import bypasses the usual optimistic-locking version check so that
// faithful round-trip preserves the dump's version values exactly.
// (See spec → "Optimistic locking under --replace".)
func (s *PortabilityService) Import(ctx context.Context, ws *portability.PortableWorkspace, opts ImportOptions) (*ImportReport, error)
```

Implementation:

1. **Pre-flight:** if `opts.Truncate && !opts.Replace`, return an `*ImportError` with one issue (`Kind="schema"`, `Message="--truncate requires --replace"`). The CLI in Phase 4 also validates this at flag-parse time; the service layer is the second line of defense.
2. **Validation pass:** call `s.validate(ctx, ws, opts)`. If non-nil, return `(report-with-no-counts, validationErr)`. The report still carries entity counts from the dump (so DryRun output is informative even when validation fails — set the per-kind counts to `len(ws.X)` regardless).
3. **DryRun short-circuit:** if `opts.DryRun`, populate `report.{Workflows, Projects, ...}` with `len(ws.X)` and return `(report, nil)`. No writes.
4. **Apply pass** inside `s.writeTx.WithTx(ctx, func(tx WriteTx) error { ... })`:
   - **If `opts.Truncate`:** call `tx.TruncateAll(ctx)` (added in Phase 1) and set `report.Truncated = true`. After this call, every entity table is empty.
   - **Faithful upsert pattern (used for every entity kind):**
     - For each entity in the dump, look up by ID via the relevant `tx.X()` accessor's `GetByID` method.
     - If the lookup returns `domain.ErrNotFound` (or the entity doesn't exist after a truncate), call the repo's `Create`. Increment `report.<kind>`.
     - If the entity exists, the validation pass already confirmed `opts.Replace == true` (otherwise it would have produced a collision issue). Perform a **read-version → delete → create** sequence using only existing repo methods:
       1. Read the existing row's `version` field via `GetByID`.
       2. Call the repo's `Delete(ctx, id, currentVersion)` to remove the row. The optimistic-locking check passes because we're using the version we just read.
       3. Call the repo's `Create` with the dump's entity. The dump's ID, version, timestamps, and every other field land verbatim.
       4. Increment both `report.<kind>` AND `report.Replaced`.

     This pattern uses only methods that already exist on every repo (`GetByID`, `Delete`, `Create`). Do **not** add new repository methods or modify any repository interface in this phase. The two-SQL-ops cost vs an in-place `UPDATE` is acceptable — imports are not on a hot path.
   - **Insert order (FK-respecting):** workflows, players, projects, tags, tasks (in topological order — see below), task_tags links, relations, annotations, notes, events. Apply the read-version → delete → create pattern at each step.
   - **Tasks in topological order:** the `tasks` insert step orders the dump's tasks so each task's `parent_id` (if non-nil) refers to a task already inserted. Pure roots first (parent_id == nil), then children of inserted tasks, etc. Algorithm:
     1. Build `byID := map[uuid.UUID]*PortableTask` from `ws.Tasks`.
     2. Build `inserted := map[uuid.UUID]bool{}`.
     3. Repeatedly scan `ws.Tasks`: for each task not yet in `inserted`, if `task.ParentID == nil` OR `inserted[*task.ParentID]` OR the parent is not in `byID` (i.e. parent already lives in the workspace, validation already confirmed it exists), insert that task and mark it inserted. Stop when a full scan inserts nothing — any tasks left at that point have a parent_id pointing at another in-dump task that itself has no resolvable parent, which is a cycle.
     4. If any task remains uninserted after termination, return an error wrapping the orphans (validation should have caught this — fail loudly).
     This sort is O(N²) worst case but N is the workspace size and imports are not on a hot path. With this ordering, each task's `parent_id` lands on the initial Create call — no second pass, no version bump, full faithful semantics for every task field including version.
   - **Tags-on-task:** after tasks land, iterate `ws.Tasks` again and write the inline `Tags []string` to the `task_tags` join. Find the existing tag-task linkage path used by `tusk task modify <id> +tag` (search for `task_tags` in `sqlite/`); reuse the same write path through `tx.Tags()` and/or `tx.Tasks()`.
   - **Final event:** build a `domain.WorkspaceImportedPayload` with the counts, `opts.Replace`, `opts.Truncate`, the dump's `SchemaVersion`, `TuskVersion`, `ExportedAt`. Wrap it in a `domain.Event` with `Type=EventWorkspaceImported`, `EntityID=""`, `EntityKind=EntityWorkspace`, `PlayerID=service.ActorFromContext(ctx)`, `CreatedAt=time.Now().UTC().Truncate(time.Millisecond)`. Call `tx.Events().Record(ctx, evt)`. Set `report.EventID = evt.ID`.
5. Return `(report, nil)` on success.

### Task 5 — Service-level tests

**New file:** `service/portability_test.go`

Use the existing test harness in `service/bundle_helpers_test.go` (see how `service/note_test.go` and `service/task_test.go` set up test environments). Each test should run against a fresh SQLite-backed environment with a real `WriteTx`.

Cover:

1. **Round-trip into empty workspace.** Pre-populate workspace A with a representative set of entities (1–2 of each kind, including a parent/child task pair, a `blocks` relation, a tag-on-task, an annotation, a project-level note, a task-level note, a couple of events). Export to `PortableWorkspace`. Import into a fresh workspace B. Re-export from B. Assert deep equality between the two `PortableWorkspace` values, modulo (a) `ExportedAt` and (b) the `workspace_imported` event row added by the import. Every other field — IDs, timestamps, version on every task (including parented tasks, thanks to the topological insert order in Task 4) — must round-trip exactly.
2. **Strict-mode collision.** Pre-seed a task in the workspace; import a `PortableWorkspace` containing the same task ID with a different title; assert `Import` returns `*portability.ImportError` with one issue, `Kind="collision"`, and the title in the workspace remains unchanged.
3. **`--replace` updates in place, faithful.** Pre-seed a task with `Title="old"`; import a `PortableWorkspace` containing the same ID with `Title="new"` and `Version=42`; with `opts.Replace = true`, assert `Import` succeeds, `report.Replaced == 1`, post-import title is `"new"`, and post-import version is `42` (faithful — the import did not re-stamp). Run this for both a root task and a parented task to confirm the topological insert preserves version in both cases.
4. **`--replace --truncate` wipes and rehydrates.** Pre-seed multiple tasks plus a project; import a `PortableWorkspace` with one project + one task; assert post-import workspace contains exactly those two entities (verify via `s.tasks.List` and `s.projects.List`).
5. **Validation errors batch correctly.** Forge a dump that violates simultaneously: (a) a task with `parent_id` pointing at a non-existent task, (b) a task with a level not in its project's taxonomy, (c) a `blocks` cycle between two tasks. Assert `Import` returns one `*ImportError` with three issues, each with the right `Kind` and `EntityID`. Order of issues doesn't matter; assert membership.
6. **DryRun does not mutate.** Pre-count tasks in the workspace. Run `Import(ctx, validDump, ImportOptions{DryRun: true, Replace: true})`. Assert (a) `report.Tasks == len(dump.Tasks)`, (b) post-call task count in the workspace equals the pre-count, (c) `report.EventID == uuid.Nil`.
7. **`workspace_imported` event lands once with correct counts.** Run a successful import. Query events via `s.bundle.Events.List(ctx, EventFilter{Type: &domain.EventWorkspaceImported})`. Assert exactly one event exists, its payload `Counts["tasks"] == report.Tasks`, and its `PlayerID` matches the actor in the context (use `service.WithActor(ctx, "test-player")`).
8. **Truncate without replace is rejected.** Call `Import(ctx, validDump, ImportOptions{Truncate: true, Replace: false})`. Assert returns `*ImportError` with one `Kind="schema"` issue mentioning `--replace`.

Use `t.Run` subtests for each so failures isolate cleanly.

### Task 6 — Wire `PortabilityService` onto `Client`

**File:** `client.go`

Add a public field to `Client`:

```go
type Client struct {
	Tasks       *service.TaskService
	Tags        *service.TagService
	Relations   *service.RelationService
	Projects    *service.ProjectService
	Workflows   *service.WorkflowService
	Players     *service.PlayerService
	Notes       *service.NoteService
	Portability *service.PortabilityService

	store *sqlite.Store
}
```

In `NewClient`, after the existing service constructors, instantiate the portability service:

```go
portabilitySvc := service.NewPortabilityService(
	writeTx,
	taskSvc, projectSvc, workflowSvc, relationSvc,
	tagSvc, playerSvc, noteSvc,
	bundle,
	tuskVersionForClient(),
)

return &Client{
	Tasks:       taskSvc,
	Tags:        tagSvc,
	Relations:   relationSvc,
	Projects:    projectSvc,
	Workflows:   workflowSvc,
	Players:     playerSvc,
	Notes:       noteSvc,
	Portability: portabilitySvc,
	store:       store,
}, nil
```

For the `tuskVersion` parameter: there's no existing version constant accessible from `client.go`. Add a small helper `func tuskVersionForClient() string { return "library" }` (or pull from `runtime/debug.ReadBuildInfo()` if it's already used elsewhere — check first). The CLI gets the real build-injected version in Phase 4 and passes it explicitly when constructing its own client; library consumers get a placeholder, which is fine because `tusk_version` is informational only.

Add a basic `client_test.go` assertion that `Client.Portability` is non-nil after `NewClient` returns successfully — confirms the wiring without exercising the service.

---

## Acceptance criteria

1. `make build` succeeds.
2. `go test ./service/... ./internal/portability/...` passes.
3. `make vet` and `make lint` succeed.
4. `service.PortabilityService` exists with `NewPortabilityService`, `Export`, and `Import` methods matching the signatures above.
5. `Client.Portability` is exposed and wired in `NewClient`.
6. The `workspace_imported` event lands in the event log on every successful (non-DryRun) import.
7. `--replace --truncate` empties every entity table inside the same transaction as the apply pass.
8. **No CLI behavior change.** The library API has new entry points; the CLI doesn't expose them yet (that's Phase 4). Existing `tusk` commands work identically.

---

## User-visible behavior preserved

- All existing `tusk` CLI commands work identically.
- All existing MCP tools and resources work identically.
- Library consumers gain a new `Client.Portability` field with `Export` and `Import`. Existing `Client` fields (`Tasks`, `Tags`, etc.) are unchanged.
- Build and existing test suite unchanged.

---

## Changes Introduced

**New files:**
- `service/portability.go` — `PortabilityService`, `NewPortabilityService`, `ImportOptions`, `ImportReport`, the unexported `validate` method, all per-kind conversion helpers.
- `service/portability_test.go` — service-level tests covering round-trip, collision, `--replace`, `--truncate`, validation errors, DryRun, event emission, and the truncate-without-replace guard.

**Modified files:**
- `client.go` — `Client` struct gains `Portability *service.PortabilityService`. `NewClient` instantiates and wires it.
- `client_test.go` — small assertion that `Portability` is non-nil after `NewClient`.
- Possibly `service/relation.go` — extract the existing `blocks` cycle DFS into a pure helper function if portability needs to reuse it. If extraction is invasive (touches more than 20 lines of relation service code), inline the same DFS logic in `service/portability.go` with a comment pointing at the canonical implementation.

**Modified interfaces:** None. The apply pass uses only existing repository methods (`GetByID`, `Create`, `Delete`, `UpdateOrderAndParent`) plus the `TruncateAll` added on `WriteTx` in Phase 1. No new repository methods, no test stub updates beyond what Phase 1 already did.

**No new env vars, no schema migration, no new dependencies, no bridge code.**
