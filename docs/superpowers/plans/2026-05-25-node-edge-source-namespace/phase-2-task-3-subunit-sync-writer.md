# Phase 2 — Task 3: Sub-unit sync writes `kind='subunit', source='markdown'`

**Phase:** 2 (Nodes table reshape)
**Spec:** § *Nodes table*

**Goal:** Update the sub-unit sync pipeline so every sub-unit row insert writes `kind='subunit'` and `source='markdown'`. After this task, both row classes carry valid `kind`/`source` values.

## Inherits From

After Task 2.2:
- File-row inserts write `kind='file', source=NULL`.
- Sub-unit-row inserts still leave `kind` and `source` NULL.
- The `nodes` schema accepts both because both columns are still nullable.

## Files

- **Modify:** `internal/subunit/sync.go` (the function that inserts sub-unit rows; likely batched inside `Sync` or a helper called by it).
- **Modify:** `internal/index/node_repo.go` if sub-unit inserts go through a shared `Upsert` helper — in that case, route them through a new sibling method (`UpsertSubUnit`) that hard-codes the sub-unit values, or expand the existing signature.
- **Modify/add:** `internal/subunit/sync_test.go` — assert inserted sub-unit rows have the correct columns.

## Steps

- [ ] **Step 1: Locate the insert path**

Run: `grep -n 'INSERT.*nodes' internal/subunit/sync.go internal/index/node_repo.go`

Determine whether sub-unit rows are written via a dedicated repo method or via the same upsert as file rows. The spec notes the sub-unit sync pipeline at `internal/subunit/sync.go:207` (re-`contains` rewrite); that's edges, but the row insert is nearby.

- [ ] **Step 2: Write the failing test**

Extend `internal/subunit/sync_test.go` with:

```go
func TestSyncWritesKindAndSourceForSubUnits(test *testing.T) {
	test.Parallel()

	// Use the existing sync test fixture pattern in this file.
	// (Refer to TestSyncInsertsUnits or similar to copy the setup.)
	// After Sync runs:
	rows, queryErr := store.DB().Query(`SELECT kind, source FROM nodes WHERE parent_id IS NOT NULL`)
	if queryErr != nil {
		test.Fatalf("query subunits: %v", queryErr)
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
		if kind == nil || *kind != "subunit" {
			test.Errorf("subunit kind = %v, want \"subunit\"", kind)
		}
		if source == nil || *source != "markdown" {
			test.Errorf("subunit source = %v, want \"markdown\"", source)
		}
	}
	if iterErr := rows.Err(); iterErr != nil {
		test.Fatalf("iter: %v", iterErr)
	}
	if seen == 0 {
		test.Fatal("expected at least one sub-unit row")
	}
}
```

Note: this test uses `parent_id IS NOT NULL` as the discriminator because Task 2.4 (the read-side switch) has not run yet. After Task 2.4 lands, equivalent assertions can use `kind='subunit'`.

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/subunit/... -run TestSyncWritesKindAndSourceForSubUnits -v`

Expected: FAIL — sub-unit rows have NULL kind/source.

- [ ] **Step 4: Update the sub-unit insert path**

If sub-unit rows are inserted via raw SQL in `internal/subunit/sync.go`, locate the `INSERT INTO nodes (...)` statement and add `kind='subunit', source='markdown'`:

```sql
INSERT INTO nodes (
    id, type, path, title, properties_json,
    last_mtime, last_size, last_checksum,
    parent_id, ordinal, embed_payload,
    kind, source
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'subunit', 'markdown')
```

If sub-unit rows go through a repo method like `UpsertSubUnit`, change that method's SQL. If they go through the shared `Upsert`, add a dedicated `UpsertSubUnit` and update `internal/subunit/sync.go` to call it instead — keeping the file-row writer separate from the sub-unit writer makes the two code paths easier to reason about.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/subunit/... -run TestSyncWritesKindAndSourceForSubUnits -v`

Expected: PASS.

- [ ] **Step 6: Run the workspace suite**

Run: `go test ./...`

Expected: clean. Sub-unit-related tests (`internal/subunit/...`, `internal/reindex/...` integration test) should all still pass.

- [ ] **Step 7: `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 8: Commit**

```
git add internal/subunit internal/index/node_repo.go
git commit -m "feat(subunit): sync writes kind='subunit', source='markdown'"
```

- [ ] **Step 9: Open the PR**

```
gh pr create --title "feat(subunit): sync writes kind='subunit', source='markdown'" --body "$(cat <<'EOF'
## Summary
- Sub-unit row inserts now populate `kind='subunit'` and `source='markdown'`
- Both file rows (Task 2.2) and sub-unit rows (this task) now carry valid kind/source values; Task 2.5 will tighten to NOT NULL + CHECK
- Phase 2, Task 3 of the node/edge source-namespace plan

## Test plan
- [ ] `go test ./internal/subunit/... -v` passes
- [ ] `go test ./...` passes (incl. reindex integration test)
- [ ] `make vet && make lint` clean
EOF
)"
```

## Done when

- Sub-unit writer test passes.
- Workspace suite green.
- PR open.
