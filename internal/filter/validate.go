package filter

import (
	"fmt"

	"github.com/germanamz/tusk/internal/manifest"
)

// ValidationError reports a validation problem with a position and a hint.
type ValidationError struct {
	Pos     int
	Message string
	Hint    string
}

func (validationErr *ValidationError) Error() string {
	if validationErr.Hint != "" {
		return fmt.Sprintf("filter: %s at column %d (%s)", validationErr.Message, validationErr.Pos+1, validationErr.Hint)
	}

	return fmt.Sprintf("filter: %s at column %d", validationErr.Message, validationErr.Pos+1)
}

// Validate walks the AST and surfaces semantic problems against the manifest.
func Validate(expr Expr, loaded manifest.Manifest) []ValidationError {
	if expr == nil {
		return nil
	}

	collector := &validationCollector{manifest: loaded}
	collector.walk(expr)

	return collector.errors
}

type validationCollector struct {
	manifest manifest.Manifest
	errors   []ValidationError
}

func (collector *validationCollector) walk(expr Expr) {
	switch typed := expr.(type) {
	case *OrExpr:
		collector.walk(typed.Left)
		collector.walk(typed.Right)
	case *AndExpr:
		collector.walk(typed.Left)
		collector.walk(typed.Right)
	case *NotExpr:
		collector.walk(typed.Inner)
	case *PropertyPredicate:
		// Properties are not validated against the manifest in Plan 4 (see spec §5.2).
	case *EdgePredicate:
		if _, declared := collector.manifest.EdgeTypes[typed.EdgeType]; !declared {
			collector.errors = append(collector.errors, ValidationError{
				Pos:     typed.Pos,
				Message: fmt.Sprintf("edge type %q not declared in manifest", typed.EdgeType),
				Hint:    suggestEdgeType(typed.EdgeType, collector.manifest.EdgeTypes),
			})
		}

		if typed.Inner != nil {
			collector.walk(typed.Inner)
		}
	case *TraversalShortcut:
		if _, declared := collector.manifest.EdgeTypes["parent"]; !declared {
			collector.errors = append(collector.errors, ValidationError{
				Pos:     typed.Pos,
				Message: "traversal shortcut requires the workspace to declare a `parent` edge type",
				Hint:    "add [edge-types.parent] to tusk.toml or use explicit `<edge>->` form",
			})
		}
	}
}

func suggestEdgeType(unknown string, available map[string]manifest.EdgeType) string {
	for name := range available {
		if levenshteinAtMostOne(unknown, name) {
			return fmt.Sprintf("did you mean %q?", name)
		}
	}

	return ""
}

func levenshteinAtMostOne(left, right string) bool {
	if len(left) == len(right) {
		differences := 0

		for index := 0; index < len(left); index++ {
			if left[index] != right[index] {
				differences++

				if differences > 1 {
					return false
				}
			}
		}

		return differences == 1
	}

	if abs(len(left)-len(right)) != 1 {
		return false
	}

	shorter, longer := left, right

	if len(left) > len(right) {
		shorter, longer = right, left
	}

	shortIdx := 0
	longIdx := 0
	differences := 0

	for shortIdx < len(shorter) && longIdx < len(longer) {
		if shorter[shortIdx] == longer[longIdx] {
			shortIdx++
			longIdx++

			continue
		}

		differences++

		if differences > 1 {
			return false
		}

		longIdx++
	}

	return true
}

func abs(value int) int {
	if value < 0 {
		return -value
	}

	return value
}
