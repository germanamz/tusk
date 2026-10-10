package query_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/query"
)

// pathsFixture builds a workspace whose ledger page names a present file, a
// missing file under a real top-level directory, and a span that names
// nothing (node.Node), with two sections for the mentions to fall under.
func pathsFixture(test *testing.T) (string, query.Deps) {
	test.Helper()

	root := test.TempDir()

	if mkErr := os.MkdirAll(filepath.Join(root, "server/ledger/core"), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	if writeErr := os.WriteFile(filepath.Join(root, "server/ledger/core/service.go"), []byte("package core\n"), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	nodes := index.NewNodeRepo(store)
	edges := index.NewEdgeRepo(store)

	for _, page := range []struct{ id, kind string }{{"technical/ledger", "technical"}, {"technical/arch", "technical"}} {
		if upsertErr := nodes.Upsert(index.NodeRow{ID: page.id, Type: page.kind, Path: page.id + ".md", Title: page.id, PropertiesJSON: `{}`}); upsertErr != nil {
			test.Fatalf("upsert: %v", upsertErr)
		}
	}

	sections := []index.NodeRow{
		{ID: "technical/ledger#S1", Title: "Ledger", StartLine: sql.NullInt64{Int64: 5, Valid: true}, EndLine: sql.NullInt64{Int64: 30, Valid: true}},
		{ID: "technical/ledger#S1.1", Title: "Core service", StartLine: sql.NullInt64{Int64: 8, Valid: true}, EndLine: sql.NullInt64{Int64: 12, Valid: true}},
	}

	for position := range sections {
		sections[position].Type = "section"
		sections[position].Path = "technical/ledger.md"
		sections[position].PropertiesJSON = `{}`
		sections[position].ParentID = sql.NullString{String: "technical/ledger", Valid: true}
	}

	if upsertErr := nodes.BulkUpsert(sections, "markdown"); upsertErr != nil {
		test.Fatalf("sections: %v", upsertErr)
	}

	ledgerRefs := []index.PathRefRow{
		{Type: "describes", Target: "server/ledger/core/service.go", Line: 9},
		{Type: "describes", Target: "server/ledger/core/service.go", Line: 10},
		{Type: "describes", Target: "server/ledger/core/service.go", Line: 20},
		{Type: "describes", Target: "server/ledger/old.go", Line: 21},
		{Type: "describes", Target: "node.Node", Line: 22},
	}

	if replaceErr := edges.ReplacePathRefs("technical/ledger", ledgerRefs); replaceErr != nil {
		test.Fatalf("refs: %v", replaceErr)
	}

	if replaceErr := edges.ReplacePathRefs("technical/arch", []index.PathRefRow{{Type: "describes", Target: "server/ledger", Line: 3}}); replaceErr != nil {
		test.Fatalf("refs: %v", replaceErr)
	}

	loaded := &manifest.Manifest{
		NodeTypes: map[string]manifest.NodeType{"technical": {}},
		EdgeTypes: manifest.EdgeTypes{"describes": {From: []string{"technical"}, Cardinality: manifest.CardinalityManyToMany, Paths: true}},
	}

	return root, query.Deps{Database: store.DB(), Manifest: loaded, Nodes: nodes, Edges: edges}
}

func runPathsQuery(test *testing.T, root string, deps query.Deps, filterText string) map[string][]query.PathRef {
	test.Helper()

	result, runErr := query.Run(context.Background(), deps, query.Request{
		Filter:        filterText,
		Include:       []string{"paths"},
		WorkspaceRoot: root,
	})

	if runErr != nil {
		test.Fatalf("Run %q: %v", filterText, runErr)
	}

	byID := map[string][]query.PathRef{}

	for _, row := range result.Rows {
		byID[row.ID] = row.Paths
	}

	return byID
}

func TestRun_IncludePathsListsAnchoredRefs(test *testing.T) {
	root, deps := pathsFixture(test)

	got := runPathsQuery(test, root, deps, "type=technical")

	want := []query.PathRef{
		{
			Path: "server/ledger/core/service.go", EdgeType: "describes", Exists: true,
			Mentions: []query.PathMention{
				{Line: 9, Section: "technical/ledger#S1.1", Heading: "Core service"},
				{Line: 10, Section: "technical/ledger#S1.1", Heading: "Core service"},
				{Line: 20, Section: "technical/ledger#S1", Heading: "Ledger"},
			},
		},
		{
			Path: "server/ledger/old.go", EdgeType: "describes", Exists: false,
			Mentions: []query.PathMention{{Line: 21, Section: "technical/ledger#S1", Heading: "Ledger"}},
		},
	}

	if !reflect.DeepEqual(got["technical/ledger"], want) {
		test.Errorf("ledger paths =\n%+v\nwant\n%+v", got["technical/ledger"], want)
	}

	if arch := got["technical/arch"]; len(arch) != 1 || arch[0].Path != "server/ledger" || arch[0].Mentions[0].Section != "" {
		test.Errorf("arch paths = %+v, want server/ledger with no section", arch)
	}
}

func TestRun_IncludePathsNarrowsToTheMatchedRefs(test *testing.T) {
	root, deps := pathsFixture(test)

	got := runPathsQuery(test, root, deps, "names-path=server/ledger/core/service.go")

	if ledger := got["technical/ledger"]; len(ledger) != 1 || ledger[0].Path != "server/ledger/core/service.go" {
		test.Errorf("ledger paths = %+v, want only the matched file", ledger)
	}

	if arch := got["technical/arch"]; len(arch) != 1 || arch[0].Path != "server/ledger" {
		test.Errorf("arch paths = %+v, want the ancestor directory it matched by", arch)
	}
}

func TestFormatMentions(test *testing.T) {
	cases := []struct {
		mentions []query.PathMention
		want     string
	}{
		{nil, ""},
		{[]query.PathMention{{}}, ""},
		{[]query.PathMention{{Line: 12, Heading: "Modules"}}, "line 12 (Modules)"},
		{[]query.PathMention{{Line: 40, Heading: "Core"}, {Line: 41, Heading: "Core"}}, "lines 40-41 (Core)"},
		{[]query.PathMention{{Line: 40, Heading: "Core"}, {Line: 41, Heading: "Wiring"}, {Line: 80}}, "lines 40 (Core), 41 (Wiring), 80"},
	}

	for _, testCase := range cases {
		if got := query.FormatMentions(testCase.mentions); got != testCase.want {
			test.Errorf("FormatMentions(%+v) = %q, want %q", testCase.mentions, got, testCase.want)
		}
	}
}

func TestRun_IncludePathsNarrowsToTheQualifiedEdgeType(test *testing.T) {
	root, deps := pathsFixture(test)

	deps.Manifest.EdgeTypes["mentions"] = manifest.EdgeType{From: []string{"*"}, Cardinality: manifest.CardinalityManyToMany, Paths: true}

	ledgerRefs := []index.PathRefRow{
		{Type: "describes", Target: "server/ledger/core/service.go", Line: 9},
		{Type: "mentions", Target: "server/ledger/core/service.go", Line: 25},
	}

	if replaceErr := deps.Edges.ReplacePathRefs("technical/ledger", ledgerRefs); replaceErr != nil {
		test.Fatalf("refs: %v", replaceErr)
	}

	if ledger := runPathsQuery(test, root, deps, "names-path=server/ledger/core/service.go")["technical/ledger"]; len(ledger) != 2 {
		test.Errorf("unqualified ledger paths = %+v, want both edge types", ledger)
	}

	got := runPathsQuery(test, root, deps, "names-path:mentions=server/ledger/core/service.go")

	if ledger := got["technical/ledger"]; len(ledger) != 1 || ledger[0].EdgeType != "mentions" {
		test.Errorf("qualified ledger paths = %+v, want only the mentions ref", ledger)
	}

	if _, matched := got["technical/arch"]; matched {
		test.Errorf("arch names server/ledger under describes only, yet matched names-path:mentions=")
	}
}
