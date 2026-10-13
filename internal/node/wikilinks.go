package node

import (
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/wikilink"
)

// MaterializeWikilinks appends body [[wikilink]] targets to every edge type
// flagged with `wikilinks = true`. Idempotent per edge via appendUnique, so
// repeated calls and duplicate links do not create duplicate edges.
//
// A relative target ([[../notes/x]]) is stored as the id it names from this
// file. A target that names a file by name ([[auth-rfc]]) is stored as written
// and listed in parsed.NameLinks: the index resolves it to a file once every
// file is known (index.ResolveLinks), and keeps re-resolving it as files come
// and go (#791).
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
			stored := wikilink.Normalize(target, parsed.Path)
			parsed.Edges[name] = appendUnique(parsed.Edges[name], stored)

			if wikilink.IsName(target) {
				if parsed.NameLinks == nil {
					parsed.NameLinks = map[string][]string{}
				}

				parsed.NameLinks[name] = appendUnique(parsed.NameLinks[name], stored)
			}
		}
	}
}
