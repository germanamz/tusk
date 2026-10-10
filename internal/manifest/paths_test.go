package manifest_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
)

func loadManifestBody(test *testing.T, body string) (*manifest.Manifest, error) {
	test.Helper()

	manifestPath := filepath.Join(test.TempDir(), "tusk.toml")

	if writeErr := os.WriteFile(manifestPath, []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	return manifest.Load(manifestPath)
}

func TestLoad_PathsTypeMayOmitToAndCardinality(test *testing.T) {
	loaded, loadErr := loadManifestBody(test, `[workspace]
name = "x"

[edge-types.describes]
from = ["technical"]
paths = true
`)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	describes := loaded.EdgeTypes["describes"]

	if !describes.Paths {
		test.Errorf("Paths = false, want true")
	}

	if describes.Cardinality != manifest.CardinalityManyToMany {
		test.Errorf("Cardinality = %q, want the many-to-many default", describes.Cardinality)
	}

	if len(describes.To) != 0 {
		test.Errorf("To = %v, want empty", describes.To)
	}

	if describes.AllowsTarget("note") {
		test.Errorf("a paths type with no to must allow no node target")
	}
}

func TestLoad_PathsTypeKeepsDeclaredCardinality(test *testing.T) {
	loaded, loadErr := loadManifestBody(test, `[workspace]
name = "x"

[edge-types.references]
from = ["*"]
to = ["*"]
cardinality = "one-to-many"
wikilinks = true
paths = true
`)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	if got := loaded.EdgeTypes["references"].Cardinality; got != manifest.CardinalityOneToMany {
		test.Errorf("Cardinality = %q, want one-to-many", got)
	}
}

func TestLoad_NonPathsTypeStillNeedsTo(test *testing.T) {
	_, loadErr := loadManifestBody(test, `[workspace]
name = "x"

[edge-types.bad]
from = ["ticket"]
cardinality = "many-to-many"
`)

	if loadErr == nil {
		test.Fatalf("expected an error for a missing to list")
	}
}

func TestPathEdgeTypeNames_SortedPathsTypesOnly(test *testing.T) {
	edgeTypes := manifest.EdgeTypes{
		"mentions":  {Paths: true},
		"blocks":    {},
		"describes": {Paths: true},
	}

	got := manifest.PathEdgeTypeNames(edgeTypes)
	want := []string{"describes", "mentions"}

	if !slices.Equal(got, want) {
		test.Errorf("PathEdgeTypeNames = %v, want %v", got, want)
	}

	if names := manifest.PathEdgeTypeNames(manifest.EdgeTypes{"blocks": {}}); len(names) != 0 {
		test.Errorf("PathEdgeTypeNames = %v, want none", names)
	}
}
