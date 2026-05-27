# Task 4.1 — `node.WriteWithLease` helper

**Phase:** 4
**Spec:** `docs/superpowers/specs/2026-05-26-tusk-concurrent-instances.md`
**Plan:** `docs/superpowers/plans/2026-05-26-concurrent-instances/PLAN.md`
**Prereqs:** Phase 2 + Phase 3 complete (the helper takes a `time.Duration`
TTL parameter sourced from `leaseconfig.Resolve` at the call site).
**Ships as:** one PR.

## Execution Rules

1. **This task = one PR.**
2. **Build green, full test suite green, lint clean.**
3. **No handler is converted in this task.** T4.2-4.5 do that. This
   task ships the helper and its tests only. The build stays green
   because the helper is unused outside its own tests.
4. **No bridge code.**

## Goal

Provide a single helper that handlers call to perform a lease-protected,
atomically-renamed write to a workspace file. The helper composes:

- Claim the `file_state` lease on the target path.
- Read the current file contents (lease guarantees no in-tusk writer
  interleaves).
- Hand contents to a caller-provided mutator function that returns the
  new contents.
- Stage the new contents in `<root>/.tusk/staging/<uuid>`, recording the
  staged path and hash in `file_state.pending_temp_path` /
  `pending_hash`.
- `os.Rename` the staged file over the target.
- Release the lease, committing the new `content_hash`, `mtime_ns`,
  `size`.

Failure at any step releases the lease via the abandon path (no
content_hash update, no rename) and propagates the error to the caller.

## Scope

### Files to add

- `internal/node/write_with_lease.go` — the helper. It takes a context,
  the workspace root, the `FileStateRepo`, the worker ID, the lease TTL,
  a target path, and a caller-supplied mutator function.

  **Before claiming the lease**, the helper performs
  `INSERT INTO file_state(path, ...) VALUES (?, ...) ON CONFLICT(path)
  DO NOTHING` with a placeholder row (empty `content_hash`, zero
  `mtime_ns`/`size`, `state = 'live'`, `last_seen_gen = 0`). This is
  the **lazy-create** path: pre-existing nodes (created before Phase 4
  shipped) have no `file_state` row yet, and Phase 6's reindex-driven
  population hasn't landed. Without the lazy-create, `Claim` would
  return `ErrBusy` on every modify/delete against a pre-existing node.
  The `ON CONFLICT DO NOTHING` makes the insert a no-op when a row
  already exists (the common case once handlers have run at least
  once per path).

  The mutator receives the on-disk bytes and returns one of three
  outcomes:
  - **Replace** — new bytes + new hash; helper stages, renames, commits.
  - **Tombstone** — helper deletes the on-disk file and transitions the
    `file_state` row to `state = 'tombstone'` via
    `FileStateRepo.Tombstone` (soft delete; row stays in the table).
  - **NoChange** — helper skips the rename and releases the lease
    without touching content_hash / mtime.
  The helper returns the propagated error from any step (claim, read,
  mutator, stage, rename, release). Match the project's existing
  helper-style and parameter ordering; do not invent a new convention.
  The three outcomes are non-negotiable — each maps to a real
  WRITE-BOTH handler shape (Modify → Replace+NoChange, Delete →
  Tombstone, Move → handled directly per T4.4 not via this helper).

- `internal/node/write_with_lease_test.go` — unit tests in `t.TempDir()`
  covering:
  - Happy path: WriteReplace ends with new content on disk, new hash in
    file_state, NULL lease columns.
  - WriteTombstone: file removed from disk, file_state row updated with
    `state = 'tombstone'` (soft delete; row remains).
  - WriteNoChange: file untouched, no staging file written, lease
    released cleanly.
  - Mutator returns error: file untouched, lease released via abandon
    path, error propagated, no staging file left behind.
  - Stale-temp recovery: pre-create a fake temp file and set
    `pending_temp_path` on the file_state row; call `WriteWithLease`;
    confirm the temp is unlinked before the write proceeds (this
    exercises T2.3's auto-cleanup path through the helper).
  - Lazy-create: call `WriteWithLease` against a path with **no**
    `file_state` row; confirm a row is inserted and the write
    succeeds. Then call again on the same path; confirm the second
    call updates (not duplicates) the existing row.
  - Concurrent calls in two goroutines targeting the same path
    serialize correctly (one waits for the other's lease). The
    lazy-create's `ON CONFLICT DO NOTHING` must not error when both
    callers race the placeholder insert.

### Files to modify

- None outside `internal/node/`. Handler conversions are T4.2-4.5.

### `.tusk/staging/` directory

- Created lazily on first staged write via `os.MkdirAll(stagingDir,
  0o755)`. Path is `filepath.Join(root, ".tusk", "staging")`.
- File naming: `<uuid>` (no extension). The implementer chooses how to
  produce the UUID — match the convention used by `index.WorkerID()` so
  there's only one UUID helper in the codebase.
- No cleanup at process startup. No periodic sweep. The only path that
  removes stale temps is the `Claim` auto-cleanup
  (T2.3's `FileStateRepo.Claim`).

## Verification

1. `make build`, `make test`, `make vet`, `make lint`, `make fmt` all
   green.
2. After a test run, `t.TempDir()` cleanup catches any leaked staging
   files; if a test leaves files behind, fix the helper.

## Out of Scope

- Hooking the helper into any handler — that's T4.2-4.5.
- Removing the workspace flock — that's Phase 5.
- A retry-on-busy policy — the helper propagates `ErrBusy` from `Claim`
  back to the caller; handler conversions in T4.2-4.5 decide whether to
  wait/retry.

## Notes for the Implementer

- The Mutator returning `WriteNoChange` is important for `node_modify`:
  if the user sets a property to its current value, the helper should
  not stage or rename anything. Skipping the rename also avoids touching
  `mtime_ns`, which would falsely trigger the watcher.
- The lease is held across the entire flow including the rename. This
  is intentional — the spec § *Lease lifecycle* explains why. Do not
  release before the rename completes.
- For `WriteTombstone`, the helper deletes the on-disk file, then
  releases the lease with a state transition to `'tombstone'`. T2.1
  fixes this as a soft delete via `FileStateRepo.Tombstone` — the row
  stays in the table as an audit record.
- The helper should accept a `context.Context` and abort cleanly if the
  context is cancelled before the rename. If cancelled after the
  rename, the rename has committed — do not try to undo it; just
  release the lease and return ctx.Err().
- Do not introduce any retries inside the helper. If `Claim` returns
  `ErrBusy`, propagate it. Handlers decide what to do (wait, error).
- The helper handles the "no row yet" case itself via the lazy-create
  `INSERT … ON CONFLICT DO NOTHING` described above. Callers do not
  need to pre-insert placeholder rows. This means modify and delete
  succeed against pre-existing nodes whose `file_state` row hasn't
  been populated yet (the common state after Phase 4 first ships and
  before Phase 6's reindex has run). Node-create still does its own
  existence check inside the mutator (`current` empty → ok, else
  error). Node-move (T4.4) uses lease primitives directly and applies
  the same lazy-create pattern on the destination path.
