package manifest

import (
	"fmt"
	"regexp"
	"sort"

	"github.com/germanamz/tusk/internal/pathglob"
)

// DefaultTitleWeight is [query] title-weight when the key is absent.
const DefaultTitleWeight = 0.5

// ScoreWeights demotes semantic results: [query.type-weights] maps a node type,
// and [query.path-weights] a path glob, to a multiplier in (0, 1] on the final
// score of every result in a file of that type or at a matching path. Path
// globs follow the filter's `path` patterns (pathglob), anchored to the whole
// workspace-relative path, so `incidents/**` covers everything under
// incidents/. The zero value demotes nothing.
type ScoreWeights struct {
	Types map[string]float64
	Paths []PathWeight
}

// PathWeight is one [query.path-weights] entry.
type PathWeight struct {
	Pattern string
	Weight  float64
	match   *regexp.Regexp
}

// Weight returns the multiplier for a file of nodeType at path: its type's
// weight times the smallest weight among the path patterns that match it, so
// overlapping patterns never compound. It is 1 when nothing applies.
func (weights ScoreWeights) Weight(nodeType, path string) float64 {
	weight := 1.0

	if typeWeight, declared := weights.Types[nodeType]; declared {
		weight = typeWeight
	}

	pathWeight := 1.0

	for _, entry := range weights.Paths {
		if entry.match.MatchString(path) {
			pathWeight = min(pathWeight, entry.Weight)
		}
	}

	return weight * pathWeight
}

// Empty reports whether no weight is declared, so every result's weight is 1.
func (weights ScoreWeights) Empty() bool {
	return len(weights.Types) == 0 && len(weights.Paths) == 0
}

// newScoreWeights checks each weight is in (0, 1] and compiles the path
// globs, sorted by pattern so the result is deterministic. Node type names are
// checked by validateTypeWeights once the whole manifest is decoded.
func newScoreWeights(types, paths map[string]float64) (ScoreWeights, error) {
	for nodeType, weight := range types {
		if rangeErr := checkScoreWeight("type-weights", nodeType, weight); rangeErr != nil {
			return ScoreWeights{}, rangeErr
		}
	}

	patterns := make([]string, 0, len(paths))

	for pattern := range paths {
		patterns = append(patterns, pattern)
	}

	sort.Strings(patterns)

	compiled := make([]PathWeight, 0, len(patterns))

	for _, pattern := range patterns {
		weight := paths[pattern]

		if rangeErr := checkScoreWeight("path-weights", pattern, weight); rangeErr != nil {
			return ScoreWeights{}, rangeErr
		}

		if pattern == "" {
			return ScoreWeights{}, fmt.Errorf("manifest: query.path-weights: a path pattern must not be empty")
		}

		match, compileErr := regexp.Compile(pathglob.ToRegexp(pattern))

		if compileErr != nil {
			return ScoreWeights{}, fmt.Errorf("manifest: query.path-weights: %q: %w", pattern, compileErr)
		}

		compiled = append(compiled, PathWeight{Pattern: pattern, Weight: weight, match: match})
	}

	if len(types) == 0 {
		types = nil
	}

	return ScoreWeights{Types: types, Paths: compiled}, nil
}

// checkScoreWeight rejects a weight outside (0, 1]. Written as a negated range
// check so NaN fails it too.
func checkScoreWeight(table, key string, weight float64) error {
	if !(weight > 0 && weight <= 1) {
		return fmt.Errorf("manifest: query.%s: %q = %v must be in (0, 1]; a weight demotes results, 1 leaves them as they are", table, key, weight)
	}

	return nil
}

// validateTypeWeights rejects a [query.type-weights] key that names no
// declared node type, so a typo doesn't silently demote nothing. Sub-unit
// types are not file types: a hit in a section takes its file's weight.
func validateTypeWeights(loaded *Manifest) error {
	names := make([]string, 0, len(loaded.ScoreWeights.Types))

	for nodeType := range loaded.ScoreWeights.Types {
		names = append(names, nodeType)
	}

	sort.Strings(names)

	for _, nodeType := range names {
		if _, declared := loaded.NodeTypes[nodeType]; !declared {
			return fmt.Errorf("manifest: query.type-weights: %q is not a declared node type", nodeType)
		}
	}

	return nil
}
