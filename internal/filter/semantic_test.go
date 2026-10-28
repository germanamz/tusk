package filter_test

import (
	"math"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/filter"
)

func TestSemanticRank_OrdersByDescendingCosine(test *testing.T) {
	candidates := []filter.SemanticCandidate{
		{NodeID: "far", Vector: []float32{0, 1, 0}},
		{NodeID: "close", Vector: []float32{1, 0, 0}},
		{NodeID: "medium", Vector: []float32{0.7, 0.7, 0}},
	}

	query := []float32{1, 0, 0}

	ranked := filter.SemanticRank(candidates, query, filter.Fusion{})

	if len(ranked) != 3 {
		test.Fatalf("len = %d", len(ranked))
	}

	if ranked[0].NodeID != "close" {
		test.Errorf("ranked[0] = %q, want close", ranked[0].NodeID)
	}

	if ranked[len(ranked)-1].NodeID != "far" {
		test.Errorf("last = %q, want far", ranked[len(ranked)-1].NodeID)
	}
}

func TestSemanticRank_HandlesEmptyCandidates(test *testing.T) {
	ranked := filter.SemanticRank(nil, []float32{1, 0}, filter.Fusion{})

	if len(ranked) != 0 {
		test.Errorf("len = %d", len(ranked))
	}
}

func TestSemanticRank_SkipsCandidatesWithMismatchedDim(test *testing.T) {
	candidates := []filter.SemanticCandidate{
		{NodeID: "good", Vector: []float32{1, 0}},
		{NodeID: "bad", Vector: []float32{1, 0, 0}},
	}

	ranked := filter.SemanticRank(candidates, []float32{1, 0}, filter.Fusion{})

	if len(ranked) != 1 || ranked[0].NodeID != "good" {
		test.Errorf("ranked = %+v, want only good", ranked)
	}
}

func TestSemanticRank_MaxPerNodeAcrossChunks(test *testing.T) {
	candidates := []filter.SemanticCandidate{
		{NodeID: "alpha", Vector: []float32{0.1, 0, 0}},   // weak
		{NodeID: "alpha", Vector: []float32{1, 0, 0}},     // strong — should win for alpha
		{NodeID: "bravo", Vector: []float32{0.5, 0.5, 0}}, // medium
		{NodeID: "bravo", Vector: []float32{0.6, 0.5, 0}}, // slightly stronger
	}

	ranked := filter.SemanticRank(candidates, []float32{1, 0, 0}, filter.Fusion{})

	if len(ranked) != 2 {
		test.Fatalf("expected 2 unique nodes, got %d: %+v", len(ranked), ranked)
	}

	if ranked[0].NodeID != "alpha" {
		test.Errorf("alpha's strong chunk should rank first; got %+v", ranked)
	}

	// Bravo's score must equal the higher of its two chunks (chunk 1, not chunk 0).
	chunk1Score := embed.CosineSimilarity([]float32{0.6, 0.5, 0}, []float32{1, 0, 0})

	for _, result := range ranked {
		if result.NodeID == "bravo" && result.Score != chunk1Score {
			test.Errorf("bravo.Score = %v, want %v (max-per-node)", result.Score, chunk1Score)
		}
	}
}

func TestSemanticRank_TracksBestChunkBody(test *testing.T) {
	query := []float32{1, 0}

	candidates := []filter.SemanticCandidate{
		{NodeID: "n1", Vector: []float32{0.1, 1}, Body: "low score body"},
		{NodeID: "n1", Vector: []float32{1, 0}, Body: "high score body"},
		{NodeID: "n2", Vector: []float32{0, 1}, Body: "n2 only chunk"},
	}

	ranked := filter.SemanticRank(candidates, query, filter.Fusion{})

	if len(ranked) != 2 {
		test.Fatalf("len = %d, want 2", len(ranked))
	}

	first := ranked[0]

	if first.NodeID != "n1" {
		test.Errorf("first.NodeID = %q, want n1", first.NodeID)
	}

	if first.BestChunkBody != "high score body" {
		test.Errorf("BestChunkBody = %q", first.BestChunkBody)
	}
}

func TestSemanticRank_LexicalLiftsWeakerCosine(test *testing.T) {
	query := []float32{1, 0}

	candidates := []filter.SemanticCandidate{
		{NodeID: "close", Vector: []float32{0.8, 0.6}},
		{NodeID: "worded", Vector: []float32{0.7, 0.71}, Lexical: 1},
	}

	unweighted := filter.SemanticRank(candidates, query, filter.Fusion{})

	if unweighted[0].NodeID != "close" {
		test.Fatalf("weight 0 should rank by cosine alone; got %+v", unweighted)
	}

	weighted := filter.SemanticRank(candidates, query, filter.Fusion{Lexical: 0.5})

	if weighted[0].NodeID != "worded" {
		test.Fatalf("weight 0.5 should lift the lexical match first; got %+v", weighted)
	}

	cosine := embed.CosineSimilarity(candidates[1].Vector, query)
	want := cosine + 0.5*1*(1-cosine)

	if weighted[0].Score != want {
		test.Errorf("Score = %v, want %v (cosine + weight*lexical*(1-cosine))", weighted[0].Score, want)
	}

	if weighted[0].Cosine != cosine || weighted[0].Lexical != 1 {
		test.Errorf("Cosine, Lexical = %v, %v; want %v, 1", weighted[0].Cosine, weighted[0].Lexical, cosine)
	}
}

func TestSemanticRank_LexicalNeverLowersAndStaysAtMostOne(test *testing.T) {
	query := []float32{1, 0}

	for _, lexicalScore := range []float64{0, 0.3, 1} {
		for _, vector := range [][]float32{{1, 0}, {0.6, 0.8}, {-0.6, 0.8}} {
			candidates := []filter.SemanticCandidate{{NodeID: "only", Vector: vector, Lexical: lexicalScore}}
			cosine := embed.CosineSimilarity(vector, query)
			score := filter.SemanticRank(candidates, query, filter.Fusion{Lexical: 1})[0].Score

			if score < cosine || score > 1 {
				test.Errorf("cosine %v lexical %v: score %v, want in [cosine, 1]", cosine, lexicalScore, score)
			}
		}
	}
}

func TestSemanticRank_TitleMatchLiftsNamedNode(test *testing.T) {
	query := []float32{1, 0}

	candidates := []filter.SemanticCandidate{
		{NodeID: "mentions", Vector: []float32{0.71, 0.704}, Lexical: 1},
		{NodeID: "named", Vector: []float32{0.6, 0.8}, Lexical: 0.3, Title: 1},
	}

	cosineOnly := filter.SemanticRank(candidates, query, filter.Fusion{Lexical: 0.5})

	if cosineOnly[0].NodeID != "mentions" {
		test.Fatalf("without the title match the closer, wordier node should lead; got %+v", cosineOnly)
	}

	ranked := filter.SemanticRank(candidates, query, filter.Fusion{Lexical: 0.5, Title: 0.7})

	if ranked[0].NodeID != "named" {
		test.Fatalf("title weight 0.7 should lift the named node first; got %+v", ranked)
	}

	cosine := embed.CosineSimilarity(candidates[1].Vector, query)
	want := 1 - (1-cosine)*(1-0.5*0.3)*(1-0.7*1)

	if math.Abs(ranked[0].Score-want) > 1e-12 {
		test.Errorf("Score = %v, want %v ((1-score) = product of the gaps)", ranked[0].Score, want)
	}

	if ranked[0].Title != 1 || ranked[0].Lexical != 0.3 {
		test.Errorf("Title, Lexical = %v, %v; want 1, 0.3", ranked[0].Title, ranked[0].Lexical)
	}
}

func TestSemanticRank_BestChunkByFusedScore(test *testing.T) {
	query := []float32{1, 0}

	candidates := []filter.SemanticCandidate{
		{NodeID: "n1", Vector: []float32{0.9, 0.44}, Body: "closer vector"},
		{NodeID: "n1", Vector: []float32{0.8, 0.6}, Body: "matching words", Lexical: 1},
	}

	ranked := filter.SemanticRank(candidates, query, filter.Fusion{Lexical: 1})

	if ranked[0].BestChunkBody != "matching words" {
		test.Errorf("BestChunkBody = %q, want the chunk with the higher fused score", ranked[0].BestChunkBody)
	}
}

func TestSemanticRank_DeterministicTieBreakByNodeID(test *testing.T) {
	candidates := []filter.SemanticCandidate{
		{NodeID: "zebra", Vector: []float32{1, 0, 0}},
		{NodeID: "apple", Vector: []float32{1, 0, 0}},
		{NodeID: "mango", Vector: []float32{1, 0, 0}},
	}

	ranked := filter.SemanticRank(candidates, []float32{1, 0, 0}, filter.Fusion{})

	if len(ranked) != 3 {
		test.Fatalf("len = %d", len(ranked))
	}

	if ranked[0].NodeID != "apple" || ranked[1].NodeID != "mango" || ranked[2].NodeID != "zebra" {
		test.Errorf("equal scores should sort by NodeID ascending; got %+v", ranked)
	}
}

func TestRenderSnippetForQuery_WindowsAroundMatch(test *testing.T) {
	prefix := strings.Repeat("alpha beta gamma ", 20)
	suffix := strings.Repeat("delta epsilon ", 20)
	body := prefix + "tusk is a graph-aware vault " + suffix

	got := filter.RenderSnippetForQuery(body, "tusk", 80)

	if !strings.Contains(strings.ToLower(got), "tusk") {
		test.Errorf("snippet should contain match; got %q", got)
	}

	if !strings.HasPrefix(got, "…") {
		test.Errorf("snippet should start with ellipsis when match is not near body start; got %q", got)
	}

	// Bound the result to roughly maxRunes (allow a few runes of slack for
	// word-boundary expansion and ellipses).
	runeCount := len([]rune(got))
	if runeCount > 90 {
		test.Errorf("snippet runeCount = %d, want <= 90", runeCount)
	}
}

func TestRenderSnippetForQuery_FallsBackWhenNoMatch(test *testing.T) {
	body := "this body has no relevant tokens whatsoever"

	got := filter.RenderSnippetForQuery(body, "xyzzy", 200)
	want := filter.RenderSnippet(body, 200)

	if got != want {
		test.Errorf("fallback mismatch:\n got = %q\nwant = %q", got, want)
	}
}

func TestRenderSnippetForQuery_StopwordsIgnored(test *testing.T) {
	filler := strings.Repeat("lorem ipsum dolor sit amet ", 10)
	body := "The quick brown fox " + filler + "tusk appears here " + filler

	got := filter.RenderSnippetForQuery(body, "what is the tusk", 80)

	if !strings.Contains(strings.ToLower(got), "tusk") {
		test.Errorf("snippet should window around 'tusk'; got %q", got)
	}

	// If the stopword "the" at position 0 had won, the snippet would start
	// with "The quick" and lack a leading ellipsis.
	if strings.HasPrefix(got, "The quick") {
		test.Errorf("stopword 'the' should not have driven the window; got %q", got)
	}
}

func TestRenderSnippetForQuery_EmptyQueryMatchesLegacy(test *testing.T) {
	body := "hello\nworld\n\nfoo bar baz"

	got := filter.RenderSnippetForQuery(body, "", 200)
	want := filter.RenderSnippet(body, 200)

	if got != want {
		test.Errorf("empty query should behave like RenderSnippet:\n got = %q\nwant = %q", got, want)
	}
}

func TestRenderSnippet(test *testing.T) {
	cases := []struct {
		name     string
		body     string
		maxRunes int
		want     string
	}{
		{"empty", "", 200, ""},
		{"short, no newlines", "hello world", 200, "hello world"},
		{"newlines collapsed", "hello\nworld\n\nfoo", 200, "hello world foo"},
		{"truncated with ellipsis", "abcdefghij", 5, "abcde…"},
		{"rune boundary preserved", "héllo世界", 6, "héllo世…"},
		{"trailing whitespace trimmed", "abc   \n  ", 200, "abc"},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(inner *testing.T) {
			got := filter.RenderSnippet(testCase.body, testCase.maxRunes)

			if got != testCase.want {
				inner.Errorf("RenderSnippet(%q, %d) = %q, want %q", testCase.body, testCase.maxRunes, got, testCase.want)
			}
		})
	}
}
