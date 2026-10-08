package query_test

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/query"
)

// keyedEmbedder is a query-side double whose vector key and format differ
// from the defaults, and which records the text each Embed call receives.
type keyedEmbedder struct {
	vector    []float32
	vectorKey string
	format    embed.Format

	mu       sync.Mutex
	received []string
}

func (stub *keyedEmbedder) Embed(_ context.Context, text []byte) ([]float32, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()

	stub.received = append(stub.received, string(text))

	return stub.vector, nil
}

func (stub *keyedEmbedder) Model() string        { return "stub" }
func (stub *keyedEmbedder) VectorKey() string    { return stub.vectorKey }
func (stub *keyedEmbedder) Format() embed.Format { return stub.format }
func (stub *keyedEmbedder) Dim() int             { return len(stub.vector) }

// seedLeafFile indexes a note with one embedded paragraph leaf whose vector
// matches the query vector {1,0,0} exactly, stored under vectorKey.
func seedLeafFile(test *testing.T, nodes *index.NodeRepo, embeddings *index.EmbeddingRepo, fileID, vectorKey string) {
	test.Helper()

	if upsertErr := nodes.Upsert(index.NodeRow{ID: fileID, Type: "note", Path: fileID + ".md", Title: fileID, PropertiesJSON: "{}", LastChecksum: "x"}); upsertErr != nil {
		test.Fatalf("%s upsert: %v", fileID, upsertErr)
	}

	if bulkErr := nodes.BulkUpsert([]index.NodeRow{
		{ID: fileID + "#s", Type: "section", Path: fileID + ".md", PropertiesJSON: `{"heading-level":1}`, LastChecksum: "x", ParentID: sql.NullString{String: fileID, Valid: true}, Ordinal: sql.NullInt64{Int64: 0, Valid: true}, EmbedPayload: sql.NullString{String: fileID + " heading", Valid: true}},
		{ID: fileID + "#s_p", Type: "paragraph", Path: fileID + ".md", PropertiesJSON: "{}", LastChecksum: "x", ParentID: sql.NullString{String: fileID + "#s", Valid: true}, Ordinal: sql.NullInt64{Int64: 1, Valid: true}, EmbedPayload: sql.NullString{String: fileID + " body", Valid: true}},
	}, "markdown"); bulkErr != nil {
		test.Fatalf("%s sub-units: %v", fileID, bulkErr)
	}

	if embedErr := embeddings.Upsert(index.EmbeddingRow{NodeID: fileID + "#s_p", ChunkIdx: 0, Model: vectorKey, ContentHash: "h_" + fileID, Vector: []float32{1, 0, 0}, Dim: 3, Body: fileID + " body"}); embedErr != nil {
		test.Fatalf("%s leaf embed: %v", fileID, embedErr)
	}
}

func runSemantic(test *testing.T, store *index.Index, nodes *index.NodeRepo, embeddings *index.EmbeddingRepo, embedder embed.Embedder) *query.SemanticResult {
	test.Helper()

	result, runErr := query.Run(context.Background(), query.Deps{
		Database:   store.DB(),
		Manifest:   loadManifestWithSubUnits(test),
		Nodes:      nodes,
		Embedder:   embedder,
		Embeddings: embeddings,
	}, query.Request{Filter: "type=note", Semantic: "graph expansion", MinScore: 0.1})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if result.Semantic == nil {
		test.Fatalf("expected a Semantic result")
	}

	return result.Semantic
}

func TestQueryRun_SemanticQueryCarriesQueryPrefix(test *testing.T) {
	store := openTestStore(test)
	nodes := index.NewNodeRepo(store)
	embeddings := index.NewEmbeddingRepo(store)

	seedLeafFile(test, nodes, embeddings, "live", "stub")

	embedder := &keyedEmbedder{
		vector:    []float32{1, 0, 0},
		vectorKey: "stub",
		format:    embed.Format{QueryPrefix: "search_query: ", DocumentPrefix: "search_document: "},
	}

	runSemantic(test, store, nodes, embeddings, embedder)

	if len(embedder.received) != 1 || embedder.received[0] != "search_query: graph expansion" {
		test.Errorf("embedder received %q, want [\"search_query: graph expansion\"]", embedder.received)
	}
}

// TestQueryRun_SemanticFiltersOnVectorKey: after a num-ctx change, vectors
// stored under the bare model name are stale and must not rank until the
// drain re-embeds them, while the reported model stays the display name.
func TestQueryRun_SemanticFiltersOnVectorKey(test *testing.T) {
	store := openTestStore(test)
	nodes := index.NewNodeRepo(store)
	embeddings := index.NewEmbeddingRepo(store)

	seedLeafFile(test, nodes, embeddings, "live", "stub#num-ctx=512")
	seedLeafFile(test, nodes, embeddings, "stale", "stub")

	semantic := runSemantic(test, store, nodes, embeddings, &keyedEmbedder{vector: []float32{1, 0, 0}, vectorKey: "stub#num-ctx=512"})

	ranked := map[string]bool{}

	for _, row := range semantic.Ranked {
		ranked[row.ID] = true
	}

	if !ranked["live"] {
		test.Errorf("vectors under the configured vector key must rank; got %v", semantic.Ranked)
	}

	if ranked["stale"] {
		test.Errorf("vectors under another vector key must not rank; got %v", semantic.Ranked)
	}

	if semantic.Model != "stub" {
		test.Errorf("Semantic.Model = %q, want the display model", semantic.Model)
	}
}
