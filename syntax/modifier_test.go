package syntax

import "testing"

func TestDefaultModifiersContainsPlusAndMinus(t *testing.T) {
	m := DefaultModifiers()
	if !m.Has('+') {
		t.Errorf("DefaultModifiers should contain '+'")
	}
	if !m.Has('-') {
		t.Errorf("DefaultModifiers should contain '-'")
	}
}

func TestDefaultModifiersRejectsUnknown(t *testing.T) {
	m := DefaultModifiers()
	for _, b := range []byte{'?', '*', '!', 'a', '0', '='} {
		if m.Has(b) {
			t.Errorf("DefaultModifiers should not contain %q", b)
		}
	}
}

func TestModifierSetWithAddsRuneImmutably(t *testing.T) {
	base := DefaultModifiers()
	extended := base.With('?')
	if !extended.Has('?') {
		t.Errorf("With('?') should register '?'")
	}
	if base.Has('?') {
		t.Errorf("original set must not be mutated by With")
	}
}

func TestModifierSetZeroValueHasNothing(t *testing.T) {
	var m ModifierSet
	if m.Has('+') {
		t.Errorf("zero ModifierSet should not contain '+'")
	}
}

func TestLexWithModifiersRegistersCustomPrefix(t *testing.T) {
	set := DefaultModifiers().With('?')
	tokens, errs := LexWithModifiers("?priority=3", set)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(tokens))
	}
	tok := tokens[0]
	if tok.Type != TokenField {
		t.Errorf("type = %v, want TokenField", tok.Type)
	}
	if tok.Modifier != '?' {
		t.Errorf("modifier = %q, want '?'", tok.Modifier)
	}
	if tok.Value != "priority=3" {
		t.Errorf("value = %q, want %q", tok.Value, "priority=3")
	}
}

func TestLexDoesNotRecogniseUnregisteredPrefix(t *testing.T) {
	tokens, _ := Lex("?priority=3")
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(tokens))
	}
	if tokens[0].Modifier != 0 {
		t.Errorf("expected no modifier, got %q", tokens[0].Modifier)
	}
	if tokens[0].Type != TokenField {
		t.Errorf("type = %v, want TokenField", tokens[0].Type)
	}
	if tokens[0].Value != "?priority=3" {
		t.Errorf("value = %q, want %q", tokens[0].Value, "?priority=3")
	}
}
