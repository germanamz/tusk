package query_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/query"
)

// loadQueryManifest loads a workspace manifest whose [query] table is body,
// with the built-in packs merged as the CLI and MCP do.
func loadQueryManifest(test *testing.T, body string) *manifest.Manifest {
	test.Helper()

	manifestPath := filepath.Join(test.TempDir(), "tusk.toml")
	content := "[workspace]\nname = \"test\"\n\n[node-types.note]\ndescription = \"n\"\n\n" + body

	if writeErr := os.WriteFile(manifestPath, []byte(content), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("load manifest: %v", loadErr)
	}

	manifest.MergeBuiltinPacks(loaded)

	return loaded
}

// runHubQuery runs semantic over the seedHub fixture under a manifest whose
// [query] table is body.
func runHubQuery(test *testing.T, body, semantic string, minScore float64) *query.SemanticResult {
	test.Helper()

	store := openTestStore(test)
	seedHub(test, store)

	result, runErr := query.Run(context.Background(), query.Deps{
		Database:   store.DB(),
		Manifest:   loadQueryManifest(test, body),
		Nodes:      index.NewNodeRepo(store),
		Embedder:   stubEmbedder{vector: []float32{1, 0}},
		Embeddings: index.NewEmbeddingRepo(store),
	}, query.Request{Filter: "type=note", Semantic: semantic, MinScore: minScore, Explain: true})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	return result.Semantic
}

// TestQueryRun_TitleMatchLiftsNamedFile pins #849 finding 8: a query naming a
// file's title lifts every passage of that file, so docs/hub (best cosine
// 0.6) overtakes docs/other (cosine 0.8), which the query doesn't name.
func TestQueryRun_TitleMatchLiftsNamedFile(test *testing.T) {
	cosineOnly := runHubQuery(test, "[query]\ntitle-weight = 0\n", "how does the hub poll", 0)

	if cosineOnly.Ranked[0].ID != "docs/other" {
		test.Fatalf("title-weight 0: ranked[0] = %s, want docs/other on cosine alone", cosineOnly.Ranked[0].ID)
	}

	result := runHubQuery(test, "[query]\ntitle-weight = 0.6\n", "how does the hub poll", 0)
	ranked := result.Ranked

	if len(ranked) != 2 || ranked[0].ID != "docs/hub" {
		test.Fatalf("ranked = %+v, want docs/hub lifted by its title", ranked)
	}

	// 1 - (1 - 0.6) * (1 - 0.6 * 1)
	if !near(ranked[0].Score, 0.84) || ranked[0].TitleScore != 1 || !near(ranked[0].CosineScore, 0.6) || !near(ranked[0].FinalScore, 0.84) {
		test.Errorf("hub score=%v title=%v cosine=%v final=%v, want 0.84, 1, 0.6, 0.84", ranked[0].Score, ranked[0].TitleScore, ranked[0].CosineScore, ranked[0].FinalScore)
	}

	if !near(ranked[1].Score, 0.8) || ranked[1].TitleScore != 0 {
		test.Errorf("other score=%v title=%v, want its bare cosine 0.8 and title 0", ranked[1].Score, ranked[1].TitleScore)
	}

	// Every passage of the named file is lifted, so the order inside the file
	// stays the cosine order.
	units := ranked[0].MatchedUnits

	if len(units) != 2 || units[0].ID != "docs/hub#S2" || units[1].TitleScore != 1 {
		test.Fatalf("units = %+v, want both sections lifted, Broadcast first", units)
	}

	if result.Trace == nil || *result.Trace != (query.Trace{Title: true}) {
		test.Errorf("Trace = %+v, want the title match alone", result.Trace)
	}
}

// TestQueryRun_PathWeightDemotesBeforeMinScore checks a [query.path-weights]
// entry scales a file's final score and re-orders the rank, and that MinScore
// applies to the weighted score.
func TestQueryRun_PathWeightDemotesBeforeMinScore(test *testing.T) {
	body := "[query]\ntitle-weight = 0\n\n[query.path-weights]\n\"docs/other.md\" = 0.5\n"
	result := runHubQuery(test, body, "slow reader", 0)
	ranked := result.Ranked

	if len(ranked) != 2 || ranked[0].ID != "docs/hub" {
		test.Fatalf("ranked = %+v, want docs/hub ahead of the demoted docs/other", ranked)
	}

	other := ranked[1]

	if !near(other.Score, 0.4) || other.Weight != 0.5 || !near(other.CosineScore, 0.8) || !near(other.FinalScore, 0.4) {
		test.Errorf("other score=%v weight=%v cosine=%v final=%v, want 0.4, 0.5, 0.8, 0.4", other.Score, other.Weight, other.CosineScore, other.FinalScore)
	}

	if ranked[0].Weight != 1 || other.MatchedUnits[0].Weight != 0.5 {
		test.Errorf("hub weight=%v, other unit weight=%v; want 1 and 0.5", ranked[0].Weight, other.MatchedUnits[0].Weight)
	}

	if result.Trace == nil || *result.Trace != (query.Trace{Weight: true}) {
		test.Errorf("Trace = %+v, want the weight alone", result.Trace)
	}

	// docs/other's one leaf weighs 0.8 * 0.5 = 0.4 and drops, as does hub's
	// Polling leaf (0.28); MinScore counts leaves.
	filtered := runHubQuery(test, body, "slow reader", 0.5)

	if len(filtered.Ranked) != 1 || filtered.Ranked[0].ID != "docs/hub" || filtered.FilteredBelowMinScore != 2 {
		test.Errorf("min_score 0.5: ranked %d rows (%+v), %d filtered; want docs/hub alone and 2 filtered", len(filtered.Ranked), filtered.Ranked, filtered.FilteredBelowMinScore)
	}
}

func TestQueryRun_TitleMatchFileLevelPath(test *testing.T) {
	store := openTestStore(test)
	nodes := index.NewNodeRepo(store)
	embeddings := index.NewEmbeddingRepo(store)

	files := []struct {
		id, title string
		vector    []float32
	}{
		{id: "notes/incident", title: "Incident 4", vector: []float32{0.8, 0.6}},
		{id: "services/ledger-sync", vector: []float32{0.6, 0.8}},
	}

	for _, file := range files {
		if upsertErr := nodes.Upsert(index.NodeRow{
			ID: file.id, Type: "note", Path: file.id + ".md", Title: file.title,
			PropertiesJSON: "{}", LastChecksum: "x",
		}); upsertErr != nil {
			test.Fatalf("upsert: %v", upsertErr)
		}

		if upsertErr := embeddings.Upsert(index.EmbeddingRow{
			NodeID: file.id, Model: "stub", ContentHash: "h_" + file.id,
			Vector: file.vector, Dim: 2, Body: "body",
		}); upsertErr != nil {
			test.Fatalf("embed upsert: %v", upsertErr)
		}
	}

	result, runErr := query.Run(context.Background(), query.Deps{
		Database:   store.DB(),
		Manifest:   loadQueryManifest(test, "[query]\ntitle-weight = 0.6\n"),
		Nodes:      nodes,
		Embedder:   stubEmbedder{vector: []float32{1, 0}},
		Embeddings: embeddings,
	}, query.Request{Filter: "type=note", Semantic: "which topic does ledger sync consume", Explain: true})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	ranked := result.Semantic.Ranked

	// The untitled file is named by its file name.
	if len(ranked) != 2 || ranked[0].ID != "services/ledger-sync" || ranked[0].TitleScore != 1 || !near(ranked[0].Score, 0.84) {
		test.Fatalf("ranked = %+v, want services/ledger-sync first at 0.84, named by its file name", ranked)
	}
}
