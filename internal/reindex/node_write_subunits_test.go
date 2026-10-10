package reindex_test

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
	"github.com/germanamz/tusk/internal/reindex"
)

// subUnitState renders every sub-unit row of fileID, the file's structural
// contains edges, and every edge leaving a sub-unit, one line each, sorted. The
// file's own content edges are left out: they are not the sub-unit pass's.
func subUnitState(test *testing.T, nodes *index.NodeRepo, edges *index.EdgeRepo, fileID string) []string {
	test.Helper()

	rows, listErr := nodes.ListSubUnitsForFile(fileID)

	if listErr != nil {
		test.Fatalf("ListSubUnitsForFile: %v", listErr)
	}

	state := make([]string, 0, len(rows))
	sources := []string{fileID}

	for _, row := range rows {
		state = append(state, fmt.Sprintf("node %+v", row))
		sources = append(sources, row.ID)
	}

	for _, source := range sources {
		edgeRows, edgeErr := edges.ListBySource(source)

		if edgeErr != nil {
			test.Fatalf("ListBySource %s: %v", source, edgeErr)
		}

		for _, edge := range edgeRows {
			if source == fileID && edge.Kind != "structural" {
				continue
			}

			state = append(state, fmt.Sprintf("edge %+v", edge))
		}
	}

	slices.Sort(state)

	return state
}

// nodeWriteFixture is a vault with one wikilinks edge type, a node service
// over its index, and a reindex runner, so a test can interleave the two.
type nodeWriteFixture struct {
	store *index.Index
	deps  node.ServiceDeps
	run   func(force bool) reindex.Report
}

func newNodeWriteFixture(test *testing.T) nodeWriteFixture {
	test.Helper()

	root := test.TempDir()
	manifestPath := filepath.Join(root, "tusk.toml")

	manifestBody := `
[workspace]
name = "test"

[node-types.note]

[edge-types.references]
from = ["*"]
to = ["*"]
cardinality = "many-to-many"
wikilinks = true
`

	if writeErr := os.WriteFile(manifestPath, []byte(manifestBody), 0o644); writeErr != nil {
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

	run := func(force bool) reindex.Report {
		test.Helper()

		report, runErr := reindex.Run(withGen(store, reindex.Config{
			Root:      root,
			Repo:      deps.Repo,
			Edges:     deps.Edges,
			EdgeTypes: loaded.EdgeTypes,
			NodeTypes: loaded.NodeTypes,
			Manifest:  loaded,
			Force:     force,
		}))

		if runErr != nil {
			test.Fatalf("Run: %v", runErr)
		}

		return *report
	}

	// Stamp the derivation and line-numbering markers so later passes are
	// incremental rather than forced by a marker mismatch.
	run(false)

	return nodeWriteFixture{store: store, deps: deps, run: run}
}

// TestNodeWrites_SubUnitsMatchForcedReindex pins #782: a page written through
// the node service's lease path is skipped by the next incremental reindex, so
// the service must leave its sub-units exactly as a full re-parse of the same
// bytes would.
func TestNodeWrites_SubUnitsMatchForcedReindex(test *testing.T) {
	fixture := newNodeWriteFixture(test)
	deps, run := fixture.deps, fixture.run
	service := node.NewServiceWithDeps(deps)

	body := "# Top\n\nalpha [[notes/other]]\n\n- one\n- two\n\n## Second\n\n```go\nx := 1\n```\n\nbeta\n"

	if _, createErr := service.Create(node.CreateInput{
		RelPath: "notes/doc.md",
		Type:    "note",
		Title:   "Doc",
		Body:    []byte(body),
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	steps := []struct {
		name  string
		write func() error
	}{
		{name: "create"},
		{name: "frontmatter modify", write: func() error {
			_, modifyErr := service.Modify(node.ModifyInput{ID: "notes/doc", SetProps: map[string]any{"owner": "me"}})
			return modifyErr
		}},
		{name: "body modify", write: func() error {
			_, modifyErr := service.Modify(node.ModifyInput{
				ID:   "notes/doc",
				Body: []byte("# Top\n\ngamma [[notes/third]]\n\n## Renamed\n\n- [ ] task\n"),
			})
			return modifyErr
		}},
	}

	for _, step := range steps {
		if step.write != nil {
			if writeErr := step.write(); writeErr != nil {
				test.Fatalf("%s: %v", step.name, writeErr)
			}
		}

		written := subUnitState(test, deps.Repo, deps.Edges, "notes/doc")

		if len(written) == 0 {
			test.Fatalf("%s: the service wrote no sub-units", step.name)
		}

		if report := run(false); report.Indexed != 0 {
			test.Fatalf("%s: incremental reindex indexed %d files, want the lease write skipped", step.name, report.Indexed)
		}

		run(true)

		if reparsed := subUnitState(test, deps.Repo, deps.Edges, "notes/doc"); !slices.Equal(written, reparsed) {
			test.Errorf("%s: service rows differ from a forced reindex:\nservice:\n  %v\nreindex:\n  %v", step.name, written, reparsed)
		}
	}
}

// TestNodeWrites_DerivationMarkerHealsStaleSubUnits pins the upgrade path for
// #782: a page an older binary created through the node service has no
// sub-units and a live file_state stamp, so only the bumped
// edgeDerivationVersion makes a plain reindex re-parse it.
func TestNodeWrites_DerivationMarkerHealsStaleSubUnits(test *testing.T) {
	fixture := newNodeWriteFixture(test)

	// A service without the manifest skips the sub-unit pass, as the
	// pre-fix one did.
	oldDeps := fixture.deps
	oldDeps.Manifest = nil

	if _, createErr := node.NewServiceWithDeps(oldDeps).Create(node.CreateInput{
		RelPath: "notes/doc.md",
		Type:    "note",
		Body:    []byte("# Top\n\nalpha\n"),
	}); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	if report := fixture.run(false); report.Indexed != 0 {
		test.Fatalf("plain reindex indexed %d files, want the lease write skipped", report.Indexed)
	}

	if got := subUnitState(test, fixture.deps.Repo, fixture.deps.Edges, "notes/doc"); len(got) != 0 {
		test.Fatalf("pre-upgrade fixture already has sub-units: %v", got)
	}

	if setErr := index.NewMetaRepo(fixture.store).Set("edge_derivation_version", "2026-10-08-skipped-file-records"); setErr != nil {
		test.Fatalf("rewind derivation marker: %v", setErr)
	}

	fixture.run(false)

	if got := subUnitState(test, fixture.deps.Repo, fixture.deps.Edges, "notes/doc"); len(got) == 0 {
		test.Errorf("the first reindex after upgrade left the page without sub-units")
	}
}
