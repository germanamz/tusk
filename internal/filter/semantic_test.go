package filter_test

import (
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

	ranked := filter.SemanticRank(candidates, query)

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
	ranked := filter.SemanticRank(nil, []float32{1, 0})

	if len(ranked) != 0 {
		test.Errorf("len = %d", len(ranked))
	}
}

func TestSemanticRank_SkipsCandidatesWithMismatchedDim(test *testing.T) {
	candidates := []filter.SemanticCandidate{
		{NodeID: "good", Vector: []float32{1, 0}},
		{NodeID: "bad", Vector: []float32{1, 0, 0}},
	}

	ranked := filter.SemanticRank(candidates, []float32{1, 0})

	if len(ranked) != 1 || ranked[0].NodeID != "good" {
		test.Errorf("ranked = %+v, want only good", ranked)
	}
}

func TestSemanticRank_MaxPerNodeAcrossChunks(test *testing.T) {
	candidates := []filter.SemanticCandidate{
		{NodeID: "alpha", ChunkIdx: 0, Vector: []float32{0.1, 0, 0}},   // weak
		{NodeID: "alpha", ChunkIdx: 1, Vector: []float32{1, 0, 0}},     // strong — should win for alpha
		{NodeID: "bravo", ChunkIdx: 0, Vector: []float32{0.5, 0.5, 0}}, // medium
		{NodeID: "bravo", ChunkIdx: 1, Vector: []float32{0.6, 0.5, 0}}, // slightly stronger
	}

	ranked := filter.SemanticRank(candidates, []float32{1, 0, 0})

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
		{NodeID: "n1", ChunkIdx: 0, Vector: []float32{0.1, 1}, Body: "low score body"},
		{NodeID: "n1", ChunkIdx: 1, Vector: []float32{1, 0}, Body: "high score body"},
		{NodeID: "n2", ChunkIdx: 0, Vector: []float32{0, 1}, Body: "n2 only chunk"},
	}

	ranked := filter.SemanticRank(candidates, query)

	if len(ranked) != 2 {
		test.Fatalf("len = %d, want 2", len(ranked))
	}

	first := ranked[0]

	if first.NodeID != "n1" {
		test.Errorf("first.NodeID = %q, want n1", first.NodeID)
	}

	if first.BestChunkIdx != 1 {
		test.Errorf("BestChunkIdx = %d, want 1", first.BestChunkIdx)
	}

	if first.BestChunkBody != "high score body" {
		test.Errorf("BestChunkBody = %q", first.BestChunkBody)
	}
}

func TestSemanticRank_DeterministicTieBreakByNodeID(test *testing.T) {
	candidates := []filter.SemanticCandidate{
		{NodeID: "zebra", ChunkIdx: 0, Vector: []float32{1, 0, 0}},
		{NodeID: "apple", ChunkIdx: 0, Vector: []float32{1, 0, 0}},
		{NodeID: "mango", ChunkIdx: 0, Vector: []float32{1, 0, 0}},
	}

	ranked := filter.SemanticRank(candidates, []float32{1, 0, 0})

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
