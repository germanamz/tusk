---
type: spec
title: Embed chunking — markdown-structural recursive splitter
session-date: "2026-05-13"
---

# Embed chunking design

Replace the `WholeDocument` chunking strategy with a markdown-structural recursive splitter so that documents larger than the embedding model's context window embed successfully across multiple chunks. Implements the chunking story deferred from Plan 5 (spec §10.7) and eliminates the failure mode that PR #369's retry cap currently masks.

## Background

On 2026-05-13 a `tusk reindex` against a ~50-node workspace fired 144,537 failing `POST /api/embeddings` requests against Ollama in 12 minutes. Root cause: `WholeDocument.Chunk` sends entire documents (some >200 KB) as one chunk; `nomic-embed-text`'s 2048-token context window overflows on anything past ~8 KB and Ollama returns HTTP 500. PR #369 capped the retry loop at three attempts per drain, so the queue now terminates instead of looping forever, but **the underlying overflow remains**. This spec resolves it.

`internal/embed/chunking.go` already exposes a `ChunkingStrategy` interface and the `embeddings` table is already keyed by `(node_id, chunk_idx)`. The storage layer is ready for multi-chunk nodes. What needs to change: the splitter, the drain loop, the payload composition, and the retrieval aggregation.

## Goals

- Docs of any size embed successfully across N chunks.
- Retrieval still returns one row per node (spec §10.8), with the per-node score being the *best* matching chunk.
- Stale chunks are cleaned up when a doc shrinks or its content changes.
- The `ChunkingStrategy` interface stays as-is so future strategies (token-aware, parent-child, etc.) plug in without breaking changes.
- Defaults are baked in — no new `tusk.toml` knobs in v1.

## Non-goals

- Token-aware splitting with a real tokenizer (current heuristic: 4 bytes ≈ 1 token).
- Per-workspace `[embeddings] chunking = "..."` selector (spec §10.7 anticipates it; deferred until there's a second strategy worth selecting between).
- Snippet generation for `--semantic` results (spec §10.8 mentions `snippet` but it's never been wired; the new ranker tracks best-chunk-per-node internally as a hook for the follow-up).
- `tusk doctor` chunking diagnostics — follow-up.
- Parent-child / small-to-big retrieval (the canonical v2 upgrade path identified during research; schema doesn't need changes today to enable it later).
- Surgical hash-skip re-embedding (current reindex re-enqueues all nodes anyway).
- OpenAI / Voyage / Anthropic providers, per-model context-window registry — Plan 5.x.
- MCP tool surface changes — none.

## Design

### Strategy: `MarkdownRecursive`

A new `ChunkingStrategy` implementation, added alongside `WholeDocument`:

```go
// internal/embed/chunking.go
type MarkdownRecursive struct {
    TargetBytes  int  // default 1600  (~400 tok)
    MaxBytes     int  // default 7200  (~1800 tok — under nomic 2048 minus header room)
    OverlapBytes int  // default 200   (~50 tok)
}

func (strategy MarkdownRecursive) Chunk(payload []byte) [][]byte { ... }
```

**Defaults:** target 1600 bytes, max 7200 bytes, overlap 200 bytes — hardcoded in the struct's zero-value behavior. These match the empirical sweet spot for `nomic-embed-text`: ~400 tokens with 10–15% overlap, well under the 2048-token limit so headers and tokenizer overhead fit.

`WholeDocument` stays in the codebase as a legitimate strategy for short docs and tests, but stops being the default. `reindex.Run` and `cmd_query.go` swap to `MarkdownRecursive{}`.

### Algorithm: recursive descent through separators

The LangChain/Chroma baseline — pegged at ~88-89% recall in Chroma's evaluation, which is the de-facto production baseline:

```
separators = [
  "\n## ",   // H2
  "\n### ",  // H3
  "\n\n",    // paragraph
  "\n",      // line
  ". ",      // sentence
  " ",       // word
  "",        // byte
]

split(text, separators):
  if len(text) <= target: return [text]
  sep := first separator in `separators` that occurs in text
  pieces := text.Split(sep)
  for each piece:
    if len(piece) > max: recurse with separators[next:]
  greedily pack pieces with sep glue until adding next piece > target → emit chunk
  add `overlap` bytes of previous chunk's tail to start of next chunk
```

Key properties:
- Heading-first means chunks align with markdown section boundaries when possible.
- The byte separator (`""`) is the floor — guarantees termination even on pathological input.
- Overlap is byte-counted, not token-counted, and stitched to the *start* of the next chunk so the next chunk's leading context matches the previous chunk's trailing context.

### Payload composition: header prepended to every chunk

`BuildPayload` is split into two functions, with `BuildPayload` kept as a thin wrapper for back-compat:

```go
// internal/embed/payload.go
func BuildHeader(parsed *node.Node) []byte  // [type] / [title] / sorted props / "---\n"
func BuildBody(parsed *node.Node) []byte    // parsed.Body verbatim

func BuildPayload(parsed *node.Node) []byte {
    return append(BuildHeader(parsed), BuildBody(parsed)...)
}
```

Chunkers operate on the *body* only. The drain loop prepends the header to each chunk's body slice before embedding. This is "contextual retrieval lite": every chunk carries doc-level context (type, title, properties), which the research (Anthropic Sep-2024) shows materially improves retrieval recall. Storage cost is small — headers are short.

The splitter operating on the body alone also means heading detection (`\n## `) doesn't trip over the `[type]` / `[title]` frontmatter lines.

### Drain loop: per-chunk embedding with delete-all-then-insert

`internal/embed/drain.go` per-batch inner loop changes:

```go
header := BuildHeader(parsed)
body := BuildBody(parsed)
bodyChunks := config.Chunker.Chunk(body)

// 1. Clear existing chunks for this node up front.
if delErr := config.Embeddings.DeleteByNodeID(queued.NodeID); delErr != nil { ... }

// 2. Embed each chunk independently.
for chunkIdx, bodyChunk := range bodyChunks {
    payload := append(append([]byte{}, header...), bodyChunk...)
    vector, embedErr := config.Embedder.Embed(ctx, payload)
    if embedErr != nil {
        // re-enqueue node (same retry-cap semantics as PR #369);
        // partial chunks 0..chunkIdx-1 are persisted but will be
        // deleted on the next retry's DeleteByNodeID.
        break
    }
    contentHash := sha256.Sum256(payload)
    config.Embeddings.Upsert(EmbeddingRow{
        NodeID:   queued.NodeID,
        ChunkIdx: chunkIdx,
        Model:    config.Embedder.Model(),
        ContentHash: hex.EncodeToString(contentHash[:]),
        Vector:   vector,
        Dim:      config.Embedder.Dim(),
    })
}
```

**Notable properties:**
- **Atomicity:** if chunk 3 of 5 fails, chunks 0-2 are persisted and the node re-enqueues. Next retry's `DeleteByNodeID` wipes the partial state and starts over. Net: eventually consistent, never permanently torn.
- **Retry semantics preserved:** `MaxEmbedAttempts` from PR #369 still bounds retries at the node level — a chunk failure counts as a node failure.
- **Empty body:** `MarkdownRecursive.Chunk(nil)` returns `[][]byte{nil}` (one empty chunk), so header-only embedding still happens for stub nodes — parity with today's behavior.
- **No new `EmbeddingRepo` method required.** `DeleteByNodeID` + `Upsert` already exist. The "delete tail when shrinking" case is covered for free by delete-all-then-insert.

### Retrieval: max-per-node aggregation

`internal/filter/semantic.go` gains a `ChunkIdx` field on `SemanticCandidate` and computes max-per-node:

```go
type SemanticCandidate struct {
    NodeID   string
    ChunkIdx int      // internal — used to break ties / track best chunk
    Vector   []float32
}

type ScoredResult struct {
    NodeID string
    Score  float64   // = max over that node's chunks
}

func SemanticRank(candidates []SemanticCandidate, queryVector []float32) []ScoredResult {
    bestByNode := map[string]float64{}
    for _, c := range candidates {
        if len(c.Vector) != len(queryVector) { continue }
        score := embed.CosineSimilarity(c.Vector, queryVector)
        if prev, ok := bestByNode[c.NodeID]; !ok || score > prev {
            bestByNode[c.NodeID] = score
        }
    }
    // materialize → []ScoredResult, sort by Score desc, return
}
```

`cmd/tusk/cmd_query.go` candidate-build loop changes from "one candidate per node" to "one candidate per chunk row" — `embeddingRepo.ListByNodeIDs` already returns every chunk per node, so the change is just unrolling the row → candidate mapping.

Output shape stays one row per node (spec §10.8 contract). Downstream callers (CLI, MCP) see no change.

**Why max-per-node, not mean or top-K-then-dedupe:** mean penalizes long docs (one perfect chunk loses to one good chunk in a 1-chunk doc); top-K-then-dedupe introduces a `K` parameter without materially changing the per-node output. Max is the production RAG consensus when the contract is per-document.

## Files involved

**Production code:**

| File | Change |
|---|---|
| `internal/embed/chunking.go` | Add `MarkdownRecursive` struct + algorithm. Keep `WholeDocument`. |
| `internal/embed/payload.go` | Split into `BuildHeader` + `BuildBody`; keep `BuildPayload` as wrapper. |
| `internal/embed/drain.go` | Per-chunk loop. `DeleteByNodeID` then `Upsert` each chunk. |
| `internal/filter/semantic.go` | Add `ChunkIdx` to `SemanticCandidate`. Max-per-node in `SemanticRank`. |
| `cmd/tusk/cmd_query.go` | Candidate build loop iterates chunk rows. Default chunker → `MarkdownRecursive{}`. |
| `internal/reindex/reindex.go` | Default `Chunker` → `MarkdownRecursive{}`. |

**Tests:**

| File | Change |
|---|---|
| `internal/embed/chunking_test.go` | Keep `WholeDocument` cases. Add `MarkdownRecursive` suite (heading-cut, paragraph-fallback, byte-fallback, target/max/overlap bounds, empty input, 225 KB synthetic stress doc). |
| `internal/embed/payload_test.go` | Coverage for `BuildHeader` / `BuildBody`; assert `BuildPayload` = `BuildHeader + BuildBody`. |
| `internal/embed/drain_test.go` | Multi-chunk fixture. Assert N `Upsert`s with sequential `ChunkIdx`. Assert `DeleteByNodeID` precedes the first `Upsert`. Assert chunk-mid failure → node re-enqueue + retry cleans partial state. |
| `internal/filter/semantic_test.go` | Multi-chunk fixture. Assert max-per-node. Assert deterministic ordering (sort by score desc; ties resolved by `node_id` ascending). |
| `internal/reindex/reindex_test.go` | Swap `WholeDocument` → `MarkdownRecursive` in the happy-path fixture. |

## Implementation sequencing

1. **Refactor `payload.go`** — introduce `BuildHeader` / `BuildBody`, keep `BuildPayload` wrapper. Pure refactor; existing tests stay green. (TDD: add `BuildHeader` / `BuildBody` tests first, then split implementation.)
2. **Implement `MarkdownRecursive`** next to `WholeDocument`. TDD: write the chunking-strategy suite first, then implement the recursive splitter.
3. **Update `drain.go`** to the per-chunk loop with delete-all-then-insert. TDD: extend `drain_test.go` first.
4. **Update `filter/semantic.go`** for max-per-node aggregation; update `cmd_query.go` candidate-build loop. TDD: `semantic_test.go` first.
5. **Swap default chunker** to `MarkdownRecursive{}` in `reindex.Run` and `cmd_query.go`.
6. **Manual verification:** re-run the handoff's 225 KB reproducer; expect zero failures and N chunks persisted per node. Confirm `tusk query --semantic "..."` returns sensible nodes.

## Open questions / risks

- **Byte-per-token heuristic (4) is English-biased.** Non-ASCII content (CJK, emoji-heavy) may have ~1-2 bytes per token, so byte-budgeted chunks could undershoot target tokens. Acceptable for v1 — log/measure when a real non-ASCII workspace appears.
- **Overlap stitching across heading boundaries** may produce a chunk whose first 200 bytes are part of the previous section. This is intentional (overlap improves recall on queries spanning section boundaries) but worth visually verifying on a real workspace.
- **Partial state during retry** means `tusk query --semantic` between a chunk failure and a successful re-drain could rank a node by its partial chunks. Acceptable — retries finish fast and partial state self-heals.
