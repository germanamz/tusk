package manifest_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
)

// hierarchyEdgeManifest declares one edge type; the cases below append a bad
// hierarchy setting to it.
const hierarchyEdgeManifest = `[workspace]
name = "probe"

[node-types.note]

[edge-types.part-of]
from        = ["note"]
to          = ["note"]
cardinality = "many-to-one"
`

// TestLoad_ErrorsNamePackageOnce pins the contract callers rely on to print a
// load error as is: every error Load returns starts with "manifest: ", once
// (#869).
func TestLoad_ErrorsNamePackageOnce(test *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"decode", "[workspace\nname = \"probe\"\n"},
		{"workspace setting", "[workspace]\nname = \"probe\"\nlink-case = \"bogus\"\n"},
		{"graph expansion", "[workspace]\nname = \"probe\"\n\n[query.graph-expansion]\nhops = \"x\"\n"},
		{"hierarchy max-depth", hierarchyEdgeManifest + "max-depth   = -1\n"},
		{"hierarchy default without alias", hierarchyEdgeManifest + "hierarchy-default = true\n"},
		{"hierarchy alias keyword", hierarchyEdgeManifest + "hierarchy   = \"tree\"\n"},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			_, loadErr := loadTOMLString(test, testCase.body)

			assertNamesManifestOnce(test, loadErr)
		})
	}

	test.Run("read", func(test *testing.T) {
		_, loadErr := manifest.Load(filepath.Join(test.TempDir(), "tusk.toml"))

		assertNamesManifestOnce(test, loadErr)
	})
}

func assertNamesManifestOnce(test *testing.T, loadErr error) {
	test.Helper()

	if loadErr == nil {
		test.Fatalf("Load succeeded, want an error")
	}

	message := loadErr.Error()

	if !strings.HasPrefix(message, "manifest: ") || strings.Count(message, "manifest: ") != 1 {
		test.Errorf("error = %q, want it to start with %q once", message, "manifest: ")
	}
}
