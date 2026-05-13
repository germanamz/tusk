#!/bin/bash
# Start `ollama serve` in the background and ensure the configured embedding
# model is present. Invoked from devcontainer.json's postStartCommand, which
# runs as remoteUser (vscode) every time the container starts.
#
# Re-entrant: skips the spawn if the daemon is already responding, and skips
# the pull if the model is already cached. Idempotent across repeat runs.
set -e

OLLAMA_HOST=${OLLAMA_HOST:-127.0.0.1:11434}
OLLAMA_MODEL=${OLLAMA_MODEL:-nomic-embed-text}
OLLAMA_LOG_DIR=${OLLAMA_LOG_DIR:-$HOME/.ollama}
OLLAMA_LOG_FILE="$OLLAMA_LOG_DIR/serve.log"

mkdir -p "$OLLAMA_LOG_DIR"

if ! curl -sf "http://${OLLAMA_HOST}/api/tags" >/dev/null 2>&1; then
    echo "start-ollama: launching ollama serve (logs: $OLLAMA_LOG_FILE)"
    nohup ollama serve >>"$OLLAMA_LOG_FILE" 2>&1 &
    disown || true

    for _ in $(seq 1 60); do
        if curl -sf "http://${OLLAMA_HOST}/api/tags" >/dev/null 2>&1; then
            break
        fi
        sleep 1
    done

    if ! curl -sf "http://${OLLAMA_HOST}/api/tags" >/dev/null 2>&1; then
        echo "start-ollama: ollama serve did not become ready in 60s — see $OLLAMA_LOG_FILE" >&2
        exit 1
    fi
else
    echo "start-ollama: ollama serve already responding on $OLLAMA_HOST"
fi

if ollama list 2>/dev/null | awk 'NR>1 {print $1}' | grep -qx "${OLLAMA_MODEL}:latest\|${OLLAMA_MODEL}"; then
    echo "start-ollama: model ${OLLAMA_MODEL} already present"
else
    echo "start-ollama: pulling ${OLLAMA_MODEL} (first run can take a few minutes)"
    ollama pull "${OLLAMA_MODEL}"
fi
