package manifest_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
)

// allSubUnitKinds is every kind `[workspace] sub-units` can list, in the
// order SubUnitKinds reports them.
var allSubUnitKinds = []string{"section", "paragraph", "list-item", "code-block", "blockquote", "table-cell"}

// TestSubUnitKinds_Values pins which kinds each accepted sub-units value
// indexes (#853).
func TestSubUnitKinds_Values(test *testing.T) {
	testCases := []struct {
		name  string
		value string // the sub-units line, or "" for none
		want  []string
	}{
		{name: "absent", want: allSubUnitKinds},
		{name: "true", value: `sub-units = true`, want: allSubUnitKinds},
		{name: "false", value: `sub-units = false`, want: []string{}},
		{name: "empty list", value: `sub-units = []`, want: []string{}},
		{name: "subset in written order", value: `sub-units = ["paragraph", "section"]`, want: []string{"section", "paragraph"}},
		{name: "duplicates", value: `sub-units = ["table-cell", "table-cell"]`, want: []string{"table-cell"}},
		{name: "every kind", value: `sub-units = ["table-cell", "blockquote", "code-block", "list-item", "paragraph", "section"]`, want: allSubUnitKinds},
	}

	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			loaded := loadInlineManifest(test, "[workspace]\nname = \"x\"\n"+testCase.value+"\n")
			got := loaded.SubUnitKinds()

			if !slices.Equal(got, testCase.want) {
				test.Fatalf("SubUnitKinds() = %v, want %v", got, testCase.want)
			}

			if enabled := loaded.SubUnitsEnabled(); enabled != (len(testCase.want) > 0) {
				test.Errorf("SubUnitsEnabled() = %v, want %v", enabled, len(testCase.want) > 0)
			}

			for _, kind := range allSubUnitKinds {
				want := slices.Contains(testCase.want, kind)

				if got := loaded.IndexesSubUnitKind(kind); got != want {
					test.Errorf("IndexesSubUnitKind(%q) = %v, want %v", kind, got, want)
				}
			}
		})
	}
}

// TestSubUnitKinds_UnloadedManifestIndexesEveryKind keeps a nil or hand-built
// manifest on the default, every kind.
func TestSubUnitKinds_UnloadedManifestIndexesEveryKind(test *testing.T) {
	for _, loaded := range []*manifest.Manifest{nil, {}} {
		if got := loaded.SubUnitKinds(); !slices.Equal(got, allSubUnitKinds) {
			test.Errorf("SubUnitKinds() = %v, want %v", got, allSubUnitKinds)
		}

		if !loaded.IndexesSubUnitKind("table-cell") {
			test.Errorf("IndexesSubUnitKind(table-cell) = false, want true")
		}
	}

	if (*manifest.Manifest)(nil).IndexesSubUnitKind("note") {
		test.Errorf("IndexesSubUnitKind(note) = true, want false: not a sub-unit kind")
	}
}

// TestSubUnitKinds_RejectsOtherValues fails Load on a sub-units value that is
// neither a bool nor a list of kind names, which loaded silently and changed
// nothing before #853.
func TestSubUnitKinds_RejectsOtherValues(test *testing.T) {
	testCases := []struct {
		name  string
		value string
		want  string // a substring the error must carry
	}{
		{name: "string", value: `sub-units = "bogus"`, want: `workspace.sub-units = "bogus" is not supported`},
		{name: "integer", value: `sub-units = 1`, want: `workspace.sub-units = 1 is not supported`},
		{name: "table", value: "sub-units = { section = true }", want: "workspace.sub-units"},
		{name: "unknown kind", value: `sub-units = ["section", "tables"]`, want: `workspace.sub-units: "tables" is not a sub-unit kind`},
		{name: "non-string item", value: `sub-units = ["section", 1]`, want: "workspace.sub-units: 1 is not a sub-unit kind"},
	}

	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			_, loadErr := loadInlineManifestAllowError(test, "[workspace]\nname = \"x\"\n"+testCase.value+"\n")

			if loadErr == nil {
				test.Fatalf("Load accepted %s, want an error", testCase.value)
			}

			if !strings.Contains(loadErr.Error(), testCase.want) {
				test.Errorf("error = %q, want it to contain %q", loadErr, testCase.want)
			}

			// The error names the accepted values.
			if !strings.Contains(loadErr.Error(), "section | paragraph | list-item | code-block | blockquote | table-cell") {
				test.Errorf("error = %q, want it to list the kinds", loadErr)
			}
		})
	}
}

// TestMergeBuiltinPacks_SubsetInstallsWholePack keeps every built-in sub-unit
// type declared when the list picks only some kinds, so a filter or rule that
// names an unindexed kind still validates and matches nothing.
func TestMergeBuiltinPacks_SubsetInstallsWholePack(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"
sub-units = ["paragraph"]
`)

	manifest.MergeBuiltinPacks(loaded)

	for _, kind := range allSubUnitKinds {
		if _, has := loaded.NodeTypes[kind]; !has {
			test.Errorf("NodeTypes missing %q after merge", kind)
		}
	}

	if _, has := loaded.EdgeTypes["contains"]; !has {
		test.Errorf("EdgeTypes missing contains after merge")
	}
}
