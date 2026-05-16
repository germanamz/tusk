---
type: package
title: internal/status — workspace snapshot
import-path: github.com/germanamz/tusk/internal/status
status: stable
---

# internal/status

Builds the quick "what's in the workspace right now" snapshot — node counts by type, total edge count, embed queue depth, last reindex timestamp. Backs `tusk status` and `tusk_status`.

## Public surface

- `Snapshot(repos…) (*Report, error)`.
- `Report.NodeCounts map[string]int`, `Report.EdgeCount int`, etc.

## Notes

Cheap call (a handful of `COUNT(*)` queries + one `MetaRepo.Get(last_reindex_at)`). Safe to call from a hot path; no lock acquisition.
