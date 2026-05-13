---
type: plan
title: Plan 2
status: shipped
pr: 353
shipped-at: "2026-05-05"
implements:
  - Tusk v1 Rebuild
---

# Tusk v1 — Plan 2: Edges + Relationships

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn isolated nodes into a graph. After this plan, frontmatter edge keys (declared in the manifest) and body wikilinks materialize as typed `Edge` rows in the index; `tusk edge add/remove/list` operates on edges; `NodeService.Create` and `reindex.Run` write edges with manifest-driven legality and cycle-detection enforcement.

**Architecture:** Extends Plan 1b's substrate. Manifest gains `[edge-types.<name>]` blocks declaring `from`/`to`/`cardinality`/`ordered`/`inverse`/`acyclic`. The index gains an `edges` table. Parsing stays pure (`ParseFile` → `Node` with a single `Properties` map). A new `node.ResolveEdges(node, edgeRegistry)` step splits edge-shaped frontmatter keys into `Node.Edges` and scans the body for `[[id]]` wikilinks, materializing `references` edges when that type is declared. `NodeService.Create` and `reindex.Run` invoke the resolver, then `EdgeRepo.UpsertAll` to persist. Legality and cycle detection live in `internal/node/edges.go` as pure validators called before persistence.

**Tech Stack:** Same as Plan 1b — Go 1.26, Cobra, BurntSushi/toml, modernc.org/sqlite, gopkg.in/yaml.v3. No new external dependencies.

**Spec reference:** `docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md` §6.3 (frontmatter properties vs edges), §6.4 (wikilinks), §7.4 (edge type declarations), §9.5 (index schema), §13.1 (filter grammar — only the structural shape is needed here; full grammar is Plan 4).

**Style rules:** All code respects `STYLE.md` — minimum 2-character identifiers (`*testing.T` → `test *testing.T`), blank lines around `err` guards, named errors on shadow.

---

## File Structure

**Created:**
```
internal/index/edge_repo.go         # EdgeRepo: UpsertAll, ListBySource, ListByTarget, ListByType, DeleteByEdge, DeleteBySource
internal/index/edge_repo_test.go
internal/node/edges.go              # Edge type constants + ResolveEdges + ValidateEdges + DetectCycle
internal/node/edges_test.go
internal/node/wikilinks.go          # ExtractWikilinks (regex over body)
internal/node/wikilinks_test.go
cmd/tusk/cmd_edge.go                # parent `edge` cobra group
cmd/tusk/cmd_edge_add.go            # tusk edge add
cmd/tusk/cmd_edge_remove.go         # tusk edge remove
cmd/tusk/cmd_edge_list.go           # tusk edge list
cmd/tusk/cmd_edge_add_test.go
cmd/tusk/cmd_edge_remove_test.go
cmd/tusk/cmd_edge_list_test.go
cmd/tusk/e2e_edges_test.go          # Plan 2's end-to-end smoke
```

**Modified:**
```
internal/manifest/manifest.go       # add EdgeType + EdgeTypes map; cardinality enum constants
internal/manifest/loader.go         # validate edge-types block (cardinality enum, non-empty from/to, no name conflicts with property keys)
internal/manifest/loader_test.go    # cover edge-types parsing + validation errors
internal/index/index.go             # extend schema with `edges` table + indexes
internal/index/index_test.go        # assert `edges` table present
internal/node/node.go               # add Edges field to Node
internal/node/parse.go              # unchanged signature; ParseFile keeps everything in Properties as before (resolution is a separate step)
internal/node/service.go            # Create resolves edges, validates, persists; Get/List unaffected for v1b but Get returns Edges map
internal/node/service_test.go       # cover edge writing through Create
internal/reindex/reindex.go         # walk → ParseFile → ResolveEdges → ValidateEdges → upsert NodeRow + edges
internal/reindex/reindex_test.go    # cover edges-from-disk
cmd/tusk/root.go                    # register newEdgeCmd
```

**Excluded for Plan 2** (lands in later plans):
- `tags: [...]` shorthand and tag-node auto-creation — **Plan 7** (type pack territory; the `kanban`/`vault`/`tags` packs own this).
- File watcher, advisory lockfile, rename-rewrite pipeline — **Plan 3**.
- Filter grammar (`<edge-name>->`, `<-`, multi-hop) — **Plan 4**. `tusk edge list` accepts only flat `--from`/`--to`/`--type` filters.
- Inverse edge derivation in queries — **Plan 4**. The `inverse` field is parsed and stored but not auto-exposed.
- `references` edge writes for wikilinks pointing to non-existent nodes (dangling refs surface as warnings) — minimal behavior in Plan 2; full doctor output in **Plan 8**.

## Module-level Conventions for Plan 2

These conventions are referenced across multiple tasks; defining them once keeps Tasks consistent.

### Edge type registry

The manifest's edge types are exposed by `internal/manifest` as a registry the parser can query:

```go
// EdgeTypes maps edge-type-name → EdgeType definition.
type EdgeTypes map[string]EdgeType
```

This registry is constructed once when the manifest loads and passed wherever edges are resolved.

### Edge value shapes in frontmatter

A frontmatter key whose name matches a declared edge type may carry one of:

- **A scalar string** — single target id (e.g., `parent: tickets/auth-epic`)
- **A YAML sequence of strings** — multiple targets, ordered or unordered per the edge type's `ordered` flag (e.g., `blocks: [tickets/refactor-storage]`)

Other shapes (maps, nested) are rejected by `ResolveEdges` with a typed error so the implementer / agent can fix the file.

### Edge persistence shape

Each frontmatter edge value materializes to one or more rows in the `edges` table:

```
(type, source_id, target_id, ordinal, source_path)
```

`ordinal` is `0` for scalar values and `0…n` for sequences (in declared order). `source_path` is the file declaring the edge (needed for rewrite-on-move in Plan 3).

`UpsertAll` deletes all edges where `source_id = node.id AND source_path = node.path` and re-inserts the new set in one transaction. This makes the operation idempotent on repeated calls (e.g., during reindex of an unchanged file).

---

## Task 0: Pre-flight verification

**Files:** none

- [ ] **Step 1: Confirm on `feat/plan-2` and clean tree**

```bash
git rev-parse --abbrev-ref HEAD
git status --short
git log --oneline -3
```

Expected: branch `feat/plan-2`; only the pre-existing `M .devcontainer/...` and `M .gitignore` (or empty); recent log starts with Plan 1b's tip (`e12ace2 test(cli): end-to-end lifecycle smoke ...`).

- [ ] **Step 2: Confirm Plan 1b tests still pass**

```bash
make test
make vet
```

Expected: all packages green, `vet` clean. Plan 2 builds on Plan 1b — failing prerequisites stop here.

---

## Task 1: Manifest — `EdgeType` schema and registry types

**Files:**
- Modify: `internal/manifest/manifest.go`

- [ ] **Step 1: Read current manifest types**

```bash
cat internal/manifest/manifest.go
```

Note the existing `Manifest` and `WorkspaceSection` types (Plan 1b shipped only those).

- [ ] **Step 2: Replace `internal/manifest/manifest.go` with the extended types**

```go
// Package manifest defines the schema and loader for tusk.toml.
package manifest

// Manifest is the parsed representation of tusk.toml at the workspace root.
type Manifest struct {
	Workspace WorkspaceSection    `toml:"workspace"`
	EdgeTypes map[string]EdgeType `toml:"edge-types"`
}

// WorkspaceSection holds top-level workspace configuration.
type WorkspaceSection struct {
	Name   string   `toml:"name"`
	Ignore []string `toml:"ignore"`
}

// Cardinality enumerates the legal values for EdgeType.Cardinality.
type Cardinality string

const (
	CardinalityOneToOne   Cardinality = "one-to-one"
	CardinalityOneToMany  Cardinality = "one-to-many"
	CardinalityManyToOne  Cardinality = "many-to-one"
	CardinalityManyToMany Cardinality = "many-to-many"
)

// EdgeType is a manifest-declared edge type.
type EdgeType struct {
	Description string      `toml:"description"`
	From        []string    `toml:"from"`        // allowed source node-types; ["*"] means any
	To          []string    `toml:"to"`          // allowed target node-types; ["*"] means any
	Cardinality Cardinality `toml:"cardinality"`
	Ordered     bool        `toml:"ordered"`
	Inverse     string      `toml:"inverse"`     // optional; name of the derived inverse edge
	Acyclic     bool        `toml:"acyclic"`
}

// AllowsSource returns true if sourceType matches the edge type's `from` list
// (literal match or `*` wildcard).
func (edgeType EdgeType) AllowsSource(sourceType string) bool {
	return matchesTypeList(edgeType.From, sourceType)
}

// AllowsTarget returns true if targetType matches the edge type's `to` list.
func (edgeType EdgeType) AllowsTarget(targetType string) bool {
	return matchesTypeList(edgeType.To, targetType)
}

func matchesTypeList(allowed []string, candidate string) bool {
	for _, entry := range allowed {
		if entry == "*" || entry == candidate {
			return true
		}
	}

	return false
}
```

- [ ] **Step 3: Verify package still compiles**

```bash
go build ./internal/manifest/...
```

Expected: exits 0.

- [ ] **Step 4: Commit**

```bash
git add internal/manifest/manifest.go
git commit -m "feat(manifest): EdgeType schema and AllowsSource/AllowsTarget helpers"
```

---

## Task 2: Manifest loader — validate edge-types

**Files:**
- Modify: `internal/manifest/loader.go`, `internal/manifest/loader_test.go`

- [ ] **Step 1: Extend the loader test — append to `internal/manifest/loader_test.go`**

Add these new tests after the existing ones:

```go
func TestLoad_ParsesEdgeTypes(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "my-brain"

[edge-types.parent]
description = "Hierarchical parent"
from = ["ticket", "project"]
to = ["ticket", "project"]
cardinality = "many-to-one"
ordered = false

[edge-types.blocks]
description = "Blocks another"
from = ["ticket"]
to = ["ticket"]
cardinality = "many-to-many"
acyclic = true

[edge-types.references]
description = "Implicit references"
from = ["*"]
to = ["*"]
cardinality = "many-to-many"
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	if len(loaded.EdgeTypes) != 3 {
		test.Fatalf("EdgeTypes len = %d, want 3", len(loaded.EdgeTypes))
	}

	parentType, hasParent := loaded.EdgeTypes["parent"]

	if !hasParent {
		test.Fatalf("parent edge type missing")
	}

	if parentType.Cardinality != manifest.CardinalityManyToOne {
		test.Errorf("parent cardinality = %q", parentType.Cardinality)
	}

	if !parentType.AllowsSource("ticket") {
		test.Errorf("parent should allow ticket source")
	}

	blocksType := loaded.EdgeTypes["blocks"]

	if !blocksType.Acyclic {
		test.Errorf("blocks should be acyclic")
	}

	referencesType := loaded.EdgeTypes["references"]

	if !referencesType.AllowsSource("anything") {
		test.Errorf("references should allow any source via wildcard")
	}
}

func TestLoad_RejectsInvalidCardinality(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "x"

[edge-types.bad]
from = ["ticket"]
to = ["ticket"]
cardinality = "bogus"
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	_, loadErr := manifest.Load(manifestPath)

	if loadErr == nil {
		test.Fatalf("expected error for invalid cardinality")
	}
}

func TestLoad_RejectsEmptyFromOrTo(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "x"

[edge-types.bad]
from = []
to = ["ticket"]
cardinality = "many-to-many"
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	_, loadErr := manifest.Load(manifestPath)

	if loadErr == nil {
		test.Fatalf("expected error for empty from list")
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/manifest/...
```

Expected: FAIL — the loader currently accepts any TOML and doesn't validate edge types.

- [ ] **Step 3: Replace `internal/manifest/loader.go`**

```go
package manifest

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// validCardinalities lists the legal Cardinality values for runtime validation.
var validCardinalities = map[Cardinality]struct{}{
	CardinalityOneToOne:   {},
	CardinalityOneToMany:  {},
	CardinalityManyToOne:  {},
	CardinalityManyToMany: {},
}

// Load reads and decodes a tusk.toml at manifestPath, validating its shape.
func Load(manifestPath string) (*Manifest, error) {
	body, readErr := os.ReadFile(manifestPath)

	if readErr != nil {
		return nil, fmt.Errorf("manifest: read %s: %w", manifestPath, readErr)
	}

	loaded := &Manifest{}

	if _, decodeErr := toml.Decode(string(body), loaded); decodeErr != nil {
		return nil, fmt.Errorf("manifest: decode %s: %w", manifestPath, decodeErr)
	}

	if validateErr := validate(loaded); validateErr != nil {
		return nil, validateErr
	}

	return loaded, nil
}

// validate walks the manifest and surfaces structural problems before they
// reach the engine.
func validate(loaded *Manifest) error {
	for name, edgeType := range loaded.EdgeTypes {
		if _, valid := validCardinalities[edgeType.Cardinality]; !valid {
			return fmt.Errorf("manifest: edge-type %q: invalid cardinality %q (want one-to-one|one-to-many|many-to-one|many-to-many)", name, edgeType.Cardinality)
		}

		if len(edgeType.From) == 0 {
			return fmt.Errorf("manifest: edge-type %q: from list must be non-empty", name)
		}

		if len(edgeType.To) == 0 {
			return fmt.Errorf("manifest: edge-type %q: to list must be non-empty", name)
		}
	}

	return nil
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/manifest/... -v
```

Expected: 6 PASS (3 from Plan 1b + 3 new).

- [ ] **Step 5: Commit**

```bash
git add internal/manifest
git commit -m "feat(manifest): edge-types parsing with cardinality and from/to validation"
```

---

## Task 3: Index — extend schema with `edges` table

**Files:**
- Modify: `internal/index/index.go`, `internal/index/index_test.go`

- [ ] **Step 1: Extend the index test — append to `internal/index/index_test.go`**

Add a single test asserting the `edges` table is present after Open:

```go
func TestOpen_CreatesEdgesTable(test *testing.T) {
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

	if !contains(tables, "edges") {
		test.Errorf("missing edges table in %v", tables)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/index/... -run TestOpen_CreatesEdgesTable
```

Expected: FAIL — edges table not in schema.

- [ ] **Step 3: Extend `internal/index/index.go`**

In `internal/index/index.go`, append the following to the `schema` const string immediately after the existing `nodes_type_idx` definition (before `manifest_snapshot`):

```sql
CREATE TABLE IF NOT EXISTS edges (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	type        TEXT NOT NULL,
	source_id   TEXT NOT NULL,
	target_id   TEXT NOT NULL,
	ordinal     INTEGER NOT NULL DEFAULT 0,
	source_path TEXT NOT NULL,
	UNIQUE(type, source_id, target_id, ordinal)
);

CREATE INDEX IF NOT EXISTS edges_source_idx ON edges(source_id);
CREATE INDEX IF NOT EXISTS edges_target_idx ON edges(target_id);
CREATE INDEX IF NOT EXISTS edges_type_idx   ON edges(type);
CREATE INDEX IF NOT EXISTS edges_source_path_idx ON edges(source_path);
```

The full `schema` const after extension should contain (in order): `nodes`, `nodes_type_idx`, `edges`, the four edges indexes, `manifest_snapshot`, `warnings`.

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/index/... -v
```

Expected: 3 PASS for index_test.go (the original 2 + the new edges table test). NodeRepo tests still pass.

- [ ] **Step 5: Commit**

```bash
git add internal/index/index.go internal/index/index_test.go
git commit -m "feat(index): edges table with source/target/type indexes"
```

---

## Task 4: EdgeRepo — UpsertAll / List* / DeleteBySource

**Files:**
- Create: `internal/index/edge_repo.go`, `internal/index/edge_repo_test.go`

- [ ] **Step 1: Write the failing test — `internal/index/edge_repo_test.go`**

```go
package index_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func newTestEdgeRepo(test *testing.T) *index.EdgeRepo {
	test.Helper()

	store := openTestIndex(test)

	return index.NewEdgeRepo(store)
}

func TestEdgeRepo_UpsertAllAndListBySource(test *testing.T) {
	repo := newTestEdgeRepo(test)

	edges := []index.EdgeRow{
		{Type: "parent", SourceID: "tickets/foo", TargetID: "tickets/epic", Ordinal: 0, SourcePath: "tickets/foo.md"},
		{Type: "blocks", SourceID: "tickets/foo", TargetID: "tickets/bar", Ordinal: 0, SourcePath: "tickets/foo.md"},
	}

	if upsertErr := repo.UpsertAll("tickets/foo", "tickets/foo.md", edges); upsertErr != nil {
		test.Fatalf("UpsertAll: %v", upsertErr)
	}

	listed, listErr := repo.ListBySource("tickets/foo")

	if listErr != nil {
		test.Fatalf("ListBySource: %v", listErr)
	}

	if len(listed) != 2 {
		test.Errorf("len = %d, want 2", len(listed))
	}
}

func TestEdgeRepo_UpsertAllReplacesExistingEdgesForSource(test *testing.T) {
	repo := newTestEdgeRepo(test)

	first := []index.EdgeRow{
		{Type: "parent", SourceID: "x", TargetID: "y", Ordinal: 0, SourcePath: "x.md"},
		{Type: "blocks", SourceID: "x", TargetID: "z", Ordinal: 0, SourcePath: "x.md"},
	}

	repo.UpsertAll("x", "x.md", first)

	second := []index.EdgeRow{
		{Type: "parent", SourceID: "x", TargetID: "y2", Ordinal: 0, SourcePath: "x.md"},
	}

	if upsertErr := repo.UpsertAll("x", "x.md", second); upsertErr != nil {
		test.Fatalf("second UpsertAll: %v", upsertErr)
	}

	listed, _ := repo.ListBySource("x")

	if len(listed) != 1 {
		test.Errorf("len = %d, want 1 after replace", len(listed))
	}

	if listed[0].TargetID != "y2" {
		test.Errorf("Target = %q, want y2", listed[0].TargetID)
	}
}

func TestEdgeRepo_ListByTarget(test *testing.T) {
	repo := newTestEdgeRepo(test)

	repo.UpsertAll("a", "a.md", []index.EdgeRow{
		{Type: "blocks", SourceID: "a", TargetID: "z", Ordinal: 0, SourcePath: "a.md"},
	})

	repo.UpsertAll("b", "b.md", []index.EdgeRow{
		{Type: "blocks", SourceID: "b", TargetID: "z", Ordinal: 0, SourcePath: "b.md"},
	})

	listed, listErr := repo.ListByTarget("z")

	if listErr != nil {
		test.Fatalf("ListByTarget: %v", listErr)
	}

	if len(listed) != 2 {
		test.Errorf("len = %d, want 2", len(listed))
	}
}

func TestEdgeRepo_ListByType(test *testing.T) {
	repo := newTestEdgeRepo(test)

	repo.UpsertAll("a", "a.md", []index.EdgeRow{
		{Type: "blocks", SourceID: "a", TargetID: "x", Ordinal: 0, SourcePath: "a.md"},
		{Type: "parent", SourceID: "a", TargetID: "y", Ordinal: 0, SourcePath: "a.md"},
	})

	listed, listErr := repo.ListByType("blocks")

	if listErr != nil {
		test.Fatalf("ListByType: %v", listErr)
	}

	if len(listed) != 1 || listed[0].TargetID != "x" {
		test.Errorf("listed = %+v", listed)
	}
}

func TestEdgeRepo_DeleteBySource(test *testing.T) {
	repo := newTestEdgeRepo(test)

	repo.UpsertAll("doomed", "doomed.md", []index.EdgeRow{
		{Type: "parent", SourceID: "doomed", TargetID: "x", Ordinal: 0, SourcePath: "doomed.md"},
	})

	if deleteErr := repo.DeleteBySource("doomed"); deleteErr != nil {
		test.Fatalf("DeleteBySource: %v", deleteErr)
	}

	listed, _ := repo.ListBySource("doomed")

	if len(listed) != 0 {
		test.Errorf("len = %d, want 0", len(listed))
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/index/... -run EdgeRepo
```

Expected: FAIL — EdgeRepo not defined.

- [ ] **Step 3: Implement — `internal/index/edge_repo.go`**

```go
package index

import (
	"database/sql"
	"fmt"
)

// EdgeRow is the index representation of a single edge.
type EdgeRow struct {
	Type       string
	SourceID   string
	TargetID   string
	Ordinal    int
	SourcePath string
}

// EdgeRepo persists EdgeRow values in the SQLite index.
type EdgeRepo struct {
	db *sql.DB
}

// NewEdgeRepo constructs an EdgeRepo backed by idx.
func NewEdgeRepo(idx *Index) *EdgeRepo {
	return &EdgeRepo{db: idx.DB()}
}

// UpsertAll replaces every edge declared by sourcePath with the provided set.
// The replacement is transactional: existing edges where source_id = sourceID
// AND source_path = sourcePath are deleted, then the new edges are inserted.
func (repo *EdgeRepo) UpsertAll(sourceID, sourcePath string, edges []EdgeRow) error {
	tx, beginErr := repo.db.Begin()

	if beginErr != nil {
		return fmt.Errorf("edgeRepo: begin: %w", beginErr)
	}

	if _, deleteErr := tx.Exec(`DELETE FROM edges WHERE source_id = ? AND source_path = ?`, sourceID, sourcePath); deleteErr != nil {
		tx.Rollback()
		return fmt.Errorf("edgeRepo: delete %s: %w", sourceID, deleteErr)
	}

	for _, edge := range edges {
		if _, insertErr := tx.Exec(`
			INSERT INTO edges (type, source_id, target_id, ordinal, source_path)
			VALUES (?, ?, ?, ?, ?)
		`, edge.Type, edge.SourceID, edge.TargetID, edge.Ordinal, edge.SourcePath); insertErr != nil {
			tx.Rollback()
			return fmt.Errorf("edgeRepo: insert %s→%s: %w", edge.SourceID, edge.TargetID, insertErr)
		}
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("edgeRepo: commit: %w", commitErr)
	}

	return nil
}

// ListBySource returns all edges where source_id = sourceID, ordered by type, ordinal.
func (repo *EdgeRepo) ListBySource(sourceID string) ([]EdgeRow, error) {
	return repo.queryEdges(`SELECT type, source_id, target_id, ordinal, source_path FROM edges WHERE source_id = ? ORDER BY type, ordinal`, sourceID)
}

// ListByTarget returns all edges where target_id = targetID, ordered by type.
func (repo *EdgeRepo) ListByTarget(targetID string) ([]EdgeRow, error) {
	return repo.queryEdges(`SELECT type, source_id, target_id, ordinal, source_path FROM edges WHERE target_id = ? ORDER BY type, source_id`, targetID)
}

// ListByType returns all edges where type = edgeType, ordered by source_id, ordinal.
func (repo *EdgeRepo) ListByType(edgeType string) ([]EdgeRow, error) {
	return repo.queryEdges(`SELECT type, source_id, target_id, ordinal, source_path FROM edges WHERE type = ? ORDER BY source_id, ordinal`, edgeType)
}

// DeleteBySource removes every edge where source_id = sourceID, regardless of source_path.
func (repo *EdgeRepo) DeleteBySource(sourceID string) error {
	_, execErr := repo.db.Exec(`DELETE FROM edges WHERE source_id = ?`, sourceID)

	if execErr != nil {
		return fmt.Errorf("edgeRepo: delete source %s: %w", sourceID, execErr)
	}

	return nil
}

func (repo *EdgeRepo) queryEdges(query, arg string) ([]EdgeRow, error) {
	rows, queryErr := repo.db.Query(query, arg)

	if queryErr != nil {
		return nil, fmt.Errorf("edgeRepo: query: %w", queryErr)
	}

	defer rows.Close()

	var results []EdgeRow

	for rows.Next() {
		row := EdgeRow{}

		if scanErr := rows.Scan(&row.Type, &row.SourceID, &row.TargetID, &row.Ordinal, &row.SourcePath); scanErr != nil {
			return nil, fmt.Errorf("edgeRepo: scan: %w", scanErr)
		}

		results = append(results, row)
	}

	return results, rows.Err()
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/index/... -v
```

Expected: all PASS (Plan 1b's index tests + 5 new EdgeRepo tests).

- [ ] **Step 5: Commit**

```bash
git add internal/index/edge_repo.go internal/index/edge_repo_test.go
git commit -m "feat(index): EdgeRepo with UpsertAll, ListBySource/ByTarget/ByType, DeleteBySource"
```

---

## Task 5: Node — `Edges` field + `ResolveEdges`

**Files:**
- Modify: `internal/node/node.go`
- Create: `internal/node/edges.go`, `internal/node/edges_test.go`

- [ ] **Step 1: Extend the `Node` type — modify `internal/node/node.go`**

```go
// Package node owns the markdown-file representation of a node and the
// service operations that create, read, and list them.
package node

// Node is the parsed representation of a markdown node file.
type Node struct {
	ID         string              // workspace-relative path without extension
	Path       string              // workspace-relative path with extension
	Type       string              // value of the required `type:` frontmatter field
	Title      string              // value of the optional `title:` frontmatter field; empty if absent
	Properties map[string]any      // frontmatter keys NOT matching a declared edge type
	Edges      map[string][]string // edge-type-name → ordered list of target node ids
	Body       []byte              // markdown body after the closing `---` delimiter
}
```

(`ParseFile` does not yet populate `Edges`; only the resolver sets it.)

- [ ] **Step 2: Write the failing test — `internal/node/edges_test.go`**

```go
package node_test

import (
	"reflect"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

func testEdgeRegistry() manifest.EdgeTypes {
	return manifest.EdgeTypes{
		"parent": manifest.EdgeType{
			From: []string{"ticket"}, To: []string{"ticket", "project"},
			Cardinality: manifest.CardinalityManyToOne,
		},
		"blocks": manifest.EdgeType{
			From: []string{"ticket"}, To: []string{"ticket"},
			Cardinality: manifest.CardinalityManyToMany,
			Acyclic:     true,
		},
		"references": manifest.EdgeType{
			From: []string{"*"}, To: []string{"*"},
			Cardinality: manifest.CardinalityManyToMany,
		},
	}
}

func TestResolveEdges_MovesEdgeKeysFromPropertiesToEdges(test *testing.T) {
	parsed := &node.Node{
		ID:   "tickets/foo",
		Type: "ticket",
		Properties: map[string]any{
			"type":     "ticket",
			"title":    "Foo",
			"priority": 3,
			"parent":   "tickets/epic",
			"blocks":   []any{"tickets/bar", "tickets/baz"},
		},
		Body: []byte(""),
	}

	if resolveErr := node.ResolveEdges(parsed, testEdgeRegistry()); resolveErr != nil {
		test.Fatalf("ResolveEdges: %v", resolveErr)
	}

	if _, lingering := parsed.Properties["parent"]; lingering {
		test.Errorf("parent should have been moved out of Properties")
	}

	if _, lingering := parsed.Properties["blocks"]; lingering {
		test.Errorf("blocks should have been moved out of Properties")
	}

	if priorityRaw, kept := parsed.Properties["priority"]; !kept || priorityRaw != 3 {
		test.Errorf("priority should remain as a property")
	}

	if !reflect.DeepEqual(parsed.Edges["parent"], []string{"tickets/epic"}) {
		test.Errorf("Edges[parent] = %v", parsed.Edges["parent"])
	}

	if !reflect.DeepEqual(parsed.Edges["blocks"], []string{"tickets/bar", "tickets/baz"}) {
		test.Errorf("Edges[blocks] = %v", parsed.Edges["blocks"])
	}
}

func TestResolveEdges_AcceptsScalarStringForEdgeKey(test *testing.T) {
	parsed := &node.Node{
		ID:   "tickets/foo",
		Type: "ticket",
		Properties: map[string]any{
			"type":   "ticket",
			"parent": "tickets/epic",
		},
	}

	if resolveErr := node.ResolveEdges(parsed, testEdgeRegistry()); resolveErr != nil {
		test.Fatalf("ResolveEdges: %v", resolveErr)
	}

	if !reflect.DeepEqual(parsed.Edges["parent"], []string{"tickets/epic"}) {
		test.Errorf("Edges[parent] = %v", parsed.Edges["parent"])
	}
}

func TestResolveEdges_RejectsMapShapedEdgeValue(test *testing.T) {
	parsed := &node.Node{
		ID:   "tickets/foo",
		Type: "ticket",
		Properties: map[string]any{
			"type":   "ticket",
			"parent": map[string]any{"id": "x"},
		},
	}

	resolveErr := node.ResolveEdges(parsed, testEdgeRegistry())

	if resolveErr == nil {
		test.Fatalf("expected error for map-shaped edge value")
	}
}

func TestResolveEdges_LeavesNonEdgeKeysAlone(test *testing.T) {
	parsed := &node.Node{
		ID:   "n",
		Type: "ticket",
		Properties: map[string]any{
			"type":     "ticket",
			"title":    "T",
			"priority": 4,
		},
	}

	if resolveErr := node.ResolveEdges(parsed, testEdgeRegistry()); resolveErr != nil {
		test.Fatalf("ResolveEdges: %v", resolveErr)
	}

	if len(parsed.Edges) != 0 {
		test.Errorf("Edges = %v, want empty", parsed.Edges)
	}
}
```

- [ ] **Step 3: Run, verify fail**

```bash
go test ./internal/node/... -run ResolveEdges
```

Expected: FAIL — `ResolveEdges` not defined.

- [ ] **Step 4: Implement — `internal/node/edges.go`**

```go
package node

import (
	"errors"
	"fmt"

	"github.com/germanamz/tusk/internal/manifest"
)

// ErrEdgeValueShape is returned by ResolveEdges when a frontmatter key matching
// an edge type carries a value that is neither a scalar string nor a sequence
// of strings.
var ErrEdgeValueShape = errors.New("node: edge value must be a string or sequence of strings")

// ResolveEdges walks node.Properties and moves entries whose key matches a
// declared edge type into node.Edges, leaving non-edge keys in place.
//
// Edge values may be:
//   - a scalar string (single target id), or
//   - a YAML sequence whose every element is a string (multiple target ids).
//
// Any other shape returns ErrEdgeValueShape wrapped with the offending key.
func ResolveEdges(parsedNode *Node, edgeTypes manifest.EdgeTypes) error {
	if parsedNode.Edges == nil {
		parsedNode.Edges = map[string][]string{}
	}

	for key, value := range parsedNode.Properties {
		if _, isEdge := edgeTypes[key]; !isEdge {
			continue
		}

		targets, convertErr := edgeValueToTargets(key, value)

		if convertErr != nil {
			return convertErr
		}

		parsedNode.Edges[key] = targets
		delete(parsedNode.Properties, key)
	}

	return nil
}

func edgeValueToTargets(key string, value any) ([]string, error) {
	switch typed := value.(type) {
	case string:
		return []string{typed}, nil
	case []any:
		result := make([]string, 0, len(typed))

		for index, element := range typed {
			elementString, isString := element.(string)

			if !isString {
				return nil, fmt.Errorf("%w: key %q index %d not a string (got %T)", ErrEdgeValueShape, key, index, element)
			}

			result = append(result, elementString)
		}

		return result, nil
	}

	return nil, fmt.Errorf("%w: key %q has unsupported value type %T", ErrEdgeValueShape, key, value)
}
```

- [ ] **Step 5: Run, verify pass**

```bash
go test ./internal/node/... -v
```

Expected: existing parse + service tests still pass; 4 new ResolveEdges tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/node/node.go internal/node/edges.go internal/node/edges_test.go
git commit -m "feat(node): ResolveEdges splits edge-shaped frontmatter into Node.Edges"
```

---

## Task 6: Wikilink extraction from body

**Files:**
- Create: `internal/node/wikilinks.go`, `internal/node/wikilinks_test.go`

- [ ] **Step 1: Write the failing test — `internal/node/wikilinks_test.go`**

```go
package node_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/germanamz/tusk/internal/node"
)

func TestExtractWikilinks_FindsBracketedTargets(test *testing.T) {
	body := []byte(`# Body

See [[notes/auth-rfc]] for context.

Also relates to [[tickets/refactor-storage]].

A second mention of [[notes/auth-rfc]] is deduped.
`)

	links := node.ExtractWikilinks(body)

	sort.Strings(links)

	want := []string{"notes/auth-rfc", "tickets/refactor-storage"}

	if !reflect.DeepEqual(links, want) {
		test.Errorf("got %v, want %v", links, want)
	}
}

func TestExtractWikilinks_IgnoresEscapedAndCodeFence(test *testing.T) {
	body := []byte("Real link [[real]].\n\n```\nfenced [[notreal]]\n```\n")

	links := node.ExtractWikilinks(body)

	if len(links) != 1 || links[0] != "real" {
		test.Errorf("got %v, want [real]", links)
	}
}

func TestExtractWikilinks_ReturnsEmptyWhenNoLinks(test *testing.T) {
	links := node.ExtractWikilinks([]byte("plain body, no brackets at all\n"))

	if len(links) != 0 {
		test.Errorf("got %v, want empty", links)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/node/... -run Wikilinks
```

Expected: FAIL.

- [ ] **Step 3: Implement — `internal/node/wikilinks.go`**

```go
package node

import (
	"bytes"
	"regexp"
	"strings"
)

// wikilinkPattern matches `[[target]]` where target is one or more characters
// that do not include `[`, `]`, or pipe `|`. (The pipe isn't used in v1, but
// reserving it for later display-text syntax is cheap.)
var wikilinkPattern = regexp.MustCompile(`\[\[([^\[\]|]+)\]\]`)

// ExtractWikilinks returns the unique list of wikilink targets from body,
// in first-seen order, ignoring fenced code blocks (```…```).
func ExtractWikilinks(body []byte) []string {
	stripped := stripFencedCodeBlocks(body)
	matches := wikilinkPattern.FindAllSubmatch(stripped, -1)

	seen := map[string]struct{}{}
	var ordered []string

	for _, match := range matches {
		target := strings.TrimSpace(string(match[1]))

		if target == "" {
			continue
		}

		if _, already := seen[target]; already {
			continue
		}

		seen[target] = struct{}{}
		ordered = append(ordered, target)
	}

	return ordered
}

// stripFencedCodeBlocks returns body with content inside triple-backtick fences
// replaced by blank space so wikilinkPattern doesn't match into code samples.
func stripFencedCodeBlocks(body []byte) []byte {
	const fence = "```"

	stripped := make([]byte, 0, len(body))
	rest := body

	for {
		openIndex := bytes.Index(rest, []byte(fence))

		if openIndex < 0 {
			stripped = append(stripped, rest...)
			return stripped
		}

		stripped = append(stripped, rest[:openIndex]...)

		afterOpen := rest[openIndex+len(fence):]
		closeIndex := bytes.Index(afterOpen, []byte(fence))

		if closeIndex < 0 {
			// Unterminated fence — drop the rest.
			return stripped
		}

		rest = afterOpen[closeIndex+len(fence):]
	}
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/node/... -v
```

Expected: all PASS, including 3 new wikilink tests.

- [ ] **Step 5: Commit**

```bash
git add internal/node/wikilinks.go internal/node/wikilinks_test.go
git commit -m "feat(node): ExtractWikilinks scans body for [[target]] references"
```

---

## Task 7: Node — `ValidateEdges` and `DetectCycle`

**Files:**
- Modify: `internal/node/edges.go`, `internal/node/edges_test.go`

- [ ] **Step 1: Append failing tests — `internal/node/edges_test.go`**

Append to the end of `internal/node/edges_test.go`:

```go
func TestValidateEdges_RejectsUnknownEdgeType(test *testing.T) {
	parsed := &node.Node{
		ID:    "x",
		Type:  "ticket",
		Edges: map[string][]string{"unknown": {"y"}},
	}

	validateErr := node.ValidateEdges(parsed, testEdgeRegistry(), node.EdgeContext{
		ResolveTargetType: func(targetID string) (string, bool) { return "ticket", true },
	})

	if validateErr == nil {
		test.Fatalf("expected error for unknown edge type")
	}
}

func TestValidateEdges_RejectsSourceTypeMismatch(test *testing.T) {
	parsed := &node.Node{
		ID:    "x",
		Type:  "note",
		Edges: map[string][]string{"parent": {"y"}}, // parent only allows ticket source
	}

	validateErr := node.ValidateEdges(parsed, testEdgeRegistry(), node.EdgeContext{
		ResolveTargetType: func(targetID string) (string, bool) { return "ticket", true },
	})

	if validateErr == nil {
		test.Fatalf("expected error for source type mismatch")
	}
}

func TestValidateEdges_RejectsTargetTypeMismatch(test *testing.T) {
	parsed := &node.Node{
		ID:    "x",
		Type:  "ticket",
		Edges: map[string][]string{"parent": {"unknown-target"}},
	}

	validateErr := node.ValidateEdges(parsed, testEdgeRegistry(), node.EdgeContext{
		ResolveTargetType: func(targetID string) (string, bool) { return "note", true }, // not in parent.To
	})

	if validateErr == nil {
		test.Fatalf("expected error for target type mismatch")
	}
}

func TestValidateEdges_AllowsUnresolvedTargetWhenContextSaysFalse(test *testing.T) {
	// Wikilinks and frontmatter edges may reference targets that don't yet
	// exist on disk. Resolver indicates this via ResolveTargetType returning
	// (_, false). ValidateEdges must allow those — the lifecycle is
	// "warn at doctor", not "reject at write".
	parsed := &node.Node{
		ID:    "x",
		Type:  "ticket",
		Edges: map[string][]string{"references": {"missing/target"}},
	}

	validateErr := node.ValidateEdges(parsed, testEdgeRegistry(), node.EdgeContext{
		ResolveTargetType: func(targetID string) (string, bool) { return "", false },
	})

	if validateErr != nil {
		test.Errorf("expected nil error for unresolved target on `references`, got %v", validateErr)
	}
}

func TestDetectCycle_ReturnsNilOnAcyclicGraph(test *testing.T) {
	// a -blocks-> b -blocks-> c
	existing := map[string][]string{
		"a": {"b"},
		"b": {"c"},
	}

	candidate := node.CycleProbe{
		EdgeType: "blocks",
		Source:   "c",
		Target:   "d",
	}

	cycleErr := node.DetectCycle(candidate, existing)

	if cycleErr != nil {
		test.Errorf("got %v, want nil (no cycle)", cycleErr)
	}
}

func TestDetectCycle_ReturnsErrorWhenAddingEdgeCreatesCycle(test *testing.T) {
	// existing: a -blocks-> b -blocks-> c
	// adding:   c -blocks-> a  (would create a→b→c→a cycle)
	existing := map[string][]string{
		"a": {"b"},
		"b": {"c"},
	}

	candidate := node.CycleProbe{
		EdgeType: "blocks",
		Source:   "c",
		Target:   "a",
	}

	cycleErr := node.DetectCycle(candidate, existing)

	if cycleErr == nil {
		test.Fatalf("expected cycle error")
	}
}

func TestDetectCycle_AllowsSelfLoopOnNonAcyclic(test *testing.T) {
	// When the caller doesn't apply DetectCycle (because Acyclic = false),
	// nothing here is exercised. This test confirms DetectCycle catches a
	// trivial self-loop when invoked.
	existing := map[string][]string{}
	candidate := node.CycleProbe{
		EdgeType: "blocks",
		Source:   "x",
		Target:   "x",
	}

	cycleErr := node.DetectCycle(candidate, existing)

	if cycleErr == nil {
		test.Fatalf("expected cycle error on self-loop")
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/node/... -run "ValidateEdges|DetectCycle"
```

Expected: FAIL — symbols undefined.

- [ ] **Step 3: Extend `internal/node/edges.go`**

Append to `internal/node/edges.go`:

```go
// EdgeContext supplies node-type resolution to ValidateEdges. The caller
// implements ResolveTargetType against whatever store represents nodes
// (in-memory map for tests, the index in production).
//
// ResolveTargetType returns (typeName, true) when the target node's type can be
// determined, and ("", false) for unresolved targets (file does not exist).
// Unresolved targets are allowed — they surface as warnings via doctor (Plan 8)
// rather than rejections at write time.
type EdgeContext struct {
	ResolveTargetType func(targetID string) (string, bool)
}

// ValidateEdges checks that every edge in node.Edges is declared in edgeTypes
// and that source/target node types match the edge type's `from`/`to` lists.
// Returns the first violation encountered, or nil if all edges are legal.
func ValidateEdges(parsedNode *Node, edgeTypes manifest.EdgeTypes, ctx EdgeContext) error {
	for edgeName, targets := range parsedNode.Edges {
		edgeType, declared := edgeTypes[edgeName]

		if !declared {
			return fmt.Errorf("node: edge %q not declared in manifest", edgeName)
		}

		if !edgeType.AllowsSource(parsedNode.Type) {
			return fmt.Errorf("node: edge %q does not allow source type %q (allowed: %v)", edgeName, parsedNode.Type, edgeType.From)
		}

		for _, targetID := range targets {
			targetType, resolved := ctx.ResolveTargetType(targetID)

			if !resolved {
				continue
			}

			if !edgeType.AllowsTarget(targetType) {
				return fmt.Errorf("node: edge %q from %q to %q: target type %q not in allowed %v", edgeName, parsedNode.ID, targetID, targetType, edgeType.To)
			}
		}
	}

	return nil
}

// CycleProbe describes a candidate edge being checked against an existing graph.
type CycleProbe struct {
	EdgeType string
	Source   string
	Target   string
}

// ErrCycleDetected is returned by DetectCycle when adding the candidate edge
// would form a cycle in the typed sub-graph.
var ErrCycleDetected = errors.New("node: edge would create a cycle")

// DetectCycle checks whether adding candidate.Source → candidate.Target to the
// existing adjacency map (already filtered to the same edge type) would create
// a cycle. Returns ErrCycleDetected (wrapped with the offending path) when
// reachable, nil otherwise.
//
// Self-loops (Source == Target) are always reported as cycles.
//
// Caller is responsible for filtering `existing` to edges of the same type.
func DetectCycle(candidate CycleProbe, existing map[string][]string) error {
	if candidate.Source == candidate.Target {
		return fmt.Errorf("%w: self-loop on %q", ErrCycleDetected, candidate.Source)
	}

	// DFS from candidate.Target. If we ever reach candidate.Source, the
	// proposed edge closes a cycle.
	visited := map[string]struct{}{}
	stack := []string{candidate.Target}

	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if current == candidate.Source {
			return fmt.Errorf("%w: %s → … → %s → %s", ErrCycleDetected, candidate.Source, candidate.Target, candidate.Source)
		}

		if _, alreadyVisited := visited[current]; alreadyVisited {
			continue
		}

		visited[current] = struct{}{}

		for _, next := range existing[current] {
			stack = append(stack, next)
		}
	}

	return nil
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/node/... -v
```

Expected: all PASS, including 7 new validate/cycle tests.

- [ ] **Step 5: Commit**

```bash
git add internal/node/edges.go internal/node/edges_test.go
git commit -m "feat(node): ValidateEdges + DetectCycle for manifest-driven legality"
```

---

## Task 8: NodeService — write edges through Create

**Files:**
- Modify: `internal/node/service.go`, `internal/node/service_test.go`

- [ ] **Step 1: Append failing tests — `internal/node/service_test.go`**

Append:

```go
func newTestServiceWithManifest(test *testing.T, edgeTypes manifest.EdgeTypes) (*node.Service, string) {
	test.Helper()

	root := test.TempDir()

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	service := node.NewServiceWithManifest(root, index.NewNodeRepo(store), index.NewEdgeRepo(store), edgeTypes)

	return service, root
}

func plan2EdgeRegistry() manifest.EdgeTypes {
	return manifest.EdgeTypes{
		"parent": manifest.EdgeType{
			From: []string{"ticket"}, To: []string{"ticket", "project"},
			Cardinality: manifest.CardinalityManyToOne,
		},
		"blocks": manifest.EdgeType{
			From: []string{"ticket"}, To: []string{"ticket"},
			Cardinality: manifest.CardinalityManyToMany,
			Acyclic:     true,
		},
		"references": manifest.EdgeType{
			From: []string{"*"}, To: []string{"*"},
			Cardinality: manifest.CardinalityManyToMany,
		},
	}
}

func TestService_CreatePersistsFrontmatterEdges(test *testing.T) {
	service, _ := newTestServiceWithManifest(test, plan2EdgeRegistry())

	parentInput := node.CreateInput{
		RelPath: "tickets/epic.md",
		Type:    "ticket",
		Title:   "Epic",
	}

	if _, parentErr := service.Create(parentInput); parentErr != nil {
		test.Fatalf("Create epic: %v", parentErr)
	}

	childInput := node.CreateInput{
		RelPath: "tickets/child.md",
		Type:    "ticket",
		Title:   "Child",
		Properties: map[string]any{
			"parent": "tickets/epic",
		},
	}

	created, createErr := service.Create(childInput)

	if createErr != nil {
		test.Fatalf("Create child: %v", createErr)
	}

	if !reflect.DeepEqual(created.Edges["parent"], []string{"tickets/epic"}) {
		test.Errorf("Edges[parent] = %v", created.Edges["parent"])
	}
}

func TestService_CreateRejectsIllegalEdgeSource(test *testing.T) {
	service, _ := newTestServiceWithManifest(test, plan2EdgeRegistry())

	if _, parentErr := service.Create(node.CreateInput{
		RelPath: "tickets/epic.md", Type: "ticket", Title: "Epic",
	}); parentErr != nil {
		test.Fatalf("Create epic: %v", parentErr)
	}

	// Note has source-type "note"; "parent" only allows "ticket" sources.
	noteInput := node.CreateInput{
		RelPath: "notes/bad.md",
		Type:    "note",
		Properties: map[string]any{
			"parent": "tickets/epic",
		},
	}

	_, createErr := service.Create(noteInput)

	if createErr == nil {
		test.Fatalf("expected error for source type mismatch")
	}
}

func TestService_CreateMaterializesWikilinksAsReferencesEdges(test *testing.T) {
	service, _ := newTestServiceWithManifest(test, plan2EdgeRegistry())

	if _, targetErr := service.Create(node.CreateInput{
		RelPath: "notes/target.md", Type: "note", Title: "Target",
	}); targetErr != nil {
		test.Fatalf("Create target: %v", targetErr)
	}

	created, createErr := service.Create(node.CreateInput{
		RelPath: "notes/source.md",
		Type:    "note",
		Title:   "Source",
		Body:    []byte("See [[notes/target]] for context.\n"),
	})

	if createErr != nil {
		test.Fatalf("Create source: %v", createErr)
	}

	wantTargets := []string{"notes/target"}

	if !reflect.DeepEqual(created.Edges["references"], wantTargets) {
		test.Errorf("Edges[references] = %v, want %v", created.Edges["references"], wantTargets)
	}
}
```

Add the `reflect`, `manifest`, `path/filepath`, and `index` imports to the test file's import block if not already present.

- [ ] **Step 2: Run, verify fail**

Expected: FAIL — `node.NewServiceWithManifest` undefined.

- [ ] **Step 3: Extend `internal/node/service.go`**

Add to the import block: `"github.com/germanamz/tusk/internal/manifest"`.

Replace the `Service` struct and `NewService` function with:

```go
// Service orchestrates filesystem and index for nodes.
type Service struct {
	root      string
	repo      *index.NodeRepo
	edges     *index.EdgeRepo
	edgeTypes manifest.EdgeTypes
}

// NewService constructs a Service for a workspace whose manifest has no edge
// types declared (Plan 1b behavior). Edge writes via this service are no-ops.
func NewService(workspaceRoot string, repo *index.NodeRepo) *Service {
	return &Service{
		root:      workspaceRoot,
		repo:      repo,
		edges:     nil,
		edgeTypes: manifest.EdgeTypes{},
	}
}

// NewServiceWithManifest constructs a Service that writes edges through edges
// and validates them against edgeTypes.
func NewServiceWithManifest(workspaceRoot string, repo *index.NodeRepo, edges *index.EdgeRepo, edgeTypes manifest.EdgeTypes) *Service {
	return &Service{
		root:      workspaceRoot,
		repo:      repo,
		edges:     edges,
		edgeTypes: edgeTypes,
	}
}
```

Replace `Service.Create` so it resolves edges, validates them, and persists via EdgeRepo. Append the following at the end of the existing successful-write path (replacing the existing `return parsed, nil` with the extended logic):

```go
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

	if resolveErr := ResolveEdges(parsed, service.edgeTypes); resolveErr != nil {
		return nil, resolveErr
	}

	for _, target := range ExtractWikilinks(parsed.Body) {
		if _, hasReferences := service.edgeTypes["references"]; !hasReferences {
			break
		}

		parsed.Edges["references"] = appendUnique(parsed.Edges["references"], target)
	}

	if validateErr := ValidateEdges(parsed, service.edgeTypes, EdgeContext{
		ResolveTargetType: service.resolveTargetType,
	}); validateErr != nil {
		return nil, validateErr
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

	if service.edges != nil {
		edgeRows := flattenEdges(parsed)

		if upsertErr := service.edges.UpsertAll(parsed.ID, parsed.Path, edgeRows); upsertErr != nil {
			return nil, upsertErr
		}
	}

	return parsed, nil
}

// resolveTargetType looks up a target's node type in the index. Returns
// ("", false) when the target is not known (which the validator treats as
// "allowed for now").
func (service *Service) resolveTargetType(targetID string) (string, bool) {
	row, getErr := service.repo.Get(targetID)

	if getErr != nil {
		return "", false
	}

	return row.Type, true
}

// flattenEdges turns parsed.Edges (map of edge-type → []targetID) into the
// EdgeRow shape expected by index.EdgeRepo.UpsertAll. Order is preserved
// within each edge type via Ordinal.
func flattenEdges(parsedNode *Node) []index.EdgeRow {
	var rows []index.EdgeRow

	for edgeType, targets := range parsedNode.Edges {
		for ordinal, target := range targets {
			rows = append(rows, index.EdgeRow{
				Type:       edgeType,
				SourceID:   parsedNode.ID,
				TargetID:   target,
				Ordinal:    ordinal,
				SourcePath: parsedNode.Path,
			})
		}
	}

	return rows
}

// appendUnique appends value to slice only if not already present.
func appendUnique(slice []string, value string) []string {
	for _, existing := range slice {
		if existing == value {
			return slice
		}
	}

	return append(slice, value)
}
```

(`Get` and `List` keep their Plan 1b implementations.)

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/node/... -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/node/service.go internal/node/service_test.go
git commit -m "feat(node): NodeService writes edges with manifest-driven validation and wikilinks"
```

---

## Task 9: Cycle detection in Service.Create

**Files:**
- Modify: `internal/node/service.go`, `internal/node/service_test.go`

**Note on test scope:** Plan 1b's `Service.Create` rejects pre-existing files, so cycles can only be introduced via the `update` path (Plan 3) or via `reindex` of cycle-introducing external edits (also Plan 3 territory once watcher lands). Plan 2's service-level integration test therefore exercises only the self-loop case; full multi-hop cycle scenarios are covered by Task 7's direct-unit `TestDetectCycle_*` tests.

- [ ] **Step 1: Append failing test — `internal/node/service_test.go`**

```go
func TestService_CreateRejectsBlocksCycle(test *testing.T) {
	service, _ := newTestServiceWithManifest(test, plan2EdgeRegistry())

	_, selfErr := service.Create(node.CreateInput{
		RelPath:    "tickets/self.md",
		Type:       "ticket",
		Properties: map[string]any{"blocks": []any{"tickets/self"}},
	})

	if selfErr == nil {
		test.Fatalf("expected cycle error for self-blocks")
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/node/... -run TestService_CreateRejectsBlocksCycle
```

Expected: FAIL — Service.Create currently lets the self-blocks edge through.

- [ ] **Step 3: Wire cycle detection into Service.Create**

In `internal/node/service.go`, after the `ValidateEdges` call and before `service.repo.Upsert`, add:

```go
	if cycleErr := service.detectCyclesForAcyclicEdges(parsed); cycleErr != nil {
		return nil, cycleErr
	}
```

Then add the helper method on `*Service`:

```go
// detectCyclesForAcyclicEdges runs DetectCycle for every edge of an Acyclic
// type, using the index as the existing-graph oracle.
func (service *Service) detectCyclesForAcyclicEdges(parsedNode *Node) error {
	if service.edges == nil {
		return nil
	}

	for edgeType, targets := range parsedNode.Edges {
		definition, declared := service.edgeTypes[edgeType]

		if !declared || !definition.Acyclic {
			continue
		}

		existing, loadErr := service.loadAdjacencyForType(edgeType)

		if loadErr != nil {
			return loadErr
		}

		for _, target := range targets {
			if cycleErr := DetectCycle(CycleProbe{EdgeType: edgeType, Source: parsedNode.ID, Target: target}, existing); cycleErr != nil {
				return cycleErr
			}
		}
	}

	return nil
}

// loadAdjacencyForType builds the existing-graph adjacency map for a single
// edge type by walking every row in EdgeRepo of that type.
func (service *Service) loadAdjacencyForType(edgeType string) (map[string][]string, error) {
	rows, listErr := service.edges.ListByType(edgeType)

	if listErr != nil {
		return nil, listErr
	}

	adjacency := map[string][]string{}

	for _, row := range rows {
		adjacency[row.SourceID] = append(adjacency[row.SourceID], row.TargetID)
	}

	return adjacency, nil
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/node/... -v
```

Expected: all PASS, including the new cycle test.

- [ ] **Step 5: Commit**

```bash
git add internal/node/service.go internal/node/service_test.go
git commit -m "feat(node): Service.Create rejects cycles on Acyclic edge types"
```

---

## Task 10: Reindex — write edges from disk

**Files:**
- Modify: `internal/reindex/reindex.go`, `internal/reindex/reindex_test.go`

- [ ] **Step 1: Append failing test — `internal/reindex/reindex_test.go`**

Append:

```go
func TestRun_PersistsFrontmatterEdges(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "tickets/epic.md", "type: ticket\ntitle: Epic\n", "Body.\n")
	writeNode(test, root, "tickets/child.md", "type: ticket\ntitle: Child\nparent: tickets/epic\n", "Body.\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	repo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	edgeTypes := manifest.EdgeTypes{
		"parent": manifest.EdgeType{
			From: []string{"ticket"}, To: []string{"ticket", "project"},
			Cardinality: manifest.CardinalityManyToOne,
		},
	}

	report, runErr := reindex.Run(reindex.Config{
		Root:      root,
		Repo:      repo,
		Edges:     edgeRepo,
		EdgeTypes: edgeTypes,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.Indexed != 2 {
		test.Errorf("Indexed = %d, want 2", report.Indexed)
	}

	listed, _ := edgeRepo.ListBySource("tickets/child")

	if len(listed) != 1 || listed[0].Type != "parent" || listed[0].TargetID != "tickets/epic" {
		test.Errorf("listed = %+v", listed)
	}
}

func TestRun_PersistsWikilinksAsReferenceEdges(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "notes/target.md", "type: note\ntitle: Target\n", "")
	writeNode(test, root, "notes/source.md", "type: note\ntitle: Source\n", "Refer to [[notes/target]].\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	repo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	edgeTypes := manifest.EdgeTypes{
		"references": manifest.EdgeType{
			From: []string{"*"}, To: []string{"*"},
			Cardinality: manifest.CardinalityManyToMany,
		},
	}

	if _, runErr := reindex.Run(reindex.Config{Root: root, Repo: repo, Edges: edgeRepo, EdgeTypes: edgeTypes}); runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	listed, _ := edgeRepo.ListBySource("notes/source")

	if len(listed) != 1 || listed[0].Type != "references" || listed[0].TargetID != "notes/target" {
		test.Errorf("listed = %+v", listed)
	}
}

func TestRun_RemovedFileAlsoRemovesEdges(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "tickets/a.md", "type: ticket\ntitle: A\nparent: tickets/b\n", "")
	writeNode(test, root, "tickets/b.md", "type: ticket\ntitle: B\n", "")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	repo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	edgeTypes := manifest.EdgeTypes{
		"parent": manifest.EdgeType{
			From: []string{"ticket"}, To: []string{"ticket"},
			Cardinality: manifest.CardinalityManyToOne,
		},
	}

	cfg := reindex.Config{Root: root, Repo: repo, Edges: edgeRepo, EdgeTypes: edgeTypes}

	if _, runErr := reindex.Run(cfg); runErr != nil {
		test.Fatalf("first Run: %v", runErr)
	}

	if rmErr := os.Remove(filepath.Join(root, "tickets/a.md")); rmErr != nil {
		test.Fatalf("rm: %v", rmErr)
	}

	if _, runErr := reindex.Run(cfg); runErr != nil {
		test.Fatalf("second Run: %v", runErr)
	}

	listed, _ := edgeRepo.ListBySource("tickets/a")

	if len(listed) != 0 {
		test.Errorf("expected zero edges after node removal, got %+v", listed)
	}
}
```

Add `manifest` to the import block.

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/reindex/...
```

Expected: FAIL — `reindex.Config` lacks `Edges` and `EdgeTypes` fields.

- [ ] **Step 3: Extend `internal/reindex/reindex.go`**

Add to imports: `"github.com/germanamz/tusk/internal/manifest"`.

Replace `Config` and `Run` with:

```go
// Config configures Run.
type Config struct {
	Root      string             // workspace root
	Repo      *index.NodeRepo    // node index repository
	Edges     *index.EdgeRepo    // edge index repository (optional; when nil, edges are not written)
	EdgeTypes manifest.EdgeTypes // declared edge types (optional; when empty, frontmatter edges are not resolved)
}

// Run walks Root, parses every *.md file with valid frontmatter, and upserts
// or removes index rows so the index matches what is on disk.
func Run(config Config) (*Report, error) {
	report := &Report{}
	seenPaths := map[string]struct{}{}
	seenIDs := map[string]struct{}{}

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

		if resolveErr := node.ResolveEdges(parsed, config.EdgeTypes); resolveErr != nil {
			report.Skipped++

			return nil
		}

		// Wikilinks → references edges (only when references is declared).
		if _, hasReferences := config.EdgeTypes["references"]; hasReferences {
			for _, target := range node.ExtractWikilinks(parsed.Body) {
				parsed.Edges["references"] = appendUnique(parsed.Edges["references"], target)
			}
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

		if config.Edges != nil {
			edgeRows := flattenEdges(parsed)

			if upsertErr := config.Edges.UpsertAll(parsed.ID, parsed.Path, edgeRows); upsertErr != nil {
				return upsertErr
			}
		}

		seenPaths[parsed.Path] = struct{}{}
		seenIDs[parsed.ID] = struct{}{}
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
		if _, kept := seenPaths[row.Path]; kept {
			continue
		}

		if deleteErr := config.Repo.DeleteByPath(row.Path); deleteErr != nil {
			return nil, deleteErr
		}

		if config.Edges != nil {
			if deleteErr := config.Edges.DeleteBySource(row.ID); deleteErr != nil {
				return nil, deleteErr
			}
		}

		report.Removed++
	}

	return report, nil
}

// appendUnique appends value to slice only if not already present.
func appendUnique(slice []string, value string) []string {
	for _, existing := range slice {
		if existing == value {
			return slice
		}
	}

	return append(slice, value)
}

// flattenEdges turns parsed.Edges into the EdgeRow shape expected by EdgeRepo.
func flattenEdges(parsedNode *node.Node) []index.EdgeRow {
	var rows []index.EdgeRow

	for edgeType, targets := range parsedNode.Edges {
		for ordinal, target := range targets {
			rows = append(rows, index.EdgeRow{
				Type:       edgeType,
				SourceID:   parsedNode.ID,
				TargetID:   target,
				Ordinal:    ordinal,
				SourcePath: parsedNode.Path,
			})
		}
	}

	return rows
}
```

(The existing `shouldSkipDir` helper stays unchanged.)

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/reindex/... -v
```

Expected: all PASS — Plan 1b's 3 reindex tests + 3 new edge tests.

- [ ] **Step 5: Commit**

```bash
git add internal/reindex
git commit -m "feat(reindex): write frontmatter edges and references-from-wikilinks"
```

---

## Task 11: CLI — `tusk edge add` and `tusk edge remove`

**Files:**
- Create: `cmd/tusk/cmd_edge.go`, `cmd/tusk/cmd_edge_add.go`, `cmd/tusk/cmd_edge_remove.go`, `cmd/tusk/cmd_edge_add_test.go`, `cmd/tusk/cmd_edge_remove_test.go`
- Modify: `cmd/tusk/root.go`

These commands operate on the **index only** (no file rewrite). Plan 2 keeps "edge add" as a low-friction way for agents to add an edge without modifying frontmatter; rewriting frontmatter on edge changes is **Plan 3**'s rename-rewrite pipeline. `tusk edge list` (Task 12) reflects both index-only edges and frontmatter-derived edges since they're persisted to the same table.

> **Important**: edges added via `tusk edge add` get a synthetic `source_path` of `__cli__` so reindex (which keys upsertAll on `(source_id, source_path)`) doesn't blow them away when it processes the source node's file. Edges declared in frontmatter use the actual file path.

- [ ] **Step 1: Write the failing test for `add` — `cmd/tusk/cmd_edge_add_test.go`**

```go
package main

import (
	"bytes"
	"testing"
)

func TestEdgeAddCmd_PersistsEdgeInIndex(test *testing.T) {
	initWorkspaceWithManifest(test, edgeManifestBody())

	createCmd := newRootCmd()
	createCmd.SetArgs([]string{"node", "create", "--type", "ticket", "--title", "A", "--path", "tickets/a.md"})

	if execErr := createCmd.Execute(); execErr != nil {
		test.Fatalf("create a: %v", execErr)
	}

	create2 := newRootCmd()
	create2.SetArgs([]string{"node", "create", "--type", "ticket", "--title", "B", "--path", "tickets/b.md"})

	if execErr := create2.Execute(); execErr != nil {
		test.Fatalf("create b: %v", execErr)
	}

	output := &bytes.Buffer{}

	addCmd := newRootCmd()
	addCmd.SetOut(output)
	addCmd.SetErr(output)
	addCmd.SetArgs([]string{"edge", "add", "--type", "blocks", "--source", "tickets/a", "--target", "tickets/b"})

	if execErr := addCmd.Execute(); execErr != nil {
		test.Fatalf("edge add: %v\noutput: %s", execErr, output.String())
	}

	listOutput := &bytes.Buffer{}

	listCmd := newRootCmd()
	listCmd.SetOut(listOutput)
	listCmd.SetErr(listOutput)
	listCmd.SetArgs([]string{"edge", "list", "--from", "tickets/a"})

	if execErr := listCmd.Execute(); execErr != nil {
		test.Fatalf("edge list: %v\noutput: %s", execErr, listOutput.String())
	}

	if !bytes.Contains(listOutput.Bytes(), []byte("blocks")) || !bytes.Contains(listOutput.Bytes(), []byte("tickets/b")) {
		test.Errorf("missing edge in list:\n%s", listOutput.String())
	}
}

// edgeManifestBody returns a TOML manifest body declaring the standard Plan 2
// edge types. initWorkspaceWithManifest writes this into tusk.toml after init.
func edgeManifestBody() string {
	return `[workspace]
name = "test"

[edge-types.blocks]
from = ["ticket"]
to = ["ticket"]
cardinality = "many-to-many"
acyclic = true

[edge-types.parent]
from = ["ticket"]
to = ["ticket", "project"]
cardinality = "many-to-one"

[edge-types.references]
from = ["*"]
to = ["*"]
cardinality = "many-to-many"
`
}
```

- [ ] **Step 2: Add the helper to `cmd/tusk/cmd_node_create_test.go`** (or a new shared test file `cmd/tusk/test_helpers_test.go` if you prefer separation)

```go
func initWorkspaceWithManifest(test *testing.T, manifestBody string) string {
	test.Helper()

	root := initWorkspace(test)

	// Replace the minimal manifest written by `tusk init` with the richer body.
	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(manifestBody), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	return root
}
```

Add `os` and `path/filepath` to the imports of that test file if not already present.

- [ ] **Step 3: Write the failing test for `remove` — `cmd/tusk/cmd_edge_remove_test.go`**

```go
package main

import (
	"bytes"
	"testing"
)

func TestEdgeRemoveCmd_DropsEdgeFromIndex(test *testing.T) {
	initWorkspaceWithManifest(test, edgeManifestBody())

	for _, args := range [][]string{
		{"node", "create", "--type", "ticket", "--title", "A", "--path", "tickets/a.md"},
		{"node", "create", "--type", "ticket", "--title", "B", "--path", "tickets/b.md"},
		{"edge", "add", "--type", "blocks", "--source", "tickets/a", "--target", "tickets/b"},
	} {
		cmd := newRootCmd()
		cmd.SetArgs(args)

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("setup %v: %v", args, execErr)
		}
	}

	removeCmd := newRootCmd()
	removeCmd.SetArgs([]string{"edge", "remove", "--type", "blocks", "--source", "tickets/a", "--target", "tickets/b"})

	if execErr := removeCmd.Execute(); execErr != nil {
		test.Fatalf("edge remove: %v", execErr)
	}

	listOutput := &bytes.Buffer{}

	listCmd := newRootCmd()
	listCmd.SetOut(listOutput)
	listCmd.SetErr(listOutput)
	listCmd.SetArgs([]string{"edge", "list", "--from", "tickets/a"})

	if execErr := listCmd.Execute(); execErr != nil {
		test.Fatalf("edge list: %v", execErr)
	}

	if bytes.Contains(listOutput.Bytes(), []byte("tickets/b")) {
		test.Errorf("edge should have been removed, list still shows it:\n%s", listOutput.String())
	}
}
```

- [ ] **Step 4: Run, verify fail**

```bash
go test ./cmd/tusk/... -run TestEdgeAddCmd
```

Expected: FAIL — `edge` subcommand unknown.

- [ ] **Step 5: Implement `cmd/tusk/cmd_edge.go`**

```go
package main

import "github.com/spf13/cobra"

func newEdgeCmd() *cobra.Command {
	edgeCmd := &cobra.Command{
		Use:   "edge",
		Short: "Manage edges (add, remove, list)",
	}

	edgeCmd.AddCommand(newEdgeAddCmd())
	edgeCmd.AddCommand(newEdgeRemoveCmd())
	edgeCmd.AddCommand(newEdgeListCmd())

	return edgeCmd
}

// cliSourcePath is the synthetic source_path attributed to edges added via
// `tusk edge add`. Edges declared in frontmatter use the actual file path; the
// CLI marker keeps the two populations distinct so reindex's per-file UpsertAll
// doesn't clobber CLI-added edges.
const cliSourcePath = "__cli__"
```

- [ ] **Step 6: Implement `cmd/tusk/cmd_edge_add.go`**

```go
package main

import (
	"fmt"
	"os"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newEdgeAddCmd() *cobra.Command {
	var (
		edgeType string
		source   string
		target   string
	)

	addCmd := &cobra.Command{
		Use:   "add",
		Short: "Add an edge to the index",
		RunE: func(cmd *cobra.Command, args []string) error {
			if edgeType == "" || source == "" || target == "" {
				return fmt.Errorf("--type, --source, and --target are required")
			}

			cwd, cwdErr := os.Getwd()

			if cwdErr != nil {
				return cwdErr
			}

			ws, findErr := workspace.Find(cwd)

			if findErr != nil {
				return fmt.Errorf("workspace: %w", findErr)
			}

			loaded, loadErr := manifest.Load(ws.ManifestPath)

			if loadErr != nil {
				return loadErr
			}

			edgeDef, declared := loaded.EdgeTypes[edgeType]

			if !declared {
				return fmt.Errorf("edge type %q not declared in manifest", edgeType)
			}

			store, openErr := index.Open(ws.IndexPath)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			nodeRepo := index.NewNodeRepo(store)

			sourceRow, sourceErr := nodeRepo.Get(source)

			if sourceErr != nil {
				return fmt.Errorf("source: %w", sourceErr)
			}

			if !edgeDef.AllowsSource(sourceRow.Type) {
				return fmt.Errorf("edge type %q does not allow source type %q", edgeType, sourceRow.Type)
			}

			if targetRow, getErr := nodeRepo.Get(target); getErr == nil {
				if !edgeDef.AllowsTarget(targetRow.Type) {
					return fmt.Errorf("edge type %q does not allow target type %q", edgeType, targetRow.Type)
				}
			}

			edgeRepo := index.NewEdgeRepo(store)

			if edgeDef.Acyclic {
				existing, listErr := edgeRepo.ListByType(edgeType)

				if listErr != nil {
					return listErr
				}

				adjacency := buildAdjacency(existing)

				if cycleErr := node.DetectCycle(node.CycleProbe{EdgeType: edgeType, Source: source, Target: target}, adjacency); cycleErr != nil {
					return cycleErr
				}
			}

			existingForSource, listErr := edgeRepo.ListBySource(source)

			if listErr != nil {
				return listErr
			}

			cliExisting := filterCLI(existingForSource)
			ordinal := nextOrdinalFor(cliExisting, edgeType)

			cliExisting = append(cliExisting, index.EdgeRow{
				Type:       edgeType,
				SourceID:   source,
				TargetID:   target,
				Ordinal:    ordinal,
				SourcePath: cliSourcePath,
			})

			if upsertErr := edgeRepo.UpsertAll(source, cliSourcePath, cliExisting); upsertErr != nil {
				return upsertErr
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Added edge %s: %s → %s\n", edgeType, source, target)

			return nil
		},
	}

	addCmd.Flags().StringVar(&edgeType, "type", "", "edge type (must be declared in tusk.toml)")
	addCmd.Flags().StringVar(&source, "source", "", "source node id (workspace-relative path without extension)")
	addCmd.Flags().StringVar(&target, "target", "", "target node id")

	return addCmd
}

func buildAdjacency(rows []index.EdgeRow) map[string][]string {
	adjacency := map[string][]string{}

	for _, row := range rows {
		adjacency[row.SourceID] = append(adjacency[row.SourceID], row.TargetID)
	}

	return adjacency
}

func filterCLI(rows []index.EdgeRow) []index.EdgeRow {
	var filtered []index.EdgeRow

	for _, row := range rows {
		if row.SourcePath == cliSourcePath {
			filtered = append(filtered, row)
		}
	}

	return filtered
}

func nextOrdinalFor(rows []index.EdgeRow, edgeType string) int {
	max := -1

	for _, row := range rows {
		if row.Type != edgeType {
			continue
		}

		if row.Ordinal > max {
			max = row.Ordinal
		}
	}

	return max + 1
}
```

- [ ] **Step 7: Implement `cmd/tusk/cmd_edge_remove.go`**

```go
package main

import (
	"fmt"
	"os"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newEdgeRemoveCmd() *cobra.Command {
	var (
		edgeType string
		source   string
		target   string
	)

	removeCmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove an edge from the index",
		RunE: func(cmd *cobra.Command, args []string) error {
			if edgeType == "" || source == "" || target == "" {
				return fmt.Errorf("--type, --source, and --target are required")
			}

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

			edgeRepo := index.NewEdgeRepo(store)

			rows, listErr := edgeRepo.ListBySource(source)

			if listErr != nil {
				return listErr
			}

			cliExisting := filterCLI(rows)

			var kept []index.EdgeRow

			removed := 0

			for _, row := range cliExisting {
				if row.Type == edgeType && row.TargetID == target {
					removed++

					continue
				}

				kept = append(kept, row)
			}

			if removed == 0 {
				return fmt.Errorf("no CLI-added edge matches type=%q source=%q target=%q", edgeType, source, target)
			}

			// Re-number ordinals to keep sequence dense.
			renumbered := renumberByType(kept)

			if upsertErr := edgeRepo.UpsertAll(source, cliSourcePath, renumbered); upsertErr != nil {
				return upsertErr
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Removed edge %s: %s → %s\n", edgeType, source, target)

			return nil
		},
	}

	removeCmd.Flags().StringVar(&edgeType, "type", "", "edge type")
	removeCmd.Flags().StringVar(&source, "source", "", "source node id")
	removeCmd.Flags().StringVar(&target, "target", "", "target node id")

	return removeCmd
}

func renumberByType(rows []index.EdgeRow) []index.EdgeRow {
	counters := map[string]int{}
	out := make([]index.EdgeRow, len(rows))

	for index, row := range rows {
		out[index] = row
		out[index].Ordinal = counters[row.Type]
		counters[row.Type]++
	}

	return out
}
```

- [ ] **Step 8: Wire `newEdgeCmd` into root**

In `cmd/tusk/root.go`, add `rootCmd.AddCommand(newEdgeCmd())` alongside the existing registrations. Note that `newEdgeListCmd` is referenced by `newEdgeCmd`; you'll implement it in Task 12. For now, create a stub `cmd/tusk/cmd_edge_list.go` so the package compiles:

```go
package main

import "github.com/spf13/cobra"

// stub — replaced in Task 12
func newEdgeListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List edges (Task 12)",
		RunE:  func(cmd *cobra.Command, args []string) error { return nil },
	}
}
```

- [ ] **Step 9: Run, verify pass**

```bash
go test ./cmd/tusk/... -run "TestEdgeAddCmd|TestEdgeRemoveCmd"
```

The add test uses `edge list` to verify — but `edge list` is currently stubbed. Add will pass; remove uses the same stubbed list. Both add and remove tests will compile but their assertions about list output will fail until Task 12 lands.

Adjust: this Task's commit should land Tasks 11 + 12 atomically because the tests rely on `edge list` being functional. **Implement Task 12 now**, then run all three test files (`add`, `remove`, `list`) together. Two commits, one for add+remove, one for list, in that order. The package compiles cleanly between them because the list stub returns `nil`.

Continue to Task 12; come back here after Task 12 is implemented to commit Task 11.

- [ ] **Step 10: Commit Task 11 (after Task 12 lands and all three tests pass)**

```bash
git add cmd/tusk/cmd_edge.go cmd/tusk/cmd_edge_add.go cmd/tusk/cmd_edge_remove.go cmd/tusk/cmd_edge_add_test.go cmd/tusk/cmd_edge_remove_test.go cmd/tusk/cmd_node_create_test.go cmd/tusk/root.go
git commit -m "feat(cli): tusk edge add and tusk edge remove (CLI-tracked edges)"
```

---

## Task 12: CLI — `tusk edge list`

**Files:**
- Modify: `cmd/tusk/cmd_edge_list.go` (replace stub with real implementation)
- Create: `cmd/tusk/cmd_edge_list_test.go`

- [ ] **Step 1: Write the failing test — `cmd/tusk/cmd_edge_list_test.go`**

```go
package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestEdgeListCmd_FiltersByFromTypeAndTo(test *testing.T) {
	initWorkspaceWithManifest(test, edgeManifestBody())

	for _, args := range [][]string{
		{"node", "create", "--type", "ticket", "--title", "A", "--path", "tickets/a.md"},
		{"node", "create", "--type", "ticket", "--title", "B", "--path", "tickets/b.md"},
		{"node", "create", "--type", "ticket", "--title", "C", "--path", "tickets/c.md"},
		{"edge", "add", "--type", "blocks", "--source", "tickets/a", "--target", "tickets/b"},
		{"edge", "add", "--type", "parent", "--source", "tickets/a", "--target", "tickets/c"},
	} {
		cmd := newRootCmd()
		cmd.SetArgs(args)

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("setup %v: %v", args, execErr)
		}
	}

	{
		out := &bytes.Buffer{}
		cmd := newRootCmd()
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"edge", "list", "--from", "tickets/a"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("list --from: %v", execErr)
		}

		body := out.String()

		if strings.Count(body, "tickets/a") < 2 {
			test.Errorf("expected at least 2 rows with source tickets/a:\n%s", body)
		}
	}

	{
		out := &bytes.Buffer{}
		cmd := newRootCmd()
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"edge", "list", "--type", "blocks"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("list --type: %v", execErr)
		}

		body := out.String()

		if !strings.Contains(body, "blocks") || strings.Contains(body, "parent") {
			test.Errorf("expected blocks-only:\n%s", body)
		}
	}

	{
		out := &bytes.Buffer{}
		cmd := newRootCmd()
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"edge", "list", "--to", "tickets/c"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("list --to: %v", execErr)
		}

		body := out.String()

		if !strings.Contains(body, "tickets/c") || strings.Contains(body, "tickets/b") {
			test.Errorf("expected target=tickets/c only:\n%s", body)
		}
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./cmd/tusk/... -run TestEdgeListCmd
```

Expected: FAIL — the stub returns nil with empty output.

- [ ] **Step 3: Replace the stub `cmd/tusk/cmd_edge_list.go`**

```go
package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newEdgeListCmd() *cobra.Command {
	var (
		fromFilter string
		toFilter   string
		typeFilter string
	)

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List edges from the index",
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

			edgeRepo := index.NewEdgeRepo(store)

			rows, queryErr := selectEdges(edgeRepo, fromFilter, toFilter, typeFilter)

			if queryErr != nil {
				return queryErr
			}

			tab := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

			fmt.Fprintln(tab, "TYPE\tSOURCE\tTARGET\tORDINAL\tSOURCE_PATH")

			for _, row := range rows {
				fmt.Fprintf(tab, "%s\t%s\t%s\t%d\t%s\n", row.Type, row.SourceID, row.TargetID, row.Ordinal, row.SourcePath)
			}

			return tab.Flush()
		},
	}

	listCmd.Flags().StringVar(&fromFilter, "from", "", "filter to edges originating from this source id")
	listCmd.Flags().StringVar(&toFilter, "to", "", "filter to edges targeting this id")
	listCmd.Flags().StringVar(&typeFilter, "type", "", "filter by edge type")

	return listCmd
}

// selectEdges picks the most-selective listing query that satisfies the active
// filter set. Plan 4 generalizes this through the filter grammar.
func selectEdges(repo *index.EdgeRepo, fromID, toID, edgeType string) ([]index.EdgeRow, error) {
	switch {
	case fromID != "":
		rows, err := repo.ListBySource(fromID)
		if err != nil {
			return nil, err
		}
		return narrow(rows, toID, edgeType), nil
	case toID != "":
		rows, err := repo.ListByTarget(toID)
		if err != nil {
			return nil, err
		}
		return narrow(rows, "", edgeType), nil
	case edgeType != "":
		return repo.ListByType(edgeType)
	}

	// Unfiltered: no list-all method on EdgeRepo (intentional — full-table
	// scans are anti-shaped). Plan 4 will provide this through the filter
	// engine. For Plan 2, require at least one filter.
	return nil, fmt.Errorf("specify at least one of --from, --to, --type")
}

func narrow(rows []index.EdgeRow, toID, edgeType string) []index.EdgeRow {
	var out []index.EdgeRow

	for _, row := range rows {
		if toID != "" && row.TargetID != toID {
			continue
		}

		if edgeType != "" && row.Type != edgeType {
			continue
		}

		out = append(out, row)
	}

	return out
}
```

- [ ] **Step 4: Run all CLI tests**

```bash
go test ./cmd/tusk/... -v
```

Expected: all PASS — Plan 1b's CLI tests + Plan 2's add/remove/list edge tests.

- [ ] **Step 5: Commit Task 11 + Task 12 in two commits, in order**

First Task 11 (without `cmd_edge_list.go` since the stub-then-real swap in Task 12 lands it), then Task 12.

Concretely:

```bash
# Task 11 — add and remove + the test helper change
git add cmd/tusk/cmd_edge.go cmd/tusk/cmd_edge_add.go cmd/tusk/cmd_edge_remove.go cmd/tusk/cmd_edge_add_test.go cmd/tusk/cmd_edge_remove_test.go cmd/tusk/cmd_node_create_test.go cmd/tusk/root.go
# (do NOT include cmd_edge_list.go yet — its stub stays uncommitted)
git commit -m "feat(cli): tusk edge add and tusk edge remove (CLI-tracked edges)"

# Task 12 — list (replaces stub with real implementation)
git add cmd/tusk/cmd_edge_list.go cmd/tusk/cmd_edge_list_test.go
git commit -m "feat(cli): tusk edge list with --from / --to / --type filters"
```

(The Task 11 commit will leave the package non-compiling because `newEdgeListCmd` is referenced but undefined; you avoid this by writing the stub `cmd_edge_list.go` first as part of Task 11's working tree, but staging it under Task 12. If your git workflow rejects intermediate non-compiling commits via lefthook's `pre-commit` `test` hook, fall back to a single combined commit titled `feat(cli): tusk edge add/remove/list (CLI-tracked edges + filters)` that bundles both.)

> **Implementer note**: lefthook's pre-commit `test` hook runs `go test ./...` on each commit. If the Task 11 commit's content can't compile (because `cmd_edge_list.go`'s stub isn't staged), the hook will fail. The pragmatic path is **the combined-commit fallback**: ship Task 11 + Task 12 as a single commit titled `feat(cli): tusk edge add/remove/list with filters`. Use the combined approach unless you have a specific reason to split.

---

## Task 13: End-to-end edges smoke test

**Files:**
- Create: `cmd/tusk/e2e_edges_test.go`

- [ ] **Step 1: Write the e2e test — `cmd/tusk/e2e_edges_test.go`**

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestE2E_EdgesLifecycle(test *testing.T) {
	tmpDir := initWorkspaceWithManifest(test, edgeManifestBody())

	// 1) Create a small graph.
	for _, args := range [][]string{
		{"node", "create", "--type", "ticket", "--title", "Epic", "--path", "tickets/epic.md"},
		{"node", "create", "--type", "ticket", "--title", "Foo", "--path", "tickets/foo.md"},
		{"node", "create", "--type", "ticket", "--title", "Bar", "--path", "tickets/bar.md"},
	} {
		cmd := newRootCmd()
		cmd.SetArgs(args)

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("create %v: %v", args, execErr)
		}
	}

	// 2) Drop a frontmatter-edge node externally — `parent` and a wikilink.
	external := filepath.Join(tmpDir, "tickets/child.md")

	body := []byte(`---
type: ticket
title: Child
parent: tickets/epic
---

This child references [[tickets/foo]] in its body.
`)

	if writeErr := os.WriteFile(external, body, 0o644); writeErr != nil {
		test.Fatalf("write external: %v", writeErr)
	}

	// 3) Reindex — should pick up the parent edge AND the references edge.
	{
		cmd := newRootCmd()
		cmd.SetArgs([]string{"reindex"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("reindex: %v", execErr)
		}
	}

	// 4) edge list --from tickets/child shows both edges.
	{
		out := &bytes.Buffer{}
		cmd := newRootCmd()
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"edge", "list", "--from", "tickets/child"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("list child edges: %v", execErr)
		}

		body := out.String()

		if !bytes.Contains(out.Bytes(), []byte("parent")) {
			test.Errorf("missing parent edge:\n%s", body)
		}

		if !bytes.Contains(out.Bytes(), []byte("references")) {
			test.Errorf("missing references edge:\n%s", body)
		}
	}

	// 5) Add a CLI-tracked edge: foo blocks bar.
	{
		cmd := newRootCmd()
		cmd.SetArgs([]string{"edge", "add", "--type", "blocks", "--source", "tickets/foo", "--target", "tickets/bar"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("edge add: %v", execErr)
		}
	}

	// 6) edge list --type blocks shows only foo→bar.
	{
		out := &bytes.Buffer{}
		cmd := newRootCmd()
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"edge", "list", "--type", "blocks"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("list blocks: %v", execErr)
		}

		if !bytes.Contains(out.Bytes(), []byte("tickets/foo")) || !bytes.Contains(out.Bytes(), []byte("tickets/bar")) {
			test.Errorf("missing foo→bar:\n%s", out.String())
		}
	}

	// 7) Try to introduce a cycle: bar blocks foo. Acyclic should reject it.
	{
		cmd := newRootCmd()
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"edge", "add", "--type", "blocks", "--source", "tickets/bar", "--target", "tickets/foo"})

		if execErr := cmd.Execute(); execErr == nil {
			test.Fatalf("expected cycle error on bar→foo blocks")
		}
	}

	// 8) Remove foo→bar.
	{
		cmd := newRootCmd()
		cmd.SetArgs([]string{"edge", "remove", "--type", "blocks", "--source", "tickets/foo", "--target", "tickets/bar"})

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("edge remove: %v", execErr)
		}
	}

	// 9) Delete the child file and reindex; edges should be gone.
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

	{
		out := &bytes.Buffer{}
		cmd := newRootCmd()
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"edge", "list", "--from", "tickets/child"})

		// Empty result is acceptable (cmd may return error "no rows" or just print
		// only the header). Acceptable as long as the body doesn't have "parent" /
		// "references" rows.
		_ = cmd.Execute()

		if bytes.Contains(out.Bytes(), []byte("references")) || bytes.Contains(out.Bytes(), []byte("parent")) {
			test.Errorf("child edges should be gone, list still shows them:\n%s", out.String())
		}
	}
}
```

- [ ] **Step 2: Run all tests**

```bash
make test
make vet
```

Expected: all PASS, exit 0.

- [ ] **Step 3: Manual smoke from a clean checkout**

```bash
make build
SMOKE=/tmp/tusk-2-smoke
rm -rf $SMOKE && mkdir -p $SMOKE && cd $SMOKE

/workspaces/tusk/bin/tusk init --name smoke
cat > tusk.toml <<'EOF'
[workspace]
name = "smoke"

[edge-types.parent]
from = ["ticket"]
to = ["ticket", "project"]
cardinality = "many-to-one"

[edge-types.blocks]
from = ["ticket"]
to = ["ticket"]
cardinality = "many-to-many"
acyclic = true

[edge-types.references]
from = ["*"]
to = ["*"]
cardinality = "many-to-many"
EOF

/workspaces/tusk/bin/tusk node create --type ticket --title "Epic" --path tickets/epic.md
/workspaces/tusk/bin/tusk node create --type ticket --title "Foo" --path tickets/foo.md
/workspaces/tusk/bin/tusk node create --type ticket --title "Bar" --path tickets/bar.md

cat > tickets/child.md <<'EOF'
---
type: ticket
title: Child
parent: tickets/epic
---

References [[tickets/foo]] in body.
EOF

/workspaces/tusk/bin/tusk reindex
/workspaces/tusk/bin/tusk edge list --from tickets/child
/workspaces/tusk/bin/tusk edge add --type blocks --source tickets/foo --target tickets/bar
/workspaces/tusk/bin/tusk edge list --type blocks

cd /workspaces/tusk
```

Expected: every command succeeds; the child node has both `parent → tickets/epic` and `references → tickets/foo` edges; `blocks` list shows foo→bar.

- [ ] **Step 4: Commit**

```bash
git add cmd/tusk/e2e_edges_test.go
git commit -m "test(cli): end-to-end edges lifecycle covering frontmatter, wikilinks, CLI add/remove, cycle, delete"
```

---

## Task 14: Final verification + push + open stacked PR

**Files:** none (publication only)

- [ ] **Step 1: Full sweep**

```bash
make test
make vet
make lint
```

Expected: all exit 0.

- [ ] **Step 2: Inspect commit graph**

```bash
git log feat/plan-1b..HEAD --oneline
```

Expected: ~12-14 commits between `feat/plan-1b` and `HEAD` (one per task in this plan, modulo Task 11/12's combined commit).

- [ ] **Step 3: Push branch**

```bash
git push -u origin feat/plan-2
```

- [ ] **Step 4: Open the stacked PR (`feat/plan-2` → `feat/plan-1b`)**

```bash
gh pr create --draft --base feat/plan-1b --head feat/plan-2 --title "Tusk v1 — Plan 2: edges + relationships" --body "$(cat <<'EOF'
## Summary

Tusk v1 — Plan 2: turn isolated nodes into a typed graph.

After this PR: frontmatter edge keys (declared in the manifest's \`[edge-types.<name>]\` blocks) and body wikilinks (\`[[id]]\`) materialize as typed rows in a new \`edges\` table; \`tusk edge add/remove/list\` operates on edges; \`NodeService.Create\` and \`reindex.Run\` write edges with manifest-driven legality and cycle-detection enforcement.

**Stacked on:** #352 (Plan 1b — first node lifecycle). Merge order: #351 → main, #352 → v1, then this PR → feat/plan-1b.

## What lands

- **Manifest extension** — \`[edge-types.<name>]\` blocks with \`from\`/\`to\`/\`cardinality\`/\`ordered\`/\`inverse\`/\`acyclic\`; loader validates cardinality enum and non-empty \`from\`/\`to\`.
- **Index extension** — new \`edges\` table with type/source/target/ordinal/source_path columns, four indexes for the standard query patterns.
- **EdgeRepo** — \`UpsertAll\`, \`ListBySource\` / \`ListByTarget\` / \`ListByType\`, \`DeleteBySource\`. UpsertAll is keyed on (source_id, source_path) so frontmatter edges and CLI-added edges co-exist without clobbering.
- **node.ResolveEdges** — splits edge-shaped frontmatter keys out of \`Node.Properties\` into \`Node.Edges\` based on the manifest's edge-type registry.
- **node.ExtractWikilinks** — regex over body, fenced-code-block-aware, dedupes targets.
- **node.ValidateEdges + DetectCycle** — pure validators called before persistence; checks source/target type legality (with \`*\` wildcard support) and DFS cycle detection on \`Acyclic\` edge types.
- **NodeService.Create** — resolves edges, materializes wikilinks as \`references\` edges (when declared), validates legality, runs cycle detection, then persists nodes + edges.
- **reindex.Run** — same edge pipeline against on-disk files; deletes edges for removed nodes.
- **CLI** — \`tusk edge add\` (with cycle detection on \`Acyclic\` types), \`tusk edge remove\`, \`tusk edge list\` with \`--from\` / \`--to\` / \`--type\` filters.

## Out of scope (later plans)

- \`tags: [...]\` shorthand and tag-node auto-creation → **Plan 7** (type-pack territory).
- File watcher, \`.gitignore\` parsing, advisory lockfile, rename-rewrite pipeline → **Plan 3**.
- Filter grammar (\`<edge-name>->\`, \`<-\`, multi-hop, AND/OR/NOT) → **Plan 4**.
- Inverse edge derivation in queries → **Plan 4**.
- Doctor warnings for dangling wikilink targets → **Plan 8**.

## Spec

[\`docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md\`](docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md) §6.3, §6.4, §7.4, §9.5.

## Plan

[\`docs/superpowers/plans/2026-05-05-tusk-v1-2-edges-and-relationships.md\`](docs/superpowers/plans/2026-05-05-tusk-v1-2-edges-and-relationships.md)

## Verification

- \`make build\` produces \`bin/tusk\`, exits 0.
- \`make test\` — 7 packages, all pass (workspace, manifest, index, node, reindex, cmd/tusk; index gains EdgeRepo tests; node gains edges + wikilinks tests).
- \`make vet\` clean.
- \`make lint\` reports \`0 issues\`.
- Pre-commit hooks ran on every commit; no \`--no-verify\`.
- End-to-end edges smoke covers: frontmatter edges, wikilink resolution, CLI add/remove, cycle rejection, node deletion → edge cleanup.
EOF
)"
```

Capture the PR URL.

- [ ] **Step 5: Verify**

```bash
gh pr view --json url,state,isDraft,baseRefName,headRefName | jq
```

Expected: state OPEN, isDraft true, base `feat/plan-1b`, head `feat/plan-2`.

---

## Self-Review Checklist

After writing the plan, before execution begins.

**Spec coverage:**
- [ ] §6.3 (frontmatter properties vs edges): ResolveEdges (Task 5).
- [ ] §6.4 (wikilinks): ExtractWikilinks + Service/reindex integration (Tasks 6, 8, 10).
- [ ] §7.4 (edge type declarations): manifest schema + loader validation (Tasks 1, 2).
- [ ] §9.5 (index schema with edges table): Task 3.
- [ ] Edge legality enforcement at write time: ValidateEdges + Service.Create wiring (Tasks 7, 8).
- [ ] Cycle detection on Acyclic edges: DetectCycle + Service hook (Tasks 7, 9, 11).
- [ ] CLI surface for edges: add/remove/list (Tasks 11, 12).
- [ ] Reindex picks up edges: Task 10.

**Out-of-scope guardrails:**
- [ ] No tags: [...] shorthand or tag-node auto-creation (deferred to Plan 7).
- [ ] No watcher / file rename pipeline (Plan 3).
- [ ] No filter grammar (Plan 4).
- [ ] No doctor / dangling reference warnings (Plan 8).

**Plan-shape:**
- [ ] No "TBD" / "fill in" placeholders.
- [ ] Every step has either complete code or an exact command.
- [ ] Every task ends with a commit step (Task 11/12 share the commit-shape note).
- [ ] Test code uses `test *testing.T` (≥ 2-character names per STYLE.md).
- [ ] Implementation code has blank lines around `if err != nil` guards.

**Type/name consistency:**
- [ ] `manifest.EdgeTypes` (map type) used uniformly across packages.
- [ ] `index.EdgeRow` field names (`Type`, `SourceID`, `TargetID`, `Ordinal`, `SourcePath`) match across EdgeRepo, NodeService, reindex, CLI.
- [ ] `node.ResolveEdges(node, edgeTypes)` signature matches across Service.Create and reindex.Run.
- [ ] `node.ValidateEdges(node, edgeTypes, EdgeContext)` signature matches its caller in Service.Create.
- [ ] `cliSourcePath` constant used uniformly in cmd_edge_*.go for CLI-attributed edges.
