package bookview

import (
	"strings"

	"github.com/germanamz/tusk/internal/index"
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

		matchedID := titleMatchID(ids)
		matchedIDs = append(matchedIDs, matchedID)
		titleMatches[target] = matchedID
	}

	// A match that is still a sub-unit here is one no file bore the title for.
	// Roll it up exactly as the id path does, or a section titled like a note
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

// titleMatchID picks which of a title's matching ids the reader should land on.
// It must be called with a non-empty list — an unmatched title is a dead link,
// which is the caller's case to answer, not a pick between candidates.
//
// FindByTitle runs no parent_id filter, so its ids include sub-unit rows: a
// section heading titled "Introduction" matches alongside a file titled
// "Introduction". A file row is the stronger match by far — its OWN title is
// what the reader typed. A sub-unit match only ever reaches the file CONTAINING
// it, whose title is something else entirely, so preferring the file is what
// keeps [[Introduction]] landing on the note actually called Introduction.
//
// Id-ASC ordering does not do this on its own, which is the trap this replaces.
// A file does sort before its OWN sub-units ("c" < "c#S1"), but that is not the
// ambiguity that bites. The collision is CROSS-file, and there the order runs
// the other way: '#' (0x23) < '-' (0x2D), so the sub-unit "alpha#S2" sorts ahead
// of the file "zeta" that genuinely bears the title — and ahead of "c-notes".
// Taking ids[0] there rolled up to an unrelated note and rendered it live.
//
// Rolling up a sub-unit match is still right when NO file bears the title:
// [[Some Section Heading]] reaching the note that contains it is the useful
// reading of that link. This reorders the preference; it never drops a match.
//
// Both branches take the first id of an id-ASC list, so an ambiguity WITHIN a
// kind stays a stable, arbitrary pick. Core refuses instead (RefErrAmbiguous,
// node/refs.go) and a read view cannot error at a reader mid-page — but it must
// not do WORSE than the write path, which for an ambiguous title creates no edge
// rather than an edge to the wrong note.
func titleMatchID(ids []string) string {
	for _, id := range ids {
		// No separator means a file id: sub-unit ids are "<fileID>#<address>",
		// and a file id can never contain "#" (index.ReservedIDReason rejects it
		// at every write boundary, which is what links.go's fileIDOf leans on).
		if !strings.Contains(id, index.SubUnitIDSeparator) {
			return id
		}
	}

	return ids[0]
}
