# Phase 2 — Project description: service, CLI, MCP, codec, render

**Initiative:** ROADMAP.md Migration (v0.13)
**Spec:** `docs/superpowers/specs/2026-04-26-roadmap-md-migration-design.md`
**Prerequisites:** Phase 1.

## Inherits From

Phase 1 left the codebase in this state:

- `domain.Project` carries a `Description string` field, persisted by the SQLite repo and round-tripped through `Create` / `GetByID` / `GetByName` / `List` / `Update`.
- `service.CreateProjectInput.Description string` and `service.ModifyProjectInput.Description **string` exist on the input types but are **ignored** by the service bodies. They are tagged with `// TODO(phase-2): plumb description` comments.
- The CLI, MCP, portability codec, `tusk project show` renderer, and `tusk config show` renderer have **no** awareness of the new field.
- Migration `013_project_description` is committed; the `_default` project has `description = ""`.
- `make test` is green.

This phase wires `Description` through every consumer so the field becomes a first-class user-visible property.

## Goal

Every place a project is created, modified, displayed, exported, or imported must read or write `Description`. After this phase the prerequisite story for the Markdown rendering work is complete and the user can do `tusk project create tusk-roadmap workflow=kanban description=@./vision.md`.

## Tasks

### Task 1 — Service layer wiring

In `service/project.go`:

1. In `ProjectService.Create`, set `Description: input.Description` on the constructed `domain.Project` literal. Remove the `// TODO(phase-2): plumb description` comment on `CreateProjectInput.Description`.
2. In `ProjectService.Modify`, between the existing `WorkflowID` and `AutoComplete` blocks, add a description-mutation block that mirrors the `Description **string` semantics:
   ```go
   if input.Description != nil {
       if *input.Description == nil {
           p.Description = ""
       } else {
           p.Description = **input.Description
       }
   }
   ```
   Remove the `// TODO(phase-2): plumb description` comment on `ModifyProjectInput.Description`.
3. Add unit tests in `service/project_test.go`:
   - `TestProjectService_Create_WithDescription` — creates a project with a non-empty description and verifies it round-trips through `GetByName`.
   - `TestProjectService_Modify_SetDescription` — sets a description on an existing project (outer non-nil + inner non-nil pointer to a string).
   - `TestProjectService_Modify_ClearDescription` — clears via outer non-nil + inner nil pointer; verifies the field becomes `""`.
   - `TestProjectService_Modify_LeaveDescription` — outer nil leaves the field unchanged.

### Task 2 — Inline syntax parser hook

In `internal/tui/project_parse.go`:

1. Extend `projectCreateFields` to carry `Description string`.
2. Extend `projectModifyFields` to carry `Description **string` (mirroring the existing fields' double-pointer pattern; see `ProjectModifyFields.Workflow *string` for the shape).
3. In the parser body that walks scanned fields (search for the `case "workflow":` arm), add a `case "description":` arm that captures the field value into the corresponding output struct's `Description`. The inline `@` reference expander already runs on string values before the parser sees them, so `description=@./vision.md` and `description=@-` work for free — no extra plumbing is needed at the parser layer.
4. For modify, the empty-value form (`description=`) clears the field: outer non-nil pointer to inner nil. This matches the `level=` empty-value clear semantics already in the parser.
5. In `internal/tui/project_parse_test.go`, add table cases for:
   - `description=plain text`
   - `description=@./fixture.md` (use a temp file fixture)
   - `description=` on modify (clear)
   - `description=` on create (parses as empty string, equivalent to omitting it)

### Task 3 — `tusk project create` / `modify` commands and `tusk project show`

In `internal/tui/project.go`:

1. In the create command's RunE, pass the parsed `Description` into the `service.CreateProjectInput` literal.
2. In the modify command's RunE, pass the parsed `Description` (`**string`) into `service.ModifyProjectInput`.
3. In the `tusk project show` text renderer (look for the function that renders project name + workflow + settings — likely `renderProjectShow` in `internal/tui/render.go`), add a Description block immediately under the project name, only when non-empty:
   ```
   Name: tusk-roadmap
   Description:
     <multi-line description, indented 2 spaces>
   ```
   Empty description: omit the block entirely.
4. In the JSON renderer for `tusk project show` (the `projectShowJSON` shape), add a `description` string field. Always emit it (even when empty) — JSON consumers expect stable schemas.
5. Add an e2e scenario in `tests/e2e/` (extend an existing project test or add a small new file) that creates a project with `description=...`, runs `tusk project show <name>`, and asserts the description appears in both text and JSON output.

### Task 4 — MCP wiring

In `internal/mcp/server.go`:

1. Locate the `tusk_project_create` tool registration (around line 546) and add a `description` parameter:
   ```go
   mcp.WithString("description", mcp.Description("Optional project description (markdown).")),
   ```
2. Locate the `tusk_project_modify` tool registration (around line 573) and add the same `description` parameter (description on its own line; the empty-string convention clears the value, mirroring the level field elsewhere).
3. In `internal/mcp/project_handlers.go`:
   - In the create handler, read the `description` argument and pass it to `service.CreateProjectInput.Description`.
   - In the modify handler, when `description` is present in the request arguments, build the `**string` pointer dance: empty string → outer non-nil + inner nil; non-empty → outer non-nil + inner pointer to value. Match the pattern already used for the `level` field on the task modify handler.
4. In every place project payloads are returned to MCP (search for `projectMCPResponse` or whatever the response builder is — it's adjacent to the create/modify handlers and to the project list / project resource handlers in `internal/mcp/server.go`), add `description` to the response shape.
5. In `internal/mcp/project_handlers_test.go`, add a test case that:
   - Creates a project via `tusk_project_create` with a `description` argument.
   - Verifies the response payload contains the field.
   - Calls `tusk_project_modify` with `description: ""` and asserts the field is cleared on the next read.

### Task 5 — Portability codec + config show

In `internal/portability/portable.go`:

1. Add `Description string `json:"description"\`` to `PortableProject`.
2. The encoder is a vanilla `json.Encoder.Encode` — no changes to `internal/portability/encode.go`.
3. The decoder is also vanilla — no changes to `internal/portability/decode.go`. Existing JSON dumps without the field decode to the empty string, which is the correct "no description" state.

In `internal/portability/encode_test.go` and `decode_test.go`:

- Extend the round-trip test fixture to seed a description on at least one project, and assert the value comes back identical after `Encode → Decode`.

In `internal/tui/config_render.go` (or wherever `tusk config show` renders the read-only `[projects.*]` section — search for `projects.` in `internal/tui/config_*.go`):

- Add `description = "..."` line to each project block when non-empty. Keep the existing TOML-ish table format. Match the indentation of adjacent keys.

### Task 6 — Service-to-codec wiring (Export / Import paths)

In `service/portability.go` (or wherever `PortabilityService` translates between `domain.Project` and `PortableProject` — search for `toPortableProject` / `fromPortableProject`):

1. In the `domain.Project → PortableProject` direction (export), copy `Description`.
2. In the `PortableProject → domain.Project` direction (import), copy `Description`.

If the PortabilityService writes projects via `ProjectRepository.Create` directly (rather than through `ProjectService`), the new field must be set on the `domain.Project` value before the repo call. Verify by tracing import-side in `service/portability_*.go` — there is exactly one project insertion path.

Add a focused test in `service/portability_test.go` (or the existing `internal/portability/encode_test.go` round-trip):

- `TestPortability_RoundTrip_ProjectDescription` — exports a workspace with a populated project description, decodes the JSON back into a fresh test workspace, asserts the project description survives.

## User-visible behaviors (acceptance criteria)

After this phase, all of the following must work:

- `tusk project create tusk-roadmap workflow=kanban description="some text"` creates a project with that description.
- `tusk project create tusk-roadmap workflow=kanban description=@./vision.md` reads file content into the description (the inline `@` expander handles this).
- `tusk project modify tusk-roadmap description="new text"` updates the description.
- `tusk project modify tusk-roadmap description=` clears the description.
- `tusk project show tusk-roadmap` renders the description in both text and JSON output. Empty description: text omits the block, JSON emits `"description": ""`.
- `tusk_project_create` / `tusk_project_modify` MCP tools accept the `description` argument with the same semantics. Empty string on modify clears.
- `tusk export` includes `"description"` on every project in the JSON dump.
- `tusk import --input <path>` round-trips the field.
- `tusk config show` lists `description = "..."` under each project that has one.
- All Phase 1 acceptance criteria still hold (CLI/MCP unchanged for any caller that does not pass `description`).
- `make test` and `make test-race` pass.

## Bridge code introduced

None. This phase removes the bridges introduced in Phase 1 and introduces no new ones.

## Changes Introduced

- **No new files** (the migration is already in place; new tests extend existing test files where possible — only add new test files if a topic doesn't fit cleanly into an existing one).
- **Modified files:**
  - `service/project.go` (Create + Modify wiring; remove TODO comments)
  - `service/project_test.go` (new service tests)
  - `internal/tui/project_parse.go` (parser hooks for `description=` field)
  - `internal/tui/project_parse_test.go` (parse cases)
  - `internal/tui/project.go` (RunE plumbing)
  - `internal/tui/render.go` (project show description block)
  - `internal/tui/config_render.go` (project description in `config show`)
  - `internal/mcp/server.go` (tool param registration)
  - `internal/mcp/project_handlers.go` (handler arg parse + response shape)
  - `internal/mcp/project_handlers_test.go` (MCP-level tests)
  - `internal/portability/portable.go` (`PortableProject.Description`)
  - `internal/portability/encode_test.go` / `decode_test.go` (round-trip)
  - `service/portability.go` (or equivalent — codec ↔ domain mapping)
  - `service/portability_test.go` (round-trip test)
  - `tests/e2e/<chosen file>` (e2e coverage)
- **No schema migrations** (Phase 1 already added the column).
- **No new dependencies.**
- **Public API additions wired through:** all the type-level additions from Phase 1 are now consumed end-to-end.
