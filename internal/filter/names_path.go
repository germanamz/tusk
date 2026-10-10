package filter

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/pathglob"
	"github.com/germanamz/tusk/internal/pathref"
)

// NamesPathKeyword is the reserved key of the names-path predicate. Like the
// hierarchy shortcuts, it shadows a frontmatter property of the same name.
const NamesPathKeyword = "names-path"

// NamesPathPredicate matches the pages that name a workspace path in inline
// code, through the path_refs table that edge types with paths = true fill.
//
// A literal Value is a path: it matches a page naming that path or any
// directory above it, since a directory ref covers what it holds. A Pattern
// value is a glob over the named paths themselves, with no ancestor expansion.
// Negated is the != form. Refs belong to file rows, so a sub-unit row never
// matches `=` and always matches `!=` (and `NOT names-path=`); scope a negated
// test with `type=` when the result may hold sub-units.
type NamesPathPredicate struct {
	Value   string
	Pattern bool
	Negated bool
	Pos     int
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

// Matches reports whether a stored ref target satisfies the predicate, ignoring
// Negated: for a literal, target is the path or a directory above it; for a
// pattern, target matches the glob.
func (pred *NamesPathPredicate) Matches(target string) bool {
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
// `names-path!=<value>`. A bare value holding `*` or `?` is a glob; quoting it
// makes the characters literal.
func (parser *Parser) parseNamesPathPredicate() Expr {
	identToken := parser.advance()
	operator := parser.advance()

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
		Value:   valueToken.Value,
		Pattern: isPatternToken(valueToken),
		Negated: operator.Kind == TokenNE,
		Pos:     identToken.Pos,
	}
}

// validateNamesPath checks that the workspace records path refs at all, and
// that a literal value is a workspace-relative path.
func (collector *validationCollector) validateNamesPath(pred *NamesPathPredicate) {
	if len(manifest.PathEdgeTypeNames(collector.manifest.EdgeTypes)) == 0 {
		collector.add(pred.Pos, "names-path needs an edge type with paths = true", "declare one in tusk.toml, e.g. [edge-types.describes] with from and paths = true")

		return
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

	membership := columnPrefix + "id IN (" + subquery + ")"

	if pred.Negated {
		return "NOT (" + membership + ")", params, nil
	}

	return membership, params, nil
}
