package query_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/query"
)

// seedHub seeds two files against the query vector {1, 0}. docs/other has the
// closest vector (cosine 0.8) and none of the query's words. docs/hub's
// Broadcast paragraph is further off (cosine 0.6) but holds the words; its
// Polling paragraph is far off and holds none.
func seedHub(test *testing.T, store *index.Index) {
	test.Helper()

	nodes := index.NewNodeRepo(store)
	embeddings := index.NewEmbeddingRepo(store)

	for _, file := range []struct{ id, title string }{{"docs/hub", "Hub"}, {"docs/other", "Other"}} {
		if upsertErr := nodes.Upsert(index.NodeRow{
			ID: file.id, Type: "note", Path: file.id + ".md", Title: file.title,
			PropertiesJSON: "{}", LastChecksum: "x",
		}); upsertErr != nil {
			test.Fatalf("file upsert: %v", upsertErr)
		}
	}

	units := []struct {
		id, typ, parent, title, payload string
		ordinal                         int
		vector                          []float32
	}{
		{id: "docs/hub#S1", typ: "section", parent: "docs/hub", title: "Polling", payload: "Polling\n...", ordinal: 0},
		{id: "docs/hub#S1P1", typ: "paragraph", parent: "docs/hub#S1", payload: "The hub polls the change source every two seconds.", ordinal: 1, vector: []float32{0.28, 0.96}},
		{id: "docs/hub#S2", typ: "section", parent: "docs/hub", title: "Broadcast", payload: "Broadcast\n...", ordinal: 2},
		{id: "docs/hub#S2P1", typ: "paragraph", parent: "docs/hub#S2", payload: "Broadcast drops the frame for a client whose channel is full rather than blocking on a slow reader.", ordinal: 3, vector: []float32{0.6, 0.8}},
		{id: "docs/other#P1", typ: "paragraph", parent: "docs/other", payload: "Lease expiry reclaims a claimed queue row.", ordinal: 0, vector: []float32{0.8, 0.6}},
	}

	for _, unit := range units {
		path := unit.parent[:strings.Index(unit.parent+"#", "#")] + ".md"

		if upsertErr := nodes.BulkUpsert([]index.NodeRow{{
			ID: unit.id, Type: unit.typ, Path: path, Title: unit.title,
			PropertiesJSON: "{}", LastChecksum: "x",
			ParentID:     sql.NullString{String: unit.parent, Valid: true},
			Ordinal:      sql.NullInt64{Int64: int64(unit.ordinal), Valid: true},
			EmbedPayload: sql.NullString{String: unit.payload, Valid: true},
		}}, "markdown"); upsertErr != nil {
			test.Fatalf("sub-unit upsert %s: %v", unit.id, upsertErr)
		}

		if unit.vector == nil {
			continue
		}

		if upsertErr := embeddings.Upsert(index.EmbeddingRow{
			NodeID: unit.id, Model: "stub", ContentHash: "h_" + unit.id,
			Vector: unit.vector, Dim: len(unit.vector), Body: unit.payload,
		}); upsertErr != nil {
			test.Fatalf("embedding upsert %s: %v", unit.id, upsertErr)
		}
	}
}

func runHub(test *testing.T, lexicalWeight float64, explain bool) []query.ScoredRow {
	test.Helper()

	return runHubResult(test, lexicalWeight, explain).Ranked
}

func runHubResult(test *testing.T, lexicalWeight float64, explain bool) *query.SemanticResult {
	test.Helper()

	store := openTestStore(test)
	seedHub(test, store)

	loaded := loadManifestWithSubUnits(test)
	loaded.LexicalWeight = lexicalWeight

	result, runErr := query.Run(context.Background(), query.Deps{
		Database:   store.DB(),
		Manifest:   loaded,
		Nodes:      index.NewNodeRepo(store),
		Embedder:   stubEmbedder{vector: []float32{1, 0}},
		Embeddings: index.NewEmbeddingRepo(store),
	}, query.Request{Filter: "type=note", Semantic: "slow reader dropped the frame", Explain: explain})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	return result.Semantic
}

func near(left, right float64) bool {
	return math.Abs(left-right) < 1e-6
}

func TestQueryRun_LexicalWeightZeroRanksByCosine(test *testing.T) {
	ranked := runHub(test, 0, false)

	if len(ranked) != 2 || ranked[0].ID != "docs/other" {
		test.Fatalf("ranked = %+v, want docs/other first on cosine alone", ranked)
	}

	if !near(ranked[1].Score, 0.6) {
		test.Errorf("docs/hub score = %v, want its bare cosine 0.6", ranked[1].Score)
	}
}

func TestQueryRun_LexicalWeightLiftsWordMatch(test *testing.T) {
	ranked := runHub(test, 0.75, false)

	if ranked[0].ID != "docs/hub" {
		test.Fatalf("ranked[0] = %s, want docs/hub lifted by its word match", ranked[0].ID)
	}

	// cosine 0.6 + 0.75 * lexical 1 * (1 - 0.6)
	if !near(ranked[0].Score, 0.9) {
		test.Errorf("docs/hub score = %v, want 0.9", ranked[0].Score)
	}

	if !near(ranked[1].Score, 0.8) {
		test.Errorf("docs/other score = %v, want its bare cosine 0.8 (no shared word)", ranked[1].Score)
	}

	units := ranked[0].MatchedUnits

	if len(units) == 0 || units[0].ID != "docs/hub#S2" {
		test.Fatalf("matched units = %v, want the Broadcast section first", unitIDs(units))
	}

	if !strings.Contains(units[0].Snippet, "slow reader") {
		test.Errorf("snippet %q should come from the Broadcast paragraph", units[0].Snippet)
	}
}

func TestQueryRun_LexicalExplainTrace(test *testing.T) {
	ranked := runHub(test, 0.75, true)
	hub := ranked[0]
	unit := hub.MatchedUnits[0]

	if !near(unit.CosineScore, 0.6) || unit.LexicalScore != 1 || !near(unit.FinalScore, 0.9) {
		test.Errorf("unit trace cosine=%v lexical=%v final=%v, want 0.6, 1, 0.9", unit.CosineScore, unit.LexicalScore, unit.FinalScore)
	}

	if !near(hub.CosineScore, 0.6) || hub.LexicalScore != 1 || !near(hub.FinalScore, 0.9) {
		test.Errorf("row trace cosine=%v lexical=%v final=%v, want 0.6, 1, 0.9", hub.CosineScore, hub.LexicalScore, hub.FinalScore)
	}

	other := ranked[1]

	if !near(other.CosineScore, 0.8) || other.LexicalScore != 0 || !near(other.FinalScore, 0.8) {
		test.Errorf("docs/other trace cosine=%v lexical=%v final=%v, want 0.8, 0, 0.8", other.CosineScore, other.LexicalScore, other.FinalScore)
	}

	encoded, marshalErr := json.Marshal(unit)

	if marshalErr != nil {
		test.Fatalf("marshal: %v", marshalErr)
	}

	if !strings.Contains(string(encoded), `"lexical_score":1`) {
		test.Errorf("unit JSON %s lacks lexical_score", encoded)
	}
}

// TestQueryRun_ExplainWithCosineAlone pins #849 finding 13: with neither
// lexical fusion nor graph expansion on, explain still reports the cosine and
// final score each row ranked by, and the trace names no other component.
func TestQueryRun_ExplainWithCosineAlone(test *testing.T) {
	result := runHubResult(test, 0, true)

	if result.Trace == nil || *result.Trace != (query.Trace{}) {
		test.Fatalf("Trace = %+v, want cosine and final alone", result.Trace)
	}

	for _, row := range result.Ranked {
		if row.CosineScore == 0 || !near(row.CosineScore, row.Score) || !near(row.FinalScore, row.Score) {
			test.Errorf("%s: cosine %v final %v, want both the score %v", row.ID, row.CosineScore, row.FinalScore, row.Score)
		}

		if row.LexicalScore != 0 || row.GraphScore != 0 || row.Distance != 0 {
			test.Errorf("%s: trace %+v carries a component the ranking didn't compute", row.ID, row)
		}

		for _, unit := range row.MatchedUnits {
			if !near(unit.CosineScore, unit.Score) || !near(unit.FinalScore, unit.Score) {
				test.Errorf("%s: cosine %v final %v, want both the score %v", unit.ID, unit.CosineScore, unit.FinalScore, unit.Score)
			}
		}
	}

	if runHubResult(test, 0, false).Trace != nil {
		test.Errorf("Trace set without explain")
	}
}

func TestQueryRun_LexicalWeightFileLevelPath(test *testing.T) {
	store := openTestStore(test)
	nodes := index.NewNodeRepo(store)
	embeddings := index.NewEmbeddingRepo(store)

	files := []struct {
		id, body string
		vector   []float32
	}{
		{id: "notes/alpha", body: "Lease expiry reclaims a claimed queue row.", vector: []float32{0.8, 0.6}},
		{id: "notes/bravo", body: "Broadcast drops the frame for a slow reader.", vector: []float32{0.6, 0.8}},
	}

	for _, file := range files {
		if upsertErr := nodes.Upsert(index.NodeRow{
			ID: file.id, Type: "note", Path: file.id + ".md", Title: file.id,
			PropertiesJSON: "{}", LastChecksum: "x",
		}); upsertErr != nil {
			test.Fatalf("upsert: %v", upsertErr)
		}

		if upsertErr := embeddings.Upsert(index.EmbeddingRow{
			NodeID: file.id, Model: "stub", ContentHash: "h_" + file.id,
			Vector: file.vector, Dim: 2, Body: file.body,
		}); upsertErr != nil {
			test.Fatalf("embed upsert: %v", upsertErr)
		}
	}

	manifestPath := filepath.Join(test.TempDir(), "tusk.toml")
	body := "[workspace]\nname = \"test\"\nsub-units = false\n\n[query]\nlexical-weight = 0.75\n"

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("load manifest: %v", loadErr)
	}

	manifest.MergeBuiltinPacks(loaded)

	result, runErr := query.Run(context.Background(), query.Deps{
		Database:   store.DB(),
		Manifest:   loaded,
		Nodes:      nodes,
		Embedder:   stubEmbedder{vector: []float32{1, 0}},
		Embeddings: embeddings,
	}, query.Request{Filter: "type=note", Semantic: "slow reader", Explain: true})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	ranked := result.Semantic.Ranked

	if len(ranked) != 2 || ranked[0].ID != "notes/bravo" {
		test.Fatalf("ranked = %+v, want notes/bravo lifted by its word match", ranked)
	}

	if !near(ranked[0].Score, 0.9) || ranked[0].LexicalScore != 1 || !near(ranked[0].CosineScore, 0.6) {
		test.Errorf("bravo score=%v lexical=%v cosine=%v, want 0.9, 1, 0.6", ranked[0].Score, ranked[0].LexicalScore, ranked[0].CosineScore)
	}
}
