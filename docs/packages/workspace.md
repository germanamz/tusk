---
type: package
title: internal/workspace — workspace discovery
import-path: github.com/germanamz/tusk/internal/workspace
status: stable
---

# internal/workspace

Workspace discovery: walk up from a starting directory to the nearest `tusk.toml`, and provide the path constants used everywhere else (`ManifestFilename = "tusk.toml"`, `IndexDirname = ".tusk"`, `IndexFilename = "index.db"`). Opening the index lives in the `indexopen` subpackage.

## Public surface

- `Find(startDir string) (*Workspace, error)` — finds `tusk.toml` walking up from `startDir`.
- `Workspace.Root`, `Workspace.ManifestPath`, `Workspace.IndexPath` — absolute paths to the root, its `tusk.toml` and `.tusk/index.db` (which may not exist yet).
- `ErrNotFound` and `*NotFoundError` — `Find`'s error when the walk reaches the filesystem root. `NotFoundError.Dir` is the directory the walk started from, and `errors.Is(err, ErrNotFound)` matches it.
- Constants: `ManifestFilename`, `IndexDirname`, `IndexFilename`.

## Notes

Every CLI command and the MCP runtime call `Find` once at startup. The CLI picks the starting directory (`cmd/tusk/workspace_dir.go`): `--workspace` / `-C`, then `TUSK_WORKSPACE`, then the working directory. `tusk init` doesn't call `Find`, because the manifest doesn't exist yet. It writes `tusk.toml` into that starting directory directly.
