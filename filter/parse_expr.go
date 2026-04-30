package filter

import (
	"fmt"
	"strings"

	"github.com/germanamz/tusk/domain"
)

// ParseExpr parses a filter string into a boolean expression tree.
// It supports AND, OR, NOT operators and parenthesized grouping.
// Adjacent terms without an explicit operator are implicitly AND'd.
// Returns nil Expr for empty input. Errors are collected, not fail-fast.
func ParseExpr(input string) (Expr, []ParseError) {
	tokens, lexErrs := Lex(input)

	var errs []ParseError
	errs = append(errs, lexErrs...)

	if len(tokens) == 0 {
		return nil, errs
	}

	ep := &exprParser{
		tokens: tokens,
		pos:    0,
		errs:   errs,
	}

	expr := ep.parseOr()

	// Check for leftover tokens (e.g., unmatched ")")
	if ep.pos < len(ep.tokens) {
		tok := ep.tokens[ep.pos]
		if tok.Type == TokenRParen {
			ep.errs = append(ep.errs, ParseError{
				Pos:     tok.Pos,
				Message: "unexpected ')'",
			})
		}
	}

	return expr, ep.errs
}

type exprParser struct {
	tokens []Token
	pos    int
	errs   []ParseError
}

func (parser *exprParser) peek() (Token, bool) {
	if parser.pos >= len(parser.tokens) {
		return Token{}, false
	}
	return parser.tokens[parser.pos], true
}

func (parser *exprParser) advance() Token {
	tok := parser.tokens[parser.pos]
	parser.pos++
	return tok
}

// parseOr: or_expr = and_expr ("OR" and_expr)*
func (parser *exprParser) parseOr() Expr {
	var children []Expr

	// Parse the first AND group, skipping validation errors
	posBefore := parser.pos
	left := parser.parseAnd()
	if left != nil {
		children = append(children, left)
	} else if parser.pos == posBefore {
		return nil
	}
	// If left is nil but tokens were consumed, continue to look for OR

	for {
		tok, ok := parser.peek()
		if !ok || tok.Type != TokenOr {
			break
		}
		parser.advance() // consume OR
		posBefore := parser.pos
		right := parser.parseAnd()
		if right == nil {
			if parser.pos == posBefore {
				parser.errs = append(parser.errs, ParseError{
					Pos:     tok.Pos,
					Message: "expected expression after OR",
				})
				break
			}
			// Tokens consumed but validation failed — continue looking for more OR
			continue
		}
		children = append(children, right)
	}

	if len(children) == 0 {
		return nil
	}
	if len(children) == 1 {
		return children[0]
	}
	return OrExpr{Children: children}
}

// parseAnd: and_expr = unary (("AND")? unary)*
// Adjacent terms without explicit AND are implicit AND.
func (parser *exprParser) parseAnd() Expr {
	var children []Expr

	// Parse the first term, skipping validation errors
	for {
		posBefore := parser.pos
		left := parser.parseUnary()
		if left != nil {
			children = append(children, left)
			break
		}
		if parser.pos == posBefore {
			// No tokens consumed — nothing to parse
			return nil
		}
		// Tokens consumed but validation failed — try the next term
	}
	for {
		tok, ok := parser.peek()
		if !ok {
			break
		}

		// Explicit AND
		if tok.Type == TokenAnd {
			parser.advance() // consume AND
			posBefore := parser.pos
			right := parser.parseUnary()
			if right == nil {
				if parser.pos == posBefore {
					// No tokens consumed — nothing after AND
					parser.errs = append(parser.errs, ParseError{
						Pos:     tok.Pos,
						Message: "expected expression after AND",
					})
					break
				}
				// Tokens consumed but validation failed — error already recorded, continue
				continue
			}
			children = append(children, right)
			continue
		}

		// Implicit AND: if the next token is a term, NOT, or LParen, it's implicitly AND'd.
		// Stop on OR, RParen, or end of tokens.
		if tok.Type == TokenOr || tok.Type == TokenRParen {
			break
		}

		// Must be a term-starting token (Field, TagInclude, TagExclude, Text, Not, LParen)
		posBefore := parser.pos
		right := parser.parseUnary()
		if right == nil {
			if parser.pos == posBefore {
				// No tokens consumed — not a term-starting token
				break
			}
			// Tokens consumed but validation failed — error already recorded, continue
			continue
		}
		children = append(children, right)
	}

	if len(children) == 1 {
		return children[0]
	}
	return AndExpr{Children: children}
}

// parseUnary: unary = "NOT" unary | primary
func (parser *exprParser) parseUnary() Expr {
	tok, ok := parser.peek()
	if !ok {
		return nil
	}

	if tok.Type == TokenNot {
		parser.advance() // consume NOT
		child := parser.parseUnary()
		if child == nil {
			parser.errs = append(parser.errs, ParseError{
				Pos:     tok.Pos,
				Message: "expected expression after NOT",
			})
			return nil
		}
		return NotExpr{Child: child}
	}

	return parser.parsePrimary()
}

// parsePrimary: primary = "(" expr ")" | term
func (parser *exprParser) parsePrimary() Expr {
	tok, ok := parser.peek()
	if !ok {
		return nil
	}

	if tok.Type == TokenLParen {
		parser.advance() // consume (
		expr := parser.parseOr()

		// Expect closing paren
		closeTok, closeOk := parser.peek()
		if !closeOk || closeTok.Type != TokenRParen {
			parser.errs = append(parser.errs, ParseError{
				Pos:     tok.Pos,
				Message: "unclosed '('",
			})
			return expr // return what we have
		}
		parser.advance() // consume )
		return expr
	}

	return parser.parseTerm()
}

// parseTerm: term = field | tag_include | tag_exclude | text
func (parser *exprParser) parseTerm() Expr {
	tok, ok := parser.peek()
	if !ok {
		return nil
	}

	switch tok.Type {
	case TokenField:
		parser.advance()
		key, value, _ := strings.Cut(tok.Value, "=")

		// Validate field — same logic as Parse() in parser.go
		if udaKey, ok := strings.CutPrefix(key, "uda."); ok {
			if udaKey == "" {
				parser.errs = append(parser.errs, ParseError{
					Pos:     tok.Pos,
					Field:   key,
					Message: "empty UDA key name",
				})
				return nil
			}
			if err := domain.ValidateUDAKey(udaKey); err != nil {
				parser.errs = append(parser.errs, ParseError{
					Pos:     tok.Pos,
					Field:   key,
					Message: err.Error(),
				})
				return nil
			}
			return TermExpr{Field: &FieldFilter{
				Key:      key,
				Value:    value,
				Modifier: tok.Modifier,
				Pos:      tok.Pos,
			}}
		}

		validator, known := fieldValidators[key]
		if !known {
			msg := "unknown field"
			if !strings.Contains(key, ".") {
				msg = fmt.Sprintf("unknown field; did you mean uda.%s?", key)
			}
			parser.errs = append(parser.errs, ParseError{
				Pos:     tok.Pos,
				Field:   key,
				Message: msg,
			})
			return nil
		}
		if err := validator(value); err != nil {
			parser.errs = append(parser.errs, ParseError{
				Pos:     tok.Pos,
				Field:   key,
				Message: err.Error(),
			})
			return nil
		}
		return TermExpr{Field: &FieldFilter{
			Key:      key,
			Value:    value,
			Modifier: tok.Modifier,
			Pos:      tok.Pos,
		}}

	case TokenTagInclude:
		parser.advance()
		return TermExpr{Tag: &TagFilter{Name: tok.Value, Pos: tok.Pos}}

	case TokenTagExclude:
		parser.advance()
		return TermExpr{Tag: &TagFilter{Name: tok.Value, Exclude: true, Pos: tok.Pos}}

	case TokenText:
		parser.advance()
		return TermExpr{Text: tok.Value}

	default:
		// Unexpected token (e.g., AND/OR at start without a preceding term).
		// Don't consume — let the caller handle it.
		return nil
	}
}
