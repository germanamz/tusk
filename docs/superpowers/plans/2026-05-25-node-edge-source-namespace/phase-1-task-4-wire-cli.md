# Phase 1 — Task 4: Wire every CLI command through `OpenOrRebuild`

**Phase:** 1 (Index rebuild infrastructure)
**Spec:** § *Schema-version contract* (CLI consumer description)

**Goal:** Replace every `index.Open(ws.IndexPath)` call site in `cmd/tusk/cmd_*.go` with a call to `indexopen.OpenOrRebuild`. After this task lands, no CLI command can encounter `ErrSchemaIncompatible` directly — the helper handles it.

The schema version has not been bumped yet, so the rebuild path is dormant in production. The change is mechanically pure refactoring: same behavior when versions match, transparent rebuild when they don't.

## Inherits From

After Task 3:
- `indexopen.OpenOrRebuild(ctx, cfg)` exists with `IndexPath`, `ReindexFactory`, `Logger`.
- `index.Open` still returns `ErrSchemaIncompatible` on mismatch.
- CLI commands still call `index.Open` directly.

## Files

- **Modify:** every `cmd/tusk/cmd_*.go` file that imports and calls `index.Open`. Per the spec touchpoints survey, this includes (but is not limited to):
  - `cmd_context.go`
  - `cmd_doctor.go`
  - `cmd_edge_add.go`
  - `cmd_edge_list.go`
  - `cmd_edge_remove.go`
  - `cmd_init.go`
  - `cmd_node_create.go`
  - `cmd_node_delete.go`
  - `cmd_node_get.go`
  - `cmd_node_list.go`
  - `cmd_node_modify.go`
  - `cmd_node_move.go`
  - `cmd_query.go`
  - `cmd_reindex.go`
  - `cmd_run.go`
  - `cmd_status.go`
  - `cmd_watch.go`

- **Modify:** any `cmd/tusk/*_test.go` that opens the index directly should remain using `index.Open` if it is testing the open path itself; tests that simulate the production CLI should switch to the helper. Decision per-test at implementation time.

The implementer should run the grep below at the start to get the authoritative list.

## Steps

- [ ] **Step 1: Enumerate the call sites**

Run:
```
grep -rln 'index.Open' cmd/tusk | sort
```

Expected: ~17 files. Record the list.

- [ ] **Step 2: Write a failing test that asserts a CLI command uses the helper**

This step is harder to write as a direct unit test — the change is structural. Instead, write an integration test in `cmd/tusk/cmd_open_or_rebuild_test.go` that exercises one CLI command (use `cmd_reindex.go` because it is the simplest and definitely opens the index) against a workspace whose index has a mismatched version, and verifies the command succeeds (i.e., the rebuild happened).

```go
package main_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestReindexCommandRebuildsOnSchemaMismatch(test *testing.T) {
	// Build the binary once in t.TempDir for the test.
	binary := filepath.Join(test.TempDir(), "tusk")
	build := exec.Command("go", "build", "-o", binary, "./cmd/tusk")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if buildErr := build.Run(); buildErr != nil {
		test.Fatalf("build tusk: %v", buildErr)
	}

	// Set up a workspace with one note and a hand-seeded mismatched index.
	root := test.TempDir()
	if writeErr := os.WriteFile(filepath.Join(root, "hello.md"), []byte("---\ntitle: hello\n---\nbody\n"), 0o644); writeErr != nil {
		test.Fatalf("write fixture: %v", writeErr)
	}
	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte("[workspace]\nname = \"test\"\n"), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	indexPath := filepath.Join(root, ".tusk", "index.db")
	if mkErr := os.MkdirAll(filepath.Dir(indexPath), 0o755); mkErr != nil {
		test.Fatalf("mkdir index dir: %v", mkErr)
	}

	// Seed an incompatible version.
	store, openErr := index.Open(indexPath)
	if openErr != nil {
		test.Fatalf("seed index open: %v", openErr)
	}
	if setErr := index.NewMetaRepo(store).Set(index.MetaSchemaVersionKey, "from-other-binary"); setErr != nil {
		test.Fatalf("seed mismatch: %v", setErr)
	}
	if closeErr := store.Close(); closeErr != nil {
		test.Fatalf("close seed: %v", closeErr)
	}

	cmd := exec.Command(binary, "reindex")
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if runErr := cmd.Run(); runErr != nil {
		test.Fatalf("tusk reindex: %v\nstderr:\n%s", runErr, stderr.String())
	}

	// Verify the rebuilt index has the current schema_version.
	reopened, reopenErr := index.Open(indexPath)
	if reopenErr != nil {
		test.Fatalf("reopen after rebuild: %v", reopenErr)
	}
	defer reopened.Close()

	got, getErr := index.NewMetaRepo(reopened).Get(index.MetaSchemaVersionKey)
	if getErr != nil {
		test.Fatalf("read schema_version: %v", getErr)
	}
	if got != index.SchemaVersion {
		test.Errorf("rebuilt schema_version = %q, want %q", got, index.SchemaVersion)
	}
}
```

This test is intentionally an exec-based integration test because the goal is to exercise the wired CLI surface.

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./cmd/tusk -run TestReindexCommandRebuildsOnSchemaMismatch -v`

Expected: failure — the command currently calls `index.Open` directly and surfaces `ErrSchemaIncompatible` to the user.

- [ ] **Step 4: Replace the call sites**

For each file in the Step 1 list, perform this replacement pattern. The exact lines vary by file; the shape is:

**Before:**
```go
store, openErr := index.Open(ws.IndexPath)
if openErr != nil {
    return fmt.Errorf("open index: %w", openErr)
}
defer store.Close()
```

**After:**
```go
store, openErr := indexopen.OpenOrRebuild(cmd.Context(), indexopen.Config{
    IndexPath: ws.IndexPath,
    ReindexFactory: func(idx *index.Index) reindex.Config {
        return reindex.Config{
            Root:      ws.Root,
            Repo:      index.NewNodeRepo(idx),
            Edges:     index.NewEdgeRepo(idx),
            EdgeTypes: loaded.EdgeTypes,
        }
    },
    Logger: func(msg string) {
        fmt.Fprintln(cmd.ErrOrStderr(), msg)
    },
})
if openErr != nil {
    return fmt.Errorf("open index: %w", openErr)
}
defer store.Close()
```

Important details:

1. **Manifest availability.** Most CLI commands load the manifest via `manifest.Load(ws.Root)` before opening the index. The `loaded` variable above refers to whatever the existing command names it. The `EdgeTypes` field is needed so `reindex.Run` can resolve frontmatter edges during rebuild. If a command does not load the manifest before opening the index, move the manifest load earlier in the function.

2. **Workspace root.** The `ws.Root` field comes from the existing workspace bootstrap (`workspace.Load`). It is already available wherever `ws.IndexPath` is.

3. **Context.** Cobra commands expose `cmd.Context()`. Use it directly.

4. **Logger.** Routes the rebuild notice to stderr so it does not pollute stdout-parsing tests. Use `cmd.ErrOrStderr()` (Cobra's stderr writer).

5. **Import additions.** Each touched file likely needs:
   - `"github.com/germanamz/tusk/internal/workspace/indexopen"`
   - `"github.com/germanamz/tusk/internal/reindex"`
   Verify each file's import block.

Walk through every file from Step 1 individually. Do not batch-script — the surrounding context matters (manifest variable name, error message wording).

- [ ] **Step 5: Run the new integration test**

Run: `go test ./cmd/tusk -run TestReindexCommandRebuildsOnSchemaMismatch -v`

Expected: PASS.

- [ ] **Step 6: Run the full CLI test suite**

Run: `go test ./cmd/tusk/... -v`

Expected: every existing test passes. If a test was opening the index with the old call signature (test files were intentionally not modified in Step 4 unless they simulate the production CLI), make a case-by-case decision: leave it on `index.Open` if it is testing `Open` itself, switch to the helper otherwise.

- [ ] **Step 7: Run the workspace suite**

Run: `go test ./...`

Expected: clean.

- [ ] **Step 8: Run `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 9: Commit**

```
git add cmd/tusk
git commit -m "refactor(cli): route every command through indexopen.OpenOrRebuild"
```

- [ ] **Step 10: Open the PR**

```
gh pr create --title "refactor(cli): route every command through indexopen.OpenOrRebuild" --body "$(cat <<'EOF'
## Summary
- Every CLI command that opens the index now calls `indexopen.OpenOrRebuild` instead of `index.Open`
- Rebuild path is dormant in production (no schema bump yet) but unit-tested via a hand-seeded mismatched index
- Phase 1, Task 4 of the node/edge source-namespace plan

## Test plan
- [ ] `go test ./cmd/tusk/... -v` passes
- [ ] `go test ./...` passes
- [ ] New `TestReindexCommandRebuildsOnSchemaMismatch` covers the rebuild flow end-to-end via the CLI binary
- [ ] `make vet && make lint` clean
EOF
)"
```

## Done when

- Every CLI command opens the index via `OpenOrRebuild`.
- The new integration test passes.
- Workspace suite green.
- PR is open.
