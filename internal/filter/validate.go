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
	collector.walk(expr, nil)

	return collector.errors
}

// ResolveSortKeys stamps ResolvedType (and EnumValues for enums) onto each sort
// key by resolving its property against the manifest within the query's
// top-level conjunctive `type=` scope — the same resolution predicates use. The
// compiler reads these so ORDER BY sorts an enum property by declared order
// (low < medium < high) instead of by its stored name. A core column, an
// undeclared property, or one that is ambiguous across the scope is left
// unresolved, and the compiler falls back to ordering on the stored value, so
// ad-hoc sorts are unaffected. Keys are stamped in place.
func ResolveSortKeys(expr Expr, loaded manifest.Manifest, keys []SortKey) {
	if len(keys) == 0 {
		return
	}

	scope := typeScope{}

	if expr != nil {
		scope = conjunctiveTypes(expr)
	}

	collector := &validationCollector{manifest: loaded}

	for index := range keys {
		if _, core := coreColumns[keys[index].Property]; core {
			continue
		}

		decls := collector.lookupPropertyDecls(keys[index].Property, scope)

		if len(decls) != 1 {
			continue
		}

		keys[index].ResolvedType = decls[0].Type

		if decls[0].Type == "enum" {
			keys[index].EnumValues = decls[0].Values
		}
	}
}

// typeScope is the set of node-type names a predicate is conjunctively
// constrained to by sibling `type=` equalities. Empty means "any type" —
// resolution then scans every declared node type.
type typeScope map[string]struct{}

type validationCollector struct {
	manifest manifest.Manifest
	errors   []ValidationError
}

// add appends a validation error. A zero-value hint renders without a hint line
// (ValidationError.Error branches on Hint != "").
func (collector *validationCollector) add(pos int, message, hint string) {
	collector.errors = append(collector.errors, ValidationError{Pos: pos, Message: message, Hint: hint})
}

func (collector *validationCollector) walk(expr Expr, scope typeScope) {
	switch typed := expr.(type) {
	case *OrExpr:
		// Branches inherit the incoming scope; a `type=` inside one branch does
		// not constrain its sibling, so nothing is added here.
		collector.walk(typed.Left, scope)
		collector.walk(typed.Right, scope)
	case *AndExpr:
		// Every node matching this AND must satisfy each conjunct, so the
		// `type=` equalities at this conjunctive level constrain both children.
		inner := unionScope(scope, conjunctiveTypes(typed))
		collector.walk(typed.Left, inner)
		collector.walk(typed.Right, inner)
	case *NotExpr:
		collector.walk(typed.Inner, scope)
	case *PropertyPredicate:
		collector.resolveProperty(typed, scope)
	case *EdgePredicate:
		ref, parseErr := typeref.Parse(typed.EdgeType)

		if parseErr != nil {
			collector.add(typed.Pos, fmt.Sprintf("edge type %q is not a valid type reference", typed.EdgeType), "")
		} else if _, declared := collector.manifest.EdgeTypes[ref.Type]; !declared {
			collector.add(typed.Pos, fmt.Sprintf("edge type %q not declared in manifest", ref.Type), suggestEdgeType(ref.Type, collector.manifest.EdgeTypes))
		}

		if typed.Inner != nil {
			// The inner predicate constrains the edge's target node, a different
			// node-type context, so the outer scope does not carry into it.
			collector.walk(typed.Inner, nil)
		}
	case *TraversalShortcut:
		collector.resolveShortcut(typed)
	case *ModifiedSincePredicate:
		collector.resolveModifiedSince(typed)
	}
}

// conjunctiveTypes collects the node-type names named by `type=<name>`
// equalities at the top conjunctive level of expr, flattening nested AndExprs
// and stopping at OrExpr / NotExpr / EdgePredicate boundaries (those do not
// positively constrain the conjunction).
func conjunctiveTypes(expr Expr) typeScope {
	scope := typeScope{}
	collectConjunctiveTypes(expr, scope)

	return scope
}

func collectConjunctiveTypes(expr Expr, scope typeScope) {
	switch typed := expr.(type) {
	case *AndExpr:
		collectConjunctiveTypes(typed.Left, scope)
		collectConjunctiveTypes(typed.Right, scope)
	case *PropertyPredicate:
		if typed.Property != "type" || typed.Op != OpEQ {
			return
		}

		if stringValue, ok := typed.Value.(StringValue); ok {
			if ref, parseErr := typeref.Parse(stringValue.V); parseErr == nil {
				scope[ref.Type] = struct{}{}
			}
		}
	}
}

// unionScope returns a new scope containing every name from base and extra.
// base may be nil (the root call passes nil).
func unionScope(base, extra typeScope) typeScope {
	merged := typeScope{}

	for name := range base {
		merged[name] = struct{}{}
	}

	for name := range extra {
		merged[name] = struct{}{}
	}

	return merged
}

// resolveProperty resolves a property predicate's declared type from the
// manifest within its conjunctive type scope and stamps ResolvedType (plus
// EnumValues for enums) so the compiler can choose a type-aware comparison.
// It also surfaces validation errors for ordering/range operators: an invalid
// enum value/index, an unparseable date/datetime, or an ambiguous property
// declared on multiple node types without a disambiguating `type=`.
//
// Core columns (id/type/path/title) and undeclared properties are left
// unresolved — the compiler falls back to its legacy behaviour, preserving
// ad-hoc property queries.
func (collector *validationCollector) resolveProperty(pred *PropertyPredicate, scope typeScope) {
	if _, core := coreColumns[pred.Property]; core {
		return
	}

	decls := collector.lookupPropertyDecls(pred.Property, scope)

	switch len(decls) {
	case 0:
		return
	case 1:
		// resolved below
	default:
		if isOrderingOrRange(pred.Op) {
			collector.add(
				pred.Pos,
				fmt.Sprintf("property %q is declared on multiple node types with different definitions", pred.Property),
				"add type=<node-type> to disambiguate",
			)
		}

		return
	}

	decl := decls[0]
	pred.ResolvedType = decl.Type

	if decl.Type == "enum" {
		pred.EnumValues = decl.Values
	}

	if isOrderingOrRange(pred.Op) {
		collector.checkTypedComparison(pred, decl)
	}
}

// lookupPropertyDecls returns the distinct declarations of property name within
// scope (or across every node type when scope is empty). Two declarations are
// the same when their type and enum value list match; more than one distinct
// declaration means the name is ambiguous.
func (collector *validationCollector) lookupPropertyDecls(name string, scope typeScope) []manifest.PropertyDecl {
	var distinct []manifest.PropertyDecl

	consider := func(nodeType manifest.NodeType) {
		for _, decl := range nodeType.Properties {
			if decl.Name != name {
				continue
			}

			if !containsEquivalentDecl(distinct, decl) {
				distinct = append(distinct, decl)
			}
		}
	}

	if len(scope) > 0 {
		for typeName := range scope {
			if nodeType, ok := collector.manifest.NodeTypes[typeName]; ok {
				consider(nodeType)
			}
		}

		return distinct
	}

	for _, nodeType := range collector.manifest.NodeTypes {
		consider(nodeType)
	}

	return distinct
}

// checkTypedComparison validates the right-hand value(s) of an ordering/range
// predicate against the resolved type, and normalizes date/datetime bounds to
// their canonical form so they sort lexically against the stored values.
func (collector *validationCollector) checkTypedComparison(pred *PropertyPredicate, decl manifest.PropertyDecl) {
	switch decl.Type {
	case "enum":
		for _, token := range comparisonTokens(pred) {
			if _, ok := resolveEnumIndex(decl.Values, token); !ok {
				collector.add(
					pred.Pos,
					fmt.Sprintf("%q is not a value or 0-based index of enum property %q", token, pred.Property),
					"valid values: "+strings.Join(decl.Values, ", "),
				)
			}
		}
	case "date":
		collector.normalizeTemporal(pred, time.DateOnly, "ISO date (YYYY-MM-DD)")
	case "datetime":
		collector.normalizeTemporal(pred, time.RFC3339, "RFC3339 datetime")
	}
}

// normalizeTemporal parses each comparison bound with layout, reporting a
// validation error on failure, and rewrites the predicate's value(s) to the
// canonical re-formatted form on success.
func (collector *validationCollector) normalizeTemporal(pred *PropertyPredicate, layout, expected string) {
	normalize := func(raw string) (string, bool) {
		parsed, parseErr := time.Parse(layout, raw)

		if parseErr != nil {
			collector.add(
				pred.Pos,
				fmt.Sprintf("%q is not a valid value for %q", raw, pred.Property),
				"expected "+expected,
			)

			return "", false
		}

		return parsed.Format(layout), true
	}

	switch value := pred.Value.(type) {
	case StringValue:
		if normalized, ok := normalize(value.V); ok {
			pred.Value = StringValue{V: normalized, Bareword: value.Bareword}
		}
	case RangeValue:
		normMin, okMin := normalize(value.Min)
		normMax, okMax := normalize(value.Max)

		if okMin && okMax {
			pred.Value = RangeValue{Min: normMin, Max: normMax}
		}
	}
}

// comparisonTokens returns the raw right-hand value tokens of a predicate: one
// for a scalar comparison, two for a range.
func comparisonTokens(pred *PropertyPredicate) []string {
	switch value := pred.Value.(type) {
	case StringValue:
		return []string{value.V}
	case RangeValue:
		return []string{value.Min, value.Max}
	}

	return nil
}

// containsEquivalentDecl reports whether decls already holds a declaration with
// the same type and enum value list as candidate.
func containsEquivalentDecl(decls []manifest.PropertyDecl, candidate manifest.PropertyDecl) bool {
	for _, existing := range decls {
		if existing.Type == candidate.Type && equalStrings(existing.Values, candidate.Values) {
			return true
		}
	}

	return false
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}

	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}

	return true
}

// isOrderingOrRange reports whether op is an ordering comparison (`<`, `<=`,
// `>`, `>=`) or the inclusive range form — the operators whose comparison
// strategy depends on the property's declared type.
func isOrderingOrRange(op Op) bool {
	return isNumericOp(op) || op == OpRange
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
