# Phase 2 — Task 6: Finishing (integration + behavior preservation)

**Phase:** 2 (Nodes table reshape)

**Goal:** Land the phase-completion PR — one end-to-end integration test confirming that the rebuilt index produces the same observable behavior as before for representative CLI commands, and a phase-summary commit message.

## Inherits From

After Task 2.5:
- `nodes.kind NOT NULL`, CHECK constraint, composite index, partial UNIQUE predicate on `kind='file'`.
- Two `SchemaVersion` bumps in Phase 2 (Task 2.1 and Task 2.5).
- Writers populate `kind`/`source` correctly.
- Readers use `kind` as the row-class discriminator everywhere.

## Files

- **Create:** `internal/index/phase2_integration_test.go` (or extend `internal/reindex/reindex_integration_test.go` if the existing fixture already covers what we need).

## Steps

- [ ] **Step 1: Write the integration test**

Create `internal/index/phase2_integration_test.go`:

```go
package index_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/workspace/indexopen"
)

// TestPhase2_RebuildPreservesNodeShape rebuilds a workspace from
// scratch and asserts every row carries valid kind/source values.
func TestPhase2_RebuildPreservesNodeShape(test *testing.T) {
	test.Parallel()

	root := test.TempDir()

	noteWithSections := `---
title: Standup
---
# Section One

Paragraph one.

# Section Two

Paragraph two.
`
	if writeErr := os.WriteFile(filepath.Join(root, "standup.md"), []byte(noteWithSections), 0o644); writeErr != nil {
		test.Fatalf("seed file: %v", writeErr)
	}
	indexPath := filepath.Join(root, ".tusk", "index.db")
	if mkErr := os.MkdirAll(filepath.Dir(indexPath), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	cfg := indexopen.Config{
		IndexPath: indexPath,
		ReindexFactory: func(idx *index.Index) reindex.Config {
			return reindex.Config{
				Root: root,
				Repo: index.NewNodeRepo(idx),
			}
		},
		Logger: func(string) {},
	}

	store, openErr := indexopen.OpenOrRebuild(context.Background(), cfg)
	if openErr != nil {
		test.Fatalf("OpenOrRebuild: %v", openErr)
	}
	defer store.Close()

	// Every row has a valid kind value.
	var nullKindCount int
	if scanErr := store.DB().QueryRow(`SELECT COUNT(*) FROM nodes WHERE kind IS NULL`).Scan(&nullKindCount); scanErr != nil {
		test.Fatalf("kind null count: %v", scanErr)
	}
	if nullKindCount != 0 {
		test.Errorf("found %d rows with NULL kind", nullKindCount)
	}

	// File rows have source=NULL; sub-units have source='markdown'.
	var fileBadCount int
	if scanErr := store.DB().QueryRow(`SELECT COUNT(*) FROM nodes WHERE kind = 'file' AND source IS NOT NULL`).Scan(&fileBadCount); scanErr != nil {
		test.Fatalf("file source count: %v", scanErr)
	}
	if fileBadCount != 0 {
		test.Errorf("found %d file rows with non-NULL source", fileBadCount)
	}

	var subUnitBadCount int
	if scanErr := store.DB().QueryRow(`SELECT COUNT(*) FROM nodes WHERE kind = 'subunit' AND (source IS NULL OR source != 'markdown')`).Scan(&subUnitBadCount); scanErr != nil {
		test.Fatalf("subunit source count: %v", scanErr)
	}
	if subUnitBadCount != 0 {
		test.Errorf("found %d sub-unit rows with wrong source", subUnitBadCount)
	}

	// At least one of each kind exists.
	var fileCount, subUnitCount int
	if scanErr := store.DB().QueryRow(`SELECT COUNT(*) FROM nodes WHERE kind='file'`).Scan(&fileCount); scanErr != nil {
		test.Fatalf("file count: %v", scanErr)
	}
	if scanErr := store.DB().QueryRow(`SELECT COUNT(*) FROM nodes WHERE kind='subunit'`).Scan(&subUnitCount); scanErr != nil {
		test.Fatalf("subunit count: %v", scanErr)
	}
	if fileCount == 0 {
		test.Error("expected at least one file row")
	}
	if subUnitCount == 0 {
		test.Error("expected at least one subunit row")
	}
}
```

- [ ] **Step 2: Run the integration test**

Run: `go test ./internal/index/... -run TestPhase2_RebuildPreservesNodeShape -v`

Expected: PASS.

- [ ] **Step 3: Run the workspace suite**

Run: `go test ./...`

Expected: clean.

- [ ] **Step 4: `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 5: Commit**

```
git add internal/index/phase2_integration_test.go
git commit -m "test(index): phase 2 nodes reshape — integration coverage"
```

- [ ] **Step 6: Open the PR**

```
gh pr create --title "feat(index): phase-2 finishing — nodes reshape complete" --body "$(cat <<'EOF'
## Summary
- End-to-end integration test rebuilds a workspace and asserts every row has a valid kind/source pair
- Phase 2 (nodes table reshape) complete: schema columns added, populated, tightened, and readers switched off `parent_id`
- Two SchemaVersion bumps landed during the phase; both rebuild transparently via `OpenOrRebuild`

## Test plan
- [ ] `go test ./internal/index/... -v` passes
- [ ] `go test ./...` passes
- [ ] `make vet && make lint` clean
EOF
)"
```

## Done when

- Integration test passes.
- Workspace suite green.
- Phase 2 finishing PR open.
