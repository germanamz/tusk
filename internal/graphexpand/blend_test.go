package graphexpand_test

import (
	"math"
	"testing"

	"github.com/germanamz/tusk/internal/graphexpand"
)

const blendEpsilon = 1e-9

func approxEqual(left, right float64) bool {
	return math.Abs(left-right) < blendEpsilon
}

func TestBlender_EmptyInputs(test *testing.T) {
	blender := graphexpand.Blender{Weight: 0.3}

	out := blender.Score(nil, nil, nil)

	if len(out) != 0 {
		test.Errorf("len(out) = %d, want 0", len(out))
	}
}

func TestBlender_SingleCandidateNoNeighbors(test *testing.T) {
	candidates := []graphexpand.Candidate{
		{NodeID: "a", CosineScore: 0.8, Distance: 0},
	}
	seedScores := map[string]float64{"a": 0.8}

	blender := graphexpand.Blender{Weight: 0.25}
	out := blender.Score(candidates, nil, seedScores)

	if len(out) != 1 {
		test.Fatalf("len(out) = %d, want 1", len(out))
	}

	row := out[0]

	if row.NodeID != "a" {
		test.Errorf("NodeID = %q, want a", row.NodeID)
	}

	if !approxEqual(row.CosineScore, 0.8) {
		test.Errorf("CosineScore = %v, want 0.8", row.CosineScore)
	}

	if row.GraphScore != 0 {
		test.Errorf("GraphScore = %v, want 0 (no seed neighbors)", row.GraphScore)
	}

	want := 0.75 * 0.8 // (1 - 0.25) * 0.8
	if !approxEqual(row.FinalScore, want) {
		test.Errorf("FinalScore = %v, want %v", row.FinalScore, want)
	}
}

func TestBlender_TwoSeedsOneEdgeEachContributesToOther(test *testing.T) {
	// a and b are both seeds; one undirected edge a-b. Each candidate has the
	// other's cosine as graph_score.
	candidates := []graphexpand.Candidate{
		{NodeID: "a", CosineScore: 0.8, Distance: 0},
		{NodeID: "b", CosineScore: 0.6, Distance: 0},
	}
	edges := []graphexpand.NeighborEdge{
		{Source: "a", Target: "b", Type: "references"},
	}
	seedScores := map[string]float64{"a": 0.8, "b": 0.6}

	blender := graphexpand.Blender{Weight: 0.2}
	out := blender.Score(candidates, edges, seedScores)

	if len(out) != 2 {
		test.Fatalf("len(out) = %d, want 2", len(out))
	}

	scored := make(map[string]graphexpand.Scored, len(out))

	for _, row := range out {
		scored[row.NodeID] = row
	}

	wantA := 0.8*0.8 + 0.2*0.6 // 0.64 + 0.12 = 0.76
	wantB := 0.8*0.6 + 0.2*0.8 // 0.48 + 0.16 = 0.64

	if !approxEqual(scored["a"].GraphScore, 0.6) {
		test.Errorf("a graph_score = %v, want 0.6", scored["a"].GraphScore)
	}

	if !approxEqual(scored["b"].GraphScore, 0.8) {
		test.Errorf("b graph_score = %v, want 0.8", scored["b"].GraphScore)
	}

	if !approxEqual(scored["a"].FinalScore, wantA) {
		test.Errorf("a final = %v, want %v", scored["a"].FinalScore, wantA)
	}

	if !approxEqual(scored["b"].FinalScore, wantB) {
		test.Errorf("b final = %v, want %v", scored["b"].FinalScore, wantB)
	}

	// Sorted by FinalScore desc: a first.
	if out[0].NodeID != "a" {
		test.Errorf("out[0] = %q, want a", out[0].NodeID)
	}
}

func TestBlender_Hop1NeighborGraphScoreAvgOfSeedNeighbors(test *testing.T) {
	// Seeds: a (0.8), b (0.6). Neighbor n at distance 1 connected to both.
	// graph_score(n) = avg(0.8, 0.6) = 0.7
	candidates := []graphexpand.Candidate{
		{NodeID: "a", CosineScore: 0.8, Distance: 0},
		{NodeID: "b", CosineScore: 0.6, Distance: 0},
		{NodeID: "n", CosineScore: 0, Distance: 1},
	}
	edges := []graphexpand.NeighborEdge{
		{Source: "a", Target: "n", Type: "references"},
		{Source: "b", Target: "n", Type: "references"},
	}
	seedScores := map[string]float64{"a": 0.8, "b": 0.6}

	blender := graphexpand.Blender{Weight: 0.5}
	out := blender.Score(candidates, edges, seedScores)

	scored := make(map[string]graphexpand.Scored, len(out))

	for _, row := range out {
		scored[row.NodeID] = row
	}

	if !approxEqual(scored["n"].GraphScore, 0.7) {
		test.Errorf("n graph_score = %v, want 0.7", scored["n"].GraphScore)
	}

	wantN := 0.5*0 + 0.5*0.7 // cosine clipped to 0 for the neighbor
	if !approxEqual(scored["n"].FinalScore, wantN) {
		test.Errorf("n final = %v, want %v", scored["n"].FinalScore, wantN)
	}
}

func TestBlender_SeedWithoutSeedNeighborsGetsZeroGraphScore(test *testing.T) {
	// a is a seed; its only neighbor n is NOT in the seed set. graph_score(a)
	// is 0 because only seeds contribute to graph_score.
	candidates := []graphexpand.Candidate{
		{NodeID: "a", CosineScore: 0.8, Distance: 0},
		{NodeID: "n", CosineScore: 0, Distance: 1},
	}
	edges := []graphexpand.NeighborEdge{
		{Source: "a", Target: "n", Type: "references"},
	}
	seedScores := map[string]float64{"a": 0.8}

	blender := graphexpand.Blender{Weight: 0.3}
	out := blender.Score(candidates, edges, seedScores)

	scored := make(map[string]graphexpand.Scored, len(out))

	for _, row := range out {
		scored[row.NodeID] = row
	}

	if scored["a"].GraphScore != 0 {
		test.Errorf("a graph_score = %v, want 0", scored["a"].GraphScore)
	}

	wantA := 0.7 * 0.8 // (1-0.3) * 0.8
	if !approxEqual(scored["a"].FinalScore, wantA) {
		test.Errorf("a final = %v, want %v", scored["a"].FinalScore, wantA)
	}
}

func TestBlender_ClipsNegativeCosineToZero(test *testing.T) {
	candidates := []graphexpand.Candidate{
		{NodeID: "a", CosineScore: -0.2, Distance: 0},
	}
	seedScores := map[string]float64{"a": -0.2}

	blender := graphexpand.Blender{Weight: 0.2}
	out := blender.Score(candidates, nil, seedScores)

	if len(out) != 1 {
		test.Fatalf("len(out) = %d, want 1", len(out))
	}

	if out[0].CosineScore != 0 {
		test.Errorf("CosineScore = %v, want 0 (clipped)", out[0].CosineScore)
	}

	if out[0].FinalScore != 0 {
		test.Errorf("FinalScore = %v, want 0", out[0].FinalScore)
	}
}

func TestBlender_WeightZeroIsCosineOnly(test *testing.T) {
	candidates := []graphexpand.Candidate{
		{NodeID: "a", CosineScore: 0.8, Distance: 0},
		{NodeID: "b", CosineScore: 0.6, Distance: 0},
	}
	edges := []graphexpand.NeighborEdge{
		{Source: "a", Target: "b", Type: "references"},
	}
	seedScores := map[string]float64{"a": 0.8, "b": 0.6}

	blender := graphexpand.Blender{Weight: 0}
	out := blender.Score(candidates, edges, seedScores)

	scored := make(map[string]graphexpand.Scored, len(out))

	for _, row := range out {
		scored[row.NodeID] = row
	}

	if !approxEqual(scored["a"].FinalScore, 0.8) {
		test.Errorf("a final = %v, want 0.8", scored["a"].FinalScore)
	}

	if !approxEqual(scored["b"].FinalScore, 0.6) {
		test.Errorf("b final = %v, want 0.6", scored["b"].FinalScore)
	}
}

func TestBlender_WeightOneIsGraphOnly(test *testing.T) {
	candidates := []graphexpand.Candidate{
		{NodeID: "a", CosineScore: 0.8, Distance: 0},
		{NodeID: "b", CosineScore: 0.6, Distance: 0},
	}
	edges := []graphexpand.NeighborEdge{
		{Source: "a", Target: "b", Type: "references"},
	}
	seedScores := map[string]float64{"a": 0.8, "b": 0.6}

	blender := graphexpand.Blender{Weight: 1}
	out := blender.Score(candidates, edges, seedScores)

	scored := make(map[string]graphexpand.Scored, len(out))

	for _, row := range out {
		scored[row.NodeID] = row
	}

	if !approxEqual(scored["a"].FinalScore, 0.6) {
		test.Errorf("a final = %v, want 0.6 (b's cosine)", scored["a"].FinalScore)
	}

	if !approxEqual(scored["b"].FinalScore, 0.8) {
		test.Errorf("b final = %v, want 0.8 (a's cosine)", scored["b"].FinalScore)
	}
}

func TestBlender_DeterministicTieBreaking(test *testing.T) {
	// Two seeds with identical cosine, no edges → identical final. Ties
	// broken by (Distance asc, NodeID asc).
	candidates := []graphexpand.Candidate{
		{NodeID: "b", CosineScore: 0.5, Distance: 0},
		{NodeID: "a", CosineScore: 0.5, Distance: 0},
	}
	seedScores := map[string]float64{"a": 0.5, "b": 0.5}

	blender := graphexpand.Blender{Weight: 0.2}
	out := blender.Score(candidates, nil, seedScores)

	if out[0].NodeID != "a" || out[1].NodeID != "b" {
		test.Errorf("tie-break order = [%q,%q], want [a,b]", out[0].NodeID, out[1].NodeID)
	}
}

func TestBlender_DistanceTieBreakBeatsNodeID(test *testing.T) {
	// Same final score, different distances. Lower distance wins.
	candidates := []graphexpand.Candidate{
		{NodeID: "a", CosineScore: 0, Distance: 2},
		{NodeID: "z", CosineScore: 0, Distance: 1},
	}

	blender := graphexpand.Blender{Weight: 0.3}
	out := blender.Score(candidates, nil, map[string]float64{})

	if out[0].NodeID != "z" {
		test.Errorf("out[0] = %q, want z (lower distance)", out[0].NodeID)
	}
}
