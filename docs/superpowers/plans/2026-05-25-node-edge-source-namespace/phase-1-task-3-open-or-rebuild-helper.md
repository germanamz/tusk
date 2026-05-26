# Phase 1 — Task 3: `OpenOrRebuild` helper

**Phase:** 1 (Index rebuild infrastructure)
**Spec:** § *Schema-version contract*, § *What users see*

**Goal:** Provide a single helper function that opens the index and, on `ErrSchemaIncompatible`, deletes the on-disk file, re-opens cleanly, runs `reindex.Run` to repopulate, and returns the populated index. Every CLI command and the MCP runtime will route through it in subsequent tasks.

The helper lives in a new package `internal/workspace/indexopen` to avoid a cycle between `internal/index` and `internal/reindex` (the latter already imports the former).

## Inherits From

After Task 2:
- `index.SchemaVersion`, `index.MetaSchemaVersionKey` exist.
- `index.Open` returns a `*SchemaVersionError` (wrapped `ErrSchemaIncompatible`) on mismatch.
- Fresh DBs and matching DBs open normally.

## Files

- **Create:** `internal/workspace/indexopen/indexopen.go`
- **Create:** `internal/workspace/indexopen/indexopen_test.go`

## Steps

- [ ] **Step 1: Confirm the package directory does not already exist**

Run: `ls internal/workspace/ 2>/dev/null`

If a `workspace` directory exists, place the new package inside it. If not, create both:

```
mkdir -p internal/workspace/indexopen
```

(The exact parent — `internal/workspace` vs a flat sibling — is a soft preference. The repo already has `internal/workspace/` based on the codebase; if it does not, place the helper at `internal/indexopen` instead and update import paths in this task and Tasks 4 and 5 accordingly.)

- [ ] **Step 2: Write the failing test**

Create `internal/workspace/indexopen/indexopen_test.go`:

```go
package indexopen_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/workspace/indexopen"
)

// fixtureWorkspace seeds a minimal workspace directory with one note
// file so reindex.Run has something to ingest. It returns the
// workspace root and the index path.
func fixtureWorkspace(test *testing.T) (root, indexPath string) {
	test.Helper()

	root = test.TempDir()

	noteContents := "---\ntitle: hello\n---\nbody\n"
	if writeErr := os.WriteFile(filepath.Join(root, "hello.md"), []byte(noteContents), 0o644); writeErr != nil {
		test.Fatalf("write fixture file: %v", writeErr)
	}

	indexPath = filepath.Join(root, ".tusk", "index.db")
	if mkErr := os.MkdirAll(filepath.Dir(indexPath), 0o755); mkErr != nil {
		test.Fatalf("mkdir index dir: %v", mkErr)
	}

	return root, indexPath
}

func TestOpenOrRebuildOpensFreshIndex(test *testing.T) {
	test.Parallel()

	root, indexPath := fixtureWorkspace(test)

	cfg := indexopen.Config{
		IndexPath: indexPath,
		ReindexFactory: func(store *index.Index) reindex.Config {
			return reindex.Config{
				Root: root,
				Repo: index.NewNodeRepo(store),
			}
		},
		Logger: func(string) {},
	}

	store, openErr := indexopen.OpenOrRebuild(context.Background(), cfg)
	if openErr != nil {
		test.Fatalf("OpenOrRebuild: %v", openErr)
	}
	defer store.Close()

	if _, statErr := os.Stat(indexPath); statErr != nil {
		test.Fatalf("expected index file at %s, got %v", indexPath, statErr)
	}
}

func TestOpenOrRebuildRebuildsOnMismatch(test *testing.T) {
	test.Parallel()

	root, indexPath := fixtureWorkspace(test)

	// First open writes the current SchemaVersion.
	first, openErr := index.Open(indexPath)
	if openErr != nil {
		test.Fatalf("seed open: %v", openErr)
	}

	meta := index.NewMetaRepo(first)
	if setErr := meta.Set(index.MetaSchemaVersionKey, "from-some-other-binary"); setErr != nil {
		test.Fatalf("seed mismatch: %v", setErr)
	}
	if closeErr := first.Close(); closeErr != nil {
		test.Fatalf("close seed: %v", closeErr)
	}

	preInfo, statErr := os.Stat(indexPath)
	if statErr != nil {
		test.Fatalf("stat seeded index: %v", statErr)
	}

	var rebuildMessages []string

	cfg := indexopen.Config{
		IndexPath: indexPath,
		ReindexFactory: func(store *index.Index) reindex.Config {
			return reindex.Config{
				Root: root,
				Repo: index.NewNodeRepo(store),
			}
		},
		Logger: func(msg string) {
			rebuildMessages = append(rebuildMessages, msg)
		},
	}

	store, openErr := indexopen.OpenOrRebuild(context.Background(), cfg)
	if openErr != nil {
		test.Fatalf("OpenOrRebuild: %v", openErr)
	}
	defer store.Close()

	postInfo, statErr := os.Stat(indexPath)
	if statErr != nil {
		test.Fatalf("stat rebuilt index: %v", statErr)
	}

	if postInfo.ModTime() == preInfo.ModTime() {
		test.Error("index file was not recreated")
	}

	if len(rebuildMessages) == 0 {
		test.Error("Logger was never called with a rebuild message")
	}

	// Sanity: the rebuilt index has the current schema version.
	got, getErr := index.NewMetaRepo(store).Get(index.MetaSchemaVersionKey)
	if getErr != nil {
		test.Fatalf("read schema_version after rebuild: %v", getErr)
	}
	if got != index.SchemaVersion {
		test.Errorf("rebuilt schema_version = %q, want %q", got, index.SchemaVersion)
	}

	// Confirm the helper did not silently swallow other errors.
	if errors.Is(openErr, index.ErrSchemaIncompatible) {
		test.Error("OpenOrRebuild returned ErrSchemaIncompatible instead of rebuilding")
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/workspace/indexopen/... -v`

Expected: build failure — package missing.

- [ ] **Step 4: Implement the helper**

Write `internal/workspace/indexopen/indexopen.go`:

```go
// Package indexopen wraps index.Open with rebuild-on-mismatch
// semantics. It lives outside both internal/index and internal/reindex
// to avoid an import cycle (reindex already depends on index; the
// rebuild flow needs to invoke reindex.Run when index.Open trips the
// schema-version sentinel).
package indexopen

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/reindex"
)

// Config drives OpenOrRebuild.
type Config struct {
	// IndexPath is the on-disk SQLite file. Passed directly to
	// index.Open and deleted on a schema-version mismatch.
	IndexPath string
	// ReindexFactory builds a reindex.Config for the freshly created
	// index. It is invoked only after a rebuild, with the new
	// *index.Index as input so the caller can construct repos.
	ReindexFactory func(*index.Index) reindex.Config
	// Logger receives a one-line human-readable message when a
	// rebuild happens. Optional; nil disables logging.
	Logger func(string)
}

// OpenOrRebuild opens the index at cfg.IndexPath. If the open trips
// index.ErrSchemaIncompatible, the on-disk file is deleted, the
// index is re-opened (which writes the current SchemaVersion to a
// fresh database), and reindex.Run repopulates it from source files
// using the Config returned by cfg.ReindexFactory.
//
// On success the returned *index.Index is open and ready for use;
// the caller is responsible for closing it. On error nothing is
// open.
func OpenOrRebuild(ctx context.Context, cfg Config) (*index.Index, error) {
	if cfg.IndexPath == "" {
		return nil, errors.New("indexopen: IndexPath is required")
	}
	if cfg.ReindexFactory == nil {
		return nil, errors.New("indexopen: ReindexFactory is required")
	}

	store, openErr := index.Open(cfg.IndexPath)
	if openErr == nil {
		return store, nil
	}

	if !errors.Is(openErr, index.ErrSchemaIncompatible) {
		return nil, openErr
	}

	if cfg.Logger != nil {
		cfg.Logger("index schema changed in this version, rebuilding…")
	}

	if removeErr := os.Remove(cfg.IndexPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return nil, fmt.Errorf("indexopen: delete stale index at %s: %w", cfg.IndexPath, removeErr)
	}

	fresh, freshErr := index.Open(cfg.IndexPath)
	if freshErr != nil {
		return nil, fmt.Errorf("indexopen: reopen after delete: %w", freshErr)
	}

	reindexCfg := cfg.ReindexFactory(fresh)
	if _, runErr := reindex.Run(reindexCfg); runErr != nil {
		fresh.Close()
		return nil, fmt.Errorf("indexopen: reindex during rebuild: %w", runErr)
	}

	_ = ctx // reserved for future cancellation wiring

	return fresh, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/workspace/indexopen/... -v`

Expected: both tests PASS.

- [ ] **Step 6: Run the workspace-wide suite**

Run: `go test ./...`

Expected: every test passes.

- [ ] **Step 7: Run `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 8: Commit**

```
git add internal/workspace/indexopen
git commit -m "feat(indexopen): add OpenOrRebuild helper for schema-version rebuilds"
```

- [ ] **Step 9: Open the PR**

```
gh pr create --title "feat(indexopen): add OpenOrRebuild helper for schema-version rebuilds" --body "$(cat <<'EOF'
## Summary
- New package `internal/workspace/indexopen` exposing `OpenOrRebuild`
- Wraps `index.Open`; on `ErrSchemaIncompatible` deletes the file, re-opens, and runs `reindex.Run` to repopulate from source files
- Lives outside `internal/index` to keep the `index` ↔ `reindex` cycle broken
- Phase 1, Task 3 of the node/edge source-namespace plan

## Test plan
- [ ] `go test ./internal/workspace/indexopen/... -v` passes (covers fresh-open and rebuild-on-mismatch)
- [ ] `go test ./...` passes
- [ ] `make vet && make lint` clean
EOF
)"
```

## Done when

- Both helper tests pass.
- Workspace suite green.
- PR is open.
