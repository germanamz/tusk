package filter

import (
	"fmt"
	"sort"
	"strings"

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
		collector.resolveShortcut(typed)
	}
}

func (collector *validationCollector) resolveShortcut(shortcut *TraversalShortcut) {
	aliasToEdge := map[string]string{}

	var defaultEdge string

	hierarchyCount := 0

	for edgeName, edge := range collector.manifest.EdgeTypes {
		if edge.Hierarchy == "" {
			continue
		}

		aliasToEdge[edge.Hierarchy] = edgeName
		hierarchyCount++

		if edge.HierarchyDefault {
			defaultEdge = edgeName
		}
	}

	if shortcut.Alias != "" {
		edgeName, found := aliasToEdge[shortcut.Alias]

		if !found {
			collector.errors = append(collector.errors, ValidationError{
				Pos:     shortcut.Pos,
				Message: fmt.Sprintf("unknown hierarchy alias %q", shortcut.Alias),
				Hint:    formatAliasList(aliasToEdge),
			})

			return
		}

		shortcut.EdgeType = edgeName

		return
	}

	if defaultEdge != "" {
		shortcut.EdgeType = defaultEdge

		return
	}

	if hierarchyCount == 1 {
		for _, only := range aliasToEdge {
			shortcut.EdgeType = only
		}

		return
	}

	if hierarchyCount == 0 {
		hint := "add hierarchy = \"<alias>\" to an edge in tusk.toml, or use explicit <edge>->*=<id>"

		if candidate := suggestHierarchyCandidate(collector.manifest.EdgeTypes); candidate != "" {
			hint = fmt.Sprintf("e.g. %s->*=<id>; or add hierarchy = \"<alias>\" to that edge in tusk.toml", candidate)
		}

		collector.errors = append(collector.errors, ValidationError{
			Pos:     shortcut.Pos,
			Message: "no hierarchy edges declared in this workspace",
			Hint:    hint,
		})

		return
	}

	collector.errors = append(collector.errors, ValidationError{
		Pos:     shortcut.Pos,
		Message: "no default hierarchy and multiple are declared",
		Hint:    fmt.Sprintf("use tree:<alias>=<id> or set hierarchy-default = true on one of: %s", formatAliasList(aliasToEdge)),
	})
}

func formatAliasList(aliasToEdge map[string]string) string {
	if len(aliasToEdge) == 0 {
		return ""
	}

	aliases := make([]string, 0, len(aliasToEdge))

	for alias := range aliasToEdge {
		aliases = append(aliases, fmt.Sprintf("%q", alias))
	}

	sort.Strings(aliases)

	return "declared aliases: " + strings.Join(aliases, ", ")
}

// suggestHierarchyCandidate picks an edge that looks hierarchy-shaped
// (many-to-one and acyclic) to surface in error messages. Returns the
// edge's name, or "" when nothing matches.
func suggestHierarchyCandidate(edges map[string]manifest.EdgeType) string {
	candidates := make([]string, 0)

	for name, edge := range edges {
		if edge.Cardinality == manifest.CardinalityManyToOne && edge.Acyclic {
			candidates = append(candidates, name)
		}
	}

	if len(candidates) == 0 {
		return ""
	}

	sort.Strings(candidates)

	return candidates[0]
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
