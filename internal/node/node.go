// Package node owns the markdown-file representation of a node and the
// service operations that create, read, and list them.
package node

// Node is the parsed representation of a markdown node file.
type Node struct {
	ID         string              // workspace-relative path without extension
	Path       string              // workspace-relative path with extension
	Type       string              // value of the required `type:` frontmatter field
	Title      string              // value of the optional `title:` frontmatter field; empty if absent
	Properties map[string]any      // frontmatter keys NOT matching a declared edge type
	Edges      map[string][]string // edge-type-name → ordered list of target node ids
	PathValues map[string][]string // paths-only edge-type-name → the workspace paths its frontmatter value names; set by ResolveEdges, recorded as path refs by PathRefs
	Body       []byte              // markdown body after the closing `---` delimiter
	BodyOffset int                 // byte offset in the file as read where Body begins; sub-unit line numbers count from the file's top
	HTMLLinks  []string            // raw <a href> values in document order; populated only for HTML nodes by ParseHTMLFile, resolved to edges by MaterializeHTMLLinks
	HTMLCode   []string            // text of <code> elements outside <pre>, in document order; populated only for HTML nodes by ParseHTMLFile, scanned for path refs by PathRefs
	HTMLImages []string            // raw <img src> values in document order; populated only for HTML nodes by ParseHTMLFile, scanned for path refs by PathRefs
}

// Clone returns a shallow copy of the Node with the Properties, Edges and
// PathValues maps deep-copied so callers can mutate the clone without
// affecting the original.
func (nd *Node) Clone() *Node {
	if nd == nil {
		return nil
	}

	cloned := *nd

	if nd.Properties != nil {
		cloned.Properties = make(map[string]any, len(nd.Properties))

		for key, value := range nd.Properties {
			cloned.Properties[key] = value
		}
	}

	cloned.Edges = cloneTargets(nd.Edges)
	cloned.PathValues = cloneTargets(nd.PathValues)

	return &cloned
}

// cloneTargets deep-copies an edge-type-name → targets map, keeping nil nil.
func cloneTargets(source map[string][]string) map[string][]string {
	if source == nil {
		return nil
	}

	cloned := make(map[string][]string, len(source))

	for key, targets := range source {
		copied := make([]string, len(targets))
		copy(copied, targets)
		cloned[key] = copied
	}

	return cloned
}
