package node_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/node"
)

// Moving an .mdx node must keep the markdown-twin id convention (the id RETAINS
// its extension: notes/guide.mdx -> id "notes/guide.mdx") AND retarget every
// referrer to the new full-extension id. This is the .mdx analogue of the #687
// HTML-referrer regression (TestRename_KeepsExtensionOnHTMLNodeID): stripping
// the extension would mint a phantom bare-stem row and rewrite referrer
// wikilinks to the dead id.
func TestRename_RetargetsReferrerToMovedMDXNode(test *testing.T) {
	root := test.TempDir()

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	edgeTypes := wikilinkEdgeTypes()

	// Seed the .mdx node on disk plus its index row (id retains the extension,
	// the way ParseFile / the reindex walk record it for a markdown twin).
	writeWorkspaceFile(test, root, "notes/guide.mdx",
		"---\ntype: note\n---\n\nimport { Callout } from './c'\n\n<Callout>guide</Callout>\n")
	seedFileNode(test, nodeRepo, "notes/guide.mdx", "notes/guide.mdx")

	// A markdown referrer wikilinks the full-extension .mdx id.
	writeWorkspaceFile(test, root, "notes/ref.md", "---\ntype: note\n---\nsee [[notes/guide.mdx]] for steps\n")
	seedFileNode(test, nodeRepo, "notes/ref", "notes/ref.md")

	if upsertErr := edgeRepo.UpsertAll("notes/ref", "notes/ref.md", []index.EdgeRow{{
		Type: "references", SourceID: "notes/ref", TargetID: "notes/guide.mdx",
		SourcePath: "notes/ref.md", Kind: "direct",
	}}); upsertErr != nil {
		test.Fatalf("seed referrer edge: %v", upsertErr)
	}

	plan, renameErr := node.Rename(
		root, nodeRepo, edgeRepo,
		index.NewFileStateRepo(store), "test-worker", time.Minute,
		edgeTypes, nil, nil, "notes/guide.mdx", "notes/manual.mdx",
	)

	if renameErr != nil {
		test.Fatalf("Rename: %v", renameErr)
	}

	if plan.NewID != "notes/manual.mdx" {
		test.Errorf("NewID = %q, want notes/manual.mdx", plan.NewID)
	}

	newRow, getErr := nodeRepo.Get("notes/manual.mdx")

	if getErr != nil {
		test.Fatalf("Get(notes/manual.mdx) = %v, want the moved node", getErr)
	}

	if newRow.Path != "notes/manual.mdx" {
		test.Errorf("moved node path = %q, want notes/manual.mdx", newRow.Path)
	}

	// No phantom bare-stem row: the extension-stripped id must never exist.
	if _, phantomErr := nodeRepo.Get("notes/manual"); phantomErr != index.ErrNodeNotFound {
		test.Errorf("Get(notes/manual) = %v, want ErrNodeNotFound (phantom row minted)", phantomErr)
	}

	refContent, _ := os.ReadFile(filepath.Join(root, "notes/ref.md"))

	if !strings.Contains(string(refContent), "[[notes/manual.mdx]]") {
		test.Errorf("referrer wikilink not rewritten to full-extension id:\n%s", string(refContent))
	}

	if strings.Contains(string(refContent), "[[notes/manual]]") {
		test.Errorf("referrer wikilink corrupted to extension-stripped id:\n%s", string(refContent))
	}

	refEdges, _ := edgeRepo.ListBySource("notes/ref")

	if len(refEdges) != 1 || refEdges[0].TargetID != "notes/manual.mdx" {
		test.Errorf("referrer edge = %+v, want single edge targeting notes/manual.mdx", refEdges)
	}
}
