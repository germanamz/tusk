package embed_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/index"
)

// leafFixture is the format fixture's file "notes/a" (title "x") with a
// section "Async" under it and one paragraph leaf in that section, the leaf
// queued after the file and section rows exist. The file is drained first so
// a recorder sees only the leaf.
func leafFixture(test *testing.T, fileTitle, propertiesJSON string) formatFixture {
	test.Helper()

	fixture := newFormatFixture(test)

	if upsertErr := fixture.nodes.Upsert(index.NodeRow{
		ID: "notes/a", Type: "service", Path: "notes/a.md", Title: fileTitle, PropertiesJSON: propertiesJSON, LastChecksum: "x",
	}); upsertErr != nil {
		test.Fatalf("upsert file: %v", upsertErr)
	}

	if upsertErr := fixture.nodes.BulkUpsert([]index.NodeRow{
		{
			ID: "notes/a#S1", Type: "section", Path: "notes/a.md", Title: "Async", PropertiesJSON: "{}", LastChecksum: "x",
			ParentID: sql.NullString{String: "notes/a", Valid: true},
			Ordinal:  sql.NullInt64{Int64: 0, Valid: true},
		},
		{
			ID: "notes/a#S1P1", Type: "paragraph", Path: "notes/a.md", Title: "leaf text", PropertiesJSON: "{}", LastChecksum: "x",
			ParentID:     sql.NullString{String: "notes/a#S1", Valid: true},
			Ordinal:      sql.NullInt64{Int64: 1, Valid: true},
			EmbedPayload: sql.NullString{String: "leaf text", Valid: true},
		},
	}, "markdown"); upsertErr != nil {
		test.Fatalf("upsert sub-units: %v", upsertErr)
	}

	fixture.drain(test, &recordingEmbedder{dim: 3, model: "stub"})
	fixture.enqueue(test, "notes/a#S1P1")

	return fixture
}

// TestDrainQueue_LeafEmbedsItsContext pins #862: a leaf embeds its file's
// title (and under "path" its headings, plus any named properties) in front
// of its passage. The stored body stays the bare passage, and the hash covers
// the context, so a context change can't skip-ack or reuse the old vector.
func TestDrainQueue_LeafEmbedsItsContext(test *testing.T) {
	cases := []struct {
		name       string
		fileTitle  string
		properties string
		format     embed.Format
		want       string
	}{
		{
			name:   "none embeds the passage bare",
			format: embed.Format{SubUnitContext: "none"},
			want:   "leaf text",
		},
		{
			name:   "title puts the file title in front",
			format: embed.Format{SubUnitContext: "title"},
			want:   "x\nleaf text",
		},
		{
			name:   "path adds the enclosing headings",
			format: embed.Format{SubUnitContext: "path"},
			want:   "x > Async\nleaf text",
		},
		{
			name:      "an untitled file goes by its file name",
			fileTitle: "-",
			format:    embed.Format{SubUnitContext: "title"},
			want:      "a\nleaf text",
		},
		{
			name:   "{title} in document-prefix takes the label instead of a line",
			format: embed.Format{SubUnitContext: "path", DocumentPrefix: "title: {title} | text: "},
			want:   "title: x > Async | text: leaf text",
		},
		{
			name:       "named properties follow the label",
			properties: `{"aliases":["ls","ledger"],"owner":"payments"}`,
			format:     embed.Format{SubUnitContext: "title", SubUnitProperties: []string{"aliases", "type", "missing"}},
			want:       "x\naliases: ls, ledger\ntype: service\nleaf text",
		},
		{
			name:       "properties alone under none, with {title} left at none",
			properties: `{"aliases":["ls"]}`,
			format:     embed.Format{SubUnitContext: "none", SubUnitProperties: []string{"aliases"}, DocumentPrefix: "title: {title} | text: "},
			want:       "title: none | text: aliases: ls\nleaf text",
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			fileTitle := "x"

			if testCase.fileTitle == "-" {
				fileTitle = ""
			}

			properties := testCase.properties

			if properties == "" {
				properties = "{}"
			}

			fixture := leafFixture(test, fileTitle, properties)
			recorder := &recordingEmbedder{dim: 3, model: "stub", format: testCase.format}

			fixture.drain(test, recorder)

			assertSent(test, recorder, testCase.want)

			row := fixture.onlyRow(test, "notes/a#S1P1")

			if row.Body != "leaf text" {
				test.Errorf("Body = %q, want the bare passage", row.Body)
			}

			if row.ContentHash != sha256Hex(testCase.want) {
				test.Errorf("ContentHash = %s, want sha256 of the text sent", row.ContentHash)
			}
		})
	}
}

// TestDrainQueue_LeafContextChangeReembeds: re-queued after its file is
// retitled, a leaf whose passage is unchanged re-embeds instead of
// skip-acking, because its hash covers the title.
func TestDrainQueue_LeafContextChangeReembeds(test *testing.T) {
	fixture := leafFixture(test, "x", "{}")
	format := embed.Format{SubUnitContext: "title"}

	fixture.drain(test, &recordingEmbedder{dim: 3, model: "stub", format: format})

	if upsertErr := fixture.nodes.Upsert(index.NodeRow{
		ID: "notes/a", Type: "service", Path: "notes/a.md", Title: "ledger-sync", PropertiesJSON: "{}", LastChecksum: "y",
	}); upsertErr != nil {
		test.Fatalf("retitle file: %v", upsertErr)
	}

	fixture.enqueue(test, "notes/a#S1P1")

	recorder := &recordingEmbedder{dim: 3, model: "stub", format: format}

	fixture.drain(test, recorder)

	assertSent(test, recorder, "ledger-sync\nleaf text")
}

// TestDrainQueue_LeafWithMissingParentFails: a leaf whose parent row can't be
// read fails as its own input, through the retry policy, rather than
// embedding without its context. The drain retries it until it parks, which
// doctor reports. (A leaf whose file went away mid-drain is deleted with the
// file, and its job with it.)
func TestDrainQueue_LeafWithMissingParentFails(test *testing.T) {
	fixture := newFormatFixture(test)

	if upsertErr := fixture.nodes.BulkUpsert([]index.NodeRow{{
		ID: "notes/a#P1", Type: "paragraph", Path: "notes/a.md", PropertiesJSON: "{}", LastChecksum: "x",
		ParentID:     sql.NullString{String: "notes/gone", Valid: true},
		Ordinal:      sql.NullInt64{Int64: 0, Valid: true},
		EmbedPayload: sql.NullString{String: "leaf text", Valid: true},
	}}, "markdown"); upsertErr != nil {
		test.Fatalf("upsert sub-unit: %v", upsertErr)
	}

	fixture.drain(test, &recordingEmbedder{dim: 3, model: "stub"})
	fixture.enqueue(test, "notes/a#P1")

	recorder := &recordingEmbedder{dim: 3, model: "stub", format: embed.Format{SubUnitContext: "title"}}

	fixture.drain(test, recorder)

	assertSent(test, recorder)

	parked, listErr := fixture.queue.ListParked()

	if listErr != nil {
		test.Fatalf("ListParked: %v", listErr)
	}

	if len(parked) != 1 || parked[0].NodeID != "notes/a#P1" || !strings.Contains(parked[0].LastError, "read context") {
		test.Errorf("parked = %+v, want the leaf parked with its context error", parked)
	}
}
