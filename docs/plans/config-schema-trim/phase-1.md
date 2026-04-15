# Phase 1 — DB-hydrated config show, and write rejection for projects/workflows

## Goal

Make `tusk config show`, `tusk config get`, `tusk config set`, and the MCP `tusk_config_set` tool treat projects and workflows as DB-owned. Reads go to the database; writes via config surfaces are rejected with a friendly pointer to `tusk project modify` / `tusk workflow modify`.

This phase does **not** touch the `config.Config` struct, `config/default.toml`, or `sqlite.SyncConfigToDB`. TOML files can still contain `[projects.*]` / `[workflows.*]` sections — phase 2 is responsible for removing the schema and rejecting legacy files at load time. This phase only changes what the *config command surface* reads and refuses to write.

## Prerequisites

- Base codebase at the commit where this plan is dropped in.
- No prior phases.

## Context the implementer must know

- `App` already holds `projectSvc *service.ProjectService` and `workflowSvc *service.WorkflowService` — see `internal/tui/app.go:31-32` and `:105-106`. No new plumbing is required to reach them from the config command.
- The current `runConfigShow` implementation lives at `internal/tui/config.go:79`. It marshals the entire `Config` struct (including the soon-to-go `Workflows` and `Projects` maps) with `go-toml/v2` and prints it. This phase keeps the struct intact but elides those two fields from the rendered output and appends DB-hydrated sections instead.
- The current `runConfigGet` lives at `internal/tui/config.go:240`. It validates keys via `config.IsValidKey`, then uses a Viper instance loaded from the marshaled Config to resolve dot paths. Keys under `projects.*` / `workflows.*` currently resolve against the TOML map values.
- The current `runConfigSet` lives at `internal/tui/config.go:355`. It uses the same IsValidKey gate and writes via Viper+go-toml.
- MCP `handleConfigSet` lives at `internal/mcp/config_handlers.go:37`. It already has a `storage.*` rejection at line 47 — a `readOnlyKeyError` helper can be introduced or the existing pattern extended.
- `config.IsValidKey` in `config/write.go:152` reflects over the `Config` struct. Since `Workflows` and `Projects` fields still exist in this phase, it will still report those prefixes as valid — the per-command prefix checks are what enforces the behavior in phase 1.
- Domain types for projection: `domain.Project` (with `Settings` containing `AutoCompleteParent`, `AutoRevertParent`, `Urgency`), `domain.Workflow` (with `Statuses map[string]domain.StatusConfig`, `Transitions []domain.WorkflowTransition`). Role names are `domain.StatusRole` strings.

## Tasks

### 1. Add a DB-side TOML renderer for projects and workflows

- Create `internal/tui/config_render.go`.
- Implement two exported helpers:
  - `func RenderWorkflowsTOML(workflows []*domain.Workflow) string`
  - `func RenderProjectsTOML(projects []*domain.Project, workflowsByID map[uuid.UUID]*domain.Workflow) string`
- Output format must match the shape that `config/default.toml` uses today so existing user muscle memory holds. Example:

    ```
    # workflows (from database — use `tusk workflow` to modify)

    [workflows.kanban.statuses.pending]
    roles = ["initial"]

    [workflows.kanban.statuses.active]
    roles = ["start", "highlight"]

    [[workflows.kanban.transitions]]
    from = "pending"
    to = "active"

    # projects (from database — use `tusk project` to modify)

    [projects.default]
    workflow = "kanban"

    [projects.backend]
    workflow = "kanban"

    [projects.backend.settings.urgency]
    blocking_weight = 15.0
    ```

- Iterate workflows and projects in sorted name order so output is deterministic (important for e2e golden stability).
- Only emit `[projects.<name>.settings.*]` sub-tables when the corresponding domain field is non-nil (mirror the `omitempty` behavior of the current TOML struct tags).
- Role and transition serialization uses the same raw strings the domain uses (`string(role)`, `transition.FromStatus`, `transition.ToStatus`).
- Write `internal/tui/config_render_test.go` covering: empty inputs, kanban-only default, a project with urgency overrides, a project with auto-complete/auto-revert settings, multi-workflow input. Assert deterministic ordering by calling the renderer twice and comparing output.

### 2. Rewrite `runConfigShow` to elide map fields and append DB sections

In `internal/tui/config.go:79` (`runConfigShow`):

- Load the config via `config.Load(a.loadOpts...)` as today.
- Define fresh local view types (in `internal/tui/config.go` or a new `internal/tui/config_view.go`). **Do not** alias or embed `config.ProjectConfig` / `config.WorkflowConfig` — those types are slated for removal in a later phase, and aliasing would create a latent compile break. Define new types with hand-written JSON/TOML tags that replicate the field layout:

    ```go
    // TOML path: a wrapper we marshal then string-append DB sections below.
    type configShowTOML struct {
        Storage config.StorageConfig `toml:"storage"`
        Urgency config.UrgencyConfig `toml:"urgency"`
        TUI     config.TUIConfig     `toml:"tui"`
        MCP     config.MCPConfig     `toml:"mcp"`
    }

    // JSON path: a single object flattening globals plus DB-sourced maps.
    type configShowJSON struct {
        Storage   config.StorageConfig    `json:"storage"`
        Urgency   config.UrgencyConfig    `json:"urgency"`
        TUI       config.TUIConfig        `json:"tui"`
        MCP       config.MCPConfig        `json:"mcp"`
        Projects  map[string]projectJSON  `json:"projects"`
        Workflows map[string]workflowJSON `json:"workflows"`
    }

    type projectJSON struct {
        Workflow string               `json:"workflow"`
        Settings projectSettingsJSON  `json:"settings"`
    }

    type projectSettingsJSON struct {
        AutoCompleteParent *autoConfigJSON    `json:"auto_complete_parent,omitempty"`
        AutoRevertParent   *autoConfigJSON    `json:"auto_revert_parent,omitempty"`
        Urgency            *urgencyOverrideJSON `json:"urgency,omitempty"`
    }

    type autoConfigJSON struct {
        TriggerStatus string `json:"trigger_status"`
        TargetStatus  string `json:"target_status"`
    }

    type urgencyOverrideJSON struct {
        PriorityWeight    *float64 `json:"priority_weight,omitempty"`
        DueWeight         *float64 `json:"due_weight,omitempty"`
        AgeWeight         *float64 `json:"age_weight,omitempty"`
        ActiveWeight      *float64 `json:"active_weight,omitempty"`
        BlockingWeight    *float64 `json:"blocking_weight,omitempty"`
        BlockedWeight     *float64 `json:"blocked_weight,omitempty"`
        TagsWeight        *float64 `json:"tags_weight,omitempty"`
        ProjectWeight     *float64 `json:"project_weight,omitempty"`
        AnnotationsWeight *float64 `json:"annotations_weight,omitempty"`
        WaitingWeight     *float64 `json:"waiting_weight,omitempty"`
    }

    type workflowJSON struct {
        Statuses    map[string]workflowStatusJSON `json:"statuses"`
        Transitions []workflowTransitionJSON      `json:"transitions"`
    }

    type workflowStatusJSON struct {
        Roles []string `json:"roles"`
    }

    type workflowTransitionJSON struct {
        From string `json:"from"`
        To   string `json:"to"`
    }
    ```

  These view types are independent of the `config.*` struct tree, so phase 2's schema trim has no effect on them.
- **Text output path** (`a.format != "json"`):
  1. Build a `configShowTOML` from the loaded config scalars.
  2. Marshal it with `go-toml/v2`.
  3. Emit the existing `# active: <path>` header, then the marshaled scalars, then a blank line, then the output of `RenderWorkflowsTOML` and `RenderProjectsTOML` fetched via `a.projectSvc.List(ctx)` and `a.workflowSvc.List(ctx)`.
  4. Build the `map[uuid.UUID]*domain.Workflow` lookup table once from the workflow list and pass to the project renderer.
- **JSON output path** (`a.format == "json"`):
  1. Build a `configShowJSON`. Populate storage/urgency/tui/mcp from the loaded config.
  2. Populate the `Projects` and `Workflows` maps from the domain results of `projectSvc.List` / `workflowSvc.List`. Write a local converter `projectToJSON(p *domain.Project, wfByID map[uuid.UUID]*domain.Workflow) projectJSON` and `workflowToJSON(w *domain.Workflow) workflowJSON` to map domain → view types.
  3. Emit via `json.NewEncoder(out).Encode(payload)`.
- Update `internal/tui/commands_test.go` tests that assert `config show` output to cover both TOML and JSON paths with a custom project present.

### 3. Route `runConfigGet` for projects/workflows to the database

In `internal/tui/config.go:240` (`runConfigGet`):

- At the top of the function, detect keys whose first segment is `projects` or `workflows`. Use `strings.SplitN(key, ".", 2)` and a switch on the leading segment.
- For these keys, call a new local helper `configGetFromDB(ctx context.Context, key string) (any, error)` that walks the dot path against service outputs:
  - `projects.<name>` → `projectSvc.GetByName(ctx, name)` — return the project as a JSON-shaped map (same shape the renderer produces).
  - `projects.<name>.workflow` → look up the project, then `workflowSvc.GetByID(...)` to get the workflow name.
  - `projects.<name>.settings.auto_complete_parent.trigger_status` etc. → drill into the domain struct, return typed leaf values or `nil`.
  - `projects.<name>.settings.urgency.<field>` → return the pointer value if set, `nil` otherwise.
  - `workflows.<name>` → return full workflow as JSON shape.
  - `workflows.<name>.statuses.<status>.roles` → return `[]string` of role names.
  - `workflows.<name>.transitions` → return list of `{from, to}` objects.
- On unknown project/workflow names or unknown leaf segments, return the same `fmt.Errorf("unknown config key: %q", key)` error used by the existing Viper path.
- For any scalar return, format per the existing switch (string/bool/int/float → one line; complex → indented JSON).
- For non-`projects.*`/`workflows.*` keys, keep the existing Viper-backed flow untouched.
- Update tests in `internal/tui/commands_test.go` to cover: `config get projects.default.workflow`, `config get workflows.kanban.statuses.pending.roles`, `config get projects.default.settings.urgency.blocking_weight` (unset → `nil` / empty output), and an unknown project.

### 4. Reject `projects.*` / `workflows.*` writes in `runConfigSet`

In `internal/tui/config.go:355` (`runConfigSet`):

- After reading `key := args[0]`, before `config.IsValidKey`, add a prefix check:

    ```go
    if strings.HasPrefix(key, "projects.") {
        return fmt.Errorf("projects.* is managed by the database — use `tusk project modify` instead")
    }
    if strings.HasPrefix(key, "workflows.") {
        return fmt.Errorf("workflows.* is managed by the database — use `tusk workflow modify` instead")
    }
    ```

- Add unit tests in `internal/tui/commands_test.go` asserting both error messages and that no file write occurs.

### 5. Reject `projects.*` / `workflows.*` writes in MCP `handleConfigSet`

In `internal/mcp/config_handlers.go:37`:

- After the existing `storage.*` guard at line 47, add two parallel guards returning `mcp.NewToolResultError` with the same wording used in task 4 (CLI) so users get a consistent message regardless of surface.
- Introduce a small local helper `func readOnlyKeyError(prefix, cmd string) string { return fmt.Sprintf("%s is managed by the database — use `%s` instead", prefix, cmd) }` if it reduces duplication; otherwise inline the two literals. Do not refactor the existing `storage.*` branch — that gets trimmed in phase 2 only if convenient; it is orthogonal to this phase.
- Add a test in `internal/mcp/config_handlers_test.go` covering both prefixes and asserting the error strings.

## Acceptance criteria (user-visible behaviors after phase 1)

1. `tusk config show` still prints a complete config, now with `[projects.*]` and `[workflows.*]` sections sourced from the database and preceded by `# ... (from database ...)` headers. Deterministic ordering.
2. `tusk config show --output json` emits a single JSON object with storage/urgency/tui/mcp at the top level plus `projects` and `workflows` keys populated from the DB.
3. `tusk config get projects.default.workflow` prints `kanban`. `tusk config get workflows.kanban.statuses.pending.roles` prints `["initial"]` (or one-per-line for text format, JSON array for json format).
4. `tusk config set projects.foo.workflow kanban` fails with `projects.* is managed by the database — use \`tusk project modify\` instead`. No file is written.
5. `tusk config set workflows.foo.statuses.x.roles initial` fails with the analogous workflow message.
6. MCP `tusk_config_set` with `key=projects.foo.workflow` or `key=workflows.kanban.x` returns the same error text.
7. All prior behaviors preserved: scalar `config get`/`set` for `storage.*` (read-only via MCP), `urgency.*`, `tui.*`, `mcp.*`, `config init`, `config path`, `config edit`, `config validate` all unchanged.

## Out of scope

- Schema removal. `config.Config.Workflows` / `.Projects` and all related struct types remain defined. TOML files containing `[projects.*]` / `[workflows.*]` still load without error. `sqlite.SyncConfigToDB` still runs at startup. All of these are phase 2.
- Cleanup of the existing `storage.*` rejection wording.

## Changes Introduced

**New files**

- `internal/tui/config_render.go` — `RenderWorkflowsTOML`, `RenderProjectsTOML`.
- `internal/tui/config_render_test.go` — renderer unit tests.
- `internal/tui/config_view.go` (optional — may also live inline in `config.go`) — the `configShowTOML`, `configShowJSON`, and associated `projectJSON` / `workflowJSON` view types.

**Modified files**

- `internal/tui/config.go` — `runConfigShow` rewritten to marshal a fresh scalar-only wrapper and append DB-sourced sections. `runConfigGet` gains DB routing for `projects.*` / `workflows.*` keys. `runConfigSet` gains prefix rejection.
- `internal/tui/commands_test.go` — tests updated/added for `config show`, `config get`, `config set` on DB-sourced keys.
- `internal/mcp/config_handlers.go` — `handleConfigSet` gains two new prefix guards.
- `internal/mcp/config_handlers_test.go` — new rejection tests.

**Unchanged but referenced**

- `internal/tui/app.go` — `projectSvc` and `workflowSvc` fields are already wired; no changes required.

**Modified interfaces**: none. No public types touched.

**New environment variables**: none.

**Schema migrations**: none.

**Added dependencies**: none.

**Bridge code**: none. This phase is additive on the read side and a hard refusal on the write side; no stubs needed.
