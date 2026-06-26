package manifest_test

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
)

// TestDefaultGraphCluster confirms the default is by = "type", Resolution = 1.0,
// and all other fields are zero, so an absent [graph.cluster] block reproduces
// today's type-based coloring exactly.
func TestDefaultGraphCluster(test *testing.T) {
	defaults := manifest.DefaultGraphCluster()

	if defaults.By != "type" {
		test.Errorf("By = %q, want %q", defaults.By, "type")
	}

	if defaults.Property != "" {
		test.Errorf("Property = %q, want empty", defaults.Property)
	}

	if defaults.Resolution != 1.0 {
		test.Errorf("Resolution = %v, want 1.0", defaults.Resolution)
	}

	if len(defaults.CommunityEdges) != 0 {
		test.Errorf("CommunityEdges = %v, want nil/empty", defaults.CommunityEdges)
	}
}

// TestGraphCluster_AbsentBlockUsesDefaults confirms that a manifest with no
// [graph.cluster] block resolves to DefaultGraphCluster, including the
// Resolution default of 1.0 (so community detection is usable without an
// explicit resolution key).
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

	if got.Resolution != 1.0 {
		test.Errorf("Resolution = %v, want 1.0 (default)", got.Resolution)
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
// clear load error. All four accepted producers are: "type", "property",
// "ancestor", "community". Arbitrary values are rejected.
func TestGraphCluster_UnknownByRejected(test *testing.T) {
	for _, bad := range []string{"unknown", "", "colour", "group"} {
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

// TestGraphCluster_ByCommunityRoundTrip confirms by = "community" with the
// default resolution (absent key → 1.0) resolves and validates correctly.
func TestGraphCluster_ByCommunityRoundTrip(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"

[graph.cluster]
by = "community"
`)

	got := loaded.GraphCluster

	if got.By != "community" {
		test.Errorf("By = %q, want %q", got.By, "community")
	}

	if got.Resolution != 1.0 {
		test.Errorf("Resolution = %v, want 1.0 (default)", got.Resolution)
	}

	if len(got.CommunityEdges) != 0 {
		test.Errorf("CommunityEdges = %v, want nil/empty (absent key)", got.CommunityEdges)
	}
}

// TestGraphCluster_ByCommunityResolutionZeroRejected confirms that an explicit
// resolution = 0 (or negative) is rejected when by = "community".
func TestGraphCluster_ByCommunityResolutionZeroRejected(test *testing.T) {
	for _, resStr := range []string{"0", "0.0", "-1.0"} {
		body := `
[workspace]
name = "x"

[graph.cluster]
by         = "community"
resolution = ` + resStr + `
`

		_, loadErr := loadInlineManifestAllowError(test, body)

		if loadErr == nil {
			test.Errorf("resolution = %s: expected error, got nil", resStr)

			continue
		}

		if !strings.Contains(loadErr.Error(), "resolution") {
			test.Errorf("resolution = %s: error %q should mention resolution", resStr, loadErr.Error())
		}
	}
}

// TestGraphCluster_ByCommunityEdgesRoundTrip confirms that community-edges
// round-trips into GraphCluster.CommunityEdges as a []string.
func TestGraphCluster_ByCommunityEdgesRoundTrip(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"

[graph.cluster]
by               = "community"
community-edges  = ["depends-on", "direct"]
`)

	got := loaded.GraphCluster.CommunityEdges

	if len(got) != 2 {
		test.Fatalf("CommunityEdges length = %d, want 2", len(got))
	}

	if got[0] != "depends-on" {
		test.Errorf("CommunityEdges[0] = %q, want %q", got[0], "depends-on")
	}

	if got[1] != "direct" {
		test.Errorf("CommunityEdges[1] = %q, want %q", got[1], "direct")
	}
}

// TestGraphCluster_ByCommunityExplicitResolutionRoundTrip confirms that an
// explicit resolution value round-trips through Load.
func TestGraphCluster_ByCommunityExplicitResolutionRoundTrip(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"

[graph.cluster]
by         = "community"
resolution = 0.5
`)

	if loaded.GraphCluster.Resolution != 0.5 {
		test.Errorf("Resolution = %v, want 0.5", loaded.GraphCluster.Resolution)
	}
}

// TestGraphCluster_HuddleDefaultsFalse confirms that an absent huddle field
// resolves to false so existing configs are not affected.
func TestGraphCluster_HuddleDefaultsFalse(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"

[graph.cluster]
by = "type"
`)

	if loaded.GraphCluster.Huddle {
		test.Error("Huddle = true, want false (default)")
	}
}

// TestGraphCluster_HuddleTrueRoundTrip confirms that huddle = true round-trips
// through Load with any accepted by value.
func TestGraphCluster_HuddleTrueRoundTrip(test *testing.T) {
	for _, byVal := range []string{"type", "property", "ancestor", "community"} {
		extra := ""

		switch byVal {
		case "property":
			extra = "\nproperty = \"team\""
		case "ancestor":
			extra = "\nedge = \"parent\""
		}

		body := `
[workspace]
name = "x"

[graph.cluster]
by     = "` + byVal + `"` + extra + `
huddle = true
`

		loaded := loadInlineManifest(test, body)

		if !loaded.GraphCluster.Huddle {
			test.Errorf("by = %q: Huddle = false, want true", byVal)
		}
	}
}

// TestGraphCluster_HuddleAcceptedWithAnyBy confirms that huddle is not gated
// on a specific producer and is accepted alongside each valid by value.
func TestGraphCluster_HuddleAcceptedWithAnyBy(test *testing.T) {
	for _, byVal := range []string{"type", "property", "ancestor", "community"} {
		extra := ""

		switch byVal {
		case "property":
			extra = "\nproperty = \"team\""
		case "ancestor":
			extra = "\nedge = \"parent\""
		}

		body := `
[workspace]
name = "x"

[graph.cluster]
by     = "` + byVal + `"` + extra + `
huddle = true
`

		loaded, loadErr := loadInlineManifestAllowError(test, body)

		if loadErr != nil {
			test.Errorf("by = %q with huddle = true: unexpected error: %v", byVal, loadErr)

			continue
		}

		if !loaded.GraphCluster.Huddle {
			test.Errorf("by = %q: Huddle = false, want true", byVal)
		}
	}
}
