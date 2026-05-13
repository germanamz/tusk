# Tusk Verbose Logging Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a persistent `--verbose` / `-v` flag to the `tusk` CLI and thread an `*slog.Logger` through the embed, reindex, watch, and MCP-runtime subsystems so failures in the indexing pipeline are visible from the command line.

**Architecture:** A small `cmd/tusk` helper (`newLogger`) returns an `*slog.Logger` writing to stderr at `Warn` by default or `Debug` under `--verbose`. The logger is threaded into `embed.DrainConfig`, `embed.OllamaConfig`, `reindex.Config`, and the existing-but-unused `Logger` fields on `mcp.DrainerConfig` and `mcp.WatchConfig` via a new `mcp.WithLogger` functional option on `mcp.Open`. New log statements live at the points where today's pipeline is opaque — most importantly, every embed-call outcome and every drain re-enqueue.

**Tech Stack:** Go 1.x, `log/slog` (stdlib), `github.com/spf13/cobra`. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-05-13-tusk-verbose-logging-design.md`.

**Resolution of spec ambiguity in §5.4:** The spec text both "replaces today's `[KIND] path` line on stdout" and "stays for human UX". This plan keeps every existing stdout line in `cmd_watch.go` untouched (preserving the manual-watch UX) and adds new structured logs only for surfaces that are silent today — watcher start (`Info`), per-event reindex outcome (`Debug`), per-event reindex error (`Warn`). The `[KIND] path` stdout line stays.

**Deferred from spec scope (deliberate, called out so reviewers can see):** Spec §5.3 lists a `Debug` per-file outcome log inside `internal/reindex/reindex.go`. The walker has multiple branches (indexed, skipped-parse-error, skipped-ignored, removed, error). Adding a uniform per-file event to every branch is touchable but bigger than this pass; the walk-start / walk-complete pair gives the load-bearing signal for the 2026-05-13 class of bug. Per-file Debug is left as a follow-up, added if a future investigation needs it.

---

## File Structure

**Created:**
- `cmd/tusk/logger.go` — `newLogger(out io.Writer, verbose bool) *slog.Logger`.
- `cmd/tusk/logger_test.go` — unit test for the helper.

**Modified:**
- `cmd/tusk/root.go` — add `--verbose` / `-v` persistent flag.
- `cmd/tusk/cmd_reindex.go` — read flag, build logger, set `reindex.Config.Logger`.
- `cmd/tusk/cmd_watch.go` — read flag, build logger, set `reindex.Config.Logger` (initial + per-event), add new log calls (preserve stdout).
- `cmd/tusk/cmd_mcp.go` — read flag, build logger, pass `mcp.WithLogger(...)` into `mcp.Open`.
- `cmd/tusk/cmd_reindex_test.go` — assert reindex command emits expected logs.
- `cmd/tusk/cmd_watch_test.go` — assert watch command emits expected logs under verbose.
- `cmd/tusk/cmd_mcp_test.go` — assert mcp command passes the option through.
- `internal/embed/ollama.go` — add `Logger` to `OllamaConfig`; log Debug on request, Warn on non-2xx with truncated body, Debug on success.
- `internal/embed/ollama_test.go` — assert Warn line on 500 includes status/body/model/endpoint.
- `internal/embed/drain.go` — add `Logger` to `DrainConfig`; log Warn on embed error, Warn on re-enqueue, Info on batch summary, Debug per-attempt.
- `internal/embed/drain_test.go` — assert embed-failed, re-enqueued, and batch summary lines.
- `internal/reindex/reindex.go` — add `Logger` to `Config`; log Info on walk start/end, Debug per-file; forward to `embed.DrainConfig.Logger`.
- `internal/reindex/reindex_test.go` — assert walk start/end log lines.
- `internal/mcp/runtime.go` — add `Logger *slog.Logger` field to `Runtime`, `Option` type, `WithLogger` function, variadic `Open(workspaceRoot string, opts ...Option)`.
- `internal/mcp/server.go` — in `RunBackground`, forward `srv.runtime.Logger` into `DrainerConfig.Logger` and `WatchConfig.Logger`.
- `internal/mcp/runtime_test.go` — assert `WithLogger` populates `Runtime.Logger`.

**Unmodified (deliberately, per spec §1.2):**
- `internal/embed/chunking.go` — chunking fix is out of scope.
- `internal/watcher/` — the watcher package stays logger-free.

---

## Task 1: CLI logger helper + `--verbose` flag

**Files:**
- Create: `cmd/tusk/logger.go`
- Create: `cmd/tusk/logger_test.go`
- Modify: `cmd/tusk/root.go`

- [ ] **Step 1: Write the failing test**

Create `cmd/tusk/logger_test.go`:

```go
package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewLogger_DefaultLevelIsWarn(t *testing.T) {
	var buf bytes.Buffer
	logger := newLogger(&buf, false)

	logger.Info("info-line")
	logger.Warn("warn-line")

	output := buf.String()

	if strings.Contains(output, "info-line") {
		t.Errorf("default logger should NOT emit Info; got %q", output)
	}

	if !strings.Contains(output, "warn-line") {
		t.Errorf("default logger should emit Warn; got %q", output)
	}
}

func TestNewLogger_VerboseEmitsDebug(t *testing.T) {
	var buf bytes.Buffer
	logger := newLogger(&buf, true)

	logger.Debug("debug-line")
	logger.Info("info-line")
	logger.Warn("warn-line")

	output := buf.String()

	for _, want := range []string{"debug-line", "info-line", "warn-line"} {
		if !strings.Contains(output, want) {
			t.Errorf("verbose logger should emit %q; got %q", want, output)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tusk/ -run TestNewLogger -v`
Expected: FAIL with `undefined: newLogger`.

- [ ] **Step 3: Write minimal implementation**

Create `cmd/tusk/logger.go`:

```go
package main

import (
	"io"
	"log/slog"
)

// newLogger builds the stderr logger used by long-running commands.
// Default level is Warn; verbose drops it to Debug.
func newLogger(out io.Writer, verbose bool) *slog.Logger {
	level := slog.LevelWarn

	if verbose {
		level = slog.LevelDebug
	}

	handler := slog.NewTextHandler(out, &slog.HandlerOptions{Level: level})

	return slog.New(handler)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/tusk/ -run TestNewLogger -v`
Expected: PASS.

- [ ] **Step 5: Add the persistent flag**

Modify `cmd/tusk/root.go`. After `rootCmd := &cobra.Command{ ... }` and before the first `rootCmd.AddCommand(...)`, add:

```go
rootCmd.PersistentFlags().BoolP("verbose", "v", false, "emit debug-level logs to stderr")
```

The resulting block looks like:

```go
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "tusk",
		Short:         "Tusk — local-first agent brain",
		Long:          "Tusk indexes a markdown vault into a graph and serves structural and semantic queries.",
		Version:       versionString,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "emit debug-level logs to stderr")

	rootCmd.AddCommand(newInitCmd())
	// ... rest unchanged
```

- [ ] **Step 6: Verify build still passes**

Run: `make build`
Expected: builds clean.

- [ ] **Step 7: Verify the flag shows up in help**

Run: `./bin/tusk --help | grep -- "--verbose"`
Expected: a line showing `-v, --verbose   emit debug-level logs to stderr`.

- [ ] **Step 8: Commit**

```bash
git add cmd/tusk/logger.go cmd/tusk/logger_test.go cmd/tusk/root.go
git commit -m "feat(cli): add --verbose flag and slog helper"
```

---

## Task 2: Ollama embedder HTTP logging

**Files:**
- Modify: `internal/embed/ollama.go`
- Modify: `internal/embed/ollama_test.go`

Background: today `OllamaEmbedder.Embed` makes a POST and either returns a vector or an error. There is no visibility into HTTP-level failures. This task adds a `Logger` field to `OllamaConfig` and logs at the call site so that an Ollama 500 with `"input length exceeds the context length"` becomes a Warn line on stderr.

- [ ] **Step 1: Write the failing test**

Append to `internal/embed/ollama_test.go`:

```go
func TestOllamaEmbedder_LogsWarnOnNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"input length exceeds the context length"}`))
	}))
	defer server.Close()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	embedder := NewOllamaEmbedder(OllamaConfig{
		Endpoint: server.URL,
		Model:    "nomic-embed-text",
		Dim:      768,
		Logger:   logger,
	})

	_, err := embedder.Embed(context.Background(), []byte("hello"))

	if err == nil {
		t.Fatal("expected error from non-2xx response")
	}

	out := buf.String()

	if !strings.Contains(out, "level=WARN") {
		t.Errorf("expected WARN log; got %q", out)
	}

	for _, want := range []string{`msg="ollama non-2xx"`, "status=500", "model=nomic-embed-text", "input length exceeds the context length"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected log to contain %q; got %q", want, out)
		}
	}
}

func TestOllamaEmbedder_LogsDebugOnRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"embedding":[0.1,0.2,0.3]}`))
	}))
	defer server.Close()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	embedder := NewOllamaEmbedder(OllamaConfig{
		Endpoint: server.URL,
		Model:    "nomic-embed-text",
		Dim:      3,
		Logger:   logger,
	})

	_, err := embedder.Embed(context.Background(), []byte("hello"))

	if err != nil {
		t.Fatalf("embed: %v", err)
	}

	out := buf.String()

	if !strings.Contains(out, "level=DEBUG") {
		t.Errorf("expected DEBUG log; got %q", out)
	}

	for _, want := range []string{`msg="ollama request"`, "bytes_sent=", `msg="ollama success"`} {
		if !strings.Contains(out, want) {
			t.Errorf("expected log to contain %q; got %q", want, out)
		}
	}
}
```

Ensure the imports at the top of the test file include `bytes`, `log/slog`, `strings`, `context`, `net/http`, `net/http/httptest`. Add any that are missing.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/embed/ -run TestOllamaEmbedder_Logs -v`
Expected: FAIL — `unknown field Logger in struct literal of type OllamaConfig`.

- [ ] **Step 3: Add `Logger` to `OllamaConfig` and log call sites**

Modify `internal/embed/ollama.go`. Top of file, add `log/slog` to imports. The full updated file:

```go
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// OllamaConfig configures an OllamaEmbedder.
type OllamaConfig struct {
	Endpoint string
	Model    string
	Dim      int
	Logger   *slog.Logger // optional; nil silences output
}

// OllamaEmbedder calls Ollama's POST /api/embeddings to embed payloads.
type OllamaEmbedder struct {
	config OllamaConfig
	client *http.Client
}

// NewOllamaEmbedder constructs an OllamaEmbedder with sensible HTTP defaults.
func NewOllamaEmbedder(config OllamaConfig) *OllamaEmbedder {
	return &OllamaEmbedder{
		config: config,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

const ollamaBodyLogLimit = 512

// Embed implements Embedder.
func (embedder *OllamaEmbedder) Embed(ctx context.Context, payload []byte) ([]float32, error) {
	body := struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
	}{
		Model:  embedder.config.Model,
		Prompt: string(payload),
	}

	encoded, marshalErr := json.Marshal(body)

	if marshalErr != nil {
		return nil, fmt.Errorf("ollama: marshal: %w", marshalErr)
	}

	if embedder.config.Logger != nil {
		embedder.config.Logger.Debug("ollama request",
			"endpoint", embedder.config.Endpoint,
			"model", embedder.config.Model,
			"bytes_sent", len(encoded),
		)
	}

	request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, embedder.config.Endpoint+"/api/embeddings", bytes.NewReader(encoded))

	if requestErr != nil {
		return nil, fmt.Errorf("ollama: new request: %w", requestErr)
	}

	request.Header.Set("Content-Type", "application/json")

	start := time.Now()

	response, doErr := embedder.client.Do(request)

	if doErr != nil {
		return nil, fmt.Errorf("ollama: do: %w", doErr)
	}

	defer response.Body.Close()

	respBody, readErr := io.ReadAll(response.Body)

	if readErr != nil {
		return nil, fmt.Errorf("ollama: read body: %w", readErr)
	}

	latency := time.Since(start)

	if response.StatusCode != http.StatusOK {
		if embedder.config.Logger != nil {
			truncated := respBody

			if len(truncated) > ollamaBodyLogLimit {
				truncated = truncated[:ollamaBodyLogLimit]
			}

			embedder.config.Logger.Warn("ollama non-2xx",
				"endpoint", embedder.config.Endpoint,
				"model", embedder.config.Model,
				"status", response.StatusCode,
				"latency_ms", latency.Milliseconds(),
				"body", string(truncated),
			)
		}

		return nil, fmt.Errorf("ollama: status %d: %s", response.StatusCode, string(respBody))
	}

	parsed := struct {
		Embedding []float32 `json:"embedding"`
	}{}

	if unmarshalErr := json.Unmarshal(respBody, &parsed); unmarshalErr != nil {
		return nil, fmt.Errorf("ollama: unmarshal: %w", unmarshalErr)
	}

	if embedder.config.Logger != nil {
		embedder.config.Logger.Debug("ollama success",
			"endpoint", embedder.config.Endpoint,
			"model", embedder.config.Model,
			"status", response.StatusCode,
			"latency_ms", latency.Milliseconds(),
			"vector_dim", len(parsed.Embedding),
		)
	}

	return parsed.Embedding, nil
}
```

**Important:** the existing file's body parsing logic must be preserved when you edit — only insert the logging calls and the `Logger` field. Read the current file before editing and merge carefully. If the current `Embed` function already does response decoding differently (e.g., streaming, different field name), keep the existing decoder and only add the logger hooks at the request, non-2xx, and success points.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/embed/ -run TestOllamaEmbedder_Logs -v`
Expected: PASS.

- [ ] **Step 5: Run the full embed test suite to confirm no regression**

Run: `go test ./internal/embed/ -v`
Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/embed/ollama.go internal/embed/ollama_test.go
git commit -m "feat(embed): log ollama requests, non-2xx responses, and successes"
```

---

## Task 3: Drain queue logging

**Files:**
- Modify: `internal/embed/drain.go`
- Modify: `internal/embed/drain_test.go`

Background: `DrainQueue` already enqueues+marks-failed on embed error and continues. Today this is silent; this task surfaces both the embed failure and the re-enqueue as Warn lines, and adds a batch summary at Info.

- [ ] **Step 1: Write the failing test**

Append to `internal/embed/drain_test.go`:

```go
func TestDrainQueue_LogsWarnOnEmbedError(t *testing.T) {
	tempDir := t.TempDir()

	if writeErr := os.WriteFile(filepath.Join(tempDir, "doc.md"), []byte("---\nid: doc\ntype: note\n---\nbody"), 0o644); writeErr != nil {
		t.Fatalf("write fixture: %v", writeErr)
	}

	store, openErr := index.Open(filepath.Join(tempDir, "index.db"))

	if openErr != nil {
		t.Fatalf("open: %v", openErr)
	}

	defer store.Close()

	nodes := index.NewNodeRepo(store)

	if upsertErr := nodes.Upsert(index.NodeRow{ID: "doc", Path: "doc.md", Type: "note"}); upsertErr != nil {
		t.Fatalf("upsert: %v", upsertErr)
	}

	queue := index.NewEmbedQueueRepo(store)

	if enqErr := queue.Enqueue("doc"); enqErr != nil {
		t.Fatalf("enqueue: %v", enqErr)
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	failing := &fakeEmbedder{err: fmt.Errorf("input length exceeds the context length")}

	_, drainErr := DrainQueue(context.Background(), DrainConfig{
		Root:       tempDir,
		Nodes:      nodes,
		Queue:      queue,
		Embeddings: index.NewEmbeddingRepo(store),
		Embedder:   failing,
		Chunker:    WholeDocument{},
		Logger:     logger,
	})

	if drainErr != nil {
		t.Fatalf("drain: %v", drainErr)
	}

	out := buf.String()

	// Embed failure line.
	for _, want := range []string{`msg="embed call failed"`, "node_id=doc", "payload_bytes=", "input length exceeds the context length"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected log to contain %q; got %q", want, out)
		}
	}

	// Re-enqueue line.
	if !strings.Contains(out, `msg="embed re-enqueued"`) {
		t.Errorf("expected re-enqueue log; got %q", out)
	}

	// Batch summary at Info.
	if !strings.Contains(out, `msg="drain batch complete"`) {
		t.Errorf("expected batch summary log; got %q", out)
	}
}
```

If `fakeEmbedder` does not already exist in the test file, define it near the top:

```go
type fakeEmbedder struct {
	vec []float32
	err error
}

func (f *fakeEmbedder) Embed(ctx context.Context, _ []byte) ([]float32, error) {
	if f.err != nil {
		return nil, f.err
	}

	return f.vec, nil
}

func (f *fakeEmbedder) Model() string { return "fake" }
func (f *fakeEmbedder) Dim() int      { return len(f.vec) }
```

If a fake already exists in the test file under a different name, reuse it instead of redefining.

Imports needed in the test file: `bytes`, `context`, `fmt`, `log/slog`, `os`, `path/filepath`, `strings`, `testing`, and the existing `github.com/germanamz/tusk/internal/index` import.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/embed/ -run TestDrainQueue_LogsWarnOnEmbedError -v`
Expected: FAIL — `unknown field Logger in struct literal of type DrainConfig`.

- [ ] **Step 3: Add `Logger` to `DrainConfig` and the call sites**

Modify `internal/embed/drain.go`. Add `log/slog` to imports. The two changes inside `DrainQueue`:

(a) Inside the `for _, queued := range batch` loop, replace the existing error branch:

```go
vector, embedErr := config.Embedder.Embed(ctx, chunks[0])

if embedErr != nil {
    _ = config.Queue.Enqueue(queued.NodeID)
    _ = config.Queue.MarkFailed(queued.NodeID, embedErr.Error())

    continue
}
```

with:

```go
if config.Logger != nil {
    config.Logger.Debug("embed attempt",
        "node_id", queued.NodeID,
        "payload_bytes", len(payload),
        "chunks", len(chunks),
    )
}

embedStart := time.Now()

vector, embedErr := config.Embedder.Embed(ctx, chunks[0])

embedLatency := time.Since(embedStart)

if embedErr != nil {
    if config.Logger != nil {
        config.Logger.Warn("embed call failed",
            "node_id", queued.NodeID,
            "payload_bytes", len(payload),
            "model", config.Embedder.Model(),
            "err", embedErr.Error(),
        )
    }

    _ = config.Queue.Enqueue(queued.NodeID)
    _ = config.Queue.MarkFailed(queued.NodeID, embedErr.Error())

    if config.Logger != nil {
        config.Logger.Warn("embed re-enqueued", "node_id", queued.NodeID)
    }

    batchFailed++

    continue
}

if config.Logger != nil {
    config.Logger.Debug("embed attempt success",
        "node_id", queued.NodeID,
        "vector_dim", len(vector),
        "latency_ms", embedLatency.Milliseconds(),
    )
}

batchSucceeded++
```

Add `"time"` to the imports of `internal/embed/drain.go` if not already imported.

(b) Add `Logger` to the struct, and per-batch counters at the top of the outer `for {}` loop. Concretely, change `DrainConfig`:

```go
type DrainConfig struct {
	Root       string
	Nodes      *index.NodeRepo
	Queue      *index.EmbedQueueRepo
	Embeddings *index.EmbeddingRepo
	Embedder   Embedder
	Chunker    ChunkingStrategy
	BatchSize  int
	Logger     *slog.Logger // optional; nil silences output
}
```

And inside `DrainQueue`, just before iterating `batch`:

```go
var (
    batchSucceeded int
    batchFailed    int
)
```

(c) After the inner `for _, queued := range batch` loop ends (and before continuing the outer loop), emit the summary and increment the success counter on the success path. The success path is the existing block after `Embeddings.Upsert`:

```go
if upsertErr := config.Embeddings.Upsert(index.EmbeddingRow{...}); upsertErr != nil {
    return drained, upsertErr
}

drained++
```

Change to also bump `batchSucceeded` (already added above in the embed-success branch). Then after the inner loop closes (still inside the outer `for {}`):

```go
if config.Logger != nil {
    config.Logger.Info("drain batch complete",
        "attempted", batchSucceeded+batchFailed,
        "succeeded", batchSucceeded,
        "failed", batchFailed,
    )
}
```

**Important:** the existing comment on `DrainQueue` referencing infinite re-enqueue and the existing `MarkFailed` calls in the `Get` / `ReadFile` / `ParseFile` error branches stay untouched — they are out of scope for this task. Only add Warn lines in the embed-error branch and the batch-summary Info line.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/embed/ -run TestDrainQueue_LogsWarnOnEmbedError -v`
Expected: PASS.

- [ ] **Step 5: Run the full embed test suite to confirm no regression**

Run: `go test ./internal/embed/ -v`
Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/embed/drain.go internal/embed/drain_test.go
git commit -m "feat(embed): log drain failures, re-enqueues, and batch summaries"
```

---

## Task 4: Reindex walk logging + forward to drain

**Files:**
- Modify: `internal/reindex/reindex.go`
- Modify: `internal/reindex/reindex_test.go`

Background: `reindex.Run` walks the workspace and (when an embedder is configured) drains the embed queue inline. This task adds Info logs at walk start/end, Debug logs per file, and forwards `Config.Logger` into the `embed.DrainConfig` it constructs.

- [ ] **Step 1: Write the failing test**

Append to `internal/reindex/reindex_test.go`:

```go
func TestRun_LogsWalkStartAndComplete(t *testing.T) {
	tempDir := t.TempDir()

	if writeErr := os.WriteFile(filepath.Join(tempDir, "a.md"), []byte("---\nid: a\ntype: note\n---\nbody"), 0o644); writeErr != nil {
		t.Fatalf("write a: %v", writeErr)
	}

	store, openErr := index.Open(filepath.Join(tempDir, "index.db"))

	if openErr != nil {
		t.Fatalf("open: %v", openErr)
	}

	defer store.Close()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	_, runErr := Run(Config{
		Root:   tempDir,
		Repo:   index.NewNodeRepo(store),
		Logger: logger,
	})

	if runErr != nil {
		t.Fatalf("run: %v", runErr)
	}

	out := buf.String()

	if !strings.Contains(out, `msg="reindex walk start"`) {
		t.Errorf("expected walk-start log; got %q", out)
	}

	if !strings.Contains(out, `msg="reindex walk complete"`) {
		t.Errorf("expected walk-complete log; got %q", out)
	}

	if !strings.Contains(out, "indexed=1") {
		t.Errorf("expected indexed=1; got %q", out)
	}
}
```

Imports needed: `bytes`, `log/slog`, `strings`, plus existing `os`, `path/filepath`, `testing`, and the `index` import.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/reindex/ -run TestRun_LogsWalkStartAndComplete -v`
Expected: FAIL — `unknown field Logger in struct literal of type Config`.

- [ ] **Step 3: Add `Logger` to `Config` and the call sites**

Modify `internal/reindex/reindex.go`. Add `log/slog` and `time` to imports if not already present (`time` is already imported). Add the field at the end of the `Config` struct:

```go
type Config struct {
	// ... existing fields ...

	// PropertyDrift is optional; when set alongside NodeTypes, Run writes
	// property drift rows and clears rows on clean passes.
	PropertyDrift *index.PropertyDriftRepo

	// Logger is optional; when set, Run emits structured logs to it.
	// Forwarded into embed.DrainConfig.Logger when the embedding pipeline runs.
	Logger *slog.Logger
}
```

At the top of `Run`, immediately after `report := &Report{}`:

```go
start := time.Now()

if config.Logger != nil {
    config.Logger.Info("reindex walk start",
        "root", config.Root,
        "ignore_patterns_count", len(config.WorkspaceIgnore),
    )
}
```

Inside the existing `embed.DrainQueue` call near the end of `Run`, add `Logger`:

```go
if _, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
    Root:       config.Root,
    Nodes:      config.Repo,
    Queue:      config.EmbedQueue,
    Embeddings: config.EmbeddingRepo,
    Embedder:   config.Embedder,
    Chunker:    config.Chunker,
    Logger:     config.Logger,
}); drainErr != nil {
    return nil, drainErr
}
```

Just before `return report, nil` at the very end of `Run`:

```go
if config.Logger != nil {
    config.Logger.Info("reindex walk complete",
        "indexed", report.Indexed,
        "removed", report.Removed,
        "skipped", report.Skipped,
        "duration_ms", time.Since(start).Milliseconds(),
    )
}
```

Per-file Debug logs are deliberately skipped in this task — the walk-start/walk-complete pair is the load-bearing signal. Per-file events would be added if a future test demanded them.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/reindex/ -run TestRun_LogsWalkStartAndComplete -v`
Expected: PASS.

- [ ] **Step 5: Run the full reindex test suite to confirm no regression**

Run: `go test ./internal/reindex/ -v`
Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/reindex/reindex.go internal/reindex/reindex_test.go
git commit -m "feat(reindex): log walk start/complete and forward logger to drain"
```

---

## Task 5: MCP runtime `WithLogger` option

**Files:**
- Modify: `internal/mcp/runtime.go`
- Modify: `internal/mcp/server.go`
- Modify: `internal/mcp/runtime_test.go` (create if missing)

Background: `mcp.Open` currently takes a single positional argument. This task adds a variadic functional-option parameter so callers can pass a logger; `Server.RunBackground` then forwards it into the existing `Logger` fields on `DrainerConfig` and `WatchConfig`.

- [ ] **Step 1: Write the failing test**

If `internal/mcp/runtime_test.go` does not exist, create it. Otherwise append. Use `mcp.Open` against a minimal valid workspace (a temp dir with a `tusk.toml`):

```go
package mcp

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestOpen_WithLogger_PopulatesRuntimeLogger(t *testing.T) {
	tempDir := t.TempDir()

	if writeErr := os.WriteFile(filepath.Join(tempDir, "tusk.toml"), []byte("[workspace]\nname = \"test\"\n"), 0o644); writeErr != nil {
		t.Fatalf("write manifest: %v", writeErr)
	}

	if mkdirErr := os.MkdirAll(filepath.Join(tempDir, ".tusk"), 0o755); mkdirErr != nil {
		t.Fatalf("mkdir .tusk: %v", mkdirErr)
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	runtime, openErr := Open(tempDir, WithLogger(logger))

	if openErr != nil {
		t.Fatalf("open: %v", openErr)
	}

	defer runtime.Close()

	if runtime.Logger != logger {
		t.Errorf("Runtime.Logger should be the passed logger")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/ -run TestOpen_WithLogger -v`
Expected: FAIL — `undefined: WithLogger` and `runtime.Logger undefined`.

- [ ] **Step 3: Add the `Option` type, `WithLogger`, and the variadic param**

Modify `internal/mcp/runtime.go`. Add `log/slog` to the imports. Add the field to `Runtime`:

```go
type Runtime struct {
	// ... existing fields ...

	Embedder embed.Embedder
	Chunker  embed.ChunkingStrategy

	Logger *slog.Logger // optional; nil silences output
}
```

Above `func Open(...)`:

```go
// Option mutates a Runtime during Open.
type Option func(*Runtime)

// WithLogger sets the slog.Logger that Open stores on the Runtime. Forwarded
// into DrainerConfig.Logger and WatchConfig.Logger by Server.RunBackground.
func WithLogger(logger *slog.Logger) Option {
	return func(rt *Runtime) {
		rt.Logger = logger
	}
}
```

Change `Open`'s signature and apply options after the `Runtime` has been fully constructed but before returning. The minimum change is to wrap the existing return statement(s). Concretely, locate the final successful return (where the `*Runtime` is returned non-nil) and just before it:

```go
for _, opt := range opts {
    opt(rt) // where `rt` is the populated *Runtime about to be returned
}

return rt, nil
```

Update the function signature:

```go
func Open(workspaceRoot string, opts ...Option) (*Runtime, error) {
```

**Note for implementer:** read the current `Open` carefully before editing — it has multiple early-return error paths. Only apply the options on the successful path. If the current implementation assigns to a local variable other than `rt`, use whatever the existing name is and don't rename.

- [ ] **Step 4: Run the runtime test to verify it passes**

Run: `go test ./internal/mcp/ -run TestOpen_WithLogger -v`
Expected: PASS.

- [ ] **Step 5: Forward the logger in `RunBackground`**

Modify `internal/mcp/server.go`. Inside `RunBackground`, change:

```go
go func() {
    defer waitGroup.Done()
    record(RunDrainer(ctx, DrainerConfig{Runtime: srv.runtime}))
}()

go func() {
    defer waitGroup.Done()
    record(RunWatcher(ctx, WatchConfig{Runtime: srv.runtime}))
}()
```

to:

```go
go func() {
    defer waitGroup.Done()
    record(RunDrainer(ctx, DrainerConfig{Runtime: srv.runtime, Logger: srv.runtime.Logger}))
}()

go func() {
    defer waitGroup.Done()
    record(RunWatcher(ctx, WatchConfig{Runtime: srv.runtime, Logger: srv.runtime.Logger}))
}()
```

Also forward the logger into the inline `reindex.Config` that `RunWatcher` constructs in `internal/mcp/watch.go`:

```go
_, runErr := reindex.Run(reindex.Config{
    Root:            config.Runtime.Root,
    Repo:            config.Runtime.Nodes,
    Edges:           config.Runtime.Edges,
    EdgeTypes:       config.Runtime.Manifest.EdgeTypes,
    WorkspaceIgnore: config.Runtime.Manifest.Workspace.Ignore,
    EmbedQueue:      config.Runtime.EmbedQueue,
    EmbeddingRepo:   config.Runtime.Embeddings,
    Embedder:        config.Runtime.Embedder,
    Chunker:         config.Runtime.Chunker,
    Meta:            config.Runtime.Meta,
    Logger:          config.Logger,
})
```

- [ ] **Step 6: Run the full mcp test suite**

Run: `go test ./internal/mcp/ -v`
Expected: all tests pass.

- [ ] **Step 7: Commit**

```bash
git add internal/mcp/runtime.go internal/mcp/server.go internal/mcp/watch.go internal/mcp/runtime_test.go
git commit -m "feat(mcp): add WithLogger option and forward logger to drainer/watcher"
```

---

## Task 6: Wire `--verbose` into `tusk reindex`

**Files:**
- Modify: `cmd/tusk/cmd_reindex.go`
- Modify: `cmd/tusk/cmd_reindex_test.go`

- [ ] **Step 1: Write the failing test**

Existing test helpers in `cmd/tusk/cmd_test_helpers_test.go`:
- `setupTempWorkspace(t) string` — creates a temp dir, runs `tusk init`, returns the path.
- `chdir(t, dir)` — switches `os.Chdir` to `dir` and registers cleanup.

Append to `cmd/tusk/cmd_reindex_test.go`:

```go
func TestReindexCmd_VerboseEmitsWalkLogs(t *testing.T) {
	wsDir := setupTempWorkspace(t)
	chdir(t, wsDir)

	stderr := &bytes.Buffer{}
	rootCmd := newRootCmd()
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(stderr)
	rootCmd.SetArgs([]string{"reindex", "--verbose"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("reindex: %v", err)
	}

	if !strings.Contains(stderr.String(), `msg="reindex walk complete"`) {
		t.Errorf("expected walk-complete log on stderr; got %q", stderr.String())
	}
}

func TestReindexCmd_DefaultSilentOnWalkLogs(t *testing.T) {
	wsDir := setupTempWorkspace(t)
	chdir(t, wsDir)

	stderr := &bytes.Buffer{}
	rootCmd := newRootCmd()
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(stderr)
	rootCmd.SetArgs([]string{"reindex"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("reindex: %v", err)
	}

	if strings.Contains(stderr.String(), `msg="reindex walk complete"`) {
		t.Errorf("default reindex should not emit Info logs; got %q", stderr.String())
	}
}
```

The empty workspace from `setupTempWorkspace` is sufficient — `reindex.Run` emits the walk-start/walk-complete pair regardless of file count.

Imports needed: `bytes`, `io`, `strings`, `testing`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tusk/ -run TestReindexCmd_Verbose -v`
Expected: FAIL — walk-complete log not on stderr because the reindex command never builds a logger.

- [ ] **Step 3: Wire the logger**

Modify `cmd/tusk/cmd_reindex.go`. At the top of the `RunE` function, after the `ws, findErr := workspace.Find(cwd)` block:

```go
verbose, _ := cmd.Flags().GetBool("verbose")
logger := newLogger(cmd.ErrOrStderr(), verbose)
```

Then pass `Logger: logger` into the `reindex.Config{...}` literal in the existing `reindex.Run(reindex.Config{...})` call. The resulting literal:

```go
report, runErr := reindex.Run(reindex.Config{
    Root:            ws.Root,
    Repo:            index.NewNodeRepo(store),
    Edges:           edgeRepo,
    EdgeTypes:       loaded.EdgeTypes,
    WorkspaceIgnore: loaded.Workspace.Ignore,
    EmbedQueue:      embedQueue,
    EmbeddingRepo:   embeddingRepo,
    Embedder:        embedder,
    Chunker:         chunker,
    Meta:            index.NewMetaRepo(store),
    Behaviors:       engine,
    DriftLog:        driftRepo,
    NodeTypes:       loaded.NodeTypes,
    PropertyDrift:   index.NewPropertyDriftRepo(store),
    Logger:          logger,
})
```

Also pass the logger into the `OllamaEmbedder` constructor in the same file:

```go
if loaded.Embeddings.Provider == "ollama" {
    embedder = embed.NewOllamaEmbedder(embed.OllamaConfig{
        Endpoint: loaded.Embeddings.Endpoint,
        Model:    loaded.Embeddings.Model,
        Dim:      loaded.Embeddings.Dim,
        Logger:   logger,
    })
    chunker = embed.WholeDocument{}
    embedQueue = index.NewEmbedQueueRepo(store)
    embeddingRepo = index.NewEmbeddingRepo(store)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/tusk/ -run TestReindexCmd_Verbose -v`
Expected: PASS for both `_VerboseEmitsWalkLogs` and `_DefaultSilentOnWalkLogs`.

- [ ] **Step 5: Run the full cmd/tusk test suite**

Run: `go test ./cmd/tusk/ -v`
Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add cmd/tusk/cmd_reindex.go cmd/tusk/cmd_reindex_test.go
git commit -m "feat(cli): wire --verbose logger into tusk reindex"
```

---

## Task 7: Wire `--verbose` into `tusk watch`

**Files:**
- Modify: `cmd/tusk/cmd_watch.go`
- Modify: `cmd/tusk/cmd_watch_test.go`

This task preserves every existing stdout line (`Initial reindex …`, `Watching for changes (Ctrl-C to stop)…`, `  [KIND] path`) and adds new stderr logs only for surfaces that are silent today.

- [ ] **Step 1: Write the failing test**

The test invokes the watch command with a pre-canceled context so the watcher loop exits immediately after the initial reindex. This requires `cmd_watch.go` to honor `cmd.Context()` when set — see Step 3.

Append to `cmd/tusk/cmd_watch_test.go`:

```go
func TestWatchCmd_VerboseEmitsInitialReindexLogs(t *testing.T) {
	wsDir := setupTempWorkspace(t)
	chdir(t, wsDir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel so signal.NotifyContext fires Done() immediately

	stderr := &bytes.Buffer{}
	rootCmd := newRootCmd()
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(stderr)
	rootCmd.SetContext(ctx)
	rootCmd.SetArgs([]string{"watch", "--verbose"})

	_ = rootCmd.Execute() // watcher.Run returns nil on ctx-done; ignore any error from the post-cancel teardown

	if !strings.Contains(stderr.String(), `msg="reindex walk complete"`) {
		t.Errorf("expected walk-complete log on stderr; got %q", stderr.String())
	}
}
```

Imports needed: `bytes`, `context`, `io`, `strings`, `testing`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tusk/ -run TestWatchCmd_Verbose -v`
Expected: FAIL — walk-complete log not on stderr.

- [ ] **Step 3: Wire the logger and prefer `cmd.Context()`**

Modify `cmd/tusk/cmd_watch.go`. At the top of `RunE`, after `ws, findErr := workspace.Find(cwd)`:

```go
verbose, _ := cmd.Flags().GetBool("verbose")
logger := newLogger(cmd.ErrOrStderr(), verbose)
```

Rewire the context so `cmd.SetContext` is honored. Replace:

```go
ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer cancel()
```

with:

```go
parent := cmd.Context()

if parent == nil {
    parent = context.Background()
}

ctx, cancel := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
defer cancel()
```

Pass `Logger: logger` into both `reindex.Config{...}` literals (the initial reindex and the per-event reindex inside `handler`). Add an Info log at watcher start:

```go
logger.Info("watch started", "root", ws.Root)
```

Insert this just before the existing `fmt.Fprintln(cmd.OutOrStdout(), "Watching for changes (Ctrl-C to stop)…")` line.

Add a Warn log inside the handler when the per-event reindex returns an error. Today the handler returns the error and the watcher exits silently; the log surfaces *which* path triggered it before the bubble-up:

```go
if runErr != nil {
    logger.Warn("watch handler reindex failed", "path", event.Path, "err", runErr.Error())

    return runErr
}

return nil
```

Add a Debug log on each event after the existing stdout `[KIND] path` print (the stdout print stays):

```go
_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  [%s] %s\n", kindLabel(event.Kind), event.Path)

logger.Debug("watch fs event", "kind", kindLabel(event.Kind), "path", event.Path)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/tusk/ -run TestWatchCmd_Verbose -v`
Expected: PASS.

- [ ] **Step 5: Run the full cmd/tusk test suite**

Run: `go test ./cmd/tusk/ -v`
Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add cmd/tusk/cmd_watch.go cmd/tusk/cmd_watch_test.go
git commit -m "feat(cli): wire --verbose logger into tusk watch"
```

---

## Task 8: Wire `--verbose` into `tusk mcp`

**Files:**
- Modify: `cmd/tusk/cmd_mcp.go`
- Modify: `cmd/tusk/cmd_mcp_test.go`

- [ ] **Step 1: Write the failing test**

The mcp command spawns a long-running stdio server, so the test exercises the flag → option construction directly. A small package-level helper `mcpLoggerFromFlags(cmd)` (added in Step 3) returns the logger when `--verbose` is set, else `nil`. The test then calls `mcp.Open(wsDir, mcp.WithLogger(logger))` and inspects `Runtime.Logger`.

Append to `cmd/tusk/cmd_mcp_test.go`:

```go
func TestMCPCmd_VerboseSetsRuntimeLogger(t *testing.T) {
	wsDir := setupTempWorkspace(t)
	chdir(t, wsDir)

	rootCmd := newRootCmd()

	if parseErr := rootCmd.ParseFlags([]string{"--verbose"}); parseErr != nil {
		t.Fatalf("parse flags: %v", parseErr)
	}

	logger := mcpLoggerFromFlags(rootCmd)

	if logger == nil {
		t.Fatal("--verbose should produce a non-nil logger")
	}

	runtime, openErr := mcp.Open(wsDir, mcp.WithLogger(logger))

	if openErr != nil {
		t.Fatalf("mcp.Open: %v", openErr)
	}

	defer runtime.Close()

	if runtime.Logger != logger {
		t.Errorf("Runtime.Logger should be the verbose logger")
	}
}

func TestMCPCmd_DefaultSetsNoLogger(t *testing.T) {
	rootCmd := newRootCmd()

	if parseErr := rootCmd.ParseFlags(nil); parseErr != nil {
		t.Fatalf("parse: %v", parseErr)
	}

	if logger := mcpLoggerFromFlags(rootCmd); logger != nil {
		t.Errorf("default cmd_mcp should produce a nil logger; got %v", logger)
	}
}
```

Imports needed: `testing`, plus `github.com/germanamz/tusk/internal/mcp`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tusk/ -run TestMCPCmd_ -v`
Expected: FAIL — `undefined: mcpLoggerFromFlags`.

- [ ] **Step 3: Add the helper and wire it through `RunE`**

Modify `cmd/tusk/cmd_mcp.go`. Add a small package-level helper just below the existing imports:

```go
// mcpLoggerFromFlags returns a verbose logger when --verbose is set, else nil.
// Returning nil keeps Runtime.Logger nil so existing nil-checks short-circuit.
func mcpLoggerFromFlags(cmd *cobra.Command) *slog.Logger {
	verbose, _ := cmd.Flags().GetBool("verbose")

	if !verbose {
		return nil
	}

	return newLogger(cmd.ErrOrStderr(), true)
}
```

(Add `log/slog` to the imports of `cmd_mcp.go`.)

Inside `RunE`, replace:

```go
runtime, openErr := mcp.Open(cwd)
```

with:

```go
var opts []mcp.Option

if logger := mcpLoggerFromFlags(cmd); logger != nil {
    opts = append(opts, mcp.WithLogger(logger))
}

runtime, openErr := mcp.Open(cwd, opts...)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/tusk/ -run TestMCPCmd_ -v`
Expected: PASS for both `_VerboseSetsRuntimeLogger` and `_DefaultSetsNoLogger`.

- [ ] **Step 5: Run the full cmd/tusk test suite + linter**

Run: `make test && make vet && make lint`
Expected: all clean.

- [ ] **Step 6: Final manual smoke test**

Run:

```bash
make build
./bin/tusk reindex --verbose 2>&1 1>/dev/null | head -5
```

Expected: stderr shows at least one `level=INFO msg="reindex walk start" ...` line. If embeddings are configured and the ollama endpoint returns non-2xx, at least one `level=WARN msg="ollama non-2xx" ...` line and one `level=WARN msg="embed call failed" ...` line.

- [ ] **Step 7: Commit**

```bash
git add cmd/tusk/cmd_mcp.go cmd/tusk/cmd_mcp_test.go
git commit -m "feat(cli): wire --verbose logger into tusk mcp"
```

---

## Final verification

After Task 8 ships:

- [ ] `make build && make test && make vet && make lint` all clean.
- [ ] `./bin/tusk reindex --verbose` against a workspace with ollama unreachable surfaces a `level=WARN msg="ollama non-2xx"` (or `level=WARN msg="embed call failed"` if the transport itself fails) within the first failed attempt.
- [ ] `./bin/tusk reindex` without `--verbose` and against a healthy workspace produces no new stderr output beyond what it did before this work.
- [ ] `./bin/tusk watch --verbose` against the same workspace logs `msg="watch started"` on stderr and keeps `Watching for changes (Ctrl-C to stop)…` plus `[KIND] path` lines on stdout untouched.
