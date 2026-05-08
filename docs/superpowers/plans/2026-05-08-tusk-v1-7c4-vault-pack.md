---
type: plan
title: Plan 7.c.4
status: shipped
pr: 363
shipped-at: "2026-05-08"
implements:
  - Plan 7.c.4 — Vault Pack Spec
  - Tusk v1 Rebuild
---

# Plan 7.c.4 — Vault Pack Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the built-in `vault` pack as data only — one TOML file at `packs/vault.toml` containing three categorical node types (`note`, `meeting`, `decision`) and two edge types (`references` for wikilink materialization, `relates-to` for user-declared "see also" links). One end-to-end smoke test verifies pack add → node creates → wikilink-as-references-edge materialization → manual `relates-to` edge add/list all work against the real pack file.

**Architecture:** Pure pack content. No engine code, no platform extensions, no behaviors. Closes the v1.c built-in pack trilogy alongside 7.c.2 (tags) and 7.c.3 (kanban). Mirrors the same data-only structure: a TOML file at `packs/<name>.toml` plus a single end-to-end CLI smoke test using `file://` against the repo-local path.

**Tech Stack:** Go 1.x · cobra-based CLI · existing `internal/typepacks` (pack platform from Plan 7.c.1) · existing `internal/node` (wikilink and edge resolution from Plan 2) · existing `internal/manifest` property validator (Plan 7.b — trivially satisfied since vault declares no properties).

**Spec:** `docs/superpowers/specs/2026-05-08-tusk-v1-7c4-vault-pack-design.md`

**Branch:** `feat/plan-7c4` (already created on top of v1 tip `8121dba`; spec doc already committed at `dfcd364`).

---

## File Structure

This plan creates one production file and modifies one test file:

| Action | Path | Purpose |
|---|---|---|
| Create | `packs/vault.toml` | Pack content — node-type / edge-types declarations |
| Modify | `cmd/tusk/cmd_pack_add_test.go` | Add one end-to-end smoke test (`TestPackAdd_VaultPackEndToEnd`) |

The smoke test reuses the existing `testSourceDir(test *testing.T) string` helper at the bottom of `cmd/tusk/cmd_pack_add_test.go` (added by Plan 7.c.2). No new helper needed.

No other files are touched. The `internal/typepacks/aliases.go` map already names `vault` (added in Plan 7.c.1); the URL `https://raw.githubusercontent.com/germanamz/tusk/main/packs/vault.toml` becomes live as soon as `packs/vault.toml` lands on `main` after the v1→main cascade.

---

## Bundle 1 — Pack file + smoke test

This is a single bundle (the entire plan). The work is small enough and the two pieces are tightly coupled (the smoke test directly consumes the pack file). Same shape as 7.c.2 and 7.c.3.

### Task 1: Create `packs/vault.toml`

**Files:**
- Create: `packs/vault.toml`

- [ ] **Step 1: Create the pack file with the five sections**

```toml
# Tusk built-in pack: vault
#
# Adds three categorical node types — `note`, `meeting`, `decision` —
# and two edge types — `references` (auto-materialized from body
# wikilinks) and `relates-to` (user-declared "see also").
#
# Pack is purely categorical: no declared properties on any node type.
# The implicit `title` is always available; the markdown body is the
# natural place for content (meeting agendas, decision rationale,
# note prose). Users wanting structured fields on a node type can
# declare them inline in their workspace manifest.
#
# Note: declaring [edge-types.references] is what activates body
# wikilink → edge materialization in the engine. A workspace with
# this pack added gets `[[some/node]]` → references-edge behavior;
# a workspace without it gets wikilinks as plain text only.
#
# `relates-to` has no declared inverse — the relation is symmetric
# in meaning, so the <- operator handles backward traversal.

[node-types.note]
description = "A free-form markdown note"
properties = []

[node-types.meeting]
description = "A meeting record (agenda, attendees, discussion in body)"
properties = []

[node-types.decision]
description = "A captured decision (rationale, status, date in body)"
properties = []

[edge-types.references]
description = "Implicit edge materialized from body wikilinks"
from        = ["*"]
to          = ["*"]
cardinality = "many-to-many"
ordered     = false
acyclic     = false
inverse     = "referenced-by"

[edge-types.relates-to]
description = "User-declared 'see also' relationship between nodes"
from        = ["*"]
to          = ["*"]
cardinality = "many-to-many"
ordered     = false
acyclic     = false
```

- [ ] **Step 2: Verify the TOML parses cleanly with the existing manifest decoder**

Run: `go test ./internal/typepacks/...`
Expected: PASS — typepacks tests are unaffected (no new code paths exercised), but this confirms nothing in the existing test suite regressed.

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 3: Commit the pack file alone**

```bash
git add packs/vault.toml
git commit -m "feat(v1): add vault pack content (packs/vault.toml)"
```

---

### Task 2: Add the end-to-end smoke test

**Files:**
- Modify: `cmd/tusk/cmd_pack_add_test.go` — append `TestPackAdd_VaultPackEndToEnd` immediately after `TestPackAdd_KanbanPackEndToEnd` and before the `testSourceDir` helper at the bottom of the file.

The test mirrors `TestPackAdd_TagsPackEndToEnd` (7.c.2) and `TestPackAdd_KanbanPackEndToEnd` (7.c.3) and additionally exercises wikilink-as-references-edge materialization following the pattern from `cmd/tusk/e2e_edges_test.go` (`os.WriteFile` + `tusk reindex`).

- [ ] **Step 1: Write the failing test**

Append the following function to `cmd/tusk/cmd_pack_add_test.go`. Insert it after the closing `}` of `TestPackAdd_KanbanPackEndToEnd` and before the `testSourceDir` helper:

```go
func TestPackAdd_VaultPackEndToEnd(test *testing.T) {
	dir := test.TempDir()

	// Resolve the repo-local pack file BEFORE chdir-ing.
	packPath := filepath.Join(testSourceDir(test), "..", "..", "packs", "vault.toml")

	if _, statErr := os.Stat(packPath); statErr != nil {
		test.Fatalf("packs/vault.toml not found at %s: %v", packPath, statErr)
	}

	chdir(test, dir)

	// 1. tusk init.
	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"init", "--name", "vault-smoke"})

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("init: %v", execErr)
	}

	// 2. tusk pack add file://<packs/vault.toml>.
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
		"[node-types.note]",
		"[node-types.meeting]",
		"[node-types.decision]",
		"[edge-types.references]",
		"[edge-types.relates-to]",
	} {
		if !strings.Contains(string(manifestBody), want) {
			test.Errorf("tusk.toml missing %s: %q", want, manifestBody)
		}
	}

	// 3. Create one of each node type.
	for _, args := range [][]string{
		{"node", "create", "--type", "note", "--title", "Auth RFC", "--path", "notes/auth-rfc.md"},
		{"node", "create", "--type", "meeting", "--title", "Standup", "--path", "meetings/standup.md"},
		{"node", "create", "--type", "decision", "--title", "Use JWT", "--path", "decisions/jwt.md"},
	} {
		rootCmd = newRootCmd()
		rootCmd.SetArgs(args)
		rootCmd.SetOut(new(bytes.Buffer))
		rootCmd.SetErr(new(bytes.Buffer))

		if execErr := rootCmd.Execute(); execErr != nil {
			test.Fatalf("create %v: %v", args, execErr)
		}
	}

	// 4. Externally write a second note containing a body wikilink.
	external := filepath.Join(dir, "notes/refback.md")

	body := []byte(`---
type: note
title: Backreference
---

This references [[notes/auth-rfc]] in the body.
`)

	if writeErr := os.WriteFile(external, body, 0o644); writeErr != nil {
		test.Fatalf("write external: %v", writeErr)
	}

	// 5. tusk reindex — picks up the wikilink and materializes a `references` edge.
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"reindex"})
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("reindex: %v", execErr)
	}

	// 6. Verify the references edge is present.
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"edge", "list", "--from", "notes/refback"})

	var refbackStdout bytes.Buffer

	rootCmd.SetOut(&refbackStdout)
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("edge list refback: %v", execErr)
	}

	if !strings.Contains(refbackStdout.String(), "references") || !strings.Contains(refbackStdout.String(), "notes/auth-rfc") {
		test.Errorf("edge list output missing references→notes/auth-rfc: %q", refbackStdout.String())
	}

	// 7. Add a relates-to edge between the decision and the note.
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{
		"edge", "add",
		"--type", "relates-to",
		"--source", "decisions/jwt",
		"--target", "notes/auth-rfc",
	})
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("edge add relates-to: %v", execErr)
	}

	// 8. Verify the relates-to edge is present.
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"edge", "list", "--from", "decisions/jwt"})

	var jwtStdout bytes.Buffer

	rootCmd.SetOut(&jwtStdout)
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("edge list decisions/jwt: %v", execErr)
	}

	if !strings.Contains(jwtStdout.String(), "relates-to") || !strings.Contains(jwtStdout.String(), "notes/auth-rfc") {
		test.Errorf("edge list output missing relates-to→notes/auth-rfc: %q", jwtStdout.String())
	}
}
```

- [ ] **Step 2: Run the test to verify it passes**

Run: `go test ./cmd/tusk/ -run TestPackAdd_VaultPackEndToEnd -v`
Expected: PASS. The test exercises pack add, three node creates (one of each type), an externally-written file with a body wikilink, reindex, edge list (verifies the materialized `references` edge), edge add for `relates-to`, and a final edge list (verifies the manual edge).

If any sub-step fails, the cause is one of:

- **Pack add fails (TOML decode error):** check the property/edge section shapes in `packs/vault.toml` match what the existing decoders expect. Cross-reference `packs/tags.toml` and `packs/kanban.toml` for known-good shapes.
- **Node create fails (property validation):** unexpected — none of the three vault node types declare any properties. If failure surfaces, it's likely a manifest-load issue, not property validation.
- **Reindex doesn't produce a `references` edge:** verify `[edge-types.references]` is present in the merged `tusk.toml` (step 2 already asserts this). If present but no edge materializes, check the wikilink text in `notes/refback.md` — it must be `[[notes/auth-rfc]]` (no `.md` extension, workspace-relative path).
- **`edge list --from notes/refback` empty or missing references:** confirm the file was written under `dir + "/notes/refback.md"` and that `notes/refback.md` (without extension) is what `edge list --from` expects (id form is workspace-relative path without extension; this is the same convention as the kanban smoke test).
- **`edge add --type relates-to` fails:** verify the edge type name in the pack file is `relates-to` (with hyphen, no underscore) and that source/target ids omit the `.md` extension.

**CLI flag verification.** The flag shapes used here match the actual CLI as of v1 tip `8121dba`:
- `node create --type --title --path` (per `cmd/tusk/cmd_node_create.go:120-122`)
- `edge add --type --source --target` (per `cmd/tusk/cmd_edge_add.go:124-126`)
- `edge list --from --to --type` (per `cmd/tusk/cmd_edge_list.go:64-66`)
- `reindex` takes no required flags (per `cmd/tusk/cmd_reindex.go`)

If the smoke test fails on flag parsing, fix the test — do not modify the CLI.

- [ ] **Step 3: Run the full test suite**

Run: `make test`
Expected: all packages pass, including `cmd/tusk/...` and the existing tags-pack and kanban-pack smoke tests.

Run: `make lint`
Expected: clean — 0 issues.

Run: `make vet`
Expected: clean.

If `make lint` flags any new lefthook-enforced style issues (Rule 1: ≥2-character identifiers; Rule 2: blank lines around `if err != nil` guards; Rule 4: named errors on shadow), fix them inline in the test.

- [ ] **Step 4: Commit the test**

```bash
git add cmd/tusk/cmd_pack_add_test.go
git commit -m "test(v1): vault pack end-to-end smoke test"
```

---

## After both tasks

Plan 7.c.4 is then ready for PR. The branch `feat/plan-7c4` should contain three commits on top of v1:

1. `dfcd364 docs(spec): plan 7.c.4 — vault pack design` (already landed)
2. `feat(v1): add vault pack content (packs/vault.toml)`
3. `test(v1): vault pack end-to-end smoke test`

Push and open the PR against `v1` with title `feat(v1): plan 7.c.4 — vault pack` and a body summarizing the pack content (three categorical node types + two edge types), the architectural notable (vault pack activates wikilink-as-edge materialization workspace-wide), and the spec divergence (master spec §7.1 decision schema dropped in favor of categorical-only).

After 7.c.4 lands, the entire 7.c series is closed: the `packs/` trilogy (tags + kanban + vault) constitutes the v1.c built-in pack roster.

---

## Self-Review Notes

**Spec coverage:**

| Spec section | Plan task |
|---|---|
| §1 (goal & scope) | Implicit — both tasks |
| §2 (pack file shape) | Task 1 step 1 — full file content |
| §3 (repo layout) | Task 1 step 1 (file goes at `packs/vault.toml`) |
| §4 (divergences) | Captured in spec; plan does not need to re-state them |
| §5 (UX patterns) | Task 2 step 1 exercises patterns 5.1, 5.2, 5.4 (CLI node create, wikilink-as-references-edge, manual relates-to edge) |
| §6 (testing strategy) | Task 2 in full |
| §7 (residuals) | Captured in spec; not implementation work |
| §8 (ledger update) | Captured in spec; not implementation work |

The spec's UX pattern 5.3 (frontmatter `relates-to` array) and 5.5 (composing with other packs) are documentation-only and aren't exercised by the smoke test — pattern 5.3 because the manual edge-add path (5.4) covers the same edge type with simpler test wiring; pattern 5.5 because adding multiple packs broadens the test scope beyond the vault pack itself.

**Placeholder scan:** No `TODO`, `TBD`, "implement later", or vague error-handling stubs.

**Type/identifier consistency:** All flag names (`--type`, `--title`, `--path`, `--source`, `--target`, `--from`) are consistent across tasks and match the CLI as defined in `cmd/tusk/cmd_node_create.go`, `cmd/tusk/cmd_edge_add.go`, and `cmd/tusk/cmd_edge_list.go`. Node ids are workspace-relative paths *without* the `.md` extension (consistent with the existing test patterns in `cmd_pack_add_test.go`, `cmd_node_modify_test.go`, and `e2e_edges_test.go`). Edge type name `relates-to` is used consistently (with hyphen, never `relatesto` or `relates_to`).
