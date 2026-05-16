---
type: package
title: internal/typepacks — pack platform
import-path: github.com/germanamz/tusk/internal/typepacks
status: stable
---

# internal/typepacks

Pack-platform plumbing for `tusk pack add`. Handles fetch (HTTP / HTTPS / file://), 30s timeout, 1 MiB cap, 3-redirect cap, hard-error collision detection against existing `tusk.toml` content, atomic write under workspace lock, and the built-in name-alias map for `tags` / `kanban` / `vault`.

## Public surface

- `Add(workspace, nameOrURL string, opts AddOptions) error` — main entry.
- `AddOptions{Force bool}` — `--force` splice override.
- Alias resolver — maps `tags` / `kanban` / `vault` to `https://raw.githubusercontent.com/germanamz/tusk/main/packs/<name>.toml`.

## Notes

The alias URLs only work after the v1 → main cascade lands (PR #351 — merged 2026-05-08); before that, only the `file://` form returned content. Engine has zero notion of packs at runtime — pack-add is purely a templating mechanism that materializes manifest sections.
