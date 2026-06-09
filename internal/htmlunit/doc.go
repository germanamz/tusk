// Package htmlunit parses standalone HTML content into the same flat
// []subunit.Unit representation the markdown sub-unit parser emits,
// plus a deterministic plain-text normalizer. It is a pure
// transformation layer: it never touches the index, the embed queue,
// or any repository. The reserved Kind names and the structural
// address grammar mirror internal/subunit verbatim; the namespace is
// distinguished downstream by the "html" source column, not by this
// package.
package htmlunit
