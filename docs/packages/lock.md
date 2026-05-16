---
type: package
title: internal/lock — workspace lock
import-path: github.com/germanamz/tusk/internal/lock
status: stable
---

# internal/lock

File-based workspace lock. Used by every command that mutates workspace state (`node create`, `node modify`, `node move`, `node delete`, `edge add`, `edge remove`, `pack add`, `reindex`, `watch`) to serialize writers across processes.

## Public surface

- `Acquire(workspace string) (*Lock, error)` — blocking lock.
- `(*Lock).Release()` — paired with `defer`.

## Notes

Uses `flock(2)` semantics; honored across processes on the same host. Read-only commands (`node get`, `node list`, `query`, `doctor`, `status`, `mcp` for read tools) skip the lock.
