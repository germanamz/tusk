package syntax

import "testing"

func TestDefaultModifiersContainsPlusAndMinus(test *testing.T) {
	m := DefaultModifiers()
	if !m.Has('+') {
		test.Errorf("DefaultModifiers should contain '+'")
	}
	if !m.Has('-') {
		test.Errorf("DefaultModifiers should contain '-'")
	}
}

func TestDefaultModifiersRejectsUnknown(test *testing.T) {
	m := DefaultModifiers()
	for _, marker := range []byte{'?', '*', '!', 'a', '0', '='} {
		if m.Has(marker) {
			test.Errorf("DefaultModifiers should not contain %q", marker)
		}
	}
}

func TestModifierSetWithAddsRuneImmutably(test *testing.T) {
	base := DefaultModifiers()
	extended := base.With('?')
	if !extended.Has('?') {
		test.Errorf("With('?') should register '?'")
	}
	if base.Has('?') {
		test.Errorf("original set must not be mutated by With")
	}
}

func TestModifierSetZeroValueHasNothing(test *testing.T) {
	var m ModifierSet
	if m.Has('+') {
		test.Errorf("zero ModifierSet should not contain '+'")
	}
}

func TestLexWithModifiersRegistersCustomPrefix(test *testing.T) {
	set := DefaultModifiers().With('?')
	tokens, errs := LexWithModifiers("?priority=3", set)
	if len(errs) > 0 {
		test.Fatalf("unexpected errors: %v", errs)
	}
	if len(tokens) != 1 {
		test.Fatalf("expected 1 token, got %d", len(tokens))
	}
	tok := tokens[0]
	if tok.Type != TokenField {
		test.Errorf("type = %v, want TokenField", tok.Type)
	}
	if tok.Modifier != '?' {
		test.Errorf("modifier = %q, want '?'", tok.Modifier)
	}
	if tok.Value != "priority=3" {
		test.Errorf("value = %q, want %q", tok.Value, "priority=3")
	}
}

func TestLexDoesNotRecogniseUnregisteredPrefix(test *testing.T) {
	tokens, _ := Lex("?priority=3")
	if len(tokens) != 1 {
		test.Fatalf("expected 1 token, got %d", len(tokens))
	}
	if tokens[0].Modifier != 0 {
		test.Errorf("expected no modifier, got %q", tokens[0].Modifier)
	}
	if tokens[0].Type != TokenField {
		test.Errorf("type = %v, want TokenField", tokens[0].Type)
	}
	if tokens[0].Value != "?priority=3" {
		test.Errorf("value = %q, want %q", tokens[0].Value, "?priority=3")
	}
}
