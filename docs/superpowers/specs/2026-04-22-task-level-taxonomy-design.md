# Design Spec — Task Level Taxonomy (v0.13)

Status: proposed
Date: 2026-04-22
Initiative: ROADMAP.md § v0.13 — Task Level Taxonomy
Scope: this document replaces the per-UDA-key schema plan with a first-class `level` field and a rank-ordered taxonomy at workspace scope with per-project override.

## 1. Goal

Enforce the milestone → initiative → story → task/spike modeling used by the roadmap self-host and Claude Code plugin skills, without constraining simpler projects. UDAs stay free-form; levels get their own primitive with structured validation.

Requirements from the ROADMAP initiative:

- Tasks carry an optional `level` field; the field is required when the project's effective taxonomy is non-empty.
- A taxonomy is a rank-ordered list of peer level groups. Rank 0 is the only root-eligible rank. Parents must sit at a strictly lower rank index than the task.
- Taxonomies resolve workspace default (TOML) → per-project override → empty. Override is full-replace; projects can explicitly opt out of the workspace default.
- Taxonomy edits are prospective — existing tasks are not re-validated. A `tusk task level-check` command surfaces violations without rejecting them.
- Level participates in filters (`level=name`, `level=a,b`), CLI inline syntax (`level=`), MCP tools (create/modify input, every task response), and the event log (`task_modified` diff entries; undo covers level changes).

## 2. Non-Goals (Deferred)

Per the initiative's explicit deferral list:

- Per-level DAG constraints beyond the rank-strict rule (e.g., "`task` may sit under `story` but not under `initiative`").
- Per-level required fields or defaults.
- Retroactive re-validation when a taxonomy is edited.
- A `--fix` mode for `tusk task level-check`.
- A dedicated MCP tool for `tusk_task_level_check` (defer to the next initiative).

The rank-based model upgrades cleanly to per-level parent sets if a stricter taxonomy becomes necessary.

## 3. Data Model

### 3.1 Task

`domain.Task` gains:

```go
Level *string // nullable; NULL when no taxonomy applies or level never set
```

`domain.TaskUpdate` gains:

```go
Level **string // nil = no change, *nil = clear, *"story" = set
```

Serialized as `"level"` (JSON / text / CSV) with `omitempty` so workspaces without taxonomies see no new field noise.

### 3.2 Taxonomy

New `domain.Taxonomy`:

```go
type Taxonomy [][]string // top rank at index 0

func (t Taxonomy) IsEmpty() bool
func (t Taxonomy) Contains(level string) bool
func (t Taxonomy) RankOf(level string) (int, bool)
func (t Taxonomy) IsTopRank(level string) bool
func (t Taxonomy) Clone() Taxonomy
func (t Taxonomy) Validate() error // rejects malformed taxonomies
```

`Validate` rejects: duplicate level names across the whole taxonomy, empty rank groups, names not matching `[a-zA-Z_][a-zA-Z0-9_-]*`, zero ranks. Called before any persistence (CLI, MCP, config loader).

### 3.3 Project Settings

`domain.ProjectSettings` gains:

```go
Taxonomy *Taxonomy `json:"taxonomy,omitempty"`
```

Tristate preserved through JSON:

| Settings.Taxonomy       | Meaning                                                   | JSON form                       |
| ----------------------- | --------------------------------------------------------- | ------------------------------- |
| `nil`                   | Inherit workspace default                                 | key absent                      |
| `&Taxonomy{}`           | Explicit opt-out (disable levels even if workspace has one) | `"taxonomy": []`              |
| `&populated`            | Project override (full replace, no per-rank merge)        | `"taxonomy": [["milestone"], ...]` |

No custom marshaler needed: `*Taxonomy` with `omitempty` already round-trips the three states through `encoding/json`.

### 3.4 Errors

```go
var ErrTaxonomyViolation = errors.New("task violates project taxonomy")

type TaxonomyError struct {
    Level       string
    ParentLevel string
    Reason      string   // "missing" | "unknown_level" | "root_requires_top_rank" | "parent_rank_not_lower"
    Taxonomy    Taxonomy
}

func (e *TaxonomyError) Error() string
func (e *TaxonomyError) Unwrap() error { return ErrTaxonomyViolation }
```

### 3.5 Database

Migration `010_task_level`:

```sql
-- up
ALTER TABLE tasks ADD COLUMN level TEXT;
CREATE INDEX idx_tasks_level ON tasks(level);

-- down
DROP INDEX idx_tasks_level;
ALTER TABLE tasks DROP COLUMN level;
```

No backfill. Existing rows keep `level = NULL`. `ProjectSettings.Taxonomy` is stored inside the existing `projects.settings` JSON blob — no new column.

## 4. Configuration

### 4.1 TOML schema

`[taxonomy]` section in `tusk.toml`:

```toml
[taxonomy]
# Ordered list of rank groups, top rank first. Leave unset or empty to
# disable level validation by default. Each inner list is a peer set at
# that rank.
# levels = [["milestone"], ["initiative"], ["story"], ["task", "spike"]]
```

`config.Config` gains:

```go
Taxonomy TaxonomyConfig `mapstructure:"taxonomy" toml:"taxonomy" json:"taxonomy"`

type TaxonomyConfig struct {
    Levels [][]string `mapstructure:"levels" toml:"levels" json:"levels"`
}
```

`config.Validate` calls `domain.Taxonomy(cfg.Taxonomy.Levels).Validate()` when `len(cfg.Taxonomy.Levels) > 0`.

### 4.2 Resolution

Single source of truth: `ProjectService.EffectiveTaxonomy`.

```go
type TaxonomySource int
const (
    TaxonomySourceNone TaxonomySource = iota
    TaxonomySourceWorkspace
    TaxonomySourceProjectOverride
)

func (s *ProjectService) EffectiveTaxonomy(p *domain.Project) (domain.Taxonomy, TaxonomySource)
```

Resolution order:

1. `p.Settings.Taxonomy` is non-nil (including `&empty` explicit opt-out) → that value. Source: `ProjectOverride`.
2. `cfg.Taxonomy.Levels` is non-empty → that value. Source: `Workspace`.
3. Otherwise → empty Taxonomy. Source: `None`.

`ProjectService` holds a read-only `*config.Config` reference, set via constructor — mirrors how the urgency engine wires defaults.

## 5. Validator

`domain.TaxonomyValidator` is pure — no repository access, no locks.

```go
type ValidationContext struct {
    Taxonomy    Taxonomy
    ParentLevel *string // nil if task has no parent; "" if parent has no level
}

type TaxonomyValidator struct{}

func (TaxonomyValidator) Check(vc ValidationContext, task *Task) error
```

Rules:

1. Empty taxonomy → accept any task state (early return nil).
2. `task.Level == nil || *task.Level == ""` → `TaxonomyError{Reason: "missing"}`.
3. `task.Level` not in taxonomy → `TaxonomyError{Reason: "unknown_level"}`.
4. No parent → task rank must be 0, else `TaxonomyError{Reason: "root_requires_top_rank"}`.
5. Has parent → parent's rank must be strictly less than task's rank, else `TaxonomyError{Reason: "parent_rank_not_lower"}`.

## 6. Service Wiring

`TaskService` gains:

- `projectSvc *ProjectService` field (DI rewire in `cmd/tusk/`).
- `validateTaxonomy(ctx, bundle, task)` helper:
  1. Load project, get effective taxonomy. If empty → return nil.
  2. If `task.ParentID != nil`, load parent from the same bundle, extract `Level`.
  3. Call `TaxonomyValidator{}.Check(ValidationContext{...}, task)`.

Invoked from:

- `TaskService.Create` — after the parent-existence check, before `bundle.WriteTx.WithTx(...)`.
- `TaskService.Update` — when `upd.Level != nil` OR `upd.ParentID != nil` OR `upd.ProjectID != nil`, after the in-memory merge and parent-cycle check, before `applyValidatedUpdate`.

All other service methods (`Start`, `Claim`, `Release`, `Complete`, `Delete`, `Pop`) skip taxonomy validation — they never touch level/parent/project.

### 6.1 Event log

`snapshotTask` captures `Level` (as `*string`). `diffTaskFields` compares via `stringPtrEqual` and emits a `"level"` field change. `task_modified` events pick it up automatically. Undo ships in v0.16; by the time it lands, the event log already carries level diffs, so no undo-specific changes are needed here.

## 7. CLI Grammar

### 7.1 Task commands

```bash
tusk task create "Build auth" project=backend level=story
tusk task modify a3f8b2c1 level=spike
tusk task modify a3f8b2c1 level=   # clears (modify only; rejected on create)
```

- `level=` inline field routes to `CreateTaskInput.Level *string` / `TaskUpdate.Level **string`.
- Empty value on modify → `*nil` (clear). Empty value on create → reject at parse time.
- No token prefix modifier accepted (`+level=` / `-level=` rejected).

### 7.2 Project commands

```bash
tusk project modify backend taxonomy.levels=milestone:initiative:story:(task,spike)
tusk project modify backend taxonomy.levels=       # clear override → inherit workspace default
tusk project modify backend taxonomy.disable=true  # explicit opt-out
tusk project modify backend taxonomy.disable=false # same as taxonomy.levels=
tusk project modify backend taxonomy=@./taxonomy.json
```

Inline taxonomy grammar (Section 7.4): `:` separates ranks, `(a,b,c)` marks peer sets. The inline-reference expander expands `@./file` into the raw string; if the field key is bare `taxonomy` (no suffix), the handler decodes the expanded content as JSON (`{"ranks": [[...]]}`).

Mutual exclusion: specifying both `taxonomy.levels=...` and `taxonomy.disable=true` in one call → rejected at parse time. No modifier accepted on any taxonomy key.

`internal/tui/project_parse.go` gains:

```go
type taxonomyAction int
const (
    taxonomyActionNone  taxonomyAction = iota // field absent
    taxonomyActionClear                        // clear override → inherit
    taxonomyActionSet                          // replace with TaxonomyValue
    taxonomyActionEmpty                        // explicit opt-out
)

type projectModifyFields struct {
    // …existing fields…
    TaxonomyAction taxonomyAction
    TaxonomyValue  domain.Taxonomy
}
```

### 7.3 Config commands

```bash
tusk config set taxonomy.levels "milestone:initiative:story:(task,spike)"
tusk config set taxonomy.levels ""   # deletes the [taxonomy] section
tusk config get taxonomy.levels      # renders the inline string
```

`config/write.go` teaches the setter about `taxonomy.levels` — parses via the inline parser, validates via `domain.Taxonomy.Validate()`, writes the `[taxonomy]` block. Empty string value removes the section.

### 7.4 Inline taxonomy parser

`internal/tui/taxonomy_parse.go` (new):

```go
// ParseTaxonomyInline parses "milestone:initiative:story:(task,spike)" into a
// domain.Taxonomy. Splits on ':' at top level (outside parens); groups wrapped
// in "(...)" split by ','.
func ParseTaxonomyInline(s string) (domain.Taxonomy, error)

// FormatTaxonomyInline is the inverse: single-peer ranks emit bare names,
// multi-peer ranks emit "(a,b,c)", joined by ':'.
func FormatTaxonomyInline(t domain.Taxonomy) string
```

Validation of the parsed result is delegated to `domain.Taxonomy.Validate()`.

## 8. MCP Surface

### 8.1 Task tools

`tusk_task_create` and `tusk_task_modify` gain an optional `level` string parameter:

- Omitted → `Level = nil` on create / no change on modify.
- Empty string on modify → `Level = *nil` (clear).
- Non-empty string → `Level = *&value`.
- Empty string on create → rejected.

`taskResponse` (in `internal/mcp/tools.go`) extends with:

```go
Level *string `json:"level,omitempty"`
```

Populated from `t.Level` in every tool that returns a task. `omitempty` keeps responses clean for level-free workspaces.

### 8.2 Project tools

`tusk_project_modify` accepts a new optional `taxonomy` object with tristate semantics:

| Payload                             | Meaning                                        |
| ----------------------------------- | ---------------------------------------------- |
| omitted                             | no change                                      |
| `"taxonomy": null`                  | clear override (inherit workspace default)     |
| `"taxonomy": {"ranks": []}`         | explicit opt-out                               |
| `"taxonomy": {"ranks": [[...]]}`    | replace with this value                        |

`tusk_project_get` / `tusk_project_list` responses extend with:

```json
{
  "name": "backend",
  "workflow": "kanban",
  "settings": { "taxonomy": {"ranks": [["milestone"], ...]} },
  "effective_taxonomy": {
    "ranks": [["milestone"], ...],
    "source": "project_override"
  }
}
```

- `settings.taxonomy` mirrors the raw project override (tristate preserved: absent / `{"ranks": []}` / populated).
- `effective_taxonomy` is the resolved result — agents read one field instead of reimplementing the resolution chain. `source` is `"workspace_default"` | `"project_override"` | `"none"`.

### 8.3 Field-level blocking

`taxonomy` is a legitimate field target for `mcp.blocked_fields.tusk_project_modify`. Defaults ship with `tusk_project_modify` already in `disabled_tools`, so no policy change is required out of the box; operators that opt agents into project edits can selectively block `taxonomy`.

### 8.4 Optimistic locking

Unchanged. Taxonomy edits bump `project.Version` through the existing `tusk_project_modify` write path; level changes bump `task.Version` through the existing `tusk_task_modify` write path.

## 9. Filter Grammar

`domain.TaskFilter` gains:

```go
Levels []string // OR match, mirrors Statuses
```

`filter/resolve.go` routes `level=` before the `uda.` prefix check — `level=` wins over any pre-existing `uda.level` usage:

```go
case "level":
    tf.Levels = strings.Split(field.Value, ",")
```

`sqlite/task.go` list query adds `level IN (?, ?, ...)` when `filter.Levels` is non-empty, mirroring the `Statuses` predicate.

Boolean operators (`NOT level=story`, `level=task OR level=spike`) compose through the existing `AndFilter` / `OrFilter` / `NotFilter` machinery with no additional work.

## 10. `tusk task level-check`

New service method:

```go
type LevelViolation struct {
    Task     *domain.Task
    Taxonomy domain.Taxonomy
    Source   TaxonomySource
    Err      *domain.TaxonomyError
}

func (s *TaskService) LevelCheck(ctx context.Context, filter domain.FilterExpr) ([]LevelViolation, error)
```

Walks tasks matching `filter` (default: whole workspace, all statuses including terminal). For each task, resolves the project's effective taxonomy, loads the parent's level if any, and runs `TaxonomyValidator.Check`. Collects violations without mutating.

CLI `tusk task level-check`:

- Inline filter syntax identical to `tusk task list` (`project=…`, `tree=…`, `status=…`, `level=…`, etc.).
- Text output: one line per violation — `<short_id>  <project>  <level>  <reason>  (parent: <parent_level>)` with colorized reason codes.
- `--output json`: array of `{"task": taskResponse, "reason": "...", "taxonomy": {"ranks": [...]}, "source": "..."}` objects.
- Exit code: 0 when no violations, 1 when any are found.

No MCP surface in this initiative.

## 11. Display Surfaces

- `tusk task get` — renders `Level: <name>` when the task's project has a non-empty effective taxonomy; hidden otherwise.
- `tusk task tree` — appends `[<level>]` to each node when effective taxonomy is non-empty.
- `tusk task list` — no default level column (keeps narrow terminals readable); JSON output serializes `level` via the shared task response (omitted when nil, same `omitempty` contract as MCP responses).
- `tusk project show` — renders the effective taxonomy + provenance marker:
  - `Taxonomy: milestone:initiative:story:(task,spike)` / `source: project override` (or `workspace default`)
  - `Taxonomy: (disabled; project opted out)` when override is the explicit empty.
  - `Taxonomy: (none)` when neither override nor workspace default is set.
- `tusk config show` — renders the workspace default under a `[taxonomy]` block via `FormatTaxonomyInline`.

## 12. Error Rendering

CLI and MCP error paths map `ErrTaxonomyViolation` → structured messages keyed on `TaxonomyError.Reason`:

| Reason                   | Message                                                                                              |
| ------------------------ | ---------------------------------------------------------------------------------------------------- |
| `missing`                | `project <name> requires a level; supply level=<top-rank peers>` (or any rank if modifying)           |
| `unknown_level`          | `level <name> is not in the taxonomy for <project>: <inline-rendered taxonomy>`                       |
| `root_requires_top_rank` | `root tasks must use the top-rank level (<top-rank peers>); got <name>`                               |
| `parent_rank_not_lower`  | `<level> cannot sit under <parent_level> — parent rank must be strictly lower`                        |

MCP tool-error envelope surfaces the structured payload as `{"code": "taxonomy_violation", "level": "...", "parent_level": "...", "reason": "..."}`.

## 13. Test Plan

### 13.1 Unit

- `domain/taxonomy_test.go` — `Validate`, `RankOf`, `IsTopRank`, `Contains`, `Clone`; malformed taxonomies (duplicate, empty rank, bad regex, zero ranks).
- `domain/taxonomy_validator_test.go` — each of the four violation reasons; empty taxonomy accepts anything; root/top-rank check; parent rank strict-less.
- `service/task_taxonomy_test.go` — Create/Update invocation points, level change alone, parent change alone, project reassignment (success and rejection), `upd.Level = *nil` clear, event-diff includes `level`.
- `service/project_test.go` — `EffectiveTaxonomy` returns each of the four source/value combinations; ProjectSettings tristate round-trips through JSON.
- `config/config_test.go` — loads taxonomy from TOML; malformed taxonomy rejected in `Validate`.
- `config/write_test.go` — `tusk config set taxonomy.levels` round-trip; delete via empty value.
- `internal/tui/taxonomy_parse_test.go` — inline parser/formatter round-trip; reject cases.
- `internal/tui/project_parse_test.go` — all taxonomyAction variants; mutual exclusion; modifier rejection.
- `internal/mcp/project_handlers_test.go` — `taxonomy` tristate on modify; blocked-fields coverage; `effective_taxonomy` shape.
- `internal/mcp/handlers_test.go` — `level` on every task tool request and response.
- `filter/resolve_test.go` — `level=a`, `level=a,b`, `NOT level=x`, ensure legacy `uda.level` no longer aliases.
- `sqlite/task_test.go` — `level` persists; filter predicate works.

### 13.2 E2E (`tests/e2e/`)

- `levels_basic` — workspace taxonomy, project, tasks at each rank, verify rendering.
- `levels_project_override` — project override with provenance.
- `levels_opt_out` — `taxonomy.disable=true` with workspace default elsewhere; unlevelled tasks accepted.
- `levels_validation` — all four `TaxonomyError` reasons surface with correct exit codes.
- `levels_prospective` — taxonomy added after task creation; `level-check` surfaces violation; untouched modifies still succeed.
- `levels_reassign` — move task between projects with differing taxonomies.
- `levels_level_check` — filter-scoped level-check, text + JSON, exit codes.

Undo is out of scope for this initiative (ships in v0.16); the `task_modified` event diff already carries level changes, so when undo lands it will cover level reverts without additional work.

## 14. Open Questions

None at spec time. All three clarifying questions resolved:

- Q1 (level-check scope) → in scope.
- Q2 (event coverage) → include in `task_modified`; undo covers level changes.
- Q3 (level-check surface) → filter-aware, no `--fix`, no MCP.

## 15. References

- `ROADMAP.md § v0.13 — Task Level Taxonomy` — source initiative.
- `PRODUCT.md § Task Levels` — user-facing description.
- `docs/plans/v0.13-task-level-taxonomy/phase-1…5-*.md` — phased implementation plans (separate docs).
