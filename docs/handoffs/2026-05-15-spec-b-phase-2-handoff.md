---
type: handoff
title: Spec B Phase 2 — batched Ollama embed requests
session-date: "2026-05-15"
---

# Spec B Phase 2 — batched Ollama embed requests

Picks up where [[docs/handoffs/2026-05-14-spec-b-parallel-batched-embed]] and PR #381 leave off. Phase 1 (parallel workers + configurable timeout) has shipped; Phase 2 layers the `BatchEmbedder` optional interface, an Ollama `prompts: []` implementation, the per-prompt fallback path, and the `embeddings.batch-size` manifest knob on top.

The authoritative reference for both phases is [[docs/superpowers/specs/2026-05-14-parallel-batched-embed-design]]. The implementation plan for Phase 2 is [[docs/superpowers/plans/2026-05-15-parallel-batched-embed]] tasks 6–10.

---

## What's already on main

[PR #381](https://github.com/germanamz/tusk/pull/381) (merged 2026-05-15, squash-merge at commit `66f2e53`) delivered:

- **`embed.DrainQueue` runs a per-node worker pool.** `DrainConfig.Workers int` field. Workers pull chunk jobs from a buffered channel, push results back to a buffered channel; the collector sorts by `chunkIdx` and runs `DeleteByNodeID + Upsert` only after every worker returns. Per-node `context.WithCancel` aborts in-flight Embed calls on first error. Per-node atomicity is now strict — either every chunk for the node is upserted, or none are.
- **`OllamaConfig.Timeout` is configurable.** Field defaults to the prior 30-second hardcoded ceiling when zero; production wiring sites pass a manifest value.
- **Two new optional `[embeddings]` manifest knobs:** `workers` (default 4) and `timeout-seconds` (default 120). Both validated `>= 0`; the loader treats zero/absent as "use the default."
- **User-facing defaults live in `internal/embed/embedder.go`:** `DefaultWorkers = 4`, `DefaultTimeoutSeconds = 120`, plus `ResolveWorkers(int) int` and `ResolveTimeoutSeconds(int) int` helpers applied at all three embedder construction sites (`cmd/tusk/cmd_reindex.go`, `cmd/tusk/cmd_query.go`, `internal/mcp/runtime.go`).
- The constant block's doc comment already forward-references `DefaultBatchSize`. Phase 2's task 9 fulfills that promise.
- The existing `MaxEmbedAttempts = 3` retry semantics are preserved end-to-end; `TestDrainQueue_GivesUpAfterMaxAttempts` and `TestDrainQueue_LogsWarnOnEmbedError` still pass.

The naming-collision avoidance the spec called out is in place: `DrainConfig.BatchSize` retains its existing meaning ("queue rows pulled per drain iteration"), and `DrainConfig.EmbedBatchSize` is the name reserved for Phase 2.

---

## Why Phase 2 matters more than the spec originally framed

The Phase 1 throughput measurement on the dogfood workspace (55 nodes, 987 chunks, warm CPU Ollama 0.23.3, `nomic-embed-text`) produced this honest result:

| Mode | Wall-clock |
|---|---|
| `workers = 1` (serial baseline) | **704s** |
| `workers = 4` (default) | **714s** |

No observable speedup. The Ollama runner held at ~9.4 cores throughout both runs — the model inference is CPU-saturating per call on this hardware, so concurrent workers contend rather than scale. A quick 4-call burst against Ollama with *small* prompts (60-byte test strings) returned in 150ms vs an estimated ~244ms serial, which confirms Ollama does parallelize when it has the headroom. With realistic chunk-sized prompts (1600–4000 bytes each, ~400–1000 tokens), that headroom vanishes.

This is exactly the risk the spec flagged in its "Risks" section under the `OLLAMA_NUM_PARALLEL` interaction heading. The measurement validates the risk.

**The implication for Phase 2:** the spec's "Phase 2 stacks ~1.3-2× on top of Phase 1" framing assumed Phase 1 would deliver its expected ~4-5×. On CPU Ollama at the default server config, Phase 1 delivered ~1×. That means Phase 2's batched path may be the only path to a meaningful throughput improvement on this hardware — not because batching is intrinsically faster, but because it sidesteps the CPU-bound-per-call ceiling by letting Ollama process N prompts inside a single request (Ollama can choose its own internal scheduling) and by removing N−1 HTTP round-trips.

Phase 2 is therefore worth more careful benchmarking attention than Phase 1 was. The plan's measurement step (task 10) should compare:

- Phase 1 baseline at `workers = 4, batch-size = 1` (the current main behavior).
- Phase 2 candidates at `batch-size ∈ {4, 8, 16}` to see where Ollama's per-batch latency sweet spot lands.

---

## What Phase 2 ships (plan tasks 6–10)

The authoritative task list is in [[docs/superpowers/plans/2026-05-15-parallel-batched-embed]]. Summary for orientation:

- **Task 6 — `BatchEmbedder` interface.** Adds the optional interface in `internal/embed/embedder.go`, plus a compile-time conformance assertion in `internal/embed/embedder_test.go`. **Commit is deferred to land together with Task 7** because the conformance assertion references `OllamaEmbedder.EmbedBatch`, which doesn't exist until Task 7. An implementer that runs Task 6 in isolation will see a compile failure and must move directly to Task 7 before attempting to commit.
- **Task 7 — `OllamaEmbedder.EmbedBatch`.** POSTs `{"model": ..., "prompts": [...]}` to `/api/embeddings`; decodes `{"embeddings": [[...]]}` in input order. Wire-format test, dimension-mismatch test, length-mismatch test, plus a CI-skipped integration test pinned to the minimum Ollama version that supports the field. Tasks 6 + 7 share one commit.
- **Task 8 — Batch path in `DrainQueue`.** Adds `DrainConfig.EmbedBatchSize int`. Per-node block type-asserts the embedder for `BatchEmbedder` and dispatches up to `batch-size` chunks per worker job. On `EmbedBatch` error, the worker falls back to per-prompt `Embed` calls for that batch only, on the same `nodeCtx`. Three new tests: batch path preferred when available, batch-failure-fallback-success, batch-failure-fallback-fail.
- **Task 9 — `embeddings.batch-size` manifest knob.** Field on `EmbeddingsSection`, loader validation (`>= 0`), `embed.DefaultBatchSize = 8` constant and `embed.ResolveBatchSize` helper, plumbing through `reindex.Config`, `mcp.Runtime`, `internal/mcp/drainer.go`, and `cmd/tusk/cmd_reindex.go`.
- **Task 10 — Race-detector pass + dogfood measurement + PR.** Same shape as Phase 1's task 5.

---

## Where to start

```bash
git checkout main
git pull
git checkout -b feat/batched-embed
```

The very first commit on this branch should bump [[tickets/spec-b-parallel-batched-embed]]: status `pending → active`, and add a Phase 1 shipped marker in the Phasing section (PR #381 link, commit `66f2e53`, the throughput numbers above). This change was prepared at the end of the previous session but intentionally deferred so it could land alongside Phase 2 work rather than as a one-off chore PR.

Then proceed with plan task 6.

If using subagent-driven execution (which worked well for Phase 1), expect the same cadence per task: one implementer dispatch + spec-compliance review + code-quality review + occasional fix-forward commits for review nits.

---

## Open considerations for the Phase 2 brainstorm-or-quick-review

Quick-review preferred — the spec already resolved the design-level open questions. These are tuning calls that deserve a glance before committing to plan-task implementation:

- **Default `batch-size` of 8.** The Phase 1 measurement evidence suggests Ollama on CPU is heavily inference-bound per call. Larger batches (16, 32) might amortize HTTP overhead more aggressively without proportionally increasing latency. The plan's task 10 has the freedom to verify empirically; if 16 looks meaningfully better in practice, update the default before merging Phase 2.
- **Minimum Ollama version for the `prompts: []` field.** The plan references "Ollama 0.1.30+" — that figure was educated-guess at spec-writing time. Confirm against Ollama's release notes; the local dev box runs 0.23.3 which is well past the cutoff. The integration test should pin the actual minimum.
- **Integration test gating.** Plan task 7's CI-skipped integration test pattern is `TUSK_OLLAMA_INTEGRATION=1`. This convention isn't used elsewhere in the repo yet; if it lands here it'll be the first. Worth checking whether there's an existing project convention for environment-gated tests, and if not, just adopt this one and document it inline.
- **`Runtime.Workers` doc comment missing.** Code review of PR #381 (item M1 on task 4's quality review) noted that `Runtime.Workers` in `internal/mcp/runtime.go` is the only undocumented new field — `reindex.Config.Workers` has the doc explaining forwarding + zero semantics, but the MCP runtime field doesn't. Fold a one-line comment into the same place Phase 2 adds `Runtime.EmbedBatchSize`.
- **Phase 1 doc-comment imprecisions.** The `defaultOllamaTimeout` comment in `internal/embed/ollama.go` says "Production callers pass a larger value from `manifest.Embeddings.TimeoutSeconds`" — true after Phase 1 wiring landed, but the wording was written in present tense before the wiring task ran. Leave it. The field comment "zero falls back to 30s" is slightly imprecise (the constructor uses `<= 0`); also leave it. These were called non-blocking by code review and remain non-blocking now.
- **Cleanup nits deferred from Phase 1.** The code reviewer for plan task 3 flagged two items that fit naturally with Phase 2's batch refactor: (a) the helper-extraction option — turning the per-node block into a `drainNode(ctx, config, queued) error` helper would flatten one level of nesting and make the batch path slot in as a sibling clearly; (b) the `embedJob`/`embedResult` local types should likely be promoted to function or package scope once Phase 2 needs to share their shape. Both are judgment calls; the plan task 8 implementer can make the call at the time.

---

## Pointers

- Spec: [[docs/superpowers/specs/2026-05-14-parallel-batched-embed-design]]
- Plan: [[docs/superpowers/plans/2026-05-15-parallel-batched-embed]] (tasks 6–10)
- Ticket: [[tickets/spec-b-parallel-batched-embed]]
- Phase 1 PR: https://github.com/germanamz/tusk/pull/381 (merged 2026-05-15, commit `66f2e53`)
- Predecessor handoff (kickoff for Phases 1 + 2): [[docs/handoffs/2026-05-14-spec-b-parallel-batched-embed]]
- Original chunking handoff (full #1–#10 context, where #9 + #10 are the two items Spec B addresses): [[docs/handoffs/2026-05-13-embed-chunking-followups]]
- Sequential drain code that Phase 1 replaced (for historical reference; current code is the per-node worker pool): `internal/embed/drain.go` at `f53b29a` and earlier.
- Embedder interface to extend in task 6: `internal/embed/embedder.go`.
- Ollama wire client to extend in task 7: `internal/embed/ollama.go`.
