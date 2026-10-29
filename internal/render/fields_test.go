package render

import (
	"bytes"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/query"
)

// TestCompactNodeRows_NamedProperties pins #851 on the compact side:
// properties.<name> keeps only that property on the record line.
func TestCompactNodeRows_NamedProperties(test *testing.T) {
	rows := []CompactRow{
		{ID: "a", Type: "service", Properties: map[string]any{"owner": "me", "domain": "billing"}},
		{ID: "bb", Type: "service", Properties: map[string]any{"domain": "checkout"}},
	}

	var buf bytes.Buffer

	if err := CompactNodeRows(&buf, rows, CompactOpts{Fields: []string{"id", "properties.owner"}}); err != nil {
		test.Fatalf("render: %v", err)
	}

	if got, want := buf.String(), "a   owner=me\nbb\n"; got != want {
		test.Errorf("compact = %q, want %q", got, want)
	}
}

func TestCompactNodeRows_UnitsFieldShowsUnits(test *testing.T) {
	rows := []CompactRow{{
		ID:           "notes/a",
		Type:         "note",
		MatchedUnits: []query.MatchedUnit{{ID: "notes/a#S1", Type: "section", Heading: "Intro"}},
	}}

	var buf bytes.Buffer

	if err := CompactNodeRows(&buf, rows, CompactOpts{Fields: []string{"id", "units"}}); err != nil {
		test.Fatalf("render: %v", err)
	}

	if !strings.Contains(buf.String(), `#S1  section "Intro"`) {
		test.Errorf("fields=units dropped the units:\n%s", buf.String())
	}
}

func TestCompactNodeRows_RejectsUnknownField(test *testing.T) {
	rows := []CompactRow{{ID: "a"}}

	if err := CompactNodeRows(&bytes.Buffer{}, rows, CompactOpts{Fields: []string{"titel"}}); err == nil {
		test.Error("render with fields=titel = nil error, want unknown field")
	}
}

func TestCounts_Total(test *testing.T) {
	var buf bytes.Buffer

	if err := Counts(&buf, &query.CountResult{Total: 3}); err != nil {
		test.Fatalf("Counts: %v", err)
	}

	if got := buf.String(); got != "3\n" {
		test.Errorf("Counts = %q, want %q", got, "3\n")
	}
}

func TestCounts_Groups(test *testing.T) {
	var buf bytes.Buffer

	result := &query.CountResult{
		Total:   4,
		GroupBy: "domain",
		Groups: []query.CountGroup{
			{Value: "billing", Count: 2},
			{Value: float64(7), Count: 1},
			{Value: nil, Count: 1},
		},
	}

	if err := Counts(&buf, result); err != nil {
		test.Fatalf("Counts: %v", err)
	}

	want := "DOMAIN   COUNT\nbilling  2\n7        1\n(none)   1\n"

	if got := buf.String(); got != want {
		test.Errorf("Counts =\n%s\nwant\n%s", got, want)
	}
}
