# Phase 2 — Task 4: Replace `parent_id IS NOT NULL` reads with `kind`

**Phase:** 2 (Nodes table reshape)
**Spec:** § *Nodes table*

**Goal:** Replace every `parent_id IS NOT NULL` (and `parent_id IS NULL`) read in the codebase with the explicit `kind = 'subunit'` (or `kind = 'file'`) form. `parent_id` stays in place for its FK role; only its use as a row-class discriminator is removed.

## Inherits From

After Task 2.3:
- Every row in `nodes` has a valid `kind` value (`'file'` or `'subunit'`).
- Schema still has nullable `kind`/`source`; CHECK constraint comes in Task 2.5.

## Files

Per the spec, the reads live in:
- `internal/index/node_repo.go`
- `internal/manifest/subunits.go`
- `internal/doctor/doctor.go`
- The partial UNIQUE index on `nodes.path` — *leave for Task 2.5* (that task rewrites the DDL atomically with the CHECK constraint).
- `internal/node/` — any helpers that read `parent_id` as a row-class signal.

Run the authoritative grep first:

```
grep -rn 'parent_id IS NOT NULL\|parent_id IS NULL' internal/
```

## Steps

- [ ] **Step 1: Enumerate the call sites**

Run the grep above. Record every match. Each will be a sub-step.

- [ ] **Step 2: Write a failing test that asserts behavioural equivalence**

Pick the most prominent reader (likely `CountSubUnits` or `CountSubUnitsByKind` in `internal/index/node_repo.go:308`). Add a test that:

1. Seeds a workspace with files and sub-units.
2. Calls the reader.
3. Asserts the count is correct.

If a test for this method already exists, copy it and add a fixture row with `parent_id` set but `kind='file'` (impossible under normal flow but constructable in a unit test) to confirm the reader uses `kind`, not `parent_id`. The test:

```go
func TestCountSubUnitsUsesKindNotParentID(test *testing.T) {
	test.Parallel()

	store, _ := index.Open(filepath.Join(test.TempDir(), "tusk.db"))
	defer store.Close()

	// Insert a row with parent_id set but kind='file' — a bogus shape
	// that exercises the discriminator difference. The CHECK constraint
	// in Task 2.5 will reject this, but it is currently permitted.
	_, execErr := store.DB().Exec(`
		INSERT INTO nodes (id, type, path, properties_json,
		                   last_mtime, last_size, last_checksum,
		                   parent_id, kind, source)
		VALUES ('weird-row', 'note', 'weird.md', '{}',
		        0, 0, '', 'some-parent', 'file', NULL)
	`)
	if execErr != nil {
		test.Fatalf("seed: %v", execErr)
	}

	got, countErr := index.NewNodeRepo(store).CountSubUnits()
	if countErr != nil {
		test.Fatalf("CountSubUnits: %v", countErr)
	}
	if got != 0 {
		test.Errorf("CountSubUnits = %d, want 0 (row has kind='file')", got)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/index/... -run TestCountSubUnitsUsesKindNotParentID -v`

Expected: FAIL — current implementation uses `parent_id IS NOT NULL`, so it counts the bogus row.

- [ ] **Step 4: Replace each occurrence**

For each match from the grep, apply the corresponding swap:

| Read | Replacement |
|---|---|
| `WHERE parent_id IS NOT NULL` | `WHERE kind = 'subunit'` |
| `WHERE parent_id IS NULL` | `WHERE kind = 'file'` |
| `AND parent_id IS NOT NULL` | `AND kind = 'subunit'` |
| `AND parent_id IS NULL` | `AND kind = 'file'` |

Touch only the predicates that use `parent_id` as a row-class discriminator. Statements that read `parent_id` for its actual value (e.g., `SELECT parent_id FROM nodes WHERE id = ?`) stay unchanged.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/index/... -run TestCountSubUnitsUsesKindNotParentID -v`

Expected: PASS.

- [ ] **Step 6: Run the full workspace suite**

Run: `go test ./...`

Expected: clean. Doctor, subunits manifest validator, and node helpers should all still pass.

- [ ] **Step 7: Re-grep to confirm nothing remains**

Run: `grep -rn 'parent_id IS NOT NULL\|parent_id IS NULL' internal/`

Expected output: only the partial UNIQUE index on `nodes.path` in `internal/index/index.go` (Task 2.5 rewrites that as part of the atomic DDL update).

- [ ] **Step 8: `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 9: Commit**

```
git add internal/
git commit -m "refactor: read kind='subunit' instead of parent_id IS NOT NULL"
```

- [ ] **Step 10: Open the PR**

```
gh pr create --title "refactor: read kind='subunit' instead of parent_id IS NOT NULL" --body "$(cat <<'EOF'
## Summary
- Replaces every `parent_id IS NOT NULL`/`IS NULL` row-class predicate with `kind = 'subunit'`/`'file'`
- `parent_id` stays as the FK pointer for sub-units; only its use as a discriminator is removed
- One read remains (`internal/index/index.go` partial UNIQUE on `nodes.path`) — Task 2.5 rewrites it atomically with the CHECK constraint and index swap
- Phase 2, Task 4 of the node/edge source-namespace plan

## Test plan
- [ ] `go test ./...` passes
- [ ] `grep -rn 'parent_id IS NOT NULL\|parent_id IS NULL' internal/` shows only the Task 2.5 line
- [ ] `make vet && make lint` clean
EOF
)"
```

## Done when

- New discriminator test passes.
- Workspace suite green.
- Only the deferred partial-UNIQUE site remains.
- PR open.
