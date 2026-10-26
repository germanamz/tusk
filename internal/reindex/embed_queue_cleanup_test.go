package reindex_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/reindex"
)

// queuedEmbedIDs lists the embed jobs in store, parked or not, sorted.
func queuedEmbedIDs(test *testing.T, store *index.Index) []string {
	test.Helper()

	rows, queryErr := store.DB().Query(`SELECT node_id FROM embed_queue WHERE kind = 'embed' ORDER BY node_id`)

	if queryErr != nil {
		test.Fatalf("list embed jobs: %v", queryErr)
	}

	defer rows.Close()

	var ids []string

	for rows.Next() {
		var id string

		if scanErr := rows.Scan(&id); scanErr != nil {
			test.Fatalf("scan: %v", scanErr)
		}

		ids = append(ids, id)
	}

	return ids
}

// TestRun_RemovedFileTakesItsEmbedJobs is #845 finding 12: with no embedder
// to drain them, a deleted file's embed jobs (its own and its sub-units')
// used to outlive its node and keep counting as pending.
func TestRun_RemovedFileTakesItsEmbedJobs(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "keep.md", "type: note\ntitle: Keep\n", "Kept.\n")
	writeNode(test, root, "gone.md", "type: note\ntitle: Gone\n", "Doomed.\n")

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	loaded := subUnitsManifest(test, true)

	runSubUnits(test, root, store, loaded, false)

	if want := []string{"gone", "gone#P1", "keep", "keep#P1"}; !reflect.DeepEqual(queuedEmbedIDs(test, store), want) {
		test.Fatalf("setup: embed jobs = %v, want %v", queuedEmbedIDs(test, store), want)
	}

	if removeErr := os.Remove(filepath.Join(root, "gone.md")); removeErr != nil {
		test.Fatalf("remove: %v", removeErr)
	}

	if report := runSubUnits(test, root, store, loaded, true); report.Removed != 1 {
		test.Fatalf("Removed = %d, want 1", report.Removed)
	}

	if want := []string{"keep", "keep#P1"}; !reflect.DeepEqual(queuedEmbedIDs(test, store), want) {
		test.Errorf("embed jobs after the reap = %v, want %v", queuedEmbedIDs(test, store), want)
	}
}

// TestRun_SweepsLeakedEmbedJobs: an index written before node deletes took
// their embed jobs with them can hold jobs for nodes that are gone; a reindex
// pass sweeps them.
func TestRun_SweepsLeakedEmbedJobs(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "keep.md", "type: note\ntitle: Keep\n", "Kept.\n")

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	queue := index.NewEmbedQueueRepo(store)

	if enqErr := queue.EnqueueMany([]string{"leaked", "leaked#P1"}); enqErr != nil {
		test.Fatalf("EnqueueMany: %v", enqErr)
	}

	if _, runErr := reindex.Run(withGen(store, reindex.Config{Root: root, Repo: index.NewNodeRepo(store), EmbedQueue: queue})); runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if want := []string{"keep"}; !reflect.DeepEqual(queuedEmbedIDs(test, store), want) {
		test.Errorf("embed jobs = %v, want %v", queuedEmbedIDs(test, store), want)
	}
}

// refusingEmbedder refuses every input that contains marker, the way a model
// refuses one too long for its context, and embeds everything else (the
// drain's probe included).
type refusingEmbedder struct{ marker string }

func (stub refusingEmbedder) Embed(_ context.Context, payload []byte) ([]float32, error) {
	if strings.Contains(string(payload), stub.marker) {
		return nil, errors.New(`ollama: HTTP 400: {"error":"the input length exceeds the context length"}`)
	}

	return []float32{0.1, 0.2, 0.3}, nil
}

func (refusingEmbedder) Model() string        { return "test" }
func (refusingEmbedder) VectorKey() string    { return "test" }
func (refusingEmbedder) Format() embed.Format { return embed.Format{} }
func (refusingEmbedder) Dim() int             { return 3 }

// TestRun_ReportsFailedEmbeds: a node the model keeps refusing fails alone —
// the pass is not deferred, its neighbour embeds — and the report counts it
// as failed so the CLI can point at doctor (#845).
func TestRun_ReportsFailedEmbeds(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "big.md", "type: note\ntitle: Big\n", "OVERSIZE\n")
	writeNode(test, root, "small.md", "type: note\ntitle: Small\n", "Fine.\n")

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	embeddings := index.NewEmbeddingRepo(store)

	report, runErr := reindex.Run(withGen(store, reindex.Config{
		Root:          root,
		Repo:          index.NewNodeRepo(store),
		EmbeddingRepo: embeddings,
		Embedder:      refusingEmbedder{marker: "OVERSIZE"},
		Chunker:       embed.WholeDocument{},
	}))

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.EmbedDeferred != nil {
		test.Errorf("EmbedDeferred = %v, want nil (one refused input is not a backend outage)", report.EmbedDeferred)
	}

	if report.EmbedFailed != 1 {
		test.Errorf("EmbedFailed = %d, want 1", report.EmbedFailed)
	}

	if rows, _ := embeddings.GetByNodeID("small"); len(rows) != 1 {
		test.Errorf("small has %d vectors, want 1", len(rows))
	}
}
