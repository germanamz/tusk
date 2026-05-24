package manifest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
)

// TestSubUnitsEnabled_DefaultsToTrueWhenAbsent confirms that the
// `[workspace] sub-units` flag defaults to true when the key is not
// declared. The default is part of the P2 contract: existing
// workspaces upgrade into sub-unit indexing unless they explicitly opt
// out.
func TestSubUnitsEnabled_DefaultsToTrueWhenAbsent(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"
`)

	if !loaded.SubUnitsEnabled() {
		test.Errorf("SubUnitsEnabled() = false, want true (default)")
	}
}

// TestSubUnitsEnabled_ExplicitTrue covers the redundant-but-valid case
// where the user writes the default explicitly.
func TestSubUnitsEnabled_ExplicitTrue(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"
sub-units = true
`)

	if !loaded.SubUnitsEnabled() {
		test.Errorf("SubUnitsEnabled() = false, want true (explicit)")
	}
}

// TestSubUnitsEnabled_ExplicitFalse confirms an explicit opt-out
// resolves to false. This is the only path that disables the
// sub-document pack — the engine must respect it.
func TestSubUnitsEnabled_ExplicitFalse(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"
sub-units = false
`)

	if loaded.SubUnitsEnabled() {
		test.Errorf("SubUnitsEnabled() = true, want false")
	}
}

// TestMergeBuiltinPacks_AddsSubdocumentNodeTypes confirms that calling
// MergeBuiltinPacks installs the pack's six node types when sub-units
// is enabled.
func TestMergeBuiltinPacks_AddsSubdocumentNodeTypes(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"
`)

	manifest.MergeBuiltinPacks(loaded)

	required := []string{"section", "paragraph", "list-item", "code-block", "blockquote", "table-cell"}

	for _, name := range required {
		if _, has := loaded.NodeTypes[name]; !has {
			test.Errorf("NodeTypes missing %q after merge", name)
		}
	}
}

// TestMergeBuiltinPacks_AddsContainsEdgeType confirms that the pack's
// single edge type (with inverse = contained-by) is installed.
func TestMergeBuiltinPacks_AddsContainsEdgeType(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"
`)

	manifest.MergeBuiltinPacks(loaded)

	edge, has := loaded.EdgeTypes["contains"]

	if !has {
		test.Fatalf("EdgeTypes missing 'contains' after merge")
	}

	if edge.Inverse != "contained-by" {
		test.Errorf("Inverse = %q, want %q", edge.Inverse, "contained-by")
	}

	if edge.Cardinality != manifest.CardinalityOneToMany {
		test.Errorf("Cardinality = %q, want one-to-many", edge.Cardinality)
	}
}

// TestMergeBuiltinPacks_NoOpWhenDisabled confirms the pack stays out
// of the manifest when the user opts out.
func TestMergeBuiltinPacks_NoOpWhenDisabled(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"
sub-units = false
`)

	manifest.MergeBuiltinPacks(loaded)

	for _, name := range []string{"section", "paragraph", "contains"} {
		if _, has := loaded.NodeTypes[name]; has {
			test.Errorf("NodeTypes contains %q with sub-units disabled", name)
		}

		if _, has := loaded.EdgeTypes[name]; has {
			test.Errorf("EdgeTypes contains %q with sub-units disabled", name)
		}
	}

	if len(loaded.SubUnitConflicts) != 0 {
		test.Errorf("SubUnitConflicts populated when sub-units disabled: %+v", loaded.SubUnitConflicts)
	}
}

// TestMergeBuiltinPacks_ReportsReservedNodeTypeConflict confirms a
// user-declared node type whose name collides with a reserved name
// surfaces a SubUnitConflict and is removed from NodeTypes (the
// built-in declaration wins).
func TestMergeBuiltinPacks_ReportsReservedNodeTypeConflict(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"

[node-types.section]
description = "user override"
properties = [
    { name = "heading-level", type = "string" },
    { name = "custom",        type = "string" },
]
`)

	manifest.MergeBuiltinPacks(loaded)

	if len(loaded.SubUnitConflicts) == 0 {
		test.Fatalf("SubUnitConflicts empty; expected at least one node-type conflict")
	}

	nodeTypeConflicts := 0
	propConflicts := 0

	for _, conflict := range loaded.SubUnitConflicts {
		switch conflict.Kind {
		case "node-type":
			if conflict.Name == "section" {
				nodeTypeConflicts++
			}
		case "property":
			if conflict.OwnerType == "section" && conflict.Name == "heading-level" {
				propConflicts++
			}
		}
	}

	if nodeTypeConflicts != 1 {
		test.Errorf("expected 1 node-type conflict for section, got %d", nodeTypeConflicts)
	}

	if propConflicts != 1 {
		test.Errorf("expected 1 property conflict for section.heading-level, got %d", propConflicts)
	}

	// The built-in declaration should now be in place.
	got, has := loaded.NodeTypes["section"]

	if !has {
		test.Fatalf("section node type missing after conflict resolution")
	}

	if got.Description == "user override" {
		test.Errorf("section description = %q, expected built-in to win", got.Description)
	}
}

// TestMergeBuiltinPacks_ReportsReservedEdgeTypeConflict confirms a
// user-declared edge type colliding with `contains` or `contained-by`
// surfaces a conflict.
func TestMergeBuiltinPacks_ReportsReservedEdgeTypeConflict(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"

[node-types.note]
properties = []

[edge-types.contains]
from = ["note"]
to = ["note"]
cardinality = "many-to-many"
`)

	manifest.MergeBuiltinPacks(loaded)

	found := false

	for _, conflict := range loaded.SubUnitConflicts {
		if conflict.Kind == "edge-type" && conflict.Name == "contains" {
			found = true
			break
		}
	}

	if !found {
		test.Errorf("expected edge-type conflict for 'contains'; got %+v", loaded.SubUnitConflicts)
	}

	// The built-in declaration wins.
	edge := loaded.EdgeTypes["contains"]

	if edge.Inverse != "contained-by" {
		test.Errorf("Inverse = %q, want contained-by (built-in)", edge.Inverse)
	}
}

// TestMergeBuiltinPacks_NoConflictForUnrelatedDeclarations confirms a
// user manifest that declares non-reserved names triggers no
// conflicts. Regression coverage for false positives.
func TestMergeBuiltinPacks_NoConflictForUnrelatedDeclarations(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"

[node-types.ticket]
properties = [
    { name = "status", type = "string" },
]

[edge-types.references]
from = ["*"]
to = ["*"]
cardinality = "many-to-many"
`)

	manifest.MergeBuiltinPacks(loaded)

	if len(loaded.SubUnitConflicts) != 0 {
		test.Errorf("unexpected SubUnitConflicts: %+v", loaded.SubUnitConflicts)
	}

	if _, has := loaded.NodeTypes["ticket"]; !has {
		test.Errorf("user-declared ticket node type was dropped")
	}

	if _, has := loaded.EdgeTypes["references"]; !has {
		test.Errorf("user-declared references edge type was dropped")
	}
}

// TestMergeBuiltinPacks_Idempotent confirms running the merge twice on
// the same manifest produces the same end state (no duplicate
// conflicts, no duplicate node-type insertions).
func TestMergeBuiltinPacks_Idempotent(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"

[node-types.section]
description = "user override"
properties = []
`)

	manifest.MergeBuiltinPacks(loaded)

	firstConflicts := len(loaded.SubUnitConflicts)
	firstNodes := len(loaded.NodeTypes)

	manifest.MergeBuiltinPacks(loaded)

	if len(loaded.SubUnitConflicts) != firstConflicts {
		test.Errorf("conflicts changed across calls: %d -> %d", firstConflicts, len(loaded.SubUnitConflicts))
	}

	if len(loaded.NodeTypes) != firstNodes {
		test.Errorf("node-type count changed across calls: %d -> %d", firstNodes, len(loaded.NodeTypes))
	}
}

// loadInlineManifest is a small helper that writes body to a temp file
// and invokes manifest.Load on it. Used by the sub-units tests instead
// of constructing a Manifest literal so the toml.MetaData captured by
// Load is present (SubUnitsEnabled discriminates absent-vs-explicit
// through MetaData.IsDefined).
func loadInlineManifest(test *testing.T, body string) *manifest.Manifest {
	test.Helper()

	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")

	if writeErr := os.WriteFile(path, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(path)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	return loaded
}
