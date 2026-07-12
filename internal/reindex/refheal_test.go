package reindex_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/reindex"
)

// refHealManifest declares a ticket type whose assignee refs a person. Test
// paths are chosen so the referencing file sorts lexically BEFORE its target
// ("aref/" < "zref/"), making the ref a forward ref on a fresh walk.
const refHealManifest = `
[workspace]
name = "test"

[node-types.person]
properties = [
    { name = "name", type = "string", required = true },
]

[node-types.ticket]
properties = [
    { name = "assignee", type = "ref", to = "person" },
    { name = "reviewer", type = "ref", to = "person" },
]
`

func refHealConfig(test *testing.T, root string, store *index.Index) reindex.Config {
	test.Helper()

	manifestPath := filepath.Join(root, "tusk.toml")

	if writeErr := os.WriteFile(manifestPath, []byte(refHealManifest), 0o644); writeErr != nil {
		test.Fatalf("write tusk.toml: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("load manifest: %v", loadErr)
	}

	return withGen(store, reindex.Config{
		Root:          root,
		Repo:          index.NewNodeRepo(store),
		Edges:         index.NewEdgeRepo(store),
		EdgeTypes:     loaded.EdgeTypes,
		NodeTypes:     loaded.NodeTypes,
		PropertyDrift: index.NewPropertyDriftRepo(store),
	})
}

// listRefHealManifest declares a ticket whose reviewers is a list-of(ref) to
// person, so a single property can carry several broken values at once.
const listRefHealManifest = `
[workspace]
name = "test"

[node-types.person]
properties = [
    { name = "name", type = "string", required = true },
]

[node-types.ticket]
properties = [
    { name = "reviewers", type = "list-of", item-type = "ref", to = "person" },
]
`

func listRefHealConfig(test *testing.T, root string, store *index.Index) reindex.Config {
	test.Helper()

	manifestPath := filepath.Join(root, "tusk.toml")

	if writeErr := os.WriteFile(manifestPath, []byte(listRefHealManifest), 0o644); writeErr != nil {
		test.Fatalf("write tusk.toml: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("load manifest: %v", loadErr)
	}

	return withGen(store, reindex.Config{
		Root:          root,
		Repo:          index.NewNodeRepo(store),
		Edges:         index.NewEdgeRepo(store),
		EdgeTypes:     loaded.EdgeTypes,
		NodeTypes:     loaded.NodeTypes,
		PropertyDrift: index.NewPropertyDriftRepo(store),
	})
}

func refDriftRows(test *testing.T, store *index.Index) []index.PropertyDriftRow {
	test.Helper()

	rows, listErr := index.NewPropertyDriftRepo(store).ListAll()

	if listErr != nil {
		test.Fatalf("ListAll: %v", listErr)
	}

	var refRows []index.PropertyDriftRow

	for _, row := range rows {
		switch row.Kind {
		case doctor.IssueRefDangling, doctor.IssueRefAmbiguous, doctor.IssueRefTypeMismatch, doctor.IssueRefCycle:
			refRows = append(refRows, row)
		}
	}

	return refRows
}

func TestReindex_FreshIndexResolvesForwardRefs(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "aref/auth.md", "type: ticket\ntitle: Auth\nassignee: alice\n", "Body.\n")
	writeNode(test, root, "zref/alice.md", "type: person\ntitle: alice\nname: Alice\n", "Bio.\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	report, runErr := reindex.Run(refHealConfig(test, root, store))

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	listed, _ := index.NewEdgeRepo(store).ListBySource("aref/auth")

	if len(listed) != 1 || listed[0].Type != "assignee" || listed[0].TargetID != "zref/alice" {
		test.Errorf("edges = %+v, want assignee -> zref/alice", listed)
	}

	if report.RefDangling != 0 {
		test.Errorf("RefDangling = %d, want 0", report.RefDangling)
	}

	if report.RefHealed != 1 {
		test.Errorf("RefHealed = %d, want 1", report.RefHealed)
	}

	if rows := refDriftRows(test, store); len(rows) != 0 {
		test.Errorf("ref drift rows = %+v, want none", rows)
	}
}

func TestReindex_DanglingHealsWhenTargetAppearsLater(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "aref/auth.md", "type: ticket\ntitle: Auth\nassignee: alice\n", "Body.\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	first, firstErr := reindex.Run(refHealConfig(test, root, store))

	if firstErr != nil {
		test.Fatalf("first Run: %v", firstErr)
	}

	if first.RefDangling != 1 {
		test.Errorf("first RefDangling = %d, want 1", first.RefDangling)
	}

	// The target appears later; the referencing file is untouched, so the
	// incremental skip leaves it out of the walk. The heal pass must still
	// retry its recorded dangling ref.
	writeNode(test, root, "zref/alice.md", "type: person\ntitle: alice\nname: Alice\n", "Bio.\n")

	second, secondErr := reindex.Run(refHealConfig(test, root, store))

	if secondErr != nil {
		test.Fatalf("second Run: %v", secondErr)
	}

	listed, _ := index.NewEdgeRepo(store).ListBySource("aref/auth")

	if len(listed) != 1 || listed[0].TargetID != "zref/alice" {
		test.Errorf("edges = %+v, want assignee -> zref/alice", listed)
	}

	if second.RefDangling != 0 {
		test.Errorf("second RefDangling = %d, want 0", second.RefDangling)
	}

	if second.RefHealed != 1 {
		test.Errorf("second RefHealed = %d, want 1", second.RefHealed)
	}

	if rows := refDriftRows(test, store); len(rows) != 0 {
		test.Errorf("ref drift rows = %+v, want none", rows)
	}
}

func TestReindex_GenuineDanglingSurvivesHealAndStaysReported(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "aref/auth.md", "type: ticket\ntitle: Auth\nassignee: missing\n", "Body.\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	if _, firstErr := reindex.Run(refHealConfig(test, root, store)); firstErr != nil {
		test.Fatalf("first Run: %v", firstErr)
	}

	// A plain second pass re-checks the recorded dangling ref and keeps
	// reporting it instead of going silent while the drift row lingers.
	second, secondErr := reindex.Run(refHealConfig(test, root, store))

	if secondErr != nil {
		test.Fatalf("second Run: %v", secondErr)
	}

	if listed, _ := index.NewEdgeRepo(store).ListBySource("aref/auth"); len(listed) != 0 {
		test.Errorf("edges = %+v, want none", listed)
	}

	if second.RefDangling != 1 {
		test.Errorf("second RefDangling = %d, want 1", second.RefDangling)
	}

	if second.RefHealed != 0 {
		test.Errorf("second RefHealed = %d, want 0", second.RefHealed)
	}

	if rows := refDriftRows(test, store); len(rows) != 1 {
		test.Errorf("ref drift rows = %+v, want exactly the surviving dangling", rows)
	}
}

func TestReindex_HealClearsRefDriftDespiteOtherPropertyDrift(test *testing.T) {
	root := test.TempDir()

	// The ticket carries an undeclared property, so the full-clean drift
	// wipe never fires for it; the healed ref row must be cleared anyway.
	writeNode(test, root, "aref/auth.md", "type: ticket\ntitle: Auth\nassignee: alice\nbogus: extra\n", "Body.\n")
	writeNode(test, root, "zref/alice.md", "type: person\ntitle: alice\nname: Alice\n", "Bio.\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	if _, runErr := reindex.Run(refHealConfig(test, root, store)); runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	listed, _ := index.NewEdgeRepo(store).ListBySource("aref/auth")

	if len(listed) != 1 || listed[0].TargetID != "zref/alice" {
		test.Errorf("edges = %+v, want assignee -> zref/alice", listed)
	}

	if rows := refDriftRows(test, store); len(rows) != 0 {
		test.Errorf("ref drift rows = %+v, want none", rows)
	}

	var undeclared bool

	rows, _ := index.NewPropertyDriftRepo(store).ListAll()

	for _, row := range rows {
		if row.NodeID == "aref/auth" && row.Kind == "undeclared-property" && row.Property == "bogus" {
			undeclared = true
		}
	}

	if !undeclared {
		test.Errorf("undeclared-property row for bogus missing, rows = %+v", rows)
	}
}

func TestReindex_PartialHealCountsPerRow(test *testing.T) {
	root := test.TempDir()

	// assignee heals (forward ref, target exists); reviewer stays dangling.
	writeNode(test, root, "aref/auth.md",
		"type: ticket\ntitle: Auth\nassignee: alice\nreviewer: nobody\n", "Body.\n")
	writeNode(test, root, "zref/alice.md", "type: person\ntitle: alice\nname: Alice\n", "Bio.\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	report, runErr := reindex.Run(refHealConfig(test, root, store))

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if report.RefHealed != 1 {
		test.Errorf("RefHealed = %d, want 1 (assignee only)", report.RefHealed)
	}

	if report.RefDangling != 1 {
		test.Errorf("RefDangling = %d, want 1 (reviewer)", report.RefDangling)
	}

	rows := refDriftRows(test, store)

	if len(rows) != 1 || rows[0].Property != "reviewer" {
		test.Errorf("ref drift rows = %+v, want only reviewer's", rows)
	}
}

// TestReindex_ListOfRefHealCountsPerValue pins #689 finding 2: two broken
// values of ONE list-of(ref) property are recorded, counted, and healed
// per value — not collapsed into a single drift row that miscounts partial
// progress as zero.
func TestReindex_ListOfRefHealCountsPerValue(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "aref/auth.md",
		"type: ticket\ntitle: Auth\nreviewers: [ghost1, ghost2]\n", "Body.\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	first, firstErr := reindex.Run(listRefHealConfig(test, root, store))

	if firstErr != nil {
		test.Fatalf("first Run: %v", firstErr)
	}

	if first.RefDangling != 2 {
		test.Errorf("first RefDangling = %d, want 2 (both broken values)", first.RefDangling)
	}

	if rows := refDriftRows(test, store); len(rows) != 2 {
		test.Errorf("ref drift rows = %+v, want two per-value rows", rows)
	}

	// One reviewer appears: a partial heal must report exactly one healed value
	// and leave the other dangling — the pre-#689 collapse reported zero.
	writeNode(test, root, "zref/ghost1.md", "type: person\ntitle: ghost1\nname: G1\n", "Bio.\n")

	second, secondErr := reindex.Run(listRefHealConfig(test, root, store))

	if secondErr != nil {
		test.Fatalf("second Run: %v", secondErr)
	}

	if second.RefHealed != 1 {
		test.Errorf("second RefHealed = %d, want 1 (ghost1 only)", second.RefHealed)
	}

	if second.RefDangling != 1 {
		test.Errorf("second RefDangling = %d, want 1 (ghost2)", second.RefDangling)
	}

	rows := refDriftRows(test, store)

	if len(rows) != 1 || rows[0].Value != "ghost2" {
		test.Errorf("ref drift rows = %+v, want only ghost2's", rows)
	}

	edges, _ := index.NewEdgeRepo(store).ListBySource("aref/auth")

	if len(edges) != 1 || edges[0].TargetID != "zref/ghost1" {
		test.Errorf("edges = %+v, want reviewers -> zref/ghost1", edges)
	}

	// The second reviewer appears: full heal.
	writeNode(test, root, "zref/ghost2.md", "type: person\ntitle: ghost2\nname: G2\n", "Bio.\n")

	third, thirdErr := reindex.Run(listRefHealConfig(test, root, store))

	if thirdErr != nil {
		test.Fatalf("third Run: %v", thirdErr)
	}

	if third.RefHealed != 1 {
		test.Errorf("third RefHealed = %d, want 1 (ghost2)", third.RefHealed)
	}

	if third.RefDangling != 0 {
		test.Errorf("third RefDangling = %d, want 0", third.RefDangling)
	}

	if rows := refDriftRows(test, store); len(rows) != 0 {
		test.Errorf("ref drift rows = %+v, want none", rows)
	}

	if edges, _ := index.NewEdgeRepo(store).ListBySource("aref/auth"); len(edges) != 2 {
		test.Errorf("edges = %+v, want both reviewer edges", edges)
	}
}

// TestReindex_DeletingRefTargetDropsEdgeAndRecordsDrift pins #689 finding 1:
// deleting a resolved ref target must wake the (byte-unchanged, walk-skipped)
// referrer so its now-stale derived edge is dropped and a ref_dangling drift row
// recorded — not left frozen at the dead id with no drift, invisible to the heal
// loop until a `reindex --force`.
func TestReindex_DeletingRefTargetDropsEdgeAndRecordsDrift(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "aref/auth.md", "type: ticket\ntitle: Auth\nassignee: alice\n", "Body.\n")
	writeNode(test, root, "zref/alice.md", "type: person\ntitle: alice\nname: Alice\n", "Bio.\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	if _, runErr := reindex.Run(refHealConfig(test, root, store)); runErr != nil {
		test.Fatalf("first Run: %v", runErr)
	}

	if edges, _ := index.NewEdgeRepo(store).ListBySource("aref/auth"); len(edges) != 1 || edges[0].TargetID != "zref/alice" {
		test.Fatalf("precondition: want assignee -> zref/alice, got %+v", edges)
	}

	if removeErr := os.Remove(filepath.Join(root, "zref/alice.md")); removeErr != nil {
		test.Fatalf("remove: %v", removeErr)
	}

	report, runErr := reindex.Run(refHealConfig(test, root, store))

	if runErr != nil {
		test.Fatalf("second Run: %v", runErr)
	}

	if edges, _ := index.NewEdgeRepo(store).ListBySource("aref/auth"); len(edges) != 0 {
		test.Errorf("edge must be dropped after the target is deleted, got %+v", edges)
	}

	if report.RefDangling != 1 {
		test.Errorf("RefDangling = %d, want 1", report.RefDangling)
	}

	rows := refDriftRows(test, store)

	if len(rows) != 1 || rows[0].NodeID != "aref/auth" || rows[0].Kind != doctor.IssueRefDangling {
		test.Errorf("want a ref_dangling drift row for aref/auth, got %+v", rows)
	}
}

// TestReindex_FsRenamingRefTargetRetargetsReferrer pins #689 finding 1's mv
// variant: a bare `mv` of a title-form ref target (delete old id, create new
// with the same title) must retarget the referrer's derived edge on the next
// plain reindex — pre-#689 the edge stayed at the dead id until `reindex
// --force`.
func TestReindex_FsRenamingRefTargetRetargetsReferrer(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "aref/auth.md", "type: ticket\ntitle: Auth\nassignee: alice\n", "Body.\n")
	writeNode(test, root, "zref/alice.md", "type: person\ntitle: alice\nname: Alice\n", "Bio.\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	if _, runErr := reindex.Run(refHealConfig(test, root, store)); runErr != nil {
		test.Fatalf("first Run: %v", runErr)
	}

	// Bare fs rename: the title "alice" now lives at a new id.
	if removeErr := os.Remove(filepath.Join(root, "zref/alice.md")); removeErr != nil {
		test.Fatalf("remove: %v", removeErr)
	}

	writeNode(test, root, "zref/alicia.md", "type: person\ntitle: alice\nname: Alice\n", "Bio.\n")

	report, runErr := reindex.Run(refHealConfig(test, root, store))

	if runErr != nil {
		test.Fatalf("second Run: %v", runErr)
	}

	edges, _ := index.NewEdgeRepo(store).ListBySource("aref/auth")

	if len(edges) != 1 || edges[0].TargetID != "zref/alicia" {
		test.Errorf("edge must retarget to zref/alicia on a plain reindex, got %+v", edges)
	}

	if report.RefDangling != 0 {
		test.Errorf("RefDangling = %d, want 0", report.RefDangling)
	}

	if rows := refDriftRows(test, store); len(rows) != 0 {
		test.Errorf("want no ref drift after retarget, got %+v", rows)
	}
}

func TestReindex_OrphanedRefDriftIsSweptNotHealed(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "aref/auth.md", "type: ticket\ntitle: Auth\nassignee: missing\n", "Body.\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	if _, firstErr := reindex.Run(refHealConfig(test, root, store)); firstErr != nil {
		test.Fatalf("first Run: %v", firstErr)
	}

	// The drifted file disappears; the reap removes its node row but nothing
	// used to clear its drift, leaving doctor noise the heal retried forever.
	if removeErr := os.Remove(filepath.Join(root, "aref/auth.md")); removeErr != nil {
		test.Fatalf("remove: %v", removeErr)
	}

	second, secondErr := reindex.Run(refHealConfig(test, root, store))

	if secondErr != nil {
		test.Fatalf("second Run: %v", secondErr)
	}

	if second.RefHealed != 0 {
		test.Errorf("RefHealed = %d, want 0 (swept orphan is not a heal)", second.RefHealed)
	}

	if second.RefDangling != 0 {
		test.Errorf("RefDangling = %d, want 0", second.RefDangling)
	}

	if rows := refDriftRows(test, store); len(rows) != 0 {
		test.Errorf("ref drift rows = %+v, want orphan swept", rows)
	}
}

// TestReindex_SweepsOrphanNonRefDrift pins finding #685 item 2: a reindex must
// clear property- and workflow-drift rows whose node no longer exists — not
// only the ref-kind property drift the heal pass already sweeps. Enum/type and
// workflow drift left behind by `tusk node delete`/`move` otherwise lingers and
// doctor reports the ghost id forever.
func TestReindex_SweepsOrphanNonRefDrift(test *testing.T) {
	root := test.TempDir()

	// One live file so the walk has a node to index.
	writeNode(test, root, "aref/live.md", "type: ticket\ntitle: Live\n", "Body.\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	firstConfig := refHealConfig(test, root, store)
	firstConfig.DriftLog = index.NewWorkflowDriftRepo(store)

	if _, runErr := reindex.Run(firstConfig); runErr != nil {
		test.Fatalf("first Run: %v", runErr)
	}

	// Seed orphan drift for nodes that do not exist — non-ref kinds the heal
	// pass never touches — simulating a delete/rename that left drift behind.
	propDrift := index.NewPropertyDriftRepo(store)

	if appendErr := propDrift.Append(index.PropertyDriftRow{
		NodeID: "aref/dead", NodeType: "ticket", Kind: doctor.IssueEnumViolation,
		Property: "status", Details: "value \"x\" not in [open, done]", ObservedAt: 1,
	}); appendErr != nil {
		test.Fatalf("append property drift: %v", appendErr)
	}

	wfDrift := index.NewWorkflowDriftRepo(store)

	if appendErr := wfDrift.Append(index.WorkflowDriftRow{
		NodeID: "aref/ghost", PackInstance: "kanban", PackKind: "workflow",
		ObservedStatus: "bogus", Property: "status", ObservedAt: 1,
	}); appendErr != nil {
		test.Fatalf("append workflow drift: %v", appendErr)
	}

	// A no-op reindex (no file changes) must still sweep the orphans.
	secondConfig := refHealConfig(test, root, store)
	secondConfig.DriftLog = index.NewWorkflowDriftRepo(store)

	if _, runErr := reindex.Run(secondConfig); runErr != nil {
		test.Fatalf("second Run: %v", runErr)
	}

	propRows, _ := propDrift.ListAll()

	for _, row := range propRows {
		if row.NodeID == "aref/dead" {
			test.Errorf("orphan property drift for aref/dead not swept: %+v", row)
		}
	}

	wfRows, _ := wfDrift.ListAll()

	if len(wfRows) != 0 {
		test.Errorf("orphan workflow drift not swept: %+v", wfRows)
	}
}

func TestRun_AsyncEnqueuesRefDriftedPaths(test *testing.T) {
	root := test.TempDir()

	writeNode(test, root, "aref/auth.md", "type: ticket\ntitle: Auth\nassignee: alice\n", "Body.\n")

	store, _ := index.Open(filepath.Join(root, ".tusk", "index.db"))
	defer store.Close()

	// Sync pass records the dangling ref while alice does not exist yet.
	if _, firstErr := reindex.Run(refHealConfig(test, root, store)); firstErr != nil {
		test.Fatalf("first Run: %v", firstErr)
	}

	writeNode(test, root, "zref/alice.md", "type: person\ntitle: alice\nname: Alice\n", "Bio.\n")

	// An async pass cannot drain, so it must hand the drifted path to the
	// background drainer alongside the changed file.
	asyncCfg := refHealConfig(test, root, store)
	asyncCfg.Async = true

	if _, asyncErr := reindex.Run(asyncCfg); asyncErr != nil {
		test.Fatalf("async Run: %v", asyncErr)
	}

	rows, queryErr := store.DB().Query(`SELECT node_id FROM embed_queue WHERE kind = 'reindex' ORDER BY node_id`)

	if queryErr != nil {
		test.Fatalf("query reindex rows: %v", queryErr)
	}

	defer rows.Close()

	ids := map[string]bool{}

	for rows.Next() {
		var id string

		if scanErr := rows.Scan(&id); scanErr != nil {
			test.Fatalf("scan: %v", scanErr)
		}

		ids[id] = true
	}

	if !ids["reindex:aref/auth.md"] {
		test.Errorf("drifted path not enqueued for heal; queue = %v", ids)
	}

	if !ids["reindex:zref/alice.md"] {
		test.Errorf("changed file missing from queue; queue = %v", ids)
	}
}
