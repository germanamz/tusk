# Embed Chunking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the `WholeDocument` chunker with a markdown-structural recursive splitter so documents of any size embed successfully across multiple chunks, and aggregate per-chunk scores back to per-node scores in retrieval.

**Architecture:** A new `MarkdownRecursive` `ChunkingStrategy` operates on the *body* of each node (frontmatter header is prepended per chunk). The drain loop becomes per-chunk with delete-all-then-insert cleanup. Retrieval keeps the one-row-per-node contract by taking the *max* chunk score per node.

**Tech Stack:** Go 1.21+, SQLite (already wired). No new dependencies. Tests use the existing `internal/embed` and `internal/filter` test patterns (stub embedders, in-memory index, `slog.Handler` capture).

**Spec:** `docs/superpowers/specs/2026-05-13-embed-chunking-design.md`

---

## File Structure

| File | Role |
|---|---|
| `internal/embed/payload.go` | Splits `BuildPayload` into `BuildHeader` + `BuildBody`; keeps `BuildPayload` as wrapper for back-compat. |
| `internal/embed/chunking.go` | Adds `MarkdownRecursive` strategy alongside the existing `WholeDocument`. Adds the recursive split + greedy pack algorithm. |
| `internal/embed/drain.go` | Inner loop becomes per-chunk. `DeleteByNodeID` runs once before the first `Upsert` per node. Header is prepended to each chunk's body before embedding. |
| `internal/filter/semantic.go` | `SemanticCandidate` gains a `ChunkIdx` field. `SemanticRank` aggregates by max-per-node. |
| `cmd/tusk/cmd_query.go` | Candidate-build loop iterates chunk rows. Default chunker switches from `WholeDocument{}` to `MarkdownRecursive{}`. |
| `internal/reindex/reindex.go` | Default `Chunker` config switches to `MarkdownRecursive{}` (call sites that pass `WholeDocument{}` explicitly stay as-is in tests, but production paths flip). |

Tests in matching `_test.go` files in each package, plus updates to existing fixtures in `internal/reindex/reindex_test.go`.

---

### Task 1: Split `BuildPayload` into `BuildHeader` + `BuildBody`

**Files:**
- Modify: `internal/embed/payload.go`
- Test: `internal/embed/payload_test.go`

The new chunker needs the header and body separately. Keep `BuildPayload` as a thin wrapper so existing callers and tests don't break.

- [ ] **Step 1: Add failing tests for `BuildHeader` and `BuildBody`**

Edit `internal/embed/payload_test.go` — append:

```go
func TestBuildHeader_IncludesTypeTitleAndSortedProperties(test *testing.T) {
	parsedNode := &node.Node{
		Type:  "ticket",
		Title: "Fix login",
		Properties: map[string]any{
			"type":     "ticket",
			"title":    "Fix login",
			"priority": 3,
			"area":     "auth",
		},
		Body: []byte("ignored body"),
	}

	header := string(embed.BuildHeader(parsedNode))

	if !strings.HasPrefix(header, "[type] ticket\n") {
		test.Errorf("header should start with `[type] ticket`: %q", header)
	}

	if !strings.Contains(header, "[title] Fix login\n") {
		test.Errorf("header missing title: %q", header)
	}

	if !strings.HasSuffix(header, "---\n") {
		test.Errorf("header should end with `---\\n` separator: %q", header)
	}

	areaIdx := strings.Index(header, "area=auth")
	priorityIdx := strings.Index(header, "priority=3")

	if areaIdx < 0 || priorityIdx < 0 || areaIdx > priorityIdx {
		test.Errorf("properties should be sorted alphabetically; got %q", header)
	}

	if strings.Contains(header, "ignored body") {
		test.Errorf("header must not include body: %q", header)
	}
}

func TestBuildBody_ReturnsParsedBodyVerbatim(test *testing.T) {
	parsedNode := &node.Node{
		Type:  "note",
		Title: "T",
		Body:  []byte("paragraph one\n\nparagraph two\n"),
	}

	body := embed.BuildBody(parsedNode)

	if string(body) != "paragraph one\n\nparagraph two\n" {
		test.Errorf("body = %q", body)
	}
}

func TestBuildPayload_EqualsHeaderPlusBody(test *testing.T) {
	parsedNode := &node.Node{
		Type:  "note",
		Title: "T",
		Properties: map[string]any{
			"type":  "note",
			"title": "T",
			"tag":   "x",
		},
		Body: []byte("body content"),
	}

	combined := append(embed.BuildHeader(parsedNode), embed.BuildBody(parsedNode)...)
	whole := embed.BuildPayload(parsedNode)

	if string(combined) != string(whole) {
		test.Errorf("BuildHeader+BuildBody != BuildPayload:\nheader+body: %q\npayload:     %q", combined, whole)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/embed/ -run 'TestBuildHeader|TestBuildBody|TestBuildPayload_EqualsHeaderPlusBody' -v
```

Expected: FAIL with `undefined: embed.BuildHeader` and `undefined: embed.BuildBody`.

- [ ] **Step 3: Refactor `BuildPayload` into `BuildHeader` + `BuildBody`**

Replace the body of `internal/embed/payload.go` with:

```go
package embed

import (
	"fmt"
	"sort"
	"strings"

	"github.com/germanamz/tusk/internal/node"
)

// BuildHeader renders a node's frontmatter (type, title, sorted remaining
// properties) followed by a `---\n` separator. The header is prepended to
// every chunk's body before embedding, so each chunk carries doc-level
// context.
func BuildHeader(parsedNode *node.Node) []byte {
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

	return []byte(builder.String())
}

// BuildBody returns the node's raw body bytes. Chunkers split this slice.
func BuildBody(parsedNode *node.Node) []byte {
	return parsedNode.Body
}

// BuildPayload renders a node into the canonical embed input by
// concatenating BuildHeader and BuildBody. Retained for callers that want
// the unchunked payload (e.g. tests, WholeDocument strategy).
func BuildPayload(parsedNode *node.Node) []byte {
	header := BuildHeader(parsedNode)
	body := BuildBody(parsedNode)

	out := make([]byte, 0, len(header)+len(body))
	out = append(out, header...)
	out = append(out, body...)

	return out
}
```

- [ ] **Step 4: Run all `internal/embed` tests to confirm green**

```bash
go test ./internal/embed/ -v
```

Expected: PASS for all `TestBuildHeader`, `TestBuildBody`, `TestBuildPayload*` (including the two pre-existing tests `TestBuildPayload_IncludesTypeTitleAndBody` and `TestBuildPayload_StableOrder` which now exercise the wrapper).

- [ ] **Step 5: Commit**

```bash
git add internal/embed/payload.go internal/embed/payload_test.go
git commit -m "refactor(embed): split BuildPayload into BuildHeader + BuildBody"
```

---

### Task 2: Implement `MarkdownRecursive` chunker

**Files:**
- Modify: `internal/embed/chunking.go`
- Test: `internal/embed/chunking_test.go`

New strategy. Existing `WholeDocument` stays; its tests stay green.

- [ ] **Step 1: Write failing tests for `MarkdownRecursive`**

Edit `internal/embed/chunking_test.go` — append:

```go
func TestMarkdownRecursive_EmptyInputReturnsSingleEmptyChunk(test *testing.T) {
	chunks := embed.MarkdownRecursive{}.Chunk(nil)

	if len(chunks) != 1 {
		test.Fatalf("len = %d, want 1", len(chunks))
	}

	if len(chunks[0]) != 0 {
		test.Errorf("chunks[0] should be empty, got %q", chunks[0])
	}
}

func TestMarkdownRecursive_SmallInputReturnsSingleChunk(test *testing.T) {
	payload := []byte("a small body that fits in one chunk")

	chunks := embed.MarkdownRecursive{}.Chunk(payload)

	if len(chunks) != 1 {
		test.Fatalf("len = %d, want 1", len(chunks))
	}

	if string(chunks[0]) != string(payload) {
		test.Errorf("chunks[0] = %q, want %q", chunks[0], payload)
	}
}

func TestMarkdownRecursive_SplitsOnH2Headings(test *testing.T) {
	// Use small budgets so we force splits without needing huge fixtures.
	strategy := embed.MarkdownRecursive{
		TargetBytes:  60,
		MaxBytes:     200,
		OverlapBytes: 0,
	}

	body := []byte("intro line\n## Section A\nalpha alpha alpha alpha\n## Section B\nbravo bravo bravo bravo\n## Section C\ncharlie charlie\n")

	chunks := strategy.Chunk(body)

	if len(chunks) < 2 {
		test.Fatalf("expected multiple chunks, got %d: %q", len(chunks), chunks)
	}

	// Every "## " marker except (possibly) the first should appear at the
	// start of some chunk — i.e. heading boundaries are preserved.
	headingChunks := 0

	for _, chunk := range chunks {
		if strings.HasPrefix(string(bytes.TrimLeft(chunk, "\n")), "## ") {
			headingChunks++
		}
	}

	if headingChunks < 2 {
		test.Errorf("expected at least 2 chunks to start at a heading, got %d. chunks=%q", headingChunks, chunks)
	}
}

func TestMarkdownRecursive_FallsBackToParagraphs(test *testing.T) {
	strategy := embed.MarkdownRecursive{
		TargetBytes:  40,
		MaxBytes:     120,
		OverlapBytes: 0,
	}

	body := []byte("para one with some words here.\n\npara two also with words here.\n\npara three with more words here.")

	chunks := strategy.Chunk(body)

	if len(chunks) < 2 {
		test.Fatalf("expected multiple chunks, got %d: %q", len(chunks), chunks)
	}

	for _, chunk := range chunks {
		if len(chunk) > strategy.MaxBytes {
			test.Errorf("chunk %q exceeds MaxBytes %d (len=%d)", chunk, strategy.MaxBytes, len(chunk))
		}
	}
}

func TestMarkdownRecursive_HardSplitsWhenNoSeparators(test *testing.T) {
	strategy := embed.MarkdownRecursive{
		TargetBytes:  10,
		MaxBytes:     20,
		OverlapBytes: 0,
	}

	body := bytes.Repeat([]byte("X"), 100)

	chunks := strategy.Chunk(body)

	if len(chunks) < 5 {
		test.Fatalf("expected at least 5 chunks for a 100-byte all-Xs input with maxBytes=20, got %d", len(chunks))
	}

	for idx, chunk := range chunks {
		if len(chunk) > strategy.MaxBytes {
			test.Errorf("chunk %d exceeds MaxBytes %d (len=%d)", idx, strategy.MaxBytes, len(chunk))
		}
	}
}

func TestMarkdownRecursive_OverlapAppearsAtStartOfNextChunk(test *testing.T) {
	strategy := embed.MarkdownRecursive{
		TargetBytes:  40,
		MaxBytes:     200,
		OverlapBytes: 10,
	}

	// Build a body with three distinct paragraph blocks so it splits cleanly.
	body := []byte("alpha alpha alpha alpha alpha alpha\n\nbravo bravo bravo bravo bravo bravo\n\ncharlie charlie charlie charlie charlie")

	chunks := strategy.Chunk(body)

	if len(chunks) < 2 {
		test.Fatalf("need at least 2 chunks for overlap test, got %d: %q", len(chunks), chunks)
	}

	prevTail := chunks[0][len(chunks[0])-strategy.OverlapBytes:]
	nextHead := chunks[1][:strategy.OverlapBytes]

	if !bytes.Equal(prevTail, nextHead) {
		test.Errorf("expected chunks[1] to start with last %d bytes of chunks[0].\nprev tail: %q\nnext head: %q", strategy.OverlapBytes, prevTail, nextHead)
	}
}

func TestMarkdownRecursive_LargeDocStaysUnderMax(test *testing.T) {
	// Reproducer for the 2026-05-13 incident: a 225 KB synthetic doc should
	// chunk to many pieces, all under MaxBytes.
	body := bytes.Repeat([]byte("Some prose with paragraphs.\n\n## Heading\n\nMore prose here.\n\n"), 4000)

	strategy := embed.MarkdownRecursive{}

	chunks := strategy.Chunk(body)

	if len(chunks) < 30 {
		test.Errorf("expected many chunks for a 225KB doc, got %d", len(chunks))
	}

	for idx, chunk := range chunks {
		if len(chunk) > strategy.MaxBytes {
			// MaxBytes defaults to 7200 — anything larger blows nomic's 2048-tok window.
			test.Errorf("chunk %d has len=%d, exceeds default MaxBytes 7200", idx, len(chunk))
		}
	}
}
```

You'll need to add `"bytes"` and `"strings"` to the test file's imports if not already present.

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/embed/ -run TestMarkdownRecursive -v
```

Expected: FAIL with `undefined: embed.MarkdownRecursive`.

- [ ] **Step 3: Implement `MarkdownRecursive` in `chunking.go`**

Replace `internal/embed/chunking.go` with:

```go
package embed

import "bytes"

// ChunkingStrategy splits a body payload into one or more chunks; each chunk
// is embedded independently. The drain loop prepends a per-node header to
// each chunk before embedding, so chunkers operate on the body only.
type ChunkingStrategy interface {
	Chunk(payload []byte) [][]byte
}

// WholeDocument returns the entire payload as a single chunk. Retained for
// short docs and tests; production paths use MarkdownRecursive.
type WholeDocument struct{}

// Chunk implements ChunkingStrategy.
func (strategy WholeDocument) Chunk(payload []byte) [][]byte {
	return [][]byte{payload}
}

// MarkdownRecursive splits a body using recursive descent through
// markdown-aware separators (H2 -> H3 -> paragraph -> line -> sentence ->
// word -> byte). Pieces are then greedily packed up to TargetBytes, with
// the previous chunk's tail seeding OverlapBytes of context into the next
// chunk. MaxBytes is a hard cap that keeps every chunk under the embedding
// model's context window.
//
// Zero-value defaults target ~400 tokens with ~50-token overlap for
// nomic-embed-text (2048-token window): TargetBytes=1600, MaxBytes=7200,
// OverlapBytes=200, on the ~4-bytes-per-token heuristic.
type MarkdownRecursive struct {
	TargetBytes  int
	MaxBytes     int
	OverlapBytes int
}

const (
	defaultTargetBytes  = 1600
	defaultMaxBytes     = 7200
	defaultOverlapBytes = 200
)

var markdownSeparators = []string{
	"\n## ",
	"\n### ",
	"\n\n",
	"\n",
	". ",
	" ",
	"",
}

func (strategy MarkdownRecursive) target() int {
	if strategy.TargetBytes > 0 {
		return strategy.TargetBytes
	}

	return defaultTargetBytes
}

func (strategy MarkdownRecursive) maxSize() int {
	if strategy.MaxBytes > 0 {
		return strategy.MaxBytes
	}

	return defaultMaxBytes
}

func (strategy MarkdownRecursive) overlap() int {
	if strategy.OverlapBytes > 0 {
		return strategy.OverlapBytes
	}

	return defaultOverlapBytes
}

// Chunk implements ChunkingStrategy.
func (strategy MarkdownRecursive) Chunk(payload []byte) [][]byte {
	if len(payload) == 0 {
		return [][]byte{nil}
	}

	pieces := splitRecursive(payload, markdownSeparators, strategy.maxSize())
	chunks := packPieces(pieces, strategy.target(), strategy.overlap())

	if len(chunks) == 0 {
		return [][]byte{nil}
	}

	return chunks
}

// splitRecursive walks separators from highest priority to lowest, splitting
// text at the first separator that occurs in it. Pieces that still exceed
// maxBytes recurse to the next separator. The empty-string separator is the
// floor and guarantees termination.
func splitRecursive(text []byte, separators []string, maxBytes int) [][]byte {
	if len(text) <= maxBytes {
		return [][]byte{text}
	}

	for idx, sep := range separators {
		if sep == "" {
			return hardSplit(text, maxBytes)
		}

		if !bytes.Contains(text, []byte(sep)) {
			continue
		}

		var pieces [][]byte

		for _, piece := range splitKeepingPrefix(text, sep) {
			if len(piece) <= maxBytes {
				pieces = append(pieces, piece)

				continue
			}

			pieces = append(pieces, splitRecursive(piece, separators[idx+1:], maxBytes)...)
		}

		return pieces
	}

	return hardSplit(text, maxBytes)
}

// splitKeepingPrefix splits text on sep, keeping sep as the prefix of every
// piece after the first. Empty pieces are dropped.
func splitKeepingPrefix(text []byte, sep string) [][]byte {
	sepBytes := []byte(sep)
	parts := bytes.Split(text, sepBytes)
	pieces := make([][]byte, 0, len(parts))

	for idx, part := range parts {
		var piece []byte

		if idx == 0 {
			piece = part
		} else {
			piece = make([]byte, 0, len(sepBytes)+len(part))
			piece = append(piece, sepBytes...)
			piece = append(piece, part...)
		}

		if len(piece) > 0 {
			pieces = append(pieces, piece)
		}
	}

	return pieces
}

// hardSplit cuts text into fixed-size byte windows. Last resort when no
// markdown separator occurs in text.
func hardSplit(text []byte, maxBytes int) [][]byte {
	var out [][]byte

	for offset := 0; offset < len(text); offset += maxBytes {
		end := offset + maxBytes

		if end > len(text) {
			end = len(text)
		}

		out = append(out, text[offset:end])
	}

	return out
}

// packPieces greedily concatenates pieces up to target bytes, then emits a
// chunk and seeds the next chunk with the previous chunk's tail (OverlapBytes).
// A single piece larger than target becomes its own chunk.
func packPieces(pieces [][]byte, target, overlap int) [][]byte {
	var (
		chunks [][]byte
		cur    []byte
	)

	for _, piece := range pieces {
		if len(cur) > 0 && len(cur)+len(piece) > target {
			chunks = append(chunks, cur)

			if overlap > 0 && len(cur) > overlap {
				tail := cur[len(cur)-overlap:]
				next := make([]byte, 0, len(tail)+len(piece))
				next = append(next, tail...)
				cur = next
			} else {
				cur = nil
			}
		}

		cur = append(cur, piece...)
	}

	if len(cur) > 0 {
		chunks = append(chunks, cur)
	}

	return chunks
}
```

- [ ] **Step 4: Run all `internal/embed` tests**

```bash
go test ./internal/embed/ -v
```

Expected: PASS for every `TestMarkdownRecursive*` and the pre-existing `WholeDocument` cases.

- [ ] **Step 5: Run `make lint` to catch STYLE rule violations**

```bash
make lint
```

Expected: no findings. (Watch for ≥2-char identifiers, blank lines around `if err != nil` guards, named errors.)

- [ ] **Step 6: Commit**

```bash
git add internal/embed/chunking.go internal/embed/chunking_test.go
git commit -m "feat(embed): add MarkdownRecursive chunking strategy"
```

---

### Task 3: Per-chunk drain loop with delete-all-then-insert

**Files:**
- Modify: `internal/embed/drain.go:92-209` (per-batch inner loop)
- Test: `internal/embed/drain_test.go`

The drain loop changes from "embed `chunks[0]`, upsert `(node_id, 0)`" to:
1. Build header once.
2. Chunk the body.
3. `DeleteByNodeID` for this node.
4. Loop chunks, embed each `header + bodyChunk`, upsert sequential `chunk_idx`.
5. On any chunk failure, break the loop and re-enqueue at the node level (PR #369's retry-cap semantics preserved).

- [ ] **Step 1: Write failing tests for multi-chunk drain behavior**

Edit `internal/embed/drain_test.go` — append:

```go
func TestDrainQueue_EmbedsEveryChunkOfMultiChunkNode(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	// Body with three H2 sections, large enough that the splitter emits 3 chunks.
	body := strings.Repeat("alpha ", 200) +
		"\n## Section B\n" + strings.Repeat("bravo ", 200) +
		"\n## Section C\n" + strings.Repeat("charlie ", 200)

	createNodeFile(test, root, "multi.md", body)

	if upsertErr := nodeRepo.Upsert(index.NodeRow{ID: "multi", Type: "note", Path: "multi.md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert: %v", upsertErr)
	}

	if enqErr := queueRepo.Enqueue("multi"); enqErr != nil {
		test.Fatalf("enqueue: %v", enqErr)
	}

	stub := &drainStubEmbedder{dim: 3, model: "stub"}

	drained, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepo,
		Queue:      queueRepo,
		Embeddings: embeddingRepo,
		Embedder:   stub,
		Chunker:    embed.MarkdownRecursive{TargetBytes: 400, MaxBytes: 2000, OverlapBytes: 0},
	})

	if drainErr != nil {
		test.Fatalf("DrainQueue: %v", drainErr)
	}

	if drained != 1 {
		test.Errorf("drained = %d, want 1", drained)
	}

	if stub.calls < 2 {
		test.Errorf("embedder.calls = %d, want >= 2 (one per chunk)", stub.calls)
	}

	rows, getErr := embeddingRepo.GetByNodeID("multi")

	if getErr != nil {
		test.Fatalf("GetByNodeID: %v", getErr)
	}

	if len(rows) != stub.calls {
		test.Errorf("persisted rows = %d, want %d (one per embed call)", len(rows), stub.calls)
	}

	for idx, row := range rows {
		if row.ChunkIdx != idx {
			test.Errorf("rows[%d].ChunkIdx = %d, want %d (sequential)", idx, row.ChunkIdx, idx)
		}
	}
}

func TestDrainQueue_DeletesStaleChunksBeforeReembedding(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	// Pre-seed 5 stale chunks for a node, then drain — the new chunk count
	// should be < 5 (we use WholeDocument for a tiny body), and all old
	// rows (chunk_idx 1..4) must be gone.
	createNodeFile(test, root, "shrinking.md", "tiny body")

	if upsertErr := nodeRepo.Upsert(index.NodeRow{ID: "shrinking", Type: "note", Path: "shrinking.md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert node: %v", upsertErr)
	}

	for idx := 0; idx < 5; idx++ {
		if upErr := embeddingRepo.Upsert(index.EmbeddingRow{
			NodeID:      "shrinking",
			ChunkIdx:    idx,
			Model:       "stub",
			ContentHash: "old",
			Vector:      []float32{0.1, 0.1, 0.1},
			Dim:         3,
		}); upErr != nil {
			test.Fatalf("seed chunk %d: %v", idx, upErr)
		}
	}

	if enqErr := queueRepo.Enqueue("shrinking"); enqErr != nil {
		test.Fatalf("enqueue: %v", enqErr)
	}

	_, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepo,
		Queue:      queueRepo,
		Embeddings: embeddingRepo,
		Embedder:   &drainStubEmbedder{dim: 3, model: "stub"},
		Chunker:    embed.WholeDocument{},
	})

	if drainErr != nil {
		test.Fatalf("DrainQueue: %v", drainErr)
	}

	rows, _ := embeddingRepo.GetByNodeID("shrinking")

	if len(rows) != 1 {
		test.Errorf("expected exactly 1 chunk after re-embed; got %d", len(rows))
	}

	for _, row := range rows {
		if row.ContentHash == "old" {
			test.Errorf("stale chunk survived: %+v", row)
		}
	}
}

func TestDrainQueue_NodeFailureReenqueuesAndCleansOnRetry(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	body := strings.Repeat("alpha ", 200) + "\n## Section B\n" + strings.Repeat("bravo ", 200)
	createNodeFile(test, root, "flaky.md", body)

	if upsertErr := nodeRepo.Upsert(index.NodeRow{ID: "flaky", Type: "note", Path: "flaky.md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert: %v", upsertErr)
	}

	if enqErr := queueRepo.Enqueue("flaky"); enqErr != nil {
		test.Fatalf("enqueue: %v", enqErr)
	}

	// Embedder that fails on the second call, succeeds otherwise.
	failingMidStream := &midStreamFailEmbedder{dim: 3, model: "stub", failAt: 2}

	_, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepo,
		Queue:      queueRepo,
		Embeddings: embeddingRepo,
		Embedder:   failingMidStream,
		Chunker:    embed.MarkdownRecursive{TargetBytes: 400, MaxBytes: 2000, OverlapBytes: 0},
	})

	if drainErr != nil {
		test.Fatalf("DrainQueue: %v", drainErr)
	}

	// After the retry cap is hit, the node is dropped. Partial state from
	// any successful retry's DeleteByNodeID + Upsert sequence must not
	// leave duplicated chunks.
	rows, _ := embeddingRepo.GetByNodeID("flaky")

	chunkIdxs := make(map[int]struct{}, len(rows))

	for _, row := range rows {
		if _, dup := chunkIdxs[row.ChunkIdx]; dup {
			test.Errorf("duplicate ChunkIdx %d after retries: %+v", row.ChunkIdx, rows)
		}

		chunkIdxs[row.ChunkIdx] = struct{}{}
	}
}

type midStreamFailEmbedder struct {
	calls  int
	dim    int
	model  string
	failAt int // 1-indexed: fail when calls reaches failAt
}

func (stub *midStreamFailEmbedder) Embed(ctx context.Context, payload []byte) ([]float32, error) {
	stub.calls++

	if stub.calls == stub.failAt {
		return nil, fmt.Errorf("simulated chunk failure")
	}

	out := make([]float32, stub.dim)

	for idx := range out {
		out[idx] = 0.1
	}

	return out, nil
}

func (stub *midStreamFailEmbedder) Model() string { return stub.model }
func (stub *midStreamFailEmbedder) Dim() int      { return stub.dim }
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/embed/ -run 'TestDrainQueue_EmbedsEveryChunk|TestDrainQueue_DeletesStaleChunks|TestDrainQueue_NodeFailureReenqueues' -v
```

Expected: FAIL — `EmbedsEveryChunk` upserts only one row (today's behavior); `DeletesStaleChunks` still has the seeded chunks 1..4 after drain.

- [ ] **Step 3: Rewrite the per-batch inner loop in `drain.go`**

Open `internal/embed/drain.go`. Find the inner `for _, queued := range batch {` loop (lines 92-209). Replace its body — from `row, getErr := config.Nodes.Get(queued.NodeID)` down to the trailing `batchSucceeded++` — with the version below.

Concretely: keep the `Drain()` call, `len(batch)==0` exit, and the trailing `Logger.Info("drain batch complete", ...)` after the loop. Replace only the per-`queued` body.

```go
for _, queued := range batch {
	row, getErr := config.Nodes.Get(queued.NodeID)

	if getErr != nil {
		continue
	}

	content, readErr := os.ReadFile(filepath.Join(config.Root, row.Path))

	if readErr != nil {
		nextAttempts := queued.Attempts + 1

		if nextAttempts < MaxEmbedAttempts {
			_ = config.Queue.ReEnqueue(queued.NodeID, nextAttempts, readErr.Error())
		}

		continue
	}

	parsed, parseErr := node.ParseFile(row.Path, content)

	if parseErr != nil {
		nextAttempts := queued.Attempts + 1

		if nextAttempts < MaxEmbedAttempts {
			_ = config.Queue.ReEnqueue(queued.NodeID, nextAttempts, parseErr.Error())
		}

		continue
	}

	header := BuildHeader(parsed)
	body := BuildBody(parsed)
	bodyChunks := config.Chunker.Chunk(body)

	if len(bodyChunks) == 0 {
		continue
	}

	if config.Logger != nil {
		config.Logger.Debug("embed attempt",
			"node_id", queued.NodeID,
			"header_bytes", len(header),
			"body_bytes", len(body),
			"chunks", len(bodyChunks),
		)
	}

	if delErr := config.Embeddings.DeleteByNodeID(queued.NodeID); delErr != nil {
		if config.Logger != nil {
			config.Logger.Warn("embed delete-before-insert failed",
				"node_id", queued.NodeID,
				"err", delErr.Error(),
			)
		}

		batchFailed++

		continue
	}

	nodeFailed := false

	for chunkIdx, bodyChunk := range bodyChunks {
		payload := make([]byte, 0, len(header)+len(bodyChunk))
		payload = append(payload, header...)
		payload = append(payload, bodyChunk...)

		embedStart := time.Now()

		vector, embedErr := config.Embedder.Embed(ctx, payload)

		embedLatency := time.Since(embedStart)

		if embedErr != nil {
			if config.Logger != nil {
				config.Logger.Warn("embed call failed",
					"node_id", queued.NodeID,
					"chunk_idx", chunkIdx,
					"chunks_total", len(bodyChunks),
					"payload_bytes", len(payload),
					"model", config.Embedder.Model(),
					"err", embedErr.Error(),
				)
			}

			nextAttempts := queued.Attempts + 1

			if nextAttempts >= MaxEmbedAttempts {
				if config.Logger != nil {
					config.Logger.Warn("embed gave up",
						"node_id", queued.NodeID,
						"attempts", nextAttempts,
						"err", embedErr.Error(),
					)
				}
			} else {
				if reEnqErr := config.Queue.ReEnqueue(queued.NodeID, nextAttempts, embedErr.Error()); reEnqErr != nil {
					if config.Logger != nil {
						config.Logger.Warn("embed re-enqueue failed",
							"node_id", queued.NodeID,
							"err", reEnqErr.Error(),
						)
					}
				} else if config.Logger != nil {
					config.Logger.Warn("embed re-enqueued",
						"node_id", queued.NodeID,
						"attempts", nextAttempts,
					)
				}
			}

			batchFailed++
			nodeFailed = true

			break
		}

		contentHash := sha256.Sum256(payload)

		if upsertErr := config.Embeddings.Upsert(index.EmbeddingRow{
			NodeID:      queued.NodeID,
			ChunkIdx:    chunkIdx,
			Model:       config.Embedder.Model(),
			ContentHash: hex.EncodeToString(contentHash[:]),
			Vector:      vector,
			Dim:         config.Embedder.Dim(),
		}); upsertErr != nil {
			return drained, upsertErr
		}

		if config.Logger != nil {
			config.Logger.Debug("embed attempt success",
				"node_id", queued.NodeID,
				"chunk_idx", chunkIdx,
				"chunks_total", len(bodyChunks),
				"vector_dim", len(vector),
				"latency_ms", embedLatency.Milliseconds(),
			)
		}
	}

	if !nodeFailed {
		drained++
		batchSucceeded++
	}
}
```

- [ ] **Step 4: Run all `internal/embed` tests**

```bash
go test ./internal/embed/ -v
```

Expected: PASS, including:
- `TestDrainQueue_EmbedsEveryChunkOfMultiChunkNode` — embedder called once per chunk; persisted rows match.
- `TestDrainQueue_DeletesStaleChunksBeforeReembedding` — pre-seeded chunks 1..4 are gone.
- `TestDrainQueue_NodeFailureReenqueuesAndCleansOnRetry` — no duplicate ChunkIdx.
- All pre-existing tests (`DrainsToEmpty`, `NoopWhenNoEmbedder`, `LogsWarnOnEmbedError`, `GivesUpAfterMaxAttempts`) still pass; the log assertions still match because the new code keeps the same `msg=` strings.

- [ ] **Step 5: Run `make lint`**

```bash
make lint
```

Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/embed/drain.go internal/embed/drain_test.go
git commit -m "feat(embed): per-chunk drain loop with delete-then-insert cleanup"
```

---

### Task 4: Max-per-node aggregation in `SemanticRank`

**Files:**
- Modify: `internal/filter/semantic.go`
- Test: `internal/filter/semantic_test.go`

`SemanticCandidate` gains a `ChunkIdx` field (used internally as a tie-breaker hook for the future snippet wiring); `SemanticRank` collapses multiple candidates per `NodeID` to one `ScoredResult` whose `Score` is the max chunk score.

- [ ] **Step 1: Write failing tests for multi-chunk aggregation**

Edit `internal/filter/semantic_test.go` — append:

```go
func TestSemanticRank_MaxPerNodeAcrossChunks(test *testing.T) {
	candidates := []filter.SemanticCandidate{
		{NodeID: "alpha", ChunkIdx: 0, Vector: []float32{0.1, 0, 0}},   // weak
		{NodeID: "alpha", ChunkIdx: 1, Vector: []float32{1, 0, 0}},      // strong — should win for alpha
		{NodeID: "bravo", ChunkIdx: 0, Vector: []float32{0.5, 0.5, 0}}, // medium
		{NodeID: "bravo", ChunkIdx: 1, Vector: []float32{0.6, 0.5, 0}}, // slightly stronger
	}

	ranked := filter.SemanticRank(candidates, []float32{1, 0, 0})

	if len(ranked) != 2 {
		test.Fatalf("expected 2 unique nodes, got %d: %+v", len(ranked), ranked)
	}

	if ranked[0].NodeID != "alpha" {
		test.Errorf("alpha's strong chunk should rank first; got %+v", ranked)
	}

	// Bravo's score must equal the higher of its two chunks (chunk 1, not chunk 0).
	chunk1Score := embed.CosineSimilarity([]float32{0.6, 0.5, 0}, []float32{1, 0, 0})

	for _, result := range ranked {
		if result.NodeID == "bravo" && result.Score != chunk1Score {
			test.Errorf("bravo.Score = %v, want %v (max-per-node)", result.Score, chunk1Score)
		}
	}
}

func TestSemanticRank_DeterministicTieBreakByNodeID(test *testing.T) {
	candidates := []filter.SemanticCandidate{
		{NodeID: "zebra", ChunkIdx: 0, Vector: []float32{1, 0, 0}},
		{NodeID: "apple", ChunkIdx: 0, Vector: []float32{1, 0, 0}},
		{NodeID: "mango", ChunkIdx: 0, Vector: []float32{1, 0, 0}},
	}

	ranked := filter.SemanticRank(candidates, []float32{1, 0, 0})

	if len(ranked) != 3 {
		test.Fatalf("len = %d", len(ranked))
	}

	if ranked[0].NodeID != "apple" || ranked[1].NodeID != "mango" || ranked[2].NodeID != "zebra" {
		test.Errorf("equal scores should sort by NodeID ascending; got %+v", ranked)
	}
}
```

You'll need to add `"github.com/germanamz/tusk/internal/embed"` to the imports.

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/filter/ -run 'TestSemanticRank_MaxPerNode|TestSemanticRank_DeterministicTieBreak' -v
```

Expected: FAIL — current implementation returns one ScoredResult per candidate (so len=4 for the first test) and uses non-deterministic ordering on ties.

- [ ] **Step 3: Rewrite `SemanticRank` to aggregate max-per-node**

Replace the body of `internal/filter/semantic.go` with:

```go
package filter

import (
	"sort"

	"github.com/germanamz/tusk/internal/embed"
)

// SemanticCandidate pairs a node's chunk vector with its ids for ranking.
// ChunkIdx is used internally to disambiguate multiple chunks per node and
// to give downstream code (e.g. snippet generation) a hook to identify which
// chunk matched best.
type SemanticCandidate struct {
	NodeID   string
	ChunkIdx int
	Vector   []float32
}

// ScoredResult is one ranked node. Score is the max cosine similarity across
// the node's chunks.
type ScoredResult struct {
	NodeID string
	Score  float64
}

// SemanticRank scores each candidate by cosine similarity to queryVector and
// returns one row per node, with Score equal to the maximum chunk score for
// that node. Results are sorted by score descending, ties broken by NodeID
// ascending for determinism. Candidates whose vectors mismatch queryVector's
// dimension are silently skipped.
func SemanticRank(candidates []SemanticCandidate, queryVector []float32) []ScoredResult {
	bestByNode := make(map[string]float64, len(candidates))

	for _, candidate := range candidates {
		if len(candidate.Vector) != len(queryVector) {
			continue
		}

		score := embed.CosineSimilarity(candidate.Vector, queryVector)

		if prev, present := bestByNode[candidate.NodeID]; !present || score > prev {
			bestByNode[candidate.NodeID] = score
		}
	}

	scored := make([]ScoredResult, 0, len(bestByNode))

	for nodeID, score := range bestByNode {
		scored = append(scored, ScoredResult{NodeID: nodeID, Score: score})
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

- [ ] **Step 4: Run all `internal/filter` tests**

```bash
go test ./internal/filter/ -v
```

Expected: PASS for the new tests AND the three pre-existing tests (`OrdersByDescendingCosine`, `HandlesEmptyCandidates`, `SkipsCandidatesWithMismatchedDim`). The pre-existing tests don't set `ChunkIdx`, which is fine — zero-value `ChunkIdx: 0` is consistent across them.

- [ ] **Step 5: Commit**

```bash
git add internal/filter/semantic.go internal/filter/semantic_test.go
git commit -m "feat(filter): aggregate semantic rank by max-per-node across chunks"
```

---

### Task 5: Iterate chunk rows in `cmd_query.go` candidate build

**Files:**
- Modify: `cmd/tusk/cmd_query.go:191-207` (candidate build loop in `runSemanticQuery`)

The `embeddingRepo.ListByNodeIDs(nodeIDs)` call already returns every chunk per node (one `EmbeddingRow` per `(node_id, chunk_idx)`). The candidate build loop just needs to pass `ChunkIdx` through; it's a one-line addition.

- [ ] **Step 1: Update the loop**

In `cmd/tusk/cmd_query.go`, find the candidate build block (around line 200):

```go
for _, embeddingRow := range loadedRows {
    candidates = append(candidates, filter.SemanticCandidate{
        NodeID: embeddingRow.NodeID,
        Vector: embeddingRow.Vector,
    })
}
```

Replace with:

```go
for _, embeddingRow := range loadedRows {
	candidates = append(candidates, filter.SemanticCandidate{
		NodeID:   embeddingRow.NodeID,
		ChunkIdx: embeddingRow.ChunkIdx,
		Vector:   embeddingRow.Vector,
	})
}
```

- [ ] **Step 2: Build and run tests**

```bash
make build && go test ./...
```

Expected: build succeeds; full test suite passes. (`SemanticRank` now collapses per-node, so the existing query path returns one row per node correctly.)

- [ ] **Step 3: Commit**

```bash
git add cmd/tusk/cmd_query.go
git commit -m "feat(cli): pass chunk_idx through semantic query candidate build"
```

---

### Task 6: Swap default chunker to `MarkdownRecursive` in production paths

**Files:**
- Modify: `internal/reindex/reindex.go` (look for `embed.WholeDocument{}` in default-config code paths)
- Modify: `cmd/tusk/cmd_reindex.go` and any other caller that constructs a `DrainConfig`/`reindex.Config` with `WholeDocument{}` for production use
- Modify: `internal/reindex/reindex_test.go` — leave existing fixture as-is or swap to `MarkdownRecursive{}` if the test fails after the production swap (see Step 2)

The interface is unchanged; the swap is mechanical. Test fixtures that explicitly pass `WholeDocument{}` for unit testing can stay (they exercise the single-chunk path).

- [ ] **Step 1: Find every `WholeDocument{}` instantiation outside tests**

```bash
git grep -n 'embed\.WholeDocument{}' -- ':!*_test.go'
```

Expected output (from the original spec audit): two call sites — `internal/reindex/reindex.go` line ~1859 area (its production config builder), and `cmd/tusk/*` (around the reindex command, ~line 2358 in the original plan-5 listing). Note actual line numbers, they may have drifted.

- [ ] **Step 2: Replace each production-path `embed.WholeDocument{}` with `embed.MarkdownRecursive{}`**

For each match from Step 1:

```go
// before
Chunker: embed.WholeDocument{},

// after
Chunker: embed.MarkdownRecursive{},
```

Leave `_test.go` files untouched — their `WholeDocument{}` usage continues to test the single-chunk path of the per-chunk drain loop, which we want.

- [ ] **Step 3: Run the full suite**

```bash
go test ./... && make lint
```

Expected: all green. If `internal/reindex/reindex_test.go` fails because its fixture relied on a specific chunk count, switch *that test's* fixture to a small enough body that `MarkdownRecursive` still emits one chunk — or leave `WholeDocument` in the test config explicitly. Match the existing test's intent.

- [ ] **Step 4: Commit**

```bash
git add internal/reindex/reindex.go cmd/tusk/*.go
git commit -m "feat(embed): default chunker is MarkdownRecursive"
```

---

### Task 7: Manual verification with the 225 KB reproducer

**Files:** none modified — this is an end-to-end smoke test.

The handoff identified a 225 KB synthetic doc as the original failure case (144,537 failing embedding calls over 12 minutes). With chunking, the same doc should embed successfully across N chunks.

- [ ] **Step 1: Spin up a scratch workspace**

```bash
mkdir -p /tmp/tusk-chunking-verify/nodes
cd /tmp/tusk-chunking-verify

cat > tusk.toml <<'EOF'
[embeddings]
provider = "ollama"
model    = "nomic-embed-text"
endpoint = "http://localhost:11434"
dim      = 768
EOF
```

- [ ] **Step 2: Generate a 225 KB markdown document**

```bash
python3 -c '
import textwrap
section = "## Section\n\n" + ("This is some prose with paragraphs. " * 20) + "\n\n"
print("---\ntype: note\ntitle: huge\n---\n")
print(section * 700)
' > /tmp/tusk-chunking-verify/nodes/huge.md
wc -c /tmp/tusk-chunking-verify/nodes/huge.md
```

Expected: ~225 KB.

- [ ] **Step 3: Run reindex against the scratch workspace**

Make sure Ollama is running locally with `nomic-embed-text` pulled. Then:

```bash
cd /workspaces/tusk
./bin/tusk reindex --root /tmp/tusk-chunking-verify --verbose 2>&1 | tee /tmp/tusk-chunking-verify/reindex.log
```

Expected: reindex completes in a few seconds. The log shows `msg="embed attempt"` with `chunks=N` (where N >> 1), followed by N `msg="embed attempt success"` lines, and no `msg="embed call failed"` or `msg="embed gave up"`.

- [ ] **Step 4: Verify chunks were persisted**

```bash
sqlite3 /tmp/tusk-chunking-verify/.tusk/index.db \
  "SELECT node_id, count(*) FROM embeddings GROUP BY node_id;"
```

Expected: at least one row, with count > 30 (the 225 KB body should chunk into many pieces under the default 7200 MaxBytes).

- [ ] **Step 5: Verify semantic query returns the node**

```bash
./bin/tusk query --root /tmp/tusk-chunking-verify --semantic "section prose"
```

Expected: one result (the `huge` node), score > 0.

- [ ] **Step 6: Push the branch and open a PR**

```bash
cd /workspaces/tusk
git push -u origin feat/embed-chunking
gh pr create --title "feat(embed): markdown-structural recursive chunking" --body "$(cat <<'EOF'
## Summary
- Replace `WholeDocument` with `MarkdownRecursive` as the default chunker — splits markdown body on H2 → H3 → paragraphs → lines → sentences → words → bytes.
- Drain loop becomes per-chunk with delete-all-then-insert cleanup; partial state self-heals on retry.
- `SemanticRank` aggregates by max-per-node across chunks; one row per node preserved (spec §10.8).
- Frontmatter header prepended to every chunk for doc-level context per chunk.

Resolves the 144k-failing-request incident from 2026-05-13. See `docs/superpowers/specs/2026-05-13-embed-chunking-design.md` for the design and `docs/handoffs/2026-05-13-embed-chunking-followups.md` for deferred follow-ups.

## Test plan
- [x] Unit tests for `BuildHeader`/`BuildBody`, `MarkdownRecursive` (8 cases incl. 225 KB stress), per-chunk drain (3 cases), max-per-node `SemanticRank` (2 cases).
- [x] Existing tests in `internal/embed`, `internal/filter`, `internal/reindex` all pass.
- [x] Manual smoke: 225 KB synthetic doc embeds across N chunks with zero failures; `--semantic` returns the node.
EOF
)"
```

---

## Self-review notes

**Spec coverage:**
- `MarkdownRecursive` strategy + algorithm → Task 2.
- `BuildHeader` / `BuildBody` split + wrapper → Task 1.
- Per-chunk drain loop with delete-all-then-insert → Task 3.
- Header prepended to each chunk → Task 3 (the `payload := header + bodyChunk` step).
- Max-per-node aggregation with deterministic tie-break → Task 4.
- `ChunkIdx` field on `SemanticCandidate` (snippet-wiring hook) → Tasks 4 + 5.
- Default chunker swap → Task 6.
- 225 KB reproducer manual verification → Task 7.

**Out-of-scope items per spec — confirmed not in plan:**
- Snippet generation, `tusk doctor` chunking diagnostics, `[embeddings] chunking = "..."` selector, token-aware splitting, parent-child retrieval, surgical hash-skip re-embedding. All catalogued in the follow-ups handoff.

**Sequencing:**
1. Payload refactor (pure, tests stay green).
2. Chunker (new code, no callers yet).
3. Drain loop (per-chunk + delete-all-insert).
4. Filter aggregation.
5. Query call-site (one-line passthrough).
6. Default chunker swap (mechanical).
7. End-to-end smoke + PR.
