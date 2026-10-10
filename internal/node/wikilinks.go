package node

import (
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/wikilink"
)

// MaterializeWikilinks appends body [[wikilink]] targets to every edge type
// flagged with `wikilinks = true`. Idempotent per edge via appendUnique, so
// repeated calls and duplicate links do not create duplicate edges.
func MaterializeWikilinks(parsed *Node, edgeTypes manifest.EdgeTypes) {
	// Extract once: the body's wikilinks do not change per edge type, but the
	// extraction strips fenced code and runs a regex over the whole body. The
	// HTML twin (ResolveHTMLLinks) already hoists its equivalent.
	targets := wikilink.Extract(parsed.Body)

	for name, edgeType := range edgeTypes {
		if !edgeType.Wikilinks {
			continue
		}

		for _, target := range targets {
			parsed.Edges[name] = appendUnique(parsed.Edges[name], target)
		}
	}
}
