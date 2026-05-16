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

Whole-document chunks (not per-paragraph) — keeps the embedding model context within bounds and avoids chunk-stitching at retrieval time. Single embedding provider (Ollama). API-provider fallbacks (OpenAI, Voyage, Anthropic) are §10.5 in the master spec but unbuilt — candidate scope for Plan 8.
