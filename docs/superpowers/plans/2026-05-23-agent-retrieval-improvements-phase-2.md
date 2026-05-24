# Agent Retrieval Improvements — Phase 2 (Sub-Document AST) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Code blocks in this plan are **data-structure proofs only** — schemas, struct shapes, manifest examples, response envelopes. Implementation bodies are deliberately omitted; describe them in prose during execution.

**Spec:** `docs/superpowers/specs/2026-05-23-agent-retrieval-improvements-design.md` §5.

**Goal:** Make sub-document units (sections, paragraphs, list-items, code blocks, blockquotes, table cells) first-class nodes in the graph, backed by an AST parser with content-hash identity and outbound-only edges. Move embeddings from file-level to leaf-unit level.

**Architecture:** A new built-in type pack (`sub-document`) registers six node types plus the `contains` edge (with derived inverse `contained-by`). Markdown bodies are parsed into a deterministic AST; each AST node above the inline level becomes a row in the existing `nodes` table, identified by `<parent-file-id>#<content-hash>`. The reindex pipeline diffs sub-units per file on every parse — inserts new hashes, drops missing hashes (cascading edges and embeddings via foreign key). The chunker is replaced by AST-driven chunking for sub-unit workspaces; the existing `MarkdownRecursive` chunker stays as the back-compat path for workspaces that opt out. Queries return parent files with `matched_units` attached, weighted by heading level for sections.

**Tech Stack:** Go, SQLite (existing `internal/index`), goldmark or yuin/goldmark-meta for the markdown AST parser, BurntSushi/toml (manifest), existing `internal/embed` pipeline.

**Prerequisites:** Phase 1 complete (services + alias + context).

---

## Inherits From

P2 builds on P1's service-layer split. The implementer can assume:

- Every read-verb call path (`tusk_query`, `tusk_node_get`, `tusk_node_list`, `tusk_edge_list`, `tusk_doctor`, `tusk_status`) routes through a `<Verb>Run(ctx, runtime, request) (result, error)` service function in `internal/<owner>/`.
- The `cliregistry` package owns the read-only verb set and the positional-name registry.
- `<Verb>Request` structs already carry `Include []string` and `Fields []string` fields.
- A `internal/render/compact.go` compact renderer exists and is used by all read verbs.
- `tusk_run`, `tusk_context`, and the alias loader are in place; their behavior depends on the service layer.
- The filter grammar accepts `modified-since:<duration|date>`.
- The `MarkdownRecursive` chunker in `internal/embed/chunking.go` is the only embedding chunking strategy; it operates on whole-file bodies.
- The `embeddings` table is keyed by `(node_id, chunk_idx)`.
- The `nodes` table has columns `(id, type, path, title, properties_json, last_mtime, last_size, last_checksum)`.

---

## Task 1: Schema migration and `sub-document` built-in type pack

**Why this task exists:** Sub-units share the `nodes` table with file nodes. The table needs two new columns (`parent_id`, `ordinal`) and the embeddings table needs a primary-key migration. The new type pack must be registered before any parsing code can produce sub-unit rows.

**Files:**

- Modify: `internal/index/schema.go` (or wherever the SQLite DDL lives) to add columns and indexes.
- Modify: `internal/index/store.go` to run the migration idempotently at `Open`.
- Create: `internal/typepacks/subdocument/pack.go` — the new built-in type pack registering node types, the `contains` edge (with `inverse = "contained-by"`), and reserved properties.
- Modify: `internal/typepacks/registry.go` (or wherever built-in packs are wired up) to include `sub-document`.
- Modify: `internal/manifest/workspace.go` to parse `[workspace] sub-units = bool` with default `true`.
- Modify: `internal/doctor/...` to surface reserved-name conflicts between the built-in pack and user manifests.
- Tests: schema-migration tests (round-trip a legacy DB through the migration and verify); type-pack registration tests; manifest-load tests for the `sub-units` flag.

**Steps:**

- [ ] **Define the schema delta.** Two new nullable columns on `nodes` plus one composite index for subtree queries. The `embeddings` primary key changes from `(node_id, chunk_idx)` to `(node_id)`; `chunk_idx` is retained as a regular column for backwards compatibility (legacy rows have `chunk_idx > 0`; new rows are always `0`).

  Data-structure proof:

  ```sql
  ALTER TABLE nodes ADD COLUMN parent_id TEXT NULL;
  ALTER TABLE nodes ADD COLUMN ordinal   INTEGER NULL;
  CREATE INDEX IF NOT EXISTS nodes_parent_id_ordinal ON nodes(parent_id, ordinal);

  -- embeddings PK migration via rebuild (SQLite cannot drop PK in place):
  CREATE TABLE embeddings_new (
      node_id        TEXT PRIMARY KEY,
      chunk_idx      INTEGER NOT NULL DEFAULT 0,
      model          TEXT NOT NULL,
      content_hash   TEXT NOT NULL,
      vector         BLOB NOT NULL,
      dim            INTEGER NOT NULL
  );
  INSERT INTO embeddings_new (node_id, chunk_idx, model, content_hash, vector, dim)
      SELECT node_id, chunk_idx, model, content_hash, vector, dim FROM embeddings;
  DROP TABLE embeddings;
  ALTER TABLE embeddings_new RENAME TO embeddings;
  ```

  Add a foreign key from `embeddings(node_id)` to `nodes(id)` with `ON DELETE CASCADE` so that dropping a sub-unit row cascades its embedding.

  Also add a foreign key from `edges(source_id)` to `nodes(id)` with `ON DELETE CASCADE` so that dropping a sub-unit row cascades its outbound edges (per §5.5's "DELETE (cascades edges and embeddings)" guarantee).

- [ ] **Migration runner.** Extend the existing one-shot migration logic (per `docs/packages/index.md`, `Open` already runs an idempotent migration that dropped the legacy `ordinal` from `edges`). Add the P2 migration as a second idempotent step, gated by a check for the presence of `parent_id` on the `nodes` table. Test by opening a DB built on `main`, asserting the migration runs cleanly; opening the same DB twice and asserting the second open is a no-op.

- [ ] **Sub-document type pack.** Register the six node types and the `contains` edge as a built-in pack. The pack also declares the reserved property schema per §5.8 of the spec.

  Data-structure proof:

  ```go
  // internal/typepacks/subdocument/pack.go
  var Pack = typepacks.Pack{
      Name: "sub-document",
      NodeTypes: []typepacks.NodeType{
          {Name: "section",     Properties: []typepacks.Property{
              {Name: "heading-level", Type: "int", Required: true},
          }},
          {Name: "paragraph",   Properties: nil},
          {Name: "list-item",   Properties: []typepacks.Property{
              {Name: "checkbox", Type: "bool", Required: false},   // nullable
          }},
          {Name: "code-block",  Properties: []typepacks.Property{
              {Name: "lang", Type: "string", Required: false},
          }},
          {Name: "blockquote",  Properties: nil},
          {Name: "table-cell",  Properties: []typepacks.Property{
              {Name: "header",        Type: "bool",   Required: true},
              {Name: "row",           Type: "int",    Required: true},
              {Name: "column",        Type: "int",    Required: true},
              {Name: "column-header", Type: "string", Required: false},
          }},
      },
      EdgeTypes: []typepacks.EdgeType{
          {Name: "contains",
           From: []string{"*"},   // any file type can contain sub-units
           To:   []string{"section", "paragraph", "list-item", "code-block", "blockquote", "table-cell"},
           Cardinality: "one-to-many",
           Ordered: true,
           Inverse: "contained-by",
           Acyclic: true},
      },
  }
  ```

  The pack is included by the engine unconditionally when `[workspace] sub-units = true`. The user cannot disable individual node types within the pack; it's all-or-nothing.

- [ ] **Manifest flag.** Parse `[workspace] sub-units = bool` with default `true` for new workspaces and existing workspaces that don't set it. Test that `sub-units = false` causes the engine to skip type-pack registration.

- [ ] **Reserved-name conflict detection.** When loading user-declared node types, edge types, and properties (under `[node-types.*]`, `[edge-types.*]`), reject any declaration that conflicts with the sub-document pack's reserved names when `sub-units = true`. The reserved set is enumerated in §5.8 of the spec. Surface the conflict in doctor; the engine prefers the built-in declaration and ignores the user's override (do not crash, do not silently shadow).

- [ ] **Verification.** Build the binary, open an existing fixture workspace, confirm the migration ran (check schema via `sqlite3 .tusk/index.db ".schema nodes"`). Run `tusk doctor` on a workspace whose `tusk.toml` declares `[node-types.section]`; confirm the conflict surfaces. Toggle `sub-units = false`, restart, confirm no reserved-name conflicts arise.

- [ ] **Commit.** `feat(index): sub-document type pack and schema migration for sub-units`.

**Pitfalls:**

- SQLite cannot drop a primary key in place. The PK migration must rebuild the table. Test on a fixture with thousands of rows so you catch performance issues; in production this runs at next `tusk` invocation after upgrade.
- Foreign keys are off by default in SQLite. Ensure the engine sets `PRAGMA foreign_keys = ON` at `Open` — check whether `internal/index/store.go` already does this.
- The migration is one-way. Document this in the commit message; rolling back to a pre-P2 binary still works for reading (the new columns are nullable), but a downgrade does not undo the embeddings PK rebuild — old binaries that don't know about the new PK still work because `(node_id)` is a strict subset of `(node_id, chunk_idx)` for legacy rows where `chunk_idx = 0`.

---

## Task 2: Markdown AST parser and content-hash identity

**Why this task exists:** Sub-units must be derived deterministically from the file. The parser maps markdown into an AST of `section`, `paragraph`, `list-item`, `code-block`, `blockquote`, and `table-cell` nodes, each with a content hash. The parser is the source of truth for what counts as a unit.

**Files:**

- Create: `internal/subunit/parse.go` — the AST parser.
- Create: `internal/subunit/parse_test.go`.
- Create: `internal/subunit/hash.go` — content hashing with collision-suffix handling.
- Create: `internal/subunit/hash_test.go`.
- Create: `internal/subunit/ast.go` — the AST node types.
- Modify: `go.mod` to add `github.com/yuin/goldmark` (and optionally `goldmark-meta` if needed for tables).
- Tests: golden-file tests for several markdown fixtures (a short note; a long doc with nested headings; a doc with checkboxes; a doc with a table; a doc with nested blockquotes; a doc with code fences).

**Steps:**

- [ ] **Pick the markdown library.** `github.com/yuin/goldmark` is the most actively-maintained Go markdown parser and supports CommonMark plus GFM tables, task lists, and footnotes via extensions. Enable the table and task-list extensions (footnotes stay disabled since footnotes aren't units — see §5.1).

- [ ] **Define the AST node types.** A `Unit` is the in-memory representation of one sub-unit. It carries everything needed to write a row into the `nodes` table plus its embed payload.

  Data-structure proof:

  ```go
  // internal/subunit/ast.go
  type Kind string

  const (
      KindSection    Kind = "section"
      KindParagraph  Kind = "paragraph"
      KindListItem   Kind = "list-item"
      KindCodeBlock  Kind = "code-block"
      KindBlockquote Kind = "blockquote"
      KindTableCell  Kind = "table-cell"
  )

  type Unit struct {
      Kind         Kind
      Hash         string            // 12 hex chars; "#a1b2c3" form
      Ordinal      int               // depth-first index within the file, 0-based
      ParentHash   string            // hash of the containing section unit; "" for file-root direct children
      Text         string            // literal body text used for storage and display
      EmbedPayload string            // text used for embedding (synthesized for table-cell)
      Properties   map[string]any    // per-kind: heading-level, checkbox, lang, header, row, column, column-header
      Title        string            // first-line excerpt (used as the title column for the row)
  }
  ```

  `ParentHash` is a sub-unit-to-sub-unit field used only during parse to populate the `parent_id` column on insert: the parent of a paragraph nested under a section is that section's row; the parent of a top-level paragraph is the file node.

- [ ] **Walk the goldmark AST.** Implement a visitor that converts goldmark's tree into a flat `[]Unit` in depth-first traversal order. Sections wrap their descendants (so `section.ParentHash` is the enclosing section's hash, or empty if none). Leaves under a section have that section's hash as parent. Leaves under no section (the document body before the first heading) have empty parent — meaning the file node is their parent.

- [ ] **Content hashing.** Hash inputs per kind:
  - `section`: heading text + heading level + the body text of all descendants concatenated.
  - `paragraph`, `blockquote`: the rendered text.
  - `list-item`: checkbox state + rendered text.
  - `code-block`: language + rendered body.
  - `table-cell`: row index + column index + cell text + column-header.

  Use SHA-256, truncate to 12 hex chars for display. Always 12 chars. Add a fixture-based test for hash stability (same markdown → same hashes; different markdown → different hashes).

- [ ] **Collision handling.** Within a single file's `[]Unit`, two identical leaves can produce the same hash. After hashing all units, post-process: any duplicate hashes get a disambiguating ordinal suffix (`#a1b2c3-1`, `#a1b2c3-2`) appended to the second and subsequent occurrences. Ordering is by AST traversal order so the suffix is deterministic. Tests must cover the exactly-two-collisions and three-collisions cases.

- [ ] **Table-cell payload synthesis (per §5.6).** For each `table-cell` unit, set `EmbedPayload` to `"<column-header>: <cell-text>"` when `column-header` is non-empty and `header` is false. Header cells (the first row) embed their own text without prefix.

- [ ] **Edge derivation hook.** Add a per-`Unit` outbound-edge derivation that re-uses `internal/node`'s existing wikilink scanner (`ExtractWikilinks`). The hook returns `[]EdgeSpec` — for the reindex pipeline in Task 3 to use. Do NOT call into `edge_repo` here; this package is a pure parser.

  Data-structure proof:

  ```go
  // internal/subunit/ast.go
  type EdgeSpec struct {
      EdgeType string // populated by Task 3's caller based on manifest's wikilinks-enabled edges
      TargetID string // file id; sub-units never target sub-units (§5.4)
  }
  ```

- [ ] **Test corpus.** Add at least eight markdown fixtures in `internal/subunit/testdata/`:
  - Single paragraph file.
  - File with H1/H2/H3 nesting.
  - File with a task list including checked and unchecked.
  - File with a table including a header row.
  - File with a fenced code block.
  - File with a nested blockquote (verify flatten-to-one-unit per §5.1).
  - File with two identical paragraphs (verify the collision suffix).
  - File with wikilinks inside paragraphs (verify edge derivation).

  For each, assert the produced `[]Unit` shape: kinds, ordinals, hashes (golden), parent hashes, properties. Hashes are golden so the test catches accidental hash-input changes (which would invalidate every workspace's index on upgrade).

- [ ] **Verification.** Run `go test ./internal/subunit/...` — all green.

- [ ] **Commit.** `feat(subunit): markdown AST parser with content-hash identity`.

**Pitfalls:**

- Goldmark normalizes whitespace and line endings. Hash inputs must use the normalized form, never the raw bytes, so hashes are stable across line-ending differences (CRLF vs LF). Document this in a comment alongside the hash function.
- The blockquote-flatten behavior per §5.1: nested blockquotes count as one unit. The AST walker collapses them; the test fixture must verify.
- Tables: goldmark emits tables as a structured AST (Table → TableRow → TableCell). Map header cells to `header: true` and body cells to `header: false`; populate `row`, `column`, `column-header` from positions.
- Footnotes and horizontal rules: do not emit units for them (§5.1). Verify by adding a fixture with a horizontal rule between sections.

---

## Task 3: Reindex pipeline integration — hash diff, insert/delete, edge re-derivation

**Why this task exists:** With the parser and schema in place, the reindex pipeline (and watcher) must produce and maintain sub-unit rows. On every parse of a file, the pipeline diffs the new sub-unit hashes against existing rows, inserts new ones, deletes missing ones, and re-derives outbound edges for inserted units. This is what §5.5 of the spec describes.

**Files:**

- Modify: `internal/reindex/reindex.go` to invoke the sub-unit pipeline per file when `sub-units = true`.
- Modify: `internal/watcher/...` for the same — though if the watcher already routes file events through `reindex`, no change is needed.
- Create: `internal/subunit/sync.go` — the diff/insert/delete pipeline.
- Create: `internal/subunit/sync_test.go`.
- Modify: `internal/index/node_repo.go` (or equivalent) to expose `ListByParent(parentID) ([]NodeRow, error)` and `BulkUpsert([]NodeRow) error`.
- Modify: `internal/node/edges.go` to make `MaterializeWikilinks` accept a sub-unit source (it currently assumes a file source).
- Tests: end-to-end reindex tests with sub-units enabled, asserting the index state after parse and after edit.

**Steps:**

- [ ] **Index repo support.** Add `NodeRepo.ListByParent(parentID string) ([]NodeRow, error)` returning all sub-units of a given file. Add `NodeRepo.BulkUpsert` and `NodeRepo.BulkDelete` taking slices of IDs / rows. Use a single transaction per file's reindex; cap the in-memory batch size.

- [ ] **The diff algorithm.** Given the new `[]Unit` from Task 2's parser and the existing `[]NodeRow` from `ListByParent`:
  1. Compute the set of new hashes from the parser output and old hashes from the index.
  2. INSERT rows for hashes in new \ old (with their full property maps and parent_id).
  3. DELETE rows for hashes in old \ new. Foreign keys cascade their edges and embeddings.
  4. UPDATE `ordinal` for rows where the hash is in both but the ordinal changed. No other field needs touching because the hash gates content equality.

  This entire diff runs inside a single SQLite transaction. On any failure the transaction rolls back; the file's sub-unit state remains its prior valid value.

  Data-structure proof of the in/out contract:

  ```go
  // internal/subunit/sync.go
  type Sync struct {
      Repo     *index.NodeRepo
      EdgeRepo *index.EdgeRepo
      EmbedQ   *embed.Queue
      Manifest *manifest.Manifest
  }

  type SyncResult struct {
      Inserted int
      Deleted  int
      Reordered int
  }

  func (s *Sync) ApplyFile(ctx context.Context, fileID string, units []subunit.Unit) (*SyncResult, error)
  ```

- [ ] **Edge re-derivation for inserted units.** For each inserted unit, derive outbound edges from its body using `ExtractWikilinks` and `ResolveRefs` (the existing `internal/node` machinery, adapted to accept a sub-unit source ID). Edge types come from the manifest: every edge type with `wikilinks = true` materializes one edge per resolved wikilink target per inserted unit. Targets are always file IDs (per §5.4 — wikilinks never target sub-units).

- [ ] **Embedding enqueue.** Inserted units go into `embed_queue` with the parent file's mtime so the drainer picks them up in deterministic order. The drainer is the existing `internal/embed/drain.go` — no changes needed there in this task (Task 4 modifies the chunker, not the drainer).

- [ ] **`contains` and `contained-by` edge population.** For each inserted unit, write a `contains` edge from the parent file to the unit (cardinality one-to-many, ordered by the unit's `ordinal`). The inverse `contained-by` is derived by the existing grammar machinery — no explicit row needed (the `inverse` declaration in the type pack handles it).

  Wait — verify whether the existing grammar treats `inverse` as a query-time pseudo-edge or whether it requires materialized inverse rows. If the latter, also write the `contained-by` row. The implementer must confirm by inspecting `internal/filter` and `internal/index` behavior; document the choice in the commit message.

- [ ] **`tusk reindex` end-to-end test.** A fixture workspace with three files of varying structure. Run `tusk reindex`; assert the `nodes` table count grows by the expected number of sub-units; assert each file's sub-units are reachable via `ListByParent`. Edit one file (change one paragraph), re-run reindex, assert: that paragraph's row is replaced (different hash); the others are untouched; the file's embed_queue has one new entry.

- [ ] **Watcher integration check.** If the watcher already routes file events through the reindex pipeline, the sub-unit sync rides for free. If not, add the same call path. Test by writing to a markdown file in a fixture workspace while a `tusk mcp` (or `tusk watch`) process runs; confirm the sub-unit rows update within the debounce window.

- [ ] **Back-compat path.** When `[workspace] sub-units = false`, the reindex pipeline must skip the sub-unit sync entirely and behave as it does on current `main`. Add a regression test covering this path.

- [ ] **Verification.** Full Go test suite green. `tusk reindex` on a fixture workspace; inspect `sqlite3 .tusk/index.db "SELECT type, COUNT(*) FROM nodes GROUP BY type"` and confirm sub-unit types appear.

- [ ] **Commit.** `feat(reindex): sub-unit diff/insert/delete pipeline with edge re-derivation`.

**Pitfalls:**

- The `ResolveRefs` and `ResolveEdges` order is fiddly in the current code (per `docs/packages/node.md`: "two code paths with different value-source assumptions"). For sub-units, only wikilinks apply — there's no frontmatter to resolve refs from. Use `ExtractWikilinks` directly and pass the result through `MaterializeWikilinks` (or a sub-unit-aware variant) for each `wikilinks = true` edge type.
- Large files with many paragraphs will produce many embed_queue entries. Verify the embedder doesn't choke on a 500-unit file (the user's memory note about `OLLAMA_NUM_PARALLEL=4` is relevant). If serial embedding is too slow on first reindex of a sub-units-enabled vault, consider increasing the drainer's batch size — but stay out of scope here; document the observed perf as a follow-up.
- The `last_mtime` on sub-unit rows is the parent file's mtime per the schema. Don't try to derive a synthetic mtime for the unit itself.

---

## Task 4: AST-driven chunker replacing `MarkdownRecursive` for sub-unit workspaces

**Why this task exists:** §5.6 specifies that each leaf sub-unit is embedded as a single vector. The existing `MarkdownRecursive` chunker operates on whole-file bodies and produces multiple chunks per file. With sub-units enabled, each sub-unit becomes the chunk — no recursive splitting needed because the AST already gave us the boundaries.

**Files:**

- Modify: `internal/embed/chunking.go` — add an `ASTChunking` strategy that chunks based on sub-unit boundaries.
- Modify: `internal/embed/drain.go` (or wherever the chunker is invoked) to pick the AST chunker when sub-units are enabled.
- Tests: per-chunker tests; an embedder integration test that verifies the drainer produces one embedding per leaf unit.

**Steps:**

- [ ] **Define the strategy.** `ASTChunking` is a new implementation of the existing `ChunkingStrategy` interface. Unlike `MarkdownRecursive`, it doesn't operate on a payload byte slice — it operates on a sub-unit. The interface needs to be widened or a parallel interface introduced.

  Two viable shapes:

  **Option A:** Add a new method to `ChunkingStrategy` so it can opt into sub-unit-aware chunking when given the unit context. This is intrusive and forces every implementation to handle both modes.

  **Option B (preferred):** Introduce a separate `UnitChunker` interface; the drainer picks whichever applies based on `[workspace] sub-units`. `WholeDocument` and `MarkdownRecursive` stay as `ChunkingStrategy` implementations for the back-compat path. `ASTChunking` is a `UnitChunker`.

  Data-structure proof:

  ```go
  // internal/embed/chunking.go
  type UnitChunker interface {
      // Chunk returns the single payload the embedder should send for this unit.
      // (Always one chunk per unit; the interface returns []byte for parity with
      // ChunkingStrategy, but length is always 1.)
      Chunk(unit subunit.Unit) []byte
  }

  type ASTChunking struct{}

  func (ASTChunking) Chunk(unit subunit.Unit) []byte {
      // returns []byte(unit.EmbedPayload) — see §5.6 for table-cell synthesis,
      // which is already done by the parser (Task 2).
  }
  ```

- [ ] **Drainer integration.** In `internal/embed/drain.go`, when a queue entry is for a sub-unit row (detected by the row's `parent_id != NULL` or `type` matching one of the sub-document kinds), use `ASTChunking`. When it's a file-level row, use the current chunker.

  Since the drainer currently operates on file bodies, this needs care: load the sub-unit's `text` (or `embed_payload` if we add a column for the synthesized form), pass it through the chunker, send to Ollama. Decide at the start: store the synthesized embed payload on the `nodes` row, or recompute on demand. Storing is simpler (one column write per insert) and matches the parser's output. Recommend: store. Add an `embed_payload TEXT` column to `nodes` in the Task 1 migration retroactively if not already present — or, since this lands in Task 4, add it in Task 4's commit.

  Update the Task 1 acceptance criteria to include `embed_payload TEXT NULL`. (Implementer note: catch this when reviewing Task 1's diff before Task 4.)

- [ ] **Sub-units = false back-compat path.** When sub-units are disabled, the drainer continues to use `MarkdownRecursive` over file bodies, identical to current `main`. Add a regression test covering this.

- [ ] **Doctor for chunk size.** The current `MarkdownRecursive` has a hard `DefaultMaxBytes = 4000` cap (per `internal/embed/chunking.go`). Sub-unit leaves can in principle exceed this if a paragraph is very long. For sub-unit workspaces, doctor surfaces any unit whose `embed_payload` exceeds the Ollama model's context window (4000 bytes is a reasonable proxy; tune later if needed). This is a warning, not a fatal — the embedder truncates per current behavior.

- [ ] **Verification.** Re-index a fixture workspace with sub-units enabled. Confirm `SELECT COUNT(*) FROM embeddings` equals the count of leaf sub-units (paragraphs + list-items + code-blocks + blockquotes + table-cells). Confirm a query against Ollama actually runs and stores a vector (Ollama must be reachable for the integration test; gate on `OLLAMA_NUM_PARALLEL` env or skip if unreachable, mirroring existing patterns in `internal/embed`).

- [ ] **Commit.** `feat(embed): AST-driven chunker for sub-unit workspaces`.

**Pitfalls:**

- The Ollama embedder has a context window per model. `nomic-embed-text` (default per spec §10.5) is 2048 tokens (~4000 bytes). A long code block or pre-formatted paragraph can blow this. Verify the embedder's truncation behavior is sane — don't fail the embedding, just truncate per the existing path.
- The `chunk_idx` column on `embeddings` is now always 0 for sub-unit rows. Verify no part of the existing code branches on `chunk_idx > 0`. If so, that's a sub-units = true / false fork to clean up.

---

## Task 5: `tusk_query` surfacing — `matched_units`, heading-level weighting, `include = units`

**Why this task exists:** With sub-units in the index and embedded, `tusk_query` becomes the surfacing point for the rich graph. Semantic queries now rank over sub-unit embeddings; results group by parent file with the matching sub-units attached. Section scores aggregate descendant leaves with heading-level weighting. Structural queries can also opt into the sub-unit view via `include = [units]`.

**Files:**

- Modify: `internal/query/...` (or `internal/mcp/tools.go`'s query handler / the Task 1 service extraction) to:
  - Rank semantic queries over sub-unit embeddings.
  - Group by parent file.
  - Compute section scores per the weight table.
  - Honor `include = [units]` for structural queries.
- Modify: `internal/render/compact.go` to render `matched_units` with hierarchical indentation and the `section H<n>` decoration.
- Modify: `internal/filter/compile.go` to allow `type=section`, `type=paragraph`, etc. predicates (the type pack registration from Task 1 should already make these valid; verify and add a test if needed).
- Tests: query-result shape tests (JSON and compact), heading-weight tests with synthetic embeddings, direct-sub-unit-query tests.

**Steps:**

- [ ] **Update the query service.** When `semantic` is set, the candidate pool for cosine ranking is the `embeddings` table (which under sub-units-enabled holds only sub-unit rows). The structural filter (the `filter` argument) is applied first to narrow the candidate IDs; cosine ranks within. This is the same hybrid pattern as today (per §10.3 of the v1 spec) — only the candidate set has changed.

- [ ] **Group results by parent file.** After ranking, walk the ordered hit list and bucket hits under their `parent_id`. The output is an ordered slice of files, each with its `matched_units`. File order is by the maximum score of its sub-units. Within a file, sub-units are ordered by descending score.

- [ ] **Heading-level weighting for sections.** Per §5.7, sections aren't directly embedded — their score is `heading_weight × max(descendant_leaf.score)`. Compute this after the leaf-level cosine ranking: walk the candidate set, for each section sub-unit row, find its descendants' best score (use the `contains` edges or the `parent_id` denormalization), multiply by the heading weight, and add the section as a synthetic hit. The weights are fixed (§5.7):

  ```
  H1: 1.00, H2: 0.85, H3: 0.70, H4: 0.55, H5: 0.40, H6: 0.25
  ```

  Sections appear in `matched_units` alongside their descendant leaves. The file's `score` is the max across all its hits (sections and leaves).

- [ ] **`include = [units]` for structural queries.** When set on a non-semantic query, each returned file's `matched_units` array is populated with the file's full sub-unit list (no scoring; the `score` field is absent). The compact renderer collapses long lists with a `(N more)` tail past some threshold — pick a reasonable default like 20 — to keep agent output bounded. Document the behavior.

- [ ] **Direct sub-unit queries.** When the filter expression names a sub-unit type (`type=section`, `type=list-item checkbox=false`, `type=table-cell column-header="Status"`), the query returns sub-units directly as top-level result rows (no `matched_units` wrapping). The `parent_id` is included in the row.

- [ ] **JSON shape.** Matches the spec §5.7 exactly:

  ```json
  {
    "results": [
      {
        "id": "notes/auth-rfc",
        "type": "note",
        "title": "Auth RFC",
        "matched_units": [
          {"id": "notes/auth-rfc#a1b2c3", "type": "section", "heading_level": 2, "ordinal": 4,  "score": 0.86, "snippet": "..."},
          {"id": "notes/auth-rfc#b4d5e6", "type": "section", "heading_level": 3, "ordinal": 7,  "score": 0.74, "snippet": "..."},
          {"id": "notes/auth-rfc#d4e5f6", "type": "paragraph",                    "ordinal": 12, "score": 0.78, "snippet": "..."}
        ]
      }
    ]
  }
  ```

  `heading_level` appears only on `section` rows. `snippet` for sections is the first leaf descendant's text truncated; for leaves it's the row's `text` truncated.

- [ ] **Compact format.** Per §5.7:

  ```
  notes/auth-rfc                note         Auth RFC                                                       0.86
    → #a1b2c3                   section H2   "Decision: OAuth 2.1 with PKCE chosen for SSO migration..."    0.86
    → #b4d5e6                   section H3   "PKCE implementation: code-verifier generation..."             0.74
    → #d4e5f6                   paragraph    "Users with SSO accounts hit the password reset flow when..."  0.78
  ```

  Section rows show `section H<n>`; other leaves show just their type. The indentation is two spaces + arrow; pure markdown bullet style would be acceptable too but the arrow is closer to existing tusk render aesthetics.

- [ ] **Snippet rendering.** Already exists for semantic queries via `filter.RenderSnippetForQuery`. Sections lack their own embedding, so their snippet should be the first descendant leaf's snippet (the best-matching one if multiple matched). Add a helper that picks the snippet for any unit type.

- [ ] **End-to-end test.** Fixture workspace with a long auth-rfc note containing labeled H2/H3 sections and several paragraphs. Run a semantic query for "OAuth PKCE" and assert the JSON matches an expected shape (heading levels, ordinals, score ordering). Run the same query with `--format compact` and compare against a golden text fixture.

- [ ] **Verification.** Full Go test suite green. Manual: `tusk query --semantic "auth flow" --include body,edges` against the fixture, eyeball the output.

- [ ] **Commit.** `feat(query): sub-unit matched_units with heading-level weighting`.

**Pitfalls:**

- Section aggregation is O(K × descendants_per_section). For K=50 candidates over a typical vault this is fine. Don't pre-compute section scores at index time; compute at query time.
- The compact renderer's existing column alignment (Task 1.3 in P1) was tuned for one-row-per-result. The hierarchical form for `matched_units` is a different layout — keep them as two render modes (`compactFile` vs `compactWithUnits`) selected automatically based on whether the row carries `matched_units`.
- If the workspace has `sub-units = false`, the query path stays as it was on current `main` — single results per file, no `matched_units`. Add a regression test.

---

## Task 6: Doctor sub-unit pane and final integration verification

**Why this task exists:** §5.9 of the spec mandates a sub-unit health pane in doctor. Plus this task is the integration smoke test — Phase 2 must be acceptance-tested end-to-end before the implementer agent declares done.

**Files:**

- Modify: `internal/doctor/...` to add the sub-unit pane.
- Modify: docs/CLI man-page outputs via `make docs` (the docs-drift pre-push hook will demand this).
- Tests: doctor-output golden tests; an integration test that exercises P2 end-to-end.

**Steps:**

- [ ] **Sub-unit metrics.** Add these to the doctor report:
  - Total sub-unit count, broken down by kind (sections, paragraphs, list-items, code-blocks, blockquotes, table-cells).
  - Hash-collision count per file (should be ~0 in normal vaults). Surface only when > 0.
  - Orphaned sub-units (rows with `parent_id` pointing to a missing file). Should be impossible if the cascade-delete foreign keys are configured; surface only when > 0 as a "this indicates a bug" warning.
  - Embed-queue depth split by file vs sub-unit.
  - Reserved-name conflicts from Task 1 (recap any user-declared types/edges/properties that shadow built-ins).

- [ ] **Manifest warning surfacing.** If `[workspace] sub-units = false` is set but the workspace already has sub-unit rows from a prior run with `sub-units = true`, doctor warns: "sub-units disabled but index contains N sub-unit rows; run `tusk reindex --force` to clean up." Don't auto-clean.

- [ ] **Integration test.** A `make test` target (or just a `go test ./...` invocation) that:
  - Reindexes a fixture workspace with sub-units enabled.
  - Asserts row counts, edge counts, embedding counts.
  - Runs a semantic query and asserts the matched_units shape.
  - Runs a direct sub-unit query (`type=list-item checkbox=false`) and asserts results.
  - Runs `tusk doctor` and asserts the sub-unit pane appears.

  This is the acceptance test for P2.

- [ ] **Regenerate CLI docs.** `make docs`. Commit any regenerated man pages and markdown CLI docs (per the docs-drift pre-push hook).

- [ ] **Verification.** Full Go test suite green. Run the integration test described above against a sample vault.

- [ ] **Commit.** `feat(doctor): sub-unit health pane and integration smoke`.

**Pitfalls:**

- If the fixture vault is small, embedding may complete before the assertions run. Add a brief drain wait or seed the embeddings synthetically for deterministic tests.

---

## Self-Review

1. **Spec coverage.** §5.1 → Tasks 1, 2. §5.2 → Task 2. §5.3 → Task 1. §5.4 → Tasks 2, 3 (edge re-derivation in 3, parser hook in 2). §5.5 → Task 3. §5.6 → Tasks 2 (payload synthesis), 4 (chunker). §5.7 → Task 5. §5.8 → Tasks 1 (reserved names), 5 (direct queries). §5.9 → Task 6. ✓
2. **Placeholder scan.** No TBDs; one note in Task 4 to retro-add `embed_payload` to Task 1's migration — surface this explicitly during execution. ✓
3. **Type consistency.** `Unit`, `EdgeSpec` defined in Task 2 and consumed by Tasks 3, 4. `Sync`, `SyncResult` in Task 3. `ASTChunking` in Task 4. ✓

---

## Changes Introduced

**New files:**
- `internal/subunit/ast.go`, `parse.go`, `parse_test.go`, `hash.go`, `hash_test.go`, `sync.go`, `sync_test.go`
- `internal/typepacks/subdocument/pack.go`
- New `internal/embed/chunking.go` `ASTChunking` type (existing file)

**Modified interfaces:**
- `internal/index/store.go` — `Open` runs the new migration.
- `internal/index/node_repo.go` — gains `ListByParent`, `BulkUpsert`, `BulkDelete`.
- `internal/reindex/reindex.go` — invokes `subunit.Sync` per file when `sub-units = true`.
- `internal/embed/drain.go` — picks the chunker based on workspace flag.
- `internal/query/...` (created in P1 Task 1) — produces `matched_units` envelope.
- `internal/render/compact.go` — renders matched_units hierarchically.
- `internal/filter/...` — accepts sub-unit type names in `type=...` predicates (likely zero code change because the type pack registration carries the names automatically; verify and test).

**New environment variables:** none.

**Schema migrations (P2 → forward only, no rollback path):**
- `nodes`: add `parent_id TEXT NULL`, `ordinal INTEGER NULL`, `embed_payload TEXT NULL`.
- `nodes`: index `(parent_id, ordinal)`.
- `embeddings`: rebuild with PK `(node_id)`, retain `chunk_idx` column for back-compat reads.
- `edges`: foreign key `source_id` → `nodes(id)` with `ON DELETE CASCADE`.
- `embeddings`: foreign key `node_id` → `nodes(id)` with `ON DELETE CASCADE`.

**Added dependencies:**
- `github.com/yuin/goldmark` and any extensions needed for GFM tables and task lists.

**Bridge code introduced:** None. The `sub-units = true/false` flag is a permanent dual-path, not a bridge — workspaces may legitimately want to opt out.

**Reserved names added to manifest validation:**
- Node types: `section`, `paragraph`, `list-item`, `code-block`, `blockquote`, `table-cell`.
- Edge types: `contains`, `contained-by`.
- Properties: `heading-level`, `checkbox`, `lang`, `header`, `row`, `column`, `column-header`.

**Doctor surfaces added:**
- Sub-unit count by kind.
- Hash-collision count.
- Orphaned sub-units.
- Embed-queue depth split by file vs sub-unit.
- `sub-units` disable-with-existing-rows warning.

---

## User-Visible Behaviors That Must Still Work

After P2 is applied, the implementer agent confirms each of these:

- All P1 user-visible behaviors continue to work unchanged.
- A workspace with `[workspace] sub-units = false` behaves identically to current `main` — no sub-unit rows, file-level embeddings via `MarkdownRecursive`.
- A workspace with `sub-units = true` (default) reindexes successfully; running `tusk node list` returns file rows as before; `tusk query --semantic` returns file rows with `matched_units` attached.
- Editing one paragraph in a markdown file results in only that paragraph's sub-unit row being replaced (other sub-units of the same file untouched).
- `tusk query type=list-item checkbox=false` returns all open todos across the vault as top-level sub-unit rows.
- `tusk query type=section heading-level<=2` returns only H1/H2 sections.
- `tusk doctor` reports the sub-unit pane.
- Wikilinks inside paragraph bodies materialize as edges from the paragraph sub-unit to the target file (not to any sub-unit).

If any of these fails, P2 is not done.
