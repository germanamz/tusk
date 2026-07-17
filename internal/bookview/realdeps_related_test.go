package bookview

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/graphexpand"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/typeref"
)

// ptrInt builds the presence-aware *int RelatedSource.Related takes for
// hops; a nil literal already expresses "absent" so no helper is needed for
// that case, and no test here needs an explicit non-nil weight override.
func ptrInt(value int) *int { return &value }

// seedRelatedGraph builds a three-file chain A -references-> B
// -references-> C, mirroring internal/graphexpand/walk_test.go's fixture
// shape: every node is a file (no ParentID), so nothing here is filtered by
// the adapter's sub-unit guard.
func seedRelatedGraph(test *testing.T, nodes *index.NodeRepo, edges *index.EdgeRepo) {
	test.Helper()

	for _, id := range []string{"A", "B", "C"} {
		if upsertErr := nodes.Upsert(index.NodeRow{
			ID: id, Type: "note", Path: id + ".md", Title: id,
			PropertiesJSON: "{}", LastChecksum: "x",
		}); upsertErr != nil {
			test.Fatalf("upsert %s: %v", id, upsertErr)
		}
	}

	if edgeErr := edges.UpsertAll("A", "A.md", []index.EdgeRow{
		{Type: "references", SourceID: "A", TargetID: "B", SourcePath: "A.md", Kind: "direct"},
	}); edgeErr != nil {
		test.Fatalf("upsert A edges: %v", edgeErr)
	}

	if edgeErr := edges.UpsertAll("B", "B.md", []index.EdgeRow{
		{Type: "references", SourceID: "B", TargetID: "C", SourcePath: "B.md", Kind: "direct"},
	}); edgeErr != nil {
		test.Fatalf("upsert B edges: %v", edgeErr)
	}
}

// TestRelatedAdapter_RanksNeighborsByDistanceWithExactScores is the brief's
// Step 1 scenario plus the controller's derived blend arithmetic: with the
// manifest's default Weight (0.2), B (distance 1) must carry GraphScore 1.0
// (the undecayed mean seed-neighbor cosine) and C (distance 2) must carry
// GraphScore 0.2 (Weight * the distance-1 propagated score). The seed A must
// be absent from the output.
func TestRelatedAdapter_RanksNeighborsByDistanceWithExactScores(test *testing.T) {
	store := openTestStore(test)

	nodes := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)

	seedRelatedGraph(test, nodes, edges)

	loaded := newTestManifest(manifest.GraphExpansion{
		Enabled: false, Hops: 1, EdgeTypes: []string{"references"},
		Weight: 0.2, CandidateMultiplier: 5,
	})

	related := NewRelated(edges, loaded, nodes)

	resp, relatedErr := related.Related(context.Background(), "A", ptrInt(2), nil, nil)

	if relatedErr != nil {
		test.Fatalf("Related: %v", relatedErr)
	}

	if len(resp.Related) != 2 {
		test.Fatalf("Related=%+v want exactly 2 entries (B, C)", resp.Related)
	}

	for _, node := range resp.Related {
		if node.ID == "A" {
			test.Fatalf("seed A present in Related: %+v", resp.Related)
		}
	}

	if resp.Related[0].ID != "B" || resp.Related[1].ID != "C" {
		test.Fatalf("Related order = %+v, want [B, C]", resp.Related)
	}

	if resp.Related[0].Distance != 1 {
		test.Errorf("B.Distance = %d, want 1", resp.Related[0].Distance)
	}

	if resp.Related[1].Distance != 2 {
		test.Errorf("C.Distance = %d, want 2", resp.Related[1].Distance)
	}

	const epsilon = 1e-9

	if diff := resp.Related[0].GraphScore - 1.0; math.Abs(diff) > epsilon {
		test.Errorf("B.GraphScore = %v, want 1.0 (mean seed-neighbor cosine, undecayed at distance<=1)", resp.Related[0].GraphScore)
	}

	if diff := resp.Related[1].GraphScore - 0.2; math.Abs(diff) > epsilon {
		test.Errorf("C.GraphScore = %v, want 0.2 (Weight * distance-1 propagated score)", resp.Related[1].GraphScore)
	}
}

// TestRelatedBlendDerivation_FinalScoresMatchDerivedFormula white-box checks
// the controller's derivation directly against graphexpand.Blender.Score,
// independent of RelatedNode's wire shape (which carries GraphScore/Distance
// but not FinalScore). Confirms: seed FinalScore = 1-W, distance-1 FinalScore
// = W, distance-2 FinalScore = W^2, and that the seed's score exceeds every
// neighbor's — the reason dropping the seed in the adapter is mandatory, not
// cosmetic.
func TestRelatedBlendDerivation_FinalScoresMatchDerivedFormula(test *testing.T) {
	store := openTestStore(test)

	nodes := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)

	seedRelatedGraph(test, nodes, edges)

	refs, parseErr := typeref.ParseMany([]string{"references"})

	if parseErr != nil {
		test.Fatalf("ParseMany: %v", parseErr)
	}

	walker := graphexpand.NewWalker(edges, refs, 2)

	seeds := []graphexpand.Candidate{{NodeID: "A", CosineScore: 1.0, Distance: 0}}

	candidates, neighborEdges, expandErr := walker.Expand(context.Background(), seeds)

	if expandErr != nil {
		test.Fatalf("Expand: %v", expandErr)
	}

	const weight = 0.2

	blender := graphexpand.Blender{Weight: weight}
	scored := blender.Score(candidates, neighborEdges, map[string]float64{"A": 1.0})

	byID := make(map[string]graphexpand.Scored, len(scored))

	for _, entry := range scored {
		byID[entry.NodeID] = entry
	}

	const epsilon = 1e-9

	seedScore, seedOK := byID["A"]

	if !seedOK {
		test.Fatalf("seed A missing from scored: %+v", scored)
	}

	if diff := seedScore.FinalScore - (1 - weight); math.Abs(diff) > epsilon {
		test.Errorf("A.FinalScore = %v, want 1-W = %v", seedScore.FinalScore, 1-weight)
	}

	bScore, bOK := byID["B"]

	if !bOK {
		test.Fatalf("B missing from scored: %+v", scored)
	}

	if diff := bScore.FinalScore - weight; math.Abs(diff) > epsilon {
		test.Errorf("B.FinalScore = %v, want W = %v", bScore.FinalScore, weight)
	}

	if diff := bScore.GraphScore - 1.0; math.Abs(diff) > epsilon {
		test.Errorf("B.GraphScore = %v, want 1.0", bScore.GraphScore)
	}

	cScore, cOK := byID["C"]

	if !cOK {
		test.Fatalf("C missing from scored: %+v", scored)
	}

	if diff := cScore.FinalScore - weight*weight; math.Abs(diff) > epsilon {
		test.Errorf("C.FinalScore = %v, want W^2 = %v", cScore.FinalScore, weight*weight)
	}

	if diff := cScore.GraphScore - weight; math.Abs(diff) > epsilon {
		test.Errorf("C.GraphScore = %v, want W = %v", cScore.GraphScore, weight)
	}

	if seedScore.FinalScore <= bScore.FinalScore || seedScore.FinalScore <= cScore.FinalScore {
		test.Errorf("seed FinalScore=%v should exceed every neighbor (B=%v, C=%v) — dropping the seed is mandatory", seedScore.FinalScore, bScore.FinalScore, cScore.FinalScore)
	}
}

// TestRelatedAdapter_NoEmbedderRequired proves the spec property that the
// Related rail is embedder-free: no embed.Embedder or index.EmbeddingRepo is
// constructed anywhere in this test (NewRelated's signature has no room for
// one), yet neighbors resolve purely from the edge graph.
func TestRelatedAdapter_NoEmbedderRequired(test *testing.T) {
	store := openTestStore(test)

	nodes := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)

	seedRelatedGraph(test, nodes, edges)

	loaded := newTestManifest(manifest.DefaultGraphExpansion())

	related := NewRelated(edges, loaded, nodes)

	resp, relatedErr := related.Related(context.Background(), "A", ptrInt(2), nil, nil)

	if relatedErr != nil {
		test.Fatalf("Related: %v (should succeed with no embedder wired anywhere)", relatedErr)
	}

	if len(resp.Related) != 2 {
		test.Fatalf("Related=%+v want [B, C] reachable via edges alone", resp.Related)
	}
}

// TestRelatedAdapter_AbsentWeightUsesManifestDefault pins the presence rule:
// a nil weight must inherit the manifest's configured Weight, never collapse
// to 0. The fixture uses Hops=2 (from the manifest, hops left nil too) so C's
// GraphScore (Weight * propagated score) reads back the effective weight
// directly — a bug that forced weight to &0.0 when absent would make this
// read 0 instead of manifestWeight.
func TestRelatedAdapter_AbsentWeightUsesManifestDefault(test *testing.T) {
	store := openTestStore(test)

	nodes := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)

	seedRelatedGraph(test, nodes, edges)

	const manifestWeight = 0.37 // distinct from 0 and from the hard-coded 0.2 default

	loaded := newTestManifest(manifest.GraphExpansion{
		Enabled: false, Hops: 2, EdgeTypes: []string{"references"},
		Weight: manifestWeight, CandidateMultiplier: 5,
	})

	related := NewRelated(edges, loaded, nodes)

	resp, relatedErr := related.Related(context.Background(), "A", nil, nil, nil)

	if relatedErr != nil {
		test.Fatalf("Related: %v", relatedErr)
	}

	if len(resp.Related) != 2 {
		test.Fatalf("Related=%+v want exactly [B, C] (manifest Hops=2)", resp.Related)
	}

	const epsilon = 1e-9

	diff := resp.Related[1].GraphScore - manifestWeight

	if diff < 0 {
		diff = -diff
	}

	if diff > epsilon {
		test.Errorf("C.GraphScore = %v, want manifest weight %v (adapter must not default absent weight to 0)", resp.Related[1].GraphScore, manifestWeight)
	}

	if resp.Related[1].GraphScore == 0 {
		test.Fatalf("C.GraphScore == 0: presence rule failed — absent weight overrode the manifest default with 0")
	}
}

// TestRelatedAdapter_SkipsSubUnitAndDanglingFarEnds seeds A with three
// outgoing edges: one to a real file (B), one to another file's sub-unit
// (P#S1), and one to an id with no node row at all (a dangling edge). Only B
// should survive to the Related output.
func TestRelatedAdapter_SkipsSubUnitAndDanglingFarEnds(test *testing.T) {
	store := openTestStore(test)

	nodes := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)

	for _, id := range []string{"A", "B", "P"} {
		if upsertErr := nodes.Upsert(index.NodeRow{
			ID: id, Type: "note", Path: id + ".md", Title: id,
			PropertiesJSON: "{}", LastChecksum: "x",
		}); upsertErr != nil {
			test.Fatalf("upsert %s: %v", id, upsertErr)
		}
	}

	if bulkErr := nodes.BulkUpsert([]index.NodeRow{{
		ID: "P#S1", Type: "note", Path: "P.md", Title: "P S1",
		PropertiesJSON: "{}", LastChecksum: "x",
		ParentID: sql.NullString{String: "P", Valid: true},
	}}, "markdown"); bulkErr != nil {
		test.Fatalf("bulk upsert P#S1: %v", bulkErr)
	}

	if edgeErr := edges.UpsertAll("A", "A.md", []index.EdgeRow{
		{Type: "references", SourceID: "A", TargetID: "B", SourcePath: "A.md", Kind: "direct"},
		{Type: "references", SourceID: "A", TargetID: "P#S1", SourcePath: "A.md", Kind: "direct"},
		{Type: "references", SourceID: "A", TargetID: "ghost", SourcePath: "A.md", Kind: "direct"},
	}); edgeErr != nil {
		test.Fatalf("upsert A edges: %v", edgeErr)
	}

	loaded := newTestManifest(manifest.GraphExpansion{
		Enabled: false, Hops: 1, EdgeTypes: []string{"references"},
		Weight: 0.2, CandidateMultiplier: 5,
	})

	related := NewRelated(edges, loaded, nodes)

	resp, relatedErr := related.Related(context.Background(), "A", nil, nil, nil)

	if relatedErr != nil {
		test.Fatalf("Related: %v", relatedErr)
	}

	if len(resp.Related) != 1 || resp.Related[0].ID != "B" {
		test.Fatalf("Related=%+v want exactly [B] (P#S1 sub-unit and ghost dangling both skipped)", resp.Related)
	}
}

// TestRelatedAdapter_NoNeighborsMarshalsEmptyArray guards the nil-slice trap
// independently of handleRelated's own defensive guard: an isolated node with
// no edges must still produce a non-nil RelatedResponse.Related that
// marshals "related" as [], never null.
func TestRelatedAdapter_NoNeighborsMarshalsEmptyArray(test *testing.T) {
	store := openTestStore(test)

	nodes := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)

	if upsertErr := nodes.Upsert(index.NodeRow{
		ID: "A", Type: "note", Path: "A.md", Title: "A",
		PropertiesJSON: "{}", LastChecksum: "x",
	}); upsertErr != nil {
		test.Fatalf("upsert A: %v", upsertErr)
	}

	loaded := newTestManifest(manifest.DefaultGraphExpansion())

	related := NewRelated(edges, loaded, nodes)

	resp, relatedErr := related.Related(context.Background(), "A", nil, nil, nil)

	if relatedErr != nil {
		test.Fatalf("Related: %v", relatedErr)
	}

	if resp.Related == nil {
		test.Fatalf("Related is nil, want a non-nil empty slice")
	}

	encoded, marshalErr := json.Marshal(resp)

	if marshalErr != nil {
		test.Fatalf("marshal: %v", marshalErr)
	}

	if !strings.Contains(string(encoded), `"related":[]`) {
		test.Fatalf("json=%s want related serialized as []", encoded)
	}
}
