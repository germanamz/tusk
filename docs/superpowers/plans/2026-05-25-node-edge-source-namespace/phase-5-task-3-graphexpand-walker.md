# Phase 5 — Task 3: `graphexpand` walker uses `EdgeRefs`

**Phase:** 5 (Reference-resolution grammar)
**Spec:** § *Reference resolution in code*

**Goal:** Update `internal/graphexpand` so `Walker.EdgeTypes []string` becomes `Walker.EdgeRefs []typeref.EdgeRef`. The walker calls `NeighborsByEdgeRefs` instead of `NeighborsByEdgeTypes`. Manifest-config consumers parse their input via `typeref.ParseMany` at the boundary.

## Inherits From

After Task 5.2:
- `NeighborsByEdgeRefs` exists in `internal/index/edge_repo.go`.
- `Walker.EdgeTypes []string` and `walker.Edges.NeighborsByEdgeTypes(...)` still in use.

## Files

- **Modify:** `internal/graphexpand/walk.go`
- **Modify:** `internal/graphexpand/walk_test.go`
- **Modify:** any caller constructing a `Walker` (notably `internal/query/expand.go`)

## Steps

- [x] **Step 1: Locate the call sites**

Run:
```
grep -rn 'graphexpand.Walker\|EdgeTypes\b' internal/
```

Identify every place a `Walker` is constructed and where its `EdgeTypes` field is populated. The query layer is the primary consumer; the manifest's `[query.graph-expansion]` config feeds into it.

- [x] **Step 2: Write the failing test**

In `internal/graphexpand/walk_test.go`:

```go
func TestWalkerUsesEdgeRefs(test *testing.T) {
	test.Parallel()

	store, _ := index.Open(filepath.Join(test.TempDir(), "tusk.db"))
	defer store.Close()
	// Seed two contains edges: one structural (markdown), one user-direct.
	seedFourNodes(test, store)
	insertEdge(test, store, "contains", "notes/a", "notes/a#sec", "notes/a.md", "structural", strptr("markdown"))
	insertEdge(test, store, "contains", "projects/a", "projects/b", "projects/a.md", "direct", nil)

	walker := graphexpand.Walker{
		Edges:    index.NewEdgeRepo(store),
		Hops:     1,
		EdgeRefs: []typeref.EdgeRef{{Scope: typeref.ScopeSource, Source: "markdown", Type: "contains"}},
	}

	distances := map[string]int{"notes/a": 0, "projects/a": 0}
	result, err := walker.Expand(distances)
	if err != nil {
		test.Fatalf("Expand: %v", err)
	}

	// The user-direct contains edge should NOT contribute because the
	// ref is ScopeSource("markdown"). Only notes/a#sec should be added
	// to the result.
	if _, ok := result["notes/a#sec"]; !ok {
		test.Error("expected notes/a#sec in expansion")
	}
	if _, ok := result["projects/b"]; ok {
		test.Error("did not expect projects/b — ref scoped to source='markdown'")
	}
}
```

- [x] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/graphexpand/... -run TestWalkerUsesEdgeRefs -v`

Expected: build failure — `EdgeRefs` undefined.

- [x] **Step 4: Update the `Walker` struct and `Expand`**

In `internal/graphexpand/walk.go`:

```go
type Walker struct {
	Edges    EdgesReader
	Hops     int
	EdgeRefs []typeref.EdgeRef // replaces EdgeTypes []string
}
```

Update `Expand` (and any internal helper) to call `NeighborsByEdgeRefs(walker.EdgeRefs, frontier)` instead of `NeighborsByEdgeTypes`. Update the `EdgesReader` interface (if present) to expose `NeighborsByEdgeRefs`.

Remove the `EdgeTypes []string` field entirely — bridge-code retention is not needed because the only callers are within tusk and the migration is atomic across this PR.

- [x] **Step 5: Update callers**

For every site found in Step 1 that constructs a `Walker`, replace:

```go
walker := graphexpand.Walker{
    Edges:     edgeRepo,
    Hops:      cfg.Hops,
    EdgeTypes: cfg.EdgeTypes, // []string
}
```

with:

```go
edgeRefs, parseErr := typeref.ParseMany(cfg.EdgeTypes)
if parseErr != nil {
    return nil, fmt.Errorf("graphexpand: parse edge types: %w", parseErr)
}
walker := graphexpand.Walker{
    Edges:    edgeRepo,
    Hops:     cfg.Hops,
    EdgeRefs: edgeRefs,
}
```

(The `cfg.EdgeTypes` source — manifest config — remains a `[]string` for now. Manifest grammar parsing belongs to query/manifest layers, not to the walker.)

- [x] **Step 6: Run the new test**

Run: `go test ./internal/graphexpand/... -run TestWalkerUsesEdgeRefs -v`

Expected: PASS.

- [x] **Step 7: Run the workspace suite**

Run: `go test ./...`

Expected: clean. Any test directly constructing a `Walker` with the old field name needs the same migration.

- [x] **Step 8: `make vet` and `make lint`**

Expected: clean.

- [x] **Step 9: Commit**

```
git add internal/graphexpand internal/query
git commit -m "refactor(graphexpand): walker takes EdgeRefs instead of EdgeTypes strings"
```

- [x] **Step 10: Open the PR**

```
gh pr create --title "refactor(graphexpand): walker takes EdgeRefs instead of EdgeTypes strings" --body "$(cat <<'EOF'
## Summary
- `Walker.EdgeTypes []string` → `Walker.EdgeRefs []typeref.EdgeRef`
- Walker calls `NeighborsByEdgeRefs` instead of `NeighborsByEdgeTypes`
- Callers (`internal/query/expand.go` and friends) parse incoming `[]string` via `typeref.ParseMany` at the boundary
- Phase 5, Task 3 of the node/edge source-namespace plan

## Test plan
- [x] `go test ./internal/graphexpand/... -v` passes (incl. new scope test)
- [x] `go test ./...` passes
- [x] `make vet && make lint` clean
EOF
)"
```

## Done when

- Scope test passes.
- Workspace suite green.
- PR open.
