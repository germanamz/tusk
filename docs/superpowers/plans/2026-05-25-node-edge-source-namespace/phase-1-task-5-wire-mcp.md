# Phase 1 — Task 5: Wire MCP runtime through `OpenOrRebuild`

**Phase:** 1 (Index rebuild infrastructure)
**Spec:** § *Schema-version contract* (MCP consumer description)

**Goal:** Replace the `index.Open` call in `internal/mcp/runtime.go` with `indexopen.OpenOrRebuild`. The MCP runtime stays online during a rebuild and emits a status notification so the calling agent sees a short unavailability followed by a restored index.

## Inherits From

After Task 4:
- All CLI commands use `OpenOrRebuild`.
- `indexopen.OpenOrRebuild(ctx, cfg)` is well-tested.
- The MCP runtime still calls `index.Open` directly.

## Files

- **Modify:** `internal/mcp/runtime.go` — the single `index.Open` call site.
- **Modify or create:** `internal/mcp/runtime_test.go` — add a rebuild-flow test if a similar test pattern exists; otherwise add a new test file dedicated to the rebuild flow.

The exact line to change can be located via:
```
grep -n 'index.Open' internal/mcp/runtime.go
```

## Steps

- [ ] **Step 1: Locate the existing call**

Run: `grep -n 'index.Open' internal/mcp/runtime.go`

Record the line number and the surrounding function. Note the manifest variable name and the way the runtime initializes repos so the `ReindexFactory` closure has the same constructor shape as the CLI's.

- [ ] **Step 2: Write the failing test**

Create or extend `internal/mcp/runtime_test.go`:

```go
package mcp_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/mcp"
)

func TestRuntimeRebuildsOnSchemaMismatch(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte("[workspace]\nname = \"test\"\n"), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}
	if writeErr := os.WriteFile(filepath.Join(root, "hello.md"), []byte("---\ntitle: hello\n---\nbody\n"), 0o644); writeErr != nil {
		test.Fatalf("write fixture: %v", writeErr)
	}

	indexPath := filepath.Join(root, ".tusk", "index.db")
	if mkErr := os.MkdirAll(filepath.Dir(indexPath), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	// Seed an incompatible version.
	seedStore, openErr := index.Open(indexPath)
	if openErr != nil {
		test.Fatalf("seed open: %v", openErr)
	}
	if setErr := index.NewMetaRepo(seedStore).Set(index.MetaSchemaVersionKey, "from-other-binary"); setErr != nil {
		test.Fatalf("seed mismatch: %v", setErr)
	}
	if closeErr := seedStore.Close(); closeErr != nil {
		test.Fatalf("close seed: %v", closeErr)
	}

	// Boot the runtime against the workspace. The exact constructor name
	// is `mcp.NewRuntime` or `mcp.Boot` — follow whatever the existing
	// code uses. The test asserts that booting succeeds despite the
	// mismatched on-disk version.
	runtime, bootErr := mcp.Boot(context.Background(), mcp.BootConfig{
		WorkspaceRoot: root,
	})
	if bootErr != nil {
		test.Fatalf("mcp.Boot: %v", bootErr)
	}
	defer runtime.Shutdown(context.Background())

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

Adjust the constructor invocation (`mcp.Boot` / `mcp.NewRuntime` / etc.) to match what the codebase actually exposes — read `internal/mcp/runtime.go` to confirm the exported boot API.

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/mcp/... -run TestRuntimeRebuildsOnSchemaMismatch -v`

Expected: failure — the runtime still surfaces `ErrSchemaIncompatible` from `index.Open`.

- [ ] **Step 4: Replace the call in `runtime.go`**

Use the same replacement pattern as the CLI task. The runtime almost certainly has a notion of a status channel or notification stream; route the rebuild log line through it.

Pattern:

```go
store, openErr := indexopen.OpenOrRebuild(ctx, indexopen.Config{
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
        runtime.notify(StatusRebuilding, msg) // or whichever status hook the MCP runtime exposes
    },
})
```

If no status hook exists, route the message to the existing logger (likely a `slog` or similar). The exact wiring is a one-line judgement call by the implementer.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/mcp/... -run TestRuntimeRebuildsOnSchemaMismatch -v`

Expected: PASS.

- [ ] **Step 6: Run the full MCP suite**

Run: `go test ./internal/mcp/... -v`

Expected: every existing MCP test passes.

- [ ] **Step 7: Run the workspace suite**

Run: `go test ./...`

Expected: clean.

- [ ] **Step 8: Run `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 9: Commit**

```
git add internal/mcp
git commit -m "refactor(mcp): route runtime through indexopen.OpenOrRebuild"
```

- [ ] **Step 10: Open the PR**

```
gh pr create --title "refactor(mcp): route runtime through indexopen.OpenOrRebuild" --body "$(cat <<'EOF'
## Summary
- MCP runtime now opens the index via `indexopen.OpenOrRebuild`
- Schema-version mismatch triggers transparent rebuild plus a status notification
- Phase 1, Task 5 of the node/edge source-namespace plan

## Test plan
- [ ] `go test ./internal/mcp/... -v` passes
- [ ] `go test ./...` passes
- [ ] New `TestRuntimeRebuildsOnSchemaMismatch` covers the rebuild flow
- [ ] `make vet && make lint` clean
EOF
)"
```

## Phase 1 finishing PR

After Task 5 merges, open one more PR titled `feat(index): phase-1 finishing — rebuild infrastructure complete`. It should:

- Add `docs/internal/index-rebuild.md` (one page) documenting the schema-version contract, the rebuild flow, and how to bump `SchemaVersion`.
- Add a single end-to-end integration test under `internal/workspace/indexopen/integration_test.go` that:
  - Creates a workspace, opens via the helper (fresh DB path).
  - Closes, seeds a mismatched version, opens again via the helper.
  - Asserts the file was recreated and `schema_version` is current.
  - Closes, opens a third time (matching version), asserts no rebuild log was emitted.
- Does **not** bump `SchemaVersion`.

## Done when

- MCP test passes.
- Workspace suite green.
- Task PR is open.
- Phase finishing PR follows.
