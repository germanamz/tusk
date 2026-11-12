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
- `Validate(packBytes) (*manifest.Manifest, error)` — checks a pack's shape and returns it parsed, rules included.
- `FindCollisions(userBody, pack) ([]string, error)` — the sections a pack shares with the workspace manifest, as qualified names (`node-types.task`, `edge-types.parent`, `behaviors.workflow.kanban`, `rule.task-has-an-owner`).

## What a pack may hold

A pack file may declare `[node-types]`, `[edge-types]`, `[behaviors]` and `[rule]`, and nothing else. Any other top-level section fails `Validate` before the manifest is touched.

- Types, edges and behaviors go through the same `manifest.Validate` the engine runs, so a pack has to be valid on its own.
- A `[rule.<name>]` block has to decode: a value of the wrong type, a key a rule does not take, or a `rule.<name>` that is not a table fails the add. `manifest.Load` leaves a block like that for doctor to report, but a pack is appended whole, so it is refused here.
- Whether a rule's filter names declared types and properties depends on the workspace the pack lands in. That check stays with `doctor.CompileRules`, which reports a bad rule as `rule-invalid` on the next `tusk doctor`.
- `[check]` is refused on purpose. A check is shell text that `tusk doctor --checks` runs, and a pack can come from a URL.

A rule name the workspace already declares is a collision, the same as a type name: without `--force` the add fails and writes nothing, and with it the workspace's block is removed before the pack is appended.

## Notes

Built-in packs used to be fetched from `raw.githubusercontent.com/.../main/packs/<name>.toml`, which needed the network and could hand an older binary a pack written for a newer one. Since #790 they are embedded (`go:embed` in `packs/packs.go`), which is also what lets `tusk init` add the vault pack offline when it adopts existing markdown. The TOML files stay at `packs/*.toml` because older releases still fetch them from there. `dev.toml` is not a built-in; it installs by URL. Engine has zero notion of packs at runtime — pack-add is purely a templating mechanism that materializes manifest sections.
