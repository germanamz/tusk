# Phase 1 — Task 2: Schema-version mismatch sentinel

**Phase:** 1 (Index rebuild infrastructure)
**Spec:** § *Schema-version contract*

**Goal:** Replace Task 1's unconditional `Set` with a read-then-compare flow: `Open` returns a `SchemaVersionError` (typed sentinel) when the on-disk version differs from the constant, instead of silently overwriting. Fresh databases (where the key is missing) get the constant written; databases written by a previous binary trip the sentinel.

## Inherits From

After Task 1:
- `index.SchemaVersion` constant exists.
- `index.MetaSchemaVersionKey` exists.
- `index.Open` unconditionally writes the constant after migrations.

This task changes the unconditional write into a versioned check.

## Files

- **Create:** `internal/index/errors.go`
- **Create:** `internal/index/schema_version_mismatch_test.go`
- **Modify:** `internal/index/index.go` (the block added in Task 1)

## Steps

- [ ] **Step 1: Write the failing test**

Create `internal/index/schema_version_mismatch_test.go`:

```go
package index_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestOpenReturnsIncompatibleWhenVersionDiffers(test *testing.T) {
	test.Parallel()

	dbPath := filepath.Join(test.TempDir(), "tusk.db")

	store, openErr := index.Open(dbPath)
	if openErr != nil {
		test.Fatalf("initial Open: %v", openErr)
	}

	meta := index.NewMetaRepo(store)
	if setErr := meta.Set(index.MetaSchemaVersionKey, "from-some-other-binary"); setErr != nil {
		test.Fatalf("seed mismatched version: %v", setErr)
	}

	if closeErr := store.Close(); closeErr != nil {
		test.Fatalf("close: %v", closeErr)
	}

	_, reopenErr := index.Open(dbPath)

	var versionErr *index.SchemaVersionError
	if !errors.As(reopenErr, &versionErr) {
		test.Fatalf("reopen returned %v, want *SchemaVersionError", reopenErr)
	}

	if !errors.Is(reopenErr, index.ErrSchemaIncompatible) {
		test.Fatal("reopen error must satisfy errors.Is(err, index.ErrSchemaIncompatible)")
	}

	if versionErr.Observed != "from-some-other-binary" {
		test.Errorf("Observed = %q, want %q", versionErr.Observed, "from-some-other-binary")
	}

	if versionErr.Expected != index.SchemaVersion {
		test.Errorf("Expected = %q, want %q", versionErr.Expected, index.SchemaVersion)
	}
}

func TestOpenAcceptsMatchingVersion(test *testing.T) {
	test.Parallel()

	dbPath := filepath.Join(test.TempDir(), "tusk.db")

	store, openErr := index.Open(dbPath)
	if openErr != nil {
		test.Fatalf("initial Open: %v", openErr)
	}
	if closeErr := store.Close(); closeErr != nil {
		test.Fatalf("close: %v", closeErr)
	}

	reopen, reopenErr := index.Open(dbPath)
	if reopenErr != nil {
		test.Fatalf("reopen with matching version: %v", reopenErr)
	}
	defer reopen.Close()
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/index/... -run 'TestOpenReturnsIncompatibleWhenVersionDiffers|TestOpenAcceptsMatchingVersion' -v`

Expected: build failure — `index.ErrSchemaIncompatible`, `index.SchemaVersionError` undefined.

- [ ] **Step 3: Create the sentinel and typed error**

Write `internal/index/errors.go`:

```go
package index

import (
	"errors"
	"fmt"
)

// ErrSchemaIncompatible is the sentinel returned (wrapped) by Open when
// the on-disk index was written by a binary with a different
// SchemaVersion. Callers detect it with errors.Is and recover by
// deleting the on-disk file and rebuilding from source via the
// reindex pipeline.
var ErrSchemaIncompatible = errors.New("index: on-disk schema version is incompatible with this binary")

// SchemaVersionError is the typed wrapper around ErrSchemaIncompatible
// that carries the observed and expected version strings so callers
// can include them in user-facing messages.
type SchemaVersionError struct {
	Observed string
	Expected string
}

// Error implements error.
func (e *SchemaVersionError) Error() string {
	return fmt.Sprintf("%s (observed=%q, expected=%q)", ErrSchemaIncompatible.Error(), e.Observed, e.Expected)
}

// Unwrap lets errors.Is match against ErrSchemaIncompatible.
func (e *SchemaVersionError) Unwrap() error {
	return ErrSchemaIncompatible
}
```

- [ ] **Step 4: Replace the unconditional Set in `Open`**

In `internal/index/index.go`, replace the block from Task 1:

```go
metaRepo := NewMetaRepo(idx)
if setErr := metaRepo.Set(MetaSchemaVersionKey, SchemaVersion); setErr != nil {
	idx.Close()
	return nil, fmt.Errorf("index: persist schema_version: %w", setErr)
}
```

with the versioned check:

```go
metaRepo := NewMetaRepo(idx)

observed, getErr := metaRepo.Get(MetaSchemaVersionKey)
if getErr != nil {
	idx.Close()
	return nil, fmt.Errorf("index: read schema_version: %w", getErr)
}

switch {
case observed == "":
	// Fresh DB (or one created by Task 1 which wrote a value
	// unconditionally — that value matches SchemaVersion). Persist
	// the constant.
	if setErr := metaRepo.Set(MetaSchemaVersionKey, SchemaVersion); setErr != nil {
		idx.Close()
		return nil, fmt.Errorf("index: persist schema_version: %w", setErr)
	}
case observed != SchemaVersion:
	idx.Close()
	return nil, &SchemaVersionError{
		Observed: observed,
		Expected: SchemaVersion,
	}
}
```

The `observed == ""` branch covers truly fresh databases. The `observed != SchemaVersion` branch trips the sentinel.

- [ ] **Step 5: Run the new tests**

Run: `go test ./internal/index/... -run 'TestOpenReturnsIncompatibleWhenVersionDiffers|TestOpenAcceptsMatchingVersion' -v`

Expected: both PASS.

- [ ] **Step 6: Run the full index suite and the workspace-wide suite**

Run:
```
go test ./internal/index/... -v
go test ./...
```

Expected: every test passes. There should be no callers in the codebase that hit `ErrSchemaIncompatible` yet because no version bump has happened.

- [ ] **Step 7: Run `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 8: Commit**

```
git add internal/index/errors.go internal/index/schema_version_mismatch_test.go internal/index/index.go
git commit -m "feat(index): return SchemaVersionError on version mismatch"
```

- [ ] **Step 9: Open the PR**

```
gh pr create --title "feat(index): return SchemaVersionError on version mismatch" --body "$(cat <<'EOF'
## Summary
- Adds `index.ErrSchemaIncompatible` sentinel and `index.SchemaVersionError` typed wrapper
- `index.Open` now compares the on-disk `meta.schema_version` against the binary's `SchemaVersion` constant; mismatch returns a wrapped error
- Fresh databases (key absent) continue to receive the constant on first open
- Phase 1, Task 2 of the node/edge source-namespace plan

## Test plan
- [ ] `go test ./internal/index/... -v` passes
- [ ] `go test ./...` passes (no callers expected to trip the sentinel)
- [ ] `make vet && make lint` clean
EOF
)"
```

## Done when

- New tests pass.
- Existing suite passes (workspace-wide).
- PR is open.
