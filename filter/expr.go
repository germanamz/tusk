package filter

// Expr is the interface for all filter expression nodes.
type Expr interface {
	exprNode() // marker method — prevents external implementations
}

// AndExpr groups child expressions with AND semantics.
// All children must match for the expression to match.
type AndExpr struct {
	Children []Expr
}

// OrExpr groups child expressions with OR semantics.
// At least one child must match for the expression to match.
type OrExpr struct {
	Children []Expr
}

// NotExpr negates a single child expression.
type NotExpr struct {
	Child Expr
}

// TermExpr wraps a single filter term — exactly one of Field, Tag, or Text
// is set.
type TermExpr struct {
	Field *FieldFilter // non-nil for field terms (e.g. status:active)
	Tag   *TagFilter   // non-nil for tag terms (e.g. +api, -docs)
	Text  string       // non-empty for free text terms
}

func (AndExpr) exprNode()  {}
func (OrExpr) exprNode()   {}
func (NotExpr) exprNode()  {}
func (TermExpr) exprNode() {}
