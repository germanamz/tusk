package subunit_test

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/internal/linenum"
	"github.com/germanamz/tusk/internal/subunit"
)

// parseWithLines parses body as a whole file and numbers its units.
func parseWithLines(test *testing.T, body string) []subunit.Unit {
	test.Helper()

	units, parseErr := subunit.Parse([]byte(body))

	if parseErr != nil {
		test.Fatalf("Parse: %v", parseErr)
	}

	subunit.AssignLines(units, []byte(body), 0, linenum.SchemeLF)

	return units
}

func TestSync_PersistsLines(test *testing.T) {
	store := openSyncTestIndex(test)
	sync, nodes, _, _ := newSync(store, referencesManifest(test))

	parent := seedFileRow(test, nodes, "notes/lines", "notes/lines.md")

	if _, applyErr := sync.ApplyFile(context.Background(), parent, parseWithLines(test, "# Title\n\nbody\n")); applyErr != nil {
		test.Fatalf("ApplyFile: %v", applyErr)
	}

	paragraph, getErr := nodes.Get("notes/lines#S1P1")

	if getErr != nil {
		test.Fatalf("Get: %v", getErr)
	}

	if paragraph.StartLine.Int64 != 3 || paragraph.EndLine.Int64 != 3 || !paragraph.StartLine.Valid || !paragraph.EndLine.Valid {
		test.Errorf("paragraph lines = %v..%v, want 3..3", paragraph.StartLine, paragraph.EndLine)
	}
}

// TestSync_LineOnlyShiftUpdatesRowWithoutReembed: inserting blank lines above
// a passage moves its line range but not its address, ordinal, content, or
// properties. The row must still turn over (or it serves stale lines forever),
// but the vector and edges still hold, so nothing is re-enqueued.
func TestSync_LineOnlyShiftUpdatesRowWithoutReembed(test *testing.T) {
	store := openSyncTestIndex(test)
	sync, nodes, _, queue := newSync(store, referencesManifest(test))

	parent := seedFileRow(test, nodes, "notes/shift", "notes/shift.md")

	if _, applyErr := sync.ApplyFile(context.Background(), parent, parseWithLines(test, "# Title\n\nbody [[other]]\n")); applyErr != nil {
		test.Fatalf("ApplyFile before: %v", applyErr)
	}

	drainEmbedQueue(test, queue)

	result, applyErr := sync.ApplyFile(context.Background(), parent, parseWithLines(test, "\n\n\n# Title\n\nbody [[other]]\n"))

	if applyErr != nil {
		test.Fatalf("ApplyFile after: %v", applyErr)
	}

	if result.Inserted != 0 || result.Deleted != 0 {
		test.Errorf("line shift churned rows: %+v", result)
	}

	if result.Reordered != 2 {
		test.Errorf("Reordered = %d, want 2 (section and paragraph moved)", result.Reordered)
	}

	paragraph, getErr := nodes.Get("notes/shift#S1P1")

	if getErr != nil {
		test.Fatalf("Get: %v", getErr)
	}

	if paragraph.StartLine.Int64 != 6 || paragraph.EndLine.Int64 != 6 {
		test.Errorf("paragraph lines = %v..%v, want 6..6", paragraph.StartLine, paragraph.EndLine)
	}

	if depth, _ := queue.Depth(); depth != 0 {
		test.Errorf("line-only shift re-enqueued %d embeds, want 0", depth)
	}
}

// TestSync_FillsLinesOnRowsWrittenWithout: rows written before line ranges
// existed hold NULL lines; the next sync (the forced upgrade pass) must fill
// them even though nothing else about the unit changed.
func TestSync_FillsLinesOnRowsWrittenWithout(test *testing.T) {
	store := openSyncTestIndex(test)
	sync, nodes, _, _ := newSync(store, referencesManifest(test))

	parent := seedFileRow(test, nodes, "notes/fill", "notes/fill.md")

	bare, parseErr := subunit.Parse([]byte("# Title\n\nbody\n"))

	if parseErr != nil {
		test.Fatalf("Parse: %v", parseErr)
	}

	if _, applyErr := sync.ApplyFile(context.Background(), parent, bare); applyErr != nil {
		test.Fatalf("ApplyFile without lines: %v", applyErr)
	}

	if _, applyErr := sync.ApplyFile(context.Background(), parent, parseWithLines(test, "# Title\n\nbody\n")); applyErr != nil {
		test.Fatalf("ApplyFile with lines: %v", applyErr)
	}

	section, getErr := nodes.Get("notes/fill#S1")

	if getErr != nil {
		test.Fatalf("Get: %v", getErr)
	}

	if section.StartLine.Int64 != 1 || section.EndLine.Int64 != 3 {
		test.Errorf("section lines = %v..%v, want 1..3", section.StartLine, section.EndLine)
	}
}
