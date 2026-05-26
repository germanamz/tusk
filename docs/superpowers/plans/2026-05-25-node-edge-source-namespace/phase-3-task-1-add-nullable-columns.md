# Phase 3 — Task 1: Add nullable `kind`/`source` columns to edges

**Phase:** 3 (Edges table reshape)
**Spec:** § *Edges table*

**Goal:** Extend the `edges` DDL with `kind TEXT NULL` and `source TEXT NULL`. No CHECK, no UNIQUE change, no index changes. Bumps `SchemaVersion`.

## Inherits From

- Phase 2 complete; `nodes` carries full `(kind, source)`.
- `edges` currently has: `id, type, source_id, target_id, source_path` with `UNIQUE(type, source_id, target_id, source_path)` (see `internal/index/index.go:45`).

## Files

- **Modify:** `internal/index/index.go` (schema constant)
- **Modify:** `internal/index/schema_version.go` (bump constant)
- **Create:** `internal/index/edges_kind_source_columns_test.go`

## Steps

- [ ] **Step 1: Write the failing test**

Create `internal/index/edges_kind_source_columns_test.go`:

```go
package index_test

import (
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestEdgesTableHasKindAndSourceColumns(test *testing.T) {
	test.Parallel()

	store, _ := index.Open(filepath.Join(test.TempDir(), "tusk.db"))
	defer store.Close()

	rows, _ := store.DB().Query(`PRAGMA table_info(edges)`)
	defer rows.Close()

	seen := map[string]bool{}
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notNull int
			dflt    any
			pk      int
		)
		_ = rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk)
		seen[name] = true
	}

	for _, want := range []string{"kind", "source"} {
		if !seen[want] {
			test.Errorf("edges table missing %q column", want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/index/... -run TestEdgesTableHasKindAndSourceColumns -v`

Expected: FAIL.

- [ ] **Step 3: Add columns to the DDL**

In `internal/index/index.go`, modify the `edges` DDL:

```go
CREATE TABLE IF NOT EXISTS edges (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	type        TEXT NOT NULL,
	source_id   TEXT NOT NULL,
	target_id   TEXT NOT NULL,
	source_path TEXT NOT NULL,
	kind        TEXT NULL,                  -- 'direct' | 'derived' | 'structural' (Phase 3)
	source      TEXT NULL,                  -- namespace identifier; NULL = user (Phase 3)
	UNIQUE(type, source_id, target_id, source_path),
	FOREIGN KEY (source_id) REFERENCES nodes(id) ON DELETE CASCADE
);
```

Do not change the UNIQUE constraint yet; Task 3.5 swaps it.

- [ ] **Step 4: Bump `SchemaVersion`**

```go
const SchemaVersion = "2026-05-25-edges-kind-source-nullable"
```

- [ ] **Step 5: Run the test**

Run: `go test ./internal/index/... -run TestEdgesTableHasKindAndSourceColumns -v`

Expected: PASS.

- [ ] **Step 6: Run the workspace suite**

Run: `go test ./...`

Expected: clean.

- [ ] **Step 7: `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 8: Commit**

```
git add internal/index/index.go internal/index/schema_version.go internal/index/edges_kind_source_columns_test.go
git commit -m "feat(index): add nullable kind/source columns to edges"
```

- [ ] **Step 9: Open the PR**

```
gh pr create --title "feat(index): add nullable kind/source columns to edges" --body "$(cat <<'EOF'
## Summary
- Adds `kind TEXT NULL` and `source TEXT NULL` to `edges` DDL
- Bumps `SchemaVersion` (rebuild via `OpenOrRebuild` is transparent)
- Tasks 3.2–3.4 populate the columns by writer path; Task 3.5 tightens to NOT NULL + CHECK
- Phase 3, Task 1 of the node/edge source-namespace plan

## Test plan
- [ ] `go test ./internal/index/... -v` passes
- [ ] `go test ./...` passes
- [ ] `make vet && make lint` clean
EOF
)"
```

## Done when

- Column-presence test passes.
- Workspace suite green.
- PR open.
