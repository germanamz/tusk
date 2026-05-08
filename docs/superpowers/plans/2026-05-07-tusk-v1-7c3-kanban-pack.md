# Plan 7.c.3 — Kanban Pack Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the built-in `kanban` pack as data + workflow config — one TOML file at `packs/kanban.toml` containing a `ticket` node type, `parent` and `blocks` edge types, and a `[behaviors.workflow.kanban]` instance. One end-to-end smoke test verifies pack add → ticket create → WBS edge → workflow transitions all work against the real pack file.

**Architecture:** Pure pack content. No engine code, no platform extensions. Mirrors Plan 7.c.2 (tags pack) structure: a TOML file at `packs/<name>.toml` plus a single end-to-end CLI smoke test using `file://` against the repo-local path. Self-contained — composes with the tags pack rather than depending on or duplicating it.

**Tech Stack:** Go 1.x · cobra-based CLI · existing `internal/typepacks` (pack platform from Plan 7.c.1) · existing `internal/behaviorpacks/workflow` (workflow validator from Plan 7) · existing `internal/manifest` property validator (Plan 7.b).

**Spec:** `docs/superpowers/specs/2026-05-07-tusk-v1-7c3-kanban-pack-design.md`

**Branch:** `feat/plan-7c3` (already created on top of v1 tip `76956ab`; spec doc already committed at `1a0c783`).

---

## File Structure

This plan creates one production file and one test file:

| Action | Path | Purpose |
|---|---|---|
| Create | `packs/kanban.toml` | Pack content — node-type/edge-types/behavior declarations |
| Modify | `cmd/tusk/cmd_pack_add_test.go` | Add one end-to-end smoke test (`TestPackAdd_KanbanPackEndToEnd`) |

The smoke test reuses the existing `testSourceDir(test *testing.T) string` helper at the bottom of `cmd/tusk/cmd_pack_add_test.go` — added by Plan 7.c.2, returns the absolute directory of the test source file via `runtime.Caller`. No new helper is needed.

No other files are touched. The `internal/typepacks/aliases.go` map already names `kanban` (added in Plan 7.c.1); the URL becomes live as soon as `packs/kanban.toml` lands on `main` after the v1→main cascade.

---

## Bundle 1 — Pack file + smoke test

This is a single bundle (the entire plan). The work is small enough and the two pieces are tightly coupled (the smoke test directly consumes the pack file).

### Task 1: Create `packs/kanban.toml`

**Files:**
- Create: `packs/kanban.toml`

- [ ] **Step 1: Create the pack file with the four sections**

```toml
# Tusk built-in pack: kanban
#
# Adds a `ticket` node type with workflow-validated status, priority,
# and due-date properties; a WBS `parent` edge for hierarchy; and a
# `blocks` edge for dependency.
#
# A "project" in this pack is just a higher-level ticket — the WBS
# parent edge captures the hierarchy. There is no separate `project`
# node type; if you need one, declare `[node-types.project]` in your
# workspace manifest.
#
# Tagging is intentionally out of scope here. To tag tickets, run
# `tusk pack add tags` (composes cleanly), then either `[[tag/x]]`
# wikilinks in the body or a per-ticket ref opt-in:
#   properties = [{ name = "tags", type = "list-of",
#                   item-type = "ref", to = "tag" }]
#
# Customizing workflow states: edit [behaviors.workflow.kanban].states
# and transitions — the workflow validator is the single source of truth
# for status. Do not redeclare `status` in [node-types.ticket].properties;
# the behavior engine reserves it and rejects duplicate declarations.

[node-types.ticket]
description = "A unit of work tracked through a workflow"
properties = [
    { name = "priority", type = "enum", values = ["low", "medium", "high"] },
    { name = "due",      type = "date" },
]

[edge-types.parent]
description = "WBS parent — this ticket is a child of another ticket"
from        = ["ticket"]
to          = ["ticket"]
cardinality = "many-to-one"
ordered     = true
acyclic     = true
inverse     = "children"

[edge-types.blocks]
description = "This ticket blocks another from progressing"
from        = ["ticket"]
to          = ["ticket"]
cardinality = "many-to-many"
ordered     = false
acyclic     = true
inverse     = "blocked-by"

[behaviors.workflow.kanban]
applies-to      = ["ticket"]
status-property = "status"

states = [
    { name = "pending",   initial = true },
    { name = "active",    start = true },
    { name = "completed", terminal = true, done = true },
]

transitions = [
    { from = "pending",   to = "active" },
    { from = "active",    to = "completed" },
    { from = "active",    to = "pending" },
    { from = "completed", to = "pending" },
]
```

- [ ] **Step 2: Verify the TOML parses cleanly with the existing manifest decoder**

Run: `go test ./internal/typepacks/...`
Expected: PASS — typepacks tests are unaffected (no new code paths exercised), but this confirms nothing in the existing test suite regressed.

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 3: Commit the pack file alone**

```bash
git add packs/kanban.toml
git commit -m "feat(v1): add kanban pack content (packs/kanban.toml)"
```

---

### Task 2: Add the end-to-end smoke test

**Files:**
- Modify: `cmd/tusk/cmd_pack_add_test.go` — append `TestPackAdd_KanbanPackEndToEnd` after the existing `TestPackAdd_TagsPackEndToEnd` and before the `testSourceDir` helper.

The test mirrors `TestPackAdd_TagsPackEndToEnd` (added in Plan 7.c.2). It resolves `packs/kanban.toml` via `testSourceDir(test)`, runs `tusk init`, runs `tusk pack add file://...`, then exercises ticket creation, WBS edges, workflow transitions, and a negative case.

- [ ] **Step 1: Write the failing test**

Append to `cmd/tusk/cmd_pack_add_test.go`, immediately after the existing `TestPackAdd_TagsPackEndToEnd` function (which ends at line 241 in the current file) and before the `testSourceDir` helper:

```go
func TestPackAdd_KanbanPackEndToEnd(test *testing.T) {
	dir := test.TempDir()

	// Resolve the repo-local pack file BEFORE chdir-ing.
	packPath := filepath.Join(testSourceDir(test), "..", "..", "packs", "kanban.toml")

	if _, statErr := os.Stat(packPath); statErr != nil {
		test.Fatalf("packs/kanban.toml not found at %s: %v", packPath, statErr)
	}

	chdir(test, dir)

	// 1. tusk init.
	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"init", "--name", "kanban-smoke"})

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("init: %v", execErr)
	}

	// 2. tusk pack add file://<packs/kanban.toml>.
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"pack", "add", "file://" + packPath})

	var addStdout, addStderr bytes.Buffer

	rootCmd.SetOut(&addStdout)
	rootCmd.SetErr(&addStderr)

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("pack add: %v\nstderr: %s", execErr, addStderr.String())
	}

	manifestBody, readErr := os.ReadFile(filepath.Join(dir, "tusk.toml"))

	if readErr != nil {
		test.Fatalf("read tusk.toml: %v", readErr)
	}

	for _, want := range []string{
		"[node-types.ticket]",
		"[edge-types.parent]",
		"[edge-types.blocks]",
		"[behaviors.workflow.kanban]",
	} {
		if !strings.Contains(string(manifestBody), want) {
			test.Errorf("tusk.toml missing %s: %q", want, manifestBody)
		}
	}

	// 3. Create a parent ticket in the "pending" state.
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{
		"node", "create",
		"--type", "ticket",
		"--title", "Parent ticket",
		"--path", "tickets/parent.md",
		"--prop", "status=pending",
		"--prop", "priority=high",
	})
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("create tickets/parent: %v", execErr)
	}

	// 4. Create a child ticket.
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{
		"node", "create",
		"--type", "ticket",
		"--title", "Child ticket",
		"--path", "tickets/child.md",
		"--prop", "status=pending",
	})
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("create tickets/child: %v", execErr)
	}

	// 5. Add a parent edge child -> parent.
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{
		"edge", "add",
		"--type", "parent",
		"--source", "tickets/child",
		"--target", "tickets/parent",
	})
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("edge add parent: %v", execErr)
	}

	// 6. Verify the parent edge is indexed via `edge list --from tickets/child`.
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"edge", "list", "--from", "tickets/child"})

	var listStdout bytes.Buffer

	rootCmd.SetOut(&listStdout)
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("edge list: %v", execErr)
	}

	if !strings.Contains(listStdout.String(), "parent") || !strings.Contains(listStdout.String(), "tickets/parent") {
		test.Errorf("edge list output missing expected parent edge: %q", listStdout.String())
	}

	// 7. Workflow transition pending -> active on the parent ticket. Legal.
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"node", "modify", "tickets/parent", "--prop", "status=active"})
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("modify pending->active: %v", execErr)
	}

	// 8. Workflow transition active -> completed. Legal.
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"node", "modify", "tickets/parent", "--prop", "status=completed"})
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("modify active->completed: %v", execErr)
	}

	// 9. Negative — workflow rejects pending -> completed (skip-state).
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"node", "modify", "tickets/child", "--prop", "status=completed"})

	var negStdout, negStderr bytes.Buffer

	rootCmd.SetOut(&negStdout)
	rootCmd.SetErr(&negStderr)

	negErr := rootCmd.Execute()

	if negErr == nil {
		test.Fatalf("expected pending->completed to fail; stdout=%q stderr=%q", negStdout.String(), negStderr.String())
	}
}
```

- [ ] **Step 2: Run the test to verify it passes against the new pack file**

Run: `go test ./cmd/tusk/ -run TestPackAdd_KanbanPackEndToEnd -v`
Expected: PASS. The test exercises pack add, ticket create (twice), edge add, edge list, two legal workflow transitions, and one rejected workflow transition.

If any sub-step fails, the cause is one of:
- TOML decode failure (`pack add` fails) → check property/edge/behavior section shapes match what the existing decoders expect.
- Property validator rejection (`node create` fails on a known-good value) → check the `status` enum values and the `priority` enum values.
- Workflow validator rejection on a legal transition (`node modify` fails on `pending->active`) → check the workflow `states` and `transitions` shape.
- Edge cycle/cardinality rejection (`edge add` fails) → check the `parent` edge cardinality and direction.
- The negative case passing instead of failing → the workflow's `transitions` table includes a `(pending, completed)` row by accident.

The most likely failure is the negative case: if `pending -> completed` succeeds, re-read the `transitions` block in the pack file to confirm only the four transitions listed are present.

If the test correctly fails because of a CLI flag-shape mismatch, fix the test's flag arguments — do not modify the CLI. The flag shapes documented above (`--type`/`--title`/`--path`/`--prop` for `node create`; `--type`/`--source`/`--target` for `edge add`; positional id for `node modify` followed by `--prop`/`--unset`) match the actual CLI as of the current v1 tip; double-check by greping `cmd/tusk/cmd_node_create.go`, `cmd/tusk/cmd_edge_add.go`, and `cmd/tusk/cmd_node_modify.go` for `Flags()` calls if there's any doubt.

- [ ] **Step 3: Run the full test suite**

Run: `make test`
Expected: all packages pass, including `cmd/tusk/...` and the existing tags-pack smoke test.

Run: `make lint`
Expected: clean.

Run: `make vet`
Expected: clean.

If `make lint` flags any new lefthook-enforced style issues (Rule 1: ≥2-character identifiers; Rule 2: blank lines around `if err != nil` guards; Rule 4: named errors on shadow), fix them inline in the test.

- [ ] **Step 4: Commit the test**

```bash
git add cmd/tusk/cmd_pack_add_test.go
git commit -m "test(v1): kanban pack end-to-end smoke test"
```

---

## After both tasks

Plan 7.c.3 is then ready for PR. The branch `feat/plan-7c3` should contain three commits on top of v1:

1. `1a0c783 docs(spec): plan 7.c.3 — kanban pack design` (already landed)
2. `feat(v1): add kanban pack content (packs/kanban.toml)`
3. `test(v1): kanban pack end-to-end smoke test`

Push and open the PR against `v1` with title `feat(v1): plan 7.c.3 — kanban pack` and a body summarizing the spec divergences (no project, no tagged, no assignee) and confirming the smoke test passes.

---

## Self-Review Notes

**Spec coverage:**

| Spec section | Plan task |
|---|---|
| §2 (pack file shape) | Task 1 step 1 — full file content |
| §3 (repo layout) | Task 1 step 1 (file goes at `packs/kanban.toml`) |
| §4 (divergences) | Captured in spec; plan does not need to re-state them |
| §5 (UX patterns) | Task 2 step 1 exercises patterns 5.1, 5.2, 5.4 (CLI ticket create, parent edge, workflow transition) |
| §6 (testing strategy) | Task 2 in full |
| §7 (residuals) | Captured in spec; not implementation work |
| §8 (ledger update) | Captured in spec; not implementation work |

The spec's UX pattern 5.5 (composing with the tags pack) is documentation-only and isn't exercised by the smoke test — adding it would require also installing the tags pack, which broadens the test's scope beyond the kanban pack itself. The tags pack already has its own end-to-end smoke test from Plan 7.c.2.

**Placeholder scan:** No `TODO`, `TBD`, "implement later", or vague error-handling stubs.

**Type/identifier consistency:** All flag names (`--type`, `--title`, `--path`, `--prop`, `--source`, `--target`, `--from`) are consistent across tasks and match the CLI as defined in `cmd/tusk/cmd_node_create.go`, `cmd/tusk/cmd_edge_add.go`, `cmd/tusk/cmd_node_modify.go`, and `cmd/tusk/cmd_edge_list.go`. Node IDs are workspace-relative paths *without* the `.md` extension (per the existing test patterns in `cmd_pack_add_test.go` and `cmd_node_modify_test.go`).
