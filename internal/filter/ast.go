package filter

import "time"

// Op is a comparison operator.
type Op int

const (
	OpEQ Op = iota
	OpNE
	OpLT
	OpLE
	OpGT
	OpGE
	OpRange
)

func (op Op) String() string {
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
	case OpRange:
		return ".."
	}

	return "?"
}

// Direction is the polarity of an edge predicate.
type Direction int

const (
	DirectionOutgoing Direction = iota
	DirectionIncoming
)

// ShortcutKind identifies a graph-traversal shortcut.
type ShortcutKind int

const (
	ShortcutTree ShortcutKind = iota
	ShortcutParentOf
	ShortcutRoot
)

// Expr is the root AST node interface.
type Expr interface {
	exprNode()
	Position() int
}

// OrExpr represents a logical OR of two expressions.
type OrExpr struct {
	Left  Expr
	Right Expr
	Pos   int
}

func (orExpr *OrExpr) exprNode()     {}
func (orExpr *OrExpr) Position() int { return orExpr.Pos }

// AndExpr represents a logical AND of two expressions.
type AndExpr struct {
	Left  Expr
	Right Expr
	Pos   int
}

func (andExpr *AndExpr) exprNode()     {}
func (andExpr *AndExpr) Position() int { return andExpr.Pos }

// NotExpr represents a logical NOT of an expression.
type NotExpr struct {
	Inner Expr
	Pos   int
}

func (notExpr *NotExpr) exprNode()     {}
func (notExpr *NotExpr) Position() int { return notExpr.Pos }

// PropertyPredicate represents a property comparison predicate.
type PropertyPredicate struct {
	Property string
	Op       Op
	Value    Value
	Pos      int
}

func (pred *PropertyPredicate) exprNode()     {}
func (pred *PropertyPredicate) Position() int { return pred.Pos }

// EdgePredicate represents a traversal through a named edge type.
type EdgePredicate struct {
	EdgeType  string
	Direction Direction
	Inner     Expr
	Pos       int
}

func (pred *EdgePredicate) exprNode()     {}
func (pred *EdgePredicate) Position() int { return pred.Pos }

// TraversalShortcut represents a graph-traversal shortcut keyword.
//
// Alias is the optional qualifier from a `keyword:alias=value` form. Empty
// when the shortcut is unqualified. The validator resolves Alias to a
// concrete edge type, stamping the result into EdgeType for the compiler.
type TraversalShortcut struct {
	Kind      ShortcutKind
	Alias     string
	NodeID    string
	EdgeType  string // resolved by Validate; the compiler refuses an empty value
	OrderedBy string // resolved by Validate from the edge type's OrderedBy; empty when the edge is not ordered
	Pos       int
}

func (shortcut *TraversalShortcut) exprNode()     {}
func (shortcut *TraversalShortcut) Position() int { return shortcut.Pos }

// ModifiedSincePredicate matches nodes whose last_mtime is at or after a
// threshold expressed as either a relative duration (e.g. "7d", "48h") or
// an absolute ISO date/datetime (e.g. "2026-05-23", "2026-05-23T12:00:00Z").
//
// The parser populates only Raw and Pos. The validator parses Raw and
// stamps one of Duration or Since (mirroring how TraversalShortcut.EdgeType
// is left empty by the parser and resolved by the validator). The compiler
// refuses to emit SQL when both Duration and Since are unset.
type ModifiedSincePredicate struct {
	Raw      string        // the value as parsed (e.g. "7d", "2026-05-01")
	Duration time.Duration // set by validator iff Raw parses as a duration
	Since    time.Time     // set by validator iff Raw parses as an absolute date/datetime
	Pos      int
}

func (pred *ModifiedSincePredicate) exprNode()     {}
func (pred *ModifiedSincePredicate) Position() int { return pred.Pos }

// Value is a value AST.
type Value interface {
	valueNode()
}

// StringValue holds a single string value.
//
// Bareword reports whether the value was written without quotes in the
// source filter (lexer TokenBareValue) vs quoted (lexer TokenString).
// The compiler uses this to distinguish `checkbox=false` (bool literal,
// coerced to integer 0 to match SQLite's json_extract of a JSON bool)
// from `checkbox="false"` (the string literal "false"). Existing code
// constructing StringValue programmatically (e.g. hand-built AST in
// tests) leaves Bareword=false, which preserves the legacy string-
// compare behaviour.
type StringValue struct {
	V        string
	Bareword bool
}

func (stringValue StringValue) valueNode() {}

// RangeValue holds a min..max range value.
type RangeValue struct {
	Min string
	Max string
}

func (rangeValue RangeValue) valueNode() {}
