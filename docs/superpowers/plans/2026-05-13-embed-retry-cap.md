## Embed-queue retry cap — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cap embed-queue retries at `MaxEmbedAttempts` (=3) so `DrainQueue` terminates gracefully when a node persistently fails to embed, replacing the dead `Queue.MarkFailed` path whose attempts counter never persisted across `Drain` cycles.

**Architecture:** `Drain` is destructive (deletes rows in the same tx as it reads them), so the existing `attempts` column was effectively write-only — `MarkFailed` wrote to a row that was already deleted. The fix introduces a new repo method `ReEnqueue(nodeID, attempts, lastError)` that reinserts the row with explicit attempts and a bumped `enqueued_at` (anti-starvation). The drain loop reads `queued.Attempts`, increments, and either calls `ReEnqueue` (if below cap) or emits a `Warn msg="embed gave up"` log and lets the row stay deleted (if at cap). The outer drain loop's existing `len(batch) == 0` termination then triggers naturally.

**Tech Stack:** Go 1.x, SQLite (via `database/sql` + `mattn/go-sqlite3`), `log/slog` (added in PR #368).

**Spec source:** `docs/handoffs/2026-05-13-embed-retry-cap.md`.

---

## Pre-flight

Confirm before starting:

- Branch is `feat/embed-retry-cap`, currently one commit ahead of `main` (just the handoff doc).
- PR #368 (`feat/verbose-logging`) is merged into `main` (verify: `gh pr view 368 --json state` → `MERGED`). The slog wiring this plan depends on landed there.
- If PR #368 had NOT merged, the plan said to rebase onto `feat/verbose-logging`. **It has merged (2026-05-13)** — no rebase needed.

Run before any code change:

```bash
make build && make test && make lint
```

Baseline must be green. If not, stop and debug — don't lay new code on a red tree.

---

## Scope note: three failure branches, not one

The handoff's example code only rewrites the **embed-error** branch in `drain.go` (~line 129–148), but `MarkFailed` is also called from the **read-error** and **parse-error** branches (lines 94 and 103). Deleting `MarkFailed` (which the handoff explicitly requires) means all three branches need the new `ReEnqueue`-with-cap pattern.

**Decision (apply uniformly):** All three branches use the same cap+ReEnqueue pattern. Only the embed-error branch gets new `embed gave up` / `embed re-enqueued` Warn logs (matching the handoff's explicit logging spec); the read-error and parse-error branches stay silent on failure as they are today, just gaining termination. This preserves the handoff's stated logging design while making `MarkFailed` deletion safe.

---

## File structure

Files this plan touches (no new files):

- `internal/index/embed_queue_repo.go` — add `ReEnqueue`, delete `MarkFailed`.
- `internal/index/embed_queue_repo_test.go` — add `ReEnqueue` tests, delete the `MarkFailed` test.
- `internal/embed/drain.go` — add `MaxEmbedAttempts` constant, replace all three `Enqueue + MarkFailed` call sites with cap+`ReEnqueue` pattern, add `embed gave up` log on the embed branch.
- `internal/embed/drain_test.go` — add `TestDrainQueue_GivesUpAfterMaxAttempts`, simplify `TestDrainQueue_LogsWarnOnEmbedError` (drop cancel hack), drop `afterCall` from `drainStubEmbedder` if unused after that.

No schema migration: the `embed_queue.attempts` column already exists and is correctly typed.

---

## Task 1: Add `ReEnqueue` to `EmbedQueueRepo`

**Files:**
- Modify: `internal/index/embed_queue_repo.go` (add new method after `Enqueue`, ~line 40)
- Modify: `internal/index/embed_queue_repo_test.go` (add new tests; keep the existing `MarkFailed` test for now — Task 3 deletes it)

This task ships green with `MarkFailed` still alive and still called from `drain.go`. The new method is dead code at the end of this task; Task 2 wires it up.

- [ ] **Step 1.1: Write the failing test `TestEmbedQueueRepo_ReEnqueuePreservesAttempts`**

Append to `internal/index/embed_queue_repo_test.go`:

```go
func TestEmbedQueueRepo_ReEnqueuePreservesAttempts(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	if enqErr := repo.Enqueue("n1"); enqErr != nil {
		test.Fatalf("Enqueue: %v", enqErr)
	}

	firstDrain, firstDrainErr := repo.Drain(10)

	if firstDrainErr != nil {
		test.Fatalf("first Drain: %v", firstDrainErr)
	}

	if len(firstDrain) != 1 || firstDrain[0].Attempts != 0 {
		test.Fatalf("first drain = %+v, want one row with Attempts=0", firstDrain)
	}

	if reErr := repo.ReEnqueue("n1", 1, "first failure"); reErr != nil {
		test.Fatalf("ReEnqueue 1: %v", reErr)
	}

	secondDrain, secondDrainErr := repo.Drain(10)

	if secondDrainErr != nil {
		test.Fatalf("second Drain: %v", secondDrainErr)
	}

	if len(secondDrain) != 1 {
		test.Fatalf("second drain len = %d, want 1", len(secondDrain))
	}

	if secondDrain[0].Attempts != 1 {
		test.Errorf("Attempts after first ReEnqueue = %d, want 1", secondDrain[0].Attempts)
	}

	if secondDrain[0].LastError != "first failure" {
		test.Errorf("LastError after first ReEnqueue = %q, want %q", secondDrain[0].LastError, "first failure")
	}

	if reErr := repo.ReEnqueue("n1", 2, "second failure"); reErr != nil {
		test.Fatalf("ReEnqueue 2: %v", reErr)
	}

	thirdDrain, thirdDrainErr := repo.Drain(10)

	if thirdDrainErr != nil {
		test.Fatalf("third Drain: %v", thirdDrainErr)
	}

	if len(thirdDrain) != 1 {
		test.Fatalf("third drain len = %d, want 1", len(thirdDrain))
	}

	if thirdDrain[0].Attempts != 2 {
		test.Errorf("Attempts after second ReEnqueue = %d, want 2", thirdDrain[0].Attempts)
	}

	if thirdDrain[0].LastError != "second failure" {
		test.Errorf("LastError after second ReEnqueue = %q, want %q", thirdDrain[0].LastError, "second failure")
	}
}
```

- [ ] **Step 1.2: Run the test, verify it fails**

Run: `go test ./internal/index/ -run TestEmbedQueueRepo_ReEnqueuePreservesAttempts -v`

Expected: FAIL with `repo.ReEnqueue undefined (type *EmbedQueueRepo has no field or method ReEnqueue)`.

- [ ] **Step 1.3: Implement `ReEnqueue` in the repo**

In `internal/index/embed_queue_repo.go`, insert this method between `Enqueue` (ends ~line 40) and `Drain` (starts ~line 44):

```go
// ReEnqueue reinserts a row for nodeID with the explicit attempts count and
// last_error, bumping enqueued_at to time.Now() so FIFO ordering reflects the
// most-recent attempt (anti-starvation). If a row already exists for nodeID
// (rare — Drain deletes before the embed loop runs — but possible if a caller
// re-enqueues out of band), its attempts, last_error, and enqueued_at are
// overwritten.
func (repo *EmbedQueueRepo) ReEnqueue(nodeID string, attempts int, lastError string) error {
	_, execErr := repo.db.Exec(`
		INSERT INTO embed_queue (node_id, enqueued_at, attempts, last_error)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
			enqueued_at = excluded.enqueued_at,
			attempts = excluded.attempts,
			last_error = excluded.last_error
	`, nodeID, time.Now().UnixNano(), attempts, lastError)

	if execErr != nil {
		return fmt.Errorf("embedQueueRepo: re-enqueue %s: %w", nodeID, execErr)
	}

	return nil
}
```

- [ ] **Step 1.4: Run the new test, verify it passes**

Run: `go test ./internal/index/ -run TestEmbedQueueRepo_ReEnqueuePreservesAttempts -v`

Expected: PASS.

- [ ] **Step 1.5: Write the optional starvation test `TestEmbedQueueRepo_ReEnqueueBumpsEnqueuedAt`**

Append to `internal/index/embed_queue_repo_test.go`:

```go
func TestEmbedQueueRepo_ReEnqueueBumpsEnqueuedAt(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	if enqErr := repo.Enqueue("n1"); enqErr != nil {
		test.Fatalf("Enqueue n1: %v", enqErr)
	}

	// Sleep so n2's enqueued_at is strictly later than n1's.
	time.Sleep(2 * time.Millisecond)

	if enqErr := repo.Enqueue("n2"); enqErr != nil {
		test.Fatalf("Enqueue n2: %v", enqErr)
	}

	firstDrain, _ := repo.Drain(10)

	if len(firstDrain) != 2 || firstDrain[0].NodeID != "n1" || firstDrain[1].NodeID != "n2" {
		test.Fatalf("first drain = %+v, want [n1, n2] FIFO", firstDrain)
	}

	time.Sleep(2 * time.Millisecond)

	if reErr := repo.ReEnqueue("n1", 1, "failed"); reErr != nil {
		test.Fatalf("ReEnqueue n1: %v", reErr)
	}

	if enqErr := repo.Enqueue("n2"); enqErr != nil {
		test.Fatalf("re-Enqueue n2: %v", enqErr)
	}

	secondDrain, _ := repo.Drain(10)

	if len(secondDrain) != 2 {
		test.Fatalf("second drain len = %d, want 2", len(secondDrain))
	}

	if secondDrain[0].NodeID != "n2" || secondDrain[1].NodeID != "n1" {
		test.Errorf("second drain order = [%s, %s], want [n2, n1] (n1 bumped after ReEnqueue)",
			secondDrain[0].NodeID, secondDrain[1].NodeID)
	}
}
```

Add `"time"` to the test file's imports if it's not already there (the file currently has no imports beyond `testing` and the `index` package).

Run: `go test ./internal/index/ -run TestEmbedQueueRepo_ReEnqueueBumpsEnqueuedAt -v`

Expected: PASS (the `ON CONFLICT DO UPDATE` already bumps `enqueued_at = excluded.enqueued_at`).

**If this test flakes** (e.g., the SQLite UnixNano timestamps tie because the kernel clock is too coarse on this host), delete the test and replace it with a one-line `TODO`:

```go
// TODO(retry-cap): assert enqueued_at-bump prevents starvation (skipped — flaky on coarse clocks).
```

Then continue. Per the handoff, this assertion is a nice-to-have, not load-bearing.

- [ ] **Step 1.6: Run the full index package test suite to confirm nothing else broke**

Run: `go test ./internal/index/ -v`

Expected: all tests PASS, including the existing `TestEmbedQueueRepo_MarkFailedKeepsInQueue` (which Task 3 deletes).

- [ ] **Step 1.7: Run lint**

Run: `make lint`

Expected: clean.

- [ ] **Step 1.8: Commit**

```bash
git add internal/index/embed_queue_repo.go internal/index/embed_queue_repo_test.go
git commit -m "feat(index): add ReEnqueue to EmbedQueueRepo

ReEnqueue reinserts a queue row with an explicit attempts counter and
last_error, bumping enqueued_at so a persistently-failing item rotates
to the tail of the FIFO instead of starving newer items. Wiring into
DrainQueue (and the deletion of the dead MarkFailed path) lands in the
follow-up commit."
```

---

## Task 2: Cap drain retries with `MaxEmbedAttempts` and switch `drain.go` to `ReEnqueue`

**Files:**
- Modify: `internal/embed/drain.go` (add `MaxEmbedAttempts`, replace three failure branches)
- Modify: `internal/embed/drain_test.go` (add new gives-up test, simplify the existing log-warn test, remove `afterCall` from the stub if unused)

After this task, `MarkFailed` has zero callers but still exists. Task 3 deletes it.

- [ ] **Step 2.1: Write the failing test `TestDrainQueue_GivesUpAfterMaxAttempts`**

Append to `internal/embed/drain_test.go`:

```go
func TestDrainQueue_GivesUpAfterMaxAttempts(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	createNodeFile(test, root, "doomed.md", "body")

	if upsertErr := nodeRepo.Upsert(index.NodeRow{ID: "doomed", Type: "note", Path: "doomed.md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert: %v", upsertErr)
	}

	if enqErr := queueRepo.Enqueue("doomed"); enqErr != nil {
		test.Fatalf("enqueue: %v", enqErr)
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	failing := &drainStubEmbedder{
		dim:     3,
		model:   "fake",
		failure: fmt.Errorf("forced failure"),
	}

	drained, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepo,
		Queue:      queueRepo,
		Embeddings: embeddingRepo,
		Embedder:   failing,
		Chunker:    embed.WholeDocument{},
		Logger:     logger,
	})

	if drainErr != nil {
		test.Fatalf("DrainQueue: %v", drainErr)
	}

	if drained != 0 {
		test.Errorf("drained = %d, want 0 (all attempts failed)", drained)
	}

	if failing.calls != embed.MaxEmbedAttempts {
		test.Errorf("embedder.calls = %d, want %d", failing.calls, embed.MaxEmbedAttempts)
	}

	out := buf.String()

	embedFailures := strings.Count(out, `msg="embed call failed"`)

	if embedFailures != embed.MaxEmbedAttempts {
		test.Errorf("`embed call failed` count = %d, want %d", embedFailures, embed.MaxEmbedAttempts)
	}

	reEnqueues := strings.Count(out, `msg="embed re-enqueued"`)

	if reEnqueues != embed.MaxEmbedAttempts-1 {
		test.Errorf("`embed re-enqueued` count = %d, want %d", reEnqueues, embed.MaxEmbedAttempts-1)
	}

	gaveUps := strings.Count(out, `msg="embed gave up"`)

	if gaveUps != 1 {
		test.Errorf("`embed gave up` count = %d, want 1", gaveUps)
	}

	if !strings.Contains(out, fmt.Sprintf("attempts=%d", embed.MaxEmbedAttempts)) {
		test.Errorf("expected `embed gave up` log to include attempts=%d; got %q", embed.MaxEmbedAttempts, out)
	}

	depth, depthErr := queueRepo.Depth()

	if depthErr != nil {
		test.Fatalf("Depth: %v", depthErr)
	}

	if depth != 0 {
		test.Errorf("queue depth after give-up = %d, want 0", depth)
	}
}
```

- [ ] **Step 2.2: Run the new test, verify it fails**

Run: `go test ./internal/embed/ -run TestDrainQueue_GivesUpAfterMaxAttempts -v`

Expected: FAIL — either the test compiles but hangs (existing retry loop never terminates) and times out, or it fails on `embed.MaxEmbedAttempts undefined`. If the test hangs, add `-timeout=10s` so it surfaces quickly: `go test ./internal/embed/ -run TestDrainQueue_GivesUpAfterMaxAttempts -v -timeout=10s`. A timeout failure here counts as a valid "red" — keep moving.

- [ ] **Step 2.3: Add `MaxEmbedAttempts` constant to `drain.go`**

In `internal/embed/drain.go`, between the package-level imports block and the `DrainConfig` struct (~line 16), insert:

```go
// MaxEmbedAttempts caps how many times DrainQueue retries a failing node
// within a single drain pass. After the cap is hit the node is dropped from
// the queue (Drain already deleted it; we just don't re-enqueue) and a
// Warn `embed gave up` line is emitted. Fresh reindex runs re-enqueue every
// indexed node with attempts=0, so the cap is per-drain, not per-node-lifetime.
const MaxEmbedAttempts = 3
```

- [ ] **Step 2.4: Replace the embed-error branch in `drain.go`**

Currently `internal/embed/drain.go:129-149` looks like:

```go
if embedErr != nil {
    if config.Logger != nil {
        config.Logger.Warn("embed call failed",
            "node_id", queued.NodeID,
            "payload_bytes", len(payload),
            "model", config.Embedder.Model(),
            "err", embedErr.Error(),
        )
    }

    _ = config.Queue.Enqueue(queued.NodeID)
    _ = config.Queue.MarkFailed(queued.NodeID, embedErr.Error())

    if config.Logger != nil {
        config.Logger.Warn("embed re-enqueued", "node_id", queued.NodeID)
    }

    batchFailed++

    continue
}
```

Replace the body (everything inside `if embedErr != nil { ... }`) with:

```go
if embedErr != nil {
    if config.Logger != nil {
        config.Logger.Warn("embed call failed",
            "node_id", queued.NodeID,
            "payload_bytes", len(payload),
            "model", config.Embedder.Model(),
            "err", embedErr.Error(),
        )
    }

    nextAttempts := queued.Attempts + 1

    if nextAttempts >= MaxEmbedAttempts {
        if config.Logger != nil {
            config.Logger.Warn("embed gave up",
                "node_id", queued.NodeID,
                "attempts", nextAttempts,
                "err", embedErr.Error(),
            )
        }
    } else {
        if reEnqErr := config.Queue.ReEnqueue(queued.NodeID, nextAttempts, embedErr.Error()); reEnqErr != nil {
            if config.Logger != nil {
                config.Logger.Warn("embed re-enqueue failed",
                    "node_id", queued.NodeID,
                    "err", reEnqErr.Error(),
                )
            }
        } else if config.Logger != nil {
            config.Logger.Warn("embed re-enqueued",
                "node_id", queued.NodeID,
                "attempts", nextAttempts,
            )
        }
    }

    batchFailed++

    continue
}
```

Note: the existing `embed re-enqueued` log now carries an `attempts=N` key (it didn't before). That's a deliberate change captured in the commit message.

- [ ] **Step 2.5: Replace the read-error branch in `drain.go`**

Currently `internal/embed/drain.go:92-97` looks like:

```go
content, readErr := os.ReadFile(filepath.Join(config.Root, row.Path))

if readErr != nil {
    _ = config.Queue.Enqueue(queued.NodeID)
    _ = config.Queue.MarkFailed(queued.NodeID, readErr.Error())

    continue
}
```

Replace the `if readErr != nil { ... }` body with:

```go
if readErr != nil {
    nextAttempts := queued.Attempts + 1

    if nextAttempts < MaxEmbedAttempts {
        _ = config.Queue.ReEnqueue(queued.NodeID, nextAttempts, readErr.Error())
    }

    continue
}
```

No new logging — this branch is silent today and stays silent. The cap is the only behavioral change.

- [ ] **Step 2.6: Replace the parse-error branch in `drain.go`**

Currently `internal/embed/drain.go:101-106` looks like:

```go
parsed, parseErr := node.ParseFile(row.Path, content)

if parseErr != nil {
    _ = config.Queue.Enqueue(queued.NodeID)
    _ = config.Queue.MarkFailed(queued.NodeID, parseErr.Error())

    continue
}
```

Replace the `if parseErr != nil { ... }` body with:

```go
if parseErr != nil {
    nextAttempts := queued.Attempts + 1

    if nextAttempts < MaxEmbedAttempts {
        _ = config.Queue.ReEnqueue(queued.NodeID, nextAttempts, parseErr.Error())
    }

    continue
}
```

- [ ] **Step 2.7: Update the doc comment on `DrainQueue`**

In `internal/embed/drain.go:29-33`, the current comment says:

```go
// DrainQueue pops every pending row from embed_queue and embeds it. Returns the
// number of nodes successfully embedded. Failed rows are re-enqueued via
// MarkFailed. When DrainConfig.Embedder is nil, DrainQueue is a no-op.
//
// ctx cancellation aborts before the next batch; in-flight batches finish.
```

Replace with:

```go
// DrainQueue pops every pending row from embed_queue and embeds it. Returns the
// number of nodes successfully embedded. Failed rows are re-enqueued (with an
// incremented attempts counter) until MaxEmbedAttempts is reached, at which
// point the row is dropped from the queue. When DrainConfig.Embedder is nil,
// DrainQueue is a no-op.
//
// ctx cancellation aborts before the next batch; in-flight batches finish.
```

- [ ] **Step 2.8: Simplify `TestDrainQueue_LogsWarnOnEmbedError`**

The current test (`internal/embed/drain_test.go:135-198`) uses `afterCall: cancel` + `context.WithCancel` to escape the previously-infinite retry loop. With the cap in place that workaround is no longer needed.

Replace the entire test function with:

```go
func TestDrainQueue_LogsWarnOnEmbedError(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	createNodeFile(test, root, "doc.md", "body")

	if upsertErr := nodeRepo.Upsert(index.NodeRow{ID: "doc", Type: "note", Path: "doc.md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert: %v", upsertErr)
	}

	if enqErr := queueRepo.Enqueue("doc"); enqErr != nil {
		test.Fatalf("enqueue: %v", enqErr)
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	failing := &drainStubEmbedder{
		dim:     3,
		model:   "fake",
		failure: fmt.Errorf("input length exceeds the context length"),
	}

	_, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepo,
		Queue:      queueRepo,
		Embeddings: embeddingRepo,
		Embedder:   failing,
		Chunker:    embed.WholeDocument{},
		Logger:     logger,
	})

	if drainErr != nil {
		test.Fatalf("drain: %v", drainErr)
	}

	out := buf.String()

	for _, want := range []string{`msg="embed call failed"`, "node_id=doc", "payload_bytes=", "input length exceeds the context length"} {
		if !strings.Contains(out, want) {
			test.Errorf("expected log to contain %q; got %q", want, out)
		}
	}

	if !strings.Contains(out, `msg="embed re-enqueued"`) {
		test.Errorf("expected re-enqueue log; got %q", out)
	}

	if !strings.Contains(out, `msg="drain batch complete"`) {
		test.Errorf("expected batch summary log; got %q", out)
	}
}
```

Key changes: no `ctx, cancel`, no `afterCall: cancel`, just `context.Background()`. The cap (3 attempts) makes the drain terminate on its own. The assertions still hold — the first failure still emits `embed call failed`, the first re-enqueue still emits `embed re-enqueued`, and the batch summary still fires (now with attempted=3 / succeeded=0 / failed=3, but the test only asserts message presence).

- [ ] **Step 2.9: Remove the now-unused `afterCall` field from `drainStubEmbedder`**

Check first whether any other test in the package still uses `afterCall`:

```bash
grep -n "afterCall" internal/embed/drain_test.go
```

If the only remaining matches are the field declaration and its `if/defer` inside `Embed`, remove all three.

In `internal/embed/drain_test.go:17-23`, change:

```go
type drainStubEmbedder struct {
	calls     int
	dim       int
	model     string
	failure   error
	afterCall func()
}
```

to:

```go
type drainStubEmbedder struct {
	calls   int
	dim     int
	model   string
	failure error
}
```

And in `internal/embed/drain_test.go:25-32`, remove the `if stub.afterCall != nil { defer stub.afterCall() }` block. The `Embed` method body becomes:

```go
func (stub *drainStubEmbedder) Embed(ctx context.Context, payload []byte) ([]float32, error) {
	stub.calls++

	if stub.failure != nil {
		return nil, stub.failure
	}

	out := make([]float32, stub.dim)

	for idx := range out {
		out[idx] = 0.1
	}

	return out, nil
}
```

If `grep` shows another test still uses `afterCall`, skip this step and leave the field alone.

- [ ] **Step 2.10: Run the embed test suite**

Run: `go test ./internal/embed/ -v`

Expected: all tests PASS, including:
- `TestDrainQueue_DrainsToEmpty` (unchanged)
- `TestDrainQueue_NoopWhenNoEmbedder` (unchanged)
- `TestDrainQueue_LogsWarnOnEmbedError` (simplified)
- `TestDrainQueue_GivesUpAfterMaxAttempts` (new)

- [ ] **Step 2.11: Run the full test suite + lint**

```bash
make test && make lint
```

Expected: green. `make test` covers the whole tree because the embed/index packages are imported elsewhere.

- [ ] **Step 2.12: Commit**

```bash
git add internal/embed/drain.go internal/embed/drain_test.go
git commit -m "feat(embed): cap drain retries at MaxEmbedAttempts

DrainQueue now gives up on a node after MaxEmbedAttempts (=3) failed
embed calls, emitting a Warn 'embed gave up' line and letting the node
fall out of the queue. The outer drain loop returns naturally once all
queued items either succeed or hit the cap.

Replaces the dead Queue.MarkFailed path (the existing attempts counter
on embed_queue was never persisting across cycles because Drain is
destructive). All three failure branches (read, parse, embed) now use
the new ReEnqueue method that preserves attempts, last_error, and bumps
enqueued_at so a persistently-failing item rotates to the tail of the
FIFO instead of starving others.

The 'embed re-enqueued' Warn log now carries an attempts=N key.

Fresh reindex runs re-enqueue with attempts=0; the cap only governs
within a single drain pass."
```

---

## Task 3: Delete the dead `MarkFailed` method and its test

**Files:**
- Modify: `internal/index/embed_queue_repo.go` (remove `MarkFailed` method)
- Modify: `internal/index/embed_queue_repo_test.go` (remove `TestEmbedQueueRepo_MarkFailedKeepsInQueue`)

After Task 2, `MarkFailed` has zero production callers (only the test calls it). This task removes both.

- [ ] **Step 3.1: Confirm zero non-test callers**

Run:

```bash
grep -rn "MarkFailed" internal/ cmd/ --include='*.go'
```

Expected output: only matches in `internal/index/embed_queue_repo.go` (the method definition) and `internal/index/embed_queue_repo_test.go` (the test we're about to delete). **If `drain.go` still shows matches, stop — Task 2 was not fully applied.**

- [ ] **Step 3.2: Delete `MarkFailed` from the repo**

In `internal/index/embed_queue_repo.go`, delete lines 93-106 (the entire `MarkFailed` method including its `// MarkFailed records...` doc comment):

```go
// MarkFailed records a failure on the queue row, leaves it queued for retry.
func (repo *EmbedQueueRepo) MarkFailed(nodeID, errorMessage string) error {
	_, execErr := repo.db.Exec(`
		UPDATE embed_queue
		SET attempts = attempts + 1, last_error = ?
		WHERE node_id = ?
	`, errorMessage, nodeID)

	if execErr != nil {
		return fmt.Errorf("embedQueueRepo: mark failed %s: %w", nodeID, execErr)
	}

	return nil
}
```

- [ ] **Step 3.3: Delete the `MarkFailed` test**

In `internal/index/embed_queue_repo_test.go`, delete the entire `TestEmbedQueueRepo_MarkFailedKeepsInQueue` function (lines 97-111):

```go
func TestEmbedQueueRepo_MarkFailedKeepsInQueue(test *testing.T) {
	repo := newTestEmbedQueueRepo(test)

	repo.Enqueue("flaky")

	if markErr := repo.MarkFailed("flaky", "ollama unreachable"); markErr != nil {
		test.Fatalf("MarkFailed: %v", markErr)
	}

	depth, _ := repo.Depth()

	if depth != 1 {
		test.Errorf("Depth after MarkFailed = %d, want 1 (still queued)", depth)
	}
}
```

- [ ] **Step 3.4: Build, test, lint**

```bash
make build && make test && make lint
```

Expected: clean build, all tests PASS, lint clean. If anything fails referencing `MarkFailed`, double-check Step 3.1's grep — there may be a caller the earlier search missed.

- [ ] **Step 3.5: Commit**

```bash
git add internal/index/embed_queue_repo.go internal/index/embed_queue_repo_test.go
git commit -m "refactor(index): delete dead MarkFailed from EmbedQueueRepo

MarkFailed was unreachable code: Drain deletes the row in the same
transaction as it reads it, so the UPDATE in MarkFailed always matched
zero rows. DrainQueue now uses ReEnqueue instead (see prior commit),
and no production code calls MarkFailed."
```

---

## Task 4: Smoke test the binary

This task isn't a code change — it's a sanity check that the production code paths still wire up. The full integration scenario (real Ollama, oversized doc) is out of scope for this branch (the chunking fix is the next handoff). What we can verify cheaply:

- [ ] **Step 4.1: Build the binary**

```bash
make build
```

Expected: `./bin/tusk` produced, no errors.

- [ ] **Step 4.2: Confirm the constant is visible from the binary perspective**

Run:

```bash
go doc ./internal/embed MaxEmbedAttempts
```

Expected: prints the const declaration and its doc comment.

- [ ] **Step 4.3: Run `go vet` on the whole tree**

```bash
go vet ./...
```

Expected: clean.

- [ ] **Step 4.4: Run `make test-race`**

```bash
make test-race
```

Expected: clean. (DrainQueue is single-goroutine in this path, but the race detector catches misuse of shared state in tests too.)

No commit for this task — it's pure verification.

---

## Out of scope (do NOT add)

From the handoff §"Out of scope":

- **Configurable cap.** Hardcoded constant. Revisit if a second consumer asks.
- **Per-content-hash sticky failures.** Each reindex starts fresh; no "this hash is poisoned" state.
- **Backoff or scheduling.** All retries happen in the same drain pass; no delay between attempts.
- **Doctor surface for items that gave up.** Skipped per "leave them alone."
- **Chunking fix** (`WholeDocument` → token-aware splitter). Separate handoff. The retry cap doesn't fix the underlying overflow; it just makes the process terminate.
- **MCP runtime test parity** (deterministic queue-timing test). Hard to write; not on this branch.
- **`--log-format=json`** / **`--log-level=warn|info|debug`** / **per-file Debug logs in `reindex.Run`**. All listed in the verbose-logging spec §7 (Future work).

If you find yourself reaching for any of these — stop, finish the cap work, then propose the next handoff.

---

## Conventions reminder (from CLAUDE.md, STYLE.md, handoff §Conventions)

- Conventional commits, scope required: `feat(embed):`, `feat(index):`, `refactor(index):`, etc.
- **No `Co-Authored-By` or "Generated with Claude Code" footers** — user preference.
- STYLE.md rules 1–4 are linter-enforced: ≥2-char identifiers (`reEnqErr`, `drainErr`, not `err`), blank lines around `if err != nil` guards, `test *testing.T` not `t *testing.T`.
- Lefthook pre-commit runs gofmt/vet/lint/test. **Don't bypass with `--no-verify`.** If a hook fails, fix the underlying issue.
- `<new-diagnostics>` blocks after edits can be stale LSP state. Verify with `go test ./... && make lint` before reacting.

---

## Definition of done

- [ ] `make build && make test && make test-race && make lint` all clean.
- [ ] `grep -rn "MarkFailed" internal/ cmd/ --include='*.go'` returns nothing.
- [ ] `grep -rn "MaxEmbedAttempts" internal/ --include='*.go'` shows the const declaration, its usage in three failure branches, and the new test referencing it.
- [ ] Three commits on `feat/embed-retry-cap` (in order): `feat(index): add ReEnqueue…`, `feat(embed): cap drain retries…`, `refactor(index): delete dead MarkFailed…`. Plus the existing `docs(handoff): embed-queue retry cap design` at the base. Four commits total ahead of `main`.
- [ ] No edits outside the four files listed under "File structure."
