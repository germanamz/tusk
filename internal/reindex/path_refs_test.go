package reindex_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/reindex"
)

// pathRefsFixture writes a technical page, a note and an HTML page that all
// name source paths, and opens an index beside them. writeNode puts four
// frontmatter lines above a markdown body (the last is blank).
func pathRefsFixture(test *testing.T) (string, *index.Index) {
	test.Helper()

	root := test.TempDir()

	writeNode(test, root, "technical/ledger.md", "type: technical\n",
		"# Ledger\n\nLives in `server/ledger/core/service.go`.\n\n- wired in `server/wire.go:12`\n")
	writeNode(test, root, "notes/aside.md", "type: note\n", "Mentions `server/wire.go` in passing.\n")
	writeNode(test, root, "technical/map.html", "",
		`<html><head><meta name="tusk:type" content="technical"></head>`+
			`<body><p>See <code>server/ledger/</code>.</p><pre><code>server/in/pre.go</code></pre></body></html>`)

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	return root, store
}

func runWithEdgeTypes(test *testing.T, root string, store *index.Index, edgeTypes manifest.EdgeTypes, nodeTypes map[string]manifest.NodeType) *reindex.Report {
	test.Helper()

	report, runErr := reindex.Run(withGen(store, reindex.Config{
		Root:      root,
		Repo:      index.NewNodeRepo(store),
		Edges:     index.NewEdgeRepo(store),
		EdgeTypes: edgeTypes,
		NodeTypes: nodeTypes,
		Manifest:  &manifest.Manifest{EdgeTypes: edgeTypes, NodeTypes: nodeTypes},
	}))

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	return report
}

func storedPathRefs(test *testing.T, store *index.Index) []string {
	test.Helper()

	refs, listErr := index.NewEdgeRepo(store).AllPathRefs()

	if listErr != nil {
		test.Fatalf("AllPathRefs: %v", listErr)
	}

	rendered := make([]string, 0, len(refs))

	for _, ref := range refs {
		rendered = append(rendered, fmt.Sprintf("%s %s %s:%d", ref.SourceID, ref.Type, ref.Target, ref.Line))
	}

	return rendered
}

var describesTechnical = manifest.EdgeTypes{
	"describes": {From: []string{"technical"}, Cardinality: manifest.CardinalityManyToMany, Paths: true},
}

func TestRun_RecordsPathRefsForPagesInFrom(test *testing.T) {
	root, store := pathRefsFixture(test)

	runWithEdgeTypes(test, root, store, describesTechnical, nil)

	got := storedPathRefs(test, store)
	want := []string{
		"technical/ledger describes server/ledger/core/service.go:7",
		"technical/ledger describes server/wire.go:9",
		"technical/map.html describes server/ledger:0",
	}

	if !slices.Equal(got, want) {
		test.Errorf("path refs =\n%v\nwant\n%v", got, want)
	}
}

func TestRun_PathRefsFollowTheConfiguration(test *testing.T) {
	root, store := pathRefsFixture(test)

	runWithEdgeTypes(test, root, store, manifest.EdgeTypes{}, nil)

	if got := storedPathRefs(test, store); len(got) != 0 {
		test.Fatalf("refs with no paths edge type = %v, want none", got)
	}

	// Turning paths on for an unchanged vault fills the refs on a plain run.
	runWithEdgeTypes(test, root, store, describesTechnical, nil)

	if got := storedPathRefs(test, store); len(got) != 3 {
		test.Fatalf("refs after enabling = %v, want 3", got)
	}

	// Widening from re-scopes the same unchanged files.
	everyone := manifest.EdgeTypes{
		"describes": {From: []string{"*"}, Cardinality: manifest.CardinalityManyToMany, Paths: true},
	}

	runWithEdgeTypes(test, root, store, everyone, nil)

	if got := storedPathRefs(test, store); !slices.Contains(got, "notes/aside describes server/wire.go:5") {
		test.Fatalf("refs after widening from = %v, want the note's ref", got)
	}

	marker, _ := index.NewMetaRepo(store).Get("path_refs")

	if !strings.Contains(marker, "describes=*") {
		test.Errorf("path_refs marker = %q, want the from list recorded", marker)
	}

	// Turning paths off drops every ref.
	runWithEdgeTypes(test, root, store, manifest.EdgeTypes{}, nil)

	if got := storedPathRefs(test, store); len(got) != 0 {
		test.Errorf("refs after disabling = %v, want none", got)
	}
}

// TestRun_PathRefLinesSurviveDateSelfHeal: the date self-heal rewrites the
// frontmatter (quoting the date) and re-reads the file, which moves where the
// body starts. Lines must count against the bytes on disk after the heal.
func TestRun_PathRefLinesSurviveDateSelfHeal(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "technical/dated.md", "type: technical\ndue: 2026-01-01\nreviewed: 2026-02-02\n",
		"intro\n`server/a.go`\n")

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	defer store.Close()

	nodeTypes := map[string]manifest.NodeType{
		"technical": {Properties: []manifest.PropertyDecl{{Name: "due", Type: "date"}, {Name: "reviewed", Type: "date"}}},
	}

	runWithEdgeTypes(test, root, store, describesTechnical, nodeTypes)

	healed, readErr := os.ReadFile(filepath.Join(root, "technical/dated.md"))

	if readErr != nil {
		test.Fatalf("read: %v", readErr)
	}

	if !strings.Contains(string(healed), `"2026-01-01"`) {
		test.Fatalf("fixture did not self-heal:\n%s", healed)
	}

	if got := storedPathRefs(test, store); !slices.Equal(got, []string{"technical/dated describes server/a.go:8"}) {
		test.Errorf("path refs = %v, want server/a.go on line 8", got)
	}
}
