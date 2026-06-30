package node

import (
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/manifest"
)

func ticketDecls() map[string]manifest.NodeType {
	return map[string]manifest.NodeType{
		"ticket": {Properties: []manifest.PropertyDecl{
			{Name: "due", Type: "date"},
			{Name: "remind_at", Type: "datetime"},
			{Name: "milestones", Type: "list-of", ItemType: "date"},
		}},
	}
}

func TestCanonicalizeDates_DeclaredDateToDateOnly(test *testing.T) {
	parsed := &Node{Type: "ticket", Properties: map[string]any{
		"due": time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC),
	}}

	if !CanonicalizeDates(parsed, ticketDecls()) {
		test.Fatalf("expected changed=true")
	}

	if got := parsed.Properties["due"]; got != "2026-06-11" {
		test.Errorf("due = %#v, want \"2026-06-11\"", got)
	}
}

func TestCanonicalizeDates_DeclaredDatetimeToRFC3339(test *testing.T) {
	parsed := &Node{Type: "ticket", Properties: map[string]any{
		"remind_at": time.Date(2026, 6, 11, 9, 30, 0, 0, time.UTC),
	}}

	if !CanonicalizeDates(parsed, ticketDecls()) {
		test.Fatalf("expected changed=true")
	}

	if got := parsed.Properties["remind_at"]; got != "2026-06-11T09:30:00Z" {
		test.Errorf("remind_at = %#v, want RFC3339", got)
	}
}

func TestCanonicalizeDates_UndeclaredTimeFallsBackToRFC3339(test *testing.T) {
	parsed := &Node{Type: "ticket", Properties: map[string]any{
		"someday": time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC),
	}}

	if !CanonicalizeDates(parsed, ticketDecls()) {
		test.Fatalf("expected changed=true")
	}

	// Undeclared property: lossless RFC3339, never a bare time.Time that
	// renderMarkdown would reject.
	if got := parsed.Properties["someday"]; got != "2026-06-11T00:00:00Z" {
		test.Errorf("someday = %#v, want RFC3339 fallback", got)
	}
}

func TestCanonicalizeDates_ListOfDates(test *testing.T) {
	parsed := &Node{Type: "ticket", Properties: map[string]any{
		"milestones": []any{
			time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC),
			"2026-07-01", // already a string — left alone
		},
	}}

	if !CanonicalizeDates(parsed, ticketDecls()) {
		test.Fatalf("expected changed=true")
	}

	list := parsed.Properties["milestones"].([]any)

	if list[0] != "2026-06-11" {
		test.Errorf("milestones[0] = %#v, want \"2026-06-11\"", list[0])
	}

	if list[1] != "2026-07-01" {
		test.Errorf("milestones[1] = %#v, want unchanged string", list[1])
	}
}

func TestCanonicalizeDates_StringDateUnchanged(test *testing.T) {
	parsed := &Node{Type: "ticket", Properties: map[string]any{
		"due":      "2026-06-11",
		"priority": 5,
		"open":     true,
	}}

	if CanonicalizeDates(parsed, ticketDecls()) {
		test.Errorf("expected changed=false for an already-string date")
	}

	if parsed.Properties["due"] != "2026-06-11" {
		test.Errorf("due mutated unexpectedly: %#v", parsed.Properties["due"])
	}
}

func TestCanonicalizeDates_NilAndEmpty(test *testing.T) {
	if CanonicalizeDates(nil, ticketDecls()) {
		test.Errorf("nil node should report no change")
	}

	if CanonicalizeDates(&Node{Type: "ticket", Properties: map[string]any{}}, ticketDecls()) {
		test.Errorf("empty properties should report no change")
	}
}
