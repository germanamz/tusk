---
type: handoff
title: Handoff 2026-05-13 — Embed-queue retry cap
session-date: "2026-05-13"
---

# Tusk — Session Handoff: Embed-queue retry cap

**Status:** Design complete, no code written. The new session should go straight to `superpowers:writing-plans` (skip brainstorming — the design is fully spec'd below) and then execute. Roughly 4–6 small commits of work.

**Branch:** `feat/embed-retry-cap`, currently sitting on `origin/main`. The retry-cap work is conceptually a continuation of `feat/verbose-logging` (PR #368), which is what made the underlying bug visible in the first place. If PR #368 has merged when you start, you're good. If it hasn't, **rebase `feat/embed-retry-cap` onto `feat/verbose-logging` first** so you have the slog wiring already in place — every test in this design uses the same `bytes.Buffer`-backed `*slog.Logger` pattern that verbose-logging introduced.

---

## Why this exists

On 2026-05-13 a `tusk reindex` run fired 144,537 failing `POST /api/embeddings` requests against a local Ollama in 12 minutes against a ~50-node workspace. The bug had two compounding causes:

1. **`internal/embed/chunking.go:15`** — `WholeDocument.Chunk` sends the entire document as one chunk. `nomic-embed-text` has a 2048-token context window, so any doc larger than ~8 KB overflows and Ollama returns HTTP 500 with `"input length exceeds the context length"`.
2. **`internal/embed/drain.go` (the embed-error branch)** — re-enqueues failed nodes with no attempt counter, so the same failure repeats forever.

`feat/verbose-logging` (PR #368) addressed visibility — operators now see one `level=WARN msg="ollama non-2xx"` line on the first failed embed instead of opaque silence. **This handoff is the retry-loop fix**: cap retries so the drain terminates gracefully. Chunking is a separate follow-up (see "Other follow-ups" below).

---

## The actual bug (worth understanding before you start)

`internal/index/embed_queue_repo.go` already has an `attempts INTEGER NOT NULL DEFAULT 0` column on the `embed_queue` table. **But the counter never persists across cycles.** Here's why:

`Drain(limit)` is destructive — it does `SELECT ... FROM embed_queue ORDER BY enqueued_at ASC LIMIT ?` and then `DELETE FROM embed_queue WHERE node_id = ?` in the same transaction. So once drained, the row is gone.

Then `drain.go`'s embed-error branch calls:

```go
_ = config.Queue.Enqueue(queued.NodeID)            // INSERT new row with attempts=0
_ = config.Queue.MarkFailed(queued.NodeID, errMsg) // UPDATE attempts=1 on that row
```

So `attempts` cycles `0 → 1 → (drained, row gone) → 0 → 1 → …` forever. The 297-vs-4 ratio we saw in manual smoke testing (297 `ollama non-2xx` Warns vs. 4 `embed call failed` Warns) is a different artifact — `MarkFailed` updates zero rows because the row was already deleted, so the existing counter is essentially write-only-and-immediately-discarded.

---

## Design (approved by user, 2026-05-13)

### Schema

Unchanged. The `attempts` column already exists and is already correctly typed.

### `internal/index/embed_queue_repo.go`

- **Add** `ReEnqueue(nodeID string, attempts int, lastError string) error`. Inserts the row with the explicit attempts count and last_error, using `INSERT ... ON CONFLICT(node_id) DO UPDATE SET attempts = excluded.attempts, last_error = excluded.last_error, enqueued_at = excluded.enqueued_at` (or equivalent). Bumps `enqueued_at` to "now" so FIFO ordering reflects most-recent attempt — without this the same failing item would always be at the head of the queue and starve other items.
- **Delete** `MarkFailed`. Its sole caller is `drain.go` and the new `ReEnqueue` subsumes its purpose. Confirm no other callers with `grep -rn "MarkFailed" internal/ cmd/ --include='*.go'` before deleting.

### `internal/embed/drain.go`

- **Add constant** at package level:

  ```go
  const MaxEmbedAttempts = 3
  ```

  Hardcoded per user choice. Configurability is YAGNI for now (see "Out of scope").

- **Replace** the existing embed-error branch (currently at ~`drain.go:130-144` after the verbose-logging changes — line numbers will shift slightly):

  ```go
  // before
  _ = config.Queue.Enqueue(queued.NodeID)
  _ = config.Queue.MarkFailed(queued.NodeID, embedErr.Error())

  if config.Logger != nil {
      config.Logger.Warn("embed re-enqueued", "node_id", queued.NodeID)
  }
  ```

  with:

  ```go
  // after
  nextAttempts := queued.Attempts + 1

  if nextAttempts >= MaxEmbedAttempts {
      if config.Logger != nil {
          config.Logger.Warn("embed gave up",
              "node_id", queued.NodeID,
              "attempts", nextAttempts,
              "err", embedErr.Error(),
          )
      }
      // Row was already deleted by Drain; not re-enqueueing means the item
      // leaves the queue and DrainQueue's outer loop will eventually exit.
  } else {
      if reErr := config.Queue.ReEnqueue(queued.NodeID, nextAttempts, embedErr.Error()); reErr != nil {
          if config.Logger != nil {
              config.Logger.Warn("embed re-enqueue failed",
                  "node_id", queued.NodeID,
                  "err", reErr.Error(),
              )
          }
      } else if config.Logger != nil {
          config.Logger.Warn("embed re-enqueued",
              "node_id", queued.NodeID,
              "attempts", nextAttempts,
          )
      }
  }
  ```

  Note the augmented `"embed re-enqueued"` log now carries `attempts` (verbose-logging didn't have this key — that's a small change to existing log semantics, callout it in the commit message).

- The outer `for {}` loop already returns when `len(batch) == 0`. No new termination logic needed — once all items either succeed (Embeddings.Upsert) or hit the cap (no re-enqueue), the queue drains and DrainQueue returns naturally.

### Behavior across multiple reindex runs

`reindex.Run` already re-enqueues every indexed node with `attempts=0` on each run (`internal/reindex/reindex.go:373-376`). Per the design decision, we leave that behavior alone — each `tusk reindex` invocation gets fresh attempts. If the underlying problem (oversized doc) is fixed by then, the embed succeeds; if not, it retries `MaxEmbedAttempts` times and gives up again. No persistent "skip this forever" state.

### Test plan

All tests follow the verbose-logging testing pattern (slog `bytes.Buffer` handler + `strings.Contains` assertions).

1. **`TestDrainQueue_GivesUpAfterMaxAttempts`** (new) in `internal/embed/drain_test.go`:
   - Enqueue one node.
   - Configure DrainQueue with an always-failing fake embedder (returns `errors.New("forced failure")`).
   - Pass a fresh `context.Background()` (no cancel hack needed — DrainQueue must self-terminate).
   - Assert: `DrainQueue` returns `nil` error.
   - Assert: stderr contains `MaxEmbedAttempts` occurrences of `msg="embed call failed"` for that node.
   - Assert: stderr contains `MaxEmbedAttempts - 1` occurrences of `msg="embed re-enqueued"` (the last failure is "gave up", not "re-enqueued").
   - Assert: stderr contains exactly one `msg="embed gave up"` line with `attempts=3`.
   - Assert: queue depth is 0 (post-condition — `repo.Depth()` returns 0).

2. **Simplify `TestDrainQueue_LogsWarnOnEmbedError`** (existing) in `internal/embed/drain_test.go`:
   - The current test uses an `afterCall func()` hook on `drainStubEmbedder` + `context.WithCancel(cancel)` to force termination, because without that the infinite retry would hang. **That workaround is no longer needed** — DrainQueue self-terminates after `MaxEmbedAttempts`. Drop the hook, drop the cancel, use `context.Background()`. The test gets shorter and the `drainStubEmbedder.afterCall` field can be removed if no other test uses it.
   - Confirm the assertions still hold: the first failure path produces `embed call failed`, `embed re-enqueued`, and the drain batch summary, all on the same buffer.

3. **`TestEmbedQueueRepo_ReEnqueuePreservesAttempts`** (new) in `internal/index/embed_queue_repo_test.go`:
   - Enqueue node `n1` (attempts=0).
   - `Drain(10)` → returns one row with `Attempts == 0`.
   - `ReEnqueue("n1", 1, "first failure")` → no error.
   - `Drain(10)` → returns one row with `Attempts == 1` and `LastError == "first failure"`.
   - `ReEnqueue("n1", 2, "second failure")` → no error.
   - `Drain(10)` → returns one row with `Attempts == 2` and `LastError == "second failure"`.
   - Cleanup: nothing — the second drain emptied the queue.

4. **`TestEmbedQueueRepo_ReEnqueueBumpsEnqueuedAt`** (new, optional) in `internal/index/embed_queue_repo_test.go`:
   - Enqueue `n1`, then enqueue `n2` (so `n1` < `n2` in `enqueued_at`).
   - `Drain(10)` returns both, `n1` first (FIFO).
   - `ReEnqueue("n1", 1, "err")` after a brief `time.Sleep(time.Millisecond)`.
   - Enqueue `n2` (fresh).
   - `Drain(10)` returns both with `n2` first now (because n1's enqueued_at was bumped to a later moment).
   - Prevents starvation of other items behind a persistently-failing one.

   **Skip this test if writing it forces flakes** — the underlying behavior is small and the assertion above is a nice-to-have. If you skip, leave a one-line `// TODO(retry-cap): assert enqueued_at-bump prevents starvation` comment.

### Commit shape (suggested)

One commit per concern, in this order:

1. `feat(index): ReEnqueue method on EmbedQueueRepo` — adds the new method + its unit test(s) + deletes `MarkFailed`. Build/test passes because nothing yet calls `MarkFailed` after the deletion.

   Wait — `drain.go` still calls `MarkFailed` until step 2 lands. **Reverse the order**: delete `MarkFailed` after step 2, or do it as one atomic commit.

   Revised: **single commit** that touches both files —
   `feat(embed): cap drain retries at MaxEmbedAttempts and replace MarkFailed with ReEnqueue`. Cleaner in the diff but a bigger atomic change. Pick what your gut says.

2. `feat(embed): add MaxEmbedAttempts cap and "embed gave up" log` — drain.go edits + new + simplified tests.

If you go single-commit, commit message:

```
feat(embed): cap drain retries at MaxEmbedAttempts

DrainQueue now gives up on a node after MaxEmbedAttempts (=3) failed
embed calls, emitting a Warn `msg="embed gave up"` line and letting the
node fall out of the queue. The outer drain loop returns naturally once
all queued items either succeed or hit the cap.

Replaces the dead Queue.MarkFailed path (the existing attempts counter
on embed_queue was never persisting across cycles because Drain is
destructive). New ReEnqueue method preserves attempts + last_error +
bumps enqueued_at so the same failing item doesn't starve others.

Fresh reindex runs re-enqueue with attempts=0; the cap only governs
within a single run.
```

---

## Out of scope (deliberately — don't add these)

- **Configurable cap** (per-workspace `tusk.toml` setting). Hardcoded constant per user choice; revisit if a second consumer asks.
- **Per-content-hash sticky failures.** Each reindex starts fresh; no "this hash is poisoned" state.
- **Backoff or scheduling.** All retries happen in the same drain pass; no delay between attempts.
- **Doctor surface for items that gave up.** Skipped per "leave them alone" reading.
- **Chunking fix** (`WholeDocument` → token-aware splitter). Separate spec, separate handoff. The retry cap doesn't fix the underlying overflow — it just makes the process terminate. The chunking fix would actually let those docs embed successfully.
- **MCP runtime test parity.** The MCP integration smoke test that surfaced PR #368's late embedder-construction bug is hard to write deterministically (queue timing). Don't try to add it as part of this work.

---

## Other follow-ups (not this handoff)

These are listed in `docs/superpowers/specs/2026-05-13-tusk-verbose-logging-design.md` §7 (Future work):

- **Chunking** (the actual root cause). Probably the next handoff. Token-aware splitting respecting the configured model's context window; would obsolete most of the retry-cap firings in practice.
- **`--log-format=json`** — programmatic consumption.
- **`--log-level=warn|info|debug`** — finer control than the boolean.
- **Per-file Debug logs in `reindex.Run`** — deferred from verbose-logging spec §5.3.

---

## Conventions (reminder)

- Conventional commits, scope required: `feat(embed):`, `feat(index):`, `test(embed):`, etc.
- No `Co-Authored-By` or "Generated with Claude Code" footers — user preference.
- STYLE.md rules 1–4 are linter-enforced. ≥2-char identifiers, named errors (`reEnqErr`, `drainErr` not `err`), blank lines around `if err != nil` guards, `test *testing.T` not `t *testing.T`.
- Lefthook pre-commit runs gofmt/vet/lint/test. Don't bypass with `--no-verify`.
- `<new-diagnostics>` blocks fire on stale LSP state after TDD steps. Verify with `go test ./... && make lint` before reacting.

---

## Where to start the new session

> "Pick up the embed-queue retry-cap work. The handoff at `docs/handoffs/2026-05-13-embed-retry-cap.md` has the complete design — skip brainstorming, invoke `superpowers:writing-plans` directly using the handoff doc as the spec. Then execute via `superpowers:subagent-driven-development`. The work is one logical bundle (~4–6 commits): new `ReEnqueue` on the queue repo, new `MaxEmbedAttempts` cap in the drain loop, new `embed gave up` Warn log, delete the dead `MarkFailed`, and the tests. Check whether PR #368 (`feat/verbose-logging`) has merged before starting — if not, rebase this branch on top of `feat/verbose-logging` so the slog wiring exists."

---

## Sync first

```bash
git fetch origin
gh pr view 368 --json state,mergedAt   # check verbose-logging merge status
git checkout feat/embed-retry-cap
git pull --rebase origin main          # or rebase on feat/verbose-logging if not merged
```

Current branch state: `feat/embed-retry-cap` sits at `origin/main` plus this handoff doc (one commit). No code changes yet.
