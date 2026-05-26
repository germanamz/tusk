# Phase 2 — Task 1: Add nullable `kind`/`source` columns to nodes

**Phase:** 2 (Nodes table reshape)
**Spec:** § *Nodes table*

**Goal:** Extend the `nodes` DDL with `kind TEXT NULL` and `source TEXT NULL` columns. No CHECK constraint yet; no writer changes; no index changes. Bumps `SchemaVersion` so existing on-disk indexes are rebuilt by `OpenOrRebuild` (the rebuild gives us a fresh DB with the new shape).

## Inherits From

- Phase 1 complete; `OpenOrRebuild` handles rebuild transparently.
- `nodes` table currently has columns `id, type, path, title, properties_json, last_mtime, last_size, last_checksum, parent_id, ordinal, embed_payload` (see `internal/index/index.go:21`).

## Files

- **Modify:** `internal/index/index.go` (schema constant)
- **Modify:** `internal/index/schema_version.go` (bump constant)
- **Create:** `internal/index/nodes_kind_source_columns_test.go`

## Steps

- [ ] **Step 1: Write the failing test**

Create `internal/index/nodes_kind_source_columns_test.go`:

```go
package index_test

import (
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestNodesTableHasKindAndSourceColumns(test *testing.T) {
	test.Parallel()

	dbPath := filepath.Join(test.TempDir(), "tusk.db")
	store, openErr := index.Open(dbPath)
	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}
	defer store.Close()

	rows, queryErr := store.DB().Query(`PRAGMA table_info(nodes)`)
	if queryErr != nil {
		test.Fatalf("table_info: %v", queryErr)
	}
	defer rows.Close()

	seen := map[string]struct{}{}
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notNull int
			dfltVal any
			pk      int
		)
		if scanErr := rows.Scan(&cid, &name, &ctype, &notNull, &dfltVal, &pk); scanErr != nil {
			test.Fatalf("scan: %v", scanErr)
		}
		seen[name] = struct{}{}
	}
	if iterErr := rows.Err(); iterErr != nil {
		test.Fatalf("iter: %v", iterErr)
	}

	for _, want := range []string{"kind", "source"} {
		if _, ok := seen[want]; !ok {
			test.Errorf("nodes table missing %q column", want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/index/... -run TestNodesTableHasKindAndSourceColumns -v`

Expected: FAIL — columns missing.

- [ ] **Step 3: Add columns to the DDL**

Open `internal/index/index.go`. Find the `nodes` DDL inside the `schema` constant (around line 21):

```go
CREATE TABLE IF NOT EXISTS nodes (
	id              TEXT PRIMARY KEY,
	type            TEXT NOT NULL,
	path            TEXT NOT NULL,
	title           TEXT,
	properties_json TEXT NOT NULL DEFAULT '{}',
	last_mtime      INTEGER NOT NULL,
	last_size       INTEGER NOT NULL,
	last_checksum   TEXT NOT NULL,
	parent_id       TEXT NULL,
	ordinal         INTEGER NULL,
	embed_payload   TEXT NULL
);
```

Insert `kind` and `source` between `embed_payload` and the closing `);`:

```go
CREATE TABLE IF NOT EXISTS nodes (
	id              TEXT PRIMARY KEY,
	type            TEXT NOT NULL,
	path            TEXT NOT NULL,
	title           TEXT,
	properties_json TEXT NOT NULL DEFAULT '{}',
	last_mtime      INTEGER NOT NULL,
	last_size       INTEGER NOT NULL,
	last_checksum   TEXT NOT NULL,
	parent_id       TEXT NULL,
	ordinal         INTEGER NULL,
	embed_payload   TEXT NULL,
	kind            TEXT NULL,                  -- row-class: 'file' | 'subunit' (Phase 2)
	source          TEXT NULL                   -- namespace identifier; NULL = user (Phase 2)
);
```

Both columns are nullable in this task; Task 2.5 promotes `kind` to `NOT NULL` and adds the CHECK constraint after Tasks 2.2 and 2.3 populate the values.

- [ ] **Step 4: Bump `SchemaVersion`**

In `internal/index/schema_version.go`:

```go
const SchemaVersion = "2026-05-25-nodes-kind-source-nullable"
```

Old value: `"2026-05-25-pre-source-namespace"`. The bump forces a rebuild via `OpenOrRebuild` on the next open.

- [ ] **Step 5: Run the new test**

Run: `go test ./internal/index/... -run TestNodesTableHasKindAndSourceColumns -v`

Expected: PASS.

- [ ] **Step 6: Run the full workspace suite**

Run: `go test ./...`

Expected: all tests pass. Some tests may exercise the rebuild flow (Phase 1 task 6); they should still pass.

- [ ] **Step 7: `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 8: Commit**

```
git add internal/index/index.go internal/index/schema_version.go internal/index/nodes_kind_source_columns_test.go
git commit -m "feat(index): add nullable kind/source columns to nodes"
```

- [ ] **Step 9: Open the PR**

```
gh pr create --title "feat(index): add nullable kind/source columns to nodes" --body "$(cat <<'EOF'
## Summary
- Adds `kind TEXT NULL` and `source TEXT NULL` to the `nodes` DDL
- Bumps `SchemaVersion` to trigger a transparent rebuild on existing indexes
- No writer changes yet — Tasks 2.2 and 2.3 populate the columns; Task 2.5 tightens to NOT NULL + CHECK
- Phase 2, Task 1 of the node/edge source-namespace plan

## Test plan
- [ ] `go test ./internal/index/... -v` passes (incl. new column-presence test)
- [ ] `go test ./...` passes
- [ ] `make vet && make lint` clean
EOF
)"
```

## Done when

- Schema introspection test passes.
- Workspace suite green.
- PR open.
