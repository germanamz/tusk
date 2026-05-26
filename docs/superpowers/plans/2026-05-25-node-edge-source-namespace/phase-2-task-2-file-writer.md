# Phase 2 — Task 2: File-row writer populates `kind` and `source`

**Phase:** 2 (Nodes table reshape)
**Spec:** § *Nodes table*

**Goal:** Update `internal/index/node_repo.go` so every file-row insert/upsert writes `kind='file'` and `source=NULL`. Sub-unit rows are still written by the sub-unit sync path (Task 2.3); this task only changes the file path.

## Inherits From

After Task 2.1:
- `nodes` table has nullable `kind` and `source` columns.
- Existing rows have `kind=NULL, source=NULL` (rebuild from source by `OpenOrRebuild`).
- Writers still ignore the new columns; inserts produce `NULL` for them.

## Files

- **Modify:** `internal/index/node_repo.go` — every `INSERT`/`INSERT OR REPLACE` and update statement touching `nodes` for file rows.
- **Modify or add:** `internal/index/node_repo_test.go` — assert `kind='file', source=NULL` for inserted file rows.

## Steps

- [ ] **Step 1: Locate the insert sites**

Run: `grep -n 'INSERT.*nodes\|UPDATE nodes' internal/index/node_repo.go`

Identify each statement that writes a file row. The primary one is the upsert in the file ingest path; check for additional update statements that touch `nodes`.

- [ ] **Step 2: Write the failing test**

Add to `internal/index/node_repo_test.go`:

```go
func TestUpsertFileRowSetsKindAndSource(test *testing.T) {
	test.Parallel()

	store, _ := index.Open(filepath.Join(test.TempDir(), "tusk.db"))
	defer store.Close()
	repo := index.NewNodeRepo(store)

	row := index.NodeRow{
		ID:         "notes/hello",
		Type:       "note",
		Path:       "notes/hello.md",
		Title:      "Hello",
		Properties: "{}",
		// Existing test fixtures already include the rest; copy from
		// a sibling test in this file for last_mtime/last_size/last_checksum.
	}

	if upsertErr := repo.Upsert(row); upsertErr != nil {
		test.Fatalf("Upsert: %v", upsertErr)
	}

	var (
		kind   *string
		source *string
	)
	scanErr := store.DB().QueryRow(`SELECT kind, source FROM nodes WHERE id = ?`, row.ID).Scan(&kind, &source)
	if scanErr != nil {
		test.Fatalf("scan: %v", scanErr)
	}

	if kind == nil || *kind != "file" {
		test.Errorf("kind = %v, want \"file\"", kind)
	}
	if source != nil {
		test.Errorf("source = %v, want NULL", *source)
	}
}
```

(Use the existing test file's fixture helper for any extra fields the `NodeRow` struct currently requires.)

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/index/... -run TestUpsertFileRowSetsKindAndSource -v`

Expected: FAIL — `kind` is NULL.

- [ ] **Step 4: Update the file-row insert statements**

In `internal/index/node_repo.go`, find each statement that inserts or upserts a file row and add `kind` and `source` to the column list with literal values:

**Before** (illustrative):
```sql
INSERT INTO nodes (
    id, type, path, title, properties_json,
    last_mtime, last_size, last_checksum
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
```

**After:**
```sql
INSERT INTO nodes (
    id, type, path, title, properties_json,
    last_mtime, last_size, last_checksum,
    kind, source
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'file', NULL)
```

Use literal `'file'` and `NULL` — file rows never have a different value. No new parameters are needed.

For `INSERT OR REPLACE` and `ON CONFLICT DO UPDATE` variants, include `kind` and `source` in the update clause too:

```sql
ON CONFLICT(id) DO UPDATE SET
    type            = excluded.type,
    path            = excluded.path,
    title           = excluded.title,
    properties_json = excluded.properties_json,
    last_mtime      = excluded.last_mtime,
    last_size       = excluded.last_size,
    last_checksum   = excluded.last_checksum,
    kind            = 'file',
    source          = NULL
```

Apply this to every file-row writer. Do not touch any insert that writes a sub-unit row (Task 2.3 handles those).

- [ ] **Step 5: Run the test**

Run: `go test ./internal/index/... -run TestUpsertFileRowSetsKindAndSource -v`

Expected: PASS.

- [ ] **Step 6: Run the full index suite**

Run: `go test ./internal/index/... -v`

Expected: clean.

- [ ] **Step 7: Run the workspace suite**

Run: `go test ./...`

Expected: clean.

- [ ] **Step 8: `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 9: Commit**

```
git add internal/index/node_repo.go internal/index/node_repo_test.go
git commit -m "feat(index): file-row upserts set kind='file', source=NULL"
```

- [ ] **Step 10: Open the PR**

```
gh pr create --title "feat(index): file-row upserts set kind='file', source=NULL" --body "$(cat <<'EOF'
## Summary
- Every insert/upsert path for file rows in `internal/index/node_repo.go` now writes literal `kind='file'` and `source=NULL`
- Sub-unit rows still written by sub-unit sync — covered in Task 2.3
- Phase 2, Task 2 of the node/edge source-namespace plan

## Test plan
- [ ] `go test ./internal/index/... -v` passes
- [ ] `go test ./...` passes
- [ ] `make vet && make lint` clean
EOF
)"
```

## Done when

- New writer test passes.
- Existing suite green.
- PR open.
