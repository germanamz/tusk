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
	"sync/atomic"
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

func TestDrainQueue_ReusesVectorWithoutModelCall(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	// Two parent files, each with a sub-unit carrying the SAME embed payload.
	for _, file := range []string{"notes/a", "notes/b"} {
		if upsertErr := nodeRepo.Upsert(index.NodeRow{
			ID: file, Type: "note", Path: file + ".md", Title: "x",
			PropertiesJSON: "{}", LastChecksum: "x",
		}); upsertErr != nil {
			test.Fatalf("upsert file %s: %v", file, upsertErr)
		}
	}

	subRows := []index.NodeRow{
		{
			ID: "notes/a#S1P1", Type: "paragraph", Path: "notes/a.md", Title: "p",
			PropertiesJSON: "{}", LastChecksum: "x",
			ParentID:     sql.NullString{String: "notes/a", Valid: true},
			Ordinal:      sql.NullInt64{Int64: 0, Valid: true},
			EmbedPayload: sql.NullString{String: "shared text", Valid: true},
		},
		{
			ID: "notes/b#S1P1", Type: "paragraph", Path: "notes/b.md", Title: "p",
			PropertiesJSON: "{}", LastChecksum: "x",
			ParentID:     sql.NullString{String: "notes/b", Valid: true},
			Ordinal:      sql.NullInt64{Int64: 0, Valid: true},
			EmbedPayload: sql.NullString{String: "shared text", Valid: true},
		},
	}

	if upsertErr := nodeRepo.BulkUpsert(subRows, "markdown"); upsertErr != nil {
		test.Fatalf("bulk upsert sub-units: %v", upsertErr)
	}

	for _, sub := range subRows {
		if enqErr := queueRepo.Enqueue(sub.ID); enqErr != nil {
			test.Fatalf("enqueue %s: %v", sub.ID, enqErr)
		}
	}

	embedder := &drainStubEmbedder{dim: 3, model: "stub"}

	drained, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepo,
		Queue:      queueRepo,
		Embeddings: embeddingRepo,
		Embedder:   embedder,
		Chunker:    embed.ASTChunking{},
		BatchSize:  50,
	})

	if drainErr != nil {
		test.Fatalf("DrainQueue: %v", drainErr)
	}

	if drained != 2 {
		test.Errorf("drained = %d, want 2", drained)
	}

	// The model is called exactly once: the second sub-unit reuses the
	// shared vector by content hash.
	if embedder.calls != 1 {
		test.Errorf("embedder.calls = %d, want 1 (identical content embeds once)", embedder.calls)
	}

	// Both sub-units still resolve their (shared) vector through the junction.
	first, _ := embeddingRepo.GetByNodeID("notes/a#S1P1")
	second, _ := embeddingRepo.GetByNodeID("notes/b#S1P1")

	if len(first) != 1 || len(second) != 1 {
		test.Errorf("each sub-unit resolves its vector: a=%d b=%d", len(first), len(second))
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

// concurrencyProbeEmbedder records the high-water mark of concurrent Embed
// calls, so a test can prove (or disprove) cross-node parallelism. Goroutine-safe.
type concurrencyProbeEmbedder struct {
	dim      int
	model    string
	hold     time.Duration
	inFlight atomic.Int32
	maxSeen  atomic.Int32
	calls    atomic.Int64
}

func (stub *concurrencyProbeEmbedder) Embed(ctx context.Context, payload []byte) ([]float32, error) {
	current := stub.inFlight.Add(1)

	for {
		prev := stub.maxSeen.Load()

		if current <= prev || stub.maxSeen.CompareAndSwap(prev, current) {
			break
		}
	}

	time.Sleep(stub.hold)
	stub.inFlight.Add(-1)
	stub.calls.Add(1)

	out := make([]float32, stub.dim)

	for idx := range out {
		out[idx] = 0.1
	}

	return out, nil
}

func (stub *concurrencyProbeEmbedder) Model() string { return stub.model }
func (stub *concurrencyProbeEmbedder) Dim() int      { return stub.dim }

// transportFailEmbedder always fails with a TransportError and holds no mutable
// state, so it is safe to call concurrently.
type transportFailEmbedder struct {
	dim   int
	model string
}

func (stub *transportFailEmbedder) Embed(ctx context.Context, payload []byte) ([]float32, error) {
	return nil, &embed.TransportError{Err: fmt.Errorf("connection refused")}
}

func (stub *transportFailEmbedder) Model() string { return stub.model }
func (stub *transportFailEmbedder) Dim() int      { return stub.dim }

func enqueueSingleChunkNodes(test *testing.T, root string, nodeRepo *index.NodeRepo, queueRepo *index.EmbedQueueRepo, count int) {
	test.Helper()

	for idx := 0; idx < count; idx++ {
		id := fmt.Sprintf("notes/n%d", idx)
		createNodeFile(test, root, id+".md", fmt.Sprintf("body %d", idx))

		if upsertErr := nodeRepo.Upsert(index.NodeRow{ID: id, Type: "note", Path: id + ".md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
			test.Fatalf("upsert %s: %v", id, upsertErr)
		}

		if enqErr := queueRepo.Enqueue(id); enqErr != nil {
			test.Fatalf("enqueue %s: %v", id, enqErr)
		}
	}
}

// TestDrainQueue_CrossNodeConcurrencyRunsNodesInParallel pins B2: with
// EmbedConcurrency > 1, multiple single-chunk nodes embed at once (the per-node
// chunk pool gives no parallelism when each node is one chunk). All nodes are
// still embedded and the queue drains.
func TestDrainQueue_CrossNodeConcurrencyRunsNodesInParallel(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	const nodes = 8

	enqueueSingleChunkNodes(test, root, nodeRepo, queueRepo, nodes)

	probe := &concurrencyProbeEmbedder{dim: 3, model: "stub", hold: 25 * time.Millisecond}

	drained, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:             root,
		Nodes:            nodeRepo,
		Queue:            queueRepo,
		Embeddings:       embeddingRepo,
		Embedder:         probe,
		Chunker:          embed.WholeDocument{},
		EmbedConcurrency: 4,
	})

	if drainErr != nil {
		test.Fatalf("DrainQueue: %v", drainErr)
	}

	if drained != nodes {
		test.Errorf("drained = %d, want %d", drained, nodes)
	}

	if max := probe.maxSeen.Load(); max < 2 {
		test.Errorf("max concurrent embeds = %d, want > 1 (cross-node pool must parallelize)", max)
	}

	if max := probe.maxSeen.Load(); max > 4 {
		test.Errorf("max concurrent embeds = %d, want <= 4 (pool must stay bounded)", max)
	}

	depth, _ := queueRepo.Depth()

	if depth != 0 {
		test.Errorf("queue depth after drain = %d, want 0", depth)
	}
}

// TestDrainQueue_SerialWhenConcurrencyOne confirms the default (EmbedConcurrency
// unset / 1) keeps the original one-node-at-a-time behavior.
func TestDrainQueue_SerialWhenConcurrencyOne(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	enqueueSingleChunkNodes(test, root, nodeRepo, queueRepo, 6)

	probe := &concurrencyProbeEmbedder{dim: 3, model: "stub", hold: 10 * time.Millisecond}

	if _, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepo,
		Queue:      queueRepo,
		Embeddings: embeddingRepo,
		Embedder:   probe,
		Chunker:    embed.WholeDocument{},
		// EmbedConcurrency unset -> serial.
	}); drainErr != nil {
		test.Fatalf("DrainQueue: %v", drainErr)
	}

	if max := probe.maxSeen.Load(); max != 1 {
		test.Errorf("max concurrent embeds = %d, want 1 (default must stay serial)", max)
	}
}

// TestDrainQueue_CrossNodeConcurrencyTransportAbortKeepsQueue confirms a
// transport failure under the concurrent pool still aborts the pass and leaves
// every row queued — no node is dropped or has its retry budget burned by a
// sibling's failure.
func TestDrainQueue_CrossNodeConcurrencyTransportAbortKeepsQueue(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	const nodes = 6

	enqueueSingleChunkNodes(test, root, nodeRepo, queueRepo, nodes)

	failing := &transportFailEmbedder{dim: 3, model: "fake"}

	_, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:             root,
		Nodes:            nodeRepo,
		Queue:            queueRepo,
		Embeddings:       embeddingRepo,
		Embedder:         failing,
		Chunker:          embed.WholeDocument{},
		EmbedConcurrency: 4,
		TTL:              time.Minute,
	})

	if drainErr == nil || !embed.IsTransportError(drainErr) {
		test.Fatalf("DrainQueue err = %v, want a TransportError", drainErr)
	}

	depth, depthErr := queueRepo.Depth()

	if depthErr != nil {
		test.Fatalf("Depth: %v", depthErr)
	}

	if depth != nodes {
		test.Errorf("queue depth after transport abort = %d, want %d (no row dropped)", depth, nodes)
	}
}

// TestDrainQueue_TransportErrorAbortsAndKeepsQueue pins the A2 fix: a transport
// failure (Ollama down / 5xx) must abort the whole drain pass and leave the row
// queued and untouched — NOT burn its retry budget and drop it. Before A2, a
// 1-2s outage would cycle a node through MaxEmbedAttempts in a single pass and
// silently evict it from semantic results.
func TestDrainQueue_TransportErrorAbortsAndKeepsQueue(test *testing.T) {
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

	failing := &drainStubEmbedder{
		dim:     3,
		model:   "fake",
		failure: &embed.TransportError{Err: fmt.Errorf("connection refused")},
	}

	// Short lease so we can re-claim the row below and prove its attempts were
	// not incremented.
	drained, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepo,
		Queue:      queueRepo,
		Embeddings: embeddingRepo,
		Embedder:   failing,
		Chunker:    embed.WholeDocument{},
		TTL:        time.Millisecond,
	})

	if drainErr == nil {
		test.Fatal("transport failure must abort the drain pass with an error")
	}

	if !embed.IsTransportError(drainErr) {
		test.Errorf("DrainQueue error should wrap a TransportError; got %v", drainErr)
	}

	if drained != 0 {
		test.Errorf("drained = %d, want 0", drained)
	}

	if failing.calls != 1 {
		test.Errorf("embedder.calls = %d, want 1 (abort after first failure, not %d retries)", failing.calls, embed.MaxEmbedAttempts)
	}

	depth, depthErr := queueRepo.Depth()

	if depthErr != nil {
		test.Fatalf("Depth: %v", depthErr)
	}

	if depth != 1 {
		test.Errorf("queue depth after transport abort = %d, want 1 (row must NOT be dropped)", depth)
	}

	// Re-claim after the short lease expires and confirm the retry budget was
	// preserved (a Nack would have bumped attempts to 1).
	time.Sleep(5 * time.Millisecond)

	reclaimed, reclaimErr := queueRepo.DrainEmbed("other-worker", 10, time.Minute)

	if reclaimErr != nil {
		test.Fatalf("DrainEmbed: %v", reclaimErr)
	}

	if len(reclaimed) != 1 {
		test.Fatalf("re-claimed rows = %d, want 1 (row should be re-leasable)", len(reclaimed))
	}

	if reclaimed[0].Attempts != 0 {
		test.Errorf("re-claimed attempts = %d, want 0 (transport abort must not burn the retry budget)", reclaimed[0].Attempts)
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
	}}, "markdown"); upsertErr != nil {
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
	}}, "markdown"); upsertErr != nil {
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
	}}, "markdown"); upsertErr != nil {
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

// TestDrainQueue_GCSkippedWhenNothingDrained asserts GCOrphanVectors runs only
// after a pass actually drains work, not on every idle tick. Orphans are
// created by content edits/deletes that flow through the queue, so an idle pass
// has nothing to collect — and a DELETE...WHERE NOT EXISTS scan every ~2s at
// idle is pure waste.
func TestDrainQueue_GCSkippedWhenNothingDrained(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	// An orphan vector: a row in embeddings with no node_embeddings mapping.
	// Insert directly (EmbeddingRepo.Upsert also writes a node-keyed mapping).
	if _, seedErr := store.DB().Exec(
		`INSERT INTO embeddings (content_hash, model, vector, dim) VALUES (?, ?, ?, ?)`,
		"orphan-hash", "stub", []byte{1, 2, 3}, 3,
	); seedErr != nil {
		test.Fatalf("seed orphan vector: %v", seedErr)
	}

	drainCfg := embed.DrainConfig{
		Root: root, Nodes: nodeRepo, Queue: queueRepo, Embeddings: embeddingRepo,
		Embedder: &drainStubEmbedder{dim: 3, model: "stub"}, Chunker: embed.WholeDocument{}, BatchSize: 50,
	}

	drained, drainErr := embed.DrainQueue(context.Background(), drainCfg)

	if drainErr != nil {
		test.Fatalf("idle DrainQueue: %v", drainErr)
	}

	if drained != 0 {
		test.Fatalf("idle drained = %d, want 0", drained)
	}

	exists, _ := embeddingRepo.ExistsByContentHashes([]string{"orphan-hash"}, "stub")

	if !exists["orphan-hash"] {
		test.Errorf("orphan vector was GC'd on an idle pass; GC should be gated on drained > 0")
	}

	// Active pass: draining a real node lets GC run on completion.
	createNodeFile(test, root, "notes/a.md", "hi")
	nodeRepo.Upsert(index.NodeRow{ID: "notes/a", Type: "note", Path: "notes/a.md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"})
	queueRepo.Enqueue("notes/a")

	activeDrained, activeErr := embed.DrainQueue(context.Background(), drainCfg)

	if activeErr != nil {
		test.Fatalf("active DrainQueue: %v", activeErr)
	}

	if activeDrained == 0 {
		test.Fatalf("active drained = 0, want > 0")
	}

	existsAfter, _ := embeddingRepo.ExistsByContentHashes([]string{"orphan-hash"}, "stub")

	if existsAfter["orphan-hash"] {
		test.Errorf("orphan vector should be collected after an active drain")
	}
}

// TestDrainQueue_ReclaimsCrashedLeaseExactlyOnce models a worker that claimed a
// row, wrote a partial (now stale) embedding, and crashed without acking —
// leaving an expired lease behind. A fresh DrainQueue must reclaim the
// expired-lease row, reconcile the stale vectors via delete-before-insert, and
// leave exactly ONE node_embeddings row (no duplicate from the crashed pass).
// The crashed pass wrote a 2-chunk embedding; the re-embed of the tiny body
// produces a single chunk, so the trailing stale chunk (idx 1) is precisely
// what delete-before-insert must reclaim.
//
// Red→green: temporarily removing the `DeleteByNodeID` call in drain.go makes
// the "exactly one node_embeddings row" assertion fail — the trailing stale
// chunk survives the re-embed and the node ends with two mappings.
func TestDrainQueue_ReclaimsCrashedLeaseExactlyOnce(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	createNodeFile(test, root, "notes/a.md", "tiny body")

	if upsertErr := nodeRepo.Upsert(index.NodeRow{ID: "notes/a", Type: "note", Path: "notes/a.md", Title: "x", PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("upsert node: %v", upsertErr)
	}

	if enqErr := queueRepo.Enqueue("notes/a"); enqErr != nil {
		test.Fatalf("enqueue: %v", enqErr)
	}

	// Simulate the crashed worker: claim the row with an already-expired lease
	// (negative TTL) and never ack it.
	crashed, claimErr := queueRepo.DrainEmbed("crashed-daemon", 10, -time.Second)

	if claimErr != nil {
		test.Fatalf("crashed claim: %v", claimErr)
	}

	if len(crashed) != 1 {
		test.Fatalf("crashed claim = %d rows, want 1", len(crashed))
	}

	// The crashed pass left two stale chunk mappings (idx 0 and 1) pointing at a
	// STALE content hash.
	for chunkIdx := 0; chunkIdx < 2; chunkIdx++ {
		if seedErr := embeddingRepo.Upsert(index.EmbeddingRow{
			NodeID:      "notes/a",
			ChunkIdx:    chunkIdx,
			Model:       "stub",
			ContentHash: "STALE",
			Vector:      []float32{0.9, 0.9, 0.9},
			Dim:         3,
		}); seedErr != nil {
			test.Fatalf("seed stale chunk %d: %v", chunkIdx, seedErr)
		}
	}

	drained, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepo,
		Queue:      queueRepo,
		Embeddings: embeddingRepo,
		Embedder:   &drainStubEmbedder{dim: 3, model: "stub"},
		Chunker:    embed.WholeDocument{},
		TTL:        time.Minute,
	})

	if drainErr != nil {
		test.Fatalf("DrainQueue: %v", drainErr)
	}

	if drained != 1 {
		test.Errorf("drained = %d, want 1 (expired lease reclaimed)", drained)
	}

	depth, depthErr := queueRepo.Depth()

	if depthErr != nil {
		test.Fatalf("Depth: %v", depthErr)
	}

	if depth != 0 {
		test.Errorf("queue depth after reclaim = %d, want 0 (row acked)", depth)
	}

	var mappingCount int

	if scanErr := store.DB().QueryRow(
		`SELECT COUNT(*) FROM node_embeddings WHERE node_id = ?`,
		"notes/a",
	).Scan(&mappingCount); scanErr != nil {
		test.Fatalf("count node_embeddings: %v", scanErr)
	}

	if mappingCount != 1 {
		test.Errorf("node_embeddings rows for notes/a = %d, want 1 (stale reclaimed, no duplicate)", mappingCount)
	}

	rows, getErr := embeddingRepo.GetByNodeID("notes/a")

	if getErr != nil {
		test.Fatalf("GetByNodeID: %v", getErr)
	}

	if len(rows) != 1 {
		test.Fatalf("resolved embeddings for notes/a = %d, want 1", len(rows))
	}

	if rows[0].ContentHash == "STALE" {
		test.Errorf("stale vector survived the reclaim: %+v", rows[0])
	}
}

// TestDrainQueue_CtxCancelMidEmbedReclaimsThenDrainsOnce cancels a drain while a
// lease is in-flight (the embedder is blocked mid-Embed). The cancelled pass
// must nack the row — clearing the lease and bumping attempts — WITHOUT writing
// a vector; a fresh pass then reclaims the row and produces exactly one vector.
// This proves a mid-flight crash leaves no partial state and no duplicate. The
// gate channel (not time.Sleep) makes the ordering deterministic.
func TestDrainQueue_CtxCancelMidEmbedReclaimsThenDrainsOnce(test *testing.T) {
	root := test.TempDir()
	store := openIndex(test, root)
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	createNodeFile(test, root, "notes/a.md", "tiny body")

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

	ctx, cancel := context.WithCancel(context.Background())

	type drainOutcome struct {
		drained int
		err     error
	}

	done := make(chan drainOutcome, 1)

	go func() {
		drained, drainErr := embed.DrainQueue(ctx, embed.DrainConfig{
			Root:       root,
			Nodes:      nodeRepo,
			Queue:      queueRepo,
			Embeddings: embeddingRepo,
			Embedder:   gate,
			Chunker:    embed.WholeDocument{},
			TTL:        time.Minute,
		})
		done <- drainOutcome{drained: drained, err: drainErr}
	}()

	select {
	case <-gate.started:
	case <-time.After(2 * time.Second):
		cancel()
		<-done
		test.Fatal("embedder never started — DrainQueue did not lease the row")
	}

	cancel()

	outcome := <-done

	if outcome.err != nil {
		test.Fatalf("cancelled DrainQueue err = %v, want nil (cancellation is graceful)", outcome.err)
	}

	if outcome.drained != 0 {
		test.Errorf("drained on cancel = %d, want 0 (no vector written)", outcome.drained)
	}

	// The cancelled pass nacked the row: it is back in the queue, and nothing
	// was written to the embeddings store.
	depth, _ := queueRepo.Depth()

	if depth != 1 {
		test.Errorf("queue depth after cancel = %d, want 1 (row re-enqueued)", depth)
	}

	rowsAfterCancel, _ := embeddingRepo.GetByNodeID("notes/a")

	if len(rowsAfterCancel) != 0 {
		test.Errorf("embeddings after cancel = %d, want 0 (nothing written mid-flight)", len(rowsAfterCancel))
	}

	// A fresh pass with a working embedder reclaims the row and writes exactly
	// one vector.
	drained, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
		Root:       root,
		Nodes:      nodeRepo,
		Queue:      queueRepo,
		Embeddings: embeddingRepo,
		Embedder:   &drainStubEmbedder{dim: 3, model: "stub"},
		Chunker:    embed.WholeDocument{},
		TTL:        time.Minute,
	})

	if drainErr != nil {
		test.Fatalf("fresh DrainQueue: %v", drainErr)
	}

	if drained != 1 {
		test.Errorf("fresh drained = %d, want 1", drained)
	}

	rows, _ := embeddingRepo.GetByNodeID("notes/a")

	if len(rows) != 1 {
		test.Errorf("embeddings after reclaim = %d, want 1", len(rows))
	}
}

// TestDrainQueue_TwoDaemonsOneDBExactlyOnce models two DISTINCT daemons (set via
// DrainConfig.WorkerID) draining ONE database file through two handles. Without
// the WorkerID override they would share index.WorkerID()'s single per-process
// UUID and never contend; with distinct ids the lease claim partitions the work.
// Every one of the 60 nodes must be embedded exactly once (no loss, no
// duplicate), the queue must fully drain, and busy_timeout must absorb the
// cross-handle write contention (no "database is locked").
func TestDrainQueue_TwoDaemonsOneDBExactlyOnce(test *testing.T) {
	root := test.TempDir()
	dbPath := filepath.Join(root, "index.db")

	storeA, openAErr := index.Open(dbPath)

	if openAErr != nil {
		test.Fatalf("open A: %v", openAErr)
	}

	defer storeA.Close()

	storeB, openBErr := index.Open(dbPath)

	if openBErr != nil {
		test.Fatalf("open B: %v", openBErr)
	}

	defer storeB.Close()

	nodeRepoA := index.NewNodeRepo(storeA)
	queueRepoA := index.NewEmbedQueueRepo(storeA)
	embeddingRepoA := index.NewEmbeddingRepo(storeA)

	nodeRepoB := index.NewNodeRepo(storeB)
	queueRepoB := index.NewEmbedQueueRepo(storeB)
	embeddingRepoB := index.NewEmbeddingRepo(storeB)

	const nodes = 60

	// Seed all 60 distinct single-chunk nodes through storeA.
	enqueueSingleChunkNodes(test, root, nodeRepoA, queueRepoA, nodes)

	// One probe embedder shared by both daemons; its counters are atomic so the
	// -race detector stays quiet across the two drainers.
	probe := &concurrencyProbeEmbedder{dim: 3, model: "stub", hold: time.Millisecond}

	type drainOutcome struct {
		drained int
		err     error
	}

	results := make(chan drainOutcome, 2)

	var daemons sync.WaitGroup

	daemons.Add(2)

	go func() {
		defer daemons.Done()

		drained, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
			Root:             root,
			Nodes:            nodeRepoA,
			Queue:            queueRepoA,
			Embeddings:       embeddingRepoA,
			Embedder:         probe,
			Chunker:          embed.WholeDocument{},
			EmbedConcurrency: 4,
			TTL:              time.Minute,
			WorkerID:         "daemon-a",
		})
		results <- drainOutcome{drained: drained, err: drainErr}
	}()

	go func() {
		defer daemons.Done()

		drained, drainErr := embed.DrainQueue(context.Background(), embed.DrainConfig{
			Root:             root,
			Nodes:            nodeRepoB,
			Queue:            queueRepoB,
			Embeddings:       embeddingRepoB,
			Embedder:         probe,
			Chunker:          embed.WholeDocument{},
			EmbedConcurrency: 4,
			TTL:              time.Minute,
			WorkerID:         "daemon-b",
		})
		results <- drainOutcome{drained: drained, err: drainErr}
	}()

	daemons.Wait()
	close(results)

	totalDrained := 0

	for outcome := range results {
		if outcome.err != nil {
			test.Fatalf("daemon DrainQueue err = %v, want nil (busy_timeout must absorb contention)", outcome.err)
		}

		totalDrained += outcome.drained
	}

	if totalDrained != nodes {
		test.Errorf("totalDrained = %d, want %d (no node lost or double-counted)", totalDrained, nodes)
	}

	depth, depthErr := queueRepoA.Depth()

	if depthErr != nil {
		test.Fatalf("Depth: %v", depthErr)
	}

	if depth != 0 {
		test.Errorf("queue depth after both daemons drained = %d, want 0", depth)
	}

	if calls := probe.calls.Load(); calls != nodes {
		test.Errorf("embedder calls = %d, want %d (each unique content embedded exactly once)", calls, nodes)
	}

	for idx := 0; idx < nodes; idx++ {
		nodeID := fmt.Sprintf("notes/n%d", idx)

		rows, getErr := embeddingRepoA.GetByNodeID(nodeID)

		if getErr != nil {
			test.Fatalf("GetByNodeID %s: %v", nodeID, getErr)
		}

		if len(rows) != 1 {
			test.Errorf("embeddings for %s = %d, want 1", nodeID, len(rows))
		}
	}
}
