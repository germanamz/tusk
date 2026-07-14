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

// newRenameDestService builds a lease-capable Service plus the shared repos a
// destination-validation rename test needs. The store is closed on cleanup.
func newRenameDestService(test *testing.T) (*node.Service, *index.NodeRepo, *index.EdgeRepo, *index.FileStateRepo, string) {
	test.Helper()

	root := test.TempDir()

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	fileState := index.NewFileStateRepo(store)

	service := node.NewServiceWithLease(
		root, nodeRepo, edgeRepo, manifest.EdgeTypes{}, nil,
		fileState, "test-worker", time.Minute,
	)

	return service, nodeRepo, edgeRepo, fileState, root
}

// TestRename_CaseOnlyRenameSucceeds pins #686 finding 1: a case-only rename
// (notes/foo -> notes/Foo) must succeed. On a case-insensitive filesystem the
// destination os.Stat resolves to the source file itself, which the bare
// existence check misread as "target already exists"; os.SameFile treats that
// as free so os.Rename can perform the case change.
func TestRename_CaseOnlyRenameSucceeds(test *testing.T) {
	service, nodeRepo, edgeRepo, fileState, root := newRenameDestService(test)

	if _, createErr := service.Create(node.CreateInput{RelPath: "notes/foo.md", Type: "note"}); createErr != nil {
		test.Fatalf("create foo: %v", createErr)
	}

	plan, renameErr := node.Rename(
		root, nodeRepo, edgeRepo, fileState, "test-worker", time.Minute,
		manifest.EdgeTypes{}, nil, nil, "notes/foo", "notes/Foo",
	)

	if renameErr != nil {
		test.Fatalf("case-only Rename: %v", renameErr)
	}

	if plan.NewID != "notes/Foo" || plan.NewPath != "notes/Foo.md" {
		test.Errorf("plan = %+v, want NewID=notes/Foo NewPath=notes/Foo.md", plan)
	}

	if _, getErr := nodeRepo.Get("notes/Foo"); getErr != nil {
		test.Errorf("notes/Foo should be indexed after case-only rename: %v", getErr)
	}

	if _, getErr := nodeRepo.Get("notes/foo"); !errors.Is(getErr, index.ErrNodeNotFound) {
		test.Errorf("old id notes/foo should be gone, Get err = %v", getErr)
	}
}

// TestRename_RejectsCrossExtensionDestination pins #686 finding 2: moving a
// node to a destination whose extension differs from the source (notes/x.md ->
// notes/b.txt) must be refused. The stripped id would collide with a sibling
// and the .txt file would never be indexed.
func TestRename_RejectsCrossExtensionDestination(test *testing.T) {
	service, nodeRepo, edgeRepo, fileState, root := newRenameDestService(test)

	if _, createErr := service.Create(node.CreateInput{RelPath: "notes/x.md", Type: "note"}); createErr != nil {
		test.Fatalf("create x: %v", createErr)
	}

	_, renameErr := node.Rename(
		root, nodeRepo, edgeRepo, fileState, "test-worker", time.Minute,
		manifest.EdgeTypes{}, nil, nil, "notes/x", "notes/b.txt",
	)

	if !errors.Is(renameErr, node.ErrExtensionMismatch) {
		test.Fatalf("Rename to .txt err = %v, want ErrExtensionMismatch", renameErr)
	}

	// The source must be untouched: still indexed at its original id/path.
	if _, getErr := nodeRepo.Get("notes/x"); getErr != nil {
		test.Errorf("notes/x should be intact after a rejected move: %v", getErr)
	}

	if _, statErr := os.Stat(filepath.Join(root, "notes/b.txt")); !os.IsNotExist(statErr) {
		test.Errorf("notes/b.txt must not be created by a rejected move, stat err = %v", statErr)
	}
}

// TestRename_MDXKeepsExtensionInID pins the .mdx move path: an .mdx node is a
// markdown twin whose id retains its extension, so a move mints an id that still
// carries ".mdx" (path == id) and the reindex re-parse re-derives it identically.
func TestRename_MDXKeepsExtensionInID(test *testing.T) {
	service, nodeRepo, edgeRepo, fileState, root := newRenameDestService(test)

	if _, createErr := service.Create(node.CreateInput{RelPath: "notes/guide.mdx", Type: "note"}); createErr != nil {
		test.Fatalf("create guide.mdx: %v", createErr)
	}

	plan, renameErr := node.Rename(
		root, nodeRepo, edgeRepo, fileState, "test-worker", time.Minute,
		manifest.EdgeTypes{}, nil, nil, "notes/guide.mdx", "notes/manual.mdx",
	)

	if renameErr != nil {
		test.Fatalf("mdx Rename: %v", renameErr)
	}

	if plan.NewID != "notes/manual.mdx" || plan.NewPath != "notes/manual.mdx" {
		test.Errorf("plan = %+v, want NewID=NewPath=notes/manual.mdx", plan)
	}

	if _, getErr := nodeRepo.Get("notes/manual.mdx"); getErr != nil {
		test.Errorf("notes/manual.mdx should be indexed after rename: %v", getErr)
	}

	if _, getErr := nodeRepo.Get("notes/guide.mdx"); !errors.Is(getErr, index.ErrNodeNotFound) {
		test.Errorf("old id notes/guide.mdx should be gone, Get err = %v", getErr)
	}
}

// TestRename_RejectsMDXToMarkdownCrossExtension pins that an .mdx -> .md move is
// a cross-class move and must be refused: the two kinds derive ids differently
// (".mdx" retained vs ".md" stripped), so the move would orphan or hijack a row.
func TestRename_RejectsMDXToMarkdownCrossExtension(test *testing.T) {
	service, nodeRepo, edgeRepo, fileState, root := newRenameDestService(test)

	if _, createErr := service.Create(node.CreateInput{RelPath: "notes/x.mdx", Type: "note"}); createErr != nil {
		test.Fatalf("create x.mdx: %v", createErr)
	}

	_, renameErr := node.Rename(
		root, nodeRepo, edgeRepo, fileState, "test-worker", time.Minute,
		manifest.EdgeTypes{}, nil, nil, "notes/x.mdx", "notes/x.md",
	)

	if !errors.Is(renameErr, node.ErrExtensionMismatch) {
		test.Fatalf("Rename .mdx -> .md err = %v, want ErrExtensionMismatch", renameErr)
	}

	if _, getErr := nodeRepo.Get("notes/x.mdx"); getErr != nil {
		test.Errorf("notes/x.mdx should be intact after a rejected move: %v", getErr)
	}
}

// TestRename_RejectsDestinationInIgnoredDir pins #686 finding 2: moving a node
// into a built-in-ignored directory (.tusk/) must be refused — the reindex walk
// skips that tree, so the node row would diverge from disk forever.
func TestRename_RejectsDestinationInIgnoredDir(test *testing.T) {
	service, nodeRepo, edgeRepo, fileState, root := newRenameDestService(test)

	if _, createErr := service.Create(node.CreateInput{RelPath: "notes/r.md", Type: "note"}); createErr != nil {
		test.Fatalf("create r: %v", createErr)
	}

	_, renameErr := node.Rename(
		root, nodeRepo, edgeRepo, fileState, "test-worker", time.Minute,
		manifest.EdgeTypes{}, nil, nil, "notes/r", ".tusk/evil.md",
	)

	if !errors.Is(renameErr, node.ErrDestinationIgnored) {
		test.Fatalf("Rename into .tusk/ err = %v, want ErrDestinationIgnored", renameErr)
	}

	if _, statErr := os.Stat(filepath.Join(root, ".tusk/evil.md")); !os.IsNotExist(statErr) {
		test.Errorf(".tusk/evil.md must not be created by a rejected move, stat err = %v", statErr)
	}
}
