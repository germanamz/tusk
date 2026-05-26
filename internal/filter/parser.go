package filter

import "fmt"

// MaxTraversalDepth is the maximum number of hops allowed in a multi-hop chain.
const MaxTraversalDepth = 5

// Parser produces an Expr AST from an input string.
type Parser struct {
	lexer  *Lexer
	buffer []Token
	errs   []ParseError
}

// NewParser constructs a Parser over input.
func NewParser(input string) *Parser {
	return &Parser{lexer: NewLexer(input)}
}

// Parse consumes the input and returns the AST plus any parse errors. A nil
// Expr with no errors is the match-all sentinel for empty input.
func (parser *Parser) Parse() (Expr, []ParseError) {
	if parser.peek().Kind == TokenEOF {
		return nil, parser.errs
	}

	expr := parser.parseExpr()

	if parser.peek().Kind != TokenEOF {
		parser.appendErr(parser.peek().Pos, "unexpected trailing content")
	}

	return expr, parser.errs
}

func (parser *Parser) parseExpr() Expr {
	return parser.parseOr()
}

func (parser *Parser) parseOr() Expr {
	left := parser.parseAnd()

	for parser.peek().Kind == TokenOr {
		opToken := parser.advance()
		right := parser.parseAnd()
		left = &OrExpr{Left: left, Right: right, Pos: opToken.Pos}
	}

	return left
}

func (parser *Parser) parseAnd() Expr {
	left := parser.parseNot()

	for {
		next := parser.peek()

		if next.Kind == TokenAnd {
			opToken := parser.advance()
			right := parser.parseNot()
			left = &AndExpr{Left: left, Right: right, Pos: opToken.Pos}

			continue
		}

		if next.Kind == TokenIdent || next.Kind == TokenLParen || next.Kind == TokenNot {
			right := parser.parseNot()
			left = &AndExpr{Left: left, Right: right, Pos: next.Pos}

			continue
		}

		break
	}

	return left
}

func (parser *Parser) parseNot() Expr {
	if parser.peek().Kind == TokenNot {
		notToken := parser.advance()

		return &NotExpr{Inner: parser.parseNot(), Pos: notToken.Pos}
	}

	return parser.parseAtom()
}

func (parser *Parser) parseAtom() Expr {
	if parser.peek().Kind == TokenLParen {
		parser.advance()
		inner := parser.parseExpr()

		if parser.peek().Kind != TokenRParen {
			parser.appendErr(parser.peek().Pos, "expected )")
		} else {
			parser.advance()
		}

		return inner
	}

	return parser.parsePredicate()
}

func (parser *Parser) parsePredicate() Expr {
	first := parser.peek()

	// `:type->...` opens with a colon. The keyword/property paths both
	// require an identifier first, so route the user-namespace edge form
	// straight into the edge parser before the identifier check below.
	if first.Kind == TokenColon {
		if parser.peekEdgeTypeRefArity() > 0 {
			return parser.parseEdgePredicate(0)
		}

		parser.appendErr(first.Pos, "expected identifier")
		parser.advance()

		return nil
	}

	if first.Kind != TokenIdent {
		parser.appendErr(first.Pos, "expected identifier")
		parser.advance()

		return nil
	}

	switch first.Value {
	case "tree", "parent", "root":
		next := parser.peekN(1)

		if next.Kind == TokenEQ || next.Kind == TokenColon {
			return parser.parseTraversalShortcut()
		}
	case "modified-since":
		next := parser.peekN(1)

		if next.Kind == TokenEQ || next.Kind == TokenColon {
			return parser.parseModifiedSincePredicate()
		}
	}

	if parser.peekEdgeTypeRefArity() > 0 {
		return parser.parseEdgePredicate(0)
	}

	return parser.parsePropertyPredicate()
}

func (parser *Parser) parseEdgePredicate(depth int) Expr {
	if depth >= MaxTraversalDepth {
		token := parser.peek()
		parser.appendErr(token.Pos, fmt.Sprintf("multi-hop chain exceeds max depth %d", MaxTraversalDepth))

		return nil
	}

	arity := parser.peekEdgeTypeRefArity()

	if arity == 0 {
		token := parser.peek()
		parser.appendErr(token.Pos, "expected edge type identifier")

		return nil
	}

	canonical, edgePos := parser.consumeEdgeTypeRef(arity)
	arrowToken := parser.advance()

	var direction Direction

	switch arrowToken.Kind {
	case TokenArrowOut:
		direction = DirectionOutgoing
	case TokenArrowIn:
		direction = DirectionIncoming
	default:
		parser.appendErr(arrowToken.Pos, "expected -> or <- after edge type")

		return nil
	}

	pred := &EdgePredicate{
		EdgeType:  canonical,
		Direction: direction,
		Pos:       edgePos,
	}

	next := parser.peek()

	switch next.Kind {
	case TokenEOF, TokenAnd, TokenOr, TokenNot, TokenRParen:
		return pred
	}

	if innerArity := parser.peekEdgeTypeRefArity(); innerArity > 0 {
		pred.Inner = parser.parseEdgePredicate(depth + 1)

		return pred
	}

	if next.Kind == TokenIdent {
		pred.Inner = parser.parsePropertyPredicate()

		return pred
	}

	parser.appendErr(next.Pos, "expected inner predicate or end of edge predicate")

	return pred
}

// peekEdgeTypeRefArity returns the number of leading tokens that compose
// a qualified edge-type identifier sitting just before an arrow operator:
// 1 for the bare `type->` form, 2 for `:type->`, 3 for `source:type->`.
// Returns 0 when no edge-type prefix is present in the lookahead buffer.
//
// Speculative tokens buffered during the lookahead are rewound on a
// negative result so callers can fall back to the value-position lexer
// (which is mode-distinct from Next()) without losing the input bytes.
func (parser *Parser) peekEdgeTypeRefArity() int {
	savedBuffer := len(parser.buffer)
	savedPos := parser.lexer.pos

	arity := parser.computeEdgeTypeRefArity()

	if arity == 0 && len(parser.buffer) > savedBuffer {
		parser.buffer = parser.buffer[:savedBuffer]
		parser.lexer.pos = savedPos
	}

	return arity
}

func (parser *Parser) computeEdgeTypeRefArity() int {
	first := parser.peek()

	if first.Kind == TokenIdent {
		second := parser.peekN(1)

		if second.Kind == TokenArrowOut || second.Kind == TokenArrowIn {
			return 1
		}

		if second.Kind == TokenColon && parser.peekN(2).Kind == TokenIdent {
			after := parser.peekN(3)

			if after.Kind == TokenArrowOut || after.Kind == TokenArrowIn {
				return 3
			}
		}

		return 0
	}

	if first.Kind == TokenColon && parser.peekN(1).Kind == TokenIdent {
		after := parser.peekN(2)

		if after.Kind == TokenArrowOut || after.Kind == TokenArrowIn {
			return 2
		}
	}

	return 0
}

// consumeEdgeTypeRef advances tokens for the edge-type prefix described
// by peekEdgeTypeRefArity and returns the canonical "[source:]type"
// string along with the position of the first consumed token. The caller
// is responsible for consuming the arrow that follows.
func (parser *Parser) consumeEdgeTypeRef(arity int) (string, int) {
	switch arity {
	case 1:
		ident := parser.advance()

		return ident.Value, ident.Pos
	case 2:
		colon := parser.advance()
		ident := parser.advance()

		return ":" + ident.Value, colon.Pos
	case 3:
		source := parser.advance()
		parser.advance()
		typeIdent := parser.advance()

		return source.Value + ":" + typeIdent.Value, source.Pos
	}

	return "", parser.peek().Pos
}

func (parser *Parser) parsePropertyPredicate() Expr {
	identToken := parser.advance()

	if identToken.Kind != TokenIdent {
		parser.appendErr(identToken.Pos, "expected property name")

		return nil
	}

	opToken := parser.advance()
	op, opOK := opTokenToOp(opToken.Kind)

	if !opOK {
		parser.appendErr(opToken.Pos, "expected comparison operator (= != < <= > >=)")

		return nil
	}

	leftValueToken := parser.lexer.NextValue()

	if leftValueToken.Kind == TokenEOF {
		parser.appendErr(leftValueToken.Pos, "expected value after operator")

		return nil
	}

	if op == OpEQ && parser.peek().Kind == TokenDotDot {
		parser.advance()
		rightValueToken := parser.lexer.NextValue()

		if rightValueToken.Kind == TokenEOF {
			parser.appendErr(rightValueToken.Pos, "expected value after ..")

			return nil
		}

		return &PropertyPredicate{
			Property: identToken.Value,
			Op:       OpRange,
			Value:    RangeValue{Min: leftValueToken.Value, Max: rightValueToken.Value},
			Pos:      identToken.Pos,
		}
	}

	return &PropertyPredicate{
		Property: identToken.Value,
		Op:       op,
		Value:    StringValue{V: leftValueToken.Value, Bareword: leftValueToken.Kind == TokenBareValue},
		Pos:      identToken.Pos,
	}
}

func (parser *Parser) parseTraversalShortcut() Expr {
	identToken := parser.advance()

	var kind ShortcutKind

	switch identToken.Value {
	case "tree":
		kind = ShortcutTree
	case "parent":
		kind = ShortcutParentOf
	case "root":
		kind = ShortcutRoot
	default:
		parser.appendErr(identToken.Pos, fmt.Sprintf("unknown traversal shortcut %q", identToken.Value))

		return nil
	}

	separator := parser.advance()

	if separator.Kind != TokenEQ && separator.Kind != TokenColon {
		parser.appendErr(separator.Pos, "expected = or : after traversal-shortcut keyword")

		return nil
	}

	var alias string

	// Qualified form is only possible after `:`. Probe without consuming
	// the input by snapshotting and restoring the lexer position; the
	// parser's buffer is empty here (parsePredicate populated it with
	// the keyword + separator, both already advance()d above).
	if separator.Kind == TokenColon {
		savedPos := parser.lexer.pos

		aliasCandidate := parser.lexer.Next()
		eqCandidate := parser.lexer.Next()

		if aliasCandidate.Kind == TokenIdent && eqCandidate.Kind == TokenEQ {
			alias = aliasCandidate.Value
		} else {
			parser.lexer.pos = savedPos
		}
	}

	valueToken := parser.lexer.NextValue()

	if valueToken.Kind == TokenEOF {
		parser.appendErr(valueToken.Pos, "expected value after =")

		return nil
	}

	return &TraversalShortcut{
		Kind:   kind,
		Alias:  alias,
		NodeID: valueToken.Value,
		Pos:    identToken.Pos,
	}
}

func (parser *Parser) parseModifiedSincePredicate() Expr {
	identToken := parser.advance()

	separator := parser.advance()

	if separator.Kind != TokenEQ && separator.Kind != TokenColon {
		parser.appendErr(separator.Pos, "expected = or : after modified-since")

		return nil
	}

	valueToken := parser.lexer.NextValue()

	if valueToken.Kind == TokenEOF {
		parser.appendErr(valueToken.Pos, "expected value after modified-since:")

		return nil
	}

	return &ModifiedSincePredicate{
		Raw: valueToken.Value,
		Pos: identToken.Pos,
	}
}

func opTokenToOp(kind TokenKind) (Op, bool) {
	switch kind {
	case TokenEQ, TokenColon:
		return OpEQ, true
	case TokenNE:
		return OpNE, true
	case TokenLT:
		return OpLT, true
	case TokenLE:
		return OpLE, true
	case TokenGT:
		return OpGT, true
	case TokenGE:
		return OpGE, true
	}

	return 0, false
}

func (parser *Parser) ensureBuffer(distance int) {
	for len(parser.buffer) <= distance {
		parser.buffer = append(parser.buffer, parser.lexer.Next())
	}
}

func (parser *Parser) peek() Token {
	parser.ensureBuffer(0)

	return parser.buffer[0]
}

func (parser *Parser) peekN(distance int) Token {
	parser.ensureBuffer(distance)

	return parser.buffer[distance]
}

func (parser *Parser) advance() Token {
	parser.ensureBuffer(0)

	token := parser.buffer[0]
	parser.buffer = parser.buffer[1:]

	return token
}

func (parser *Parser) appendErr(pos int, message string) {
	parser.errs = append(parser.errs, ParseError{Pos: pos, Message: message})
}
