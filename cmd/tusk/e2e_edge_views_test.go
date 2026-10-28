package main

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/index"
)

// edgeViewsManifest is the #848 repro manifest: notes describe services, and
// any node references any other through a body [[wikilink]].
const edgeViewsManifest = `[workspace]
name = "probe"

[node-types.service]
description = "s"

[node-types.note]
description = "n"

[edge-types.describes]
from = ["note"]
to = ["service"]
cardinality = "many-to-many"

[edge-types.references]
from = ["*"]
to = ["*"]
cardinality = "many-to-many"
wikilinks = true
`

// edgeViewsSource links target from the intro paragraph, a paragraph under
// "## Section" and a list item under "## Second", so the file and five of its
// sub-units each store a references edge to target.
const edgeViewsSource = "---\ntype: note\n---\n\n# Source\n\nIntro links [[target]].\n\n## Section\n\nOne paragraph that links [[target]].\n\n## Second\n\n- item with [[target]]\n"

func setupEdgeViews(test *testing.T) string {
	test.Helper()

	root := initWorkspaceWithManifest(test, edgeViewsManifest)

	writeWorkspaceFile(test, root, "target.md", "---\ntype: service\n---\n\n# Target\n\n## Setup\n\nInstall it.\n")
	writeWorkspaceFile(test, root, "src.md", edgeViewsSource)

	if out, reindexErr := runCLI("reindex"); reindexErr != nil {
		test.Fatalf("reindex: %v\n%s", reindexErr, out)
	}

	return root
}

func mustRunCLI(test *testing.T, args ...string) string {
	test.Helper()

	out, runErr := runCLI(args...)

	if runErr != nil {
		test.Fatalf("tusk %s: %v\n%s", strings.Join(args, " "), runErr, out)
	}

	return out
}

// TestE2E_EdgeListFoldsSubUnitLinks pins #848 finding 4: one link per passage
// lists as one row from the referring file, with the passages as units in
// document order, and --sub-units brings back the stored rows.
func TestE2E_EdgeListFoldsSubUnitLinks(test *testing.T) {
	setupEdgeViews(test)

	folded := mustRunCLI(test, "edge", "list", "--to", "target", "--type", "references", "--format", "compact")
	wantFolded := "references  src  target  src.md  units=src#S1P1,src#S1.1P1,src#S1.2L1\n"

	if folded != wantFolded {
		test.Errorf("edge list --to target:\n got %q\nwant %q", folded, wantFolded)
	}

	stored := mustRunCLI(test, "edge", "list", "--to", "target", "--type", "references", "--format", "compact", "--sub-units")

	var sources []string

	for line := range strings.Lines(stored) {
		sources = append(sources, strings.Fields(line)[1])
	}

	wantSources := []string{"src", "src#S1", "src#S1.1", "src#S1.1P1", "src#S1.2", "src#S1.2L1", "src#S1P1"}

	if !slices.Equal(sources, wantSources) {
		test.Errorf("--sub-units sources = %v, want %v", sources, wantSources)
	}
}

// TestE2E_EdgeListFromFileHidesContains pins that a file's own listing leaves
// out its structural contains edges, shows where in the file each link is
// written, and still lists contains when asked for by type or as stored.
func TestE2E_EdgeListFromFileHidesContains(test *testing.T) {
	setupEdgeViews(test)

	table := mustRunCLI(test, "edge", "list", "--from", "src")
	wantTable := "TYPE        SOURCE  TARGET  SOURCE_PATH  UNITS\n" +
		"references  src     target  src.md       src#S1P1,src#S1.1P1,src#S1.2L1\n"

	if table != wantTable {
		test.Errorf("edge list --from src:\n got %q\nwant %q", table, wantTable)
	}

	byType := mustRunCLI(test, "edge", "list", "--from", "src", "--type", "contains", "--format", "compact")

	if count := strings.Count(byType, "contains"); count != 6 {
		test.Errorf("edge list --from src --type contains lists %d contains edges, want 6:\n%s", count, byType)
	}

	subUnit := mustRunCLI(test, "edge", "list", "--from", "src#S1.1P1", "--format", "compact")
	wantSubUnit := "references  src#S1.1P1  target  src.md\n"

	if subUnit != wantSubUnit {
		test.Errorf("edge list --from src#S1.1P1:\n got %q\nwant %q", subUnit, wantSubUnit)
	}
}

// TestE2E_NodeGetEdgeFilters pins the node get side of finding 4: the folded
// view by default, --edge-type and --direction to narrow it (each implying
// edges), and --sub-units for the stored rows.
func TestE2E_NodeGetEdgeFilters(test *testing.T) {
	setupEdgeViews(test)

	type edgeRef struct {
		Type      string   `json:"type"`
		Direction string   `json:"direction"`
		TargetID  string   `json:"target_id"`
		Units     []string `json:"units"`
	}

	edgesOf := func(args ...string) []edgeRef {
		test.Helper()

		out := mustRunCLI(test, append([]string{"node", "get"}, append(args, "--json")...)...)

		var payload struct {
			Body  *string   `json:"body"`
			Edges []edgeRef `json:"edges"`
		}

		if unmarshalErr := json.Unmarshal([]byte(out), &payload); unmarshalErr != nil {
			test.Fatalf("node get %v: %v\n%s", args, unmarshalErr, out)
		}

		return payload.Edges
	}

	all := edgesOf("target", "--include", "edges")
	want := []edgeRef{{Type: "references", Direction: "in", TargetID: "src", Units: []string{"src#S1P1", "src#S1.1P1", "src#S1.2L1"}}}

	if len(all) != 1 || all[0].Type != want[0].Type || all[0].Direction != want[0].Direction ||
		all[0].TargetID != want[0].TargetID || !slices.Equal(all[0].Units, want[0].Units) {
		test.Errorf("node get target --include edges = %+v, want %+v (no contains, one row from src)", all, want)
	}

	out := edgesOf("src", "--edge-type", "references", "--direction", "out")

	if len(out) != 1 || out[0].TargetID != "target" || !slices.Equal(out[0].Units, want[0].Units) {
		test.Errorf("node get src --edge-type references --direction out = %+v, want one edge to target with its units", out)
	}

	if in := edgesOf("src", "--direction", "in"); len(in) != 0 {
		test.Errorf("node get src --direction in = %+v, want none", in)
	}

	stored := edgesOf("target", "--sub-units")

	var contains, references int

	for _, edge := range stored {
		switch edge.Type {
		case "contains":
			contains++
		case "references":
			references++
		}
	}

	if contains != 3 || references != 7 {
		test.Errorf("node get target --sub-units: %d contains and %d references, want 3 and 7: %+v", contains, references, stored)
	}

	if out, runErr := runCLI("node", "get", "target", "--direction", "sideways"); runErr == nil {
		test.Errorf("node get --direction sideways succeeded:\n%s", out)
	}
}

// TestE2E_EdgeViewsKeepAuthoredOrder pins #848 finding 16: a file's targets of
// one type list in the order it writes them (frontmatter list, then body
// links), in edge list and node get alike, and an index written before the
// order was stored gets it on the next reindex.
func TestE2E_EdgeViewsKeepAuthoredOrder(test *testing.T) {
	root := initWorkspaceWithManifest(test, edgeViewsManifest)

	for _, id := range []string{"zeta", "alpha", "mid"} {
		writeWorkspaceFile(test, root, id+".md", "---\ntype: service\n---\n\n# "+id+"\n")
	}

	writeWorkspaceFile(test, root, "doc.md", "---\ntype: note\ndescribes:\n  - zeta\n  - alpha\n  - mid\n---\n\n# Doc\n\nSee [[mid]], then [[zeta]].\n")

	mustRunCLI(test, "reindex")

	assertOrder := func(label string) {
		test.Helper()

		listed := mustRunCLI(test, "edge", "list", "--from", "doc", "--format", "compact")

		var targets []string

		for line := range strings.Lines(listed) {
			fields := strings.Fields(line)
			targets = append(targets, fields[0]+" "+fields[2])
		}

		want := []string{"describes zeta", "describes alpha", "describes mid", "references mid", "references zeta"}

		if !slices.Equal(targets, want) {
			test.Errorf("%s: edge list --from doc = %v, want %v", label, targets, want)
		}

		got := mustRunCLI(test, "node", "get", "doc", "--direction", "out", "--format", "compact")

		wantEdges := "  → describes zeta\n  → describes alpha\n  → describes mid\n" +
			"  → references mid  units=doc#S1P1\n  → references zeta  units=doc#S1P1\n"

		if !strings.HasSuffix(got, wantEdges) {
			test.Errorf("%s: node get doc --direction out:\n%s", label, got)
		}
	}

	assertOrder("fresh index")

	// An index from before #848 stores no positions; the edge-derivation
	// version bump re-derives every file and fills them in.
	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("open index: %v", openErr)
	}

	if _, execErr := store.DB().Exec(`UPDATE edges SET position = NULL`); execErr != nil {
		test.Fatalf("clear positions: %v", execErr)
	}

	if setErr := index.NewMetaRepo(store).Set("edge_derivation_version", "2026-10-16-block-references"); setErr != nil {
		test.Fatalf("set edge_derivation_version: %v", setErr)
	}

	store.Close()

	mustRunCLI(test, "reindex")
	assertOrder("after the upgrade reindex")
}
