package node_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
	"github.com/germanamz/tusk/internal/reindex"
	"github.com/germanamz/tusk/internal/wikilink"
)

// adoptedFixture is a workspace whose plain markdown is indexed under
// `default-type = "note"` (#790), with the service the CLI and MCP build.
type adoptedFixture struct {
	root    string
	store   *index.Index
	loaded  *manifest.Manifest
	service *node.Service
}

func newAdoptedFixture(test *testing.T, files map[string]string) adoptedFixture {
	test.Helper()

	root := test.TempDir()

	for relPath, content := range files {
		absPath := filepath.Join(root, relPath)

		if mkErr := os.MkdirAll(filepath.Dir(absPath), 0o755); mkErr != nil {
			test.Fatalf("mkdir: %v", mkErr)
		}

		if writeErr := os.WriteFile(absPath, []byte(content), 0o644); writeErr != nil {
			test.Fatalf("write %s: %v", relPath, writeErr)
		}
	}

	edgeTypes := manifest.EdgeTypes{
		"references": {From: []string{"*"}, To: []string{"*"}, Cardinality: manifest.CardinalityManyToMany, Wikilinks: true},
		"relates-to": {From: []string{"*"}, To: []string{"*"}, Cardinality: manifest.CardinalityManyToMany},
	}
	nodeTypes := map[string]manifest.NodeType{
		"note": {Properties: []manifest.PropertyDecl{{Name: "due", Type: "date"}}},
	}
	loaded := &manifest.Manifest{
		Workspace: manifest.WorkspaceSection{DefaultType: "note"},
		EdgeTypes: edgeTypes,
		NodeTypes: nodeTypes,
	}

	store := openTempIndex(test, root)

	if _, runErr := reindex.Run(reindex.Config{
		Root:       root,
		Repo:       index.NewNodeRepo(store),
		Edges:      index.NewEdgeRepo(store),
		EdgeTypes:  edgeTypes,
		NodeTypes:  nodeTypes,
		Manifest:   loaded,
		Meta:       index.NewMetaRepo(store),
		FileStates: index.NewFileStateRepo(store),
		EmbedQueue: index.NewEmbedQueueRepo(store),
		Workers:    2,
	}); runErr != nil {
		test.Fatalf("reindex: %v", runErr)
	}

	return adoptedFixture{
		root:    root,
		store:   store,
		loaded:  loaded,
		service: node.NewServiceWithDeps(node.DepsFromIndex(root, store, loaded, nil, nil)),
	}
}

func (fixture adoptedFixture) read(test *testing.T, relPath string) string {
	test.Helper()

	content, readErr := os.ReadFile(filepath.Join(fixture.root, relPath))

	if readErr != nil {
		test.Fatalf("read %s: %v", relPath, readErr)
	}

	return string(content)
}

func (fixture adoptedFixture) targets(test *testing.T, sourceID, edgeType string) []string {
	test.Helper()

	rows, listErr := index.NewEdgeRepo(fixture.store).ListBySource(sourceID)

	if listErr != nil {
		test.Fatalf("ListBySource: %v", listErr)
	}

	var targets []string

	for _, row := range rows {
		if row.Type == edgeType {
			targets = append(targets, row.TargetID)
		}
	}

	slices.Sort(targets)

	return targets
}

func TestAdopted_GetParsesAsTheIndexedType(test *testing.T) {
	fixture := newAdoptedFixture(test, map[string]string{"plain.md": "# Plain\n\nHello.\n"})

	got, getErr := fixture.service.Get("plain")

	if getErr != nil {
		test.Fatalf("Get: %v", getErr)
	}

	if got.Type != "note" || got.Title != "Plain" {
		test.Errorf("Type, Title = %q, %q; want note, Plain", got.Type, got.Title)
	}
}

func TestAdopted_BodyOnlyModifyLeavesNoFrontmatter(test *testing.T) {
	fixture := newAdoptedFixture(test, map[string]string{
		"plain.md":  "# Plain\n\nHello.\n",
		"target.md": "# Target\n",
	})

	modified, modifyErr := fixture.service.Modify(node.ModifyInput{
		ID:   "plain",
		Body: []byte("# Plain\n\nNow links [[target]].\n"),
	})

	if modifyErr != nil {
		test.Fatalf("Modify: %v", modifyErr)
	}

	if got := fixture.read(test, "plain.md"); got != "# Plain\n\nNow links [[target]].\n" {
		test.Errorf("file = %q; a body-only modify must not add frontmatter", got)
	}

	if modified.Type != "note" || !modified.Adopted {
		test.Errorf("Type, Adopted = %q, %v; want note, true", modified.Type, modified.Adopted)
	}

	if got := fixture.targets(test, "plain", "references"); !slices.Equal(got, []string{"target"}) {
		test.Errorf("references = %q, want the new wikilink", got)
	}
}

func TestAdopted_PropertyModifyWritesFrontmatterWithoutType(test *testing.T) {
	fixture := newAdoptedFixture(test, map[string]string{"plain.md": "# Plain\n\nHello.\n"})

	if _, modifyErr := fixture.service.Modify(node.ModifyInput{
		ID:       "plain",
		SetProps: map[string]any{"due": "2026-11-01"},
	}); modifyErr != nil {
		test.Fatalf("Modify: %v", modifyErr)
	}

	got := fixture.read(test, "plain.md")

	if !strings.Contains(got, "due: \"2026-11-01\"") || strings.Contains(got, "type:") {
		test.Errorf("file = %q; want the set property and no type", got)
	}

	row, getErr := index.NewNodeRepo(fixture.store).Get("plain")

	if getErr != nil {
		test.Fatalf("Get row: %v", getErr)
	}

	if row.Type != "note" || row.Title != "Plain" {
		test.Errorf("row Type, Title = %q, %q; want note, Plain", row.Type, row.Title)
	}
}

func TestAdopted_EdgeAddAndRemove(test *testing.T) {
	fixture := newAdoptedFixture(test, map[string]string{
		"plain.md": "---\ndue: 2026-11-01\n---\n# Plain\n",
		"other.md": "# Other\n",
	})

	if addErr := fixture.service.AddEdge("relates-to", "plain", "other"); addErr != nil {
		test.Fatalf("AddEdge: %v", addErr)
	}

	got := fixture.read(test, "plain.md")

	if strings.Contains(got, "type:") || !strings.Contains(got, "relates-to: other") {
		test.Errorf("file = %q; want the edge written and no type", got)
	}

	// The unquoted date is canonicalized against note's declared date
	// property, so it stays date-only rather than becoming a timestamp.
	if !strings.Contains(got, "due: \"2026-11-01\"") {
		test.Errorf("file = %q; want due re-emitted as a quoted date", got)
	}

	if targets := fixture.targets(test, "plain", "relates-to"); !slices.Equal(targets, []string{"other"}) {
		test.Errorf("relates-to = %q, want [other]", targets)
	}

	if removeErr := fixture.service.RemoveEdge("relates-to", "plain", "other"); removeErr != nil {
		test.Fatalf("RemoveEdge: %v", removeErr)
	}

	if targets := fixture.targets(test, "plain", "relates-to"); len(targets) != 0 {
		test.Errorf("relates-to after remove = %q, want none", targets)
	}

	row, getErr := index.NewNodeRepo(fixture.store).Get("plain")

	if getErr != nil {
		test.Fatalf("Get row: %v", getErr)
	}

	if row.Type != "note" {
		test.Errorf("row Type = %q after the edge round trip, want note", row.Type)
	}
}

func TestAdopted_MoveRewritesAdoptedReferrer(test *testing.T) {
	fixture := newAdoptedFixture(test, map[string]string{
		"plain.md":     "# Plain\n\nSee [[ideas/old]].\n",
		"ideas/old.md": "# Old idea\n",
	})

	if _, renameErr := node.Rename(
		fixture.root,
		index.NewNodeRepo(fixture.store),
		index.NewEdgeRepo(fixture.store),
		index.NewFileStateRepo(fixture.store),
		"test-worker",
		0,
		fixture.loaded.EdgeTypes,
		fixture.loaded.NodeTypes,
		nil,
		fixture.loaded.LineNumbering(), wikilink.DefaultCase,
		"ideas/old",
		"ideas/new.md",
	); renameErr != nil {
		test.Fatalf("Rename: %v", renameErr)
	}

	if got := fixture.read(test, "plain.md"); got != "# Plain\n\nSee [[ideas/new]].\n" {
		test.Errorf("referrer = %q; want the wikilink retargeted and no frontmatter added", got)
	}

	if got := fixture.targets(test, "plain", "references"); !slices.Equal(got, []string{"ideas/new"}) {
		test.Errorf("references = %q, want [ideas/new]", got)
	}

	row, getErr := index.NewNodeRepo(fixture.store).Get("ideas/new")

	if getErr != nil {
		test.Fatalf("Get moved row: %v", getErr)
	}

	if row.Type != "note" {
		test.Errorf("moved row Type = %q, want note", row.Type)
	}
}

// TestAdopted_SubUnitIDResolvesToTheFileType covers a get or modify addressed
// at one of an adopted file's sub-units: the file is what gets parsed, so the
// fallback must be the file node's type, not the sub-unit's (section,
// paragraph). Modify would otherwise re-persist the file row under it.
func TestAdopted_SubUnitIDResolvesToTheFileType(test *testing.T) {
	fixture := newAdoptedFixture(test, map[string]string{"plain.md": "# Plain\n\nHello.\n"})

	got, getErr := fixture.service.Get("plain#S1")

	if getErr != nil {
		test.Fatalf("Get: %v", getErr)
	}

	if got.ID != "plain" || got.Type != "note" {
		test.Errorf("Get(plain#S1) = %q typed %q; want the file node, typed note", got.ID, got.Type)
	}

	if _, modifyErr := fixture.service.Modify(node.ModifyInput{
		ID:       "plain#S1",
		SetProps: map[string]any{"due": "2026-11-01"},
	}); modifyErr != nil {
		test.Fatalf("Modify: %v", modifyErr)
	}

	row, rowErr := index.NewNodeRepo(fixture.store).Get("plain")

	if rowErr != nil {
		test.Fatalf("Get row: %v", rowErr)
	}

	if row.Type != "note" {
		test.Errorf("file row Type = %q after a modify through a sub-unit id, want note", row.Type)
	}
}
