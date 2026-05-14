---
type: spec
title: Snippet generation and doctor chunking diagnostics
session-date: "2026-05-14"
---

# Snippet generation and doctor chunking diagnostics

Spec A from the embed-chunking follow-ups handoff
(`docs/handoffs/2026-05-13-embed-chunking-followups.md`). Covers items
**#1 snippet generation** and **#2 doctor chunking diagnostics**.

Both items become cheap once the embeddings table carries the chunk body
text. This spec bundles them so the schema change ships once.

## Goals

1. Semantic queries return a `snippet` field — the first ~200 characters
   of the highest-scoring chunk's body — in CLI JSON, CLI tabwriter, and
   the MCP `tusk_query` tool.
2. `tusk doctor` reports per-workspace chunking aggregates (total
   embedded nodes, total chunks, mean/median/max chunks-per-node,
   top-5 nodes by chunk count) and surfaces two new issue kinds:
   `embed-large-chunk` (a chunk's body is ≥ 90% of the chunker's
   `MaxBytes`) and `embed-no-chunks` (a node is indexed but has zero
   embedding rows).

## Non-goals

- Sub-window highlighting (query-term-centered snippets). Handoff
  explicitly punts.
- Snippet column for non-semantic queries. Without ranking there is no
  "best chunk".
- JSON output for `tusk doctor`. Existing doctor command does not emit
  structured output; that is a separate concern.
- Online schema migration. The existing index is treated as throwaway:
  schema bumps require nuking `.tusk/index.db` and re-running
  `tusk reindex`. See [Migration](#migration) below.

## Architecture

The single load-bearing piece is **storing each chunk's body alongside
its vector**. Once that is in place, both features reduce to small
read-path additions.

### Schema change

```sql
CREATE TABLE embeddings (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    node_id      TEXT NOT NULL,
    chunk_idx    INTEGER NOT NULL DEFAULT 0,
    model        TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    vector       BLOB NOT NULL,
    dim          INTEGER NOT NULL,
    body         TEXT NOT NULL DEFAULT '',   -- new
    UNIQUE(node_id, chunk_idx)
);
```

`body` holds the body slice (the same `bodyChunk` that drain.go
already produces from `Chunker.Chunk(body)`). The frontmatter header
is *not* stored — it is reconstructed from the node row when needed
and is identical across a node's chunks.

### EmbeddingRow

```go
type EmbeddingRow struct {
    NodeID      string
    ChunkIdx    int
    Model       string
    ContentHash string
    Vector      []float32
    Dim         int
    Body        string  // new
}
```

`Upsert` writes body; `scanEmbeddings` reads it.

### Stats query

A new repo method:

```go
// Stats aggregates the embeddings table for tusk doctor.
func (repo *EmbeddingRepo) Stats(largeChunkThreshold int) (EmbeddingStats, error)

type EmbeddingStats struct {
    TotalNodes   int                  // distinct node_ids in embeddings
    TotalChunks  int                  // COUNT(*) of embeddings
    MeanChunks   float64              // TotalChunks / TotalNodes (0 when no nodes)
    MedianChunks int                  // median chunk count per node
    MaxChunks    int
    TopByChunks  []NodeChunkCount     // up to 5, descending; ties broken by node_id
    LargeChunks  []NodeChunkInfo      // chunks whose length(body) >= largeChunkThreshold
}

type NodeChunkCount struct {
    NodeID string
    Chunks int
}

type NodeChunkInfo struct {
    NodeID   string
    ChunkIdx int
    BodyLen  int
}
```

`largeChunkThreshold` is supplied by the caller (doctor passes
`int(0.9 * embed.DefaultMaxBytes)` — see below). Keeping the threshold
out of the repo makes the SQL parameterizable and the repo unaware of
chunker policy.

Zero-chunk-node detection is *not* part of `Stats` — it requires a
join against the `nodes` table. The doctor computes it separately by
listing all node IDs from `NodeRepo` and subtracting the set of node
IDs returned by a new `EmbeddingRepo.ListNodeIDs() ([]string, error)`.

### Chunker default exposure

`internal/embed/chunking.go` already defines `defaultMaxBytes = 4000`
as a package-private constant. Promote it to an exported constant
`DefaultMaxBytes = 4000` so the doctor can compute the threshold
without hard-coding the number itself.

The `MarkdownRecursive` zero-value still uses the default when
`MaxBytes` is unset; nothing else changes about the chunker.

### Drain loop

In `internal/embed/drain.go`, the chunk-write loop changes one line:

```go
if upsertErr := config.Embeddings.Upsert(index.EmbeddingRow{
    NodeID:      queued.NodeID,
    ChunkIdx:    chunkIdx,
    Model:       config.Embedder.Model(),
    ContentHash: hex.EncodeToString(contentHash[:]),
    Vector:      vector,
    Dim:         config.Embedder.Dim(),
    Body:        string(bodyChunk),  // new
}); upsertErr != nil { ... }
```

`bodyChunk` is already in scope. No other drain changes.

### Semantic ranking

`internal/filter/semantic.go` extends both `SemanticCandidate` and
`ScoredResult`:

```go
type SemanticCandidate struct {
    NodeID   string
    ChunkIdx int
    Vector   []float32
    Body     string  // new; the body of this chunk (no header)
}

type ScoredResult struct {
    NodeID        string
    Score         float64
    BestChunkIdx  int     // new
    BestChunkBody string  // new; the Body of the candidate that produced Score
}
```

`SemanticRank` is updated so the best-chunk bookkeeping tracks not just
the score but also the chunk idx and body that produced it. The
existing dimension-skip behavior and tie-breaking rules are unchanged.

### Query renderers

Both `cmd/tusk/cmd_query.go` (`runSemanticQuery`) and
`internal/mcp/tools.go` (`registerQueryTool` semantic branch) construct
candidates with `Body: embeddingRow.Body` and consume
`scored.BestChunkBody`.

#### Snippet formatting

The render-time helper is a tiny pure function:

```go
// renderSnippet returns the first maxRunes of body with newlines
// collapsed to single spaces and a trailing ellipsis if truncated.
// maxRunes counts runes, not bytes, so multi-byte content is not split
// mid-codepoint. Returns "" when body is empty.
func renderSnippet(body string, maxRunes int) string
```

Lives next to the existing rendering code — likely a small new file
`cmd/tusk/snippet.go` so both `cmd_query.go` and the MCP tool can call
it (the MCP tool imports `cmd/tusk`? No — `internal/mcp` is its own
package). Therefore: place the helper in `internal/filter` next to
`ScoredResult`, exported as `filter.RenderSnippet`. Both call sites
import filter already.

`maxRunes` is hard-coded to 200 in both call sites. No flag, no
manifest knob — handoff explicitly punts on that for v1.

#### CLI tabwriter

Today's header:
```
ID  SCORE
```

New header:
```
ID  SCORE   SNIPPET
```

The score column is `%.4f` as today. The snippet column is the output
of `RenderSnippet(scored.BestChunkBody, 200)`.

#### CLI JSON

`runSemanticQuery` currently ignores the `--json` flag and always
writes tabwriter. This spec wires real JSON output for the semantic
path because the handoff explicitly lists snippet in JSON output as
part of the deliverable.

The JSON shape is an array of result objects, one per ranked row,
mirroring the MCP response shape so the formats stay aligned:

```json
[
  {"id": "...", "score": 0.7421, "type": "note", "path": "...",
   "title": "...", "snippet": "..."}
]
```

When `--semantic` is set, the structural-stub branch at line 97-101 of
the current `cmd_query.go` (which prints `[]\n`) is bypassed by the
earlier `runSemanticQuery` shortcut. We add a `--json` branch inside
`runSemanticQuery` that builds the same shape and emits one JSON
document followed by `\n`.

#### MCP `tusk_query`

The ranking-result map gains `"snippet"`:

```go
ranking = append(ranking, map[string]any{
    "id":      scored.NodeID,
    "score":   scored.Score,
    "type":    byID[scored.NodeID].Type,
    "path":    byID[scored.NodeID].Path,
    "title":   byID[scored.NodeID].Title,
    "snippet": filter.RenderSnippet(scored.BestChunkBody, 200),
})
```

### Doctor

`internal/doctor/doctor.go`:

- `Config` gains two optional fields:
  - `Embeddings *index.EmbeddingRepo` — when nil, embed stats are
    skipped (mirrors how `WorkflowDrift` and `PropertyDrift` are
    optional today).
  - `Manifest *manifest.Manifest` — used to read
    `[embeddings]` block so the doctor only emits stats when the
    workspace has embeddings configured.
- `Report` gains `EmbedStats *doctor.EmbedStatsReport`. Pointer so it
  can be `nil` when the workspace has no embedding config or when the
  embeddings repo is not supplied.

```go
type EmbedStatsReport struct {
    TotalNodes   int
    TotalChunks  int
    MeanChunks   float64
    MedianChunks int
    MaxChunks    int
    TopByChunks  []index.NodeChunkCount
}
```

Two new issue kinds:

```go
IssueEmbedLargeChunk = "embed-large-chunk"
IssueEmbedNoChunks   = "embed-no-chunks"
```

Issue messages:

- `embed-large-chunk`: `chunk %d body is %d bytes (≥ %d threshold,
  chunker MaxBytes %d)` — operators can compare to MaxBytes.
- `embed-no-chunks`: `node has no embedding rows`.

Doctor logic (when `Config.Embeddings != nil && Config.Manifest !=
nil && Manifest.Embeddings.Provider != ""`):

1. Call `Embeddings.Stats(int(0.9 * embed.DefaultMaxBytes))`.
2. For each `LargeChunks` entry, append an `embed-large-chunk` Issue.
3. List all indexed node IDs (via `NodeRepo.List(ListFilter{})`),
   embedded node IDs (via `Embeddings.ListNodeIDs()`), and pending
   node IDs (via `EmbedQueue.ListNodeIDs()`). For nodes in indexed
   minus embedded *and* not pending, append `embed-no-chunks` Issues.
   Pending nodes are excluded because they have not failed — they
   simply have not run yet.
4. Populate `Report.EmbedStats` from the Stats result.

A new `EmbedQueueRepo.ListNodeIDs() ([]string, error)` method is
required for the pending check. It is one query and ~10 lines.

`cmd/tusk/cmd_doctor.go` is extended to pass the new repos and to
print the stats block after issues:

```
embed stats: 47 nodes, 188 chunks (mean 4.0, median 3, max 12)
top by chunks:
  notes/long-meeting-notes.md  12
  specs/v1-design.md           9
  ...
```

When `Report.EmbedStats == nil` (no embeddings config or not wired),
the stats block is omitted entirely.

### Tests

| Layer | Test |
|---|---|
| `internal/index/embedding_repo_test.go` | Body round-trips through Upsert/scan. |
| `internal/index/embedding_repo_test.go` | `Stats` with 0, 1, N nodes; mean/median correctness; top-5 ordering with ties; LargeChunks threshold inclusive comparison. |
| `internal/index/embedding_repo_test.go` | `ListNodeIDs` returns distinct, sorted node IDs. |
| `internal/filter/semantic_test.go` | `SemanticRank` returns BestChunkIdx and BestChunkBody matching the highest-scoring chunk per node, even when chunks tie on score (lowest ChunkIdx wins, deterministic). |
| `internal/filter/semantic_test.go` | `RenderSnippet` truncates at rune boundaries, collapses newlines, appends ellipsis when truncated, returns `""` for empty input. |
| `internal/doctor/doctor_test.go` | `Run` populates `EmbedStats` when wired; emits `embed-large-chunk` and `embed-no-chunks` issues. |
| `cmd/tusk/cmd_query_semantic_test.go` | Tabwriter output includes `SNIPPET` column with expected truncated content. |
| `cmd/tusk/e2e_semantic_test.go` | End-to-end semantic query against fixture workspace returns snippet content matching the chunk body. |
| `cmd/tusk/cmd_doctor_test.go` | Doctor prints stats block; prints nothing when embeddings repo absent. |
| `cmd/tusk/e2e_mcp_test.go` (or equivalent MCP test) | `tusk_query` semantic response includes `snippet` key. |

## Migration

The schema change is not backward compatible for existing index DBs
(the `body` column is new). Users with an existing `.tusk/index.db`
must:

```
rm .tusk/index.db
tusk reindex
```

This is documented in the release notes / PR description, not in code.
No migration logic is added to `index.Open`. Rationale: pre-1.0 tusk
already requires reindex after frontmatter / chunker changes; one more
hard cutover is acceptable and keeps the schema bootstrap simple.

## Risks and mitigations

- **Storage growth.** Body text duplicates body content already on
  disk. At ~50 nodes × ~10 chunks × ~4 KB → ~2 MB. Negligible. If a
  workspace grows to thousands of nodes, this stays bounded by the
  body bytes already on disk × 1× (no overlap multiplier in practice
  because overlap regions are short).
- **Reindex required.** Acceptable for pre-1.0; documented above.
- **Snippet whitespace from code-dense markdown.** Code fences are
  preserved by the chunker. After collapsing newlines, code snippets
  may look like blobs in the 200-char snippet. Acceptable for v1;
  improving this is a future highlight/snippet pass.
- **Stats query cost.** `length(body)` over the full embeddings table
  is O(N rows). N is small (~hundreds in typical workspaces).
  Doctor is interactive; no problem.

## Out of scope (defer to later spec)

- Snippet for non-semantic queries (no best chunk concept).
- JSON output for `tusk doctor`.
- Query-term-centered snippet windows.
- Configurable snippet length / large-chunk threshold via manifest.

## File touchpoints

- `internal/index/index.go` — schema DDL
- `internal/index/embedding_repo.go` — `Body` field, `Stats`,
  `ListNodeIDs`
- `internal/index/embed_queue_repo.go` — `ListNodeIDs` for the
  pending-exclusion check in doctor
- `internal/embed/chunking.go` — promote `DefaultMaxBytes` to exported
- `internal/embed/drain.go` — set `Body` on Upsert
- `internal/filter/semantic.go` — `Body` on candidate; BestChunk* on
  result; `RenderSnippet` helper
- `cmd/tusk/cmd_query.go` — populate candidate Body; render SNIPPET
  column
- `internal/mcp/tools.go` — populate candidate Body; add snippet to
  result map
- `internal/doctor/doctor.go` — `EmbedStats` field; new issue kinds;
  stats + large-chunk + no-chunks computation
- `cmd/tusk/cmd_doctor.go` — wire EmbeddingRepo + Manifest; print
  stats block
- Tests across all the above (see Tests table)

## Acceptance criteria

1. `tusk query 'type=note' --semantic 'authentication flow'` prints
   tabwriter rows with an inhabited SNIPPET column. The same command
   with `--json` emits a JSON array with each result carrying a
   `snippet` field.
2. `mcp tusk_query` with `semantic` set returns each result with a
   non-empty `snippet` for any node whose embedding rows have body
   text.
3. `tusk doctor` on a workspace with embeddings configured prints:
   - Existing issues
   - `embed-large-chunk` issues for chunks ≥ 90% of `DefaultMaxBytes`
   - `embed-no-chunks` issues for indexed nodes missing from
     embeddings (and not currently queued)
   - `embed stats:` summary line plus `top by chunks:` block
4. `tusk doctor` on a workspace without `[embeddings]` configured
   prints exactly today's output (no new lines).
5. After `rm .tusk/index.db && tusk reindex`, all of the above work
   on a fresh DB. Existing DBs without the `body` column produce a
   clear error from `Open` (the standard SQLite "no such column"
   surfaced through the existing scan error path is sufficient).
6. All new and existing tests pass: `make test`, `make vet`,
   `make lint`.
