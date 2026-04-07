package filter

import (
	"strings"

	"github.com/germanamz/tusk/internal/domain"
)

// fieldValidators maps field names to their validation functions.
// Do not modify after init.
var fieldValidators = map[string]func(string) error{
	"status":      validateStatus,
	"project":     validateProject,
	"priority":    validatePriority,
	"due":         validateDue,
	"parent":      validateShortID,
	"tree":        validateShortID,
	"waiting":     validateBool,
	"title":       validateNonEmpty,
	"description": validateNonEmpty,
	"claimed_by":  validateNonEmpty,
	"unclaimed":   validateBool,
}

// Parse takes a raw filter string and returns the AST plus any parse errors.
// It always returns a FilterSet (possibly empty) even when errors are present,
// so callers can use partial results if desired.
func Parse(input string) (*FilterSet, []ParseError) {
	tokens, lexErrs := Lex(input)

	fs := &FilterSet{}
	var errs []ParseError
	errs = append(errs, lexErrs...)

	for _, tok := range tokens {
		switch tok.Type {
		case TokenTagInclude:
			fs.Tags = append(fs.Tags, TagFilter{
				Name: tok.Value[1:], // strip leading '+'
				Pos:  tok.Pos,
			})

		case TokenTagExclude:
			fs.Tags = append(fs.Tags, TagFilter{
				Name:    tok.Value[1:], // strip leading '-'
				Exclude: true,
				Pos:     tok.Pos,
			})

		case TokenText:
			fs.Text = append(fs.Text, tok.Value)

		case TokenAnd, TokenOr, TokenNot, TokenLParen, TokenRParen:
			// In Parse (input building for tusk add/modify), boolean keywords
			// and parens are plain text, not operators. Preserve them as title words.
			fs.Text = append(fs.Text, tok.Value)

		case TokenField:
			key, value, _ := strings.Cut(tok.Value, ":")
			// Check for uda.* prefix before static field lookup
			if udaKey, ok := strings.CutPrefix(key, "uda."); ok {
				if udaKey == "" {
					errs = append(errs, ParseError{
						Pos:     tok.Pos,
						Field:   key,
						Message: "empty UDA key name",
					})
					continue
				}
				if err := domain.ValidateUDAKey(udaKey); err != nil {
					errs = append(errs, ParseError{
						Pos:     tok.Pos,
						Field:   key,
						Message: err.Error(),
					})
					continue
				}
				fs.Fields = append(fs.Fields, FieldFilter{
					Key:   key,
					Value: value,
					Pos:   tok.Pos,
				})
				continue
			}
			validator, known := fieldValidators[key]
			if !known {
				errs = append(errs, ParseError{
					Pos:     tok.Pos,
					Field:   key,
					Message: "unknown field",
				})
				continue
			}
			if err := validator(value); err != nil {
				errs = append(errs, ParseError{
					Pos:     tok.Pos,
					Field:   key,
					Message: err.Error(),
				})
				continue
			}
			fs.Fields = append(fs.Fields, FieldFilter{
				Key:   key,
				Value: value,
				Pos:   tok.Pos,
			})
		}
	}

	return fs, errs
}
