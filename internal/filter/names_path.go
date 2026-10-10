package filter

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/pathglob"
	"github.com/germanamz/tusk/internal/pathref"
)

// NamesPathKeyword is the reserved key of the names-path predicate. Like the
// hierarchy shortcuts, it shadows a frontmatter property of the same name.
const NamesPathKeyword = "names-path"

// NamesPathPredicate matches the pages that name a workspace path, through the
// path_refs table that edge types with paths = true fill.
//
// A literal Value is a path: it matches a page naming that path or any
// directory above it, since a directory ref covers what it holds. A Pattern
// value is a glob over the named paths themselves, with no ancestor expansion.
// EdgeType, set by the qualified form `names-path:<edge-type>=`, limits the
// match to refs of that edge type; empty matches every paths edge type.
// Negated is the != form. Refs belong to file rows, so a sub-unit row never
// matches `=` and always matches `!=` (and `NOT names-path=`); scope a negated
// test with `type=` when the result may hold sub-units.
type NamesPathPredicate struct {
	Value    string
	EdgeType string
	Pattern  bool
	Negated  bool
	Pos      int
}

func (pred *NamesPathPredicate) exprNode()     {}
func (pred *NamesPathPredicate) Position() int { return pred.Pos }

// Targets returns the stored ref targets a literal predicate matches: the
// cleaned path and every directory above it, nearest first. It returns false
// for a pattern or a value that isn't a workspace-relative path.
func (pred *NamesPathPredicate) Targets() ([]string, bool) {
	if pred.Pattern {
		return nil, false
	}

	cleaned, ok := pathref.Clean(pred.Value)

	if !ok {
		return nil, false
	}

	return pathref.Ancestors(cleaned), true
}

// Matches reports whether a stored ref of edgeType naming target satisfies the
// predicate, ignoring Negated: the edge type is the qualified one, if any, and
// for a literal, target is the path or a directory above it; for a pattern,
// target matches the glob.
func (pred *NamesPathPredicate) Matches(edgeType, target string) bool {
	if pred.EdgeType != "" && pred.EdgeType != edgeType {
		return false
	}

	if pred.Pattern {
		matcher, compileErr := regexp.Compile(pathglob.ToRegexp(pred.Value))

		return compileErr == nil && matcher.MatchString(target)
	}

	targets, ok := pred.Targets()

	if !ok {
		return false
	}

	for _, candidate := range targets {
		if candidate == target {
			return true
		}
	}

	return false
}

// OuterNamesPaths returns the non-negated names-path predicates at the outer
// level of expr, through AND and OR but not under NOT or past an edge arrow:
// the ones that say which refs made a row match. The query layer narrows a
// row's listed paths to those refs.
func OuterNamesPaths(expr Expr) []*NamesPathPredicate {
	var found []*NamesPathPredicate

	var walk func(current Expr)

	walk = func(current Expr) {
		switch typed := current.(type) {
		case *OrExpr:
			walk(typed.Left)
			walk(typed.Right)
		case *AndExpr:
			walk(typed.Left)
			walk(typed.Right)
		case *NamesPathPredicate:
			if !typed.Negated {
				found = append(found, typed)
			}
		}
	}

	walk(expr)

	return found
}

// parseNamesPathPredicate parses `names-path=<value>`, `names-path:<value>` or
// `names-path!=<value>`, and the qualified `names-path:<edge-type>=<value>` and
// `names-path:<edge-type>!=<value>`. A bare value holding `*` or `?` is a glob;
// quoting it makes the characters literal.
func (parser *Parser) parseNamesPathPredicate() Expr {
	identToken := parser.advance()
	operator := parser.advance()

	var edgeType string

	// The qualified form is only possible after `:`. Probe for an edge type
	// followed by = or != without consuming input, the way the hierarchy
	// shortcuts probe for an alias; parsePredicate already advance()d the
	// keyword and the separator, so the parser's buffer is empty. Anything
	// else after the `:` is the value itself. A stored path never holds a
	// `=`, so reading `names-path:a=b` as qualified loses no match.
	if operator.Kind == TokenColon {
		savedPos := parser.lexer.pos

		typeCandidate := parser.lexer.Next()
		operatorCandidate := parser.lexer.Next()

		if typeCandidate.Kind == TokenIdent && (operatorCandidate.Kind == TokenEQ || operatorCandidate.Kind == TokenNE) {
			edgeType = typeCandidate.Value
			operator = operatorCandidate
		} else {
			parser.lexer.pos = savedPos
		}
	}

	if operator.Kind != TokenEQ && operator.Kind != TokenColon && operator.Kind != TokenNE {
		parser.appendTokenErr(operator, "names-path takes = or !=")

		return nil
	}

	valueToken := parser.lexer.NextValue()

	if !hasValue(valueToken) {
		parser.appendTokenErr(valueToken, "expected a path after names-path")

		return nil
	}

	return &NamesPathPredicate{
		Value:    valueToken.Value,
		EdgeType: edgeType,
		Pattern:  isPatternToken(valueToken),
		Negated:  operator.Kind == TokenNE,
		Pos:      identToken.Pos,
	}
}

// validateNamesPath checks that the workspace records path refs at all, that a
// qualified edge type is one of the paths edge types, and that a literal value
// is a workspace-relative path.
func (collector *validationCollector) validateNamesPath(pred *NamesPathPredicate) {
	pathTypes := manifest.PathEdgeTypeNames(collector.manifest.EdgeTypes)

	if len(pathTypes) == 0 {
		collector.add(pred.Pos, "names-path needs an edge type with paths = true", "declare one in tusk.toml, e.g. [edge-types.describes] with from and paths = true")

		return
	}

	if pred.EdgeType != "" && !slices.Contains(pathTypes, pred.EdgeType) {
		message := fmt.Sprintf("names-path: edge type %q does not set paths = true", pred.EdgeType)

		if _, declared := collector.manifest.EdgeTypes[pred.EdgeType]; !declared {
			message = fmt.Sprintf("names-path: edge type %q not declared in manifest", pred.EdgeType)
		}

		hint := suggestName(pred.EdgeType, pathTypes)

		if hint == "" {
			hint = "paths edge types: " + strings.Join(pathTypes, ", ")
		}

		collector.add(pred.Pos, message, hint)
	}

	if _, ok := pred.Targets(); !pred.Pattern && !ok {
		collector.add(pred.Pos, fmt.Sprintf("names-path %q is not a workspace-relative path", pred.Value), "drop a leading / and any ..")
	}
}

// compileNamesPath emits a membership test of the row's id against the pages
// whose refs match. columnPrefix qualifies the row's id column at depth.
func compileNamesPath(pred *NamesPathPredicate, columnPrefix string) (string, []any, error) {
	var (
		subquery string
		params   []any
	)

	if pred.Pattern {
		regexSQL, regexParams, patternErr := compilePattern("target", OpEQ, pred.Value)

		if patternErr != nil {
			return "", nil, patternErr
		}

		subquery = "SELECT source_id FROM path_refs WHERE " + regexSQL
		params = regexParams
	} else {
		targets, ok := pred.Targets()

		if !ok {
			return "", nil, fmt.Errorf("compile: names-path %q is not a workspace-relative path", pred.Value)
		}

		subquery = "SELECT source_id FROM path_refs WHERE target IN (" + strings.Repeat("?, ", len(targets)-1) + "?)"

		for _, target := range targets {
			params = append(params, target)
		}
	}

	if pred.EdgeType != "" {
		subquery += " AND type = ?"
		params = append(params, pred.EdgeType)
	}

	membership := columnPrefix + "id IN (" + subquery + ")"

	if pred.Negated {
		return "NOT (" + membership + ")", params, nil
	}

	return membership, params, nil
}
