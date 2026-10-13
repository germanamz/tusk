package reindex_test

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/germanamz/tusk/internal/embed"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/reindex"
)

// adoptNodeTypes and adoptEdgeTypes are the slice of the vault pack the
// default-type tests need: a note type and a wikilinks edge.
var (
	adoptNodeTypes = map[string]manifest.NodeType{"note": {}, "decision": {}}
	adoptEdgeTypes = manifest.EdgeTypes{
		"references": {From: []string{"*"}, To: []string{"*"}, Cardinality: manifest.CardinalityManyToMany, Wikilinks: true},
	}
)

// defaultTypeFixture writes a folder of plain markdown (no frontmatter, and
// frontmatter without a type), one typed decision and one untyped HTML page,
// and opens an index beside them.
func defaultTypeFixture(test *testing.T) (string, *index.Index) {
	test.Helper()

	root := test.TempDir()

	writeRaw(test, root, "ideas/auth.md", []byte("# Auth ideas\n\nSee [[decisions/sso]] and [[standup]].\n"))
	writeRaw(test, root, "standup.md", []byte("---\ndate: 2026-10-01\ntags: [team]\n---\n\nNotes from standup.\n"))
	writeRaw(test, root, "decisions/sso.md", []byte("---\ntype: decision\ntitle: Use SSO\n---\n\nWe chose SSO.\n"))
	writeRaw(test, root, "site/page.html", []byte("<html><head><title>Page</title></head><body>generated</body></html>"))

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	return root, store
}

func runWithDefaultType(test *testing.T, root string, store *index.Index, defaultType string) *reindex.Report {
	test.Helper()

	return runWithDefaultTypeLogged(test, root, store, defaultType, nil)
}

func runWithDefaultTypeLogged(test *testing.T, root string, store *index.Index, defaultType string, logger *slog.Logger) *reindex.Report {
	test.Helper()

	report, runErr := reindex.Run(withGen(store, reindex.Config{
		Logger:    logger,
		Root:      root,
		Repo:      index.NewNodeRepo(store),
		Edges:     index.NewEdgeRepo(store),
		EdgeTypes: adoptEdgeTypes,
		NodeTypes: adoptNodeTypes,
		Manifest: &manifest.Manifest{
			Workspace: manifest.WorkspaceSection{DefaultType: defaultType},
			EdgeTypes: adoptEdgeTypes,
			NodeTypes: adoptNodeTypes,
		},
	}))

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	return report
}

// fileNodes returns "id type title" for every file-level node, sorted.
func fileNodes(test *testing.T, store *index.Index) []string {
	test.Helper()

	rows, listErr := index.NewNodeRepo(store).ListFileNodes()

	if listErr != nil {
		test.Fatalf("ListFileNodes: %v", listErr)
	}

	described := make([]string, 0, len(rows))

	for _, row := range rows {
		described = append(described, row.ID+" "+row.Type+" "+row.Title)
	}

	slices.Sort(described)

	return described
}

func TestRun_DefaultTypeAdoptsUntypedMarkdown(test *testing.T) {
	root, store := defaultTypeFixture(test)

	report := runWithDefaultType(test, root, store, "note")

	want := []string{
		"decisions/sso decision Use SSO",
		"ideas/auth note Auth ideas",
		"standup note standup",
	}

	if got := fileNodes(test, store); !slices.Equal(got, want) {
		test.Errorf("file nodes = %q, want %q", got, want)
	}

	if report.Indexed != 3 || report.NotNodes != 1 {
		test.Errorf("Indexed, NotNodes = %d, %d; want 3 indexed and the untyped HTML page as the 1 non-node",
			report.Indexed, report.NotNodes)
	}

	edges, listErr := index.NewEdgeRepo(store).ListBySource("ideas/auth")

	if listErr != nil {
		test.Fatalf("ListBySource: %v", listErr)
	}

	var targets []string

	for _, edge := range edges {
		if edge.Type == "references" {
			targets = append(targets, edge.TargetID)
		}
	}

	slices.Sort(targets)

	if !slices.Equal(targets, []string{"decisions/sso", "standup"}) {
		test.Errorf("references from ideas/auth = %q, want both wikilinks as edges", targets)
	}
}

// TestRun_DefaultTypeLeavesFilesUntouched pins the issue's promise that an
// adopted file is never written: not given a type, and not put through the
// date self-heal a typed file's unquoted date gets, which would only fail on
// a file with no type and log a WARN for each one.
func TestRun_DefaultTypeLeavesFilesUntouched(test *testing.T) {
	root, store := defaultTypeFixture(test)

	before := map[string][]byte{}

	for _, relPath := range []string{"ideas/auth.md", "standup.md"} {
		content, readErr := os.ReadFile(filepath.Join(root, relPath))

		if readErr != nil {
			test.Fatalf("read %s: %v", relPath, readErr)
		}

		before[relPath] = content
	}

	var logs bytes.Buffer

	runWithDefaultTypeLogged(test, root, store, "note",
		slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))

	if logs.Len() > 0 {
		test.Errorf("adopting the files logged warnings: %s", logs.String())
	}

	for relPath, content := range before {
		after, readErr := os.ReadFile(filepath.Join(root, relPath))

		if readErr != nil {
			test.Fatalf("read %s: %v", relPath, readErr)
		}

		if string(after) != string(content) {
			test.Errorf("%s was rewritten:\nbefore %q\nafter  %q", relPath, content, after)
		}
	}
}

// TestRun_DefaultTypeChangeForcesFullPass covers turning the setting on in a
// workspace that was already indexed: the plain files are unchanged on disk,
// so the incremental skip alone would never re-read them.
func TestRun_DefaultTypeChangeForcesFullPass(test *testing.T) {
	root, store := defaultTypeFixture(test)

	runWithDefaultType(test, root, store, "")

	if got := fileNodes(test, store); !slices.Equal(got, []string{"decisions/sso decision Use SSO"}) {
		test.Fatalf("without default-type, file nodes = %q; want only the typed decision", got)
	}

	runWithDefaultType(test, root, store, "note")

	if got := fileNodes(test, store); len(got) != 3 {
		test.Errorf("after turning default-type on, file nodes = %q; want the 2 plain files adopted", got)
	}
}

// TestRun_DefaultTypeOffDropsAdoptedNodes covers turning the setting back
// off: the adopted files are still on disk, so the reaper never sees them,
// and the drain has to drop their rows itself.
func TestRun_DefaultTypeOffDropsAdoptedNodes(test *testing.T) {
	root, store := defaultTypeFixture(test)

	runWithDefaultType(test, root, store, "note")
	report := runWithDefaultType(test, root, store, "")

	if got := fileNodes(test, store); !slices.Equal(got, []string{"decisions/sso decision Use SSO"}) {
		test.Errorf("after turning default-type off, file nodes = %q; want only the typed decision", got)
	}

	if report.Removed != 2 {
		test.Errorf("Removed = %d, want the 2 formerly adopted files", report.Removed)
	}

	subUnits, listErr := index.NewNodeRepo(store).ListSubUnitsForFile("ideas/auth")

	if listErr != nil {
		test.Fatalf("ListSubUnitsForFile: %v", listErr)
	}

	if len(subUnits) != 0 {
		test.Errorf("ideas/auth kept %d sub-unit rows after it stopped being a node", len(subUnits))
	}

	edges, edgeErr := index.NewEdgeRepo(store).ListBySource("ideas/auth")

	if edgeErr != nil {
		test.Fatalf("ListBySource: %v", edgeErr)
	}

	if len(edges) != 0 {
		test.Errorf("ideas/auth kept %d edges after it stopped being a node", len(edges))
	}
}

// TestRun_FileThatLosesItsTypeIsDropped is the bug the default-type switch
// exposed: a file edited to drop its `type:` used to keep its node row (and
// edges and sub-units) indefinitely, since it is still on disk and the reaper
// only drops rows for files that are gone.
func TestRun_FileThatLosesItsTypeIsDropped(test *testing.T) {
	root, store := defaultTypeFixture(test)

	runWithDefaultType(test, root, store, "")
	writeRaw(test, root, "decisions/sso.md", []byte("# Use SSO\n\nNo longer typed.\n"))

	report := runWithDefaultType(test, root, store, "")

	if got := fileNodes(test, store); len(got) != 0 {
		test.Errorf("file nodes = %q; want none once the decision lost its type", got)
	}

	if report.Removed != 1 {
		test.Errorf("Removed = %d, want 1", report.Removed)
	}
}

// TestRun_DefaultTypeEmbedsAdoptedNodes covers the embed drain, which re-reads
// each file to build its payload: an adopted file has no type of its own, so a
// strict parse there would fail it and leave the node out of semantic search.
func TestRun_DefaultTypeEmbedsAdoptedNodes(test *testing.T) {
	root, store := defaultTypeFixture(test)

	queueRepo := index.NewEmbedQueueRepo(store)
	embeddingRepo := index.NewEmbeddingRepo(store)

	_, runErr := reindex.Run(withGen(store, reindex.Config{
		Root:          root,
		Repo:          index.NewNodeRepo(store),
		Edges:         index.NewEdgeRepo(store),
		EdgeTypes:     adoptEdgeTypes,
		NodeTypes:     adoptNodeTypes,
		EmbedQueue:    queueRepo,
		EmbeddingRepo: embeddingRepo,
		Embedder:      &stubEmbedder{model: "test", dim: 3},
		Chunker:       embed.WholeDocument{},
		Manifest: &manifest.Manifest{
			Workspace: manifest.WorkspaceSection{DefaultType: "note"},
			EdgeTypes: adoptEdgeTypes,
			NodeTypes: adoptNodeTypes,
		},
	}))

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	for _, nodeID := range []string{"ideas/auth", "standup"} {
		vectors, getErr := embeddingRepo.GetByNodeID(nodeID)

		if getErr != nil {
			test.Fatalf("GetByNodeID %s: %v", nodeID, getErr)
		}

		if len(vectors) == 0 {
			test.Errorf("adopted node %s has no embedding", nodeID)
		}
	}

	if depth, _ := queueRepo.DepthByKind("embed"); depth != 0 {
		test.Errorf("embed queue depth = %d, want 0 after the drain", depth)
	}
}
