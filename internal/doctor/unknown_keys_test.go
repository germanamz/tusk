package doctor_test

import (
	"reflect"
	"testing"

	"github.com/germanamz/tusk/internal/doctor"
)

const unknownKeysManifest = `[workspace]
name = "brain"

[edge-types.depends-on]
from        = ["*"]
to          = ["*"]
cardinality = "many-to-many"
acylic      = true

[rule.typo]
filter  = "type=note"
severty = "warning"

[mystery.lint]
command = "python3 lint.py"
`

// A typo'd key or a table tusk doesn't know loads silently, so doctor warns
// about it (#852). The rule's own typo stays a rule-invalid error.
func TestRun_ReportsUnknownManifestKeys(test *testing.T) {
	loaded := loadGraphExpansionManifest(test, unknownKeysManifest)

	report, runErr := doctor.Run(doctor.Config{Manifest: loaded})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	want := []doctor.Issue{
		{Kind: doctor.IssueManifestUnknownKey, Severity: doctor.SeverityWarning, NodeID: "edge-types.depends-on.acylic", Message: "tusk does not read this key; check its spelling, or upgrade tusk if a newer version added it"},
		{Kind: doctor.IssueManifestUnknownKey, Severity: doctor.SeverityWarning, NodeID: "mystery", Message: "tusk does not read this table; check its spelling, or upgrade tusk if a newer version added it"},
	}

	if got := issuesOfKind(report, doctor.IssueManifestUnknownKey); !reflect.DeepEqual(got, want) {
		test.Fatalf("unknown-key issues =\n%+v\nwant\n%+v", got, want)
	}
}

// Reload lists the manifest problems that don't fail a load, worded once for
// the CLI and MCP.
func TestManifestWarnings(test *testing.T) {
	loaded := loadGraphExpansionManifest(test, unknownKeysManifest)

	want := []string{
		`rule "typo": unknown key "severty" (a rule takes description, severity, filter)`,
		"tusk.toml: edge-types.depends-on.acylic: tusk does not read this key; check its spelling, or upgrade tusk if a newer version added it",
		"tusk.toml: mystery: tusk does not read this table; check its spelling, or upgrade tusk if a newer version added it",
	}

	if got := doctor.ManifestWarnings(loaded); !reflect.DeepEqual(got, want) {
		test.Fatalf("ManifestWarnings =\n%q\nwant\n%q", got, want)
	}

	if got := doctor.ManifestWarnings(nil); len(got) != 0 {
		test.Fatalf("ManifestWarnings(nil) = %q, want none", got)
	}
}
