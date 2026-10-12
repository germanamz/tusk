package reindex_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/reindex"
)

// unreachableEmbedder fails every call the way the Ollama client does when the
// backend refuses the connection.
type unreachableEmbedder struct{}

func (unreachableEmbedder) Embed(context.Context, []byte) ([]float32, error) {
	return nil, &embed.TransportError{Err: errors.New("dial tcp 127.0.0.1:1: connect: connection refused")}
}

func (unreachableEmbedder) Model() string        { return "test" }
func (unreachableEmbedder) VectorKey() string    { return "test" }
func (unreachableEmbedder) Format() embed.Format { return embed.Format{} }
func (unreachableEmbedder) Dim() int             { return 3 }

// An unreachable embedder must not fail the pass: the structural index is
// built, the error is reported on EmbedDeferred, the queued nodes stay queued,
// and the meta markers are stamped so the next pass stays incremental (#787).
func TestRun_UnreachableEmbedderDefersEmbedding(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "a.md", "type: note\ntitle: A\n", "Body.\n")
	writeNode(test, root, "b.md", "type: note\ntitle: B\n", "Body.\n")

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	queueRepo := index.NewEmbedQueueRepo(store)
	metaRepo := index.NewMetaRepo(store)

	report, runErr := reindex.Run(withGen(store, reindex.Config{
		Root:          root,
		Repo:          nodeRepo,
		Edges:         index.NewEdgeRepo(store),
		EdgeTypes:     manifest.EdgeTypes{},
		EmbedQueue:    queueRepo,
		EmbeddingRepo: index.NewEmbeddingRepo(store),
		Embedder:      unreachableEmbedder{},
		Chunker:       embed.WholeDocument{},
		Meta:          metaRepo,
	}))

	if runErr != nil {
		test.Fatalf("Run: %v, want nil (an unreachable embedder defers embedding)", runErr)
	}

	if !embed.IsTransportError(report.EmbedDeferred) {
		test.Errorf("EmbedDeferred = %v, want the transport error", report.EmbedDeferred)
	}

	if report.Indexed != 2 {
		test.Errorf("Indexed = %d, want 2", report.Indexed)
	}

	if _, getErr := nodeRepo.Get("a"); getErr != nil {
		test.Errorf("node a must be indexed: %v", getErr)
	}

	if depth, _ := queueRepo.DepthByKind("embed"); depth != 2 {
		test.Errorf("embed queue depth = %d, want 2 (deferred, not dropped)", depth)
	}

	if stamped, _ := metaRepo.Get(index.LastReindexAtKey); stamped == "" {
		test.Errorf("%s not stamped after a deferred embed", index.LastReindexAtKey)
	}

	// The markers are stamped, so a second pass over unchanged files skips
	// them instead of forcing a full re-process.
	second, secondErr := reindex.Run(withGen(store, reindex.Config{
		Root:       root,
		Repo:       nodeRepo,
		EmbedQueue: queueRepo,
		Meta:       metaRepo,
	}))

	if secondErr != nil {
		test.Fatalf("second Run: %v", secondErr)
	}

	if second.Indexed != 0 {
		test.Errorf("second pass Indexed = %d, want 0 (incremental skip)", second.Indexed)
	}
}

// Without an embedder there is nothing to defer.
func TestRun_NoEmbedderLeavesEmbedDeferredNil(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "a.md", "type: note\ntitle: A\n", "Body.\n")

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	report, runErr := reindex.Run(withGen(store, reindex.Config{
		Root: root,
		Repo: index.NewNodeRepo(store),
	}))

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.EmbedDeferred != nil {
		test.Errorf("EmbedDeferred = %v, want nil", report.EmbedDeferred)
	}
}
