package syntax

import "strings"

// ParseFields lexes input and folds the tokens into a FilterSet without any
// domain validation. It is the shared primitive used by filter.Parse (which
// adds task-field validation on top) and by consumer commands such as
// workflow and project config that maintain their own field vocabularies.
//
// Every FieldFilter carries Key, Value, and Modifier exactly as the lexer
// produced them. Boolean keywords (AND/OR/NOT) and parentheses are preserved
// as free text, mirroring filter.Parse behaviour for non-expression uses.
func ParseFields(input string) (FilterSet, []ParseError) {
	tokens, errs := Lex(input)
	var fs FilterSet

	for _, tok := range tokens {
		switch tok.Type {
		case TokenField:
			key, value, _ := strings.Cut(tok.Value, "=")
			fs.Fields = append(fs.Fields, FieldFilter{
				Key:      key,
				Value:    value,
				Modifier: tok.Modifier,
				Pos:      tok.Pos,
			})

		case TokenTagInclude:
			fs.Tags = append(fs.Tags, TagFilter{
				Name: tok.Value,
				Pos:  tok.Pos,
			})

		case TokenTagExclude:
			fs.Tags = append(fs.Tags, TagFilter{
				Name:    tok.Value,
				Exclude: true,
				Pos:     tok.Pos,
			})

		case TokenText, TokenAnd, TokenOr, TokenNot, TokenLParen, TokenRParen:
			fs.Text = append(fs.Text, tok.Value)
		}
	}

	return fs, errs
}
