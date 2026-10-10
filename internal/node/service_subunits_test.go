package node_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

// subUnitManifest is a vault with one wikilinks edge type. Loaded from disk so
// the toml metadata SubUnitsEnabled reads is present.
const subUnitManifest = `
[workspace]
name = "test"
%s

[node-types.note]

[edge-types.references]
from = ["*"]
to = ["*"]
cardinality = "many-to-many"
wikilinks = true
`

type subUnitFixture struct {
	service *node.Service
	nodes   *index.NodeRepo
	edges   *index.EdgeRepo
	queue   *index.EmbedQueueRepo
}

func newSubUnitService(test *testing.T, workspaceExtra string) subUnitFixture {
	test.Helper()

	root := test.TempDir()
	manifestPath := filepath.Join(root, "tusk.toml")

	if writeErr := os.WriteFile(manifestPath, fmt.Appendf(nil, subUnitManifest, workspaceExtra), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("manifest.Load: %v", loadErr)
	}

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	deps := node.DepsFromIndex(root, store, loaded, nil, nil)
	deps.LeaseTTL = time.Minute

	return subUnitFixture{
		service: node.NewServiceWithDeps(deps),
		nodes:   deps.Repo,
		edges:   deps.Edges,
		queue:   deps.EmbedQueue,
	}
}

// subUnitLines renders each sub-unit of fileID as "id lines start-end: payload".
func subUnitLines(test *testing.T, nodes *index.NodeRepo, fileID string) []string {
	test.Helper()

	rows, listErr := nodes.ListSubUnitsForFile(fileID)

	if listErr != nil {
		test.Fatalf("ListSubUnitsForFile: %v", listErr)
	}

	rendered := make([]string, 0, len(rows))

	for _, row := range rows {
		rendered = append(rendered, fmt.Sprintf("%s lines %d-%d: %s",
			row.ID, row.StartLine.Int64, row.EndLine.Int64, row.EmbedPayload.String))
	}

	return rendered
}

func edgesFrom(test *testing.T, edges *index.EdgeRepo, sourceID, edgeType string) []string {
	test.Helper()

	rows, listErr := edges.ListBySource(sourceID)

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

// The frontmatter (type + title) and its two delimiters plus the blank line
// after them put the body's first line at 6.
const subUnitBody = "# Top\n\nalpha [[notes/other]]\n\n## Second\n\nbeta\n"

func TestService_CreateSyncsSubUnits(test *testing.T) {
	fixture := newSubUnitService(test, "")

	if _, createErr := fixture.service.Create(node.CreateInput{
		RelPath: "notes/doc.md",
		Type:    "note",
		Title:   "Doc",
		Body:    []byte(subUnitBody),
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	want := []string{
		"notes/doc#S1 lines 6-12: Top\nalpha [[notes/other]]\nSecond\nbeta",
		"notes/doc#S1P1 lines 8-8: alpha [[notes/other]]",
		"notes/doc#S1.1 lines 10-12: Second\nbeta",
		"notes/doc#S1.1P1 lines 12-12: beta",
	}

	if got := subUnitLines(test, fixture.nodes, "notes/doc"); !slices.Equal(got, want) {
		test.Errorf("sub-units after Create:\n got %q\nwant %q", got, want)
	}

	contains := edgesFrom(test, fixture.edges, "notes/doc", "contains")

	if !slices.Equal(contains, []string{"notes/doc#S1", "notes/doc#S1.1", "notes/doc#S1.1P1", "notes/doc#S1P1"}) {
		test.Errorf("contains edges = %v, want one per sub-unit", contains)
	}

	if got := edgesFrom(test, fixture.edges, "notes/doc#S1P1", "references"); !slices.Equal(got, []string{"notes/other"}) {
		test.Errorf("paragraph wikilink edges = %v, want [notes/other]", got)
	}

	queued, queueErr := fixture.queue.ListNodeIDs()

	if queueErr != nil {
		test.Fatalf("ListNodeIDs: %v", queueErr)
	}

	for _, leaf := range []string{"notes/doc#S1P1", "notes/doc#S1.1P1"} {
		if !slices.Contains(queued, leaf) {
			test.Errorf("embed queue %v is missing leaf %s", queued, leaf)
		}
	}
}

func TestService_ModifyBodyResyncsSubUnits(test *testing.T) {
	fixture := newSubUnitService(test, "")

	if _, createErr := fixture.service.Create(node.CreateInput{
		RelPath: "notes/doc.md", Type: "note", Title: "Doc", Body: []byte(subUnitBody),
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	if _, modifyErr := fixture.service.Modify(node.ModifyInput{
		ID:   "notes/doc",
		Body: []byte("# Top\n\ngamma\n\n## Renamed\n\ndelta\n\nepsilon\n"),
	}); modifyErr != nil {
		test.Fatalf("Modify: %v", modifyErr)
	}

	want := []string{
		"notes/doc#S1 lines 6-14: Top\ngamma\nRenamed\ndelta\nepsilon",
		"notes/doc#S1P1 lines 8-8: gamma",
		"notes/doc#S1.1 lines 10-14: Renamed\ndelta\nepsilon",
		"notes/doc#S1.1P1 lines 12-12: delta",
		"notes/doc#S1.1P2 lines 14-14: epsilon",
	}

	if got := subUnitLines(test, fixture.nodes, "notes/doc"); !slices.Equal(got, want) {
		test.Errorf("sub-units after a body Modify:\n got %q\nwant %q", got, want)
	}

	if got := edgesFrom(test, fixture.edges, "notes/doc#S1P1", "references"); len(got) != 0 {
		test.Errorf("the replaced paragraph still links %v", got)
	}

	if got := edgesFrom(test, fixture.edges, "notes/doc", "contains"); len(got) != len(want) {
		test.Errorf("contains edges = %v, want %d", got, len(want))
	}
}

func TestService_FrontmatterModifyShiftsSubUnitLines(test *testing.T) {
	fixture := newSubUnitService(test, "")

	if _, createErr := fixture.service.Create(node.CreateInput{
		RelPath: "notes/doc.md", Type: "note", Title: "Doc", Body: []byte(subUnitBody),
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	// "owner" sorts after "title", so it lands on line 4 and pushes the body
	// down one line.
	if _, modifyErr := fixture.service.Modify(node.ModifyInput{
		ID:       "notes/doc",
		SetProps: map[string]any{"owner": "me"},
	}); modifyErr != nil {
		test.Fatalf("Modify: %v", modifyErr)
	}

	want := []string{
		"notes/doc#S1 lines 7-13: Top\nalpha [[notes/other]]\nSecond\nbeta",
		"notes/doc#S1P1 lines 9-9: alpha [[notes/other]]",
		"notes/doc#S1.1 lines 11-13: Second\nbeta",
		"notes/doc#S1.1P1 lines 13-13: beta",
	}

	if got := subUnitLines(test, fixture.nodes, "notes/doc"); !slices.Equal(got, want) {
		test.Errorf("sub-units after a frontmatter Modify:\n got %q\nwant %q", got, want)
	}

	if got := edgesFrom(test, fixture.edges, "notes/doc#S1P1", "references"); !slices.Equal(got, []string{"notes/other"}) {
		test.Errorf("paragraph wikilink edges = %v, want them kept", got)
	}
}

func TestService_SubUnitsDisabledWritesNone(test *testing.T) {
	fixture := newSubUnitService(test, "sub-units = false")

	if _, createErr := fixture.service.Create(node.CreateInput{
		RelPath: "notes/doc.md", Type: "note", Title: "Doc", Body: []byte(subUnitBody),
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	if _, modifyErr := fixture.service.Modify(node.ModifyInput{ID: "notes/doc", Body: []byte("# Other\n\ntext\n")}); modifyErr != nil {
		test.Fatalf("Modify: %v", modifyErr)
	}

	if got := subUnitLines(test, fixture.nodes, "notes/doc"); len(got) != 0 {
		test.Errorf("sub-units written with sub-units = false: %q", got)
	}

	if got := edgesFrom(test, fixture.edges, "notes/doc", "contains"); len(got) != 0 {
		test.Errorf("contains edges written with sub-units = false: %v", got)
	}
}
