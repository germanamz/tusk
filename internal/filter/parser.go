package filter

import "fmt"

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

	return parser.parsePropertyPredicate()
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
