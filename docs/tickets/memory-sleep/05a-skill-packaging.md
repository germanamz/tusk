---
type: ticket
title: Ship a skill with the plugin
blocks:
  - docs/tickets/memory-sleep/05b-skill-content
order: 1
parent: docs/tickets/memory-sleep/05-sleep-skill
points: 3
priority: medium
status: pending
---

The plugin that `tusk claude install` writes has a manifest and a hooks module today (`internal/claudeplugin`, `docs/packages/claudeplugin.md`). This ticket makes it carry a skill too, with placeholder text. The real text is [[docs/tickets/memory-sleep/05b-skill-content]].

## Scope

- Find out how a plugin in `.claude/skills/tusk/` exposes a skill, and under what name a person invokes it.
- `install` writes the skill file from the embedded assets, `uninstall` removes it, and `status` reports it.
- A newer tusk refreshes the skill on the next `install`, the way it refreshes the hooks module.
- Update `docs/packages/claudeplugin.md`.

## Done when

- In an interactive Claude Code session started at a scratch vault's root, the skill is listed and can be invoked. `claude -p` does not load the plugin, so that is not a valid check.
- `make claude-plugin-check` passes.
- The Go tests cover install, uninstall and status with the skill file.
