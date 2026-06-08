---
type: package
title: internal/indexepoch — index epoch sentinel
import-path: github.com/germanamz/tusk/internal/indexepoch
status: stable
---

# internal/indexepoch

Manages the `.tusk/epoch` sentinel: a monotonically increasing integer in a file separate from the index DB. A daemon reads it to detect that the index was reset and recreated even when its own DB handle has been orphaned by another process's delete-and-recreate. The package holds no process state.

## Public surface

- `Read(root string) (int64, error)` — current epoch for the workspace, or `0` when the sentinel file does not yet exist.
- `Bump(root string) (int64, error)` — increment by one and write atomically (temp file in the same directory + rename); returns the new value.
- `EpochFilename` — the sentinel file name inside `.tusk/` (`"epoch"`).

## Why it lives outside the DB

A process that resets the index deletes and recreates the SQLite file. Any sibling holding the old handle is now pointed at a ghost inode and cannot tell from its handle alone that anything changed. The epoch sentinel is a tiny side-channel that survives the delete-and-recreate: siblings poll it (the `RunEpochWatcher` tick) or react to an fsnotify event (the `RunIndexEpochFastWatcher` fast-path) and reopen when it advances beyond the value they last converged to. Detection needs no DB handle, so it works across the orphaning that motivates it.

## Notes

`Bump` writes via temp-file + rename, so a reset surfaces to a directory watcher as a CREATE/RENAME event for `.tusk/epoch` (the transient `epoch.tmp-*` files also fire events; watchers filter by base name). Callers serialize concurrent bumps with the workspace lock; absent that, `Bump` is last-writer-wins.
