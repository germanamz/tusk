---
type: probe-report
title: Spec B — host-Ollama (Metal) probe results
session-date: "2026-05-15"
host-session: yes
---

# Host-Ollama Metal probe — results

Run from a Claude session on the Mac host, per `docs/handoffs/2026-05-15-spec-b-host-ollama-probe.md`. Hand back to the in-container session.

## Environment

| | |
|---|---|
| macOS | 26.3.1 (build 25D771280a) |
| Chip | Apple M1 Max |
| RAM | 32 GB (25.0 GiB visible to Metal) |
| Ollama version | 0.23.4 (already installed via `/Applications/Ollama.app`) |
| Ollama binary | `/Applications/Ollama.app/Contents/Resources/ollama` (symlinked at `/usr/local/bin/ollama`) |
| Model | `nomic-embed-text` pulled fresh into `~/.ollama/models` |
| Bind | **`127.0.0.1:11434`** (loopback only — see Security below) |

Far newer than the 0.3.4 minimum the handoff called out; `/api/embed` works.

## Metal confirmed

From `~/.../ollama-serve.log`:

```
library=Metal compute=0.0 name=Metal description="Apple M1 Max" total="25.0 GiB" available="25.0 GiB"
ggml_metal_library_init: using embedded metal library
ggml_metal_device_init: GPU name:   Apple M1 Max
ggml_metal_device_init: GPU family: MTLGPUFamilyApple7  (1007)
ggml_metal_device_init: has unified memory    = true
GPULayers:13[ID:0 Layers:13(0..12)]
```

13/13 GPU layers loaded onto Metal. No CPU fallback in the log.

## Latency results

All measurements on a 2169-byte prompt (= chunk-sized, matching what tusk actually sends), warm model.

### Step 5a — singular call latency

| endpoint | call | wall-clock (5-trial after warmup) |
|---|---|---|
| `/api/embed` | single string | **71 ms** (range 71–91, mean of 4 stable trials = 71ms) |
| `/api/embeddings` (legacy) | single string | **69 ms** (range 69–73, mean = 70ms) |

Includes ~5–10ms of curl/JSON overhead. Pure model time is closer to **60ms/call**.

**Compared to the in-container CPU baseline of ~1.5–2 s/call (extrapolated from Phase 1's 704s / ~400 chunks):**
**~25× faster on Metal.**

### Step 5b — `/api/embed` batch endpoint

| call | total wall-clock | per-item |
|---|---|---|
| `/api/embed` batch of 8 (3-trial mean) | 442 ms | **55 ms/item** |
| 8× sequential singular (extrapolated) | 568 ms | 71 ms/item |

Batch is only **~22% faster per item** — *below* the handoff's 30% threshold to justify Phase 2.

`/api/embed` does **not** hang on Metal. The 30s hangs the in-container session reported (see `docs/handoffs/2026-05-15-spec-b-phase-2-paused.md`) were CPU-saturation symptoms, not a real Ollama bug.

## Decision matrix verdict

Singular Metal latency: **71 ms/call → ≤ 100ms row.**

> **Close Spec B as obsoleted.** Phase 1 already shipped harmlessly; Phase 2 has no headroom. Full reindex on Metal will likely take 30–60s, not 704s.

Math: ~400 chunks × 71ms = **~28 s sequential**, vs Phase 1's measured **704s on CPU**.

## Step 5+ — host-side full reindex (confirmed)

Per user direction, skipped the container bridge and ran reindex directly on the host:

```
make build                                              # produced arm64 ./bin/tusk
OLLAMA_HOST=127.0.0.1:11434 ./bin/tusk reindex
```

Result: **Reindex done: 58 indexed, 0 removed, 4 skipped — 41.407 s wall-clock.**

- Pre-reindex embed queue depth: 37
- Post-reindex embed queue depth: 0
- Vs Phase 1's CPU baseline of **704s**: **17× end-to-end speedup** (and that's including filesystem walking, parsing, validation, DB writes, etc., which now dominate the runtime).

**Embedding is no longer the bottleneck on Metal.** 37 embeds × 71ms ≈ 2.6s of actual model time inside a 41s reindex — embed is now ~6% of reindex wall-clock, where on CPU it was the entire job. Parallel workers or batched embed would speed up a 2-3s slice of a 41s job. Not worth a spec.

Even Phase 1's parallel-worker code (workers=4) is unlikely to help much on Metal — `OLLAMA_NUM_PARALLEL=1` is the daemon's default, so concurrent requests serialize at the runner anyway. Worth one measurement to confirm, not a spec.

## Container reachability — IMPORTANT

The handoff assumed the host session would expose Ollama to the devcontainer (`OLLAMA_HOST=0.0.0.0:11434` + `host.docker.internal` from inside the container). After consulting with the user, **we chose not to do this.**

Findings:

1. **Docker Desktop on Mac has no host-side Docker-only interface.** The host's only interfaces are `lo0` (127.0.0.1) and `en0` (192.168.0.127 — the LAN IP). Docker Desktop's bridge lives inside its LinuxKit VM. From the container's perspective, `host.docker.internal` routes to the host's regular en0 IP — meaning **any bind that exposes Ollama to the container also exposes it to the LAN**.
2. **Binding to 0.0.0.0 or to en0 has identical blast radius.** Either route requires trusting the macOS Application Firewall.
3. **User concern:** wants to work from cafés / shared networks without exposing Ollama. Reasonable — and Docker Desktop on Mac can't satisfy that constraint via interface-binding alone.

**User decision:** skip the container detour entirely. Run `tusk reindex` from the **Mac host** against `localhost:11434` to measure the Metal reindex wall-clock. The container session is *not* required for this measurement.

If the container session wants reindex parity later, three options exist, in increasing user friction:
- **On-demand socat bridge** (`socat TCP-LISTEN:11434,bind=0.0.0.0,fork,reuseaddr TCP:127.0.0.1:11434`) wrapped in a `tusk-bridge {on,off}` script — only runs when user explicitly enables it. socat is already installed (`/opt/homebrew/bin/socat`, brew).
- **Rebind Ollama** to `0.0.0.0:11434` only when at home — `pkill ollama && OLLAMA_HOST=0.0.0.0:11434 ollama serve &`.
- **Enable macOS firewall stealth mode** + leave Ollama on 0.0.0.0 permanently. Trusts a single config switch.

Container session: **do not** edit `.devcontainer/devcontainer.json` to set `OLLAMA_HOST=host.docker.internal:11434` unless the user has run a bridge on the host. Default assumption: host-only.

## Recommendations to the in-container session

1. **Close Spec B as obsoleted.** Phase 1's parallel-worker code is shipped and harmless; Phase 2 (batched embed) has no headroom worth the wire-format work. Write the closeout into the Spec B ticket / plan, including these numbers.
2. **Host-side reindex measured: 41.4s end-to-end** (see Step 5+ above). 17× speedup vs CPU baseline. Container session can re-reindex its own state to reproduce, but the conclusion does not depend on it.
3. **Keep Ollama on `127.0.0.1`** as the default daemon bind on this Mac. Don't persist `OLLAMA_HOST=0.0.0.0` to shell rcs.
4. **Do not** uninstall in-container Ollama yet — it's the control measurement.

## Artifacts left on host

- Ollama daemon running on `127.0.0.1:11434` (started by this session, `OLLAMA_HOST=127.0.0.1:11434 ollama serve`, log at `$TMPDIR/ollama-serve.log`). Will keep running until user kills it or reboots.
- `nomic-embed-text` model pulled into `~/.ollama/models`.
- `socat` installed via brew (1.8.1.1).
- Symlink `/usr/local/bin/ollama` → `/Applications/Ollama.app/Contents/Resources/ollama`.
- Rebuilt `./bin/tusk` for darwin/arm64 via `make build` (replaces the prior Linux-built binary).
- `.tusk/index.db` re-reindexed from the host — container session should re-run `tusk reindex` to be in sync once it resumes (queue depth currently 0, last reindex ts updated).
- No git-tracked file edits. No commits. No PRs.
