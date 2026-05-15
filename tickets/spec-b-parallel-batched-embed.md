---
type: ticket
title: Spec B — parallel embed workers + batched Ollama requests
status: completed
priority: high
---

## Resolution — closed as obsoleted (2026-05-15)

Phase 1 shipped (PR #381, `workers` knob + 30s→120s timeout default). Phase 2 (batched `/api/embed`) is **obsoleted by Metal-accelerated host Ollama**, not by additional code.

Measurement (Apple M1 Max, Ollama 0.23.4, `nomic-embed-text`, full reindex of this workspace):

| | CPU (in-container) | Metal (host) | Speedup |
|---|---|---|---|
| Per-call embed latency | ~1.5–2 s | **71 ms** | ~25× |
| Full reindex wall-clock | **704 s** | **41 s** | **17×** |

Embedding is now ~6% of reindex wall-clock; the rest is filesystem walk + DB writes. `/api/embed` batch-of-8 was measured at only 22% faster per item than singular calls — below the 30% threshold the spec set to justify the wire-format work, and irrelevant given the absolute numbers. See [[docs/probes/2026-05-15-host-ollama-metal]] for the full probe log and the decision matrix that closed this out. The supporting handoffs are [[docs/handoffs/2026-05-15-spec-b-phase-2-paused]] (the in-container pause that triggered the probe) and [[docs/handoffs/2026-05-15-spec-b-host-ollama-probe]] (the host-session briefing).

Operational note: keep host Ollama on `127.0.0.1:11434`. Exposing it to the devcontainer would also expose it to the LAN on this network shape; the devcontainer can fall back to its in-container CPU Ollama or run a `socat`-on-demand bridge when the user opts in.

## Outcome (original)

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

- **Phase 1 — #9 parallel workers (+ timeout knob). Shipped 2026-05-15 in PR #381 (squash-merge `66f2e53`).** Throughput measurement on the dogfood workspace (55 nodes, 921 chunks, warm CPU Ollama, `nomic-embed-text`): `workers=1` → 704s; `workers=4` (new default) → 714s. No observable speedup — the Ollama runner held ~9.4 cores on every call, so concurrent workers contend rather than scale on CPU. Validates the `OLLAMA_NUM_PARALLEL` interaction risk the spec flagged. Implication: Phase 2 batching is the load-bearing optimization on this hardware, not an incremental win on top of Phase 1's ~4-5×. See plan tasks 1–5 and [[docs/handoffs/2026-05-15-spec-b-phase-2-handoff]] for full context.
- **Phase 2 — #10 batched Ollama requests.** Composes on top of Phase 1. Adds `BatchEmbedder` optional interface; drain prefers batch when available. Ships as its own PR. See plan tasks 6–10.
