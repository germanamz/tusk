package htmlunit

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"
)

// blockTags is the set of element names that introduce a paragraph
// break (blank line) in normalized output. Inline elements (em, span,
// a, strong, …) do not break.
var blockTags = map[string]bool{
	"p": true, "div": true, "section": true, "article": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"ul": true, "ol": true, "li": true, "blockquote": true,
	"pre": true, "table": true, "tr": true, "td": true, "th": true,
	"header": true, "footer": true, "main": true, "aside": true, "nav": true,
}

// NormalizeText renders HTML source to deterministic plain prose:
// tags stripped, block elements separated by a blank line, inline
// elements joined, HTML entities decoded (by x/net/html), intra-block
// whitespace collapsed to single spaces, and head/script/style/comment
// content excluded. Used by the file-level body (Phase 3) and the
// render verb (Phase 6).
func NormalizeText(source []byte) string {
	doc, err := html.Parse(bytes.NewReader(source))
	if err != nil {
		return ""
	}

	var blocks []string
	var cur strings.Builder

	flush := func() {
		text := strings.Join(strings.Fields(cur.String()), " ")
		if text != "" {
			blocks = append(blocks, text)
		}
		cur.Reset()
	}

	var visit func(node *html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode {
			switch node.Data {
			case "head", "script", "style":
				return
			}
		}
		if node.Type == html.TextNode {
			cur.WriteString(node.Data)
			cur.WriteByte(' ')
		}

		isBlock := node.Type == html.ElementNode && blockTags[node.Data]
		if isBlock {
			flush()
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
		if isBlock {
			flush()
		}
	}

	visit(doc)
	flush()

	return strings.Join(blocks, "\n\n")
}
