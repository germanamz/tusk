package reindex_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/reindex"
)

// The schemas the #831 report walks through, in order: a vault with nothing
// declared, then the vault pack's note type and wikilinks edge type, then the
// edge type deleted again, then note gaining an owner ref property.
const (
	schemaNone = "[workspace]\nname = \"test\"\n"

	schemaNoteDecl = "\n[node-types.note]\ndescription = \"A note.\"\nproperties = []\n"

	schemaReferences = "\n[edge-types.references]\nfrom = [\"*\"]\nto = [\"*\"]\ncardinality = \"many-to-many\"\nwikilinks = true\n"

	schemaOwnerDecl = "\n[node-types.note]\nproperties = [{ name = \"owner\", type = \"ref\", to = \"note\" }]\n"
)

// schemaChangeFixture writes the report's two notes, home linking auth from
// its body and naming it in an owner property, and opens an index beside them.
func schemaChangeFixture(test *testing.T) (string, *index.Index) {
	test.Helper()

	root := test.TempDir()

	writeNode(test, root, "notes/auth.md", "type: note\ntitle: Auth\n", "Auth.\n")
	writeNode(test, root, "notes/home.md", "type: note\ntitle: Home\nowner: \"[[notes/auth]]\"\n", "See [[notes/auth]].\n")

	store, openErr := index.Open(filepath.Join(root, ".tusk", "index.db"))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	test.Cleanup(func() { store.Close() })

	return root, store
}

// schemaManifest loads body the way the CLI does, built-in packs merged.
func schemaManifest(test *testing.T, body string) *manifest.Manifest {
	test.Helper()

	path := filepath.Join(test.TempDir(), "tusk.toml")

	if writeErr := os.WriteFile(path, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	loaded, loadErr := manifest.Load(path)

	if loadErr != nil {
		test.Fatalf("manifest.Load: %v", loadErr)
	}

	manifest.MergeBuiltinPacks(loaded)

	return loaded
}

// runSchema runs one incremental pass with the per-file set the CLI wires:
// the manifest's edge and node types and the property drift log.
func runSchema(test *testing.T, root string, store *index.Index, loaded *manifest.Manifest) *reindex.Report {
	test.Helper()

	report, runErr := reindex.Run(withGen(store, reindex.Config{
		Root:          root,
		Repo:          index.NewNodeRepo(store),
		Edges:         index.NewEdgeRepo(store),
		EdgeTypes:     loaded.EdgeTypes,
		NodeTypes:     loaded.NodeTypes,
		PropertyDrift: index.NewPropertyDriftRepo(store),
		Manifest:      loaded,
	}))

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	return report
}

func homeEdges(test *testing.T, store *index.Index, edgeType string) int {
	test.Helper()

	return countRows(test, store,
		`SELECT COUNT(*) FROM edges WHERE source_id LIKE 'notes/home%' AND target_id = 'notes/auth' AND type = '`+edgeType+`'`)
}

func ownerDrift(test *testing.T, store *index.Index) int {
	test.Helper()

	return countRows(test, store,
		`SELECT COUNT(*) FROM property_drift WHERE node_id = 'notes/home' AND kind = 'undeclared-property' AND property = 'owner'`)
}

// TestRun_SchemaChangeReachesUnchangedFiles is the #831 report: every edit to
// tusk.toml below changes what the worker derives from both notes, yet neither
// note changes on disk, so before the schema was recorded each incremental
// pass skipped them and kept the edges and drift of the first schema.
func TestRun_SchemaChangeReachesUnchangedFiles(test *testing.T) {
	root, store := schemaChangeFixture(test)

	if report := runSchema(test, root, store, schemaManifest(test, schemaNone)); report.Indexed != 2 {
		test.Fatalf("setup: Indexed = %d, want 2", report.Indexed)
	}

	if got := homeEdges(test, store, "references"); got != 0 {
		test.Fatalf("setup: references edges = %d with no edge type declared", got)
	}

	// Steps 3-6: declaring note and a wikilinks edge type.
	report := runSchema(test, root, store, schemaManifest(test, schemaNone+schemaNoteDecl+schemaReferences))

	if !report.SchemaChanged || report.Indexed != 2 {
		test.Errorf("after declaring references: SchemaChanged = %v, Indexed = %d, want true and 2", report.SchemaChanged, report.Indexed)
	}

	if got := homeEdges(test, store, "references"); got == 0 {
		test.Errorf("references edges = 0 after declaring a wikilinks edge type")
	}

	if got := ownerDrift(test, store); got == 0 {
		test.Errorf("no undeclared-property drift for owner once note is declared without it")
	}

	// Step 7: deleting the edge type.
	runSchema(test, root, store, schemaManifest(test, schemaNone+schemaNoteDecl))

	if got := homeEdges(test, store, "references"); got != 0 {
		test.Errorf("references edges = %d after the edge type was deleted, want 0", got)
	}

	// Steps 8-9: declaring owner as a ref property.
	runSchema(test, root, store, schemaManifest(test, schemaNone+schemaOwnerDecl))

	if got := homeEdges(test, store, "owner"); got != 1 {
		test.Errorf("owner edges = %d after declaring the ref property, want 1", got)
	}

	if got := ownerDrift(test, store); got != 0 {
		test.Errorf("undeclared-property drift for owner = %d after declaring it, want 0", got)
	}
}

// TestRun_SchemaChangeForcesOnePass checks the schema is recorded once the
// pass that applied it succeeds, so the next pass skips unchanged files again,
// and that a description edit, which nothing indexed reads, costs no pass.
func TestRun_SchemaChangeForcesOnePass(test *testing.T) {
	root, store := schemaChangeFixture(test)
	declared := schemaManifest(test, schemaNone+schemaNoteDecl+schemaReferences)

	runSchema(test, root, store, declared)

	if report := runSchema(test, root, store, declared); report.SchemaChanged || report.Indexed != 0 {
		test.Errorf("second pass, same schema: SchemaChanged = %v, Indexed = %d, want false and 0", report.SchemaChanged, report.Indexed)
	}

	redescribed := schemaManifest(test, schemaNone+
		"\n[node-types.note]\ndescription = \"Something written down.\"\nproperties = []\n"+schemaReferences)

	if report := runSchema(test, root, store, redescribed); report.SchemaChanged || report.Indexed != 0 {
		test.Errorf("description-only edit: SchemaChanged = %v, Indexed = %d, want false and 0", report.SchemaChanged, report.Indexed)
	}
}

// TestRun_SchemaUnrecordedForcesOnePass covers an index from before the schema
// was recorded: it may hold edges from any earlier schema, so the first pass
// of this build re-processes every file once.
func TestRun_SchemaUnrecordedForcesOnePass(test *testing.T) {
	root, store := schemaChangeFixture(test)
	declared := schemaManifest(test, schemaNone+schemaNoteDecl+schemaReferences)

	runSchema(test, root, store, declared)

	if _, execErr := store.DB().Exec(`DELETE FROM meta WHERE key = ?`, index.ManifestSchemaKey); execErr != nil {
		test.Fatalf("drop %s: %v", index.ManifestSchemaKey, execErr)
	}

	if report := runSchema(test, root, store, declared); !report.SchemaChanged || report.Indexed != 2 {
		test.Errorf("pass on an index without the key: SchemaChanged = %v, Indexed = %d, want true and 2", report.SchemaChanged, report.Indexed)
	}
}
