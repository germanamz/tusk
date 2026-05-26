# Phase 3 — Task 4: Sub-unit `contains` edges → `(structural, markdown)`

**Phase:** 3 (Edges table reshape)
**Spec:** § *Edges table*

**Goal:** Update the sub-unit sync pipeline so every `contains` (and synthesized `contained-by`) edge writes `kind='structural', source='markdown'`.

## Inherits From

After Task 3.3:
- Ref-derived edges → `("derived", nil)`.
- Frontmatter-direct edges → `("direct", nil)`.
- Sub-unit structural edges still use the placeholder from Task 3.2.

## Files

- **Modify:** `internal/subunit/sync.go` — `rewriteContains` and any helper that inserts `contains`/`contained-by` edges.
- **Modify/add:** `internal/subunit/sync_test.go` — assert structural edges carry the correct values.

## Steps

- [ ] **Step 1: Locate the structural-edge writes**

Run: `grep -n 'contains\|contained-by\|Insert.*edge' internal/subunit/sync.go`

Identify each call. The spec notes `subunit/sync.go:67` (`containsEdgeType = "contains"`) and `:281` (insert contains) as the primary sites.

- [ ] **Step 2: Write the failing test**

In `internal/subunit/sync_test.go`:

```go
func TestSubUnitContainsEdgesAreStructural(test *testing.T) {
	test.Parallel()

	// Use the existing sync test fixture pattern. After Sync runs:
	rows, queryErr := store.DB().Query(`
		SELECT kind, source FROM edges WHERE type = 'contains'
	`)
	if queryErr != nil {
		test.Fatalf("query: %v", queryErr)
	}
	defer rows.Close()

	var seen int
	for rows.Next() {
		var (
			kind   *string
			source *string
		)
		if scanErr := rows.Scan(&kind, &source); scanErr != nil {
			test.Fatalf("scan: %v", scanErr)
		}
		seen++
		if kind == nil || *kind != "structural" {
			test.Errorf("contains edge kind = %v, want \"structural\"", kind)
		}
		if source == nil || *source != "markdown" {
			test.Errorf("contains edge source = %v, want \"markdown\"", source)
		}
	}
	if seen == 0 {
		test.Fatal("expected at least one contains edge")
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/subunit/... -run TestSubUnitContainsEdgesAreStructural -v`

Expected: FAIL — placeholder values from Task 3.2 are wrong.

- [ ] **Step 4: Update the writer call sites**

In `internal/subunit/sync.go`, for every `Insert` call that writes a `contains` or `contained-by` edge, pass `("structural", &markdownSource)` where `markdownSource := "markdown"`. Define the constant near the top of the file:

```go
const structuralEdgeKind = "structural"
var markdownSource = "markdown"
```

Then update each call:

```go
if insertErr := edgeRepo.Insert(edge, structuralEdgeKind, &markdownSource); insertErr != nil {
    return fmt.Errorf("insert structural contains edge: %w", insertErr)
}
```

If `contained-by` edges are synthesized in a separate code path (e.g., grammar machinery in `internal/node/edges.go`), update that path too — `contained-by` is structurally produced from `contains` and shares the same `(kind, source)`.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/subunit/... -run TestSubUnitContainsEdgesAreStructural -v`

Expected: PASS.

- [ ] **Step 6: Run the workspace suite**

Run: `go test ./...`

Expected: clean.

- [ ] **Step 7: `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 8: Commit**

```
git add internal/subunit internal/node
git commit -m "feat(subunit): contains/contained-by edges write kind='structural', source='markdown'"
```

- [ ] **Step 9: Open the PR**

```
gh pr create --title "feat(subunit): contains/contained-by edges write kind='structural', source='markdown'" --body "$(cat <<'EOF'
## Summary
- Sub-unit sync (and the contained-by synthesis path) now writes `kind='structural', source='markdown'` on every structural edge
- All three edge writer paths now populate kind/source correctly:
  - ref-derived → ("derived", NULL) [Task 3.2]
  - frontmatter-direct → ("direct", NULL) [Task 3.3]
  - structural → ("structural", "markdown") [this task]
- Phase 3, Task 4 of the node/edge source-namespace plan

## Test plan
- [ ] `go test ./internal/subunit/... -v` passes
- [ ] `go test ./...` passes
- [ ] `make vet && make lint` clean
EOF
)"
```

## Done when

- Structural-edge test passes.
- Workspace suite green.
- PR open.
