package reindex_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/reindex"
)

// subUnitsFixture writes a note with a heading, a block-id paragraph and a
// list, plus a second note that links the block, and opens an index beside
// them.
func subUnitsFixture(test *testing.T) (string, *index.Index) {
	test.Helper()

	root := test.TempDir()

	writeNode(test, root, "notes/auth.md", "type: note\ntitle: Auth\n",
		"# Login\n\nUse SSO. ^sso\n\n- step one\n- step two\n")
	writeNode(test, root, "notes/home.md", "type: note\ntitle: Home\n", "See [[notes/auth#^sso]].\n")

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	return root, store
}

// subUnitsManifest loads a manifest with a wikilinks edge type, writing
// `sub-units = false` when enabled is false and leaving the key out (the
// default, on) otherwise. SubUnitsEnabled reads the TOML metadata, so the
// manifest has to come from manifest.Load.
func subUnitsManifest(test *testing.T, enabled bool) *manifest.Manifest {
	test.Helper()

	body := "[workspace]\nname = \"test\"\n"

	if !enabled {
		body += "sub-units = false\n"
	}

	body += "\n[edge-types.references]\nfrom = [\"*\"]\nto = [\"*\"]\ncardinality = \"many-to-many\"\nwikilinks = true\n"

	path := filepath.Join(test.TempDir(), "tusk.toml")

	if writeErr := os.WriteFile(path, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(path)

	if loadErr != nil {
		test.Fatalf("manifest.Load: %v", loadErr)
	}

	if loaded.SubUnitsEnabled() != enabled {
		test.Fatalf("SubUnitsEnabled() = %v, want %v", loaded.SubUnitsEnabled(), enabled)
	}

	return loaded
}

func runSubUnits(test *testing.T, root string, store *index.Index, loaded *manifest.Manifest, force bool) *reindex.Report {
	test.Helper()

	report, runErr := reindex.Run(withGen(store, reindex.Config{
		Root:      root,
		Repo:      index.NewNodeRepo(store),
		Edges:     index.NewEdgeRepo(store),
		EdgeTypes: loaded.EdgeTypes,
		Manifest:  loaded,
		Force:     force,
	}))

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	return report
}

// countRows runs a COUNT(*) query against store.
func countRows(test *testing.T, store *index.Index, query string) int {
	test.Helper()

	var count int

	if scanErr := store.DB().QueryRow(query).Scan(&count); scanErr != nil {
		test.Fatalf("%s: %v", query, scanErr)
	}

	return count
}

// blockLinkTargets returns where the home note's block reference points.
func blockLinkTargets(test *testing.T, store *index.Index) []string {
	test.Helper()

	rows, queryErr := store.DB().Query(`SELECT target_id FROM edges WHERE link = 'notes/auth#^sso' ORDER BY target_id`)

	if queryErr != nil {
		test.Fatalf("query block link: %v", queryErr)
	}

	defer rows.Close()

	var targets []string

	for rows.Next() {
		var target string

		if scanErr := rows.Scan(&target); scanErr != nil {
			test.Fatalf("scan block link: %v", scanErr)
		}

		targets = append(targets, target)
	}

	return targets
}

// assertNoSubUnits checks that nothing an earlier pass with sub-units on wrote
// survives: rows, the edges they source, the files' contains edges, their
// embedding mappings, and links resolved into them.
func assertNoSubUnits(test *testing.T, store *index.Index) {
	test.Helper()

	if got := countRows(test, store, `SELECT COUNT(*) FROM nodes WHERE kind = 'subunit'`); got != 0 {
		test.Errorf("sub-unit rows = %d, want 0", got)
	}

	if got := countRows(test, store, `SELECT COUNT(*) FROM edges WHERE type = 'contains'`); got != 0 {
		test.Errorf("contains edges = %d, want 0", got)
	}

	if got := countRows(test, store, `SELECT COUNT(*) FROM edges WHERE instr(source_id, '#') > 0`); got != 0 {
		test.Errorf("edges sourced from sub-units = %d, want 0", got)
	}

	if got := countRows(test, store, `SELECT COUNT(*) FROM node_embeddings WHERE instr(node_id, '#') > 0`); got != 0 {
		test.Errorf("sub-unit embedding mappings = %d, want 0", got)
	}

	for _, target := range blockLinkTargets(test, store) {
		if target != "notes/auth" {
			test.Errorf("block link targets %q, want the note itself once its units are gone", target)
		}
	}
}

// indexWithSubUnits runs a pass with sub-units on and maps an embedding onto
// every sub-unit, so the tests can see the mappings go with the rows.
func indexWithSubUnits(test *testing.T, root string, store *index.Index) int {
	test.Helper()

	runSubUnits(test, root, store, subUnitsManifest(test, true), false)

	total := countRows(test, store, `SELECT COUNT(*) FROM nodes WHERE kind = 'subunit'`)

	if total == 0 {
		test.Fatalf("setup: no sub-unit rows with sub-units on")
	}

	if _, execErr := store.DB().Exec(`
		INSERT INTO node_embeddings (node_id, chunk_idx, content_hash, model)
		SELECT id, 0, 'hash-' || id, 'fake' FROM nodes WHERE kind = 'subunit'`); execErr != nil {
		test.Fatalf("setup: map embeddings: %v", execErr)
	}

	targets := blockLinkTargets(test, store)

	if len(targets) == 0 || targets[0] == "notes/auth" {
		test.Fatalf("setup: block link targets = %q, want the unit the marker names", targets)
	}

	return total
}

// TestRun_SubUnitsOffForceDropsRows is the #827 report: with sub-units turned
// off, a forced pass re-read every file and still left every sub-unit row,
// because the worker skipped the sub-unit pass outright and the sync is the
// only code that deletes those rows.
func TestRun_SubUnitsOffForceDropsRows(test *testing.T) {
	root, store := subUnitsFixture(test)
	total := indexWithSubUnits(test, root, store)

	report := runSubUnits(test, root, store, subUnitsManifest(test, false), true)

	assertNoSubUnits(test, store)

	if report.SubUnitsDeleted != total {
		test.Errorf("SubUnitsDeleted = %d, want %d", report.SubUnitsDeleted, total)
	}
}

// TestRun_SubUnitsOffConvergesWithoutForce covers the files nothing re-reads:
// turning the setting off changes no file on disk, so the incremental skip
// would pass over every one of them unless the change itself forces a pass.
func TestRun_SubUnitsOffConvergesWithoutForce(test *testing.T) {
	root, store := subUnitsFixture(test)
	indexWithSubUnits(test, root, store)

	runSubUnits(test, root, store, subUnitsManifest(test, false), false)

	assertNoSubUnits(test, store)
}

// TestRun_SubUnitsOnFillsUnchangedFiles is the other direction: turning
// sub-units back on has to reach the files the last pass indexed without them.
func TestRun_SubUnitsOnFillsUnchangedFiles(test *testing.T) {
	root, store := subUnitsFixture(test)

	runSubUnits(test, root, store, subUnitsManifest(test, false), false)

	if got := countRows(test, store, `SELECT COUNT(*) FROM nodes WHERE kind = 'subunit'`); got != 0 {
		test.Fatalf("setup: sub-unit rows = %d with sub-units off", got)
	}

	report := runSubUnits(test, root, store, subUnitsManifest(test, true), false)

	if report.SubUnitsInserted == 0 {
		test.Errorf("SubUnitsInserted = 0 after turning sub-units on, want the unchanged files' units")
	}

	targets := blockLinkTargets(test, store)

	if len(targets) == 0 || targets[0] == "notes/auth" {
		test.Errorf("block link targets = %q, want the unit the marker names", targets)
	}
}

// TestRebuildConfig_DerivesSubUnits covers the schema rebuild: its config
// carried no manifest, so the worker skipped the sub-unit pass and the rebuilt
// index held no sub-units, while meta recorded the default setting (on) and so
// no later pass would fill them.
func TestRebuildConfig_DerivesSubUnits(test *testing.T) {
	root, store := subUnitsFixture(test)

	cfg := reindex.RebuildConfig(root, store, subUnitsManifest(test, true))
	cfg.Workers = 2

	if _, runErr := reindex.Run(cfg); runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	if got := countRows(test, store, `SELECT COUNT(*) FROM nodes WHERE kind = 'subunit'`); got == 0 {
		test.Errorf("sub-unit rows = 0 after a rebuild with sub-units on")
	}
}

// TestRun_SubUnitsSettingForcesOnePass checks the setting is recorded once the
// pass that applied it succeeds: the next pass under the same setting skips
// the unchanged files again. An index from before the setting was recorded
// holds no key, which reads as on, so the default needs no pass on upgrade.
func TestRun_SubUnitsSettingForcesOnePass(test *testing.T) {
	root, store := subUnitsFixture(test)
	on := subUnitsManifest(test, true)
	off := subUnitsManifest(test, false)

	runSubUnits(test, root, store, on, false)

	if report := runSubUnits(test, root, store, off, false); report.Indexed != 2 {
		test.Errorf("first pass with sub-units off: Indexed = %d, want 2", report.Indexed)
	}

	if report := runSubUnits(test, root, store, off, false); report.Indexed != 0 {
		test.Errorf("second pass with sub-units off: Indexed = %d, want 0", report.Indexed)
	}

	runSubUnits(test, root, store, on, false)

	if _, execErr := store.DB().Exec(`DELETE FROM meta WHERE key = 'sub_units'`); execErr != nil {
		test.Fatalf("drop sub_units key: %v", execErr)
	}

	if report := runSubUnits(test, root, store, on, false); report.Indexed != 0 {
		test.Errorf("pass on an index without the key, sub-units on: Indexed = %d, want 0", report.Indexed)
	}
}
