---
type: package
title: internal/status — workspace snapshot
import-path: github.com/germanamz/tusk/internal/status
status: stable
---

# internal/status

Builds the quick "what's in the workspace right now" snapshot — node counts by type, total edge count, embed queue depth, last reindex timestamp. Backs `tusk status` and `tusk_status`.

The embed queue depth counts pending rows only. `EmbedFailed` counts the rows that stopped retrying (parked; `tusk doctor` lists them as `embed-failed`), and `EmbedWaiting` is set when the workspace has no `[embeddings]` section, so the pending rows wait for one rather than for a drainer. Pass `Config.Manifest` for the latter; nil leaves it false (#845).

## Public surface

- `Snapshot(repos…) (*Report, error)`.
- `Report.NodeCounts map[string]int`, `Report.EdgeCount int`, etc.

## Notes

Cheap call (a handful of `COUNT(*)` queries + one `MetaRepo.Get(last_reindex_at)`). Safe to call from a hot path; no lock acquisition.
