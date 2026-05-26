package graphexpand_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/graphexpand"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/typeref"
)

func refsAny(types ...string) []typeref.EdgeRef {
	refs := make([]typeref.EdgeRef, len(types))
	for index, edgeType := range types {
		refs[index] = typeref.EdgeRef{Scope: typeref.ScopeAny, Type: edgeType}
	}
	return refs
}

// fixtureGraph builds a small graph used across walker tests:
//
//	f1 -references-> f2
//	f1 -references-> f3
//	f3 -references-> f4
//	f4 -references-> f5
//	f1 -contains-> s1..s3
//	f2 -contains-> s4..s5
//	f3 -contains-> s6..s8
//	f4 -contains-> s9
//	f5 -contains-> s10
//
// Returns the EdgeRepo holding these edges plus the node ids.
func fixtureGraph(test *testing.T) *index.EdgeRepo {
	test.Helper()

	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	nodeRepo := index.NewNodeRepo(store)

	fileIDs := []string{"f1", "f2", "f3", "f4", "f5"}
	subUnitIDs := []string{"s1", "s2", "s3", "s4", "s5", "s6", "s7", "s8", "s9", "s10"}

	for _, nodeID := range append(append([]string{}, fileIDs...), subUnitIDs...) {
		row := index.NodeRow{
			ID:             nodeID,
			Type:           "note",
			Path:           nodeID + ".md",
			Title:          nodeID,
			PropertiesJSON: "{}",
			LastChecksum:   "x",
		}

		if upsertErr := nodeRepo.Upsert(row); upsertErr != nil {
			test.Fatalf("upsert %s: %v", nodeID, upsertErr)
		}
	}

	edgeRepo := index.NewEdgeRepo(store)

	references := map[string][]index.EdgeRow{
		"f1": {
			{Type: "references", SourceID: "f1", TargetID: "f2", SourcePath: "f1.md", Kind: "direct"},
			{Type: "references", SourceID: "f1", TargetID: "f3", SourcePath: "f1.md", Kind: "direct"},
		},
		"f3": {
			{Type: "references", SourceID: "f3", TargetID: "f4", SourcePath: "f3.md", Kind: "direct"},
		},
		"f4": {
			{Type: "references", SourceID: "f4", TargetID: "f5", SourcePath: "f4.md", Kind: "direct"},
		},
	}

	contains := map[string][]string{
		"f1": {"s1", "s2", "s3"},
		"f2": {"s4", "s5"},
		"f3": {"s6", "s7", "s8"},
		"f4": {"s9"},
		"f5": {"s10"},
	}

	for fileID, edges := range references {
		merged := append([]index.EdgeRow{}, edges...)

		for _, subID := range contains[fileID] {
			merged = append(merged, index.EdgeRow{
				Type:       "contains",
				SourceID:   fileID,
				TargetID:   subID,
				SourcePath: fileID + ".md",
				Kind:       "structural",
				Source:     sql.NullString{String: "markdown", Valid: true},
			})
		}

		if upsertErr := edgeRepo.UpsertAll(fileID, fileID+".md", merged); upsertErr != nil {
			test.Fatalf("upsert edges for %s: %v", fileID, upsertErr)
		}
	}

	// f2 and f5 only have contains edges, no references; write them too.
	for _, fileID := range []string{"f2", "f5"} {
		var edges []index.EdgeRow

		for _, subID := range contains[fileID] {
			edges = append(edges, index.EdgeRow{
				Type:       "contains",
				SourceID:   fileID,
				TargetID:   subID,
				SourcePath: fileID + ".md",
				Kind:       "structural",
				Source:     sql.NullString{String: "markdown", Valid: true},
			})
		}

		if upsertErr := edgeRepo.UpsertAll(fileID, fileID+".md", edges); upsertErr != nil {
			test.Fatalf("upsert contains for %s: %v", fileID, upsertErr)
		}
	}

	return edgeRepo
}

func candidateIDs(candidates []graphexpand.Candidate) []string {
	ids := make([]string, 0, len(candidates))

	for _, candidate := range candidates {
		ids = append(ids, candidate.NodeID)
	}

	return ids
}

func candidateByID(test *testing.T, candidates []graphexpand.Candidate, nodeID string) graphexpand.Candidate {
	test.Helper()

	for _, candidate := range candidates {
		if candidate.NodeID == nodeID {
			return candidate
		}
	}

	test.Fatalf("candidate %s not found in %+v", nodeID, candidates)

	return graphexpand.Candidate{}
}

func TestWalker_OneHop_ReferencesOnly(test *testing.T) {
	edgeRepo := fixtureGraph(test)

	walker := graphexpand.NewWalker(edgeRepo, refsAny("references"), 1)

	seeds := []graphexpand.Candidate{
		{NodeID: "f1", CosineScore: 0.9, Distance: 0},
		{NodeID: "f3", CosineScore: 0.7, Distance: 0},
	}

	candidates, edges, expandErr := walker.Expand(context.Background(), seeds)

	if expandErr != nil {
		test.Fatalf("Expand: %v", expandErr)
	}

	// Expect seeds plus f2 (via f1) and f4 (via f3). f3 is already a seed so
	// the f1->f3 edge doesn't add a candidate but does contribute an edge.
	gotIDs := candidateIDs(candidates)
	wantIDs := []string{"f1", "f3", "f2", "f4"}

	if len(gotIDs) != len(wantIDs) {
		test.Fatalf("candidates = %v, want subset %v", gotIDs, wantIDs)
	}

	idSet := map[string]bool{}

	for _, candidateID := range gotIDs {
		idSet[candidateID] = true
	}

	for _, wantID := range wantIDs {
		if !idSet[wantID] {
			test.Errorf("missing candidate %s (got %v)", wantID, gotIDs)
		}
	}

	// Seeds keep their cosine score and Distance=0.
	f1Candidate := candidateByID(test, candidates, "f1")

	if f1Candidate.Distance != 0 || f1Candidate.CosineScore != 0.9 {
		test.Errorf("f1 candidate = %+v, want distance=0 cosine=0.9", f1Candidate)
	}

	f3Candidate := candidateByID(test, candidates, "f3")

	if f3Candidate.Distance != 0 || f3Candidate.CosineScore != 0.7 {
		test.Errorf("f3 candidate = %+v, want distance=0 cosine=0.7", f3Candidate)
	}

	// New neighbors get cosine=0 and distance=1.
	f2Candidate := candidateByID(test, candidates, "f2")

	if f2Candidate.Distance != 1 || f2Candidate.CosineScore != 0 {
		test.Errorf("f2 candidate = %+v, want distance=1 cosine=0", f2Candidate)
	}

	f4Candidate := candidateByID(test, candidates, "f4")

	if f4Candidate.Distance != 1 || f4Candidate.CosineScore != 0 {
		test.Errorf("f4 candidate = %+v, want distance=1 cosine=0", f4Candidate)
	}

	// Edge set: f1-f2, f1-f3, f3-f4. All references type. Dedup means a
	// single entry for f1-f3 even though both endpoints are seeds.
	if len(edges) != 3 {
		test.Fatalf("edges = %+v, want 3", edges)
	}

	wantEdges := map[string]bool{
		"references|f1|f2": true,
		"references|f1|f3": true,
		"references|f3|f4": true,
	}

	for _, edge := range edges {
		key := edge.Type + "|" + edge.Source + "|" + edge.Target

		if !wantEdges[key] {
			test.Errorf("unexpected edge %s", key)
		}
	}
}

func TestWalker_OneHop_ReferencesAndContains(test *testing.T) {
	edgeRepo := fixtureGraph(test)

	walker := graphexpand.NewWalker(edgeRepo, refsAny("references", "contains"), 1)

	seeds := []graphexpand.Candidate{
		{NodeID: "f1", CosineScore: 0.9, Distance: 0},
		{NodeID: "f3", CosineScore: 0.7, Distance: 0},
	}

	candidates, _, expandErr := walker.Expand(context.Background(), seeds)

	if expandErr != nil {
		test.Fatalf("Expand: %v", expandErr)
	}

	idSet := map[string]bool{}

	for _, candidate := range candidates {
		idSet[candidate.NodeID] = true
	}

	// Expect seeds, references neighbors, and sub-units of f1 and f3.
	for _, wantID := range []string{"f1", "f3", "f2", "f4", "s1", "s2", "s3", "s6", "s7", "s8"} {
		if !idSet[wantID] {
			test.Errorf("missing candidate %s in %v", wantID, candidateIDs(candidates))
		}
	}

	// Sub-units that are NOT attached to f1 or f3 must not appear.
	for _, unwantedID := range []string{"s4", "s5", "s9", "s10", "f5"} {
		if idSet[unwantedID] {
			test.Errorf("unexpected candidate %s", unwantedID)
		}
	}
}

func TestWalker_TwoHop_References(test *testing.T) {
	edgeRepo := fixtureGraph(test)

	walker := graphexpand.NewWalker(edgeRepo, refsAny("references"), 2)

	seeds := []graphexpand.Candidate{
		{NodeID: "f1", CosineScore: 0.9, Distance: 0},
		{NodeID: "f3", CosineScore: 0.7, Distance: 0},
	}

	candidates, _, expandErr := walker.Expand(context.Background(), seeds)

	if expandErr != nil {
		test.Fatalf("Expand: %v", expandErr)
	}

	// hop-2 should add f5 via f4.
	f5Candidate := candidateByID(test, candidates, "f5")

	if f5Candidate.Distance != 2 {
		test.Errorf("f5 distance = %d, want 2", f5Candidate.Distance)
	}

	// f4 is reached at hop 1; it must NOT regress to distance 2.
	f4Candidate := candidateByID(test, candidates, "f4")

	if f4Candidate.Distance != 1 {
		test.Errorf("f4 distance = %d, want 1", f4Candidate.Distance)
	}

	// f2 is at distance 1; ensure it stays at distance 1 even though it
	// could be re-reached via the hop-2 union containing f1.
	f2Candidate := candidateByID(test, candidates, "f2")

	if f2Candidate.Distance != 1 {
		test.Errorf("f2 distance = %d, want 1", f2Candidate.Distance)
	}
}

func TestWalker_DeterministicOrdering(test *testing.T) {
	edgeRepo := fixtureGraph(test)

	walker := graphexpand.NewWalker(edgeRepo, refsAny("references"), 2)

	seeds := []graphexpand.Candidate{
		{NodeID: "f3", CosineScore: 0.7, Distance: 0},
		{NodeID: "f1", CosineScore: 0.9, Distance: 0},
	}

	candidates, edges, expandErr := walker.Expand(context.Background(), seeds)

	if expandErr != nil {
		test.Fatalf("Expand: %v", expandErr)
	}

	// Candidates: distance asc, then NodeID asc.
	for index := 1; index < len(candidates); index++ {
		prev := candidates[index-1]
		curr := candidates[index]

		if prev.Distance > curr.Distance {
			test.Errorf("candidates not sorted by distance: %+v then %+v", prev, curr)
		}

		if prev.Distance == curr.Distance && prev.NodeID > curr.NodeID {
			test.Errorf("candidates not sorted by NodeID within distance: %+v then %+v", prev, curr)
		}
	}

	// Edges: Type, Source, Target ascending.
	for index := 1; index < len(edges); index++ {
		prev := edges[index-1]
		curr := edges[index]

		if prev.Type > curr.Type {
			test.Errorf("edges not sorted by type")
		}

		if prev.Type == curr.Type && prev.Source > curr.Source {
			test.Errorf("edges not sorted by source within type")
		}

		if prev.Type == curr.Type && prev.Source == curr.Source && prev.Target > curr.Target {
			test.Errorf("edges not sorted by target within source")
		}
	}
}

func TestWalker_UnknownEdgeTypeProducesNoRows(test *testing.T) {
	edgeRepo := fixtureGraph(test)

	walker := graphexpand.NewWalker(edgeRepo, refsAny("no-such-type"), 1)

	seeds := []graphexpand.Candidate{
		{NodeID: "f1", CosineScore: 1.0, Distance: 0},
	}

	candidates, edges, expandErr := walker.Expand(context.Background(), seeds)

	if expandErr != nil {
		test.Fatalf("Expand: %v", expandErr)
	}

	if len(candidates) != 1 || candidates[0].NodeID != "f1" {
		test.Errorf("candidates = %+v, want only seed f1", candidates)
	}

	if len(edges) != 0 {
		test.Errorf("edges = %+v, want empty", edges)
	}
}

func TestWalker_ContextCancellation(test *testing.T) {
	edgeRepo := fixtureGraph(test)

	walker := graphexpand.NewWalker(edgeRepo, refsAny("references"), 2)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, expandErr := walker.Expand(ctx, []graphexpand.Candidate{
		{NodeID: "f1", CosineScore: 1.0, Distance: 0},
	})

	if expandErr == nil {
		test.Fatalf("Expand: want context cancel error, got nil")
	}

	if expandErr != context.Canceled {
		test.Errorf("Expand err = %v, want context.Canceled", expandErr)
	}
}

func TestWalker_EmptySeeds(test *testing.T) {
	edgeRepo := fixtureGraph(test)

	walker := graphexpand.NewWalker(edgeRepo, refsAny("references"), 1)

	candidates, edges, expandErr := walker.Expand(context.Background(), nil)

	if expandErr != nil {
		test.Fatalf("Expand: %v", expandErr)
	}

	if len(candidates) != 0 {
		test.Errorf("candidates = %+v, want empty", candidates)
	}

	if len(edges) != 0 {
		test.Errorf("edges = %+v, want empty", edges)
	}
}

func TestWalker_DefensiveCopyEdgeRefs(test *testing.T) {
	edgeRepo := fixtureGraph(test)

	edgeRefs := refsAny("references")
	walker := graphexpand.NewWalker(edgeRepo, edgeRefs, 1)

	// Mutate the caller's slice — walker must be unaffected.
	edgeRefs[0] = typeref.EdgeRef{Scope: typeref.ScopeAny, Type: "mutated"}

	candidates, _, expandErr := walker.Expand(context.Background(), []graphexpand.Candidate{
		{NodeID: "f1", CosineScore: 0.9, Distance: 0},
	})

	if expandErr != nil {
		test.Fatalf("Expand: %v", expandErr)
	}

	idSet := map[string]bool{}

	for _, candidate := range candidates {
		idSet[candidate.NodeID] = true
	}

	if !idSet["f2"] || !idSet["f3"] {
		test.Errorf("walker did not defensively copy edge refs; candidates = %v", candidateIDs(candidates))
	}
}

// TestWalker_ScopeSourceFiltersUserEdges confirms that a ScopeSource ref
// matches only edges with edges.source equal to that source — a
// user-direct edge sharing the same type must not contribute.
func TestWalker_ScopeSourceFiltersUserEdges(test *testing.T) {
	store, openErr := index.Open(filepath.Join(test.TempDir(), "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	nodeRepo := index.NewNodeRepo(store)

	for _, nodeID := range []string{"f1", "structural-target", "user-target"} {
		if upsertErr := nodeRepo.Upsert(index.NodeRow{
			ID:             nodeID,
			Type:           "note",
			Path:           nodeID + ".md",
			Title:          nodeID,
			PropertiesJSON: "{}",
			LastChecksum:   "x",
		}); upsertErr != nil {
			test.Fatalf("upsert %s: %v", nodeID, upsertErr)
		}
	}

	edgeRepo := index.NewEdgeRepo(store)

	if upsertErr := edgeRepo.UpsertAll("f1", "f1.md", []index.EdgeRow{
		// Source-scoped (edges.source = "markdown").
		{Type: "contains", SourceID: "f1", TargetID: "structural-target", SourcePath: "f1.md", Kind: "structural", Source: sql.NullString{String: "markdown", Valid: true}},
		// User-direct (edges.source IS NULL) — same type, different scope.
		{Type: "contains", SourceID: "f1", TargetID: "user-target", SourcePath: "f1.md", Kind: "direct"},
	}); upsertErr != nil {
		test.Fatalf("upsert edges: %v", upsertErr)
	}

	walker := graphexpand.NewWalker(edgeRepo, []typeref.EdgeRef{
		{Scope: typeref.ScopeSource, Source: "markdown", Type: "contains"},
	}, 1)

	candidates, _, expandErr := walker.Expand(context.Background(), []graphexpand.Candidate{
		{NodeID: "f1", CosineScore: 0.9, Distance: 0},
	})

	if expandErr != nil {
		test.Fatalf("Expand: %v", expandErr)
	}

	idSet := map[string]bool{}

	for _, candidate := range candidates {
		idSet[candidate.NodeID] = true
	}

	if !idSet["structural-target"] {
		test.Errorf("missing markdown-scope candidate structural-target in %v", candidateIDs(candidates))
	}

	if idSet["user-target"] {
		test.Errorf("unexpected user-direct candidate user-target — ref scoped to source='markdown'")
	}
}
