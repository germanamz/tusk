# Phase 5 — Task 2: `NeighborsByEdgeRefs` in `internal/index/edge_repo.go`

**Phase:** 5 (Reference-resolution grammar)
**Spec:** § *Reference resolution in code*

**Goal:** Add `NeighborsByEdgeRefs([]typeref.EdgeRef, sourceIDs)` to `EdgeRepo`, alongside the existing `NeighborsByEdgeTypes`. The new method builds a grouped OR clause that respects scope semantics. The old method stays in place during Phase 5; Task 5.5 deletes it.

## Inherits From

- After Task 5.1: `typeref` package exists with `Ref`/`Scope` types and `Parse`/`ParseMany`.

## Files

- **Modify:** `internal/index/edge_repo.go`
- **Modify/add:** `internal/index/edge_repo_test.go`

## Steps

- [ ] **Step 1: Write the failing tests**

Add to `internal/index/edge_repo_test.go`:

```go
func TestNeighborsByEdgeRefs_ScopeAnyMatchesUnion(test *testing.T) {
	test.Parallel()

	store, _ := index.Open(filepath.Join(test.TempDir(), "tusk.db"))
	defer store.Close()
	seedFourNodes(test, store)

	// Two contains edges: one user-namespace, one structural.
	insertEdge(test, store, "contains", "projects/a", "projects/b", "projects/a.md", "direct", nil)
	insertEdge(test, store, "contains", "notes/a", "notes/a#sec", "notes/a.md", "structural", strptr("markdown"))

	repo := index.NewEdgeRepo(store)
	rows, err := repo.NeighborsByEdgeRefs(
		[]typeref.EdgeRef{{Scope: typeref.ScopeAny, Type: "contains"}},
		[]string{"projects/a", "notes/a"},
	)
	if err != nil {
		test.Fatalf("NeighborsByEdgeRefs: %v", err)
	}
	if len(rows) != 2 {
		test.Errorf("ScopeAny returned %d rows, want 2", len(rows))
	}
}

func TestNeighborsByEdgeRefs_ScopeUserMatchesNullSource(test *testing.T) {
	test.Parallel()

	store, _ := index.Open(filepath.Join(test.TempDir(), "tusk.db"))
	defer store.Close()
	seedFourNodes(test, store)
	insertEdge(test, store, "contains", "projects/a", "projects/b", "projects/a.md", "direct", nil)
	insertEdge(test, store, "contains", "notes/a", "notes/a#sec", "notes/a.md", "structural", strptr("markdown"))

	repo := index.NewEdgeRepo(store)
	rows, err := repo.NeighborsByEdgeRefs(
		[]typeref.EdgeRef{{Scope: typeref.ScopeUser, Type: "contains"}},
		[]string{"projects/a", "notes/a"},
	)
	if err != nil {
		test.Fatalf("NeighborsByEdgeRefs: %v", err)
	}
	if len(rows) != 1 {
		test.Errorf("ScopeUser returned %d rows, want 1 (only the user-namespace edge)", len(rows))
	}
	if rows[0].SourceID != "projects/a" {
		test.Errorf("ScopeUser returned wrong edge: %+v", rows[0])
	}
}

func TestNeighborsByEdgeRefs_ScopeSourceMatchesOnePack(test *testing.T) {
	test.Parallel()

	store, _ := index.Open(filepath.Join(test.TempDir(), "tusk.db"))
	defer store.Close()
	seedFourNodes(test, store)
	insertEdge(test, store, "contains", "projects/a", "projects/b", "projects/a.md", "direct", nil)
	insertEdge(test, store, "contains", "notes/a", "notes/a#sec", "notes/a.md", "structural", strptr("markdown"))

	repo := index.NewEdgeRepo(store)
	rows, err := repo.NeighborsByEdgeRefs(
		[]typeref.EdgeRef{{Scope: typeref.ScopeSource, Source: "markdown", Type: "contains"}},
		[]string{"projects/a", "notes/a"},
	)
	if err != nil {
		test.Fatalf("NeighborsByEdgeRefs: %v", err)
	}
	if len(rows) != 1 {
		test.Errorf("ScopeSource returned %d rows, want 1 (only the markdown edge)", len(rows))
	}
}

// Helpers: implement inline if absent.
//
// seedFourNodes inserts four valid file/subunit rows so the FKs on
// the edge inserts above are satisfied. Use existing test fixture
// patterns from the file.
//
// insertEdge writes one row with the given (kind, source). Source is
// *string so callers can pass nil for the user namespace.
//
// strptr returns &s.
```

The helper functions (`seedFourNodes`, `insertEdge`, `strptr`) should be added to the test file if they do not already exist. They are small utilities the existing tests likely also need.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/index/... -run TestNeighborsByEdgeRefs -v`

Expected: build failure — `NeighborsByEdgeRefs` undefined.

- [ ] **Step 3: Implement `NeighborsByEdgeRefs`**

In `internal/index/edge_repo.go`, append:

```go
// NeighborsByEdgeRefs is the typeref-aware sibling of
// NeighborsByEdgeTypes. It accepts parsed EdgeRef values and builds a
// grouped OR predicate so each scope (Any, User, Source) maps to its
// correct SQL form:
//
//   ScopeAny    → type = ?
//   ScopeUser   → source IS NULL AND type = ?
//   ScopeSource → source = ?       AND type = ?
//
// Multiple refs are combined with OR. Endpoint filter (source_id or
// target_id) is the same as the legacy method.
func (repo *EdgeRepo) NeighborsByEdgeRefs(refs []typeref.EdgeRef, sourceIDs []string) ([]EdgeRow, error) {
	if len(refs) == 0 || len(sourceIDs) == 0 {
		return nil, nil
	}

	uniqueIDs := dedupStrings(sourceIDs)

	clauses := make([]string, 0, len(refs))
	args := make([]any, 0, len(refs)*2+2*len(uniqueIDs))

	for _, ref := range refs {
		switch ref.Scope {
		case typeref.ScopeAny:
			clauses = append(clauses, "type = ?")
			args = append(args, ref.Type)
		case typeref.ScopeUser:
			clauses = append(clauses, "(source IS NULL AND type = ?)")
			args = append(args, ref.Type)
		case typeref.ScopeSource:
			clauses = append(clauses, "(source = ? AND type = ?)")
			args = append(args, ref.Source, ref.Type)
		default:
			return nil, fmt.Errorf("edgeRepo: unsupported ref scope %v", ref.Scope)
		}
	}

	idPlaceholders := strings.TrimRight(strings.Repeat("?,", len(uniqueIDs)), ",")
	whereRefs := "(" + strings.Join(clauses, " OR ") + ")"

	queryText := fmt.Sprintf(`
		SELECT type, source_id, target_id, source_path
		FROM edges
		WHERE %s
		  AND (source_id IN (%s) OR target_id IN (%s))
		ORDER BY type, source_id, target_id
	`, whereRefs, idPlaceholders, idPlaceholders)

	for _, id := range uniqueIDs {
		args = append(args, id)
	}
	for _, id := range uniqueIDs {
		args = append(args, id)
	}

	rows, queryErr := repo.db.Query(queryText, args...)
	if queryErr != nil {
		return nil, fmt.Errorf("edgeRepo: NeighborsByEdgeRefs: %w", queryErr)
	}
	defer rows.Close()

	return scanEdgeRows(rows)
}
```

Use the existing `scanEdgeRows` helper if available, or follow the pattern from `NeighborsByEdgeTypes` for assembling the result slice.

Add the import:
```go
import (
    ...
    "github.com/germanamz/tusk/internal/typeref"
)
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/index/... -run TestNeighborsByEdgeRefs -v`

Expected: all PASS.

- [ ] **Step 5: Workspace suite**

Run: `go test ./...`

Expected: clean.

- [ ] **Step 6: `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 7: Commit**

```
git add internal/index/edge_repo.go internal/index/edge_repo_test.go
git commit -m "feat(index): add NeighborsByEdgeRefs accepting typeref scope-aware refs"
```

- [ ] **Step 8: Open the PR**

```
gh pr create --title "feat(index): add NeighborsByEdgeRefs accepting typeref scope-aware refs" --body "$(cat <<'EOF'
## Summary
- New `EdgeRepo.NeighborsByEdgeRefs` method accepts `[]typeref.EdgeRef`
- Builds a grouped OR predicate matching each ref's `Scope` (Any/User/Source)
- Lives alongside existing `NeighborsByEdgeTypes` for the duration of Phase 5; Task 5.5 deletes the old method
- Phase 5, Task 2 of the node/edge source-namespace plan

## Test plan
- [ ] `go test ./internal/index/... -v` passes (incl. three scope cases)
- [ ] `go test ./...` passes
- [ ] `make vet && make lint` clean
EOF
)"
```

## Done when

- Three scope-case tests pass.
- Workspace suite green.
- PR open.
