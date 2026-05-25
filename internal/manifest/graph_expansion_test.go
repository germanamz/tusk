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
