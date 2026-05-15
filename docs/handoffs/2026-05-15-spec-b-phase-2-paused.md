---
type: handoff
title: Spec B Phase 2 — paused on Ollama wire-format discovery
session-date: "2026-05-15"
---

# Spec B Phase 2 — paused on Ollama wire-format discovery

This handoff supersedes [[docs/handoffs/2026-05-15-spec-b-phase-2-handoff]] for the purposes of "what to do next." Phase 2 was kicked off, the first implementation commit landed and was then reverted, and the branch `feat/batched-embed` currently sits at one commit — the ticket bump.

The pause is not a regression on the work; it's the implementer flagging a spec-level defect that the previous handoff's "verify the version pin" checkbox surfaced. The defect is bigger than a version-pin tweak.

---

## Prerequisite — run the host-Ollama probe first

Before acting on anything below, read [[docs/handoffs/2026-05-15-spec-b-host-ollama-probe]]. It dispatches a Claude session **on the Mac host** to install Metal-accelerated Ollama natively and measure per-call embed latency. Docker Desktop on macOS can't pass Metal through to the Linux container, so the in-container Ollama referenced throughout this file is CPU-bound by hardware, not by configuration.

If the host probe shows Metal Ollama delivers ≤100ms per chunk-sized embed call (expected on Apple Silicon), **most of this handoff becomes obsolete**: Phase 2's batching has no meaningful headroom against a sub-100ms baseline, and Spec B should be closed as "obsoleted by GPU embedding" rather than re-spec'd. The empirical probes below were designed for the CPU regime; on Metal the relevant measurement is full-reindex wall-clock against the 704s CPU baseline, not the batch-vs-singular microbenchmark.

Only fall through to "What to do FIRST in the next session" and the spec/plan edits below if the host probe comes back negative (Intel Mac, Metal not active, or single-call latency still > 500ms).

---

## Branch state

```
feat/batched-embed
└─ cdd57bd  docs(ticket): activate Spec B ticket and record Phase 1 shipped marker
   └─ main (889c07f)
```

`cdd57bd` is kept because the Phase 1 shipped marker it adds to [[tickets/spec-b-parallel-batched-embed]] is independently correct and useful regardless of Phase 2's eventual shape. The previous handoff explicitly asked for this commit as the first thing on the Phase 2 branch.

The implementation commit `42d597d` ("feat(embed): add BatchEmbedder interface and OllamaEmbedder.EmbedBatch") was reset out of the branch. Its content is described in detail below — anyone resuming Phase 2 should treat it as a useful first draft of what the code shape will look like, *with the wire format corrected*.

---

## What was attempted

Following [[docs/handoffs/2026-05-15-spec-b-phase-2-handoff]] verbatim:

1. Created `feat/batched-embed` off main.
2. Bumped [[tickets/spec-b-parallel-batched-embed]] status `pending → active` and recorded the Phase 1 measurement evidence in the Phasing section (commit `cdd57bd`).
3. Dispatched an implementer subagent for combined plan Tasks 6 + 7: `BatchEmbedder` optional interface + `OllamaEmbedder.EmbedBatch` method + four tests (three wire-format/error tests + one `TUSK_OLLAMA_INTEGRATION`-gated integration test). The implementer landed commit `42d597d` with all unit tests green and the integration test skip-gated.

The implementer self-reported `DONE_WITH_CONCERNS` because of a wire-format discovery during the version-pin verification step. That concern turned out to be load-bearing, was empirically reproduced against the local Ollama, and led to the pause.

---

## The wire-format discovery

The previous handoff asked the implementer to "confirm against Ollama's release notes" the assertion that `/api/embeddings` accepts a `prompts: []string` field starting at Ollama 0.1.30. The implementer cross-checked Ollama's GitHub releases and reported that the entire premise of the spec's wire format is wrong — not just the version cutoff.

**What the spec / plan / commit `42d597d` does:**

```
POST /api/embeddings
{"model": "...", "prompts": ["...", "..."]}
```

→ expects response `{"embeddings": [[...], [...]]}`

**What Ollama actually does:**

- The legacy `/api/embeddings` endpoint (singular) takes `{"model": "...", "prompt": "..."}` (singular `prompt` string) and returns `{"embedding": [...]}` (singular `embedding` array). It has never accepted a `prompts` array.
- Batch embedding was added in **Ollama 0.3.4** ([release notes](https://github.com/ollama/ollama/releases)) via a **new endpoint** `/api/embed` that takes `{"model": "...", "input": <string|string[]>}` and returns `{"embeddings": [[...]]}`.

The response shape `{"embeddings": [[...]]}` that the spec specified happens to match the real `/api/embed` response — but the URL is `/api/embed`, not `/api/embeddings`, and the request field is `input`, not `prompts`.

**Empirical confirmation against the local Ollama 0.23.3:**

| Request | Result |
|---|---|
| `POST /api/embeddings  {"prompt": "hi"}` (singular) | Works (Phase 1 measurement proved this end-to-end) |
| `POST /api/embeddings  {"prompts": ["hi","there"]}` (plan's format) | Returns `{"embedding":[]}` instantly — silently ignores the unknown field, finds no `prompt`, returns an empty embedding |
| `POST /api/embed  {"input": "hi"}` (single string, new endpoint) | Probed in-session; timed out at 30s with no response. Inconclusive — could be a model-warmup or process-state issue, could be a real bug. Needs a clean reproduction. |
| `POST /api/embed  {"input": ["hi","there"]}` (batch, new endpoint) | Same: timed out at 30s with no response. |

Why the unit tests in commit `42d597d` passed despite this: they use `httptest.NewServer` to mock the response, so the URL path and field names in the request are never validated against a real Ollama. Only the `TUSK_OLLAMA_INTEGRATION=1`-gated test would have caught it, and that gate is correctly `t.Skip`-by-default for CI hygiene.

**Why the existing implementation degrades safely if shipped as-is:** the implementation in `42d597d` checked `len(decoded.Embeddings) != len(payloads)` and returned an error on mismatch. Against real Ollama, every batch call would receive `{"embedding": []}`, which decodes as zero embeddings, the mismatch fires, the batch errors out, and Task 8's per-prompt fallback would kick in for every batch. The net effect would be the Phase 1 single-prompt path + one wasted HTTP round-trip per batch — measurably slower than Phase 1, never faster.

---

## How this stacks with the Phase 1 evidence

The previous handoff already noted that Phase 1 (parallel workers) delivered **0× speedup** on this dogfood workspace's CPU Ollama:

| Mode | Wall-clock |
|---|---|
| `workers = 1` | 704s |
| `workers = 4` | 714s |

The reasoning given was that Ollama on CPU is inference-bound per call — concurrent workers contend rather than scale.

Phase 2 was framed as the load-bearing optimization that would sidestep that ceiling by batching N prompts into one HTTP call (letting Ollama do its own internal scheduling, removing N−1 round-trips). But there's no evidence in this repo, and no evidence I was able to capture in-session, that Ollama's batch endpoint on this hardware actually delivers any speedup over the singular endpoint. The integration test that would have measured this was `t.Skip`-by-default and never run during plan or implementation.

**The combined picture is:** Spec B was based on the premise that client-side changes can speed up Ollama embed throughput on CPU. Phase 1 disproved the premise for the workers axis. Phase 2's batching axis is unvalidated, and the spec's prescribed wire format wouldn't have hit the real batch endpoint anyway. Until someone runs a clean side-by-side measurement of `/api/embed` (batch) vs `/api/embeddings` (singular) on this hardware, Phase 2 has no evidence behind it.

---

## Open questions for the next session

Resolve these **before** rewriting code, in roughly this order:

1. **Does `/api/embed` actually work on Ollama 0.23.3 + `nomic-embed-text`?** My in-session probes hung at 30s with zero response. The model was loaded (`/api/ps` confirmed) but the `expires_at` was ~5 minutes away when I probed, so it may have been on its way out. Run a clean probe with the model freshly warmed: `curl http://localhost:11434/api/embed -d '{"model":"nomic-embed-text","input":"hi"}'`, then `'{"input":["hi","there"]}'`. Capture latency and response shape.

2. **If it works: is batching actually faster?** Time `/api/embed` with `input: [<8 strings>]` vs 8 sequential calls to `/api/embeddings` with singular `prompt`. The prompts should be chunk-sized (1600-4000 bytes each), not 2-byte strings, because Phase 1 already proved tiny prompts behave differently from realistic chunks. If batch is not at least ~20-30% faster on 8-element batches, Phase 2 has no headroom on this hardware and the entire spec needs re-thinking.

3. **If batching does help: what's the optimal batch size?** Test `input: [<N strings>]` for `N ∈ {2, 4, 8, 16, 32}` and find where per-prompt latency stops improving. This becomes the empirically-grounded default for `embeddings.batch-size` — the spec's "8" was a guess.

4. **What about server-side `OLLAMA_NUM_PARALLEL`?** The previous handoff suggested Phase 1's 0× result might be related to Ollama's server-side parallelism being saturated already. Is `OLLAMA_NUM_PARALLEL` set on this install? Does raising it change Phase 1's numbers? This isn't strictly a Phase 2 question but it bears on whether the *whole* Spec B premise is viable on CPU Ollama or whether the only real fix is a hardware/runtime change.

If (1) or (2) come back negative, the right call is to **abandon Phase 2** and close the ticket with a finding: "client-side embed throughput optimizations don't move the needle on CPU Ollama; revisit when a GPU or alternative embedder provider is in play."

If (1) and (2) come back positive, revise the spec ([[docs/superpowers/specs/2026-05-14-parallel-batched-embed-design]]) and the plan ([[docs/superpowers/plans/2026-05-15-parallel-batched-embed]]) with the verified wire format and the measured optimal batch size, then resume Phase 2 from Task 6.

---

## What the spec / plan need when Phase 2 resumes

When (and only when) the empirical questions above come back positive, edit these files before re-dispatching an implementer:

- **Spec** ([[docs/superpowers/specs/2026-05-14-parallel-batched-embed-design]]):
  - "Ollama wire format" subsection under Phase 2 — replace `/api/embeddings` + `prompts: []string` with `/api/embed` + `input: []string`. Update the response decode note (it was already correct).
  - "Risks" subsection — replace "minimum Ollama version supporting `prompts: []`" with "minimum Ollama version 0.3.4 (introduces `/api/embed`)".
  - Update the "Open questions" final list — question #5 currently says "BatchEmbedder as a minimal optional interface" which is still correct; no change needed there.

- **Plan** ([[docs/superpowers/plans/2026-05-15-parallel-batched-embed]]):
  - **Task 7 — Implement `OllamaEmbedder.EmbedBatch`** — update the implementation snippet's URL from `embedder.config.Endpoint+"/api/embeddings"` to `embedder.config.Endpoint+"/api/embed"`. Change the inline-comment version pin from `0.1.30` to `0.3.4`. Change the request body's `Prompts []string \`json:"prompts"\`` to `Input []string \`json:"input"\``. The decode struct and dimension-check logic are unchanged.
  - **Task 7 Step 1 tests** — the assertion `strings.Contains(capturedBody, \`"prompts":["hi","ho"]\`)` becomes `strings.Contains(capturedBody, \`"input":["hi","ho"]\`)`. The two error-shape tests are unchanged.
  - **Task 7 integration test** (which the previous handoff asked the implementer to add fresh — the implementer did add it in `42d597d`, and the added test is a usable starting point modulo the same URL/field renames) — keep the `TUSK_OLLAMA_INTEGRATION=1` gate convention, but **adopt a project-wide hook** for it. The convention "first env-gated test in the repo" deserves a one-paragraph note in either CONTRIBUTING-style docs or in a comment above the gate. The next session can decide where.
  - **Task 9 default** — `embed.DefaultBatchSize` is currently set to 8 in the plan. Update to whatever the empirical optimum from question (3) above turns out to be.
  - **Task 10 measurement** — the plan currently has the implementer record Phase 2 wall-clock against Phase 1 baseline. Add an explicit gate: "If Phase 2 wall-clock isn't at least 2× faster than Phase 1 baseline on the dogfood workspace, do not merge — escalate to planner instead." Without this gate, Phase 2 can ship as a no-op feature.

The `BatchEmbedder` interface design (Task 6) is unaffected by the wire-format issue; the interface contract is generic over providers. Keep it as-is.

The drain-loop batch dispatcher (Task 8) is also unaffected; it doesn't know which endpoint the embedder hits.

The manifest knob plumbing (Task 9) is unaffected; the knob value just changes.

---

## What about the reverted commit's content

The reverted commit `42d597d` is a usable starting point for whoever resumes. Its diff added:

- `internal/embed/embedder.go` — `BatchEmbedder` interface with the spec-prescribed contract. **Reusable as-is.**
- `internal/embed/embedder_test.go` — compile-time conformance assertion `_ embed.BatchEmbedder = (*embed.OllamaEmbedder)(nil)`. **Reusable as-is.**
- `internal/embed/ollama.go` — `(*OllamaEmbedder).EmbedBatch`. **Needs URL + field rename**, then reusable.
- `internal/embed/ollama_test.go` — 3 wire-format/error tests + 1 `TUSK_OLLAMA_INTEGRATION`-gated integration test. **Needs URL + field-assertion rename in the first test**, then reusable.

To recover the diff for the next session: `git show 42d597d` is still reachable in reflog until garbage collection. Alternatively, the implementer subagent's report (in this session's transcript) has the full file content quoted.

---

## What to do FIRST in the next session

Don't open this file and start editing the spec/plan. Don't dispatch an implementer. Do the empirical probe first.

```bash
# Warm the model.
curl -s http://localhost:11434/api/embeddings -d '{"model":"nomic-embed-text","prompt":"warmup"}' | head -c 100

# Then probe the new batch endpoint.
time curl -s http://localhost:11434/api/embed -d '{"model":"nomic-embed-text","input":"hi"}' | head -c 200
time curl -s http://localhost:11434/api/embed -d '{"model":"nomic-embed-text","input":["hi","there"]}' | head -c 200
```

If those come back cleanly, run a chunk-sized timed comparison (8 sequential singular calls vs 1 batch-of-8) using realistic prompt sizes from the dogfood workspace. Decide based on results.

If they don't come back cleanly within ~5 seconds each on a warm model, that itself is the answer: this Ollama install can't do batch embedding usefully, and Phase 2 should be abandoned rather than re-spec'd.

---

## Pointers

- Predecessor handoff (now partially obsolete — its prescribed flow is the right shape, the wire-format premise is wrong): [[docs/handoffs/2026-05-15-spec-b-phase-2-handoff]]
- Spec needing revision: [[docs/superpowers/specs/2026-05-14-parallel-batched-embed-design]]
- Plan needing revision: [[docs/superpowers/plans/2026-05-15-parallel-batched-embed]]
- Ticket (status remains `active`; the pause is captured here, not in the ticket): [[tickets/spec-b-parallel-batched-embed]]
- Phase 1 PR (the shipped baseline this builds on): https://github.com/germanamz/tusk/pull/381
- Reverted commit (reachable via reflog while it survives gc): `42d597d`
- Ollama batch API release notes (where the discovery was sourced): https://github.com/ollama/ollama/releases (search for "0.3.4" / "/api/embed")
