---
type: package
title: internal/watcher — fsnotify integration
import-path: github.com/germanamz/tusk/internal/watcher
status: stable
---

# internal/watcher

Long-running fsnotify-based file watcher. Subscribes to the workspace tree (minus ignored directories), debounces rapid event bursts, and re-parses + re-indexes touched files. Keeps the index in lockstep with disk during `tusk watch` and `tusk mcp`.

## Public surface

- `Watcher` — long-running goroutine.
- `New(workspaceRoot string, matcher ignore.Matcher) (*Watcher, error)` — walks the tree once, watching every directory the matcher does not reject.
- `(*Watcher).Run(ctx, handler) error` — blocks until ctx is cancelled, dropping events whose path the matcher rejects.

## Notes

Honors `[workspace] ignore` via the same matcher the reindex walker uses. The watcher does NOT call out to `internal/reindex.Run` — it processes events one file at a time through the same parse → validate → upsert path that `internal/node/Service.Modify` uses for explicit writes.
