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

	if first.Kind != TokenIdent {
		parser.appendErr(first.Pos, "expected identifier")
		parser.advance()

		return nil
	}

	switch first.Value {
	case "tree", "parent", "root":
		next := parser.peekN(1)

		if next.Kind == TokenEQ {
			return parser.parseTraversalShortcut()
		}
	}

	next := parser.peekN(1)

	if next.Kind == TokenArrowOut || next.Kind == TokenArrowIn {
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

	identToken := parser.advance()

	if identToken.Kind != TokenIdent {
		parser.appendErr(identToken.Pos, "expected edge type identifier")

		return nil
	}

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
		EdgeType:  identToken.Value,
		Direction: direction,
		Pos:       identToken.Pos,
	}

	next := parser.peek()

	switch next.Kind {
	case TokenEOF, TokenAnd, TokenOr, TokenNot, TokenRParen:
		return pred
	}

	if next.Kind == TokenIdent {
		afterIdent := parser.peekN(1)

		if afterIdent.Kind == TokenArrowOut || afterIdent.Kind == TokenArrowIn {
			pred.Inner = parser.parseEdgePredicate(depth + 1)

			return pred
		}

		pred.Inner = parser.parsePropertyPredicate()

		return pred
	}

	parser.appendErr(next.Pos, "expected inner predicate or end of edge predicate")

	return pred
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
		Value:    StringValue{V: leftValueToken.Value},
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

	eqToken := parser.advance()

	if eqToken.Kind != TokenEQ {
		parser.appendErr(eqToken.Pos, "expected = after traversal-shortcut keyword")

		return nil
	}

	valueToken := parser.lexer.NextValue()

	if valueToken.Kind == TokenEOF {
		parser.appendErr(valueToken.Pos, "expected value after =")

		return nil
	}

	return &TraversalShortcut{Kind: kind, NodeID: valueToken.Value, Pos: identToken.Pos}
}

func opTokenToOp(kind TokenKind) (Op, bool) {
	switch kind {
	case TokenEQ:
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
