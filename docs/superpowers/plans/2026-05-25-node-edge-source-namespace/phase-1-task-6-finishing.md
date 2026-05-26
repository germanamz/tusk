# Phase 1 — Task 6: Finishing (integration + docs)

**Phase:** 1 (Index rebuild infrastructure)

**Goal:** Land the phase-completion PR — end-to-end integration test exercising the helper across the full open-mismatch-rebuild-reopen cycle, plus a short internal doc explaining when and how to bump `SchemaVersion`.

## Inherits From

After Task 5:
- `index.SchemaVersion`, `MetaSchemaVersionKey`, `ErrSchemaIncompatible`, `SchemaVersionError` exist.
- `indexopen.OpenOrRebuild` exists.
- CLI and MCP route through the helper.
- No version bump has happened; rebuild path is exercised only by tests that hand-seed the meta key.

## Files

- **Create:** `internal/workspace/indexopen/integration_test.go`
- **Create:** `docs/internal/index-rebuild.md`

## Steps

- [ ] **Step 1: Write the integration test**

Create `internal/workspace/indexopen/integration_test.go`:

```go
package indexopen_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/workspace/indexopen"
)

func TestFullCycle_OpenMismatchRebuildReopen(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	if writeErr := os.WriteFile(filepath.Join(root, "hello.md"), []byte("---\ntitle: hello\n---\nbody\n"), 0o644); writeErr != nil {
		test.Fatalf("seed file: %v", writeErr)
	}
	indexPath := filepath.Join(root, ".tusk", "index.db")
	if mkErr := os.MkdirAll(filepath.Dir(indexPath), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	factory := func(idx *index.Index) reindex.Config {
		return reindex.Config{
			Root: root,
			Repo: index.NewNodeRepo(idx),
		}
	}

	var logs []string
	cfg := indexopen.Config{
		IndexPath:      indexPath,
		ReindexFactory: factory,
		Logger:         func(m string) { logs = append(logs, m) },
	}

	// 1. Fresh open writes SchemaVersion.
	first, openErr := indexopen.OpenOrRebuild(context.Background(), cfg)
	if openErr != nil {
		test.Fatalf("fresh open: %v", openErr)
	}
	if got, _ := index.NewMetaRepo(first).Get(index.MetaSchemaVersionKey); got != index.SchemaVersion {
		test.Errorf("fresh schema_version = %q, want %q", got, index.SchemaVersion)
	}
	if closeErr := first.Close(); closeErr != nil {
		test.Fatalf("close fresh: %v", closeErr)
	}
	if len(logs) != 0 {
		test.Errorf("unexpected rebuild log on fresh open: %v", logs)
	}

	// 2. Seed mismatch, reopen via helper, assert rebuild.
	second, openErr := index.Open(indexPath)
	if openErr != nil {
		test.Fatalf("seed reopen: %v", openErr)
	}
	if setErr := index.NewMetaRepo(second).Set(index.MetaSchemaVersionKey, "from-other-binary"); setErr != nil {
		test.Fatalf("seed mismatch: %v", setErr)
	}
	if closeErr := second.Close(); closeErr != nil {
		test.Fatalf("close seed: %v", closeErr)
	}

	third, openErr := indexopen.OpenOrRebuild(context.Background(), cfg)
	if openErr != nil {
		test.Fatalf("rebuild open: %v", openErr)
	}
	defer third.Close()

	if got, _ := index.NewMetaRepo(third).Get(index.MetaSchemaVersionKey); got != index.SchemaVersion {
		test.Errorf("after rebuild schema_version = %q, want %q", got, index.SchemaVersion)
	}
	if len(logs) == 0 {
		test.Error("expected at least one rebuild log entry")
	}

	// 3. Re-open with matching version — no further rebuild log.
	logsBefore := len(logs)
	fourth, openErr := indexopen.OpenOrRebuild(context.Background(), cfg)
	if openErr != nil {
		test.Fatalf("steady-state reopen: %v", openErr)
	}
	defer fourth.Close()
	if len(logs) != logsBefore {
		test.Errorf("unexpected rebuild log on matching-version reopen: %v", logs[logsBefore:])
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./internal/workspace/indexopen/... -run TestFullCycle_OpenMismatchRebuildReopen -v`

Expected: PASS.

- [ ] **Step 3: Write the docs page**

Create `docs/internal/index-rebuild.md`:

```markdown
# Index Rebuild on Schema Change

This page documents the schema-version contract that lets tusk
detect an incompatible on-disk index and rebuild it from source.

## When to bump `SchemaVersion`

`internal/index.SchemaVersion` is the on-disk schema generation.
Bump it whenever the schema shape changes in a way that an
in-place migration cannot bridge:

- New `NOT NULL` columns added to existing tables
- New `CHECK` constraints
- Modified `UNIQUE` constraints
- Changed index predicates
- Any DDL that the existing `CREATE TABLE IF NOT EXISTS` path
  cannot apply to an existing table

Adding tables, adding indexes, and other purely additive changes
do not require a bump.

## Rebuild flow

`index.Open` reads `meta.schema_version` and compares it against
`SchemaVersion`. A mismatch returns `*index.SchemaVersionError`
(wrapped `ErrSchemaIncompatible`).

`internal/workspace/indexopen.OpenOrRebuild` catches the sentinel,
deletes the on-disk file, re-opens (writing the current
`SchemaVersion` into a fresh database), and runs `reindex.Run` to
repopulate from source. Every CLI command and the MCP runtime go
through this helper.

## User experience

First invocation after upgrade emits one log line:

```
index schema changed in this version, rebuilding…
```

…then runs a full reindex. Cost is identical to
`tusk reindex --force`. Subsequent invocations open instantly.
```

- [ ] **Step 4: Run `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 5: Commit**

```
git add internal/workspace/indexopen/integration_test.go docs/internal/index-rebuild.md
git commit -m "test(indexopen): full open-mismatch-rebuild-reopen integration"
```

- [ ] **Step 6: Open the PR**

```
gh pr create --title "feat(index): phase-1 finishing — rebuild infrastructure complete" --body "$(cat <<'EOF'
## Summary
- Adds the full-cycle integration test for `indexopen.OpenOrRebuild`
- Documents the schema-version contract in `docs/internal/index-rebuild.md`
- Phase 1 (rebuild infrastructure) complete

## Test plan
- [ ] `go test ./internal/workspace/indexopen/... -v` passes
- [ ] `go test ./...` passes
- [ ] `make vet && make lint` clean
EOF
)"
```

## Done when

- Integration test passes.
- Internal doc exists.
- Phase 1 finishing PR is open.
