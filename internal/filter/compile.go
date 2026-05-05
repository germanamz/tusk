package filter

import (
	"fmt"
	"strings"
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

	builder.WriteString(`SELECT id, type, path, title, properties_json, last_mtime, last_size, last_checksum FROM nodes WHERE `)
	builder.WriteString(whereClause)

	if len(opts.SortKeys) > 0 {
		builder.WriteString(" ORDER BY ")
		builder.WriteString(compileOrderBy(opts.SortKeys))
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
}

func (state *compileState) compileWhere(expr Expr) (string, []any, error) {
	if expr == nil {
		return "1 = 1", nil, nil
	}

	switch typed := expr.(type) {
	case *OrExpr:
		left, leftParams, leftErr := state.compileWhere(typed.Left)

		if leftErr != nil {
			return "", nil, leftErr
		}

		right, rightParams, rightErr := state.compileWhere(typed.Right)

		if rightErr != nil {
			return "", nil, rightErr
		}

		return "(" + left + ") OR (" + right + ")", append(leftParams, rightParams...), nil
	case *AndExpr:
		left, leftParams, leftErr := state.compileWhere(typed.Left)

		if leftErr != nil {
			return "", nil, leftErr
		}

		right, rightParams, rightErr := state.compileWhere(typed.Right)

		if rightErr != nil {
			return "", nil, rightErr
		}

		return "(" + left + ") AND (" + right + ")", append(leftParams, rightParams...), nil
	case *NotExpr:
		inner, innerParams, innerErr := state.compileWhere(typed.Inner)

		if innerErr != nil {
			return "", nil, innerErr
		}

		return "NOT (" + inner + ")", innerParams, nil
	case *PropertyPredicate:
		return compileProperty(typed)
	case *EdgePredicate:
		return compileEdgePredicate(typed, 0)
	case *TraversalShortcut:
		state.cteCounter++
		whereClause, ctes, traversalParams, traversalErr := compileTraversalShortcut(typed, state.cteCounter)

		if traversalErr != nil {
			return "", nil, traversalErr
		}

		state.ctes = append(state.ctes, ctes...)

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

func compileProperty(predicate *PropertyPredicate) (string, []any, error) {
	column, isCoreColumn := propertyColumn(predicate.Property)

	if predicate.Op == OpRange {
		rangeValue, ok := predicate.Value.(RangeValue)

		if !ok {
			return "", nil, fmt.Errorf("compile: OpRange with non-RangeValue")
		}

		if isCoreColumn {
			return column + " BETWEEN ? AND ?", []any{rangeValue.Min, rangeValue.Max}, nil
		}

		extract := fmt.Sprintf(`CAST(json_extract(properties_json, '$.%s') AS INTEGER)`, predicate.Property)

		return extract + " BETWEEN ? AND ?", []any{rangeValue.Min, rangeValue.Max}, nil
	}

	stringValue, ok := predicate.Value.(StringValue)

	if !ok {
		return "", nil, fmt.Errorf("compile: PropertyPredicate.Value is not StringValue")
	}

	sqlOp := opToSQL(predicate.Op)

	if isCoreColumn {
		return column + " " + sqlOp + " ?", []any{stringValue.V}, nil
	}

	if isNumericOp(predicate.Op) {
		return fmt.Sprintf(`CAST(json_extract(properties_json, '$.%s') AS INTEGER) %s ?`, predicate.Property, sqlOp), []any{stringValue.V}, nil
	}

	return fmt.Sprintf(`json_extract(properties_json, '$.%s') %s ?`, predicate.Property, sqlOp), []any{stringValue.V}, nil
}

func propertyColumn(name string) (string, bool) {
	if _, isCore := coreColumns[name]; isCore {
		return name, true
	}

	return "", false
}

func opToSQL(op Op) string {
	switch op {
	case OpEQ:
		return "="
	case OpNE:
		return "!="
	case OpLT:
		return "<"
	case OpLE:
		return "<="
	case OpGT:
		return ">"
	case OpGE:
		return ">="
	}

	return "?"
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

	if predicate.Inner == nil {
		sql := fmt.Sprintf("EXISTS (SELECT 1 FROM edges %s WHERE %s AND %s.type = ?)", edgeAlias, sourceColumn, edgeAlias)

		return sql, []any{predicate.EdgeType}, nil
	}

	innerSQL, innerParams, innerErr := compileInnerOnAlias(predicate.Inner, nodeAlias, depth)

	if innerErr != nil {
		return "", nil, innerErr
	}

	sql := fmt.Sprintf("EXISTS (SELECT 1 FROM edges %s JOIN nodes %s ON %s WHERE %s AND %s.type = ? AND %s)",
		edgeAlias, nodeAlias, joinColumn, sourceColumn, edgeAlias, innerSQL)

	params := append([]any{predicate.EdgeType}, innerParams...)

	return sql, params, nil
}

func compileInnerOnAlias(inner Expr, alias string, depth int) (string, []any, error) {
	switch typed := inner.(type) {
	case *PropertyPredicate:
		return compilePropertyOnAlias(typed, alias)
	case *EdgePredicate:
		return compileEdgePredicate(typed, depth+1)
	}

	return "", nil, fmt.Errorf("compile: unsupported inner predicate type %T", inner)
}

func compilePropertyOnAlias(predicate *PropertyPredicate, alias string) (string, []any, error) {
	if predicate.Op == OpRange {
		rangeValue := predicate.Value.(RangeValue)

		if _, isCore := coreColumns[predicate.Property]; isCore {
			return fmt.Sprintf("%s.%s BETWEEN ? AND ?", alias, predicate.Property), []any{rangeValue.Min, rangeValue.Max}, nil
		}

		return fmt.Sprintf(`CAST(json_extract(%s.properties_json, '$.%s') AS INTEGER) BETWEEN ? AND ?`, alias, predicate.Property), []any{rangeValue.Min, rangeValue.Max}, nil
	}

	stringValue := predicate.Value.(StringValue)
	sqlOp := opToSQL(predicate.Op)

	if _, isCore := coreColumns[predicate.Property]; isCore {
		return fmt.Sprintf("%s.%s %s ?", alias, predicate.Property, sqlOp), []any{stringValue.V}, nil
	}

	if isNumericOp(predicate.Op) {
		return fmt.Sprintf(`CAST(json_extract(%s.properties_json, '$.%s') AS INTEGER) %s ?`, alias, predicate.Property, sqlOp), []any{stringValue.V}, nil
	}

	return fmt.Sprintf(`json_extract(%s.properties_json, '$.%s') %s ?`, alias, predicate.Property, sqlOp), []any{stringValue.V}, nil
}

// compileTraversalShortcut returns a WHERE-clause fragment, a slice of CTE
// definition strings, and params. The caller stitches the CTE definitions
// into a single WITH RECURSIVE clause prepended to the SELECT.
func compileTraversalShortcut(shortcut *TraversalShortcut, counter int) (string, []string, []any, error) {
	switch shortcut.Kind {
	case ShortcutParentOf:
		whereClause := "EXISTS (SELECT 1 FROM edges WHERE source_id = nodes.id AND type = 'parent' AND target_id = ?)"

		return whereClause, nil, []any{shortcut.NodeID}, nil
	case ShortcutTree:
		cteName := fmt.Sprintf("descendants_%d", counter)
		cteBody := fmt.Sprintf(`%s AS (
    SELECT target_id, 1 AS depth FROM edges WHERE source_id = ? AND type = 'parent'
    UNION ALL
    SELECT edges.target_id, %s.depth + 1 FROM %s
        JOIN edges ON edges.source_id = %s.target_id
        WHERE edges.type = 'parent' AND %s.depth < 5
)`, cteName, cteName, cteName, cteName, cteName)

		whereClause := fmt.Sprintf("nodes.id IN (SELECT target_id FROM %s)", cteName)

		return whereClause, []string{cteBody}, []any{shortcut.NodeID}, nil
	case ShortcutRoot:
		ascendantsName := fmt.Sprintf("ascendants_%d", counter)
		descendantsName := fmt.Sprintf("from_root_%d", counter)
		ascendantsBody := fmt.Sprintf(`%s AS (
    SELECT target_id, 1 AS depth FROM edges WHERE source_id = ? AND type = 'parent'
    UNION ALL
    SELECT edges.target_id, %s.depth + 1 FROM %s
        JOIN edges ON edges.source_id = %s.target_id
        WHERE edges.type = 'parent' AND %s.depth < 5
)`, ascendantsName, ascendantsName, ascendantsName, ascendantsName, ascendantsName)

		descendantsBody := fmt.Sprintf(`%s AS (
    SELECT id AS target_id, 1 AS depth FROM nodes
        WHERE id IN (SELECT target_id FROM %s ORDER BY depth DESC LIMIT 1)
            OR id = ?
    UNION ALL
    SELECT edges.target_id, %s.depth + 1 FROM %s
        JOIN edges ON edges.source_id = %s.target_id
        WHERE edges.type = 'parent' AND %s.depth < 5
)`, descendantsName, ascendantsName, descendantsName, descendantsName, descendantsName, descendantsName)

		whereClause := fmt.Sprintf("nodes.id IN (SELECT target_id FROM %s)", descendantsName)

		return whereClause, []string{ascendantsBody, descendantsBody}, []any{shortcut.NodeID, shortcut.NodeID}, nil
	}

	return "", nil, nil, fmt.Errorf("compile: unknown traversal shortcut kind %v", shortcut.Kind)
}
