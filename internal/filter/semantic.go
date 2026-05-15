package filter

import (
	"sort"
	"strings"
	"unicode"

	"github.com/germanamz/tusk/internal/embed"
)

// SemanticCandidate pairs a node's chunk vector with its ids for ranking.
// Body carries the chunk's body text (no header prefix) so renderers can
// produce a snippet of the highest-scoring chunk per node.
type SemanticCandidate struct {
	NodeID   string
	ChunkIdx int
	Vector   []float32
	Body     string
}

// ScoredResult is one ranked node. Score is the max cosine similarity across
// the node's chunks. BestChunkIdx and BestChunkBody identify and carry the
// body of the chunk that produced Score.
type ScoredResult struct {
	NodeID        string
	Score         float64
	BestChunkIdx  int
	BestChunkBody string
}

// SemanticRank scores each candidate by cosine similarity to queryVector and
// returns one row per node, with Score equal to the maximum chunk score for
// that node. BestChunkIdx and BestChunkBody come from the chunk that produced
// the max. Results are sorted by score descending, ties broken by NodeID
// ascending. Candidates whose vectors mismatch queryVector's dimension are
// silently skipped.
func SemanticRank(candidates []SemanticCandidate, queryVector []float32) []ScoredResult {
	type bestEntry struct {
		score    float64
		chunkIdx int
		body     string
	}

	bestByNode := make(map[string]bestEntry, len(candidates))

	for _, candidate := range candidates {
		if len(candidate.Vector) != len(queryVector) {
			continue
		}

		score := embed.CosineSimilarity(candidate.Vector, queryVector)

		prev, present := bestByNode[candidate.NodeID]

		if !present || score > prev.score {
			bestByNode[candidate.NodeID] = bestEntry{
				score:    score,
				chunkIdx: candidate.ChunkIdx,
				body:     candidate.Body,
			}
		}
	}

	scored := make([]ScoredResult, 0, len(bestByNode))

	for nodeID, entry := range bestByNode {
		scored = append(scored, ScoredResult{
			NodeID:        nodeID,
			Score:         entry.score,
			BestChunkIdx:  entry.chunkIdx,
			BestChunkBody: entry.body,
		})
	}

	sort.Slice(scored, func(left, right int) bool {
		if scored[left].Score == scored[right].Score {
			return scored[left].NodeID < scored[right].NodeID
		}

		return scored[left].Score > scored[right].Score
	})

	return scored
}

// RenderSnippet returns the leading maxRunes runes of body with internal
// whitespace runs collapsed to single spaces, trailing whitespace stripped,
// and an ellipsis (U+2026) appended when truncation occurred. Returns the
// empty string for empty input or when only whitespace remains.
func RenderSnippet(body string, maxRunes int) string {
	return RenderSnippetForQuery(body, "", maxRunes)
}

// RenderSnippetForQuery returns a maxRunes-bounded snippet of body. When query
// is non-empty, the snippet windows around the earliest non-stopword token from
// query that occurs in body (case-insensitive), respecting word boundaries and
// prefixing/suffixing an ellipsis (U+2026) when the window is clipped. When the
// query is empty or no token matches, it falls back to the leading-runes
// behavior of RenderSnippet.
func RenderSnippetForQuery(body, query string, maxRunes int) string {
	if maxRunes <= 0 || len(body) == 0 {
		return ""
	}

	collapsed := collapseWhitespace(body)
	if len(collapsed) == 0 {
		return ""
	}

	runes := []rune(collapsed)

	matchPos := earliestQueryMatch(runes, query)

	if matchPos < 0 {
		if len(runes) <= maxRunes {
			return string(runes)
		}

		return string(runes[:maxRunes]) + "…"
	}

	before := maxRunes / 5
	after := maxRunes - before

	start := matchPos - before
	if start < 0 {
		start = 0
	}

	end := matchPos + after
	if end > len(runes) {
		end = len(runes)
	}

	if start > 0 {
		// Advance start to the next space boundary so we don't begin mid-word.
		for start < matchPos && !unicode.IsSpace(runes[start-1]) {
			start++
		}
	}

	if end < len(runes) {
		// Retract end to the previous space boundary so we don't cut mid-word.
		for end > matchPos && !unicode.IsSpace(runes[end]) {
			end--
		}
	}

	result := string(runes[start:end])

	if start > 0 {
		result = "…" + result
	}

	if end < len(runes) {
		result += "…"
	}

	return result
}

// collapseWhitespace returns body with runs of ASCII whitespace collapsed to a
// single space and leading/trailing whitespace stripped.
func collapseWhitespace(body string) string {
	var (
		builder       []rune
		previousSpace bool
	)

	for _, ch := range body {
		if ch == '\n' || ch == '\r' || ch == '\t' || ch == ' ' {
			if len(builder) == 0 {
				continue
			}

			if previousSpace {
				continue
			}

			builder = append(builder, ' ')
			previousSpace = true

			continue
		}

		builder = append(builder, ch)
		previousSpace = false
	}

	for len(builder) > 0 && builder[len(builder)-1] == ' ' {
		builder = builder[:len(builder)-1]
	}

	return string(builder)
}

// snippetStopwords is the small fixed set of English function words that
// shouldn't drive snippet windowing on their own.
var snippetStopwords = map[string]struct{}{
	"a": {}, "an": {}, "and": {}, "are": {}, "as": {}, "at": {},
	"be": {}, "but": {}, "by": {},
	"do": {}, "does": {},
	"for": {}, "from": {},
	"has": {}, "have": {}, "how": {},
	"i": {}, "in": {}, "is": {}, "it": {}, "its": {},
	"no": {}, "not": {},
	"of": {}, "on": {}, "or": {},
	"that": {}, "the": {}, "to": {},
	"was": {}, "what": {}, "when": {}, "where": {}, "which": {}, "who": {}, "why": {}, "will": {}, "with": {},
	"you": {}, "your": {},
}

// earliestQueryMatch returns the earliest rune index in runes where any
// non-stopword token from query appears (case-insensitive), or -1 when there
// is no match or the query has no usable tokens.
func earliestQueryMatch(runes []rune, query string) int {
	if query == "" {
		return -1
	}

	tokens := queryTokens(query)
	if len(tokens) == 0 {
		return -1
	}

	lower := strings.ToLower(string(runes))

	best := -1

	for _, token := range tokens {
		idx := strings.Index(lower, token)
		if idx < 0 {
			continue
		}

		// Convert byte index back to rune index.
		runeIdx := len([]rune(lower[:idx]))
		if best < 0 || runeIdx < best {
			best = runeIdx
		}
	}

	return best
}

// queryTokens splits query on non-letter/digit runes, lowercases, and drops
// stopwords and empty tokens.
func queryTokens(query string) []string {
	fields := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	tokens := make([]string, 0, len(fields))

	for _, field := range fields {
		if _, skip := snippetStopwords[field]; skip {
			continue
		}

		tokens = append(tokens, field)
	}

	return tokens
}
