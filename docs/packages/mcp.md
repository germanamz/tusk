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
- `NewServer(*Runtime) *Server` — wraps mcp-go. The initialize response's instructions name the runtime's absolute root, `tusk.toml` and index paths rather than `./`, because under `--workspace` the server runs outside the vault. Clients carry the instructions and `tusk_query`'s definition on every agent turn, so both are terse shorthand written for a model and held under a byte budget (`TestPerTurnBudget`, #815). Guidance only writers need sits in the descriptions of the tools it concerns (`tusk_reload`, `tusk_reset`) and in the `workflow` help topic.
- `(*Server).RunBackground(ctx) error` — starts the five background goroutines (gated on `Workers > 0`).
- Tools: `tusk_status`, `tusk_node_get`, `tusk_node_list`, `tusk_query`, `tusk_doctor`, `tusk_node_create`, `tusk_node_modify`, `tusk_node_move`, `tusk_node_delete`, `tusk_edge_add`, `tusk_edge_remove`, `tusk_edge_list`, `tusk_reindex`, `tusk_reset`.
- Edge views (#848): `tusk_edge_list` and `tusk_node_get` fold a link's sub-unit copies onto one row from its file, with `units` naming the passages; `sub_units: true` returns the stored rows. `tusk_node_get` also takes `edge_type` and `direction` (`in` / `out`), and each of the three implies `include` edges. The new arguments are read with `argval`, so a value of the wrong JSON type is an error, not a silent default.
- Fields and counts (#851): `fields` projects the JSON rows of `tusk_query`, `tusk_node_list`, `tusk_node_get` and `tusk_run` through `query.ProjectRows`/`ProjectRow`, as it already did their compact output; an unknown name is an error. `tusk_query` takes `count` (boolean) and `group_by` (string), read with `argval`; a count returns the `query.CountResult` JSON, or `render.Counts` text when `format` is `"compact"`, and ignores the 50-row structural page.

## Runtime-swap model

The `Runtime` (its open DB handle especially) is held behind an `RWMutex`. Read handlers take the read-lock for their whole body via the guarded `register`; the index-replacement ops take the write-lock briefly around the pointer swap. `snapshotRuntime` hands background goroutines (drainers, watchers) a pointer under a brief read-lock so a long Ollama-bound pass never blocks a swap. A second mutex, `resetMu`, serializes every replacement op in-process so their flock / write-lock acquisition orders cannot interleave into a deadlock.

- `reopenInPlace` — the non-destructive swap: open the new handle, swap, then close the old one (a failed open leaves the live handle installed, so the server keeps serving).
- `tusk_reset` — drops and rebuilds the index via `internal/reset`: `AcquireLock` (readers stay served during the flock-await), then the brief write-lock around `PerformLocked` + the swap, then an Async rebuild.
- `siblingReopen` / `maybeReopenForEpoch` — react to another process resetting the shared index. They await the resetter via the flock, swap to a fresh handle, and become the recreator if the index file is absent (deleted by hand, or left missing by a reset from a tusk older than #839 that crashed mid-reset).

## Epoch watchers

A reset bumps the `.tusk/epoch` sentinel ([`internal/epoch`](epoch.md), `epoch.Index`). Two background goroutines drive convergence on it:

- `RunEpochWatcher` — the deterministic backstop: polls `.tusk/epoch` on a 2s tick and calls `maybeReopenForEpoch`.
- `RunIndexEpochFastWatcher` — the low-latency fast-path: an fsnotify watch on the `.tusk/` directory that fires on a change to the `epoch` sentinel and calls `maybeReopenForEpoch` immediately, so siblings converge in milliseconds rather than up to one tick. Both call the same convergence path; the fast-path is pure latency optimization.

The two epoch-watcher pairs (index + manifest, see below) always run, regardless of `Workers`. The resource-heavy passes — the embed/reindex drainers and the file watcher — stay gated on `Workers > 0`. So a read-only (`workers=0`) daemon still converges a sibling's index reset and manifest reload (convergence is a consistency property, not an indexing one); it just does no content indexing itself.

## Manifest reload

A `tusk reload` command or `tusk_reload` tool explicitly reloads the manifest (`tusk.toml`): it re-reads and validates the file, atomically swaps the in-memory schema (manifest, behavior engine, node service) reusing the open index handle, and kicks a reindex to re-validate indexed content against the new schema. Reload is **explicit-only** — the file watcher never auto-reloads on `tusk.toml` writes.

The originating process validates, swaps, bumps the `.tusk/manifest-epoch` sentinel ([`internal/epoch`](epoch.md), `epoch.Manifest`), and owns the single reindex. Sibling daemons converge on the bump via their own watcher pair — `RunManifestEpochWatcher` (2s poll) and `RunManifestEpochFastWatcher` (fsnotify fast-path) — calling `maybeReloadManifestForEpoch` → `siblingReloadManifest`, which reloads the manifest **only** (never reindexes; the originator owns that). When a reset and a reload land in the same window, `siblingReopen` re-reads `manifest-epoch` under the same flock and installs the fresh manifest alongside the fresh index, so a sibling never serves the new index against the stale schema.

Validation matches boot semantics, so a hot reload reaches the same in-memory state a restart would: blocking on a TOML parse/structural error or a behavior-engine build failure (the swap is refused and the epoch is not bumped); non-blocking on dangling aliases / bad `[context]` entries (dropped and surfaced as warnings, exactly as boot does). `seenManifestEpoch` advances only after a successful swap, so a half-written `tusk.toml` is retried on the next tick rather than poisoning convergence.

The `tusk_reload` response carries the new `manifest_epoch`, a `diff` (added/removed node-types, edge-types, behaviors), the `reindex` report, and any `validation_errors` / `warnings`. The CLI `tusk reload` has no previous manifest to diff against, so it prints the loaded schema summary instead; `--reindex` runs a synchronous reindex for the no-daemon case.

**Embedding settings on reload.** The `tusk_reload` reindex walk runs the `[embeddings]` settings check (see [reindex](reindex.md)) but doesn't drain inline: after a settings change the queue is the whole vault, and the background drainer does it off `reindexMu`. The CLI `tusk reload` runs the same check even without `--reindex` (`reindex.ApplyEmbeddingSettings`), because converging siblings never reindex and nothing else would record the new settings; its output reports `embed_settings_requeued`. Each drainer tick carries its runtime snapshot's settings fingerprint, so a pass still running when a reload swaps the runtime stops at its next node and the next tick resumes under the new settings.

## Notes

Workspace-config commands stay CLI-only in v1.c — `tusk pack add` has no MCP equivalent yet (carried as 7.c.1 §10 ledger #10). Structured warnings via stderr text-line parsing remains a v1 expediency (Plans 7, 7.b, 7.c.1 residuals).
