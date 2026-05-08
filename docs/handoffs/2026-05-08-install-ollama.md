---
type: handoff
title: Handoff 2026-05-08 — Install Ollama in devcontainer
session-date: "2026-05-08"
---

# Install Ollama in the devcontainer

## Why

Tusk's semantic-retrieval layer (Plan 5, `[[docs/packages/embed]]`) is built and shipped, but currently inert because the devcontainer image has no Ollama. Reindex's enqueue + drain phase runs only when `loaded.Embeddings.Provider == "ollama"` (`cmd/tusk/cmd_reindex.go:62`). With `tusk.toml` carrying no `[embeddings]` section, the embed queue stays empty and the embeddings table never populates. Structural queries work today; semantic queries no-op.

This handoff lands Ollama as a first-class part of the devenv so semantic queries become a normal capability of the workspace.

## Current state (verified 2026-05-08)

- `which ollama` → not found
- `curl http://localhost:11434/api/tags` → connection refused
- Devcontainer base: `mcr.microsoft.com/devcontainers/base:ubuntu-24.04` (`.devcontainer/Dockerfile`)
- Egress proxied through tinyproxy at `127.0.0.1:8888` (`HTTP_PROXY` / `HTTPS_PROXY` env)
- Allowlist: `.devcontainer/tinyproxy-filter` — `ollama.com` and `registry.ollama.ai` are NOT on it; `github.com`, `*.githubusercontent.com`, `objects.githubusercontent.com` ARE on it
- Container runs with `--cap-drop=ALL` plus a few capability adds; `--security-opt=no-new-privileges:true`. No systemd. The container's `entrypoint.sh` does process orchestration

## Approach options

**A. GitHub-release binary, no firewall change** (recommended).
The Ollama binary is hosted on GitHub releases, which already routes through the existing allowlist. Add a `RUN` step in `.devcontainer/Dockerfile` that fetches the architecture-matched tarball from `github.com/ollama/ollama/releases/download/v<X>/ollama-linux-${ARCH}.tgz`, extracts it to `/usr/local/bin/ollama`, and chmods it. Pulling models still needs `registry.ollama.ai` though — see B.

**B. Add `registry.ollama.ai` (and any model-CDN host) to `.devcontainer/tinyproxy-filter`.**
Without this, `ollama pull <model>` fails. Strict allowlist is the security posture, so this should be a deliberate decision: pin the exact host pattern, regenerate the proxy config, and document. Inspect `ollama pull` traffic to enumerate every host it actually contacts (registry redirects to a CDN) and add each one.

**C. Run Ollama as a sidecar service** (heavier).
Use a docker-compose (or VS Code dev-container compose) setup so Ollama runs in its own container with its own networking, mounted volume for models, and a stable hostname (e.g., `ollama:11434`). Keeps the dev shell pristine and lets the model cache survive `docker-compose down`. Requires reworking `devcontainer.json` from a single-build to a `dockerComposeFile` shape.

Recommendation: **A + B**, both small additions to the existing devcontainer, lowest deviation from current shape. Save C for when GPU access or multi-service orchestration is needed.

## Concrete tasks

1. **Dockerfile addition** — install Ollama from the GitHub release. Determine the right URL pattern for arm64 / amd64 (`uname -m`). Verify the binary launches with `ollama --version`.
2. **Allowlist additions** — append `registry.ollama.ai` and any CDN domains the pull traffic resolves to. Test by running `ollama pull nomic-embed-text` (or chosen model) inside a fresh container.
3. **Persistent model cache** — add a named volume mount for `~/.ollama` (Ollama's model storage) so models survive container rebuilds. Pattern matches the existing `tusk-devcontainer-go-cache` volume in `devcontainer.json`.
4. **entrypoint hook** — start `ollama serve` in the background at container start (a `&` invocation in `.devcontainer/entrypoint.sh`, or a `postStartCommand` in `devcontainer.json`). Health-check by polling `127.0.0.1:11434/api/tags` until ready or a timeout fires.
5. **Pre-pull the embedding model** — once `ollama serve` is up, pull the chosen model. Recommended: `nomic-embed-text` (137 M params, 768-dim, English-strong, fast; matches `dim = 768` in `tusk.toml`). Alternative: `mxbai-embed-large` (335 M, 1024-dim) if higher-recall is wanted later.
6. **Wire tusk** — add to `tusk.toml`:
   ```toml
   [embeddings]
   provider = "ollama"
   endpoint = "http://localhost:11434"
   model = "nomic-embed-text"
   dim = 768
   ```
7. **Update `[[docs/dev-environment]]`** with the new "Ollama starts at `127.0.0.1:11434`, models in `~/.ollama`" line so future contributors know.

## Verification

```bash
# Service up
curl -s http://localhost:11434/api/tags | jq '.models | length'  # > 0

# Embedding round-trip
curl -s http://localhost:11434/api/embeddings -d '{"model":"nomic-embed-text","prompt":"hello"}' | jq '.embedding | length'  # 768

# Tusk drain
./bin/tusk reindex                                  # queue should populate then drain
./bin/tusk status                                   # `embed queue depth` returns to 0
# Inspect embeddings table: should have ~43 rows (one per indexed node)
```

## References

- `[[docs/superpowers/specs/2026-05-08-tusk-workspace-bootstrap-design]]` — context: why the workspace exists and what was bootstrapped
- `[[docs/packages/embed]]` — the embed package this handoff brings online
- `[[docs/packages/reindex]]` — where the enqueue+drain decision lives
- `[[docs/dev-environment]]` — needs an addendum once Ollama is wired

## After this handoff

The next thing up is `[[docs/handoffs/2026-05-08-continue-migration]]` — re-enable semantic indexing in `tusk.toml`, run a fresh reindex, and continue the dogfooding migration follow-ups.
