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
