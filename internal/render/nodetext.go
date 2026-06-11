package render

import (
	"bytes"
	"path/filepath"
	"strings"

	"github.com/germanamz/tusk/internal/htmltext"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

// frontmatterDelimiter opens and closes a YAML frontmatter block. Render strips
// the block so node metadata never leaks into the rendered prose.
var frontmatterDelimiter = []byte("---")

// plaintextParser mirrors the goldmark instance internal/subunit uses
// (CommonMark + Table + TaskList, footnotes off) so markdownPlainText sees the
// same AST the sub-unit pass does. It is package-scoped and reused per call.
var plaintextParser = goldmark.New(
	goldmark.WithExtensions(
		extension.Table,
		extension.TaskList,
	),
)

// NodeText renders a node's raw body to plain text, dispatching by the path's
// file extension. HTML kinds (.html/.htm) route through htmltext.NormalizeText
// (the same normalizer the HTML indexing pass uses); every other extension —
// including .md and the bare-stem case — routes through markdownPlainText.
// The function is pure: it performs no I/O and never mutates body.
func NodeText(path string, body []byte) string {
	switch filepath.Ext(path) {
	case ".html", ".htm":
		return htmltext.NormalizeText(body)
	default:
		return markdownPlainText(body)
	}
}

// markdownPlainText extracts the prose from markdown source: it parses with the
// shared goldmark parser and walks each top-level block down to its leaf text
// nodes, concatenating only the rendered characters. Markup tokens (emphasis
// markers, list bullets, headings hashes) are dropped because they are syntax,
// not text leaves. Top-level blocks are joined with a blank line so paragraphs
// stay separated. The function is pure and performs no I/O.
func markdownPlainText(body []byte) string {
	source := stripFrontmatter(body)
	doc := plaintextParser.Parser().Parse(text.NewReader(source))

	var parts []string

	for block := doc.FirstChild(); block != nil; block = block.NextSibling() {
		segment := strings.TrimRight(blockText(block, source), "\n")

		if segment != "" {
			parts = append(parts, segment)
		}
	}

	return strings.TrimRight(strings.Join(parts, "\n\n"), "\n")
}

// blockText collects the plain-text leaves of a single block subtree. It walks
// the AST and appends only the textual content of leaf nodes (Text, String,
// inline/fenced code), skipping the markup tokens goldmark keeps in the source
// spans of container nodes. This is the fidelity boundary the plan calls out:
// code-block and table layout collapse to their character content.
func blockText(node ast.Node, source []byte) string {
	var out strings.Builder

	_ = ast.Walk(node, func(walked ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch typed := walked.(type) {
		case *ast.Text:
			out.Write(typed.Segment.Value(source))
		case *ast.String:
			out.Write(typed.Value)
		case *ast.CodeSpan:
			out.WriteString(string(typed.Text(source))) //nolint:staticcheck // Text on a code span yields its literal content; no replacement gives the same view.

			return ast.WalkSkipChildren, nil
		case *ast.FencedCodeBlock:
			writeLines(&out, walked, source)

			return ast.WalkSkipChildren, nil
		case *ast.CodeBlock:
			writeLines(&out, walked, source)

			return ast.WalkSkipChildren, nil
		}

		return ast.WalkContinue, nil
	})

	return out.String()
}

// stripFrontmatter removes a leading YAML frontmatter block ("---" … "---")
// from a markdown body so node metadata never appears in the rendered prose.
// Bodies without a frontmatter block are returned unchanged.
func stripFrontmatter(body []byte) []byte {
	trimmed := bytes.TrimLeft(body, " \t\r\n")

	if !bytes.HasPrefix(trimmed, frontmatterDelimiter) {
		return body
	}

	afterOpen := bytes.TrimLeft(trimmed[len(frontmatterDelimiter):], "\r\n")

	closingIndex := bytes.Index(afterOpen, append([]byte("\n"), frontmatterDelimiter...))

	if closingIndex < 0 {
		return body
	}

	rest := afterOpen[closingIndex+len("\n")+len(frontmatterDelimiter):]

	return bytes.TrimLeft(rest, "\r\n")
}

// writeLines appends the raw line content of a code block to out.
func writeLines(out *strings.Builder, node ast.Node, source []byte) {
	lines := node.Lines()

	for index := 0; index < lines.Len(); index++ {
		segment := lines.At(index)
		out.Write(segment.Value(source))
	}
}
