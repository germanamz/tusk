---
type: handoff
title: Embed chunking — deferred follow-ups
session-date: "2026-05-13"
---

# Embed chunking follow-ups

The chunking design (`docs/superpowers/specs/2026-05-13-embed-chunking-design.md`) intentionally defers several items so the v1 fix stays focused. This handoff catalogs them so they don't get lost. Each is independent and can be picked up separately.

The shipping design lands `MarkdownRecursive` as the new default chunker, per-chunk embedding in the drain loop, and max-per-node aggregation in retrieval. Anything in this doc is *additive* to that baseline.

---

## 1. Snippet generation for `--semantic` results

**Status:** spec §10.8 lists `"snippet"` in the result shape. Never wired up. The new `SemanticRank` tracks best-chunk-per-node internally (via `SemanticCandidate.ChunkIdx`), which is the hook this follow-up needs.

**What to do:**
- Extend `ScoredResult` with `BestChunkIdx int`.
- In `cmd_query.go`, load the body of the best chunk for each ranked node (or just store the chunk body alongside the vector in `embeddings`, trading storage for read speed).
- Render `snippet` field in JSON output and the human-readable tabwriter format.
- MCP `tusk_query` tool exposes it too.

**Watch out for:**
- Headers are prepended to every chunk's *payload*; the snippet should probably be the *body* slice only, not the full payload.
- Long chunks may need a sub-window (first 200 chars containing query terms? hard without a real ranker — punt to "first 200 chars of best chunk" for v1).

---

## 2. `tusk doctor` chunking diagnostics

**Status:** spec §10.7 originally said "engine truncates at the boundary and `tusk doctor` surfaces the truncation." We no longer truncate — but a diagnostic for *how* nodes are chunking is still valuable for operators.

**What to do:**
- Add a `tusk doctor` check that reports per-node chunk count distribution: total nodes embedded, mean/median/max chunks per node, top-5 nodes by chunk count.
- Flag nodes whose largest chunk approaches `MaxBytes` (potential mid-split; user should consider restructuring the doc).
- Flag nodes with zero chunks (embed never succeeded — should match the `embed gave up` warn from PR #369).

**Implementation hint:** add a `Stats()` method to `EmbeddingRepo` returning aggregates, then a doctor check that consumes it. Doctor's existing pattern is in `cmd/tusk/cmd_doctor.go`.

---

## 3. Per-workspace `[embeddings] chunking = "..."` selector

**Status:** spec §10.7 anticipates this; not wired. Worth doing when there's a second strategy worth selecting between.

**What to do:**
- Add `Chunking string` to `manifest.Embeddings`.
- Validate against `{"markdown-recursive", "whole-document"}` (and future strategies).
- Wire a strategy factory in `internal/embed/` (e.g., `NewChunker(name string) (ChunkingStrategy, error)`).
- Both `reindex.Run` and `cmd_query.go` use the factory.
- Default when unset: `"markdown-recursive"`.

**Don't bundle into v1** because we only have one strategy. The selector with one option is dead weight.

---

## 4. Token-aware splitting

**Status:** v1 uses byte heuristic (4 bytes ≈ 1 token). English-biased; non-ASCII workspaces will see chunks larger in tokens than expected.

**What to do:**
- Pull in `tiktoken-go` or a model-appropriate tokenizer (nomic doesn't publish a Go binding — may need to call out to Python or estimate).
- Make `MarkdownRecursive` size knobs token-counted, not byte-counted.
- Per-model tokenizer registry — same shape as the per-model context-window registry that Plan 5.x will need anyway.

**Trigger:** when an actual non-ASCII workspace (Japanese notes, code-heavy) shows pathological chunk sizes.

---

## 5. Parent-child / small-to-big retrieval

**Status:** the canonical v2 upgrade path from the chunking research. LangChain's `ParentDocumentRetriever`, LlamaIndex's `AutoMergingRetriever`. Embed small (~256-token) children for *recall*, return larger parent passages for *synthesis*.

**Why it's worth doing:** biggest published quality jump available without an LLM dependency. Schema-friendly with our existing `(node_id, chunk_idx)` table — just add a `parent_chunk_idx` column (nullable, additive migration).

**What to do:**
- Add `parent_chunk_idx INTEGER NULL` to `embeddings` table (migration).
- Add a `Hierarchical` chunker that emits both small-child chunks (embedded) and parent boundaries (stored as text, not embedded).
- `cmd_query.go` retrieval path: rank by children, return parent body slice as snippet.
- Probably worth a fresh spec — non-trivial design space.

**Don't try to bundle:** materially changes the retrieval shape; deserves its own brainstorm.

---

## 6. Surgical hash-skip re-embedding

**Status:** v1 uses delete-all-then-insert — every reindex re-embeds every chunk of every node. Cheap with local Ollama; potentially expensive once API providers (OpenAI, Voyage) land.

**What to do:**
- Before embedding a chunk, query its existing `(node_id, chunk_idx, content_hash)`; skip if hash matches and model matches.
- Replace `DeleteByNodeID` + `Upsert all` with `Upsert changed + DeleteRange WHERE chunk_idx >= newCount`.
- Adds state-machine complexity; only worth it when API costs make it pay.

**Trigger:** OpenAI / Voyage providers landing (Plan 5.x), or measurably-painful reindex times on big workspaces.

---

## 7. Per-model context-window registry

**Status:** v1 hardcodes the `MaxBytes` cap in `MarkdownRecursive`'s zero-value defaults. Fine while we're Ollama+nomic-only.

**What to do:**
- Internal `map[string]ModelCapability{maxTokens, recommendedChunk, tokensPerByte}` keyed by `provider/model`.
- Strategy reads from the registry at construction time.
- Couples cleanly to follow-up #4 (tokenizer registry per model).

**Trigger:** second provider lands.

---

## 8. Contextual retrieval (Anthropic Sep-2024)

**Status:** research called out as the strongest single recall delta (-49% retrieval failure). Requires a generation-model call per chunk to prepend ~50-100 tokens of LLM-generated context. Tusk doesn't have a generation-model dependency today.

**Why it's interesting:** if Tusk gains generation-model integration for any reason (e.g., automated tagging, summary generation), contextual retrieval becomes free incremental quality.

**Don't pull this in alone** — the LLM dependency is the cost, not the embedding side.

---

## Where this fits

None of these are urgent. Snippet generation (#1) is probably the most user-visible and the cheapest; doctor diagnostics (#2) help operators trust the new chunker. The rest are quality / scale optimizations to revisit as the workspace grows or providers expand.

When picking one up: start a fresh branch off `main`, link back to this doc and the original chunking spec for context. Each follow-up is independent — no ordering constraints between them.
