package doctor_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/index"
)

// A [[name]] two files answer to is reported with both files as
// ambiguous-wikilink; a name no file has stays a dangling edge (#791).
func TestRun_AmbiguousWikilinkNamesTheCandidates(test *testing.T) {
	fixture := newDoctorFixture(test)

	fixture.file(test, "a/dup", "note")
	fixture.file(test, "b/dup", "note")
	fixture.file(test, "docs/src", "note")

	if upsertErr := fixture.edges.UpsertAll("docs/src", "docs/src.md", []index.EdgeRow{
		{Type: "references", SourceID: "docs/src", TargetID: "dup#S1", SourcePath: "docs/src.md", Kind: "direct", Link: "dup#S1"},
		{Type: "references", SourceID: "docs/src", TargetID: "gone", SourcePath: "docs/src.md", Kind: "direct", Link: "gone"},
	}); upsertErr != nil {
		test.Fatalf("UpsertAll: %v", upsertErr)
	}

	report := run(test, fixture.config(nil))
	ambiguous := issuesOfKind(report, doctor.IssueAmbiguousWikilink)

	if len(ambiguous) != 1 {
		test.Fatalf("ambiguous-wikilink issues = %+v, want 1", ambiguous)
	}

	want := `edge "references" -> [[dup#S1]] names 2 files: a/dup, b/dup; link one by its path`

	if ambiguous[0].Message != want || ambiguous[0].NodeID != "docs/src" || ambiguous[0].Severity != doctor.SeverityError {
		test.Errorf("issue = %+v, want an error at docs/src with message %q", ambiguous[0], want)
	}

	dangling := issuesOfKind(report, doctor.IssueDanglingEdge)

	if len(dangling) != 1 || dangling[0].Message != `edge "references" -> "gone" (target missing)` {
		test.Errorf("dangling-edge issues = %+v, want the one for gone", dangling)
	}
}
