package syntax

// ModifierSet is an immutable set of byte-valued token prefix markers the
// lexer recognises. The syntax package attaches no semantics to any member —
// whether '+' means "add", "include", or "append" is the consumer's call.
//
// Extending the recognised set is deliberately a one-line change: add a byte
// in DefaultModifiers, or call With on an existing set to build a custom
// registry at the call site.
type ModifierSet struct {
	mask [256]bool
}

// DefaultModifiers returns the built-in set recognised by the default Lex.
// To register a new global modifier, add its byte here.
func DefaultModifiers() ModifierSet {
	var m ModifierSet
	m.mask['+'] = true
	m.mask['-'] = true
	return m
}

// Has reports whether b is registered in the set.
func (m ModifierSet) Has(b byte) bool {
	return m.mask[b]
}

// With returns a copy of the set with b added. The receiver is not modified.
func (m ModifierSet) With(b byte) ModifierSet {
	out := m
	out.mask[b] = true
	return out
}
