package reindex_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
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

func TestRun_PersistsFrontmatterEdges(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "tickets/epic.md", "type: ticket\ntitle: Epic\n", "Body.\n")
	writeNode(test, root, "tickets/child.md", "type: ticket\ntitle: Child\nparent: tickets/epic\n", "Body.\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	repo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	edgeTypes := manifest.EdgeTypes{
		"parent": manifest.EdgeType{
			From: []string{"ticket"}, To: []string{"ticket", "project"},
			Cardinality: manifest.CardinalityManyToOne,
		},
	}

	report, runErr := reindex.Run(reindex.Config{
		Root:      root,
		Repo:      repo,
		Edges:     edgeRepo,
		EdgeTypes: edgeTypes,
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.Indexed != 2 {
		test.Errorf("Indexed = %d, want 2", report.Indexed)
	}

	listed, _ := edgeRepo.ListBySource("tickets/child")

	if len(listed) != 1 || listed[0].Type != "parent" || listed[0].TargetID != "tickets/epic" {
		test.Errorf("listed = %+v", listed)
	}
}

func TestRun_PersistsWikilinksAsReferenceEdges(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "notes/target.md", "type: note\ntitle: Target\n", "")
	writeNode(test, root, "notes/source.md", "type: note\ntitle: Source\n", "Refer to [[notes/target]].\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	repo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	edgeTypes := manifest.EdgeTypes{
		"references": manifest.EdgeType{
			From: []string{"*"}, To: []string{"*"},
			Cardinality: manifest.CardinalityManyToMany,
		},
	}

	if _, runErr := reindex.Run(reindex.Config{Root: root, Repo: repo, Edges: edgeRepo, EdgeTypes: edgeTypes}); runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	listed, _ := edgeRepo.ListBySource("notes/source")

	if len(listed) != 1 || listed[0].Type != "references" || listed[0].TargetID != "notes/target" {
		test.Errorf("listed = %+v", listed)
	}
}

func TestRun_RemovedFileAlsoRemovesEdges(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "tickets/a.md", "type: ticket\ntitle: A\nparent: tickets/b\n", "")
	writeNode(test, root, "tickets/b.md", "type: ticket\ntitle: B\n", "")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	repo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	edgeTypes := manifest.EdgeTypes{
		"parent": manifest.EdgeType{
			From: []string{"ticket"}, To: []string{"ticket"},
			Cardinality: manifest.CardinalityManyToOne,
		},
	}

	cfg := reindex.Config{Root: root, Repo: repo, Edges: edgeRepo, EdgeTypes: edgeTypes}

	if _, runErr := reindex.Run(cfg); runErr != nil {
		test.Fatalf("first Run: %v", runErr)
	}

	if rmErr := os.Remove(filepath.Join(root, "tickets/a.md")); rmErr != nil {
		test.Fatalf("rm: %v", rmErr)
	}

	if _, runErr := reindex.Run(cfg); runErr != nil {
		test.Fatalf("second Run: %v", runErr)
	}

	listed, _ := edgeRepo.ListBySource("tickets/a")

	if len(listed) != 0 {
		test.Errorf("expected zero edges after node removal, got %+v", listed)
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
