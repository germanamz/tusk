# Data Portability — Design

**Initiative:** v0.13 — Data Portability
**Status:** Design approved 2026-04-26
**Authors:** German Meza (with brainstorm assist)

---

## Scope

### In scope

- `tusk export --format json [--output <path>]` — full workspace dump; stdout by default.
- `tusk export --format csv [--output <path>]` — flat tabular task export.
- `tusk import --format json --input <path>` — rehydrate workspace; `--replace` for row-level upsert; `--replace --truncate` for wipe-and-restore; `--dry-run` to preview.
- New `internal/portability/` package owning the JSON and CSV codecs over a neutral `PortableWorkspace` value.
- New `service.PortabilityService` orchestrating Export and Import atomically through the existing repos.
- New `domain.EventWorkspaceImported` event type + `domain.EntityWorkspace` entity-kind constant.
- ROADMAP.md edits reflecting the narrowed initiative scope (see "ROADMAP.md edits" section below).

### Moved out

- **Markdown rendering** moves under the `Initiative: ROADMAP.md Migration` block as `tusk task tree --format markdown` (export-only, thin renderer extending the existing tree renderer). Markdown import is dropped entirely.
- **MCP `tusk_export` / `tusk_import` tools** are dropped. Data lifecycle is a CLI / human concern; agents do not back up or rehydrate workspaces.

### Locked decisions (from the brainstorm)

| Decision | Choice |
|---|---|
| Code home | Dedicated `internal/portability/` package (codecs) + new `service.PortabilityService` (orchestration). CLI commands are thin. |
| Import semantics | **Faithful**: preserves IDs, `created_at`, `modified_at`, `version` exactly. Single `workspace_imported` envelope event, not per-entity events. |
| Atomicity | Pre-validation pass collects every issue; apply pass runs inside one SQLite transaction. |
| `--replace` | Row-level upsert on collision. |
| `--replace --truncate` | Wipe every entity table inside the same transaction before applying the dump. Requires `--replace`. |
| JSON envelope | `schema_version: 1` + `tusk_version` + flat top-level lists. Tags assigned to a task appear inline (`"tags": ["api"]`) on the task; tag definitions live in the top-level `tags: [...]` list. |
| MCP surface | None for export/import. |

---

## Architecture

```
internal/tui (CLI)
        ↓
service.PortabilityService    ←→    internal/portability/{json,csv}
        ↓                                    (pure codec, no DB knowledge)
service.WriteTx + repositories
        ↓
sqlite store
```

The portability codecs know nothing about the database; the service knows nothing about the wire format. The CLI command picks the format, hands bytes to the codec, hands the resulting `PortableWorkspace` to the service.

### Package layout

**New code:**

```
internal/portability/
  portable.go           # PortableWorkspace and per-entity DTOs (the neutral wire shape)
  json/
    encode.go           # PortableWorkspace → []byte (writer-based)
    decode.go           # []byte → PortableWorkspace (single-pass parser, schema_version check)
  csv/
    encode.go           # PortableWorkspace.Tasks → []byte (export only)
service/
  portability.go        # PortabilityService — orchestrates Export/Import using existing repos via WriteTx
domain/
  event_portability.go  # EventWorkspaceImported + payload struct + EntityWorkspace constant
internal/tui/
  export.go             # buildExportCmd, runExport
  import.go             # buildImportCmd, runImport
```

**Touched code:**

- `service/tx.go` — `WriteTx` interface grows accessors for the entity kinds portability writes (Projects, Workflows, Players, Tags, Annotations, Notes). Today it surfaces only Tasks/Relations/Events; portability needs the full set inside one transaction.
- `client.go` — wires `PortabilityService` onto `Client` (new public field `Portability`).
- `internal/tui/app.go` — registers `buildExportCmd()` / `buildImportCmd()` on the root command.
- `service/bundle_helpers_test.go`, `service/task_claim_test.go`, `service/task_tx_invariant_test.go` — small updates to the test stubs that implement `WriteTx`.

**No schema migration.** Portability is read/write over the existing schema.

**No new repository methods.** Read paths use the `List(ctx)` accessor each repo already exposes plus per-task lookups for tags/annotations/relations.

---

## `PortableWorkspace` and the JSON envelope

### Top-level shape (`internal/portability/portable.go`)

```go
type PortableWorkspace struct {
    SchemaVersion int                  `json:"schema_version"`
    TuskVersion   string               `json:"tusk_version"`
    ExportedAt    time.Time            `json:"exported_at"`

    Workflows   []PortableWorkflow     `json:"workflows"`
    Projects    []PortableProject      `json:"projects"`
    Players     []PortablePlayer       `json:"players"`
    Tags        []PortableTag          `json:"tags"`
    Tasks       []PortableTask         `json:"tasks"`
    Relations   []PortableRelation     `json:"relations"`
    Annotations []PortableAnnotation   `json:"annotations"`
    Notes       []PortableNote         `json:"notes"`
    Events      []PortableEvent        `json:"events"`
}
```

Per-entity DTOs are 1:1 with `domain` types — same fields, same JSON tags, no business logic. They exist so format consumers don't have to import `domain`, and so the wire format can drift from the in-memory shape if it needs to (e.g. the v0.13 post-implementation review item about cleared `order` round-trip).

### Field-shape decisions worth calling out

- **`tasks[].tags`** — `[]string` (tag names) inline on each task. Authoritative tag rows (with color, UUID) live in top-level `tags`. Names referenced inline must exist in `tags`.
- **`tasks[].uda`** — `map[string]string` (matches the existing UDA value contract).
- **`tasks[].order`** — `*float64`. Encoded as either a JSON number or `null` (never omitted), so cleared-order round-trip works (resolves the open v0.13 follow-up at ROADMAP.md:1305).
- **`tasks[].urgency_overrides`** — full struct with `omitempty` per-field; null when no overrides.
- **`tasks[].claimed_by` / `claimed_at`** — round-tripped exactly. The referenced player must exist in the same dump (or in the workspace, under `--replace`).
- **`workflows[]`** — name, statuses (with roles), transitions, version, timestamps. The built-in kanban workflow round-trips like any other.
- **`projects[]`** — name, workflow ID, settings (auto-complete, auto-revert, taxonomy override, urgency overrides, note window). The built-in default project round-trips like any other.
- **`events[]`** — generic `Event` shape (id, type, entity_id, entity_kind, player_id, payload, created_at). Payload is `json.RawMessage` so unknown event types round-trip without the codec needing to know every payload struct.
- **`annotations[]`, `notes[]`, `relations[]`, `tags[]`, `players[]`** — full field set as exported by the existing `domain` types.

### `schema_version` policy

- Current value: `1`.
- Import rejects any other `schema_version` with a clear error: *"this dump was produced by tusk version X with schema_version Y; this tusk supports up to schema_version Z"*.
- Bumped on any breaking change to the wire shape (new required fields, removed fields, changed types, changed semantic meaning). Additive optional fields don't bump.

### `tusk_version`

Informational. Surfaced in error messages and in the `workspace_imported` event payload. Never used to gate import logic.

---

## `PortabilityService` API

Lives at `service/portability.go`.

```go
type PortabilityService struct {
    writeTx     WriteTxProvider
    tasks       *TaskService
    projects    *ProjectService
    workflows   *WorkflowService
    relations   *RelationService
    tags        *TagService
    players     *PlayerService
    notes       *NoteService

    // Repository handles for read paths the services don't expose
    // (annotations, raw events). Portability is a workspace-wide reader,
    // so it sits one level closer to the data layer than typical services.
    bundle      *RepoBundle

    tuskVersion string
}

func NewPortabilityService(
    writeTx WriteTxProvider,
    tasks *TaskService, projects *ProjectService, workflows *WorkflowService,
    relations *RelationService, tags *TagService, players *PlayerService, notes *NoteService,
    bundle *RepoBundle, tuskVersion string,
) *PortabilityService
```

### Public methods

```go
// Export reads the entire workspace into a PortableWorkspace value.
// No transaction wrap (read-only over WAL); the snapshot is best-effort
// consistent — concurrent writers may produce a slightly inconsistent dump,
// documented as known. Workspaces using portability for backup should pause
// concurrent writers themselves.
func (s *PortabilityService) Export(ctx context.Context) (*PortableWorkspace, error)

// ImportOptions controls Import behavior. Zero value = strict (fail on any
// collision, no truncation, full apply).
type ImportOptions struct {
    Replace  bool // row-level upsert on collision
    Truncate bool // wipe-and-restore mode; requires Replace
    DryRun   bool // run validation pass; report counts; no writes
}

// ImportReport is what Import returns. Counts populate even on DryRun.
type ImportReport struct {
    Workflows, Projects, Players, Tags, Tasks,
    Relations, Annotations, Notes, Events int

    Replaced  int       // rows updated under --replace
    Truncated bool      // tables emptied under --replace --truncate
    EventID   uuid.UUID // workspace_imported event ID (zero on DryRun)
}

// Import validates the dump in one pass, then applies it inside a single
// transaction. Returns ErrConflict on duplicate IDs without --replace; returns
// a structured taxonomy / FK / cycle error on validation failure.
func (s *PortabilityService) Import(ctx context.Context, ws *PortableWorkspace, opts ImportOptions) (*ImportReport, error)
```

### Validation pass (no writes)

1. **Schema check** — `schema_version == 1`; otherwise reject.
2. **Referential integrity** — every `task.parent_id`, `task.project_id`, `relation.source_id`/`target_id`, `annotation.task_id`, `note.task_id`/`note.player_id`/`note.project_id`, `project.workflow_id`, `tasks[].tags[]` resolves to an entity in this dump **or** an existing entity in the workspace (under `--replace` only). Strict mode requires every reference to live inside the dump.
3. **Taxonomy** — for every project with a non-empty effective taxonomy (project override or workspace default), every task in that project runs through `domain.TaxonomyValidator`. Same code path as `tusk task create` and `tusk task modify`.
4. **Relation cycles** — `blocks` edges run through the existing cycle DFS in `RelationService` (extracted to a pure helper if not already).
5. **Workflow well-formedness** — each imported workflow runs through the existing workflow validator (exactly-one initial/start/done/delete; transition graph references known statuses).
6. **Collisions** — for every imported entity, look up by ID; if present and `--replace` is not set, fail with `ErrConflict` and the offending ID.

If validation fails, the report carries no counts and no writes have happened. The validation pass collects **every** issue before returning — no fail-fast — so the user sees the full picture in one round-trip.

**Optimistic locking under `--replace`:** import bypasses the usual `WHERE version = ?` check and forcibly writes the dump's `version` value. Faithful round-trip requires this: a backup taken at version=5 must restore at version=5. Concurrent writers during import would race anyway and are out of scope (see "Known limitation").

### Apply pass (inside one `WriteTx`)

Order matters because of FKs:

```text
(if --truncate) DELETE FROM events, notes, annotations, relations, task_tags,
                tasks, projects, workflows, tags, players

INSERT/UPSERT workflows
INSERT/UPSERT players
INSERT/UPSERT projects (FKs workflows)
INSERT/UPSERT tags
INSERT/UPSERT tasks (FKs projects, parent tasks)  -- two-pass when parent_id
                                                     references a task later in
                                                     the list; second pass
                                                     UPDATEs parent_id only.
INSERT INTO task_tags (task, tag) -- from inline tasks[].tags
INSERT/UPSERT relations (FKs tasks)
INSERT/UPSERT annotations (FKs tasks)
INSERT/UPSERT notes (FKs projects, players, optional tasks)
INSERT/UPSERT events (FKs nothing — entity_id is opaque)

INSERT one event: workspace_imported
  payload: {schema_version, tusk_version, exported_at, replace, truncate, counts: {...}}
COMMIT
```

The `workspace_imported` event has its own type constant (`domain.EventWorkspaceImported`) and a typed payload struct living in `domain/event_portability.go`, following the existing event-task / event-relation file split.

### Truncate-and-default-project behavior

Under `--truncate`, tables are emptied and re-populated from the dump alone. The dump always carries the built-in kanban workflow + default project (because export emits everything including built-ins), so the round-trip case stays consistent. **If the dump omits them, the post-truncate workspace has no default project** — which is the correct behavior for "the dump is the source of truth." Documented behavior; no special-case re-seeding.

---

## CLI surface

### `tusk export`

```text
Usage: tusk export --format <fmt> [--output <path>]

Flags:
  --format string   "json" or "csv" (required; no default — explicit choice avoids surprises)
  --output string   path to write to; default "-" (stdout)
```

Behavior:

- Calls `Portability.Export(ctx)` once, hands the `PortableWorkspace` to the matching codec, writes bytes to stdout or the file.
- JSON output is pretty-printed (2-space indent) for human-friendliness; consumers can re-pipe through `jq -c` for compact form.
- CSV is one row per task (see Section "CSV columns").
- Exit code: 0 on success; non-zero with a stderr message on read or codec failure.
- Atomic file write: write to a `*.tmp` next to `--output` then rename, so `tusk export --output ws.json` never leaves a half-written file. Stdout is best-effort (caller's pipe).

### `tusk import`

```text
Usage: tusk import --format json --input <path> [--replace] [--truncate] [--dry-run]

Flags:
  --format string   "json" only (CSV import is not in scope)
  --input string    path to read from; "-" for stdin
  --replace         row-level upsert on collision (default: fail on collision)
  --truncate        wipe every entity table before applying; requires --replace
  --dry-run         validate and report counts; no writes
```

Behavior:

- Reads the file (or stdin), decodes to `PortableWorkspace`, calls `Portability.Import(ctx, ws, opts)`.
- On success, prints the `ImportReport` summary in text format (`--format text`) or as JSON (`--format json`).
- On validation failure, prints a structured error: kind (`taxonomy`, `cycle`, `fk`, `collision`), entity kind/ID/short_id, and a one-line human message. Exit code: non-zero. Multiple errors batch in a single report.
- `--truncate` without `--replace` is rejected at flag-parse time with a clear message.
- `--dry-run` always exits 0 if validation passed, 1 if it failed; stdout carries the report; nothing is written.

### Stdin / stdout symmetry

`--input -` reads stdin, mirroring how `description=@-` already works in inline syntax. The TTY guard rejects `-` when stdin is not piped, same as the existing expander.

---

## CSV columns

Export only. One row per task.

```text
short_id, id, parent_short_id, project, level, status, priority, order,
title, description, due_at, wait_until, claimed_by, claimed_at,
tags, uda, created_at, modified_at, version
```

Conventions:

- `parent_short_id` resolves the parent reference to the human-friendly short ID (parents always exist in the same dump, so the lookup never fails).
- `project` is the project **name**, not the UUID. Spreadsheets get readable values; round-tripping is a non-goal for CSV.
- `tags` collapses into a single space-separated cell (`api urgent`).
- `uda` collapses into a single cell of `key1=val1;key2=val2` (semicolon-separated; values quoted if they contain `;` or `=`).
- `description` is included as a single CSV-quoted cell. Newlines in descriptions are preserved (RFC 4180 allows quoted newlines).
- Empty cells for nullable columns (`due_at`, `wait_until`, `claimed_by`, `claimed_at`, `parent_short_id`, `level`, `order`).
- `order` is a float for non-null values, empty cell for NULL — placed after `priority` to match the JSON/display order. (Resolves the open v0.13 follow-up at ROADMAP.md:1305.)
- Notes, annotations, relations, urgency_overrides, events do not appear — they don't fit a flat per-task row, and CSV is for spreadsheet workflows, not portability.

---

## Error envelope (import)

`PortabilityService.Import` returns a typed `ImportError` carrying a slice of issues:

```go
type ImportIssue struct {
    Kind        string  // "schema" | "taxonomy" | "fk" | "cycle" | "workflow" | "collision"
    EntityKind  string  // "task" | "relation" | "project" | ...
    EntityID    string  // UUID if present, short_id if known, "" if neither
    JSONPointer string  // e.g. "/tasks/42/parent_id" for codec-level errors
    Message     string  // one-line human message
}

type ImportError struct {
    Issues []ImportIssue
}

func (e *ImportError) Error() string  // collapses to "import failed: <N> issues"
```

The validation pass collects every issue before returning. The CLI renders the issue list as a table in `--format text` and as a JSON array in `--format json`.

---

## `workspace_imported` event payload

```go
const EventWorkspaceImported EventType = "workspace_imported"
const EntityWorkspace EntityKind = "workspace"

type WorkspaceImportedPayload struct {
    Kind          EventType      `json:"kind"`
    SchemaVersion int            `json:"schema_version"`
    SourceTuskVer string         `json:"source_tusk_version"`
    ExportedAt    time.Time      `json:"exported_at"`
    Replace       bool           `json:"replace"`
    Truncate      bool           `json:"truncate"`
    Counts        map[string]int `json:"counts"` // entity_kind → count inserted/updated
}

func (WorkspaceImportedPayload) EventKind() EventType { return EventWorkspaceImported }
```

The event is emitted exactly once per import, inside the apply transaction, with the actor pulled from `service.ActorFromContext` (so `--player` flows through).

---

## Testing strategy

### Unit tests

- `internal/portability/json/encode_test.go` and `decode_test.go`:
  - Round-trip table tests: build a `PortableWorkspace` in memory, encode to JSON, decode back, assert deep equality. Cover every nullable field (especially `task.order` cleared, `urgency_overrides` empty, `claimed_*` set/unset, multi-line descriptions, UDA edge cases).
  - Schema version rejection: forge a JSON dump with `schema_version: 999`; decoder returns the typed error.
- `internal/portability/csv/encode_test.go`:
  - Golden-file test under `internal/portability/csv/testdata/` for a small fixture workspace; assert byte-for-byte equality.

### Service tests (`service/portability_test.go`)

- Export of a populated workspace, decode via the JSON codec, assert every entity round-trips (one test per entity kind: tasks, relations, annotations, notes, events, players, tags, projects, workflows).
- Import into an empty workspace (the round-trip e2e of the export above) — counts match, entities are byte-equal to original via `domain.Task`/etc. comparison.
- Strict-mode collision: pre-seed one task, import a dump containing the same UUID, assert `ImportError` with `Kind=collision`.
- `--replace` behavior: pre-seed a task with `Title="old"`, import dump with same UUID and `Title="new"`, assert post-import title is `"new"` and version is what the dump declared (faithful).
- `--replace --truncate`: pre-seed multiple entities, import a dump with one project + one task, assert all pre-seeded entities are gone and only the dumped two remain.
- Validation errors: forge dumps that violate (a) taxonomy, (b) FK to a missing project, (c) `blocks` cycle, (d) malformed workflow. Assert each surfaces as a distinct `ImportIssue` with the right `Kind` and `EntityID`.
- `--dry-run`: same as the round-trip test but with `DryRun: true`; assert no rows changed in the DB and counts populate.
- `workspace_imported` event: assert exactly one event of that type with the right counts and actor exists post-import.

### E2E tests (`tests/e2e/portability_test.go`)

Run by the existing 2×2 harness (DB config × output format).

- `tusk export --format json --output ws.json` → `tusk import --format json --input ws.json --replace` produces an identical workspace to the source. Use `tusk export` again and diff the two JSON files (modulo `exported_at`).
- `tusk export --format json` to stdout, piped to `tusk import --format json --input -` after `--replace --truncate` — same equality check.
- `tusk export --format csv` against a fixture workspace, golden-file diff.
- Schema version error path: forge a stub dump and assert exit code + stderr contents.
- Validation errors: bad-FK, bad-taxonomy, cycle, collision-without-replace each produce the structured exit code and report.
- `--dry-run` produces a report and no DB mutation (verify by counting tasks before/after).

### Manual smoke tests

Human-run shell sequences to validate the feature end-to-end after implementation. Each is independent and runs against a temporary workspace (`TUSK_DB=$(mktemp).db`) so it doesn't touch the real one.

**Smoke 1 — Full round-trip into an empty workspace**

```bash
tusk export --format json --output /tmp/ws.json

TUSK_DB=$(mktemp -t tusk-smoke.XXX.db) tusk import --format json --input /tmp/ws.json
TUSK_DB=$(mktemp -t tusk-smoke.XXX.db) tusk export --format json --output /tmp/ws-rt.json

diff <(jq 'del(.exported_at, .events[] | select(.type == "workspace_imported"))' /tmp/ws.json) \
     <(jq 'del(.exported_at, .events[] | select(.type == "workspace_imported"))' /tmp/ws-rt.json)
```

**Smoke 2 — Stdin / stdout pipeline**

```bash
tusk export --format json | TUSK_DB=$(mktemp).db tusk import --format json --input - --replace --truncate
```

**Smoke 3 — `--dry-run` on a real dump**

```bash
tusk export --format json --output /tmp/ws.json
tusk import --format json --input /tmp/ws.json --replace --dry-run

tusk task list --format json | jq 'length'
tusk import --format json --input /tmp/ws.json --replace --dry-run
tusk task list --format json | jq 'length'  # should match
```

**Smoke 4 — Validation error surfaces cleanly**

```bash
jq '.tasks[0].parent_id = "00000000-0000-0000-0000-deadbeefdead"' /tmp/ws.json > /tmp/bad.json
TUSK_DB=$(mktemp).db tusk import --format json --input /tmp/bad.json
echo "exit: $?"  # non-zero
```

**Smoke 5 — Collision without `--replace` is rejected**

```bash
tusk export --format json --output /tmp/ws.json
tusk import --format json --input /tmp/ws.json
# Expect: ImportError with Kind=collision, exit non-zero
```

**Smoke 6 — `--replace --truncate` wipes and rehydrates**

```bash
DB=$(mktemp -t tusk-smoke.XXX.db)
TUSK_DB=$DB tusk task create "garbage row that should disappear"
TUSK_DB=$DB tusk import --format json --input /tmp/ws.json --replace --truncate
TUSK_DB=$DB tusk task list | grep -q "garbage row" && echo "FAIL: garbage survived" || echo "OK"
```

**Smoke 7 — CSV opens cleanly in a spreadsheet tool**

```bash
tusk export --format csv --output /tmp/tasks.csv
head -2 /tmp/tasks.csv
# Open /tmp/tasks.csv in LibreOffice / Numbers / Excel — verify multi-line descriptions
# render correctly (cells stay one row each), tags column reads "api urgent",
# uda column reads "key1=val1;key2=val2".
```

**Smoke 8 — Schema-version mismatch path**

```bash
jq '.schema_version = 999' /tmp/ws.json > /tmp/future.json
TUSK_DB=$(mktemp).db tusk import --format json --input /tmp/future.json
echo "exit: $?"  # non-zero, clear error message
```

**Smoke 9 — `workspace_imported` event lands once with the right counts**

```bash
tusk import --format json --input /tmp/ws.json --replace
sqlite3 ~/.local/share/tusk/tusk.db \
  "SELECT event_type, json_extract(payload, '\$.counts') FROM events
   WHERE event_type='workspace_imported' ORDER BY created_at DESC LIMIT 1;"
```

---

## Rollout / migration impact

- **No database migration.** Portability is read/write over the existing schema.
- `WriteTx` interface grows accessors. This is non-breaking for external implementers since `WriteTx` lives inside `service/` and is not exported as a stable interface (it's used in tests only outside `service/` and `client.go`). Test stubs in `service/bundle_helpers_test.go`, `service/task_claim_test.go`, and `service/task_tx_invariant_test.go` need a small update.
- New `EntityWorkspace` constant and `EventWorkspaceImported` constant land in `domain/`. The existing event log retention prunes them like any other event.
- `PortabilityService` is wired onto `Client.Portability`, exposing the same Export/Import API to library consumers as the CLI uses.

### Known limitation (documented)

Export under concurrent writes can produce a slightly inconsistent snapshot — a task can change between the tasks-list query and the relations-list query. For backup, the user is expected to pause writers. We don't add a workspace-wide read lock; SQLite WAL doesn't make this trivial without a `BEGIN IMMEDIATE` that would block all writers for the duration of the export. Worth revisiting if it becomes a real issue.

---

## ROADMAP.md edits (deliverable, applied during implementation)

Three edits to roll into the implementation plan, applied as part of the first commit:

### Edit 1 — Reshape `Initiative: Data Portability` (ROADMAP.md:1084–1109)

Replace the four current stories with three narrowed stories:

- **Story: JSON export and import** — adds `--replace --truncate`, faithful import semantics, single envelope event, pre-validation pass, `schema_version: 1` envelope.
- **Story: CSV export** — unchanged from current text.
- **Story: PortabilityService and codec package** — replaces the deleted "MCP tools" story; describes the `internal/portability/` package, the `service.PortabilityService`, the `WriteTx` extension, the `Client.Portability` exposure, and the `EventWorkspaceImported` / `EntityWorkspace` additions.

Adds a `> **Deferred (not v0.13):** CSV import` blockquote at the bottom of the initiative — same pattern as the deferred-work note already used in the Task Level Taxonomy initiative (ROADMAP.md:1024). Body: *"CSV import is a future story. Editing tasks in a spreadsheet and pushing changes back requires partial-row merge semantics, a story for how the lossy CSV shape interacts with `--replace`, and a CSV-specific validation pass — none of which the v0.13 use cases need. The CSV column set defined in this initiative is the export contract; the import story will define how those columns map back."*

Deletes the **Story: Markdown export and import** block entirely.

### Edit 2 — Reshape `Initiative: ROADMAP.md Migration` (ROADMAP.md:1111–1124)

Adds a new **Story: Markdown rendering (export-only)** at the top of the initiative covering `tusk task tree --format markdown` (extends the existing tree renderer; no top-level `tusk render` command). Describes the dialect: H1 per project, H2 per root task, nested bullets, `[x]` for done-role statuses, inline `level=`, `priority=`, `due=`, `order=`, `uda.*=` tokens, trailing `+tag`, `status=<name>` for non-binary states, annotations and notes as labeled child lists, urgency_overrides / recurrence / attachments / claim state silently dropped.

Updates the migration script story: removes the "or hands the markdown file directly to `tusk import --format markdown`" alternative since markdown import is gone — JSON is the only import path.

Updates the verification step and cutover step to point at `tusk task tree --format markdown` instead of `tusk export --format markdown`.

### Edit 3 — Update v0.13 milestone summary (ROADMAP.md:971)

Tightens the wording from "bidirectional Data Portability" to "bidirectional JSON Data Portability" and notes that the markdown renderer lives under the ROADMAP.md Migration initiative.

---

## Open questions resolved during the brainstorm

| Question | Resolution |
|---|---|
| New package vs methods on services? | Dedicated `internal/portability/` package + new `service.PortabilityService`. |
| Faithful or auditable import? | Faithful. Single envelope event. |
| `--replace` semantics? | Row-level upsert. `--replace --truncate` for wipe-and-restore. |
| Markdown dialect specifics? | Markdown removed from this initiative. Lives under ROADMAP.md Migration as a thin export-only renderer. |
| MCP tool inputs (path vs inline vs both)? | No MCP tools. Data lifecycle is a CLI concern. |
| Schema versioning? | `schema_version: 1` integer + `tusk_version` string. Reject unknown versions. |
| JSON top-level shape? | Flat lists. Inline tags-on-task `[]string` plus authoritative top-level `tags: [...]`. |
| Truncate-and-default-project behavior? | Dump is the source of truth. No special-case re-seeding. |

---

## Out of scope (explicitly)

### Dropped permanently

- Markdown import. (Markdown is a rendering format under ROADMAP.md Migration; JSON is the only import path.)
- MCP `tusk_export` / `tusk_import` tools. (Data lifecycle is a CLI / human concern.)

### Deferred to a later milestone

- **CSV import.** This initiative ships CSV export only. CSV import is a future story (likely v0.14 or later) — the use case is "edit tasks in a spreadsheet, push the changes back" and it requires non-trivial design work that's not justified by current demand: how partial CSV rows merge with existing tasks, how the lossy CSV shape (no relations, no annotations, no urgency_overrides) interacts with `--replace` semantics, and whether CSV import gets its own validation pass distinct from JSON's. Tracked as a separate ROADMAP story (see ROADMAP edits below) so it surfaces in future planning.
- Bidirectional sync between running tusk instances (separate v0.16+ initiative).
- Schema conversion shims for old `schema_version` values (added when the schema first breaks).
- Workspace-wide read locks on export (see "Known limitation").
- Streaming codec for very large workspaces (current target: workspaces in the low-thousands range comfortably; revisit if dumps become memory-pressured).
