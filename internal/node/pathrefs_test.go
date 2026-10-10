package node_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/linenum"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/node"
)

func pathRefEdgeTypes() manifest.EdgeTypes {
	return manifest.EdgeTypes{
		"describes": {From: []string{"technical"}, Cardinality: manifest.CardinalityManyToMany, Paths: true},
		"related":   {From: []string{"*"}, To: []string{"*"}, Cardinality: manifest.CardinalityManyToMany},
	}
}

func newPathRefService(test *testing.T) (*node.Service, *index.NodeRepo, *index.EdgeRepo, *index.FileStateRepo, string) {
	test.Helper()

	root := test.TempDir()

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	nodes := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)
	fileState := index.NewFileStateRepo(store)

	service := node.NewServiceWithDeps(node.ServiceDeps{
		WorkspaceRoot: root,
		Repo:          nodes,
		Edges:         edges,
		EdgeTypes:     pathRefEdgeTypes(),
		FileState:     fileState,
		WorkerID:      "test-worker",
		LeaseTTL:      time.Minute,
		LineNumbering: linenum.SchemeLF,
	})

	return service, nodes, edges, fileState, root
}

func pathRefLines(test *testing.T, edges *index.EdgeRepo, id string) []string {
	test.Helper()

	refs, listErr := edges.PathRefsFrom([]string{id})

	if listErr != nil {
		test.Fatalf("PathRefsFrom: %v", listErr)
	}

	rendered := make([]string, 0, len(refs))

	for _, ref := range refs {
		rendered = append(rendered, fmt.Sprintf("%s:%d", ref.Target, ref.Line))
	}

	return rendered
}

func TestService_WritesKeepPathRefsInStep(test *testing.T) {
	service, nodes, edges, fileState, root := newPathRefService(test)

	// type, title and the two delimiters put the body's first line at 5.
	if _, createErr := service.Create(node.CreateInput{
		RelPath: "technical/ledger.md",
		Type:    "technical",
		Title:   "Ledger",
		Body:    []byte("Lives in `server/ledger/service.go`.\n"),
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	if _, createErr := service.Create(node.CreateInput{RelPath: "notes/other.md", Type: "note", Body: []byte("`server/x.go`\n")}); createErr != nil {
		test.Fatalf("Create note: %v", createErr)
	}

	created := pathRefLines(test, edges, "technical/ledger")

	if len(created) != 1 {
		test.Fatalf("after Create = %v, want one ref", created)
	}

	if got := pathRefLines(test, edges, "notes/other"); len(got) != 0 {
		test.Errorf("a note is outside describes' from, got %v", got)
	}

	if _, modifyErr := service.Modify(node.ModifyInput{ID: "technical/ledger", SetProps: map[string]any{"owner": "ledger-team"}}); modifyErr != nil {
		test.Fatalf("Modify: %v", modifyErr)
	}

	modified := pathRefLines(test, edges, "technical/ledger")

	if len(modified) != 1 || modified[0] == created[0] {
		test.Errorf("after a frontmatter Modify = %v, want the line moved from %v", modified, created)
	}

	if addErr := service.AddEdge("related", "technical/ledger", "notes/other"); addErr != nil {
		test.Fatalf("AddEdge: %v", addErr)
	}

	added := pathRefLines(test, edges, "technical/ledger")

	if len(added) != 1 || added[0] == modified[0] {
		test.Errorf("after AddEdge = %v, want the line moved from %v", added, modified)
	}

	if _, renameErr := node.Rename(
		root, nodes, edges, fileState, "test-worker", time.Minute,
		pathRefEdgeTypes(), nil, nil, linenum.DefaultScheme, "technical/ledger", "technical/ledger-core.md",
	); renameErr != nil {
		test.Fatalf("Rename: %v", renameErr)
	}

	if got := pathRefLines(test, edges, "technical/ledger-core"); !slices.Equal(got, added) {
		test.Errorf("after Rename = %v, want %v carried over", got, added)
	}

	if got := pathRefLines(test, edges, "technical/ledger"); len(got) != 0 {
		test.Errorf("old id still has refs: %v", got)
	}
}

func TestService_MoveRederivesRelativeLinks(test *testing.T) {
	service, nodes, edges, fileState, root := newPathRefService(test)

	if _, createErr := service.Create(node.CreateInput{
		RelPath: "technical/ledger.md",
		Type:    "technical",
		Title:   "Ledger",
		Body:    []byte("See [the service](../server/x.go) and `server/y.go`.\n"),
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	if got := pathRefLines(test, edges, "technical/ledger"); !slices.Equal(got, []string{"server/x.go:6", "server/y.go:6"}) {
		test.Fatalf("after Create = %v", got)
	}

	if _, renameErr := node.Rename(
		root, nodes, edges, fileState, "test-worker", time.Minute,
		pathRefEdgeTypes(), nil, nil, linenum.SchemeLF, "technical/ledger", "technical/deep/ledger.md",
	); renameErr != nil {
		test.Fatalf("Rename: %v", renameErr)
	}

	// The link now resolves against technical/deep; the code span is
	// workspace-relative and stays put.
	if got := pathRefLines(test, edges, "technical/deep/ledger"); !slices.Equal(got, []string{"server/y.go:6", "technical/server/x.go:6"}) {
		test.Errorf("after Rename = %v", got)
	}
}

func TestResolveEdges_PathsOnlyValuesAreWorkspacePaths(test *testing.T) {
	edgeTypes := manifest.EdgeTypes{
		"describes": {From: []string{"technical"}, Cardinality: manifest.CardinalityManyToMany, Paths: true},
		"mentions":  {From: []string{"*"}, To: []string{"*"}, Cardinality: manifest.CardinalityManyToMany, Paths: true},
	}

	parsed := &node.Node{ID: "technical/a", Path: "technical/a.md", Type: "technical", Properties: map[string]any{
		"describes": []any{"server/a.go", "./server/b.go", "server/a.go", "https://x.dev/a.go", "/abs.go"},
		"mentions":  "notes/x",
	}}

	if resolveErr := node.ResolveEdges(parsed, edgeTypes); resolveErr != nil {
		test.Fatalf("ResolveEdges: %v", resolveErr)
	}

	if got := parsed.PathValues["describes"]; !slices.Equal(got, []string{"server/a.go", "server/b.go"}) {
		test.Errorf("describes paths = %v", got)
	}

	// A value that isn't a workspace path stays an edge, for doctor to report.
	if got := parsed.Edges["describes"]; !slices.Equal(got, []string{"https://x.dev/a.go", "/abs.go"}) {
		test.Errorf("describes edges = %v", got)
	}

	// mentions declares to, so its values are node ids as before.
	if got := parsed.Edges["mentions"]; !slices.Equal(got, []string{"notes/x"}) || parsed.PathValues["mentions"] != nil {
		test.Errorf("mentions edges = %v, paths = %v", got, parsed.PathValues["mentions"])
	}

	if _, kept := parsed.Properties["describes"]; kept {
		test.Errorf("describes left in properties")
	}

	// A page whose type the edge type doesn't allow keeps the edge, where the
	// from check reports it.
	note := &node.Node{ID: "notes/b", Path: "notes/b.md", Type: "note", Properties: map[string]any{"describes": "server/a.go"}}

	if resolveErr := node.ResolveEdges(note, edgeTypes); resolveErr != nil {
		test.Fatalf("ResolveEdges note: %v", resolveErr)
	}

	if !slices.Equal(note.Edges["describes"], []string{"server/a.go"}) || note.PathValues != nil {
		test.Errorf("note edges = %v, paths = %v", note.Edges, note.PathValues)
	}
}

func TestPathRefs_FrontmatterPathsCarryTheirLines(test *testing.T) {
	cases := map[string]struct {
		file string
		want []string
	}{
		"block sequence": {
			file: "---\n" + // 1
				"type: technical\n" + // 2
				"describes:\n" + // 3
				"  - server/ledger/core/service.go\n" + // 4
				"  - \"server/ledger/\"\n" + // 5
				"---\n" + // 6
				"Body names `server/wire.go`.\n", // 7
			want: []string{"server/ledger/core/service.go:4", "server/ledger:5", "server/wire.go:7"},
		},
		"flow sequence after a BOM": {
			file: "\xef\xbb\xbf---\ntype: technical\n\ndescribes: [server/a.go, Makefile]\n---\n",
			want: []string{"Makefile:4", "server/a.go:4"},
		},
		"scalar": {
			file: "---\ntype: technical\ndescribes: server/a.go\n---\n",
			want: []string{"server/a.go:3"},
		},
	}

	for name, each := range cases {
		parsed, parseErr := node.ParseFile("technical/ledger.md", []byte(each.file))

		if parseErr != nil {
			test.Fatalf("%s: ParseFile: %v", name, parseErr)
		}

		if resolveErr := node.ResolveEdges(parsed, pathRefEdgeTypes()); resolveErr != nil {
			test.Fatalf("%s: ResolveEdges: %v", name, resolveErr)
		}

		var got []string

		for _, row := range node.PathRefs(parsed, []byte(each.file), pathRefEdgeTypes(), linenum.SchemeLF) {
			got = append(got, fmt.Sprintf("%s:%d", row.Target, row.Line))
		}

		slices.Sort(got)

		if !slices.Equal(got, each.want) {
			test.Errorf("%s: PathRefs = %v, want %v", name, got, each.want)
		}
	}
}

func TestPathRefs_HTMLLinksImagesAndMeta(test *testing.T) {
	page := `<html><head><meta name="tusk:type" content="technical">` +
		`<meta name="tusk:describes" content="server/a.go"></head><body>` +
		`<p><a href="../server/b.go">b</a> <img src="img/c.png"> <code>server/d.go</code>` +
		` <a href="https://x.dev">x</a> <a href="#top">top</a></p></body></html>`

	parsed, parseErr := node.ParseContentFile("site/page.html", []byte(page))

	if parseErr != nil {
		test.Fatalf("ParseContentFile: %v", parseErr)
	}

	if resolveErr := node.ResolveEdges(parsed, pathRefEdgeTypes()); resolveErr != nil {
		test.Fatalf("ResolveEdges: %v", resolveErr)
	}

	var got []string

	for _, row := range node.PathRefs(parsed, []byte(page), pathRefEdgeTypes(), linenum.SchemeLF) {
		got = append(got, fmt.Sprintf("%s:%d", row.Target, row.Line))
	}

	slices.Sort(got)

	want := []string{"server/a.go:0", "server/b.go:0", "server/d.go:0", "site/img/c.png:0"}

	if !slices.Equal(got, want) {
		test.Errorf("PathRefs = %v, want %v", got, want)
	}
}

func TestService_EdgeAddOnAPathsOnlyTypeRecordsAPath(test *testing.T) {
	service, _, edges, _, _ := newPathRefService(test)

	if _, createErr := service.Create(node.CreateInput{RelPath: "technical/ledger.md", Type: "technical", Title: "Ledger"}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	if addErr := service.AddEdge("describes", "technical/ledger", "server/ledger/core/service.go"); addErr != nil {
		test.Fatalf("AddEdge: %v", addErr)
	}

	refs, listErr := edges.PathRefsFrom([]string{"technical/ledger"})

	if listErr != nil {
		test.Fatalf("PathRefsFrom: %v", listErr)
	}

	if len(refs) != 1 || refs[0].Target != "server/ledger/core/service.go" || refs[0].Line < 2 {
		test.Fatalf("after AddEdge refs = %+v, want the path on its frontmatter line", refs)
	}

	rows, rowsErr := edges.ListBySource("technical/ledger")

	if rowsErr != nil {
		test.Fatalf("ListBySource: %v", rowsErr)
	}

	for _, row := range rows {
		if row.Type == "describes" {
			test.Errorf("a path value became an edge row: %+v", row)
		}
	}

	if addErr := service.AddEdge("describes", "technical/ledger", "https://x.dev/a.go"); addErr == nil {
		test.Errorf("AddEdge of a URL = nil, want a not-a-path error")
	}

	if removeErr := service.RemoveEdge("describes", "technical/ledger", "server/ledger/core/service.go"); removeErr != nil {
		test.Fatalf("RemoveEdge: %v", removeErr)
	}

	if got := pathRefLines(test, edges, "technical/ledger"); len(got) != 0 {
		test.Errorf("after RemoveEdge = %v, want none", got)
	}
}

func TestRename_LeavesPathsOnlyValuesAsWritten(test *testing.T) {
	service, nodes, edges, fileState, root := newPathRefService(test)

	if _, createErr := service.Create(node.CreateInput{RelPath: "notes/foo.md", Type: "note", Title: "Foo"}); createErr != nil {
		test.Fatalf("Create note: %v", createErr)
	}

	if _, createErr := service.Create(node.CreateInput{
		RelPath:    "technical/ledger.md",
		Type:       "technical",
		Title:      "Ledger",
		Properties: map[string]any{"describes": []any{"notes/foo"}, "related": "notes/foo"},
	}); createErr != nil {
		test.Fatalf("Create technical: %v", createErr)
	}

	if _, renameErr := node.Rename(
		root, nodes, edges, fileState, "test-worker", time.Minute,
		pathRefEdgeTypes(), nil, nil, linenum.SchemeLF, "notes/foo", "notes/bar.md",
	); renameErr != nil {
		test.Fatalf("Rename: %v", renameErr)
	}

	content, readErr := os.ReadFile(filepath.Join(root, "technical/ledger.md"))

	if readErr != nil {
		test.Fatalf("read: %v", readErr)
	}

	if !strings.Contains(string(content), "related: notes/bar") {
		test.Errorf("the related edge was not retargeted:\n%s", content)
	}

	if !strings.Contains(string(content), "- notes/foo") {
		test.Errorf("the describes path was rewritten:\n%s", content)
	}
}
