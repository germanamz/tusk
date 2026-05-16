---
type: package
title: internal/ignore — gitignore matcher
import-path: github.com/germanamz/tusk/internal/ignore
status: stable
---

# internal/ignore

Gitignore-style pattern matcher used by the watcher and reindex walker to skip non-vault files. Reads `[workspace] ignore = […]` from `tusk.toml` and the workspace's `.gitignore` (if present).

## Public surface

- `Matcher` — compiled pattern set.
- `Matches(relPath string, isDir bool) bool` — main entry.
- `New(patterns []string) *Matcher` — construction.

## Notes

Patterns are gitignore semantics: trailing-`/` for directories, glob wildcards, leading-`!` for negation. The reindex walker calls `Matches` per directory entry and returns `filepath.SkipDir` for matched directories.
