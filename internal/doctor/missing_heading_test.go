package doctor_test

import (
	"slices"
	"testing"

	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/index"
)

// A heading link whose heading is gone lands on its note, or waits at its
// fragment when the source links the note already; both are missing-heading
// warnings, not dangling edges. A resolved section, a structural address and
// a block reference (parked here) are not reported, and a heading link into a
// missing note is still a dangling edge (#808).
func TestRun_MissingHeadingWarnsForAHeadingLinkThatMissed(test *testing.T) {
	fixture := newDoctorFixture(test)

	fixture.file(test, "notes/rfc", "note")
	fixture.file(test, "docs/src", "note")
	fixture.subUnits(test, "notes/rfc", "notes/rfc#S1", "notes/rfc#S1P1")
	fixture.subUnits(test, "docs/src", "docs/src#P1")

	link := func(source, target, text string) index.EdgeRow {
		return index.EdgeRow{Type: "references", SourceID: source, TargetID: target, SourcePath: "docs/src.md", Kind: "direct", Link: text}
	}

	if upsertErr := fixture.edges.UpsertAll("docs/src", "docs/src.md", []index.EdgeRow{
		link("docs/src", "notes/rfc", "rfc#Renamed"),
		link("docs/src", "notes/rfc#Typo", "rfc#Typo"),
		link("docs/src", "notes/rfc#S1", "rfc#Overview"),
		link("docs/src", "notes/rfc#S1P1", "notes/rfc#S1P1"),
		link("docs/src", "notes/rfc#^block", "rfc#^block"),
		link("docs/src", "notes/gone#Heading", "notes/gone#Heading"),
	}); upsertErr != nil {
		test.Fatalf("UpsertAll: %v", upsertErr)
	}

	// The paragraph links the note only through the missed heading, so its
	// row rests on the note while the file row's is parked; one finding.
	if upsertErr := fixture.edges.UpsertAll("docs/src#P1", "docs/src.md", []index.EdgeRow{
		link("docs/src#P1", "notes/rfc", "rfc#Typo"),
	}); upsertErr != nil {
		test.Fatalf("UpsertAll: %v", upsertErr)
	}

	report := run(test, fixture.config(nil))

	var messages []string

	for _, issue := range issuesOfKind(report, doctor.IssueMissingHeading) {
		if issue.NodeID != "docs/src" || issue.Severity != doctor.SeverityWarning {
			test.Errorf("issue = %+v, want a warning at docs/src", issue)
		}

		messages = append(messages, issue.Message)
	}

	want := []string{
		`edge "references" -> [[rfc#Renamed]] names no heading in notes/rfc`,
		`edge "references" -> [[rfc#Typo]] names no heading in notes/rfc`,
	}

	if !slices.Equal(messages, want) {
		test.Errorf("missing-heading messages =\n%q\nwant\n%q", messages, want)
	}

	if typo := issuesOfKind(report, doctor.IssueMissingHeading); len(typo) == 2 && !slices.Equal(typo[1].Locations, []string{"docs/src#P1"}) {
		test.Errorf("rfc#Typo locations = %q, want the paragraph carrying it", typo[1].Locations)
	}

	dangling := issuesOfKind(report, doctor.IssueDanglingEdge)

	if len(dangling) != 1 || dangling[0].Message != `edge "references" -> "notes/gone#Heading" (target missing)` {
		test.Errorf("dangling-edge issues = %+v, want only the link into the missing note", dangling)
	}
}
