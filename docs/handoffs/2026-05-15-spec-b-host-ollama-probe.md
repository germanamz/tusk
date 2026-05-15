---
type: handoff
title: Spec B — host-Ollama (Metal) probe, run on the Mac host
session-date: "2026-05-15"
---

# Spec B — host-Ollama (Metal) probe, run on the Mac host

You are a Claude session running **on the Mac host**, outside the tusk devcontainer. A sibling session inside the devcontainer is paused waiting for your result. This file is your full briefing — you do not need to read any other handoff, spec, plan, or source file in the tusk repo to do your job. You may or may not have the repo cloned locally; either is fine. Do not edit repo files from this side.

---

## TL;DR of why you exist

Tusk uses Ollama (`nomic-embed-text`) to embed markdown chunks during reindex. Two specs were drafted to speed this up:

- **Phase 1 — parallel embed workers** — shipped. Measured **0× speedup** on the dogfood workspace (704s with workers=1, 714s with workers=4).
- **Phase 2 — batched `/api/embed`** — paused. Wire-format bug discovered in the spec, *and* no evidence batching helps on the current hardware.

The current Ollama runs **inside the devcontainer**, on CPU only. Docker Desktop on macOS cannot pass Apple Silicon's Metal GPU through to Linux containers — that's a Docker limitation, not a config oversight.

The hypothesis: if Ollama runs **natively on the Mac host** with Metal acceleration, single-call embed latency drops by 10–50×, which likely **obsoletes Spec B entirely**. Neither parallel workers nor batched embed has meaningful headroom if each call already takes 20ms instead of 2s.

Your job is to install host Ollama, prove (or disprove) the latency drop, and make host Ollama reachable from the devcontainer so the in-container agent can re-measure the full reindex baseline.

---

## What you will do, in order

### 1. Verify the environment

```bash
sw_vers                  # macOS version
uname -m                 # arm64 = Apple Silicon, x86_64 = Intel
sysctl hw.memsize        # RAM (need ~4GB free for nomic-embed-text)
which ollama || true     # is Ollama already installed?
```

If `uname -m` says `x86_64`, stop and report back — Intel Macs have no Metal advantage worth pursuing, and the host-Ollama plan doesn't apply. Hand control back to the container session.

### 2. Install Ollama if not present

Preferred (homebrew):
```bash
brew install --cask ollama-app   # menubar app + CLI
```

Or download the DMG from `https://ollama.com/download/mac` and install manually. Either is fine; the menubar app auto-starts `ollama serve` on login.

If Ollama is already installed, capture the version:
```bash
ollama --version
```

We need **Ollama 0.3.4 or newer** — that's the release that introduced the `/api/embed` batch endpoint. If older, upgrade (`brew upgrade ollama-app` or re-download).

### 3. Configure Ollama to accept connections from the devcontainer

By default, Ollama binds to `127.0.0.1:11434` on macOS. The devcontainer reaches the Mac host via `host.docker.internal`, which resolves to a bridge IP — not loopback. So you must rebind to `0.0.0.0:11434`.

If using the menubar app:
```bash
launchctl setenv OLLAMA_HOST "0.0.0.0:11434"
```
Then quit the menubar app (right-click → Quit) and relaunch it.

If running `ollama serve` from a terminal:
```bash
OLLAMA_HOST=0.0.0.0:11434 ollama serve
```

**Security note to flag to the user before proceeding:** binding to `0.0.0.0` means anything on the same LAN can hit `http://<mac-ip>:11434`. The macOS firewall blocks inbound by default; verify it's on (System Settings → Network → Firewall). If the user is on an untrusted network (coffee shop, conference), warn them and offer to defer. For a home/dev network this is fine.

Verify:
```bash
curl -s http://localhost:11434/api/version
ifconfig en0 | grep "inet "          # capture the Mac's LAN IP
curl -s http://<that-ip>:11434/api/version   # should also work
```

### 4. Pull the model and confirm Metal is in use

```bash
ollama pull nomic-embed-text
```

Then run one inference and tail the log to confirm Metal:
```bash
curl -s http://localhost:11434/api/embeddings \
  -d '{"model":"nomic-embed-text","prompt":"warmup"}' >/dev/null

# Check the log — path varies by install:
tail -50 ~/.ollama/logs/server.log 2>/dev/null || \
  log show --predicate 'process == "ollama"' --info --last 1m | tail -50
```

Look for lines mentioning `metal`, `ggml_metal_init`, or `Metal GPU`. If you see CPU-only fallbacks, stop and report — something is wrong with the install.

### 5. Run the latency gate

This is the decision-making measurement. Two questions:

**(a) How fast is a single embed call on Metal?**

```bash
# Generate a realistic chunk-sized prompt (~2KB, what tusk actually sends).
PROMPT=$(printf 'lorem ipsum dolor sit amet %.0s' {1..80})

# Warm the model.
curl -s http://localhost:11434/api/embeddings \
  -d "{\"model\":\"nomic-embed-text\",\"prompt\":\"$PROMPT\"}" >/dev/null

# Time 10 sequential singular calls.
time for i in $(seq 1 10); do
  curl -s http://localhost:11434/api/embeddings \
    -d "{\"model\":\"nomic-embed-text\",\"prompt\":\"$PROMPT $i\"}" >/dev/null
done
```

Record total wall-clock and per-call mean. CPU baseline (in-container) is roughly **1.5–2 seconds per chunk-sized call** based on Phase 1's 704s / ~400 chunks math. Expect Metal to land at **20–80ms per call** — i.e., 10s total for 10 calls, vs ~15–20s on CPU.

**(b) Does `/api/embed` (batch) work and is it faster than singular?**

The previous in-container session found `/api/embed` calls hung at 30s with no response — but that was on a CPU-saturated, soon-to-unload model. Retry on Metal:

```bash
# Single string through the new endpoint.
time curl -s http://localhost:11434/api/embed \
  -d "{\"model\":\"nomic-embed-text\",\"input\":\"$PROMPT\"}" | head -c 200
echo

# Batch of 8.
INPUTS=$(printf '"%s %d",' "$PROMPT" {1..8} | sed 's/,$//')
time curl -s http://localhost:11434/api/embed \
  -d "{\"model\":\"nomic-embed-text\",\"input\":[$INPUTS]}" | head -c 200
echo
```

Compare batch-of-8 wall-clock against 8× the singular-call time from (a). Batch should be at least ~30% faster to justify Phase 2.

### 6. Decision matrix

| (a) singular Metal latency | (b) batch-of-8 vs 8× singular | What to recommend |
|---|---|---|
| ≤ 100ms/call | irrelevant if (a) is this fast | **Close Spec B as obsoleted.** Phase 1 already shipped harmlessly; Phase 2 has no headroom. Full reindex on Metal will likely take 30–60s, not 704s. |
| 100–500ms/call | batch ≥ 30% faster | **Resume Phase 2** with corrected wire format (`/api/embed` + `input: []string`, min Ollama 0.3.4). Phase 1 parallel workers may also start delivering speedup now. |
| 100–500ms/call | batch ≤ 30% faster (or hangs) | **Ship Phase 1's existing parallel-worker code as the win**; close Phase 2; close Spec B. |
| > 500ms/call | — | Something is wrong with Metal acceleration. Investigate before recommending anything. |

### 7. Hand back to the in-container session

Once host Ollama is verified and the latency numbers are captured, write a short report (paste into the user's chat, or save to `~/tusk-host-ollama-probe-report.md` if the user prefers). Include:

- macOS version, chip, RAM
- Ollama version
- Confirmation Metal is active (log excerpt)
- Mac LAN IP that the devcontainer should target (usually `host.docker.internal` works; capture the literal IP as a fallback)
- Singular-call mean latency on chunk-sized prompts (from step 5a)
- `/api/embed` single + batch-of-8 timings (from step 5b)
- Your row in the decision matrix and your recommendation

The in-container agent will then:
- Update `.devcontainer/devcontainer.json` to set `OLLAMA_HOST=host.docker.internal:11434`
- Disable or guard `.devcontainer/start-ollama.sh` so the in-container daemon doesn't spawn
- Re-run `tusk reindex` against host Ollama and record the new wall-clock vs the 704s CPU baseline
- Update the spec/plan/ticket per your recommendation

---

## What you must NOT do

- **Do not** edit any file inside `/workspaces/tusk` or its host-side mount. The container session owns repo edits.
- **Do not** push commits or open PRs from the host.
- **Do not** uninstall, reset, or reconfigure the existing in-container Ollama. It's the control measurement; we may want to A/B against it.
- **Do not** persist `OLLAMA_HOST=0.0.0.0` to the user's shell rc files without asking — `launchctl setenv` is session-scoped, which is the right default. Persistent global binding is a separate decision.
- **Do not** install Ollama via `curl | sh` from random URLs. Use brew or the official ollama.com DMG.

---

## Quick troubleshooting

- **`host.docker.internal` doesn't resolve from the container** — Docker Desktop has it built in; if it fails, the container's `/etc/hosts` may need `host.docker.internal host-gateway` via `extra_hosts`. That's a container-side fix; flag it and the in-container agent will handle.
- **Metal log lines absent** — confirm you're on Apple Silicon (`uname -m` = `arm64`). On Intel Macs Ollama falls back to CPU regardless.
- **Model pull hangs at 0%** — Ollama uses HTTPS to download; check the user isn't behind a corporate proxy. Set `HTTPS_PROXY` if needed.
- **Embed call returns `{"embedding":[]}`** on `/api/embeddings` — you sent `prompts` (plural) instead of `prompt` (singular). The legacy endpoint never accepted a list; that's the bug that paused Phase 2.
- **`/api/embed` returns 404** — Ollama is older than 0.3.4. Upgrade.

---

## Pointers (for the user, if they ask)

- Container-side handoff that triggered this: `docs/handoffs/2026-05-15-spec-b-phase-2-paused.md` (in the tusk repo)
- Spec B ticket: `tickets/spec-b-parallel-batched-embed` (in the tusk repo)
- Ollama batch API release notes: https://github.com/ollama/ollama/releases (search "0.3.4" / "/api/embed")
- Docker for Mac GPU passthrough tracking issue: https://github.com/docker/for-mac/issues/2738 (still open — confirms the limitation, you don't need to argue with it)
