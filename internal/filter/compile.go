package filter

import (
	"fmt"
	"strings"
	"time"

	"github.com/germanamz/tusk/internal/typeref"
)

// CompileOptions configures Compile.
type CompileOptions struct {
	SortKeys []SortKey
	Take     int
	Skip     int
}

// Compile turns an AST + options into parameterized SQL against the nodes table.
func Compile(expr Expr, opts CompileOptions) (string, []any, error) {
	if opts.Skip > 0 && opts.Take == 0 {
		return "", nil, fmt.Errorf("compile: --skip requires --take")
	}

	state := &compileState{}

	whereClause, params, whereErr := state.compileWhere(expr)

	if whereErr != nil {
		return "", nil, whereErr
	}

	var builder strings.Builder

	if len(state.ctes) > 0 {
		builder.WriteString("WITH RECURSIVE ")
		builder.WriteString(strings.Join(state.ctes, ", "))
		builder.WriteString(" ")
	}

	builder.WriteString(`SELECT id, type, path, title, properties_json, last_mtime, last_size, last_checksum, parent_id FROM nodes WHERE `)
	builder.WriteString(whereClause)

	if len(opts.SortKeys) > 0 {
		builder.WriteString(" ORDER BY ")
		builder.WriteString(compileOrderBy(opts.SortKeys))
	} else if state.defaultOrderBy != "" {
		builder.WriteString(" ORDER BY ")
		builder.WriteString(state.defaultOrderBy)
	}

	if opts.Take > 0 {
		fmt.Fprintf(&builder, " LIMIT %d", opts.Take)

		if opts.Skip > 0 {
			fmt.Fprintf(&builder, " OFFSET %d", opts.Skip)
		}
	}

	return builder.String(), params, nil
}

type compileState struct {
	ctes       []string
	cteCounter int
	// defaultOrderBy captures the first non-empty OrderedBy seen during
	// compileWhere's recursive descent. When two or more traversal shortcuts
	// appear in the same expression (e.g., `tree=X AND parent=Y`), the
	// leftmost one's OrderedBy wins. This is deterministic given a fixed
	// AST traversal order, but is undefined if the AST is reshaped.
	// Callers that want explicit ordering should pass --sort to override.
	defaultOrderBy string
}

// recursiveAncestorsCTE returns the recursive-CTE body that walks `type` edges
// from a seed node UP to its ancestors, to depth 5. Hierarchy edges are
// child→parent (the property lives on the child, e.g. `parent: <id>`, so the
// edge points from the child source to the parent target), so following
// source→target climbs toward the root. The seed's parent is its edge target;
// each recursion joins the next parent on source_id. ShortcutRoot uses this to
// find a node's topmost ancestor. Result column: target_id (the ancestor ids).
func recursiveAncestorsCTE(name string) string {
	return fmt.Sprintf(`%s AS (
    SELECT target_id, 1 AS depth FROM edges WHERE source_id = ? AND type = ?
    UNION ALL
    SELECT edges.target_id, %s.depth + 1 FROM %s
        JOIN edges ON edges.source_id = %s.target_id
        WHERE edges.type = ? AND %s.depth < 5
)`, name, name, name, name, name)
}

// recursiveDescendantsCTE returns the recursive-CTE body that walks `type` edges
// from a seed node DOWN to its descendants, to depth 5. For child→parent
// hierarchy edges, a node's children are the edge sources whose target is that
// node, so descending follows target→source: the seed's children are the
// sources of edges whose target is the seed, and each recursion joins the next
// generation on target_id. ShortcutTree uses this. Result column: node_id (the
// descendant ids).
func recursiveDescendantsCTE(name string) string {
	return fmt.Sprintf(`%s AS (
    SELECT source_id AS node_id, 1 AS depth FROM edges WHERE target_id = ? AND type = ?
    UNION ALL
    SELECT edges.source_id, %s.depth + 1 FROM %s
        JOIN edges ON edges.target_id = %s.node_id
        WHERE edges.type = ? AND %s.depth < 5
)`, name, name, name, name, name)
}

// compileBinary compiles the two operands of an AND/OR node on the same state —
// left first, so CTE numbering and the leftmost-OrderedBy-wins side effects are
// preserved — and joins their SQL with joiner, left params before right.
func (state *compileState) compileBinary(left, right Expr, joiner string) (string, []any, error) {
	leftSQL, leftParams, leftErr := state.compileWhere(left)

	if leftErr != nil {
		return "", nil, leftErr
	}

	rightSQL, rightParams, rightErr := state.compileWhere(right)

	if rightErr != nil {
		return "", nil, rightErr
	}

	return "(" + leftSQL + ") " + joiner + " (" + rightSQL + ")", append(leftParams, rightParams...), nil
}

func (state *compileState) compileWhere(expr Expr) (string, []any, error) {
	if expr == nil {
		return "1 = 1", nil, nil
	}

	switch typed := expr.(type) {
	case *OrExpr:
		return state.compileBinary(typed.Left, typed.Right, "OR")
	case *AndExpr:
		return state.compileBinary(typed.Left, typed.Right, "AND")
	case *NotExpr:
		inner, innerParams, innerErr := state.compileWhere(typed.Inner)

		if innerErr != nil {
			return "", nil, innerErr
		}

		return "NOT (" + inner + ")", innerParams, nil
	case *PropertyPredicate:
		return compileProperty(typed, "")
	case *ModifiedSincePredicate:
		return compileModifiedSince(typed)
	case *EdgePredicate:
		return compileEdgePredicate(typed, 0)
	case *TraversalShortcut:
		state.cteCounter++
		whereClause, ctes, traversalParams, traversalErr := compileTraversalShortcut(typed, state.cteCounter)

		if traversalErr != nil {
			return "", nil, traversalErr
		}

		state.ctes = append(state.ctes, ctes...)

		if typed.OrderedBy != "" && state.defaultOrderBy == "" {
			state.defaultOrderBy = fmt.Sprintf(
				`COALESCE(json_extract(nodes.properties_json, '$."%s"'), 0), nodes.id`,
				typed.OrderedBy,
			)
		}

		return whereClause, traversalParams, nil
	}

	return "", nil, fmt.Errorf("compile: unknown AST node type %T", expr)
}

var coreColumns = map[string]struct{}{
	"id":    {},
	"type":  {},
	"path":  {},
	"title": {},
}

// compileModifiedSince emits a `last_mtime >= ?` comparison against the
// nodes table. last_mtime is stored as unix nanoseconds (see
// internal/index/index.go schema), so the threshold is converted with
// UnixNano(). Either Duration or Since must be set by the validator;
// emitting SQL without a resolved threshold mirrors the
// TraversalShortcut.EdgeType check.
func compileModifiedSince(predicate *ModifiedSincePredicate) (string, []any, error) {
	var thresholdNs int64

	switch {
	case predicate.Duration > 0:
		thresholdNs = time.Now().Add(-predicate.Duration).UnixNano()
	case !predicate.Since.IsZero():
		thresholdNs = predicate.Since.UnixNano()
	default:
		return "", nil, fmt.Errorf("compile: modified-since has unresolved value (validator must run before compile)")
	}

	return "last_mtime >= ?", []any{thresholdNs}, nil
}

// compileTypeRef parses raw as a typeref and returns a SQL fragment plus
// params constraining a (source, type) pair on the nodes/edges table.
// columnPrefix is "" for the top-level nodes table or "<alias>." for an
// aliased row inside an edge predicate. Bare names emit the legacy
// `type <op> ?` shape; qualified names add a `source IS NULL` or
// `source = ?` conjunct per spec § Naming conventions.
func compileTypeRef(raw, columnPrefix string, op Op) (string, []any, error) {
	ref, parseErr := typeref.Parse(raw)

	if parseErr != nil {
		return "", nil, fmt.Errorf("compile: parse type ref: %w", parseErr)
	}

	sqlOp, opErr := opToSQL(op)

	if opErr != nil {
		return "", nil, opErr
	}

	switch ref.Scope {
	case typeref.ScopeAny:
		return fmt.Sprintf("%stype %s ?", columnPrefix, sqlOp), []any{ref.Type}, nil
	case typeref.ScopeUser:
		return fmt.Sprintf("%ssource IS NULL AND %stype %s ?", columnPrefix, columnPrefix, sqlOp), []any{ref.Type}, nil
	case typeref.ScopeSource:
		return fmt.Sprintf("%ssource = ? AND %stype %s ?", columnPrefix, columnPrefix, sqlOp), []any{ref.Source, ref.Type}, nil
	}

	return "", nil, fmt.Errorf("compile: unknown typeref scope %v", ref.Scope)
}

// compileProperty compiles a PropertyPredicate against a column namespace.
// columnPrefix is "" for the top-level nodes table (bare `type`,
// `json_extract(properties_json, ...)`) or "<alias>." for a row inside an edge
// predicate (`n0.type`, `json_extract(n0.properties_json, ...)`), following the
// same convention compileTypeRef uses. All type assertions are checked so a
// malformed AST returns an error instead of panicking.
func compileProperty(predicate *PropertyPredicate, columnPrefix string) (string, []any, error) {
	column, isCoreColumn := propertyColumn(predicate.Property)

	if predicate.Property == "type" && predicate.Op != OpRange {
		stringValue, ok := predicate.Value.(StringValue)

		if !ok {
			return "", nil, fmt.Errorf("compile: type predicate with non-StringValue")
		}

		return compileTypeRef(stringValue.V, columnPrefix, predicate.Op)
	}

	if predicate.Op == OpRange {
		rangeValue, ok := predicate.Value.(RangeValue)

		if !ok {
			return "", nil, fmt.Errorf("compile: OpRange with non-RangeValue")
		}

		if isCoreColumn {
			return columnPrefix + column + " BETWEEN ? AND ?", []any{rangeValue.Min, rangeValue.Max}, nil
		}

		extract := fmt.Sprintf(`CAST(json_extract(%sproperties_json, '$.%s') AS INTEGER)`, columnPrefix, predicate.Property)

		return extract + " BETWEEN ? AND ?", []any{rangeValue.Min, rangeValue.Max}, nil
	}

	stringValue, ok := predicate.Value.(StringValue)

	if !ok {
		return "", nil, fmt.Errorf("compile: PropertyPredicate.Value is not StringValue")
	}

	sqlOp, opErr := opToSQL(predicate.Op)

	if opErr != nil {
		return "", nil, opErr
	}

	if isCoreColumn {
		return columnPrefix + column + " " + sqlOp + " ?", []any{stringValue.V}, nil
	}

	if intValue, isBool := boolLiteralAsInt(stringValue, predicate.Op); isBool {
		return fmt.Sprintf(`json_extract(%sproperties_json, '$.%s') %s ?`, columnPrefix, predicate.Property, sqlOp), []any{intValue}, nil
	}

	if isNumericOp(predicate.Op) {
		return fmt.Sprintf(`CAST(json_extract(%sproperties_json, '$.%s') AS INTEGER) %s ?`, columnPrefix, predicate.Property, sqlOp), []any{stringValue.V}, nil
	}

	return fmt.Sprintf(`json_extract(%sproperties_json, '$.%s') %s ?`, columnPrefix, predicate.Property, sqlOp), []any{stringValue.V}, nil
}

// boolLiteralAsInt returns (1/0, true) when value is a bareword
// `true`/`false` and op is `=` or `!=`, signalling that the compiler
// should compare against an integer (SQLite's json_extract surfaces
// JSON booleans as integer 0/1). Quoted values (`="false"`) are treated
// as strings and skip this coercion, mirroring Python/JSON conventions
// where the bareword denotes a bool literal.
func boolLiteralAsInt(value StringValue, op Op) (int, bool) {
	if !value.Bareword {
		return 0, false
	}

	if op != OpEQ && op != OpNE {
		return 0, false
	}

	switch value.V {
	case "true":
		return 1, true
	case "false":
		return 0, true
	}

	return 0, false
}

func propertyColumn(name string) (string, bool) {
	if _, isCore := coreColumns[name]; isCore {
		return name, true
	}

	return "", false
}

// opToSQL maps a binary comparison operator to its SQL glyph. The glyph is the
// single source of truth in Op.String(); opToSQL refuses operators that are not
// binary comparisons (OpRange, which compiles to BETWEEN, and any unknown
// value) rather than emitting a "?" sentinel into a query.
func opToSQL(op Op) (string, error) {
	if op == OpRange || !op.valid() {
		return "", fmt.Errorf("compile: %v is not a binary comparison operator", op)
	}

	return op.String(), nil
}

func isNumericOp(op Op) bool {
	switch op {
	case OpLT, OpLE, OpGT, OpGE:
		return true
	}

	return false
}

func compileOrderBy(keys []SortKey) string {
	parts := make([]string, 0, len(keys))

	for _, key := range keys {
		column, isCore := propertyColumn(key.Property)
		expression := column

		if !isCore {
			expression = fmt.Sprintf(`json_extract(properties_json, '$.%s')`, key.Property)
		}

		direction := "ASC"

		if key.Descending {
			direction = "DESC"
		}

		parts = append(parts, expression+" "+direction)
	}

	return strings.Join(parts, ", ")
}

func compileEdgePredicate(predicate *EdgePredicate, depth int) (string, []any, error) {
	edgeAlias := fmt.Sprintf("e%d", depth)
	nodeAlias := fmt.Sprintf("n%d", depth)
	parentRef := "nodes"

	if depth > 0 {
		parentRef = fmt.Sprintf("n%d", depth-1)
	}

	var sourceColumn, joinColumn string

	if predicate.Direction == DirectionOutgoing {
		sourceColumn = fmt.Sprintf("%s.source_id = %s.id", edgeAlias, parentRef)
		joinColumn = fmt.Sprintf("%s.id = %s.target_id", nodeAlias, edgeAlias)
	} else {
		sourceColumn = fmt.Sprintf("%s.target_id = %s.id", edgeAlias, parentRef)
		joinColumn = fmt.Sprintf("%s.id = %s.source_id", nodeAlias, edgeAlias)
	}

	edgeClause, edgeParams, edgeErr := compileTypeRef(predicate.EdgeType, edgeAlias+".", OpEQ)

	if edgeErr != nil {
		return "", nil, edgeErr
	}

	if predicate.Inner == nil {
		sql := fmt.Sprintf("EXISTS (SELECT 1 FROM edges %s WHERE %s AND %s)", edgeAlias, sourceColumn, edgeClause)

		return sql, edgeParams, nil
	}

	innerSQL, innerParams, innerErr := compileInnerOnAlias(predicate.Inner, nodeAlias, depth)

	if innerErr != nil {
		return "", nil, innerErr
	}

	sql := fmt.Sprintf("EXISTS (SELECT 1 FROM edges %s JOIN nodes %s ON %s WHERE %s AND %s AND %s)",
		edgeAlias, nodeAlias, joinColumn, sourceColumn, edgeClause, innerSQL)

	params := append(edgeParams, innerParams...)

	return sql, params, nil
}

func compileInnerOnAlias(inner Expr, alias string, depth int) (string, []any, error) {
	switch typed := inner.(type) {
	case *PropertyPredicate:
		return compileProperty(typed, alias+".")
	case *EdgePredicate:
		return compileEdgePredicate(typed, depth+1)
	}

	return "", nil, fmt.Errorf("compile: unsupported inner predicate type %T", inner)
}

// compileTraversalShortcut returns a WHERE-clause fragment, a slice of CTE
// definition strings, and params. The caller stitches the CTE definitions
// into a single WITH RECURSIVE clause prepended to the SELECT.
//
// The edge type is supplied by shortcut.EdgeType, populated by the
// validator from the manifest. If empty, the validator did not run or the
// AST was hand-constructed without resolution — return an error rather
// than emit ambiguous SQL.
func compileTraversalShortcut(shortcut *TraversalShortcut, counter int) (string, []string, []any, error) {
	if shortcut.EdgeType == "" {
		return "", nil, nil, fmt.Errorf("compile: traversal shortcut has unresolved edge type (validator must run before compile)")
	}

	edge := shortcut.EdgeType

	switch shortcut.Kind {
	case ShortcutParentOf:
		whereClause := "EXISTS (SELECT 1 FROM edges WHERE source_id = nodes.id AND type = ? AND target_id = ?)"

		return whereClause, nil, []any{edge, shortcut.NodeID}, nil
	case ShortcutTree:
		cteName := fmt.Sprintf("descendants_%d", counter)
		cteBody := recursiveDescendantsCTE(cteName)

		whereClause := fmt.Sprintf("nodes.id IN (SELECT node_id FROM %s)", cteName)

		return whereClause, []string{cteBody}, []any{shortcut.NodeID, edge, edge}, nil
	case ShortcutRoot:
		ascendantsName := fmt.Sprintf("ascendants_%d", counter)
		descendantsName := fmt.Sprintf("from_root_%d", counter)
		ascendantsBody := recursiveAncestorsCTE(ascendantsName)

		// Seed from the seed node's topmost ancestor (deepest ascendant) — or
		// the seed itself when it is already a root — then descend the whole
		// tree from there (target→source, matching recursiveDescendantsCTE).
		descendantsBody := fmt.Sprintf(`%s AS (
    SELECT id AS node_id, 1 AS depth FROM nodes
        WHERE id IN (SELECT target_id FROM %s ORDER BY depth DESC LIMIT 1)
            OR id = ?
    UNION ALL
    SELECT edges.source_id, %s.depth + 1 FROM %s
        JOIN edges ON edges.target_id = %s.node_id
        WHERE edges.type = ? AND %s.depth < 5
)`, descendantsName, ascendantsName, descendantsName, descendantsName, descendantsName, descendantsName)

		whereClause := fmt.Sprintf("nodes.id IN (SELECT node_id FROM %s)", descendantsName)

		return whereClause, []string{ascendantsBody, descendantsBody}, []any{shortcut.NodeID, edge, edge, shortcut.NodeID, edge}, nil
	}

	return "", nil, nil, fmt.Errorf("compile: unknown traversal shortcut kind %v", shortcut.Kind)
}
