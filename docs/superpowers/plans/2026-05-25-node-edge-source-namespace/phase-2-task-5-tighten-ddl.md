# Phase 2 — Task 5b: Tighten nodes DDL (NOT NULL, CHECK, index swap)

> **Was Task 5.** During execution we discovered the original Task 5 cannot land in a single PR without breaking the lefthook pre-commit test gate (~15 tests across four packages call the wrong writer for the row shape they construct, and one legacy-migration test bootstraps a pre-`kind` schema that the new `CREATE INDEX` cannot run against). The prep cleanup lives in `phase-2-task-5a-align-writers-and-tests.md` and **must** ship first. This file describes the residual DDL change only.

**Phase:** 2 (Nodes table reshape)
**Spec:** § *Nodes table*, § *Index schema bump & rebuild*

**Goal:** Promote `kind` to `NOT NULL`, add the CHECK constraint guaranteeing `(kind, source, parent_id)` agreement, replace `nodes_type_idx` with the composite `nodes_kind_type_idx`, and rewrite the partial UNIQUE index on `nodes.path` to predicate on `kind='file'`. Bump `SchemaVersion` so existing dev indexes are rebuilt via `OpenOrRebuild`.

## Inherits From

After Task 2.5a (which itself inherits from 2.4):
- Every `Upsert` call site supplies a file-shaped row (`ParentID` NULL).
- Every `BulkUpsert` call site supplies sub-unit-shaped rows (`ParentID` non-NULL).
- No direct `INSERT INTO nodes` omits `kind`/`source`.
- `TestOpen_P2MigrationFromLegacyDB` is gone, so we are free to add `CREATE INDEX` statements that reference the `kind` column without breaking the bootstrap on legacy DBs (legacy DBs are now handled exclusively by `OpenOrRebuild`'s drop-and-reindex path).

After Task 2.4:
- Both writers (file and sub-unit) populate `kind` and `source` correctly.
- Every read uses `kind` as the row-class discriminator.
- The only remaining `parent_id IS NULL` predicate is the partial UNIQUE index on `nodes.path` (added by `migrateRelaxNodesPathUnique`'s fallback branch).
- Schema constants still have `kind`/`source` as nullable; `nodes_type_idx` and the old partial UNIQUE on `path` are still in place.

## Legacy-DB upgrade contract

Per spec § *What the schema bump removes*:

> Any pre-existing index lacks the new `schema_version` key, so `Open` returns `ErrSchemaIncompatible` and the file is dropped before the old migration path could run.

This is the contract this task relies on. **Concrete implication:** the bootstrap statements in the `schema` constant must succeed against any DB that already carries `schema_version` set to the prior value (`2026-05-25-nodes-kind-source-nullable`, from Task 2.1) — those DBs have `kind` and `source` as columns, so `CREATE INDEX … ON nodes(kind, type)` will resolve. The only DBs whose bootstrap *fails* are pre-Task-2.1 indexes that never had the `kind` column at all, and those are explicitly outside the supported upgrade window (users on such indexes have to delete the file by hand — release notes call this out).

We do **not** add a `migrateAddKindSourceColumns` migration. The rebuild model is the upgrade path.

## Files

- **Modify:** `internal/index/index.go` (schema constant + `migrateRelaxNodesPathUnique` fallback)
- **Modify:** `internal/index/schema_version.go` (bump constant)
- **Add:** `internal/index/nodes_check_constraint_test.go`

## Steps

- [ ] **Step 1: Write the failing schema-shape tests**

Create `internal/index/nodes_check_constraint_test.go`:

```go
package index_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestNodesKindIsNotNullAndHasCheckConstraint(test *testing.T) {
	test.Parallel()

	dbPath := filepath.Join(test.TempDir(), "tusk.db")
	store, _ := index.Open(dbPath)
	defer store.Close()

	var sqlText string
	scanErr := store.DB().QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='nodes'`).Scan(&sqlText)
	if scanErr != nil {
		test.Fatalf("read nodes DDL: %v", scanErr)
	}

	if !strings.Contains(sqlText, "kind") || !strings.Contains(sqlText, "NOT NULL") {
		test.Errorf("nodes DDL missing NOT NULL kind:\n%s", sqlText)
	}
	if !strings.Contains(sqlText, "CHECK") {
		test.Errorf("nodes DDL missing CHECK constraint:\n%s", sqlText)
	}
}

func TestNodesCheckRejectsBadKindShape(test *testing.T) {
	test.Parallel()

	dbPath := filepath.Join(test.TempDir(), "tusk.db")
	store, _ := index.Open(dbPath)
	defer store.Close()

	// file rows with source != NULL must be rejected
	_, execErr := store.DB().Exec(`
		INSERT INTO nodes (id, type, path, properties_json,
		                   last_mtime, last_size, last_checksum,
		                   kind, source)
		VALUES ('bad-file', 'note', 'bad.md', '{}', 0, 0, '',
		        'file', 'markdown')
	`)
	if execErr == nil {
		test.Error("CHECK should reject file row with non-NULL source")
	}

	// subunit rows with NULL source must be rejected
	_, execErr = store.DB().Exec(`
		INSERT INTO nodes (id, type, path, properties_json,
		                   last_mtime, last_size, last_checksum,
		                   parent_id, kind, source)
		VALUES ('bad-sub', 'section', 'bad.md', '{}', 0, 0, '',
		        'some-parent', 'subunit', NULL)
	`)
	if execErr == nil {
		test.Error("CHECK should reject subunit row with NULL source")
	}
}

func TestNodesPartialUniqueIsKindFile(test *testing.T) {
	test.Parallel()

	dbPath := filepath.Join(test.TempDir(), "tusk.db")
	store, _ := index.Open(dbPath)
	defer store.Close()

	var sqlText string
	scanErr := store.DB().QueryRow(`
		SELECT sql FROM sqlite_master
		WHERE type='index' AND tbl_name='nodes' AND sql LIKE '%path%'
	`).Scan(&sqlText)
	if scanErr != nil {
		test.Fatalf("read path index DDL: %v", scanErr)
	}

	if !strings.Contains(sqlText, "kind = 'file'") && !strings.Contains(sqlText, "kind='file'") {
		test.Errorf("partial UNIQUE index does not predicate on kind='file':\n%s", sqlText)
	}
}

func TestNodesHasKindTypeCompositeIndex(test *testing.T) {
	test.Parallel()

	dbPath := filepath.Join(test.TempDir(), "tusk.db")
	store, _ := index.Open(dbPath)
	defer store.Close()

	var name string
	scanErr := store.DB().QueryRow(`
		SELECT name FROM sqlite_master
		WHERE type='index' AND tbl_name='nodes' AND name='nodes_kind_type_idx'
	`).Scan(&name)
	if scanErr != nil {
		test.Fatalf("nodes_kind_type_idx missing: %v", scanErr)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/index/... -run 'TestNodesKind|TestNodesCheck|TestNodesPartialUnique|TestNodesHasKindType' -v`

Expected: all FAIL.

- [ ] **Step 3: Update the `nodes` DDL in `internal/index/index.go`**

Change the `nodes` `CREATE TABLE` to:

```go
CREATE TABLE IF NOT EXISTS nodes (
	id              TEXT PRIMARY KEY,           -- workspace-relative path without extension
	type            TEXT NOT NULL,
	path            TEXT NOT NULL,              -- workspace-relative file path with extension; unique among file rows only
	title           TEXT,
	properties_json TEXT NOT NULL DEFAULT '{}',
	last_mtime      INTEGER NOT NULL,
	last_size       INTEGER NOT NULL,
	last_checksum   TEXT NOT NULL,
	parent_id       TEXT NULL,
	ordinal         INTEGER NULL,
	embed_payload   TEXT NULL,
	kind            TEXT NOT NULL,              -- row-class: 'file' | 'subunit'
	source          TEXT NULL,                  -- namespace identifier; NULL = user
	CHECK (
		(kind = 'file'    AND source IS NULL     AND parent_id IS NULL) OR
		(kind = 'subunit' AND source IS NOT NULL AND parent_id IS NOT NULL)
	)
);
```

Replace the `CREATE INDEX … nodes_type_idx` declaration (drop the standalone single-column index; the composite covers it):

```go
CREATE INDEX IF NOT EXISTS nodes_kind_type_idx ON nodes(kind, type);
```

Also update the descriptive comment that still mentions `parent_id IS NULL` for the partial UNIQUE on path:

```go
-- The partial UNIQUE index on path (file rows only) is created by
-- migrateRelaxNodesPathUnique once the sub-units columns exist —
-- sub-unit rows inherit their parent file's path, so a table-level
-- UNIQUE(path) constraint would block them. The predicate is on
-- kind='file' (previously parent_id IS NULL — equivalent under the
-- CHECK, but matches the post-tighten discriminator).
```

- [ ] **Step 4: Update `migrateRelaxNodesPathUnique`'s fallback predicate**

In `internal/index/index.go`, find the fallback branch (around line 521):

```go
if _, execErr := conn.ExecContext(ctx,
    `CREATE UNIQUE INDEX IF NOT EXISTS nodes_file_path_uidx ON nodes(path) WHERE parent_id IS NULL`,
); execErr != nil {
    return fmt.Errorf("index: create nodes_file_path_uidx: %w", execErr)
}
```

Replace the predicate:

```go
if _, execErr := conn.ExecContext(ctx,
    `CREATE UNIQUE INDEX IF NOT EXISTS nodes_file_path_uidx ON nodes(path) WHERE kind = 'file'`,
); execErr != nil {
    return fmt.Errorf("index: create nodes_file_path_uidx: %w", execErr)
}
```

The rebuild block inside `migrateRelaxNodesPathUnique` (the one that copies `nodes` → `nodes_new` with `CREATE TABLE nodes_new` lacking the `kind`/`source` columns) is reachable only when `nodesPathNeedsRelax` returns true, which only happens on the legacy pre-P2 auto-unique constraint shape. Under the new rebuild model those DBs are dropped by `OpenOrRebuild`; the dead branch is left in place per spec § *What the schema bump removes* ("The existing migration code is harmless if left in place: it never executes after this change ships.").

`IF NOT EXISTS` means this statement is a no-op on a DB that already carries `nodes_file_path_uidx` with the old predicate — but those DBs trip the SchemaVersion bump in Step 5 and get rebuilt fresh, so the predicate change actually takes effect.

- [ ] **Step 5: Bump `SchemaVersion`**

In `internal/index/schema_version.go`:

```go
const SchemaVersion = "2026-05-25-nodes-tightened"
```

- [ ] **Step 6: Run the schema-shape tests**

Run: `go test ./internal/index/... -run 'TestNodesKind|TestNodesCheck|TestNodesPartialUnique|TestNodesHasKindType' -v`

Expected: all PASS.

- [ ] **Step 7: Run the full workspace suite**

Run: `go test ./...`

Expected: clean. The Phase 1 rebuild integration test will trip the new version and rebuild — exactly what's expected.

If anything still fails, the Task 2.5a prep PR missed a call site. Capture the failure list and address each via the rule in 2.5a (the right writer for the row's `kind`).

- [ ] **Step 8: `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 9: Commit**

```bash
git add internal/index/index.go internal/index/schema_version.go internal/index/nodes_check_constraint_test.go
git commit -m "feat(index): tighten nodes DDL with NOT NULL kind, CHECK, composite index"
```

- [ ] **Step 10: Open the PR**

```bash
gh pr create --title "feat(index): tighten nodes DDL with NOT NULL kind, CHECK, composite index" --body "$(cat <<'EOF'
## Summary
- `nodes.kind` is now `NOT NULL`
- New CHECK constraint enforces `(kind, source, parent_id)` agreement
- Replaces `nodes_type_idx` with composite `nodes_kind_type_idx`
- Partial UNIQUE on `nodes.path` now predicates on `kind='file'` instead of `parent_id IS NULL`
- Bumps `SchemaVersion` to trigger transparent rebuild via `OpenOrRebuild`
- Phase 2, Task 5b of the node/edge source-namespace plan
- Builds on #<2.5a PR number> (writer/test alignment)

## Test plan
- [ ] `go test ./internal/index/... -v` passes
- [ ] `go test ./...` passes (rebuild path exercised end-to-end)
- [ ] `make vet && make lint` clean
EOF
)"
```

## Done when

- All four new schema tests pass.
- Workspace suite green.
- PR open.
