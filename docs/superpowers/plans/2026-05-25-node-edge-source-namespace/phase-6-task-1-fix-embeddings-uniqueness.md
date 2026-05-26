# Phase 6 — Task 1: Fix embeddings uniqueness shape

**Phase:** 6 (Cleanup)
**Spec:** § *What the schema bump removes* — extended in this phase to also correct the embeddings DDL the dead migration was ratcheting toward.

**Goal:** Replace `embeddings.node_id TEXT NOT NULL UNIQUE` with a composite `UNIQUE(node_id, chunk_idx)` so the table stores one row per chunk, not one row per node. Update `EmbeddingRepo.Upsert`'s `ON CONFLICT` target to match, and rewrite the two tests that currently pin the wrong behavior. Bump `SchemaVersion` so existing indexes rebuild via `OpenOrRebuild`.

## Why this is needed

The current shape is silently incorrect. `internal/embed/drain.go` chunks file-row bodies through `MarkdownRecursive` and calls the embedder once per chunk, but `EmbeddingRepo.Upsert`'s `ON CONFLICT(node_id) DO UPDATE` collapses all those writes onto a single row — last chunk wins. The hash-skip path (`embeddingsMatch` at `internal/embed/drain.go:53-72`) requires `len(existing) == len(newHashes)`; with 1 row stored and N chunks computed, that check is permanently false for any multi-chunk node. Result: every reindex pass re-embeds every chunk of every multi-chunk node, regardless of whether content changed. On a 21-node vault this generated ~16,000 embedding POSTs per reindex against Ollama, sustained at ~30 req/s while the MCP watcher kept firing fresh reindex passes on every fs event. Restoring per-`(node_id, chunk_idx)` uniqueness lets the hash-skip fire and ends the loop.

Two tests currently lock the bug in as expected behavior — `internal/embed/drain_test.go:337-339` asserts `len(rows) == 1` for a multi-chunk node, and `internal/index/embedding_repo_test.go:277-279` asserts `TotalChunks == 4` when 4 single-chunk nodes are inserted. Both comments anticipate a future "Task 4" that would restore many-chunks-per-node semantics via AST sub-units; that work has not landed, and meanwhile the hash-skip is broken in production. This task makes the storage shape match the chunker output today.

## Inherits From

After Phase 5:
- `OpenOrRebuild` is wired through every CLI entry point and the MCP runtime; any `SchemaVersion` bump is handled transparently with a one-time rebuild from source files.
- `nodes` and `edges` carry `(kind, source, type)`; the namespace work is complete.
- No code outside `internal/index/index.go`, `internal/index/embedding_repo.go`, `internal/embed/`, and the two named tests reads or writes the embeddings DDL directly.

## Files

- **Modify:** `internal/index/index.go` — embeddings table DDL.
- **Modify:** `internal/index/embedding_repo.go` — `Upsert` ON CONFLICT target and comment.
- **Modify:** `internal/index/schema_version.go` — bump constant.
- **Modify:** `internal/embed/drain_test.go` — `TestDrainQueue_EmbedsEveryChunkOfMultiChunkNode` (lines 277-340 in the current tree) updated to assert one persisted row per chunk; the comment block at lines 330-336 referencing "Task 4" and `UNIQUE(node_id)` is removed.
- **Modify:** `internal/index/embedding_repo_test.go` — only the misleading comment at lines 235-241 of `TestEmbeddingRepo_Stats_Aggregates` is deleted. The test's fixture (4 nodes, 1 chunk each, asserting `TotalChunks == 4` and `MaxChunks == 1`) continues to pass under the corrected schema without changes, so the assertions stay.

The remaining `len(rows) != 1` assertions in `drain_test.go` (lines 392, 799, 914) were audited and remain correct under the corrected schema:
- Line 392 (`TestDrainQueue_DeletesStaleChunksBeforeReembedding`) — uses `embed.WholeDocument{}` which produces a single chunk; the assertion that exactly 1 row survives after the delete-before-insert clean-up is still right.
- Line 799 — `WholeDocument` chunker, 1 chunk expected.
- Line 914 — sub-unit row, embedded as a single chunk (no chunker call).

No additional test changes are required beyond the two named above.

## Steps

- [ ] **Step 1: Add a failing test pinning the corrected shape**

In `internal/index/embedding_repo_test.go`, add a new test (or refactor the existing aggregate test) that inserts two chunks for the same `node_id` with distinct `chunk_idx` values and asserts both rows persist:

```go
func TestEmbeddingRepo_Upsert_AllowsMultipleChunksPerNode(test *testing.T) {
    test.Parallel()
    repo := newTestEmbeddingRepo(test, "n")

    base := index.EmbeddingRow{
        NodeID: "n", Model: "m", ContentHash: "h", Vector: []float32{0.1}, Dim: 1,
    }

    base.ChunkIdx = 0
    if err := repo.Upsert(base); err != nil {
        test.Fatalf("upsert chunk 0: %v", err)
    }

    base.ChunkIdx = 1
    base.ContentHash = "h2"
    if err := repo.Upsert(base); err != nil {
        test.Fatalf("upsert chunk 1: %v", err)
    }

    rows, getErr := repo.GetByNodeID("n")
    if getErr != nil {
        test.Fatalf("GetByNodeID: %v", getErr)
    }
    if len(rows) != 2 {
        test.Errorf("rows = %d, want 2 (UNIQUE(node_id, chunk_idx) keeps both)", len(rows))
    }
}
```

Run: `go test ./internal/index/... -run TestEmbeddingRepo_Upsert_AllowsMultipleChunksPerNode -v`

Expected: FAIL — the column-level `UNIQUE(node_id)` rejects the second insert.

- [ ] **Step 2: Update the embeddings DDL**

In `internal/index/index.go` (currently around lines 68-80), change:

```sql
CREATE TABLE IF NOT EXISTS embeddings (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    node_id      TEXT NOT NULL UNIQUE,
    chunk_idx    INTEGER NOT NULL DEFAULT 0,
    ...
    FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
);
```

to:

```sql
CREATE TABLE IF NOT EXISTS embeddings (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    node_id      TEXT NOT NULL,
    chunk_idx    INTEGER NOT NULL DEFAULT 0,
    ...
    FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE,
    UNIQUE(node_id, chunk_idx)
);
```

Leave `CREATE INDEX IF NOT EXISTS embeddings_node_idx ON embeddings(node_id);` in place — it still serves per-node lookups via `GetByNodeID`.

- [ ] **Step 3: Update `Upsert` ON CONFLICT target**

In `internal/index/embedding_repo.go:47-57`, change:

```sql
INSERT INTO embeddings (node_id, chunk_idx, model, content_hash, vector, dim, body)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(node_id) DO UPDATE SET
    chunk_idx    = excluded.chunk_idx,
    ...
```

to:

```sql
INSERT INTO embeddings (node_id, chunk_idx, model, content_hash, vector, dim, body)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(node_id, chunk_idx) DO UPDATE SET
    model        = excluded.model,
    content_hash = excluded.content_hash,
    vector       = excluded.vector,
    dim          = excluded.dim,
    body         = excluded.body
```

Drop `chunk_idx = excluded.chunk_idx` from the update clause — it is now part of the conflict target and unchangeable per row. Update the comment at lines 41-46 to describe the corrected semantic: one row per `(node_id, chunk_idx)`; updates land on the matching chunk.

- [ ] **Step 4: Bump `SchemaVersion`**

In `internal/index/schema_version.go`, update the constant to a value that names the change:

```go
const SchemaVersion = "2026-05-26-embeddings-per-chunk"
```

(Use a date stamp matching the implementation day if 2026-05-26 has passed.) Phase 2 already ended on `2026-05-25-nodes-tightened`; Phase 3 will bump to `2026-XX-XX-edges-tightened`; this bump rides at the tail. `OpenOrRebuild` handles the rebuild transparently.

- [ ] **Step 5: Re-run the failing test from Step 1**

Run: `go test ./internal/index/... -run TestEmbeddingRepo_Upsert_AllowsMultipleChunksPerNode -v`

Expected: PASS.

- [ ] **Step 6: Update the regression-pinning test in `drain_test.go`**

In `internal/embed/drain_test.go:330-339` (inside `TestDrainQueue_EmbedsEveryChunkOfMultiChunkNode`), replace the comment block and the "want 1" assertion with the corrected expectation:

```go
// embeddings now stores one row per (node_id, chunk_idx); a multi-chunk
// node persists one row per chunk so embeddingsMatch can short-circuit
// re-embed on unchanged content.
if len(rows) != stub.calls {
    test.Errorf("persisted rows = %d, want %d (one per chunk)", len(rows), stub.calls)
}
```

Delete the entire `// P2 migration: ...` comment block at lines 330-336.

- [ ] **Step 7: Clean up the stale comment in `embedding_repo_test.go`**

In `internal/index/embedding_repo_test.go:235-241` (inside `TestEmbeddingRepo_Stats_Aggregates`), delete the multi-line comment block that frames `UNIQUE(node_id)` as the intended P2 behavior and references a future "Task 4". The test fixture and assertions (`TotalChunks == 4`, `MaxChunks == 1`) remain correct under the new schema — the test inserts 4 single-chunk nodes, which persists 4 rows under either schema — so no other lines change.

Update the assertion message at line 278 from `"want 4 (one per node under UNIQUE(node_id))"` to `"want 4"` to remove the now-obsolete schema reference.

- [ ] **Step 8: Verify the hash-skip now fires**

Add or extend `TestDrainQueue_SkipsEmbedWhenContentUnchanged` to cover a multi-chunk node:

```go
func TestDrainQueue_SkipsEmbedWhenContentUnchanged_MultiChunk(test *testing.T) {
    // Setup: a node whose body chunks into >= 2 pieces under the configured chunker.
    // First DrainQueue pass embeds every chunk; stub.calls increases by N.
    // Re-enqueue the same node without changing content.
    // Second DrainQueue pass must NOT call the embedder again.
    // stub.calls after second pass == stub.calls after first pass.
}
```

This is the contract that the schema fix enables; lock it in.

- [ ] **Step 9: Run the full workspace suite**

Run: `make test`

Expected: clean. The pre-commit hook will run this too (per `project_lefthook_pre_commit_runs_full_tests`); the suite must be green before commit.

- [ ] **Step 10: `make vet` and `make lint`**

Expected: clean.

- [ ] **Step 11: Manual smoke (optional but recommended)**

Against the dogfood vault:

1. Delete `.tusk/index.db*` to force a fresh start.
2. Run `./bin/tusk reindex` and note the Ollama `POST /api/embeddings` count from `~/.ollama/logs/server.log`.
3. Run `./bin/tusk reindex` a second time without modifying any file.
4. The second pass should produce **zero** embed POSTs.

This mirrors the verification claim in commit `0b6f11a` (which only achieved zero-on-second-pass for single-chunk nodes; this fix extends that to multi-chunk).

- [ ] **Step 12: Commit**

```
git add internal/index internal/embed
git commit -m "fix(index): embeddings UNIQUE(node_id, chunk_idx) so hash-skip can fire"
```

- [ ] **Step 13: Open the PR**

```
gh pr create --title "fix(index): embeddings UNIQUE(node_id, chunk_idx) so hash-skip can fire" --body "$(cat <<'EOF'
## Summary
- Replaces `UNIQUE(node_id)` with `UNIQUE(node_id, chunk_idx)` on the embeddings table
- Fixes `Upsert` ON CONFLICT target to match
- Bumps `SchemaVersion` so `OpenOrRebuild` rebuilds existing indexes
- Replaces two tests that pinned the previous shape as expected behavior

## Why
The previous shape forced all chunks of a multi-chunk node onto a single row (last-chunk-wins). The `embeddingsMatch` hash-skip in `internal/embed/drain.go` requires `len(existing) == len(newHashes)`, which could never hold for a multi-chunk node, so every reindex pass re-embedded every chunk. Combined with the MCP watcher firing reindex per fs event, this produced a sustained runaway embed loop against Ollama (~30 req/s on a 21-node vault).

## Test plan
- [ ] `make test` clean
- [ ] `make vet && make lint` clean
- [ ] Manual: two consecutive `tusk reindex` runs against an unchanged vault produce zero embed POSTs on the second run (multi-chunk nodes included)
EOF
)"
```

## User-Visible Behavior

**Preserved:**

- No CLI flag, MCP wire format, manifest grammar, or markdown file format change.
- `tusk query` and `tusk mcp` retrieval continue to function over the same node set; no node disappears from search.
- `tusk reindex` continues to be idempotent on unchanged content — and now actually skips re-embedding for multi-chunk nodes, which it was supposed to do per `0b6f11a`'s claim.

**Intentionally changed:**

- Semantic retrieval rank order for multi-chunk nodes may differ post-rebuild. Pre-fix, the only embedding searchable per multi-chunk node was the *last* chunk's vector (everything else was overwritten by the `ON CONFLICT(node_id)` upsert); post-fix, every chunk is searchable. This is a quality improvement, not a regression, but any snapshot test that captured pre-fix nearest-neighbor IDs will surface a diff. The implementer should expect that and treat such diffs as expected outcomes of the fix, not as bugs.
- `tusk doctor` aggregate `TotalChunks` will rise for any workspace with multi-chunk file rows; the previous count was an artifact of the broken shape, not accurate storage.

## Out of scope

- The MCP watcher's full-tree reindex per fs event is a separate inefficiency (`internal/mcp/watch.go:21-57`); partial reindex is deferred to "Plan 8" of the v1 design. With the hash-skip now working, the watcher's repeated reindex passes become near-no-ops, so this task removes the urgency around that work without addressing it directly.
- An alternative design where file rows embed only a whole-body single chunk (and granularity comes exclusively from sub-units) is *not* pursued here. It would preserve `UNIQUE(node_id)` and avoid the schema bump, but it would also drop chunked retrieval for any vault without sub-unit nodes — including the user's current dogfood vault. Revisit if/when sub-unit coverage is universal.

## Done when

- New per-`(node_id, chunk_idx)` unique constraint live in the DDL.
- `Upsert` writes one row per chunk; no collisions across chunks.
- `embeddingsMatch` short-circuit verified by passing test against a multi-chunk node.
- `SchemaVersion` bumped; existing indexes rebuild transparently on first run.
- Two regression-pinning tests rewritten or removed.
- Workspace suite green; `make vet && make lint` clean.
- PR open.
