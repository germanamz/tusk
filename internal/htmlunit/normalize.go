package htmlunit

import "github.com/germanamz/tusk/internal/htmltext"

// NormalizeText renders HTML source to deterministic plain prose: tags
// stripped, block elements separated by a blank line, inline elements joined,
// HTML entities decoded, intra-block whitespace collapsed, and
// head/script/style/comment content excluded.
//
// The implementation lives in the dependency-light leaf package
// internal/htmltext so the same normalizer can be shared by internal/node
// (which cannot import htmlunit — see the htmltext package doc for the cycle)
// and the render verb. This function is retained as htmlunit's public surface.
func NormalizeText(source []byte) string {
	return htmltext.NormalizeText(source)
}
