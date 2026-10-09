package reindex_test

import (
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/reindex"
)

// lineNumberingFixture indexes one note whose body holds a lone CR, so the
// lf and universal schemes number its last paragraph differently. writeNode
// puts four frontmatter lines above the body.
func lineNumberingFixture(test *testing.T) (string, *index.Index) {
	test.Helper()

	root := test.TempDir()

	writeNode(test, root, "notes/doc.md", "type: note\n", "# Title\n\nalpha\rbeta\n\ngamma\n")

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	return root, store
}

func runWithScheme(test *testing.T, root string, store *index.Index, scheme string) {
	test.Helper()

	loaded := &manifest.Manifest{Workspace: manifest.WorkspaceSection{LineNumbering: scheme}}

	if _, runErr := reindex.Run(withGen(store, reindex.Config{
		Root:     root,
		Repo:     index.NewNodeRepo(store),
		Edges:    index.NewEdgeRepo(store),
		Manifest: loaded,
	})); runErr != nil {
		test.Fatalf("Run (%s): %v", scheme, runErr)
	}
}

func subUnitLines(test *testing.T, store *index.Index, id string) [2]int64 {
	test.Helper()

	row, getErr := index.NewNodeRepo(store).Get(id)

	if getErr != nil {
		test.Fatalf("Get %s: %v", id, getErr)
	}

	return [2]int64{row.StartLine.Int64, row.EndLine.Int64}
}

// TestRun_RecordsFileRelativeSubUnitLines: lines count from the top of the
// file, frontmatter included, not from the start of the parsed body.
func TestRun_RecordsFileRelativeSubUnitLines(test *testing.T) {
	root, store := lineNumberingFixture(test)

	runWithScheme(test, root, store, "")

	if got := subUnitLines(test, store, "notes/doc#S1"); got != [2]int64{5, 9} {
		test.Errorf("S1 lines = %v, want [5 9]", got)
	}

	if got := subUnitLines(test, store, "notes/doc#S1P2"); got != [2]int64{9, 9} {
		test.Errorf("S1P2 lines = %v, want [9 9]", got)
	}
}

// TestRun_LineNumberingChangeRefillsLines: switching the scheme on an
// unchanged vault must renumber every sub-unit on the next plain reindex. The
// incremental mtime+size skip would otherwise keep the old numbers.
func TestRun_LineNumberingChangeRefillsLines(test *testing.T) {
	root, store := lineNumberingFixture(test)

	runWithScheme(test, root, store, "lf")

	if got := subUnitLines(test, store, "notes/doc#S1P2"); got != [2]int64{9, 9} {
		test.Fatalf("lf: S1P2 lines = %v, want [9 9]", got)
	}

	runWithScheme(test, root, store, "universal")

	if got := subUnitLines(test, store, "notes/doc#S1P2"); got != [2]int64{10, 10} {
		test.Errorf("universal: S1P2 lines = %v, want [10 10]", got)
	}

	marker, _ := index.NewMetaRepo(store).Get("line_numbering")

	if marker != "universal" {
		test.Errorf("line_numbering marker = %q, want universal", marker)
	}
}

// TestRun_FillsLinesOnUpgrade: an index from a binary that predates line
// ranges has NULL lines and no line_numbering marker. The next plain reindex
// must fill the lines without the file changing.
func TestRun_FillsLinesOnUpgrade(test *testing.T) {
	root, store := lineNumberingFixture(test)

	runWithScheme(test, root, store, "")

	if _, execErr := store.DB().Exec(`
		UPDATE nodes SET start_line = NULL, end_line = NULL;
		DELETE FROM meta WHERE key = 'line_numbering';
	`); execErr != nil {
		test.Fatalf("rewind to pre-upgrade shape: %v", execErr)
	}

	runWithScheme(test, root, store, "")

	if got := subUnitLines(test, store, "notes/doc#S1"); got != [2]int64{5, 9} {
		test.Errorf("S1 lines after upgrade pass = %v, want [5 9]", got)
	}
}
