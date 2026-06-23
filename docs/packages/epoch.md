---
type: package
title: internal/epoch — index & manifest epoch sentinels
import-path: github.com/germanamz/tusk/internal/epoch
status: stable
---

# internal/epoch

Manages the `.tusk` epoch sentinels: monotonically increasing integers in files separate from the index DB and the in-memory manifest. A daemon reads a sentinel to detect that a sibling reset the index (or hot-reloaded the manifest) even when its own DB handle / in-memory schema has been orphaned by another process's delete-and-recreate. The package holds no process state.

Two sentinels share this machinery, distinguished only by filename:

- `Index` (`.tusk/epoch`) — bumped on index reset.
- `Manifest` (`.tusk/manifest-epoch`) — bumped on manifest reload.

This package replaces the former `internal/indexepoch` and `internal/manifestepoch`, which were byte-identical modulo the sentinel filename. They were originally kept split as intentional WET to keep the index-reset and manifest-reload convergence paths from interfering. Threading the filename through a typed handle removes the byte-copy without re-coupling the two paths: each sentinel is a distinct `Epoch` value with its own file, so a manifest-validation failure still never masquerades as an index-epoch bump and a reset's index-epoch bump still never forces a schema reload.

## Public surface

- `Epoch` — a typed handle over one sentinel file.
- `Index` / `Manifest` — the two package-level handles.
- `IndexEpochFile` (`"epoch"`) / `ManifestEpochFile` (`"manifest-epoch"`) — the sentinel base names inside `.tusk/`.
- `(Epoch) Read(root string) (int64, error)` — current epoch for the workspace, or `0` when the sentinel file does not yet exist.
- `(Epoch) Bump(root string) (int64, error)` — increment by one and write atomically (temp file in the same directory + rename); returns the new value.
- `(Epoch) Filename() string` — the sentinel's base name inside `.tusk/`; used by the fsnotify fast-watchers to filter events.

The two sentinel filenames and the on-disk format (a single decimal integer + newline) are a wire contract with sibling daemons; do not change them.

## Why it lives outside the DB / schema

A process that resets the index deletes and recreates the SQLite file. Any sibling holding the old handle is now pointed at a ghost inode and cannot tell from its handle alone that anything changed. Likewise, a process that reloads the manifest validates and swaps a fresh schema in memory; a sibling cannot tell from its own in-memory schema that anything changed. Each epoch sentinel is a tiny side-channel that survives the swap: siblings poll it (the tick watcher) or react to an fsnotify event (the fast-path) and converge when it advances beyond the value they last saw. Detection needs neither the DB handle nor the schema pointer, so it works across the orphaning that motivates it.

## How it drives convergence

The originating reset (the `tusk_reset` MCP tool or `tusk reset` CLI) or reload (the `tusk_reload` MCP tool or `tusk reload` CLI) performs its swap and `Bump`s the relevant sentinel. Sibling daemons detect the advance — via the 2s tick watcher (`RunEpochWatcher` / `RunManifestEpochWatcher`) or the fsnotify fast-path (`RunIndexEpochFastWatcher` / `RunManifestEpochFastWatcher`) — and converge: an index bump reopens the handle, a manifest bump reloads the schema only (never reindexes; the originator owns that). A daemon advances its seen value only after a successful converge, so a transiently-invalid (half-written) `tusk.toml` is retried on the next tick rather than consuming the bump. The watcher scaffolding (tick + fsnotify setup, filename filter, dispatch loop) is shared across both sentinels; the asymmetric sibling-converge bodies stay separate. See [`internal/mcp`](mcp.md) for the watcher and convergence wiring.

## Notes

`Bump` writes via temp-file + rename, so an event surfaces to a directory watcher as a CREATE/RENAME for the sentinel (the transient `*.tmp-*` files also fire events; watchers filter by base name via `Filename()`). Callers serialize concurrent bumps with the workspace lock; absent that, `Bump` is last-writer-wins.
