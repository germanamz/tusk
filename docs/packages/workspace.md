---
type: package
title: internal/workspace — workspace open/close
import-path: github.com/germanamz/tusk/internal/workspace
status: stable
---

# internal/workspace

Workspace lifecycle: locate the manifest, ensure the index directory exists, open the SQLite store, and provide path constants used everywhere else (`ManifestFilename = "tusk.toml"`, `IndexDirname = ".tusk"`, `IndexFilename = "tusk.db"`).

## Public surface

- `Open(dir string) (*Workspace, error)` — finds `tusk.toml` walking up from `dir`.
- `(*Workspace).Close()`.
- `Workspace.Root` — absolute path to the workspace root.
- `Workspace.Manifest` — loaded `*manifest.Manifest`.
- `Workspace.Index` — opened `*index.Store`.
- Constants: `ManifestFilename`, `IndexDirname`, `IndexFilename`.

## Notes

Most CLI commands and the MCP runtime call `workspace.Open(cwd)` once at startup. `tusk init` sidesteps `Open` because the manifest doesn't exist yet — it writes `tusk.toml` directly via `os.WriteFile`.
