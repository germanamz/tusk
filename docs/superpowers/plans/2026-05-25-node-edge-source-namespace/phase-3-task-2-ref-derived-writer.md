# Phase 3 — Task 2: Ref-derived edge writer → `(derived, NULL)`

**Phase:** 3 (Edges table reshape)
**Spec:** § *Edges table*

**Goal:** Update the path that writes edges synthesized from a node-type's `references` property declaration so every such row carries `kind='derived', source=NULL`.

The synthesis machinery lives in `internal/manifest/loader.go` (`synthesizeRefEdgeTypes`); the actual writes happen wherever the resolver inserts the edge rows produced by that mechanism. The implementer must trace from `synthesizeRefEdgeTypes` to the eventual `INSERT INTO edges` call site.

## Inherits From

After Task 3.1:
- `edges.kind` and `edges.source` exist as nullable columns.
- No writer populates them yet.

## Files

- **Modify:** `internal/index/edge_repo.go` — `InsertEdge`-style methods, particularly any helper used by the ref-derived path. Add `kind`/`source` parameters or hard-code the values per call site.
- **Modify:** the call site invoking those writers from the ref-derived path. Likely in `internal/node/edges.go` or `internal/reindex/reindex.go` where `ResolveEdges` runs.
- **Modify/add:** `internal/index/edge_repo_test.go` or a sibling test asserting derived edges carry `kind='derived', source=NULL`.

## Steps

- [ ] **Step 1: Locate the ref-derived write path**

Run:
```
grep -rn 'synthesizeRefEdgeTypes\|ref.*edge\|references' internal/manifest internal/node internal/reindex | head -40
```

Identify which edge-writing function consumes the synthesized edge types. Typically `node.ResolveEdges` walks frontmatter `references` properties and emits edges.

- [ ] **Step 2: Write the failing test**

In `internal/index/edge_repo_test.go` (or a sibling), add:

```go
func TestRefDerivedEdgesCarryDerivedKind(test *testing.T) {
	test.Parallel()

	// Use a fixture workspace where a node-type declares
	// `references: [tag]` and a note has `tags: [retro]`.
	// After reindex, the synthesized "tagged-with" edge should
	// have kind='derived', source=NULL.

	// Use the existing test fixture pattern from reindex_test.go for
	// boilerplate, then assert:
	var (
		kind   *string
		source *string
	)
	scanErr := store.DB().QueryRow(`
		SELECT kind, source FROM edges
		WHERE type = 'tagged-with'
		LIMIT 1
	`).Scan(&kind, &source)
	if scanErr != nil {
		test.Fatalf("scan: %v", scanErr)
	}
	if kind == nil || *kind != "derived" {
		test.Errorf("derived edge kind = %v, want \"derived\"", kind)
	}
	if source != nil {
		test.Errorf("derived edge source = %v, want NULL", *source)
	}
}
```

The implementer must wire up the fixture using whatever helper already exists in the test file (look for `seedFileRow`, `seedManifest`, etc.).

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/index/... -run TestRefDerivedEdgesCarryDerivedKind -v`

Expected: FAIL — kind is NULL.

- [ ] **Step 4: Extend the edge writer signature**

In `internal/index/edge_repo.go`, locate the `InsertEdge` or `InsertEdges` method (or whatever the codebase exposes). Either:

(a) Add `kind, source` as parameters and update SQL accordingly, OR
(b) Add a dedicated `InsertDerivedEdge` method whose SQL hard-codes `kind='derived', source=NULL`.

Pick (a) for flexibility: the same method serves Tasks 3.3 and 3.4.

```go
func (repo *EdgeRepo) Insert(edge EdgeRow, kind string, source *string) error {
	_, execErr := repo.db.Exec(`
		INSERT OR IGNORE INTO edges (type, source_id, target_id, source_path, kind, source)
		VALUES (?, ?, ?, ?, ?, ?)
	`, edge.Type, edge.SourceID, edge.TargetID, edge.SourcePath, kind, source)
	return execErr
}
```

(If the existing signature is materially different — e.g., batched inserts, prepared statement reuse — preserve that shape and just thread `kind`/`source` through.)

- [ ] **Step 5: Update the ref-derived call site**

In the file located in Step 1, call the writer with `kind="derived", source=nil`:

```go
if insertErr := edgeRepo.Insert(edge, "derived", nil); insertErr != nil {
    return fmt.Errorf("insert derived edge: %w", insertErr)
}
```

Only the ref-derived path uses these constants in this task; the frontmatter-direct path (Task 3.3) and the structural path (Task 3.4) will pass their own values.

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/index/... -run TestRefDerivedEdgesCarryDerivedKind -v`

Expected: PASS.

- [ ] **Step 7: Run the workspace suite**

Run: `go test ./...`

Expected: many tests will fail temporarily because the new method signature has more parameters than the old. Update every call site to pass `"direct"` and `nil` as a placeholder so the build is green; Tasks 3.3 and 3.4 will refine those call sites with their correct kind values.

Run again: `go test ./...`

Expected: clean.

- [ ] **Step 8: `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 9: Commit**

```
git add internal/
git commit -m "feat(edges): ref-derived edges write kind='derived', source=NULL"
```

- [ ] **Step 10: Open the PR**

```
gh pr create --title "feat(edges): ref-derived edges write kind='derived', source=NULL" --body "$(cat <<'EOF'
## Summary
- Extends edge writer signature with `kind` and `source` parameters
- Ref-derived edges (synthesized from `references` declarations) write `("derived", NULL)`
- All other call sites pass placeholder `"direct"`/`nil` to keep the build green; Tasks 3.3 and 3.4 refine them
- Phase 3, Task 2 of the node/edge source-namespace plan

## Test plan
- [ ] `go test ./internal/index/... -v` passes (incl. new derived-edge test)
- [ ] `go test ./...` passes
- [ ] `make vet && make lint` clean
EOF
)"
```

## Done when

- Derived-edge test passes.
- Workspace suite green.
- PR open.
