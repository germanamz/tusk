package subunit_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/subunit"
)

// kindsBody holds a unit of most kinds: a section, a paragraph that links,
// a list item and a table with one header and one body cell.
const kindsBody = "# Title\n\nFirst paragraph [[other]].\n\n- item\n\n| a |\n| - |\n| 1 |\n"

// kindsManifest loads a manifest whose [workspace] sub-units line is
// subUnits ("" for none), with one wikilink edge type and any further tables
// given, such as a [node-types.<name>] that sets its own sub-units.
func kindsManifest(test *testing.T, subUnits string, tables ...string) *manifest.Manifest {
	test.Helper()

	body := "[workspace]\nname = \"x\"\n" + subUnits + "\n\n" +
		"[edge-types.references]\nfrom = [\"*\"]\nto = [\"*\"]\ncardinality = \"many-to-many\"\nwikilinks = true\n"

	for _, table := range tables {
		body += "\n" + table + "\n"
	}
	path := filepath.Join(test.TempDir(), "tusk.toml")

	if writeErr := os.WriteFile(path, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(path)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	return loaded
}

// subUnitState is what a file's sub-units look like in the index: each row's
// parent, the file's contains targets, the rows that hold a references edge
// and the rows queued for embedding.
type subUnitState struct {
	parents   map[string]string
	contains  []string
	linkRows  []string
	embedRows []string
}

func readSubUnitState(test *testing.T, nodes *index.NodeRepo, edges *index.EdgeRepo, queue *index.EmbedQueueRepo, fileID string) subUnitState {
	test.Helper()

	rows, listErr := nodes.ListSubUnitsForFile(fileID)

	if listErr != nil {
		test.Fatalf("ListSubUnitsForFile: %v", listErr)
	}

	state := subUnitState{parents: map[string]string{}}

	for _, row := range rows {
		state.parents[row.ID] = row.ParentID.String
	}

	allEdges, edgesErr := edges.ListAll()

	if edgesErr != nil {
		test.Fatalf("ListAll: %v", edgesErr)
	}

	for _, edge := range allEdges {
		switch {
		case edge.Type == "contains" && edge.SourceID == fileID:
			state.contains = append(state.contains, edge.TargetID)
		case edge.Type == "references" && edge.SourceID != fileID:
			state.linkRows = append(state.linkRows, edge.SourceID)
		}
	}

	queued, queueErr := queue.ListNodeIDs()

	if queueErr != nil {
		test.Fatalf("ListNodeIDs: %v", queueErr)
	}

	for _, id := range queued {
		if id != fileID {
			state.embedRows = append(state.embedRows, id)
		}
	}

	sort.Strings(state.contains)
	sort.Strings(state.linkRows)
	sort.Strings(state.embedRows)

	return state
}

// TestSync_KeepsOnlyListedKinds writes only the kinds `[workspace] sub-units`
// lists (#853). A leaf whose section is left out keeps its structural id, so
// a [[note#S1P1]] link and its vector survive the setting, and hangs off the
// file instead of a section row that isn't there.
func TestSync_KeepsOnlyListedKinds(test *testing.T) {
	store := openSyncTestIndex(test)
	sync, nodes, edges, queue := newSync(store, kindsManifest(test, `sub-units = ["paragraph", "list-item"]`))
	parent := seedFileRow(test, nodes, "notes/intro", "notes/intro.md")

	units, parseErr := subunit.Parse([]byte(kindsBody))

	if parseErr != nil {
		test.Fatalf("Parse: %v", parseErr)
	}

	result, applyErr := sync.ApplyFile(context.Background(), parent, units)

	if applyErr != nil {
		test.Fatalf("ApplyFile: %v", applyErr)
	}

	if result.Inserted != 2 {
		test.Errorf("Inserted = %d, want 2 (the paragraph and the list item)", result.Inserted)
	}

	state := readSubUnitState(test, nodes, edges, queue, parent.ID)
	wantRows := []string{"notes/intro#S1L1", "notes/intro#S1P1"}

	if got := sortedKeys(state.parents); !slices.Equal(got, wantRows) {
		test.Fatalf("rows = %v, want %v", got, wantRows)
	}

	for id, parentID := range state.parents {
		if parentID != parent.ID {
			test.Errorf("%s parent_id = %q, want the file %q", id, parentID, parent.ID)
		}
	}

	if !slices.Equal(state.contains, wantRows) {
		test.Errorf("contains targets = %v, want %v", state.contains, wantRows)
	}

	if want := []string{"notes/intro#S1P1"}; !slices.Equal(state.linkRows, want) {
		test.Errorf("rows with a references edge = %v, want %v", state.linkRows, want)
	}

	if !slices.Equal(state.embedRows, wantRows) {
		test.Errorf("queued sub-units = %v, want %v", state.embedRows, wantRows)
	}
}

// TestSync_KindListFlipConverges narrows the kinds and widens them back. The
// left-out kinds' rows go with their edges and embed jobs, and a kept leaf
// moves to the file and back to its section, since its parent changes with
// nothing else about it.
func TestSync_KindListFlipConverges(test *testing.T) {
	store := openSyncTestIndex(test)
	allKinds, nodes, edges, queue := newSync(store, kindsManifest(test, ""))
	parent := seedFileRow(test, nodes, "notes/intro", "notes/intro.md")

	units, parseErr := subunit.Parse([]byte(kindsBody))

	if parseErr != nil {
		test.Fatalf("Parse: %v", parseErr)
	}

	if _, applyErr := allKinds.ApplyFile(context.Background(), parent, units); applyErr != nil {
		test.Fatalf("ApplyFile (every kind): %v", applyErr)
	}

	full := readSubUnitState(test, nodes, edges, queue, parent.ID)

	if got := full.parents["notes/intro#S1P1"]; got != "notes/intro#S1" {
		test.Fatalf("paragraph parent with every kind = %q, want the section", got)
	}

	if len(full.parents) != 5 {
		test.Fatalf("rows with every kind = %v, want 5", sortedKeys(full.parents))
	}

	narrow, _, _, _ := newSync(store, kindsManifest(test, `sub-units = ["paragraph"]`))

	result, narrowErr := narrow.ApplyFile(context.Background(), parent, units)

	if narrowErr != nil {
		test.Fatalf("ApplyFile (paragraphs): %v", narrowErr)
	}

	if result.Deleted != 4 {
		test.Errorf("Deleted = %d, want 4 (section, list item, two cells)", result.Deleted)
	}

	narrowed := readSubUnitState(test, nodes, edges, queue, parent.ID)
	want := []string{"notes/intro#S1P1"}

	if got := sortedKeys(narrowed.parents); !slices.Equal(got, want) {
		test.Fatalf("rows after narrowing = %v, want %v", got, want)
	}

	if got := narrowed.parents["notes/intro#S1P1"]; got != parent.ID {
		test.Errorf("paragraph parent after narrowing = %q, want the file", got)
	}

	if !slices.Equal(narrowed.contains, want) || !slices.Equal(narrowed.linkRows, want) {
		test.Errorf("contains = %v, link rows = %v, want both %v", narrowed.contains, narrowed.linkRows, want)
	}

	for _, id := range narrowed.embedRows {
		if id != "notes/intro#S1P1" {
			test.Errorf("embed job for dropped row %s survived", id)
		}
	}

	if _, widenErr := allKinds.ApplyFile(context.Background(), parent, units); widenErr != nil {
		test.Fatalf("ApplyFile (every kind again): %v", widenErr)
	}

	widened := readSubUnitState(test, nodes, edges, queue, parent.ID)

	if got := widened.parents["notes/intro#S1P1"]; got != "notes/intro#S1" {
		test.Errorf("paragraph parent after widening = %q, want the section", got)
	}

	if !slices.Equal(sortedKeys(widened.parents), sortedKeys(full.parents)) || !slices.Equal(widened.contains, full.contains) {
		test.Errorf("widened rows = %v, contains = %v, want %v and %v", sortedKeys(widened.parents), widened.contains, sortedKeys(full.parents), full.contains)
	}
}

// TestSync_TypeOverridePicksKinds indexes each file with the kinds its type
// names in [node-types], and the workspace's for a type that names none
// (#867): a spec keeps every kind, a meeting none, and a note the workspace's
// paragraphs, hanging off the file since sections aren't indexed for it.
func TestSync_TypeOverridePicksKinds(test *testing.T) {
	store := openSyncTestIndex(test)
	loaded := kindsManifest(test, `sub-units = ["paragraph"]`,
		"[node-types.spec]\nsub-units = true",
		"[node-types.meeting]\nsub-units = false",
	)
	sync, nodes, edges, queue := newSync(store, loaded)

	units, parseErr := subunit.Parse([]byte(kindsBody))

	if parseErr != nil {
		test.Fatalf("Parse: %v", parseErr)
	}

	testCases := []struct {
		id, typeName string
		wantRows     []string
		wantParent   string // the paragraph's parent, when it is kept
	}{
		{id: "notes/intro", typeName: "note", wantRows: []string{"notes/intro#S1P1"}, wantParent: "notes/intro"},
		{id: "specs/api", typeName: "spec", wantRows: []string{"specs/api#S1", "specs/api#S1L1", "specs/api#S1P1", "specs/api#S1T1R0C0", "specs/api#S1T1R1C0"}, wantParent: "specs/api#S1"},
		{id: "meetings/standup", typeName: "meeting", wantRows: []string{}},
	}

	for _, testCase := range testCases {
		parent := seedFileRow(test, nodes, testCase.id, testCase.id+".md")
		parent.Type = testCase.typeName

		if upsertErr := nodes.Upsert(parent); upsertErr != nil {
			test.Fatalf("retype %s: %v", testCase.id, upsertErr)
		}

		if _, applyErr := sync.ApplyFile(context.Background(), parent, units); applyErr != nil {
			test.Fatalf("ApplyFile %s: %v", testCase.id, applyErr)
		}

		state := readSubUnitState(test, nodes, edges, queue, parent.ID)

		if got := sortedKeys(state.parents); !slices.Equal(got, testCase.wantRows) {
			test.Errorf("%s (%s) rows = %v, want %v", testCase.id, testCase.typeName, got, testCase.wantRows)
		}

		if testCase.wantParent != "" {
			if got := state.parents[testCase.id+"#S1P1"]; got != testCase.wantParent {
				test.Errorf("%s paragraph parent = %q, want %q", testCase.id, got, testCase.wantParent)
			}
		}
	}
}

func sortedKeys(set map[string]string) []string {
	keys := make([]string, 0, len(set))

	for key := range set {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}
