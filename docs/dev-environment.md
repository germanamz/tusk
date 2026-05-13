---
type: note
title: Dev Environment
---

# Dev Environment

Tusk ships a dev container (`.devcontainer/`) that builds a reproducible sandbox with Go, Node, Neovim, Claude Code, GitHub CLI, `gopls`, `dlv`, `golangci-lint`, `lefthook`, **Zellij**, and a local **Ollama** instance pre-installed. This doc covers the recommended host-side setup and day-to-day usage.

## Recommended stack

- **Host terminal:** [Ghostty](https://ghostty.org/) on macOS (or WezTerm / Alacritty / Kitty on Linux).
- **Inside container:** Zellij as the terminal multiplexer, with Neovim, the Tusk CLI, and Claude Code in separate panes.

Why this combination:

- Native GPU rendering on the host.
- Zellij keeps a persistent pane layout even if you detach and re-attach.
- Good clipboard support end-to-end via OSC52 (all four host terminals support it; Zellij passes through).

## Users

The image defines two users. Pick based on what you're doing:

| User | Sudo | Network egress | Intended for |
|------|------|----------------|--------------|
| `vscode` | no | only through the local tinyproxy allowlist (anthropic, github, npm, go proxy) | running the Claude Code agent |
| `dev` | yes (NOPASSWD) | unrestricted | installing extra tools, admin/debug work |

`devcontainer.json` sets `remoteUser: vscode`, so the default attach — whether via VS Code Dev Containers, `devcontainer` CLI, or DevPod — lands you as `vscode`.

To attach as `dev` instead:

```bash
docker exec -it -u dev <container-id-or-name> bash
```

## Starting Zellij

Zellij is not auto-started. Run it when you want it:

```bash
zellij                      # blank session, default config
zellij --layout tusk        # preset Tusk layout (nvim + tusk shell + claude)
zellij list-sessions        # what's running
zellij attach <session>     # reattach to a detached session
zellij kill-session <name>  # nuke a stuck session
```

Both users (`vscode` and `dev`) share the same base config at `/etc/zellij/config.kdl` and the `tusk` layout at `/etc/zellij/layouts/tusk.kdl`. Override per-user by creating your own `~/.config/zellij/config.kdl` and exporting `ZELLIJ_CONFIG_DIR=$HOME/.config/zellij`.

The `tusk` layout opens:

- **Editor** (left, 70%) — `nvim`
- **Tusk shell** (right top) — interactive `bash` for `tusk task tree`, `tusk task create`, etc.
- **Claude** (right bottom) — `claude --dangerously-skip-permissions` (Claude Code CLI with prompt-level permissions bypassed — the dev container's firewall + no-sudo `vscode` user is the real sandbox, so prompt gates add friction without safety)

## Ollama (semantic indexing)

`ollama serve` is started in the background each time the container boots by `.devcontainer/start-ollama.sh` (wired in via `postStartCommand`). On first boot the script also pulls the embedding model named in `tusk.toml`'s `[embeddings]` block (default `nomic-embed-text`, 768-dim).

- Endpoint: `http://127.0.0.1:11434`
- Model cache: `/home/vscode/.ollama` (named volume `tusk-devcontainer-ollama`, survives `make devcontainer-down` but not `make devcontainer-nuke`)
- Logs: `/home/vscode/.ollama/serve.log`

Quick checks:

```bash
curl -s http://localhost:11434/api/tags | jq '.models | length'    # >= 1 once pull completes
curl -s http://localhost:11434/api/embeddings \
    -d '{"model":"nomic-embed-text","prompt":"hello"}' | jq '.embedding | length'   # 768
```

If `ollama pull` fails with a proxy/CONNECT error, the model registry's CDN host is probably missing from `.devcontainer/tinyproxy-filter`. The allowlist already includes `registry.ollama.ai` and `*.ollama.com`; tail `/var/log/tinyproxy/tinyproxy.log` (as `dev`) to see which host got refused and add the matching pattern.

## Ghostty host setup

Minimal `~/.config/ghostty/config` for smooth Tusk + Zellij work:

```
macos-option-as-alt = true
shell-integration-features = ssh-env,ssh-terminfo
clipboard-read = allow
clipboard-write = allow
```

- `macos-option-as-alt` prevents Option-key sequences from inserting special characters in nvim/Zellij keybinds.
- `shell-integration-features = ssh-env,ssh-terminfo` makes `TERM=xterm-ghostty` usable on remote hosts that don't have Ghostty's terminfo installed — Ghostty ships the terminfo over the SSH connection.
- `clipboard-read/write = allow` enables OSC52 passthrough so yanks inside Zellij/nvim land in the macOS clipboard.

## First-run checklist

1. Open the repo in VS Code → "Reopen in Container" (or `devcontainer up`).
2. Wait for the image to build (first build pulls Go, Node, nvim, Zellij).
3. Attach as `vscode` by default. Confirm: `zellij --version` should print a version.
4. In a second terminal, attach as `dev` if you need sudo: `docker exec -it -u dev <container> bash`.
5. Start the preset workspace: `zellij --layout tusk`.
6. Test clipboard: yank in nvim (`yy`) → paste on the host (`Cmd+V`).

## Known issues & workarounds

| Issue | Severity | Workaround |
|-------|----------|------------|
| macOS Option/Alt sequences swallowed by Ghostty | High | Set `macos-option-as-alt = true` in Ghostty config (above). |
| `TERM=xterm-ghostty` unrecognized on the remote side | Medium | Set `shell-integration-features = ssh-env,ssh-terminfo` in Ghostty. The container's default `TERM` falls back to `xterm-256color` for non-Ghostty processes. |
| Clipboard (OSC52) silently drops | Medium | Allow clipboard read/write in Ghostty config. `copy_on_select = true` is set in the shared Zellij config. |
| Multi-user confusion — commands fail with permission errors | Low | You're probably attached as `vscode` and trying to `sudo` or hit a blocked egress host. Reattach as `dev` for that task. |
| Zellij panics on very long sessions | Low | `zellij kill-session <name>` and start a new one. |
| Neovim scrolling artifacts inside a Zellij pane | Low | Update Neovim (the image pulls stable on each rebuild); report upstream if it persists. |

## Troubleshooting

```bash
# Is Zellij installed?
zellij --version
which zellij                        # expect /usr/local/bin/zellij

# Is the shared config visible?
echo $ZELLIJ_CONFIG_DIR             # expect /etc/zellij
ls /etc/zellij /etc/zellij/layouts

# Which user am I?
whoami                              # vscode or dev
id                                  # check group membership

# Can I reach anthropic / github?
curl -sS -o /dev/null -w '%{http_code}\n' https://api.anthropic.com/
curl -sS -o /dev/null -w '%{http_code}\n' https://api.github.com/
# As vscode these go via tinyproxy (allowlisted). As dev they go direct.

# Firewall state (needs sudo, so run as dev)
sudo iptables -L OUTPUT -n --line-numbers
```

If `zellij --layout tusk` fails with "layout not found", confirm `/etc/zellij/layouts/tusk.kdl` exists and `$ZELLIJ_CONFIG_DIR` is set. A bare `zellij options --dump-layout tusk` will print the parsed layout or a parse error.
