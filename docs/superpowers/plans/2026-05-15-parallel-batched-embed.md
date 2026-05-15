---
type: plan
title: Parallel Embed Workers + Batched Ollama — Implementation Plan
status: draft
implements:
  - Parallel Embed Workers + Batched Ollama Requests — Design
---

# Parallel Embed Workers + Batched Ollama Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `embed.DrainQueue` parallel and batch-aware, raise the hardcoded Ollama HTTP timeout, and expose all three as manifest knobs — shipped as two PRs (Phase 1 = workers + timeout, Phase 2 = batch).

**Architecture:** A per-node worker pool replaces the sequential chunk loop in `internal/embed/drain.go`, with collect-then-upsert atomicity. An optional `BatchEmbedder` interface lets `OllamaEmbedder` accept multiple prompts per HTTP call when configured. Three new optional fields on `manifest.EmbeddingsSection` (`workers`, `timeout-seconds`, `batch-size`) control the behavior; defaults preserve current single-threaded sequential semantics when unset.

**Tech Stack:** Go 1.23+, SQLite (WAL), Ollama `/api/embeddings`. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-05-14-parallel-batched-embed-design.md` is the authoritative reference for behavior and rationale. This plan strictly implements that spec.

---

## File Structure

**Phase 1 — workers + timeout knob:**

| File | Disposition | Responsibility after change |
|---|---|---|
| `internal/manifest/manifest.go` | Modify | `EmbeddingsSection` gains `Workers` and `TimeoutSeconds` int fields |
| `internal/manifest/loader.go` | Modify | Validates the two new fields (`Workers >= 1`, `TimeoutSeconds > 0` when set) |
| `internal/manifest/loader_test.go` | Modify | Round-trip tests for new fields + rejection tests for invalid values |
| `internal/embed/ollama.go` | Modify | `OllamaConfig.Timeout time.Duration` field; constructor uses it or falls back to 30s |
| `internal/embed/ollama_test.go` | Modify | Test that `OllamaConfig.Timeout` actually drives the HTTP client |
| `internal/embed/drain.go` | Modify | `DrainConfig.Workers int` field; per-node sequential chunk loop becomes a worker pool with collect-then-upsert atomicity |
| `internal/embed/drain_test.go` | Modify | Concurrency, atomicity, and parity tests |
| `internal/reindex/reindex.go` | Modify | `Config.Workers int`; passed into `DrainConfig.Workers` |
| `internal/mcp/runtime.go` | Modify | `Runtime.Workers int`; reads `manifest.Embeddings.Workers`. `OllamaConfig.Timeout` plumbed |
| `internal/mcp/drainer.go` | Modify | Passes `Runtime.Workers` into `DrainConfig.Workers` |
| `cmd/tusk/cmd_reindex.go` | Modify | Passes `Workers` into `reindex.Config.Workers` and `Timeout` into `OllamaConfig.Timeout` |
| `cmd/tusk/cmd_query.go` | Modify | Passes `Timeout` into `OllamaConfig.Timeout` |

**Phase 2 — batched Ollama requests:**

| File | Disposition | Responsibility after change |
|---|---|---|
| `internal/embed/embedder.go` | Modify | Adds optional `BatchEmbedder` interface |
| `internal/embed/embedder_test.go` | Modify | Compile-time interface conformance assertion for the stubs in this file |
| `internal/embed/ollama.go` | Modify | `OllamaEmbedder.EmbedBatch` method (POSTs `prompts: []string`) |
| `internal/embed/ollama_test.go` | Modify | Wire-format and dimension-mismatch tests; integration test (skipped by default) |
| `internal/embed/drain.go` | Modify | `DrainConfig.EmbedBatchSize int`; per-node loop type-asserts and uses batch path with per-prompt fallback |
| `internal/embed/drain_test.go` | Modify | Batch-path success, batch-fail-fallback-success, batch-fail-fallback-fail tests |
| `internal/manifest/manifest.go` | Modify | `EmbeddingsSection.BatchSize int` field |
| `internal/manifest/loader.go` | Modify | Validates `BatchSize >= 1` |
| `internal/manifest/loader_test.go` | Modify | Round-trip + validation tests |
| `internal/reindex/reindex.go` | Modify | `Config.EmbedBatchSize int`; forwarded to DrainConfig |
| `internal/mcp/runtime.go` | Modify | `Runtime.EmbedBatchSize int`; reads `manifest.Embeddings.BatchSize` |
| `internal/mcp/drainer.go` | Modify | Passes `Runtime.EmbedBatchSize` into DrainConfig |
| `cmd/tusk/cmd_reindex.go` | Modify | Passes `BatchSize` into `reindex.Config.EmbedBatchSize` |

No new files. All changes fit into existing files.

---

# Phase 1 — Workers + Timeout Knob

Phase 1 ships as one PR. After Phase 1 lands on `main`, Phase 2 starts on a new branch off main.

---

## Task 1: Add `Workers` and `TimeoutSeconds` to manifest

**Files:**
- Modify: `internal/manifest/manifest.go:61-67` (EmbeddingsSection)
- Modify: `internal/manifest/loader.go:428-440` (validation block for `[embeddings]`)
- Test: `internal/manifest/loader_test.go` (add four cases)

- [ ] **Step 1: Add the four failing tests in `loader_test.go`**

These follow the existing pattern in this file: write the manifest to a temp file, call `manifest.Load(path)`, assert against the returned struct. Append after `TestLoad_ParsesEmbeddings`:

```go
func TestLoad_ParsesEmbeddingsWorkers(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "x"

[embeddings]
provider = "ollama"
model    = "nomic-embed-text"
endpoint = "http://localhost:11434"
dim      = 768
workers  = 6
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)
	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	if loaded.Embeddings.Workers != 6 {
		test.Errorf("Workers = %d, want 6", loaded.Embeddings.Workers)
	}
}

func TestLoad_ParsesEmbeddingsTimeoutSeconds(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "x"

[embeddings]
provider        = "ollama"
model           = "nomic-embed-text"
endpoint        = "http://localhost:11434"
dim             = 768
timeout-seconds = 240
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)
	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	if loaded.Embeddings.TimeoutSeconds != 240 {
		test.Errorf("TimeoutSeconds = %d, want 240", loaded.Embeddings.TimeoutSeconds)
	}
}

func TestLoad_RejectsNegativeWorkers(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	// Use -1 (not 0) because go-toml decodes both absent and explicit-zero
	// as int(0); the validator can't distinguish those, so "absent" wins.
	body := `[workspace]
name = "x"

[embeddings]
provider = "ollama"
model    = "nomic-embed-text"
endpoint = "http://localhost:11434"
dim      = 768
workers  = -1
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	_, loadErr := manifest.Load(manifestPath)
	if loadErr == nil {
		test.Fatalf("Load: expected error for workers=-1")
	}
	if !strings.Contains(loadErr.Error(), "workers") {
		test.Errorf("Load error = %q, want it to mention 'workers'", loadErr.Error())
	}
}

func TestLoad_RejectsNegativeTimeoutSeconds(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "x"

[embeddings]
provider        = "ollama"
model           = "nomic-embed-text"
endpoint        = "http://localhost:11434"
dim             = 768
timeout-seconds = -5
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	_, loadErr := manifest.Load(manifestPath)
	if loadErr == nil {
		test.Fatalf("Load: expected error for timeout-seconds=-5")
	}
	if !strings.Contains(loadErr.Error(), "timeout-seconds") {
		test.Errorf("Load error = %q, want it to mention 'timeout-seconds'", loadErr.Error())
	}
}
```

If `strings` is not already imported in `loader_test.go`, add it.

- [ ] **Step 2: Run tests to verify all four fail**

Run: `go test ./internal/manifest/... -run "Workers|TimeoutSeconds" -v`

Expected: 4 failures (fields not on EmbeddingsSection / validation not enforced).

- [ ] **Step 3: Add the two fields to EmbeddingsSection**

Edit `internal/manifest/manifest.go`. Replace the EmbeddingsSection struct:

```go
// EmbeddingsSection configures the active embedding provider.
//
// Plan 5 supports provider = "ollama" only; the loader rejects other values.
// API providers (openai/voyage/anthropic) land in Plan 5.x.
//
// Workers, BatchSize, and TimeoutSeconds tune the embedding pipeline:
// Workers caps concurrency per node, BatchSize caps prompts per HTTP call
// (Phase 2 of Spec B), TimeoutSeconds sets the HTTP client timeout for
// the embedder. All optional; sensible defaults apply when omitted.
type EmbeddingsSection struct {
	Provider       string `toml:"provider"`
	Model          string `toml:"model"`
	Endpoint       string `toml:"endpoint"`
	Dim            int    `toml:"dim"`
	APIKey         string `toml:"api-key"`
	Workers        int    `toml:"workers"`
	TimeoutSeconds int    `toml:"timeout-seconds"`
}
```

(Note: `BatchSize` is added in Task 9; the comment mentions it now to lock the doc and avoid churn.)

- [ ] **Step 4: Add validation in loader.go**

Edit `internal/manifest/loader.go`. Inside the existing `if loaded.Embeddings.Provider != ""` block (around line 428), append two new validation checks **before** the closing brace:

```go
		if loaded.Embeddings.Workers < 0 {
			return fmt.Errorf("manifest: embeddings.workers must be >= 0 (got %d); zero or absent uses the default", loaded.Embeddings.Workers)
		}

		if loaded.Embeddings.TimeoutSeconds < 0 {
			return fmt.Errorf("manifest: embeddings.timeout-seconds must be >= 0 (got %d); zero or absent uses the default", loaded.Embeddings.TimeoutSeconds)
		}
```

These reject negative values explicitly. Zero is treated as "absent → use default" because go-toml decodes both an explicit `workers = 0` and a missing field as `int(0)`, so the loader cannot distinguish them. The Resolve helpers added in Task 4 supply the user-facing default of 4 (workers) and 120 (timeout-seconds) when the field is zero.

- [ ] **Step 5: Re-run the four tests to verify they pass**

Run: `go test ./internal/manifest/... -run "Workers|TimeoutSeconds" -v`

Expected: 4 passes.

- [ ] **Step 6: Run the full manifest test package to confirm no regressions**

Run: `go test ./internal/manifest/...`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/manifest/manifest.go internal/manifest/loader.go internal/manifest/loader_test.go
git commit -m "feat(manifest): add embeddings.workers and embeddings.timeout-seconds"
```

---

## Task 2: Make `OllamaConfig.Timeout` configurable

**Files:**
- Modify: `internal/embed/ollama.go:14-34` (config + constructor)
- Test: `internal/embed/ollama_test.go` (add timeout test)

- [ ] **Step 1: Write the failing timeout test in `ollama_test.go`**

Append a new test function:

```go
func TestOllamaEmbedder_RespectsConfiguredTimeout(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// Sleep longer than the configured timeout to force a client-side abort.
		time.Sleep(200 * time.Millisecond)
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"embedding":[0.1,0.2,0.3]}`))
	}))
	defer server.Close()

	embedder := embed.NewOllamaEmbedder(embed.OllamaConfig{
		Endpoint: server.URL,
		Model:    "stub",
		Dim:      3,
		Timeout:  50 * time.Millisecond,
	})

	_, err := embedder.Embed(context.Background(), []byte("hello"))

	if err == nil {
		test.Fatalf("Embed: expected timeout error, got nil")
	}

	if !strings.Contains(err.Error(), "context deadline exceeded") &&
		!strings.Contains(err.Error(), "Client.Timeout") {
		test.Errorf("Embed error = %q, want a timeout-related error", err.Error())
	}
}
```

If `strings`, `time`, `httptest`, `http`, or `context` are missing from the test file's imports, add them.

- [ ] **Step 2: Run the test to verify it fails to compile**

Run: `go test ./internal/embed/... -run TestOllamaEmbedder_RespectsConfiguredTimeout -v`

Expected: compile error — `OllamaConfig` has no `Timeout` field.

- [ ] **Step 3: Add `Timeout` field and update constructor**

Edit `internal/embed/ollama.go`. Replace `OllamaConfig`:

```go
// OllamaConfig configures an OllamaEmbedder.
type OllamaConfig struct {
	Endpoint string
	Model    string
	Dim      int
	Logger   *slog.Logger // optional; nil silences output
	Timeout  time.Duration // optional; zero falls back to 30s
}
```

Replace `NewOllamaEmbedder`:

```go
// defaultOllamaTimeout is the fallback HTTP client timeout when
// OllamaConfig.Timeout is unset. Production callers pass a larger value
// from manifest.Embeddings.TimeoutSeconds (default 120s); this constant
// preserves prior behavior for callers that don't set the field.
const defaultOllamaTimeout = 30 * time.Second

// NewOllamaEmbedder constructs an OllamaEmbedder with sensible HTTP defaults.
func NewOllamaEmbedder(config OllamaConfig) *OllamaEmbedder {
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = defaultOllamaTimeout
	}

	return &OllamaEmbedder{
		config: config,
		client: &http.Client{Timeout: timeout},
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/embed/... -run TestOllamaEmbedder_RespectsConfiguredTimeout -v`

Expected: PASS.

- [ ] **Step 5: Run the full embed package to confirm no regressions**

Run: `go test ./internal/embed/...`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/embed/ollama.go internal/embed/ollama_test.go
git commit -m "feat(embed): make OllamaEmbedder timeout configurable"
```

---

## Task 3: Convert per-node chunk loop to worker pool

**Files:**
- Modify: `internal/embed/drain.go:24-249` (DrainConfig + per-node loop)
- Test: `internal/embed/drain_test.go` (add 3 tests)

This task is the largest in the plan. It restructures the inner loop of `DrainQueue` into a per-node worker pool with collect-then-upsert atomicity. Read `internal/embed/drain.go` end-to-end before starting; the existing structure must be preserved everywhere except inside the `for _, queued := range batch` block, after `bodyChunks` is computed.

- [ ] **Step 1: Write the three failing tests in `drain_test.go`**

Append three new test functions. The tests use a sleep-stub embedder and a multi-chunk node setup. Helper function first:

```go
type sleepStubEmbedder struct {
	dim       int
	model     string
	sleep     time.Duration
	failChunk int  // 0 means never fail; otherwise fail when calls hits this number
	mu        sync.Mutex
	calls     int
}

func (stub *sleepStubEmbedder) Embed(ctx context.Context, payload []byte) ([]float32, error) {
	stub.mu.Lock()
	stub.calls++
	current := stub.calls
	stub.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(stub.sleep):
	}

	if stub.failChunk != 0 && current == stub.failChunk {
		return nil, fmt.Errorf("stub: forced failure on call %d", current)
	}

	out := make([]float32, stub.dim)
	for idx := range out {
		out[idx] = 0.1
	}
	return out, nil
}

func (stub *sleepStubEmbedder) Model() string { return stub.model }
func (stub *sleepStubEmbedder) Dim() int      { return stub.dim }
```

Now the three tests:

```go
func TestDrainQueue_WorkersConcurrencySpeedup(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	// 20 chunks via a body large enough to cross the chunker's MaxBytes.
	body := strings.Repeat("paragraph paragraph paragraph paragraph.\n\n", 600)
	createNodeFile(test, root, "notes/big.md", body)
	nodeRepo.Upsert(index.NodeRow{ID: "notes/big", Type: "note", Path: "notes/big.md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"})

	stub := &sleepStubEmbedder{dim: 3, model: "stub", sleep: 50 * time.Millisecond}

	queueRepo.Enqueue("notes/big")
	t1 := time.Now()
	_, err := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root: root, Nodes: nodeRepo, Queue: queueRepo, Embeddings: embeddingRepo,
		Embedder: stub, Chunker: embed.MarkdownRecursive{}, Workers: 1,
	})
	serial := time.Since(t1)
	if err != nil {
		test.Fatalf("serial drain: %v", err)
	}

	embeddingRepo.DeleteByNodeID("notes/big")
	stub.calls = 0
	queueRepo.Enqueue("notes/big")
	t2 := time.Now()
	_, err = embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root: root, Nodes: nodeRepo, Queue: queueRepo, Embeddings: embeddingRepo,
		Embedder: stub, Chunker: embed.MarkdownRecursive{}, Workers: 4,
	})
	parallel := time.Since(t2)
	if err != nil {
		test.Fatalf("parallel drain: %v", err)
	}

	if parallel*2 > serial {
		test.Errorf("parallel (%v) was not measurably faster than serial (%v)", parallel, serial)
	}
}

func TestDrainQueue_WorkerErrorAtomicity(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	body := strings.Repeat("paragraph paragraph paragraph paragraph.\n\n", 200)
	createNodeFile(test, root, "notes/big.md", body)
	nodeRepo.Upsert(index.NodeRow{ID: "notes/big", Type: "note", Path: "notes/big.md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"})
	queueRepo.Enqueue("notes/big")

	stub := &sleepStubEmbedder{dim: 3, model: "stub", sleep: 5 * time.Millisecond, failChunk: 3}

	_, err := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root: root, Nodes: nodeRepo, Queue: queueRepo, Embeddings: embeddingRepo,
		Embedder: stub, Chunker: embed.MarkdownRecursive{}, Workers: 4,
	})
	if err != nil {
		test.Fatalf("DrainQueue: %v", err)
	}

	rows, _ := embeddingRepo.GetByNodeID("notes/big")
	if len(rows) != 0 {
		test.Errorf("rows after error = %d, want 0 (per-node atomicity)", len(rows))
	}

	depth, _ := queueRepo.Depth()
	if depth == 0 {
		test.Errorf("queue depth after error = 0, want >0 (node should be re-enqueued)")
	}
}

func TestDrainQueue_WorkersDefaultParityWithSerial(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	createNodeFile(test, root, "notes/a.md", "hi")
	nodeRepo.Upsert(index.NodeRow{ID: "notes/a", Type: "note", Path: "notes/a.md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"})
	queueRepo.Enqueue("notes/a")

	// Workers field NOT set → must default to 1 and produce identical
	// behavior to the pre-Spec-B code path.
	_, err := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root: root, Nodes: nodeRepo, Queue: queueRepo, Embeddings: embeddingRepo,
		Embedder: &drainStubEmbedder{dim: 3, model: "stub"},
		Chunker:  embed.WholeDocument{}, BatchSize: 50,
	})
	if err != nil {
		test.Fatalf("DrainQueue: %v", err)
	}

	rows, _ := embeddingRepo.GetByNodeID("notes/a")
	if len(rows) != 1 {
		test.Errorf("rows = %d, want 1", len(rows))
	}
}
```

The `GetByNodeID` repo method may need to be added if it doesn't exist; check `internal/index/embedding.go` first. If absent, add it as part of this task: a small helper that returns all `EmbeddingRow` rows for a node ID, sorted by chunk_idx. Add a single test for the helper in `internal/index/embedding_test.go` if that file exists.

Add `sync` and `time` to the test file's imports if missing.

- [ ] **Step 2: Run the three tests to verify they fail**

Run: `go test ./internal/embed/... -run "TestDrainQueue_(WorkersConcurrencySpeedup|WorkerErrorAtomicity|WorkersDefaultParityWithSerial)" -v`

Expected: failures (DrainConfig has no `Workers` field; concurrency speedup not present).

- [ ] **Step 3: Add `Workers` field to DrainConfig**

Edit `internal/embed/drain.go`. In `DrainConfig`:

```go
// DrainConfig configures DrainQueue.
type DrainConfig struct {
	Root       string                // workspace root (required when Embedder is set)
	Nodes      *index.NodeRepo       // node repo for path lookups
	Queue      *index.EmbedQueueRepo // queue repo (required)
	Embeddings *index.EmbeddingRepo  // embeddings repo (required when Embedder is set)
	Embedder   Embedder              // when nil, DrainQueue is a no-op
	Chunker    ChunkingStrategy      // required when Embedder is set
	BatchSize  int                   // queue rows pulled per drain iteration; defaults to 50
	Workers    int                   // concurrent embed calls per node; defaults to 1 (serial)
	Logger     *slog.Logger          // optional; nil silences output
}
```

- [ ] **Step 4: Restructure the per-node block in `DrainQueue` to use a worker pool**

Replace the existing `for chunkIdx, bodyChunk := range bodyChunks { ... }` block (currently around `drain.go:155-233`) with the worker-pool flow. The full replacement block, inserted after the existing `if delErr := config.Embeddings.DeleteByNodeID(...) {...}` check and before the closing of `for _, queued := range batch`:

```go
			workers := config.Workers
			if workers < 1 {
				workers = 1
			}
			if workers > len(bodyChunks) {
				workers = len(bodyChunks)
			}

			type embedJob struct {
				chunkIdx int
				payload  []byte
			}
			type embedResult struct {
				chunkIdx int
				vector   []float32
				body     []byte
				err      error
			}

			nodeCtx, cancel := context.WithCancel(ctx)

			jobs := make(chan embedJob, len(bodyChunks))
			results := make(chan embedResult, len(bodyChunks))

			var wg sync.WaitGroup
			for w := 0; w < workers; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for job := range jobs {
						if nodeCtx.Err() != nil {
							results <- embedResult{chunkIdx: job.chunkIdx, err: nodeCtx.Err()}
							continue
						}

						vec, err := config.Embedder.Embed(nodeCtx, job.payload)
						results <- embedResult{
							chunkIdx: job.chunkIdx,
							vector:   vec,
							body:     job.payload[len(header):], // body is payload minus header
							err:      err,
						}
					}
				}()
			}

			for chunkIdx, bodyChunk := range bodyChunks {
				payload := make([]byte, 0, len(header)+len(bodyChunk))
				payload = append(payload, header...)
				payload = append(payload, bodyChunk...)
				jobs <- embedJob{chunkIdx: chunkIdx, payload: payload}
			}
			close(jobs)

			go func() {
				wg.Wait()
				close(results)
			}()

			collected := make([]embedResult, 0, len(bodyChunks))
			var firstErr error
			for r := range results {
				if r.err != nil && firstErr == nil {
					firstErr = r.err
					cancel()
				}
				collected = append(collected, r)
			}
			cancel()

			if firstErr != nil {
				if config.Logger != nil {
					config.Logger.Warn("embed call failed",
						"node_id", queued.NodeID,
						"chunks_total", len(bodyChunks),
						"model", config.Embedder.Model(),
						"err", firstErr.Error(),
					)
				}

				nextAttempts := queued.Attempts + 1
				if nextAttempts >= MaxEmbedAttempts {
					if config.Logger != nil {
						config.Logger.Warn("embed gave up",
							"node_id", queued.NodeID,
							"attempts", nextAttempts,
							"err", firstErr.Error(),
						)
					}
				} else {
					if reEnqErr := config.Queue.ReEnqueue(queued.NodeID, nextAttempts, firstErr.Error()); reEnqErr != nil {
						if config.Logger != nil {
							config.Logger.Warn("embed re-enqueue failed",
								"node_id", queued.NodeID,
								"err", reEnqErr.Error(),
							)
						}
					} else if config.Logger != nil {
						config.Logger.Warn("embed re-enqueued",
							"node_id", queued.NodeID,
							"attempts", nextAttempts,
						)
					}
				}

				batchFailed++
				continue
			}

			sort.Slice(collected, func(i, j int) bool {
				return collected[i].chunkIdx < collected[j].chunkIdx
			})

			for _, r := range collected {
				payload := make([]byte, 0, len(header)+len(r.body))
				payload = append(payload, header...)
				payload = append(payload, r.body...)
				contentHash := sha256.Sum256(payload)

				if upsertErr := config.Embeddings.Upsert(index.EmbeddingRow{
					NodeID:      queued.NodeID,
					ChunkIdx:    r.chunkIdx,
					Model:       config.Embedder.Model(),
					ContentHash: hex.EncodeToString(contentHash[:]),
					Vector:      r.vector,
					Dim:         config.Embedder.Dim(),
					Body:        string(r.body),
				}); upsertErr != nil {
					return drained, upsertErr
				}

				if config.Logger != nil {
					config.Logger.Debug("embed attempt success",
						"node_id", queued.NodeID,
						"chunk_idx", r.chunkIdx,
						"chunks_total", len(bodyChunks),
						"vector_dim", len(r.vector),
					)
				}
			}

			drained++
			batchSucceeded++
```

Remove the old `nodeFailed bool` variable and the `if !nodeFailed { drained++; batchSucceeded++ }` block at the end of the per-node iteration; the new structure increments inside the success branch directly.

Add to imports at the top of `drain.go`:

```go
	"sort"
	"sync"
```

- [ ] **Step 5: Run the three tests to verify they pass**

Run: `go test ./internal/embed/... -run "TestDrainQueue_(WorkersConcurrencySpeedup|WorkerErrorAtomicity|WorkersDefaultParityWithSerial)" -v`

Expected: PASS.

- [ ] **Step 6: Run the full embed test package to confirm parity**

Run: `go test ./internal/embed/...`

Expected: PASS. All pre-existing drain tests must still pass; if any fails, the new code path has drifted from prior behavior — fix the implementation, do not adjust the existing tests.

- [ ] **Step 7: Run race detector**

Run: `go test -race ./internal/embed/...`

Expected: PASS, no race warnings.

- [ ] **Step 8: Commit**

```bash
git add internal/embed/drain.go internal/embed/drain_test.go
git commit -m "feat(embed): per-node worker pool in DrainQueue"
```

If `internal/index/embedding.go` and its test were touched in Step 1, include them in this commit.

---

## Task 4: Wire manifest fields through to construction sites

**Files:**
- Modify: `internal/reindex/reindex.go:60-100, 392-402` (Config + DrainConfig construction)
- Modify: `internal/mcp/runtime.go:80-152` (Runtime struct + ollama config wiring)
- Modify: `internal/mcp/drainer.go:40-47` (DrainConfig construction)
- Modify: `cmd/tusk/cmd_reindex.go:60-93` (OllamaConfig + reindex.Config construction)
- Modify: `cmd/tusk/cmd_query.go:149-153` (OllamaConfig construction)

- [ ] **Step 1: Add `Workers` to `reindex.Config` and forward to DrainConfig**

Edit `internal/reindex/reindex.go`. In the `Config` struct (around the existing `Logger` field), add:

```go
	// Workers caps concurrent embed calls per node when the embedding pipeline
	// runs. Forwarded to embed.DrainConfig.Workers. Zero means "serial".
	Workers int
```

In the `DrainQueue` call site (around line 392), pass `Workers`:

```go
		if _, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
			Root:       config.Root,
			Nodes:      config.Repo,
			Queue:      config.EmbedQueue,
			Embeddings: config.EmbeddingRepo,
			Embedder:   config.Embedder,
			Chunker:    config.Chunker,
			Workers:    config.Workers,
			Logger:     config.Logger,
		}); drainErr != nil {
```

- [ ] **Step 2: Add `Workers` to `mcp.Runtime` and forward through drainer**

Edit `internal/mcp/runtime.go`. Find the `Runtime` struct definition (search for `type Runtime struct`) and add a `Workers int` field next to `Embedder`. Then in the constructor, when `loaded.Embeddings.Provider == "ollama"` (around line 141), populate it:

```go
	if loaded.Embeddings.Provider == "ollama" {
		timeout := time.Duration(loaded.Embeddings.TimeoutSeconds) * time.Second
		rt.Embedder = embed.NewOllamaEmbedder(embed.OllamaConfig{
			Endpoint: loaded.Embeddings.Endpoint,
			Model:    loaded.Embeddings.Model,
			Dim:      loaded.Embeddings.Dim,
			Logger:   rt.Logger,
			Timeout:  timeout,
		})
		rt.Chunker = embed.MarkdownRecursive{}
		rt.Workers = loaded.Embeddings.Workers
	}
```

Add `"time"` to runtime.go imports if missing.

Edit `internal/mcp/drainer.go`. In the `DrainQueue` call site (line 40), pass `Workers`:

```go
			drained, drainErr := embed.DrainQueue(ctx, embed.DrainConfig{
				Root:       config.Runtime.Root,
				Nodes:      config.Runtime.Nodes,
				Queue:      config.Runtime.EmbedQueue,
				Embeddings: config.Runtime.Embeddings,
				Embedder:   config.Runtime.Embedder,
				Chunker:    config.Runtime.Chunker,
				Workers:    config.Runtime.Workers,
			})
```

- [ ] **Step 3: Update `cmd/tusk/cmd_reindex.go` to pass workers + timeout**

Edit `cmd/tusk/cmd_reindex.go`. In the `if loaded.Embeddings.Provider == "ollama"` block (around line 65), add timeout:

```go
		if loaded.Embeddings.Provider == "ollama" {
			timeout := time.Duration(loaded.Embeddings.TimeoutSeconds) * time.Second
			embedder = embed.NewOllamaEmbedder(embed.OllamaConfig{
				Endpoint: loaded.Embeddings.Endpoint,
				Model:    loaded.Embeddings.Model,
				Dim:      loaded.Embeddings.Dim,
				Logger:   logger,
				Timeout:  timeout,
			})
			chunker = embed.MarkdownRecursive{}
			embedQueue = index.NewEmbedQueueRepo(store)
			embeddingRepo = index.NewEmbeddingRepo(store)
		}
```

In the `reindex.Run(reindex.Config{...})` call site (around line 77), add a `Workers: loaded.Embeddings.Workers,` field.

Add `"time"` to cmd_reindex.go imports if missing.

- [ ] **Step 4: Update `cmd/tusk/cmd_query.go` to pass timeout**

Edit `cmd/tusk/cmd_query.go`. In `runSemanticQuery` (around line 149):

```go
	timeout := time.Duration(loaded.Embeddings.TimeoutSeconds) * time.Second
	embedder := embed.NewOllamaEmbedder(embed.OllamaConfig{
		Endpoint: loaded.Embeddings.Endpoint,
		Model:    loaded.Embeddings.Model,
		Dim:      loaded.Embeddings.Dim,
		Timeout:  timeout,
	})
```

Add `"time"` to cmd_query.go imports if missing.

- [ ] **Step 5: Apply manifest defaults at the wiring layer**

The plumbing above passes raw values from manifest. When the manifest fields are unset (zero), the embedder constructor falls back to its 30s default and DrainConfig.Workers=0 falls back to 1. But the spec defaults are 4 workers and 120s — these are *user-facing* defaults, applied when the manifest field is absent.

Choose: apply defaults at the manifest loader (transforms zero → spec default before returning) OR at the wiring sites (transforms zero → spec default before passing to constructor). The cleaner choice is **at the wiring sites** because the loader's contract is "round-trip the file faithfully" and the spec defaults are a behavior choice of the embedding pipeline, not the manifest.

Add a small helper in `internal/embed/embedder.go` (this file is short and the helper belongs near the embedder code):

```go
// DefaultWorkers, DefaultTimeoutSeconds, and DefaultBatchSize are the
// user-facing defaults applied at construction sites when the manifest
// field is unset. Phase 2 of Spec B adds DefaultBatchSize.
const (
	DefaultWorkers        = 4
	DefaultTimeoutSeconds = 120
)

// ResolveWorkers returns the configured workers value or DefaultWorkers when zero.
func ResolveWorkers(configured int) int {
	if configured <= 0 {
		return DefaultWorkers
	}
	return configured
}

// ResolveTimeoutSeconds returns the configured value or DefaultTimeoutSeconds when zero.
func ResolveTimeoutSeconds(configured int) int {
	if configured <= 0 {
		return DefaultTimeoutSeconds
	}
	return configured
}
```

In each wiring site, replace the raw `loaded.Embeddings.Workers` and `loaded.Embeddings.TimeoutSeconds` reads with the resolver helpers:

```go
	timeout := time.Duration(embed.ResolveTimeoutSeconds(loaded.Embeddings.TimeoutSeconds)) * time.Second
	// ...
	Workers: embed.ResolveWorkers(loaded.Embeddings.Workers),
```

This means `cmd_reindex.go`, `cmd_query.go`, and `internal/mcp/runtime.go` all use the resolvers. The drain-package constants `DrainConfig.Workers == 0 → 1` and `OllamaConfig.Timeout == 0 → 30s` remain in place as low-level safety nets for tests and direct callers; the resolvers are the user-facing defaults.

- [ ] **Step 6: Run the full test suite to confirm nothing regressed**

Run: `make test`

Expected: PASS.

- [ ] **Step 7: Manually exercise the wiring in this workspace**

Run: `make build && ./bin/tusk reindex`

Expected: clean reindex completes. Stop on first error.

- [ ] **Step 8: Commit**

```bash
git add internal/reindex/reindex.go internal/mcp/runtime.go internal/mcp/drainer.go cmd/tusk/cmd_reindex.go cmd/tusk/cmd_query.go internal/embed/embedder.go
git commit -m "feat(embed,cli,mcp): wire manifest workers/timeout into drain and embedder"
```

---

## Task 5: Phase 1 race-detector pass, throughput measurement, PR

**Files:** none modified — this is verification + ship.

- [ ] **Step 1: Race-detector pass on the embed package and the wider codebase**

Run: `make test-race`

Expected: PASS with no race warnings. If the race detector flags anything in the new worker pool code, debug before proceeding — do not ship a known race.

- [ ] **Step 2: Capture sequential baseline wall-clock**

Reset the local index and reindex with `workers = 1` (serial). In a temporary state, set `workers = 1` in `tusk.toml`'s `[embeddings]` section (don't commit this), then:

```bash
rm -f .tusk/index.db
time ./bin/tusk reindex
```

Record the wall-clock seconds. Run twice and take the second number (warm cache).

- [ ] **Step 3: Capture parallel wall-clock**

Restore `tusk.toml` to either no `workers` setting (uses default 4) or set `workers = 8`. Then:

```bash
rm -f .tusk/index.db
time ./bin/tusk reindex
```

Record. Compare to Step 2.

- [ ] **Step 4: Run `tusk doctor` and capture the stats block**

```bash
./bin/tusk doctor
```

Expected: clean output, the embed stats block reports the same node/chunk counts before and after. Save the output snippet for the PR description.

- [ ] **Step 5: Push and open the Phase 1 PR**

```bash
git push -u origin feat/parallel-batched-embed
gh pr create --title "feat(embed): parallel embed workers and configurable timeout" --body "$(cat <<'EOF'
## Summary

- Per-node worker pool inside `embed.DrainQueue`: chunks for a single node embed concurrently. Default `Workers=4`. Atomic per-node upsert (collect-then-upsert) preserves retry semantics and incidentally fixes the partial-write-on-give-up wart in the prior code.
- Configurable Ollama HTTP timeout via new `embeddings.timeout-seconds` manifest knob (default 120s). Replaces the hardcoded 30-second ceiling that was causing `embed gave up` retries on warm CPU Ollama.
- New manifest fields `embeddings.workers` and `embeddings.timeout-seconds`; both optional, validated as `>= 1` and `> 0` respectively.

Spec: `docs/superpowers/specs/2026-05-14-parallel-batched-embed-design.md` Phase 1.
Plan: `docs/superpowers/plans/2026-05-15-parallel-batched-embed.md` Tasks 1–5.

## Throughput measurement (dogfood workspace)

| Workers | Wall-clock |
|---|---|
| 1 (serial) | <fill in from Step 2> |
| <default 4> | <fill in from Step 3> |

`tusk doctor` output:
```
<paste stats block from Step 4>
```

## Test plan

- [x] `go test ./...` passes
- [x] `make test-race` passes (no races in new worker pool)
- [x] Manual reindex on this workspace completes cleanly under default and `workers=1` settings
- [x] `tusk doctor` reports unchanged node/chunk counts before vs after
EOF
)"
```

Expected: PR opens. Capture the PR URL.

- [ ] **Step 6: Wait for CI and merge once green**

This step is the human checkpoint. Do not auto-merge.

---

# Phase 2 — Batched Ollama Requests

Phase 2 starts after Phase 1's PR has merged to `main`. Branch off main:

```bash
git checkout main
git pull
git checkout -b feat/batched-embed
```

---

## Task 6: Add `BatchEmbedder` optional interface

**Files:**
- Modify: `internal/embed/embedder.go` (add interface)
- Modify: `internal/embed/embedder_test.go` (compile-time conformance check)

- [ ] **Step 1: Write the failing conformance test**

Append to `internal/embed/embedder_test.go`:

```go
// Compile-time assertion that OllamaEmbedder implements both Embedder and BatchEmbedder.
var (
	_ embed.Embedder      = (*embed.OllamaEmbedder)(nil)
	_ embed.BatchEmbedder = (*embed.OllamaEmbedder)(nil)
)
```

If the `var (...)` block already exists, add the second line to it.

- [ ] **Step 2: Run to verify compile failure**

Run: `go test ./internal/embed/... -run TestEmbedder -v` (or just the package).

Expected: compile failure — `embed.BatchEmbedder` undefined.

- [ ] **Step 3: Add the interface to `embedder.go`**

Edit `internal/embed/embedder.go`. Append after the existing `Embedder` interface:

```go
// BatchEmbedder is an optional extension of Embedder for providers that
// accept multiple prompts per HTTP call. The drain loop type-asserts
// for this interface and prefers the batch path when available and when
// DrainConfig.EmbedBatchSize > 1.
//
// Contract: the returned slice has exactly len(payloads) elements in the
// same order as the input. Any error applies to the entire batch (no
// partial successes returned). Drain falls back to per-prompt Embed for
// the failed batch's chunks within the same attempt.
type BatchEmbedder interface {
	Embedder
	EmbedBatch(ctx context.Context, payloads [][]byte) ([][]float32, error)
}
```

- [ ] **Step 4: Note that `OllamaEmbedder.EmbedBatch` doesn't exist yet — the conformance check will still fail**

Run: `go test ./internal/embed/... -v`

Expected: still fails to compile (no method `EmbedBatch` on `OllamaEmbedder`). This is intentional — Task 7 implements `EmbedBatch`.

- [ ] **Step 5: Defer commit to end of Task 7**

Don't commit yet — the package doesn't compile. Move directly to Task 7.

---

## Task 7: Implement `OllamaEmbedder.EmbedBatch`

**Files:**
- Modify: `internal/embed/ollama.go` (add EmbedBatch method)
- Modify: `internal/embed/ollama_test.go` (wire-format + dimension-mismatch + integration tests)

- [ ] **Step 1: Write the failing tests**

Append to `internal/embed/ollama_test.go`:

```go
func TestOllamaEmbedder_EmbedBatchSendsPromptsArray(test *testing.T) {
	var capturedBody string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, _ := io.ReadAll(request.Body)
		capturedBody = string(raw)
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"embeddings":[[0.1,0.2,0.3],[0.4,0.5,0.6]]}`))
	}))
	defer server.Close()

	embedder := embed.NewOllamaEmbedder(embed.OllamaConfig{
		Endpoint: server.URL,
		Model:    "stub",
		Dim:      3,
	})

	vectors, err := embedder.EmbedBatch(context.Background(), [][]byte{[]byte("hi"), []byte("ho")})
	if err != nil {
		test.Fatalf("EmbedBatch: %v", err)
	}

	if len(vectors) != 2 {
		test.Fatalf("vectors len = %d, want 2", len(vectors))
	}
	if vectors[0][0] != 0.1 || vectors[1][0] != 0.4 {
		test.Errorf("vectors out of order: %v", vectors)
	}
	if !strings.Contains(capturedBody, `"prompts":["hi","ho"]`) {
		test.Errorf("request body = %q, want it to contain prompts array", capturedBody)
	}
}

func TestOllamaEmbedder_EmbedBatchDimensionMismatchErrors(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"embeddings":[[0.1,0.2]]}`)) // wrong dim
	}))
	defer server.Close()

	embedder := embed.NewOllamaEmbedder(embed.OllamaConfig{
		Endpoint: server.URL,
		Model:    "stub",
		Dim:      3,
	})

	_, err := embedder.EmbedBatch(context.Background(), [][]byte{[]byte("hi")})
	if err == nil {
		test.Fatalf("EmbedBatch: expected dim-mismatch error, got nil")
	}
}

func TestOllamaEmbedder_EmbedBatchLengthMismatchErrors(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"embeddings":[[0.1,0.2,0.3]]}`)) // 1 returned, 2 requested
	}))
	defer server.Close()

	embedder := embed.NewOllamaEmbedder(embed.OllamaConfig{
		Endpoint: server.URL,
		Model:    "stub",
		Dim:      3,
	})

	_, err := embedder.EmbedBatch(context.Background(), [][]byte{[]byte("hi"), []byte("ho")})
	if err == nil {
		test.Fatalf("EmbedBatch: expected length-mismatch error, got nil")
	}
}
```

Add `"io"` to imports if missing.

- [ ] **Step 2: Run to verify compile failures (no `EmbedBatch` method)**

Run: `go test ./internal/embed/... -v`

Expected: compile error — `OllamaEmbedder` has no `EmbedBatch` method.

- [ ] **Step 3: Implement `EmbedBatch` on OllamaEmbedder**

Edit `internal/embed/ollama.go`. Append after the existing `Embed` method:

```go
// EmbedBatch implements BatchEmbedder.
//
// Posts {"model": ..., "prompts": [...]} to /api/embeddings and decodes
// {"embeddings": [[...]]} preserving input order. Errors apply to the
// entire batch — drain falls back to per-prompt Embed for the failed
// batch's chunks within the same attempt.
//
// Minimum Ollama version supporting the prompts: [] field is 0.1.30.
func (embedder *OllamaEmbedder) EmbedBatch(ctx context.Context, payloads [][]byte) ([][]float32, error) {
	prompts := make([]string, len(payloads))
	for i, p := range payloads {
		prompts[i] = string(p)
	}

	body := struct {
		Model   string   `json:"model"`
		Prompts []string `json:"prompts"`
	}{
		Model:   embedder.config.Model,
		Prompts: prompts,
	}

	encoded, marshalErr := json.Marshal(body)
	if marshalErr != nil {
		return nil, fmt.Errorf("ollama: marshal batch: %w", marshalErr)
	}

	request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, embedder.config.Endpoint+"/api/embeddings", bytes.NewReader(encoded))
	if requestErr != nil {
		return nil, fmt.Errorf("ollama: new batch request: %w", requestErr)
	}
	request.Header.Set("Content-Type", "application/json")

	response, doErr := embedder.client.Do(request)
	if doErr != nil {
		return nil, fmt.Errorf("ollama: post batch: %w", doErr)
	}
	defer response.Body.Close()

	responseBody, readErr := io.ReadAll(response.Body)
	if readErr != nil {
		return nil, fmt.Errorf("ollama: read batch body: %w", readErr)
	}

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama: batch HTTP %d: %s", response.StatusCode, string(responseBody))
	}

	var decoded struct {
		Embeddings [][]float64 `json:"embeddings"`
	}
	if decodeErr := json.Unmarshal(responseBody, &decoded); decodeErr != nil {
		return nil, fmt.Errorf("ollama: decode batch: %w", decodeErr)
	}

	if len(decoded.Embeddings) != len(payloads) {
		return nil, fmt.Errorf("ollama: batch returned %d embeddings, expected %d", len(decoded.Embeddings), len(payloads))
	}

	vectors := make([][]float32, len(decoded.Embeddings))
	for i, row := range decoded.Embeddings {
		if len(row) != embedder.config.Dim {
			return nil, fmt.Errorf("ollama: batch row %d returned %d dims, expected %d (model %q)", i, len(row), embedder.config.Dim, embedder.config.Model)
		}
		vec := make([]float32, len(row))
		for j, v := range row {
			vec[j] = float32(v)
		}
		vectors[i] = vec
	}

	return vectors, nil
}
```

- [ ] **Step 4: Run the new tests + the conformance check from Task 6**

Run: `go test ./internal/embed/... -run "TestOllamaEmbedder_EmbedBatch" -v`

Expected: 3 PASS. The compile-time conformance check in `embedder_test.go` also now compiles.

- [ ] **Step 5: Run the full embed package**

Run: `go test ./internal/embed/...`

Expected: PASS.

- [ ] **Step 6: Commit Tasks 6 + 7 together**

```bash
git add internal/embed/embedder.go internal/embed/embedder_test.go internal/embed/ollama.go internal/embed/ollama_test.go
git commit -m "feat(embed): add BatchEmbedder interface and OllamaEmbedder.EmbedBatch"
```

---

## Task 8: Add batch path to drain loop with per-prompt fallback

**Files:**
- Modify: `internal/embed/drain.go` (add EmbedBatchSize, batch dispatcher in worker)
- Modify: `internal/embed/drain_test.go` (3 batch tests)

- [ ] **Step 1: Write the three failing tests**

Append to `drain_test.go`. Need a stub that implements `BatchEmbedder` with controllable batch behavior:

```go
type batchStubEmbedder struct {
	dim         int
	model       string
	failBatch   bool       // when true, EmbedBatch returns an error
	failOnEmbed bool       // when true, Embed fallback returns an error
	mu          sync.Mutex
	batchCalls  int
	embedCalls  int
}

func (stub *batchStubEmbedder) Embed(ctx context.Context, payload []byte) ([]float32, error) {
	stub.mu.Lock()
	stub.embedCalls++
	stub.mu.Unlock()

	if stub.failOnEmbed {
		return nil, fmt.Errorf("stub: forced embed failure")
	}
	out := make([]float32, stub.dim)
	for i := range out {
		out[i] = 0.2
	}
	return out, nil
}

func (stub *batchStubEmbedder) EmbedBatch(ctx context.Context, payloads [][]byte) ([][]float32, error) {
	stub.mu.Lock()
	stub.batchCalls++
	stub.mu.Unlock()

	if stub.failBatch {
		return nil, fmt.Errorf("stub: forced batch failure")
	}
	out := make([][]float32, len(payloads))
	for i := range payloads {
		v := make([]float32, stub.dim)
		for j := range v {
			v[j] = 0.3
		}
		out[i] = v
	}
	return out, nil
}

func (stub *batchStubEmbedder) Model() string { return stub.model }
func (stub *batchStubEmbedder) Dim() int      { return stub.dim }
```

The three tests:

```go
func TestDrainQueue_BatchPathPreferred(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	body := strings.Repeat("paragraph paragraph paragraph paragraph.\n\n", 200)
	createNodeFile(test, root, "notes/big.md", body)
	nodeRepo.Upsert(index.NodeRow{ID: "notes/big", Type: "note", Path: "notes/big.md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"})
	queueRepo.Enqueue("notes/big")

	stub := &batchStubEmbedder{dim: 3, model: "stub"}

	_, err := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root: root, Nodes: nodeRepo, Queue: queueRepo, Embeddings: embeddingRepo,
		Embedder: stub, Chunker: embed.MarkdownRecursive{}, Workers: 2, EmbedBatchSize: 4,
	})
	if err != nil {
		test.Fatalf("DrainQueue: %v", err)
	}

	if stub.batchCalls == 0 {
		test.Errorf("batchCalls = 0, want >0 (batch path should be preferred)")
	}
	if stub.embedCalls != 0 {
		test.Errorf("embedCalls = %d, want 0 (no fallback expected on success)", stub.embedCalls)
	}
}

func TestDrainQueue_BatchFailureFallsBackToEmbed(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	body := strings.Repeat("paragraph paragraph paragraph paragraph.\n\n", 100)
	createNodeFile(test, root, "notes/big.md", body)
	nodeRepo.Upsert(index.NodeRow{ID: "notes/big", Type: "note", Path: "notes/big.md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"})
	queueRepo.Enqueue("notes/big")

	stub := &batchStubEmbedder{dim: 3, model: "stub", failBatch: true}

	_, err := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root: root, Nodes: nodeRepo, Queue: queueRepo, Embeddings: embeddingRepo,
		Embedder: stub, Chunker: embed.MarkdownRecursive{}, Workers: 2, EmbedBatchSize: 4,
	})
	if err != nil {
		test.Fatalf("DrainQueue: %v", err)
	}

	if stub.embedCalls == 0 {
		test.Errorf("embedCalls = 0, want >0 (per-prompt fallback should run after batch failure)")
	}

	depth, _ := queueRepo.Depth()
	if depth != 0 {
		test.Errorf("queue depth = %d, want 0 (node should have succeeded via fallback)", depth)
	}
}

func TestDrainQueue_BatchAndFallbackBothFailReEnqueues(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	body := strings.Repeat("paragraph paragraph paragraph paragraph.\n\n", 100)
	createNodeFile(test, root, "notes/big.md", body)
	nodeRepo.Upsert(index.NodeRow{ID: "notes/big", Type: "note", Path: "notes/big.md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"})
	queueRepo.Enqueue("notes/big")

	stub := &batchStubEmbedder{dim: 3, model: "stub", failBatch: true, failOnEmbed: true}

	_, err := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root: root, Nodes: nodeRepo, Queue: queueRepo, Embeddings: embeddingRepo,
		Embedder: stub, Chunker: embed.MarkdownRecursive{}, Workers: 2, EmbedBatchSize: 4,
	})
	if err != nil {
		test.Fatalf("DrainQueue: %v", err)
	}

	depth, _ := queueRepo.Depth()
	if depth == 0 {
		test.Errorf("queue depth = 0, want >0 (node should be re-enqueued)")
	}
}
```

- [ ] **Step 2: Run to verify all three fail (no `EmbedBatchSize` field)**

Run: `go test ./internal/embed/... -run TestDrainQueue_Batch -v`

Expected: compile error — `DrainConfig` has no `EmbedBatchSize` field.

- [ ] **Step 3: Add `EmbedBatchSize` to DrainConfig and dispatch batch jobs in workers**

Edit `internal/embed/drain.go`. Add `EmbedBatchSize int` to DrainConfig:

```go
	EmbedBatchSize int  // chunks per EmbedBatch call when Embedder implements BatchEmbedder; defaults to 1 (per-prompt path)
```

Modify the per-node block (the worker pool added in Task 3) to use batches. The new structure: at the top of the per-node block, type-assert and decide batchSize. Producer dispatches batch jobs (groups of up to batchSize chunks) instead of single chunks. Worker calls `EmbedBatch` if available; on error, calls `Embed` per payload as fallback within the same job.

Replace the existing `embedJob`/`embedResult` types and worker body with:

```go
			batchSize := config.EmbedBatchSize
			if batchSize < 1 {
				batchSize = 1
			}

			batchEmbedder, useBatch := config.Embedder.(BatchEmbedder)
			useBatch = useBatch && batchSize > 1

			type embedJob struct {
				chunkIdxs []int
				payloads  [][]byte
				bodies    [][]byte
			}
			type embedResult struct {
				chunkIdx int
				vector   []float32
				body     []byte
				err      error
			}

			nodeCtx, cancel := context.WithCancel(ctx)

			// Total job count = ceil(len(bodyChunks) / batchSize)
			numJobs := (len(bodyChunks) + batchSize - 1) / batchSize
			jobs := make(chan embedJob, numJobs)
			results := make(chan embedResult, len(bodyChunks))

			workers := config.Workers
			if workers < 1 {
				workers = 1
			}
			if workers > numJobs {
				workers = numJobs
			}

			runJob := func(job embedJob) {
				if nodeCtx.Err() != nil {
					for _, idx := range job.chunkIdxs {
						results <- embedResult{chunkIdx: idx, err: nodeCtx.Err()}
					}
					return
				}

				if useBatch {
					vectors, err := batchEmbedder.EmbedBatch(nodeCtx, job.payloads)
					if err == nil {
						for i, vec := range vectors {
							results <- embedResult{chunkIdx: job.chunkIdxs[i], vector: vec, body: job.bodies[i]}
						}
						return
					}

					// Fallback: per-prompt Embed for this batch only.
					if config.Logger != nil {
						config.Logger.Warn("embed batch failed, falling back to per-prompt",
							"node_id", queued.NodeID,
							"batch_size", len(job.payloads),
							"err", err.Error(),
						)
					}
				}

				for i, payload := range job.payloads {
					if nodeCtx.Err() != nil {
						results <- embedResult{chunkIdx: job.chunkIdxs[i], err: nodeCtx.Err()}
						continue
					}

					vec, err := config.Embedder.Embed(nodeCtx, payload)
					results <- embedResult{
						chunkIdx: job.chunkIdxs[i],
						vector:   vec,
						body:     job.bodies[i],
						err:      err,
					}
				}
			}

			var wg sync.WaitGroup
			for w := 0; w < workers; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for job := range jobs {
						runJob(job)
					}
				}()
			}

			// Producer: group consecutive chunks into batch jobs.
			for start := 0; start < len(bodyChunks); start += batchSize {
				end := start + batchSize
				if end > len(bodyChunks) {
					end = len(bodyChunks)
				}

				job := embedJob{
					chunkIdxs: make([]int, 0, end-start),
					payloads:  make([][]byte, 0, end-start),
					bodies:    make([][]byte, 0, end-start),
				}
				for chunkIdx := start; chunkIdx < end; chunkIdx++ {
					bodyChunk := bodyChunks[chunkIdx]
					payload := make([]byte, 0, len(header)+len(bodyChunk))
					payload = append(payload, header...)
					payload = append(payload, bodyChunk...)

					job.chunkIdxs = append(job.chunkIdxs, chunkIdx)
					job.payloads = append(job.payloads, payload)
					job.bodies = append(job.bodies, bodyChunk)
				}
				jobs <- job
			}
			close(jobs)
```

The collector (`for r := range results`), error-handling block, and sort+upsert block from Task 3 remain unchanged — they still consume `len(bodyChunks)` results.

- [ ] **Step 4: Run the three batch tests**

Run: `go test ./internal/embed/... -run TestDrainQueue_Batch -v`

Expected: 3 PASS.

- [ ] **Step 5: Run the full embed package + race detector**

Run: `go test -race ./internal/embed/...`

Expected: PASS, no races.

- [ ] **Step 6: Commit**

```bash
git add internal/embed/drain.go internal/embed/drain_test.go
git commit -m "feat(embed): batched EmbedBatch path in drain with per-prompt fallback"
```

---

## Task 9: Wire `embeddings.batch-size` through manifest and construction sites

**Files:**
- Modify: `internal/manifest/manifest.go` (add BatchSize field)
- Modify: `internal/manifest/loader.go` (validate)
- Modify: `internal/manifest/loader_test.go` (round-trip + rejection)
- Modify: `internal/embed/embedder.go` (DefaultBatchSize const + ResolveBatchSize)
- Modify: `internal/reindex/reindex.go` (Config.EmbedBatchSize → DrainConfig.EmbedBatchSize)
- Modify: `internal/mcp/runtime.go` (Runtime.EmbedBatchSize)
- Modify: `internal/mcp/drainer.go` (forward into DrainConfig)
- Modify: `cmd/tusk/cmd_reindex.go` (pass through reindex.Config)

- [ ] **Step 1: Write the failing manifest tests**

Append to `internal/manifest/loader_test.go`, following the same `tmpDir + WriteFile + manifest.Load` pattern used in Task 1:

```go
func TestLoad_ParsesEmbeddingsBatchSize(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "x"

[embeddings]
provider   = "ollama"
model      = "nomic-embed-text"
endpoint   = "http://localhost:11434"
dim        = 768
batch-size = 16
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)
	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	if loaded.Embeddings.BatchSize != 16 {
		test.Errorf("BatchSize = %d, want 16", loaded.Embeddings.BatchSize)
	}
}

func TestLoad_RejectsNegativeBatchSize(test *testing.T) {
	tmpDir := test.TempDir()
	manifestPath := filepath.Join(tmpDir, "tusk.toml")

	body := `[workspace]
name = "x"

[embeddings]
provider   = "ollama"
model      = "nomic-embed-text"
endpoint   = "http://localhost:11434"
dim        = 768
batch-size = -1
`

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	_, loadErr := manifest.Load(manifestPath)
	if loadErr == nil {
		test.Fatalf("Load: expected error for batch-size=-1")
	}
	if !strings.Contains(loadErr.Error(), "batch-size") {
		test.Errorf("Load error = %q, want it to mention 'batch-size'", loadErr.Error())
	}
}
```

- [ ] **Step 2: Run to verify failures**

Run: `go test ./internal/manifest/... -run BatchSize -v`

Expected: 2 failures.

- [ ] **Step 3: Add `BatchSize` to EmbeddingsSection and validation**

Edit `internal/manifest/manifest.go`. Add a `BatchSize int` field with TOML tag `batch-size` to `EmbeddingsSection`. The struct now reads:

```go
type EmbeddingsSection struct {
	Provider       string `toml:"provider"`
	Model          string `toml:"model"`
	Endpoint       string `toml:"endpoint"`
	Dim            int    `toml:"dim"`
	APIKey         string `toml:"api-key"`
	Workers        int    `toml:"workers"`
	TimeoutSeconds int    `toml:"timeout-seconds"`
	BatchSize      int    `toml:"batch-size"`
}
```

Edit `internal/manifest/loader.go`. In the `[embeddings]` validation block, append:

```go
		if loaded.Embeddings.BatchSize < 0 {
			return fmt.Errorf("manifest: embeddings.batch-size must be >= 0 (got %d); zero or absent uses the default", loaded.Embeddings.BatchSize)
		}
```

- [ ] **Step 4: Add `DefaultBatchSize` and `ResolveBatchSize` helper**

Edit `internal/embed/embedder.go`. Update the constants block from Task 4:

```go
const (
	DefaultWorkers        = 4
	DefaultTimeoutSeconds = 120
	DefaultBatchSize      = 8
)

func ResolveBatchSize(configured int) int {
	if configured <= 0 {
		return DefaultBatchSize
	}
	return configured
}
```

- [ ] **Step 5: Forward through reindex / mcp runtime / drainer / cmd_reindex**

Edit `internal/reindex/reindex.go`: add `EmbedBatchSize int` to Config, forward to `DrainConfig.EmbedBatchSize` in the call site.

Edit `internal/mcp/runtime.go`: add `EmbedBatchSize int` to Runtime, populate from `embed.ResolveBatchSize(loaded.Embeddings.BatchSize)` in the constructor.

Edit `internal/mcp/drainer.go`: forward `config.Runtime.EmbedBatchSize` into DrainConfig.

Edit `cmd/tusk/cmd_reindex.go`: pass `EmbedBatchSize: embed.ResolveBatchSize(loaded.Embeddings.BatchSize)` into `reindex.Config`.

- [ ] **Step 6: Run all tests**

Run: `make test`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/manifest/manifest.go internal/manifest/loader.go internal/manifest/loader_test.go internal/embed/embedder.go internal/reindex/reindex.go internal/mcp/runtime.go internal/mcp/drainer.go cmd/tusk/cmd_reindex.go
git commit -m "feat(embed,manifest,cli,mcp): wire embeddings.batch-size through to drain"
```

---

## Task 10: Phase 2 race-detector pass, throughput measurement, PR

**Files:** none modified.

- [ ] **Step 1: Race-detector pass**

Run: `make test-race`

Expected: PASS.

- [ ] **Step 2: Capture wall-clock with batching**

```bash
rm -f .tusk/index.db
time ./bin/tusk reindex
```

Record. Compare to the Phase 1 numbers in the PR description for that branch.

- [ ] **Step 3: Open the Phase 2 PR**

```bash
git push -u origin feat/batched-embed
gh pr create --title "feat(embed): batched Ollama EmbedBatch path with per-prompt fallback" --body "$(cat <<'EOF'
## Summary

- New optional `BatchEmbedder` interface; `OllamaEmbedder.EmbedBatch` POSTs `prompts: []` to `/api/embeddings` (Ollama 0.1.30+).
- Drain loop type-asserts and dispatches up to `embeddings.batch-size` chunks per HTTP call (default 8). Composes with Phase 1 workers — worker count caps parallelism, batch size caps prompts per call.
- Per-prompt fallback within an attempt: if `EmbedBatch` errors, the worker retries that batch's chunks one-at-a-time via `Embed`. Failure of any per-prompt call re-enqueues the node with `attempts+1`. Successful fallback does not consume an attempt.
- New manifest knob `embeddings.batch-size` (int, default 8, validated `>= 1`).

Spec: `docs/superpowers/specs/2026-05-14-parallel-batched-embed-design.md` Phase 2.
Plan: `docs/superpowers/plans/2026-05-15-parallel-batched-embed.md` Tasks 6–10.

## Throughput measurement (dogfood workspace)

| Mode | Wall-clock |
|---|---|
| Phase 1 baseline (workers=4, batch=1) | <fill in> |
| Phase 2 (workers=4, batch=8) | <fill in> |

## Test plan

- [x] `go test ./...` passes
- [x] `make test-race` passes
- [x] Batch path test asserts EmbedBatch is called and Embed is not (success path)
- [x] Batch failure → per-prompt fallback test asserts Embed is called and node succeeds
- [x] Batch + fallback failure test asserts node re-enqueues
EOF
)"
```

- [ ] **Step 4: Wait for CI and merge once green**

Human checkpoint.

---

## Plan self-review notes

This section is for the planner, not the implementer — it documents the spec-coverage check.

- All five resolved questions from the spec map to tasks: #1 timeout knob → Tasks 1, 2, 4; #2 per-node parallelism → Task 3; #3 worker default 4 → Task 4 (ResolveWorkers); #4 batch default 8 → Task 9 (ResolveBatchSize); #5 BatchEmbedder optional interface → Tasks 6, 7.
- All four risks from the spec are noted in either the task body or the PR description. The `OLLAMA_NUM_PARALLEL` interaction is left for the PR author to mention; the memory cost of atomic collect is implicit in the implementation; SQLite write contention is preserved (single-writer collector).
- The naming-collision avoidance — keeping `DrainConfig.BatchSize` and adding `EmbedBatchSize` — is preserved across Tasks 3, 8, 9.
- The "zero means preserve prior behavior" convention is consistent: `DrainConfig.Workers == 0 → 1`, `DrainConfig.EmbedBatchSize == 0 → 1`, `OllamaConfig.Timeout == 0 → 30s`. The user-facing defaults (4, 8, 120s) live in `embed.Resolve*` helpers, applied at wiring sites only.
