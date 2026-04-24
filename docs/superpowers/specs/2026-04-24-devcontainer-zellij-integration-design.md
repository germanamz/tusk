# Dev Container — Zellij Integration

**Branch:** `chore/devcontainer-improvements`
**Date:** 2026-04-24
**Status:** design approved, awaiting implementation plan

## Goal

Make Zellij available inside the dev container as a shared tool for both the `vscode` (agent) and `dev` (admin) users, with a sensible default config, a ready-to-use Tusk layout, and host-side onboarding docs for Ghostty users. No auto-start, no surprise behavior changes for existing contributors.

## Non-goals

- No Nerd Font install in the container (fonts are rendered by the host terminal).
- No per-user config volumes. Config is shared via `/etc/zellij/`.
- No `postCreateCommand` or VS Code terminal overrides in `devcontainer.json`.
- No auto-`exec zellij` from `.bashrc` (would surprise `docker exec` callers and the agent attach flow).
- No forced `TERM` override.
- No CI smoke test for Zellij presence.

## Scope

### 1. Install Zellij at image build time

Add a layer to `.devcontainer/Dockerfile` that fetches the multi-arch musl binary from GitHub releases and installs it system-wide at `/usr/local/bin/zellij`. Mirror the existing nvim/go multi-arch pattern — detect `amd64`/`arm64` from `dpkg --print-architecture`, map to `x86_64`/`aarch64`, pull `zellij-<arch>-unknown-linux-musl.tar.gz`, extract the binary, `chmod 0755`.

Build-time internet is unrestricted, so no firewall change is needed. `github.com` and `objects.githubusercontent.com` are already in `tinyproxy-filter` for runtime if an operator ever re-fetches.

Both users pick up the binary via their existing `PATH` (`/usr/local/bin`).

### 2. Ship a shared default config

Create `/etc/zellij/config.kdl` at build time with minimal, non-intrusive defaults:

- `default_shell "bash"`
- `mouse_mode true`
- `copy_on_select true`
- No `copy_command` — rely on terminal OSC52 passthrough (Ghostty supports it natively); setting `pbcopy` would fail inside the Linux container.
- No custom keybinds (the task list's `Super c` / `Super v` fight with terminal-native shortcuts and are unnecessary with `copy_on_select`).

Both users pick up the shared config via an image-level `ENV ZELLIJ_CONFIG_DIR=/etc/zellij` in the Dockerfile. `ENV` is inherited by every process in the container regardless of shell type (login vs interactive, `docker exec` vs `bash -l`), which is more robust than a `/etc/profile.d/*.sh` script that depends on PAM/profile sourcing. Users who want to override per-user can create `~/.config/zellij/config.kdl` and re-export `ZELLIJ_CONFIG_DIR=$HOME/.config/zellij` in their own shell rc.

### 3. Ship a sample Tusk layout

Create `/etc/zellij/layouts/tusk.kdl` at build time. Intent: launching `zellij --layout tusk` drops the user into a three-pane workspace suited to Tusk development:

- **Editor pane** (left, large) — runs `nvim`.
- **Tusk shell pane** (right top) — interactive `bash` so the user can freely run `tusk task tree`, `tusk task create`, etc. (Running a bare `tusk` as the pane command would exit immediately since Tusk is a CLI, not a TUI.)
- **Claude pane** (right bottom) — runs `claude --dangerously-skip-permissions`. Prompt-level permissions are redundant here: the dev container is already a sandbox (egress restricted to the tinyproxy allowlist, `vscode` user has no sudo), so the outer sandbox is the real safety boundary and prompts only add friction.

Deliberately omitted from the task list's version: the `tusk mcp serve` pane. MCP is usually launched on demand by the client (Claude Code, an IDE, etc.), not run as a persistent foreground process for a solo developer. The layout stays lean; users can add a fourth pane locally if they want.

### 4. Documentation

Create `docs/dev-environment.md` covering:

- Recommended stack (Ghostty host + Zellij inside container + nvim + tusk + claude).
- DevPod / `devcontainer` attach commands, including the distinction: attach as `vscode` for the agent (restricted, only path out is tinyproxy allowlist), attach as `dev` for admin work (unrestricted, sudoer).
- How to start Zellij: `zellij` for a blank session, `zellij --layout tusk` for the preset.
- Known-issues / workarounds table, copied and trimmed from the integration task list — limited to items relevant to this container (drop generic "mouse pane resize flaky over SSH", keep Ghostty Option/Alt, OSC52 clipboard, `TERM=xterm-ghostty`, multi-user confusion).
- Troubleshooting: `zellij kill-session`, `zellij list-sessions`, verifying install.

Add a short pointer from `README.md`'s existing Quick Start / contributor section to the new doc. Do **not** rewrite the Quick Start.

### 5. No changes to `devcontainer.json`

Everything we ship here is baked into the image or is host-side documentation. `devcontainer.json`'s `remoteUser: vscode`, the existing mounts, `runArgs`, `containerEnv`, and `customizations` stay as-is.

## Files touched

- `.devcontainer/Dockerfile` — new install layer, `ENV ZELLIJ_CONFIG_DIR=/etc/zellij`, and `COPY` for the config file + layouts dir.
- `.devcontainer/zellij-config.kdl` (new) — copied into image at `/etc/zellij/config.kdl`.
- `.devcontainer/zellij-layouts/tusk.kdl` (new) — copied into image at `/etc/zellij/layouts/tusk.kdl`.
- `docs/dev-environment.md` (new).
- `README.md` — one-line pointer to the new doc.

## Verification

After `docker build` and attach:

- `zellij --version` succeeds as both `vscode` and `dev`.
- `echo $ZELLIJ_CONFIG_DIR` prints `/etc/zellij` in a fresh interactive shell as each user.
- A new Zellij session honors the shared config (mouse mode + copy on select).
- `zellij --layout tusk` opens the three-pane layout.
- `docs/dev-environment.md` renders cleanly and the README link resolves.

## Out of scope / deferred

- Automatic Zellij start on shell entry (ergonomic but disruptive; revisit if contributors request it).
- Per-user layout persistence volume (easy to add later if users customize layouts heavily).
- CI job verifying Zellij presence.
- Windows/WSL Ghostty notes.
- Devcontainer Feature for Zellij reusable across repos.
