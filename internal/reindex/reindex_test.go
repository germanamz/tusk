package reindex_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/reindex"
)

func TestRun_IndexesAllMarkdownNodes(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "notes/auth.md", "type: note\ntitle: Auth\n", "Body.\n")
	writeNode(test, root, "tickets/fix.md", "type: ticket\ntitle: Fix\n", "Body.\n")
	writeNode(test, root, "ignored.txt", "", "not markdown")

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	defer store.Close()

	repo := index.NewNodeRepo(store)

	report, runErr := reindex.Run(reindex.Config{Root: root, Repo: repo})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.Indexed != 2 {
		test.Errorf("Indexed = %d, want 2", report.Indexed)
	}

	loaded, listErr := repo.List(index.ListFilter{})

	if listErr != nil {
		test.Fatalf("List: %v", listErr)
	}

	if len(loaded) != 2 {
		test.Errorf("len = %d, want 2", len(loaded))
	}
}

func TestRun_SkipsTuskInternalDir(test *testing.T) {
	root := test.TempDir()

	if mkErr := os.MkdirAll(filepath.Join(root, ".tusk"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	if writeErr := os.WriteFile(filepath.Join(root, ".tusk", "fake.md"), []byte("---\ntype: note\n---\n"), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	writeNode(test, root, "real.md", "type: note\n", "Body.\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	repo := index.NewNodeRepo(store)

	_, runErr := reindex.Run(reindex.Config{Root: root, Repo: repo})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	loaded, _ := repo.List(index.ListFilter{})

	if len(loaded) != 1 || loaded[0].ID != "real" {
		test.Errorf("unexpected: %+v", loaded)
	}
}

func TestRun_RemovesStaleEntries(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "stale.md", "type: note\n", "Body.\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	repo := index.NewNodeRepo(store)

	if _, runErr := reindex.Run(reindex.Config{Root: root, Repo: repo}); runErr != nil {
		test.Fatalf("first Run: %v", runErr)
	}

	if rmErr := os.Remove(filepath.Join(root, "stale.md")); rmErr != nil {
		test.Fatalf("rm: %v", rmErr)
	}

	report, runErr := reindex.Run(reindex.Config{Root: root, Repo: repo})

	if runErr != nil {
		test.Fatalf("second Run: %v", runErr)
	}

	if report.Removed != 1 {
		test.Errorf("Removed = %d, want 1", report.Removed)
	}

	if _, getErr := repo.Get("stale"); getErr != index.ErrNodeNotFound {
		test.Errorf("err = %v, want ErrNodeNotFound", getErr)
	}
}

func writeNode(test *testing.T, root, relPath, frontmatter, body string) {
	test.Helper()

	abs := filepath.Join(root, relPath)

	if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	content := "---\n" + frontmatter + "---\n\n" + body

	if filepath.Ext(relPath) != ".md" {
		content = body
	}

	if writeErr := os.WriteFile(abs, []byte(content), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}
}
