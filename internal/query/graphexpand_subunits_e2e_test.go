package query_test

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/query"
)

// seedSubUnitFile writes one file node plus one embedded paragraph leaf per
// vector (`<fileID>#P1`, `<fileID>#P2`, ...), mirroring what the sub-unit
// pipeline produces for a multi-paragraph markdown file.
func seedSubUnitFile(t *testing.T, store *index.Index, fileID, nodeType, title string, vectors ...[]float32) {
	t.Helper()

	nodes := index.NewNodeRepo(store)
	embeddings := index.NewEmbeddingRepo(store)

	if err := nodes.Upsert(index.NodeRow{
		ID:             fileID,
		Type:           nodeType,
		Path:           fileID + ".md",
		Title:          title,
		PropertiesJSON: "{}",
		LastChecksum:   "x",
	}); err != nil {
		t.Fatalf("file upsert %s: %v", fileID, err)
	}

	for ordinal, vector := range vectors {
		leafID := fmt.Sprintf("%s#P%d", fileID, ordinal+1)

		if err := nodes.BulkUpsert([]index.NodeRow{{
			ID:             leafID,
			Type:           "paragraph",
			Path:           fileID + ".md",
			PropertiesJSON: "{}",
			LastChecksum:   "x",
			ParentID:       sql.NullString{String: fileID, Valid: true},
			Ordinal:        sql.NullInt64{Int64: int64(ordinal), Valid: true},
			EmbedPayload:   sql.NullString{String: title + " body", Valid: true},
		}}, "markdown"); err != nil {
			t.Fatalf("sub-unit upsert %s: %v", leafID, err)
		}

		if err := embeddings.Upsert(index.EmbeddingRow{
			NodeID:      leafID,
			ChunkIdx:    0,
			Model:       "stub",
			ContentHash: "h_" + leafID,
			Vector:      vector,
			Dim:         len(vector),
			Body:        title + " body",
		}); err != nil {
			t.Fatalf("embedding upsert %s: %v", leafID, err)
		}
	}
}

func subUnitDeps(store *index.Index) query.Deps {
	loaded := &manifest.Manifest{}
	manifest.MergeBuiltinPacks(loaded)

	return query.Deps{
		Database:   store.DB(),
		Manifest:   loaded,
		Embedder:   stubEmbedder{vector: []float32{1, 0}},
		Embeddings: index.NewEmbeddingRepo(store),
		Nodes:      index.NewNodeRepo(store),
		Edges:      index.NewEdgeRepo(store),
	}
}

func rowsByID(t *testing.T, result *query.Result) map[string]query.ScoredRow {
	t.Helper()

	if result.Semantic == nil {
		t.Fatalf("expected semantic result")
	}

	rows := make(map[string]query.ScoredRow, len(result.Semantic.Ranked))

	for _, row := range result.Semantic.Ranked {
		if strings.Contains(row.ID, "#") {
			t.Errorf("sub-unit id %q surfaced as a top-level row", row.ID)
		}

		rows[row.ID] = row
	}

	return rows
}

func assertScore(t *testing.T, label string, got, want float64) {
	t.Helper()

	if math.Abs(got-want) > 1e-6 {
		t.Errorf("%s = %v, want %v", label, got, want)
	}
}

// TestQueryRun_GraphExpansion_SubUnitPath_BlendsFileLevelEdges is the
// regression test for the "expansion is inert on the default path" bug:
// with sub-unit embeddings present (the default for every indexed
// workspace), user-declared edges join FILE ids while the leaf cosine rank
// carries `file#hash` ids, so the walker never matched an edge and every
// score collapsed to (1-weight) * cosine with graph_score always 0.
//
// Fixture: dave has two paragraphs (leaf cosines 1.0 and 0.28) so the
// max-leaf file aggregation is distinguishable from mean/first/last; one
// `team` edge joins the files. The weight is asymmetric (0.3) so a
// coefficient swap in the blend cannot slip past the assertions.
//
//	people/dave (leaves 1.0, 0.28) -[team]-> teams/puma (leaf 0.6)
//
// Expected per spec §6.1 at the file level: graph(dave) = 0.6 (puma's file
// cosine), graph(puma) = 1.0 (dave's MAX leaf), final(dave) = 0.7*1.0 +
// 0.3*0.6 = 0.88, final(puma) = 0.7*0.6 + 0.3*1.0 = 0.72, and dave's weak
// leaf blends its own cosine: 0.7*0.28 + 0.3*0.6 = 0.376.
func TestQueryRun_GraphExpansion_SubUnitPath_BlendsFileLevelEdges(test *testing.T) {
	store := openTestStore(test)

	seedSubUnitFile(test, store, "people/dave", "person", "Dave", []float32{1, 0}, []float32{0.28, 0.96})
	seedSubUnitFile(test, store, "teams/puma", "team", "Puma", []float32{0.6, 0.8})

	edges := index.NewEdgeRepo(store)

	if err := edges.UpsertAll("people/dave", "people/dave.md", []index.EdgeRow{
		{Type: "team", SourceID: "people/dave", TargetID: "teams/puma", SourcePath: "people/dave.md", Kind: "derived"},
	}); err != nil {
		test.Fatalf("edge upsert: %v", err)
	}

	result, runErr := query.Run(context.Background(), subUnitDeps(store), query.Request{
		Filter:   "",
		Semantic: "billing engineer",
		Explain:  true,
		GraphExpansion: &manifest.GraphExpansion{
			Enabled:             true,
			Hops:                1,
			EdgeTypes:           []string{"team"},
			Weight:              0.3,
			CandidateMultiplier: 5,
		},
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	rows := rowsByID(test, result)

	dave, hasDave := rows["people/dave"]

	if !hasDave {
		test.Fatalf("people/dave missing from ranked: %+v", result.Semantic.Ranked)
	}

	puma, hasPuma := rows["teams/puma"]

	if !hasPuma {
		test.Fatalf("teams/puma missing from ranked: %+v", result.Semantic.Ranked)
	}

	// The inert-expansion bug collapsed these to (1-w) * cosine with
	// graph_score 0. The asymmetric weight pins the coefficient order.
	assertScore(test, "dave score", dave.Score, 0.88)
	assertScore(test, "puma score", puma.Score, 0.72)

	// Row-level explain trace for a seed row: aggregated max-leaf cosine,
	// graph term, blended final (== Score), and hop distance 0.
	assertScore(test, "dave row cosine_score", dave.CosineScore, 1.0)
	assertScore(test, "dave row graph_score", dave.GraphScore, 0.6)
	assertScore(test, "dave row final_score", dave.FinalScore, 0.88)

	if dave.Distance != 0 {
		test.Errorf("dave row distance = %d, want 0", dave.Distance)
	}

	// puma's graph term must come from dave's MAX leaf (1.0), not the mean
	// or the last-seen leaf — that is the file seed aggregation contract.
	assertScore(test, "puma row graph_score", puma.GraphScore, 1.0)

	// Per-leaf explain traces: the strong leaf carries the file blend; the
	// weak leaf blends its OWN cosine with the shared file graph term.
	if len(dave.MatchedUnits) != 2 {
		test.Fatalf("dave matched_units = %d, want 2", len(dave.MatchedUnits))
	}

	strong, weak := dave.MatchedUnits[0], dave.MatchedUnits[1]

	if strong.ID != "people/dave#P1" || weak.ID != "people/dave#P2" {
		test.Fatalf("dave units = (%s, %s), want (people/dave#P1, people/dave#P2)", strong.ID, weak.ID)
	}

	assertScore(test, "strong leaf cosine_score", strong.CosineScore, 1.0)
	assertScore(test, "strong leaf graph_score", strong.GraphScore, 0.6)
	assertScore(test, "strong leaf final_score", strong.FinalScore, 0.88)
	assertScore(test, "weak leaf final_score", weak.FinalScore, 0.376)
}

// TestQueryRun_GraphExpansion_SubUnitPath_SurfacesWalkedNeighbor covers the
// reporter's retest gate: a 1-hop neighbor of the top seed that has NO
// embeddings of its own (so it can never be a cosine seed) must enter the
// result set at dist=1 with a non-zero graph_score.
//
//	people/dave (leaf cosine 1.0) -[guide]-> notes/runbook (no embeddings)
//
// With hops=1, weight=0.5: final(runbook) = 0.5 * graph = 0.5 * 1.0 = 0.5.
func TestQueryRun_GraphExpansion_SubUnitPath_SurfacesWalkedNeighbor(test *testing.T) {
	store := openTestStore(test)

	seedSubUnitFile(test, store, "people/dave", "person", "Dave", []float32{1, 0})

	nodes := index.NewNodeRepo(store)

	// notes/runbook is a file node with no sub-units and no embeddings.
	if err := nodes.Upsert(index.NodeRow{
		ID:             "notes/runbook",
		Type:           "note",
		Path:           "notes/runbook.md",
		Title:          "Runbook",
		PropertiesJSON: "{}",
		LastChecksum:   "x",
	}); err != nil {
		test.Fatalf("runbook upsert: %v", err)
	}

	edges := index.NewEdgeRepo(store)

	if err := edges.UpsertAll("people/dave", "people/dave.md", []index.EdgeRow{
		{Type: "guide", SourceID: "people/dave", TargetID: "notes/runbook", SourcePath: "people/dave.md", Kind: "direct"},
	}); err != nil {
		test.Fatalf("edge upsert: %v", err)
	}

	expansion := &manifest.GraphExpansion{
		Enabled:             true,
		Hops:                1,
		EdgeTypes:           []string{"guide"},
		Weight:              0.5,
		CandidateMultiplier: 5,
	}

	result, runErr := query.Run(context.Background(), subUnitDeps(store), query.Request{
		Filter:         "",
		Semantic:       "billing engineer",
		Explain:        true,
		GraphExpansion: expansion,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	rows := rowsByID(test, result)

	runbook, hasRunbook := rows["notes/runbook"]

	if !hasRunbook {
		test.Fatalf("notes/runbook (1-hop neighbor of the top seed) missing from ranked: %+v", result.Semantic.Ranked)
	}

	if runbook.Distance != 1 {
		test.Errorf("runbook distance = %d, want 1", runbook.Distance)
	}

	assertScore(test, "runbook graph_score", runbook.GraphScore, 1.0)
	assertScore(test, "runbook score", runbook.Score, 0.5)
	assertScore(test, "runbook final_score", runbook.FinalScore, runbook.Score)

	// The walked-in row must resolve node metadata (title/type/path).
	if runbook.Title != "Runbook" || runbook.Type != "note" {
		test.Errorf("runbook meta = (%q, %q), want (Runbook, note)", runbook.Title, runbook.Type)
	}

	// Same fixture with MinScore above the walked-in final: the runbook must
	// be filtered and counted exactly once, and dave's own leaf (final =
	// 0.5*1.0 + 0.5*0 = 0.5, runbook is not a seed) drops too.
	filtered, filteredErr := query.Run(context.Background(), subUnitDeps(store), query.Request{
		Filter:         "",
		Semantic:       "billing engineer",
		MinScore:       0.6,
		GraphExpansion: expansion,
	})

	if filteredErr != nil {
		test.Fatalf("Run (min-score): %v", filteredErr)
	}

	if count := len(filtered.Semantic.Ranked); count != 0 {
		test.Errorf("ranked with min-score 0.6 = %d rows, want 0", count)
	}

	if filtered.Semantic.FilteredBelowMinScore != 2 {
		test.Errorf("FilteredBelowMinScore = %d, want 2 (dave leaf + runbook bare row)", filtered.Semantic.FilteredBelowMinScore)
	}
}

// TestQueryRun_GraphExpansion_SubUnitPath_SeedPoolTruncation pins the
// take * candidate-multiplier seed-pool semantics against the file-level
// path's behavior:
//
//   - a rank-tail file WALKED IN at dist>0 holds its position on graph merit
//     alone (its leaves' cosines are zeroed, so Score == FinalScore and it
//     cannot leapfrog seeds);
//   - a rank-tail file NOT walked in drops out entirely.
//
// Fixture: cosines one=1.0, two=0.8, three=0.6, four=0.28; edge one->three;
// take=2 * multiplier=1 seeds {one, two}. Expected finals (w=0.3):
// one = 0.7 (graph 0: three is not a seed), two = 0.56, three = 0.3*1.0 =
// 0.3 (cosine zeroed, graph = one's cosine), four = dropped.
func TestQueryRun_GraphExpansion_SubUnitPath_SeedPoolTruncation(test *testing.T) {
	store := openTestStore(test)

	seedSubUnitFile(test, store, "aaa/one", "note", "One", []float32{1, 0})
	seedSubUnitFile(test, store, "bbb/two", "note", "Two", []float32{0.8, 0.6})
	seedSubUnitFile(test, store, "ccc/three", "note", "Three", []float32{0.6, 0.8})
	seedSubUnitFile(test, store, "ddd/four", "note", "Four", []float32{0.28, 0.96})

	edges := index.NewEdgeRepo(store)

	if err := edges.UpsertAll("aaa/one", "aaa/one.md", []index.EdgeRow{
		{Type: "team", SourceID: "aaa/one", TargetID: "ccc/three", SourcePath: "aaa/one.md", Kind: "derived"},
	}); err != nil {
		test.Fatalf("edge upsert: %v", err)
	}

	expansion := &manifest.GraphExpansion{
		Enabled:             true,
		Hops:                1,
		EdgeTypes:           []string{"team"},
		Weight:              0.3,
		CandidateMultiplier: 1,
	}

	page1, page1Err := query.Run(context.Background(), subUnitDeps(store), query.Request{
		Filter:         "",
		Semantic:       "anything",
		Take:           2,
		Explain:        true,
		GraphExpansion: expansion,
	})

	if page1Err != nil {
		test.Fatalf("Run (page 1): %v", page1Err)
	}

	if ids := rankedIDs(page1); len(ids) != 2 || ids[0] != "aaa/one" || ids[1] != "bbb/two" {
		test.Fatalf("page 1 = %v, want [aaa/one bbb/two]", ids)
	}

	assertScore(test, "one score", page1.Semantic.Ranked[0].Score, 0.7)
	assertScore(test, "two score", page1.Semantic.Ranked[1].Score, 0.56)

	page2, page2Err := query.Run(context.Background(), subUnitDeps(store), query.Request{
		Filter:         "",
		Semantic:       "anything",
		Take:           2,
		Skip:           2,
		Explain:        true,
		GraphExpansion: expansion,
	})

	if page2Err != nil {
		test.Fatalf("Run (page 2): %v", page2Err)
	}

	// Exactly one row: ccc/three (walked in). ddd/four fell outside the seed
	// pool without being walked in, so it dropped from the candidate set —
	// the same truncation the file-level path applies.
	if ids := rankedIDs(page2); len(ids) != 1 || ids[0] != "ccc/three" {
		test.Fatalf("page 2 = %v, want [ccc/three]", ids)
	}

	three := page2.Semantic.Ranked[0]

	assertScore(test, "three score", three.Score, 0.3)
	assertScore(test, "three final_score", three.FinalScore, three.Score)
	assertScore(test, "three cosine_score", three.CosineScore, 0)
	assertScore(test, "three graph_score", three.GraphScore, 1.0)

	if three.Distance != 1 {
		test.Errorf("three distance = %d, want 1", three.Distance)
	}

	// Its leaf survives as a matched unit, blended on graph merit alone.
	if len(three.MatchedUnits) != 1 || three.MatchedUnits[0].ID != "ccc/three#P1" {
		test.Fatalf("three matched_units = %+v, want [ccc/three#P1]", three.MatchedUnits)
	}

	assertScore(test, "three leaf final_score", three.MatchedUnits[0].FinalScore, 0.3)
}

// TestQueryRun_GraphExpansion_SubUnitPath_DefaultEdgeTypesWithContains runs
// the manifest DEFAULT edge-types list — which includes the structural
// `contains` type — over a workspace where the sub-unit pipeline has written
// its file -> `file#hash` containment edges. The walk reaches sub-unit ids
// at hop 1; they must fold onto their (already-seeded) parent files rather
// than surface as rows or distort the graph averages.
func TestQueryRun_GraphExpansion_SubUnitPath_DefaultEdgeTypesWithContains(test *testing.T) {
	store := openTestStore(test)

	seedSubUnitFile(test, store, "people/dave", "person", "Dave", []float32{1, 0})
	seedSubUnitFile(test, store, "teams/puma", "team", "Puma", []float32{0.6, 0.8})

	edges := index.NewEdgeRepo(store)

	markdown := sql.NullString{String: "markdown", Valid: true}

	if err := edges.UpsertAll("people/dave", "people/dave.md", []index.EdgeRow{
		{Type: "references", SourceID: "people/dave", TargetID: "teams/puma", SourcePath: "people/dave.md", Kind: "direct"},
		{Type: "contains", SourceID: "people/dave", TargetID: "people/dave#P1", SourcePath: "people/dave.md", Kind: "structural", Source: markdown},
	}); err != nil {
		test.Fatalf("edge upsert dave: %v", err)
	}

	if err := edges.UpsertAll("teams/puma", "teams/puma.md", []index.EdgeRow{
		{Type: "contains", SourceID: "teams/puma", TargetID: "teams/puma#P1", SourcePath: "teams/puma.md", Kind: "structural", Source: markdown},
	}); err != nil {
		test.Fatalf("edge upsert puma: %v", err)
	}

	result, runErr := query.Run(context.Background(), subUnitDeps(store), query.Request{
		Filter:   "",
		Semantic: "anything",
		Explain:  true,
		GraphExpansion: &manifest.GraphExpansion{
			Enabled:             true,
			Hops:                1,
			EdgeTypes:           manifest.DefaultGraphExpansion().EdgeTypes,
			Weight:              0.3,
			CandidateMultiplier: 5,
		},
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	// rowsByID fails the test if any '#' id leaks into the row set.
	rows := rowsByID(test, result)

	if len(rows) != 2 {
		test.Fatalf("ranked rows = %d, want 2 (dave, puma): %+v", len(rows), result.Semantic.Ranked)
	}

	// Structural containment edges must not distort the blend: same numbers
	// as the plain two-file fixture (non-seed sub-unit neighbors do not
	// enter the graph average).
	assertScore(test, "dave score", rows["people/dave"].Score, 0.88)
	assertScore(test, "puma score", rows["teams/puma"].Score, 0.72)
}

// TestQueryRun_GraphExpansion_SubUnitPath_CrossFileSubUnitTarget covers
// edges whose TARGET is another file's sub-unit id (wikilinks like
// [[other#S1P3]] persist raw '#' targets). The walked-in sub-unit's
// graph-derived trace must be re-attributed to its parent file, which then
// surfaces as the result row.
//
//	aaa/top (leaf cosine 1.0) -[references]-> bbb/other#P1
//
// take=1 * multiplier=1 seeds {aaa/top}; bbb/other (leaf cosine 0.6, outside
// the pool) inherits the sub-unit trace: graph = 1.0, final = 0.5 * 1.0.
func TestQueryRun_GraphExpansion_SubUnitPath_CrossFileSubUnitTarget(test *testing.T) {
	store := openTestStore(test)

	seedSubUnitFile(test, store, "aaa/top", "note", "Top", []float32{1, 0})
	seedSubUnitFile(test, store, "bbb/other", "note", "Other", []float32{0.6, 0.8})

	edges := index.NewEdgeRepo(store)

	if err := edges.UpsertAll("aaa/top", "aaa/top.md", []index.EdgeRow{
		{Type: "references", SourceID: "aaa/top", TargetID: "bbb/other#P1", SourcePath: "aaa/top.md", Kind: "direct"},
	}); err != nil {
		test.Fatalf("edge upsert: %v", err)
	}

	result, runErr := query.Run(context.Background(), subUnitDeps(store), query.Request{
		Filter:   "",
		Semantic: "anything",
		Take:     1,
		Skip:     1,
		Explain:  true,
		GraphExpansion: &manifest.GraphExpansion{
			Enabled:             true,
			Hops:                1,
			EdgeTypes:           []string{"references"},
			Weight:              0.5,
			CandidateMultiplier: 1,
		},
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	rows := rowsByID(test, result)

	other, hasOther := rows["bbb/other"]

	if !hasOther {
		test.Fatalf("bbb/other (parent of the walked-in sub-unit target) missing: %+v", result.Semantic.Ranked)
	}

	assertScore(test, "other score", other.Score, 0.5)
	assertScore(test, "other final_score", other.FinalScore, other.Score)
	assertScore(test, "other graph_score", other.GraphScore, 1.0)

	if other.Distance != 1 {
		test.Errorf("other distance = %d, want 1", other.Distance)
	}
}

// TestQueryRun_GraphExpansion_SubUnitPath_NoDoubleCountBelowMinScore pins the
// FilteredBelowMinScore accounting for a walked-in file whose ranked leaves
// were all MinScore-dropped: the file counts once (per dropped leaf), not
// again for its bare row.
//
//	aaa/top (leaf cosine 1.0) -[team]-> ccc/tail (leaf cosine 0.28)
//
// take=1 * multiplier=1 seeds {aaa/top}; w=0.2. tail's leaf final = w*1.0 =
// 0.2 < MinScore 0.4 (one count); its bare row must not add a second.
func TestQueryRun_GraphExpansion_SubUnitPath_NoDoubleCountBelowMinScore(test *testing.T) {
	store := openTestStore(test)

	seedSubUnitFile(test, store, "aaa/top", "note", "Top", []float32{1, 0})
	seedSubUnitFile(test, store, "ccc/tail", "note", "Tail", []float32{0.28, 0.96})

	edges := index.NewEdgeRepo(store)

	if err := edges.UpsertAll("aaa/top", "aaa/top.md", []index.EdgeRow{
		{Type: "team", SourceID: "aaa/top", TargetID: "ccc/tail", SourcePath: "aaa/top.md", Kind: "derived"},
	}); err != nil {
		test.Fatalf("edge upsert: %v", err)
	}

	result, runErr := query.Run(context.Background(), subUnitDeps(store), query.Request{
		Filter:   "",
		Semantic: "anything",
		Take:     1,
		MinScore: 0.4,
		GraphExpansion: &manifest.GraphExpansion{
			Enabled:             true,
			Hops:                1,
			EdgeTypes:           []string{"team"},
			Weight:              0.2,
			CandidateMultiplier: 1,
		},
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if ids := rankedIDs(result); len(ids) != 1 || ids[0] != "aaa/top" {
		test.Fatalf("ranked = %v, want [aaa/top]", ids)
	}

	if result.Semantic.FilteredBelowMinScore != 1 {
		test.Errorf("FilteredBelowMinScore = %d, want 1 (tail leaf only, no bare-row double count)", result.Semantic.FilteredBelowMinScore)
	}
}

func rankedIDs(result *query.Result) []string {
	ids := make([]string, 0, len(result.Semantic.Ranked))

	for _, row := range result.Semantic.Ranked {
		ids = append(ids, row.ID)
	}

	return ids
}
