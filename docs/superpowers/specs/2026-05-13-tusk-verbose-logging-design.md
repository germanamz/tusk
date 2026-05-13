---
type: spec
title: Tusk Verbose Logging — Embed/Reindex/Watch
---

# Tusk — Verbose Logging Design

- **Status:** Draft
- **Date:** 2026-05-13
- **Author:** German Meza
- **Scope:** Add a persistent `--verbose` / `-v` flag to the `tusk` CLI and thread an `slog.Logger` through the embed, reindex, watch, and MCP-runtime subsystems so failures in the indexing pipeline are visible from the command line. Adds structured log statements at the points where today's pipeline is opaque — most importantly, every embed-call outcome and every drain re-enqueue.
- **Motivation:** On 2026-05-13 a reindex run silently fired 144,537 failing `POST /api/embeddings` requests against Ollama (input length exceeded model context) before any signal reached the operator. The CLI emitted no output during the run, the drain loop re-enqueued failures forever, and the bug only surfaced by reading Ollama's own log. We want the next instance of this class of bug to show up in `tusk`'s stderr inside the first failed attempt.

---

## 1. Goal & Scope

### 1.1 In scope

- A persistent root flag `--verbose` / `-v` (boolean) on the root `tusk` command.
- A small `cmd/tusk/logger.go` helper that builds an `*slog.Logger` from the flag.
- Thread the logger through:
  - `internal/embed/drain.go` (`DrainConfig.Logger`).
  - `internal/embed/ollama.go` (`OllamaEmbedder` constructor accepts a logger).
  - `internal/reindex` (`Config.Logger`).
  - `internal/mcp` — wire the *existing but currently unused* `Logger` fields on `DrainerConfig` and `WatcherConfig` from the CLI side, via `mcp.Open` / `mcp.NewServer`.
- New log statements covering the events listed in §4.
- The `tusk reindex`, `tusk watch`, and `tusk mcp` commands all read the persistent flag and construct the same logger.
- Tests for the events that are load-bearing diagnostics (embed-error log, re-enqueue log, drain summary log).

### 1.2 Out of scope

- Fixing `WholeDocument` chunking in `internal/embed/chunking.go`. That is the root cause of the 2026-05-13 incident, but the fix is a separate ticket; this spec only makes the failure mode visible.
- Fixing the infinite-retry behavior in `DrainQueue` (`embed: drain: 109-113`). Same reasoning — separate ticket. Once logs are in place, the retry loop will be loud, which is itself the diagnostic signal.
- Logging in the MCP server's per-tool request handlers, `tusk query`, `tusk doctor`, `tusk edge`, `tusk node`, `tusk pack`, `tusk init`, `tusk status`. These commands are short-lived and synchronous; today's pain is in the long-running indexing path. They can adopt the same logger when needed.
- A `--log-level=warn|info|debug` flag. The boolean covers the warn-vs-debug split that pain motivates; finer levels can be added when a third consumer asks for them.
- A `--log-format=json` flag. Text output is the right default for a CLI; JSON can be added if a programmatic consumer appears.
- Logger plumbing into `internal/watcher`. The CLI command layer logs filesystem events; the watcher package itself stays logger-free.

### 1.3 Backward compatibility

- Without `--verbose`, output on stdout for `tusk reindex` and `tusk watch` remains exactly what it is today (the human summary line and the `[KIND] path` event lines respectively). New log output goes to **stderr**, never stdout, so scripts parsing stdout are unaffected.
- Default log level is `Warn`. Today's reindex shows no embed errors at all; after this change the operator sees one `Warn` line per failed embed. That is a deliberate behavior change — silence on embed failure is itself the bug we are fixing.
- The MCP server's stdio transport uses stdin/stdout for JSON-RPC; logs go to stderr and do not collide.

---

## 2. Background — Why structured logs, why now

Today the only `slog` usage in the repo is two `Logger *slog.Logger // optional; nil silences output` fields on `mcp.DrainerConfig` and `mcp.WatcherConfig`. Both are nil-checked, both are never set by any caller, both produce no output. The pattern is correct; the wiring is missing.

When the embed pipeline failed on 2026-05-13, the symptoms were:

- `tusk reindex` produced no stdout, no stderr, no progress output for 12 minutes.
- The `.tusk/index.db-wal` file grew to 4.1 MB, which looked like progress but was unrelated to embedding success.
- `bytes_sent` on the TCP socket to Ollama climbed to ~4 GB, which also looked like progress.
- The actual diagnostic — that every single POST returned 500 with `input length exceeds the context length` — was only visible by `tail`-ing `~/.ollama/serve.log`.

The minimum bar for this spec: in the equivalent future incident, `tusk reindex` (without `--verbose`) prints a `Warn`-level line on the first failed embed that names the node, the payload size, the model, and the error string. That single line would have collapsed today's investigation from "watch sockets and Ollama logs" to "read the first line of stderr".

---

## 3. Surface change — CLI

### 3.1 Root flag

In `cmd/tusk/root.go`:

```go
rootCmd.PersistentFlags().BoolP("verbose", "v", false, "emit debug-level logs to stderr")
```

The flag is read inside each `RunE` via `cmd.Flags().GetBool("verbose")`. No global package state.

### 3.2 Logger helper

New file `cmd/tusk/logger.go`:

```go
package main

import (
    "io"
    "log/slog"
)

// newLogger builds the stderr logger used by long-running commands.
// Default level is Warn; --verbose drops it to Debug.
func newLogger(out io.Writer, verbose bool) *slog.Logger {
    level := slog.LevelWarn
    if verbose {
        level = slog.LevelDebug
    }

    handler := slog.NewTextHandler(out, &slog.HandlerOptions{Level: level})
    return slog.New(handler)
}
```

The `io.Writer` parameter is `cmd.ErrOrStderr()` from each command's `RunE`, so tests can capture log output by passing a `bytes.Buffer`.

### 3.3 Per-command wiring

- `cmd_reindex.go`: build the logger once at the top of `RunE`, pass it into `reindex.Config.Logger` and (transitively, via `reindex.Run`) into `embed.DrainConfig.Logger`.
- `cmd_watch.go`: build the logger once, pass into `reindex.Config.Logger` for the initial reindex and into each per-event reindex inside the handler. Replace the existing `fmt.Fprintln(cmd.OutOrStdout(), "Initial reindex …")` and `fmt.Fprintf(... "[CREATE] path")` calls with structured `Info` / `Debug` log calls on the same logger. The "watching for changes (Ctrl-C to stop)…" line stays on stdout as user-facing UX.
- `cmd_mcp.go`: build the logger once and pass it through a new `mcp.Open(cwd, mcp.WithLogger(logger))` functional option (or equivalent — see §4.3), so the existing `Logger` fields on `mcp.DrainerConfig` and `mcp.WatcherConfig` start firing.

---

## 4. Plumbing — what changes in each package

### 4.1 `internal/embed`

`DrainConfig` already has the right shape for a new field:

```go
type DrainConfig struct {
    // ...existing fields...
    Logger *slog.Logger // optional; nil silences output
}
```

`OllamaConfig` gains a `Logger *slog.Logger` field. `NewOllamaEmbedder` keeps its single-argument signature. The HTTP call site (`Embed`) logs at `Debug` on each request and at `Warn` on each non-2xx response — including the response body, so future "input length exceeds the context length" errors are visible without needing to read Ollama's logs.

### 4.2 `internal/reindex`

`Config` adds:

```go
type Config struct {
    // ...existing fields...
    Logger *slog.Logger // optional; nil silences output
}
```

`Run` forwards `Logger` into the `embed.DrainConfig` it builds internally, so the reindex caller only configures the logger in one place.

### 4.3 `internal/mcp`

The existing `Logger` fields on `DrainerConfig` and `WatcherConfig` stay. The change is at the runtime entrypoint: `mcp.Open` gains a variadic functional-option parameter so callers without a logger keep the current call shape and the CLI can opt in:

```go
type Option func(*Runtime)

func WithLogger(logger *slog.Logger) Option { ... }

func Open(workspaceRoot string, opts ...Option) (*Runtime, error) { ... }
```

`Runtime` gains a `Logger *slog.Logger` field that `Open` populates from the options, and the runtime's internal callers (`Server.RunBackground` and friends) forward it into `DrainerConfig.Logger` and `WatcherConfig.Logger` when constructing those configs.

### 4.4 `internal/watcher`

Unchanged. No logger field. The CLI command layer in `cmd_watch.go` and `internal/mcp/watch.go` is where filesystem events are logged.

---

## 5. Event set

All log calls are `cmd.ErrOrStderr()`-bound and use `slog`'s key=value text format. Levels are chosen so a default run shows nothing under healthy conditions and one Warn line per anomaly under failure conditions; `--verbose` adds per-attempt detail.

### 5.1 `internal/embed/drain.go`

- **`Warn` — embed call failed.** Fired in the existing error branch at `drain.go:109`. Keys: `node_id`, `payload_bytes`, `model`, `err`. This is the single most important log line in the whole spec; it is what would have surfaced the 2026-05-13 bug on the first attempt.
- **`Warn` — node re-enqueued after failure.** Fired immediately after the above, when `Queue.Enqueue` + `Queue.MarkFailed` run on the same node. Keys: `node_id`. Distinct from the embed-failed line because seeing this line repeat for the same `node_id` is the signal for the retry-forever bug.
- **`Info` — drain batch complete.** Fired once per outer-loop batch (`drain.go:60`). Keys: `attempted`, `succeeded`, `failed`. Lets the operator see drain throughput without going to Debug.
- **`Debug` — per-attempt start.** Keys: `node_id`, `payload_bytes`, `chunks`. Fired before `Embedder.Embed`.
- **`Debug` — per-attempt success.** Keys: `node_id`, `vector_dim`, `latency_ms`. Fired after a successful `Embeddings.Upsert`.

### 5.2 `internal/embed/ollama.go`

- **`Debug` — request.** Keys: `endpoint`, `model`, `bytes_sent`. Fired before the HTTP POST.
- **`Warn` — non-2xx response.** Keys: `endpoint`, `model`, `status`, `latency_ms`, `body` (truncated to ~512 bytes). Fired when the response code is not 200. This is what makes Ollama's own error message — "input length exceeds the context length" — visible from inside tusk.
- **`Debug` — successful response.** Keys: `endpoint`, `model`, `status`, `latency_ms`, `vector_dim`.

### 5.3 `internal/reindex`

- **`Info` — walk start.** Keys: `root`, `ignore_patterns_count`.
- **`Info` — walk complete.** Keys: `indexed`, `removed`, `skipped`, `duration_ms`, plus the violation counters from the existing `Report` struct.
- **`Debug` — per-file outcome.** Keys: `path`, `outcome` (one of `indexed`, `skipped-ignored`, `skipped-unchanged`, `removed`, `error`). For an `error` outcome, also include `err`.

### 5.4 `cmd/tusk/cmd_watch.go`

- **`Info` — watcher started.** Keys: `root`.
- **`Debug` — filesystem event.** Keys: `kind` (CREATE / MODIFY / RENAME / DELETE), `path`. Replaces today's `[KIND] path` line on stdout. The stdout line stays for human UX.
- **`Warn` — per-event reindex error.** Keys: `path`, `err`. Today this just propagates as a returned error from the handler; the log call makes the failing path visible before the watcher exits.

### 5.5 `internal/mcp` (existing logger sites)

No new call sites required. The existing `Logger.Warn("drainer error", ...)` and `Logger.Info("drainer batch", ...)` calls in `internal/mcp/drainer.go` and `internal/mcp/watch.go` already cover the right events; this spec wires up the logger that has been nil-passed since they were written.

---

## 6. Testing

- `cmd/tusk/cmd_reindex_test.go`: add a test that runs reindex against a fixture with an embedder that returns an error, captures stderr, and asserts the Warn line includes the node ID, payload bytes, model name, and error string.
- `cmd/tusk/cmd_watch_test.go`: add a test that captures stderr for a single CREATE event and asserts the Debug line under `--verbose` and absence of that line without.
- `internal/embed/drain_test.go`: add a `Logger` field to the existing harness and assert the embed-failed and re-enqueued lines are emitted in the failure path.
- `internal/embed/ollama_test.go`: assert the Warn line on non-2xx including the truncated response body.
- `internal/reindex/run_test.go`: assert the walk-start and walk-complete Info lines under `--verbose` and absence under default.

All tests use `slog.NewTextHandler` against a `bytes.Buffer`, so assertions are simple `strings.Contains` checks on the captured output.

---

## 7. Future work (ledger)

1. Fix `WholeDocument` chunking — token-aware splitting that respects the configured model's context window. The 2026-05-13 incident is rooted here; this spec only makes it visible.
2. Fix the infinite-retry loop in `DrainQueue`. Options: attempt counter on `embed_queue` rows with a terminal-failure state, exponential backoff per node, or refusal to enqueue payloads above a configured byte threshold.
3. Roll the logger into the remaining commands (`query`, `doctor`, `edge`, `node`, `pack`, `init`, `status`) once a second consumer asks for it.
4. `--log-format=json` for programmatic consumption.
5. `--log-level=warn|info|debug` if the boolean stops being enough.
