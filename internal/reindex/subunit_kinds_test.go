package reindex_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
)

// subUnitKindsManifest loads a manifest with a wikilinks edge type and the
// given [workspace] sub-units line ("" leaves the key out).
func subUnitKindsManifest(test *testing.T, subUnits string) *manifest.Manifest {
	test.Helper()

	body := "[workspace]\nname = \"test\"\n" + subUnits + "\n" +
		"\n[edge-types.references]\nfrom = [\"*\"]\nto = [\"*\"]\ncardinality = \"many-to-many\"\nwikilinks = true\n"
	path := filepath.Join(test.TempDir(), "tusk.toml")

	if writeErr := os.WriteFile(path, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(path)

	if loadErr != nil {
		test.Fatalf("manifest.Load: %v", loadErr)
	}

	return loaded
}

// TestRun_SubUnitKindsChangeConvergesWithoutForce narrows the indexed kinds
// on an index built with every kind (#853). No file changes on disk, so the
// changed setting has to force the pass that drops the left-out kinds' rows,
// their contains edges and their embedding mappings. The kept paragraph keeps
// its id, so the block link into it still resolves, and it now hangs off the
// file.
func TestRun_SubUnitKindsChangeConvergesWithoutForce(test *testing.T) {
	root, store := subUnitsFixture(test)
	indexWithSubUnits(test, root, store)

	paragraphs := subUnitKindsManifest(test, `sub-units = ["paragraph"]`)

	if report := runSubUnits(test, root, store, paragraphs, false); report.Indexed != 2 {
		test.Errorf("first pass with paragraphs only: Indexed = %d, want 2", report.Indexed)
	}

	if got := countRows(test, store, `SELECT COUNT(*) FROM nodes WHERE kind = 'subunit' AND type <> 'paragraph'`); got != 0 {
		test.Errorf("rows of left-out kinds = %d, want 0", got)
	}

	if got := countRows(test, store, `SELECT COUNT(*) FROM nodes WHERE id = 'notes/auth#S1P1' AND parent_id = 'notes/auth'`); got != 1 {
		test.Errorf("paragraph under the file = %d rows, want 1", got)
	}

	if got := countRows(test, store, `SELECT COUNT(*) FROM edges WHERE type = 'contains' AND target_id NOT IN ('notes/auth#S1P1', 'notes/home#P1')`); got != 0 {
		test.Errorf("contains edges to left-out kinds = %d, want 0", got)
	}

	if got := countRows(test, store, `SELECT COUNT(*) FROM node_embeddings WHERE instr(node_id, '#') > 0 AND node_id NOT IN ('notes/auth#S1P1', 'notes/home#P1')`); got != 0 {
		test.Errorf("embedding mappings of left-out kinds = %d, want 0", got)
	}

	if got := countRows(test, store, `SELECT COUNT(*) FROM node_embeddings WHERE node_id = 'notes/auth#S1P1'`); got != 1 {
		test.Errorf("kept paragraph's embedding mappings = %d, want 1 (same id, same vector)", got)
	}

	if targets := blockLinkTargets(test, store); len(targets) == 0 || targets[0] != "notes/auth#S1P1" {
		test.Errorf("block link targets = %q, want the kept paragraph", targets)
	}

	if report := runSubUnits(test, root, store, paragraphs, false); report.Indexed != 0 {
		test.Errorf("second pass with paragraphs only: Indexed = %d, want 0", report.Indexed)
	}
}

// TestRun_SubUnitKindsSettingIsCanonical records the setting by the kinds it
// picks, not how the list is written: reordering a list, or listing every
// kind where the default was, forces no pass.
func TestRun_SubUnitKindsSettingIsCanonical(test *testing.T) {
	root, store := subUnitsFixture(test)

	runSubUnits(test, root, store, subUnitKindsManifest(test, `sub-units = ["section", "paragraph"]`), false)

	if report := runSubUnits(test, root, store, subUnitKindsManifest(test, `sub-units = ["paragraph", "section", "paragraph"]`), false); report.Indexed != 0 {
		test.Errorf("same kinds written differently: Indexed = %d, want 0", report.Indexed)
	}

	runSubUnits(test, root, store, subUnitKindsManifest(test, ""), false)

	every := `sub-units = ["table-cell", "blockquote", "code-block", "list-item", "paragraph", "section"]`

	if report := runSubUnits(test, root, store, subUnitKindsManifest(test, every), false); report.Indexed != 0 {
		test.Errorf("every kind listed after the default: Indexed = %d, want 0", report.Indexed)
	}

	if report := runSubUnits(test, root, store, subUnitKindsManifest(test, `sub-units = []`), false); report.Indexed != 2 {
		test.Errorf("empty list after every kind: Indexed = %d, want 2", report.Indexed)
	}

	assertNoSubUnits(test, store)

	if report := runSubUnits(test, root, store, subUnitKindsManifest(test, `sub-units = false`), false); report.Indexed != 0 {
		test.Errorf("false after an empty list: Indexed = %d, want 0 (both index nothing)", report.Indexed)
	}
}
