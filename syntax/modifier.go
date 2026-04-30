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
	var ms ModifierSet
	ms.mask['+'] = true
	ms.mask['-'] = true
	return ms
}

// Has reports whether marker is registered in the set.
func (ms ModifierSet) Has(marker byte) bool {
	return ms.mask[marker]
}

// With returns a copy of the set with marker added. The receiver is not modified.
func (ms ModifierSet) With(marker byte) ModifierSet {
	out := ms
	out.mask[marker] = true
	return out
}
