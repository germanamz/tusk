package graphview

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

// fakeEmbeddingSource implements EmbeddingSource for tests.
type fakeEmbeddingSource struct {
	rows []index.EmbeddingRow
}

func (fake *fakeEmbeddingSource) ListByNodeIDs(_ []string) ([]index.EmbeddingRow, error) {
	return fake.rows, nil
}

// embRow is a convenience constructor for EmbeddingRow values used in tests.
func embRow(nodeID string, chunkIdx int, contentHash string, vector []float32) index.EmbeddingRow {
	return index.EmbeddingRow{
		NodeID:      nodeID,
		ChunkIdx:    chunkIdx,
		Model:       "nomic-embed-text",
		ContentHash: contentHash,
		Vector:      vector,
		Dim:         len(vector),
	}
}

// approxEqual reports whether left and right differ by at most tol in every element.
func approxEqual(left, right []float32, tol float64) bool {
	if len(left) != len(right) {
		return false
	}

	for i := range left {
		if math.Abs(float64(left[i])-float64(right[i])) > tol {
			return false
		}
	}

	return true
}

// unitNorm returns the L2 norm of v as float64.
func unitNorm(v []float32) float64 {
	var sum float64

	for _, x := range v {
		sum += float64(x) * float64(x)
	}

	return math.Sqrt(sum)
}

// --- buildEmbeddingsResponse tests ---

func TestBuildEmbeddingsResponse_Empty(t *testing.T) {
	resp := buildEmbeddingsResponse(nil)

	if resp.Model != "" {
		t.Errorf("Model = %q, want empty", resp.Model)
	}

	if resp.Dim != 0 {
		t.Errorf("Dim = %d, want 0", resp.Dim)
	}

	if len(resp.Vectors) != 0 {
		t.Errorf("Vectors len = %d, want 0", len(resp.Vectors))
	}

	// Signature must be sha256 of empty input per spec.
	const wantSig = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if resp.Signature != wantSig {
		t.Errorf("Signature = %q, want %q (sha256 of empty)", resp.Signature, wantSig)
	}

	// Signature must be stable across two empty calls.
	resp2 := buildEmbeddingsResponse(nil)
	if resp.Signature != resp2.Signature {
		t.Errorf("Signature not stable on empty: %q vs %q", resp.Signature, resp2.Signature)
	}
}

func TestBuildEmbeddingsResponse_SingleChunk(t *testing.T) {
	// Vector [3, 4, 0] → norm 5 → normalized [0.6, 0.8, 0].
	rows := []index.EmbeddingRow{
		embRow("notes/a", 0, "hash-a0", []float32{3, 4, 0}),
	}

	resp := buildEmbeddingsResponse(rows)

	if resp.Model != "nomic-embed-text" {
		t.Errorf("Model = %q, want nomic-embed-text", resp.Model)
	}

	if resp.Dim != 3 {
		t.Errorf("Dim = %d, want 3", resp.Dim)
	}

	vec, ok := resp.Vectors["notes/a"]
	if !ok {
		t.Fatal("notes/a missing from Vectors")
	}

	want := []float32{0.6, 0.8, 0}

	if !approxEqual(vec, want, 1e-6) {
		t.Errorf("vector = %v, want %v", vec, want)
	}

	if n := unitNorm(vec); math.Abs(n-1.0) > 1e-6 {
		t.Errorf("unit norm = %f, want 1.0", n)
	}
}

func TestBuildEmbeddingsResponse_TwoChunks(t *testing.T) {
	// Chunk 0: [1, 0, 0], chunk 1: [0, 1, 0].
	// Mean: [0.5, 0.5, 0], norm = sqrt(0.5), normalized ≈ [1/√2, 1/√2, 0].
	rows := []index.EmbeddingRow{
		embRow("notes/b", 0, "hash-b0", []float32{1, 0, 0}),
		embRow("notes/b", 1, "hash-b1", []float32{0, 1, 0}),
	}

	resp := buildEmbeddingsResponse(rows)

	vec, ok := resp.Vectors["notes/b"]
	if !ok {
		t.Fatal("notes/b missing from Vectors")
	}

	if len(vec) != 3 {
		t.Errorf("vector len = %d, want 3", len(vec))
	}

	inv2 := float32(1.0 / math.Sqrt(2))
	want := []float32{inv2, inv2, 0}

	if !approxEqual(vec, want, 1e-6) {
		t.Errorf("vector = %v, want %v", vec, want)
	}

	if n := unitNorm(vec); math.Abs(n-1.0) > 1e-6 {
		t.Errorf("unit norm = %f, want 1.0", n)
	}
}

func TestBuildEmbeddingsResponse_ZeroVectorOmitted(t *testing.T) {
	rows := []index.EmbeddingRow{
		embRow("notes/zero", 0, "hash-z0", []float32{0, 0, 0}),
	}

	resp := buildEmbeddingsResponse(rows)

	if _, ok := resp.Vectors["notes/zero"]; ok {
		t.Error("zero-vector node should be omitted from Vectors")
	}
}

func TestBuildEmbeddingsResponse_SignatureStableAndChanges(t *testing.T) {
	rows := []index.EmbeddingRow{
		embRow("notes/a", 0, "hash-a0", []float32{3, 4, 0}),
		embRow("notes/b", 0, "hash-b0", []float32{1, 0, 0}),
	}

	resp1 := buildEmbeddingsResponse(rows)
	resp2 := buildEmbeddingsResponse(rows)

	if resp1.Signature != resp2.Signature {
		t.Errorf("Signature not stable: %q vs %q", resp1.Signature, resp2.Signature)
	}

	// Change a content hash → signature must differ.
	rowsChanged := []index.EmbeddingRow{
		embRow("notes/a", 0, "hash-a0-changed", []float32{3, 4, 0}),
		embRow("notes/b", 0, "hash-b0", []float32{1, 0, 0}),
	}

	resp3 := buildEmbeddingsResponse(rowsChanged)

	if resp1.Signature == resp3.Signature {
		t.Error("Signature should differ when content hash changes")
	}
}

// --- handleEmbeddings HTTP tests ---

func TestHandleEmbeddings_NilSource(t *testing.T) {
	// Deps.Embeddings == nil → 200 + empty payload.
	nodes := &fakeNodes{
		files: []index.NodeRow{
			fileRow("notes/a", "note", "A", ""),
		},
	}
	srv := New(Deps{Nodes: nodes, Edges: &fakeEdges{}})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/embeddings")
	if err != nil {
		t.Fatalf("GET /api/embeddings: %v", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var payload EmbeddingsResponse
	if decErr := json.NewDecoder(resp.Body).Decode(&payload); decErr != nil {
		t.Fatalf("decode: %v", decErr)
	}

	if len(payload.Vectors) != 0 {
		t.Errorf("Vectors len = %d, want 0", len(payload.Vectors))
	}
}

func TestHandleEmbeddings_PopulatedSource(t *testing.T) {
	nodes := &fakeNodes{
		files: []index.NodeRow{
			fileRow("notes/a", "note", "A", ""),
			fileRow("notes/b", "note", "B", ""),
		},
	}
	embs := &fakeEmbeddingSource{
		rows: []index.EmbeddingRow{
			embRow("notes/a", 0, "hash-a0", []float32{3, 4, 0}),
			embRow("notes/b", 0, "hash-b0", []float32{1, 0, 0}),
		},
	}

	srv := New(Deps{Nodes: nodes, Edges: &fakeEdges{}, Embeddings: embs})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/embeddings")
	if err != nil {
		t.Fatalf("GET /api/embeddings: %v", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var payload EmbeddingsResponse
	if decErr := json.NewDecoder(resp.Body).Decode(&payload); decErr != nil {
		t.Fatalf("decode: %v", decErr)
	}

	if payload.Model != "nomic-embed-text" {
		t.Errorf("Model = %q, want nomic-embed-text", payload.Model)
	}

	if payload.Dim != 3 {
		t.Errorf("Dim = %d, want 3", payload.Dim)
	}

	if len(payload.Vectors) != 2 {
		t.Errorf("Vectors len = %d, want 2", len(payload.Vectors))
	}

	vecA, ok := payload.Vectors["notes/a"]
	if !ok {
		t.Fatal("notes/a missing from Vectors")
	}

	if n := unitNorm(vecA); math.Abs(n-1.0) > 1e-6 {
		t.Errorf("notes/a unit norm = %f, want 1.0", n)
	}

	if payload.Signature == "" {
		t.Error("Signature should be non-empty")
	}
}
