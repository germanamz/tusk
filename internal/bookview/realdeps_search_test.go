package bookview

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

// stubEmbedder returns a fixed query vector for ranking tests. Verbatim
// pattern from internal/query/query_service_test.go:18-27; internal/query's
// helpers live in package query_test and cannot be imported from here.
type stubEmbedder struct {
	vector []float32
}

func (stub stubEmbedder) Embed(_ context.Context, _ []byte) ([]float32, error) {
	return stub.vector, nil
}

func (stub stubEmbedder) Model() string { return "stub" }
func (stub stubEmbedder) Dim() int      { return len(stub.vector) }

// openTestStore opens a fresh index db for a test. Verbatim pattern from
// internal/query/query_service_test.go:30-42.
func openTestStore(test *testing.T) *index.Index {
	test.Helper()

	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	return store
}

// seedNoteWithVector upserts a "note" file node plus a chunk-0 embedding under
// the "stub" model, so cosine similarity against a query vector is
// predictable. Model MUST be "stub" (matching stubEmbedder.Model()) or the
// query layer's model filter silently drops the candidate (#684).
func seedNoteWithVector(test *testing.T, nodes *index.NodeRepo, embeddings *index.EmbeddingRepo, id string, vector []float32) {
	test.Helper()

	if upsertErr := nodes.Upsert(index.NodeRow{
		ID:             id,
		Type:           "note",
		Path:           id + ".md",
		Title:          id,
		PropertiesJSON: "{}",
		LastChecksum:   "x",
	}); upsertErr != nil {
		test.Fatalf("upsert node %s: %v", id, upsertErr)
	}

	if embedErr := embeddings.Upsert(index.EmbeddingRow{
		NodeID:      id,
		ChunkIdx:    0,
		Model:       "stub",
		ContentHash: "h_" + id,
		Vector:      vector,
		Dim:         len(vector),
		Body:        id + " body",
	}); embedErr != nil {
		test.Fatalf("upsert embedding %s: %v", id, embedErr)
	}
}

// newTestManifest returns a hand-built manifest with the builtin packs merged
// in (so "type=note" is a valid filter) and an explicit GraphExpansion —
// required because a hand-built &manifest.Manifest{} otherwise carries the
// GraphExpansion zero value (Hops 0, CandidateMultiplier 0), which makes the
// walker hard-error rather than resolving to the loader's usual defaults.
func newTestManifest(graphExpansion manifest.GraphExpansion) *manifest.Manifest {
	loaded := &manifest.Manifest{}
	manifest.MergeBuiltinPacks(loaded)
	loaded.GraphExpansion = graphExpansion

	return loaded
}

// TestSearcher_SemanticExpandExplain_PopulatesScores covers the brief's Step 1
// semantic + Expand + Explain case: "seed" is aligned with the query vector
// and "neighbor" is linked to it via a "references" edge but has an
// orthogonal (cosine 0) embedding, so neighbor's FinalScore/GraphScore can
// only come from the graph-expansion blend (the mean cosine of its
// seed-neighbors), never from cosine alone.
func TestSearcher_SemanticExpandExplain_PopulatesScores(test *testing.T) {
	store := openTestStore(test)

	nodes := index.NewNodeRepo(store)
	embeddings := index.NewEmbeddingRepo(store)
	edges := index.NewEdgeRepo(store)

	seedNoteWithVector(test, nodes, embeddings, "seed", []float32{1, 0})
	seedNoteWithVector(test, nodes, embeddings, "neighbor", []float32{0, 1})

	if edgeErr := edges.UpsertAll("seed", "seed.md", []index.EdgeRow{
		{Type: "references", SourceID: "seed", TargetID: "neighbor", SourcePath: "seed.md", Kind: "direct"},
	}); edgeErr != nil {
		test.Fatalf("edge upsert: %v", edgeErr)
	}

	loaded := newTestManifest(manifest.GraphExpansion{
		Enabled: false, Hops: 1, EdgeTypes: []string{"references"},
		Weight: 0.4, CandidateMultiplier: 5,
	})

	svc := NewSearcher(store.DB(), loaded, stubEmbedder{vector: []float32{1, 0}}, embeddings, nodes, edges, test.TempDir())

	resp, searchErr := svc.Search(context.Background(), SearchRequest{
		Q: "anything", Filter: "type=note", Expand: true, Hops: 1, Explain: true, Limit: 10,
	})

	if searchErr != nil {
		test.Fatalf("Search: %v", searchErr)
	}

	var neighborMatch *Match

	for idx := range resp.Matches {
		if resp.Matches[idx].ID == "neighbor" {
			neighborMatch = &resp.Matches[idx]
		}
	}

	if neighborMatch == nil {
		test.Fatalf("neighbor missing from matches: %+v", resp.Matches)
	}

	if neighborMatch.FinalScore == 0 {
		test.Errorf("neighbor FinalScore = 0, want > 0 (graph-expansion blend)")
	}

	if neighborMatch.GraphScore == 0 {
		test.Errorf("neighbor GraphScore = 0, want > 0 (graph-expansion blend)")
	}
}

// TestSearcher_StructuralOnly_ScoreIsOne covers the brief's structural-only
// case: no Q, a Filter, every returned match's Score is exactly 1 (the
// structural path carries no ranking signal).
func TestSearcher_StructuralOnly_ScoreIsOne(test *testing.T) {
	store := openTestStore(test)

	nodes := index.NewNodeRepo(store)
	embeddings := index.NewEmbeddingRepo(store)
	edges := index.NewEdgeRepo(store)

	for _, id := range []string{"alpha", "beta"} {
		if upsertErr := nodes.Upsert(index.NodeRow{
			ID: id, Type: "note", Path: id + ".md", Title: id,
			PropertiesJSON: "{}", LastChecksum: "x",
		}); upsertErr != nil {
			test.Fatalf("upsert %s: %v", id, upsertErr)
		}
	}

	loaded := newTestManifest(manifest.DefaultGraphExpansion())

	svc := NewSearcher(store.DB(), loaded, stubEmbedder{vector: []float32{1, 0}}, embeddings, nodes, edges, test.TempDir())

	resp, searchErr := svc.Search(context.Background(), SearchRequest{Filter: "type=note", Limit: 10})

	if searchErr != nil {
		test.Fatalf("Search: %v", searchErr)
	}

	if len(resp.Matches) != 2 {
		test.Fatalf("matches=%d want 2: %+v", len(resp.Matches), resp.Matches)
	}

	for _, match := range resp.Matches {
		if match.Score != 1 {
			test.Errorf("match %q Score=%v want 1", match.ID, match.Score)
		}
	}
}

// TestSearcher_AbsentWeight_UsesManifestDefault pins the presence rule that is
// this task's whole point: a request that omits Weight (arrives as the Go
// zero value, 0, indistinguishable on the wire from an explicit weight=0)
// must inherit the manifest's configured weight, not silently override it
// with 0.
//
// The fixture is built so the pinned quantity is exact, not just "non-zero":
// with only two candidate nodes, both fall within the seed limit and "neighbor"
// scores at graph-expansion distance <= 1 relative to "seed", with cosine 0.
// At distance <= 1, GraphScore is the mean seed-neighbor cosine (1.0 here,
// UNDECAYED by weight — blend.go only applies Weight at distance >= 2), so
// FinalScore = (1-W)*0 + W*1 = W. That makes neighbor's FinalScore read back
// the effective weight directly: with the request's Weight left at 0 (unset),
// FinalScore must equal the manifest's 0.37, not 0.
//
// Verification that this fails on `&0`: temporarily changing
// graphExpansionOverridesFromSearch's Weight guard from `req.Weight != 0` to
// always taking the branch (i.e. always setting `weight := req.Weight;
// over.Weight = &weight`) reproduces exactly the bug this test pins — a
// request with Weight unset (0) would then force override.Weight = &0.0,
// MergeGraphExpansion would set resolved.Weight = 0, and neighbor's
// FinalScore would compute to 0 instead of 0.37, failing the assertion below.
func TestSearcher_AbsentWeight_UsesManifestDefault(test *testing.T) {
	store := openTestStore(test)

	nodes := index.NewNodeRepo(store)
	embeddings := index.NewEmbeddingRepo(store)
	edges := index.NewEdgeRepo(store)

	seedNoteWithVector(test, nodes, embeddings, "seed", []float32{1, 0})
	seedNoteWithVector(test, nodes, embeddings, "neighbor", []float32{0, 1})

	if edgeErr := edges.UpsertAll("seed", "seed.md", []index.EdgeRow{
		{Type: "references", SourceID: "seed", TargetID: "neighbor", SourcePath: "seed.md", Kind: "direct"},
	}); edgeErr != nil {
		test.Fatalf("edge upsert: %v", edgeErr)
	}

	const manifestWeight = 0.37 // distinct from 0 and from any hard-coded default (0.2)

	loaded := newTestManifest(manifest.GraphExpansion{
		Enabled: false, Hops: 1, EdgeTypes: []string{"references"},
		Weight: manifestWeight, CandidateMultiplier: 5,
	})

	svc := NewSearcher(store.DB(), loaded, stubEmbedder{vector: []float32{1, 0}}, embeddings, nodes, edges, test.TempDir())

	// Weight is deliberately left unset (Go zero value 0) on the request —
	// SearchRequest has no way to express "unset" on the wire.
	resp, searchErr := svc.Search(context.Background(), SearchRequest{
		Q: "anything", Filter: "type=note", Expand: true, Hops: 1, Explain: true, Limit: 10,
	})

	if searchErr != nil {
		test.Fatalf("Search: %v", searchErr)
	}

	var neighborMatch *Match

	for idx := range resp.Matches {
		if resp.Matches[idx].ID == "neighbor" {
			neighborMatch = &resp.Matches[idx]
		}
	}

	if neighborMatch == nil {
		test.Fatalf("neighbor missing from matches: %+v", resp.Matches)
	}

	const epsilon = 1e-9

	diff := neighborMatch.FinalScore - manifestWeight

	if diff < 0 {
		diff = -diff
	}

	if diff > epsilon {
		test.Errorf("neighbor FinalScore = %v, want manifest weight %v (adapter must not default absent Weight to 0)", neighborMatch.FinalScore, manifestWeight)
	}

	if neighborMatch.FinalScore == 0 {
		test.Fatalf("neighbor FinalScore == 0: presence rule failed — absent Weight overrode the manifest default with 0")
	}
}

// TestSearcher_NilMatches_MarshalsEmptyArray guards the nil-slice trap at the
// wire-byte level: a zero-result search must produce a non-nil Matches slice
// that marshals "matches" as [], never null.
func TestSearcher_NilMatches_MarshalsEmptyArray(test *testing.T) {
	store := openTestStore(test)

	nodes := index.NewNodeRepo(store)
	embeddings := index.NewEmbeddingRepo(store)
	edges := index.NewEdgeRepo(store)

	loaded := newTestManifest(manifest.DefaultGraphExpansion())

	svc := NewSearcher(store.DB(), loaded, stubEmbedder{vector: []float32{1, 0}}, embeddings, nodes, edges, test.TempDir())

	resp, searchErr := svc.Search(context.Background(), SearchRequest{Filter: "type=note", Limit: 10})

	if searchErr != nil {
		test.Fatalf("Search: %v", searchErr)
	}

	if resp.Matches == nil {
		test.Fatalf("Matches is nil, want a non-nil empty slice")
	}

	if len(resp.Matches) != 0 {
		test.Fatalf("Matches=%+v want empty (no nodes indexed)", resp.Matches)
	}

	encoded, marshalErr := json.Marshal(resp)

	if marshalErr != nil {
		test.Fatalf("marshal: %v", marshalErr)
	}

	if !strings.Contains(string(encoded), `"matches":[]`) {
		test.Fatalf("json=%s want matches serialized as []", encoded)
	}
}
