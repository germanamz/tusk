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

	p := &exprParser{
		tokens: tokens,
		pos:    0,
		errs:   errs,
	}

	expr := p.parseOr()

	// Check for leftover tokens (e.g., unmatched ")")
	if p.pos < len(p.tokens) {
		tok := p.tokens[p.pos]
		if tok.Type == TokenRParen {
			p.errs = append(p.errs, ParseError{
				Pos:     tok.Pos,
				Message: "unexpected ')'",
			})
		}
	}

	return expr, p.errs
}

type exprParser struct {
	tokens []Token
	pos    int
	errs   []ParseError
}

func (p *exprParser) peek() (Token, bool) {
	if p.pos >= len(p.tokens) {
		return Token{}, false
	}
	return p.tokens[p.pos], true
}

func (p *exprParser) advance() Token {
	tok := p.tokens[p.pos]
	p.pos++
	return tok
}

// parseOr: or_expr = and_expr ("OR" and_expr)*
func (p *exprParser) parseOr() Expr {
	var children []Expr

	// Parse the first AND group, skipping validation errors
	posBefore := p.pos
	left := p.parseAnd()
	if left != nil {
		children = append(children, left)
	} else if p.pos == posBefore {
		return nil
	}
	// If left is nil but tokens were consumed, continue to look for OR

	for {
		tok, ok := p.peek()
		if !ok || tok.Type != TokenOr {
			break
		}
		p.advance() // consume OR
		posBefore := p.pos
		right := p.parseAnd()
		if right == nil {
			if p.pos == posBefore {
				p.errs = append(p.errs, ParseError{
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
func (p *exprParser) parseAnd() Expr {
	var children []Expr

	// Parse the first term, skipping validation errors
	for {
		posBefore := p.pos
		left := p.parseUnary()
		if left != nil {
			children = append(children, left)
			break
		}
		if p.pos == posBefore {
			// No tokens consumed — nothing to parse
			return nil
		}
		// Tokens consumed but validation failed — try the next term
	}
	for {
		tok, ok := p.peek()
		if !ok {
			break
		}

		// Explicit AND
		if tok.Type == TokenAnd {
			p.advance() // consume AND
			posBefore := p.pos
			right := p.parseUnary()
			if right == nil {
				if p.pos == posBefore {
					// No tokens consumed — nothing after AND
					p.errs = append(p.errs, ParseError{
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
		posBefore := p.pos
		right := p.parseUnary()
		if right == nil {
			if p.pos == posBefore {
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
func (p *exprParser) parseUnary() Expr {
	tok, ok := p.peek()
	if !ok {
		return nil
	}

	if tok.Type == TokenNot {
		p.advance() // consume NOT
		child := p.parseUnary()
		if child == nil {
			p.errs = append(p.errs, ParseError{
				Pos:     tok.Pos,
				Message: "expected expression after NOT",
			})
			return nil
		}
		return NotExpr{Child: child}
	}

	return p.parsePrimary()
}

// parsePrimary: primary = "(" expr ")" | term
func (p *exprParser) parsePrimary() Expr {
	tok, ok := p.peek()
	if !ok {
		return nil
	}

	if tok.Type == TokenLParen {
		p.advance() // consume (
		expr := p.parseOr()

		// Expect closing paren
		closeTok, closeOk := p.peek()
		if !closeOk || closeTok.Type != TokenRParen {
			p.errs = append(p.errs, ParseError{
				Pos:     tok.Pos,
				Message: "unclosed '('",
			})
			return expr // return what we have
		}
		p.advance() // consume )
		return expr
	}

	return p.parseTerm()
}

// parseTerm: term = field | tag_include | tag_exclude | text
func (p *exprParser) parseTerm() Expr {
	tok, ok := p.peek()
	if !ok {
		return nil
	}

	switch tok.Type {
	case TokenField:
		p.advance()
		key, value, _ := strings.Cut(tok.Value, "=")

		// Validate field — same logic as Parse() in parser.go
		if udaKey, ok := strings.CutPrefix(key, "uda."); ok {
			if udaKey == "" {
				p.errs = append(p.errs, ParseError{
					Pos:     tok.Pos,
					Field:   key,
					Message: "empty UDA key name",
				})
				return nil
			}
			if err := domain.ValidateUDAKey(udaKey); err != nil {
				p.errs = append(p.errs, ParseError{
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
			p.errs = append(p.errs, ParseError{
				Pos:     tok.Pos,
				Field:   key,
				Message: msg,
			})
			return nil
		}
		if err := validator(value); err != nil {
			p.errs = append(p.errs, ParseError{
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
		p.advance()
		return TermExpr{Tag: &TagFilter{Name: tok.Value, Pos: tok.Pos}}

	case TokenTagExclude:
		p.advance()
		return TermExpr{Tag: &TagFilter{Name: tok.Value, Exclude: true, Pos: tok.Pos}}

	case TokenText:
		p.advance()
		return TermExpr{Text: tok.Value}

	default:
		// Unexpected token (e.g., AND/OR at start without a preceding term).
		// Don't consume — let the caller handle it.
		return nil
	}
}
