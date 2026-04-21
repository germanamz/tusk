#!/bin/bash
# PID 1 of the dev container. Runs as root, applies the egress firewall,
# then exec's the long-running CMD (sleep infinity). All user shells are
# attached afterward via `docker exec -u <user>`, by which time the
# firewall is already in place.
set -e

if [ "$(id -u)" -ne 0 ]; then
    echo "entrypoint must run as root (got uid $(id -u))" >&2
    exit 1
fi

/usr/local/bin/init-firewall.sh

# Keep ~/.claude.json across rebuilds by stashing the real file inside
# ~/.claude/ (which is on a named volume) and exposing it at the canonical
# path via a symlink. Runs as vscode because with --cap-drop=ALL the
# container root has no DAC_OVERRIDE, so it can't write inside the
# vscode-owned home dir.
runuser -u vscode -- bash -c '
    set -e
    CLAUDE_HOME=/home/vscode/.claude
    CLAUDE_CANON=/home/vscode/.claude.json
    CLAUDE_PERSIST="$CLAUDE_HOME/claude.json.persist"

    mkdir -p "$CLAUDE_HOME"
    if [ -f "$CLAUDE_CANON" ] && [ ! -L "$CLAUDE_CANON" ]; then
        mv -f "$CLAUDE_CANON" "$CLAUDE_PERSIST"
    fi
    if [ ! -e "$CLAUDE_PERSIST" ]; then
        : > "$CLAUDE_PERSIST"
    fi
    ln -sfn "$CLAUDE_PERSIST" "$CLAUDE_CANON"
'

exec "$@"
