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

// Moving an HTML node must keep the HTML id convention (the id RETAINS its
// extension: docs/a.html -> id "docs/a.html"). Regression test for #687
// finding 1: Rename stripped the extension unconditionally, minting a phantom
// row id "docs/b" with path "docs/b.html". That corrupted referrer wikilinks
// on disk ([[docs/b]] instead of [[docs/b.html]]) and wedged reindex forever
// on a UNIQUE nodes.path constraint (the phantom row occupied the path the
// re-parse tried to claim under the correct id).
func TestRename_KeepsExtensionOnHTMLNodeID(test *testing.T) {
	root := test.TempDir()

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	edgeTypes := wikilinkEdgeTypes()

	// Seed the HTML node on disk plus its index row (its id retains the
	// extension, the way ParseHTMLFile / the reindex walk record it).
	htmlBody := "<!doctype html>\n<html><head><meta name=\"tusk:type\" content=\"note\">" +
		"<title>Widget Guide</title></head><body><p>guide</p></body></html>\n"
	writeWorkspaceFile(test, root, "docs/a.html", htmlBody)
	seedFileNode(test, nodeRepo, "docs/a.html", "docs/a.html")

	// A markdown referrer wikilinks the full-extension HTML id.
	writeWorkspaceFile(test, root, "notes/ref.md", "---\ntype: note\n---\nsee [[docs/a.html]] for steps\n")
	seedFileNode(test, nodeRepo, "notes/ref", "notes/ref.md")

	if upsertErr := edgeRepo.UpsertAll("notes/ref", "notes/ref.md", []index.EdgeRow{{
		Type: "references", SourceID: "notes/ref", TargetID: "docs/a.html",
		SourcePath: "notes/ref.md", Kind: "direct",
	}}); upsertErr != nil {
		test.Fatalf("seed referrer edge: %v", upsertErr)
	}

	plan, renameErr := node.Rename(
		root, nodeRepo, edgeRepo,
		index.NewFileStateRepo(store), "test-worker", time.Minute,
		edgeTypes, nil, nil, "docs/a.html", "docs/b.html",
	)

	if renameErr != nil {
		test.Fatalf("Rename: %v", renameErr)
	}

	if plan.NewID != "docs/b.html" {
		test.Errorf("NewID = %q, want docs/b.html", plan.NewID)
	}

	newRow, getErr := nodeRepo.Get("docs/b.html")

	if getErr != nil {
		test.Fatalf("Get(docs/b.html) = %v, want the moved node", getErr)
	}

	if newRow.Path != "docs/b.html" {
		test.Errorf("moved node path = %q, want docs/b.html", newRow.Path)
	}

	// No phantom bare-stem row: the extension-stripped id must never exist.
	if _, phantomErr := nodeRepo.Get("docs/b"); phantomErr != index.ErrNodeNotFound {
		test.Errorf("Get(docs/b) = %v, want ErrNodeNotFound (phantom row minted)", phantomErr)
	}

	refContent, _ := os.ReadFile(filepath.Join(root, "notes/ref.md"))

	if !strings.Contains(string(refContent), "[[docs/b.html]]") {
		test.Errorf("referrer wikilink not rewritten to full-extension id:\n%s", string(refContent))
	}

	if strings.Contains(string(refContent), "[[docs/b]]") {
		test.Errorf("referrer wikilink corrupted to extension-stripped id:\n%s", string(refContent))
	}

	refEdges, _ := edgeRepo.ListBySource("notes/ref")

	if len(refEdges) != 1 || refEdges[0].TargetID != "docs/b.html" {
		test.Errorf("referrer edge = %+v, want single edge targeting docs/b.html", refEdges)
	}
}

// Moving a markdown node linked from an HTML <a href> must rewrite the href on
// disk so the reference follows the move. Regression test for #687 finding 2:
// the href was left untouched, so the re-derive loop re-parsed the stale href
// and reverted the retargeted edge back to the dead old id in the same Rename
// call — a permanent dangling edge no reindex could heal (the referrer's bytes
// never changed).
func TestRename_RewritesHTMLReferrerHrefOnDisk(test *testing.T) {
	root := test.TempDir()

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	edgeTypes := wikilinkEdgeTypes()

	service := node.NewServiceWithLease(
		root, nodeRepo, edgeRepo, edgeTypes, nil,
		index.NewFileStateRepo(store), "test-worker", time.Minute,
	)

	if _, targetErr := service.Create(node.CreateInput{
		RelPath: "notes/target.md", Type: "note", Title: "Target",
	}); targetErr != nil {
		test.Fatalf("create target: %v", targetErr)
	}

	// An HTML referrer links the markdown target through a dir-relative href
	// (no extension — the markdown id has none).
	htmlBody := "<!doctype html>\n<html><head><meta name=\"tusk:type\" content=\"note\">" +
		"<title>Page</title></head><body><p>See <a href=\"../notes/target\">the note</a>.</p></body></html>\n"
	writeWorkspaceFile(test, root, "docs/page.html", htmlBody)
	seedFileNode(test, nodeRepo, "docs/page.html", "docs/page.html")

	if upsertErr := edgeRepo.UpsertAll("docs/page.html", "docs/page.html", []index.EdgeRow{{
		Type: "references", SourceID: "docs/page.html", TargetID: "notes/target",
		SourcePath: "docs/page.html", Kind: "direct",
	}}); upsertErr != nil {
		test.Fatalf("seed referrer edge: %v", upsertErr)
	}

	if _, renameErr := node.Rename(
		root, nodeRepo, edgeRepo,
		index.NewFileStateRepo(store), "test-worker", time.Minute,
		edgeTypes, nil, nil, "notes/target", "notes/renamed.md",
	); renameErr != nil {
		test.Fatalf("Rename: %v", renameErr)
	}

	pageContent, _ := os.ReadFile(filepath.Join(root, "docs/page.html"))

	if !strings.Contains(string(pageContent), `href="../notes/renamed"`) {
		test.Errorf("HTML href not rewritten on disk:\n%s", string(pageContent))
	}

	if strings.Contains(string(pageContent), `href="../notes/target"`) {
		test.Errorf("stale HTML href left on disk:\n%s", string(pageContent))
	}

	pageEdges, _ := edgeRepo.ListBySource("docs/page.html")

	if len(pageEdges) != 1 || pageEdges[0].TargetID != "notes/renamed" {
		test.Errorf("HTML referrer edge = %+v, want single edge targeting notes/renamed", pageEdges)
	}

	if stale, _ := edgeRepo.ListByTarget("notes/target"); len(stale) != 0 {
		test.Errorf("edges still target the dead id notes/target: %+v", stale)
	}
}

// writeWorkspaceFile writes content to a workspace-relative path under root,
// creating parent directories.
func writeWorkspaceFile(test *testing.T, root, relPath, content string) {
	test.Helper()

	abs := filepath.Join(root, relPath)

	if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
		test.Fatalf("mkdir %s: %v", filepath.Dir(abs), mkErr)
	}

	if writeErr := os.WriteFile(abs, []byte(content), 0o644); writeErr != nil {
		test.Fatalf("write %s: %v", relPath, writeErr)
	}
}

// seedFileNode records a file-level node row the way the reindex walk would,
// so Rename has an index row to move.
func seedFileNode(test *testing.T, nodeRepo *index.NodeRepo, nodeID, path string) {
	test.Helper()

	if upsertErr := nodeRepo.Upsert(index.NodeRow{
		ID: nodeID, Type: "note", Path: path, Title: nodeID,
		PropertiesJSON: `{}`, LastMtime: 1, LastSize: 1, LastChecksum: "h",
	}); upsertErr != nil {
		test.Fatalf("seed node row %s: %v", nodeID, upsertErr)
	}
}
