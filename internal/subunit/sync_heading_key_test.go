package subunit_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/germanamz/tusk/internal/htmlunit"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/subunit"
)

// A section row carries the heading key a heading link is matched on (#808),
// and a row written before the key existed turns over to gain it without a
// re-embed; leaves carry none.
func TestSync_SectionRowsCarryTheirHeadingKey(test *testing.T) {
	store := openSyncTestIndex(test)
	sync, nodes, _, queue := newSync(store, referencesManifest(test))

	parent := seedFileRow(test, nodes, "notes/keys", "notes/keys.md")
	body := "# Open Questions: *Phase 2*\n\nbody\n"

	if _, applyErr := sync.ApplyFile(context.Background(), parent, parseWithLines(test, body)); applyErr != nil {
		test.Fatalf("ApplyFile: %v", applyErr)
	}

	section, getErr := nodes.Get("notes/keys#S1")

	if getErr != nil {
		test.Fatalf("Get: %v", getErr)
	}

	if section.NameKey != (sql.NullString{String: "open questions phase 2", Valid: true}) {
		test.Errorf("section name_key = %+v, want the folded heading", section.NameKey)
	}

	if paragraph, _ := nodes.Get("notes/keys#S1P1"); paragraph.NameKey.Valid {
		test.Errorf("paragraph name_key = %+v, want NULL", paragraph.NameKey)
	}

	// The row as an index from before #808 holds it.
	stale := *section
	stale.NameKey = sql.NullString{}

	if upsertErr := nodes.BulkUpsert([]index.NodeRow{stale}, "markdown"); upsertErr != nil {
		test.Fatalf("BulkUpsert: %v", upsertErr)
	}

	drainEmbedQueue(test, queue)

	result, applyErr := sync.ApplyFile(context.Background(), parent, parseWithLines(test, body))

	if applyErr != nil {
		test.Fatalf("ApplyFile again: %v", applyErr)
	}

	if result.Reordered != 1 {
		test.Errorf("Reordered = %d, want the keyless section turned over", result.Reordered)
	}

	if refilled, _ := nodes.Get("notes/keys#S1"); refilled.NameKey.String != "open questions phase 2" {
		test.Errorf("refilled name_key = %+v", refilled.NameKey)
	}

	if depth, _ := queue.Depth(); depth != 0 {
		test.Errorf("filling a key re-enqueued %d embeds, want 0", depth)
	}
}

// The markdown and HTML walkers read the same heading text for a heading, so
// a heading link finds a section in either format.
func TestParse_MarkdownAndHTMLSectionsShareTheirHeading(test *testing.T) {
	markdown, markdownErr := subunit.Parse([]byte("## Foo: `Bar` *baz*\n"))
	html, htmlErr := htmlunit.Parse([]byte("<h2>Foo: <code>Bar</code> <em>baz</em></h2>"))

	if markdownErr != nil || htmlErr != nil || len(markdown) != 1 || len(html) != 1 {
		test.Fatalf("Parse: %+v, %v; %+v, %v", markdown, markdownErr, html, htmlErr)
	}

	if markdown[0].Heading != "Foo: Bar baz" || html[0].Heading != markdown[0].Heading {
		test.Errorf("headings = %q (markdown), %q (HTML); want both %q", markdown[0].Heading, html[0].Heading, "Foo: Bar baz")
	}
}
