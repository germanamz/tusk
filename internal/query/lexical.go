package query

import (
	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/lexical"
)

// lexicalWeight is the workspace's [query] lexical-weight, or 0 (cosine
// alone) when there is no manifest.
func lexicalWeight(deps Deps) float64 {
	if deps.Manifest == nil {
		return 0
	}

	return deps.Manifest.LexicalWeight
}

// scoreLexical sets each candidate's Lexical to how well its body matches the
// query's words, relative to the other candidates (lexical.Scores). It does
// nothing at weight 0, so a workspace ranking by cosine alone tokenizes
// nothing.
func scoreLexical(candidates []filter.SemanticCandidate, semantic string, weight float64) {
	if weight <= 0 || len(candidates) == 0 {
		return
	}

	bodies := make([]string, len(candidates))

	for position, candidate := range candidates {
		bodies[position] = candidate.Body
	}

	for position, score := range lexical.Scores(semantic, bodies) {
		candidates[position].Lexical = score
	}
}
