package embed_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/index"
)

type drainStubEmbedder struct {
	calls   int
	dim     int
	model   string
	failure error
}

func (stub *drainStubEmbedder) Embed(ctx context.Context, payload []byte) ([]float32, error) {
	stub.calls++

	if stub.failure != nil {
		return nil, stub.failure
	}

	out := make([]float32, stub.dim)

	for idx := range out {
		out[idx] = 0.1
	}

	return out, nil
}

func (stub *drainStubEmbedder) Model() string { return stub.model }
func (stub *drainStubEmbedder) Dim() int      { return stub.dim }

func TestDrainQueue_DrainsToEmpty(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	createNodeFile(test, root, "notes/a.md", "hi")
	createNodeFile(test, root, "notes/b.md", "ho")

	for _, id := range []string{"notes/a", "notes/b"} {
		nodeRepo.Upsert(index.NodeRow{ID: id, Type: "note", Path: id + ".md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"})
		queueRepo.Enqueue(id)
	}

	drained, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepo,
		Queue:      queueRepo,
		Embeddings: embeddingRepo,
		Embedder:   &drainStubEmbedder{dim: 3, model: "stub"},
		Chunker:    embed.WholeDocument{},
		BatchSize:  50,
	})

	if drainErr != nil {
		test.Fatalf("DrainQueue: %v", drainErr)
	}

	if drained != 2 {
		test.Errorf("drained = %d, want 2", drained)
	}

	depth, _ := queueRepo.Depth()

	if depth != 0 {
		test.Errorf("depth = %d, want 0", depth)
	}
}

func TestDrainQueue_NoopWhenNoEmbedder(test *testing.T) {
	store := openIndex(test, test.TempDir())
	defer store.Close()

	drained, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Queue: index.NewEmbedQueueRepo(store),
	})

	if drainErr != nil {
		test.Fatalf("DrainQueue: %v", drainErr)
	}

	if drained != 0 {
		test.Errorf("drained = %d, want 0", drained)
	}
}

func openIndex(test *testing.T, root string) *index.Index {
	test.Helper()

	store, openErr := index.Open(filepath.Join(root, "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	return store
}

func createNodeFile(test *testing.T, root, relPath, body string) {
	test.Helper()

	abs := filepath.Join(root, relPath)

	if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	frontmatter := "---\ntype: note\ntitle: x\n---\n\n" + body + "\n"

	if writeErr := os.WriteFile(abs, []byte(frontmatter), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}
}

func TestDrainQueue_LogsWarnOnEmbedError(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	createNodeFile(test, root, "doc.md", "body")

	if upsertErr := nodeRepo.Upsert(index.NodeRow{ID: "doc", Type: "note", Path: "doc.md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert: %v", upsertErr)
	}

	if enqErr := queueRepo.Enqueue("doc"); enqErr != nil {
		test.Fatalf("enqueue: %v", enqErr)
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	failing := &drainStubEmbedder{
		dim:     3,
		model:   "fake",
		failure: fmt.Errorf("input length exceeds the context length"),
	}

	_, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepo,
		Queue:      queueRepo,
		Embeddings: embeddingRepo,
		Embedder:   failing,
		Chunker:    embed.WholeDocument{},
		Logger:     logger,
	})

	if drainErr != nil {
		test.Fatalf("drain: %v", drainErr)
	}

	out := buf.String()

	for _, want := range []string{`msg="embed call failed"`, "node_id=doc", "payload_bytes=", "input length exceeds the context length"} {
		if !strings.Contains(out, want) {
			test.Errorf("expected log to contain %q; got %q", want, out)
		}
	}

	if !strings.Contains(out, `msg="embed re-enqueued"`) {
		test.Errorf("expected re-enqueue log; got %q", out)
	}

	if !strings.Contains(out, `msg="drain batch complete"`) {
		test.Errorf("expected batch summary log; got %q", out)
	}
}

func TestDrainQueue_GivesUpAfterMaxAttempts(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	createNodeFile(test, root, "doomed.md", "body")

	if upsertErr := nodeRepo.Upsert(index.NodeRow{ID: "doomed", Type: "note", Path: "doomed.md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert: %v", upsertErr)
	}

	if enqErr := queueRepo.Enqueue("doomed"); enqErr != nil {
		test.Fatalf("enqueue: %v", enqErr)
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	failing := &drainStubEmbedder{
		dim:     3,
		model:   "fake",
		failure: fmt.Errorf("forced failure"),
	}

	drained, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepo,
		Queue:      queueRepo,
		Embeddings: embeddingRepo,
		Embedder:   failing,
		Chunker:    embed.WholeDocument{},
		Logger:     logger,
	})

	if drainErr != nil {
		test.Fatalf("DrainQueue: %v", drainErr)
	}

	if drained != 0 {
		test.Errorf("drained = %d, want 0 (all attempts failed)", drained)
	}

	if failing.calls != embed.MaxEmbedAttempts {
		test.Errorf("embedder.calls = %d, want %d", failing.calls, embed.MaxEmbedAttempts)
	}

	out := buf.String()

	embedFailures := strings.Count(out, `msg="embed call failed"`)

	if embedFailures != embed.MaxEmbedAttempts {
		test.Errorf("`embed call failed` count = %d, want %d", embedFailures, embed.MaxEmbedAttempts)
	}

	reEnqueues := strings.Count(out, `msg="embed re-enqueued"`)

	if reEnqueues != embed.MaxEmbedAttempts-1 {
		test.Errorf("`embed re-enqueued` count = %d, want %d", reEnqueues, embed.MaxEmbedAttempts-1)
	}

	gaveUps := strings.Count(out, `msg="embed gave up"`)

	if gaveUps != 1 {
		test.Errorf("`embed gave up` count = %d, want 1", gaveUps)
	}

	if !strings.Contains(out, fmt.Sprintf("attempts=%d", embed.MaxEmbedAttempts)) {
		test.Errorf("expected `embed gave up` log to include attempts=%d; got %q", embed.MaxEmbedAttempts, out)
	}

	depth, depthErr := queueRepo.Depth()

	if depthErr != nil {
		test.Fatalf("Depth: %v", depthErr)
	}

	if depth != 0 {
		test.Errorf("queue depth after give-up = %d, want 0", depth)
	}
}

func TestDrainQueue_EmbedsEveryChunkOfMultiChunkNode(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	// Body with three H2 sections, large enough that the splitter emits 3 chunks.
	body := strings.Repeat("alpha ", 200) +
		"\n## Section B\n" + strings.Repeat("bravo ", 200) +
		"\n## Section C\n" + strings.Repeat("charlie ", 200)

	createNodeFile(test, root, "multi.md", body)

	if upsertErr := nodeRepo.Upsert(index.NodeRow{ID: "multi", Type: "note", Path: "multi.md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert: %v", upsertErr)
	}

	if enqErr := queueRepo.Enqueue("multi"); enqErr != nil {
		test.Fatalf("enqueue: %v", enqErr)
	}

	stub := &drainStubEmbedder{dim: 3, model: "stub"}

	drained, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepo,
		Queue:      queueRepo,
		Embeddings: embeddingRepo,
		Embedder:   stub,
		Chunker:    embed.MarkdownRecursive{TargetBytes: 400, MaxBytes: 2000, OverlapBytes: 0},
	})

	if drainErr != nil {
		test.Fatalf("DrainQueue: %v", drainErr)
	}

	if drained != 1 {
		test.Errorf("drained = %d, want 1", drained)
	}

	if stub.calls < 2 {
		test.Errorf("embedder.calls = %d, want >= 2 (one per chunk)", stub.calls)
	}

	rows, getErr := embeddingRepo.GetByNodeID("multi")

	if getErr != nil {
		test.Fatalf("GetByNodeID: %v", getErr)
	}

	if len(rows) != stub.calls {
		test.Errorf("persisted rows = %d, want %d (one per chunk)", len(rows), stub.calls)
	}
}

func TestDrainQueue_DeletesStaleChunksBeforeReembedding(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	// Pre-seed 5 stale chunks for a node, then drain — the new chunk count
	// should be < 5 (we use WholeDocument for a tiny body), and all old
	// rows (chunk_idx 1..4) must be gone.
	createNodeFile(test, root, "shrinking.md", "tiny body")

	if upsertErr := nodeRepo.Upsert(index.NodeRow{ID: "shrinking", Type: "note", Path: "shrinking.md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert node: %v", upsertErr)
	}

	for idx := 0; idx < 5; idx++ {
		if upErr := embeddingRepo.Upsert(index.EmbeddingRow{
			NodeID:      "shrinking",
			ChunkIdx:    idx,
			Model:       "stub",
			ContentHash: "old",
			Vector:      []float32{0.1, 0.1, 0.1},
			Dim:         3,
		}); upErr != nil {
			test.Fatalf("seed chunk %d: %v", idx, upErr)
		}
	}

	if enqErr := queueRepo.Enqueue("shrinking"); enqErr != nil {
		test.Fatalf("enqueue: %v", enqErr)
	}

	_, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepo,
		Queue:      queueRepo,
		Embeddings: embeddingRepo,
		Embedder:   &drainStubEmbedder{dim: 3, model: "stub"},
		Chunker:    embed.WholeDocument{},
	})

	if drainErr != nil {
		test.Fatalf("DrainQueue: %v", drainErr)
	}

	rows, _ := embeddingRepo.GetByNodeID("shrinking")

	if len(rows) != 1 {
		test.Errorf("expected exactly 1 chunk after re-embed; got %d", len(rows))
	}

	for _, row := range rows {
		if row.ContentHash == "old" {
			test.Errorf("stale chunk survived: %+v", row)
		}
	}
}

func TestDrainQueue_NodeFailureReenqueuesAndCleansOnRetry(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	body := strings.Repeat("alpha ", 200) + "\n## Section B\n" + strings.Repeat("bravo ", 200)
	createNodeFile(test, root, "flaky.md", body)

	if upsertErr := nodeRepo.Upsert(index.NodeRow{ID: "flaky", Type: "note", Path: "flaky.md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert: %v", upsertErr)
	}

	if enqErr := queueRepo.Enqueue("flaky"); enqErr != nil {
		test.Fatalf("enqueue: %v", enqErr)
	}

	// Embedder that fails on the second call, succeeds otherwise.
	failingMidStream := &midStreamFailEmbedder{dim: 3, model: "stub", failAt: 2}

	_, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepo,
		Queue:      queueRepo,
		Embeddings: embeddingRepo,
		Embedder:   failingMidStream,
		Chunker:    embed.MarkdownRecursive{TargetBytes: 400, MaxBytes: 2000, OverlapBytes: 0},
	})

	if drainErr != nil {
		test.Fatalf("DrainQueue: %v", drainErr)
	}

	// After the retry cap is hit, the node is dropped. Partial state from
	// any successful retry's DeleteByNodeID + Upsert sequence must not
	// leave duplicated chunks.
	rows, _ := embeddingRepo.GetByNodeID("flaky")

	chunkIdxs := make(map[int]struct{}, len(rows))

	for _, row := range rows {
		if _, dup := chunkIdxs[row.ChunkIdx]; dup {
			test.Errorf("duplicate ChunkIdx %d after retries: %+v", row.ChunkIdx, rows)
		}

		chunkIdxs[row.ChunkIdx] = struct{}{}
	}
}

type midStreamFailEmbedder struct {
	calls  int
	dim    int
	model  string
	failAt int // 1-indexed: fail when calls reaches failAt
}

func (stub *midStreamFailEmbedder) Embed(ctx context.Context, payload []byte) ([]float32, error) {
	stub.calls++

	if stub.calls == stub.failAt {
		return nil, fmt.Errorf("simulated chunk failure")
	}

	out := make([]float32, stub.dim)

	for idx := range out {
		out[idx] = 0.1
	}

	return out, nil
}

func (stub *midStreamFailEmbedder) Model() string { return stub.model }
func (stub *midStreamFailEmbedder) Dim() int      { return stub.dim }

func TestDrainQueue_StoresChunkBody(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	bodyText := "This is the body of the chunk we want to verify."
	createNodeFile(test, root, "notes/snippet-target.md", bodyText)

	nodeRepo.Upsert(index.NodeRow{
		ID:             "notes/snippet-target",
		Type:           "note",
		Path:           "notes/snippet-target.md",
		Title:          "Snippet Target",
		PropertiesJSON: "{}",
		LastChecksum:   "x",
	})

	if enqErr := queueRepo.Enqueue("notes/snippet-target"); enqErr != nil {
		test.Fatalf("enqueue: %v", enqErr)
	}

	drained, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepo,
		Queue:      queueRepo,
		Embeddings: embeddingRepo,
		Embedder:   &drainStubEmbedder{dim: 3, model: "stub"},
		Chunker:    embed.WholeDocument{},
	})

	if drainErr != nil {
		test.Fatalf("drain: %v", drainErr)
	}

	if drained != 1 {
		test.Fatalf("drained = %d, want 1", drained)
	}

	rows, getErr := embeddingRepo.GetByNodeID("notes/snippet-target")

	if getErr != nil {
		test.Fatalf("get: %v", getErr)
	}

	if len(rows) == 0 {
		test.Fatal("no embedding rows persisted")
	}

	if !strings.Contains(rows[0].Body, "body of the chunk") {
		test.Errorf("first chunk body = %q, want substring 'body of the chunk'", rows[0].Body)
	}
}

type sleepStubEmbedder struct {
	dim   int
	model string
	sleep time.Duration
	mu    sync.Mutex
	calls int
}

func (stub *sleepStubEmbedder) Embed(ctx context.Context, payload []byte) ([]float32, error) {
	stub.mu.Lock()
	stub.calls++
	stub.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(stub.sleep):
	}

	out := make([]float32, stub.dim)
	for idx := range out {
		out[idx] = 0.1
	}

	return out, nil
}

func (stub *sleepStubEmbedder) Model() string { return stub.model }
func (stub *sleepStubEmbedder) Dim() int      { return stub.dim }

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

	// Permanent failure: every embed call errors. Across MaxEmbedAttempts
	// retries, no chunk should ever be persisted (per-node atomicity), and
	// the queue eventually drops the node after the give-up.
	stub := &alwaysFailingSleepStubEmbedder{dim: 3, model: "stub", sleep: 1 * time.Millisecond}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	_, err := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root: root, Nodes: nodeRepo, Queue: queueRepo, Embeddings: embeddingRepo,
		Embedder: stub, Chunker: embed.MarkdownRecursive{}, Workers: 4,
		Logger: logger,
	})

	if err != nil {
		test.Fatalf("DrainQueue: %v", err)
	}

	// Per-node atomicity: at no point are partial chunks persisted. Because
	// every attempt fails, the final state is zero rows.
	rows, _ := embeddingRepo.GetByNodeID("notes/big")
	if len(rows) != 0 {
		test.Errorf("rows after error = %d, want 0 (per-node atomicity)", len(rows))
	}

	// Re-enqueue happened on the first failure (attempts=1) — visible in
	// logs even though the queue eventually empties via give-up.
	out := buf.String()
	if !strings.Contains(out, `msg="embed re-enqueued"`) || !strings.Contains(out, "attempts=1") {
		test.Errorf("expected first failure to log re-enqueue with attempts=1; got %q", out)
	}
}

type alwaysFailingSleepStubEmbedder struct {
	dim   int
	model string
	sleep time.Duration
	mu    sync.Mutex
	calls int
}

func (stub *alwaysFailingSleepStubEmbedder) Embed(ctx context.Context, payload []byte) ([]float32, error) {
	stub.mu.Lock()
	stub.calls++
	stub.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(stub.sleep):
	}

	return nil, fmt.Errorf("stub: forced failure")
}

func (stub *alwaysFailingSleepStubEmbedder) Model() string { return stub.model }
func (stub *alwaysFailingSleepStubEmbedder) Dim() int      { return stub.dim }

func TestDrainQueue_SkipsEmbedWhenContentUnchanged(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	createNodeFile(test, root, "notes/stable.md", "unchanged body")

	if upsertErr := nodeRepo.Upsert(index.NodeRow{ID: "notes/stable", Type: "note", Path: "notes/stable.md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert: %v", upsertErr)
	}

	stub := &drainStubEmbedder{dim: 3, model: "stub"}

	if enqErr := queueRepo.Enqueue("notes/stable"); enqErr != nil {
		test.Fatalf("enqueue: %v", enqErr)
	}

	if _, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepo,
		Queue:      queueRepo,
		Embeddings: embeddingRepo,
		Embedder:   stub,
		Chunker:    embed.WholeDocument{},
	}); drainErr != nil {
		test.Fatalf("first drain: %v", drainErr)
	}

	firstPassCalls := stub.calls

	if firstPassCalls == 0 {
		test.Fatalf("first drain made no embed calls; setup is broken")
	}

	rowsBefore, _ := embeddingRepo.GetByNodeID("notes/stable")

	if len(rowsBefore) == 0 {
		test.Fatalf("first drain persisted no rows; setup is broken")
	}

	// Re-enqueue the same node without changing the file. This mirrors the
	// reindex-loop pattern that enqueues every seen node every pass.
	if enqErr := queueRepo.Enqueue("notes/stable"); enqErr != nil {
		test.Fatalf("re-enqueue: %v", enqErr)
	}

	drained, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepo,
		Queue:      queueRepo,
		Embeddings: embeddingRepo,
		Embedder:   stub,
		Chunker:    embed.WholeDocument{},
	})

	if drainErr != nil {
		test.Fatalf("second drain: %v", drainErr)
	}

	if drained != 1 {
		test.Errorf("drained = %d, want 1 (node was still processed, just not re-embedded)", drained)
	}

	if stub.calls != firstPassCalls {
		test.Errorf("embedder.calls = %d after second drain, want %d (no re-embedding for unchanged content)", stub.calls, firstPassCalls)
	}

	rowsAfter, _ := embeddingRepo.GetByNodeID("notes/stable")

	if len(rowsAfter) != len(rowsBefore) {
		test.Errorf("row count after second drain = %d, want %d (existing embeddings preserved)", len(rowsAfter), len(rowsBefore))
	}

	for i := range rowsAfter {
		if rowsAfter[i].ContentHash != rowsBefore[i].ContentHash {
			test.Errorf("rows[%d].ContentHash changed: was %q, now %q", i, rowsBefore[i].ContentHash, rowsAfter[i].ContentHash)
		}
	}
}

func TestDrainQueue_SkipsEmbedWhenContentUnchanged_MultiChunk(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	body := strings.Repeat("alpha ", 200) +
		"\n## Section B\n" + strings.Repeat("bravo ", 200) +
		"\n## Section C\n" + strings.Repeat("charlie ", 200)

	createNodeFile(test, root, "notes/multi.md", body)

	if upsertErr := nodeRepo.Upsert(index.NodeRow{ID: "notes/multi", Type: "note", Path: "notes/multi.md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert: %v", upsertErr)
	}

	stub := &drainStubEmbedder{dim: 3, model: "stub"}
	chunker := embed.MarkdownRecursive{TargetBytes: 400, MaxBytes: 2000, OverlapBytes: 0}

	if enqErr := queueRepo.Enqueue("notes/multi"); enqErr != nil {
		test.Fatalf("enqueue: %v", enqErr)
	}

	if _, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepo,
		Queue:      queueRepo,
		Embeddings: embeddingRepo,
		Embedder:   stub,
		Chunker:    chunker,
	}); drainErr != nil {
		test.Fatalf("first drain: %v", drainErr)
	}

	firstPassCalls := stub.calls

	if firstPassCalls < 2 {
		test.Fatalf("first drain made %d embed calls; need >= 2 chunks for this test", firstPassCalls)
	}

	if enqErr := queueRepo.Enqueue("notes/multi"); enqErr != nil {
		test.Fatalf("re-enqueue: %v", enqErr)
	}

	drained, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepo,
		Queue:      queueRepo,
		Embeddings: embeddingRepo,
		Embedder:   stub,
		Chunker:    chunker,
	})

	if drainErr != nil {
		test.Fatalf("second drain: %v", drainErr)
	}

	if drained != 1 {
		test.Errorf("drained = %d, want 1", drained)
	}

	if stub.calls != firstPassCalls {
		test.Errorf("embedder.calls = %d after second drain, want %d (no re-embedding for unchanged multi-chunk content)", stub.calls, firstPassCalls)
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

// recordingEmbedder captures every payload passed to Embed so sub-unit
// drainer tests can assert "the embedder saw exactly this byte slice"
// without parsing logs.
type recordingEmbedder struct {
	mu       sync.Mutex
	payloads [][]byte
	dim      int
	model    string
}

func (stub *recordingEmbedder) Embed(_ context.Context, payload []byte) ([]float32, error) {
	stub.mu.Lock()
	captured := make([]byte, len(payload))
	copy(captured, payload)
	stub.payloads = append(stub.payloads, captured)
	stub.mu.Unlock()

	out := make([]float32, stub.dim)

	for idx := range out {
		out[idx] = 0.1
	}

	return out, nil
}

func (stub *recordingEmbedder) Model() string { return stub.model }
func (stub *recordingEmbedder) Dim() int      { return stub.dim }

func TestDrainQueue_SubUnitEmbedsEmbedPayloadDirectly(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	// Seed a parent file row but never write the file to disk: the sub-unit
	// branch must NOT read the file or parse it. If the drainer falls
	// through to the legacy path, os.ReadFile will fail and the test will
	// catch the regression.
	if upsertErr := nodeRepo.Upsert(index.NodeRow{
		ID:             "notes/parent",
		Type:           "note",
		Path:           "notes/parent.md",
		Title:          "parent",
		PropertiesJSON: "{}",
		LastChecksum:   "x",
	}); upsertErr != nil {
		test.Fatalf("upsert parent: %v", upsertErr)
	}

	subUnitID := "notes/parent#abcd1234"

	if upsertErr := nodeRepo.BulkUpsert([]index.NodeRow{{
		ID:             subUnitID,
		Type:           "paragraph",
		Path:           "notes/parent.md",
		Title:          "",
		PropertiesJSON: "{}",
		LastChecksum:   "x",
		ParentID:       sql.NullString{String: "notes/parent", Valid: true},
		Ordinal:        sql.NullInt64{Int64: 0, Valid: true},
		EmbedPayload:   sql.NullString{String: "test payload", Valid: true},
	}}); upsertErr != nil {
		test.Fatalf("upsert sub-unit: %v", upsertErr)
	}

	if enqErr := queueRepo.Enqueue(subUnitID); enqErr != nil {
		test.Fatalf("enqueue: %v", enqErr)
	}

	stub := &recordingEmbedder{dim: 3, model: "stub"}

	drained, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepo,
		Queue:      queueRepo,
		Embeddings: embeddingRepo,
		Embedder:   stub,
		// Pass MarkdownRecursive to prove the drainer ignores Chunker for
		// sub-unit rows. If the branch wired this up wrong the sub-unit
		// would split on the (non-existent) markdown separators and we'd
		// see a different chunk count or wrong payload.
		Chunker: embed.MarkdownRecursive{},
	})

	if drainErr != nil {
		test.Fatalf("DrainQueue: %v", drainErr)
	}

	if drained != 1 {
		test.Errorf("drained = %d, want 1", drained)
	}

	if len(stub.payloads) != 1 {
		test.Fatalf("embedder calls = %d, want 1 (one vector per sub-unit per §5.6)", len(stub.payloads))
	}

	if string(stub.payloads[0]) != "test payload" {
		test.Errorf("embedded payload = %q, want %q (no header, no file context)", stub.payloads[0], "test payload")
	}

	rows, getErr := embeddingRepo.GetByNodeID(subUnitID)

	if getErr != nil {
		test.Fatalf("GetByNodeID: %v", getErr)
	}

	if len(rows) != 1 {
		test.Fatalf("persisted rows = %d, want 1", len(rows))
	}

	if rows[0].Body != "test payload" {
		test.Errorf("stored body = %q, want %q", rows[0].Body, "test payload")
	}

	if rows[0].ChunkIdx != 0 {
		test.Errorf("chunk_idx = %d, want 0 (sub-units are always single-chunk)", rows[0].ChunkIdx)
	}
}

func TestDrainQueue_SubUnitWithEmptyEmbedPayloadIsSkipped(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	subUnitID := "notes/parent#empty"

	if upsertErr := nodeRepo.BulkUpsert([]index.NodeRow{{
		ID:             subUnitID,
		Type:           "paragraph",
		Path:           "notes/parent.md",
		PropertiesJSON: "{}",
		LastChecksum:   "x",
		ParentID:       sql.NullString{String: "notes/parent", Valid: true},
		Ordinal:        sql.NullInt64{Int64: 0, Valid: true},
		EmbedPayload:   sql.NullString{String: "", Valid: true},
	}}); upsertErr != nil {
		test.Fatalf("upsert: %v", upsertErr)
	}

	if enqErr := queueRepo.Enqueue(subUnitID); enqErr != nil {
		test.Fatalf("enqueue: %v", enqErr)
	}

	stub := &recordingEmbedder{dim: 3, model: "stub"}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	drained, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepo,
		Queue:      queueRepo,
		Embeddings: embeddingRepo,
		Embedder:   stub,
		Chunker:    embed.WholeDocument{},
		Logger:     logger,
	})

	if drainErr != nil {
		test.Fatalf("DrainQueue: %v", drainErr)
	}

	if drained != 0 {
		test.Errorf("drained = %d, want 0 (empty payload skipped)", drained)
	}

	if len(stub.payloads) != 0 {
		test.Errorf("embedder calls = %d, want 0 (don't embed empty prompts)", len(stub.payloads))
	}

	if !strings.Contains(buf.String(), "embed skip empty sub-unit payload") {
		test.Errorf("expected warn log for empty payload; got %q", buf.String())
	}

	rows, _ := embeddingRepo.GetByNodeID(subUnitID)
	if len(rows) != 0 {
		test.Errorf("rows = %d, want 0", len(rows))
	}
}

func TestDrainQueue_MixedFileAndSubUnitBatch(test *testing.T) {
	// Drain a queue containing one file-level row AND one sub-unit row in
	// the same batch. Both must be embedded with their own respective
	// payloads — the file-level row reads from disk and chunks, the
	// sub-unit row reads from embed_payload and emits one chunk.
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	createNodeFile(test, root, "file-only.md", "file body content")

	if upsertErr := nodeRepo.Upsert(index.NodeRow{
		ID:             "file-only",
		Type:           "note",
		Path:           "file-only.md",
		Title:          "x",
		PropertiesJSON: "{}",
		LastChecksum:   "x",
	}); upsertErr != nil {
		test.Fatalf("upsert file: %v", upsertErr)
	}

	if upsertErr := nodeRepo.BulkUpsert([]index.NodeRow{{
		ID:             "parent#sub",
		Type:           "paragraph",
		Path:           "parent.md",
		PropertiesJSON: "{}",
		LastChecksum:   "x",
		ParentID:       sql.NullString{String: "parent", Valid: true},
		Ordinal:        sql.NullInt64{Int64: 0, Valid: true},
		EmbedPayload:   sql.NullString{String: "sub unit payload", Valid: true},
	}}); upsertErr != nil {
		test.Fatalf("upsert sub: %v", upsertErr)
	}

	for _, id := range []string{"file-only", "parent#sub"} {
		if enqErr := queueRepo.Enqueue(id); enqErr != nil {
			test.Fatalf("enqueue %s: %v", id, enqErr)
		}
	}

	stub := &recordingEmbedder{dim: 3, model: "stub"}

	drained, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepo,
		Queue:      queueRepo,
		Embeddings: embeddingRepo,
		Embedder:   stub,
		Chunker:    embed.WholeDocument{},
	})

	if drainErr != nil {
		test.Fatalf("DrainQueue: %v", drainErr)
	}

	if drained != 2 {
		test.Errorf("drained = %d, want 2", drained)
	}

	if len(stub.payloads) != 2 {
		test.Fatalf("embedder calls = %d, want 2", len(stub.payloads))
	}

	// Sub-unit payload is the EmbedPayload string verbatim; file payload
	// contains the BuildHeader+body shape ("[type] note" prefix is the
	// canonical marker).
	var sawSubUnit, sawFile bool

	for _, payload := range stub.payloads {
		if string(payload) == "sub unit payload" {
			sawSubUnit = true
			continue
		}

		if strings.Contains(string(payload), "[type] note") && strings.Contains(string(payload), "file body content") {
			sawFile = true
		}
	}

	if !sawSubUnit {
		test.Errorf("did not see verbatim sub-unit payload among %q", stub.payloads)
	}

	if !sawFile {
		test.Errorf("did not see headered file payload among %q", stub.payloads)
	}
}

// gateStubEmbedder blocks every Embed call until release is closed. Tests use
// it to observe the queue's leased state while a claim is still in-flight.
type gateStubEmbedder struct {
	dim     int
	model   string
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (stub *gateStubEmbedder) Embed(ctx context.Context, payload []byte) ([]float32, error) {
	stub.once.Do(func() { close(stub.started) })

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-stub.release:
		return nil, fmt.Errorf("released")
	}
}

func (stub *gateStubEmbedder) Model() string { return stub.model }
func (stub *gateStubEmbedder) Dim() int      { return stub.dim }

// TestDrainQueue_HonorsConfiguredTTL confirms that DrainConfig.TTL flows
// through to the EmbedQueueRepo.Drain claim. The test gates the embedder so
// the row stays leased while we read its leased_until_ns directly from the
// DB, then releases the embedder to let DrainQueue tear down cleanly.
func TestDrainQueue_HonorsConfiguredTTL(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	createNodeFile(test, root, "notes/a.md", "hi")

	if upsertErr := nodeRepo.Upsert(index.NodeRow{ID: "notes/a", Type: "note", Path: "notes/a.md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert: %v", upsertErr)
	}

	if enqErr := queueRepo.Enqueue("notes/a"); enqErr != nil {
		test.Fatalf("enqueue: %v", enqErr)
	}

	gate := &gateStubEmbedder{
		dim:     3,
		model:   "stub",
		started: make(chan struct{}),
		release: make(chan struct{}),
	}

	ttl := 5 * time.Second

	drainDone := make(chan struct{})

	go func() {
		defer close(drainDone)
		_, _ = embed.DrainQueue(context.Background(), embed.DrainConfig{
			Root:       root,
			Nodes:      nodeRepo,
			Queue:      queueRepo,
			Embeddings: embeddingRepo,
			Embedder:   gate,
			Chunker:    embed.WholeDocument{},
			TTL:        ttl,
		})
	}()

	select {
	case <-gate.started:
	case <-time.After(2 * time.Second):
		close(gate.release)
		<-drainDone
		test.Fatalf("embedder never started — DrainQueue did not claim the lease")
	}

	lowerBound := time.Now().Add(ttl - 1500*time.Millisecond).UnixNano()
	upperBound := time.Now().Add(ttl + 1500*time.Millisecond).UnixNano()

	var leasedUntilNs int64

	if scanErr := store.DB().QueryRow(
		`SELECT leased_until_ns FROM embed_queue WHERE node_id = ?`,
		"notes/a",
	).Scan(&leasedUntilNs); scanErr != nil {
		close(gate.release)
		<-drainDone
		test.Fatalf("query leased_until_ns: %v", scanErr)
	}

	if leasedUntilNs < lowerBound || leasedUntilNs > upperBound {
		close(gate.release)
		<-drainDone
		test.Errorf("leased_until_ns = %d, want in [%d, %d] (now + ~%v)", leasedUntilNs, lowerBound, upperBound, ttl)

		return
	}

	close(gate.release)
	<-drainDone
}

// TestDrainQueue_DefaultsTTLWhenUnset confirms that DrainQueue falls back to
// the 60-second default lease window when DrainConfig.TTL is zero. This is
// the back-compat path for existing callers that built DrainConfig without
// the TTL field.
func TestDrainQueue_DefaultsTTLWhenUnset(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	createNodeFile(test, root, "notes/a.md", "hi")

	if upsertErr := nodeRepo.Upsert(index.NodeRow{ID: "notes/a", Type: "note", Path: "notes/a.md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert: %v", upsertErr)
	}

	if enqErr := queueRepo.Enqueue("notes/a"); enqErr != nil {
		test.Fatalf("enqueue: %v", enqErr)
	}

	gate := &gateStubEmbedder{
		dim:     3,
		model:   "stub",
		started: make(chan struct{}),
		release: make(chan struct{}),
	}

	drainDone := make(chan struct{})

	go func() {
		defer close(drainDone)
		_, _ = embed.DrainQueue(context.Background(), embed.DrainConfig{
			Root:       root,
			Nodes:      nodeRepo,
			Queue:      queueRepo,
			Embeddings: embeddingRepo,
			Embedder:   gate,
			Chunker:    embed.WholeDocument{},
			// TTL intentionally omitted; defaults to 60s.
		})
	}()

	select {
	case <-gate.started:
	case <-time.After(2 * time.Second):
		close(gate.release)
		<-drainDone
		test.Fatalf("embedder never started")
	}

	defaultTTL := 60 * time.Second
	lowerBound := time.Now().Add(defaultTTL - 2*time.Second).UnixNano()
	upperBound := time.Now().Add(defaultTTL + 2*time.Second).UnixNano()

	var leasedUntilNs int64

	if scanErr := store.DB().QueryRow(
		`SELECT leased_until_ns FROM embed_queue WHERE node_id = ?`,
		"notes/a",
	).Scan(&leasedUntilNs); scanErr != nil {
		close(gate.release)
		<-drainDone
		test.Fatalf("query leased_until_ns: %v", scanErr)
	}

	close(gate.release)
	<-drainDone

	if leasedUntilNs < lowerBound || leasedUntilNs > upperBound {
		test.Errorf("leased_until_ns = %d, want in [%d, %d] (now + ~60s default)", leasedUntilNs, lowerBound, upperBound)
	}
}
