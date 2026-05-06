# Tusk v1 — Plan 5: Semantic Retrieval

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship semantic retrieval over the v1 node index — manifest-driven embedding configuration, an Ollama-backed `Embedder`, an async embedding queue drained by `tusk reindex`, and a `--semantic` flag on `tusk query` that performs structural prefilter + cosine-similarity ranking.

**Architecture:** New `internal/embed/` package containing the `Embedder` interface, an Ollama HTTP implementation, a `ChunkingStrategy` interface with a `whole-document` default, and a payload builder that derives a node's embeddable text from frontmatter + body. Index gains two tables: `embeddings` (vector BLOB + content_hash) and `embed_queue` (pending nodes with attempts + last_error). `NodeService.Create` and `reindex.Run` enqueue affected nodes; `reindex.Run` drains the queue at the end of each pass by calling the active embedder per node. `tusk query --semantic <text>` parses any structural filter as a candidate set, then ranks candidates by cosine similarity to the embedded query string, returning top N (or all matched, sorted by score, if `--take` not given).

**Tech Stack:** Go 1.26 + the existing `internal/manifest`, `internal/index`, `internal/node`, `internal/reindex`, `internal/filter` packages. One new external dependency: none — `net/http` from stdlib drives Ollama. Vectors are stored as raw float32 BLOBs and compared in Go (pure-Go cosine; no `sqlite-vec`).

**Spec reference:** `docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md` §10 (retrieval), §13 (open questions).

**Style rules:** Code respects `STYLE.md` — minimum 2-character identifiers (`*testing.T` → `test *testing.T`), blank lines around `err` guards, named errors on shadow.

---

## File Structure

**Created:**
```
internal/embed/
  embedder.go            # Embedder interface + Vector type + helpers
  embedder_test.go
  ollama.go              # OllamaEmbedder (HTTP client against localhost:11434)
  ollama_test.go
  chunking.go            # ChunkingStrategy interface + WholeDocument
  chunking_test.go
  payload.go             # BuildPayload(node *node.Node) []byte (embed input)
  payload_test.go
  cosine.go              # CosineSimilarity(a, b []float32) float64
  cosine_test.go

internal/index/
  embedding_repo.go      # EmbeddingRow + EmbeddingRepo (Upsert, GetByNodeID, ListByNodeIDs, DeleteByNodeID)
  embedding_repo_test.go
  embed_queue_repo.go    # QueueRow + EmbedQueueRepo (Enqueue, Drain, MarkFailed, Depth)
  embed_queue_repo_test.go

internal/filter/
  semantic.go            # SemanticRanker.Rank([]Candidate, queryVector) []ScoredResult
  semantic_test.go

cmd/tusk/
  cmd_query_semantic_test.go  # tusk query --semantic e2e coverage
```

**Modified:**
```
internal/manifest/manifest.go        # add EmbeddingsSection + provider/model/endpoint/dim/api-key
internal/manifest/loader.go          # validate provider in {ollama}, dim > 0
internal/manifest/loader_test.go     # cover [embeddings] parsing + validation errors
internal/index/index.go              # add embeddings + embed_queue tables; chunk_idx schema column
internal/index/index_test.go         # assert embeddings + embed_queue tables present
internal/node/service.go             # Create enqueues node into embed_queue when EmbedQueue is configured
internal/node/service_test.go        # cover enqueue-on-create
internal/reindex/reindex.go          # Config gains EmbedQueue + Embedder + ChunkingStrategy; Run drains queue at end of pass
internal/reindex/reindex_test.go     # cover queue drain with mock embedder
cmd/tusk/cmd_reindex.go              # wire embedder + queue + chunker; honor --no-embed flag
cmd/tusk/cmd_query.go                # add --semantic <text> flag; route to semantic ranker
cmd/tusk/root.go                     # (no changes; commands already registered)
```

**Excluded for Plan 5** (deferred per spec §10):
- OpenAI / Voyage / Anthropic providers — Plan 5.x. Plan 5 ships only the Ollama provider; the `Embedder` interface admits future plugins without API changes.
- Bundled local ONNX model — v1.x.
- `tusk mcp` long-running drainer — Plan 6 (MCP server runs the worker continuously; Plan 5's drain is one-shot via `tusk reindex`).
- Embedding-aware re-embed on content drift (vector watcher) — Plan 8.
- `sqlite-vec` integration — out of scope; Plan 5 does pure-Go cosine over candidate set bounded by structural prefilter.

## Module Conventions for Plan 5

**Vector encoding.** Vectors are `[]float32`. On disk: little-endian raw bytes (4 bytes per dim). The `Embedder` returns `[]float32`; the repo serializes to/from BLOB via `encoding/binary`.

**Provider config.** Plan 5 supports `provider = "ollama"` only. The loader rejects other values with a clear error pointing at Plan 5.x.

**Embedder contract:**
```go
type Embedder interface {
    Embed(ctx context.Context, payload []byte) ([]float32, error)
    Model() string
    Dim() int
}
```

**ChunkingStrategy contract:**
```go
type ChunkingStrategy interface {
    Chunk(payload []byte) [][]byte
}
```

`WholeDocument{}` returns `[][]byte{payload}` — exactly one chunk per node. Future strategies plug in here.

**Payload builder:**
```go
func BuildPayload(loaded *node.Node) []byte
```

Produces the embed input as documented in spec §10.4: `[type] {type}\n[title] {title}\n<other frontmatter k=v lines>\n---\n{body}`.

**Semantic ranker contract:**
```go
type Candidate struct {
    NodeID  string
    Vector  []float32 // single chunk for Plan 5
}

type ScoredResult struct {
    NodeID string
    Score  float64
}

func Rank(candidates []Candidate, queryVector []float32) []ScoredResult
```

Ranks by descending cosine similarity. Caller applies LIMIT/OFFSET.

---

## Task 0: Pre-flight verification

- [ ] **Step 1: Confirm on `feat/plan-5` and clean tree**

```bash
git rev-parse --abbrev-ref HEAD
git status --short
git log --oneline -3
```

Expected: branch `feat/plan-5`; only the pre-existing devcontainer/gitignore unstaged changes (or empty); recent log starts with the v1 tip post-Plan-4 (`0176553 feat(v1): plan 4 — filter grammar (#355)`).

- [ ] **Step 2: Confirm prior tests pass**

```bash
make test
make vet
```

Expected: 12 packages green, vet clean.

---

## Task 1: Manifest — `[embeddings]` schema

**Files:** Modify `internal/manifest/manifest.go`.

- [ ] **Step 1: Read current manifest types**

```bash
cat internal/manifest/manifest.go
```

- [ ] **Step 2: Append `EmbeddingsSection` and wire it into `Manifest`**

In `internal/manifest/manifest.go`, add after `WorkspaceSection`:

```go
// EmbeddingsSection configures the active embedding provider.
//
// Plan 5 supports provider = "ollama" only; the loader rejects other values.
// API providers (openai/voyage/anthropic) land in Plan 5.x.
type EmbeddingsSection struct {
	Provider string `toml:"provider"`
	Model    string `toml:"model"`
	Endpoint string `toml:"endpoint"`
	Dim      int    `toml:"dim"`
	APIKey   string `toml:"api-key"`
}
```

Update `Manifest` to reference it:

```go
type Manifest struct {
	Workspace  WorkspaceSection    `toml:"workspace"`
	EdgeTypes  map[string]EdgeType `toml:"edge-types"`
	Embeddings EmbeddingsSection   `toml:"embeddings"`
}
```

- [ ] **Step 3: Verify package compiles**

```bash
go build ./internal/manifest/...
```

Expected: exits 0. (No tests added in this task; loader validation lands in Task 2.)

- [ ] **Step 4: Commit**

```bash
git add internal/manifest/manifest.go
git commit -m "feat(manifest): EmbeddingsSection schema for [embeddings] block"
```

---

## Task 2: Manifest loader — validate `[embeddings]`

**Files:** Modify `internal/manifest/loader.go`, `internal/manifest/loader_test.go`.

- [ ] **Step 1: Append failing tests**

```go
func TestLoad_ParsesEmbeddings(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "my-brain"

[embeddings]
provider = "ollama"
model = "nomic-embed-text"
endpoint = "http://localhost:11434"
dim = 768
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	if loaded.Embeddings.Provider != "ollama" {
		test.Errorf("Provider = %q", loaded.Embeddings.Provider)
	}

	if loaded.Embeddings.Dim != 768 {
		test.Errorf("Dim = %d", loaded.Embeddings.Dim)
	}
}

func TestLoad_RejectsUnknownEmbeddingsProvider(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "x"

[embeddings]
provider = "bogus"
model = "x"
dim = 768
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	_, loadErr := manifest.Load(manifestPath)

	if loadErr == nil {
		test.Fatalf("expected error for unknown provider")
	}
}

func TestLoad_RejectsZeroDim(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "x"

[embeddings]
provider = "ollama"
model = "x"
dim = 0
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	_, loadErr := manifest.Load(manifestPath)

	if loadErr == nil {
		test.Fatalf("expected error for dim = 0")
	}
}

func TestLoad_AcceptsAbsentEmbeddings(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "x"
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	if loaded.Embeddings.Provider != "" {
		test.Errorf("Provider should be empty when [embeddings] absent: %q", loaded.Embeddings.Provider)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/manifest/... -run TestLoad_(ParsesEmbeddings|RejectsUnknown|RejectsZero|AcceptsAbsent)
```

Expected: FAIL — loader doesn't validate `[embeddings]` yet.

- [ ] **Step 3: Extend `internal/manifest/loader.go`'s `validate` function**

Add after the existing edge-types loop:

```go
	if loaded.Embeddings.Provider != "" {
		if loaded.Embeddings.Provider != "ollama" {
			return fmt.Errorf("manifest: embeddings.provider = %q is not supported (Plan 5 supports \"ollama\" only; OpenAI/Voyage/Anthropic land in Plan 5.x)", loaded.Embeddings.Provider)
		}

		if loaded.Embeddings.Dim <= 0 {
			return fmt.Errorf("manifest: embeddings.dim must be > 0")
		}

		if loaded.Embeddings.Model == "" {
			return fmt.Errorf("manifest: embeddings.model must be set when embeddings.provider is configured")
		}
	}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/manifest/... -v
```

Expected: 10 PASS (6 prior + 4 new).

- [ ] **Step 5: Commit**

```bash
git add internal/manifest
git commit -m "feat(manifest): validate [embeddings] block (provider=ollama, dim>0, model set)"
```

---

## Task 3: Index — `embeddings` and `embed_queue` tables

**Files:** Modify `internal/index/index.go`, `internal/index/index_test.go`.

- [ ] **Step 1: Append failing tests**

```go
func TestOpen_CreatesEmbeddingsAndQueueTables(test *testing.T) {
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

	for _, required := range []string{"embeddings", "embed_queue"} {
		if !contains(tables, required) {
			test.Errorf("missing table %q in %v", required, tables)
		}
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/index/... -run TestOpen_CreatesEmbeddings
```

Expected: FAIL.

- [ ] **Step 3: Extend the `schema` const in `internal/index/index.go`**

Append the following SQL to the `schema` const, after the existing edges-related indexes and before `manifest_snapshot`:

```sql
CREATE TABLE IF NOT EXISTS embeddings (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	node_id      TEXT NOT NULL,
	chunk_idx    INTEGER NOT NULL DEFAULT 0,
	model        TEXT NOT NULL,
	content_hash TEXT NOT NULL,
	vector       BLOB NOT NULL,
	dim          INTEGER NOT NULL,
	UNIQUE(node_id, chunk_idx)
);

CREATE INDEX IF NOT EXISTS embeddings_node_idx ON embeddings(node_id);

CREATE TABLE IF NOT EXISTS embed_queue (
	node_id     TEXT PRIMARY KEY,
	enqueued_at INTEGER NOT NULL,
	attempts    INTEGER NOT NULL DEFAULT 0,
	last_error  TEXT
);
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/index/... -v
```

Expected: prior tests still pass + the new TestOpen_CreatesEmbeddingsAndQueueTables passes.

- [ ] **Step 5: Commit**

```bash
git add internal/index/index.go internal/index/index_test.go
git commit -m "feat(index): embeddings and embed_queue tables for semantic retrieval"
```

---

## Task 4: `EmbeddingRepo` — Upsert / GetByNodeID / ListByNodeIDs / DeleteByNodeID

**Files:** Create `internal/index/embedding_repo.go`, `internal/index/embedding_repo_test.go`.

- [ ] **Step 1: Write failing tests**

```go
package index_test

import (
	"reflect"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func newTestEmbeddingRepo(test *testing.T) *index.EmbeddingRepo {
	test.Helper()

	store := openTestIndex(test)

	return index.NewEmbeddingRepo(store)
}

func TestEmbeddingRepo_UpsertAndGet(test *testing.T) {
	repo := newTestEmbeddingRepo(test)

	row := index.EmbeddingRow{
		NodeID:       "tickets/foo",
		ChunkIdx:     0,
		Model:        "nomic-embed-text",
		ContentHash:  "abc123",
		Vector:       []float32{0.1, 0.2, 0.3},
		Dim:          3,
	}

	if upsertErr := repo.Upsert(row); upsertErr != nil {
		test.Fatalf("Upsert: %v", upsertErr)
	}

	loaded, getErr := repo.GetByNodeID("tickets/foo")

	if getErr != nil {
		test.Fatalf("GetByNodeID: %v", getErr)
	}

	if len(loaded) != 1 {
		test.Fatalf("len = %d, want 1", len(loaded))
	}

	if !reflect.DeepEqual(loaded[0].Vector, []float32{0.1, 0.2, 0.3}) {
		test.Errorf("Vector = %v, want [0.1 0.2 0.3]", loaded[0].Vector)
	}

	if loaded[0].Dim != 3 {
		test.Errorf("Dim = %d", loaded[0].Dim)
	}
}

func TestEmbeddingRepo_UpsertReplacesByContentHash(test *testing.T) {
	repo := newTestEmbeddingRepo(test)

	first := index.EmbeddingRow{
		NodeID: "x", ChunkIdx: 0, Model: "m", ContentHash: "h1",
		Vector: []float32{0.1}, Dim: 1,
	}

	if upsertErr := repo.Upsert(first); upsertErr != nil {
		test.Fatalf("first: %v", upsertErr)
	}

	second := index.EmbeddingRow{
		NodeID: "x", ChunkIdx: 0, Model: "m", ContentHash: "h2",
		Vector: []float32{0.5}, Dim: 1,
	}

	if upsertErr := repo.Upsert(second); upsertErr != nil {
		test.Fatalf("second: %v", upsertErr)
	}

	loaded, _ := repo.GetByNodeID("x")

	if len(loaded) != 1 {
		test.Fatalf("len = %d, want 1 after replace", len(loaded))
	}

	if loaded[0].ContentHash != "h2" {
		test.Errorf("ContentHash = %q, want h2", loaded[0].ContentHash)
	}
}

func TestEmbeddingRepo_ListByNodeIDs(test *testing.T) {
	repo := newTestEmbeddingRepo(test)

	for _, row := range []index.EmbeddingRow{
		{NodeID: "a", ChunkIdx: 0, Model: "m", ContentHash: "h", Vector: []float32{0.1}, Dim: 1},
		{NodeID: "b", ChunkIdx: 0, Model: "m", ContentHash: "h", Vector: []float32{0.2}, Dim: 1},
		{NodeID: "c", ChunkIdx: 0, Model: "m", ContentHash: "h", Vector: []float32{0.3}, Dim: 1},
	} {
		repo.Upsert(row)
	}

	loaded, listErr := repo.ListByNodeIDs([]string{"a", "c"})

	if listErr != nil {
		test.Fatalf("ListByNodeIDs: %v", listErr)
	}

	if len(loaded) != 2 {
		test.Errorf("len = %d, want 2", len(loaded))
	}
}

func TestEmbeddingRepo_DeleteByNodeID(test *testing.T) {
	repo := newTestEmbeddingRepo(test)

	repo.Upsert(index.EmbeddingRow{
		NodeID: "doomed", ChunkIdx: 0, Model: "m", ContentHash: "h",
		Vector: []float32{0.1}, Dim: 1,
	})

	if deleteErr := repo.DeleteByNodeID("doomed"); deleteErr != nil {
		test.Fatalf("DeleteByNodeID: %v", deleteErr)
	}

	loaded, _ := repo.GetByNodeID("doomed")

	if len(loaded) != 0 {
		test.Errorf("len = %d, want 0", len(loaded))
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/index/... -run TestEmbeddingRepo
```

Expected: FAIL — `index.EmbeddingRepo` undefined.

- [ ] **Step 3: Implement `internal/index/embedding_repo.go`**

```go
package index

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"fmt"
)

// EmbeddingRow is the index representation of one node's embedding (one chunk).
type EmbeddingRow struct {
	NodeID      string
	ChunkIdx    int
	Model       string
	ContentHash string
	Vector      []float32
	Dim         int
}

// EmbeddingRepo persists EmbeddingRow values in the SQLite index.
type EmbeddingRepo struct {
	db *sql.DB
}

// NewEmbeddingRepo constructs an EmbeddingRepo backed by idx.
func NewEmbeddingRepo(idx *Index) *EmbeddingRepo {
	return &EmbeddingRepo{db: idx.DB()}
}

// Upsert inserts or replaces the embedding for (node_id, chunk_idx).
func (repo *EmbeddingRepo) Upsert(row EmbeddingRow) error {
	encoded, encodeErr := encodeVector(row.Vector)

	if encodeErr != nil {
		return encodeErr
	}

	_, execErr := repo.db.Exec(`
		INSERT INTO embeddings (node_id, chunk_idx, model, content_hash, vector, dim)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id, chunk_idx) DO UPDATE SET
			model        = excluded.model,
			content_hash = excluded.content_hash,
			vector       = excluded.vector,
			dim          = excluded.dim
	`, row.NodeID, row.ChunkIdx, row.Model, row.ContentHash, encoded, row.Dim)

	if execErr != nil {
		return fmt.Errorf("embeddingRepo: upsert %s: %w", row.NodeID, execErr)
	}

	return nil
}

// GetByNodeID returns all embeddings (chunks) for nodeID, ordered by chunk_idx.
func (repo *EmbeddingRepo) GetByNodeID(nodeID string) ([]EmbeddingRow, error) {
	rows, queryErr := repo.db.Query(`
		SELECT node_id, chunk_idx, model, content_hash, vector, dim
		FROM embeddings
		WHERE node_id = ?
		ORDER BY chunk_idx
	`, nodeID)

	if queryErr != nil {
		return nil, fmt.Errorf("embeddingRepo: get %s: %w", nodeID, queryErr)
	}

	defer rows.Close()

	return scanEmbeddings(rows)
}

// ListByNodeIDs returns all embeddings whose node_id is in nodeIDs.
func (repo *EmbeddingRepo) ListByNodeIDs(nodeIDs []string) ([]EmbeddingRow, error) {
	if len(nodeIDs) == 0 {
		return nil, nil
	}

	placeholders := make([]byte, 0, len(nodeIDs)*2-1)
	args := make([]any, 0, len(nodeIDs))

	for index, nodeID := range nodeIDs {
		if index > 0 {
			placeholders = append(placeholders, ',')
		}

		placeholders = append(placeholders, '?')
		args = append(args, nodeID)
	}

	query := fmt.Sprintf(`SELECT node_id, chunk_idx, model, content_hash, vector, dim FROM embeddings WHERE node_id IN (%s) ORDER BY node_id, chunk_idx`, string(placeholders))

	rows, queryErr := repo.db.Query(query, args...)

	if queryErr != nil {
		return nil, fmt.Errorf("embeddingRepo: list: %w", queryErr)
	}

	defer rows.Close()

	return scanEmbeddings(rows)
}

// DeleteByNodeID removes every embedding for nodeID.
func (repo *EmbeddingRepo) DeleteByNodeID(nodeID string) error {
	_, execErr := repo.db.Exec(`DELETE FROM embeddings WHERE node_id = ?`, nodeID)

	if execErr != nil {
		return fmt.Errorf("embeddingRepo: delete %s: %w", nodeID, execErr)
	}

	return nil
}

func scanEmbeddings(rows *sql.Rows) ([]EmbeddingRow, error) {
	var results []EmbeddingRow

	for rows.Next() {
		var (
			row     EmbeddingRow
			encoded []byte
		)

		if scanErr := rows.Scan(&row.NodeID, &row.ChunkIdx, &row.Model, &row.ContentHash, &encoded, &row.Dim); scanErr != nil {
			return nil, fmt.Errorf("embeddingRepo: scan: %w", scanErr)
		}

		decoded, decodeErr := decodeVector(encoded, row.Dim)

		if decodeErr != nil {
			return nil, decodeErr
		}

		row.Vector = decoded
		results = append(results, row)
	}

	return results, rows.Err()
}

func encodeVector(vector []float32) ([]byte, error) {
	buffer := &bytes.Buffer{}

	for _, value := range vector {
		if writeErr := binary.Write(buffer, binary.LittleEndian, value); writeErr != nil {
			return nil, fmt.Errorf("embeddingRepo: encode vector: %w", writeErr)
		}
	}

	return buffer.Bytes(), nil
}

func decodeVector(encoded []byte, dim int) ([]float32, error) {
	if len(encoded)%4 != 0 {
		return nil, fmt.Errorf("embeddingRepo: vector blob length %d is not a multiple of 4", len(encoded))
	}

	if len(encoded)/4 != dim {
		return nil, fmt.Errorf("embeddingRepo: vector blob has %d float32s, dim says %d", len(encoded)/4, dim)
	}

	result := make([]float32, dim)
	reader := bytes.NewReader(encoded)

	for index := 0; index < dim; index++ {
		if readErr := binary.Read(reader, binary.LittleEndian, &result[index]); readErr != nil {
			return nil, fmt.Errorf("embeddingRepo: decode vector at index %d: %w", index, readErr)
		}
	}

	return result, nil
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/index/... -v
```

Expected: all index tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/index/embedding_repo.go internal/index/embedding_repo_test.go
git commit -m "feat(index): EmbeddingRepo with float32 BLOB encoding"
```

---

## Task 5: `EmbedQueueRepo` — Enqueue / Drain / MarkFailed / Depth

**Files:** Create `internal/index/embed_queue_repo.go`, `internal/index/embed_queue_repo_test.go`.

- [ ] **Step 1: Write failing tests**

```go
package index_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func newTestEmbedQueueRepo(test *testing.T) *index.EmbedQueueRepo {
	test.Helper()

	store := openTestIndex(test)

	return index.NewEmbedQueueRepo(store)
}

func TestEmbedQueueRepo_EnqueueAndDepth(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	for _, nodeID := range []string{"a", "b", "c"} {
		if enqueueErr := repo.Enqueue(nodeID); enqueueErr != nil {
			test.Fatalf("Enqueue %s: %v", nodeID, enqueueErr)
		}
	}

	depth, depthErr := repo.Depth()

	if depthErr != nil {
		test.Fatalf("Depth: %v", depthErr)
	}

	if depth != 3 {
		test.Errorf("Depth = %d, want 3", depth)
	}
}

func TestEmbedQueueRepo_EnqueueIsIdempotent(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	for index := 0; index < 3; index++ {
		if enqueueErr := repo.Enqueue("same"); enqueueErr != nil {
			test.Fatalf("Enqueue %d: %v", index, enqueueErr)
		}
	}

	depth, _ := repo.Depth()

	if depth != 1 {
		test.Errorf("Depth = %d, want 1 after idempotent enqueue", depth)
	}
}

func TestEmbedQueueRepo_DrainReturnsEnqueuedNodesAndRemovesThem(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	for _, nodeID := range []string{"a", "b", "c"} {
		repo.Enqueue(nodeID)
	}

	drained, drainErr := repo.Drain(10)

	if drainErr != nil {
		test.Fatalf("Drain: %v", drainErr)
	}

	if len(drained) != 3 {
		test.Errorf("len = %d, want 3", len(drained))
	}

	depth, _ := repo.Depth()

	if depth != 0 {
		test.Errorf("Depth after drain = %d, want 0", depth)
	}
}

func TestEmbedQueueRepo_DrainHonorsLimit(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	for _, nodeID := range []string{"a", "b", "c", "d", "e"} {
		repo.Enqueue(nodeID)
	}

	drained, _ := repo.Drain(2)

	if len(drained) != 2 {
		test.Errorf("len = %d, want 2", len(drained))
	}

	depth, _ := repo.Depth()

	if depth != 3 {
		test.Errorf("Depth after partial drain = %d, want 3", depth)
	}
}

func TestEmbedQueueRepo_MarkFailedKeepsInQueue(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	repo.Enqueue("flaky")

	if markErr := repo.MarkFailed("flaky", "ollama unreachable"); markErr != nil {
		test.Fatalf("MarkFailed: %v", markErr)
	}

	depth, _ := repo.Depth()

	if depth != 1 {
		test.Errorf("Depth after MarkFailed = %d, want 1 (still queued)", depth)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/index/... -run TestEmbedQueueRepo
```

Expected: FAIL.

- [ ] **Step 3: Implement `internal/index/embed_queue_repo.go`**

```go
package index

import (
	"database/sql"
	"fmt"
	"time"
)

// QueueRow represents a row in embed_queue.
type QueueRow struct {
	NodeID     string
	EnqueuedAt int64
	Attempts   int
	LastError  string
}

// EmbedQueueRepo persists pending embed jobs.
type EmbedQueueRepo struct {
	db *sql.DB
}

// NewEmbedQueueRepo constructs an EmbedQueueRepo backed by idx.
func NewEmbedQueueRepo(idx *Index) *EmbedQueueRepo {
	return &EmbedQueueRepo{db: idx.DB()}
}

// Enqueue inserts a row for nodeID. Idempotent — if the row exists, no-op.
func (repo *EmbedQueueRepo) Enqueue(nodeID string) error {
	_, execErr := repo.db.Exec(`
		INSERT INTO embed_queue (node_id, enqueued_at, attempts)
		VALUES (?, ?, 0)
		ON CONFLICT(node_id) DO NOTHING
	`, nodeID, time.Now().UnixNano())

	if execErr != nil {
		return fmt.Errorf("embedQueueRepo: enqueue %s: %w", nodeID, execErr)
	}

	return nil
}

// Drain returns up to limit rows oldest-first AND removes them from the queue
// in one transaction. Returns the rows so the caller can pass them to the
// embedder.
func (repo *EmbedQueueRepo) Drain(limit int) ([]QueueRow, error) {
	tx, beginErr := repo.db.Begin()

	if beginErr != nil {
		return nil, fmt.Errorf("embedQueueRepo: begin: %w", beginErr)
	}

	rows, queryErr := tx.Query(`
		SELECT node_id, enqueued_at, attempts, COALESCE(last_error, '')
		FROM embed_queue
		ORDER BY enqueued_at ASC
		LIMIT ?
	`, limit)

	if queryErr != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("embedQueueRepo: drain query: %w", queryErr)
	}

	var drained []QueueRow

	for rows.Next() {
		var row QueueRow

		if scanErr := rows.Scan(&row.NodeID, &row.EnqueuedAt, &row.Attempts, &row.LastError); scanErr != nil {
			rows.Close()
			_ = tx.Rollback()
			return nil, fmt.Errorf("embedQueueRepo: drain scan: %w", scanErr)
		}

		drained = append(drained, row)
	}

	rows.Close()

	for _, row := range drained {
		if _, deleteErr := tx.Exec(`DELETE FROM embed_queue WHERE node_id = ?`, row.NodeID); deleteErr != nil {
			_ = tx.Rollback()
			return nil, fmt.Errorf("embedQueueRepo: drain delete %s: %w", row.NodeID, deleteErr)
		}
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return nil, fmt.Errorf("embedQueueRepo: drain commit: %w", commitErr)
	}

	return drained, nil
}

// MarkFailed records a failure on the queue row, leaves it queued for retry.
func (repo *EmbedQueueRepo) MarkFailed(nodeID, errorMessage string) error {
	_, execErr := repo.db.Exec(`
		UPDATE embed_queue
		SET attempts = attempts + 1, last_error = ?
		WHERE node_id = ?
	`, errorMessage, nodeID)

	if execErr != nil {
		return fmt.Errorf("embedQueueRepo: mark failed %s: %w", nodeID, execErr)
	}

	return nil
}

// Depth returns the number of pending rows.
func (repo *EmbedQueueRepo) Depth() (int, error) {
	var depth int

	if scanErr := repo.db.QueryRow(`SELECT COUNT(*) FROM embed_queue`).Scan(&depth); scanErr != nil {
		return 0, fmt.Errorf("embedQueueRepo: depth: %w", scanErr)
	}

	return depth, nil
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/index/... -v
```

Expected: 5 PASS for EmbedQueueRepo + earlier passing.

- [ ] **Step 5: Commit**

```bash
git add internal/index/embed_queue_repo.go internal/index/embed_queue_repo_test.go
git commit -m "feat(index): EmbedQueueRepo with idempotent enqueue and transactional drain"
```

---

## Task 6: Embedder interface, ChunkingStrategy, payload builder

**Files:** Create `internal/embed/embedder.go`, `internal/embed/embedder_test.go`, `internal/embed/chunking.go`, `internal/embed/chunking_test.go`, `internal/embed/payload.go`, `internal/embed/payload_test.go`.

- [ ] **Step 1: Write failing tests**

`internal/embed/embedder_test.go`:

```go
package embed_test

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/internal/embed"
)

type stubEmbedder struct {
	model string
	dim   int
}

func (stub stubEmbedder) Embed(ctx context.Context, payload []byte) ([]float32, error) {
	return make([]float32, stub.dim), nil
}

func (stub stubEmbedder) Model() string { return stub.model }
func (stub stubEmbedder) Dim() int      { return stub.dim }

func TestEmbedder_InterfaceContract(test *testing.T) {
	var implementer embed.Embedder = stubEmbedder{model: "test", dim: 3}

	vector, embedErr := implementer.Embed(context.Background(), []byte("hello"))

	if embedErr != nil {
		test.Fatalf("Embed: %v", embedErr)
	}

	if len(vector) != 3 {
		test.Errorf("Vector len = %d", len(vector))
	}

	if implementer.Model() != "test" {
		test.Errorf("Model = %q", implementer.Model())
	}

	if implementer.Dim() != 3 {
		test.Errorf("Dim = %d", implementer.Dim())
	}
}
```

`internal/embed/chunking_test.go`:

```go
package embed_test

import (
	"reflect"
	"testing"

	"github.com/germanamz/tusk/internal/embed"
)

func TestWholeDocument_ReturnsSingleChunk(test *testing.T) {
	strategy := embed.WholeDocument{}

	chunks := strategy.Chunk([]byte("hello world"))

	if len(chunks) != 1 {
		test.Fatalf("len = %d, want 1", len(chunks))
	}

	if !reflect.DeepEqual(chunks[0], []byte("hello world")) {
		test.Errorf("chunks[0] = %q", chunks[0])
	}
}

func TestWholeDocument_HandlesEmptyPayload(test *testing.T) {
	chunks := embed.WholeDocument{}.Chunk([]byte(""))

	if len(chunks) != 1 || len(chunks[0]) != 0 {
		test.Errorf("got %v, want one empty chunk", chunks)
	}
}
```

`internal/embed/payload_test.go`:

```go
package embed_test

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/node"
)

func TestBuildPayload_IncludesTypeTitleAndBody(test *testing.T) {
	parsedNode := &node.Node{
		Type:  "ticket",
		Title: "Fix login",
		Properties: map[string]any{
			"type":     "ticket",
			"title":    "Fix login",
			"priority": 3,
		},
		Body: []byte("Body text here.\n"),
	}

	payload := embed.BuildPayload(parsedNode)

	rendered := string(payload)

	if !strings.Contains(rendered, "[type] ticket") {
		test.Errorf("payload missing type marker: %q", rendered)
	}

	if !strings.Contains(rendered, "[title] Fix login") {
		test.Errorf("payload missing title marker: %q", rendered)
	}

	if !strings.Contains(rendered, "Body text here.") {
		test.Errorf("payload missing body: %q", rendered)
	}

	if !strings.Contains(rendered, "priority=3") {
		test.Errorf("payload missing extra property: %q", rendered)
	}
}

func TestBuildPayload_StableOrder(test *testing.T) {
	// Same input should produce identical output across calls.
	parsedNode := &node.Node{
		Type: "note",
		Properties: map[string]any{
			"type":  "note",
			"title": "X",
			"a":     "1",
			"b":     "2",
			"c":     "3",
		},
		Body: []byte("body"),
	}

	first := embed.BuildPayload(parsedNode)
	second := embed.BuildPayload(parsedNode)

	if string(first) != string(second) {
		test.Errorf("BuildPayload not stable:\nfirst:  %q\nsecond: %q", first, second)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/embed/...
```

Expected: FAIL — package doesn't exist.

- [ ] **Step 3: Implement `internal/embed/embedder.go`**

```go
// Package embed contains the embedding pipeline for tusk's semantic retrieval:
// the Embedder interface, ChunkingStrategy interface, payload builder, and
// cosine similarity helper. Concrete embedder implementations live in this
// package (ollama.go); the interface admits more without touching the rest
// of the system.
package embed

import "context"

// Embedder produces a single vector per payload.
type Embedder interface {
	Embed(ctx context.Context, payload []byte) ([]float32, error)
	Model() string
	Dim() int
}
```

- [ ] **Step 4: Implement `internal/embed/chunking.go`**

```go
package embed

// ChunkingStrategy splits a payload into one or more chunks; each chunk is
// embedded independently. Plan 5 ships only WholeDocument; future strategies
// (fixed-token, sentence-aware, etc.) plug in here.
type ChunkingStrategy interface {
	Chunk(payload []byte) [][]byte
}

// WholeDocument is the default Plan 5 strategy: one chunk per node, the entire
// payload.
type WholeDocument struct{}

// Chunk implements ChunkingStrategy.
func (strategy WholeDocument) Chunk(payload []byte) [][]byte {
	return [][]byte{payload}
}
```

- [ ] **Step 5: Implement `internal/embed/payload.go`**

```go
package embed

import (
	"fmt"
	"sort"
	"strings"

	"github.com/germanamz/tusk/internal/node"
)

// BuildPayload renders a node into the canonical embed input.
//
// Format (per spec §10.4):
//
//	[type] {type}
//	[title] {title}
//	{remaining frontmatter properties as key=value, sorted by key}
//	---
//	{body}
//
// Order is stable so the resulting content_hash is reproducible.
func BuildPayload(parsedNode *node.Node) []byte {
	var builder strings.Builder

	fmt.Fprintf(&builder, "[type] %s\n", parsedNode.Type)

	if parsedNode.Title != "" {
		fmt.Fprintf(&builder, "[title] %s\n", parsedNode.Title)
	}

	keys := make([]string, 0, len(parsedNode.Properties))

	for key := range parsedNode.Properties {
		if key == "type" || key == "title" {
			continue
		}

		keys = append(keys, key)
	}

	sort.Strings(keys)

	for _, key := range keys {
		fmt.Fprintf(&builder, "%s=%v\n", key, parsedNode.Properties[key])
	}

	builder.WriteString("---\n")
	builder.Write(parsedNode.Body)

	return []byte(builder.String())
}
```

- [ ] **Step 6: Run, verify pass**

```bash
go test ./internal/embed/... -v
```

Expected: 5 PASS (1 embedder + 2 chunking + 2 payload).

- [ ] **Step 7: Commit**

```bash
git add internal/embed/embedder.go internal/embed/chunking.go internal/embed/payload.go internal/embed/embedder_test.go internal/embed/chunking_test.go internal/embed/payload_test.go
git commit -m "feat(embed): Embedder interface, WholeDocument chunking, payload builder"
```

---

## Task 7: Cosine similarity helper

**Files:** Create `internal/embed/cosine.go`, `internal/embed/cosine_test.go`.

- [ ] **Step 1: Write failing tests**

```go
package embed_test

import (
	"math"
	"testing"

	"github.com/germanamz/tusk/internal/embed"
)

func TestCosineSimilarity_OrthogonalVectorsZero(test *testing.T) {
	left := []float32{1, 0}
	right := []float32{0, 1}

	score := embed.CosineSimilarity(left, right)

	if math.Abs(score) > 1e-6 {
		test.Errorf("score = %v, want 0", score)
	}
}

func TestCosineSimilarity_IdenticalVectorsOne(test *testing.T) {
	vector := []float32{0.5, 0.5, 0.5}

	score := embed.CosineSimilarity(vector, vector)

	if math.Abs(score-1.0) > 1e-6 {
		test.Errorf("score = %v, want 1", score)
	}
}

func TestCosineSimilarity_OppositeVectorsNegativeOne(test *testing.T) {
	left := []float32{1, 0}
	right := []float32{-1, 0}

	score := embed.CosineSimilarity(left, right)

	if math.Abs(score+1.0) > 1e-6 {
		test.Errorf("score = %v, want -1", score)
	}
}

func TestCosineSimilarity_DimMismatchReturnsZero(test *testing.T) {
	left := []float32{1, 0, 0}
	right := []float32{0, 1}

	score := embed.CosineSimilarity(left, right)

	if score != 0 {
		test.Errorf("score = %v, want 0 (dim mismatch falls back to 0)", score)
	}
}

func TestCosineSimilarity_ZeroVectorReturnsZero(test *testing.T) {
	left := []float32{0, 0, 0}
	right := []float32{1, 1, 1}

	score := embed.CosineSimilarity(left, right)

	if score != 0 {
		test.Errorf("score = %v, want 0 (zero vector → 0)", score)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/embed/... -run TestCosineSimilarity
```

Expected: FAIL.

- [ ] **Step 3: Implement `internal/embed/cosine.go`**

```go
package embed

import "math"

// CosineSimilarity computes the cosine of the angle between two vectors.
//
// Returns 0 when the vectors have mismatched dimensions or either is the
// zero vector.
func CosineSimilarity(left, right []float32) float64 {
	if len(left) != len(right) || len(left) == 0 {
		return 0
	}

	var dot, leftMag, rightMag float64

	for index := 0; index < len(left); index++ {
		leftValue := float64(left[index])
		rightValue := float64(right[index])

		dot += leftValue * rightValue
		leftMag += leftValue * leftValue
		rightMag += rightValue * rightValue
	}

	if leftMag == 0 || rightMag == 0 {
		return 0
	}

	return dot / (math.Sqrt(leftMag) * math.Sqrt(rightMag))
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/embed/... -v
```

Expected: 5 cosine tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/embed/cosine.go internal/embed/cosine_test.go
git commit -m "feat(embed): pure-Go cosine similarity helper"
```

---

## Task 8: Ollama embedder

**Files:** Create `internal/embed/ollama.go`, `internal/embed/ollama_test.go`.

- [ ] **Step 1: Write failing tests using `httptest`**

```go
package embed_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/germanamz/tusk/internal/embed"
)

func TestOllamaEmbedder_PostsToEmbeddingsEndpoint(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/embeddings" {
			test.Errorf("path = %q, want /api/embeddings", request.URL.Path)
		}

		var payload struct {
			Model  string `json:"model"`
			Prompt string `json:"prompt"`
		}

		if decodeErr := json.NewDecoder(request.Body).Decode(&payload); decodeErr != nil {
			test.Fatalf("decode: %v", decodeErr)
		}

		if payload.Model != "test-model" {
			test.Errorf("model = %q, want test-model", payload.Model)
		}

		if payload.Prompt != "hello world" {
			test.Errorf("prompt = %q", payload.Prompt)
		}

		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"embedding": []float64{0.1, 0.2, 0.3},
		})
	}))

	defer server.Close()

	embedder := embed.NewOllamaEmbedder(embed.OllamaConfig{
		Endpoint: server.URL,
		Model:    "test-model",
		Dim:      3,
	})

	vector, embedErr := embedder.Embed(context.Background(), []byte("hello world"))

	if embedErr != nil {
		test.Fatalf("Embed: %v", embedErr)
	}

	if len(vector) != 3 {
		test.Fatalf("len = %d", len(vector))
	}

	if vector[0] != 0.1 || vector[1] != 0.2 || vector[2] != 0.3 {
		test.Errorf("vector = %v, want [0.1 0.2 0.3]", vector)
	}
}

func TestOllamaEmbedder_ErrorsOnNon200(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "boom", http.StatusInternalServerError)
	}))

	defer server.Close()

	embedder := embed.NewOllamaEmbedder(embed.OllamaConfig{
		Endpoint: server.URL,
		Model:    "x",
		Dim:      3,
	})

	_, embedErr := embedder.Embed(context.Background(), []byte("hello"))

	if embedErr == nil {
		test.Fatalf("expected error on 500")
	}
}

func TestOllamaEmbedder_ErrorsOnDimMismatch(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"embedding": []float64{0.1, 0.2}, // 2 dims, but config says 3
		})
	}))

	defer server.Close()

	embedder := embed.NewOllamaEmbedder(embed.OllamaConfig{
		Endpoint: server.URL,
		Model:    "x",
		Dim:      3,
	})

	_, embedErr := embedder.Embed(context.Background(), []byte("x"))

	if embedErr == nil {
		test.Fatalf("expected dim-mismatch error")
	}
}

func TestOllamaEmbedder_ModelAndDim(test *testing.T) {
	embedder := embed.NewOllamaEmbedder(embed.OllamaConfig{
		Endpoint: "http://example",
		Model:    "nomic-embed-text",
		Dim:      768,
	})

	if embedder.Model() != "nomic-embed-text" {
		test.Errorf("Model = %q", embedder.Model())
	}

	if embedder.Dim() != 768 {
		test.Errorf("Dim = %d", embedder.Dim())
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/embed/... -run TestOllamaEmbedder
```

Expected: FAIL.

- [ ] **Step 3: Implement `internal/embed/ollama.go`**

```go
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OllamaConfig configures an OllamaEmbedder.
type OllamaConfig struct {
	Endpoint string // e.g. "http://localhost:11434"
	Model    string // e.g. "nomic-embed-text"
	Dim      int    // expected vector dimension; mismatch is an error
}

// OllamaEmbedder calls Ollama's POST /api/embeddings to embed payloads.
type OllamaEmbedder struct {
	config OllamaConfig
	client *http.Client
}

// NewOllamaEmbedder constructs an OllamaEmbedder with sensible HTTP defaults.
func NewOllamaEmbedder(config OllamaConfig) *OllamaEmbedder {
	return &OllamaEmbedder{
		config: config,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Embed implements Embedder.
func (embedder *OllamaEmbedder) Embed(ctx context.Context, payload []byte) ([]float32, error) {
	body := struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
	}{
		Model:  embedder.config.Model,
		Prompt: string(payload),
	}

	encoded, marshalErr := json.Marshal(body)

	if marshalErr != nil {
		return nil, fmt.Errorf("ollama: marshal: %w", marshalErr)
	}

	request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, embedder.config.Endpoint+"/api/embeddings", bytes.NewReader(encoded))

	if requestErr != nil {
		return nil, fmt.Errorf("ollama: new request: %w", requestErr)
	}

	request.Header.Set("Content-Type", "application/json")

	response, doErr := embedder.client.Do(request)

	if doErr != nil {
		return nil, fmt.Errorf("ollama: post: %w", doErr)
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(response.Body)
		return nil, fmt.Errorf("ollama: HTTP %d: %s", response.StatusCode, string(responseBody))
	}

	var decoded struct {
		Embedding []float64 `json:"embedding"`
	}

	if decodeErr := json.NewDecoder(response.Body).Decode(&decoded); decodeErr != nil {
		return nil, fmt.Errorf("ollama: decode: %w", decodeErr)
	}

	if len(decoded.Embedding) != embedder.config.Dim {
		return nil, fmt.Errorf("ollama: returned %d dims, expected %d (model %q)", len(decoded.Embedding), embedder.config.Dim, embedder.config.Model)
	}

	vector := make([]float32, len(decoded.Embedding))

	for index, value := range decoded.Embedding {
		vector[index] = float32(value)
	}

	return vector, nil
}

// Model implements Embedder.
func (embedder *OllamaEmbedder) Model() string {
	return embedder.config.Model
}

// Dim implements Embedder.
func (embedder *OllamaEmbedder) Dim() int {
	return embedder.config.Dim
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/embed/... -v
```

Expected: 4 ollama tests pass + earlier embed tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/embed/ollama.go internal/embed/ollama_test.go
git commit -m "feat(embed): Ollama HTTP embedder against /api/embeddings"
```

---

## Task 9: NodeService — enqueue on Create

**Files:** Modify `internal/node/service.go`, `internal/node/service_test.go`.

- [ ] **Step 1: Append failing test**

```go
func TestService_CreateEnqueuesEmbedding(test *testing.T) {
	root := test.TempDir()

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)

	service := node.NewServiceWithEmbedQueue(root, nodeRepo, edgeRepo, manifest.EdgeTypes{}, queueRepo)

	if _, createErr := service.Create(node.CreateInput{
		RelPath: "n.md", Type: "note", Title: "N",
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	depth, _ := queueRepo.Depth()

	if depth != 1 {
		test.Errorf("queue depth = %d, want 1", depth)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Expected: FAIL — `node.NewServiceWithEmbedQueue` undefined.

- [ ] **Step 3: Extend `internal/node/service.go`**

Add a constructor variant and a queue field on `Service`:

```go
type Service struct {
	root       string
	repo       *index.NodeRepo
	edges      *index.EdgeRepo
	edgeTypes  manifest.EdgeTypes
	embedQueue *index.EmbedQueueRepo
}

// NewServiceWithEmbedQueue is like NewServiceWithManifest but also enqueues
// embed jobs on Create. When embedQueue is nil, behavior matches
// NewServiceWithManifest.
func NewServiceWithEmbedQueue(workspaceRoot string, repo *index.NodeRepo, edges *index.EdgeRepo, edgeTypes manifest.EdgeTypes, embedQueue *index.EmbedQueueRepo) *Service {
	return &Service{
		root:       workspaceRoot,
		repo:       repo,
		edges:      edges,
		edgeTypes:  edgeTypes,
		embedQueue: embedQueue,
	}
}
```

In `Service.Create`, after the node + edge upserts succeed and before the `return parsed, nil`, add:

```go
	if service.embedQueue != nil {
		if enqueueErr := service.embedQueue.Enqueue(parsed.ID); enqueueErr != nil {
			return nil, enqueueErr
		}
	}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/node/... -v
```

Expected: all node tests pass + the new enqueue test.

- [ ] **Step 5: Commit**

```bash
git add internal/node/service.go internal/node/service_test.go
git commit -m "feat(node): Service.Create enqueues embed job when EmbedQueue configured"
```

---

## Task 10: Reindex — drain embed queue at end of pass

**Files:** Modify `internal/reindex/reindex.go`, `internal/reindex/reindex_test.go`.

- [ ] **Step 1: Append failing test**

```go
type stubEmbedder struct {
	model string
	dim   int
	calls int
}

func (stub *stubEmbedder) Embed(ctx context.Context, payload []byte) ([]float32, error) {
	stub.calls++

	return make([]float32, stub.dim), nil
}

func (stub *stubEmbedder) Model() string { return stub.model }
func (stub *stubEmbedder) Dim() int      { return stub.dim }

func TestRun_DrainsEmbedQueue(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "a.md", "type: note\ntitle: A\n", "Body.\n")
	writeNode(test, root, "b.md", "type: note\ntitle: B\n", "Body.\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)
	embedder := &stubEmbedder{model: "test", dim: 3}

	cfg := reindex.Config{
		Root:           root,
		Repo:           nodeRepo,
		Edges:          edgeRepo,
		EdgeTypes:      manifest.EdgeTypes{},
		EmbedQueue:     queueRepo,
		EmbeddingRepo:  embeddingRepo,
		Embedder:       embedder,
		Chunker:        embed.WholeDocument{},
	}

	report, runErr := reindex.Run(cfg)

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.Indexed != 2 {
		test.Errorf("Indexed = %d, want 2", report.Indexed)
	}

	if embedder.calls != 2 {
		test.Errorf("embedder calls = %d, want 2", embedder.calls)
	}

	depth, _ := queueRepo.Depth()

	if depth != 0 {
		test.Errorf("queue depth = %d, want 0 after drain", depth)
	}

	loaded, _ := embeddingRepo.GetByNodeID("a")

	if len(loaded) != 1 || loaded[0].Dim != 3 {
		test.Errorf("embedding for a = %+v", loaded)
	}
}
```

Add the imports `context`, `github.com/germanamz/tusk/internal/embed` to `reindex_test.go`.

- [ ] **Step 2: Run, verify fail**

Expected: FAIL — `reindex.Config` lacks the new fields.

- [ ] **Step 3: Extend `internal/reindex/reindex.go`**

Update `Config`:

```go
type Config struct {
	Root            string
	Repo            *index.NodeRepo
	Edges           *index.EdgeRepo
	EdgeTypes       manifest.EdgeTypes
	WorkspaceIgnore []string

	// Embedding pipeline (optional). When all four are set, Run drains the
	// embed_queue at the end of the pass by invoking Embedder for each node.
	EmbedQueue    *index.EmbedQueueRepo
	EmbeddingRepo *index.EmbeddingRepo
	Embedder      embed.Embedder
	Chunker       embed.ChunkingStrategy
}
```

(Add `"github.com/germanamz/tusk/internal/embed"` to imports.)

After `Run` completes the upsert loop and stale-row cleanup but before `return report, nil`, add:

```go
	if config.EmbedQueue != nil && config.EmbeddingRepo != nil && config.Embedder != nil && config.Chunker != nil {
		// Enqueue every indexed node so the drain loop covers them.
		for path := range seenPaths {
			id := strings.TrimSuffix(path, ".md")
			_ = config.EmbedQueue.Enqueue(id)
		}

		drainErr := drainEmbedQueue(config)

		if drainErr != nil {
			return nil, drainErr
		}
	}
```

Add the helper function:

```go
const embedBatchSize = 50

func drainEmbedQueue(config Config) error {
	for {
		batch, drainErr := config.EmbedQueue.Drain(embedBatchSize)

		if drainErr != nil {
			return drainErr
		}

		if len(batch) == 0 {
			return nil
		}

		for _, queued := range batch {
			row, getErr := config.Repo.Get(queued.NodeID)

			if getErr != nil {
				// Node was deleted between enqueue and drain — skip.
				continue
			}

			content, readErr := os.ReadFile(filepath.Join(config.Root, row.Path))

			if readErr != nil {
				_ = config.EmbedQueue.Enqueue(queued.NodeID)
				_ = config.EmbedQueue.MarkFailed(queued.NodeID, readErr.Error())

				continue
			}

			parsed, parseErr := node.ParseFile(row.Path, content)

			if parseErr != nil {
				_ = config.EmbedQueue.Enqueue(queued.NodeID)
				_ = config.EmbedQueue.MarkFailed(queued.NodeID, parseErr.Error())

				continue
			}

			payload := embed.BuildPayload(parsed)
			chunks := config.Chunker.Chunk(payload)

			if len(chunks) == 0 {
				continue
			}

			vector, embedErr := config.Embedder.Embed(context.Background(), chunks[0])

			if embedErr != nil {
				_ = config.EmbedQueue.Enqueue(queued.NodeID)
				_ = config.EmbedQueue.MarkFailed(queued.NodeID, embedErr.Error())

				continue
			}

			contentHash := sha256.Sum256(payload)

			if upsertErr := config.EmbeddingRepo.Upsert(index.EmbeddingRow{
				NodeID:      queued.NodeID,
				ChunkIdx:    0,
				Model:       config.Embedder.Model(),
				ContentHash: hex.EncodeToString(contentHash[:]),
				Vector:      vector,
				Dim:         config.Embedder.Dim(),
			}); upsertErr != nil {
				return upsertErr
			}
		}
	}
}
```

(Add `"context"` and `"github.com/germanamz/tusk/internal/embed"` to imports if not already present.)

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/reindex/... -v
```

Expected: all reindex tests pass + the new drain test.

- [ ] **Step 5: Commit**

```bash
git add internal/reindex/reindex.go internal/reindex/reindex_test.go
git commit -m "feat(reindex): drain embed queue at end of pass via configured embedder"
```

---

## Task 11: Semantic ranker

**Files:** Create `internal/filter/semantic.go`, `internal/filter/semantic_test.go`.

- [ ] **Step 1: Write failing tests**

```go
package filter_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/filter"
)

func TestSemanticRank_OrdersByDescendingCosine(test *testing.T) {
	candidates := []filter.SemanticCandidate{
		{NodeID: "far", Vector: []float32{0, 1, 0}},
		{NodeID: "close", Vector: []float32{1, 0, 0}},
		{NodeID: "medium", Vector: []float32{0.7, 0.7, 0}},
	}

	query := []float32{1, 0, 0}

	ranked := filter.SemanticRank(candidates, query)

	if len(ranked) != 3 {
		test.Fatalf("len = %d", len(ranked))
	}

	if ranked[0].NodeID != "close" {
		test.Errorf("ranked[0] = %q, want close", ranked[0].NodeID)
	}

	if ranked[len(ranked)-1].NodeID != "far" {
		test.Errorf("last = %q, want far", ranked[len(ranked)-1].NodeID)
	}
}

func TestSemanticRank_HandlesEmptyCandidates(test *testing.T) {
	ranked := filter.SemanticRank(nil, []float32{1, 0})

	if len(ranked) != 0 {
		test.Errorf("len = %d", len(ranked))
	}
}

func TestSemanticRank_SkipsCandidatesWithMismatchedDim(test *testing.T) {
	candidates := []filter.SemanticCandidate{
		{NodeID: "good", Vector: []float32{1, 0}},
		{NodeID: "bad", Vector: []float32{1, 0, 0}}, // 3-dim
	}

	ranked := filter.SemanticRank(candidates, []float32{1, 0})

	if len(ranked) != 1 || ranked[0].NodeID != "good" {
		test.Errorf("ranked = %+v, want only good", ranked)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/filter/... -run TestSemanticRank
```

Expected: FAIL.

- [ ] **Step 3: Implement `internal/filter/semantic.go`**

```go
package filter

import (
	"sort"

	"github.com/germanamz/tusk/internal/embed"
)

// SemanticCandidate pairs a node id with its embedding vector for ranking.
type SemanticCandidate struct {
	NodeID string
	Vector []float32
}

// ScoredResult is one ranked candidate.
type ScoredResult struct {
	NodeID string
	Score  float64
}

// SemanticRank computes cosine similarity between each candidate's vector and
// queryVector, then returns the candidates sorted by descending score.
// Candidates whose vectors mismatch queryVector's dimension are silently
// skipped (they cannot be ranked).
func SemanticRank(candidates []SemanticCandidate, queryVector []float32) []ScoredResult {
	scored := make([]ScoredResult, 0, len(candidates))

	for _, candidate := range candidates {
		if len(candidate.Vector) != len(queryVector) {
			continue
		}

		score := embed.CosineSimilarity(candidate.Vector, queryVector)
		scored = append(scored, ScoredResult{NodeID: candidate.NodeID, Score: score})
	}

	sort.Slice(scored, func(left, right int) bool {
		return scored[left].Score > scored[right].Score
	})

	return scored
}
```

- [ ] **Step 4: Run, verify pass**

```bash
go test ./internal/filter/... -run TestSemanticRank -v
```

Expected: 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/filter/semantic.go internal/filter/semantic_test.go
git commit -m "feat(filter): SemanticRank computes cosine and sorts candidates"
```

---

## Task 12: `tusk query --semantic` and `tusk reindex` wiring

**Files:** Modify `cmd/tusk/cmd_query.go`, `cmd/tusk/cmd_reindex.go`. Create `cmd/tusk/cmd_query_semantic_test.go`.

- [ ] **Step 1: Write failing test**

```go
package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestQueryCmd_SemanticErrorsWhenEmbeddingsAbsent(test *testing.T) {
	initWorkspace(test)

	createCmd := newRootCmd()
	createCmd.SetArgs([]string{"node", "create", "--type", "note", "--title", "X", "--path", "x.md"})

	if execErr := createCmd.Execute(); execErr != nil {
		test.Fatalf("create: %v", execErr)
	}

	out := &bytes.Buffer{}

	queryCmd := newRootCmd()
	queryCmd.SetOut(out)
	queryCmd.SetErr(out)
	queryCmd.SetArgs([]string{"query", "type=note", "--semantic", "auth bug"})

	queryErr := queryCmd.Execute()

	if queryErr == nil {
		test.Fatalf("expected error when embeddings provider not configured")
	}

	if !strings.Contains(queryErr.Error(), "embeddings") {
		test.Errorf("error should mention embeddings: %v", queryErr)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./cmd/tusk/... -run TestQueryCmd_Semantic
```

Expected: FAIL — `--semantic` flag not implemented.

- [ ] **Step 3: Extend `cmd/tusk/cmd_query.go`**

Add the `--semantic` flag, route through the semantic ranker when set:

```go
	var semanticQuery string

	queryCmd.Flags().StringVar(&semanticQuery, "semantic", "", "rank results by cosine similarity to this query string (requires [embeddings] in tusk.toml)")
```

In the `RunE` body, after computing the structural SQL but before executing it, branch on `semanticQuery`:

```go
	if semanticQuery != "" {
		return runSemanticQuery(cmd, ws, loaded, sql, params, sortKeys, take, skip, semanticQuery)
	}
```

Add the helper at the end of the file:

```go
func runSemanticQuery(cmd *cobra.Command, ws *workspace.Workspace, loaded *manifest.Manifest, structuralSQL string, structuralParams []any, sortKeys []filter.SortKey, take, skip int, semanticQuery string) error {
	if loaded.Embeddings.Provider == "" {
		return fmt.Errorf("--semantic requires [embeddings] block in tusk.toml")
	}

	if loaded.Embeddings.Provider != "ollama" {
		return fmt.Errorf("--semantic: unsupported provider %q (Plan 5 supports ollama only)", loaded.Embeddings.Provider)
	}

	embedder := embed.NewOllamaEmbedder(embed.OllamaConfig{
		Endpoint: loaded.Embeddings.Endpoint,
		Model:    loaded.Embeddings.Model,
		Dim:      loaded.Embeddings.Dim,
	})

	queryVector, queryErr := embedder.Embed(context.Background(), []byte(semanticQuery))

	if queryErr != nil {
		return queryErr
	}

	store, openErr := index.Open(ws.IndexPath)

	if openErr != nil {
		return openErr
	}

	defer store.Close()

	rows, structuralErr := store.DB().Query(structuralSQL, structuralParams...)

	if structuralErr != nil {
		return structuralErr
	}

	defer rows.Close()

	var nodeIDs []string

	for rows.Next() {
		var (
			rowID, rowType, rowPath, rowTitle, propertiesRaw, lastChecksum string
			lastMtime, lastSize                                            int64
		)

		if scanErr := rows.Scan(&rowID, &rowType, &rowPath, &rowTitle, &propertiesRaw, &lastMtime, &lastSize, &lastChecksum); scanErr != nil {
			return scanErr
		}

		nodeIDs = append(nodeIDs, rowID)
	}

	embeddingRepo := index.NewEmbeddingRepo(store)
	loadedRows, loadErr := embeddingRepo.ListByNodeIDs(nodeIDs)

	if loadErr != nil {
		return loadErr
	}

	candidates := make([]filter.SemanticCandidate, 0, len(loadedRows))

	for _, embeddingRow := range loadedRows {
		candidates = append(candidates, filter.SemanticCandidate{
			NodeID: embeddingRow.NodeID,
			Vector: embeddingRow.Vector,
		})
	}

	ranked := filter.SemanticRank(candidates, queryVector)

	if take > 0 {
		startIdx := skip

		if startIdx > len(ranked) {
			startIdx = len(ranked)
		}

		endIdx := startIdx + take

		if endIdx > len(ranked) {
			endIdx = len(ranked)
		}

		ranked = ranked[startIdx:endIdx]
	}

	tab := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

	_, _ = fmt.Fprintln(tab, "ID\tSCORE")

	for _, scored := range ranked {
		_, _ = fmt.Fprintf(tab, "%s\t%.4f\n", scored.NodeID, scored.Score)
	}

	_ = sortKeys // reserved for hybrid sort+semantic; currently semantic ordering wins.

	return tab.Flush()
}
```

(Add `"context"` and `"github.com/germanamz/tusk/internal/embed"` to imports.)

- [ ] **Step 4: Wire `tusk reindex` to drain the queue**

In `cmd/tusk/cmd_reindex.go`, when the manifest declares an embeddings provider, construct an embedder and pass it through:

Inside the existing `withWorkspaceLock` body, before the `reindex.Run` call:

```go
		var embedder embed.Embedder
		var chunker embed.ChunkingStrategy
		var embedQueue *index.EmbedQueueRepo
		var embeddingRepo *index.EmbeddingRepo

		if loaded.Embeddings.Provider == "ollama" {
			embedder = embed.NewOllamaEmbedder(embed.OllamaConfig{
				Endpoint: loaded.Embeddings.Endpoint,
				Model:    loaded.Embeddings.Model,
				Dim:      loaded.Embeddings.Dim,
			})
			chunker = embed.WholeDocument{}
			embedQueue = index.NewEmbedQueueRepo(store)
			embeddingRepo = index.NewEmbeddingRepo(store)
		}

		report, runErr := reindex.Run(reindex.Config{
			Root:            ws.Root,
			Repo:            index.NewNodeRepo(store),
			Edges:           index.NewEdgeRepo(store),
			EdgeTypes:       loaded.EdgeTypes,
			WorkspaceIgnore: loaded.Workspace.Ignore,
			EmbedQueue:      embedQueue,
			EmbeddingRepo:   embeddingRepo,
			Embedder:        embedder,
			Chunker:         chunker,
		})
```

Add `"github.com/germanamz/tusk/internal/embed"` to imports.

- [ ] **Step 5: Run, verify pass**

```bash
go test ./cmd/tusk/... -v
```

Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add cmd/tusk/cmd_query.go cmd/tusk/cmd_reindex.go cmd/tusk/cmd_query_semantic_test.go
git commit -m "feat(cli): tusk query --semantic with cosine ranking; tusk reindex wires embedder"
```

---

## Task 13: End-to-end + final verify + push + open stacked PR

**Files:** Create `cmd/tusk/e2e_semantic_test.go`.

- [ ] **Step 1: Write the e2e test (uses an httptest-backed Ollama mock)**

```go
package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestE2E_SemanticRetrieval(test *testing.T) {
	tmpDir := initWorkspace(test)

	// Stand up a mock Ollama. Returns a vector based on the prompt's first byte
	// so different inputs produce orthogonal-ish vectors.
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			Prompt string `json:"prompt"`
		}

		_ = json.NewDecoder(request.Body).Decode(&payload)

		first := byte(0)

		if len(payload.Prompt) > 0 {
			first = payload.Prompt[0]
		}

		// Three-dim vector: emphasizes a different axis per first letter.
		vector := []float64{0, 0, 0}

		switch {
		case first == 'a' || first == 'A':
			vector[0] = 1
		case first == 'b' || first == 'B':
			vector[1] = 1
		default:
			vector[2] = 1
		}

		_ = json.NewEncoder(writer).Encode(map[string]any{"embedding": vector})
	}))

	defer server.Close()

	manifestBody := `[workspace]
name = "test"

[embeddings]
provider = "ollama"
model = "test-model"
endpoint = "` + server.URL + `"
dim = 3
`

	if writeErr := os.WriteFile(filepath.Join(tmpDir, "tusk.toml"), []byte(manifestBody), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	for _, args := range [][]string{
		{"node", "create", "--type", "note", "--title", "Apples", "--path", "a.md"},
		{"node", "create", "--type", "note", "--title", "Bananas", "--path", "b.md"},
		{"node", "create", "--type", "note", "--title", "Cherries", "--path", "c.md"},
	} {
		cmd := newRootCmd()
		cmd.SetArgs(args)

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("setup %v: %v", args, execErr)
		}
	}

	reindexCmd := newRootCmd()
	reindexCmd.SetArgs([]string{"reindex"})

	if execErr := reindexCmd.Execute(); execErr != nil {
		test.Fatalf("reindex: %v", execErr)
	}

	out := &bytes.Buffer{}

	queryCmd := newRootCmd()
	queryCmd.SetOut(out)
	queryCmd.SetErr(out)
	queryCmd.SetArgs([]string{"query", "type=note", "--semantic", "apple varieties"})

	if execErr := queryCmd.Execute(); execErr != nil {
		test.Fatalf("query --semantic: %v", execErr)
	}

	body := out.String()

	// "apple varieties" starts with 'a', so the mock returns [1,0,0]; node "a"
	// (Apples) was embedded with the same vector during reindex (its title
	// starts with 'A'), so it should rank first.
	firstResult := firstNonHeaderLine(body)

	if !strings.HasPrefix(firstResult, "a") {
		test.Errorf("expected 'a' to rank first, got body:\n%s", body)
	}
}

// firstNonHeaderLine returns the first line of body after the table header.
func firstNonHeaderLine(body string) string {
	lines := strings.Split(body, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "ID") {
			continue
		}

		return trimmed
	}

	return ""
}
```

- [ ] **Step 2: Run all tests + sweep**

```bash
make test
make vet
make lint
```

Expected: all exit 0.

- [ ] **Step 3: Commit e2e**

```bash
git add cmd/tusk/e2e_semantic_test.go
git commit -m "test(cli): e2e semantic retrieval with mock Ollama server"
```

- [ ] **Step 4: Inspect commits**

```bash
git log v1..HEAD --oneline
```

Expected: 13 commits.

- [ ] **Step 5: Push**

```bash
git push -u origin feat/plan-5
```

- [ ] **Step 6: Open stacked PR**

```bash
gh pr create --draft --base v1 --head feat/plan-5 --title "feat(v1): plan 5 — semantic retrieval (ollama)" --body "$(cat <<'EOF'
## Summary

Tusk v1 — Plan 5: semantic retrieval over the v1 node index.

**Stacked on:** v1. Merge to v1, then v1 merges to main when ready.

## What lands

- New \`internal/embed/\` package: \`Embedder\` interface, \`ChunkingStrategy\` interface (with \`WholeDocument\` default), payload builder, cosine-similarity helper, Ollama HTTP embedder.
- New index tables: \`embeddings\` (vector BLOB) and \`embed_queue\` (pending nodes). \`EmbeddingRepo\` and \`EmbedQueueRepo\`.
- Manifest \`[embeddings]\` block (provider, model, endpoint, dim, api-key). Loader validates provider in {ollama} and dim > 0.
- \`NodeService.Create\` enqueues embed jobs when an EmbedQueue is configured.
- \`reindex.Run\` drains the embed queue at end of pass, calling the configured Embedder per node.
- \`tusk query --semantic <text>\` runs the structural filter as a candidate set, embeds the query, ranks candidates by cosine similarity, returns top results.
- \`tusk reindex\` wires the configured Ollama embedder when the manifest declares one.

## Out of scope

- OpenAI / Voyage / Anthropic providers → Plan 5.x.
- Bundled local ONNX → v1.x.
- Continuous live drainer → Plan 6 (MCP server).
- Vector-watcher (re-embed on content drift) → Plan 8.
- \`sqlite-vec\` integration → not planned.

## Testing notes

E2E uses an httptest-backed mock Ollama. Production users need a running Ollama at the configured endpoint.

## Spec

[\`docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md\`](docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md) §10.

## Plan

[\`docs/superpowers/plans/2026-05-06-tusk-v1-5-semantic-retrieval.md\`](docs/superpowers/plans/2026-05-06-tusk-v1-5-semantic-retrieval.md)
EOF
)"
```

- [ ] **Step 7: Verify**

```bash
gh pr view --json url,state,isDraft,baseRefName,headRefName | jq
```

Expected: state OPEN, isDraft true, base `v1`, head `feat/plan-5`.

---

## Self-Review Checklist

**Spec coverage:**
- [ ] §10.4 embedding payload — Task 6.
- [ ] §10.5 provider config — Task 1+2.
- [ ] §10.6 async pipeline — Tasks 5, 9, 10.
- [ ] §10.7 chunking — Task 6.
- [ ] §10.8 result shape — Task 12.

**Out-of-scope guardrails:**
- [ ] No OpenAI/Voyage/Anthropic (Plan 5.x).
- [ ] No live drainer (Plan 6 MCP).
- [ ] No vector watcher (Plan 8).
- [ ] No sqlite-vec dependency.

**Plan-shape:**
- [ ] No "TBD" placeholders.
- [ ] Every step has either complete code or an exact command.
- [ ] Test code uses `test *testing.T`.
- [ ] Implementation code follows blank-line-around-err-guard rule.

**Type/name consistency:**
- [ ] `EmbeddingRow` shape consistent across `embedding_repo.go` and consumers.
- [ ] `Embedder` interface shape (`Embed`/`Model`/`Dim`) consistent across stub, Ollama impl, reindex consumer.
- [ ] `SemanticCandidate`/`ScoredResult` shapes consistent in `filter/semantic.go` and `cmd_query.go`.
