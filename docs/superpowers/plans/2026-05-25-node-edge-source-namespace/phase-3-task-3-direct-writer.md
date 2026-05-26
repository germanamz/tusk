# Phase 3 — Task 3: Frontmatter property-value edges → `(direct, NULL)`

**Phase:** 3 (Edges table reshape)
**Spec:** § *Edges table*

**Goal:** Update the path that writes edges from user frontmatter property values (e.g., `mentions: [people/dana]`) so every such row carries `kind='direct', source=NULL`. Distinct from the ref-derived path (Task 3.2): direct edges are explicit values the user wrote, derived edges come from a node-type declaration.

## Inherits From

After Task 3.2:
- `edgeRepo.Insert` accepts `(kind, source)` parameters.
- Ref-derived path passes `("derived", nil)`.
- All other call sites pass placeholder `("direct", nil)` so the build compiles. Some of those are actually correct — this task identifies which ones genuinely mean `direct` and either confirms the placeholder or fixes it.

## Files

- **Modify:** Any frontmatter-edge call site invoking `edgeRepo.Insert` (or whatever was renamed in Task 3.2). Typically these live in `internal/node/edges.go` or where `MaterializeWikilinks` writes wikilink edges.
- **Modify/add:** `internal/index/edge_repo_test.go` or sibling — assert a frontmatter-direct edge carries the correct columns.

## Steps

- [ ] **Step 1: Locate the frontmatter-direct write sites**

Search for the writer call sites:
```
grep -rn 'edgeRepo.Insert\|edgeRepo\.Insert\|Insert.*edge' internal/node internal/reindex internal/subunit
```

Categorise each call site:
- **Direct frontmatter values** — user wrote `mentions: [...]` in YAML; goes to a user-declared edge-type. → `("direct", nil)`
- **Ref-derived** — already handled in Task 3.2. Confirm it still says `("derived", nil)`.
- **Wikilink-resolved edges** — synthesized from `[[wikilink]]` mentions. Treat as `direct` (the user wrote the wikilink in body text; it's an explicit reference, not a manifest-driven derivation).
- **Structural sub-unit edges** — handled in Task 3.4. Leave the placeholder for now.

- [ ] **Step 2: Write the failing test**

In `internal/index/edge_repo_test.go` or `internal/node/edges_test.go`:

```go
func TestFrontmatterDirectEdgesCarryDirectKind(test *testing.T) {
	test.Parallel()

	// Fixture: a note with frontmatter `mentions: [people/dana]`
	// where `mentions` is a user-declared edge-type (NOT a ref
	// declaration). After reindex, the edge should carry
	// kind='direct', source=NULL.

	var (
		kind   *string
		source *string
	)
	scanErr := store.DB().QueryRow(`
		SELECT kind, source FROM edges
		WHERE type = 'mentions'
		LIMIT 1
	`).Scan(&kind, &source)
	if scanErr != nil {
		test.Fatalf("scan: %v", scanErr)
	}
	if kind == nil || *kind != "direct" {
		test.Errorf("direct edge kind = %v, want \"direct\"", kind)
	}
	if source != nil {
		test.Errorf("direct edge source = %v, want NULL", *source)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails or passes**

Run: `go test ./internal/index/... -run TestFrontmatterDirectEdgesCarryDirectKind -v`

If the placeholder from Task 3.2 already covers this path correctly, the test may PASS. That's fine; this task simply confirms the right value is in place.

If FAIL, the placeholder is wrong somewhere — fix it in Step 4.

- [ ] **Step 4: Audit and correct each frontmatter-direct call site**

For each call site from Step 1 categorized as direct or wikilink-resolved, confirm the call passes `"direct"` explicitly with a clear inline comment:

```go
// direct: user authored this from a frontmatter property value
if insertErr := edgeRepo.Insert(edge, "direct", nil); insertErr != nil {
    return fmt.Errorf("insert direct edge: %w", insertErr)
}
```

If any call site was previously using a string literal `"derived"` for a frontmatter-direct path (placeholder mis-classification), correct it.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/index/... -run TestFrontmatterDirectEdgesCarryDirectKind -v`

Expected: PASS.

- [ ] **Step 6: Run the workspace suite**

Run: `go test ./...`

Expected: clean.

- [ ] **Step 7: `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 8: Commit**

```
git add internal/
git commit -m "feat(edges): frontmatter-direct edges write kind='direct'"
```

- [ ] **Step 9: Open the PR**

```
gh pr create --title "feat(edges): frontmatter-direct edges write kind='direct'" --body "$(cat <<'EOF'
## Summary
- Frontmatter property-value edges (user-authored explicit references) write `kind='direct', source=NULL`
- Distinguishes them from ref-derived edges (Task 3.2) and structural sub-unit edges (Task 3.4)
- Wikilink-resolved edges treated as direct (user authored the wikilink text)
- Phase 3, Task 3 of the node/edge source-namespace plan

## Test plan
- [ ] `go test ./internal/index/... -v` passes
- [ ] `go test ./...` passes
- [ ] `make vet && make lint` clean
EOF
)"
```

## Done when

- Frontmatter-direct test passes.
- Workspace suite green.
- PR open.
