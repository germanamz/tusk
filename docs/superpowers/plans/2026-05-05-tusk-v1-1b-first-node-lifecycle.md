# Tusk v1 — Plan 1b: First Node Lifecycle

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver the first end-to-end node lifecycle for Tusk v1: a single binary that can `tusk init` a workspace, `tusk node create` a markdown file with frontmatter, persist its metadata in a SQLite index, retrieve it via `tusk node get`, list nodes via `tusk node list`, and rebuild the index from disk via `tusk reindex`.

**Architecture:** Layered Go binary. CLI (Cobra) → service layer (NodeService, Reindex) → repository layer (Index store backed by SQLite via `modernc.org/sqlite`) + filesystem walker. Manifest (`tusk.toml`) and workspace discovery via walk-up from CWD. Frontmatter parsed as YAML 1.2 (`gopkg.in/yaml.v3`); manifest as TOML (`github.com/BurntSushi/toml`). No edges, no watcher, no semantic retrieval, no behavior packs — those are later plans. Each task follows TDD where applicable: failing test → minimal impl → passing test → commit.

**Tech Stack:** Go 1.26, Cobra (`github.com/spf13/cobra`), `modernc.org/sqlite` (pure-Go SQLite), `gopkg.in/yaml.v3`, `github.com/BurntSushi/toml`.

**Spec reference:** `docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md` §4–§9 (Architecture, Workspace Layout, File Format, Manifest, Indexing).

**Style rules:** All code in this plan respects `STYLE.md` — minimum 2-character identifiers (including `*testing.T` → `test *testing.T`), blank lines around `err` guards, named errors on shadow, ≥ 2-character file/package names.

---

## File Structure

```
cmd/tusk/
  main.go             # entrypoint — invokes root command
  root.go             # root cobra command + persistent flags
  cmd_init.go         # `tusk init`
  cmd_node.go         # `tusk node` parent
  cmd_node_create.go  # `tusk node create`
  cmd_node_get.go     # `tusk node get`
  cmd_node_list.go    # `tusk node list`
  cmd_reindex.go      # `tusk reindex`

internal/workspace/
  workspace.go        # Workspace type + Find (walk-up discovery)
  workspace_test.go

internal/manifest/
  manifest.go         # Manifest type + property type system
  loader.go           # Load(path) → Manifest
  loader_test.go

internal/index/
  index.go            # Index struct + Open/Close + schema bootstrap
  index_test.go
  node_repo.go        # NodeRepo: Upsert, Get, List, DeleteByPath
  node_repo_test.go

internal/node/
  node.go             # Node domain type, Property type
  parse.go            # ParseFile(path, body) → Node
  parse_test.go
  service.go          # NodeService: Create, Get, List
  service_test.go

internal/reindex/
  reindex.go          # Reindex(workspace) — walk + parse + upsert
  reindex_test.go
```

**Why this shape:** each package has one responsibility. `workspace` knows how to find the root and resolve paths. `manifest` parses and validates `tusk.toml`. `index` is the SQLite-backed cache. `node` owns the markdown file format and the high-level Create/Get/List operations. `reindex` is the bulk-rebuild pipeline. `cmd/tusk` is thin — each subcommand wires services together. Tests live next to their code (Go convention).

**Excluded for Plan 1b** (lands in later plans):
- Edges and edge legality (Plan 2)
- Wikilink resolution (Plan 2)
- File watcher and `.gitignore` parsing (Plan 3)
- Filter grammar (Plan 4)
- Embedding pipeline / semantic queries (Plan 5)
- MCP server (Plan 6)
- Behavior packs and type packs (Plan 7)
- Doctor command, rename rewrite pipeline, advisory locks (Plan 3 / 8)

For Plan 1b, `tusk init` writes a minimal `tusk.toml` with no type packs activated. `tusk node create` accepts `--type <name>` as a free-form string with no manifest validation. `tusk node list` accepts no filter syntax beyond a single optional `--type` flag.

---

## Task 0: Pre-flight verification

**Files:** none (read-only)

- [ ] **Step 1: Confirm on `v1` and clean tree**

```bash
git rev-parse --abbrev-ref HEAD
git status --short
```

Expected: branch `v1`; only the pre-existing `M .devcontainer/devcontainer.json`, `M .devcontainer/init-firewall.sh`, `M .gitignore` (or empty). If anything else is unstaged, stop and ask.

- [ ] **Step 2: Confirm baseline `go.mod` and Make targets work**

```bash
cat go.mod
make build
make test
```

Expected: bare `go.mod` (module + `go` directive), all targets exit 0.

---

## Task 1: Add Cobra dependency and CLI skeleton

**Files:**
- Modify: `go.mod`, `go.sum` (created)
- Create: `cmd/tusk/main.go`, `cmd/tusk/root.go`

- [ ] **Step 1: Add Cobra dependency**

```bash
go get github.com/spf13/cobra@latest
```

- [ ] **Step 2: Create `cmd/tusk/main.go`**

```go
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

- [ ] **Step 3: Create `cmd/tusk/root.go`**

```go
package main

import "github.com/spf13/cobra"

const versionString = "v1.0.0-dev"

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "tusk",
		Short:         "Tusk — local-first agent brain",
		Long:          "Tusk indexes a markdown vault into a graph and serves structural and semantic queries.",
		Version:       versionString,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	return rootCmd
}
```

- [ ] **Step 4: Update Makefile `build` target**

Replace the `build` target body in `Makefile` so it actually compiles:

```makefile
build:
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/tusk
```

- [ ] **Step 5: Verify build, help, version**

```bash
make build
./bin/tusk --help
./bin/tusk --version
```

Expected: build exits 0; `--help` prints usage; `--version` prints `tusk version v1.0.0-dev`.

- [ ] **Step 6: Commit**

```bash
git add cmd/tusk go.mod go.sum Makefile
git commit -m "feat(cli): cobra root command and binary entrypoint"
```

---

## Task 2: Workspace discovery package

**Files:**
- Create: `internal/workspace/workspace.go`, `internal/workspace/workspace_test.go`

- [ ] **Step 1: Write the failing test — `internal/workspace/workspace_test.go`**

```go
package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/workspace"
)

func TestFind_FindsManifestInCurrentDir(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	if writeErr := os.WriteFile(manifestPath, []byte("[workspace]\nname=\"test\"\n"), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	found, findErr := workspace.Find(tmpDir)

	if findErr != nil {
		test.Fatalf("Find: %v", findErr)
	}

	if found.Root != tmpDir {
		test.Errorf("Root = %q, want %q", found.Root, tmpDir)
	}

	if found.ManifestPath != manifestPath {
		test.Errorf("ManifestPath = %q, want %q", found.ManifestPath, manifestPath)
	}
}

func TestFind_WalksUpToParent(test *testing.T) {
	tmpDir := test.TempDir()
	subDir := filepath.Join(tmpDir, "sub", "deeper")

	if mkErr := os.MkdirAll(subDir, 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	if writeErr := os.WriteFile(manifestPath, []byte("[workspace]\nname=\"test\"\n"), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	found, findErr := workspace.Find(subDir)

	if findErr != nil {
		test.Fatalf("Find: %v", findErr)
	}

	if found.Root != tmpDir {
		test.Errorf("Root = %q, want %q", found.Root, tmpDir)
	}
}

func TestFind_ReturnsErrNotFoundWhenNoManifest(test *testing.T) {
	tmpDir := test.TempDir()

	_, findErr := workspace.Find(tmpDir)

	if findErr == nil {
		test.Fatalf("expected error, got nil")
	}

	if !errorIsNotFound(findErr) {
		test.Errorf("error = %v, want ErrNotFound", findErr)
	}
}

func errorIsNotFound(err error) bool {
	return err != nil && err.Error() == workspace.ErrNotFound.Error()
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/workspace/...
```

Expected: FAIL with "package workspace not found" or "Find undefined".

- [ ] **Step 3: Implement — `internal/workspace/workspace.go`**

```go
// Package workspace discovers the Tusk workspace root by walking up from a
// starting directory looking for tusk.toml.
package workspace

import (
	"errors"
	"os"
	"path/filepath"
)

// ManifestFilename is the canonical manifest file name at the workspace root.
const ManifestFilename = "tusk.toml"

// IndexDirname is the gitignored directory holding the local index database.
const IndexDirname = ".tusk"

// IndexFilename is the SQLite index file name inside IndexDirname.
const IndexFilename = "index.db"

// ErrNotFound is returned by Find when no tusk.toml is found by walking up.
var ErrNotFound = errors.New("workspace: no tusk.toml found")

// Workspace describes a located Tusk workspace on disk.
type Workspace struct {
	Root         string // absolute path to workspace root
	ManifestPath string // absolute path to tusk.toml
	IndexPath    string // absolute path to .tusk/index.db (may not yet exist)
}

// Find walks up from startDir looking for tusk.toml. Returns ErrNotFound
// once it reaches the filesystem root without finding one.
func Find(startDir string) (*Workspace, error) {
	current, absErr := filepath.Abs(startDir)

	if absErr != nil {
		return nil, absErr
	}

	for {
		manifestPath := filepath.Join(current, ManifestFilename)
		stat, statErr := os.Stat(manifestPath)

		if statErr == nil && !stat.IsDir() {
			return &Workspace{
				Root:         current,
				ManifestPath: manifestPath,
				IndexPath:    filepath.Join(current, IndexDirname, IndexFilename),
			}, nil
		}

		parent := filepath.Dir(current)

		if parent == current {
			return nil, ErrNotFound
		}

		current = parent
	}
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/workspace/... -v
```

Expected: 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/workspace
git commit -m "feat(workspace): walk-up discovery of tusk.toml"
```

---

## Task 3: Manifest types + TOML loader

**Files:**
- Create: `internal/manifest/manifest.go`, `internal/manifest/loader.go`, `internal/manifest/loader_test.go`
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add TOML dependency**

```bash
go get github.com/BurntSushi/toml@latest
```

- [ ] **Step 2: Write the failing test — `internal/manifest/loader_test.go`**

```go
package manifest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
)

func TestLoad_ParsesMinimalManifest(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "my-brain"
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	if loaded.Workspace.Name != "my-brain" {
		test.Errorf("Name = %q, want %q", loaded.Workspace.Name, "my-brain")
	}
}

func TestLoad_ParsesIgnorePatterns(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "my-brain"
ignore = ["build/", "*.tmp"]
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	if len(loaded.Workspace.Ignore) != 2 {
		test.Fatalf("Ignore len = %d, want 2", len(loaded.Workspace.Ignore))
	}

	if loaded.Workspace.Ignore[0] != "build/" {
		test.Errorf("Ignore[0] = %q", loaded.Workspace.Ignore[0])
	}
}

func TestLoad_ReturnsErrorOnMalformedTOML(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	if writeErr := os.WriteFile(manifestPath, []byte("not = valid = toml"), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	_, loadErr := manifest.Load(manifestPath)

	if loadErr == nil {
		test.Fatalf("expected error, got nil")
	}
}
```

- [ ] **Step 3: Run, verify fail**

```bash
go test ./internal/manifest/...
```

Expected: FAIL — package not found.

- [ ] **Step 4: Implement types — `internal/manifest/manifest.go`**

```go
// Package manifest defines the schema and loader for tusk.toml.
package manifest

// Manifest is the parsed representation of tusk.toml at the workspace root.
type Manifest struct {
	Workspace WorkspaceSection `toml:"workspace"`
}

// WorkspaceSection holds top-level workspace configuration.
//
// Plan 1b ships only Name and Ignore; type packs, embeddings config, behaviors,
// and the inline node-types/edge-types tables land in later plans.
type WorkspaceSection struct {
	Name    string   `toml:"name"`
	Ignore  []string `toml:"ignore"`
}
```

- [ ] **Step 5: Implement loader — `internal/manifest/loader.go`**

```go
package manifest

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Load reads and decodes a tusk.toml at manifestPath.
func Load(manifestPath string) (*Manifest, error) {
	body, readErr := os.ReadFile(manifestPath)

	if readErr != nil {
		return nil, fmt.Errorf("manifest: read %s: %w", manifestPath, readErr)
	}

	loaded := &Manifest{}

	if _, decodeErr := toml.Decode(string(body), loaded); decodeErr != nil {
		return nil, fmt.Errorf("manifest: decode %s: %w", manifestPath, decodeErr)
	}

	return loaded, nil
}
```

- [ ] **Step 6: Run, verify pass**

```bash
go test ./internal/manifest/... -v
```

Expected: 3 PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/manifest go.mod go.sum
git commit -m "feat(manifest): TOML loader with workspace ignore patterns"
```

---

## Task 4: Index — open + schema bootstrap

**Files:**
- Create: `internal/index/index.go`, `internal/index/index_test.go`
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add SQLite driver**

```bash
go get modernc.org/sqlite@latest
```

- [ ] **Step 2: Write the failing test — `internal/index/index_test.go`**

```go
package index_test

import (
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestOpen_CreatesSchemaOnFirstOpen(test *testing.T) {
	dbPath := filepath.Join(test.TempDir(), "index.db")

	store, openErr := index.Open(dbPath)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	tables, queryErr := store.ListTables()

	if queryErr != nil {
		test.Fatalf("ListTables: %v", queryErr)
	}

	requiredTables := []string{"nodes", "manifest_snapshot", "warnings"}

	for _, required := range requiredTables {
		if !contains(tables, required) {
			test.Errorf("missing table %q in %v", required, tables)
		}
	}
}

func TestOpen_IsIdempotent(test *testing.T) {
	dbPath := filepath.Join(test.TempDir(), "index.db")

	first, firstErr := index.Open(dbPath)

	if firstErr != nil {
		test.Fatalf("first Open: %v", firstErr)
	}

	first.Close()

	second, secondErr := index.Open(dbPath)

	if secondErr != nil {
		test.Fatalf("second Open: %v", secondErr)
	}

	second.Close()
}

func contains(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}

	return false
}
```

- [ ] **Step 3: Run, verify fail**

```bash
go test ./internal/index/...
```

Expected: FAIL — package not found.

- [ ] **Step 4: Implement — `internal/index/index.go`**

```go
// Package index manages the local SQLite index that mirrors the workspace.
package index

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Index is the SQLite-backed local cache of the workspace.
type Index struct {
	db   *sql.DB
	path string
}

const schema = `
CREATE TABLE IF NOT EXISTS nodes (
	id              TEXT PRIMARY KEY,           -- workspace-relative path without extension
	type            TEXT NOT NULL,
	path            TEXT NOT NULL UNIQUE,       -- workspace-relative file path with extension
	title           TEXT,
	properties_json TEXT NOT NULL DEFAULT '{}', -- JSON object of all non-edge frontmatter properties
	last_mtime      INTEGER NOT NULL,           -- unix nanoseconds
	last_size       INTEGER NOT NULL,
	last_checksum   TEXT NOT NULL               -- sha256 hex
);

CREATE INDEX IF NOT EXISTS nodes_type_idx ON nodes(type);

CREATE TABLE IF NOT EXISTS manifest_snapshot (
	loaded_at INTEGER NOT NULL,                 -- unix nanoseconds
	body_json TEXT NOT NULL                     -- JSON-serialized snapshot of the manifest
);

CREATE TABLE IF NOT EXISTS warnings (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	node_id   TEXT,                             -- nullable: warning may be workspace-scoped
	kind      TEXT NOT NULL,
	message   TEXT NOT NULL,
	since     INTEGER NOT NULL                  -- unix nanoseconds
);
`

// Open opens (and bootstraps if needed) the index at dbPath. The parent
// directory is created if missing.
func Open(dbPath string) (*Index, error) {
	if mkErr := os.MkdirAll(filepath.Dir(dbPath), 0o755); mkErr != nil {
		return nil, fmt.Errorf("index: ensure dir: %w", mkErr)
	}

	db, openErr := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)")

	if openErr != nil {
		return nil, fmt.Errorf("index: open sqlite: %w", openErr)
	}

	if _, execErr := db.Exec(schema); execErr != nil {
		db.Close()
		return nil, fmt.Errorf("index: bootstrap schema: %w", execErr)
	}

	return &Index{db: db, path: dbPath}, nil
}

// Close releases the underlying database handle.
func (idx *Index) Close() error {
	return idx.db.Close()
}

// DB exposes the underlying *sql.DB for repository packages in the same module.
func (idx *Index) DB() *sql.DB {
	return idx.db
}

// ListTables returns the names of all user tables, sorted.
func (idx *Index) ListTables() ([]string, error) {
	rows, queryErr := idx.db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)

	if queryErr != nil {
		return nil, fmt.Errorf("index: list tables: %w", queryErr)
	}

	defer rows.Close()

	var names []string

	for rows.Next() {
		var name string

		if scanErr := rows.Scan(&name); scanErr != nil {
			return nil, fmt.Errorf("index: scan table: %w", scanErr)
		}

		names = append(names, name)
	}

	return names, rows.Err()
}
```

- [ ] **Step 5: Run, verify pass**

```bash
go test ./internal/index/... -v
```

Expected: 2 PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/index go.mod go.sum
git commit -m "feat(index): SQLite open and schema bootstrap"
```

---

## Task 5: NodeRepo — Upsert / Get / List / DeleteByPath

**Files:**
- Create: `internal/index/node_repo.go`, `internal/index/node_repo_test.go`

- [ ] **Step 1: Write the failing test — `internal/index/node_repo_test.go`**

```go
package index_test

import (
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func openTestIndex(test *testing.T) *index.Index {
	test.Helper()

	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	return store
}

func TestNodeRepo_UpsertAndGet(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewNodeRepo(store)

	row := index.NodeRow{
		ID:           "notes/auth-rfc",
		Type:         "note",
		Path:         "notes/auth-rfc.md",
		Title:        "Auth RFC",
		PropertiesJSON: `{"title":"Auth RFC"}`,
		LastMtime:    100,
		LastSize:     42,
		LastChecksum: "abc123",
	}

	if upsertErr := repo.Upsert(row); upsertErr != nil {
		test.Fatalf("Upsert: %v", upsertErr)
	}

	loaded, loadErr := repo.Get("notes/auth-rfc")

	if loadErr != nil {
		test.Fatalf("Get: %v", loadErr)
	}

	if loaded.Type != "note" || loaded.Title != "Auth RFC" {
		test.Errorf("got = %+v", loaded)
	}
}

func TestNodeRepo_UpsertReplacesExistingRow(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewNodeRepo(store)

	first := index.NodeRow{
		ID: "x", Type: "ticket", Path: "x.md", Title: "first",
		PropertiesJSON: `{}`, LastMtime: 1, LastSize: 1, LastChecksum: "a",
	}

	second := index.NodeRow{
		ID: "x", Type: "ticket", Path: "x.md", Title: "second",
		PropertiesJSON: `{}`, LastMtime: 2, LastSize: 2, LastChecksum: "b",
	}

	if upsertErr := repo.Upsert(first); upsertErr != nil {
		test.Fatalf("first upsert: %v", upsertErr)
	}

	if upsertErr := repo.Upsert(second); upsertErr != nil {
		test.Fatalf("second upsert: %v", upsertErr)
	}

	loaded, loadErr := repo.Get("x")

	if loadErr != nil {
		test.Fatalf("Get: %v", loadErr)
	}

	if loaded.Title != "second" {
		test.Errorf("Title = %q, want %q", loaded.Title, "second")
	}
}

func TestNodeRepo_GetReturnsErrNotFoundWhenMissing(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewNodeRepo(store)

	_, getErr := repo.Get("missing")

	if getErr != index.ErrNodeNotFound {
		test.Errorf("err = %v, want ErrNodeNotFound", getErr)
	}
}

func TestNodeRepo_ListAll(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewNodeRepo(store)

	rows := []index.NodeRow{
		{ID: "a", Type: "ticket", Path: "a.md", Title: "A", PropertiesJSON: `{}`, LastMtime: 1, LastSize: 1, LastChecksum: "1"},
		{ID: "b", Type: "note", Path: "b.md", Title: "B", PropertiesJSON: `{}`, LastMtime: 2, LastSize: 2, LastChecksum: "2"},
	}

	for _, row := range rows {
		if upsertErr := repo.Upsert(row); upsertErr != nil {
			test.Fatalf("upsert: %v", upsertErr)
		}
	}

	listed, listErr := repo.List(index.ListFilter{})

	if listErr != nil {
		test.Fatalf("List: %v", listErr)
	}

	if len(listed) != 2 {
		test.Fatalf("len = %d, want 2", len(listed))
	}
}

func TestNodeRepo_ListByType(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewNodeRepo(store)

	repo.Upsert(index.NodeRow{ID: "a", Type: "ticket", Path: "a.md", Title: "A", PropertiesJSON: `{}`, LastMtime: 1, LastSize: 1, LastChecksum: "1"})
	repo.Upsert(index.NodeRow{ID: "b", Type: "note", Path: "b.md", Title: "B", PropertiesJSON: `{}`, LastMtime: 2, LastSize: 2, LastChecksum: "2"})

	listed, listErr := repo.List(index.ListFilter{Type: "ticket"})

	if listErr != nil {
		test.Fatalf("List: %v", listErr)
	}

	if len(listed) != 1 || listed[0].ID != "a" {
		test.Errorf("listed = %+v", listed)
	}
}

func TestNodeRepo_DeleteByPath(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewNodeRepo(store)

	repo.Upsert(index.NodeRow{ID: "x", Type: "note", Path: "x.md", Title: "X", PropertiesJSON: `{}`, LastMtime: 1, LastSize: 1, LastChecksum: "1"})

	if deleteErr := repo.DeleteByPath("x.md"); deleteErr != nil {
		test.Fatalf("DeleteByPath: %v", deleteErr)
	}

	_, getErr := repo.Get("x")

	if getErr != index.ErrNodeNotFound {
		test.Errorf("err after delete = %v, want ErrNodeNotFound", getErr)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/index/... -run NodeRepo
```

Expected: FAIL.

- [ ] **Step 3: Implement — `internal/index/node_repo.go`**

```go
package index

import (
	"database/sql"
	"errors"
	"fmt"
)

// ErrNodeNotFound is returned by NodeRepo.Get when the node id is not in the index.
var ErrNodeNotFound = errors.New("index: node not found")

// NodeRow is the index representation of a node.
type NodeRow struct {
	ID             string
	Type           string
	Path           string
	Title          string
	PropertiesJSON string
	LastMtime      int64
	LastSize       int64
	LastChecksum   string
}

// ListFilter narrows a NodeRepo.List call. Plan 1b supports type only.
type ListFilter struct {
	Type string
}

// NodeRepo persists NodeRow values in the SQLite index.
type NodeRepo struct {
	db *sql.DB
}

// NewNodeRepo constructs a NodeRepo backed by idx.
func NewNodeRepo(idx *Index) *NodeRepo {
	return &NodeRepo{db: idx.DB()}
}

// Upsert inserts or replaces a node row.
func (repo *NodeRepo) Upsert(row NodeRow) error {
	_, execErr := repo.db.Exec(`
		INSERT INTO nodes (id, type, path, title, properties_json, last_mtime, last_size, last_checksum)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			type            = excluded.type,
			path            = excluded.path,
			title           = excluded.title,
			properties_json = excluded.properties_json,
			last_mtime      = excluded.last_mtime,
			last_size       = excluded.last_size,
			last_checksum   = excluded.last_checksum
	`, row.ID, row.Type, row.Path, row.Title, row.PropertiesJSON, row.LastMtime, row.LastSize, row.LastChecksum)

	if execErr != nil {
		return fmt.Errorf("nodeRepo: upsert %s: %w", row.ID, execErr)
	}

	return nil
}

// Get returns the row with the given id, or ErrNodeNotFound.
func (repo *NodeRepo) Get(nodeID string) (*NodeRow, error) {
	row := repo.db.QueryRow(`
		SELECT id, type, path, title, properties_json, last_mtime, last_size, last_checksum
		FROM nodes
		WHERE id = ?
	`, nodeID)

	loaded := &NodeRow{}
	scanErr := row.Scan(&loaded.ID, &loaded.Type, &loaded.Path, &loaded.Title, &loaded.PropertiesJSON, &loaded.LastMtime, &loaded.LastSize, &loaded.LastChecksum)

	if scanErr == sql.ErrNoRows {
		return nil, ErrNodeNotFound
	}

	if scanErr != nil {
		return nil, fmt.Errorf("nodeRepo: get %s: %w", nodeID, scanErr)
	}

	return loaded, nil
}

// List returns rows matching filter, ordered by id ASC.
func (repo *NodeRepo) List(filter ListFilter) ([]NodeRow, error) {
	query := `SELECT id, type, path, title, properties_json, last_mtime, last_size, last_checksum FROM nodes`
	args := []any{}

	if filter.Type != "" {
		query += ` WHERE type = ?`
		args = append(args, filter.Type)
	}

	query += ` ORDER BY id ASC`

	rows, queryErr := repo.db.Query(query, args...)

	if queryErr != nil {
		return nil, fmt.Errorf("nodeRepo: list: %w", queryErr)
	}

	defer rows.Close()

	var results []NodeRow

	for rows.Next() {
		row := NodeRow{}

		if scanErr := rows.Scan(&row.ID, &row.Type, &row.Path, &row.Title, &row.PropertiesJSON, &row.LastMtime, &row.LastSize, &row.LastChecksum); scanErr != nil {
			return nil, fmt.Errorf("nodeRepo: scan: %w", scanErr)
		}

		results = append(results, row)
	}

	return results, rows.Err()
}

// DeleteByPath removes the node row whose path equals filePath.
func (repo *NodeRepo) DeleteByPath(filePath string) error {
	_, execErr := repo.db.Exec(`DELETE FROM nodes WHERE path = ?`, filePath)

	if execErr != nil {
		return fmt.Errorf("nodeRepo: delete %s: %w", filePath, execErr)
	}

	return nil
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/index/... -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/index
git commit -m "feat(index): NodeRepo CRUD with upsert / get / list / delete"
```

---

## Task 6: Node domain type + frontmatter parser

**Files:**
- Create: `internal/node/node.go`, `internal/node/parse.go`, `internal/node/parse_test.go`
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add YAML dependency**

```bash
go get gopkg.in/yaml.v3@latest
```

- [ ] **Step 2: Write the failing test — `internal/node/parse_test.go`**

```go
package node_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/node"
)

func TestParseFile_ExtractsFrontmatterAndBody(test *testing.T) {
	content := []byte(`---
type: ticket
title: Fix login bug
priority: 3
---

# Fix login bug

The body.
`)

	parsed, parseErr := node.ParseFile("tickets/fix-login-bug.md", content)

	if parseErr != nil {
		test.Fatalf("ParseFile: %v", parseErr)
	}

	if parsed.ID != "tickets/fix-login-bug" {
		test.Errorf("ID = %q", parsed.ID)
	}

	if parsed.Type != "ticket" {
		test.Errorf("Type = %q", parsed.Type)
	}

	if parsed.Title != "Fix login bug" {
		test.Errorf("Title = %q", parsed.Title)
	}

	priority, ok := parsed.Properties["priority"]

	if !ok {
		test.Fatalf("priority not in Properties")
	}

	if priorityInt, isInt := priority.(int); !isInt || priorityInt != 3 {
		test.Errorf("priority = %v (%T), want 3 (int)", priority, priority)
	}

	if string(parsed.Body) != "# Fix login bug\n\nThe body.\n" {
		test.Errorf("Body = %q", string(parsed.Body))
	}
}

func TestParseFile_HandlesNoFrontmatter(test *testing.T) {
	content := []byte("# Just a body\n\nNo frontmatter.\n")

	_, parseErr := node.ParseFile("notes/plain.md", content)

	if parseErr != node.ErrMissingFrontmatter {
		test.Errorf("err = %v, want ErrMissingFrontmatter", parseErr)
	}
}

func TestParseFile_RequiresTypeField(test *testing.T) {
	content := []byte(`---
title: missing type
---

body
`)

	_, parseErr := node.ParseFile("x.md", content)

	if parseErr != node.ErrMissingType {
		test.Errorf("err = %v, want ErrMissingType", parseErr)
	}
}

func TestParseFile_StripsExtensionFromID(test *testing.T) {
	content := []byte("---\ntype: note\n---\n\nbody\n")

	parsed, parseErr := node.ParseFile("a/b/c.md", content)

	if parseErr != nil {
		test.Fatalf("ParseFile: %v", parseErr)
	}

	if parsed.ID != "a/b/c" {
		test.Errorf("ID = %q, want a/b/c", parsed.ID)
	}
}
```

- [ ] **Step 3: Run, verify fail**

```bash
go test ./internal/node/...
```

Expected: FAIL.

- [ ] **Step 4: Implement domain type — `internal/node/node.go`**

```go
// Package node owns the markdown-file representation of a node and the
// service operations that create, read, and list them.
package node

// Node is the parsed representation of a markdown node file.
type Node struct {
	ID         string                 // workspace-relative path without extension
	Path       string                 // workspace-relative path with extension
	Type       string                 // value of the required `type:` frontmatter field
	Title      string                 // value of the optional `title:` frontmatter field; empty if absent
	Properties map[string]any         // all frontmatter keys (including `type` and `title`)
	Body       []byte                 // markdown body after the closing `---` delimiter
}
```

- [ ] **Step 5: Implement parser — `internal/node/parse.go`**

```go
package node

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrMissingFrontmatter indicates the file does not begin with a YAML frontmatter block.
var ErrMissingFrontmatter = errors.New("node: missing frontmatter")

// ErrMissingType indicates the frontmatter has no `type:` field.
var ErrMissingType = errors.New("node: missing required `type` field")

var frontmatterDelimiter = []byte("---")

// ParseFile parses content as a Tusk node file. relPath is the workspace-relative
// path (with extension); the canonical id is relPath stripped of its extension.
func ParseFile(relPath string, content []byte) (*Node, error) {
	frontmatterBytes, body, splitErr := splitFrontmatter(content)

	if splitErr != nil {
		return nil, splitErr
	}

	properties := map[string]any{}

	if decodeErr := yaml.Unmarshal(frontmatterBytes, &properties); decodeErr != nil {
		return nil, fmt.Errorf("node: decode frontmatter %s: %w", relPath, decodeErr)
	}

	typeValue, hasType := properties["type"].(string)

	if !hasType || typeValue == "" {
		return nil, ErrMissingType
	}

	title, _ := properties["title"].(string)

	properties = normalizeYAMLNumbers(properties)

	return &Node{
		ID:         strings.TrimSuffix(relPath, filepath.Ext(relPath)),
		Path:       relPath,
		Type:       typeValue,
		Title:      title,
		Properties: properties,
		Body:       body,
	}, nil
}

// splitFrontmatter returns the YAML body (without delimiters) and the remaining
// markdown body. The file must begin with `---\n`.
func splitFrontmatter(content []byte) ([]byte, []byte, error) {
	trimmed := bytes.TrimLeft(content, " \t\r\n")

	if !bytes.HasPrefix(trimmed, frontmatterDelimiter) {
		return nil, nil, ErrMissingFrontmatter
	}

	afterOpen := trimmed[len(frontmatterDelimiter):]

	// Skip the newline after the opening delimiter.
	afterOpen = bytes.TrimLeft(afterOpen, "\r\n")

	closingIndex := bytes.Index(afterOpen, append([]byte("\n"), frontmatterDelimiter...))

	if closingIndex < 0 {
		return nil, nil, ErrMissingFrontmatter
	}

	frontmatter := afterOpen[:closingIndex]
	rest := afterOpen[closingIndex+len("\n")+len(frontmatterDelimiter):]

	body := bytes.TrimLeft(rest, "\r\n")

	return frontmatter, body, nil
}

// normalizeYAMLNumbers walks a parsed YAML map and converts number-shaped values
// from the YAML library's default int / float64 to plain int where the value
// fits losslessly. This keeps test assertions stable across YAML library
// behavior changes.
func normalizeYAMLNumbers(values map[string]any) map[string]any {
	for key, value := range values {
		switch typed := value.(type) {
		case int:
			values[key] = typed
		case int64:
			values[key] = int(typed)
		case float64:
			if typed == float64(int(typed)) {
				values[key] = int(typed)
			}
		}
	}

	return values
}
```

- [ ] **Step 6: Run, verify pass**

```bash
go test ./internal/node/... -v
```

Expected: 4 PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/node go.mod go.sum
git commit -m "feat(node): YAML frontmatter parser and Node domain type"
```

---

## Task 7: NodeService — Create / Get / List

**Files:**
- Create: `internal/node/service.go`, `internal/node/service_test.go`

- [ ] **Step 1: Write the failing test — `internal/node/service_test.go`**

```go
package node_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/node"
)

func newTestService(test *testing.T) (*node.Service, string) {
	test.Helper()

	root := test.TempDir()

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	service := node.NewService(root, index.NewNodeRepo(store))

	return service, root
}

func TestService_CreateWritesFileAndIndexes(test *testing.T) {
	service, root := newTestService(test)

	created, createErr := service.Create(node.CreateInput{
		RelPath: "tickets/fix-login.md",
		Type:    "ticket",
		Title:   "Fix login",
		Body:    []byte("Some body.\n"),
	})

	if createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	if created.ID != "tickets/fix-login" {
		test.Errorf("ID = %q", created.ID)
	}

	onDisk, readErr := os.ReadFile(filepath.Join(root, "tickets/fix-login.md"))

	if readErr != nil {
		test.Fatalf("read file: %v", readErr)
	}

	if !contains(string(onDisk), "type: ticket") {
		test.Errorf("file missing type: %s", string(onDisk))
	}

	loaded, getErr := service.Get("tickets/fix-login")

	if getErr != nil {
		test.Fatalf("Get: %v", getErr)
	}

	if loaded.Title != "Fix login" {
		test.Errorf("Title = %q", loaded.Title)
	}
}

func TestService_CreateRejectsExistingFile(test *testing.T) {
	service, _ := newTestService(test)

	if _, firstErr := service.Create(node.CreateInput{
		RelPath: "x.md", Type: "note", Body: []byte(""),
	}); firstErr != nil {
		test.Fatalf("first Create: %v", firstErr)
	}

	_, secondErr := service.Create(node.CreateInput{
		RelPath: "x.md", Type: "note", Body: []byte(""),
	})

	if secondErr != node.ErrAlreadyExists {
		test.Errorf("err = %v, want ErrAlreadyExists", secondErr)
	}
}

func TestService_ListReturnsAllNodes(test *testing.T) {
	service, _ := newTestService(test)

	service.Create(node.CreateInput{RelPath: "a.md", Type: "note", Body: []byte("")})
	service.Create(node.CreateInput{RelPath: "b.md", Type: "ticket", Body: []byte("")})

	all, listErr := service.List(node.ListFilter{})

	if listErr != nil {
		test.Fatalf("List: %v", listErr)
	}

	if len(all) != 2 {
		test.Errorf("len = %d, want 2", len(all))
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for offset := 0; offset+len(needle) <= len(haystack); offset++ {
		if haystack[offset:offset+len(needle)] == needle {
			return offset
		}
	}

	return -1
}
```

- [ ] **Step 2: Run, verify fail**

Expected: FAIL — `node.Service`, `node.NewService`, `node.CreateInput`, `node.ErrAlreadyExists` not found.

- [ ] **Step 3: Implement — `internal/node/service.go`**

```go
package node

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/germanamz/tusk/internal/index"
)

// ErrAlreadyExists is returned by Create when the target file already exists.
var ErrAlreadyExists = errors.New("node: file already exists")

// CreateInput configures Service.Create.
type CreateInput struct {
	RelPath    string         // workspace-relative target path including extension (e.g. "tickets/foo.md")
	Type       string         // required type
	Title      string         // optional title; if empty, no title key is written
	Properties map[string]any // additional frontmatter properties (excluding type and title)
	Body       []byte         // markdown body
}

// ListFilter narrows Service.List. Plan 1b supports type only.
type ListFilter struct {
	Type string
}

// Service orchestrates filesystem and index for nodes.
type Service struct {
	root string
	repo *index.NodeRepo
}

// NewService constructs a Service for workspace at workspaceRoot.
func NewService(workspaceRoot string, repo *index.NodeRepo) *Service {
	return &Service{root: workspaceRoot, repo: repo}
}

// Create writes the node file and upserts the index row in one operation.
func (service *Service) Create(input CreateInput) (*Node, error) {
	absPath := filepath.Join(service.root, input.RelPath)

	if _, statErr := os.Stat(absPath); statErr == nil {
		return nil, ErrAlreadyExists
	}

	properties := map[string]any{"type": input.Type}

	if input.Title != "" {
		properties["title"] = input.Title
	}

	for key, value := range input.Properties {
		properties[key] = value
	}

	rendered, renderErr := renderMarkdown(properties, input.Body)

	if renderErr != nil {
		return nil, renderErr
	}

	if mkErr := os.MkdirAll(filepath.Dir(absPath), 0o755); mkErr != nil {
		return nil, fmt.Errorf("node: mkdir %s: %w", filepath.Dir(absPath), mkErr)
	}

	if writeErr := os.WriteFile(absPath, rendered, 0o644); writeErr != nil {
		return nil, fmt.Errorf("node: write %s: %w", absPath, writeErr)
	}

	stat, statErr := os.Stat(absPath)

	if statErr != nil {
		return nil, fmt.Errorf("node: stat %s: %w", absPath, statErr)
	}

	parsed, parseErr := ParseFile(input.RelPath, rendered)

	if parseErr != nil {
		return nil, parseErr
	}

	checksum := sha256Hex(rendered)
	propertiesJSON, marshalErr := json.Marshal(parsed.Properties)

	if marshalErr != nil {
		return nil, fmt.Errorf("node: marshal properties: %w", marshalErr)
	}

	if upsertErr := service.repo.Upsert(index.NodeRow{
		ID:             parsed.ID,
		Type:           parsed.Type,
		Path:           parsed.Path,
		Title:          parsed.Title,
		PropertiesJSON: string(propertiesJSON),
		LastMtime:      stat.ModTime().UnixNano(),
		LastSize:       stat.Size(),
		LastChecksum:   checksum,
	}); upsertErr != nil {
		return nil, upsertErr
	}

	return parsed, nil
}

// Get loads a node by id, reading the file from disk.
func (service *Service) Get(nodeID string) (*Node, error) {
	row, getErr := service.repo.Get(nodeID)

	if getErr != nil {
		return nil, getErr
	}

	content, readErr := os.ReadFile(filepath.Join(service.root, row.Path))

	if readErr != nil {
		return nil, fmt.Errorf("node: read %s: %w", row.Path, readErr)
	}

	return ParseFile(row.Path, content)
}

// List returns nodes from the index matching filter. Bodies are not loaded.
func (service *Service) List(filter ListFilter) ([]Node, error) {
	rows, listErr := service.repo.List(index.ListFilter{Type: filter.Type})

	if listErr != nil {
		return nil, listErr
	}

	results := make([]Node, 0, len(rows))

	for _, row := range rows {
		results = append(results, Node{
			ID:    row.ID,
			Path:  row.Path,
			Type:  row.Type,
			Title: row.Title,
		})
	}

	return results, nil
}

// renderMarkdown serializes properties as YAML frontmatter and concatenates body.
func renderMarkdown(properties map[string]any, body []byte) ([]byte, error) {
	var builder strings.Builder

	builder.WriteString("---\n")

	// Render `type` first, then `title`, then remaining keys in insertion order
	// for stable output. We rely on the small property set in v1; a sorted-by-key
	// pass is added if/when ordering becomes meaningful for diffs.
	if typeValue, hasType := properties["type"].(string); hasType {
		builder.WriteString("type: ")
		builder.WriteString(typeValue)
		builder.WriteString("\n")
	}

	if titleValue, hasTitle := properties["title"].(string); hasTitle && titleValue != "" {
		builder.WriteString("title: ")
		builder.WriteString(titleValue)
		builder.WriteString("\n")
	}

	for key, value := range properties {
		if key == "type" || key == "title" {
			continue
		}

		switch typed := value.(type) {
		case string:
			builder.WriteString(key)
			builder.WriteString(": ")
			builder.WriteString(typed)
			builder.WriteString("\n")
		case int:
			builder.WriteString(key)
			builder.WriteString(": ")
			builder.WriteString(fmt.Sprintf("%d", typed))
			builder.WriteString("\n")
		case bool:
			builder.WriteString(key)
			builder.WriteString(": ")
			builder.WriteString(fmt.Sprintf("%t", typed))
			builder.WriteString("\n")
		default:
			return nil, fmt.Errorf("node: unsupported frontmatter type for %s: %T (Plan 1b supports string/int/bool only)", key, value)
		}
	}

	builder.WriteString("---\n\n")
	builder.Write(body)

	if !strings.HasSuffix(string(body), "\n") {
		builder.WriteString("\n")
	}

	return []byte(builder.String()), nil
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)

	return hex.EncodeToString(sum[:])
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/node/... -v
```

Expected: 7 PASS (4 parse + 3 service).

- [ ] **Step 5: Commit**

```bash
git add internal/node
git commit -m "feat(node): NodeService Create / Get / List with frontmatter rendering"
```

---

## Task 8: Reindex pipeline

**Files:**
- Create: `internal/reindex/reindex.go`, `internal/reindex/reindex_test.go`

- [ ] **Step 1: Write the failing test — `internal/reindex/reindex_test.go`**

```go
package reindex_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/reindex"
)

func TestRun_IndexesAllMarkdownNodes(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "notes/auth.md", "type: note\ntitle: Auth\n", "Body.\n")
	writeNode(test, root, "tickets/fix.md", "type: ticket\ntitle: Fix\n", "Body.\n")
	writeNode(test, root, "ignored.txt", "", "not markdown")

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	repo := index.NewNodeRepo(store)

	report, runErr := reindex.Run(reindex.Config{Root: root, Repo: repo})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.Indexed != 2 {
		test.Errorf("Indexed = %d, want 2", report.Indexed)
	}

	loaded, listErr := repo.List(index.ListFilter{})

	if listErr != nil {
		test.Fatalf("List: %v", listErr)
	}

	if len(loaded) != 2 {
		test.Errorf("len = %d, want 2", len(loaded))
	}
}

func TestRun_SkipsTuskInternalDir(test *testing.T) {
	root := test.TempDir()

	if mkErr := os.MkdirAll(filepath.Join(root, ".tusk"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	if writeErr := os.WriteFile(filepath.Join(root, ".tusk", "fake.md"), []byte("---\ntype: note\n---\n"), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	writeNode(test, root, "real.md", "type: note\n", "Body.\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	repo := index.NewNodeRepo(store)

	_, runErr := reindex.Run(reindex.Config{Root: root, Repo: repo})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	loaded, _ := repo.List(index.ListFilter{})

	if len(loaded) != 1 || loaded[0].ID != "real" {
		test.Errorf("unexpected: %+v", loaded)
	}
}

func TestRun_RemovesStaleEntries(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "stale.md", "type: note\n", "Body.\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	repo := index.NewNodeRepo(store)

	if _, runErr := reindex.Run(reindex.Config{Root: root, Repo: repo}); runErr != nil {
		test.Fatalf("first Run: %v", runErr)
	}

	if rmErr := os.Remove(filepath.Join(root, "stale.md")); rmErr != nil {
		test.Fatalf("rm: %v", rmErr)
	}

	report, runErr := reindex.Run(reindex.Config{Root: root, Repo: repo})

	if runErr != nil {
		test.Fatalf("second Run: %v", runErr)
	}

	if report.Removed != 1 {
		test.Errorf("Removed = %d, want 1", report.Removed)
	}

	if _, getErr := repo.Get("stale"); getErr != index.ErrNodeNotFound {
		test.Errorf("err = %v, want ErrNodeNotFound", getErr)
	}
}

func writeNode(test *testing.T, root, relPath, frontmatter, body string) {
	test.Helper()

	abs := filepath.Join(root, relPath)

	if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	content := "---\n" + frontmatter + "---\n\n" + body

	if filepath.Ext(relPath) != ".md" {
		content = body
	}

	if writeErr := os.WriteFile(abs, []byte(content), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Expected: FAIL — package not found.

- [ ] **Step 3: Implement — `internal/reindex/reindex.go`**

```go
// Package reindex walks a workspace, parses every markdown node, and brings the
// index up to date with what is on disk.
package reindex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/node"
)

// Config configures Run.
type Config struct {
	Root string           // workspace root
	Repo *index.NodeRepo  // index repository
}

// Report summarizes a reindex pass.
type Report struct {
	Indexed int // number of node files freshly indexed or refreshed
	Removed int // number of stale rows deleted (file no longer on disk)
	Skipped int // number of files skipped (parse error or off-schema)
}

// Run walks Root, parses every *.md file with valid frontmatter, and upserts
// or removes index rows so the index matches what is on disk.
func Run(config Config) (*Report, error) {
	report := &Report{}
	seen := map[string]struct{}{}

	walkErr := filepath.WalkDir(config.Root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			if shouldSkipDir(config.Root, path, entry.Name()) {
				return filepath.SkipDir
			}

			return nil
		}

		if filepath.Ext(path) != ".md" {
			return nil
		}

		relPath, relErr := filepath.Rel(config.Root, path)

		if relErr != nil {
			return relErr
		}

		// Normalize to forward slashes for cross-platform IDs.
		relPath = filepath.ToSlash(relPath)

		content, readErr := os.ReadFile(path)

		if readErr != nil {
			return fmt.Errorf("reindex: read %s: %w", path, readErr)
		}

		parsed, parseErr := node.ParseFile(relPath, content)

		if parseErr != nil {
			report.Skipped++

			return nil
		}

		stat, statErr := entry.Info()

		if statErr != nil {
			return fmt.Errorf("reindex: stat %s: %w", path, statErr)
		}

		propertiesJSON, marshalErr := json.Marshal(parsed.Properties)

		if marshalErr != nil {
			return fmt.Errorf("reindex: marshal %s: %w", relPath, marshalErr)
		}

		checksum := sha256.Sum256(content)

		if upsertErr := config.Repo.Upsert(index.NodeRow{
			ID:             parsed.ID,
			Type:           parsed.Type,
			Path:           parsed.Path,
			Title:          parsed.Title,
			PropertiesJSON: string(propertiesJSON),
			LastMtime:      stat.ModTime().UnixNano(),
			LastSize:       stat.Size(),
			LastChecksum:   hex.EncodeToString(checksum[:]),
		}); upsertErr != nil {
			return upsertErr
		}

		seen[parsed.Path] = struct{}{}
		report.Indexed++

		return nil
	})

	if walkErr != nil {
		return nil, fmt.Errorf("reindex: walk: %w", walkErr)
	}

	existingRows, listErr := config.Repo.List(index.ListFilter{})

	if listErr != nil {
		return nil, listErr
	}

	for _, row := range existingRows {
		if _, kept := seen[row.Path]; kept {
			continue
		}

		if deleteErr := config.Repo.DeleteByPath(row.Path); deleteErr != nil {
			return nil, deleteErr
		}

		report.Removed++
	}

	return report, nil
}

// shouldSkipDir returns true for directories the walker must not descend into.
// Plan 1b only skips .tusk and .git; .gitignore parsing arrives in Plan 3.
func shouldSkipDir(root, dirPath, name string) bool {
	if dirPath == root {
		return false
	}

	switch name {
	case ".tusk", ".git":
		return true
	}

	if strings.HasPrefix(name, ".") {
		// Skip hidden directories by default; users can opt in via inline manifest later.
		return true
	}

	return false
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/reindex/... -v
```

Expected: 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/reindex
git commit -m "feat(reindex): walk workspace, upsert/remove index rows"
```

---

## Task 9: `tusk init` command

**Files:**
- Create: `cmd/tusk/cmd_init.go`
- Modify: `cmd/tusk/root.go` (register the subcommand)

- [ ] **Step 1: Write the failing test — `cmd/tusk/cmd_init_test.go`**

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestInitCmd_CreatesManifestAndIndex(test *testing.T) {
	tmpDir := test.TempDir()
	original, getCwdErr := os.Getwd()

	if getCwdErr != nil {
		test.Fatalf("Getwd: %v", getCwdErr)
	}

	test.Cleanup(func() { os.Chdir(original) })

	if chdirErr := os.Chdir(tmpDir); chdirErr != nil {
		test.Fatalf("Chdir: %v", chdirErr)
	}

	rootCmd := newRootCmd()
	output := &bytes.Buffer{}
	rootCmd.SetOut(output)
	rootCmd.SetErr(output)
	rootCmd.SetArgs([]string{"init", "--name", "test-vault"})

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("Execute: %v", execErr)
	}

	if _, statErr := os.Stat(filepath.Join(tmpDir, "tusk.toml")); statErr != nil {
		test.Errorf("tusk.toml not created: %v", statErr)
	}

	if _, statErr := os.Stat(filepath.Join(tmpDir, ".tusk", "index.db")); statErr != nil {
		test.Errorf(".tusk/index.db not created: %v", statErr)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./cmd/tusk/...
```

Expected: FAIL — `init` subcommand unknown.

- [ ] **Step 3: Implement — `cmd/tusk/cmd_init.go`**

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

const defaultManifestTemplate = `[workspace]
name = %q
ignore = []
`

const defaultGitignoreEntries = "\n# Tusk local index\n.tusk/\n"

func newInitCmd() *cobra.Command {
	var name string

	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a Tusk workspace in the current directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, getCwdErr := os.Getwd()

			if getCwdErr != nil {
				return getCwdErr
			}

			manifestPath := filepath.Join(cwd, workspace.ManifestFilename)

			if _, statErr := os.Stat(manifestPath); statErr == nil {
				return fmt.Errorf("init: %s already exists", workspace.ManifestFilename)
			}

			if writeErr := os.WriteFile(manifestPath, []byte(fmt.Sprintf(defaultManifestTemplate, name)), 0o644); writeErr != nil {
				return fmt.Errorf("init: write manifest: %w", writeErr)
			}

			indexPath := filepath.Join(cwd, workspace.IndexDirname, workspace.IndexFilename)

			store, openErr := index.Open(indexPath)

			if openErr != nil {
				return fmt.Errorf("init: bootstrap index: %w", openErr)
			}

			store.Close()

			if appendErr := appendGitignore(filepath.Join(cwd, ".gitignore")); appendErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "init: warning: could not update .gitignore: %v\n", appendErr)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Initialized Tusk workspace at %s\n", cwd)

			return nil
		},
	}

	initCmd.Flags().StringVar(&name, "name", "my-brain", "workspace name written into tusk.toml")

	return initCmd
}

// appendGitignore appends Tusk's gitignore stanza if not already present.
// Missing file is fine — a fresh stanza is written.
func appendGitignore(gitignorePath string) error {
	body, readErr := os.ReadFile(gitignorePath)

	if readErr != nil && !os.IsNotExist(readErr) {
		return readErr
	}

	if hasTuskStanza(body) {
		return nil
	}

	updated := append(body, []byte(defaultGitignoreEntries)...)

	return os.WriteFile(gitignorePath, updated, 0o644)
}

func hasTuskStanza(body []byte) bool {
	for _, line := range splitLines(body) {
		if line == ".tusk/" {
			return true
		}
	}

	return false
}

func splitLines(body []byte) []string {
	var lines []string
	var current []byte

	for _, character := range body {
		if character == '\n' {
			lines = append(lines, string(current))
			current = current[:0]

			continue
		}

		current = append(current, character)
	}

	if len(current) > 0 {
		lines = append(lines, string(current))
	}

	return lines
}
```

- [ ] **Step 4: Wire it into root — modify `cmd/tusk/root.go`**

Replace the body of `newRootCmd` so it registers subcommands:

```go
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "tusk",
		Short:         "Tusk — local-first agent brain",
		Long:          "Tusk indexes a markdown vault into a graph and serves structural and semantic queries.",
		Version:       versionString,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	rootCmd.AddCommand(newInitCmd())

	return rootCmd
}
```

- [ ] **Step 5: Run, verify pass**

```bash
go test ./cmd/tusk/...
```

Expected: PASS.

- [ ] **Step 6: Manual smoke**

```bash
make build
mkdir -p /tmp/tusk-smoke && cd /tmp/tusk-smoke && rm -rf tusk.toml .tusk
/workspaces/tusk/bin/tusk init --name smoke
ls
cat tusk.toml
ls .tusk
cd /workspaces/tusk
```

Expected: `tusk.toml` and `.tusk/index.db` exist; manifest contains `name = "smoke"`.

- [ ] **Step 7: Commit**

```bash
git add cmd/tusk
git commit -m "feat(cli): tusk init creates manifest, index, and gitignore stanza"
```

---

## Task 10: `tusk node create` command

**Files:**
- Create: `cmd/tusk/cmd_node.go`, `cmd/tusk/cmd_node_create.go`
- Modify: `cmd/tusk/root.go` (register node subcommand)

- [ ] **Step 1: Write the failing test — `cmd/tusk/cmd_node_create_test.go`**

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestNodeCreateCmd_WritesFile(test *testing.T) {
	tmpDir := initWorkspace(test)

	output := &bytes.Buffer{}

	rootCmd := newRootCmd()
	rootCmd.SetOut(output)
	rootCmd.SetErr(output)
	rootCmd.SetArgs([]string{"node", "create", "--type", "note", "--title", "Hello", "--path", "notes/hello.md"})

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("Execute: %v\noutput: %s", execErr, output.String())
	}

	body, readErr := os.ReadFile(filepath.Join(tmpDir, "notes/hello.md"))

	if readErr != nil {
		test.Fatalf("read: %v", readErr)
	}

	if !bytes.Contains(body, []byte("type: note")) || !bytes.Contains(body, []byte("title: Hello")) {
		test.Errorf("body missing expected frontmatter:\n%s", string(body))
	}
}

func initWorkspace(test *testing.T) string {
	test.Helper()

	tmpDir := test.TempDir()
	originalCwd, getCwdErr := os.Getwd()

	if getCwdErr != nil {
		test.Fatalf("Getwd: %v", getCwdErr)
	}

	test.Cleanup(func() { os.Chdir(originalCwd) })

	if chdirErr := os.Chdir(tmpDir); chdirErr != nil {
		test.Fatalf("Chdir: %v", chdirErr)
	}

	rootCmd := newRootCmd()
	rootCmd.SetArgs([]string{"init", "--name", "test"})

	if execErr := rootCmd.Execute(); execErr != nil {
		test.Fatalf("init: %v", execErr)
	}

	return tmpDir
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./cmd/tusk/... -run TestNodeCreateCmd
```

Expected: FAIL — `node` subcommand unknown.

- [ ] **Step 3: Implement parent — `cmd/tusk/cmd_node.go`**

```go
package main

import "github.com/spf13/cobra"

func newNodeCmd() *cobra.Command {
	nodeCmd := &cobra.Command{
		Use:   "node",
		Short: "Manage individual nodes (create, get, list)",
	}

	nodeCmd.AddCommand(newNodeCreateCmd())
	nodeCmd.AddCommand(newNodeGetCmd())
	nodeCmd.AddCommand(newNodeListCmd())

	return nodeCmd
}
```

- [ ] **Step 4: Implement create — `cmd/tusk/cmd_node_create.go`**

```go
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newNodeCreateCmd() *cobra.Command {
	var (
		nodeType string
		title    string
		relPath  string
	)

	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new node file and index it",
		RunE: func(cmd *cobra.Command, args []string) error {
			if relPath == "" {
				return fmt.Errorf("--path is required")
			}

			if nodeType == "" {
				return fmt.Errorf("--type is required")
			}

			cwd, getCwdErr := os.Getwd()

			if getCwdErr != nil {
				return getCwdErr
			}

			workspace, findErr := workspace.Find(cwd)

			if findErr != nil {
				return fmt.Errorf("workspace: %w", findErr)
			}

			store, openErr := index.Open(workspace.IndexPath)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			service := node.NewService(workspace.Root, index.NewNodeRepo(store))

			body, readErr := readBodyOrEmpty(cmd.InOrStdin())

			if readErr != nil {
				return readErr
			}

			created, createErr := service.Create(node.CreateInput{
				RelPath: relPath,
				Type:    nodeType,
				Title:   title,
				Body:    body,
			})

			if createErr != nil {
				return createErr
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Created %s (id=%s)\n", created.Path, created.ID)

			return nil
		},
	}

	createCmd.Flags().StringVar(&nodeType, "type", "", "node type (e.g. ticket, note)")
	createCmd.Flags().StringVar(&title, "title", "", "optional node title")
	createCmd.Flags().StringVar(&relPath, "path", "", "workspace-relative path with extension (e.g. notes/hello.md)")

	return createCmd
}

// readBodyOrEmpty reads markdown body from stdin if there is piped data; returns
// an empty body otherwise. (Plan 1b accepts an empty body.)
func readBodyOrEmpty(stdin io.Reader) ([]byte, error) {
	stat, statOK := stdin.(*os.File)

	if !statOK {
		return []byte(""), nil
	}

	fileInfo, fileErr := stat.Stat()

	if fileErr != nil {
		return []byte(""), nil
	}

	// If stdin is a terminal (character device), no piped body.
	if (fileInfo.Mode() & os.ModeCharDevice) != 0 {
		return []byte(""), nil
	}

	body, readErr := io.ReadAll(stat)

	if readErr != nil {
		return nil, readErr
	}

	return body, nil
}
```

- [ ] **Step 5: Wire into root**

Edit `cmd/tusk/root.go` to add `rootCmd.AddCommand(newNodeCmd())` alongside the existing `newInitCmd()` registration.

- [ ] **Step 6: Run, verify pass**

```bash
go test ./cmd/tusk/...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/tusk
git commit -m "feat(cli): tusk node create writes file and indexes it"
```

---

## Task 11: `tusk node get` and `tusk node list` (provisional stubs)

Implement the get/list commands referenced by `cmd_node.go` so it compiles. These are tiny passthroughs to the service.

**Files:**
- Create: `cmd/tusk/cmd_node_get.go`, `cmd/tusk/cmd_node_list.go`, `cmd/tusk/cmd_node_get_test.go`, `cmd/tusk/cmd_node_list_test.go`

- [ ] **Step 1: Write tests for get — `cmd/tusk/cmd_node_get_test.go`**

```go
package main

import (
	"bytes"
	"testing"
)

func TestNodeGetCmd_PrintsFrontmatterAndBody(test *testing.T) {
	initWorkspace(test)

	create := newRootCmd()
	create.SetArgs([]string{"node", "create", "--type", "note", "--title", "Hi", "--path", "x.md"})

	if execErr := create.Execute(); execErr != nil {
		test.Fatalf("create: %v", execErr)
	}

	output := &bytes.Buffer{}

	getCmd := newRootCmd()
	getCmd.SetOut(output)
	getCmd.SetErr(output)
	getCmd.SetArgs([]string{"node", "get", "x"})

	if execErr := getCmd.Execute(); execErr != nil {
		test.Fatalf("get: %v\noutput: %s", execErr, output.String())
	}

	if !bytes.Contains(output.Bytes(), []byte("type: note")) {
		test.Errorf("output missing type: %s", output.String())
	}

	if !bytes.Contains(output.Bytes(), []byte("title: Hi")) {
		test.Errorf("output missing title: %s", output.String())
	}
}
```

- [ ] **Step 2: Write tests for list — `cmd/tusk/cmd_node_list_test.go`**

```go
package main

import (
	"bytes"
	"testing"
)

func TestNodeListCmd_PrintsCreatedNodes(test *testing.T) {
	initWorkspace(test)

	first := newRootCmd()
	first.SetArgs([]string{"node", "create", "--type", "note", "--path", "a.md"})

	if execErr := first.Execute(); execErr != nil {
		test.Fatalf("first: %v", execErr)
	}

	second := newRootCmd()
	second.SetArgs([]string{"node", "create", "--type", "ticket", "--path", "b.md"})

	if execErr := second.Execute(); execErr != nil {
		test.Fatalf("second: %v", execErr)
	}

	output := &bytes.Buffer{}

	listCmd := newRootCmd()
	listCmd.SetOut(output)
	listCmd.SetErr(output)
	listCmd.SetArgs([]string{"node", "list"})

	if execErr := listCmd.Execute(); execErr != nil {
		test.Fatalf("list: %v", execErr)
	}

	if !bytes.Contains(output.Bytes(), []byte("a")) || !bytes.Contains(output.Bytes(), []byte("b")) {
		test.Errorf("missing rows: %s", output.String())
	}
}

func TestNodeListCmd_FiltersByType(test *testing.T) {
	initWorkspace(test)

	for _, args := range [][]string{
		{"node", "create", "--type", "note", "--path", "a.md"},
		{"node", "create", "--type", "ticket", "--path", "b.md"},
	} {
		cmd := newRootCmd()
		cmd.SetArgs(args)

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("create: %v", execErr)
		}
	}

	output := &bytes.Buffer{}

	listCmd := newRootCmd()
	listCmd.SetOut(output)
	listCmd.SetErr(output)
	listCmd.SetArgs([]string{"node", "list", "--type", "ticket"})

	if execErr := listCmd.Execute(); execErr != nil {
		test.Fatalf("list: %v", execErr)
	}

	body := output.String()

	if bytes.Contains(output.Bytes(), []byte(" a ")) || bytes.Contains(output.Bytes(), []byte("\ta\t")) {
		test.Errorf("expected only ticket in output, got: %s", body)
	}

	if !bytes.Contains(output.Bytes(), []byte("b")) {
		test.Errorf("missing b: %s", body)
	}
}
```

- [ ] **Step 3: Run, verify fail**

```bash
go test ./cmd/tusk/...
```

Expected: FAIL — `newNodeGetCmd` / `newNodeListCmd` unknown.

- [ ] **Step 4: Implement get — `cmd/tusk/cmd_node_get.go`**

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newNodeGetCmd() *cobra.Command {
	getCmd := &cobra.Command{
		Use:   "get <node-id>",
		Short: "Print the markdown file for a node by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, cwdErr := os.Getwd()

			if cwdErr != nil {
				return cwdErr
			}

			ws, findErr := workspace.Find(cwd)

			if findErr != nil {
				return fmt.Errorf("workspace: %w", findErr)
			}

			store, openErr := index.Open(ws.IndexPath)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			service := node.NewService(ws.Root, index.NewNodeRepo(store))

			loaded, getErr := service.Get(args[0])

			if getErr != nil {
				return getErr
			}

			// Print the on-disk markdown verbatim so the output matches what
			// any other tool reading the file would see.
			rendered, renderErr := os.ReadFile(filepath.Join(ws.Root, loaded.Path))

			if renderErr != nil {
				return renderErr
			}

			fmt.Fprint(cmd.OutOrStdout(), string(rendered))

			return nil
		},
	}

	return getCmd
}
```

- [ ] **Step 5: Implement list — `cmd/tusk/cmd_node_list.go`**

```go
package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newNodeListCmd() *cobra.Command {
	var typeFilter string

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List nodes from the index",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, cwdErr := os.Getwd()

			if cwdErr != nil {
				return cwdErr
			}

			ws, findErr := workspace.Find(cwd)

			if findErr != nil {
				return fmt.Errorf("workspace: %w", findErr)
			}

			store, openErr := index.Open(ws.IndexPath)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			service := node.NewService(ws.Root, index.NewNodeRepo(store))

			nodes, listErr := service.List(node.ListFilter{Type: typeFilter})

			if listErr != nil {
				return listErr
			}

			tab := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

			fmt.Fprintln(tab, "ID\tTYPE\tTITLE\tPATH")

			for _, item := range nodes {
				fmt.Fprintf(tab, "%s\t%s\t%s\t%s\n", item.ID, item.Type, item.Title, item.Path)
			}

			return tab.Flush()
		},
	}

	listCmd.Flags().StringVar(&typeFilter, "type", "", "filter by node type (exact match)")

	return listCmd
}
```

- [ ] **Step 6: Run, verify pass**

```bash
go test ./cmd/tusk/...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/tusk
git commit -m "feat(cli): tusk node get and tusk node list"
```

---

## Task 12: `tusk reindex` command

**Files:**
- Create: `cmd/tusk/cmd_reindex.go`, `cmd/tusk/cmd_reindex_test.go`

- [ ] **Step 1: Write the failing test — `cmd/tusk/cmd_reindex_test.go`**

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestReindexCmd_PicksUpExternalFile(test *testing.T) {
	tmpDir := initWorkspace(test)

	external := filepath.Join(tmpDir, "external.md")
	body := []byte("---\ntype: note\ntitle: External\n---\n\nbody.\n")

	if writeErr := os.WriteFile(external, body, 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	output := &bytes.Buffer{}

	reindexCmd := newRootCmd()
	reindexCmd.SetOut(output)
	reindexCmd.SetErr(output)
	reindexCmd.SetArgs([]string{"reindex"})

	if execErr := reindexCmd.Execute(); execErr != nil {
		test.Fatalf("reindex: %v", execErr)
	}

	listOutput := &bytes.Buffer{}

	listCmd := newRootCmd()
	listCmd.SetOut(listOutput)
	listCmd.SetErr(listOutput)
	listCmd.SetArgs([]string{"node", "list"})

	if execErr := listCmd.Execute(); execErr != nil {
		test.Fatalf("list: %v", execErr)
	}

	if !bytes.Contains(listOutput.Bytes(), []byte("external")) {
		test.Errorf("missing external in list:\n%s", listOutput.String())
	}
}
```

- [ ] **Step 2: Run, verify fail**

Expected: FAIL — `reindex` unknown.

- [ ] **Step 3: Implement — `cmd/tusk/cmd_reindex.go`**

```go
package main

import (
	"fmt"
	"os"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newReindexCmd() *cobra.Command {
	reindexCmd := &cobra.Command{
		Use:   "reindex",
		Short: "Walk the workspace and bring the index up to date with disk",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, cwdErr := os.Getwd()

			if cwdErr != nil {
				return cwdErr
			}

			ws, findErr := workspace.Find(cwd)

			if findErr != nil {
				return fmt.Errorf("workspace: %w", findErr)
			}

			store, openErr := index.Open(ws.IndexPath)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			report, runErr := reindex.Run(reindex.Config{
				Root: ws.Root,
				Repo: index.NewNodeRepo(store),
			})

			if runErr != nil {
				return runErr
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Reindex done: %d indexed, %d removed, %d skipped\n", report.Indexed, report.Removed, report.Skipped)

			return nil
		},
	}

	return reindexCmd
}
```

- [ ] **Step 4: Wire into root — modify `cmd/tusk/root.go`**

Add `rootCmd.AddCommand(newReindexCmd())` alongside the other registrations.

- [ ] **Step 5: Run, verify pass**

```bash
go test ./cmd/tusk/...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/tusk
git commit -m "feat(cli): tusk reindex picks up external file changes"
```

---

## Task 13: End-to-end smoke test

**Files:**
- Create: `cmd/tusk/e2e_test.go`

- [ ] **Step 1: Write the e2e test — `cmd/tusk/e2e_test.go`**

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestE2E_FullLifecycle(test *testing.T) {
	tmpDir := initWorkspace(test)

	// 1) Create a node via CLI.
	{
		cmd := newRootCmd()
		cmd.SetArgs([]string{"node", "create", "--type", "ticket", "--title", "Fix bug", "--path", "tickets/fix-bug.md"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("create: %v", execErr)
		}
	}

	// 2) Drop a second node externally (no CLI).
	external := filepath.Join(tmpDir, "notes/random.md")

	if mkErr := os.MkdirAll(filepath.Dir(external), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	body := []byte("---\ntype: note\ntitle: Random\n---\n\nBody.\n")

	if writeErr := os.WriteFile(external, body, 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	// 3) Reindex picks it up.
	{
		cmd := newRootCmd()
		cmd.SetArgs([]string{"reindex"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("reindex: %v", execErr)
		}
	}

	// 4) List shows both.
	listOut := &bytes.Buffer{}
	{
		cmd := newRootCmd()
		cmd.SetOut(listOut)
		cmd.SetErr(listOut)
		cmd.SetArgs([]string{"node", "list"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("list: %v", execErr)
		}
	}

	output := listOut.String()

	if !bytes.Contains([]byte(output), []byte("tickets/fix-bug")) {
		test.Errorf("missing fix-bug:\n%s", output)
	}

	if !bytes.Contains([]byte(output), []byte("notes/random")) {
		test.Errorf("missing random:\n%s", output)
	}

	// 5) Get the externally-created one.
	getOut := &bytes.Buffer{}
	{
		cmd := newRootCmd()
		cmd.SetOut(getOut)
		cmd.SetErr(getOut)
		cmd.SetArgs([]string{"node", "get", "notes/random"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("get: %v", execErr)
		}
	}

	if !bytes.Contains(getOut.Bytes(), []byte("title: Random")) {
		test.Errorf("get output missing title:\n%s", getOut.String())
	}

	// 6) Delete the external file and reindex; list no longer shows it.
	if rmErr := os.Remove(external); rmErr != nil {
		test.Fatalf("rm: %v", rmErr)
	}

	{
		cmd := newRootCmd()
		cmd.SetArgs([]string{"reindex"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("second reindex: %v", execErr)
		}
	}

	listOut.Reset()

	{
		cmd := newRootCmd()
		cmd.SetOut(listOut)
		cmd.SetErr(listOut)
		cmd.SetArgs([]string{"node", "list"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("second list: %v", execErr)
		}
	}

	if bytes.Contains(listOut.Bytes(), []byte("notes/random")) {
		test.Errorf("random should be gone after delete + reindex:\n%s", listOut.String())
	}
}
```

- [ ] **Step 2: Run**

```bash
go test ./cmd/tusk/... -v -run E2E
```

Expected: PASS.

- [ ] **Step 3: Run the full suite**

```bash
make test
make vet
```

Expected: all PASS, exit 0.

- [ ] **Step 4: Manual smoke from a clean checkout**

```bash
make build
SMOKE=/tmp/tusk-1b-smoke
rm -rf $SMOKE && mkdir -p $SMOKE && cd $SMOKE

/workspaces/tusk/bin/tusk init --name smoke
/workspaces/tusk/bin/tusk node create --type ticket --title "Fix login" --path tickets/fix-login.md
/workspaces/tusk/bin/tusk node create --type note    --title "Auth RFC"  --path notes/auth-rfc.md

cat <<'EOF' > notes/external.md
---
type: note
title: External
---

External body.
EOF

/workspaces/tusk/bin/tusk reindex
/workspaces/tusk/bin/tusk node list
/workspaces/tusk/bin/tusk node list --type note
/workspaces/tusk/bin/tusk node get notes/external

cd /workspaces/tusk
```

Expected: every command succeeds; `node list` shows three nodes (`tickets/fix-login`, `notes/auth-rfc`, `notes/external`); `--type note` shows two; `get` prints the external file's frontmatter and body.

- [ ] **Step 5: Commit**

```bash
git add cmd/tusk
git commit -m "test(cli): end-to-end lifecycle smoke covering create / get / list / reindex"
```

---

## Task 14: Final verification + push

**Files:** none

- [ ] **Step 1: Full test suite + lint**

```bash
make test
make vet
make lint
```

Expected: all exit 0.

- [ ] **Step 2: Inspect commit graph**

```bash
git log v1..HEAD --oneline
```

Expected: 14 commits matching the tasks above.

- [ ] **Step 3: Push branch**

```bash
git push origin v1
```

PR #351 (already open as draft) automatically picks up the new commits.

- [ ] **Step 4: Update PR body checklist**

Use `gh pr edit 351 --body` (or via the GitHub UI) to flip Plan 1b's checkbox in the "Plans landing on this branch" list:

```
- [x] Plan 1a — v0 cleanup + v1 branch setup
- [x] Plan 1b — first node lifecycle (`tusk init`, `node create`/`get`/`list`, `reindex`)
- [ ] Plan 2 — edges + relationships
...
```

- [ ] **Step 5: Verify PR**

```bash
gh pr view 351 --json state,isDraft,headRefName,commits
```

Expected: state OPEN, isDraft true, head v1, commit count >= 17 (3 from Plan 1a + 14 from Plan 1b).

---

## Self-Review Checklist

Run after writing the plan, before execution begins.

**Spec coverage:**
- [ ] Workspace layout (§5.1) — directory + `.tusk/` covered by Task 9 (`init`)
- [ ] File naming and identity (§5.2) — id-from-path covered by Task 6 (parser) and Task 7 (service)
- [ ] Frontmatter (§6.1, YAML 1.2) — covered by Task 6 (yaml.v3)
- [ ] Reserved keys `type` (§6.2) — covered by Task 6 (`ErrMissingType`)
- [ ] Manifest TOML (§7.1) — covered by Task 3 (loader)
- [ ] Index schema (§9.5) — Plan 1b ships `nodes`, `manifest_snapshot`, `warnings` in Task 4. `edges`, `embeddings`, `embed_queue` come in later plans.
- [ ] Bootstrap (§9.6) — `tusk init` (Task 9) creates `tusk.toml`, `.tusk/index.db`, gitignore stanza
- [ ] Reindex (§9.3) — covered by Task 8 + Task 12. Drift detection via mtime + checksum is per spec; full implementation lands here. `--no-embed` flag is unused in 1b (no embeddings yet); add it as a hidden no-op so Plan 5 can wire it without surface change.

**Out-of-scope guardrails:**
- [ ] No edges in 1b — Task 6's parser ignores edge-shaped frontmatter keys (treats them as opaque properties). Plan 2 adds explicit edge handling.
- [ ] No filter grammar — `node list --type` is the only filter.
- [ ] No watcher — Task 8 is one-shot reindex only.
- [ ] No embeddings — Task 4's schema does not create the `embeddings`/`embed_queue` tables.
- [ ] No behaviors — workflow validation is absent; status changes via `node modify` are NOT implemented in 1b.

**Plan-shape:**
- [ ] No "TBD"/"TODO"/"fill in" placeholders.
- [ ] Every step has either complete code or an exact command.
- [ ] Every task ends with a commit step.
- [ ] Test code uses `test *testing.T` (≥ 2-character names per STYLE.md).
- [ ] Implementation code has blank lines around `if err != nil` guards (STYLE.md rule 2).

**Type/name consistency:**
- [ ] `workspace.Find` returns `*Workspace` with `Root`, `ManifestPath`, `IndexPath` — used uniformly across `cmd_init`, `cmd_node_create`, `cmd_node_get`, `cmd_node_list`, `cmd_reindex`.
- [ ] `index.NodeRow` field names match between Task 5 (definition) and Task 7 (consumer in `node.Service.Create`).
- [ ] `node.ParseFile(relPath, content)` signature is consistent across Task 6 (definition), Task 7 (service), Task 8 (reindex).
- [ ] `index.NewNodeRepo(store)` signature is consistent across all consumers.
