package htmlunit

import (
	"testing"

	"github.com/germanamz/tusk/internal/subunit"
)

// TestParse_EmptyListItemEmitsNoUnit guards #682 item 5 for the HTML walker: a
// bare <li></li> (or whitespace-only) is authoring scaffolding with no content,
// so it must not emit an empty-payload list-item node — the twin of
// subunit.walkListItem's skip.
func TestParse_EmptyListItemEmitsNoUnit(test *testing.T) {
	units, err := Parse([]byte(`<ul><li>first</li><li></li><li>  </li><li>last</li></ul>`))
	if err != nil {
		test.Fatalf("parse: %v", err)
	}

	var items int

	for _, unit := range units {
		if unit.Kind != subunit.KindListItem {
			continue
		}

		items++

		if unit.Text == "" || unit.EmbedPayload == "" {
			test.Errorf("emitted a list item with empty content: %+v", unit)
		}
	}

	if items != 2 {
		test.Fatalf("want 2 list-item units (empties skipped), got %d", items)
	}
}

// TestParse_EmptyListItemStillRecursesNestedList guards #682 item 5: an empty
// <li> that only wraps a nested list emits no unit for itself, but its nested
// items are still emitted as peers.
func TestParse_EmptyListItemStillRecursesNestedList(test *testing.T) {
	units, err := Parse([]byte(`<ul><li><ul><li>nested item</li></ul></li></ul>`))
	if err != nil {
		test.Fatalf("parse: %v", err)
	}

	var texts []string

	for _, unit := range units {
		if unit.Kind == subunit.KindListItem {
			texts = append(texts, unit.Text)
		}
	}

	if len(texts) != 1 || texts[0] != "nested item" {
		test.Fatalf("want only the nested item emitted, got %v", texts)
	}
}

// TestParse_EmptyCheckboxListItemStaysNode guards #682 item 5: a text-less
// checkbox <li> still carries a queryable checkbox property, so it stays a node
// even though its payload is empty.
func TestParse_EmptyCheckboxListItemStaysNode(test *testing.T) {
	units, err := Parse([]byte(`<ul><li><input type="checkbox" checked></li></ul>`))
	if err != nil {
		test.Fatalf("parse: %v", err)
	}

	var item *subunit.Unit

	for idx := range units {
		if units[idx].Kind == subunit.KindListItem {
			item = &units[idx]

			break
		}
	}

	if item == nil {
		test.Fatalf("empty checkbox item should still emit a node, got %+v", units)
	}

	if _, ok := item.Properties["checkbox"]; !ok {
		test.Errorf("checkbox property missing: %+v", item.Properties)
	}
}
