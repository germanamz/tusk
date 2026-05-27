# Concurrent Tusk Instances — Design

**Status:** Draft **Date:** 2026-05-26 **Author:** German Meza (with Claude
Code)

## Summary

Allow multiple Tusk MCP server instances to run concurrently on the same
workspace. Replace the runtime-lifetime workspace flock with a per-file lease in
a dedicated `file_state` table and a per-job lease on `embed_queue`. Both leases
are TTL-based, claimed atomically via `UPDATE … RETURNING`, and reclaimed on
expiry without operator intervention. SQLite WAL plus `busy_timeout` already
handles DB-level concurrency; the leases add the FS-level coordination that
today's flock provides too coarsely.

Reindex stops being a single critical section: it becomes per-file work that any
instance can drain, naturally serialized against ongoing writes by the same file
lease.

This is an incompatible index schema change. Tusk's index is deterministic and
rebuildable from source files, so indexes written by prior releases are simply
dropped on first run after upgrade and reconstructed via the existing `reindex`
pipeline. The cost is a one-time full reindex per workspace.

## Background

Tusk currently holds an exclusive OS-level flock at `<root>/.tusk/lock`
(`internal/lock/lock.go`) for the entire lifetime of the MCP server
(`internal/mcp/runtime.go:96-107`). A second `tusk mcp serve` on the same
workspace fails to start. CLI mutations briefly contend with the running MCP
server for the same lock.

The underlying storage layer is already concurrency-friendly:
- SQLite is opened in WAL mode with a 5s busy timeout
(`internal/index/index.go:144`).
- `embed_queue` is persistent in SQLite (`internal/index/embed_queue_repo.go`)
and survives restarts.
- Embeddings have `UNIQUE(node_id, chunk_idx)`; `DrainQueue` already
hash-compares per chunk and skips work where content is unchanged
(`internal/embed/drain.go:53-73`).

The lock is the only thing preventing multiple instances. It was designed to
protect a "single-writer, multi-reader" model, but in practice the MCP server
sits on the *write* lock the whole time even when only serving reads. As more
agents target the same vault, this serialization becomes the bottleneck.

## Problems

**P1 — One MCP instance per workspace.** Multiple agents working a single vault
must funnel every request through one process. Read parallelism is bounded by
that process's goroutine pool, not by the underlying storage.

**P2 — Reindex blocks everything.** While reindex runs, no other mutation can
begin (and no second instance can start at all). Large vault reindexes can take
minutes and use a lot of resources.

**P3 — CLI ↔ MCP contention.** Running `tusk node create` from a shell while an
MCP server is up requires the MCP server to release the lock between operations,
which it doesn't — the CLI has to wait, retry, or give up.

**P4 — Reindex orphan-reap race (latent today, blocking under concurrency).**
Reindex builds an in-memory `seenPaths` set (`internal/reindex/reindex.go:106`),
then post-walk deletes any node row whose path isn't in the set
(`reindex.go:417-439`). Under concurrency, a node created between walk-start and
reap-time would be deleted from the index immediately after creation. Even
today, this is a real race when the MCP server runs reindex while a `watch` is
also firing.

## Design

### `file_state` — per-file coordination table

A new SQLite table separate from `nodes` and `edges`. Its only responsibility is
concurrency coordination and crash recovery for FS writes. The graph schema
stays untouched.

```sql
CREATE TABLE file_state (
  path              TEXT PRIMARY KEY,
  content_hash      TEXT NOT NULL,            -- last hash we wrote or observed
  mtime_ns          INTEGER NOT NULL,
  size              INTEGER NOT NULL,
  state             TEXT NOT NULL,            -- 'live' | 'tombstone'
  leased_by         TEXT,                     -- worker id, NULL = unleased
  leased_until_ns   INTEGER,                  -- absolute expiry, NULL = unleased
  pending_temp_path TEXT,                     -- in-flight write target, NULL = none
  pending_hash      TEXT,                     -- hash of content being staged, NULL = none
  last_seen_gen     INTEGER NOT NULL DEFAULT 0, -- reindex generation
  updated_at_ns     INTEGER NOT NULL
);

CREATE INDEX idx_file_state_lease
  ON file_state(leased_until_ns)
  WHERE leased_by IS NOT NULL;

CREATE INDEX idx_file_state_seen
  ON file_state(last_seen_gen);
```

`content_hash` is record-keeping for the watcher's dedup, not a CAS token. The
lease provides exclusion; no optimistic-concurrency check is needed on top.

### Lease lifecycle

Atomic claim (single statement):

```sql
UPDATE file_state
SET    leased_by       = :worker_id,
       leased_until_ns = :now + :ttl,
       updated_at_ns   = :now
WHERE  path = :path
  AND  (leased_by IS NULL OR leased_until_ns < :now)
RETURNING pending_temp_path, pending_hash, content_hash;
```

If the `RETURNING` block is empty, somebody else holds an unexpired lease —
caller waits with backoff (or returns busy to the agent for interactive tools;
TBD per handler).

On claim success, the helper inspects `pending_temp_path`. If non-NULL, a
previous lessee crashed mid-write: unlink that file, clear `pending_temp_path`
and `pending_hash`. This is the only temp-cleanup path. There is no separate
sweep pass.

Release (commit path):

```sql
UPDATE file_state
SET    content_hash      = :new_hash,
       mtime_ns          = :new_mtime,
       size              = :new_size,
       pending_temp_path = NULL,
       pending_hash      = NULL,
       leased_by         = NULL,
       leased_until_ns   = NULL,
       updated_at_ns     = :now
WHERE  path = :path AND leased_by = :worker_id;
```

Release (abandon path, e.g., handler error before rename): clear
`leased_by`/`leased_until_ns`/`pending_temp_path`/`pending_hash` without
touching `content_hash` or `mtime_ns`. The unlinked temp file goes with it.

### Temp file location

Staged writes land at `<root>/.tusk/staging/<uuid>`. Justifications:

1. No pollution of the markdown vault.
2. Same filesystem as the destination (`.tusk/` is always a subdirectory of the
   workspace root), so `os.Rename` is atomic.
3. Already gitignored as part of `.tusk/`.
4. Bounded emergency-cleanup blast radius: `rm -rf .tusk/staging/` is safe by
   construction.
5. `file_state.pending_temp_path` records the exact path; no directory scans, no
   glob.

### Write flow (`node_modify`, `node_create`, `node_move`, `node_delete`)

```
1. claim lease on file_state[path]            (auto-cleans stale temp)
2. read file from disk (lease guarantees freshness)
3. apply mutation (frontmatter delta or move/delete)
4. UPDATE file_state SET pending_temp_path = ?, pending_hash = ?
5. write new content to .tusk/staging/<uuid>
6. os.Rename(temp, target)
7. UPDATE file_state SET content_hash = pending_hash,
                         pending_temp_path = NULL,
                         pending_hash = NULL,
                         leased_by = NULL,
                         leased_until_ns = NULL
8. update nodes / edges / embed_queue in normal transactions
```

For `node_modify` specifically: scope is **frontmatter only** by design. Body
changes are out-of-band FS writes caught by the existing `watch`, debounced, and
reindexed. The current handler still accepts an optional `body` parameter
(`internal/mcp/tools.go:1032`); see *Precondition: Drop `body` from
`node_modify`*.

### Embed queue lease

`embed_queue` gets the same lease columns plus a `kind` discriminator so the
same queue can carry both embed jobs and per-file reindex jobs (see *Reindex
as parallel queue work*). Treating it as one job queue with a `kind` column
keeps the lease primitive single-purpose and avoids a parallel
`reindex_queue` table.

```sql
ALTER TABLE embed_queue ADD COLUMN leased_by TEXT;
ALTER TABLE embed_queue ADD COLUMN leased_until_ns INTEGER;
ALTER TABLE embed_queue ADD COLUMN lease_started_at_ns INTEGER;
ALTER TABLE embed_queue ADD COLUMN kind TEXT NOT NULL DEFAULT 'embed';
```

Workers filter by `kind` when claiming. Existing enqueue paths produce
`kind = 'embed'` (the default); reindex enqueues produce `kind = 'reindex'`.
The table may be renamed to `job_queue` in a future cleanup; that rename is
out of scope for this design.

`Drain` changes from "select + delete in one transaction" to "claim lease
atomically":

```sql
UPDATE embed_queue
SET    leased_by       = :worker_id,
       leased_until_ns = :now + :ttl
WHERE  node_id IN (
         SELECT node_id FROM embed_queue
         WHERE leased_by IS NULL OR leased_until_ns < :now
         ORDER BY enqueued_at ASC
         LIMIT :batch_size
       )
RETURNING node_id, enqueued_at, attempts, last_error;
```

On success: process embeddings, then `DELETE FROM embed_queue WHERE node_id = ?
AND leased_by = ?`. On failure: `UPDATE … SET leased_by = NULL, leased_until_ns
= NULL, attempts = attempts + 1, last_error = ?` — the row returns to the pool
for the next worker or the same worker after backoff. On crash: lease expires,
the next `Drain` reclaims it. Idempotency is preserved by the existing
`(node_id, chunk_idx)` UNIQUE constraint and hash comparison in `DrainQueue`.

### Reindex as parallel queue work

Reindex stops being a single big function. The walk produces per-file jobs that
any worker can drain.

The walk itself:
- Bumps a global `reindex_gen` counter (stored in `meta`).
- For each file encountered, takes the `file_state` lease briefly, upserts
`file_state` row with new hash/mtime/size, sets `last_seen_gen = current_gen`,
releases.
- Enqueues a reindex job per file (in `embed_queue` or a sibling `reindex_queue`
table — TBD; see *Open Items*).

Reap is generation-based and re-stats:
- Find candidates: `SELECT path FROM file_state WHERE last_seen_gen <
current_gen`.
- For each candidate: claim lease → `os.Stat` confirms missing → transition to
`tombstone` → cascade delete from `nodes`/`edges`.
- Two concurrent reindex walks no longer race: each file's `last_seen_gen` is
updated by whichever walk reached it most recently; both walks see consistent
post-walk state.

Reindex coalescing is no longer a *correctness* requirement. Coalescing may
still be desirable as an efficiency optimization (don't walk twice in 10
seconds) but is out of scope for this spec.

### Workspace lock removal

`internal/mcp/runtime.go:96-107` no longer acquires the workspace lock at
startup. CLI mutation paths (`internal/cli/cmd_node*.go`, `cmd_edge*.go`,
`cmd_reindex.go`, `cmd_watch.go`, `cmd_doctor.go`) stop acquiring it for normal
operations and instead use `file_state` leases for the files they touch.

The `internal/lock` package is retained for **schema migrations only** —
migrations run at most once per index version and benefit from a workspace-wide
exclusion. This is documented in `internal/lock/lock.go`'s package comment.

### Worker configuration

Each MCP instance runs an embed-worker pool by default. Pool size resolution,
highest precedence first:
1. `TUSK_EMBED_WORKERS` env var.
2. `embed.workers` in manifest TOML.
3. Default: `max(1, runtime.NumCPU() / 2)`.

Setting either to `0` opts the instance out of embed work entirely. **The watch
is also disabled** in this mode — an instance that cannot drain the queue should
not be producing work for it either. The instance becomes a pure read-server:
it answers queries, accepts mutations (which still enqueue via the normal write
flow), but does not observe FS changes and does not embed.

The MCP server emits a `WARN` log line on startup explaining the consequence:

```
WARN embed workers disabled; watch is also disabled in this instance.
     Ensure another instance (or scheduled `tusk reindex`) drives indexing
     for this workspace, otherwise the index will go stale.
```

Operators are responsible for ensuring at least one instance, or a scheduled
`tusk reindex`, keeps the index fresh. The MCP server does not attempt to
detect or coordinate this across instances.

### Lease TTL

Default: 60 seconds. Configurable via `lease.ttl_seconds` in manifest TOML; env
override `TUSK_LEASE_TTL_SECONDS`. Applies to both `file_state` and
`embed_queue` leases (separate keys if they diverge in practice — TBD; see *Open
Items*).

TTL is the only crash-recovery primitive. There are no heartbeats and no
liveness probes. A worker either completes within its TTL or its lease expires
and another worker takes over. Embedding large nodes that genuinely take longer
than the TTL must either (a) operate in chunked sub-jobs, each within TTL, or
(b) bump the configured TTL up.

### Optimistic concurrency

**Not adopted.** Earlier iterations considered an `(node_id, content_hash)` CAS
on commit. Once we narrowed `node_modify` to frontmatter-only and accepted that
body changes are out-of-band FS writes caught by `watch`, the only remaining
race is an agent writing the file body in the microseconds between tusk's
in-lease read and rename. That race exists for any "two writers to one file"
architecture and is not solvable at the application layer.

The lease prevents all *tusk-mediated* races on a per-file basis. That's
sufficient.

## Precondition: Drop `body` from `node_modify`

`tusk_node_modify` currently accepts an optional `body` parameter
(`internal/mcp/tools.go:1032`); `node.ModifyInput` carries it through
(`internal/node/service.go:37`) and the implementation replaces the body when
the field is non-nil (`internal/node/service.go:389-392`). The CLI command
already omits this surface (`cmd/tusk/cmd_node_modify.go` only exposes
`--prop` and `--unset`).

This spec's frontmatter-only invariant requires removing the body path from
`node_modify` entirely. Concrete scope:

- **MCP:** drop the `body` parameter from `tusk_node_modify` tool registration
  (`internal/mcp/tools.go:1032`) and the body assignment in the handler
  (`internal/mcp/tools.go:1051-1054`).
- **Service:** drop the `Body *[]byte` field from `node.ModifyInput` and the
  conditional body-replacement block in `Service.Modify`. The body always
  comes from `parsed.Body` (the on-disk content).
- **Help text:** update the MCP tool description to make clear that body
  changes go through direct FS writes plus the watch.
- **Tests:** no test passes `ModifyInput.Body` today, so no test churn is
  expected — verify before landing.

Land this as its own commit ahead of the concurrency work. It is a strict
prerequisite: without it, the lease design has a body-rewrite race against
out-of-band FS writers that this spec explicitly does not address.

## Upgrade

No in-place migration. Tusk's index is deterministic and rebuildable from the
markdown vault, so the upgrade path is to drop the existing index and let
`reindex` reconstruct it under the new schema.

On first open after upgrade:
1. Detect the schema version mismatch.
2. Drop the existing `.tusk/` index (or move it aside as a backup, TBD).
3. Run a full `reindex` against the vault. `file_state` rows are created during
   the walk; `embed_queue` populates as usual with lease columns present from
   the start.

The rebuild runs under the workspace flock (the one remaining legitimate use of
the flock) so a partially-rebuilt index can never be observed by a concurrent
instance.

## Out of Scope

- Cross-machine coordination. `.tusk/` remains per-machine. Vault sync is still
git's job.
- Distributed embedding (workers on different hosts). Workers must share the
same SQLite file; that means same filesystem, same machine.
- Reindex coalescing across instances. Generation-based reap makes concurrent
walks safe; deduplicating them is an efficiency optimization for a later spec.
- Replacing the workspace flock for schema migrations. The flock is the right
primitive for that rare, brief, system-wide event.

## Validation

The implementation is correct when:
- Two MCP server instances start successfully against the same workspace.
- Concurrent `node_modify` calls to *different* files run in parallel
(verifiable by latency under load).
- Concurrent `node_modify` calls to the *same* file serialize via the lease and
both changes land.
- A worker killed mid-write leaves a `pending_temp_path` row; the next claimant
of that file cleans the temp and succeeds.
- A worker killed mid-embed leaves a leased `embed_queue` row; after TTL another
worker reclaims and re-embeds correctly (hash skip means no duplicate work if
the first worker actually wrote any rows).
- A reindex started during ongoing mutations does not delete newly-created
nodes.
- Two concurrent reindex walks complete without false orphan deletions.
- An instance configured with `TUSK_EMBED_WORKERS=0` serves reads and mutations
but never claims from `embed_queue`; another instance drains as normal.

## References

- Current lock: `internal/lock/lock.go`
- Current MCP startup: `internal/mcp/runtime.go:96-107`
- Embed queue: `internal/index/embed_queue_repo.go`
- Embed drain: `internal/embed/drain.go`
- Reindex walk + reap: `internal/reindex/reindex.go:106,407,417-439`
- SQLite WAL setup: `internal/index/index.go:144`
- Embeddings UNIQUE constraint: `internal/index/index.go:80`
- `node_modify` handler: `internal/mcp/tools.go:1026`
