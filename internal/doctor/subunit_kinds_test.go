package doctor_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

// loadSubUnitKindsManifest loads a minimal tusk.toml with the given
// [workspace] sub-units line.
func loadSubUnitKindsManifest(test *testing.T, subUnits string) *manifest.Manifest {
	test.Helper()

	path := filepath.Join(test.TempDir(), "tusk.toml")

	if writeErr := os.WriteFile(path, []byte("[workspace]\nname = \"x\"\n"+subUnits+"\n"), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(path)

	if loadErr != nil {
		test.Fatalf("manifest.Load: %v", loadErr)
	}

	return loaded
}

// TestRun_FragmentMissesFollowIndexedKinds reports a heading link that missed
// only while sections are indexed, and a block reference that missed only
// while every kind a ^block-id can name is (#853). A heading link into a note
// whose sections aren't indexed rests on the note, like one with sub-units
// off, and a block id can sit on any leaf, so with one leaf kind left out the
// miss may be a block that just isn't indexed.
func TestRun_FragmentMissesFollowIndexedKinds(test *testing.T) {
	testCases := []struct {
		name         string
		subUnits     string
		wantHeadings int
		wantBlocks   int
	}{
		{name: "every kind", subUnits: "", wantHeadings: 1, wantBlocks: 1},
		{name: "sections only", subUnits: `sub-units = ["section"]`, wantHeadings: 1, wantBlocks: 0},
		{name: "every leaf kind", subUnits: `sub-units = ["paragraph", "list-item", "code-block", "blockquote", "table-cell"]`, wantHeadings: 0, wantBlocks: 1},
		{name: "one leaf kind left out", subUnits: `sub-units = ["section", "paragraph", "list-item", "code-block", "blockquote"]`, wantHeadings: 1, wantBlocks: 0},
	}

	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			fixture := newDoctorFixture(test)

			fixture.file(test, "notes/rfc", "note")
			fixture.file(test, "docs/src", "note")

			link := func(target, text string) index.EdgeRow {
				return index.EdgeRow{Type: "references", SourceID: "docs/src", TargetID: target, SourcePath: "docs/src.md", Kind: "direct", Link: text}
			}

			if upsertErr := fixture.edges.UpsertAll("docs/src", "docs/src.md", []index.EdgeRow{
				link("notes/rfc", "rfc#^gone"),
				link("notes/rfc", "rfc#Renamed"),
			}); upsertErr != nil {
				test.Fatalf("UpsertAll: %v", upsertErr)
			}

			report := run(test, fixture.config(loadSubUnitKindsManifest(test, testCase.subUnits)))

			if got := len(issuesOfKind(report, doctor.IssueMissingHeading)); got != testCase.wantHeadings {
				test.Errorf("missing-heading issues = %d, want %d", got, testCase.wantHeadings)
			}

			if got := len(issuesOfKind(report, doctor.IssueMissingBlock)); got != testCase.wantBlocks {
				test.Errorf("missing-block issues = %d, want %d", got, testCase.wantBlocks)
			}
		})
	}
}

// TestRun_ExcludedKindRowsAreDirty reports rows of a kind `sub-units` leaves
// out as sub-units-disabled-dirty, naming the kinds and their counts, until a
// reindex drops them. Rows of indexed kinds are no issue (#853).
func TestRun_ExcludedKindRowsAreDirty(test *testing.T) {
	fixture := newDoctorFixture(test)

	fixture.file(test, "notes/n", "note")

	parent := sql.NullString{String: "notes/n", Valid: true}
	rows := []index.NodeRow{
		{ID: "notes/n#P1", Type: "paragraph"},
		{ID: "notes/n#T1R0C0", Type: "table-cell"},
		{ID: "notes/n#T1R1C0", Type: "table-cell"},
		{ID: "notes/n#L1", Type: "list-item"},
	}

	for position := range rows {
		rows[position].Path = "notes/n.md"
		rows[position].PropertiesJSON = "{}"
		rows[position].LastChecksum = "x"
		rows[position].ParentID = parent
		rows[position].Ordinal = sql.NullInt64{Int64: int64(position), Valid: true}
	}

	if bulkErr := fixture.nodes.BulkUpsert(rows, "markdown"); bulkErr != nil {
		test.Fatalf("bulk upsert sub-units: %v", bulkErr)
	}

	report := run(test, fixture.config(loadSubUnitKindsManifest(test, `sub-units = ["paragraph"]`)))
	dirty := issuesOfKind(report, doctor.IssueSubUnitsDisabledDirty)

	if len(dirty) != 1 {
		test.Fatalf("sub-units-disabled-dirty issues = %+v, want 1", dirty)
	}

	for _, want := range []string{"list-item 1", "table-cell 2", "`tusk reindex`"} {
		if !strings.Contains(dirty[0].Message, want) {
			test.Errorf("message = %q, want it to contain %q", dirty[0].Message, want)
		}
	}

	if strings.Contains(dirty[0].Message, "paragraph") {
		test.Errorf("message = %q names an indexed kind", dirty[0].Message)
	}

	clean := run(test, fixture.config(loadSubUnitKindsManifest(test, `sub-units = ["paragraph", "list-item", "table-cell"]`)))

	if got := issuesOfKind(clean, doctor.IssueSubUnitsDisabledDirty); len(got) != 0 {
		test.Errorf("sub-units-disabled-dirty with every stored kind indexed = %+v, want none", got)
	}
}
