# Data Portability — Phase 4: CLI commands + ROADMAP edits + smoke verification

**Initiative:** v0.13 — Data Portability
**Spec:** `docs/superpowers/specs/2026-04-26-data-portability-design.md`
**Phase:** 4 of 4
**Prerequisites:** Phase 3 (PortabilityService) must be complete and merged.
**Can run in parallel with:** Nothing.

---

## Inherits From

After Phases 1, 2, and 3:

- **From Phase 1:** `service.WriteTx` exposes every entity-kind accessor; sqlite Tx has `Workflows()` and `Players()`; event payload type for `workspace_imported` exists in `domain/`.
- **From Phase 2:** `internal/portability` exposes `PortableWorkspace`, `Encode`, `Decode`, `ImportError`, `ImportIssue`, `SchemaVersion = 1`.
- **From Phase 3:** `service.PortabilityService` exists with `Export(ctx)` and `Import(ctx, ws, opts)`. `Client.Portability` is wired. The service handles validation, atomic apply, `--truncate` mode, optional `--dry-run`, and emits the `workspace_imported` event. **Library consumers can already use the feature.** This phase exposes it through the CLI and finishes the initiative.

You can rely on all of the above being in place. If any prior phase's symbols are missing, stop and verify the prior phases landed cleanly.

---

## Why this phase

Phase 4 closes the initiative by:

1. Adding the `tusk export` and `tusk import` CLI commands as thin wrappers over `Client.Portability`.
2. Adding e2e tests to the existing harness so the feature is exercised through the full CLI surface (and re-run in the 2×2 DB-config × output-format matrix the harness already enforces).
3. Applying the three ROADMAP.md edits described in the spec, so the roadmap reflects the as-shipped scope.
4. Producing a brief manual verification note from running smoke tests 1–8.

After this phase, the Data Portability initiative is fully shippable: feature live, roadmap up to date, and the human-side verification recorded.

---

## Tasks

### Task 1 — Create `internal/tui/export.go`

**New file:** `internal/tui/export.go`

Implement `(a *App) buildExportCmd() *cobra.Command` and `(a *App) runExport(cmd *cobra.Command, args []string) error`.

```go
func (a *App) buildExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export [--output <path>]",
		Short: "Export the workspace to a JSON dump",
		Long: `Export the entire workspace as a JSON dump suitable for backup,
migration, or rehydration via 'tusk import'.

Output is pretty-printed JSON; pipe through 'jq -c' for compact form.

JSON is the only format. The dump includes every workflow, project,
player, tag, task, relation, annotation, note, and event in the
workspace. The dump's schema_version is 1; future tusk versions may
introduce conversion shims for older versions.`,
		Example: `  # Write to stdout
  tusk export

  # Write to a file (atomic — written via *.tmp then rename)
  tusk export --output /tmp/ws.json`,
		RunE: a.runExport,
	}
	cmd.Flags().StringP("output", "o", "-", `path to write to; "-" for stdout`)
	return cmd
}
```

`runExport`:

1. Read the `--output` flag value.
2. Call `a.portabilitySvc.Export(cmd.Context())` (the `App` struct will need a new `portabilitySvc *service.PortabilityService` field — add it; wire it in Task 3 along with command registration).
3. If `--output == "-"`, encode straight to `cmd.OutOrStdout()` via `portability.Encode`.
4. Otherwise: write to `<path>.tmp` first, then `os.Rename(<path>.tmp, <path>)`. Use `os.CreateTemp` in the same directory as the target so the rename is on the same filesystem (otherwise `os.Rename` can fail across mounts). On any error, attempt to remove the tmp file and return the wrapped error.
5. Return any encoder error directly. The CLI's `SilenceUsage` / `SilenceErrors` configuration on the root command (already set in `app.go`) will surface the error cleanly.

### Task 2 — Create `internal/tui/import.go`

**New file:** `internal/tui/import.go`

Implement `(a *App) buildImportCmd() *cobra.Command` and `(a *App) runImport(cmd *cobra.Command, args []string) error`.

```go
func (a *App) buildImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import --input <path> [--replace] [--truncate] [--dry-run]",
		Short: "Import a JSON dump into the workspace",
		Long: `Import a JSON workspace dump produced by 'tusk export'. By default
fails on any collision (an entity in the dump whose ID already exists
in the workspace). Use --replace to overwrite collisions row-by-row,
or --replace --truncate to wipe every entity table before applying
the dump (wipe-and-restore mode).

Import preserves IDs, timestamps, and version numbers exactly so a
backup round-trips losslessly. Per-entity events are not emitted —
one workspace_imported event records the import.

Validation runs in a single pass before any writes, collecting every
issue (schema, FK, taxonomy, blocks-cycle, workflow well-formedness,
collision) so you see the full picture in one round-trip.`,
		Example: `  # Import from a file
  tusk import --input /tmp/ws.json

  # Restore over the existing workspace
  tusk import --input /tmp/ws.json --replace

  # Wipe and rehydrate from the dump
  tusk import --input /tmp/ws.json --replace --truncate

  # Validate without writing
  tusk import --input /tmp/ws.json --dry-run

  # Read from stdin
  cat ws.json | tusk import --input -`,
		RunE: a.runImport,
	}
	cmd.Flags().StringP("input", "i", "", `path to read from; "-" for stdin`)
	cmd.Flags().Bool("replace", false, "row-level upsert on collision")
	cmd.Flags().Bool("truncate", false, "wipe every entity table before applying; requires --replace")
	cmd.Flags().Bool("dry-run", false, "validate and report counts; no writes")
	_ = cmd.MarkFlagRequired("input")
	return cmd
}
```

`runImport`:

1. Read all four flag values.
2. **Reject `--truncate` without `--replace` at flag-parse time:** if `truncate && !replace`, return `fmt.Errorf("--truncate requires --replace")`. The service rejects this too, but failing at the CLI is faster and gives a clearer error.
3. Open the input source:
   - If `--input == "-"`, use `cmd.InOrStdin()`. **TTY guard:** before reading, check whether stdin is a terminal (use `term.IsTerminal(int(os.Stdin.Fd()))` or replicate however the existing `internal/tui/expand.go` does it for the `@-` case). If it is, return `fmt.Errorf("--input -: stdin is a terminal; pipe a file or omit the flag")`. Mirror the existing TTY guard's wording where possible.
   - Otherwise: `os.Open(path)` with deferred `Close()`.
4. Decode: `ws, err := portability.Decode(reader)`. If err is `*portability.ImportError`, render its issues (see Task 2b below) and return a non-zero exit error. If err is some other I/O error, return it directly.
5. Build options: `service.ImportOptions{Replace: replace, Truncate: truncate, DryRun: dryRun}`.
6. Call `report, err := a.portabilitySvc.Import(cmd.Context(), ws, opts)`.
7. If err is `*portability.ImportError`, render its issues and return a non-zero exit error.
8. Otherwise render the `report` (see Task 2b).

**Task 2b — Render helpers.** Add a small helper in the same file to render an `ImportReport` and an `ImportError`. Match the existing rendering pattern: respect the persistent `--format text` / `--format json` flag from `App`. For text, print one line per entity kind with counts (e.g. `tasks: 42 (3 replaced)`), the truncate flag, and the event ID. For JSON, marshal `ImportReport` directly.

Same for `ImportError`: text format prints one line per `ImportIssue` (e.g. `[fk] task a3f8b2c1: parent_id 00000000-... not found`); JSON marshals the issue list.

If the project already has a "render to format" helper (look in `internal/tui/render.go` or similar), use it. If not, two small functions in `import.go` are fine.

### Task 3 — Register both commands and wire `portabilitySvc` on `App`

**File:** `internal/tui/app.go`

1. Add a field on `App`:

```go
type App struct {
	// ...existing fields...
	portabilitySvc *service.PortabilityService
	// ...
}
```

2. In whatever constructor / wiring function builds an `App` (look for `NewApp` or where the existing `taskSvc`, `tagSvc`, etc. are assigned), assign `portabilitySvc` from the same wiring source. The constructor likely takes a parameter list of services; add `portabilitySvc *service.PortabilityService` to that parameter list and assign it.

3. In `cmd/tusk/main.go` (the binary entry point that constructs the `App`), wire the new service. The existing code likely already constructs each service from the SQLite `Store` and friends; mirror those patterns:

```go
portabilitySvc := service.NewPortabilityService(
	writeTx,
	taskSvc, projectSvc, workflowSvc, relationSvc,
	tagSvc, playerSvc, noteSvc,
	bundle,
	versionInfo.Version, // use the build-time version
)
app := tui.NewApp(/* ...existing args..., */ portabilitySvc /* , ... */)
```

The `versionInfo.Version` value is the same one passed to `tuskmcp.New(...)` — look for it in `app.go` around the `mcpCmd` setup.

4. Register both commands on the root in `app.go`:

```go
a.root.AddCommand(a.buildExportCmd())
a.root.AddCommand(a.buildImportCmd())
```

Place them next to the existing `a.root.AddCommand(a.buildTaskCmd())` block so the registration order stays scannable.

### Task 4 — E2E tests in `tests/e2e/portability_test.go`

**New file:** `tests/e2e/portability_test.go`

Use the existing e2e harness. Look at `tests/e2e/<existing>_test.go` files to confirm the `Scenario` / `Step` shape; the project comment in `CLAUDE.md` documents the pattern (steps can reference prior results with `$0.short_id`).

Cover at minimum:

1. **Full round-trip** (Smoke 1 in spec): create a few tasks, export to a file, run import into a fresh DB, re-export, diff the two JSON files modulo `exported_at` and the `workspace_imported` event. Use `jq` if it's already a test dependency; otherwise compute the diff in Go via `cmp.Diff` on decoded `PortableWorkspace` values.
2. **Stdin/stdout pipeline** (Smoke 2): pipe `tusk export` into `tusk import --replace --truncate --input -` against a fresh DB. Verify equality.
3. **Schema-version error path** (Smoke 8): write a stub dump with `schema_version: 999`, run `tusk import --input <stub>`, assert non-zero exit and stderr contains both "999" and "1".
4. **FK validation error**: write a stub dump with a task whose `parent_id` is a UUID not present in the dump or workspace, run import, assert non-zero exit and the rendered issue lists `fk` as the kind.
5. **Collision without `--replace`** (Smoke 5): run a successful import, then run the same import again, assert non-zero exit and a `collision` issue.
6. **`--dry-run` does not mutate** (Smoke 3): count tasks before, run dry-run, count after, assert equal.

Each scenario runs through the harness's existing 2×2 matrix automatically. Skip any matrix variant that doesn't make sense for the scenario (e.g. JSON output assertion makes the JSON-format variant the canonical one for stderr-message tests).

### Task 5 — Apply the three ROADMAP.md edits

**File:** `ROADMAP.md`

The spec's "ROADMAP.md edits" section names three edits with line numbers and exact diff intent. Apply them faithfully:

**Edit 1 — Reshape `Initiative: Data Portability` (around lines 1084–1109).**

Replace the four current stories with two:

- **Story: JSON export and import** — bullet list covering: `tusk export [--output <path>]` with stdout default; `tusk import --input <path>` with `--replace`, `--replace --truncate`, `--dry-run`; faithful semantics (preserves IDs, timestamps, versions exactly; one `workspace_imported` envelope event); pre-validation pass collecting every issue before any write; apply pass in one SQLite transaction; envelope carries `schema_version: 1` + `tusk_version`; unknown `schema_version` is rejected; `domain.TaxonomyValidator` runs on every task; sibling `order` serializes as JSON number or `null`. **No `--format` flag** — JSON is the only format.
- **Story: PortabilityService and codec package** — `internal/portability/` package owning the JSON codec over a neutral `PortableWorkspace`; `service.PortabilityService` orchestrating Export and Import; `service.WriteTx` extended to surface every entity kind; `Client.Portability` exposing the same API to library consumers; `domain.EventWorkspaceImported` event type + `domain.EntityWorkspace` constant.

**Delete entirely** the existing `Story: Markdown export and import` block and the existing `Story: CSV export` block and the existing `Story: MCP tools` block.

**Add at the bottom of the initiative** a deferred-work blockquote in the same style as the `> **Deferred (not v0.13):**` note at ROADMAP.md:1024:

```markdown
> **Deferred (not v0.13):** CSV export and import. JSON covers backup, migration, and the v0.13 ROADMAP self-host. CSV (in either direction) returns when there's a concrete spreadsheet-workflow demand: export needs a column-shape decision and lossy-field documentation; import additionally needs partial-row merge semantics, a story for how the lossy CSV shape interacts with `--replace`, and a CSV-specific validation pass.
```

**Edit 2 — Reshape `Initiative: ROADMAP.md Migration` (around lines 1111–1124).**

Add a new **Story: Markdown rendering (export-only)** at the top of the initiative covering `tusk task tree --format markdown` extending the existing tree renderer. Bullets describing the dialect (H1 per project, H2 per root task, nested bullets for descendants, `[x]` for done-role statuses, inline `level=`, `priority=`, `due=`, `order=`, `uda.*=` tokens, trailing `+tag`, `status=<name>` for non-binary states, annotations and notes as labeled child lists, urgency_overrides / recurrence / attachments / claim state silently dropped because round-trip lives in JSON).

Update the existing **Story: Migration script** bullet that currently says "or hands the markdown file directly to `tusk import --format markdown`, whichever path lands first" — remove the markdown alternative; JSON is the only import path. Update the verification step to point at `tusk task tree --format markdown` (not `tusk export --format markdown`).

Update the existing **Story: Cutover** first bullet from `tusk export --format markdown` to `tusk task tree --format markdown`.

**Edit 3 — Update v0.13 milestone summary (line 971).**

Tighten "bidirectional Data Portability" to "bidirectional JSON Data Portability" and note the markdown renderer lives under the ROADMAP.md Migration initiative. The exact replacement text appears in the spec's "ROADMAP.md edits → Edit 3" section.

After applying all three edits, run the existing roadmap lint / test (if any) — check `Makefile` for a `roadmap-check` or similar target; if none exists, just confirm `git diff ROADMAP.md` reads as expected.

### Task 6 — Run smoke tests 1–8 and capture results

Run smoke tests 1–8 from the spec ("Manual smoke tests" section). Each runs against a temporary `TUSK_DB=$(mktemp).db` workspace so it doesn't touch any real data.

Record results in a brief verification note saved at `docs/superpowers/plans/2026-04-26-data-portability-smoke-results.md` with the following shape:

```markdown
# Data Portability — Smoke Test Results

Run on <date> against commit <SHA> of branch feat/data-portability.

## Smoke 1 — Full round-trip into an empty workspace
Result: PASS / FAIL
Notes: <if FAIL, paste the first divergence; if PASS, one-liner like "diff returned empty">

## Smoke 2 — Stdin / stdout pipeline
Result: PASS / FAIL
Notes: ...

[... through Smoke 8]
```

If any smoke test fails, **do not mark the phase complete**. Open an issue (or append a TODO at the top of the verification note) describing the failure and stop. The planning agent (the human running this) will decide whether the failure blocks the phase or is a known limitation.

A successful smoke pass plus a green `make build && make test` is the final gate for this phase.

---

## Acceptance criteria

1. `make build` succeeds.
2. `make test` (unit + e2e) succeeds.
3. `make vet` and `make lint` succeed.
4. `tusk export` and `tusk import` are registered top-level commands. `tusk help` lists them.
5. `tusk export --output /tmp/ws.json` produces a valid `PortableWorkspace` JSON dump with `schema_version: 1`.
6. `tusk import --input /tmp/ws.json` rehydrates a fresh workspace and emits exactly one `workspace_imported` event.
7. `tusk import --input <bad.json>` exits non-zero with a structured `ImportError` rendered to stderr in the configured format.
8. ROADMAP.md reflects the three edits described above; the Data Portability initiative now has two stories plus a deferred-CSV blockquote.
9. Smoke tests 1–8 run successfully and the result note is committed at `docs/superpowers/plans/2026-04-26-data-portability-smoke-results.md`.

---

## User-visible behavior preserved AND added

**Preserved:**
- All existing `tusk` CLI commands (every subcommand under `tusk task`, `tusk project`, `tusk workflow`, `tusk tag`, `tusk player`, `tusk note`, `tusk config`, `tusk mcp`, `tusk completion`, `tusk version`) work identically.
- All existing MCP tools and resources work identically.
- Library `Client` API: every existing field works as before. `Client.Portability` (added in Phase 3) is unchanged.

**Added:**
- `tusk export [--output <path>]` — writes a JSON workspace dump.
- `tusk import --input <path> [--replace] [--truncate] [--dry-run]` — rehydrates a workspace from a JSON dump, with collision and atomicity controls.
- ROADMAP.md reflects the as-shipped scope.

---

## Changes Introduced

**New files:**
- `internal/tui/export.go` — `buildExportCmd`, `runExport`.
- `internal/tui/import.go` — `buildImportCmd`, `runImport`, `ImportReport` / `ImportError` rendering helpers (or via shared render code if it exists).
- `tests/e2e/portability_test.go` — e2e scenarios covering round-trip, stdin/stdout, schema mismatch, FK error, collision, dry-run.
- `docs/superpowers/plans/2026-04-26-data-portability-smoke-results.md` — manual verification record from Task 6.

**Modified files:**
- `internal/tui/app.go` — `App` struct gains `portabilitySvc` field; constructor accepts it; root command registers `buildExportCmd()` and `buildImportCmd()`.
- `cmd/tusk/main.go` — instantiates `service.NewPortabilityService(...)` and passes it to the `tui.App` constructor.
- `ROADMAP.md` — three edits per spec → "ROADMAP.md edits".

**Modified interfaces:** None new — the CLI consumes the API Phase 3 already exposed.

**No new env vars, no schema migration, no new dependencies, no bridge code.**

---

## Cleanup after the planning agent's post-implementation review

Per the phase-planning rules, all plan docs are cleaned up from the repository after the final review and verification. Once the planning agent (the human) has verified the four phases and signed off, delete:

- `docs/superpowers/plans/2026-04-26-data-portability-phase-1.md`
- `docs/superpowers/plans/2026-04-26-data-portability-phase-2.md`
- `docs/superpowers/plans/2026-04-26-data-portability-phase-3.md`
- `docs/superpowers/plans/2026-04-26-data-portability-phase-4.md`
- `docs/superpowers/plans/2026-04-26-data-portability-smoke-results.md`

The spec at `docs/superpowers/specs/2026-04-26-data-portability-design.md` stays. The implementer agent does not perform this cleanup — the planning agent does it as part of post-implementation review.
