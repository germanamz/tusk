package reindex_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/reindex"
)

// caseInsensitiveFS reports whether root sits on a case-insensitive filesystem
// (macOS APFS, Windows NTFS). The #686 phantom can only form there — on a
// case-sensitive filesystem the pre- and post-rename names are distinct files
// and the reaper's ordinary not-exist path already tombstones the old one.
func caseInsensitiveFS(test *testing.T, root string) bool {
	test.Helper()

	probe := filepath.Join(root, "casecheck")

	if writeErr := os.WriteFile(probe, []byte("x"), 0o644); writeErr != nil {
		test.Fatalf("case probe write: %v", writeErr)
	}

	defer os.Remove(probe)

	_, statErr := os.Stat(filepath.Join(root, "CASECHECK"))

	return statErr == nil
}

// TestRun_ReapsCaseAliasPhantom pins #686 finding 1 (2)-(4): after an fs-level
// case rename (mv notes/foo.md notes/Foo.md) + reindex, the index must hold
// exactly one node under the new-case id — the stale-case row must be reaped,
// not re-stamped live forever by a case-insensitive os.Stat.
func TestRun_ReapsCaseAliasPhantom(test *testing.T) {
	root := test.TempDir()

	if !caseInsensitiveFS(test, root) {
		test.Skip("phantom only forms on a case-insensitive filesystem")
	}

	writeNode(test, root, "notes/foo.md", "type: note\n", "plain body about the foo topic\n")

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	repo := index.NewNodeRepo(store)

	if _, runErr := reindex.Run(withGen(store, reindex.Config{Root: root, Repo: repo})); runErr != nil {
		test.Fatalf("initial Run: %v", runErr)
	}

	// fs-level case rename outside tusk, then reindex.
	if renameErr := os.Rename(filepath.Join(root, "notes/foo.md"), filepath.Join(root, "notes/Foo.md")); renameErr != nil {
		test.Fatalf("case rename: %v", renameErr)
	}

	report, runErr := reindex.Run(withGen(store, reindex.Config{Root: root, Repo: repo}))

	if runErr != nil {
		test.Fatalf("second Run: %v", runErr)
	}

	if report.Removed != 1 {
		test.Errorf("Removed = %d, want 1 (stale-case notes/foo reaped)", report.Removed)
	}

	loaded, listErr := repo.List(index.ListFilter{Type: "note"})

	if listErr != nil {
		test.Fatalf("List: %v", listErr)
	}

	if len(loaded) != 1 {
		test.Fatalf("note count = %d, want exactly 1 (no phantom duplicate); got %+v", len(loaded), loaded)
	}

	if loaded[0].ID != "notes/Foo" {
		test.Errorf("surviving node id = %q, want notes/Foo", loaded[0].ID)
	}

	if _, getErr := repo.Get("notes/foo"); getErr == nil {
		test.Errorf("stale-case node notes/foo must be gone")
	}
}
