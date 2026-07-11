package subunit_test

import (
	"context"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/subunit"
)

// TestSync_PropertyOnlyEditConverges guards #682 item 1: toggling a task
// checkbox is a property-only edit that leaves the embed payload
// byte-identical, so the leaf content hash does not move. The sync must still
// turn the row over so properties_json reflects the new checkbox state instead
// of serving the stale value forever.
func TestSync_PropertyOnlyEditConverges(test *testing.T) {
	store := openSyncTestIndex(test)
	loaded := referencesManifest(test)
	sync, nodes, _, _ := newSync(store, loaded)

	parent := seedFileRow(test, nodes, "notes/tasks", "notes/tasks.md")

	before, parseErr := subunit.Parse([]byte("- [ ] write spec\n"))
	if parseErr != nil {
		test.Fatalf("Parse before: %v", parseErr)
	}

	if _, err := sync.ApplyFile(context.Background(), parent, before); err != nil {
		test.Fatalf("ApplyFile before: %v", err)
	}

	after, parseErr := subunit.Parse([]byte("- [x] write spec\n"))
	if parseErr != nil {
		test.Fatalf("Parse after: %v", parseErr)
	}

	if _, err := sync.ApplyFile(context.Background(), parent, after); err != nil {
		test.Fatalf("ApplyFile after: %v", err)
	}

	rows, listErr := nodes.ListSubUnitsForFile(parent.ID)
	if listErr != nil {
		test.Fatalf("ListSubUnitsForFile: %v", listErr)
	}

	var found bool

	for _, row := range rows {
		if row.Type != string(subunit.KindListItem) {
			continue
		}

		found = true

		if !strings.Contains(row.PropertiesJSON, `"checkbox":true`) {
			test.Errorf("checkbox did not converge after property-only edit; properties_json = %s", row.PropertiesJSON)
		}
	}

	if !found {
		test.Fatalf("no list-item row persisted\nrows = %+v", rows)
	}
}

// TestSync_CodeFenceLangOnlyEditConverges guards #682 item 1: changing only a
// fenced code block's language tag (```go -> ```python) leaves the code body —
// and therefore the embed payload and content hash — byte-identical. The sync
// must still turn the row over so properties_json reflects the new lang.
func TestSync_CodeFenceLangOnlyEditConverges(test *testing.T) {
	store := openSyncTestIndex(test)
	loaded := referencesManifest(test)
	sync, nodes, _, _ := newSync(store, loaded)

	parent := seedFileRow(test, nodes, "notes/code", "notes/code.md")

	before, parseErr := subunit.Parse([]byte("```go\nfmt.Println()\n```\n"))
	if parseErr != nil {
		test.Fatalf("Parse before: %v", parseErr)
	}

	if _, err := sync.ApplyFile(context.Background(), parent, before); err != nil {
		test.Fatalf("ApplyFile before: %v", err)
	}

	after, parseErr := subunit.Parse([]byte("```python\nfmt.Println()\n```\n"))
	if parseErr != nil {
		test.Fatalf("Parse after: %v", parseErr)
	}

	if _, err := sync.ApplyFile(context.Background(), parent, after); err != nil {
		test.Fatalf("ApplyFile after: %v", err)
	}

	rows, listErr := nodes.ListSubUnitsForFile(parent.ID)
	if listErr != nil {
		test.Fatalf("ListSubUnitsForFile: %v", listErr)
	}

	var found bool

	for _, row := range rows {
		if row.Type != string(subunit.KindCodeBlock) {
			continue
		}

		found = true

		if !strings.Contains(row.PropertiesJSON, `"lang":"python"`) {
			test.Errorf("lang did not converge after lang-only edit; properties_json = %s", row.PropertiesJSON)
		}
	}

	if !found {
		test.Fatalf("no code-block row persisted\nrows = %+v", rows)
	}
}

// TestSync_EmptyPayloadLeafNotEnqueued guards #682 item 5: a leaf with an empty
// embed payload (e.g. a text-less "- [ ]" task item) can never embed, so the
// sync must not enqueue it — otherwise the drain re-logs an "embed skip empty
// sub-unit payload" WARN on every pass. A real sibling item is still enqueued.
func TestSync_EmptyPayloadLeafNotEnqueued(test *testing.T) {
	store := openSyncTestIndex(test)
	loaded := referencesManifest(test)
	sync, nodes, _, queue := newSync(store, loaded)

	parent := seedFileRow(test, nodes, "notes/checklist", "notes/checklist.md")

	units, parseErr := subunit.Parse([]byte("# Checklist\n\n- real item\n- [ ] \n"))
	if parseErr != nil {
		test.Fatalf("Parse: %v", parseErr)
	}

	if _, err := sync.ApplyFile(context.Background(), parent, units); err != nil {
		test.Fatalf("ApplyFile: %v", err)
	}

	queued, listErr := queue.ListNodeIDs()
	if listErr != nil {
		test.Fatalf("ListNodeIDs: %v", listErr)
	}

	enqueued := map[string]bool{}
	for _, id := range queued {
		enqueued[id] = true
	}

	rows, rowsErr := nodes.ListSubUnitsForFile(parent.ID)
	if rowsErr != nil {
		test.Fatalf("ListSubUnitsForFile: %v", rowsErr)
	}

	var emptyID, realID string

	for _, row := range rows {
		if row.Type != string(subunit.KindListItem) {
			continue
		}

		if row.EmbedPayload.String == "" {
			emptyID = row.ID
		} else {
			realID = row.ID
		}
	}

	if realID == "" || emptyID == "" {
		test.Fatalf("want one real and one empty-payload list item; rows = %+v", rows)
	}

	if !enqueued[realID] {
		test.Errorf("real item %s should be enqueued for embedding", realID)
	}

	if enqueued[emptyID] {
		test.Errorf("empty-payload item %s must not be enqueued (can never embed)", emptyID)
	}
}
