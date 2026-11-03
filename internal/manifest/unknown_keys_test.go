package manifest_test

import (
	"reflect"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
)

// A key no part of tusk reads is recorded, not rejected (#852): a typo inside
// a known table, or a table a newer tusk added. An unknown table is one entry,
// not one per key under it. Rule keys belong to rule-invalid, and a behavior
// instance's settings to its behavior, so neither is listed here.
func TestLoad_RecordsUnknownKeys(test *testing.T) {
	loaded, loadErr := loadManifestBody(test, `top-level-typo = 1

[workspace]
name = "x"
nmae = "typo"

[node-types.note]
properties = [{ name = "a", type = "string", requried = true }, { name = "order", type = "int" }]

[edge-types.depends-on]
from = ["note"]
to = ["note"]
cardinality = "many-to-many"
acylic = true
ordered = true

[query]
lexical-weight = 0.5
type-weights = { note = 0.8 }

[query.graph-expansion]
enabled = true
wieght = 0.3

[graph.cluster]
by = "community"
community-edges = ["depends-on"]
resolution = 1.0

[alias.recent]
command = "query"
descripton = "typo"
args = { sort = "mtime" }

[context]
pinned = ["a"]

[context.recent]
command = "query"
colour = "blue"

[rule.typo]
filter = "type=note"
severty = "warning"

[behaviors.workflow.notes]
applies-to = ["note"]
states = [{ name = "open", initial = true }, { name = "done", terminal = true }]
transitions = [{ from = "open", to = "done" }]
anything = 1

[mystery]
a = 1

[mystery.deeper]
b = 2
`)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	want := []manifest.UnknownKey{
		{Key: "alias.recent.descripton"},
		{Key: "context.recent.colour"},
		{Key: "edge-types.depends-on.acylic"},
		{Key: "mystery", Table: true},
		{Key: "node-types.note.properties.requried"},
		{Key: "query.graph-expansion.wieght"},
		{Key: "top-level-typo"},
		{Key: "workspace.nmae"},
	}

	if !reflect.DeepEqual(loaded.UnknownKeys, want) {
		test.Fatalf("UnknownKeys =\n%+v\nwant\n%+v", loaded.UnknownKeys, want)
	}
}

func TestLoad_NoUnknownKeysInAPlainManifest(test *testing.T) {
	loaded, loadErr := loadManifestBody(test, schemaFixture)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	if len(loaded.UnknownKeys) != 0 {
		test.Fatalf("UnknownKeys = %+v, want none", loaded.UnknownKeys)
	}
}
