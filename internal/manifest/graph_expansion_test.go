package manifest_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
)

// TestDefaultGraphExpansion confirms the default values match the spec:
// Enabled=false, Hops=1, EdgeTypes=["references","parent","tagged","contains"],
// Weight=0.2, CandidateMultiplier=5.
func TestDefaultGraphExpansion(test *testing.T) {
	defaults := manifest.DefaultGraphExpansion()

	if defaults.Enabled {
		test.Errorf("Enabled = true, want false")
	}

	if defaults.Hops != 1 {
		test.Errorf("Hops = %d, want 1", defaults.Hops)
	}

	wantEdges := []string{"references", "parent", "tagged", "contains"}

	if !reflect.DeepEqual(defaults.EdgeTypes, wantEdges) {
		test.Errorf("EdgeTypes = %v, want %v", defaults.EdgeTypes, wantEdges)
	}

	if defaults.Weight != 0.2 {
		test.Errorf("Weight = %v, want 0.2", defaults.Weight)
	}

	if defaults.CandidateMultiplier != 5 {
		test.Errorf("CandidateMultiplier = %d, want 5", defaults.CandidateMultiplier)
	}
}

// TestGraphExpansion_AbsentBlockUsesDefaults confirms that a manifest with
// no [query.graph-expansion] block resolves to defaults.
func TestGraphExpansion_AbsentBlockUsesDefaults(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"
`)

	got := loaded.GraphExpansion

	defaults := manifest.DefaultGraphExpansion()

	if !reflect.DeepEqual(got, defaults) {
		test.Errorf("GraphExpansion = %#v, want defaults %#v", got, defaults)
	}
}

// TestGraphExpansion_FullExplicitBlockRoundTrip confirms every field is
// decoded when the user sets the full block.
func TestGraphExpansion_FullExplicitBlockRoundTrip(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"

[query.graph-expansion]
enabled              = true
hops                 = 2
edge-types           = ["references", "parent"]
weight               = 0.5
candidate-multiplier = 10
`)

	got := loaded.GraphExpansion

	if !got.Enabled {
		test.Errorf("Enabled = false, want true")
	}

	if got.Hops != 2 {
		test.Errorf("Hops = %d, want 2", got.Hops)
	}

	wantEdges := []string{"references", "parent"}

	if !reflect.DeepEqual(got.EdgeTypes, wantEdges) {
		test.Errorf("EdgeTypes = %v, want %v", got.EdgeTypes, wantEdges)
	}

	if got.Weight != 0.5 {
		test.Errorf("Weight = %v, want 0.5", got.Weight)
	}

	if got.CandidateMultiplier != 10 {
		test.Errorf("CandidateMultiplier = %d, want 10", got.CandidateMultiplier)
	}
}

// TestGraphExpansion_PartialBlockFillsDefaults confirms that a block missing
// some keys keeps defaults for the missing keys and uses the explicit
// values for the present keys.
func TestGraphExpansion_PartialBlockFillsDefaults(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"

[query.graph-expansion]
enabled = true
hops    = 2
`)

	got := loaded.GraphExpansion

	if !got.Enabled {
		test.Errorf("Enabled = false, want true (explicit)")
	}

	if got.Hops != 2 {
		test.Errorf("Hops = %d, want 2 (explicit)", got.Hops)
	}

	defaults := manifest.DefaultGraphExpansion()

	if !reflect.DeepEqual(got.EdgeTypes, defaults.EdgeTypes) {
		test.Errorf("EdgeTypes = %v, want defaults %v", got.EdgeTypes, defaults.EdgeTypes)
	}

	if got.Weight != defaults.Weight {
		test.Errorf("Weight = %v, want default %v", got.Weight, defaults.Weight)
	}

	if got.CandidateMultiplier != defaults.CandidateMultiplier {
		test.Errorf("CandidateMultiplier = %d, want default %d", got.CandidateMultiplier, defaults.CandidateMultiplier)
	}
}

// TestGraphExpansion_RejectsInvalidHops confirms Load returns a hard error
// when hops is outside {1, 2}.
func TestGraphExpansion_RejectsInvalidHops(test *testing.T) {
	for _, badValue := range []int{0, 3, -1, 10} {
		body := `
[workspace]
name = "x"

[query.graph-expansion]
enabled = true
hops    = ` + intToString(badValue) + `
`

		_, loadErr := loadInlineManifestAllowError(test, body)

		if loadErr == nil {
			test.Errorf("hops = %d: expected error, got nil", badValue)

			continue
		}

		if !strings.Contains(loadErr.Error(), "hops") {
			test.Errorf("hops = %d: error %q does not mention hops", badValue, loadErr.Error())
		}
	}
}

// TestGraphExpansion_RejectsInvalidWeight confirms Load rejects weight
// outside [0.0, 1.0].
func TestGraphExpansion_RejectsInvalidWeight(test *testing.T) {
	for _, badValue := range []string{"-0.1", "1.1", "2.0"} {
		body := `
[workspace]
name = "x"

[query.graph-expansion]
enabled = true
weight  = ` + badValue + `
`

		_, loadErr := loadInlineManifestAllowError(test, body)

		if loadErr == nil {
			test.Errorf("weight = %s: expected error, got nil", badValue)

			continue
		}

		if !strings.Contains(loadErr.Error(), "weight") {
			test.Errorf("weight = %s: error %q does not mention weight", badValue, loadErr.Error())
		}
	}
}

// TestGraphExpansion_RejectsInvalidCandidateMultiplier confirms that
// candidate-multiplier < 1 is a hard error.
func TestGraphExpansion_RejectsInvalidCandidateMultiplier(test *testing.T) {
	for _, badValue := range []int{0, -1, -5} {
		body := `
[workspace]
name = "x"

[query.graph-expansion]
enabled              = true
candidate-multiplier = ` + intToString(badValue) + `
`

		_, loadErr := loadInlineManifestAllowError(test, body)

		if loadErr == nil {
			test.Errorf("candidate-multiplier = %d: expected error, got nil", badValue)

			continue
		}

		if !strings.Contains(loadErr.Error(), "candidate-multiplier") {
			test.Errorf("candidate-multiplier = %d: error %q does not mention candidate-multiplier", badValue, loadErr.Error())
		}
	}
}

// TestGraphExpansion_UnknownEdgeTypeAllowed confirms unknown edge-type
// names do NOT cause a load error. Doctor warns about them in Task 4.
func TestGraphExpansion_UnknownEdgeTypeAllowed(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"

[query.graph-expansion]
enabled    = true
edge-types = ["references", "no-such-edge"]
`)

	wantEdges := []string{"references", "no-such-edge"}

	if !reflect.DeepEqual(loaded.GraphExpansion.EdgeTypes, wantEdges) {
		test.Errorf("EdgeTypes = %v, want %v (unknown names preserved verbatim)", loaded.GraphExpansion.EdgeTypes, wantEdges)
	}
}

// TestGraphExpansion_ValidateAcceptsDefaults confirms the defaults pass the
// Validate hard rules so production code that constructs DefaultGraphExpansion
// never trips a self-check.
func TestGraphExpansion_ValidateAcceptsDefaults(test *testing.T) {
	defaults := manifest.DefaultGraphExpansion()

	if errs := defaults.Validate(); len(errs) > 0 {
		test.Errorf("DefaultGraphExpansion().Validate() returned %d errors: %v", len(errs), errs)
	}
}

// TestGraphExpansion_ValidateReportsAllErrors confirms Validate accumulates
// every rule violation instead of stopping at the first.
func TestGraphExpansion_ValidateReportsAllErrors(test *testing.T) {
	bad := manifest.GraphExpansion{
		Enabled:             true,
		Hops:                7,
		EdgeTypes:           []string{"references"},
		Weight:              -1,
		CandidateMultiplier: 0,
	}

	errs := bad.Validate()

	if len(errs) < 3 {
		test.Errorf("expected >= 3 errors, got %d: %v", len(errs), errs)
	}
}

// TestGraphExpansion_LoadReportsAllErrors confirms Load aggregates every
// validation failure into a single error so users see hops AND weight problems
// at once instead of fixing them one round-trip at a time.
func TestGraphExpansion_LoadReportsAllErrors(test *testing.T) {
	body := `
[workspace]
name = "x"

[query.graph-expansion]
enabled = true
hops    = 7
weight  = 1.5
`

	_, loadErr := loadInlineManifestAllowError(test, body)

	if loadErr == nil {
		test.Fatalf("expected error for bad hops AND bad weight, got nil")
	}

	if !strings.Contains(loadErr.Error(), "hops") {
		test.Errorf("error %q should mention hops", loadErr.Error())
	}

	if !strings.Contains(loadErr.Error(), "weight") {
		test.Errorf("error %q should mention weight", loadErr.Error())
	}
}

// TestDefaultGraphExpansion_EdgeTypesIsolated confirms that two separate calls
// to DefaultGraphExpansion return EdgeTypes slices that do not share backing
// storage. A shared backing array would let one caller's mutation leak into
// every subsequent default, which the resolver and tests rely on staying
// pristine.
func TestDefaultGraphExpansion_EdgeTypesIsolated(test *testing.T) {
	first := manifest.DefaultGraphExpansion()
	second := manifest.DefaultGraphExpansion()

	if len(first.EdgeTypes) == 0 || len(second.EdgeTypes) == 0 {
		test.Fatalf("default EdgeTypes unexpectedly empty: first=%v second=%v",
			first.EdgeTypes, second.EdgeTypes)
	}

	if &first.EdgeTypes[0] == &second.EdgeTypes[0] {
		test.Errorf("DefaultGraphExpansion EdgeTypes share backing array; want fresh slice per call")
	}
}

// intToString is a tiny stand-in for strconv.Itoa to keep the test file's
// import list small.
func intToString(value int) string {
	if value == 0 {
		return "0"
	}

	negative := value < 0

	if negative {
		value = -value
	}

	var digits []byte

	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}

	if negative {
		digits = append([]byte{'-'}, digits...)
	}

	return string(digits)
}

// ptrTo returns a pointer to value. MergeGraphExpansion's overrides are
// optionals, so the tests need addressable literals.
func ptrTo[T any](value T) *T {
	return &value
}

// TestMergeGraphExpansion_NoOverridesPreservesBase confirms the merger returns
// the manifest defaults verbatim when no per-call override is set.
func TestMergeGraphExpansion_NoOverridesPreservesBase(test *testing.T) {
	base := manifest.DefaultGraphExpansion()

	got, mergeErr := manifest.MergeGraphExpansion(base, manifest.GraphExpansionOverrides{})

	if mergeErr != nil {
		test.Fatalf("MergeGraphExpansion: %v", mergeErr)
	}

	if got == nil {
		test.Fatalf("MergeGraphExpansion: nil result")
	}

	if got.Enabled != base.Enabled {
		test.Errorf("Enabled = %v, want %v", got.Enabled, base.Enabled)
	}

	if got.Hops != base.Hops {
		test.Errorf("Hops = %d, want %d", got.Hops, base.Hops)
	}

	if got.Weight != base.Weight {
		test.Errorf("Weight = %v, want %v", got.Weight, base.Weight)
	}
}

// TestMergeGraphExpansion_EnabledTrueBeatsWorkspaceDisabled confirms an
// explicit Enabled=true turns the feature on even when the workspace ships with
// enabled=false. Drives what the CLI's --graph-expand resolves to.
func TestMergeGraphExpansion_EnabledTrueBeatsWorkspaceDisabled(test *testing.T) {
	base := manifest.DefaultGraphExpansion() // Enabled = false.

	got, mergeErr := manifest.MergeGraphExpansion(base, manifest.GraphExpansionOverrides{
		Enabled: ptrTo(true),
	})

	if mergeErr != nil {
		test.Fatalf("MergeGraphExpansion: %v", mergeErr)
	}

	if !got.Enabled {
		test.Errorf("Enabled = false, want true (an explicit true should enable)")
	}
}

// TestMergeGraphExpansion_EnabledFalseBeatsWorkspaceEnabled confirms an
// explicit Enabled=false disables the feature even when the workspace manifest
// enables it. An earlier CLI switch arm only ran when the value was true,
// silently dropping the user's explicit false.
func TestMergeGraphExpansion_EnabledFalseBeatsWorkspaceEnabled(test *testing.T) {
	base := manifest.DefaultGraphExpansion()
	base.Enabled = true // Workspace ships with enabled = true.

	got, mergeErr := manifest.MergeGraphExpansion(base, manifest.GraphExpansionOverrides{
		Enabled: ptrTo(false),
	})

	if mergeErr != nil {
		test.Fatalf("MergeGraphExpansion: %v", mergeErr)
	}

	if got.Enabled {
		test.Errorf("Enabled = true, want false (an explicit false must beat workspace enabled=true)")
	}
}

// TestMergeGraphExpansion_AbsentEnabledInheritsWorkspace pins the erratum that
// prompted this extraction: a nil Enabled override must inherit the workspace
// value, never force expansion on.
func TestMergeGraphExpansion_AbsentEnabledInheritsWorkspace(test *testing.T) {
	for _, workspaceEnabled := range []bool{true, false} {
		base := manifest.DefaultGraphExpansion()
		base.Enabled = workspaceEnabled

		got, mergeErr := manifest.MergeGraphExpansion(base, manifest.GraphExpansionOverrides{
			Hops: ptrTo(2),
		})

		if mergeErr != nil {
			test.Fatalf("MergeGraphExpansion: %v", mergeErr)
		}

		if got.Enabled != workspaceEnabled {
			test.Errorf("Enabled = %v, want %v (absent override must inherit)", got.Enabled, workspaceEnabled)
		}
	}
}

// TestMergeGraphExpansion_EdgeTypesNotAliased confirms the resolved
// GraphExpansion does not share the backing array of its base.EdgeTypes slice.
// The MCP server fans requests out across goroutines, so an aliased slice would
// race once a future caller mutates it.
func TestMergeGraphExpansion_EdgeTypesNotAliased(test *testing.T) {
	base := manifest.DefaultGraphExpansion()

	if len(base.EdgeTypes) == 0 {
		test.Fatalf("default EdgeTypes unexpectedly empty")
	}

	got, mergeErr := manifest.MergeGraphExpansion(base, manifest.GraphExpansionOverrides{})

	if mergeErr != nil {
		test.Fatalf("MergeGraphExpansion: %v", mergeErr)
	}

	if &got.EdgeTypes[0] == &base.EdgeTypes[0] {
		test.Errorf("resolved EdgeTypes shares backing array with base; want clone")
	}
}

// TestMergeGraphExpansion_EdgeTypesOverrideNotAliased confirms an overriding
// slice is cloned too, so the caller's slice (cobra-owned, or JSON-decoder
// owned for MCP) is not shared with the resolved configuration.
func TestMergeGraphExpansion_EdgeTypesOverrideNotAliased(test *testing.T) {
	base := manifest.DefaultGraphExpansion()
	supplied := []string{"references"}

	got, mergeErr := manifest.MergeGraphExpansion(base, manifest.GraphExpansionOverrides{
		EdgeTypes: &supplied,
	})

	if mergeErr != nil {
		test.Fatalf("MergeGraphExpansion: %v", mergeErr)
	}

	if !reflect.DeepEqual(got.EdgeTypes, []string{"references"}) {
		test.Fatalf("EdgeTypes = %v, want [references]", got.EdgeTypes)
	}

	if &got.EdgeTypes[0] == &supplied[0] {
		test.Errorf("resolved EdgeTypes shares backing array with the override; want clone")
	}
}

// TestMergeGraphExpansion_EmptyEdgeTypesOverrideIsHonored confirms a non-nil,
// len-0 override is a legitimate "follow no edge types" instruction rather than
// an absent one, and that it does not fall back to the base.
func TestMergeGraphExpansion_EmptyEdgeTypesOverrideIsHonored(test *testing.T) {
	base := manifest.DefaultGraphExpansion()
	empty := []string{}

	got, mergeErr := manifest.MergeGraphExpansion(base, manifest.GraphExpansionOverrides{
		EdgeTypes: &empty,
	})

	if mergeErr != nil {
		test.Fatalf("MergeGraphExpansion: %v", mergeErr)
	}

	if len(got.EdgeTypes) != 0 {
		test.Errorf("EdgeTypes = %v, want empty (an explicit empty override must be honored)", got.EdgeTypes)
	}

	if got.EdgeTypes == nil {
		test.Errorf("EdgeTypes = nil, want a non-nil empty slice")
	}
}

// TestMergeGraphExpansion_ExplicitZeroHopsRejected pins a case the old
// out-of-band presence flags could not express: hops=0 is a real, invalid
// override and must error rather than be silently read as "absent".
func TestMergeGraphExpansion_ExplicitZeroHopsRejected(test *testing.T) {
	base := manifest.DefaultGraphExpansion()

	_, mergeErr := manifest.MergeGraphExpansion(base, manifest.GraphExpansionOverrides{
		Hops: ptrTo(0),
	})

	if mergeErr == nil {
		test.Fatalf("expected an error for an explicit hops=0")
	}

	if !strings.Contains(mergeErr.Error(), "must be 1 or 2") {
		test.Errorf("error %q should explain the hops range", mergeErr.Error())
	}
}

// TestMergeGraphExpansion_ExplicitZeroWeightHonored pins the counterpart: an
// explicit weight=0 is in range, so it must land as 0 rather than inherit the
// base's 0.2. This is the whole reason the overrides are optionals.
func TestMergeGraphExpansion_ExplicitZeroWeightHonored(test *testing.T) {
	base := manifest.DefaultGraphExpansion()

	if base.Weight == 0 {
		test.Fatalf("default Weight unexpectedly 0; the test cannot distinguish")
	}

	got, mergeErr := manifest.MergeGraphExpansion(base, manifest.GraphExpansionOverrides{
		Weight: ptrTo(0.0),
	})

	if mergeErr != nil {
		test.Fatalf("MergeGraphExpansion: %v", mergeErr)
	}

	if got.Weight != 0 {
		test.Errorf("Weight = %v, want 0 (an explicit zero must be honored)", got.Weight)
	}
}

// TestMergeGraphExpansion_HopsValidatedBeforeWeight pins the order both callers
// shipped: a call passing two out-of-range values surfaces the hops error.
func TestMergeGraphExpansion_HopsValidatedBeforeWeight(test *testing.T) {
	base := manifest.DefaultGraphExpansion()

	_, mergeErr := manifest.MergeGraphExpansion(base, manifest.GraphExpansionOverrides{
		Hops:   ptrTo(9),
		Weight: ptrTo(5.0),
	})

	if mergeErr == nil {
		test.Fatalf("expected an error")
	}

	if !strings.Contains(mergeErr.Error(), "1 or 2") {
		test.Errorf("error %q should be the hops error; hops is validated first", mergeErr.Error())
	}
}

// TestMergeGraphExpansion_LabelsNameTheCallersKnob pins the per-caller wording.
// The CLI's message is pinned byte-for-byte by a golden test, and MCP's labels
// carry a trailing colon deliberately, so both callers keep their exact strings.
func TestMergeGraphExpansion_LabelsNameTheCallersKnob(test *testing.T) {
	cases := []struct {
		name     string
		labels   manifest.GraphExpansionLabels
		over     manifest.GraphExpansionOverrides
		wantText string
	}{
		{
			name:     "cli hops",
			labels:   manifest.GraphExpansionLabels{Hops: "--hops", Weight: "--graph-weight"},
			over:     manifest.GraphExpansionOverrides{Hops: ptrTo(3)},
			wantText: "--hops must be 1 or 2 (got 3)",
		},
		{
			name:     "cli weight",
			labels:   manifest.GraphExpansionLabels{Hops: "--hops", Weight: "--graph-weight"},
			over:     manifest.GraphExpansionOverrides{Weight: ptrTo(1.5)},
			wantText: "--graph-weight must be in [0.0, 1.0] (got 1.5)",
		},
		{
			name:     "mcp hops keeps its colon",
			labels:   manifest.GraphExpansionLabels{Hops: "hops:", Weight: "graph_weight:"},
			over:     manifest.GraphExpansionOverrides{Hops: ptrTo(5)},
			wantText: "hops: must be 1 or 2 (got 5)",
		},
		{
			name:     "mcp weight keeps its colon",
			labels:   manifest.GraphExpansionLabels{Hops: "hops:", Weight: "graph_weight:"},
			over:     manifest.GraphExpansionOverrides{Weight: ptrTo(2.0)},
			wantText: "graph_weight: must be in [0.0, 1.0] (got 2)",
		},
		{
			name:     "unset labels fall back to the bare key",
			over:     manifest.GraphExpansionOverrides{Hops: ptrTo(7)},
			wantText: "hops must be 1 or 2 (got 7)",
		},
		{
			name:     "unset weight label falls back to the bare key",
			over:     manifest.GraphExpansionOverrides{Weight: ptrTo(-1.0)},
			wantText: "weight must be in [0.0, 1.0] (got -1)",
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			over := testCase.over
			over.Labels = testCase.labels

			_, mergeErr := manifest.MergeGraphExpansion(manifest.DefaultGraphExpansion(), over)

			if mergeErr == nil {
				test.Fatalf("expected an error")
			}

			if mergeErr.Error() != testCase.wantText {
				test.Errorf("error = %q, want %q", mergeErr.Error(), testCase.wantText)
			}
		})
	}
}
