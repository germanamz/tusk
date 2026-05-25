package render

import (
	"bytes"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/query"
)

func TestCompactNodeRows_Fixture(test *testing.T) {
	rows := []CompactRow{
		{
			ID:    "notes/auth-rfc",
			Type:  "note",
			Title: "Auth RFC",
		},
		{
			ID:    "tickets/fix-login",
			Type:  "ticket",
			Title: "Fix login bug",
			Properties: map[string]any{
				"status":   "active",
				"priority": 3,
			},
		},
	}

	var buf bytes.Buffer

	if err := CompactNodeRows(&buf, rows, CompactOpts{}); err != nil {
		test.Fatalf("render: %v", err)
	}

	want := strings.Join([]string{
		"notes/auth-rfc     note    Auth RFC",
		"tickets/fix-login  ticket  Fix login bug  priority=3 status=active",
		"",
	}, "\n")

	if got := buf.String(); got != want {
		test.Errorf("compact output mismatch.\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestCompactNodeRows_WithBodyAndEdges(test *testing.T) {
	rows := []CompactRow{
		{
			ID:    "notes/auth-rfc",
			Type:  "note",
			Title: "Auth RFC",
			Body:  "body line 1\nbody line 2",
			Edges: []query.EdgeRef{
				{Type: "references", Direction: "out", TargetID: "notes/oauth-rfc", TargetTitle: "OAuth RFC"},
			},
		},
		{
			ID:    "tickets/fix-login",
			Type:  "ticket",
			Title: "Fix login bug",
		},
	}

	var buf bytes.Buffer

	if err := CompactNodeRows(&buf, rows, CompactOpts{}); err != nil {
		test.Fatalf("render: %v", err)
	}

	want := strings.Join([]string{
		"notes/auth-rfc     note    Auth RFC",
		"  body line 1",
		"  body line 2",
		"  → references notes/oauth-rfc OAuth RFC",
		"tickets/fix-login  ticket  Fix login bug",
		"",
	}, "\n")

	if got := buf.String(); got != want {
		test.Errorf("compact output mismatch.\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestCompactNodeRows_FieldsProjection(test *testing.T) {
	rows := []CompactRow{
		{
			ID:    "x",
			Type:  "note",
			Title: "X",
			Body:  "hidden",
		},
	}

	var buf bytes.Buffer

	if err := CompactNodeRows(&buf, rows, CompactOpts{Fields: []string{"id", "title"}}); err != nil {
		test.Fatalf("render: %v", err)
	}

	want := "x  X\n"

	if got := buf.String(); got != want {
		test.Errorf("compact projection mismatch.\n--- want ---\n%q\n--- got ---\n%q", want, got)
	}
}

func TestCompactNodeRows_Deterministic(test *testing.T) {
	rows := []CompactRow{
		{ID: "a", Type: "n", Title: "A", Properties: map[string]any{"x": 1, "y": 2, "z": 3}},
	}

	var bufA, bufB bytes.Buffer

	if err := CompactNodeRows(&bufA, rows, CompactOpts{}); err != nil {
		test.Fatal(err)
	}

	if err := CompactNodeRows(&bufB, rows, CompactOpts{}); err != nil {
		test.Fatal(err)
	}

	if !bytes.Equal(bufA.Bytes(), bufB.Bytes()) {
		test.Errorf("non-deterministic output:\nA=%q\nB=%q", bufA.String(), bufB.String())
	}
}

func TestCompactNodeRows_MatchedUnitsSemantic(test *testing.T) {
	rows := []CompactRow{
		{
			ID:       "notes/auth-rfc",
			Type:     "note",
			Title:    "Auth RFC",
			Score:    0.86,
			HasScore: true,
			MatchedUnits: []query.MatchedUnit{
				{ID: "notes/auth-rfc#a1b2c3", Type: "section", HeadingLevel: 2, Ordinal: 4, Score: 0.86, Snippet: "Decision OAuth", HasScore: true},
				{ID: "notes/auth-rfc#b4d5e6", Type: "section", HeadingLevel: 3, Ordinal: 7, Score: 0.74, Snippet: "PKCE implementation", HasScore: true},
				{ID: "notes/auth-rfc#d4e5f6", Type: "paragraph", Ordinal: 12, Score: 0.78, Snippet: "Users with SSO", HasScore: true},
			},
		},
	}

	var buf bytes.Buffer

	if err := CompactNodeRows(&buf, rows, CompactOpts{}); err != nil {
		test.Fatalf("render: %v", err)
	}

	got := buf.String()

	// Spot-check the structural shape rather than the entire string so
	// minor padding tweaks don't churn the test. The matched-unit lines
	// must use the arrow indentation, decorate sections with H<level>,
	// and end with a score column for semantic hits.
	mustContain := []string{
		"notes/auth-rfc",
		"  → #a1b2c3",
		"section H2",
		"section H3",
		"paragraph",
		"0.8600",
		"0.7400",
		"0.7800",
		"\"Decision OAuth\"",
	}

	for _, fragment := range mustContain {
		if !strings.Contains(got, fragment) {
			test.Errorf("output missing %q\n%s", fragment, got)
		}
	}
}

func TestCompactNodeRows_MatchedUnitsTruncationTail(test *testing.T) {
	units := make([]query.MatchedUnit, 0, 25)

	for idx := 0; idx < 25; idx++ {
		units = append(units, query.MatchedUnit{
			ID:   "notes/big#u" + strings.Repeat("x", 1),
			Type: "paragraph", Ordinal: idx,
		})
	}

	rows := []CompactRow{{ID: "notes/big", Type: "note", Title: "Big", MatchedUnits: units}}

	var buf bytes.Buffer

	if err := CompactNodeRows(&buf, rows, CompactOpts{}); err != nil {
		test.Fatalf("render: %v", err)
	}

	got := buf.String()

	if !strings.Contains(got, "(5 more)") {
		test.Errorf("missing (5 more) tail in %q", got)
	}
}

func TestCompactNodeRows_MatchedUnitsStructuralOmitsScore(test *testing.T) {
	rows := []CompactRow{
		{
			ID:    "notes/x",
			Type:  "note",
			Title: "X",
			MatchedUnits: []query.MatchedUnit{
				{ID: "notes/x#a", Type: "paragraph", Ordinal: 0},
				{ID: "notes/x#b", Type: "paragraph", Ordinal: 1},
			},
		},
	}

	var buf bytes.Buffer

	if err := CompactNodeRows(&buf, rows, CompactOpts{}); err != nil {
		test.Fatalf("render: %v", err)
	}

	got := buf.String()

	if strings.Contains(got, "0.0000") {
		test.Errorf("structural matched_units must omit score column: %q", got)
	}
}

func TestCompactNodeRows_ExplainTail(test *testing.T) {
	rows := []CompactRow{
		{
			ID:    "notes/x",
			Type:  "note",
			Title: "X",

			Score:    0.84,
			HasScore: true,

			CosineScore: 0.74,
			GraphScore:  0.66,
			FinalScore:  0.84,
			Distance:    1,
			HasExplain:  true,
		},
	}

	var buf bytes.Buffer

	if err := CompactNodeRows(&buf, rows, CompactOpts{}); err != nil {
		test.Fatalf("render: %v", err)
	}

	got := buf.String()

	mustContain := []string{
		"score=0.8400",
		"cosine=0.7400",
		"graph=0.6600",
		"final=0.8400",
		"dist=1",
	}

	for _, fragment := range mustContain {
		if !strings.Contains(got, fragment) {
			test.Errorf("explain tail missing %q in:\n%s", fragment, got)
		}
	}
}

func TestCompactNodeRows_ExplainOmittedWithoutFlag(test *testing.T) {
	rows := []CompactRow{
		{ID: "notes/x", Type: "note", Title: "X", Score: 0.84, HasScore: true},
	}

	var buf bytes.Buffer

	if err := CompactNodeRows(&buf, rows, CompactOpts{}); err != nil {
		test.Fatalf("render: %v", err)
	}

	if strings.Contains(buf.String(), "cosine=") {
		test.Errorf("non-explain row leaked cosine token: %q", buf.String())
	}
}

func TestCompactEdgeRows_Fixture(test *testing.T) {
	rows := []EdgeListEntry{
		{Type: "blocks", SourceID: "tickets/a", TargetID: "tickets/b", SourcePath: "tickets/a.md"},
		{Type: "links", SourceID: "notes/x", TargetID: "notes/y", SourcePath: "notes/x.md"},
	}

	var buf bytes.Buffer

	if err := CompactEdgeRows(&buf, rows); err != nil {
		test.Fatalf("render: %v", err)
	}

	want := strings.Join([]string{
		"blocks  tickets/a  tickets/b  tickets/a.md",
		"links   notes/x    notes/y    notes/x.md",
		"",
	}, "\n")

	if got := buf.String(); got != want {
		test.Errorf("compact edge output mismatch.\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}
