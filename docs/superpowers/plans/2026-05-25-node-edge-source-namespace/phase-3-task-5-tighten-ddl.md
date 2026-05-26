# Phase 3 — Task 5: Tighten edges DDL (NOT NULL, CHECK, UNIQUE swap, new indexes)

**Phase:** 3 (Edges table reshape)
**Spec:** § *Edges table*

**Goal:** Promote `edges.kind` to `NOT NULL`, add the CHECK constraint, swap the UNIQUE constraint to include `source`, add `edges_source_type_idx` and `edges_kind_idx`. Bump `SchemaVersion`.

## Inherits From

After Task 3.4:
- All three writer paths populate `kind`/`source` correctly.
- Schema still has nullable `kind`/`source` and the old UNIQUE constraint without `source`.

## Files

- **Modify:** `internal/index/index.go` (schema constant)
- **Modify:** `internal/index/schema_version.go`
- **Create:** `internal/index/edges_check_constraint_test.go`

## Steps

- [ ] **Step 1: Write the failing tests**

Create `internal/index/edges_check_constraint_test.go`:

```go
package index_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestEdgesKindIsNotNullAndHasCheck(test *testing.T) {
	test.Parallel()

	store, _ := index.Open(filepath.Join(test.TempDir(), "tusk.db"))
	defer store.Close()

	var sqlText string
	if scanErr := store.DB().QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='edges'`).Scan(&sqlText); scanErr != nil {
		test.Fatalf("read DDL: %v", scanErr)
	}

	if !strings.Contains(sqlText, "CHECK") {
		test.Errorf("edges DDL missing CHECK constraint:\n%s", sqlText)
	}
	if !strings.Contains(sqlText, "kind") || !strings.Contains(sqlText, "NOT NULL") {
		test.Errorf("edges DDL missing NOT NULL kind:\n%s", sqlText)
	}
}

func TestEdgesCheckRejectsBadShape(test *testing.T) {
	test.Parallel()

	store, _ := index.Open(filepath.Join(test.TempDir(), "tusk.db"))
	defer store.Close()

	// direct edge with non-NULL source should be rejected
	_, execErr := store.DB().Exec(`
		INSERT INTO edges (type, source_id, target_id, source_path, kind, source)
		VALUES ('mentions', 'a', 'b', 'a', 'direct', 'markdown')
	`)
	if execErr == nil {
		test.Error("CHECK should reject direct edge with non-NULL source")
	}

	// structural edge with NULL source should be rejected
	_, execErr = store.DB().Exec(`
		INSERT INTO edges (type, source_id, target_id, source_path, kind, source)
		VALUES ('contains', 'a', 'b', 'a', 'structural', NULL)
	`)
	if execErr == nil {
		test.Error("CHECK should reject structural edge with NULL source")
	}
}

func TestEdgesUniqueIncludesSource(test *testing.T) {
	test.Parallel()

	store, _ := index.Open(filepath.Join(test.TempDir(), "tusk.db"))
	defer store.Close()

	var sqlText string
	if scanErr := store.DB().QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='edges'`).Scan(&sqlText); scanErr != nil {
		test.Fatalf("read DDL: %v", scanErr)
	}

	if !strings.Contains(sqlText, "UNIQUE(source") {
		test.Errorf("edges UNIQUE constraint must include source as the first column:\n%s", sqlText)
	}
}

func TestEdgesHasSourceTypeAndKindIndexes(test *testing.T) {
	test.Parallel()

	store, _ := index.Open(filepath.Join(test.TempDir(), "tusk.db"))
	defer store.Close()

	for _, idxName := range []string{"edges_source_type_idx", "edges_kind_idx"} {
		var found string
		if scanErr := store.DB().QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name = ?`, idxName).Scan(&found); scanErr != nil {
			test.Errorf("missing index %q: %v", idxName, scanErr)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/index/... -run 'TestEdges' -v`

Expected: all FAIL.

- [ ] **Step 3: Update the `edges` DDL**

In `internal/index/index.go`:

```go
CREATE TABLE IF NOT EXISTS edges (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	type        TEXT NOT NULL,
	source_id   TEXT NOT NULL,
	target_id   TEXT NOT NULL,
	source_path TEXT NOT NULL,
	kind        TEXT NOT NULL,
	source      TEXT NULL,
	UNIQUE(source, type, source_id, target_id, source_path),
	FOREIGN KEY (source_id) REFERENCES nodes(id) ON DELETE CASCADE,
	CHECK (
		(kind IN ('direct', 'derived') AND source IS NULL) OR
		(kind = 'structural'           AND source IS NOT NULL)
	)
);
```

Update the index declarations (replacing or adding):

```go
CREATE INDEX IF NOT EXISTS edges_source_idx      ON edges(source_id);
CREATE INDEX IF NOT EXISTS edges_target_idx      ON edges(target_id);
CREATE INDEX IF NOT EXISTS edges_type_idx        ON edges(type);
CREATE INDEX IF NOT EXISTS edges_source_path_idx ON edges(source_path);
CREATE INDEX IF NOT EXISTS edges_source_type_idx ON edges(source, type);
CREATE INDEX IF NOT EXISTS edges_kind_idx        ON edges(kind);
```

- [ ] **Step 4: Bump `SchemaVersion`**

```go
const SchemaVersion = "2026-05-25-edges-tightened"
```

- [ ] **Step 5: Run the schema-shape tests**

Run: `go test ./internal/index/... -run 'TestEdges' -v`

Expected: all PASS.

- [ ] **Step 6: Run the workspace suite**

Run: `go test ./...`

Expected: clean. Existing edges-related tests should pass because all writers populate the correct values from Tasks 3.2–3.4.

- [ ] **Step 7: `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 8: Commit**

```
git add internal/index/index.go internal/index/schema_version.go internal/index/edges_check_constraint_test.go
git commit -m "feat(index): tighten edges DDL with NOT NULL kind, CHECK, UNIQUE+source"
```

- [ ] **Step 9: Open the PR**

```
gh pr create --title "feat(index): tighten edges DDL with NOT NULL kind, CHECK, UNIQUE+source" --body "$(cat <<'EOF'
## Summary
- `edges.kind` promoted to `NOT NULL`
- CHECK constraint enforces direct/derived → source NULL, structural → source non-NULL
- UNIQUE constraint now `(source, type, source_id, target_id, source_path)`
- New indexes: `edges_source_type_idx`, `edges_kind_idx`
- Bumps `SchemaVersion`
- Phase 3, Task 5 of the node/edge source-namespace plan

## Test plan
- [ ] `go test ./internal/index/... -v` passes
- [ ] `go test ./...` passes
- [ ] `make vet && make lint` clean
EOF
)"
```

## Done when

- All four schema tests pass.
- Workspace suite green.
- PR open.
