# Snippet generation and doctor chunking diagnostics — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Surface a `snippet` field in semantic query results (CLI tabwriter, CLI `--json`, MCP) and add per-workspace chunking diagnostics to `tusk doctor`.

**Architecture:** Store each chunk's body text alongside its vector in the `embeddings` table. With body persisted, semantic ranking returns the best-chunk body and renderers trim a snippet; doctor aggregates chunk stats and flags near-MaxBytes chunks and indexed-but-unembedded nodes. Schema change is non-backward-compatible — existing index DBs must be deleted and rebuilt with `tusk reindex`.

**Tech Stack:** Go, SQLite (modernc.org/sqlite), text/tabwriter, encoding/json, cobra, mcpgo.

**Spec:** `docs/superpowers/specs/2026-05-14-snippet-and-doctor-design.md`

---

## File Structure

| Path | Action | Responsibility |
|---|---|---|
| `internal/embed/chunking.go` | Modify | Promote `defaultMaxBytes` → exported `DefaultMaxBytes`. |
| `internal/index/index.go` | Modify | Add `body TEXT NOT NULL DEFAULT ''` to `embeddings` DDL. |
| `internal/index/embedding_repo.go` | Modify | Add `Body` field to `EmbeddingRow`; new `Stats`, `ListNodeIDs`. |
| `internal/index/embedding_repo_test.go` | Modify | Tests for new fields/methods. |
| `internal/index/embed_queue_repo.go` | Modify | New `ListNodeIDs`. |
| `internal/index/embed_queue_repo_test.go` | Create-or-modify | Test for `ListNodeIDs`. |
| `internal/embed/drain.go` | Modify | Pass `Body: string(bodyChunk)` on Upsert. |
| `internal/embed/drain_test.go` | Modify | Assert Body is stored. |
| `internal/filter/semantic.go` | Modify | `Body` on candidate; `BestChunkIdx`/`BestChunkBody` on result; `RenderSnippet` helper. |
| `internal/filter/semantic_test.go` | Modify | Tests for ranking returning best chunk + snippet rendering. |
| `cmd/tusk/cmd_query.go` | Modify | Render `SNIPPET` column; emit `--json` for semantic. |
| `cmd/tusk/cmd_query_semantic_test.go` | Modify | Assert SNIPPET column + JSON shape. |
| `internal/mcp/tools.go` | Modify | Add `snippet` to semantic result map. |
| `cmd/tusk/e2e_mcp_test.go` or new MCP test | Modify | Assert `snippet` present in MCP `tusk_query`. |
| `internal/doctor/doctor.go` | Modify | New `EmbedStatsReport`; issue kinds `embed-large-chunk`, `embed-no-chunks`. |
| `internal/doctor/doctor_test.go` | Modify | Tests for stats + issues. |
| `cmd/tusk/cmd_doctor.go` | Modify | Wire `EmbeddingRepo` + `Manifest`; print stats block. |
| `cmd/tusk/cmd_doctor_test.go` | Modify | Assert stats block output. |
| `cmd/tusk/e2e_semantic_test.go` | Modify | E2E snippet content assertion. |

---

## Task 1: Export DefaultMaxBytes

**Files:**
- Modify: `internal/embed/chunking.go`

- [ ] **Step 1: Rename `defaultMaxBytes` to `DefaultMaxBytes`**

In `internal/embed/chunking.go`, change:

```go
const (
	defaultTargetBytes  = 1600
	defaultMaxBytes     = 4000
	defaultOverlapBytes = 200
)
```

to:

```go
const (
	defaultTargetBytes = 1600
	// DefaultMaxBytes is the chunker's hard upper bound for a single chunk's
	// byte length. Doctor diagnostics use this to flag chunks that approach
	// the cap.
	DefaultMaxBytes     = 4000
	defaultOverlapBytes = 200
)
```

Update the only existing reference inside the same file (`maxSize()`):

```go
func (strategy MarkdownRecursive) maxSize() int {
	if strategy.MaxBytes > 0 {
		return strategy.MaxBytes
	}

	return DefaultMaxBytes
}
```

- [ ] **Step 2: Run tests to confirm nothing broke**

Run: `go test ./internal/embed/...`
Expected: PASS (no behavior change).

- [ ] **Step 3: Commit**

```bash
git add internal/embed/chunking.go
git commit -m "refactor(embed): export DefaultMaxBytes for doctor diagnostics"
```

---

## Task 2: Add body column to embeddings schema and EmbeddingRow.Body field

**Files:**
- Modify: `internal/index/index.go`
- Modify: `internal/index/embedding_repo.go`
- Modify: `internal/index/embedding_repo_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/index/embedding_repo_test.go`:

```go
func TestEmbeddingRepo_BodyRoundTrip(test *testing.T) {
	repo := newTestEmbeddingRepo(test)

	row := index.EmbeddingRow{
		NodeID:      "tickets/foo",
		ChunkIdx:    0,
		Model:       "nomic-embed-text",
		ContentHash: "h1",
		Vector:      []float32{0.1},
		Dim:         1,
		Body:        "# Heading\n\nFirst paragraph of the chunk.",
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

	if loaded[0].Body != row.Body {
		test.Errorf("Body = %q, want %q", loaded[0].Body, row.Body)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/index/ -run TestEmbeddingRepo_BodyRoundTrip -v`
Expected: FAIL — `EmbeddingRow has no field Body` compile error.

- [ ] **Step 3: Add body column to schema**

In `internal/index/index.go`, update the `embeddings` CREATE TABLE in the `schema` constant:

```sql
CREATE TABLE IF NOT EXISTS embeddings (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	node_id      TEXT NOT NULL,
	chunk_idx    INTEGER NOT NULL DEFAULT 0,
	model        TEXT NOT NULL,
	content_hash TEXT NOT NULL,
	vector       BLOB NOT NULL,
	dim          INTEGER NOT NULL,
	body         TEXT NOT NULL DEFAULT '',
	UNIQUE(node_id, chunk_idx)
);
```

- [ ] **Step 4: Add Body field and update Upsert / scan**

In `internal/index/embedding_repo.go`:

```go
type EmbeddingRow struct {
	NodeID      string
	ChunkIdx    int
	Model       string
	ContentHash string
	Vector      []float32
	Dim         int
	Body        string
}
```

Update `Upsert`:

```go
func (repo *EmbeddingRepo) Upsert(row EmbeddingRow) error {
	encoded, encodeErr := encodeVector(row.Vector)

	if encodeErr != nil {
		return encodeErr
	}

	_, execErr := repo.db.Exec(`
		INSERT INTO embeddings (node_id, chunk_idx, model, content_hash, vector, dim, body)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id, chunk_idx) DO UPDATE SET
			model        = excluded.model,
			content_hash = excluded.content_hash,
			vector       = excluded.vector,
			dim          = excluded.dim,
			body         = excluded.body
	`, row.NodeID, row.ChunkIdx, row.Model, row.ContentHash, encoded, row.Dim, row.Body)

	if execErr != nil {
		return fmt.Errorf("embeddingRepo: upsert %s: %w", row.NodeID, execErr)
	}

	return nil
}
```

Update `GetByNodeID`:

```go
func (repo *EmbeddingRepo) GetByNodeID(nodeID string) ([]EmbeddingRow, error) {
	rows, queryErr := repo.db.Query(`
		SELECT node_id, chunk_idx, model, content_hash, vector, dim, body
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
```

Update `ListByNodeIDs` query string:

```go
query := fmt.Sprintf(`SELECT node_id, chunk_idx, model, content_hash, vector, dim, body FROM embeddings WHERE node_id IN (%s) ORDER BY node_id, chunk_idx`, string(placeholders))
```

Update `scanEmbeddings`:

```go
func scanEmbeddings(rows *sql.Rows) ([]EmbeddingRow, error) {
	var results []EmbeddingRow

	for rows.Next() {
		var (
			row     EmbeddingRow
			encoded []byte
		)

		if scanErr := rows.Scan(&row.NodeID, &row.ChunkIdx, &row.Model, &row.ContentHash, &encoded, &row.Dim, &row.Body); scanErr != nil {
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
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/index/ -run TestEmbeddingRepo_BodyRoundTrip -v`
Expected: PASS.

Run: `go test ./internal/index/...`
Expected: PASS (existing tests must still pass — they set Body="" implicitly via the zero value).

- [ ] **Step 6: Commit**

```bash
git add internal/index/index.go internal/index/embedding_repo.go internal/index/embedding_repo_test.go
git commit -m "feat(index): store chunk body alongside vector in embeddings"
```

---

## Task 3: EmbeddingRepo.ListNodeIDs

**Files:**
- Modify: `internal/index/embedding_repo.go`
- Modify: `internal/index/embedding_repo_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/index/embedding_repo_test.go`:

```go
func TestEmbeddingRepo_ListNodeIDs(test *testing.T) {
	repo := newTestEmbeddingRepo(test)

	rows := []index.EmbeddingRow{
		{NodeID: "b/two", ChunkIdx: 0, Model: "m", ContentHash: "h", Vector: []float32{0.1}, Dim: 1},
		{NodeID: "a/one", ChunkIdx: 0, Model: "m", ContentHash: "h", Vector: []float32{0.1}, Dim: 1},
		{NodeID: "a/one", ChunkIdx: 1, Model: "m", ContentHash: "h", Vector: []float32{0.1}, Dim: 1},
		{NodeID: "c/three", ChunkIdx: 0, Model: "m", ContentHash: "h", Vector: []float32{0.1}, Dim: 1},
	}

	for _, row := range rows {
		if upsertErr := repo.Upsert(row); upsertErr != nil {
			test.Fatalf("Upsert %s/%d: %v", row.NodeID, row.ChunkIdx, upsertErr)
		}
	}

	ids, listErr := repo.ListNodeIDs()

	if listErr != nil {
		test.Fatalf("ListNodeIDs: %v", listErr)
	}

	want := []string{"a/one", "b/two", "c/three"}

	if !reflect.DeepEqual(ids, want) {
		test.Errorf("ListNodeIDs = %v, want %v", ids, want)
	}
}

func TestEmbeddingRepo_ListNodeIDs_Empty(test *testing.T) {
	repo := newTestEmbeddingRepo(test)

	ids, listErr := repo.ListNodeIDs()

	if listErr != nil {
		test.Fatalf("ListNodeIDs: %v", listErr)
	}

	if len(ids) != 0 {
		test.Errorf("ListNodeIDs = %v, want empty", ids)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/index/ -run TestEmbeddingRepo_ListNodeIDs -v`
Expected: FAIL — `repo.ListNodeIDs undefined`.

- [ ] **Step 3: Implement ListNodeIDs**

Add to `internal/index/embedding_repo.go`:

```go
// ListNodeIDs returns every distinct node_id present in the embeddings
// table, sorted ascending.
func (repo *EmbeddingRepo) ListNodeIDs() ([]string, error) {
	rows, queryErr := repo.db.Query(`SELECT DISTINCT node_id FROM embeddings ORDER BY node_id`)

	if queryErr != nil {
		return nil, fmt.Errorf("embeddingRepo: list node ids: %w", queryErr)
	}

	defer rows.Close()

	var ids []string

	for rows.Next() {
		var id string

		if scanErr := rows.Scan(&id); scanErr != nil {
			return nil, fmt.Errorf("embeddingRepo: list node ids scan: %w", scanErr)
		}

		ids = append(ids, id)
	}

	return ids, rows.Err()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/index/ -run TestEmbeddingRepo_ListNodeIDs -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/index/embedding_repo.go internal/index/embedding_repo_test.go
git commit -m "feat(index): add EmbeddingRepo.ListNodeIDs for doctor diagnostics"
```

---

## Task 4: EmbedQueueRepo.ListNodeIDs

**Files:**
- Modify: `internal/index/embed_queue_repo.go`
- Modify: `internal/index/embed_queue_repo_test.go` (create if absent — check first)

- [ ] **Step 1: Check existing test file**

Run: `ls internal/index/embed_queue_repo_test.go 2>/dev/null || echo MISSING`

- [ ] **Step 2: Write the failing test**

If the file exists, append the test. Otherwise create it with this content:

```go
package index_test

import (
	"reflect"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

func TestEmbedQueueRepo_ListNodeIDs(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewEmbedQueueRepo(store)

	for _, nodeID := range []string{"b/two", "a/one", "c/three"} {
		if enqErr := repo.Enqueue(nodeID); enqErr != nil {
			test.Fatalf("Enqueue %s: %v", nodeID, enqErr)
		}
	}

	ids, listErr := repo.ListNodeIDs()

	if listErr != nil {
		test.Fatalf("ListNodeIDs: %v", listErr)
	}

	want := []string{"a/one", "b/two", "c/three"}

	if !reflect.DeepEqual(ids, want) {
		test.Errorf("ListNodeIDs = %v, want %v", ids, want)
	}
}

func TestEmbedQueueRepo_ListNodeIDs_Empty(test *testing.T) {
	store := openTestIndex(test)
	repo := index.NewEmbedQueueRepo(store)

	ids, listErr := repo.ListNodeIDs()

	if listErr != nil {
		test.Fatalf("ListNodeIDs: %v", listErr)
	}

	if len(ids) != 0 {
		test.Errorf("ListNodeIDs = %v, want empty", ids)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/index/ -run TestEmbedQueueRepo_ListNodeIDs -v`
Expected: FAIL — `repo.ListNodeIDs undefined`.

- [ ] **Step 4: Implement ListNodeIDs**

Add to `internal/index/embed_queue_repo.go`:

```go
// ListNodeIDs returns every pending node_id, sorted ascending.
func (repo *EmbedQueueRepo) ListNodeIDs() ([]string, error) {
	rows, queryErr := repo.db.Query(`SELECT node_id FROM embed_queue ORDER BY node_id`)

	if queryErr != nil {
		return nil, fmt.Errorf("embedQueueRepo: list node ids: %w", queryErr)
	}

	defer rows.Close()

	var ids []string

	for rows.Next() {
		var id string

		if scanErr := rows.Scan(&id); scanErr != nil {
			return nil, fmt.Errorf("embedQueueRepo: list node ids scan: %w", scanErr)
		}

		ids = append(ids, id)
	}

	return ids, rows.Err()
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/index/ -run TestEmbedQueueRepo_ListNodeIDs -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/index/embed_queue_repo.go internal/index/embed_queue_repo_test.go
git commit -m "feat(index): add EmbedQueueRepo.ListNodeIDs for pending exclusion"
```

---

## Task 5: EmbeddingRepo.Stats

**Files:**
- Modify: `internal/index/embedding_repo.go`
- Modify: `internal/index/embedding_repo_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/index/embedding_repo_test.go`:

```go
func TestEmbeddingRepo_Stats_Empty(test *testing.T) {
	repo := newTestEmbeddingRepo(test)

	stats, statsErr := repo.Stats(3600)

	if statsErr != nil {
		test.Fatalf("Stats: %v", statsErr)
	}

	if stats.TotalNodes != 0 || stats.TotalChunks != 0 || stats.MaxChunks != 0 {
		test.Errorf("Stats empty case: %+v", stats)
	}

	if stats.MeanChunks != 0 {
		test.Errorf("MeanChunks empty = %v, want 0", stats.MeanChunks)
	}

	if len(stats.TopByChunks) != 0 || len(stats.LargeChunks) != 0 {
		test.Errorf("TopByChunks/LargeChunks empty case: %+v", stats)
	}
}

func TestEmbeddingRepo_Stats_Aggregates(test *testing.T) {
	repo := newTestEmbeddingRepo(test)

	insert := func(nodeID string, count int, bodyLen int) {
		body := strings.Repeat("x", bodyLen)
		for chunkIdx := 0; chunkIdx < count; chunkIdx++ {
			row := index.EmbeddingRow{
				NodeID:      nodeID,
				ChunkIdx:    chunkIdx,
				Model:       "m",
				ContentHash: "h",
				Vector:      []float32{0.1},
				Dim:         1,
				Body:        body,
			}

			if upsertErr := repo.Upsert(row); upsertErr != nil {
				test.Fatalf("Upsert %s/%d: %v", nodeID, chunkIdx, upsertErr)
			}
		}
	}

	// node a: 1 chunk @ 500 bytes
	// node b: 3 chunks @ 100 bytes
	// node c: 5 chunks @ 3700 bytes (large)
	// node d: 2 chunks @ 100 bytes
	insert("a", 1, 500)
	insert("b", 3, 100)
	insert("c", 5, 3700)
	insert("d", 2, 100)

	stats, statsErr := repo.Stats(3600)

	if statsErr != nil {
		test.Fatalf("Stats: %v", statsErr)
	}

	if stats.TotalNodes != 4 {
		test.Errorf("TotalNodes = %d, want 4", stats.TotalNodes)
	}

	if stats.TotalChunks != 11 {
		test.Errorf("TotalChunks = %d, want 11", stats.TotalChunks)
	}

	if stats.MaxChunks != 5 {
		test.Errorf("MaxChunks = %d, want 5", stats.MaxChunks)
	}

	// per-node chunk counts: [1, 3, 5, 2] → sorted [1, 2, 3, 5] → median = (2+3)/2 = 2 (integer floor)
	if stats.MedianChunks != 2 {
		test.Errorf("MedianChunks = %d, want 2", stats.MedianChunks)
	}

	wantMean := 11.0 / 4.0
	if stats.MeanChunks != wantMean {
		test.Errorf("MeanChunks = %v, want %v", stats.MeanChunks, wantMean)
	}

	if len(stats.TopByChunks) != 4 || stats.TopByChunks[0].NodeID != "c" || stats.TopByChunks[0].Chunks != 5 {
		test.Errorf("TopByChunks = %+v", stats.TopByChunks)
	}

	// 5 large chunks expected (all of c, body=3700 >= 3600)
	if len(stats.LargeChunks) != 5 {
		test.Errorf("LargeChunks count = %d, want 5", len(stats.LargeChunks))
	}

	for _, large := range stats.LargeChunks {
		if large.NodeID != "c" || large.BodyLen != 3700 {
			test.Errorf("LargeChunks entry %+v", large)
		}
	}
}
```

Make sure `strings` is imported in the test file (add `"strings"` to the imports).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/index/ -run TestEmbeddingRepo_Stats -v`
Expected: FAIL — `repo.Stats undefined`, type `EmbeddingStats` undefined.

- [ ] **Step 3: Implement Stats and supporting types**

Add to `internal/index/embedding_repo.go`:

```go
// EmbeddingStats aggregates the embeddings table for tusk doctor.
type EmbeddingStats struct {
	TotalNodes   int
	TotalChunks  int
	MeanChunks   float64
	MedianChunks int
	MaxChunks    int
	TopByChunks  []NodeChunkCount
	LargeChunks  []NodeChunkInfo
}

// NodeChunkCount pairs a node id with its chunk count.
type NodeChunkCount struct {
	NodeID string
	Chunks int
}

// NodeChunkInfo identifies one chunk and its body length.
type NodeChunkInfo struct {
	NodeID   string
	ChunkIdx int
	BodyLen  int
}

// Stats returns aggregate statistics over the embeddings table.
// largeChunkThreshold is the inclusive byte length at or above which a chunk
// is reported in LargeChunks.
func (repo *EmbeddingRepo) Stats(largeChunkThreshold int) (EmbeddingStats, error) {
	var stats EmbeddingStats

	// Per-node counts.
	rows, queryErr := repo.db.Query(`
		SELECT node_id, COUNT(*) AS chunk_count
		FROM embeddings
		GROUP BY node_id
		ORDER BY chunk_count DESC, node_id ASC
	`)

	if queryErr != nil {
		return stats, fmt.Errorf("embeddingRepo: stats counts: %w", queryErr)
	}

	defer rows.Close()

	var perNode []NodeChunkCount

	for rows.Next() {
		var entry NodeChunkCount

		if scanErr := rows.Scan(&entry.NodeID, &entry.Chunks); scanErr != nil {
			return stats, fmt.Errorf("embeddingRepo: stats scan: %w", scanErr)
		}

		perNode = append(perNode, entry)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return stats, rowsErr
	}

	stats.TotalNodes = len(perNode)

	for _, entry := range perNode {
		stats.TotalChunks += entry.Chunks

		if entry.Chunks > stats.MaxChunks {
			stats.MaxChunks = entry.Chunks
		}
	}

	if stats.TotalNodes > 0 {
		stats.MeanChunks = float64(stats.TotalChunks) / float64(stats.TotalNodes)
	}

	stats.MedianChunks = medianChunkCount(perNode)

	topN := 5

	if len(perNode) < topN {
		topN = len(perNode)
	}

	stats.TopByChunks = append(stats.TopByChunks, perNode[:topN]...)

	// Large chunks (length(body) >= threshold).
	largeRows, largeErr := repo.db.Query(`
		SELECT node_id, chunk_idx, length(body) AS body_len
		FROM embeddings
		WHERE length(body) >= ?
		ORDER BY node_id, chunk_idx
	`, largeChunkThreshold)

	if largeErr != nil {
		return stats, fmt.Errorf("embeddingRepo: stats large: %w", largeErr)
	}

	defer largeRows.Close()

	for largeRows.Next() {
		var info NodeChunkInfo

		if scanErr := largeRows.Scan(&info.NodeID, &info.ChunkIdx, &info.BodyLen); scanErr != nil {
			return stats, fmt.Errorf("embeddingRepo: stats large scan: %w", scanErr)
		}

		stats.LargeChunks = append(stats.LargeChunks, info)
	}

	return stats, largeRows.Err()
}

// medianChunkCount returns the integer median chunk count from a slice already
// sorted by chunk count DESC, node_id ASC. An empty input returns 0.
func medianChunkCount(perNode []NodeChunkCount) int {
	if len(perNode) == 0 {
		return 0
	}

	counts := make([]int, len(perNode))

	for idx, entry := range perNode {
		counts[idx] = entry.Chunks
	}

	sort.Ints(counts)

	mid := len(counts) / 2

	if len(counts)%2 == 1 {
		return counts[mid]
	}

	return (counts[mid-1] + counts[mid]) / 2
}
```

Add `"sort"` to the imports in `internal/index/embedding_repo.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/index/ -run TestEmbeddingRepo_Stats -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/index/embedding_repo.go internal/index/embedding_repo_test.go
git commit -m "feat(index): aggregate Stats over embeddings for doctor"
```

---

## Task 6: drain.go writes chunk body

**Files:**
- Modify: `internal/embed/drain.go`
- Modify: `internal/embed/drain_test.go`

- [ ] **Step 1: Inspect existing drain test to find a good insertion point**

Run: `grep -n "func Test" internal/embed/drain_test.go | head -10`

Pick a test that already drives the success path and adapt or add a new test asserting `Body` after Drain.

- [ ] **Step 2: Add a failing test**

Add to `internal/embed/drain_test.go`:

```go
func TestDrainQueue_StoresChunkBody(test *testing.T) {
	dir := test.TempDir()
	indexPath := filepath.Join(dir, "index.db")

	nodePath := filepath.Join(dir, "notes", "snippet-target.md")

	if mkErr := os.MkdirAll(filepath.Dir(nodePath), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	bodyText := "# Heading\n\nThis is the body of the chunk we want to verify."
	fileContent := "---\ntype: note\ntitle: Snippet Target\n---\n\n" + bodyText

	if writeErr := os.WriteFile(nodePath, []byte(fileContent), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	store, openErr := index.Open(indexPath)

	if openErr != nil {
		test.Fatalf("index open: %v", openErr)
	}

	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)

	if upsertErr := nodeRepo.Upsert(index.NodeRow{
		ID:    "notes/snippet-target",
		Type:  "note",
		Title: "Snippet Target",
		Path:  "notes/snippet-target.md",
	}); upsertErr != nil {
		test.Fatalf("node upsert: %v", upsertErr)
	}

	queue := index.NewEmbedQueueRepo(store)

	if enqErr := queue.Enqueue("notes/snippet-target"); enqErr != nil {
		test.Fatalf("enqueue: %v", enqErr)
	}

	embeddingsRepo := index.NewEmbeddingRepo(store)

	drained, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       dir,
		Nodes:      nodeRepo,
		Queue:      queue,
		Embeddings: embeddingsRepo,
		Embedder:   &stubEmbedder{model: "test", dim: 1, vector: []float32{0.5}},
		Chunker:    embed.MarkdownRecursive{},
	})

	if drainErr != nil {
		test.Fatalf("drain: %v", drainErr)
	}

	if drained != 1 {
		test.Fatalf("drained = %d, want 1", drained)
	}

	rows, getErr := embeddingsRepo.GetByNodeID("notes/snippet-target")

	if getErr != nil {
		test.Fatalf("get: %v", getErr)
	}

	if len(rows) == 0 {
		test.Fatal("no embedding rows persisted")
	}

	if !strings.Contains(rows[0].Body, "body of the chunk") {
		test.Errorf("first chunk body = %q, want substring 'body of the chunk'", rows[0].Body)
	}
}
```

Confirm imports include `"context"`, `"os"`, `"path/filepath"`, `"strings"`, the `index` package, and the existing `stubEmbedder` (or equivalent fake). If the file already defines a stub embedder with a different name, adapt the call.

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/embed/ -run TestDrainQueue_StoresChunkBody -v`
Expected: FAIL — `Body` is empty.

- [ ] **Step 4: Pass Body in Upsert**

In `internal/embed/drain.go`, modify the chunk-write block (around lines 211-219) to include the body slice:

```go
if upsertErr := config.Embeddings.Upsert(index.EmbeddingRow{
	NodeID:      queued.NodeID,
	ChunkIdx:    chunkIdx,
	Model:       config.Embedder.Model(),
	ContentHash: hex.EncodeToString(contentHash[:]),
	Vector:      vector,
	Dim:         config.Embedder.Dim(),
	Body:        string(bodyChunk),
}); upsertErr != nil {
	return drained, upsertErr
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/embed/ -run TestDrainQueue_StoresChunkBody -v`
Expected: PASS.

Run: `go test ./internal/embed/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/embed/drain.go internal/embed/drain_test.go
git commit -m "feat(embed): persist chunk body during drain for snippets"
```

---

## Task 7: SemanticCandidate + ScoredResult carry best chunk body

**Files:**
- Modify: `internal/filter/semantic.go`
- Modify: `internal/filter/semantic_test.go`

- [ ] **Step 1: Write the failing test**

Replace or add to `internal/filter/semantic_test.go`:

```go
func TestSemanticRank_TracksBestChunkBody(test *testing.T) {
	query := []float32{1, 0}

	candidates := []filter.SemanticCandidate{
		{NodeID: "n1", ChunkIdx: 0, Vector: []float32{0.1, 1}, Body: "low score body"},
		{NodeID: "n1", ChunkIdx: 1, Vector: []float32{1, 0}, Body: "high score body"},
		{NodeID: "n2", ChunkIdx: 0, Vector: []float32{0, 1}, Body: "n2 only chunk"},
	}

	ranked := filter.SemanticRank(candidates, query)

	if len(ranked) != 2 {
		test.Fatalf("len = %d, want 2", len(ranked))
	}

	first := ranked[0]

	if first.NodeID != "n1" {
		test.Errorf("first.NodeID = %q, want n1", first.NodeID)
	}

	if first.BestChunkIdx != 1 {
		test.Errorf("BestChunkIdx = %d, want 1", first.BestChunkIdx)
	}

	if first.BestChunkBody != "high score body" {
		test.Errorf("BestChunkBody = %q", first.BestChunkBody)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/filter/ -run TestSemanticRank_TracksBestChunkBody -v`
Expected: FAIL — `Body` field undefined, `BestChunkIdx`/`BestChunkBody` undefined.

- [ ] **Step 3: Update types and SemanticRank**

Edit `internal/filter/semantic.go`:

```go
package filter

import (
	"sort"

	"github.com/germanamz/tusk/internal/embed"
)

// SemanticCandidate pairs a node's chunk vector with its ids for ranking.
// Body carries the chunk's body text (no header prefix) so renderers can
// produce a snippet of the highest-scoring chunk per node.
type SemanticCandidate struct {
	NodeID   string
	ChunkIdx int
	Vector   []float32
	Body     string
}

// ScoredResult is one ranked node. Score is the max cosine similarity across
// the node's chunks. BestChunkIdx and BestChunkBody identify and carry the
// body of the chunk that produced Score.
type ScoredResult struct {
	NodeID        string
	Score         float64
	BestChunkIdx  int
	BestChunkBody string
}

// SemanticRank scores each candidate by cosine similarity to queryVector and
// returns one row per node, with Score equal to the maximum chunk score for
// that node. BestChunkIdx and BestChunkBody come from the chunk that produced
// the max. Results are sorted by score descending, ties broken by NodeID
// ascending. Candidates whose vectors mismatch queryVector's dimension are
// silently skipped.
func SemanticRank(candidates []SemanticCandidate, queryVector []float32) []ScoredResult {
	type bestEntry struct {
		score    float64
		chunkIdx int
		body     string
	}

	bestByNode := make(map[string]bestEntry, len(candidates))

	for _, candidate := range candidates {
		if len(candidate.Vector) != len(queryVector) {
			continue
		}

		score := embed.CosineSimilarity(candidate.Vector, queryVector)

		prev, present := bestByNode[candidate.NodeID]

		if !present || score > prev.score {
			bestByNode[candidate.NodeID] = bestEntry{
				score:    score,
				chunkIdx: candidate.ChunkIdx,
				body:     candidate.Body,
			}
		}
	}

	scored := make([]ScoredResult, 0, len(bestByNode))

	for nodeID, entry := range bestByNode {
		scored = append(scored, ScoredResult{
			NodeID:        nodeID,
			Score:         entry.score,
			BestChunkIdx:  entry.chunkIdx,
			BestChunkBody: entry.body,
		})
	}

	sort.Slice(scored, func(left, right int) bool {
		if scored[left].Score == scored[right].Score {
			return scored[left].NodeID < scored[right].NodeID
		}

		return scored[left].Score > scored[right].Score
	})

	return scored
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/filter/ -run TestSemanticRank_TracksBestChunkBody -v`
Expected: PASS.

Run: `go test ./internal/filter/...`
Expected: PASS (the existing semantic ranking test still passes because the score and node id behavior is unchanged).

- [ ] **Step 5: Commit**

```bash
git add internal/filter/semantic.go internal/filter/semantic_test.go
git commit -m "feat(filter): SemanticRank returns best-chunk idx and body"
```

---

## Task 8: filter.RenderSnippet helper

**Files:**
- Modify: `internal/filter/semantic.go`
- Modify: `internal/filter/semantic_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/filter/semantic_test.go`:

```go
func TestRenderSnippet(test *testing.T) {
	cases := []struct {
		name     string
		body     string
		maxRunes int
		want     string
	}{
		{"empty", "", 200, ""},
		{"short, no newlines", "hello world", 200, "hello world"},
		{"newlines collapsed", "hello\nworld\n\nfoo", 200, "hello world foo"},
		{"truncated with ellipsis", "abcdefghij", 5, "abcde…"},
		{"rune boundary preserved", "héllo世界", 6, "héllo世"},
		{"trailing whitespace trimmed", "abc   \n  ", 200, "abc"},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(inner *testing.T) {
			got := filter.RenderSnippet(testCase.body, testCase.maxRunes)

			if got != testCase.want {
				inner.Errorf("RenderSnippet(%q, %d) = %q, want %q", testCase.body, testCase.maxRunes, got, testCase.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/filter/ -run TestRenderSnippet -v`
Expected: FAIL — `filter.RenderSnippet undefined`.

- [ ] **Step 3: Implement RenderSnippet**

Append to `internal/filter/semantic.go`:

```go
// RenderSnippet returns the leading maxRunes runes of body with internal
// whitespace runs collapsed to single spaces, trailing whitespace stripped,
// and an ellipsis (U+2026) appended when truncation occurred. Returns the
// empty string for empty input or when only whitespace remains.
func RenderSnippet(body string, maxRunes int) string {
	if maxRunes <= 0 || len(body) == 0 {
		return ""
	}

	var (
		builder       []rune
		previousSpace bool
		truncated     bool
	)

	for _, r := range body {
		if r == '\n' || r == '\r' || r == '\t' || r == ' ' {
			if len(builder) == 0 {
				// Skip leading whitespace.
				continue
			}

			if previousSpace {
				continue
			}

			if len(builder) >= maxRunes {
				truncated = true
				break
			}

			builder = append(builder, ' ')
			previousSpace = true

			continue
		}

		if len(builder) >= maxRunes {
			truncated = true
			break
		}

		builder = append(builder, r)
		previousSpace = false
	}

	// Strip trailing space (only one possible because we collapsed).
	for len(builder) > 0 && builder[len(builder)-1] == ' ' {
		builder = builder[:len(builder)-1]
	}

	if len(builder) == 0 {
		return ""
	}

	result := string(builder)

	if truncated {
		result += "…"
	}

	return result
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/filter/ -run TestRenderSnippet -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/filter/semantic.go internal/filter/semantic_test.go
git commit -m "feat(filter): add RenderSnippet helper for semantic snippets"
```

---

## Task 9: CLI tabwriter SNIPPET column for --semantic

**Files:**
- Modify: `cmd/tusk/cmd_query.go`
- Modify: `cmd/tusk/cmd_query_semantic_test.go`

- [ ] **Step 1: Add a failing test**

The existing pattern in `cmd_query_semantic_test.go` is: `initWorkspace(test)` to scaffold a temp workspace, `newRootCmd()` to build the cobra root, `SetOut(buf)` + `SetArgs(...)` + `Execute()`. The simplest extension is to inline a stub Ollama server (same pattern as `TestE2E_SemanticRetrieval`) so the test owns its end-to-end fixture without a shared helper.

Append:

```go
func TestQueryCmd_SemanticRendersSnippetColumn(test *testing.T) {
	tmpDir := initWorkspace(test)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			Prompt string `json:"prompt"`
		}

		_ = json.NewDecoder(request.Body).Decode(&payload)

		// Deterministic stub: all rows score 1.0 against any query.
		_ = json.NewEncoder(writer).Encode(map[string]any{"embedding": []float64{1, 0, 0}})
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

	mustCreateNode := func(args ...string) {
		cmd := newRootCmd()
		cmd.SetArgs(args)

		if execErr := cmd.Execute(); execErr != nil {
			test.Fatalf("setup %v: %v", args, execErr)
		}
	}

	if mkErr := os.MkdirAll(filepath.Join(tmpDir, "notes"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	if writeErr := os.WriteFile(filepath.Join(tmpDir, "notes/snippet.md"), []byte(`---
type: note
title: Snippet
---

This is the body of the chunk we want surfaced as a snippet.
`), 0o644); writeErr != nil {
		test.Fatalf("write node: %v", writeErr)
	}

	mustCreateNode("reindex")

	out := &bytes.Buffer{}

	queryCmd := newRootCmd()
	queryCmd.SetOut(out)
	queryCmd.SetErr(out)
	queryCmd.SetArgs([]string{"query", "type=note", "--semantic", "authentication"})

	if execErr := queryCmd.Execute(); execErr != nil {
		test.Fatalf("query: %v\nout:\n%s", execErr, out.String())
	}

	if !strings.Contains(out.String(), "SNIPPET") {
		test.Errorf("output missing SNIPPET header:\n%s", out.String())
	}

	if !strings.Contains(out.String(), "body of the chunk") {
		test.Errorf("output missing snippet content:\n%s", out.String())
	}
}
```

Add imports as needed: `"bytes"`, `"encoding/json"`, `"net/http"`, `"net/http/httptest"`, `"os"`, `"path/filepath"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tusk/ -run TestRunSemanticQuery_RendersSnippetColumn -v`
Expected: FAIL — SNIPPET header missing.

- [ ] **Step 3: Update runSemanticQuery to populate candidate Body and render SNIPPET column**

In `cmd/tusk/cmd_query.go`, modify the candidate-build loop:

```go
candidates := make([]filter.SemanticCandidate, 0, len(loadedRows))

for _, embeddingRow := range loadedRows {
	candidates = append(candidates, filter.SemanticCandidate{
		NodeID:   embeddingRow.NodeID,
		ChunkIdx: embeddingRow.ChunkIdx,
		Vector:   embeddingRow.Vector,
		Body:     embeddingRow.Body,
	})
}
```

And the tabwriter rendering at the end of `runSemanticQuery`:

```go
tab := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

_, _ = fmt.Fprintln(tab, "ID\tSCORE\tSNIPPET")

for _, scored := range ranked {
	_, _ = fmt.Fprintf(tab, "%s\t%.4f\t%s\n",
		scored.NodeID,
		scored.Score,
		filter.RenderSnippet(scored.BestChunkBody, 200),
	)
}

return tab.Flush()
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/tusk/ -run TestRunSemanticQuery_RendersSnippetColumn -v`
Expected: PASS.

Run: `go test ./cmd/tusk/...`
Expected: PASS. If a pre-existing semantic test asserted the old "ID SCORE" header verbatim, update its expected substring to include "SNIPPET".

- [ ] **Step 5: Commit**

```bash
git add cmd/tusk/cmd_query.go cmd/tusk/cmd_query_semantic_test.go
git commit -m "feat(query): render SNIPPET column in --semantic tabwriter"
```

---

## Task 10: CLI --json output for --semantic

**Files:**
- Modify: `cmd/tusk/cmd_query.go`
- Modify: `cmd/tusk/cmd_query_semantic_test.go`

- [ ] **Step 1: Write the failing test**

Append to `cmd/tusk/cmd_query_semantic_test.go`. Re-uses the same stub server / workspace bootstrap as Task 9's test — extract them into a helper if you want, but a copy is fine:

```go
func TestQueryCmd_SemanticJSONIncludesSnippet(test *testing.T) {
	tmpDir := initWorkspace(test)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string]any{"embedding": []float64{1, 0, 0}})
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

	if mkErr := os.MkdirAll(filepath.Join(tmpDir, "notes"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	if writeErr := os.WriteFile(filepath.Join(tmpDir, "notes/snippet.md"), []byte(`---
type: note
title: Snippet
---

JSON snippet body content.
`), 0o644); writeErr != nil {
		test.Fatalf("write node: %v", writeErr)
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
	queryCmd.SetArgs([]string{"query", "type=note", "--semantic", "anything", "--json"})

	if execErr := queryCmd.Execute(); execErr != nil {
		test.Fatalf("query: %v\nout:\n%s", execErr, out.String())
	}

	var results []map[string]any

	if jsonErr := json.Unmarshal(out.Bytes(), &results); jsonErr != nil {
		test.Fatalf("unmarshal: %v\nout:\n%s", jsonErr, out.String())
	}

	if len(results) == 0 {
		test.Fatalf("empty result list:\n%s", out.String())
	}

	snippet, ok := results[0]["snippet"].(string)

	if !ok || snippet == "" {
		test.Errorf("result[0] missing snippet: %v", results[0])
	}

	if !strings.Contains(snippet, "JSON snippet body") {
		test.Errorf("snippet content unexpected: %q", snippet)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tusk/ -run TestRunSemanticQuery_JSONOutputIncludesSnippet -v`
Expected: FAIL — output is tabwriter, JSON unmarshal fails.

- [ ] **Step 3: Plumb emitJSON into runSemanticQuery**

In `cmd/tusk/cmd_query.go`, change the call site:

```go
if semanticQuery != "" {
	return runSemanticQuery(cmd, ws, loaded, sqlQuery, params, take, skip, semanticQuery, emitJSON)
}
```

Update the signature and add the JSON branch at the end of `runSemanticQuery`:

```go
func runSemanticQuery(cmd *cobra.Command, ws *workspace.Workspace, loaded *manifest.Manifest, structuralSQL string, structuralParams []any, take, skip int, semanticQuery string, emitJSON bool) error {
	// ... existing body, with the candidate Body wiring from Task 9 ...

	if emitJSON {
		out := make([]map[string]any, 0, len(ranked))

		for _, scored := range ranked {
			out = append(out, map[string]any{
				"id":      scored.NodeID,
				"score":   scored.Score,
				"snippet": filter.RenderSnippet(scored.BestChunkBody, 200),
			})
		}

		encoder := json.NewEncoder(cmd.OutOrStdout())

		return encoder.Encode(out)
	}

	tab := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

	_, _ = fmt.Fprintln(tab, "ID\tSCORE\tSNIPPET")

	for _, scored := range ranked {
		_, _ = fmt.Fprintf(tab, "%s\t%.4f\t%s\n",
			scored.NodeID,
			scored.Score,
			filter.RenderSnippet(scored.BestChunkBody, 200),
		)
	}

	return tab.Flush()
}
```

Add `"encoding/json"` to the imports if missing.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/tusk/ -run TestRunSemanticQuery_JSONOutputIncludesSnippet -v`
Expected: PASS.

Run: `go test ./cmd/tusk/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/tusk/cmd_query.go cmd/tusk/cmd_query_semantic_test.go
git commit -m "feat(query): --json output for --semantic with snippet"
```

---

## Task 11: MCP tusk_query includes snippet

**Files:**
- Modify: `internal/mcp/tools.go`
- Modify: one of `cmd/tusk/e2e_mcp_test.go` or `internal/mcp/tools_test.go` (whichever has existing semantic coverage — check both)

- [ ] **Step 1: Find existing MCP semantic test**

Run: `grep -ln "semantic" cmd/tusk/e2e_mcp_test.go internal/mcp/*_test.go 2>/dev/null`

Pick the file with existing semantic coverage as the home for the new assertion.

- [ ] **Step 2: Write the failing test**

Add (adapting helper names to match existing patterns):

```go
func TestMCPTuskQuery_SemanticReturnsSnippet(test *testing.T) {
	// Boot the MCP server against a fixture workspace whose embeddings
	// repo has Body populated (same fixture as cmd_query_semantic_test).
	resp := callMCPSemanticQuery(test, "authentication flow")

	var payload struct {
		Results []map[string]any `json:"results"`
		Count   int              `json:"count"`
		Model   string           `json:"model"`
	}

	if jsonErr := json.Unmarshal([]byte(resp), &payload); jsonErr != nil {
		test.Fatalf("unmarshal: %v\nresp:\n%s", jsonErr, resp)
	}

	if len(payload.Results) == 0 {
		test.Fatalf("empty results")
	}

	snippet, ok := payload.Results[0]["snippet"].(string)

	if !ok {
		test.Fatalf("result[0] missing snippet: %v", payload.Results[0])
	}

	if snippet == "" {
		test.Errorf("snippet empty for top result")
	}
}
```

`callMCPSemanticQuery` follows the existing MCP fixture pattern in the file.

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./... -run TestMCPTuskQuery_SemanticReturnsSnippet -v`
Expected: FAIL — snippet key absent.

- [ ] **Step 4: Wire snippet into MCP response**

In `internal/mcp/tools.go`, modify the candidate build loop in `registerQueryTool`:

```go
candidates := make([]filter.SemanticCandidate, 0, len(loaded))

for _, embeddingRow := range loaded {
	candidates = append(candidates, filter.SemanticCandidate{
		NodeID:   embeddingRow.NodeID,
		ChunkIdx: embeddingRow.ChunkIdx,
		Vector:   embeddingRow.Vector,
		Body:     embeddingRow.Body,
	})
}
```

Modify the ranking output:

```go
for _, scored := range ranked {
	ranking = append(ranking, map[string]any{
		"id":      scored.NodeID,
		"score":   scored.Score,
		"type":    byID[scored.NodeID].Type,
		"path":    byID[scored.NodeID].Path,
		"title":   byID[scored.NodeID].Title,
		"snippet": filter.RenderSnippet(scored.BestChunkBody, 200),
	})
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./... -run TestMCPTuskQuery_SemanticReturnsSnippet -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/tools.go <test-file-path>
git commit -m "feat(mcp): include snippet in tusk_query semantic results"
```

---

## Task 12: Doctor EmbedStats + new issue kinds

**Files:**
- Modify: `internal/doctor/doctor.go`
- Modify: `internal/doctor/doctor_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/doctor/doctor_test.go`:

```go
func TestRun_EmbedStatsAndIssues(test *testing.T) {
	store := openTestIndex(test)

	nodes := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)
	embedQueue := index.NewEmbedQueueRepo(store)
	embeddings := index.NewEmbeddingRepo(store)

	// nodeA: 1 chunk @ small body
	if upsertErr := nodes.Upsert(index.NodeRow{ID: "a", Type: "note", Title: "A", Path: "a.md"}); upsertErr != nil {
		test.Fatalf("upsert a: %v", upsertErr)
	}

	if upsertErr := embeddings.Upsert(index.EmbeddingRow{
		NodeID: "a", ChunkIdx: 0, Model: "m", ContentHash: "h",
		Vector: []float32{0.1}, Dim: 1, Body: "short body",
	}); upsertErr != nil {
		test.Fatalf("upsert embed a: %v", upsertErr)
	}

	// nodeB: 1 chunk @ near-MaxBytes body
	if upsertErr := nodes.Upsert(index.NodeRow{ID: "b", Type: "note", Title: "B", Path: "b.md"}); upsertErr != nil {
		test.Fatalf("upsert b: %v", upsertErr)
	}

	if upsertErr := embeddings.Upsert(index.EmbeddingRow{
		NodeID: "b", ChunkIdx: 0, Model: "m", ContentHash: "h",
		Vector: []float32{0.1}, Dim: 1, Body: strings.Repeat("x", 3800),
	}); upsertErr != nil {
		test.Fatalf("upsert embed b: %v", upsertErr)
	}

	// nodeC: indexed, no embeddings, NOT pending → should flag.
	if upsertErr := nodes.Upsert(index.NodeRow{ID: "c", Type: "note", Title: "C", Path: "c.md"}); upsertErr != nil {
		test.Fatalf("upsert c: %v", upsertErr)
	}

	// nodeD: indexed, no embeddings, IS pending → should NOT flag.
	if upsertErr := nodes.Upsert(index.NodeRow{ID: "d", Type: "note", Title: "D", Path: "d.md"}); upsertErr != nil {
		test.Fatalf("upsert d: %v", upsertErr)
	}

	if enqErr := embedQueue.Enqueue("d"); enqErr != nil {
		test.Fatalf("enqueue d: %v", enqErr)
	}

	report, runErr := doctor.Run(doctor.Config{
		Nodes:      nodes,
		Edges:      edges,
		EmbedQueue: embedQueue,
		Embeddings: embeddings,
		Manifest: &manifest.Manifest{Embeddings: manifest.EmbeddingsSection{Provider: "ollama"}},
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.EmbedStats == nil {
		test.Fatal("EmbedStats is nil")
	}

	if report.EmbedStats.TotalNodes != 2 || report.EmbedStats.TotalChunks != 2 {
		test.Errorf("EmbedStats counts: %+v", report.EmbedStats)
	}

	var sawLarge, sawNoChunks bool

	for _, issue := range report.Issues {
		switch issue.Kind {
		case doctor.IssueEmbedLargeChunk:
			if issue.NodeID == "b" {
				sawLarge = true
			}
		case doctor.IssueEmbedNoChunks:
			if issue.NodeID == "c" {
				sawNoChunks = true
			}

			if issue.NodeID == "d" {
				test.Errorf("pending node d reported as no-chunks")
			}
		}
	}

	if !sawLarge {
		test.Errorf("missing embed-large-chunk for node b: %+v", report.Issues)
	}

	if !sawNoChunks {
		test.Errorf("missing embed-no-chunks for node c: %+v", report.Issues)
	}
}

func TestRun_EmbedStatsNilWithoutEmbeddingsConfig(test *testing.T) {
	store := openTestIndex(test)
	nodes := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)
	embedQueue := index.NewEmbedQueueRepo(store)

	report, runErr := doctor.Run(doctor.Config{
		Nodes:      nodes,
		Edges:      edges,
		EmbedQueue: embedQueue,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.EmbedStats != nil {
		test.Errorf("EmbedStats = %+v, want nil", report.EmbedStats)
	}
}
```

Check existing imports in `doctor_test.go`; add `"strings"`, `manifest`, and adjust the `openTestIndex` helper reference to match the package's existing test helper (it may already exist as a sibling helper or you may need to add one that opens a temp `*index.Index`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/doctor/ -run TestRun_EmbedStats -v`
Expected: FAIL — `IssueEmbedLargeChunk`, `IssueEmbedNoChunks`, `Config.Embeddings`, `Config.Manifest`, `Report.EmbedStats` undefined.

- [ ] **Step 3: Implement doctor changes**

In `internal/doctor/doctor.go`:

```go
package doctor

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

// ... existing issue-kind const block ...

const (
	IssueEmbedLargeChunk = "embed-large-chunk"
	IssueEmbedNoChunks   = "embed-no-chunks"
)
```

Update `Config`:

```go
type Config struct {
	Nodes         *index.NodeRepo
	Edges         *index.EdgeRepo
	EmbedQueue    *index.EmbedQueueRepo
	WorkflowDrift *index.WorkflowDriftRepo
	PropertyDrift *index.PropertyDriftRepo
	Embeddings    *index.EmbeddingRepo
	Manifest      *manifest.Manifest
}
```

Update `Report`:

```go
type Report struct {
	Issues          []Issue
	EmbedQueueDepth int
	EmbedStats      *EmbedStatsReport
}

// EmbedStatsReport summarizes chunking aggregates for tusk doctor.
type EmbedStatsReport struct {
	TotalNodes   int
	TotalChunks  int
	MeanChunks   float64
	MedianChunks int
	MaxChunks    int
	TopByChunks  []index.NodeChunkCount
}
```

At the end of `Run`, before `return report, nil`, add:

```go
	if config.Embeddings != nil && config.Manifest != nil && config.Manifest.Embeddings.Provider != "" {
		threshold := int(0.9 * float64(embed.DefaultMaxBytes))

		stats, statsErr := config.Embeddings.Stats(threshold)

		if statsErr != nil {
			return nil, statsErr
		}

		report.EmbedStats = &EmbedStatsReport{
			TotalNodes:   stats.TotalNodes,
			TotalChunks:  stats.TotalChunks,
			MeanChunks:   stats.MeanChunks,
			MedianChunks: stats.MedianChunks,
			MaxChunks:    stats.MaxChunks,
			TopByChunks:  stats.TopByChunks,
		}

		for _, info := range stats.LargeChunks {
			report.Issues = append(report.Issues, Issue{
				Kind:    IssueEmbedLargeChunk,
				NodeID:  info.NodeID,
				Message: fmt.Sprintf("chunk %d body is %d bytes (≥ %d threshold, chunker MaxBytes %d)", info.ChunkIdx, info.BodyLen, threshold, embed.DefaultMaxBytes),
			})
		}

		if config.Nodes != nil {
			noChunks, noChunksErr := findNoChunkNodes(config.Nodes, config.Embeddings, config.EmbedQueue)

			if noChunksErr != nil {
				return nil, noChunksErr
			}

			report.Issues = append(report.Issues, noChunks...)
		}
	}
```

Add a helper:

```go
func findNoChunkNodes(nodes *index.NodeRepo, embeddings *index.EmbeddingRepo, queue *index.EmbedQueueRepo) ([]Issue, error) {
	indexed, listErr := nodes.List(index.ListFilter{})

	if listErr != nil {
		return nil, fmt.Errorf("doctor: list nodes: %w", listErr)
	}

	embeddedIDs, embeddedErr := embeddings.ListNodeIDs()

	if embeddedErr != nil {
		return nil, fmt.Errorf("doctor: list embedded nodes: %w", embeddedErr)
	}

	embeddedSet := make(map[string]struct{}, len(embeddedIDs))

	for _, id := range embeddedIDs {
		embeddedSet[id] = struct{}{}
	}

	pendingSet := map[string]struct{}{}

	if queue != nil {
		pendingIDs, pendingErr := queue.ListNodeIDs()

		if pendingErr != nil {
			return nil, fmt.Errorf("doctor: list pending: %w", pendingErr)
		}

		for _, id := range pendingIDs {
			pendingSet[id] = struct{}{}
		}
	}

	var issues []Issue

	for _, row := range indexed {
		if _, embedded := embeddedSet[row.ID]; embedded {
			continue
		}

		if _, pending := pendingSet[row.ID]; pending {
			continue
		}

		issues = append(issues, Issue{
			Kind:    IssueEmbedNoChunks,
			NodeID:  row.ID,
			Message: "node has no embedding rows",
		})
	}

	return issues, nil
}
```

(`strings` may not be needed for these changes — leave the existing imports as-is, and only add `embed`, `manifest` if not already present.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/doctor/ -run TestRun_EmbedStats -v`
Expected: PASS.

Run: `go test ./internal/doctor/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/doctor/doctor.go internal/doctor/doctor_test.go
git commit -m "feat(doctor): embed stats + large-chunk and no-chunks issues"
```

---

## Task 13: cmd_doctor wires repos + manifest, prints stats block

**Files:**
- Modify: `cmd/tusk/cmd_doctor.go`
- Modify: `cmd/tusk/cmd_doctor_test.go`

- [ ] **Step 1: Write the failing test**

The existing pattern in `cmd_doctor_test.go` uses `runCLISplit(root, args...)` against a temp workspace built with helpers like `setupTempWorkspace` or `newWorkspaceWithNodeTypes`. The new test follows the same shape, plus the stub Ollama server pattern from `e2e_semantic_test.go`:

```go
func TestDoctor_PrintsEmbedStatsBlock(test *testing.T) {
	tmpDir := initWorkspace(test)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string]any{"embedding": []float64{1, 0, 0}})
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

	if mkErr := os.MkdirAll(filepath.Join(tmpDir, "notes"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	if writeErr := os.WriteFile(filepath.Join(tmpDir, "notes/a.md"), []byte(`---
type: note
title: A
---

Body for A.
`), 0o644); writeErr != nil {
		test.Fatalf("write A: %v", writeErr)
	}

	reindexCmd := newRootCmd()
	reindexCmd.SetArgs([]string{"reindex"})

	if execErr := reindexCmd.Execute(); execErr != nil {
		test.Fatalf("reindex: %v", execErr)
	}

	out := &bytes.Buffer{}

	doctorCmd := newRootCmd()
	doctorCmd.SetOut(out)
	doctorCmd.SetErr(out)
	doctorCmd.SetArgs([]string{"doctor"})

	if execErr := doctorCmd.Execute(); execErr != nil {
		test.Fatalf("doctor: %v\nout:\n%s", execErr, out.String())
	}

	if !strings.Contains(out.String(), "embed stats:") {
		test.Errorf("output missing 'embed stats:' line:\n%s", out.String())
	}

	if !strings.Contains(out.String(), "top by chunks:") {
		test.Errorf("output missing 'top by chunks:' block:\n%s", out.String())
	}
}

func TestDoctor_OmitsEmbedStatsWithoutConfig(test *testing.T) {
	// initWorkspace creates a workspace without an [embeddings] section, so
	// loaded.Embeddings.Provider == "" and the stats branch must be skipped.
	_ = initWorkspace(test)

	out := &bytes.Buffer{}

	doctorCmd := newRootCmd()
	doctorCmd.SetOut(out)
	doctorCmd.SetErr(out)
	doctorCmd.SetArgs([]string{"doctor"})

	if execErr := doctorCmd.Execute(); execErr != nil {
		test.Fatalf("doctor: %v", execErr)
	}

	if strings.Contains(out.String(), "embed stats:") {
		test.Errorf("output includes 'embed stats:' when no embeddings configured:\n%s", out.String())
	}
}
```

Add imports as needed: `"bytes"`, `"encoding/json"`, `"net/http"`, `"net/http/httptest"`, `"os"`, `"path/filepath"` (already used in `e2e_semantic_test.go`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tusk/ -run TestDoctor_PrintsEmbedStatsBlock -v`
Expected: FAIL — output does not contain `embed stats:`.

- [ ] **Step 3: Update cmd_doctor**

Rewrite `cmd/tusk/cmd_doctor.go`:

```go
package main

import (
	"fmt"
	"os"

	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	doctorCmd := &cobra.Command{
		Use:   "doctor",
		Short: "Surface validation warnings and index health issues",
		RunE: func(cmd *cobra.Command, args []string) error {
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

			store, openErr := index.Open(ws.IndexPath)

			if openErr != nil {
				return openErr
			}

			defer store.Close()

			report, runErr := doctor.Run(doctor.Config{
				Nodes:         index.NewNodeRepo(store),
				Edges:         index.NewEdgeRepo(store),
				EmbedQueue:    index.NewEmbedQueueRepo(store),
				WorkflowDrift: index.NewWorkflowDriftRepo(store),
				PropertyDrift: index.NewPropertyDriftRepo(store),
				Embeddings:    index.NewEmbeddingRepo(store),
				Manifest:      loaded,
			})

			if runErr != nil {
				return runErr
			}

			out := cmd.OutOrStdout()

			if len(report.Issues) == 0 {
				_, _ = fmt.Fprintln(out, "doctor: no issues")
			}

			for _, issue := range report.Issues {
				_, _ = fmt.Fprintf(out, "  [%s] %s: %s\n", issue.Kind, issue.NodeID, issue.Message)
			}

			_, _ = fmt.Fprintf(out, "embed queue depth: %d\n", report.EmbedQueueDepth)

			if report.EmbedStats != nil {
				stats := report.EmbedStats

				_, _ = fmt.Fprintf(out, "embed stats: %d nodes, %d chunks (mean %.1f, median %d, max %d)\n",
					stats.TotalNodes, stats.TotalChunks, stats.MeanChunks, stats.MedianChunks, stats.MaxChunks)

				if len(stats.TopByChunks) > 0 {
					_, _ = fmt.Fprintln(out, "top by chunks:")

					for _, entry := range stats.TopByChunks {
						_, _ = fmt.Fprintf(out, "  %s\t%d\n", entry.NodeID, entry.Chunks)
					}
				}
			}

			return nil
		},
	}

	return doctorCmd
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/tusk/ -run TestDoctor -v`
Expected: PASS.

Run: `go test ./cmd/tusk/...`
Expected: PASS. If a pre-existing doctor test asserted output verbatim and now sees extra lines, relax it to substring checks.

- [ ] **Step 5: Commit**

```bash
git add cmd/tusk/cmd_doctor.go cmd/tusk/cmd_doctor_test.go
git commit -m "feat(doctor): print embed stats and top-by-chunks block"
```

---

## Task 14: E2E semantic snippet assertion

**Files:**
- Modify: `cmd/tusk/e2e_semantic_test.go`

- [ ] **Step 1: Extend `TestE2E_SemanticRetrieval`**

The existing `TestE2E_SemanticRetrieval` already builds a stub Ollama, creates three nodes (Apples/Bananas/Cherries), reindexes, and runs `tusk query --semantic 'apricot'`. Append snippet assertions to that test (do not introduce a new fixture test — reuse the one that already exists).

Locate the line after the existing semantic query's output assertion. Append:

```go
	if !strings.Contains(out.String(), "SNIPPET") {
		test.Errorf("tabwriter missing SNIPPET column:\n%s", out.String())
	}

	// Re-run with --json and assert snippet key is present.
	jsonOut := &bytes.Buffer{}

	jsonCmd := newRootCmd()
	jsonCmd.SetOut(jsonOut)
	jsonCmd.SetErr(jsonOut)
	jsonCmd.SetArgs([]string{"query", "type=note", "--semantic", "apricot", "--json"})

	if execErr := jsonCmd.Execute(); execErr != nil {
		test.Fatalf("json query: %v\n%s", execErr, jsonOut.String())
	}

	var results []map[string]any

	if jsonErr := json.Unmarshal(jsonOut.Bytes(), &results); jsonErr != nil {
		test.Fatalf("json unmarshal: %v\n%s", jsonErr, jsonOut.String())
	}

	if len(results) == 0 {
		test.Fatalf("empty json results:\n%s", jsonOut.String())
	}

	if _, ok := results[0]["snippet"].(string); !ok {
		test.Errorf("result[0] missing snippet key: %v", results[0])
	}
```

If the existing fixture's node bodies are empty (the `node create` calls only set title/path), edit the test's setup so each created node has a real body — write the file content directly with `os.WriteFile` after `node create`, or replace the `node create` calls with raw file writes that include both frontmatter and body. The snippet assertion needs non-empty body content for the assertion `results[0]["snippet"]` to be a non-empty string. The plan's stronger assertion can therefore be:

```go
	snippet, ok := results[0]["snippet"].(string)

	if !ok {
		test.Errorf("result[0] missing snippet key: %v", results[0])
	} else if snippet == "" {
		test.Errorf("result[0] snippet is empty; ensure fixture node bodies are non-empty")
	}
```

- [ ] **Step 3: Run test to verify it passes**

Run: `go test ./cmd/tusk/ -run TestE2E_SemanticQuery_SnippetEndToEnd -v`
Expected: PASS.

- [ ] **Step 4: Final sanity sweep**

Run all:

```bash
make vet
make lint
make test
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/tusk/e2e_semantic_test.go
git commit -m "test(e2e): assert snippet content in semantic query and --json output"
```

---

## Acceptance check (post-implementation)

After Task 14, verify against the spec's Acceptance Criteria:

1. `tusk query 'type=note' --semantic 'auth flow'` shows SNIPPET column. ← Tasks 9, 14
2. `tusk query ... --semantic --json` emits snippet field. ← Tasks 10, 14
3. `mcp tusk_query` returns snippet. ← Task 11
4. `tusk doctor` on workspace with [embeddings]: prints stats, large-chunk and no-chunks issues. ← Tasks 12, 13
5. `tusk doctor` on workspace without [embeddings]: no new lines. ← Task 13
6. After `rm .tusk/index.db && tusk reindex`, everything works on a fresh DB. ← manual smoke; if the user wants, run `make build && rm .tusk/index.db && ./bin/tusk reindex && ./bin/tusk doctor` in a local fixture.
7. `make test`, `make vet`, `make lint` all green. ← Task 14
