// Package node owns the markdown-file representation of a node and the
// service operations that create, read, and list them.
package node

// Node is the parsed representation of a markdown node file.
type Node struct {
	ID         string         // workspace-relative path without extension
	Path       string         // workspace-relative path with extension
	Type       string         // value of the required `type:` frontmatter field
	Title      string         // value of the optional `title:` frontmatter field; empty if absent
	Properties map[string]any // all frontmatter keys (including `type` and `title`)
	Body       []byte         // markdown body after the closing `---` delimiter
}
