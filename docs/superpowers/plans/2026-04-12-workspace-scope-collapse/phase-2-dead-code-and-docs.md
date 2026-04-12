# Phase 2 — Dead Code Cleanup, Config Schema Removal, and Release Docs

> **For the implementer:** This is the authoritative phase doc. The design spec in `PRODUCT.md` and `phase-1-runtime-collapse.md` in this directory are reference material only. Do not reintroduce any code deleted by Phase 1, and do not touch service routing — all behavior changes already shipped in Phase 1.

**Goal:** Delete the dead bridge code that Phase 1 left behind, remove `db_path` from the config schema and CLI parser, write the user migration guide, update the v0.9 release notes, and tick off the four stories under "Initiative: Workspace Scope Collapse" in `ROADMAP.md`.

**Prerequisites:** Phase 1 (`phase-1-runtime-collapse.md`) must be merged. This phase cannot start until Phase 1 is on `main`.

**Tech Stack:** Same as Phase 1 — Go 1.22+, SQLite, Cobra, Viper. No new dependencies.

## Inherits From Phase 1

When you start, the codebase will be in the state Phase 1 left it:

- `cmd/tusk/main.go` opens a single `*sqlite.Store` via `sqlite.New(absDB, migrations.FS)` and wraps it in one `*service.RepoBundle`. The resolver closure ignores the project ID (other than validating existence in `cfg.Projects`).
- `client.go` has the same single-bundle shape.
- `service/task.go` and `service/relation.go` have single-`resolve()` read paths. There is no cross-store guard anywhere in the service layer.
- `domain/errors.go` does **not** contain `ErrCrossStoreRelation`.
- `service/relation_crossstore_test.go` and `tests/e2e/project_db_path_test.go` are deleted.
- `sqlite/paths.go` exists and exports `ResolveWorkspacePath`.
- The full suite (`make test-race && make vet && make lint`) is green.

The dead code you will remove in this phase:

- `sqlite/registry.go` — `StoreRegistry`, its constructors, `resolveDBPath`, `openPath`. Entire file.
- `sqlite/registry_test.go` — `TestStoreRegistry_*`. Entire file. (Its tests still pass today because `registry.go` compiles in isolation.)
- `config.ProjectConfig.DBPath` field and its TOML/mapstructure tags.
- `config.ProjectMutation.DBPath` field and the apply block that writes it.
- `config` tests that round-trip or mutate `DBPath`.
- `db-path` cases in `internal/tui/project_parse.go`'s `applyProjectField` and `parseProjectModify`.
- Any `project_parse_test.go` assertions that cover `db-path=`.

The `config/default.toml` comment block already does not contain a `db_path` example — do not add one. Double-check with `rg -n 'db_path' config/default.toml`, which should return zero hits.

## User-visible behavior contract

After Phase 2 ships, the following must still work exactly as in Phase 1:

- Every CLI verb (`add`, `list`, `info`, `tree`, `modify`, `start`, `done`, `delete`, `pop`, `claim`, `release`, `available`, `next`, `link`, `unlink`, `annotate`, `project create`, `project modify`, `project delete`, `workflow *`, `config *`, `tag *`, `player *`, `timer *`, `attach`, `export`).
- Project filters via SQL (`tusk list project=backend`).
- Cross-project relations and cross-project task moves (unlocked by Phase 1).
- Optimistic locking, workflow enforcement, urgency scoring.

Intentional behavior change introduced by **this** phase:

- `tusk project create <name> workflow=kanban db-path=/foo.db` now errors with `unknown field "db-path"`. Previously (in Phase 1) it was silently accepted and ignored. Users who scripted this must update their scripts. The migration doc shipped in Task 4 covers the transition.
- Loading a `tusk.toml` with `[projects.<name>].db_path = "..."` — the field is ignored by Viper on unmarshal (unknown keys do not error in Viper by default). Do not add strict TOML validation in this phase. If you believe the product should error on stale `db_path` keys, flag it to the planning agent and leave a note in the migration doc instead.

---

## Task 1: Delete `sqlite.StoreRegistry`

**Files:**
- Delete: `sqlite/registry.go`
- Delete: `sqlite/registry_test.go`

- [ ] **Step 1: Confirm nothing outside these files imports `StoreRegistry`**

Run: `rg -n "StoreRegistry|NewStoreRegistry" --type go`
Expected: matches are confined to `sqlite/registry.go` and `sqlite/registry_test.go`. Any other hit is a bug Phase 1 should have caught — stop and report to the planning agent.

- [ ] **Step 2: Confirm nothing outside these files calls the private `resolveDBPath`**

Run: `rg -n "resolveDBPath" sqlite/ --type go`
Expected: matches confined to `sqlite/registry.go`. (Phase 1 intentionally duplicated this logic into the exported `sqlite.ResolveWorkspacePath` in `sqlite/paths.go`, so the old helper has no callers.)

Note: `cmd/tusk/main.go` has a function **also** called `resolveDBPath` that resolves the `--db` flag / `TUSK_DB` env var. That is a different, unrelated helper; do not touch it. The grep above is scoped to `sqlite/` specifically so you do not see it.

- [ ] **Step 3: Delete both files**

Run: `rm sqlite/registry.go sqlite/registry_test.go`

- [ ] **Step 4: Build and run sqlite tests**

Run: `go build ./... && go test ./sqlite/... -count=1`
Expected: PASS. `sqlite/paths.go` remains as the only path helper in the package.

- [ ] **Step 5: Commit**

```bash
git add sqlite/registry.go sqlite/registry_test.go
git commit -m "refactor(sqlite): delete StoreRegistry"
```

---

## Task 2: Remove `DBPath` from `ProjectConfig` and `ProjectMutation`

**Files:**
- Modify: `config/config.go:86-90`
- Modify: `config/project.go:60-71,145-208`
- Modify: `config/project_test.go`
- Modify: `config/config_test.go:512-548`

- [ ] **Step 1: Delete `TestProjectConfig_DBPathRoundTrip` in `config/config_test.go`**

Remove lines 512–548 (the whole function). Use `rg -n "TestProjectConfig_DBPathRoundTrip" config/` to confirm there is only one occurrence before deleting.

- [ ] **Step 2: Delete `TestModifyProject_SetAndClearDBPath` and strip DBPath assertions**

In `config/project_test.go`:
- Delete the entire `TestModifyProject_SetAndClearDBPath` function (currently at lines 148–169).
- Update the round-trip test at lines 11–27: change `proj := ProjectConfig{Workflow: "kanban", DBPath: "/tmp/b.db"}` to `proj := ProjectConfig{Workflow: "kanban"}`, and change the assertion at line 24 to check only `got.Workflow != "kanban"`.
- Any other function that references `DBPath` — grep `rg -n "DBPath" config/project_test.go` — remove the offending assertions or the test if the entire test is about `DBPath`.

- [ ] **Step 3: Remove `DBPath` from `ProjectConfig`**

In `config/config.go`, change the struct to:

```go
type ProjectConfig struct {
    Workflow string                `mapstructure:"workflow" toml:"workflow"`
    Settings ProjectSettingsConfig `mapstructure:"settings" toml:"settings"`
}
```

- [ ] **Step 4: Remove `DBPath` from `ProjectMutation` and `ModifyProject`**

In `config/project.go`:
- Change the struct definition (currently lines 64–71) to:

```go
type ProjectMutation struct {
    Workflow        *string
    AutoCompleteSet *AutoCompleteParentConfig
    AutoRevertSet   *AutoRevertParentConfig
    UrgencySet      map[string]float64
    UrgencyDelta    map[string]float64
}
```

- Update the struct comment (currently line 61) to:

```go
// ProjectMutation describes changes to apply to an existing project.
// nil = don't change, non-nil = set.
```

- Delete the apply block (currently lines 159–161):

```go
if mut.DBPath != nil {
    proj.DBPath = *mut.DBPath
}
```

- [ ] **Step 5: Build and run config tests**

Run: `go test ./config/... -count=1`
Expected: PASS. If tests reference `DBPath` in ways you did not update in Step 2, stop and fix them before continuing.

- [ ] **Step 6: Grep for stragglers**

Run: `rg -n "DBPath|db_path" config/ --type go`
Expected: zero matches.

- [ ] **Step 7: Commit**

```bash
git add config/config.go config/project.go config/config_test.go config/project_test.go
git commit -m "refactor(config): remove per-project db_path from ProjectConfig"
```

---

## Task 3: Remove `db-path=` from the project CLI parser

**Files:**
- Modify: `internal/tui/project_parse.go:53-99,101-171`
- Check: `internal/tui/project_parse_test.go` (if present)

- [ ] **Step 1: Grep for test coverage of `db-path=`**

Run: `rg -n "db-path" internal/tui/`
Expected: matches inside `internal/tui/project_parse.go` (the two cases you will delete), plus possibly `internal/tui/project_parse_test.go` or similar. Delete any test cases that feed `db-path=` into the parser or assert it on mutations.

- [ ] **Step 2: Delete the `case "db-path":` branch in `applyProjectField`**

In `internal/tui/project_parse.go`, find:

```go
case "db-path":
    proj.DBPath = value
```

(around lines 57–58) and delete both lines. The surrounding `switch key {}` block should collapse cleanly.

- [ ] **Step 3: Delete the `case "db-path":` branch in `parseProjectModify`**

Find:

```go
case "db-path":
    v := f.Value
    mut.DBPath = &v
```

(around lines 137–139) and delete all three lines.

- [ ] **Step 4: Run the TUI package tests**

Run: `go test ./internal/tui/... -count=1`
Expected: PASS.

- [ ] **Step 5: Grep for stragglers one more time**

Run: `rg -n "db-path|DBPath" internal/tui/ --type go`
Expected: zero matches.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/
git commit -m "refactor(tui): remove db-path= from project CLI parser"
```

---

## Task 4: Write the migration guide and update v0.9 release notes

**Files:**
- Create: `docs/workspace-scope-migration.md`
- Create or modify: `docs/releases/v0.9.md`

- [ ] **Step 1: Check whether `tusk import` exists**

Run: `rg -n '"import"' internal/tui/`
Expected: either a match (an `import` subcommand is wired) or no match (not yet implemented). You will use this result to decide which recipe to put in the migration doc.

- [ ] **Step 2: Create `docs/workspace-scope-migration.md`**

If Step 1 found a `tusk import` command, write this content:

```markdown
# Migrating from per-project databases

Tusk v0.9 removes `[projects.<name>].db_path`. Every project declared in
a config file now shares the single database at `storage.path`. If you
had set `db_path` on one or more projects in an earlier version, follow
the manual migration below. Tusk ships no automatic converter — the
feature predates v0.1 and had no production users, but the steps are
documented here for completeness.

## 1. Export each per-project database

For every project that used its own file, point the old CLI at that
database and dump the contents to JSON:

    TUSK_DB=/path/to/backend.db tusk export --format json > backend.json
    TUSK_DB=/path/to/frontend.db tusk export --format json > frontend.json

Each export is a complete, self-contained dump — tasks, relations,
annotations, tags, players.

## 2. Remove `db_path` from your config

Open your `~/.config/tusk/config.toml` and delete every
`db_path = ...` line under `[projects.<name>]`. Keep the workflow
binding and any urgency overrides. Viper ignores unknown keys, but you
should remove them so nobody is misled in the future.

## 3. Import into the workspace database

Run the new CLI with `storage.path` pointing at your consolidated
workspace file (the default is `~/.local/share/tusk/tusk.db`) and
import each export in turn:

    tusk import backend.json
    tusk import frontend.json

## 4. Verify

    tusk list project=backend
    tusk list project=frontend

You should see the same task set you had in the per-project files. Run
`tusk tree project=backend` to confirm hierarchies survived, and
`tusk info <short_id>` on a handful of tasks to spot-check relations
and annotations.

## Notes

- Cross-project relations now work by construction — the old
  `ErrCrossStoreRelation` rejection is gone. If you had two tasks in
  separate per-project files that you wished you could link with
  `blocks`, you can do so after import.
- Short IDs are regenerated during import only on collision. Typical
  migrations preserve every short ID verbatim.
- Tags are deduped by name. Two per-project files that both defined a
  `bug` tag collapse to a single `bug` tag in the workspace store.
```

If Step 1 found no `tusk import`, replace section 3 with a `jq` recipe that reads the export format and produces `tusk add` / `tusk link` / `tusk annotate` / `tusk tag` commands. The export format is whatever `tusk export --format json` currently produces — inspect it first by running the command against any tusk DB and piping the result into `jq keys`. Shape the recipe so each task is re-created with its original title, project, priority, parent, UDA, and status, then re-attach tags, then recreate relations. Document in the guide that the recipe is lossy in one respect: timestamps (`created_at`, `modified_at`) cannot be preserved because `tusk add` does not accept them as flags.

Whichever variant you choose, keep the rest of the guide identical.

- [ ] **Step 3: Update `docs/releases/v0.9.md`**

Run: `ls docs/releases/`
If `v0.9.md` does not exist, create it using the same layout as `docs/releases/v0.8.md` — read that file first and match its structure (front matter, section headings, tone).

Whether the file existed or was newly created, ensure it contains a top-level `## Breaking changes` section with this entry:

```markdown
### Per-project `db_path` removed

Projects no longer carry a `db_path` field. Every project declared in a
config file shares the single database declared by that file's
`storage.path`. Cross-project operations — `tusk list`, `tusk pop`,
`tusk available`, `tusk link` — now run as single-database queries, and
relations can span projects inside the same workspace.

Users who had set `[projects.<name>].db_path` in a previous version
should follow the manual export/import procedure in
[docs/workspace-scope-migration.md](../workspace-scope-migration.md).
Unknown keys in the config are ignored on load, so a stale `db_path`
line will not break startup — but it will silently do nothing, which
is exactly the footgun the migration doc helps users avoid.
```

Place the `## Breaking changes` section near the top of the release notes, right after any summary paragraph.

- [ ] **Step 4: Commit**

```bash
git add docs/workspace-scope-migration.md docs/releases/v0.9.md
git commit -m "docs: add workspace scope migration guide and v0.9 breaking change note"
```

---

## Task 5: Tick off the roadmap and run the final verification sweep

**Files:**
- Modify: `ROADMAP.md:569-589`

- [ ] **Step 1: Mark every story and checklist item under "Initiative: Workspace Scope Collapse" as done**

Open `ROADMAP.md` and locate the `### Initiative: Workspace Scope Collapse` heading (at line 565 at the time this plan was written). The four stories and all their nested checklist items are currently `- [ ]`. Change every `- [ ]` in that initiative block to `- [x]`:

- Story: Remove per-project db_path from the config schema
- Story: Collapse StoreRegistry to a single workspace store
- Story: Remove cross-store fan-out from services
- Story: Migration guidance for existing per-project DBs

Do not touch the adjacent initiatives (the previous one above line 565 or "Initiative: Explicit Config File Resolver" below).

- [ ] **Step 2: Full build, test-race, vet, lint**

Run: `make build && make test-race && make vet && make lint`
Expected: PASS across the board.

- [ ] **Step 3: Final straggler grep**

Run:

```bash
rg -n 'db_path|db-path|StoreRegistry|CrossStoreRelation|multiBundleResolver' . \
  --glob '!ROADMAP.md' \
  --glob '!docs/**' \
  --glob '!**/*.md'
```

Expected: zero matches in Go source. Any hit is a leftover from an earlier task — fix it before committing. (The `--glob` exclusions are there because `ROADMAP.md` historical records and the migration doc both legitimately mention these names.)

- [ ] **Step 4: Smoke test cross-project workflow end to end**

```bash
TUSK_DB="$(mktemp -t tusk-phase2-XXXX.db)"
export TUSK_DB
./bin/tusk add "alpha" project=default
./bin/tusk project create backend workflow=kanban
./bin/tusk add "beta" project=backend
./bin/tusk list
./bin/tusk list project=backend
./bin/tusk project modify backend auto-complete.trigger=completed
# db-path= must now error:
if ./bin/tusk project create obsolete workflow=kanban db-path=/tmp/foo.db 2>/dev/null; then
    echo "ERROR: db-path= was accepted, but should have been rejected"
    exit 1
fi
./bin/tusk project delete backend || true
rm "$TUSK_DB"* 2>/dev/null || true
unset TUSK_DB
```

Expected: the `db-path=` call fails with `unknown field "db-path"`, every other call succeeds.

- [ ] **Step 5: Commit the roadmap update**

```bash
git add ROADMAP.md
git commit -m "docs: tick off Workspace Scope Collapse stories in the roadmap"
```

---

## Changes Introduced

**Deleted files:**
- `sqlite/registry.go`
- `sqlite/registry_test.go`

**Modified interfaces:**
- `config.ProjectConfig` — `DBPath string` field removed.
- `config.ProjectMutation` — `DBPath *string` field removed; `ModifyProject` apply block shortened accordingly.
- `internal/tui` project parser — `db-path` field no longer accepted in `project create` or `project modify`. The parser now returns `unknown field "db-path"`.

**New files:**
- `docs/workspace-scope-migration.md`
- `docs/releases/v0.9.md` (created if absent)

**Updated files:**
- `ROADMAP.md` — four `Initiative: Workspace Scope Collapse` stories ticked.
- `docs/releases/v0.9.md` — `## Breaking changes` section added near the top.

**Bridge code removed (all targeted by Phase 1's bridge-removal table):**
- `sqlite.StoreRegistry` and its tests — removed in Task 1.
- `config.ProjectConfig.DBPath` — removed in Task 2.
- `config.ProjectMutation.DBPath` — removed in Task 2.
- `db-path` cases in `internal/tui/project_parse.go` — removed in Task 3.

**No new environment variables, schema migrations, or dependencies.**

**Acceptance criteria:**
- `rg -n 'db_path|db-path|StoreRegistry|CrossStoreRelation' --type go` returns zero hits.
- `make test-race`, `make vet`, `make lint` green.
- `tusk project create foo workflow=kanban db-path=/x.db` now errors.
- Cross-project `tusk link`, `tusk modify project=<other>`, and `tusk list project=<name>` still work exactly as they did at the end of Phase 1.
- `docs/workspace-scope-migration.md` and the `## Breaking changes` section of `docs/releases/v0.9.md` are present.
- The four `Initiative: Workspace Scope Collapse` stories in `ROADMAP.md` are ticked.
