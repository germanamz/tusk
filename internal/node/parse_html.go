package node

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// htmlMetaPrefix is the name= prefix that marks a <meta> tag as a Tusk
// directive: <meta name="tusk:KEY" content="VALUE">.
const htmlMetaPrefix = "tusk:"

// htmlTypeKey and htmlTitleKey are the reserved meta keys that map to the
// node's Type and Title fields rather than to Properties.
const (
	htmlTypeKey  = "type"
	htmlTitleKey = "title"
)

// ParseHTMLFile parses content as a standalone HTML node file. relPath is the
// workspace-relative path (with extension); unlike ParseFile, the HTML node id
// RETAINS its extension (foo.html -> id "foo.html") so it never collides with a
// same-stem markdown node. The parse is lenient: malformed HTML is parsed like a
// browser and never errors. A node type is required via
// <meta name="tusk:type" content="...">; its absence returns ErrMissingType,
// mirroring markdown's required `type:` frontmatter.
func ParseHTMLFile(relPath string, content []byte) (*Node, error) {
	root, parseErr := html.Parse(strings.NewReader(string(content)))

	if parseErr != nil {
		return nil, parseErr
	}

	directives := collectMetaDirectives(root)

	typeValue := directives.last(htmlTypeKey)

	if typeValue == "" {
		return nil, ErrMissingType
	}

	title := directives.last(htmlTitleKey)

	if title == "" {
		title = firstElementText(root, func(node *html.Node) bool {
			return node.DataAtom == atom.Title
		})
	}

	if title == "" {
		title = firstElementText(root, func(node *html.Node) bool {
			return node.DataAtom == atom.H1
		})
	}

	return &Node{
		ID:         relPath,
		Path:       relPath,
		Type:       typeValue,
		Title:      title,
		Properties: map[string]any{},
	}, nil
}

// metaDirectives holds tusk:KEY meta values in document order. A key may repeat;
// callers decide last-wins vs. list collection.
type metaDirectives struct {
	order  []string
	values map[string][]string
}

func (directives metaDirectives) last(key string) string {
	vals := directives.values[key]

	if len(vals) == 0 {
		return ""
	}

	return vals[len(vals)-1]
}

// collectMetaDirectives walks the parsed tree and records every
// <meta name="tusk:KEY" content="VALUE"> with the "tusk:" prefix stripped from
// the key, in document order.
func collectMetaDirectives(root *html.Node) metaDirectives {
	directives := metaDirectives{values: map[string][]string{}}

	var walk func(node *html.Node)

	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.DataAtom == atom.Meta {
			var name, contentAttr string

			for _, attr := range node.Attr {
				switch attr.Key {
				case "name":
					name = attr.Val
				case "content":
					contentAttr = attr.Val
				}
			}

			if strings.HasPrefix(name, htmlMetaPrefix) {
				key := strings.TrimPrefix(name, htmlMetaPrefix)

				if _, seen := directives.values[key]; !seen {
					directives.order = append(directives.order, key)
				}

				directives.values[key] = append(directives.values[key], contentAttr)
			}
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}

	walk(root)

	return directives
}

// firstElementText returns the collapsed text content of the first element in
// document order satisfying match, or "" if none match. Whitespace runs are
// collapsed to single spaces and the result is trimmed.
func firstElementText(root *html.Node, match func(*html.Node) bool) string {
	var found *html.Node

	var walk func(node *html.Node)

	walk = func(node *html.Node) {
		if found != nil {
			return
		}

		if node.Type == html.ElementNode && match(node) {
			found = node

			return
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}

	walk(root)

	if found == nil {
		return ""
	}

	return collapseText(elementText(found))
}

// elementText concatenates all descendant text-node content of node in
// document order, with no whitespace processing.
func elementText(node *html.Node) string {
	var builder strings.Builder

	var walk func(current *html.Node)

	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
		}

		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}

	walk(node)

	return builder.String()
}

// collapseText collapses internal whitespace runs to single spaces and trims
// the result.
func collapseText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
