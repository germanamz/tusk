# Phase 1 — Task 1: Schema-version constant and meta access

**Phase:** 1 (Index rebuild infrastructure)
**Spec:** `docs/superpowers/specs/2026-05-25-node-edge-source-namespace-design.md` § *Schema-version contract*

**Goal:** Introduce a `SchemaVersion` constant in `internal/index` and a single source of truth for the `meta` key that stores it on disk. No behavior change yet; later tasks compare against this constant.

## Inherits From

Base codebase. `internal/index/meta_repo.go` already provides `Get(key)` and `Set(key, value)`.

## Files

- **Create:** `internal/index/schema_version.go`
- **Create:** `internal/index/schema_version_test.go`

## Steps

- [ ] **Step 1: Write the failing test**

Create `internal/index/schema_version_test.go`:

```go
package index_test

import (
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestSchemaVersionConstantNonEmpty(test *testing.T) {
	test.Parallel()

	if index.SchemaVersion == "" {
		test.Fatal("index.SchemaVersion must be a non-empty identifier")
	}
}

func TestMetaSchemaVersionKey(test *testing.T) {
	test.Parallel()

	if index.MetaSchemaVersionKey != "schema_version" {
		test.Fatalf("MetaSchemaVersionKey = %q, want %q", index.MetaSchemaVersionKey, "schema_version")
	}
}

func TestOpenWritesSchemaVersion(test *testing.T) {
	test.Parallel()

	dir := test.TempDir()
	store, openErr := index.Open(filepath.Join(dir, "tusk.db"))
	if openErr != nil {
		test.Fatalf("index.Open: %v", openErr)
	}
	defer store.Close()

	meta := index.NewMetaRepo(store)

	got, getErr := meta.Get(index.MetaSchemaVersionKey)
	if getErr != nil {
		test.Fatalf("meta.Get: %v", getErr)
	}

	if got != index.SchemaVersion {
		test.Fatalf("meta[%s] = %q, want %q", index.MetaSchemaVersionKey, got, index.SchemaVersion)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/index/... -run 'TestSchemaVersion|TestMetaSchemaVersionKey|TestOpenWritesSchemaVersion' -v`

Expected: build failure — `index.SchemaVersion`, `index.MetaSchemaVersionKey` undefined.

- [ ] **Step 3: Create the constant file**

Write `internal/index/schema_version.go`:

```go
package index

// SchemaVersion is the on-disk schema generation this binary writes.
// Open compares the value stored in the meta table against this
// constant; a mismatch (or a missing key) means the on-disk index
// was written by a different binary and must be rebuilt from source
// files. The string is opaque — bump it whenever the schema shape
// (DDL, indexes, CHECK constraints) changes in a way that the in-place
// migration code cannot bridge.
const SchemaVersion = "2026-05-25-pre-source-namespace"

// MetaSchemaVersionKey is the key under which SchemaVersion is stored
// in the meta table. The value lives next to other workspace-scoped
// key/value pairs (see meta_repo.go).
const MetaSchemaVersionKey = "schema_version"
```

The initial value uses today's date with a descriptive suffix — Phase 2 will bump it to a new value (`2026-05-25-nodes-source`) and Phase 3 to another (`2026-05-25-edges-source`). The exact string is not user-visible; it is opaque.

- [ ] **Step 4: Make `Open` write the constant on first creation**

Open `internal/index/index.go`. Find the `Open` function (returns `*Index, error`). After the schema DDL has executed and after all existing migrations have run, write the constant. Locate the line where the function returns the populated `*Index` successfully and insert just before:

```go
metaRepo := NewMetaRepo(idx)
if setErr := metaRepo.Set(MetaSchemaVersionKey, SchemaVersion); setErr != nil {
	idx.Close()
	return nil, fmt.Errorf("index: persist schema_version: %w", setErr)
}
```

If `fmt` is not already imported in `index.go`, add it to the import block.

This Set is unconditional — it overwrites any prior value. That is correct for Task 1 because there is no mismatch handling yet; Task 2 will gate this behind a version check.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/index/... -run 'TestSchemaVersion|TestMetaSchemaVersionKey|TestOpenWritesSchemaVersion' -v`

Expected: PASS on all three.

- [ ] **Step 6: Run the full index test suite to verify no regressions**

Run: `go test ./internal/index/... -v`

Expected: every existing test passes.

- [ ] **Step 7: Run `go vet` and the linter**

Run:
```
make vet
make lint
```

Expected: both pass.

- [ ] **Step 8: Commit**

```
git add internal/index/schema_version.go internal/index/schema_version_test.go internal/index/index.go
git commit -m "feat(index): add SchemaVersion constant and meta persistence"
```

- [ ] **Step 9: Open the PR**

```
git push -u origin <branch>
gh pr create --title "feat(index): add SchemaVersion constant and meta persistence" --body "$(cat <<'EOF'
## Summary
- Introduces `index.SchemaVersion` and `index.MetaSchemaVersionKey` constants
- `index.Open` now persists the current `SchemaVersion` to the `meta` table on every successful open
- Phase 1, Task 1 of the node/edge source-namespace plan (see docs/superpowers/specs/2026-05-25-node-edge-source-namespace-design.md)

## Test plan
- [ ] `go test ./internal/index/... -v` passes
- [ ] `make vet && make lint` clean
EOF
)"
```

## Done when

- The three new tests pass.
- The full existing index suite still passes.
- A PR is open referencing Phase 1, Task 1 of this plan.
