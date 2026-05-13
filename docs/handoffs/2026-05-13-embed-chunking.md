---
type: handoff
title: Handoff 2026-05-13 — Embed chunking (token-aware splitter)
session-date: "2026-05-13"
---

# Tusk — Session Handoff: Embed chunking

**Status:** **Design NOT complete.** Open questions remain around chunking strategy, multi-chunk storage shape, and retrieval aggregation. The new session should start with `superpowers:brainstorming` — this handoff frames the problem and gathers context, but does not pre-spec the design.

**Branch:** start a fresh branch off `main` (e.g., `feat/embed-chunking`). `main` is at `a6a2e6f` (the merged retry-cap PR #369).

---

## Why this exists

On 2026-05-13 a `tusk reindex` run fired 144,537 failing `POST /api/embeddings` requests against a local Ollama in 12 minutes against a ~50-node workspace. The bug had two compounding causes:

1. **Oversized chunks** — `internal/embed/chunking.go:WholeDocument.Chunk` sends the entire document as one chunk. `nomic-embed-text` has a 2048-token context window, so any doc larger than ~8 KB overflows and Ollama returns HTTP 500 with `"input length exceeds the context length"`.
2. **Infinite retry** — `DrainQueue` re-enqueued failed nodes with no attempt counter, so the same failure repeated forever.

PR #369 (`feat(embed): cap embed-queue retries at MaxEmbedAttempts`, merged 2026-05-13) fixed #2 — the drain now terminates gracefully after 3 attempts with a `Warn msg="embed gave up"` line. **This handoff is the chunking fix**: split oversized payloads so they actually embed successfully, eliminating the failure mode that the retry cap currently masks.

Manually verified after PR #369: against a 225 KB synthetic doc, reindex completes in 5.5 s with 3 failures + 1 give-up. With chunking, that same doc should embed successfully across N chunks instead.

---

## The actual problem

`internal/embed/chunking.go` (all 17 lines):

```go
type ChunkingStrategy interface {
    Chunk(payload []byte) [][]byte
}

type WholeDocument struct{}

func (strategy WholeDocument) Chunk(payload []byte) [][]byte {
    return [][]byte{payload}
}
```

`WholeDocument` is the only `ChunkingStrategy` implementation; the interface was designed to allow future strategies but none exist yet.

Worse, `internal/embed/drain.go:140` takes only `chunks[0]`:

```go
vector, embedErr := config.Embedder.Embed(ctx, chunks[0])
// ...
if upsertErr := config.Embeddings.Upsert(index.EmbeddingRow{
    NodeID:      queued.NodeID,
    ChunkIdx:    0,   // <-- always 0
    ...
});
```

So even if a strategy returned multiple chunks today, only the first would be embedded and stored. **The chunking fix requires touching three layers, not just `chunking.go`:** the strategy (split correctly), the drain loop (embed every chunk), and the retrieval pipeline (aggregate scores across chunks per node).

`internal/index/embedding_repo.go` is already chunk-aware — the primary key is `(node_id, chunk_idx)`, `Upsert` handles `ON CONFLICT`, `GetByNodeID` returns all chunks for a node ordered by `chunk_idx`. **Storage is ready.** The two layers that need design work are split-strategy and retrieval.

---

## Open questions to brainstorm

### 1. Chunking strategy

Options to weigh:

- **Fixed-byte windows** (e.g., 6 KB with 500-byte overlap). Simple, no model dependency, but cuts mid-word/mid-sentence.
- **Fixed-token windows** using a tokenizer (e.g., `tiktoken-go`, or model-specific). Accurate but adds a dependency and a per-model concern (the model's tokenizer must match what the embedding API uses internally).
- **Sentence-aware splitting** (split on `. `, `\n\n`, then group). Preserves semantic units; trickier with code blocks, lists, tables.
- **Markdown-structural** (split on `## ` headings, then sub-split). Mirrors how humans navigate the doc. Requires a markdown parser.
- **Hybrid**: structural for the top-level cut, then byte/token fallback for oversized sections.

Other axes:
- **Overlap between chunks** — yes/no/configurable. Overlap improves retrieval recall but inflates storage 1.1–1.5×.
- **Max chunk size** — derived from model context (2048 tokens for `nomic-embed-text`), with a safety margin. Should this be (a) hardcoded per strategy, (b) read from `[embeddings]` in `tusk.toml`, or (c) read from a model-capability registry?

### 2. Payload composition

`internal/embed/drain.go:108` calls `BuildPayload(parsed)` — what's in the payload today, and does chunking need to respect that structure? Quick read of `internal/embed/payload.go` (not yet inspected as part of this handoff) is the right next step before the brainstorm.

### 3. Retrieval aggregation

`internal/filter/semantic.go` currently assumes one vector per node:

```go
type SemanticCandidate struct {
    NodeID string
    Vector []float32   // single vector
}
```

With multi-chunk embeddings, the candidate set becomes one row per `(node_id, chunk_idx)`. Options:

- **Max-score-per-node** — score every chunk, keep each node's best. Simple, biases toward "any chunk is a strong match." Risk: short docs with one perfect line beat long docs with diffuse relevance.
- **Mean-score-per-node** — average across chunks. Biases the other way; a few weak chunks drag a strong one down.
- **Top-K chunks then dedupe by node** — score, take top-K rows, deduplicate to node IDs in order. Common in production RAG.
- **Return chunks, not nodes** — change the result shape. Cleaner for citation/highlighting but breaks the existing `--semantic` flag's contract that one row per node comes back.

Which one is right depends on the *consumer* of `--semantic` (MCP-via-Claude vs. CLI human user). Worth checking how callers use the ranked output before deciding.

### 4. Re-embed semantics across reindex

Each `tusk reindex` re-enqueues every indexed node with `attempts=0` (see `internal/reindex/reindex.go:373-376`). With chunking, what happens to *old* chunks for a node when the doc content changes?

- If a doc shrinks from 3 chunks to 1, the embed pipeline should remove chunks 1 and 2 for that node. Today's `Upsert` is per-`(node_id, chunk_idx)` — no cleanup of stale rows.
- If the model changes, *all* chunks need re-embed. Today the `content_hash` column makes per-chunk re-embed decidable, but there's no cross-chunk consistency check.

Probably need a "purge stale chunks for node" step somewhere in the drain or the per-node embed flow.

### 5. Per-model context-window awareness

`nomic-embed-text` is 2048 tokens. OpenAI `text-embedding-3-small` is 8191. Voyage models vary. Hardcoding 2048 in the strategy ties us to one model; reading it from `tusk.toml`'s `[embeddings]` block pushes the burden onto the user; a registry per provider+model is the most accurate but most invasive.

For Plan 5's "Ollama only" reality, hardcoded-per-strategy may be the right pragmatic choice; revisit when OpenAI/Voyage providers land (`internal/manifest/loader.go:430` is the gate today).

---

## Files involved

Production code:
- `internal/embed/chunking.go` — strategy interface + `WholeDocument` (the thing being replaced/joined).
- `internal/embed/drain.go` — the loop. Currently embeds only `chunks[0]` with `ChunkIdx: 0`. Needs a per-chunk loop.
- `internal/embed/payload.go` — `BuildPayload` shapes the input. Read it before the brainstorm.
- `internal/index/embedding_repo.go` — already chunk-aware; probably needs a `DeleteByNodeIDExcept(nodeID, keepChunkIdxs []int)` for stale-chunk cleanup.
- `internal/filter/semantic.go` — needs aggregation logic across multiple chunks per node.
- `cmd/tusk/cmd_query.go:139` (`runSemanticQuery`) — the consumer of `SemanticRank`. Decides what shape the result should take.

Tests:
- `internal/embed/chunking_test.go` — only tests `WholeDocument`. Will need substantial new coverage per strategy.
- `internal/embed/drain_test.go` — tests assume one chunk per node today. Multi-chunk path needs assertions on N upserts per node.
- `internal/filter/semantic_test.go` — if aggregation changes, tests need to cover the new shape.

Docs:
- `docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md` and `docs/superpowers/plans/2026-05-06-tusk-v1-5-semantic-retrieval.md` — the Plan 5 design that landed `WholeDocument` as a deliberate Plan-5 simplification with the chunking story explicitly deferred. Re-read §"Future work" or equivalent to make sure the design intent matches whatever you propose here.

---

## What the retry cap does and does NOT guarantee

After PR #369:

- A persistently-failing node fails fast (3 attempts per drain) and the queue terminates.
- The drain loop emits `Warn msg="embed gave up"` with `attempts=3` and the error, so operators see the failure.
- The node still exists in the index — only the *embedding* is missing. Structural queries still work; semantic queries miss this node.

The retry cap is a **safety net**, not a fix. Chunking makes the cap rarely fire because the actual root cause (overflow) is resolved at the source. Both should ship.

---

## Out of scope (deliberately — don't try to bundle)

- **Per-model context-window registry.** Decide a pragmatic default for nomic-embed-text; revisit when a second provider lands.
- **Cross-doc deduplication / near-duplicate detection.** Separate problem.
- **Re-ranking with a different model after recall.** Out of scope; not part of Plan 5.
- **Caching embeddings across model upgrades.** The `content_hash + model` columns make this possible, but actual cache-key design is its own session.
- **Anything that changes the MCP tool surface** (`tusk_query` etc.). The semantic flag should continue to take a string and return ranked nodes — only the *internals* of how scores are computed should change.

---

## Where to start the new session

> "Pick up the embed chunking work. Background and open questions are in `docs/handoffs/2026-05-13-embed-chunking.md`. The retry-cap PR #369 has merged on `main` — start a fresh branch off `main`. **Brainstorm first** with `superpowers:brainstorming` — the chunking strategy, retrieval aggregation, and stale-chunk cleanup are all open design questions. Once the brainstorm produces a spec, write a plan and execute. Read `internal/embed/payload.go` early; it's the one file I didn't audit and it informs whether structural splitting is feasible."

---

## Sync first

```bash
git fetch origin
git checkout main
git pull --ff-only
git checkout -b feat/embed-chunking
```

---

## Conventions reminder (unchanged from prior handoff)

- Conventional commits, scope required: `feat(embed):`, `feat(index):`, `feat(filter):`, etc.
- No `Co-Authored-By` or "Generated with Claude Code" footers — user preference.
- STYLE.md rules 1–4 are linter-enforced. ≥2-char identifiers, named errors, blank lines around `if err != nil` guards, `test *testing.T` not `t *testing.T`.
- Lefthook pre-commit runs gofmt/vet/lint/test. Don't bypass with `--no-verify`.
- `<new-diagnostics>` blocks fire on stale LSP state after TDD steps. Verify with `go test ./... && make lint` before reacting.
- `make test-race` errors with `CGO requires CGO_ENABLED=1` on this devcontainer — a pre-existing env quirk. Run race tests via `CGO_ENABLED=1 go test -race ./...` if you need them.
