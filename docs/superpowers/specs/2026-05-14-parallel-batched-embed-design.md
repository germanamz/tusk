---
type: spec
title: Parallel Embed Workers + Batched Ollama Requests — Design
---

# Parallel Embed Workers + Batched Ollama Requests

> **Status: superseded by host-Metal Ollama (2026-05-15).** Phase 1 (parallel workers + timeout knob) shipped in PR #381 and is still useful as a manifest-tunable safety knob. Phase 2 (batched `/api/embed`) was obsoleted before implementation: the host-Ollama Metal probe ([[docs/probes/2026-05-15-host-ollama-metal]]) brought per-call latency to ~71 ms, dropping the full reindex from 704 s to 41 s. Batching had only a 22% per-item win at that point — below the 30% threshold this spec set as the bar to justify the wire-format rework. See [[tickets/spec-b-parallel-batched-embed]] for the closeout summary.
>
> The text below is preserved as the original design context.

## Why

Embedding throughput on CPU Ollama is unusably slow for medium workspaces, and the hardcoded 30-second HTTP timeout in `internal/embed/ollama.go:32` silently caps how long a single embedding call can take — right at the latency a warm CPU `nomic-embed-text` call actually needs. PR #380's smoke test ran into this directly: reindexing this workspace's 51 nodes / 921 chunks took ~670 seconds, and small workspaces routinely hit `embed gave up` retries before completing.

Three composable changes close the gap:

1. **Parallel workers (#9)** — bound a worker pool inside `embed.DrainQueue` so multiple embedder calls run concurrently for the same node. Expected wall-clock improvement on the dogfood workspace: ~4-5× at `Workers=4`.
2. **Batched Ollama requests (#10)** — Ollama's `/api/embeddings` accepts a `prompts: []string` field. Sending N chunks per HTTP call removes per-call TLS+HTTP overhead. Stacks on top of workers for additional ~1.3-2× depending on workspace shape.
3. **Configurable embedder timeout** — the 30-second hardcoded ceiling becomes a manifest knob with a 120-second default, pulled in by the same "CPU Ollama is slow" pain.

This spec covers all three as one logical change shipped in two PRs (workers + timeout, then batch). Items #3–#8 from the [[docs/handoffs/2026-05-13-embed-chunking-followups|chunking handoff]] remain trigger-gated follow-ups outside Spec B's scope.

## Goals

- Cut wall-clock embedding time on warm CPU Ollama by a measured 4× minimum on the dogfood workspace.
- Preserve per-node atomicity, `MaxEmbedAttempts = 3` retry semantics, and bit-identical output for callers that opt into the single-worker baseline.
- Make embedder concurrency, batch size, and timeout configurable per workspace via the manifest, with conservative defaults that don't surprise existing users.
- Keep the embedder interface pluggable: future OpenAI/Voyage providers can opt into batching without touching the drain loop.

## Non-goals

- Cross-node parallelism. The dogfood workspace's chunk distribution (median 8, max 109) shows the top 5 nodes saturate an 8-worker pool on their own. Per-node parallelism is the right starting shape; cross-node coordination is meaningfully harder for marginal gain.
- New retry policies. `MaxEmbedAttempts = 3` and per-node give-up semantics stay exactly as they are.
- A general benchmark suite. Throughput is measured manually via the `tusk doctor` stats block on a real workspace, captured in the PR description before and after.
- Provider abstraction beyond a minimal `BatchEmbedder` interface. OpenAI/Voyage support lands when the original follow-up #6 triggers.

## Architecture overview

Three locations in the `internal/embed` package change. The queue model, the embedder's place in the system, and the retry control flow are unchanged.

The first change is in `internal/embed/drain.go`. The per-node chunk loop becomes a per-node worker pool. A fresh pool is created and torn down for each queued node; workers consume chunk jobs from a buffered channel and push results to a buffered results channel. After collecting all results, the main goroutine performs `DeleteByNodeID` followed by ordered `Upsert` calls. Errors cancel pending jobs via a per-node `context.WithCancel` and trigger the existing re-enqueue / give-up path.

The second change is in `internal/embed/embedder.go`. A new optional `BatchEmbedder` interface extends `Embedder` with a single `EmbedBatch` method. The drain loop type-asserts; when present, workers run the batch path, otherwise they run the single-prompt path unchanged. Stub embedders in tests can implement either interface; future providers plug in their own `BatchEmbedder` without touching the drain code.

The third change is in `internal/embed/ollama.go`. `OllamaEmbedder` gains a configurable HTTP client timeout via a new `OllamaConfig.Timeout` field, plumbed from the manifest. In Phase 2 it also implements `BatchEmbedder` by POSTing `prompts: []string` to `/api/embeddings` and decoding `embeddings: [][]float64` in input order.

The configuration surface (`manifest.EmbeddingsSection`) grows by three optional fields: `workers`, `batch-size`, and `timeout-seconds`. All defaults preserve current behavior when omitted.

## Phase 1 — workers and timeout knob

### Per-node worker pool

The worker pool is created inside the existing per-node block in `DrainQueue`, immediately after the chunker emits `bodyChunks` and after `DeleteByNodeID` has cleared prior embeddings for this attempt. Worker count is clamped to `min(config.Workers, len(bodyChunks))` — a 109-chunk node with `Workers=4` runs four goroutines; a two-chunk node runs two. The job channel and the results channel are both buffered to `len(bodyChunks)`, so no producer can block once dispatch starts.

A per-node `context.WithCancel` is derived from the drain context. Workers receive this `nodeCtx` and pass it to `Embedder.Embed`. The producer iterates `bodyChunks` in order, assembles each payload from `header + bodyChunk` (the existing logic), and sends one `embedJob{chunkIdx, payload}` per chunk onto the job channel. Once all jobs are sent, the producer closes the channel; each worker exits cleanly when the channel drains.

The collector reads from the results channel until it has received `len(bodyChunks)` results or the results channel is closed. A `sync.WaitGroup`-driven closer goroutine closes the results channel once all workers return, so the collector loop terminates deterministically. The first result that carries an error triggers a single call to `cancel()`; the collector continues draining remaining results (workers may emit one more before they observe the cancel) but discards them. The first error is preserved for the post-collect error-handling block.

On error: the collector calls the same re-enqueue path the current sequential code uses — increment attempts, call `Queue.ReEnqueue` if below the cap, or emit the `embed gave up` log line if not. Nothing is upserted. The node moves on, and the drain proceeds to the next queued row in the batch.

On success: the collector sorts results by chunkIdx (channel ordering is non-deterministic), then performs `len(bodyChunks)` sequential `Upsert` calls. SQLite WAL's writer mutex serializes these naturally; per-node atomicity is preserved because `DeleteByNodeID` already ran before any worker started.

### Cancellation and goroutine lifetime

The per-node `defer cancel()` guarantees that even if the drain returns mid-node (panic, parent context cancel), in-flight HTTP requests don't outlive the function call. Workers exit the moment they observe `nodeCtx.Done()` after their current call returns; OllamaEmbedder's HTTP client, because it uses `http.NewRequestWithContext(nodeCtx, ...)`, abandons in-flight requests as soon as the context fires.

### Configurable timeout

`OllamaConfig` gains a `Timeout time.Duration` field. The constructor uses it directly if non-zero, otherwise falls back to the existing 30-second constant — this preserves behavior for any test or code path that constructs `OllamaConfig` without setting the field. The three production wiring sites — `cmd/tusk/cmd_reindex.go`, `cmd/tusk/cmd_query.go`, and `internal/mcp/runtime.go` — all read `manifest.Embeddings.TimeoutSeconds` and pass it as `time.Duration(seconds) * time.Second` into the constructor.

### Manifest knobs added in Phase 1

The `[embeddings]` TOML section accepts two new optional fields:

| Field | Type | Default | Meaning |
|---|---|---|---|
| `workers` | int | 4 | Concurrent embed calls per node. Must be ≥ 1. Set to 1 to preserve strictly-serial behavior. |
| `timeout-seconds` | int | 120 | HTTP client timeout for the Ollama embedder. Must be > 0. Replaces the previous hardcoded 30s. |

The manifest loader rejects zero or negative values with a clear error rather than silently coercing. Absent fields yield the defaults; existing manifests don't need to be touched.

`DrainConfig` adds a `Workers int` field. Zero is treated as 1 (single-threaded) so existing tests don't need to set it explicitly.

## Phase 2 — batched Ollama requests

### BatchEmbedder interface

A new `BatchEmbedder` interface in `internal/embed/embedder.go` extends `Embedder` with `EmbedBatch(ctx context.Context, payloads [][]byte) ([][]float32, error)`. The method contract is that the returned slice has exactly `len(payloads)` elements in the same order, or an error applies to the entire batch. Partial successes are not expressible — a batch call either landed cleanly or it didn't. This keeps the drain loop's branching shallow.

```go
type BatchEmbedder interface {
    Embedder
    EmbedBatch(ctx context.Context, payloads [][]byte) ([][]float32, error)
}
```

The drain loop performs a single type assertion at the top of each per-node block. When the embedder implements `BatchEmbedder` and the configured batch size is greater than 1, workers run the batch path. Otherwise the Phase 1 single-prompt path is used unchanged.

### Worker shape under batching

The chunk job channel changes from "one chunk per send" to "up to N chunks per send" — the producer groups consecutive chunks into batch jobs before dispatching. A 109-chunk node with `Workers=4, EmbedBatchSize=8` runs as 14 batch jobs distributed across 4 workers, each worker handling 3-4 batches sequentially. Total Ollama HTTP calls: 14 instead of 109. Throughput improvement comes from removed per-call HTTP overhead, not from added parallelism (parallelism is still capped at `Workers`).

Each worker pulls one batch job at a time and calls `EmbedBatch` once. On success, it emits N result records on the results channel, each tagged with its original chunkIdx. The downstream collection-and-upsert loop is unchanged — it still collects `len(bodyChunks)` total results, sorts by chunkIdx, and does the atomic delete-and-upsert pair.

### Per-prompt fallback within an attempt

When a worker's `EmbedBatch` call returns an error, the worker does not immediately signal node-level failure. It falls back by calling `Embed` for each payload in the failed batch sequentially, on the same `nodeCtx`. If all per-prompt calls succeed, the worker emits N successful results and the node attempt continues normally. If any per-prompt call fails, the worker emits an error result, which triggers the same node-level cancel-and-re-enqueue path as Phase 1.

This makes the batch path opportunistic: when Ollama is healthy, HTTP round-trips are saved; when a single batch call hits a transient failure — mid-batch model OOM, a single prompt-too-long for the batch but not for a single prompt, network blip — recovery happens within the same attempt rather than burning one of three `MaxEmbedAttempts`. Worst case is roughly 2× the network calls for that one batch.

### Ollama wire format

`OllamaEmbedder.EmbedBatch` POSTs to `/api/embeddings` with a `prompts: []string` body field instead of `prompt: string`. The response carries `embeddings: [][]float64` in input order. The implementation validates that the response array length equals `len(payloads)` and that each row's dimension matches `config.Dim`. Any mismatch returns a single batch-level error, which triggers the per-prompt fallback described above.

The minimum Ollama version supporting the `prompts` field is documented inline in `ollama.go` and in the manifest pack notes. The integration test (skipped in CI by default) pins to that version.

### Manifest knob added in Phase 2

The `[embeddings]` TOML section accepts one more optional field:

| Field | Type | Default | Meaning |
|---|---|---|---|
| `batch-size` | int | 8 | Chunks per `EmbedBatch` HTTP call. Must be ≥ 1. Set to 1 to disable the batch path even when the embedder implements `BatchEmbedder`. |

`DrainConfig` adds an `EmbedBatchSize int` field. Zero or 1 means the drain skips the type assertion and stays on the single-prompt path.

## Error handling and retry semantics

The existing retry control flow is preserved end-to-end:

- A node's first chunk error during an attempt cancels remaining workers, discards collected results, increments `attempts`, and re-enqueues if below `MaxEmbedAttempts`.
- At `attempts >= MaxEmbedAttempts`, the queue row is dropped and a `Warn embed gave up` line is emitted, exactly as today.
- Fresh reindex runs re-enqueue every indexed node with `attempts=0` — the cap is per-drain, not per-node-lifetime.
- Per-node atomicity holds: either all chunks for a node are upserted (success) or none are (error). This is an incidental improvement over the current code, which can leave partial chunks persisted if a give-up happens mid-chunk; the new collect-then-upsert ordering closes that gap.
- Batch failures inside Phase 2 do not consume an attempt unless the per-prompt fallback also fails. From the queue's perspective, batched and non-batched paths look identical.

## Configuration plumbing

The three sites that construct embedders today — `cmd/tusk/cmd_reindex.go`, `cmd/tusk/cmd_query.go`, and `internal/mcp/runtime.go` — all read from `manifest.Embeddings`. Phase 1 extends each to pass `Workers` into the `DrainConfig` they construct and `Timeout` into the `OllamaConfig`. Phase 2 extends them to pass `EmbedBatchSize` into the `DrainConfig`.

The MCP runtime path is the trickiest because its `Embedder` is shared between drain and query. Since `EmbedBatch` only matters at drain time and the type assertion happens inside `DrainQueue`, the query path is unaffected — `tusk_query` continues to call `Embed` per query string.

`DrainConfig.BatchSize` keeps its current name and meaning ("queue rows pulled per drain iteration"). A doc-comment clarifies it. The new field for #10 is `EmbedBatchSize`, named for its purpose. No rename, no churn in existing tests.

The convention across new fields: zero means "preserve prior behavior." `DrainConfig.Workers == 0` → 1. `DrainConfig.EmbedBatchSize == 0` → 1. `OllamaConfig.Timeout == 0` → 30s. This keeps existing `drain_test.go` and `ollama_test.go` cases green without sweeping changes to test setup.

## Testing approach

### Phase 1 tests

Concurrency is verified with a stub embedder that sleeps a fixed duration per call (around 50ms) and a node carrying 20 chunks. The test asserts that wall-clock with `Workers=4` is measurably under one-half of `Workers=1` (soft bound; the theoretical ratio is 1/4, but CI scheduling variance can compress the gap).

Atomicity is verified with a stub that errors on chunk index 3 of a 5-chunk node. The test asserts zero embedding rows exist for the node after the drain returns, that the node was re-enqueued with `attempts=1`, and (in a second test) that three attempts hit the `embed gave up` log and drop the queue row.

Behavioral parity is verified by re-running the existing single-worker drain scenarios with `Workers=1` explicitly set, asserting bit-identical embedding rows compared to the pre-change baseline (model, content hash, vector, body all match). This guards against accidental behavior drift for callers that don't opt in.

A race-detector pass via `make test-race` runs the whole `internal/embed` package. The handoff calls this out explicitly and it's a non-negotiable green light for Phase 1.

The timeout knob gets a focused Ollama test using the existing `httptest.Server` infrastructure in `ollama_test.go`: the server sleeps longer than the configured timeout, the test asserts that `OllamaConfig.Timeout` actually drives client behavior. Pairs with a manifest loader test that round-trips the field.

### Phase 2 tests

A new `BatchEmbedder` stub asserts that drain calls `EmbedBatch` instead of `Embed` when present, and that vectors come out in input order tagged to original chunkIdx values. A mixed-success stub returns a batch-level error on the first call and individual successes on subsequent `Embed` calls; the test asserts the per-prompt fallback path runs and the node attempt completes without incrementing attempts. A failing-fallback variant — batch errors AND a per-prompt call errors — asserts the node is re-enqueued with `attempts=1`.

The Ollama batch wire format is covered by an `httptest.Server`-driven test that asserts the outgoing JSON carries `prompts: []` (not `prompt:`) and that the response shape is decoded in order and converted to `[]float32` per element. A dimension-mismatch case asserts the whole batch errors out and triggers fallback.

An integration test against a real local Ollama instance is added but marked `t.Skip` unless `TUSK_OLLAMA_INTEGRATION=1` is set, so CI doesn't depend on Ollama being available. The integration test documents the minimum Ollama version supporting `prompts: []`.

### Throughput-on-real-workspace measurement

Before merging each PR, run the `tusk doctor` stats block on the dogfood workspace, capture sequential wall-clock, then reindex with the new defaults and capture again. The expected improvement — roughly 4-5× for Phase 1 and an additional 1.3-2× for Phase 2 stacking on top — is recorded in the PR description as a smoke-level validation. No automated benchmark is committed; measuring on a real workspace at PR time is more meaningful than a microbenchmark whose results drift with hardware and CI runners.

## Open questions

None remaining at design time. All five open questions from the handoff are resolved:

1. **Timeout knob:** included in Phase 1 as `embeddings.timeout-seconds` (default 120s).
2. **Per-node vs cross-node parallelism:** per-node, justified by the handoff's chunk distribution data.
3. **Worker count default:** 4. Conservative; matches Ollama's typical `OLLAMA_NUM_PARALLEL`.
4. **Batch size default:** 8. The handoff's pick; amortizes HTTP overhead without making each failure expensive.
5. **Provider abstraction:** `BatchEmbedder` as a minimal optional interface, type-asserted at the drain layer.

## Risks

- **Ollama `prompts: []` minimum version.** If the dogfood developer's local Ollama is older than the supporting version, Phase 2's batch path will silently fall back to per-prompt every call. Mitigation: the version is documented and the integration test pins it.
- **`OLLAMA_NUM_PARALLEL` interaction.** Setting `embeddings.workers` higher than Ollama's server-side parallelism just queues internally on the Ollama side. The worker default of 4 errs conservative to avoid this. Mitigation: documented in the pack notes; users with GPU or larger parallelism budgets are expected to tune.
- **Memory cost of atomic collect.** Holding all vectors for a 109-chunk node in memory before upsert costs roughly 109 × 768 floats × 4 bytes = 335 KB. Tolerable for the workspace shapes we ship to.
- **SQLite write contention.** Multiple goroutines no longer call `Upsert` — only the collector does, sequentially. Write contention does not increase from today.

## References

- [[docs/handoffs/2026-05-14-spec-b-parallel-batched-embed]] — the handoff that scoped this work.
- [[docs/handoffs/2026-05-13-embed-chunking-followups]] — the predecessor with full context on items #9 and #10.
- [[tickets/spec-b-parallel-batched-embed]] — the ticket node anchoring this spec.
- [[docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design]] — the parent v1 design.
