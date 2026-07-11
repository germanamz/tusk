package node_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/node"
)

// TestCreate_RejectsNonIndexableExtension pins #686 (the create-side twin of
// finding 2): creating a node with a non-indexable extension must be refused.
// The reindex walk only indexes .md/.html/.htm, so a .txt node row would be a
// permanent phantom the orphan reaper never sees.
func TestCreate_RejectsNonIndexableExtension(test *testing.T) {
	service, _, _, root := newLeaseTestService(test, "test-worker")

	_, createErr := service.Create(node.CreateInput{RelPath: "zz.txt", Type: "note"})

	if !errors.Is(createErr, node.ErrNotIndexableExt) {
		test.Fatalf("Create zz.txt err = %v, want ErrNotIndexableExt", createErr)
	}

	if _, statErr := os.Stat(filepath.Join(root, "zz.txt")); !os.IsNotExist(statErr) {
		test.Errorf("zz.txt must not be created, stat err = %v", statErr)
	}
}

// TestCreate_RejectsDestinationInIgnoredDir pins #686 (create-side twin): a
// node created inside a built-in-ignored directory (.tusk/) must be refused.
func TestCreate_RejectsDestinationInIgnoredDir(test *testing.T) {
	service, _, _, root := newLeaseTestService(test, "test-worker")

	_, createErr := service.Create(node.CreateInput{RelPath: ".tusk/zz.md", Type: "note"})

	if !errors.Is(createErr, node.ErrDestinationIgnored) {
		test.Fatalf("Create .tusk/zz.md err = %v, want ErrDestinationIgnored", createErr)
	}

	if _, statErr := os.Stat(filepath.Join(root, ".tusk/zz.md")); !os.IsNotExist(statErr) {
		test.Errorf(".tusk/zz.md must not be created, stat err = %v", statErr)
	}
}
