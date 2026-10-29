package filter

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/germanamz/tusk/internal/pathglob"
	"github.com/germanamz/tusk/internal/typeref"
)

// ErrSkipRequiresTake is returned by Compile when Skip is set without an
// effective Take. Exported as a sentinel so the semantic query path — which
// windows after ranking and never reaches this compiler with the user's skip —
// can enforce the same contract via errors.Is.
var ErrSkipRequiresTake = errors.New("compile: --skip requires --take")

// CompileOptions configures Compile.
type CompileOptions struct {
	SortKeys []SortKey
	Take     int
	Skip     int

	// FilesOnly restricts the result to file nodes. Sub-units share their
	// file's path, so a path filter otherwise returns every section and
	// paragraph of each matching file too.
	FilesOnly bool

	// Now is the clock that relative dates (`today-90d`, `now-48h`) and
	// `modified-since:<duration>` resolve against. Required when the filter
	// holds one; Compile fails rather than guess.
	Now time.Time
}

// Compile turns an AST + options into parameterized SQL against the nodes table.
func Compile(expr Expr, opts CompileOptions) (string, []any, error) {
	if opts.Skip > 0 && opts.Take == 0 {
		return "", nil, ErrSkipRequiresTake
	}

	state := &compileState{now: opts.Now}

	whereClause, whereParams, whereErr := state.compileWhere(expr, 0)

	if whereErr != nil {
		return "", nil, whereErr
	}

	// Bind params in the order their placeholders appear in the assembled SQL:
	// the CTE bodies (prepended below as `WITH RECURSIVE …`) come first, then the
	// WHERE clause. Prepending state.cteParams here — rather than binding in
	// AST-traversal order — is what keeps tree=/root= shortcuts correct when they
	// are not the leftmost param-contributing leaf.
	params := append(append([]any{}, state.cteParams...), whereParams...)

	var builder strings.Builder

	if len(state.ctes) > 0 {
		builder.WriteString("WITH RECURSIVE ")
		builder.WriteString(strings.Join(state.ctes, ", "))
		builder.WriteString(" ")
	}

	builder.WriteString(`SELECT id, type, path, title, properties_json, last_mtime, last_size, last_checksum, parent_id FROM nodes WHERE `)

	if opts.FilesOnly {
		whereClause = "(" + whereClause + ") AND parent_id IS NULL"
	}

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
	ctes []string
	// cteParams holds the bind params for the CTE bodies collected in ctes, in
	// the same order the CTEs are emitted. Because Compile prepends the CTE
	// definitions ahead of the SELECT (`WITH RECURSIVE <ctes> SELECT … WHERE`),
	// their `?` placeholders are positionally first in the assembled statement.
	// Keeping their params in a dedicated slice — instead of interleaving them
	// with WHERE-clause params in AST-traversal order — lets Compile prepend
	// them so positional binding matches the string order regardless of where
	// the emitting shortcut sits in the expression (the regression for tree=/
	// root= misbinding when they were not the leftmost leaf).
	cteParams  []any
	cteCounter int
	// defaultOrderBy captures the first non-empty OrderedBy seen during
	// compileWhere's recursive descent. When two or more traversal shortcuts
	// appear in the same expression (e.g., `tree=X AND parent=Y`), the
	// leftmost one's OrderedBy wins. This is deterministic given a fixed
	// AST traversal order, but is undefined if the AST is reshaped.
	// Callers that want explicit ordering should pass --sort to override.
	defaultOrderBy string
	// now is CompileOptions.Now, the clock relative dates and
	// modified-since durations resolve against. Zero when the caller set none.
	now time.Time
}

// walkStep returns what the recursive step of a walk CTE named name selects as
// the next row's depth, and the condition that stops it. A bounded walk counts
// hops up to maxHops. An unbounded one counts only up to minHops, as far as the
// final `depth >= minHops` test needs, so the depth stops growing: a node the
// walk reaches again is a row the CTE's UNION has already seen, and the walk
// ends even on a cycle.
func walkStep(name string, minHops, maxHops int) (depth, stop string) {
	if maxHops > 0 {
		return name + ".depth + 1", fmt.Sprintf(" AND %s.depth < %d", name, maxHops)
	}

	if minHops <= 1 {
		return "1", ""
	}

	return fmt.Sprintf("MIN(%s.depth + 1, %d)", name, minHops), ""
}

// hierarchyColumns returns the edges columns that hold a hierarchy edge's
// child and parent: the source names its parent by default (`parent: <id>`
// points child→parent), and parentIsSource turns that around for edges such as
// has-part, where the parent names its children.
func hierarchyColumns(parentIsSource bool) (child, parent string) {
	if parentIsSource {
		return "target_id", "source_id"
	}

	return "source_id", "target_id"
}

// compileBinary compiles the two operands of an AND/OR node on the same state —
// left first, so CTE numbering and the leftmost-OrderedBy-wins side effects are
// preserved — and joins their SQL with joiner, left params before right.
func (state *compileState) compileBinary(left, right Expr, joiner string, depth int) (string, []any, error) {
	leftSQL, leftParams, leftErr := state.compileWhere(left, depth)

	if leftErr != nil {
		return "", nil, leftErr
	}

	rightSQL, rightParams, rightErr := state.compileWhere(right, depth)

	if rightErr != nil {
		return "", nil, rightErr
	}

	return "(" + leftSQL + ") " + joiner + " (" + rightSQL + ")", append(leftParams, rightParams...), nil
}

// rowRef names the node row an expression at depth constrains: the outer
// nodes table at depth 0, or the target alias n<depth-1> of the edge predicate
// that encloses it.
func rowRef(depth int) string {
	if depth == 0 {
		return "nodes"
	}

	return fmt.Sprintf("n%d", depth-1)
}

// columnPrefix is the column qualifier for the row at depth: none for the outer
// nodes table, whose columns the top-level WHERE names bare, and "n<k>." for an
// edge's target alias.
func columnPrefix(depth int) string {
	if depth == 0 {
		return ""
	}

	return rowRef(depth) + "."
}

// compileWhere compiles expr against the row at depth (see rowRef). Every
// expression kind compiles at any depth, so the term after an edge arrow can be
// a group, a NOT, a shortcut, or a recency check as well as a property
// comparison or another hop.
func (state *compileState) compileWhere(expr Expr, depth int) (string, []any, error) {
	if expr == nil {
		return "1 = 1", nil, nil
	}

	switch typed := expr.(type) {
	case *OrExpr:
		return state.compileBinary(typed.Left, typed.Right, "OR", depth)
	case *AndExpr:
		return state.compileBinary(typed.Left, typed.Right, "AND", depth)
	case *NotExpr:
		inner, innerParams, innerErr := state.compileWhere(typed.Inner, depth)

		if innerErr != nil {
			return "", nil, innerErr
		}

		// A comparison against a missing property is NULL in SQL, and so is
		// NOT NULL, which would drop the row from both p=v and NOT p=v. IS
		// TRUE reads NULL as false, so NOT matches exactly the rows the inner
		// term doesn't.
		return "NOT ((" + inner + ") IS TRUE)", innerParams, nil
	case *PropertyPredicate:
		return state.compileProperty(typed, columnPrefix(depth))
	case *ModifiedSincePredicate:
		return compileModifiedSince(typed, columnPrefix(depth), state.now)
	case *NamesPathPredicate:
		return compileNamesPath(typed, columnPrefix(depth))
	case *EdgePredicate:
		return state.compileEdgePredicate(typed, depth)
	case *TraversalShortcut:
		state.cteCounter++
		whereClause, ctes, cteParams, whereParams, traversalErr := compileTraversalShortcut(typed, rowRef(depth), state.cteCounter)

		if traversalErr != nil {
			return "", nil, traversalErr
		}

		// CTE definitions and their params are collected on the state so Compile
		// can prepend both, in emission order, ahead of the WHERE clause. Only
		// whereParams belong to the fragment returned here (the placeholders that
		// sit inline in whereClause); the CTE params travel separately.
		state.ctes = append(state.ctes, ctes...)
		state.cteParams = append(state.cteParams, cteParams...)

		// Only a top-level shortcut orders the result. Inside an edge predicate
		// it constrains the edge's target, whose sibling order says nothing
		// about the rows returned.
		if depth == 0 && typed.OrderedBy != "" && state.defaultOrderBy == "" {
			state.defaultOrderBy = fmt.Sprintf(
				`COALESCE(json_extract(nodes.properties_json, '$."%s"'), 0), nodes.id`,
				typed.OrderedBy,
			)
		}

		return whereClause, whereParams, nil
	}

	return "", nil, fmt.Errorf("compile: unknown AST node type %T", expr)
}

var coreColumns = map[string]struct{}{
	"id":    {},
	"type":  {},
	"path":  {},
	"title": {},
}

// compileModifiedSince emits a `last_mtime >= ?` comparison against the row
// columnPrefix names ("" for the nodes table, "<alias>." inside an edge
// predicate). last_mtime is stored as unix nanoseconds (see
// internal/index/index.go schema), so the threshold is converted with
// UnixNano(). Either Duration or Since must be set by the validator;
// emitting SQL without a resolved threshold mirrors the
// TraversalShortcut.EdgeType check. A duration counts back from now, the
// compile clock, which the caller must set.
func compileModifiedSince(predicate *ModifiedSincePredicate, columnPrefix string, now time.Time) (string, []any, error) {
	var thresholdNs int64

	switch {
	case predicate.Duration > 0:
		if now.IsZero() {
			return "", nil, fmt.Errorf("compile: modified-since:%s needs CompileOptions.Now", predicate.Raw)
		}

		thresholdNs = now.Add(-predicate.Duration).UnixNano()
	case !predicate.Since.IsZero():
		thresholdNs = predicate.Since.UnixNano()
	default:
		return "", nil, fmt.Errorf("compile: modified-since has unresolved value (validator must run before compile)")
	}

	return columnPrefix + "last_mtime >= ?", []any{thresholdNs}, nil
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

	// A qualified `!=` is the complement of its `=`: a row from another
	// source, or with no source, passes, which `source = ? AND type != ?`
	// would drop.
	if op == OpNE && ref.Scope != typeref.ScopeAny {
		equal, params, equalErr := compileTypeRef(raw, columnPrefix, OpEQ)

		if equalErr != nil {
			return "", nil, equalErr
		}

		return "NOT ((" + equal + ") IS TRUE)", params, nil
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
//
// `!=` is the complement of `=`: it compiles to the null-safe IS NOT, so a row
// without the property passes it.
func (state *compileState) compileProperty(predicate *PropertyPredicate, columnPrefix string) (string, []any, error) {
	column, isCoreColumn := propertyColumn(predicate.Property)

	if _, isAny := predicate.Value.(AnyValue); isAny {
		return compilePresence(predicate, columnPrefix, column, isCoreColumn)
	}

	if list, isList := predicate.Value.(ListValue); isList || predicate.Op == OpContains {
		if isCoreColumn {
			return "", nil, fmt.Errorf("compile: %q is not a list", predicate.Property)
		}

		return state.compileListMatch(predicate, columnPrefix, list)
	}

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

		switch {
		case isTextComparedType(predicate.ResolvedType):
			low, lowErr := state.temporalOperand(StringValue{V: rangeValue.Min, Bareword: rangeValue.MinBareword}, predicate.ResolvedType)
			high, highErr := state.temporalOperand(StringValue{V: rangeValue.Max, Bareword: rangeValue.MaxBareword}, predicate.ResolvedType)

			if boundErr := errors.Join(lowErr, highErr); boundErr != nil {
				return "", nil, boundErr
			}

			return quotedExtract(columnPrefix, predicate.Property) + " BETWEEN ? AND ?", []any{low, high}, nil
		case predicate.ResolvedType == "string":
			return quotedExtract(columnPrefix, predicate.Property) + " BETWEEN ? AND ?", []any{rangeValue.Min, rangeValue.Max}, nil
		case predicate.ResolvedType == "enum":
			return compileEnumRange(predicate, columnPrefix, rangeValue)
		case predicate.ResolvedType == "float":
			return "CAST(" + quotedExtract(columnPrefix, predicate.Property) + " AS REAL) BETWEEN ? AND ?", []any{rangeValue.Min, rangeValue.Max}, nil
		}

		// int and undeclared properties keep integer affinity.
		extract := "CAST(" + quotedExtract(columnPrefix, predicate.Property) + " AS INTEGER)"

		return extract + " BETWEEN ? AND ?", []any{rangeValue.Min, rangeValue.Max}, nil
	}

	stringValue, ok := predicate.Value.(StringValue)

	if !ok {
		return "", nil, fmt.Errorf("compile: PropertyPredicate.Value is not StringValue")
	}

	if _, matchable := patternProperties[predicate.Property]; matchable && stringValue.Bareword && pathglob.IsPattern(stringValue.V) {
		return compilePattern(columnPrefix+column, predicate.Op, stringValue.V)
	}

	sqlOp, opErr := opToSQL(predicate.Op)

	if opErr != nil {
		return "", nil, opErr
	}

	if predicate.Op == OpNE {
		sqlOp = "IS NOT"
	}

	if isCoreColumn {
		return columnPrefix + column + " " + sqlOp + " ?", []any{stringValue.V}, nil
	}

	if intValue, isBool := boolLiteralAsInt(stringValue, predicate.Op); isBool {
		return quotedExtract(columnPrefix, predicate.Property) + " " + sqlOp + " ?", []any{intValue}, nil
	}

	// Choose the comparison affinity from the property's declared type so that
	// equality AND ordering both compare in the property's own domain:
	//   int            -> CAST(... AS INTEGER)  (fixes exact `=`, which previously
	//                     TEXT-compared a bound string against an integer-stored
	//                     value and never matched)
	//   float          -> CAST(... AS REAL)     (no truncation of the fractional part)
	//   date/datetime  -> TEXT                  (ISO strings sort chronologically)
	//   string         -> TEXT                  (lexical, not integer-coerced)
	//   enum ordering  -> IN-set by declared order; enum equality stays a name match
	// An undeclared property keeps the legacy behaviour — integer affinity for the
	// ordering operators, TEXT for equality — so ad-hoc property queries are
	// unaffected.
	switch {
	case isTextComparedType(predicate.ResolvedType):
		operand, operandErr := state.temporalOperand(stringValue, predicate.ResolvedType)

		if operandErr != nil {
			return "", nil, operandErr
		}

		return quotedExtract(columnPrefix, predicate.Property) + " " + sqlOp + " ?", []any{operand}, nil
	case predicate.ResolvedType == "string":
		return quotedExtract(columnPrefix, predicate.Property) + " " + sqlOp + " ?", []any{stringValue.V}, nil
	case predicate.ResolvedType == "enum":
		if isNumericOp(predicate.Op) {
			return compileEnumOrdering(predicate, columnPrefix, stringValue.V)
		}

		return quotedExtract(columnPrefix, predicate.Property) + " " + sqlOp + " ?", []any{stringValue.V}, nil
	case predicate.ResolvedType == "float":
		return "CAST(" + quotedExtract(columnPrefix, predicate.Property) + " AS REAL) " + sqlOp + " ?", []any{stringValue.V}, nil
	case predicate.ResolvedType == "int":
		return "CAST(" + quotedExtract(columnPrefix, predicate.Property) + " AS INTEGER) " + sqlOp + " ?", []any{stringValue.V}, nil
	}

	// Undeclared property: legacy affinity — integer for ordering, TEXT for equality.
	if isNumericOp(predicate.Op) {
		return "CAST(" + quotedExtract(columnPrefix, predicate.Property) + " AS INTEGER) " + sqlOp + " ?", []any{stringValue.V}, nil
	}

	return quotedExtract(columnPrefix, predicate.Property) + " " + sqlOp + " ?", []any{stringValue.V}, nil
}

// compilePresence compiles `p=*` (has a value) and `p!=*` (missing). A core
// column has a value unless it is empty. A frontmatter property has one when
// json_each yields a non-null item: a scalar yields itself, while absent, null
// and an empty list yield none.
func compilePresence(predicate *PropertyPredicate, columnPrefix, column string, isCoreColumn bool) (string, []any, error) {
	has := fmt.Sprintf("EXISTS (SELECT 1 FROM %s WHERE item.value IS NOT NULL)", jsonItems(columnPrefix, predicate.Property))

	if isCoreColumn {
		has = "COALESCE(" + columnPrefix + column + ", '') <> ''"
	}

	switch predicate.Op {
	case OpEQ:
		return has, nil, nil
	case OpNE:
		return "NOT (" + has + ")", nil, nil
	}

	return "", nil, fmt.Errorf("compile: `*` only works with = or !=, got %s", predicate.Op)
}

// compileListMatch compiles `p~=v` (the list holds v) and `p=[a, b]` (it holds
// every item, in any order), plus the `!=` complement of a list. Each item is
// one EXISTS over the property's items; a list value ANDs them.
func (state *compileState) compileListMatch(predicate *PropertyPredicate, columnPrefix string, list ListValue) (string, []any, error) {
	items := list.Items

	if predicate.Op == OpContains {
		stringValue, ok := predicate.Value.(StringValue)

		if !ok {
			return "", nil, fmt.Errorf("compile: ~= with non-StringValue")
		}

		items = []StringValue{stringValue}
	}

	if len(items) == 0 {
		return "", nil, fmt.Errorf("compile: a list needs at least one item")
	}

	clauses := make([]string, 0, len(items))

	var params []any

	for _, item := range items {
		clause, param, itemErr := state.compileItemMatch(predicate, columnPrefix, item)

		if itemErr != nil {
			return "", nil, itemErr
		}

		clauses = append(clauses, clause)
		params = append(params, param)
	}

	holdsAll := strings.Join(clauses, " AND ")

	switch predicate.Op {
	case OpEQ, OpContains:
		return holdsAll, params, nil
	case OpNE:
		return "NOT (" + holdsAll + ")", params, nil
	}

	return "", nil, fmt.Errorf("compile: a list only works with = or !=, got %s", predicate.Op)
}

// compileItemMatch returns an EXISTS that is true when one of the property's
// items equals operand, compared in the item type's domain. json_each walks
// an array's items and yields a scalar as its single row, so an undeclared
// property matches whether it holds a list or one value.
func (state *compileState) compileItemMatch(predicate *PropertyPredicate, columnPrefix string, operand StringValue) (string, any, error) {
	itemType := comparedType(predicate)
	comparison := "item.value = ?"

	var param any = operand.V

	if intValue, isBool := boolLiteralAsInt(operand, OpEQ); isBool && (itemType == "" || itemType == "bool") {
		param = intValue
	} else {
		switch {
		case itemType == "int":
			comparison = "CAST(item.value AS INTEGER) = ?"
		case itemType == "float":
			comparison = "CAST(item.value AS REAL) = ?"
		case isTextComparedType(itemType):
			resolved, operandErr := state.temporalOperand(operand, itemType)

			if operandErr != nil {
				return "", nil, operandErr
			}

			param = resolved
		}
	}

	return fmt.Sprintf("EXISTS (SELECT 1 FROM %s WHERE %s)", jsonItems(columnPrefix, predicate.Property), comparison), param, nil
}

// temporalOperand binds a date or datetime operand: an unquoted relative date
// resolves against the compile clock in the stored format of valueType, and
// any other value binds as written.
func (state *compileState) temporalOperand(operand StringValue, valueType string) (any, error) {
	relative, isRelative := parseRelativeDate(operand.V)

	if !operand.Bareword || !isRelative || !isTextComparedType(valueType) {
		return operand.V, nil
	}

	if state.now.IsZero() {
		return nil, fmt.Errorf("compile: relative date %q needs CompileOptions.Now", operand.V)
	}

	return formatTemporal(relative.resolve(state.now), valueType), nil
}

// comparedType is the declared type of the values a predicate compares: a
// list's item type, or the property's own type. Empty when undeclared.
func comparedType(predicate *PropertyPredicate) string {
	if predicate.ResolvedType == "list-of" {
		return predicate.ItemType
	}

	return predicate.ResolvedType
}

// jsonItems is the json_each source over a property's value, aliased item: an
// array's items, or a scalar as its single row. quotedExtract's precondition
// on the property name applies.
func jsonItems(columnPrefix, property string) string {
	return fmt.Sprintf(`json_each(%sproperties_json, '$."%s"') AS item`, columnPrefix, property)
}

// compilePattern matches column against a path glob. The glob is translated to
// an anchored regex (see internal/pathglob) and the engine evaluates it through
// the REGEXP operator, which internal/index registers for SQLite. This is the
// only place the compiler spells "matches a regex", so porting to an engine
// with a native regex operator (Postgres `~`, MySQL REGEXP_LIKE) changes just
// this function.
func compilePattern(column string, op Op, pattern string) (string, []any, error) {
	params := []any{pathglob.ToRegexp(pattern)}

	switch op {
	case OpEQ:
		return column + " REGEXP ?", params, nil
	case OpNE:
		return "NOT (" + column + " REGEXP ?)", params, nil
	}

	return "", nil, fmt.Errorf("compile: a pattern only works with = or !=, got %s", op)
}

// isTextComparedType reports whether a resolved property type compares
// lexically. date (YYYY-MM-DD) and datetime (RFC3339) are stored as ISO
// strings that sort chronologically, so ordering/range operators compare them
// as TEXT rather than coercing to integer.
func isTextComparedType(resolvedType string) bool {
	return resolvedType == "date" || resolvedType == "datetime"
}

// quotedExtract builds a json_extract over a quoted JSON path key. Quoting the
// key keeps property names with hyphens, dots, or other path metacharacters
// unambiguous, so every property extract in this compiler routes through it.
//
// Precondition: property must not contain a `"`. The key is embedded between
// double quotes without escaping, so an embedded quote would malform the path.
// Every caller's property name originates from the filter lexer
// (isIdentStart/isIdentContinue) or SortKey validation (isIdentifier), both of
// which constrain names to [A-Za-z_][A-Za-z0-9_-]*, so a quote can never reach
// here — no escaping machinery is needed for inputs the charset can't produce.
func quotedExtract(columnPrefix, property string) string {
	return fmt.Sprintf(`json_extract(%sproperties_json, '$."%s"')`, columnPrefix, property)
}

// compileEnumOrdering expands an ordering operator (`<`, `<=`, `>`, `>=`) on an
// enum property into an IN-set over the member names that satisfy it, comparing
// the stored name strings directly. raw may be a declared value name or a
// 0-based index into EnumValues.
func compileEnumOrdering(predicate *PropertyPredicate, columnPrefix, raw string) (string, []any, error) {
	idx, ok := resolveEnumIndex(predicate.EnumValues, raw)

	if !ok {
		return "", nil, fmt.Errorf("compile: %q is not a value or index of enum property %q", raw, predicate.Property)
	}

	sql, params := compileInSet(columnPrefix, predicate.Property, enumSatisfyingNames(predicate.EnumValues, predicate.Op, idx))

	return sql, params, nil
}

// compileEnumRange expands an inclusive `lo..hi` range on an enum property into
// an IN-set over the member names between the two bounds (by declared order).
// A reversed range (lo after hi) yields the empty set, matching BETWEEN.
func compileEnumRange(predicate *PropertyPredicate, columnPrefix string, rangeValue RangeValue) (string, []any, error) {
	loIdx, okLo := resolveEnumIndex(predicate.EnumValues, rangeValue.Min)
	hiIdx, okHi := resolveEnumIndex(predicate.EnumValues, rangeValue.Max)

	if !okLo {
		return "", nil, fmt.Errorf("compile: %q is not a value or index of enum property %q", rangeValue.Min, predicate.Property)
	}

	if !okHi {
		return "", nil, fmt.Errorf("compile: %q is not a value or index of enum property %q", rangeValue.Max, predicate.Property)
	}

	var names []string

	for index := loIdx; index <= hiIdx; index++ {
		names = append(names, predicate.EnumValues[index])
	}

	sql, params := compileInSet(columnPrefix, predicate.Property, names)

	return sql, params, nil
}

// compileInSet renders `json_extract(...) IN (?, ...)` over the given member
// names, or the constant-false predicate `0 = 1` when the set is empty (a
// legitimately empty satisfying set matches nothing, which is correct — not a
// silent failure).
func compileInSet(columnPrefix, property string, names []string) (string, []any) {
	if len(names) == 0 {
		return "0 = 1", nil
	}

	placeholders := strings.Repeat("?, ", len(names)-1) + "?"
	params := make([]any, len(names))

	for index, name := range names {
		params[index] = name
	}

	return quotedExtract(columnPrefix, property) + " IN (" + placeholders + ")", params
}

// resolveEnumIndex maps a raw comparison token to its 0-based position in
// values. The token may be a declared value name or a numeric index; ok is
// false when it is neither (an out-of-range index or an undeclared name). Both
// Validate (validity check) and Compile (IN-set expansion) route through this
// helper so they never disagree.
func resolveEnumIndex(values []string, raw string) (int, bool) {
	for index, value := range values {
		if value == raw {
			return index, true
		}
	}

	if numeric, convErr := strconv.Atoi(raw); convErr == nil && numeric >= 0 && numeric < len(values) {
		return numeric, true
	}

	return 0, false
}

// enumSatisfyingNames returns the member names whose index satisfies `index op
// threshold`, in ascending declared order.
func enumSatisfyingNames(values []string, op Op, threshold int) []string {
	var names []string

	for index, value := range values {
		keep := false

		switch op {
		case OpLT:
			keep = index < threshold
		case OpLE:
			keep = index <= threshold
		case OpGT:
			keep = index > threshold
		case OpGE:
			keep = index >= threshold
		}

		if keep {
			names = append(names, value)
		}
	}

	return names
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
// binary comparisons (OpRange, which compiles to BETWEEN, OpContains, which
// compiles to an EXISTS over the items, and any unknown value) rather than
// emitting a "?" sentinel into a query.
func opToSQL(op Op) (string, error) {
	if op == OpRange || op == OpContains || !op.valid() {
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
			if key.ResolvedType == "enum" && len(key.EnumValues) > 0 {
				expression = enumOrderExpression(key.Property, key.EnumValues)
			} else {
				expression = quotedExtract("", key.Property)
			}
		}

		direction := "ASC"

		if key.Descending {
			direction = "DESC"
		}

		parts = append(parts, expression+" "+direction)
	}

	return strings.Join(parts, ", ")
}

// enumOrderExpression maps an enum property's stored NAME to its 0-based declared
// position, so ORDER BY sorts by declared order (low < medium < high) instead of
// lexically by name. A stored value absent from the enum sorts last. Enum values
// come from the trusted manifest but are not charset-constrained, so any single
// quote is doubled to keep the inlined CASE literal well-formed (mirroring how
// quotedExtract inlines the equally-trusted property name).
func enumOrderExpression(property string, values []string) string {
	var builder strings.Builder

	builder.WriteString("CASE ")
	builder.WriteString(quotedExtract("", property))

	for index, value := range values {
		fmt.Fprintf(&builder, " WHEN '%s' THEN %d", strings.ReplaceAll(value, "'", "''"), index)
	}

	fmt.Fprintf(&builder, " ELSE %d END", len(values))

	return builder.String()
}

// compileEdgePredicate emits an EXISTS over the edges leaving (or entering) the
// row at depth, joined to their far-end node as n<depth>. The inner term
// compiles one level down, where rowRef resolves to that alias. Sibling hops at
// the same depth reuse the alias names, which is safe because each sits in its
// own EXISTS subquery.
func (state *compileState) compileEdgePredicate(predicate *EdgePredicate, depth int) (string, []any, error) {
	if predicate.repeated() {
		return state.compileRepeatedEdge(predicate, depth)
	}

	edgeAlias := fmt.Sprintf("e%d", depth)
	nodeAlias := fmt.Sprintf("n%d", depth)
	parentRef := rowRef(depth)

	// The edges are read through the index of the row they hang off. With no
	// sqlite_stat1 the planner would probe by the edge type instead, through
	// the (source, type) prefix of the edges UNIQUE index for a namespaced
	// type: a few values, so every outer row walked most of the table (#838).
	var sourceColumn, joinColumn, anchorIndex string

	if predicate.Direction == DirectionOutgoing {
		sourceColumn = fmt.Sprintf("%s.source_id = %s.id", edgeAlias, parentRef)
		joinColumn = fmt.Sprintf("%s.id = %s.target_id", nodeAlias, edgeAlias)
		anchorIndex = "edges_source_target_idx"
	} else {
		sourceColumn = fmt.Sprintf("%s.target_id = %s.id", edgeAlias, parentRef)
		joinColumn = fmt.Sprintf("%s.id = %s.source_id", nodeAlias, edgeAlias)
		anchorIndex = "edges_target_idx"
	}

	edgeTable := fmt.Sprintf("edges %s INDEXED BY %s", edgeAlias, anchorIndex)

	edgeClause, edgeParams, edgeErr := compileTypeRef(predicate.EdgeType, edgeAlias+".", OpEQ)

	if edgeErr != nil {
		return "", nil, edgeErr
	}

	if predicate.Inner == nil {
		sql := fmt.Sprintf("EXISTS (SELECT 1 FROM %s WHERE %s AND %s)", edgeTable, sourceColumn, edgeClause)

		return sql, edgeParams, nil
	}

	innerSQL, innerParams, innerErr := state.compileWhere(predicate.Inner, depth+1)

	if innerErr != nil {
		return "", nil, innerErr
	}

	// The inner term is parenthesized because it can be a disjunction, and
	// `… AND type = ? AND (a) OR (b)` would let the OR escape the edge join.
	sql := fmt.Sprintf("EXISTS (SELECT 1 FROM %s JOIN nodes %s ON %s WHERE %s AND %s AND (%s))",
		edgeTable, nodeAlias, joinColumn, sourceColumn, edgeClause, innerSQL)

	params := append(edgeParams, innerParams...)

	return sql, params, nil
}

// compileRepeatedEdge compiles `->+` and `->{m,n}` (and their `<-` forms) as a
// recursive CTE walked back from the far end: it starts at the nodes one edge
// away from a node the inner term matches, or from any edge of the type when
// there is no inner term, and steps one edge further toward the row each round.
// A row matches when the walk reaches it within the hop count. The CTE is
// uncorrelated, so it runs once for the whole query, and the inner term
// compiles inside it at depth+1 like a single hop's does. Its params, the inner
// term's included, travel with the CTE (see compileState.cteParams); any CTE
// the inner term needs is emitted first.
func (state *compileState) compileRepeatedEdge(predicate *EdgePredicate, depth int) (string, []any, error) {
	edgeAlias := fmt.Sprintf("e%d", depth)
	nodeAlias := fmt.Sprintf("n%d", depth)

	// near is the edge's end toward the row, far the end toward the inner
	// term. The step finds the next edge by its far end, through that column's
	// index (#838).
	near, far, farIndex := "source_id", "target_id", "edges_target_idx"

	if predicate.Direction == DirectionIncoming {
		near, far, farIndex = "target_id", "source_id", "edges_source_target_idx"
	}

	seedTypeClause, seedTypeParams, seedTypeErr := compileTypeRef(predicate.EdgeType, edgeAlias+".", OpEQ)

	if seedTypeErr != nil {
		return "", nil, seedTypeErr
	}

	params := append([]any{}, seedTypeParams...)
	seed := fmt.Sprintf("SELECT %s.%s, 1 FROM edges %s WHERE %s", edgeAlias, near, edgeAlias, seedTypeClause)

	if predicate.Inner != nil {
		innerSQL, innerParams, innerErr := state.compileWhere(predicate.Inner, depth+1)

		if innerErr != nil {
			return "", nil, innerErr
		}

		seed = fmt.Sprintf("SELECT %s.%s, 1 FROM edges %s JOIN nodes %s ON %s.id = %s.%s WHERE %s AND (%s)",
			edgeAlias, near, edgeAlias, nodeAlias, nodeAlias, edgeAlias, far, seedTypeClause, innerSQL)
		params = append(params, innerParams...)
	}

	stepTypeClause, stepTypeParams, stepTypeErr := compileTypeRef(predicate.EdgeType, edgeAlias+".", OpEQ)

	if stepTypeErr != nil {
		return "", nil, stepTypeErr
	}

	params = append(params, stepTypeParams...)

	state.cteCounter++
	name := fmt.Sprintf("hops_%d", state.cteCounter)
	minHops := max(predicate.MinHops, 1)
	stepDepth, stop := walkStep(name, minHops, predicate.MaxHops)

	state.ctes = append(state.ctes, fmt.Sprintf(`%s(node_id, depth) AS (
    %s
    UNION
    SELECT %s.%s, %s FROM %s
        JOIN edges %s INDEXED BY %s ON %s.%s = %s.node_id
        WHERE %s%s
)`, name, seed, edgeAlias, near, stepDepth, name, edgeAlias, farIndex, edgeAlias, far, name, stepTypeClause, stop))
	state.cteParams = append(state.cteParams, params...)

	reached := "SELECT node_id FROM " + name

	if minHops > 1 {
		reached += fmt.Sprintf(" WHERE depth >= %d", minHops)
	}

	return fmt.Sprintf("%s.id IN (%s)", rowRef(depth), reached), nil, nil
}

// compileTraversalShortcut returns a WHERE-clause fragment, a slice of CTE
// definition strings, the CTE bodies' bind params, and the WHERE fragment's own
// bind params. The caller stitches the CTE definitions into a single WITH
// RECURSIVE clause prepended to the SELECT and binds cteParams ahead of
// whereParams to match that string order.
//
// The two param slices are kept distinct because the CTE placeholders sit at the
// front of the assembled statement while the WHERE-fragment placeholders sit
// inline: the recursive shortcuts (ShortcutTree, ShortcutRoot) put all their
// params in the CTE bodies (whereParams nil), while the inline ShortcutParentOf
// has no CTE (cteParams nil). Interleaving them in AST-traversal order is what
// misbound the recursive shortcuts when they were not the leftmost leaf.
//
// shortcut.ParentIsSource picks which end of the edge is the parent (see
// hierarchyColumns), and shortcut.MaxDepth caps tree= and root= walks; both
// come from the edge type through Validate.
//
// rowRef names the node row the shortcut constrains: "nodes" at the top level,
// or an edge predicate's target alias when the shortcut follows an arrow.
//
// The edge type is supplied by shortcut.EdgeType, populated by the
// validator from the manifest. If empty, the validator did not run or the
// AST was hand-constructed without resolution — return an error rather
// than emit ambiguous SQL.
func compileTraversalShortcut(shortcut *TraversalShortcut, rowRef string, counter int) (whereClause string, ctes []string, cteParams []any, whereParams []any, err error) {
	if shortcut.EdgeType == "" {
		return "", nil, nil, nil, fmt.Errorf("compile: traversal shortcut has unresolved edge type (validator must run before compile)")
	}

	edge := shortcut.EdgeType
	child, parent := hierarchyColumns(shortcut.ParentIsSource)

	switch shortcut.Kind {
	case ShortcutParentOf:
		whereClause := fmt.Sprintf("EXISTS (SELECT 1 FROM edges WHERE %s = %s.id AND type = ? AND %s = ?)", child, rowRef, parent)

		return whereClause, nil, nil, []any{edge, shortcut.NodeID}, nil
	case ShortcutTree:
		cteName := fmt.Sprintf("descendants_%d", counter)
		cteBody := hierarchyWalkCTE(cteName, fmt.Sprintf("SELECT %s, 1 FROM edges WHERE %s = ? AND type = ?", child, parent), parent, child, shortcut.MaxDepth)

		whereClause := fmt.Sprintf("%s.id IN (SELECT node_id FROM %s)", rowRef, cteName)

		return whereClause, []string{cteBody}, []any{shortcut.NodeID, edge, edge}, nil, nil
	case ShortcutRoot:
		ancestorsName := fmt.Sprintf("ancestors_%d", counter)
		fromRootName := fmt.Sprintf("from_root_%d", counter)
		ancestorsBody := hierarchyWalkCTE(ancestorsName, fmt.Sprintf("SELECT %s, 1 FROM edges WHERE %s = ? AND type = ?", parent, child), child, parent, shortcut.MaxDepth)

		// The roots are the ancestors with no parent of their own, plus the
		// topmost ones max-depth let the walk reach. The tree is walked down
		// from them and from the seed itself, so a seed that is its own root,
		// or sits on a loop with no root, still returns its own subtree.
		top := fmt.Sprintf("NOT EXISTS (SELECT 1 FROM edges up WHERE up.%s = %s.node_id AND up.type = ?)", child, ancestorsName)

		if shortcut.MaxDepth > 0 {
			top = fmt.Sprintf("(%s OR %s.depth >= %d)", top, ancestorsName, shortcut.MaxDepth)
		}

		seed := fmt.Sprintf("SELECT id, 0 FROM nodes WHERE id = ? OR id IN (SELECT node_id FROM %s WHERE %s)", ancestorsName, top)
		fromRootBody := hierarchyWalkCTE(fromRootName, seed, parent, child, shortcut.MaxDepth)

		whereClause := fmt.Sprintf("%s.id IN (SELECT node_id FROM %s)", rowRef, fromRootName)

		return whereClause, []string{ancestorsBody, fromRootBody}, []any{shortcut.NodeID, edge, edge, shortcut.NodeID, edge, edge}, nil, nil
	}

	return "", nil, nil, nil, fmt.Errorf("compile: unknown traversal shortcut kind %v", shortcut.Kind)
}

// hierarchyWalkCTE returns a recursive CTE named name over (node_id, depth):
// the rows of seed, then each node an edge of the shortcut's type leads to
// from a node already found, joining on the edge's from column and taking its
// to column. Its recursive step binds one param, the edge type. maxDepth stops
// the walk that many levels from the seed's first row; zero walks to the end.
func hierarchyWalkCTE(name, seed, from, to string, maxDepth int) string {
	depth, stop := walkStep(name, 1, maxDepth)

	return fmt.Sprintf(`%s(node_id, depth) AS (
    %s
    UNION
    SELECT edges.%s, %s FROM %s
        JOIN edges ON edges.%s = %s.node_id
        WHERE edges.type = ?%s
)`, name, seed, to, depth, name, from, name, stop)
}
