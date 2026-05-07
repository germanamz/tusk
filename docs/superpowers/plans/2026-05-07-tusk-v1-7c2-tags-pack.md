# Tusk v1 — Plan 7.c.2: Tags Pack

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the built-in `tags` pack as a single TOML file at `packs/tags.toml` containing the universal `tag` node type and the `tagged` edge type, plus one end-to-end smoke test that exercises the pack against the real 7.c.1 `tusk pack add` machinery. No engine code.

**Architecture:** Data-only pack. Establishes the `packs/<name>.toml` repository convention used by 7.c.3 (kanban) and 7.c.4 (vault). The 7.c.1 alias map already points the `tags` name at `https://raw.githubusercontent.com/germanamz/tusk/main/packs/tags.toml`, so once `packs/tags.toml` reaches `main` the URL goes live without further code changes. Smoke test uses `file://` against the repo-local pack file to validate end-to-end behavior under PR review.

**Tech Stack:** TOML pack file content; existing `cmd/tusk/cmd_pack_add_test.go` test conventions; no new dependencies.

**Spec reference:** `docs/superpowers/specs/2026-05-07-tusk-v1-7c2-tags-pack-design.md`. Plan 7.c.1's pack platform spec (`docs/superpowers/specs/2026-05-07-tusk-v1-7c1-pack-platform-and-ref-design.md`) is the prerequisite.

**Style rules:** Code respects `STYLE.md` — minimum 2-character identifiers, blank lines around `err` guards, named errors on shadow. Lefthook enforces pre-commit; never `--no-verify`.

**Plan-doc style:** This plan is small. Tasks show file content and test code in full; there is no production Go code to describe.

---

## File Structure

**Created:**

```
packs/
  tags.toml                # the pack file (this plan)
```

**Modified:**

```
cmd/tusk/cmd_pack_add_test.go    # one new end-to-end test function
```

**Excluded for Plan 7.c.2** (per spec §1.2):

- `tags:` frontmatter shorthand — dropped from v1.c.
- Auto-creation of missing tag nodes — dropped from v1.c.
- Path templating — dropped from v1.c.
- Manifest gate flag for strict resolution — dropped from v1.c.
- "Empty tag bodies" doctor surface — dropped from v1.c.
- A `packs/README.md` — not warranted while there is one pack file.

---

## Module Conventions for Plan 7.c.2

**Pack file location.** Pack files live at `packs/<name>.toml` under the tusk repo root. The directory is created in this plan and conventionalized for 7.c.3 (`kanban.toml`) and 7.c.4 (`vault.toml`). The 7.c.1 alias map (`internal/typepacks/aliases.go`) already references these paths via the canonical raw GitHub URLs.

**Smoke test placement.** The end-to-end test for the tags pack lives as a new test function in `cmd/tusk/cmd_pack_add_test.go` (alongside the 7.c.1 pack-add tests), not in a new file. The pack content has no engine logic to unit-test; this single end-to-end test serves as the regression guard.

**Repo-root resolution in the smoke test.** The test runs with working directory `cmd/tusk/`. The pack file is at `<repo-root>/packs/tags.toml`. The test resolves the pack path via `filepath.Abs("../../packs/tags.toml")` and constructs a `file://` URL from the absolute path.

**TDD discipline.** Each task: write the failing test → run to confirm fail → write the minimal implementation → run to confirm pass → commit. Lefthook pre-commit (gofmt, vet, lint, test) runs at each commit; never `--no-verify`. If lefthook blocks, fix the underlying issue.

For Task 1 the "test" is the smoke test from Task 2 fetched in advance — but the smoke test depends on the pack file existing, so the natural ordering is: pack file first (Task 1), smoke test second (Task 2). Task 1's verification is "the file is well-formed TOML and parses against the existing typepacks validator." Task 2's verification is the end-to-end smoke.

---

## Task 0: Pre-flight verification

**Files:** none.

- [ ] **Step 1: Verify branch + clean state**

```bash
git rev-parse --abbrev-ref HEAD
git status --porcelain
```

Expected: `feat/plan-7c2`; only the spec doc (already committed) and possibly the local `tusk` binary as untracked.

- [ ] **Step 2: Verify pre-Plan-7.c.2 is green**

```bash
make build && make test
```

Expected: build succeeds; all tests pass.

- [ ] **Step 3: Verify spec is in tree**

```bash
test -f docs/superpowers/specs/2026-05-07-tusk-v1-7c2-tags-pack-design.md && echo OK
```

Expected: `OK`.

---

## Task 1: Create `packs/tags.toml`

**Files:** Create `packs/tags.toml`.

- [ ] **Step 1: Write the pack file**

Create `packs/tags.toml` with this exact content:

```toml
# Tusk built-in pack: tags
#
# Adds a universal `tag` node type and a `tagged` edge type.
# Tags are simple nodes; nodes can carry multiple tags via the
# many-to-many `tagged` edge.
#
# Usage patterns:
#   - Wikilinks in body: `[[tag/auth]]` materializes a `tagged` edge.
#   - Wikilinks in frontmatter: `tagged: ["[[tag/auth]]"]`.
#   - Ref property opt-in (per node type):
#       properties = [{ name = "tags", type = "list-of", item-type = "ref", to = "tag" }]
#     Then write: `tags: [auth, security]` in frontmatter.
#   - Direct edge: `tusk edge add tagged <node-id> tag/<tag-name>`.

[node-types.tag]
description = "A label that can be applied to any node"
properties = []

[edge-types.tagged]
description = "Marks a node as tagged with a tag"
from = ["*"]
to = ["tag"]
cardinality = "many-to-many"
ordered = false
```

- [ ] **Step 2: Verify the pack file decodes against the existing validator**

The 7.c.1 `internal/typepacks` package already has a `Validate` function that runs both the disallowed-section check and the manifest-schema validator. Verify the pack file passes:

```bash
go run -tags=ignore - <<'EOF'
package main

import (
	"fmt"
	"os"

	"github.com/germanamz/tusk/internal/typepacks"
)

func main() {
	body, readErr := os.ReadFile("packs/tags.toml")

	if readErr != nil {
		fmt.Println("read:", readErr)
		os.Exit(1)
	}

	if _, validateErr := typepacks.Validate(body); validateErr != nil {
		fmt.Println("validate:", validateErr)
		os.Exit(1)
	}

	fmt.Println("OK")
}
EOF
```

Expected: `OK` printed to stdout. (If the inline script form is awkward in the implementer's environment, an alternative verification is `go test ./internal/typepacks/...` — the existing tests don't reference `packs/tags.toml` yet, so no new test fails, but at minimum the pack file shouldn't break anything.)

A simpler one-shot verification using the existing CLI is also available once `tusk` is built: `cd $(mktemp -d) && tusk init --name verify && tusk pack add "file://$REPO_ROOT/packs/tags.toml"`. Whichever works in the implementer's environment is fine.

- [ ] **Step 3: Commit**

```bash
git add packs/tags.toml
git commit -m "feat(packs): tags pack — universal tag node and tagged edge"
```

---

## Task 2: End-to-end smoke test

**Files:** Modify `cmd/tusk/cmd_pack_add_test.go`.

- [ ] **Step 1: Append the failing test to `cmd/tusk/cmd_pack_add_test.go`**

The smoke test exercises the full pack-add → schema-merge → node-create → edge-add chain against the actual `packs/tags.toml` from the repo. Append this test function to the end of `cmd/tusk/cmd_pack_add_test.go`:

```go
func TestPackAdd_TagsPackEndToEnd(test *testing.T) {
	dir := test.TempDir()

	// Resolve the repo-local pack file BEFORE chdir-ing — runtime.Caller
	// returns this test file's absolute path, so we don't depend on cwd.
	packPath := filepath.Join(testSourceDir(test), "..", "..", "packs", "tags.toml")

	if _, statErr := os.Stat(packPath); statErr != nil {
		test.Fatalf("packs/tags.toml not found at %s: %v", packPath, statErr)
	}

	chdir(test, dir)

	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"init", "--name", "tags-smoke"})

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("init: %v", execErr)
	}

	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"pack", "add", "file://" + packPath})

	var stdout, stderr bytes.Buffer

	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("pack add: %v\nstderr: %s", execErr, stderr.String())
	}

	manifestBody, readErr := os.ReadFile(filepath.Join(dir, "tusk.toml"))

	if readErr != nil {
		test.Fatalf("read tusk.toml: %v", readErr)
	}

	if !strings.Contains(string(manifestBody), "[node-types.tag]") {
		test.Errorf("tusk.toml missing [node-types.tag]: %q", manifestBody)
	}

	if !strings.Contains(string(manifestBody), "[edge-types.tagged]") {
		test.Errorf("tusk.toml missing [edge-types.tagged]: %q", manifestBody)
	}

	// Create two tag nodes.
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"node", "create", "--type", "tag", "--title", "auth"})
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("create tag/auth: %v", execErr)
	}

	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"node", "create", "--type", "tag", "--title", "security"})
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("create tag/security: %v", execErr)
	}

	// Add a tagged edge between them (many-to-many semantic — works tag-to-tag too).
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"edge", "add", "tagged", "tag/auth", "tag/security"})
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("add tagged edge: %v", execErr)
	}

	// Verify the edge via `edge list`.
	rootCmd = newRootCmd()
	rootCmd.SetArgs([]string{"edge", "list", "--from", "tag/auth"})

	var listStdout bytes.Buffer

	rootCmd.SetOut(&listStdout)
	rootCmd.SetErr(new(bytes.Buffer))

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("edge list: %v", execErr)
	}

	if !strings.Contains(listStdout.String(), "tagged") || !strings.Contains(listStdout.String(), "tag/security") {
		test.Errorf("edge list output missing expected edge: %q", listStdout.String())
	}
}

// testSourceDir returns the absolute directory of this test source file.
// runtime.Caller(0) returns the caller's file path; calling it from a helper
// in cmd/tusk/cmd_pack_add_test.go yields <repo>/cmd/tusk, which is what we
// need to resolve <repo>/packs/tags.toml regardless of the test's cwd.
func testSourceDir(test *testing.T) string {
	test.Helper()

	_, callerFile, _, ok := runtime.Caller(0)

	if !ok {
		test.Fatal("runtime.Caller failed")
	}

	return filepath.Dir(callerFile)
}
```

Add `runtime` to the imports of `cmd_pack_add_test.go` (it likely is not present; add it). The existing `chdir` helper at `cmd/tusk/cmd_test_helpers_test.go` returns no value — it registers a `t.Cleanup` to restore the original cwd — so the test calls `chdir(test, dir)` with no `defer`.

- [ ] **Step 2: Run, verify pass**

```bash
go test ./cmd/tusk/... -run TestPackAdd_TagsPackEndToEnd -v
```

Expected: PASS. The test exercises the full chain end-to-end:
1. `tusk init` writes a fresh manifest.
2. `tusk pack add file://<repo>/packs/tags.toml` fetches and merges the pack.
3. The resulting tusk.toml contains both pack sections.
4. `tusk node create --type tag` works (the manifest validates the new schema).
5. `tusk edge add tagged` works (edge type is recognized).
6. `tusk edge list` returns the edge.

If any step fails, that's a real regression — investigate before retrying. The first time the test is run after `packs/tags.toml` is committed, it should simply pass.

- [ ] **Step 3: Run the full test suite to confirm no regression**

```bash
go test ./...
```

Expected: PASS across all packages.

- [ ] **Step 4: Commit**

```bash
git add cmd/tusk/cmd_pack_add_test.go
git commit -m "test(cli): end-to-end smoke for tusk pack add tags"
```

---

## Task 3: Final verification + draft PR

**Files:** none new.

- [ ] **Step 1: Full test suite + race**

```bash
make test
```

Expected: PASS.

```bash
make test-race
```

Expected: PASS (or noted as unavailable if CGO is disabled in the implementer's environment, matching the convention from Plan 7.c.1).

- [ ] **Step 2: Lint + vet + fmt**

```bash
make lint
make vet
make fmt
git status --porcelain  # confirm fmt didn't dirty the tree
```

Expected: PASS, clean tree.

- [ ] **Step 3: Push branch**

```bash
git push -u origin feat/plan-7c2
```

- [ ] **Step 4: Open draft PR**

```bash
git log --oneline v1..feat/plan-7c2
gh pr create --base v1 --head feat/plan-7c2 --draft \
  --title "feat(v1): plan 7.c.2 — tags pack" \
  --body "$(cat <<'EOF'
## Summary
- New `packs/tags.toml` — built-in tags pack containing `[node-types.tag]` (empty properties; markdown body is the natural place for tag context) and `[edge-types.tagged]` (`from = ["*"]`, `to = ["tag"]`, many-to-many, unordered). Data only; no engine code.
- Establishes `packs/<name>.toml` repository convention. The 7.c.1 alias map already points the `tags` name at the canonical raw-GitHub URL — once this lands on `main`, `tusk pack add tags` works without further code changes.
- One end-to-end smoke test in `cmd/tusk/cmd_pack_add_test.go` exercising the actual pack file via the real `tusk pack add` chain plus tag-node creation and `tagged`-edge add.
- Explicitly drops v1 design spec §6 features (`tags:` frontmatter shorthand, auto-creation, path templating, manifest gate flag, empty-tag-body doctor surface). Users wanting bare-string-array UX opt in per node-type via the 7.c.1 ref pattern.

## Spec
- docs/superpowers/specs/2026-05-07-tusk-v1-7c2-tags-pack-design.md

## Test plan
- [ ] make test green
- [ ] tusk pack add file://./packs/tags.toml against a fresh workspace produces a valid tusk.toml with both [node-types.tag] and [edge-types.tagged]
- [ ] tusk node create --type tag --title <name> creates tag/<name>.md after the pack is added
- [ ] tusk edge add tagged <node-id> <tag-id> succeeds when both node and tag exist
- [ ] After v1 → main cascade, tusk pack add tags (alias form) fetches the canonical URL and works end-to-end
EOF
)"
```

Return the PR URL. The plan ends here; the PR is the artifact.

---

## Summary

This plan ships:

1. The built-in `tags` pack as a single TOML file (`packs/tags.toml`) — universal `tag` node type plus universal-from `tagged` edge type. Data only.
2. The `packs/<name>.toml` repository convention, used by 7.c.3 (kanban) and 7.c.4 (vault) ahead.
3. One end-to-end smoke test in `cmd/tusk/cmd_pack_add_test.go` exercising the actual pack file against the 7.c.1 pack-add platform.

Subsequent plans:

- **7.c.3** — kanban pack content (`ticket`, `project`, `parent`, `blocks`, `tagged`; workflow behavior on tickets). Note the design choice in 7.c.3 about whether the kanban pack depends on tags being already added (and surfaces a clear collision message) or re-declares `tagged` itself.
- **7.c.4** — vault pack content (`note`, `meeting`, `decision`; `references`, `relates-to`).

---

## Spec

`docs/superpowers/specs/2026-05-07-tusk-v1-7c2-tags-pack-design.md`

---

## Self-Review Notes

**1. Spec coverage.** Every spec section maps to a task or is documentation-only:

| Spec § | Task(s) |
|---|---|
| §1 Goal & Scope | All — Plan 7.c.2 implements the full in-scope list |
| §2 Pack File | T1 (creates `packs/tags.toml`) |
| §3 Repository Layout | T1 (creates `packs/` directory by adding the first file) |
| §4 User UX Patterns | Documentation; T2 exercises pattern 4.4 (direct edge) end-to-end |
| §5 Testing Strategy | T2 (smoke test) |
| §6 Open Questions / Residuals | Documentation, not tasks |
| §7 Plan 7.c+ Ledger Updates | Documentation, not tasks |

§4's patterns 4.1 (body wikilink), 4.2 (frontmatter wikilink), 4.3 (ref opt-in) are documented for users; their underlying mechanisms (Plan 2 wikilink edges, 7.c.1 ref resolver) are already covered by their respective plan test suites. Smoke-testing them again here would duplicate coverage; pattern 4.4 (`tusk edge add tagged`) is the most direct exercise of the pack's edge type and is what T2 tests.

**2. Placeholder scan.** No `TBD` / `TODO` / `FIXME` markers. Every task either shows test code in full or specifies file content verbatim. The pack file content is the actual TOML to write; the smoke test is the actual Go to write.

**3. Type / method consistency.** No new types declared by this plan. The smoke test uses existing helpers (`newRootCmd`, `chdir`) from `cmd/tusk/cmd_test_helpers_test.go` and existing pack-platform machinery (`tusk pack add`, `tusk node create`, `tusk edge add`, `tusk edge list`). The new helper `previousCwd` is local to this test file and is the only addition.

**4. Bundle assignment.** Plan 7.c.2 is small enough for a single implementer subagent. All three tasks (T0, T1, T2, T3) ship in one bundle.

| Bundle | Tasks | Theme |
|---|---|---|
| 1 | T0–T3 | Tags pack file + smoke test + draft PR |

The plan ships the entire deliverable in one bundle because the work is too small to split usefully — splitting "create the file" from "add the smoke test" from "create the PR" would create artificial coordination overhead.
