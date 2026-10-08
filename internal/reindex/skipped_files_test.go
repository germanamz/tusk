package reindex_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/reindex"
)

// skipFixture is a workspace root plus an open index whose Config wires the
// edge repo and a wikilink-enabled `references` edge type, matching the shape
// of a real rebuild config.
type skipFixture struct {
	root       string
	store      *index.Index
	repo       *index.NodeRepo
	fileStates *index.FileStateRepo
	edgeTypes  manifest.EdgeTypes
}

func newSkipFixture(test *testing.T) *skipFixture {
	test.Helper()

	root := test.TempDir()
	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	return &skipFixture{
		root:       root,
		store:      store,
		repo:       index.NewNodeRepo(store),
		fileStates: index.NewFileStateRepo(store),
		edgeTypes: manifest.EdgeTypes{
			"references": manifest.EdgeType{
				From: []string{"*"}, To: []string{"*"},
				Cardinality: manifest.CardinalityManyToMany,
				Wikilinks:   true,
			},
		},
	}
}

func (fixture *skipFixture) run(test *testing.T) *reindex.Report {
	test.Helper()

	report, runErr := reindex.Run(withGen(fixture.store, reindex.Config{
		Root:       fixture.root,
		Repo:       fixture.repo,
		Edges:      index.NewEdgeRepo(fixture.store),
		EdgeTypes:  fixture.edgeTypes,
		Manifest:   &manifest.Manifest{EdgeTypes: fixture.edgeTypes},
		FileStates: fixture.fileStates,
	}))

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	return report
}

func (fixture *skipFixture) write(test *testing.T, relPath, content string) {
	test.Helper()

	abs := filepath.Join(fixture.root, relPath)

	if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	if writeErr := os.WriteFile(abs, []byte(content), 0o644); writeErr != nil {
		test.Fatalf("write %s: %v", relPath, writeErr)
	}
}

func (fixture *skipFixture) skips(test *testing.T) map[string]string {
	test.Helper()

	rows, listErr := fixture.fileStates.ListSkips()

	if listErr != nil {
		test.Fatalf("ListSkips: %v", listErr)
	}

	out := make(map[string]string, len(rows))

	for _, row := range rows {
		out[row.Path] = row.Reason
	}

	return out
}

const brokenFrontmatter = "---\ntype: [unclosed\n---\n\n# Broken\n"

func TestRun_RecordsSkipForUndecodableFrontmatter(test *testing.T) {
	fixture := newSkipFixture(test)

	fixture.write(test, "docs/broken.md", brokenFrontmatter)
	fixture.write(test, "docs/fine.md", "---\ntype: note\n---\n\n# Fine\n")

	fixture.run(test)

	skips := fixture.skips(test)

	if len(skips) != 1 {
		test.Fatalf("skips = %v, want exactly docs/broken.md", skips)
	}

	if reason := skips["docs/broken.md"]; !strings.Contains(reason, "decode frontmatter") {
		test.Errorf("reason = %q, want the frontmatter decode error", reason)
	}
}

// TestRun_NoSkipRecordForNonNodes pins that a file which is simply not a node
// (no frontmatter, or frontmatter without a type) is not a fault: vaults keep
// plain markdown next to their nodes on purpose.
func TestRun_NoSkipRecordForNonNodes(test *testing.T) {
	fixture := newSkipFixture(test)

	fixture.write(test, "README.md", "# Just prose\n\nNo frontmatter here.\n")
	fixture.write(test, "docs/untyped.md", "---\ntitle: Untyped\n---\n\nBody.\n")
	fixture.write(test, "docs/page.html", "<html><head><title>Plain</title></head><body>hi</body></html>")

	fixture.run(test)

	if skips := fixture.skips(test); len(skips) != 0 {
		test.Errorf("skips = %v, want none for non-node files", skips)
	}
}

func TestRun_RecordsSkipForEdgeShapeFailure(test *testing.T) {
	fixture := newSkipFixture(test)

	fixture.write(test, "docs/badedge.md", "---\ntype: note\nreferences: 42\n---\n\nBody.\n")

	fixture.run(test)

	if reason := fixture.skips(test)["docs/badedge.md"]; !strings.Contains(reason, "references") {
		test.Errorf("reason = %q, want the edge value-shape error naming references", reason)
	}
}

func TestRun_RecordsSkipForReservedID(test *testing.T) {
	fixture := newSkipFixture(test)

	fixture.write(test, "notes/y#S1.md", "---\ntype: note\n---\n\nBody.\n")

	fixture.run(test)

	if reason := fixture.skips(test)["notes/y#S1.md"]; reason == "" {
		test.Errorf("skips = %v, want a record for the reserved-id path", fixture.skips(test))
	}
}

func TestRun_ClearsSkipWhenFileFixed(test *testing.T) {
	fixture := newSkipFixture(test)

	fixture.write(test, "docs/broken.md", brokenFrontmatter)
	fixture.run(test)

	fixture.write(test, "docs/broken.md", "---\ntype: note\n---\n\n# Fixed now\n")
	fixture.run(test)

	if skips := fixture.skips(test); len(skips) != 0 {
		test.Errorf("skips = %v, want none after the fix", skips)
	}

	if _, getErr := fixture.repo.Get("docs/broken"); getErr != nil {
		test.Errorf("fixed file not indexed: %v", getErr)
	}
}

func TestRun_ClearsSkipWhenFileBecomesNonNode(test *testing.T) {
	fixture := newSkipFixture(test)

	fixture.write(test, "docs/broken.md", brokenFrontmatter)
	fixture.run(test)

	fixture.write(test, "docs/broken.md", "# No longer a node\n\nThe frontmatter was removed on purpose.\n")
	fixture.run(test)

	if skips := fixture.skips(test); len(skips) != 0 {
		test.Errorf("skips = %v, want none once the file is a plain non-node", skips)
	}
}

func TestRun_ClearsSkipWhenFileDeleted(test *testing.T) {
	fixture := newSkipFixture(test)

	fixture.write(test, "docs/broken.md", brokenFrontmatter)
	fixture.write(test, "notes/y#S1.md", "---\ntype: note\n---\n\nBody.\n")
	fixture.run(test)

	for _, relPath := range []string{"docs/broken.md", "notes/y#S1.md"} {
		if rmErr := os.Remove(filepath.Join(fixture.root, relPath)); rmErr != nil {
			test.Fatalf("remove %s: %v", relPath, rmErr)
		}
	}

	fixture.run(test)

	if skips := fixture.skips(test); len(skips) != 0 {
		test.Errorf("skips = %v, want none after the reaper tombstones the files", skips)
	}
}

// TestRun_KeepsStaleNodeRowAndRecordsSkip pins the state doctor has to
// explain: a file that indexed once and then broke keeps its last good node
// row, and the skip record is what tells the two apart.
func TestRun_KeepsStaleNodeRowAndRecordsSkip(test *testing.T) {
	fixture := newSkipFixture(test)

	fixture.write(test, "docs/doc.md", "---\ntype: note\n---\n\n# Good\n")
	fixture.run(test)

	fixture.write(test, "docs/doc.md", brokenFrontmatter)
	fixture.run(test)

	if _, getErr := fixture.repo.Get("docs/doc"); getErr != nil {
		test.Errorf("stale node row gone: %v", getErr)
	}

	if reason := fixture.skips(test)["docs/doc.md"]; reason == "" {
		test.Errorf("no skip record for the broken previously-indexed file")
	}
}

// TestRun_DerivationMarkerRecordsAlreadyBrokenFile pins the upgrade path: a
// vault indexed by a binary that predates skip records has a broken file
// stamped live in file_state and no record. The mtime+size check would never
// re-read it, so the bumped edgeDerivationVersion must force one pass that
// records it.
func TestRun_DerivationMarkerRecordsAlreadyBrokenFile(test *testing.T) {
	fixture := newSkipFixture(test)

	fixture.write(test, "docs/broken.md", brokenFrontmatter)
	fixture.run(test)

	// Simulate the pre-upgrade index: no skip record, and the marker rewound
	// to the previous derivation version.
	if clearErr := fixture.fileStates.ClearSkip("docs/broken.md"); clearErr != nil {
		test.Fatalf("ClearSkip: %v", clearErr)
	}

	if setErr := index.NewMetaRepo(fixture.store).Set("edge_derivation_version", "2026-07-11-special-char-file-ids"); setErr != nil {
		test.Fatalf("rewind derivation marker: %v", setErr)
	}

	fixture.run(test)

	if reason := fixture.skips(test)["docs/broken.md"]; reason == "" {
		test.Errorf("already-broken file not recorded on the first reindex after upgrade")
	}

	// And a plain pass after that keeps the record (nothing clears it).
	fixture.run(test)

	if _, ok := fixture.skips(test)["docs/broken.md"]; !ok {
		test.Errorf("skip record lost on an unchanged second pass")
	}

	if _, getErr := fixture.repo.Get("docs/broken"); !errors.Is(getErr, index.ErrNodeNotFound) {
		test.Errorf("broken file indexed: err = %v", getErr)
	}
}
