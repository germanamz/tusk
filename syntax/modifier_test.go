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
