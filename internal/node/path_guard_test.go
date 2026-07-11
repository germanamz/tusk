package node_test

import (
	"errors"
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

// TestCreate_RejectsReservedID pins #683: the write surface must refuse to
// author a file whose derived node id would collide with reserved id syntax
// ('#' aliases the sub-unit separator; a "reindex:" prefix collides with the
// embed-queue key namespace). Otherwise tusk could create a file the indexer is
// then forced to silently skip.
func TestCreate_RejectsReservedID(test *testing.T) {
	root := test.TempDir()
	service, store := newPathGuardService(test, root)

	defer store.Close()

	reservedPaths := []string{"notes/a#b.md", "reindex:notes.md", "reindex:sub/x.md"}

	for _, badPath := range reservedPaths {
		if _, createErr := service.Create(node.CreateInput{RelPath: badPath, Type: "note", Title: "X"}); !errors.Is(createErr, node.ErrReservedID) {
			test.Errorf("Create(%q) err = %v, want ErrReservedID", badPath, createErr)
		}

		if _, statErr := os.Stat(filepath.Join(root, badPath)); !os.IsNotExist(statErr) {
			test.Errorf("Create(%q) wrote a file for a reserved id (stat err = %v)", badPath, statErr)
		}
	}

	// A bracket name is not reserved and must still be creatable.
	if _, createErr := service.Create(node.CreateInput{RelPath: "notes/[wip] ok.md", Type: "note", Title: "OK"}); createErr != nil {
		test.Errorf("Create(bracket path) = %v, want success", createErr)
	}
}

// TestRename_RejectsReservedID pins the Rename twin of TestCreate_RejectsReservedID.
func TestRename_RejectsReservedID(test *testing.T) {
	root := test.TempDir()
	service, store := newPathGuardService(test, root)

	defer store.Close()

	if _, createErr := service.Create(node.CreateInput{RelPath: "notes/a.md", Type: "note", Title: "A"}); createErr != nil {
		test.Fatalf("create: %v", createErr)
	}

	for _, badTarget := range []string{"notes/b#c", "reindex:moved"} {
		if _, renameErr := node.Rename(
			root, index.NewNodeRepo(store), index.NewEdgeRepo(store),
			index.NewFileStateRepo(store), "test-worker", time.Minute,
			manifest.EdgeTypes{}, nil, nil, "notes/a", badTarget,
		); !errors.Is(renameErr, node.ErrReservedID) {
			test.Errorf("Rename to %q err = %v, want ErrReservedID", badTarget, renameErr)
		}
	}

	if _, statErr := os.Stat(filepath.Join(root, "notes/a.md")); statErr != nil {
		test.Errorf("original file should be intact after rejected rename: %v", statErr)
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
