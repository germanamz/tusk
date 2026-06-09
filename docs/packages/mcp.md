---
type: package
title: internal/mcp — MCP server
import-path: github.com/germanamz/tusk/internal/mcp
status: stable
---

# internal/mcp

MCP server. Bundles the open workspace + index + services into a `Runtime`, registers a tool per CLI verb on the mcp-go core, and serves over stdio (default) or SSE. Runs the embed-queue drainer + fsnotify watcher in the same process so the index stays warm across tool calls.

## Public surface

- `Open(workspaceDir string) (*Runtime, error)` — long-lived bundle.
- `(*Runtime).Close()`.
- `NewServer(*Runtime) *Server` — wraps mcp-go.
- `(*Server).RunBackground(ctx) error` — starts the five background goroutines (gated on `Workers > 0`).
- Tools: `tusk_status`, `tusk_node_get`, `tusk_node_list`, `tusk_query`, `tusk_doctor`, `tusk_node_create`, `tusk_node_modify`, `tusk_node_move`, `tusk_node_delete`, `tusk_edge_add`, `tusk_edge_remove`, `tusk_reindex`, `tusk_reset`.

## Runtime-swap model

The `Runtime` (its open DB handle especially) is held behind an `RWMutex`. Read handlers take the read-lock for their whole body via the guarded `register`; the index-replacement ops take the write-lock briefly around the pointer swap. `snapshotRuntime` hands background goroutines (drainers, watchers) a pointer under a brief read-lock so a long Ollama-bound pass never blocks a swap. A second mutex, `resetMu`, serializes every replacement op in-process so their flock / write-lock acquisition orders cannot interleave into a deadlock.

- `reopenInPlace` — the non-destructive swap: open the new handle, swap, then close the old one (a failed open leaves the live handle installed, so the server keeps serving).
- `tusk_reset` — drops and rebuilds the index via `internal/reset`: `AcquireLock` (readers stay served during the flock-await), then the brief write-lock around `PerformLocked` + the swap, then an Async rebuild.
- `siblingReopen` / `maybeReopenForEpoch` — react to another process resetting the shared index. They await the resetter via the flock, swap to a fresh handle, and (if the resetter crashed mid-reset, leaving the file absent) become the recreator.

## Epoch watchers

A reset bumps the `.tusk/epoch` sentinel (`internal/indexepoch`). Two background goroutines drive convergence on it:

- `RunEpochWatcher` — the deterministic backstop: polls `.tusk/epoch` on a 2s tick and calls `maybeReopenForEpoch`.
- `RunIndexEpochFastWatcher` — the low-latency fast-path: an fsnotify watch on the `.tusk/` directory that fires on a change to the `epoch` sentinel and calls `maybeReopenForEpoch` immediately, so siblings converge in milliseconds rather than up to one tick. Both call the same convergence path; the fast-path is pure latency optimization.

The two epoch-watcher pairs (index + manifest, see below) always run, regardless of `Workers`. The resource-heavy passes — the embed/reindex drainers and the file watcher — stay gated on `Workers > 0`. So a read-only (`workers=0`) daemon still converges a sibling's index reset and manifest reload (convergence is a consistency property, not an indexing one); it just does no content indexing itself.

## Manifest reload

A `tusk reload` command or `tusk_reload` tool explicitly reloads the manifest (`tusk.toml`): it re-reads and validates the file, atomically swaps the in-memory schema (manifest, behavior engine, node service) reusing the open index handle, and kicks a reindex to re-validate indexed content against the new schema. Reload is **explicit-only** — the file watcher never auto-reloads on `tusk.toml` writes.

The originating process validates, swaps, bumps the `.tusk/manifest-epoch` sentinel (`internal/manifestepoch`), and owns the single reindex. Sibling daemons converge on the bump via their own watcher pair — `RunManifestEpochWatcher` (2s poll) and `RunManifestEpochFastWatcher` (fsnotify fast-path) — calling `maybeReloadManifestForEpoch` → `siblingReloadManifest`, which reloads the manifest **only** (never reindexes; the originator owns that). When a reset and a reload land in the same window, `siblingReopen` re-reads `manifest-epoch` under the same flock and installs the fresh manifest alongside the fresh index, so a sibling never serves the new index against the stale schema.

Validation matches boot semantics, so a hot reload reaches the same in-memory state a restart would: blocking on a TOML parse/structural error or a behavior-engine build failure (the swap is refused and the epoch is not bumped); non-blocking on dangling aliases / bad `[context]` entries (dropped and surfaced as warnings, exactly as boot does). `seenManifestEpoch` advances only after a successful swap, so a half-written `tusk.toml` is retried on the next tick rather than poisoning convergence.

The `tusk_reload` response carries the new `manifest_epoch`, a `diff` (added/removed node-types, edge-types, behaviors), the `reindex` report, and any `validation_errors` / `warnings`. The CLI `tusk reload` has no previous manifest to diff against, so it prints the loaded schema summary instead; `--reindex` runs a synchronous reindex for the no-daemon case.

## Notes

Workspace-config commands stay CLI-only in v1.c — `tusk pack add` has no MCP equivalent yet (carried as 7.c.1 §10 ledger #10). Structured warnings via stderr text-line parsing remains a v1 expediency (Plans 7, 7.b, 7.c.1 residuals).
