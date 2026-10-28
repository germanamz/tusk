package query

import (
	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/lexical"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/wikilink"
)

// fusionWeights is the workspace's [query] lexical-weight and title-weight,
// or the zero Fusion (cosine alone) when there is no manifest.
func fusionWeights(deps Deps) filter.Fusion {
	if deps.Manifest == nil {
		return filter.Fusion{}
	}

	return filter.Fusion{Lexical: deps.Manifest.LexicalWeight, Title: deps.Manifest.TitleWeight}
}

// scoreWeights is the workspace's [query.type-weights] and
// [query.path-weights], or none when there is no manifest.
func scoreWeights(deps Deps) manifest.ScoreWeights {
	if deps.Manifest == nil {
		return manifest.ScoreWeights{}
	}

	return deps.Manifest.ScoreWeights
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

// scoreTitles sets each candidate's Title to how much of its file's name the
// query names (lexical.Terms.Coverage), so a query about "ledger-sync" lifts
// every passage of the file of that name, including the one that answers
// without naming it. nameOf returns the name of a candidate's file (nodeName).
// It does nothing at weight 0.
func scoreTitles(candidates []filter.SemanticCandidate, semantic string, weight float64, nameOf func(candidateID string) string) {
	if weight <= 0 || len(candidates) == 0 {
		return
	}

	terms := lexical.QueryTerms(semantic)

	if len(terms) == 0 {
		return
	}

	coverage := make(map[string]float64)

	for position, candidate := range candidates {
		name := nameOf(candidate.NodeID)
		score, scored := coverage[name]

		if !scored {
			score = terms.Coverage(name)
			coverage[name] = score
		}

		candidates[position].Title = score
	}
}

// nodeName is what a query names a file node by: its title, or its file name
// (what a [[name]] link names it by) when it has none.
func nodeName(row Row) string {
	if row.Title != "" {
		return row.Title
	}

	if row.Path == "" {
		return ""
	}

	return wikilink.FileName(row.Path)
}
