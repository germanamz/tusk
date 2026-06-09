---
type: package
title: internal/manifestepoch — manifest epoch sentinel
import-path: github.com/germanamz/tusk/internal/manifestepoch
status: stable
---

# internal/manifestepoch

Manages the `.tusk/manifest-epoch` sentinel: a monotonically increasing integer in a file separate from the index DB. A daemon reads it to detect that another process hot-reloaded the manifest (`tusk.toml`) and converge onto the new schema. The package holds no process state. It is a structural sibling of [`internal/indexepoch`](indexepoch.md) — same mechanics, a distinct sentinel.

## Public surface

- `Read(root string) (int64, error)` — current manifest epoch for the workspace, or `0` when the sentinel file does not yet exist.
- `Bump(root string) (int64, error)` — increment by one and write atomically (temp file in the same directory + rename); returns the new value.
- `ManifestEpochFilename` — the sentinel file name inside `.tusk/` (`"manifest-epoch"`).

## Why a separate sentinel from indexepoch

Index reset and manifest reload are independent events that must converge independently: a manifest-validation failure must never masquerade as an index-epoch bump, and a reset's index-epoch bump must never force a schema reload. Keeping `manifest-epoch` distinct from `.tusk/epoch` keeps the two convergence paths from interfering, and leaves the index-reset machinery (`internal/indexepoch`, shipped earlier) untouched. The two packages are near-identical by design rather than sharing a premature abstraction for two callers.

## How it drives convergence

The originating reload (the `tusk_reload` MCP tool or the `tusk reload` CLI) validates the fresh manifest, swaps the in-memory schema, and `Bump`s this sentinel. Sibling daemons detect the advance — via the `RunManifestEpochWatcher` 2s poll or the `RunManifestEpochFastWatcher` fsnotify fast-path — and reload their manifest only (never reindex; the originator owns that). A daemon advances its `seenManifestEpoch` only after a successful swap, so a transiently-invalid (half-written) `tusk.toml` is retried on the next tick rather than consuming the bump. See [`internal/mcp`](mcp.md) for the watcher and convergence wiring.

## Notes

`Bump` writes via temp-file + rename, so a reload surfaces to a directory watcher as a CREATE/RENAME event for `.tusk/manifest-epoch` (the transient `manifest-epoch.tmp-*` files also fire events; watchers filter by base name). Callers serialize concurrent bumps with the workspace lock; absent that, `Bump` is last-writer-wins.
