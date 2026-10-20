---
type: package
title: internal/reset — destructive index reset
import-path: github.com/germanamz/tusk/internal/reset
status: stable
---

# internal/reset

Destructive "drop the index and rebuild" core, independent of the CLI or MCP surface. Both `tusk reset` and the `tusk_reset` MCP tool delegate the dangerous part — emptying the SQLite index — to this package so the ordering guarantee lives in one place. The markdown files are the source of truth, so the only thing destroyed is the cache.

## Public surface

- `Perform(ctx, Config) (*Result, error)` — one-shot convenience: acquire the workspace flock, run the core, release. Used by the CLI, which has no live in-process readers.
- `AcquireLock(ctx, root, ttl) (*lock.WorkspaceLock, error)` — acquire the flock alone. A live MCP daemon calls this so readers keep being served during the (possibly contended) flock-await, then takes its brief runtime write-lock only around the swap.
- `PerformLocked(Config) (*Result, error)` — the core, assuming the flock is already held (via `AcquireLock`); it neither acquires nor releases the flock.
- `Config{Root, IndexPath, LockTTL, Quiesce, Reopen}` and `Result{Epoch, DeletedArtifacts, Store}`.

## Ordering guarantee

`PerformLocked` runs under the workspace lock and guarantees:

1. **Quiesce** the caller's handle (`Config.Quiesce`; nil = no-op) — stop/close anything holding the old DB.
2. **Empty** the index in place via `index.Recreate`: it bootstraps an empty DB beside the old one and copies it into the existing file through SQLite's backup API, falling back to renaming it over the old file when the old one can't be opened. A tusk process that doesn't take the flock (`tusk status`, say) reads the old index or the empty one instead of racing the reset to create the file (#839).
3. **Reap** the `.tusk/staging` directory.
4. **Reopen** a fresh handle (`Config.Reopen`, caller hook) and rebuild the caller's repos.
5. **Bump** the `.tusk/epoch` sentinel so sibling daemons converge onto the fresh DB.

On any error before the epoch bump, the epoch is left untouched so siblings are not signaled toward a half-built index.

## Notes

Reindex is the caller's responsibility — `reset` repopulates nothing. The Async-vs-blocking choice is surface-specific (the CLI reindexes synchronously; the MCP tool kicks an Async walk to keep the lock hold short), so it lives outside this package.
