---
type: ticket
title: Spec B — parallel embed workers + batched Ollama requests
status: pending
priority: high
---

## Outcome

Embed throughput on CPU Ollama is unusably slow for medium workspaces: ~30s per chunk, sequential, with a hardcoded 30s timeout that the embedder itself routinely brushes against. Spec B fixes this with two composable changes — parallel worker pool inside `embed.DrainQueue` (#9), and batched Ollama `/api/embeddings` requests (#10) — landing as two PRs.

Expected impact, grounded by the doctor stats block on this workspace (51 nodes, 921 chunks, median 8 chunks/node, top node 109 chunks): sequential wall-clock ~670s → ~150s with `Workers=8` per-node parallelism (~4.5×). Batching layers an additional smaller win by removing per-call HTTP overhead.

## Pointers

- Design spec: [[docs/superpowers/specs/2026-05-14-parallel-batched-embed-design]]
- Implementation plan: [[docs/superpowers/plans/2026-05-15-parallel-batched-embed]]
- Handoff: [[docs/handoffs/2026-05-14-spec-b-parallel-batched-embed]]
- Predecessor handoff (full context for #1–#10): [[docs/handoffs/2026-05-13-embed-chunking-followups]]
- Predecessor PR (snippet + doctor): #380
- Sequential drain code to replace: `internal/embed/drain.go`
- Embedder interface to extend: `internal/embed/embedder.go`, `internal/embed/ollama.go`
- Retry semantics that must be preserved: PR #369 (`MaxEmbedAttempts = 3`, per-node atomicity)

## Out of scope

- Items #3–#8 from the chunking handoff (snippet ranking polish, parent/child retrieval, alternative providers, etc.) — these remain trigger-gated follow-ups; not bundled here.
- Provider abstraction beyond a `BatchEmbedder` interface. OpenAI/Voyage batch endpoints land when #6 triggers.
- Re-tuning `MaxBytes` or chunker strategy. PR #377 already tightened it; the 30 large-chunk flags surfaced in PR #380 are a signal for #5 (parent/child retrieval), not for Spec B.

## Phasing

- **Phase 1 — #9 parallel workers (+ timeout knob).** Biggest wall-clock win, narrowest blast radius. Ships as its own PR. See plan tasks 1–5.
- **Phase 2 — #10 batched Ollama requests.** Composes on top of Phase 1. Adds `BatchEmbedder` optional interface; drain prefers batch when available. Ships as its own PR. See plan tasks 6–10.
