package embed_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		test.Errorf("persisted rows = %d, want %d (one per embed call)", len(rows), stub.calls)
	}

	for idx, row := range rows {
		if row.ChunkIdx != idx {
			test.Errorf("rows[%d].ChunkIdx = %d, want %d (sequential)", idx, row.ChunkIdx, idx)
		}
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
