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
- `Client` — HTTP wrapper around Ollama's `/api/embeddings`.
- `CosineSearch` — in-memory ranking over the `embeddings` table.

## Notes

Sub-units embed per-leaf (the AST already chose the semantic boundary); file-level rows (when sub-units are disabled) chunk whole-document via `MarkdownRecursive`. Vectors are **content-addressed**: the `embeddings` table holds one row per `(content_hash, model)`, and the `node_embeddings` junction maps each node-chunk to its shared vector. So identical content embeds once and is shared across nodes, a sub-unit whose address shifts on a restructure reuses its vector with no model call, and vectors no mapping references are GC'd when the embed queue drains. Single embedding provider (Ollama); API-provider fallbacks (OpenAI, Voyage, Anthropic) are §10.5 in the master spec but unbuilt.
