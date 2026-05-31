package filter

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/typeref"
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

// add appends a validation error. A zero-value hint renders without a hint line
// (ValidationError.Error branches on Hint != "").
func (collector *validationCollector) add(pos int, message, hint string) {
	collector.errors = append(collector.errors, ValidationError{Pos: pos, Message: message, Hint: hint})
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
		ref, parseErr := typeref.Parse(typed.EdgeType)

		if parseErr != nil {
			collector.add(typed.Pos, fmt.Sprintf("edge type %q is not a valid type reference", typed.EdgeType), "")
		} else if _, declared := collector.manifest.EdgeTypes[ref.Type]; !declared {
			collector.add(typed.Pos, fmt.Sprintf("edge type %q not declared in manifest", ref.Type), suggestEdgeType(ref.Type, collector.manifest.EdgeTypes))
		}

		if typed.Inner != nil {
			collector.walk(typed.Inner)
		}
	case *TraversalShortcut:
		collector.resolveShortcut(typed)
	case *ModifiedSincePredicate:
		collector.resolveModifiedSince(typed)
	}
}

// resolveModifiedSince parses pred.Raw into either a duration or an
// absolute date and stamps the result back onto pred. The heuristic: a
// raw value containing "-<digit>" (e.g. "2026-05-23") is tried as a date
// first; anything else is tried as a duration first.
func (collector *validationCollector) resolveModifiedSince(pred *ModifiedSincePredicate) {
	if pred.Raw == "" {
		collector.add(pred.Pos, "modified-since: empty value", `expected duration like "7d" or ISO date like "2026-05-23"`)

		return
	}

	preferDate := looksLikeDate(pred.Raw)

	if preferDate {
		if parsedTime, ok := parseAbsoluteTime(pred.Raw); ok {
			pred.Since = parsedTime

			return
		}

		if parsedDuration, ok := parseFilterDuration(pred.Raw); ok {
			pred.Duration = parsedDuration

			return
		}
	} else {
		if parsedDuration, ok := parseFilterDuration(pred.Raw); ok {
			pred.Duration = parsedDuration

			return
		}

		if parsedTime, ok := parseAbsoluteTime(pred.Raw); ok {
			pred.Since = parsedTime

			return
		}
	}

	collector.add(pred.Pos, fmt.Sprintf("modified-since: unparseable value %q", pred.Raw), `expected duration like "7d" or ISO date like "2026-05-23"`)
}

// looksLikeDate returns true when raw contains a '-' immediately followed
// by a digit, which distinguishes "2026-05-23" from durations like "7d".
func looksLikeDate(raw string) bool {
	for index := 0; index < len(raw)-1; index++ {
		if raw[index] == '-' && raw[index+1] >= '0' && raw[index+1] <= '9' {
			return true
		}
	}

	return false
}

// parseFilterDuration accepts the standard time.ParseDuration suffixes
// (h, m, s, ms, us, ns) plus a single "Nd" or compound "NdMh" prefix for
// days. Negative durations and zero-length input are rejected.
func parseFilterDuration(raw string) (time.Duration, bool) {
	if raw == "" {
		return 0, false
	}

	if strings.HasPrefix(raw, "-") {
		return 0, false
	}

	rewritten, ok := rewriteDays(raw)

	if !ok {
		return 0, false
	}

	parsed, err := time.ParseDuration(rewritten)

	if err != nil {
		return 0, false
	}

	if parsed <= 0 {
		return 0, false
	}

	return parsed, true
}

// rewriteDays scans raw for a leading run of digits followed by 'd' and
// replaces that segment with the equivalent number of hours so that
// time.ParseDuration (which has no day unit) accepts the rest of the
// string. Returns ok=false on overflow or malformed digit/d sequences.
func rewriteDays(raw string) (string, bool) {
	digitsEnd := 0

	for digitsEnd < len(raw) && unicode.IsDigit(rune(raw[digitsEnd])) {
		digitsEnd++
	}

	if digitsEnd == 0 || digitsEnd >= len(raw) || raw[digitsEnd] != 'd' {
		return raw, true
	}

	days := 0

	for index := 0; index < digitsEnd; index++ {
		days = days*10 + int(raw[index]-'0')

		if days > 1_000_000 {
			return "", false
		}
	}

	hours := days * 24
	tail := raw[digitsEnd+1:]

	return fmt.Sprintf("%dh%s", hours, tail), true
}

// parseAbsoluteTime tries a small set of ISO-8601 formats in decreasing
// specificity.
//
// Bare dates ("2006-01-02") and naive datetimes (no Z/offset) are treated
// as UTC midnight. Users near a day boundary should use the explicit Z form
// or an RFC3339 timestamp with an explicit offset.
func parseAbsoluteTime(raw string) (time.Time, bool) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02",
	}

	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, true
		}
	}

	return time.Time{}, false
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
			collector.add(shortcut.Pos, fmt.Sprintf("unknown hierarchy alias %q", shortcut.Alias), formatAliasList(aliasToEdge))

			return
		}

		collector.assignEdge(shortcut, edgeName)

		return
	}

	if defaultEdge != "" {
		collector.assignEdge(shortcut, defaultEdge)

		return
	}

	if hierarchyCount == 1 {
		for _, only := range aliasToEdge {
			collector.assignEdge(shortcut, only)
		}

		return
	}

	if hierarchyCount == 0 {
		hint := "add hierarchy = \"<alias>\" to an edge in tusk.toml, or use explicit <edge>->*=<id>"

		if candidate := suggestHierarchyCandidate(collector.manifest.EdgeTypes); candidate != "" {
			hint = fmt.Sprintf("e.g. %s->*=<id>; or add hierarchy = \"<alias>\" to that edge in tusk.toml", candidate)
		}

		collector.add(shortcut.Pos, "no hierarchy edges declared in this workspace", hint)

		return
	}

	collector.add(shortcut.Pos, "no default hierarchy and multiple are declared", fmt.Sprintf("use tree:<alias>=<id> or set hierarchy-default = true on one of: %s", formatAliasList(aliasToEdge)))
}

// assignEdge stamps the resolved edge name onto the shortcut and, when the
// edge type declares an ordering property, also copies OrderedBy so the
// compiler can emit a default ORDER BY clause.
func (collector *validationCollector) assignEdge(shortcut *TraversalShortcut, edgeName string) {
	shortcut.EdgeType = edgeName

	if edgeDef, ok := collector.manifest.EdgeTypes[edgeName]; ok {
		shortcut.OrderedBy = edgeDef.OrderedBy
	}
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
