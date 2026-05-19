package filter

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

// Value is a value AST.
type Value interface {
	valueNode()
}

// StringValue holds a single string value.
type StringValue struct {
	V string
}

func (stringValue StringValue) valueNode() {}

// RangeValue holds a min..max range value.
type RangeValue struct {
	Min string
	Max string
}

func (rangeValue RangeValue) valueNode() {}
