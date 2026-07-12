package node_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

func TestDelete_RemovesFileAndEdges(test *testing.T) {
	root := test.TempDir()

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)

	edgeTypes := manifest.EdgeTypes{
		"parent": manifest.EdgeType{From: []string{"ticket"}, To: []string{"ticket"}, Cardinality: manifest.CardinalityManyToOne},
	}

	service := node.NewServiceWithLease(
		root, nodeRepo, edgeRepo, edgeTypes, nil,
		index.NewFileStateRepo(store), "test-worker", time.Minute,
	)

	if _, parentErr := service.Create(node.CreateInput{
		RelPath: "tickets/parent.md", Type: "ticket", Title: "Parent",
	}); parentErr != nil {
		test.Fatalf("create parent: %v", parentErr)
	}

	if _, childErr := service.Create(node.CreateInput{
		RelPath:    "tickets/child.md",
		Type:       "ticket",
		Title:      "Child",
		Properties: map[string]any{"parent": "tickets/parent"},
	}); childErr != nil {
		test.Fatalf("create child: %v", childErr)
	}

	if deleteErr := node.Delete(
		root, nodeRepo, edgeRepo, index.NewFileStateRepo(store), nil, "test-worker", time.Minute, "tickets/child",
	); deleteErr != nil {
		test.Fatalf("Delete: %v", deleteErr)
	}

	if _, statErr := os.Stat(filepath.Join(root, "tickets/child.md")); !os.IsNotExist(statErr) {
		test.Errorf("expected file removed, got stat err = %v", statErr)
	}

	if _, getErr := nodeRepo.Get("tickets/child"); getErr != index.ErrNodeNotFound {
		test.Errorf("expected ErrNodeNotFound, got %v", getErr)
	}

	outgoing, _ := edgeRepo.ListBySource("tickets/child")

	if len(outgoing) != 0 {
		test.Errorf("expected zero outgoing edges, got %+v", outgoing)
	}
}

func TestDelete_ReturnsErrorWhenNodeNotFound(test *testing.T) {
	root := test.TempDir()

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)

	deleteErr := node.Delete(
		root, nodeRepo, edgeRepo, index.NewFileStateRepo(store), nil, "test-worker", time.Minute, "tickets/missing",
	)

	if deleteErr == nil {
		test.Fatalf("expected error for missing node")
	}
}

func TestRename_MovesFileAndRewritesReferringEdgesInFrontmatter(test *testing.T) {
	root := test.TempDir()

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)

	edgeTypes := manifest.EdgeTypes{
		"parent": manifest.EdgeType{From: []string{"ticket"}, To: []string{"ticket"}, Cardinality: manifest.CardinalityManyToOne},
	}

	service := node.NewServiceWithLease(
		root, nodeRepo, edgeRepo, edgeTypes, nil,
		index.NewFileStateRepo(store), "test-worker", time.Minute,
	)

	if _, parentErr := service.Create(node.CreateInput{
		RelPath: "tickets/old-parent.md", Type: "ticket", Title: "Parent",
	}); parentErr != nil {
		test.Fatalf("create parent: %v", parentErr)
	}

	if _, childErr := service.Create(node.CreateInput{
		RelPath:    "tickets/child.md",
		Type:       "ticket",
		Title:      "Child",
		Properties: map[string]any{"parent": "tickets/old-parent"},
	}); childErr != nil {
		test.Fatalf("create child: %v", childErr)
	}

	plan, renameErr := node.Rename(
		root, nodeRepo, edgeRepo,
		index.NewFileStateRepo(store), "test-worker", time.Minute,
		edgeTypes, nil, nil, "tickets/old-parent", "tickets/new-parent.md",
	)

	if renameErr != nil {
		test.Fatalf("Rename: %v", renameErr)
	}

	if plan.NewID != "tickets/new-parent" {
		test.Errorf("NewID = %q, want tickets/new-parent", plan.NewID)
	}

	if _, statErr := os.Stat(filepath.Join(root, "tickets/new-parent.md")); statErr != nil {
		test.Errorf("expected new file: %v", statErr)
	}

	if _, statErr := os.Stat(filepath.Join(root, "tickets/old-parent.md")); !os.IsNotExist(statErr) {
		test.Errorf("expected old file gone, stat err = %v", statErr)
	}

	if _, getErr := nodeRepo.Get("tickets/new-parent"); getErr != nil {
		test.Errorf("Get(new) = %v", getErr)
	}

	childEdges, _ := edgeRepo.ListBySource("tickets/child")

	found := false

	for _, edge := range childEdges {
		if edge.Type == "parent" && edge.TargetID == "tickets/new-parent" {
			found = true
		}
	}

	if !found {
		test.Errorf("child edge should now target tickets/new-parent: %+v", childEdges)
	}

	childContent, _ := os.ReadFile(filepath.Join(root, "tickets/child.md"))

	if !strings.Contains(string(childContent), "parent: tickets/new-parent") {
		test.Errorf("child frontmatter not rewritten:\n%s", string(childContent))
	}
}

func TestRename_RewritesBlockSequenceEdgeTargets(test *testing.T) {
	root := test.TempDir()

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)

	edgeTypes := manifest.EdgeTypes{
		"blocks": manifest.EdgeType{From: []string{"ticket"}, To: []string{"ticket"}, Cardinality: manifest.CardinalityManyToMany},
	}

	service := node.NewServiceWithLease(
		root, nodeRepo, edgeRepo, edgeTypes, nil,
		index.NewFileStateRepo(store), "test-worker", time.Minute,
	)

	for _, target := range []string{"tickets/a", "tickets/b"} {
		if _, createErr := service.Create(node.CreateInput{
			RelPath: target + ".md", Type: "ticket", Title: target,
		}); createErr != nil {
			test.Fatalf("create %s: %v", target, createErr)
		}
	}

	if _, refErr := service.Create(node.CreateInput{
		RelPath:    "tickets/ref.md",
		Type:       "ticket",
		Title:      "Ref",
		Properties: map[string]any{"blocks": []any{"tickets/a", "tickets/b"}},
	}); refErr != nil {
		test.Fatalf("create ref: %v", refErr)
	}

	refContent, _ := os.ReadFile(filepath.Join(root, "tickets/ref.md"))

	if !strings.Contains(string(refContent), "  - tickets/a\n") {
		test.Fatalf("precondition: ref should store blocks as a block sequence:\n%s", string(refContent))
	}

	if _, renameErr := node.Rename(
		root, nodeRepo, edgeRepo,
		index.NewFileStateRepo(store), "test-worker", time.Minute,
		edgeTypes, nil, nil, "tickets/a", "tickets/a-renamed.md",
	); renameErr != nil {
		test.Fatalf("Rename: %v", renameErr)
	}

	rewritten, _ := os.ReadFile(filepath.Join(root, "tickets/ref.md"))

	if strings.Contains(string(rewritten), "  - tickets/a\n") {
		test.Errorf("block-sequence target left dangling at old id:\n%s", string(rewritten))
	}

	if !strings.Contains(string(rewritten), "  - tickets/a-renamed\n") {
		test.Errorf("block-sequence target not rewritten to new id:\n%s", string(rewritten))
	}

	refEdges, _ := edgeRepo.ListBySource("tickets/ref")

	var renamedFound, siblingKept bool

	for _, edge := range refEdges {
		if edge.Type != "blocks" {
			continue
		}

		if edge.TargetID == "tickets/a-renamed" {
			renamedFound = true
		}

		if edge.TargetID == "tickets/b" {
			siblingKept = true
		}
	}

	if !renamedFound || !siblingKept {
		test.Errorf("edges after rename = %+v, want blocks→{tickets/a-renamed, tickets/b}", refEdges)
	}
}

func TestRename_InheritsSourceExtensionWhenTargetHasNone(test *testing.T) {
	root := test.TempDir()

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)

	service := node.NewServiceWithLease(
		root, nodeRepo, edgeRepo, manifest.EdgeTypes{}, nil,
		index.NewFileStateRepo(store), "test-worker", time.Minute,
	)

	if _, createErr := service.Create(node.CreateInput{RelPath: "notes/foo.md", Type: "note"}); createErr != nil {
		test.Fatalf("create foo: %v", createErr)
	}

	plan, renameErr := node.Rename(
		root, nodeRepo, edgeRepo,
		index.NewFileStateRepo(store), "test-worker", time.Minute,
		manifest.EdgeTypes{}, nil, nil, "notes/foo", "notes/bar",
	)

	if renameErr != nil {
		test.Fatalf("Rename: %v", renameErr)
	}

	if plan.NewPath != "notes/bar.md" {
		test.Errorf("NewPath = %q, want notes/bar.md", plan.NewPath)
	}

	if plan.NewID != "notes/bar" {
		test.Errorf("NewID = %q, want notes/bar", plan.NewID)
	}

	if _, statErr := os.Stat(filepath.Join(root, "notes/bar.md")); statErr != nil {
		test.Errorf("expected notes/bar.md on disk, stat err = %v", statErr)
	}

	if _, statErr := os.Stat(filepath.Join(root, "notes/bar")); !os.IsNotExist(statErr) {
		test.Errorf("expected extensionless notes/bar to NOT exist, stat err = %v", statErr)
	}
}

func TestRename_HonorsExplicitTargetExtension(test *testing.T) {
	root := test.TempDir()

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)

	service := node.NewServiceWithLease(
		root, nodeRepo, edgeRepo, manifest.EdgeTypes{}, nil,
		index.NewFileStateRepo(store), "test-worker", time.Minute,
	)

	if _, createErr := service.Create(node.CreateInput{RelPath: "notes/foo.md", Type: "note"}); createErr != nil {
		test.Fatalf("create foo: %v", createErr)
	}

	plan, renameErr := node.Rename(
		root, nodeRepo, edgeRepo,
		index.NewFileStateRepo(store), "test-worker", time.Minute,
		manifest.EdgeTypes{}, nil, nil, "notes/foo", "notes/bar.md",
	)

	if renameErr != nil {
		test.Fatalf("Rename: %v", renameErr)
	}

	if plan.NewPath != "notes/bar.md" || plan.NewID != "notes/bar" {
		test.Errorf("plan = %+v, want NewPath=notes/bar.md NewID=notes/bar", plan)
	}
}

func TestRename_ReturnsErrorWhenTargetExists(test *testing.T) {
	root := test.TempDir()

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)

	service := node.NewServiceWithLease(
		root, nodeRepo, edgeRepo, manifest.EdgeTypes{}, nil,
		index.NewFileStateRepo(store), "test-worker", time.Minute,
	)

	_, _ = service.Create(node.CreateInput{RelPath: "a.md", Type: "note"})
	_, _ = service.Create(node.CreateInput{RelPath: "b.md", Type: "note"})

	_, renameErr := node.Rename(
		root, nodeRepo, edgeRepo,
		index.NewFileStateRepo(store), "test-worker", time.Minute,
		manifest.EdgeTypes{}, nil, nil, "a", "b.md",
	)

	if renameErr == nil {
		test.Fatalf("expected error renaming over existing target")
	}
}

// wikilinkEdgeTypes returns the edge-type set used by the wikilink rename
// tests: a single "references" type with wikilinks = true, mirroring the
// common workspace manifest shape.
func wikilinkEdgeTypes() manifest.EdgeTypes {
	return manifest.EdgeTypes{
		"references": manifest.EdgeType{
			From: []string{"*"}, To: []string{"*"},
			Cardinality: manifest.CardinalityManyToMany,
			Wikilinks:   true,
		},
	}
}

// Rename must rewrite body [[wikilinks]] in referring files on disk — not
// just YAML frontmatter — and its index re-derive must keep the referring
// file's wikilink-derived file-level edge, retargeted at the new id.
// Regression test: rename left `[[old-id]]` on disk and dropped the
// referrer's file-level references edge entirely.
func TestRename_RewritesBodyWikilinksOnDisk(test *testing.T) {
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

	if _, refErr := service.Create(node.CreateInput{
		RelPath: "notes/referrer.md", Type: "note", Title: "Referrer",
		Body: []byte("see [[notes/target]] for context\n\n```\n[[notes/target]] in code is rewritten too\n```\n"),
	}); refErr != nil {
		test.Fatalf("create referrer: %v", refErr)
	}

	// Seed the wikilink-derived file-level edge the way the reindex worker
	// would have written it.
	if upsertErr := edgeRepo.UpsertAll("notes/referrer", "notes/referrer.md", []index.EdgeRow{{
		Type: "references", SourceID: "notes/referrer", TargetID: "notes/target",
		SourcePath: "notes/referrer.md", Kind: "direct",
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

	content, _ := os.ReadFile(filepath.Join(root, "notes/referrer.md"))

	if !strings.Contains(string(content), "see [[notes/renamed]] for context") {
		test.Errorf("body wikilink not rewritten:\n%s", string(content))
	}

	// Fenced code is rewritten too: the sub-unit pipeline derives edges
	// from code-block content (fence markers are absent from unit text),
	// so leaving `[[old]]` inside a fence would flip the code-block and
	// section edges back to the dead id on the next re-parse.
	if !strings.Contains(string(content), "```\n[[notes/renamed]] in code is rewritten too\n```") {
		test.Errorf("fenced code block not rewritten:\n%s", string(content))
	}

	referrerEdges, _ := edgeRepo.ListBySource("notes/referrer")

	var referencesTargets []string

	for _, edge := range referrerEdges {
		if edge.Type == "references" {
			referencesTargets = append(referencesTargets, edge.TargetID)
		}
	}

	if len(referencesTargets) != 1 || referencesTargets[0] != "notes/renamed" {
		test.Errorf("referrer file-level references = %v, want [notes/renamed]", referencesTargets)
	}
}

// #690: Rename must rewrite an Obsidian aliased body wikilink
// `[[old|display]]` on disk — retargeting the id while preserving the display
// text — and keep the referrer's derived file-level edge. Regression test: the
// alias form matched neither the extractor nor the rewriter, so the link
// derived no edge (the referrer never appeared in affected files) and was left
// as a dead link on disk after the target moved.
func TestRename_RewritesAliasedBodyWikilinkOnDisk(test *testing.T) {
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

	if _, refErr := service.Create(node.CreateInput{
		RelPath: "notes/referrer.md", Type: "note", Title: "Referrer",
		Body: []byte("see [[notes/target|the target]] for context\n"),
	}); refErr != nil {
		test.Fatalf("create referrer: %v", refErr)
	}

	// Seed the aliased-wikilink-derived file-level edge the way the reindex
	// worker (now alias-aware) would have written it.
	if upsertErr := edgeRepo.UpsertAll("notes/referrer", "notes/referrer.md", []index.EdgeRow{{
		Type: "references", SourceID: "notes/referrer", TargetID: "notes/target",
		SourcePath: "notes/referrer.md", Kind: "direct",
	}}); upsertErr != nil {
		test.Fatalf("seed referrer edge: %v", upsertErr)
	}

	plan, renameErr := node.Rename(
		root, nodeRepo, edgeRepo,
		index.NewFileStateRepo(store), "test-worker", time.Minute,
		edgeTypes, nil, nil, "notes/target", "notes/renamed.md",
	)
	if renameErr != nil {
		test.Fatalf("Rename: %v", renameErr)
	}

	if len(plan.AffectedFiles) != 1 || plan.AffectedFiles[0] != "notes/referrer.md" {
		test.Errorf("AffectedFiles = %v, want [notes/referrer.md]", plan.AffectedFiles)
	}

	content, _ := os.ReadFile(filepath.Join(root, "notes/referrer.md"))

	if !strings.Contains(string(content), "see [[notes/renamed|the target]] for context") {
		test.Errorf("aliased body wikilink not rewritten:\n%s", string(content))
	}

	referrerEdges, _ := edgeRepo.ListBySource("notes/referrer")

	var referencesTargets []string

	for _, edge := range referrerEdges {
		if edge.Type == "references" {
			referencesTargets = append(referencesTargets, edge.TargetID)
		}
	}

	if len(referencesTargets) != 1 || referencesTargets[0] != "notes/renamed" {
		test.Errorf("referrer file-level references = %v, want [notes/renamed]", referencesTargets)
	}
}

// Rename must retarget incoming edges whose SOURCE is a sub-unit of another
// file, and incoming edges that TARGET a sub-unit id of the moved file.
// Regression test: only file-level rows were re-derived; sub-unit-sourced
// rows kept the old target forever (permanent doctor dangling-edge noise).
func TestRename_RetargetsSubUnitSourcedIncomingEdges(test *testing.T) {
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

	if _, refErr := service.Create(node.CreateInput{
		RelPath: "notes/referrer.md", Type: "note", Title: "Referrer",
		Body: []byte("# Section\n\nsee [[notes/target]]\n"),
	}); refErr != nil {
		test.Fatalf("create referrer: %v", refErr)
	}

	// Seed a sub-unit row of the referrer plus its outbound edge, the way
	// the sub-unit sync would have written them.
	subRow := index.NodeRow{
		ID: "notes/referrer#S1", Type: "section", Path: "notes/referrer.md",
		Title: "Section", PropertiesJSON: `{}`, LastMtime: 1, LastSize: 1, LastChecksum: "h",
		ParentID: sql.NullString{String: "notes/referrer", Valid: true},
	}

	if upsertErr := nodeRepo.BulkUpsert([]index.NodeRow{subRow}, "markdown"); upsertErr != nil {
		test.Fatalf("seed sub-unit row: %v", upsertErr)
	}

	if upsertErr := edgeRepo.UpsertAll("notes/referrer#S1", "notes/referrer.md", []index.EdgeRow{{
		Type: "references", SourceID: "notes/referrer#S1", TargetID: "notes/target",
		SourcePath: "notes/referrer.md", Kind: "direct",
	}}); upsertErr != nil {
		test.Fatalf("seed sub-unit edge: %v", upsertErr)
	}

	// A third file targets a sub-unit id OF the moved file — through
	// frontmatter and through a body wikilink, the two shapes a real
	// workspace produces. Its file-level edges are seeded the way the
	// worker would have derived them.
	if _, thirdErr := service.Create(node.CreateInput{
		RelPath: "notes/third.md", Type: "note", Title: "Third",
		Properties: map[string]any{"references": "notes/target#S1"},
		Body:       []byte("deep link to [[notes/target#S1P1]]\n"),
	}); thirdErr != nil {
		test.Fatalf("create third: %v", thirdErr)
	}

	if upsertErr := edgeRepo.UpsertAll("notes/third", "notes/third.md", []index.EdgeRow{
		{Type: "references", SourceID: "notes/third", TargetID: "notes/target#S1",
			SourcePath: "notes/third.md", Kind: "direct"},
		{Type: "references", SourceID: "notes/third", TargetID: "notes/target#S1P1",
			SourcePath: "notes/third.md", Kind: "direct"},
	}); upsertErr != nil {
		test.Fatalf("seed sub-unit-targeted edges: %v", upsertErr)
	}

	if _, renameErr := node.Rename(
		root, nodeRepo, edgeRepo,
		index.NewFileStateRepo(store), "test-worker", time.Minute,
		edgeTypes, nil, nil, "notes/target", "notes/renamed.md",
	); renameErr != nil {
		test.Fatalf("Rename: %v", renameErr)
	}

	subEdges, _ := edgeRepo.ListBySource("notes/referrer#S1")

	if len(subEdges) != 1 || subEdges[0].TargetID != "notes/renamed" {
		test.Errorf("sub-unit-sourced edge = %+v, want single edge targeting notes/renamed", subEdges)
	}

	for _, oldTargetID := range []string{"notes/target", "notes/target#S1", "notes/target#S1P1"} {
		if stale, _ := edgeRepo.ListByTarget(oldTargetID); len(stale) != 0 {
			test.Errorf("edges still target %s: %+v", oldTargetID, stale)
		}
	}

	for _, newTargetID := range []string{"notes/renamed#S1", "notes/renamed#S1P1"} {
		if listed, _ := edgeRepo.ListByTarget(newTargetID); len(listed) != 1 {
			test.Errorf("sub-unit-targeted edge not retargeted to %s, got %+v", newTargetID, listed)
		}
	}

	thirdContent, _ := os.ReadFile(filepath.Join(root, "notes/third.md"))

	// The rewritten value is double-quoted: `#` would otherwise start a
	// YAML comment in a plain scalar.
	if !strings.Contains(string(thirdContent), `references: "notes/renamed#S1"`) {
		test.Errorf("third frontmatter sub-unit ref not rewritten:\n%s", string(thirdContent))
	}

	if !strings.Contains(string(thirdContent), "[[notes/renamed#S1P1]]") {
		test.Errorf("third body sub-unit wikilink not rewritten:\n%s", string(thirdContent))
	}
}

// After a rename, the destination's file_state must NOT claim the moved
// file's current mtime+size as already observed: the moved file's sub-unit
// rows were dropped with the old id, and the incremental reindex walk skips
// files whose recorded mtime+size match disk — which would leave the moved
// file without sub-units forever.
func TestRename_LeavesDestinationStaleSoReindexRebuildsSubUnits(test *testing.T) {
	root := test.TempDir()

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)
	fileState := index.NewFileStateRepo(store)

	service := node.NewServiceWithLease(
		root, nodeRepo, edgeRepo, manifest.EdgeTypes{}, nil,
		fileState, "test-worker", time.Minute,
	)

	if _, createErr := service.Create(node.CreateInput{
		RelPath: "notes/moved.md", Type: "note", Title: "Moved",
		Body: []byte("# Heading\n\nbody\n"),
	}); createErr != nil {
		test.Fatalf("create: %v", createErr)
	}

	if _, renameErr := node.Rename(
		root, nodeRepo, edgeRepo, fileState, "test-worker", time.Minute,
		manifest.EdgeTypes{}, nil, nil, "notes/moved", "notes/dest.md",
	); renameErr != nil {
		test.Fatalf("Rename: %v", renameErr)
	}

	stat, statErr := os.Stat(filepath.Join(root, "notes/dest.md"))

	if statErr != nil {
		test.Fatalf("stat dest: %v", statErr)
	}

	state, getErr := fileState.Get("notes/dest.md")

	if getErr != nil {
		test.Fatalf("file_state get: %v", getErr)
	}

	if state.MtimeNs == stat.ModTime().UnixNano() && state.Size == stat.Size() {
		test.Errorf("destination file_state claims current disk mtime+size; the next reindex would skip the moved file and never rebuild its sub-units")
	}
}

// A note whose body wikilinks itself must survive its own rename: the
// referring file IS the moved file, which now lives at the new path.
// Regression test: the rewrite loop read the referrer at its recorded
// source_path — the old path, already renamed away — and errored.
func TestRename_RewritesSelfReferenceInMovedFileBody(test *testing.T) {
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

	if _, createErr := service.Create(node.CreateInput{
		RelPath: "notes/self.md", Type: "note", Title: "Self",
		Body: []byte("I reference [[notes/self]] in my own body\n"),
	}); createErr != nil {
		test.Fatalf("create: %v", createErr)
	}

	if upsertErr := edgeRepo.UpsertAll("notes/self", "notes/self.md", []index.EdgeRow{{
		Type: "references", SourceID: "notes/self", TargetID: "notes/self",
		SourcePath: "notes/self.md", Kind: "direct",
	}}); upsertErr != nil {
		test.Fatalf("seed self edge: %v", upsertErr)
	}

	plan, renameErr := node.Rename(
		root, nodeRepo, edgeRepo,
		index.NewFileStateRepo(store), "test-worker", time.Minute,
		edgeTypes, nil, nil, "notes/self", "notes/renamed-self.md",
	)

	if renameErr != nil {
		test.Fatalf("Rename: %v", renameErr)
	}

	content, _ := os.ReadFile(filepath.Join(root, "notes/renamed-self.md"))

	if !strings.Contains(string(content), "[[notes/renamed-self]]") {
		test.Errorf("self wikilink not rewritten:\n%s", string(content))
	}

	// The plan must report where the touched file lives NOW — MCP clients
	// re-read affected_files, and the old path no longer exists.
	if len(plan.AffectedFiles) != 1 || plan.AffectedFiles[0] != "notes/renamed-self.md" {
		test.Errorf("AffectedFiles = %v, want [notes/renamed-self.md]", plan.AffectedFiles)
	}
}

// The moved file's own outgoing frontmatter edges must survive the rename in
// the index immediately — not only after the next reindex. Regression test:
// the outgoing-edge rebase listed edges AFTER DeleteByPath had already
// cascaded them away, so the rebase never copied anything.
func TestRename_KeepsMovedFilesOutgoingEdges(test *testing.T) {
	root := test.TempDir()

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	nodeRepo := index.NewNodeRepo(store)
	edgeRepo := index.NewEdgeRepo(store)

	edgeTypes := manifest.EdgeTypes{
		"parent": manifest.EdgeType{From: []string{"ticket"}, To: []string{"ticket"}, Cardinality: manifest.CardinalityManyToOne},
	}

	service := node.NewServiceWithLease(
		root, nodeRepo, edgeRepo, edgeTypes, nil,
		index.NewFileStateRepo(store), "test-worker", time.Minute,
	)

	if _, parentErr := service.Create(node.CreateInput{
		RelPath: "tickets/parent.md", Type: "ticket", Title: "Parent",
	}); parentErr != nil {
		test.Fatalf("create parent: %v", parentErr)
	}

	if _, childErr := service.Create(node.CreateInput{
		RelPath:    "tickets/child.md",
		Type:       "ticket",
		Title:      "Child",
		Properties: map[string]any{"parent": "tickets/parent"},
	}); childErr != nil {
		test.Fatalf("create child: %v", childErr)
	}

	if _, renameErr := node.Rename(
		root, nodeRepo, edgeRepo,
		index.NewFileStateRepo(store), "test-worker", time.Minute,
		edgeTypes, nil, nil, "tickets/child", "tickets/renamed-child.md",
	); renameErr != nil {
		test.Fatalf("Rename: %v", renameErr)
	}

	outgoing, _ := edgeRepo.ListBySource("tickets/renamed-child")

	if len(outgoing) != 1 || outgoing[0].Type != "parent" || outgoing[0].TargetID != "tickets/parent" {
		test.Errorf("outgoing = %+v, want single parent edge to tickets/parent", outgoing)
	}
}

// Structural `contains` edges must NOT be re-based across a rename: their
// sub-unit target rows were dropped with the old path and only the next
// re-parse recreates them — a re-based contains edge would dangle at a
// not-yet-existing sub-unit id until then. The sub-unit sync owns them.
func TestRename_DoesNotRebaseStructuralContainsEdges(test *testing.T) {
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

	if _, createErr := service.Create(node.CreateInput{
		RelPath: "notes/moved.md", Type: "note", Title: "Moved",
		Body: []byte("# Heading\n\nbody\n"),
	}); createErr != nil {
		test.Fatalf("create: %v", createErr)
	}

	// Seed the sub-unit row and its structural contains edge the way the
	// sub-unit sync would have written them.
	subRow := index.NodeRow{
		ID: "notes/moved#S1", Type: "section", Path: "notes/moved.md",
		Title: "Heading", PropertiesJSON: `{}`, LastMtime: 1, LastSize: 1, LastChecksum: "h",
		ParentID: sql.NullString{String: "notes/moved", Valid: true},
	}

	if upsertErr := nodeRepo.BulkUpsert([]index.NodeRow{subRow}, "markdown"); upsertErr != nil {
		test.Fatalf("seed sub-unit row: %v", upsertErr)
	}

	if insertErr := edgeRepo.InsertIgnore([]index.EdgeRow{{
		Type: "contains", SourceID: "notes/moved", TargetID: "notes/moved#S1",
		SourcePath: "notes/moved.md", Kind: "structural",
		Source: sql.NullString{String: "markdown", Valid: true},
	}}); insertErr != nil {
		test.Fatalf("seed contains edge: %v", insertErr)
	}

	if _, renameErr := node.Rename(
		root, nodeRepo, edgeRepo,
		index.NewFileStateRepo(store), "test-worker", time.Minute,
		edgeTypes, nil, nil, "notes/moved", "notes/dest.md",
	); renameErr != nil {
		test.Fatalf("Rename: %v", renameErr)
	}

	for _, edge := range mustListBySource(test, edgeRepo, "notes/dest") {
		if edge.Kind == "structural" {
			test.Errorf("structural edge re-based across rename: %+v", edge)
		}
	}
}

func mustListBySource(test *testing.T, edgeRepo *index.EdgeRepo, sourceID string) []index.EdgeRow {
	test.Helper()

	listed, listErr := edgeRepo.ListBySource(sourceID)

	if listErr != nil {
		test.Fatalf("ListBySource(%s): %v", sourceID, listErr)
	}

	return listed
}
