# Phase 4 — Project taxonomy CLI/MCP, filter routing, config CLI

Initiative: v0.13 Task Level Taxonomy
Design spec: `docs/superpowers/specs/2026-04-22-task-level-taxonomy-design.md`

## Prerequisites

Phase 3 merged. Specifically:

- `TaskService` validates taxonomy on `Create` / `Update` and emits `level` in `task_modified` event diffs.
- CLI `tusk task create` / `modify` accept `level=` inline.
- MCP `tusk_task_create` / `tusk_task_modify` accept `level`; every task response emits `level` with `omitempty`.
- `domain.TaskFilter.Levels` exists; `sqlite/task.go` list query filters on it.

## Inherits From

Codebase state at end of Phase 3:

- End-to-end task-level path works when the workspace default taxonomy is set in `tusk.toml` (hand-edited).
- `ProjectSettings.Taxonomy` is persisted but has no CLI or MCP setter.
- `filter/resolve.go` has no `level=` route — `TaskFilter.Levels` is only reachable from Go code.
- `tusk config set taxonomy.levels ...` is not yet accepted by the writer.

## Goal

Expose taxonomy management on every remaining surface: per-project CLI override, MCP project tool, filter grammar, and the config CLI. After this phase, an operator can set up a workspace default, override it per project, filter tasks by level, and read back the effective taxonomy — all without hand-editing TOML or JSON.

## Tasks

### Task 4.1 — Inline taxonomy parser / formatter

Create `internal/tui/taxonomy_parse.go`:

```go
package tui

import (
    "fmt"
    "strings"

    "github.com/germanamz/tusk/domain"
)

// ParseTaxonomyInline parses "milestone:initiative:story:(task,spike)" into a
// domain.Taxonomy. Splits on ':' at top level (outside parens); a segment
// wrapped in "(...)" splits its body by ',' to form a peer group; a bare
// segment becomes a single-element group.
//
// Whitespace inside groups is trimmed. Empty input returns an empty
// domain.Taxonomy. Delegates structural validation to domain.Taxonomy.Validate.
func ParseTaxonomyInline(s string) (domain.Taxonomy, error)

// FormatTaxonomyInline renders the inverse. Single-peer ranks emit the bare
// level name; multi-peer ranks emit "(a,b,c)". Ranks joined by ':'.
// Returns "" for empty taxonomies.
func FormatTaxonomyInline(t domain.Taxonomy) string
```

Implementation notes:

- The parser walks the string tracking paren depth to correctly split only top-level `:`. A naive `strings.Split` won't work because rank boundaries sit at the same character used inside groups is not allowed (commas separate peers, colons do not appear inside groups).
- After parsing, call `result.Validate()` and return any error verbatim.
- Formatter is straightforward: loop ranks, emit each with / without parens depending on `len(rank) > 1`.

Create `internal/tui/taxonomy_parse_test.go`:

- Round-trip: `parse(format(t)) == t` for several taxonomies including all-single, all-multi, and mixed.
- Reject: unmatched `(`, unmatched `)`, empty groups `()`, duplicate level names, names with invalid characters.
- Whitespace tolerance: `" milestone : (task , spike) "` → `{{"milestone"}, {"task", "spike"}}`.
- Empty input: returns empty taxonomy, no error.

### Task 4.2 — `tusk project modify` taxonomy fields

Edit `internal/tui/project_parse.go`:

- Add types/fields:
  ```go
  type taxonomyAction int
  const (
      taxonomyActionNone  taxonomyAction = iota
      taxonomyActionClear                        // clear override → inherit workspace default
      taxonomyActionSet                          // replace with TaxonomyValue
      taxonomyActionEmpty                        // explicit opt-out
  )

  type projectModifyFields struct {
      // ...existing fields...
      TaxonomyAction taxonomyAction
      TaxonomyValue  domain.Taxonomy
  }
  ```

- Extend `parseProjectModify` to handle four keys:
  - `taxonomy.levels=<inline>` → `action = Set`, parse via `ParseTaxonomyInline`, validate.
  - `taxonomy.levels=""` (empty value) → `action = Clear`.
  - `taxonomy.disable=true` → `action = Empty`.
  - `taxonomy.disable=false` → `action = Clear`.
  - `taxonomy=@./file.json` (bare `taxonomy`, no suffix) — the inline `@` reference expander is invoked *explicitly* by the handler, not automatically. Before calling `parseProjectModify`, the `tusk project modify` handler must run each field value that may contain `@` references through `expandRefs` (see `internal/tui/expand.go`; follow the pattern used for `description=@...` in `internal/tui/commands.go`). After expansion, the `taxonomy=` value arrives as raw JSON text. Decode it as:
    ```json
    {"ranks": [["milestone"], ["initiative"], ["story"], ["task", "spike"]]}
    ```
    via an intermediate struct:
    ```go
    var payload struct {
        Ranks [][]string `json:"ranks"`
    }
    if err := json.Unmarshal([]byte(value), &payload); err != nil { ... }
    tax := domain.Taxonomy(payload.Ranks)
    if err := tax.Validate(); err != nil { ... }
    ```
    Assign `TaxonomyValue = tax`, `action = Set`. Reject when the bare `taxonomy` value is not valid JSON of this shape.

  Implementer note: `domain.Taxonomy` is `[][]string` (from Phase 1), not a struct. Do not try to unmarshal directly into `domain.Taxonomy` — the `{"ranks": [...]}` wrapper requires the intermediate struct shown above. The same shape is mirrored on the MCP side (Task 4.3).
  - Reject any modifier (`+taxonomy.levels=`, `-taxonomy.disable=`, etc.).

- Guard mutual exclusion: if both `taxonomy.levels=<non-empty>` and `taxonomy.disable=true` appear in the same call, return a parse error naming both.

- Extend the handler (the `tusk project modify` execution path, `internal/tui/project.go` or equivalent) to apply `TaxonomyAction` to `project.Settings.Taxonomy`:
  | Action   | Effect                          |
  | -------- | ------------------------------- |
  | `None`   | leave `Settings.Taxonomy` alone |
  | `Clear`  | `Settings.Taxonomy = nil`       |
  | `Empty`  | `Settings.Taxonomy = &domain.Taxonomy{}` |
  | `Set`    | `Settings.Taxonomy = &TaxonomyValue` |

Extend `internal/tui/project_parse_test.go` covering each action variant, the mutual-exclusion error, modifier rejection, and the JSON-via-`@` path.

### Task 4.3 — MCP `tusk_project_modify` + response shape

Edit `internal/mcp/project_handlers.go` and `internal/mcp/tools.go` (wherever the project modify input schema is defined):

- Add an optional `taxonomy` object to the `tusk_project_modify` input. Tristate semantics follow spec § 8.2:
  | Payload                             | Meaning                                  |
  | ----------------------------------- | ---------------------------------------- |
  | field omitted                       | no change                                |
  | `"taxonomy": null`                  | clear override                           |
  | `"taxonomy": {"ranks": []}`         | explicit opt-out                         |
  | `"taxonomy": {"ranks": [["...", ...]]}` | replace with this value              |

  Decode with a pointer-to-struct so we can distinguish omitted vs null. The wrapper must carry its own nullability marker — the simplest shape is a `*TaxonomyPayload` where `TaxonomyPayload{ Ranks [][]string }`, combined with a top-level flag indicating "caller passed null". Match whatever pattern the MCP framework exposes for tristate optional fields. If no native pattern exists, decode the raw JSON into `json.RawMessage`, then hand-parse the three states.

- In the project-modify handler:
  1. Omitted → leave `project.Settings.Taxonomy` alone.
  2. `null` → set to `nil`.
  3. `{"ranks": []}` → set to `&domain.Taxonomy{}`.
  4. `{"ranks": [...]}` → marshal via `domain.Taxonomy.Validate`; on success set `&value`; on failure return a tool error.

- Extend `projectResponse` (or the equivalent response type) with:
  ```go
  type effectiveTaxonomyResponse struct {
      Ranks  [][]string `json:"ranks"`
      Source string     `json:"source"` // "workspace_default" | "project_override" | "none"
  }

  // In the project response:
  Settings struct {
      // ...existing fields...
      Taxonomy *taxonomyPayload `json:"taxonomy,omitempty"` // mirrors project override tristate
  } `json:"settings"`
  EffectiveTaxonomy effectiveTaxonomyResponse `json:"effective_taxonomy"`
  ```
  `settings.taxonomy` preserves tristate (`omitempty` when `Settings.Taxonomy == nil`; empty `ranks` array when `&empty`; populated otherwise). `effective_taxonomy` is always present and derived via `ProjectService.EffectiveTaxonomy`.

- Extend `internal/mcp/project_handlers_test.go` covering:
  - `tusk_project_modify` with each of the four tristate payload shapes → correct persisted override.
  - `tusk_project_get` response carries both `settings.taxonomy` and `effective_taxonomy` with correct `source` values.
  - `mcp.blocked_fields.tusk_project_modify = ["taxonomy"]` prevents taxonomy edits via MCP.

### Task 4.4 — Filter `level=` routing

Edit `filter/resolve.go`:

- Add `case "level":` to the switch in `resolveField`, positioned among the named cases (not in the `uda.` default clause):
  ```go
  case "level":
      tf.Levels = strings.Split(field.Value, ",")
  ```

  Position it between `case "status":` and `case "project":` to keep related "enum-like" matches together.

Extend `filter/resolve_test.go`:

- `level=story` → `TaskFilter.Levels == []string{"story"}`.
- `level=story,task` → `TaskFilter.Levels == []string{"story", "task"}`.
- `NOT level=spike` wrapped in a NotFilter resolves correctly.
- `uda.level=whatever` still routes to `TaskFilter.UDA` (no regression for legacy free-form use).

### Task 4.5 — `tusk config set taxonomy.levels`

Edit `config/write.go`:

- Locate the dispatch logic that routes `tusk config set <key> <value>` to typed writers.
- Add a branch for `key == "taxonomy.levels"`:
  - Empty value → delete the `[taxonomy]` section (or delete only the `levels` key, whichever keeps the TOML writer happy with reopens). Spec treats both equivalently since Phase 2's `Config.Validate` skips empty taxonomies.
  - Non-empty → `ParseTaxonomyInline(value)`; on success, call `domain.Taxonomy.Validate()`; on success, write `[taxonomy] levels = [[...], [...]]` to the active file.
- Support `tusk config get taxonomy.levels` — read `cfg.Taxonomy.Levels`, render via `FormatTaxonomyInline`. Return empty string when unset.

Extend `config/write_test.go`:

- Round-trip: `Set("taxonomy.levels", "milestone:story")` → re-read config → `cfg.Taxonomy.Levels == [][]string{{"milestone"}, {"story"}}`.
- Delete: `Set("taxonomy.levels", "")` on a file that has the section → subsequent `Load` returns empty taxonomy.
- Malformed: `Set("taxonomy.levels", "a::b")` returns an error; file is not modified.

### Task 4.6 — Tests recap and integration run

Confirm all new test files pass individually, then run the full suite:

```bash
make test
make test-race
make vet
make lint
```

Scenarios to manually smoke-test in a scratch workspace (these become formal E2E cases in Phase 5):

- `tusk project modify backend taxonomy.levels=milestone:story` then `tusk task create "x" project=backend level=story` succeeds.
- `tusk task list level=milestone project=backend` filters correctly.
- `tusk config set taxonomy.levels "epic:ticket"` persists; `tusk config get taxonomy.levels` returns `"epic:ticket"`.

## Changes Introduced

**New files:**
- `internal/tui/taxonomy_parse.go`
- `internal/tui/taxonomy_parse_test.go`

**Modified files:**
- `internal/tui/project_parse.go` — `taxonomyAction`, `TaxonomyValue`, parse branches, mutual-exclusion guard
- `internal/tui/project_parse_test.go` — coverage for each variant
- `internal/tui/project.go` (handler) — apply `TaxonomyAction` to `project.Settings.Taxonomy`
- `internal/mcp/project_handlers.go`, `internal/mcp/tools.go` — `taxonomy` input tristate, `settings.taxonomy` + `effective_taxonomy` response fields
- `internal/mcp/project_handlers_test.go` — tristate + blocked-fields coverage
- `filter/resolve.go` — `case "level":` branch
- `filter/resolve_test.go` — `level=` coverage + `uda.level` regression guard
- `config/write.go` — `taxonomy.levels` set/get branches
- `config/write_test.go` — round-trip, delete, malformed cases

**New environment variables / dependencies:** none.

**Schema migration:** none.

**Bridge code:** none. Every addition in this phase is directly consumed by its immediate caller or user.

## Behavioral Acceptance

- `tusk project modify <name> taxonomy.levels=milestone:initiative:story:(task,spike)` writes a project override; subsequent task creates honor it.
- `tusk project modify <name> taxonomy.disable=true` opts the project out of the workspace default.
- `tusk project modify <name> taxonomy=@./taxonomy.json` imports a structured JSON taxonomy.
- `tusk_project_modify` via MCP with the `taxonomy` object produces identical persisted state across all four tristate shapes.
- `tusk task list level=story`, `level=story,task`, and boolean variants (`NOT level=spike`, etc.) filter correctly.
- `tusk config set taxonomy.levels "..."` and `tusk config get taxonomy.levels` round-trip.
- `tusk_project_get` returns `effective_taxonomy` with the correct `source` for every resolution state.
- All Phase 1-3 acceptance criteria still hold.
- `make test`, `make test-race`, `make vet`, `make lint` pass.
