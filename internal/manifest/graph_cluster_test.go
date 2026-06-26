package manifest_test

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
)

// TestDefaultGraphCluster confirms the default is by = "type" and all other
// fields are zero, so an absent [graph.cluster] block reproduces today's
// type-based coloring exactly.
func TestDefaultGraphCluster(test *testing.T) {
	defaults := manifest.DefaultGraphCluster()

	if defaults.By != "type" {
		test.Errorf("By = %q, want %q", defaults.By, "type")
	}

	if defaults.Property != "" {
		test.Errorf("Property = %q, want empty", defaults.Property)
	}
}

// TestGraphCluster_AbsentBlockUsesDefaults confirms that a manifest with no
// [graph.cluster] block resolves to DefaultGraphCluster.
func TestGraphCluster_AbsentBlockUsesDefaults(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"
`)

	got := loaded.GraphCluster

	defaults := manifest.DefaultGraphCluster()

	if got.By != defaults.By {
		test.Errorf("By = %q, want default %q", got.By, defaults.By)
	}

	if got.Property != defaults.Property {
		test.Errorf("Property = %q, want default %q", got.Property, defaults.Property)
	}
}

// TestGraphCluster_ByTypeRoundTrip confirms the explicit by = "type" block
// round-trips through Load without error.
func TestGraphCluster_ByTypeRoundTrip(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"

[graph.cluster]
by = "type"
`)

	if loaded.GraphCluster.By != "type" {
		test.Errorf("By = %q, want %q", loaded.GraphCluster.By, "type")
	}
}

// TestGraphCluster_ByPropertyRoundTrip confirms by = "property" with a
// non-empty property field round-trips through Load without error.
func TestGraphCluster_ByPropertyRoundTrip(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"

[graph.cluster]
by       = "property"
property = "team"
`)

	if loaded.GraphCluster.By != "property" {
		test.Errorf("By = %q, want %q", loaded.GraphCluster.By, "property")
	}

	if loaded.GraphCluster.Property != "team" {
		test.Errorf("Property = %q, want %q", loaded.GraphCluster.Property, "team")
	}
}

// TestGraphCluster_ByPropertyEmptyPropertyRejected confirms that
// by = "property" with an empty (absent) property field is a hard error.
func TestGraphCluster_ByPropertyEmptyPropertyRejected(test *testing.T) {
	_, loadErr := loadInlineManifestAllowError(test, `
[workspace]
name = "x"

[graph.cluster]
by = "property"
`)

	if loadErr == nil {
		test.Fatalf("expected error for by=property with empty property, got nil")
	}

	if !strings.Contains(loadErr.Error(), "property") {
		test.Errorf("error %q should mention property", loadErr.Error())
	}
}

// TestGraphCluster_UnknownByRejected confirms that unknown by values produce a
// clear load error. Phase 3 accepts "type", "property", and "ancestor";
// "community" and arbitrary values are still rejected.
func TestGraphCluster_UnknownByRejected(test *testing.T) {
	for _, bad := range []string{"community", "unknown", ""} {
		body := `
[workspace]
name = "x"

[graph.cluster]
by = "` + bad + `"
`

		_, loadErr := loadInlineManifestAllowError(test, body)

		if loadErr == nil {
			test.Errorf("by = %q: expected error, got nil", bad)

			continue
		}

		if !strings.Contains(loadErr.Error(), "by") {
			test.Errorf("by = %q: error %q does not mention by", bad, loadErr.Error())
		}
	}
}

// TestGraphCluster_ValidateAcceptsDefaults confirms the defaults pass Validate
// so production code that constructs DefaultGraphCluster never trips a
// self-check.
func TestGraphCluster_ValidateAcceptsDefaults(test *testing.T) {
	defaults := manifest.DefaultGraphCluster()

	if errs := defaults.Validate(); len(errs) > 0 {
		test.Errorf("DefaultGraphCluster().Validate() returned %d errors: %v", len(errs), errs)
	}
}

// TestGraphCluster_ValidateAcceptsPropertyWithField confirms that
// by = "property" plus a non-empty property passes Validate.
func TestGraphCluster_ValidateAcceptsPropertyWithField(test *testing.T) {
	cluster := manifest.GraphCluster{By: "property", Property: "team"}

	if errs := cluster.Validate(); len(errs) > 0 {
		test.Errorf("Validate() returned errors: %v", errs)
	}
}

// TestGraphCluster_ByAncestorRoundTrip confirms by = "ancestor" with a
// non-empty edge round-trips through Load without error, and that edge,
// depth, and parent-is-source are decoded correctly.
func TestGraphCluster_ByAncestorRoundTrip(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"

[graph.cluster]
by              = "ancestor"
edge            = "parent"
depth           = 2
parent-is-source = true
`)

	got := loaded.GraphCluster

	if got.By != "ancestor" {
		test.Errorf("By = %q, want %q", got.By, "ancestor")
	}

	if got.Edge != "parent" {
		test.Errorf("Edge = %q, want %q", got.Edge, "parent")
	}

	if got.Depth != 2 {
		test.Errorf("Depth = %d, want 2", got.Depth)
	}

	if !got.ParentIsSource {
		test.Errorf("ParentIsSource = false, want true")
	}
}

// TestGraphCluster_ByAncestorDefaultDepthAndDirection confirms that absent
// depth and parent-is-source fields default to 0 and false respectively.
func TestGraphCluster_ByAncestorDefaultDepthAndDirection(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"

[graph.cluster]
by   = "ancestor"
edge = "parent"
`)

	got := loaded.GraphCluster

	if got.Depth != 0 {
		test.Errorf("Depth = %d, want 0 (default)", got.Depth)
	}

	if got.ParentIsSource {
		test.Errorf("ParentIsSource = true, want false (default)")
	}
}

// TestGraphCluster_ByAncestorMissingEdgeRejected confirms that by = "ancestor"
// with no edge field is a hard error.
func TestGraphCluster_ByAncestorMissingEdgeRejected(test *testing.T) {
	_, loadErr := loadInlineManifestAllowError(test, `
[workspace]
name = "x"

[graph.cluster]
by = "ancestor"
`)

	if loadErr == nil {
		test.Fatal("expected error for by=ancestor with no edge, got nil")
	}

	if !strings.Contains(loadErr.Error(), "edge") {
		test.Errorf("error %q should mention edge", loadErr.Error())
	}
}

// TestGraphCluster_CommunityStillRejected confirms that by = "community" is
// still rejected in Phase 3.
func TestGraphCluster_CommunityStillRejected(test *testing.T) {
	_, loadErr := loadInlineManifestAllowError(test, `
[workspace]
name = "x"

[graph.cluster]
by = "community"
`)

	if loadErr == nil {
		test.Fatal("expected error for by=community, got nil")
	}

	if !strings.Contains(loadErr.Error(), "by") {
		test.Errorf("error %q should mention by", loadErr.Error())
	}
}
