package node

import (
	"reflect"
	"strings"
	"testing"
)

// renderRoundTrip renders properties+body and reparses, returning the reparsed
// node. It fails the test if render or reparse errors.
func renderRoundTrip(test *testing.T, properties map[string]any, body []byte) *Node {
	test.Helper()

	rendered, renderErr := renderMarkdown(properties, body)

	if renderErr != nil {
		test.Fatalf("renderMarkdown: %v", renderErr)
	}

	reparsed, parseErr := ParseFile("x/n.md", rendered)

	if parseErr != nil {
		test.Fatalf("reparse of rendered bytes failed: %v\nrendered:\n%s", parseErr, rendered)
	}

	return reparsed
}

// D3: a non-string title value must not be silently dropped on re-render.
func TestRender_NonStringTitlePreserved(test *testing.T) {
	rendered, err := renderMarkdown(map[string]any{"type": "note", "title": 2026}, []byte("body\n"))

	if err != nil {
		test.Fatalf("renderMarkdown: %v", err)
	}

	if !strings.Contains(string(rendered), "title: 2026") {
		test.Fatalf("non-string title dropped; rendered:\n%s", rendered)
	}
}

// D4: an empty list must round-trip as an empty list, not collapse to null and
// then make the node unrenderable.
func TestRender_EmptyListRoundTrips(test *testing.T) {
	first := renderRoundTrip(test, map[string]any{"type": "note", "tags": []any{}}, []byte("body\n"))

	tags, ok := first.Properties["tags"].([]any)

	if !ok || tags == nil || len(tags) != 0 {
		test.Fatalf("empty list did not round-trip: got %#v", first.Properties["tags"])
	}

	// Second render must still succeed (regression: null -> unsupported type).
	if _, err := renderMarkdown(first.Properties, first.Body); err != nil {
		test.Fatalf("second render after empty-list round-trip failed: %v", err)
	}
}

// D5: a multi-line string value must preserve its interior newlines rather than
// folding them to spaces. (A trailing newline immediately before the closing
// `---` is consumed by splitFrontmatter — a separate, pre-existing quirk — so
// the value under test carries no trailing newline.)
func TestRender_MultilineStringPreserved(test *testing.T) {
	reparsed := renderRoundTrip(test, map[string]any{"type": "note", "summary": "first line\nsecond line"}, []byte("body\n"))

	if got := reparsed.Properties["summary"]; got != "first line\nsecond line" {
		test.Fatalf("multiline string not preserved: got %q", got)
	}
}

// H1: nested map / null / list-of-non-strings must serialize and round-trip
// instead of hard-failing the render.
func TestRender_ComplexShapesRoundTrip(test *testing.T) {
	props := map[string]any{
		"type":     "note",
		"meta":     map[string]any{"owner": "alice", "team": "infra"},
		"reviewed": nil,
		"weights":  []any{1, 2, 3},
	}

	reparsed := renderRoundTrip(test, props, []byte("body\n"))

	if got, want := reparsed.Properties["meta"], (map[string]any{"owner": "alice", "team": "infra"}); !reflect.DeepEqual(got, want) {
		test.Fatalf("nested map not preserved: got %#v", got)
	}

	if got, present := reparsed.Properties["reviewed"]; !present || got != nil {
		test.Fatalf("null value not preserved: got %#v present=%v", got, present)
	}

	if got, want := reparsed.Properties["weights"], []any{1, 2, 3}; !reflect.DeepEqual(got, want) {
		test.Fatalf("list-of-int not preserved: got %#v", got)
	}
}

// C1: repeated renders of the same properties must be byte-identical (keys are
// emitted in a deterministic, sorted order after type/title).
func TestRender_DeterministicKeyOrder(test *testing.T) {
	props := map[string]any{
		"type": "ticket", "title": "t", "status": "open",
		"priority": "high", "owner": "alice", "due": "2026-06-11",
	}

	var first string

	for iteration := range 12 {
		out, err := renderMarkdown(props, []byte("b\n"))

		if err != nil {
			test.Fatalf("render: %v", err)
		}

		if iteration == 0 {
			first = string(out)
			continue
		}

		if string(out) != first {
			test.Fatalf("render not deterministic:\n--- first ---\n%s\n--- iter %d ---\n%s", first, iteration, out)
		}
	}

	// type first, title second, then remaining keys sorted alphabetically.
	if !strings.HasPrefix(first, "---\ntype: ticket\ntitle: t\n") {
		test.Fatalf("preamble order wrong:\n%s", first)
	}

	if idxDue, idxOwner := strings.Index(first, "due:"), strings.Index(first, "owner:"); idxDue > idxOwner {
		test.Fatalf("keys not sorted (due should precede owner):\n%s", first)
	}
}

// #662 guard: a date-shaped string must stay quoted so it does not reparse as a
// timestamp.
func TestRender_DateStringStaysQuoted(test *testing.T) {
	rendered, err := renderMarkdown(map[string]any{"type": "ticket", "due": "2026-06-11"}, []byte("b\n"))

	if err != nil {
		test.Fatalf("render: %v", err)
	}

	if !strings.Contains(string(rendered), `due: "2026-06-11"`) {
		test.Fatalf("date string not quoted; rendered:\n%s", rendered)
	}
}
