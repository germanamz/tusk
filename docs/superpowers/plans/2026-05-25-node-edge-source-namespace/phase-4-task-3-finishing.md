# Phase 4 — Task 3: Finishing (end-to-end user-namespace test)

**Phase:** 4 (Reservation rescoping)

**Goal:** Land the phase-completion PR — an end-to-end test exercising a workspace whose manifest declares user-namespace `section` and `contains`, run reindex, and verify both `(file, NULL, section)` user-declared rows and `(subunit, markdown, section)` pack-derived rows coexist in the same index.

## Inherits From

After Task 4.2:
- `subdocument.Source()` returns `"markdown"`.
- `SubUnitConflict` no longer fires for user-vs-pack collisions.

## Files

- **Create:** `internal/index/phase4_integration_test.go`

## Steps

- [x] **Step 1: Write the integration test**

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

func TestPhase4_UserSectionAndPackSectionCoexist(test *testing.T) {
	test.Parallel()

	root := test.TempDir()

	manifestText := `[workspace]
name = "test"
sub-units = true

[node-types.section]

[node-types.section.properties.summary]
type = "string"
`
	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(manifestText), 0o644); writeErr != nil {
		test.Fatalf("seed manifest: %v", writeErr)
	}

	// User-declared section node.
	if writeErr := os.WriteFile(filepath.Join(root, "section-overview.md"), []byte("---\ntitle: Overview\nsummary: hi\n---\n"), 0o644); writeErr != nil {
		test.Fatalf("seed user section: %v", writeErr)
	}

	// File with markdown sub-units (pack section rows).
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

	// User-declared section row.
	var userSectionCount int
	if scanErr := store.DB().QueryRow(`SELECT COUNT(*) FROM nodes WHERE kind='file' AND source IS NULL AND type='section'`).Scan(&userSectionCount); scanErr != nil {
		test.Fatalf("user section count: %v", scanErr)
	}
	if userSectionCount == 0 {
		test.Error("expected user-declared (file, NULL, section) row")
	}

	// Pack-derived section row.
	var packSectionCount int
	if scanErr := store.DB().QueryRow(`SELECT COUNT(*) FROM nodes WHERE kind='subunit' AND source='markdown' AND type='section'`).Scan(&packSectionCount); scanErr != nil {
		test.Fatalf("pack section count: %v", scanErr)
	}
	if packSectionCount == 0 {
		test.Error("expected pack-derived (subunit, markdown, section) row")
	}
}
```

- [x] **Step 2: Run the test**

Run: `go test ./internal/index/... -run TestPhase4_UserSectionAndPackSectionCoexist -v`

Expected: PASS.

- [x] **Step 3: Workspace suite**

Run: `go test ./...`

Expected: clean.

- [x] **Step 4: `make vet` and `make lint`**

Expected: clean.

- [x] **Step 5: Commit**

```
git add internal/index/phase4_integration_test.go
git commit -m "test(index): user-namespace section coexists with pack-namespace section"
```

- [x] **Step 6: Open the PR**

```
gh pr create --title "feat(manifest): phase-4 finishing — user/pack reservations coexist" --body "$(cat <<'EOF'
## Summary
- Integration test confirms a workspace can declare a user-namespace `section` node-type and still receive pack-derived `(subunit, markdown, section)` rows
- Phase 4 (reservation rescoping) complete

## Test plan
- [ ] `go test ./internal/index/... -v` passes
- [ ] `go test ./...` passes
- [ ] `make vet && make lint` clean
EOF
)"
```

## Done when

- Coexistence test passes.
- Workspace suite green.
- Phase 4 finishing PR open.
