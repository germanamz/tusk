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

// TestRun_FragmentMissesFollowTargetType gates missing-heading and
// missing-block on the kinds the target note's type indexes, its own
// sub-units when its [node-types] table sets them (#867).
func TestRun_FragmentMissesFollowTargetType(test *testing.T) {
	testCases := []struct {
		name     string
		subUnits string
		reported string // the note whose misses are reported
		silent   string // the note whose misses rest on it
	}{
		{name: "meeting narrows", subUnits: "\n[node-types.meeting]\nsub-units = [\"paragraph\"]", reported: "notes/rfc", silent: "meetings/m"},
		{name: "meeting widens", subUnits: "sub-units = false\n\n[node-types.meeting]\nsub-units = true", reported: "meetings/m", silent: "notes/rfc"},
	}

	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			fixture := newDoctorFixture(test)

			fixture.file(test, "notes/rfc", "note")
			fixture.file(test, "meetings/m", "meeting")
			fixture.file(test, "docs/src", "note")

			link := func(target, text string) index.EdgeRow {
				return index.EdgeRow{Type: "references", SourceID: "docs/src", TargetID: target, SourcePath: "docs/src.md", Kind: "direct", Link: text}
			}

			if upsertErr := fixture.edges.UpsertAll("docs/src", "docs/src.md", []index.EdgeRow{
				link("notes/rfc", "notes/rfc#^gone"),
				link("notes/rfc", "notes/rfc#Renamed"),
				link("meetings/m", "meetings/m#^gone"),
				link("meetings/m", "meetings/m#Renamed"),
			}); upsertErr != nil {
				test.Fatalf("UpsertAll: %v", upsertErr)
			}

			report := run(test, fixture.config(loadSubUnitKindsManifest(test, testCase.subUnits)))

			for _, kind := range []string{doctor.IssueMissingHeading, doctor.IssueMissingBlock} {
				issues := issuesOfKind(report, kind)

				if len(issues) != 1 {
					test.Errorf("%s issues = %+v, want 1, for %s", kind, issues, testCase.reported)

					continue
				}

				if !strings.HasSuffix(issues[0].Message, " in "+testCase.reported) {
					test.Errorf("%s message = %q, want it about %s, not %s", kind, issues[0].Message, testCase.reported, testCase.silent)
				}
			}
		})
	}
}

// TestRun_TypeOverrideExcludedRowsAreDirty reports stored rows of a kind
// their file's type leaves out, one issue per place the setting comes from:
// the workspace for types that set none, the [node-types] table for a type
// that sets its own (#867).
func TestRun_TypeOverrideExcludedRowsAreDirty(test *testing.T) {
	fixture := newDoctorFixture(test)

	fixture.file(test, "notes/n", "note")
	fixture.file(test, "meetings/m", "meeting")

	var rows []index.NodeRow

	for fileID, kinds := range map[string][]string{
		"notes/n":    {"paragraph", "table-cell"},
		"meetings/m": {"paragraph", "list-item", "list-item"},
	} {
		for position, kind := range kinds {
			rows = append(rows, index.NodeRow{
				ID:             fileID + "#U" + string(rune('1'+position)),
				Type:           kind,
				Path:           fileID + ".md",
				PropertiesJSON: "{}",
				LastChecksum:   "x",
				ParentID:       sql.NullString{String: fileID, Valid: true},
				Ordinal:        sql.NullInt64{Int64: int64(position), Valid: true},
			})
		}
	}

	if bulkErr := fixture.nodes.BulkUpsert(rows, "markdown"); bulkErr != nil {
		test.Fatalf("bulk upsert sub-units: %v", bulkErr)
	}

	report := run(test, fixture.config(loadSubUnitKindsManifest(test, "sub-units = [\"paragraph\"]\n\n[node-types.meeting]\nsub-units = [\"paragraph\", \"table-cell\"]")))
	dirty := issuesOfKind(report, doctor.IssueSubUnitsDisabledDirty)

	if len(dirty) != 2 {
		test.Fatalf("sub-units-disabled-dirty issues = %+v, want 2", dirty)
	}

	checks := []struct {
		want, notWant []string
	}{
		{want: []string{"[workspace] sub-units", "(table-cell 1)"}, notWant: []string{"list-item", "paragraph"}},
		{want: []string{"[node-types.meeting] sub-units", "meeting files", "(list-item 2)", "`tusk reindex`"}, notWant: []string{"table-cell", "paragraph"}},
	}

	for position, check := range checks {
		for _, want := range check.want {
			if !strings.Contains(dirty[position].Message, want) {
				test.Errorf("issue %d message = %q, want it to contain %q", position, dirty[position].Message, want)
			}
		}

		for _, notWant := range check.notWant {
			if strings.Contains(dirty[position].Message, notWant) {
				test.Errorf("issue %d message = %q, want no %q", position, dirty[position].Message, notWant)
			}
		}
	}

	clean := run(test, fixture.config(loadSubUnitKindsManifest(test, "sub-units = [\"paragraph\", \"table-cell\"]\n\n[node-types.meeting]\nsub-units = [\"paragraph\", \"list-item\"]")))

	if got := issuesOfKind(clean, doctor.IssueSubUnitsDisabledDirty); len(got) != 0 {
		test.Errorf("sub-units-disabled-dirty with every stored kind indexed for its type = %+v, want none", got)
	}
}
