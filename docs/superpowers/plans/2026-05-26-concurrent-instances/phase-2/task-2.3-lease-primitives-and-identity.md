# Task 2.3 — Lease primitive + worker identity

**Phase:** 2
**Spec:** `docs/superpowers/specs/2026-05-26-tusk-concurrent-instances.md`
**Plan:** `docs/superpowers/plans/2026-05-26-concurrent-instances/PLAN.md`
**Prereqs:** T2.1 landed. (T2.2 is not required — this task only adds
`FileStateRepo.Claim`/`Release` plus `WorkerID()`; the analogous
embed_queue claim lives in T3.1.)
**Ships as:** one PR.

## Execution Rules

1. **This task = one PR.**
2. **Build green, tests green, lint clean.**
3. **No production caller uses these primitives yet.** Phase 3 (embed
   queue) and Phase 4 (handlers) start consuming them. The PR ships the
   primitive and its unit tests only.
4. **No bridge code.** These are net-new helpers.

## Goal

Provide the lease primitive that Phase 3 and Phase 4 will build on:
atomic claim, release, and stale-temp cleanup against `file_state`. Plus
a stable per-process worker identity (a UUID generated at process start)
that lease holders use as `leased_by`.

The same primitive is used for `embed_queue` leases in Phase 3, but the
two tables have different commit semantics (file_state commits update
content_hash + clear pending_*; embed_queue commits delete the row), so
the helper is structured to share the claim/release lifecycle while
leaving table-specific commit logic to the caller.

## Scope

### Files to add

- `internal/index/lease.go` — the lease helpers. Two related APIs:
  1. `FileStateRepo.Claim(path, workerID, ttl) (*ClaimedLease, error)`
     - Returns an `ErrBusy` sentinel if no row exists or the row is
       leased by someone else.
     - On success, returns `*ClaimedLease` carrying the current
       `content_hash`, `pending_temp_path`, and `pending_hash` read from
       the row inside the same statement (via `RETURNING`).
     - If `pending_temp_path` is non-NULL on claim, the helper unlinks
       that file from disk (it's a crashed predecessor's temp) and
       clears the `pending_*` columns before returning. The unlink uses
       `os.Remove`; ENOENT is non-fatal.

  2. `FileStateRepo.Release(ctx ReleaseContext) error`
     - `ReleaseContext` carries the path, worker ID, and the commit
       outcome:
       - On success: new `content_hash`, `mtime_ns`, `size`, optional
         new `state`.
       - On abandon: keep `content_hash` / `mtime_ns` / `size` untouched.
     - Always clears `leased_by`, `leased_until_ns`, `pending_temp_path`,
       `pending_hash`.

  Stale-temp cleanup is intentionally fused into `Claim` (not a separate
  sweep). The spec § *Lease lifecycle* documents this design.

- `internal/index/worker_id.go` (or extend an existing place — implementer's
  call) — a process-wide worker identity:
  - Generated once at process startup using `crypto/rand` + a UUID
    encoding helper. The user's preferred dependency is whatever's
    already in `go.mod`; check first. If none, `crypto/rand` + base32 of
    16 bytes is sufficient.
  - Exposed via a package-level `WorkerID() string`. The identity is
    cached after first call; subsequent calls return the same value.
  - The identity is process-scoped, not workspace-scoped. Two MCP servers
    on the same workspace get different IDs.

- `internal/index/lease_test.go` — unit tests covering:
  - Claim succeeds against a fresh row with no lease.
  - Claim fails with `ErrBusy` when another worker holds an unexpired
    lease.
  - Claim succeeds when an expired lease exists (reclamation).
  - Claim cleans up a `pending_temp_path` set by a prior caller (write a
    real file in `t.TempDir()`, set the path, claim, assert the file is
    gone and `pending_temp_path` is NULL).
  - Release commits content_hash/mtime/size when the success path is
    taken.
  - Release on abandon path leaves content_hash/mtime/size alone.
  - Two `WorkerID()` calls in the same process return identical strings;
    `WorkerID()` after a re-exec returns a different string (simulated
    by resetting the cached value in a test helper).

### Files to modify

- None outside of `internal/index/`. Bringing the lease into production
  code is the job of Phase 3 and Phase 4.

## Verification

1. `make build`, `make test`, `make vet`, `make lint` all green.
2. The lease tests run quickly (each <50ms) — concurrent SQLite TempDir
   setups are fast.

## Out of Scope

- Lease-aware `EmbedQueueRepo.Drain` — that's T3.1.
- Lease-aware handlers — that's Phase 4 (T4.1 introduces the
  `WriteWithLease` helper that composes `Claim` + temp-file staging +
  rename + `Release`).
- TTL configuration plumbing — that's T3.2.
- Heartbeats or lease renewal — not part of the design. A worker either
  completes within its TTL or its lease expires.

## Notes for the Implementer

- The `Claim` query is the one shown in the spec § *Lease lifecycle*:
  `UPDATE file_state SET leased_by=?, leased_until_ns=? WHERE path=? AND
  (leased_by IS NULL OR leased_until_ns < ?) RETURNING …`. SQLite
  supports `RETURNING` since 3.35.
- The lease primitive must work for both `file_state` and `embed_queue`
  rows in spirit (same shape, atomic UPDATE … RETURNING), but the actual
  Go helpers in this task live on `FileStateRepo`. T3.1 introduces the
  analogous claim on `EmbedQueueRepo` directly; the two share design
  rather than code. If you find a clean way to share the SQL fragment
  (e.g., a small helper that takes table + lease-key columns), feel free
  — but don't over-abstract for two call sites.
- The cleanup of `pending_temp_path` must be the **only** path that
  removes stale temps. No startup sweep, no reindex sweep, nothing
  scanning `.tusk/staging/` for old files. The spec § *Temp file
  location* makes this explicit.
- `ErrBusy` should mirror the existing `lock.ErrBusy` shape
  (`internal/lock/lock.go:22`) for caller ergonomics.
