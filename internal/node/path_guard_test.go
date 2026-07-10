package node_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

func newPathGuardService(test *testing.T, root string) (*node.Service, *index.Index) {
	test.Helper()

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	service := node.NewServiceWithLease(
		root, index.NewNodeRepo(store), index.NewEdgeRepo(store), manifest.EdgeTypes{}, nil,
		index.NewFileStateRepo(store), "test-worker", time.Minute,
	)

	return service, store
}

func TestCreate_RejectsPathEscapingVault(test *testing.T) {
	root := test.TempDir()
	service, store := newPathGuardService(test, root)

	defer store.Close()

	badPaths := []string{"../escape.md", "../../etc/evil.md", "/tmp/evil.md"}

	for _, badPath := range badPaths {
		if _, createErr := service.Create(node.CreateInput{RelPath: badPath, Type: "note", Title: "X"}); createErr == nil {
			test.Errorf("Create(%q) = nil error, want rejection", badPath)
		}
	}

	if _, statErr := os.Stat(filepath.Join(root, "..", "escape.md")); !os.IsNotExist(statErr) {
		test.Errorf("escape file was written outside the vault (stat err = %v)", statErr)
	}

	if _, createErr := service.Create(node.CreateInput{RelPath: "notes/ok.md", Type: "note", Title: "OK"}); createErr != nil {
		test.Errorf("Create(local path) = %v, want success", createErr)
	}
}

func TestRename_RejectsPathEscapingVault(test *testing.T) {
	root := test.TempDir()
	service, store := newPathGuardService(test, root)

	defer store.Close()

	if _, createErr := service.Create(node.CreateInput{RelPath: "notes/a.md", Type: "note", Title: "A"}); createErr != nil {
		test.Fatalf("create: %v", createErr)
	}

	if _, renameErr := node.Rename(
		root, index.NewNodeRepo(store), index.NewEdgeRepo(store),
		index.NewFileStateRepo(store), "test-worker", time.Minute,
		manifest.EdgeTypes{}, nil, nil, "notes/a", "../escape",
	); renameErr == nil {
		test.Errorf("Rename to a traversal path = nil error, want rejection")
	}

	if _, statErr := os.Stat(filepath.Join(root, "notes/a.md")); statErr != nil {
		test.Errorf("original file should be intact after rejected rename: %v", statErr)
	}
}
