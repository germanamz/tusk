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
// file. A target found by lookup (wikilink.NeedsLookup: a file name,
// [[auth-rfc]], or any #fragment, [[notes/x#Some heading]]) is also listed in
// parsed.LookupLinks: the index resolves it once every file is known
// (index.ResolveLinks), and keeps re-resolving it as files and their headings
// come and go (#791, #808).
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

			if wikilink.NeedsLookup(target) {
				if parsed.LookupLinks == nil {
					parsed.LookupLinks = map[string][]string{}
				}

				parsed.LookupLinks[name] = appendUnique(parsed.LookupLinks[name], stored)
			}
		}
	}
}
