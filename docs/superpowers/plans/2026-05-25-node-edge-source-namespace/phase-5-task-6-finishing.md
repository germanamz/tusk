# Phase 5 — Task 6: Finishing (end-to-end reference resolution)

**Phase:** 5 (Reference-resolution grammar)

**Goal:** Land the phase-completion PR — one integration test exercising all three reference forms (`type`, `:type`, `source:type`) end-to-end through the CLI or MCP path.

## Inherits From

After Task 5.5:
- `typeref` package and parser exist.
- Walker, query, and MCP layers all parse via `typeref`.
- `NeighborsByEdgeTypes` is gone.

## Files

- **Create:** `internal/query/phase5_integration_test.go`

## Steps

- [ ] **Step 1: Write the integration test**

```go
package query_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/graphexpand"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/typeref"
	"github.com/germanamz/tusk/internal/workspace/indexopen"
)

func TestPhase5_ThreeReferenceFormsAcrossWalker(test *testing.T) {
	test.Parallel()

	root := test.TempDir()

	manifestText := `[workspace]
name = "test"
sub-units = true

[edge-types.contains]
from = ["project"]
to   = ["task"]
`
	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(manifestText), 0o644); writeErr != nil {
		test.Fatalf("seed manifest: %v", writeErr)
	}

	if writeErr := os.WriteFile(filepath.Join(root, "project-launch.md"), []byte("---\ntitle: Launch\ncontains: [task-a]\n---\n"), 0o644); writeErr != nil {
		test.Fatalf("seed project: %v", writeErr)
	}
	if writeErr := os.WriteFile(filepath.Join(root, "task-a.md"), []byte("---\ntitle: Task A\n---\n"), 0o644); writeErr != nil {
		test.Fatalf("seed task: %v", writeErr)
	}

	noteText := `---
title: Standup
---
# Section A

Body.
`
	if writeErr := os.WriteFile(filepath.Join(root, "standup.md"), []byte(noteText), 0o644); writeErr != nil {
		test.Fatalf("seed note: %v", writeErr)
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
			}
		},
		Logger: func(string) {},
	})
	if openErr != nil {
		test.Fatalf("OpenOrRebuild: %v", openErr)
	}
	defer store.Close()

	edges := index.NewEdgeRepo(store)

	cases := []struct {
		name        string
		input       string
		expectIDs   []string
		notExpect   []string
	}{
		{
			name:      "bare matches union (both user and markdown)",
			input:     "contains",
			expectIDs: []string{"task-a", "standup#a3f1"}, // placeholder hash; assert size, not exact id
		},
		{
			name:      ":type matches user only",
			input:     ":contains",
			expectIDs: []string{"task-a"},
			notExpect: []string{"standup#"}, // no subunit
		},
		{
			name:      "source:type matches one source only",
			input:     "markdown:contains",
			expectIDs: []string{"standup#"}, // any subunit id
			notExpect: []string{"task-a"},
		},
	}

	for _, tc := range cases {
		tc := tc
		test.Run(tc.name, func(test *testing.T) {
			test.Parallel()

			ref, parseErr := typeref.Parse(tc.input)
			if parseErr != nil {
				test.Fatalf("Parse(%q): %v", tc.input, parseErr)
			}

			walker := graphexpand.Walker{
				Edges:    edges,
				Hops:     1,
				EdgeRefs: []typeref.EdgeRef{ref},
			}

			distances := map[string]int{"project-launch": 0, "standup": 0}
			result, err := walker.Expand(distances)
			if err != nil {
				test.Fatalf("Expand: %v", err)
			}

			for _, want := range tc.expectIDs {
				found := false
				for id := range result {
					if strings.HasPrefix(id, want) {
						found = true
						break
					}
				}
				if !found {
					test.Errorf("missing %q in result %v", want, result)
				}
			}

			for _, dont := range tc.notExpect {
				for id := range result {
					if strings.HasPrefix(id, dont) {
						test.Errorf("did not expect %q-prefixed id, got %q", dont, id)
					}
				}
			}
		})
	}
}
```

(Adjust the `Expand` return shape and result inspection to match the actual `graphexpand.Walker` API.)

- [ ] **Step 2: Run the test**

Run: `go test ./internal/query/... -run TestPhase5_ThreeReferenceFormsAcrossWalker -v`

Expected: PASS.

- [ ] **Step 3: Workspace suite**

Run: `go test ./...`

Expected: clean.

- [ ] **Step 4: `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 5: Commit**

```
git add internal/query/phase5_integration_test.go
git commit -m "test(query): phase 5 reference resolution — three forms integration"
```

- [ ] **Step 6: Open the PR**

```
gh pr create --title "feat(query): phase-5 finishing — reference resolution complete" --body "$(cat <<'EOF'
## Summary
- End-to-end test verifies bare, `:type`, and `source:type` forms behave as documented through the walker
- Phase 5 (reference resolution grammar) complete

## Test plan
- [ ] `go test ./internal/query/... -v` passes
- [ ] `go test ./...` passes
- [ ] `make vet && make lint` clean
EOF
)"
```

## Done when

- Three-form integration test passes.
- Workspace suite green.
- Phase 5 finishing PR open.
