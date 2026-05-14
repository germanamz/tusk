---
type: handoff
title: Spec B — parallel + batched embed (#9 + #10)
session-date: "2026-05-14"
---

# Spec B — parallel embed workers and batched Ollama requests

Picks up where PR #380 (snippet + doctor) leaves off. This is the
second tranche of follow-ups from
`docs/handoffs/2026-05-13-embed-chunking-followups.md` — specifically
**#9 (parallel embed workers in the drain loop)** and **#10 (batched
Ollama embedding requests)**, which compose naturally and should be
brainstormed together.

Each remains independent of all other follow-ups in the original
handoff (#1, #2 — done; #3, #4, #5, #6, #7, #8 — still trigger-gated).

---

## Why this matters now

PR #380's smoke test exposed the bottleneck in concrete terms: on
CPU-only Ollama, a single `nomic-embed-text` embedding call takes
~30 seconds — right at the hardcoded `OllamaEmbedder` timeout
(`internal/embed/ollama.go:32`, `http.Client{Timeout: 30 * time.Second}`).
A workspace with 50 nodes × ~3 chunks each at 30s sequential calls is
~75 minutes of wall-clock wait, and the timeout means even small
workspaces hit `embed gave up` retries before completing.

Two compounding fixes:

1. **#9 Parallel workers** — bound a worker pool inside
   `embed.DrainQueue` so multiple embedder calls run concurrently.
   On CPU Ollama, expected speedup is ~4-8x; on GPU, much more.
2. **#10 Batched requests** — Ollama's `/api/embeddings` accepts a
   `prompts: []string` field. Sending N chunks per HTTP call removes
   ~50-100ms of TLS+HTTP overhead per chunk.

The two compose: N workers × M-chunk batches.

The 30s timeout itself is also a real issue. The PR-#380 smoke had
to fall back to a stub server because CPU inference latency exceeded
the hardcoded ceiling. Whether to fix the timeout as part of Spec B
or as a separate pre-cursor commit is a design call (see "Open
questions" below).

---

## Current state (as of PR #380)

### Sequential drain loop

`internal/embed/drain.go` — the chunk-write loop:

```go
for chunkIdx, bodyChunk := range bodyChunks {
    payload := append(header, bodyChunk...)
    vector, embedErr := config.Embedder.Embed(ctx, payload)   // <-- serial
    if embedErr != nil { /* retry/give-up logic */ break }
    config.Embeddings.Upsert(index.EmbeddingRow{
        NodeID: queued.NodeID,
        ChunkIdx: chunkIdx,
        ...
        Body: string(bodyChunk),   // added in PR #380
    })
}
```

The outer loop iterates queue rows; the inner loop embeds chunks
strictly sequentially. The retry semantics (PR #369, `MaxEmbedAttempts
= 3`) are node-level — on the first chunk failure for a node, the
loop breaks, the node is re-enqueued (or given up after 3 attempts),
and no chunks are upserted.

### Embedder interface

`internal/embed/embedder.go` — single method:

```go
type Embedder interface {
    Embed(ctx context.Context, payload []byte) ([]float32, error)
    Model() string
    Dim() int
}
```

`OllamaEmbedder` (`internal/embed/ollama.go`) POSTs one prompt per
request, hardcoded 30s client timeout.

### Embedding row write semantics

`EmbeddingRepo.Upsert` keys on `(node_id, chunk_idx)`. SQLite WAL
mode allows concurrent reads but serializes writes — so multiple
goroutines calling `Upsert` against the same `*sql.DB` will block on
the write mutex, but won't corrupt anything. The PR-#369 retry-cap
flow still expects all-or-nothing per node: either all chunks for a
node succeed and are upserted, or none are. **Per-node
delete-then-upsert is the source of truth for "this node has been
re-embedded."**

---

## Design hints (not a spec)

### #9 Parallel workers

**Suggested shape:**

```go
type embedJob struct {
    chunkIdx  int
    bodyChunk []byte
    payload   []byte
}

type embedResult struct {
    chunkIdx int
    vector   []float32
    err      error
}

// Inside DrainQueue, per node:
jobs := make(chan embedJob, len(bodyChunks))
results := make(chan embedResult, len(bodyChunks))

for w := 0; w < config.Workers; w++ {
    go func() {
        for job := range jobs {
            vec, err := config.Embedder.Embed(ctx, job.payload)
            results <- embedResult{chunkIdx: job.chunkIdx, vector: vec, err: err}
        }
    }()
}

for chunkIdx, bodyChunk := range bodyChunks {
    payload := assemble(header, bodyChunk)
    jobs <- embedJob{chunkIdx, bodyChunk, payload}
}
close(jobs)

// Collect — abort the rest of this node on first error.
collected := make([]embedResult, 0, len(bodyChunks))
for range bodyChunks {
    r := <-results
    if r.err != nil {
        // cancel pending: drain remaining results, do not upsert.
        ...
    }
    collected = append(collected, r)
}

// All successful → DeleteByNodeID + Upsert collected in chunk_idx order.
```

**Key decisions:**

- `Workers` is a `DrainConfig` knob; default ~4-8. Configurable per
  workspace via `manifest.Embeddings.Workers` (new field) or a
  `tusk reindex --workers N` flag, or both.
- Workers are per-node (re-created each node) **or** shared across
  the whole drain pass (a single pool drains many nodes
  concurrently). Per-node is simpler and matches the existing
  retry/upsert atomicity. Shared workers are more efficient but
  require coordinating multi-node atomicity — meaningfully harder.
- On per-node error: cancel pending jobs with `context.WithCancel`,
  drain results, re-enqueue, do not upsert.
- The upsert remains serial inside the per-node block. SQLite write
  contention is not a per-node concern at this granularity.

**Tests required:**

- Drain N=20 nodes, assert wall-clock with `Workers=4` < ~1/3 of
  `Workers=1` (using a stub embedder that sleeps 50ms per call).
- Drain a node where 1 of 5 chunks errors — assert the node is
  re-enqueued and no embeddings for it are upserted.
- Drain with `Workers=1` produces byte-identical results to
  pre-change behavior.
- Race detector run (`make test-race`) is clean.

### #10 Batched Ollama requests

**Suggested shape:** extend the embedder interface with an optional
`EmbedBatch`:

```go
type Embedder interface {
    Embed(ctx context.Context, payload []byte) ([]float32, error)
    Model() string
    Dim() int
}

type BatchEmbedder interface {
    Embedder
    EmbedBatch(ctx context.Context, payloads [][]byte) ([][]float32, error)
}
```

Drain loop checks for `BatchEmbedder` and prefers the batch path when
available. Stub embedders in tests can implement either.

Ollama's `/api/embeddings` accepts `prompts: []string` since Ollama
0.1.30+ (need to pin a min version). Response is `embeddings: [][]float32`
in the same order.

**Key decisions:**

- `BatchSize` knob: default ~8. Larger batches reduce HTTP overhead
  but increase memory and the cost of a single failure.
- Partial-batch failure handling: Ollama may return a mixed response.
  Conservative approach: on any batch failure, fall back to per-prompt
  retry for that batch's chunks. (Don't try to parse partial responses;
  simpler.)
- Composition with #9: each worker calls `EmbedBatch` with up to
  `BatchSize` chunks pulled from the job channel. Throughput is
  bounded by min(`Workers * BatchSize`, Ollama's effective concurrency).

**Tests required:**

- `EmbedBatch` returns vectors in input order.
- Mixed-success batch: stub returns first 3 OK, 4th fails → drain
  falls back to per-prompt retry for the failed chunk.
- Min Ollama version documented; integration test (skipped in CI)
  pinned to that version.

---

## Open questions for the brainstorm

1. **Hardcoded 30s timeout.** Should Spec B raise this, make it
   configurable, or leave it as-is and let workers + batching make
   each call's wall-clock easier? Probably worth surfacing as a
   `manifest.Embeddings.TimeoutSeconds` knob with a higher default
   (e.g. 120s) — orthogonal to #9/#10 but pulled in by the same
   "embedding throughput sucks on CPU Ollama" pain.

2. **Per-node vs cross-node parallelism.** The simpler design parallelizes
   chunks within a node. A workspace with many single-chunk nodes
   gets no benefit. Worth a quick analysis of typical chunk-per-node
   distribution (run `tusk doctor`'s new stats block on a real
   workspace to ground-truth this).

3. **Worker count default.** 4? 8? `runtime.NumCPU()`? Ollama's own
   server has a `OLLAMA_NUM_PARALLEL` env var; oversubscribing past
   that just queues internally. A conservative default + manifest
   knob is probably right.

4. **Configurable batch size for #10.** Same question: what's the
   default, what knob exposes it.

5. **Provider abstraction.** If/when OpenAI/Voyage providers land
   (#6 trigger), they likely have native batch endpoints with
   different size limits. The `BatchEmbedder` interface should be
   neutral to those.

---

## Where to start

After PR #380 merges:

```bash
git checkout main
git pull
# from main:
git worktree add .worktrees/parallel-batched-embed -b feat/parallel-batched-embed
cd .worktrees/parallel-batched-embed
make build && make test
```

Then run the brainstorming skill on this handoff doc. The brainstorm
should produce two outputs: a spec at
`docs/superpowers/specs/YYYY-MM-DD-parallel-batched-embed-design.md`
and a phased plan at
`docs/superpowers/plans/YYYY-MM-DD-parallel-batched-embed.md`.

A reasonable phasing:

- **Phase 1:** Parallel workers (#9) + optional timeout knob — biggest
  wall-clock win, narrowest blast radius.
- **Phase 2:** `BatchEmbedder` interface + Ollama batch implementation
  (#10) — composes on top of Phase 1, smaller incremental win.

Each phase should ship as its own PR.

---

## Pointers

- Original chunking handoff with full context for both items:
  `docs/handoffs/2026-05-13-embed-chunking-followups.md` §9 and §10.
- Sequential drain code to replace:
  `internal/embed/drain.go` (the `for chunkIdx, bodyChunk := range
  bodyChunks` block around line 155-232).
- Embedder interface:
  `internal/embed/embedder.go`, `internal/embed/ollama.go`.
- Retry semantics from PR #369:
  `internal/embed/drain.go:17-22` (the `MaxEmbedAttempts` const + the
  surrounding re-enqueue logic). Must be preserved.
- PR #380 (snippet + doctor) — the doctor stats block is the cleanest
  way to measure throughput impact on a real workspace before and
  after.
