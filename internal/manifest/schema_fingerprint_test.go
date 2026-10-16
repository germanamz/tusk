package manifest_test

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
)

// schemaFixture is a manifest declaring one of each table SchemaFingerprint
// hashes. Tests derive variants from it with strings.Replace.
const schemaFixture = `[workspace]
name = "x"

[node-types.note]
description = "A note."
properties = [
  { name = "owner", type = "ref", to = "note", description = "Who owns it." },
  { name = "status", type = "string" },
]

[edge-types.references]
description = "Body links."
from        = ["note"]
to          = ["note"]
cardinality = "many-to-many"
wikilinks   = true

[behaviors.workflow.notes]
applies-to = ["note"]
states = [
  { name = "open", initial = true },
  { name = "done", terminal = true },
]
transitions = [{ from = "open", to = "done" }]
`

func fingerprintOf(test *testing.T, body string) string {
	test.Helper()

	loaded := loadTOMLFromString(test, body)
	manifest.MergeBuiltinPacks(loaded)

	return loaded.SchemaFingerprint()
}

func TestSchemaFingerprint_StableAcrossLoads(test *testing.T) {
	first := fingerprintOf(test, schemaFixture)
	second := fingerprintOf(test, schemaFixture)

	if first == "" {
		test.Fatal("SchemaFingerprint() = \"\" for a manifest with types")
	}

	if first != second {
		test.Errorf("SchemaFingerprint() differs across loads of one file: %q vs %q", first, second)
	}
}

func TestSchemaFingerprint_NilManifestIsEmpty(test *testing.T) {
	var loaded *manifest.Manifest

	if got := loaded.SchemaFingerprint(); got != "" {
		test.Errorf("SchemaFingerprint() on nil = %q, want \"\"", got)
	}
}

func TestSchemaFingerprint_ChangesWithDerivingDeclarations(test *testing.T) {
	base := fingerprintOf(test, schemaFixture)

	for name, edit := range map[string][2]string{
		"edge wikilinks off":     {"wikilinks   = true", "wikilinks   = false"},
		"edge from list":         {`from        = ["note"]`, `from        = ["*"]`},
		"edge type removed":      {"[edge-types.references]", "[edge-types.cites]"},
		"property type":          {`{ name = "status", type = "string" }`, `{ name = "status", type = "int" }`},
		"property required":      {`{ name = "status", type = "string" }`, `{ name = "status", type = "string", required = true }`},
		"property ref target":    {`to = "note", description`, `to = "*", description`},
		"node type renamed":      {"[node-types.note]", "[node-types.memo]\n[node-types.note]"},
		"workflow transition":    {`transitions = [{ from = "open", to = "done" }]`, `transitions = []`},
		"workflow applies-to":    {`applies-to = ["note"]`, `applies-to = ["memo"]`},
		"edge-type order option": {"wikilinks   = true", "wikilinks   = true\nordered     = \"status\""},
	} {
		edited := strings.Replace(schemaFixture, edit[0], edit[1], 1)

		if edited == schemaFixture {
			test.Fatalf("%s: edit %q matched nothing in the fixture", name, edit[0])
		}

		if got := fingerprintOf(test, edited); got == base {
			test.Errorf("%s: SchemaFingerprint() unchanged, want it to change", name)
		}
	}
}

func TestSchemaFingerprint_IgnoresDescriptions(test *testing.T) {
	base := fingerprintOf(test, schemaFixture)

	edited := strings.NewReplacer(
		`description = "A note."`, `description = "A short note."`,
		`description = "Who owns it."`, `description = "The owning note."`,
		`description = "Body links."`, `description = "Wikilinks in the body."`,
	).Replace(schemaFixture)

	if got := fingerprintOf(test, edited); got != base {
		test.Errorf("SchemaFingerprint() changed on a description-only edit")
	}
}

func TestSchemaFingerprint_IgnoresUnrelatedSections(test *testing.T) {
	base := fingerprintOf(test, schemaFixture)

	edited := schemaFixture + `
[embeddings]
provider = "ollama"
model    = "nomic-embed-text"
endpoint = "http://localhost:11434"
dim      = 768

[rule.no-owner]
filter = "type=note AND NOT owner"
`

	if got := fingerprintOf(test, edited); got != base {
		test.Errorf("SchemaFingerprint() changed when only [embeddings] and [rule] were added")
	}
}
