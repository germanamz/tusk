package filter

import (
	"fmt"

	"github.com/germanamz/tusk/internal/pathglob"
)

// MaxTraversalDepth is the maximum number of hops allowed in a multi-hop chain.
const MaxTraversalDepth = 5

// Parser produces an Expr AST from an input string.
type Parser struct {
	lexer  *Lexer
	buffer []Token
	errs   []ParseError

	// edgeDepth counts the edge predicates enclosing the term being parsed, so
	// MaxTraversalDepth holds whether the hops chain directly (`a-> b-> x=1`)
	// or sit inside a group (`a-> (b-> x=1 OR c-> y=2)`).
	edgeDepth int
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
		parser.appendTokenErr(parser.peek(), "unexpected trailing content")
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
			parser.appendTokenErr(parser.peek(), "expected )")
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
			return parser.parseEdgePredicate()
		}

		parser.appendTokenErr(first, "expected identifier")
		parser.advance()

		return nil
	}

	if first.Kind != TokenIdent {
		parser.appendTokenErr(first, "expected identifier")
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
	case NamesPathKeyword:
		// Any comparison routes here so `names-path>x` is an error, not a
		// silent property comparison that matches nothing.
		if _, isOperator := opTokenToOp(parser.peekN(1).Kind); isOperator {
			return parser.parseNamesPathPredicate()
		}
	}

	if parser.peekEdgeTypeRefArity() > 0 {
		return parser.parseEdgePredicate()
	}

	return parser.parsePropertyPredicate()
}

func (parser *Parser) parseEdgePredicate() Expr {
	// A hop past the limit is reported but still parsed. Returning without
	// consuming it would hand the same tokens back to an enclosing group's
	// parseAnd, which reads them as an implicit AND and lands here again.
	if parser.edgeDepth >= MaxTraversalDepth {
		parser.appendErr(parser.peek().Pos, fmt.Sprintf("multi-hop chain exceeds max depth %d", MaxTraversalDepth))
	}

	arity := parser.peekEdgeTypeRefArity()

	if arity == 0 {
		token := parser.peek()
		parser.appendTokenErr(token, "expected edge type identifier")

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
		parser.appendTokenErr(arrowToken, "expected -> or <- after edge type")

		return nil
	}

	pred := &EdgePredicate{
		EdgeType:  canonical,
		Direction: direction,
		Pos:       edgePos,
	}

	next := parser.peek()

	switch next.Kind {
	case TokenEOF, TokenAnd, TokenOr, TokenRParen:
		return pred
	case TokenIdent, TokenColon, TokenLParen, TokenNot:
		// The arrow takes the single next term, the way AND does: any
		// predicate, `NOT <term>`, or a parenthesized group. So
		// `a-> x=1 y=2` stays `(a-> x=1) AND y=2`, and constraining the
		// target by both takes `a-> (x=1 y=2)`.
		parser.edgeDepth++
		pred.Inner = parser.parseNot()
		parser.edgeDepth--

		return pred
	}

	parser.appendTokenErr(next, "expected inner predicate or end of edge predicate")

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
		parser.appendTokenErr(identToken, "expected property name")

		return nil
	}

	opToken := parser.advance()
	op, opOK := opTokenToOp(opToken.Kind)

	if !opOK {
		parser.appendTokenErr(opToken, "expected comparison operator (= != < <= > >=)")

		return nil
	}

	leftValueToken := parser.lexer.NextValue()

	if !hasValue(leftValueToken) {
		parser.appendTokenErr(leftValueToken, "expected value after operator")

		return nil
	}

	if op == OpEQ && parser.peek().Kind == TokenDotDot {
		parser.advance()
		rightValueToken := parser.lexer.NextValue()

		if !hasValue(rightValueToken) {
			parser.appendTokenErr(rightValueToken, "expected value after ..")

			return nil
		}

		for _, boundToken := range []Token{leftValueToken, rightValueToken} {
			if isPatternToken(boundToken) {
				parser.appendErr(boundToken.Pos, "a range bound can't be a pattern")

				return nil
			}
		}

		return &PropertyPredicate{
			Property: identToken.Value,
			Op:       OpRange,
			Value:    RangeValue{Min: leftValueToken.Value, Max: rightValueToken.Value},
			Pos:      identToken.Pos,
		}
	}

	if isPatternToken(leftValueToken) {
		if _, matchable := patternProperties[identToken.Value]; !matchable {
			parser.appendErr(leftValueToken.Pos, "wildcards (* and ?) only match path and id; quote the value to compare it literally")

			return nil
		}

		if op != OpEQ && op != OpNE {
			parser.appendErr(leftValueToken.Pos, "a pattern only works with = or !=")

			return nil
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
		parser.appendTokenErr(separator, "expected = or : after traversal-shortcut keyword")

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

	if !hasValue(valueToken) {
		parser.appendTokenErr(valueToken, "expected value after =")

		return nil
	}

	if isPatternToken(valueToken) {
		parser.appendErr(valueToken.Pos, "a hierarchy shortcut takes a node id, not a pattern")

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
		parser.appendTokenErr(separator, "expected = or : after modified-since")

		return nil
	}

	valueToken := parser.lexer.NextValue()

	if !hasValue(valueToken) {
		parser.appendTokenErr(valueToken, "expected value after modified-since:")

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

// appendTokenErr records message at token's position. An illegal token's own
// description wins, since it names the actual problem (the character, or the
// unclosed string) instead of what the parser hoped to see there.
func (parser *Parser) appendTokenErr(token Token, message string) {
	if token.Kind == TokenIllegal {
		message = token.Value
	}

	parser.appendErr(token.Pos, message)
}

// hasValue reports whether a NextValue token carries a value, as opposed to
// EOF (nothing value-shaped at this position) or an illegal token.
func hasValue(token Token) bool {
	return token.Kind == TokenString || token.Kind == TokenBareValue
}

// patternProperties are the properties a glob pattern can match: the
// workspace-relative path and the id derived from it.
var patternProperties = map[string]struct{}{
	"id":   {},
	"path": {},
}

// isPatternToken reports whether a value token is a glob pattern. Only a bare
// value can be one; quoting a value makes its `*` and `?` literal.
func isPatternToken(token Token) bool {
	return token.Kind == TokenBareValue && pathglob.IsPattern(token.Value)
}
