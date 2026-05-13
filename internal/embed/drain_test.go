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
