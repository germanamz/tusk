---
type: package
title: internal/embed — semantic indexing
import-path: github.com/germanamz/tusk/internal/embed
status: stable
---

# internal/embed

Semantic-retrieval pipeline. Holds the embed queue (per-node TODO rows in SQLite), drains the queue against an Ollama HTTP client, computes pure-Go cosine similarity at query time, and returns ranked node IDs.

## Public surface

- `Drainer` — long-running goroutine; exposed by `mcp` and `watch` runtimes.
- `DrainQueue` — one-shot drain entry point shared between reindex and the live drainer.
- `OllamaEmbedder` — HTTP wrapper around Ollama's `/api/embed` (Ollama 0.3.4 or newer). It sends `truncate: false`, so a text longer than the model's context is refused rather than clipped silently; it then resends that text with `truncate: true`, embeds the part that fits, and logs a warning.
- `CosineSearch` — in-memory ranking over the `embeddings` table.
- `Embedder` — `Embed` sends text verbatim; `Model` is the display name, `VectorKey` the storage identity, `Format` the text shaping.
- `Format` — `Query`, `Document(title, payload)`, and `Header(node)` apply the `[embeddings]` prefixes and header mode. `EmbedQuery` is the query-side entry point that applies the query prefix.
- `VectorKey`, `VectorKeyFor`, `DisplayModel` — the model plus `#num-ctx=N` when set, and back.
- `DocumentFingerprint` — hash of every document-side `[embeddings]` setting; reindex compares it to detect a settings change. Bare leaves (`sub-unit-context = "none"`, no properties) add nothing to it, so those settings fingerprint as they did before the two keys existed.
- `FormatFor(section)` — the `Format` (prefixes, header mode, sub-unit context and properties) a workspace's `[embeddings]` section produces; `NewFromManifest` builds the embedder with it.

## Notes

Sub-units embed per-leaf (the AST already chose the semantic boundary); file-level rows (when sub-units are disabled) chunk whole-document via `MarkdownRecursive`. A leaf embeds behind its context (`subunit.LeafContext`, #862): by default its file's title and the enclosing headings on a line of their own (`ledger-sync > Async`), under `sub-unit-context = "title"` the title alone, plus any `sub-unit-properties`. The drain walks the leaf's `parent_id` chain to the file row to read them (`readLeafContext`); a parent it can't read fails the leaf through the retry policy rather than embedding it without context. With `{title}` in `document-prefix` the label fills the placeholder and takes no line. A passage that answers a question about a page often never names the page, so without the title its vector misses a query that does. A leaf longer than `chunk-max-bytes` (a big code block or table) can't fit one vector, so it is split by `MarkdownRecursive` too, and its best chunk ranks it. Vectors are **content-addressed**: the `embeddings` table holds one row per `(content_hash, model)`, and the `node_embeddings` junction maps each node-chunk to its shared vector. So identical content embeds once and is shared across nodes, a sub-unit whose address shifts on a restructure reuses its vector with no model call, and vectors no mapping references are GC'd when the embed queue drains. Single embedding provider (Ollama); API-provider fallbacks (OpenAI, Voyage, Anthropic) are §10.5 in the master spec but unbuilt.

**What identifies a vector.** The drain builds each chunk's text as document prefix (with `{title}` filled in) + header + chunk and stores the sha256 of exactly those bytes as the content hash. Any setting that changes the text (`document-prefix`, a title edit under `{title}`, `document-header`, chunk sizes, `sub-unit-context`, `sub-unit-properties`) therefore changes the hash, and an old vector can't be skip-acked or reused. So does a title rename for every leaf under it; the sub-unit sync folds the same context into the leaf's `content_hash`, which is what re-enqueues those leaves. Settings that change the output for the same text (`model`, `num-ctx`) go into the vector key stored in the `model` column, which the reuse check and the query filter both compare. With every new key at its default the file chunks' text, hash, and key are byte-identical to what tusk stored before these settings existed. Leaves are not: the default `sub-unit-context = "path"` re-embeds every leaf once on the upgrade that brings it, and `"none"` keeps them as they were. The stored `body` is always the bare chunk, so snippets, passage `text` and the word match never see the context.

**When an embed fails.** The drain embeds a one-word probe after any failed call. If the probe embeds, the backend is serving and the input is at fault: the row is nacked with its error and the drain goes on to the next row. After `MaxEmbedAttempts` (3) such failures the row is parked (`EmbedQueueRepo.Park`): the drain stops claiming it, it no longer counts as pending, and doctor reports it as `embed-failed`. Re-enqueueing the node re-arms it, which happens when the node changes, on `reindex --force`, and on an `[embeddings]` change. If the probe fails too, the backend can't serve anything (Ollama down, overloaded, model missing), so the drain stops with a `TransportError` and charges no node an attempt; the rows stay leased and are retried after the lease expires. Asking the backend, rather than reading status codes or error text, also catches an input that crashes the model runner with a 5xx every time. A pass cancelled mid-embed hands its rows back uncharged and skips the probe (#845).

**Draining under the right settings.** `DrainConfig.Meta` and `Fingerprint` carry the `DocumentFingerprint` the drainer's embedder and chunker were built from. Before each node (ahead of the unchanged-skip and reuse paths, whose old-format hashes would otherwise match stale vectors and ack the row) and again before writing vectors, the drain compares it with the fingerprint reindex recorded. On a mismatch it releases its claimed rows (`EmbedQueueRepo.Release`, no attempt spent) and stops with a warning, leaving the queue to a drainer on the current settings.
