# Task 6.2 — Generation-based reap with `os.Stat` confirmation

**Phase:** 6
**Spec:** `docs/superpowers/specs/2026-05-26-tusk-concurrent-instances.md`
**Plan:** `docs/superpowers/plans/2026-05-26-concurrent-instances/PLAN.md`
**Prereqs:** T6.1 landed.
**Ships as:** one PR.

## Execution Rules

1. **This task = one PR.**
2. **Build green, full test suite green, lint clean.**
3. **Removes the in-memory `seenPaths` set** from `reindex.go`. Reap
   logic moves to a query over `file_state` plus `os.Stat`
   confirmation.
4. **No schema changes. No bridge code.**

## Goal

Close the orphan-reap race (spec § *P4*) by replacing the in-memory
`seenPaths` reap with a generation-based query and an `os.Stat`
re-verification per candidate. Two concurrent walks no longer race on
deletion; a file created mid-walk no longer gets deleted by a stale
snapshot.

## Scope

### Files to modify

- `internal/reindex/reindex.go`
  - Delete the `seenPaths` map (line 106) and its writes (line 407).
  - Delete the post-walk reap loop (lines 417-439) that iterates
    `Repo.List()` and calls `DeleteByPath` / `DeleteBySource`.
  - Replace with a new reap pass:
    1. Query `file_state` for rows where `last_seen_gen < <this walk's
       gen>` and `state = 'live'`. Use
       `FileStateRepo.ListByGenLessThan` from T2.1.
    2. For each candidate, claim the lease (using the lease primitive
       from T2.3) to ensure no other process is mid-write to the same
       file. If `Claim` returns `ErrBusy`, **skip this candidate** and
       move on — the current lease holder is a live writer who will
       update `last_seen_gen` as part of its commit, so the next
       walk's reap will reconsider. Do not block-retry; do not fail
       the reap pass.
    3. Inside the lease, `os.Stat` the file. If it still exists, the
       file was created or modified by another process *after* our walk
       passed its directory — update `last_seen_gen` to current and
       release without deleting.
    4. If the file is genuinely missing, transition the `file_state`
       row to `'tombstone'` via `FileStateRepo.Tombstone` (soft delete
       per T2.1), then cascade-delete the node row via
       `NodeRepo.DeleteByPath` and edges via `EdgeRepo.DeleteBySource`.
    5. Release the lease.
  - Wrap candidate processing in a small loop with a configurable
    parallelism (default 1 — the per-file lease serializes against
    handlers anyway; parallelism here is for IO overlap, not
    contention reduction).

### Files to leave alone

- T6.1's walk + `last_seen_gen` update logic stays untouched.

### Tests

- Existing reindex tests pass after updating any assertion that
  depended on the old `seenPaths`-based reap timing.
- New test: walk completes; before reap, an external goroutine creates
  a new file `notes/new.md` and Upserts its `file_state` row with the
  current gen via the handler path; reap does **not** delete it (the
  row's `last_seen_gen` is not less than the walk's gen).
- New test: walk completes; a file is removed from disk between walk
  and reap; reap deletes the `file_state` row and the node row.
- New test: walk completes; a file is removed from disk during the
  walk but **after** the walker passed it (so `last_seen_gen` was
  written); reap's `os.Stat` confirms missing; row is reaped. The race
  window narrows but does not close — document this in the test.
- New test: two concurrent walks produce two reap passes; each only
  considers rows below its own generation; no double-delete or
  false-delete.

## Verification

1. `make build`, `make test`, `make vet`, `make lint`, `make fmt` green.

## Out of Scope

- Per-file reindex jobs in the queue — T6.3.
- Worker drain of reindex jobs — T6.4.

## Notes for the Implementer

- The lease-on-reap is critical: without it, the reap could `os.Stat`
  while another process is mid-rename, see a transient absence, and
  delete a row that was about to be repopulated. Holding the lease
  serializes against any writer.
- A file may genuinely be `os.Stat`-missing during a brief rename
  window between `unlink(old)` and `link(new)`. The probability is
  vanishingly small with `os.Rename` (atomic), but if you want to be
  paranoid, gate deletion behind "missing on two consecutive stats
  inside the lease." For the first pass, single-stat is fine.
- Do not introduce a separate "tombstone" pass. Reap directly
  transitions the state and cascades the deletions.
