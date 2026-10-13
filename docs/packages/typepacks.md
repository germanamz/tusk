---
type: package
title: internal/typepacks — pack platform
import-path: github.com/germanamz/tusk/internal/typepacks
status: stable
---

# internal/typepacks

Pack-platform plumbing for `tusk pack add` and `tusk init`. Handles loading (a built-in name from the binary; HTTP / HTTPS / file:// URLs with a 30s timeout, 1 MiB cap and 3-redirect cap), hard-error collision detection against existing `tusk.toml` content, and the atomic write under the workspace lock.

## Public surface

- `AddPack(ctx, source, force, workspaceRoot) error` — main entry. Loads the pack before taking the workspace lock, so an unknown name fails fast and a URL fetch never holds the lock.
- `LoadPack(ctx, source) ([]byte, error)` — a built-in name (`kanban`, `tags`, `vault`) returns the copy compiled into the binary by the root [`packs`](../../packs/packs.go) package; anything with `://` is fetched; any other name is an error listing the built-ins.
- `Fetch(ctx, rawURL) ([]byte, error)` — the URL loader.

## Notes

Built-in packs used to be fetched from `raw.githubusercontent.com/.../main/packs/<name>.toml`, which needed the network and could hand an older binary a pack written for a newer one. Since #790 they are embedded (`go:embed` in `packs/packs.go`), which is also what lets `tusk init` add the vault pack offline when it adopts existing markdown. The TOML files stay at `packs/*.toml` because older releases still fetch them from there. `dev.toml` is not a built-in; it installs by URL. Engine has zero notion of packs at runtime — pack-add is purely a templating mechanism that materializes manifest sections.
