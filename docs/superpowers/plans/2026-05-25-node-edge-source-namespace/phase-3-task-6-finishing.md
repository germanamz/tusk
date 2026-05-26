# Phase 3 — Task 6: Finishing (edge writer integration)

**Phase:** 3 (Edges table reshape)

**Goal:** Land the phase-completion PR — one integration test exercising all three edge writer paths in a single reindex run, plus the phase-summary commit.

## Inherits From

After Task 3.5:
- `edges.kind NOT NULL`, CHECK constraint, UNIQUE includes `source`, new indexes.
- All three writer paths populate `kind`/`source` correctly.

## Files

- **Create:** `internal/index/phase3_integration_test.go`

## Steps

- [ ] **Step 1: Write the integration test**

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

func TestPhase3_AllThreeEdgeKindsPresentAfterRebuild(test *testing.T) {
	test.Parallel()

	root := test.TempDir()

	manifestText := `[workspace]
name = "test"
sub-units = true

[node-types.note]

[node-types.tag]

[node-types.note.properties.tags]
references = ["tag"]
`
	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(manifestText), 0o644); writeErr != nil {
		test.Fatalf("seed manifest: %v", writeErr)
	}

	noteText := `---
title: Standup
mentions: [people/dana]
tags: [retro]
---
# Section A

Body text.
`
	if writeErr := os.WriteFile(filepath.Join(root, "standup.md"), []byte(noteText), 0o644); writeErr != nil {
		test.Fatalf("seed note: %v", writeErr)
	}

	if writeErr := os.WriteFile(filepath.Join(root, "people-dana.md"), []byte("---\ntitle: Dana\n---\n"), 0o644); writeErr != nil {
		test.Fatalf("seed dana: %v", writeErr)
	}

	if writeErr := os.WriteFile(filepath.Join(root, "tags-retro.md"), []byte("---\ntitle: Retro\n---\n"), 0o644); writeErr != nil {
		test.Fatalf("seed tag: %v", writeErr)
	}

	indexPath := filepath.Join(root, ".tusk", "index.db")
	if mkErr := os.MkdirAll(filepath.Dir(indexPath), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	store, openErr := indexopen.OpenOrRebuild(context.Background(), indexopen.Config{
		IndexPath: indexPath,
		ReindexFactory: func(idx *index.Index) reindex.Config {
			return reindex.Config{
				Root: root,
				Repo: index.NewNodeRepo(idx),
				// EdgeTypes loaded from manifest in real flow; tests
				// may use the manifest loader helper here. Refer to
				// reindex_integration_test.go for the pattern.
			}
		},
		Logger: func(string) {},
	})
	if openErr != nil {
		test.Fatalf("OpenOrRebuild: %v", openErr)
	}
	defer store.Close()

	rows, queryErr := store.DB().Query(`SELECT kind, COUNT(*) FROM edges GROUP BY kind`)
	if queryErr != nil {
		test.Fatalf("query: %v", queryErr)
	}
	defer rows.Close()

	counts := map[string]int{}
	for rows.Next() {
		var (
			kind  string
			count int
		)
		if scanErr := rows.Scan(&kind, &count); scanErr != nil {
			test.Fatalf("scan: %v", scanErr)
		}
		counts[kind] = count
	}

	if counts["direct"] == 0 {
		test.Error("expected at least one direct edge (from mentions)")
	}
	if counts["derived"] == 0 {
		test.Error("expected at least one derived edge (from tags references)")
	}
	if counts["structural"] == 0 {
		test.Error("expected at least one structural edge (contains/contained-by)")
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./internal/index/... -run TestPhase3_AllThreeEdgeKindsPresentAfterRebuild -v`

Expected: PASS.

- [ ] **Step 3: Workspace suite**

Run: `go test ./...`

Expected: clean.

- [ ] **Step 4: `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 5: Commit**

```
git add internal/index/phase3_integration_test.go
git commit -m "test(index): phase 3 edges reshape — integration coverage"
```

- [ ] **Step 6: Open the PR**

```
gh pr create --title "feat(index): phase-3 finishing — edges reshape complete" --body "$(cat <<'EOF'
## Summary
- End-to-end test verifies all three edge kinds (`direct`, `derived`, `structural`) are present after a full rebuild
- Phase 3 (edges table reshape) complete: columns added, populated by writer path, tightened with CHECK + UNIQUE + new indexes

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
- Phase 3 finishing PR open.
