package bookview

import (
	"github.com/germanamz/tusk/internal/node"
)

// resolveWikilinks maps every [[target]] in body to the node it resolves to, so
// the frontend can rewrite each link into an in-app navigation — or a dead-link
// marker — without a round trip per link. That is the whole point of resolving
// server-side: a reader's note may carry dozens of links, and the alternative is
// a request each.
//
// The keys are node.ExtractWikilinks's targets verbatim, which is what makes the
// map usable: they are alias-stripped ([[b|Bee]] keys on "b" — the label is
// presentation and is never resolved), trimmed, de-duplicated, and keep any
// "#section" fragment. The client cuts the same target out of the body text it
// is rewriting, so it finds each key by the cut it has already made.
//
// The body passed in must be the one the client renders (frontmatter stripped):
// a frontmatter ref is never rewritten, so an entry for it could only be a key
// nothing matches. Those refs already reach the reader through the links rails.
//
// Alias splitting is deliberately delegated to node.ExtractWikilinks rather than
// re-implemented here. The resolver and the rewriter agreeing on where a target
// ends is load-bearing — a second copy of that cut is exactly the bug #690 fixed.
//
// The returned map is always non-nil: it marshals to an object, and a nil map
// would put "wikilinks": null on the wire, forcing every consumer to null-check
// before indexing. ExtractWikilinks returns a nil slice for a link-free body, so
// that is the common path, not an edge case.
func (srv *Server) resolveWikilinks(body []byte) (map[string]WikilinkTarget, error) {
	targets := node.ExtractWikilinks(body)
	resolved := make(map[string]WikilinkTarget, len(targets))

	// Most targets name an id, so try every one that way first — in a single
	// batched lookup, rolled up to file level (see fileRowsFor).
	byID, idErr := srv.fileRowsFor(targets)

	if idErr != nil {
		return nil, idErr
	}

	// What is left names a title rather than an id ([[Some Note]]), the other
	// form a human writes. FindByTitle has no batched form, so this costs one
	// lookup per unresolved target — but it only collects ids, leaving the rows
	// to a second batched call rather than a Get each.
	matchedIDs := make([]string, 0, len(targets))
	titleMatches := make(map[string]string, len(targets))

	for _, target := range targets {
		if row, found := byID[target]; found {
			resolved[target] = WikilinkTarget{ID: row.ID, Title: row.Title, Exists: true}

			continue
		}

		// "*": a wikilink names a node, not a node of some expected type.
		ids, findErr := srv.deps.Nodes.FindByTitle("*", target)

		if findErr != nil {
			return nil, findErr
		}

		if len(ids) == 0 {
			// Present in the map, and unresolved. The client needs the entry to
			// mark the link dead; omitting it would read as "not a link".
			resolved[target] = WikilinkTarget{Exists: false}

			continue
		}

		// Ids come back ordered by id ASC and the title may be ambiguous. The
		// first is a stable, arbitrary pick — and the ordering quietly favours
		// the right one: a file sorts before its own sub-units ("c" < "c#S1").
		matchedIDs = append(matchedIDs, ids[0])
		titleMatches[target] = ids[0]
	}

	// FindByTitle has no parent_id filter, so a title match can be a sub-unit.
	// Roll those up exactly as the id path does, or a section titled like a note
	// would resolve to an id whose payload contradicts itself.
	byTitle, titleErr := srv.fileRowsFor(matchedIDs)

	if titleErr != nil {
		return nil, titleErr
	}

	for target, matchedID := range titleMatches {
		row, found := byTitle[matchedID]

		if !found {
			// The title matched a row whose file is gone — nothing navigable
			// stands behind it, so it is a dead link, not a blank one.
			resolved[target] = WikilinkTarget{Exists: false}

			continue
		}

		resolved[target] = WikilinkTarget{ID: row.ID, Title: row.Title, Exists: true}
	}

	return resolved, nil
}
