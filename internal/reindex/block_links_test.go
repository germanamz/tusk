package reindex_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

// The quotes note's units, as the sub-unit walker addresses them, with the
// ids its markers give them:
//
//	Intro paragraph.                      P1
//	A quote worth keeping. ^quote-1       P2  quote-1
//	- first                               L1  list-1 (the line after the list)
//	- second                              L2
const blockQuotes = "Intro paragraph.\n\nA quote worth keeping. ^quote-1\n\n- first\n- second\n\n^list-1\n"

// Block references resolve to the units Obsidian's ^block-id markers name and
// follow the ids as the note changes, without the linking note being re-read
// (#819).
func TestRun_BlockReferencesResolveToUnitsAndFollowEdits(test *testing.T) {
	root, store := nameLinkFixture(test)

	writeRaw(test, root, "notes/quotes.md", []byte(blockQuotes))
	writeRaw(test, root, "inbox/blocks.md", []byte("See [[quotes#^quote-1]], [[notes/quotes#^LIST-1|the list]] and [[quotes#^later]].\n"))

	runNameLinks(test, root, store, "")

	// ^later names nothing yet, so it lands on the note.
	wantEdgeTargets(test, store, "inbox/blocks.md", "references", "notes/quotes", "notes/quotes#L1", "notes/quotes#P2")

	nodes := index.NewNodeRepo(store)
	quote, getErr := nodes.Get("notes/quotes#P2")

	if getErr != nil {
		test.Fatalf("Get: %v", getErr)
	}

	if quote.EmbedPayload.String != "A quote worth keeping." || quote.NameKey.String != "^quote-1" {
		test.Errorf("quote row = %+v, want the text without its marker, keyed ^quote-1", quote)
	}

	// The marker line after the list is no unit of its own.
	if marker, _ := nodes.Get("notes/quotes#P3"); marker != nil {
		test.Errorf("the ^list-1 line is a unit: %+v", marker)
	}

	// A paragraph ahead renumbers the rest, ^quote-1 moves to a later
	// paragraph and ^later appears.
	rewriteLater(test, root, "notes/quotes.md", []byte(
		"New first.\n\nIntro paragraph. ^later\n\nA quote worth keeping.\n\nAnother quote. ^quote-1\n\n- first\n- second\n\n^list-1\n",
	))

	runNameLinks(test, root, store, "")

	wantEdgeTargets(test, store, "inbox/blocks.md", "references", "notes/quotes#L1", "notes/quotes#P2", "notes/quotes#P4")
}
