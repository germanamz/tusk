# Phase 5 — Task 5: MCP boundary parses qualified names; delete `NeighborsByEdgeTypes`

**Phase:** 5 (Reference-resolution grammar)
**Spec:** § *Naming conventions*

**Goal:** Update MCP tool argument handlers so the remaining direct-input type-name fields (notably `tusk_edge_list`'s `type` field) parse via `typeref.Parse` and feed scope-aware queries. Delete `NeighborsByEdgeTypes` (replaced by `NeighborsByEdgeRefs` in Task 5.2) — the bridge code's removal target.

## What 5.5 does **not** need to do

After Tasks 5.3, 5.4, and 5.4a, several MCP surfaces already inherit typeref behavior transparently:

- `tusk_context` / `tusk_query`'s `graph_edge_types` argument flows into `manifest.GraphExpansion.EdgeTypes`, which `internal/query/query_service.go` already passes through `typeref.ParseMany` (landed in Task 5.3).
- `tusk_node_list`'s `type` argument is wrapped as `fmt.Sprintf("type=%s", typeFilter)` and handed to `query.ListRun`. After Task 5.4 the filter compiler parses that value as a typeref, so qualified node-type strings work end-to-end with no MCP-side change.
- After Task 5.4a, the filter grammar also accepts qualified edge-type identifiers, so any MCP path that hands a user-supplied filter string straight to the parser inherits the new grammar automatically.

## Inherits From

After Task 5.4a:
- `typeref.Parse` / `ParseMany` available.
- Filter compiler and parser accept the full `<source>:<type>` grammar for both node-type literals and edge-type identifiers.
- `NeighborsByEdgeRefs` is the canonical neighbor lookup.
- `NeighborsByEdgeTypes` still exists but has no production callers outside of `tusk_edge_list`'s `index.EdgeListRun` path, which this task replaces.

## Files

- **Modify:** `internal/mcp/runtime.go` — tool argument handlers.
- **Modify:** `internal/index/edge_repo.go` — delete `NeighborsByEdgeTypes` and its test.
- **Modify/add:** `internal/mcp/runtime_test.go` — MCP-side parsing test.

## Steps

- [ ] **Step 1: Locate MCP tool arguments that take type names directly**

Run:
```
grep -rn 'argString.*"type"\|EdgeListRequest\|NeighborsByEdgeTypes' internal/mcp internal/index
```

The remaining direct-input surfaces after the work in 5.3–5.4a are:

- `tusk_edge_list` (`internal/mcp/tools.go:496-512`) — passes `type` straight into `index.EdgeListRun`'s `EdgeListRequest.Type` which is used as a literal SQL `type = ?` filter.
- Any remaining caller of `NeighborsByEdgeTypes` (verify via grep — should be limited to tests and possibly one production site).

`tusk_context` / `tusk_query`'s `graph_edge_types` and `tusk_node_list`'s `type` are *not* in scope — they inherit typeref behavior from Tasks 5.3 and 5.4 respectively (see "What 5.5 does not need to do" above).

- [ ] **Step 2: Write the failing test**

In `internal/mcp/runtime_test.go`:

```go
func TestMCPContextAcceptsQualifiedEdgeType(test *testing.T) {
	test.Parallel()

	// Fixture: a workspace with both a markdown contains edge and
	// a user-direct contains edge. Call tusk_context with
	// edge-types=["markdown:contains"]. Assert only the markdown
	// edge is followed.

	// ... boilerplate identical to the integration test in
	// runtime_test.go (Phase 1 task 5) ...

	resp, err := runtime.InvokeTool(ctx, "tusk_context", map[string]any{
		"seeds":      []string{"notes/standup"},
		"edge-types": []string{"markdown:contains"},
	})
	if err != nil {
		test.Fatalf("InvokeTool: %v", err)
	}

	// Only sub-unit nodes should appear in the expansion (markdown
	// contains points file → subunit). projects/* nodes should not.
	for _, node := range resp.Nodes {
		if !strings.Contains(node.ID, "#") {
			test.Errorf("non-subunit node %q leaked through markdown:contains filter", node.ID)
		}
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/mcp/... -run TestMCPContextAcceptsQualifiedEdgeType -v`

Expected: FAIL — MCP currently treats `"markdown:contains"` as a literal type name.

- [ ] **Step 4: Update MCP argument handlers**

In `internal/mcp/runtime.go`, find each tool handler that consumes type-name arguments. For each, parse via `typeref.ParseMany`:

```go
edgeRefs, parseErr := typeref.ParseMany(args.EdgeTypes)
if parseErr != nil {
    return nil, fmt.Errorf("tusk_context: parse edge-types: %w", parseErr)
}

walker := graphexpand.Walker{
    Edges:    edgeRepo,
    Hops:     args.Hops,
    EdgeRefs: edgeRefs,
}
```

(Reuse the same pattern as the query layer; this is the boundary that turns wire strings into refs.)

For single-value arguments (e.g., one type filter), use `typeref.Parse` directly.

- [ ] **Step 5: Delete `NeighborsByEdgeTypes`**

In `internal/index/edge_repo.go`, locate `NeighborsByEdgeTypes` and delete it along with its tests. Confirm via grep that nothing in the workspace still calls it:

```
grep -rn 'NeighborsByEdgeTypes' .
```

Expected: no matches.

If any test file still references it, those tests have either been converted to `NeighborsByEdgeRefs` or are now redundant. Make a per-test judgment call.

- [ ] **Step 6: Run the new test**

Run: `go test ./internal/mcp/... -run TestMCPContextAcceptsQualifiedEdgeType -v`

Expected: PASS.

- [ ] **Step 7: Run the workspace suite**

Run: `go test ./...`

Expected: clean.

- [ ] **Step 8: `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 9: Commit**

```
git add internal/mcp internal/index/edge_repo.go internal/index/edge_repo_test.go
git commit -m "feat(mcp): parse qualified type references; remove NeighborsByEdgeTypes"
```

- [ ] **Step 10: Open the PR**

```
gh pr create --title "feat(mcp): parse qualified type references; remove NeighborsByEdgeTypes" --body "$(cat <<'EOF'
## Summary
- MCP tool argument handlers parse type-name inputs via `typeref.Parse`/`typeref.ParseMany`
- Deletes `EdgeRepo.NeighborsByEdgeTypes` — the bridge code introduced in Task 5.2 has fulfilled its purpose
- Phase 5, Task 5 of the node/edge source-namespace plan

## Test plan
- [ ] `go test ./internal/mcp/... -v` passes (incl. qualified-name MCP test)
- [ ] `go test ./...` passes
- [ ] `grep -rn 'NeighborsByEdgeTypes' .` returns no matches
- [ ] `make vet && make lint` clean
EOF
)"
```

## Done when

- MCP qualified-name test passes.
- `NeighborsByEdgeTypes` is gone.
- Workspace suite green.
- PR open.
