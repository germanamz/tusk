# Phase 5 — Task 4: Query layer parses incoming type names

**Phase:** 5 (Reference-resolution grammar)
**Spec:** § *Naming conventions*

**Goal:** Every place in `internal/query` that accepts a type name as a string argument now parses it via `typeref.Parse` and stores it as a `Ref`. Bare names continue to behave identically (`ScopeAny`); qualified forms get scoped matching.

## Inherits From

After Task 5.3:
- Graph-expansion walker uses `EdgeRefs`.
- `typeref.Parse`/`ParseMany` available.

## Files

- **Modify:** `internal/query/query_service.go`
- **Modify:** `internal/query/matched_units.go`
- **Modify:** `internal/query/semantic_subunits.go`
- **Modify:** `internal/query/list_service.go`
- **Modify:** any test file that constructs a query with a `Type` field.

## Steps

- [ ] **Step 1: Locate the type-name acceptance points**

Run:
```
grep -rn 'Type\s*string\|filter.*type\|EdgeTypes\|NodeType' internal/query
```

For each struct field of type `string` that names a node-type or edge-type, decide whether to:

(a) Replace with `typeref.Ref` — when the field is genuinely user-input.
(b) Leave as `string` and parse at use site — when the field is internal plumbing already constrained to a single source.

Default to (a) for user-input fields; (b) for internal-only.

- [ ] **Step 2: Write the failing test**

In `internal/query/query_service_test.go`:

```go
func TestQueryServiceRespectsQualifiedTypeReference(test *testing.T) {
	test.Parallel()

	// Fixture: seed two `section` rows — one (file, NULL, section)
	// user-declared, one (subunit, markdown, section) pack-derived.
	// Query for `:section` (user namespace only) and assert only one
	// row returns.

	got, err := service.Query(context.Background(), query.Request{
		NodeType: ":section",
	})
	if err != nil {
		test.Fatalf("Query: %v", err)
	}
	if len(got.Nodes) != 1 {
		test.Errorf("got %d nodes, want 1 (only user-namespace section)", len(got.Nodes))
	}
	if got.Nodes[0].Kind != "file" {
		test.Errorf("got kind %q, want \"file\"", got.Nodes[0].Kind)
	}
}
```

(Adapt to the real `query.Request` / `query.Result` types as exposed by the codebase.)

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/query/... -run TestQueryServiceRespectsQualifiedTypeReference -v`

Expected: FAIL — current implementation treats `:section` as a literal type name and returns nothing.

- [ ] **Step 4: Update the query service**

In `internal/query/query_service.go`, where the request's node-type filter is applied:

**Before:**
```go
if req.NodeType != "" {
    where = append(where, "type = ?")
    args = append(args, req.NodeType)
}
```

**After:**
```go
if req.NodeType != "" {
    ref, parseErr := typeref.Parse(req.NodeType)
    if parseErr != nil {
        return nil, fmt.Errorf("query: parse NodeType: %w", parseErr)
    }
    switch ref.Scope {
    case typeref.ScopeAny:
        where = append(where, "type = ?")
        args = append(args, ref.Type)
    case typeref.ScopeUser:
        where = append(where, "source IS NULL AND type = ?")
        args = append(args, ref.Type)
    case typeref.ScopeSource:
        where = append(where, "source = ? AND type = ?")
        args = append(args, ref.Source, ref.Type)
    }
}
```

(If the codebase already has a helper for building source-aware predicates from a `Ref`, use it. Otherwise inline the switch in the few places where it occurs.)

Apply the same pattern to:
- `matched_units.go` — any type filter
- `semantic_subunits.go` — any type filter
- `list_service.go` — any type filter

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/query/... -run TestQueryServiceRespectsQualifiedTypeReference -v`

Expected: PASS.

- [ ] **Step 6: Run the workspace suite**

Run: `go test ./...`

Expected: clean.

- [ ] **Step 7: `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 8: Commit**

```
git add internal/query
git commit -m "feat(query): parse type filters as typeref.Ref"
```

- [ ] **Step 9: Open the PR**

```
gh pr create --title "feat(query): parse type filters as typeref.Ref" --body "$(cat <<'EOF'
## Summary
- Every type-filter input in `internal/query` is parsed via `typeref.Parse` and translated to scope-aware SQL predicates
- Bare names continue to match union; qualified names (`:type`, `source:type`) scope as documented
- Phase 5, Task 4 of the node/edge source-namespace plan

## Test plan
- [ ] `go test ./internal/query/... -v` passes (incl. qualified-type test)
- [ ] `go test ./...` passes
- [ ] `make vet && make lint` clean
EOF
)"
```

## Done when

- Qualified-type query test passes.
- Workspace suite green.
- PR open.
