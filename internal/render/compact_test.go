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
