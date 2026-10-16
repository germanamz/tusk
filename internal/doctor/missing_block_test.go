package doctor_test

import (
	"slices"
	"testing"

	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/index"
)

// A block reference whose ^id names no unit of its note lands on the note, or
// waits at its fragment beside a twin; both are missing-block warnings, apart
// from missing-heading. A resolved block is not reported, and a block
// reference into a missing note is still a dangling edge (#819).
func TestRun_MissingBlockWarnsForABlockReferenceThatMissed(test *testing.T) {
	fixture := newDoctorFixture(test)

	fixture.file(test, "notes/rfc", "note")
	fixture.file(test, "docs/src", "note")
	fixture.subUnits(test, "notes/rfc", "notes/rfc#P1")

	link := func(target, text string) index.EdgeRow {
		return index.EdgeRow{Type: "references", SourceID: "docs/src", TargetID: target, SourcePath: "docs/src.md", Kind: "direct", Link: text}
	}

	if upsertErr := fixture.edges.UpsertAll("docs/src", "docs/src.md", []index.EdgeRow{
		link("notes/rfc", "rfc#^gone"),
		link("notes/rfc#^parked", "rfc#^parked"),
		link("notes/rfc#P1", "rfc#^quote-1"),
		link("notes/rfc#Renamed", "rfc#Renamed"),
		link("notes/missing#^x", "notes/missing#^x"),
	}); upsertErr != nil {
		test.Fatalf("UpsertAll: %v", upsertErr)
	}

	report := run(test, fixture.config(nil))

	var messages []string

	for _, issue := range issuesOfKind(report, doctor.IssueMissingBlock) {
		if issue.NodeID != "docs/src" || issue.Severity != doctor.SeverityWarning {
			test.Errorf("issue = %+v, want a warning at docs/src", issue)
		}

		messages = append(messages, issue.Message)
	}

	want := []string{
		`edge "references" -> [[rfc#^gone]] names no block in notes/rfc`,
		`edge "references" -> [[rfc#^parked]] names no block in notes/rfc`,
	}

	if !slices.Equal(messages, want) {
		test.Errorf("missing-block messages =\n%q\nwant\n%q", messages, want)
	}

	if headings := issuesOfKind(report, doctor.IssueMissingHeading); len(headings) != 1 {
		test.Errorf("missing-heading issues = %+v, want only [[rfc#Renamed]]", headings)
	}

	dangling := issuesOfKind(report, doctor.IssueDanglingEdge)

	if len(dangling) != 1 || dangling[0].Message != `edge "references" -> "notes/missing#^x" (target missing)` {
		test.Errorf("dangling-edge issues = %+v, want only the link into the missing note", dangling)
	}
}
