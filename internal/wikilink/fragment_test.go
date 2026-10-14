package wikilink_test

import (
	"reflect"
	"testing"

	"github.com/germanamz/tusk/internal/wikilink"
)

func TestHeadingKey(test *testing.T) {
	for text, want := range map[string]string{
		"Some heading":         "some heading",
		"  Some   heading  ":   "some heading",
		"Foo: Bar":             "foo bar",
		"foo-bar":              "foo bar",
		"Foo/Bar?":             "foo bar",
		"`code` and **bold**":  "code and bold",
		"don't panic":          "don t panic",
		"Ünïcode Straße":       "ünïcode straße",
		"v1.2 release":         "v1 2 release",
		"🚀 Launch":             "launch",
		"!!!":                  "",
		"Q1":                   "q1",
		"snake_case_heading":   "snake case heading",
		"Line\nbreak\theading": "line break heading",
	} {
		if got := wikilink.HeadingKey(text); got != want {
			test.Errorf("HeadingKey(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestNeedsLookup(test *testing.T) {
	for target, want := range map[string]bool{
		"auth-rfc":             true,
		"auth-rfc#S1":          true,
		"notes/auth-rfc":       false,
		"notes/auth-rfc#S1":    true,
		"notes/auth-rfc#Setup": true,
		"#Setup":               true,
		"../notes/x#Setup":     true,
		"../notes/x":           false,
	} {
		if got := wikilink.NeedsLookup(target); got != want {
			test.Errorf("NeedsLookup(%q) = %v, want %v", target, got, want)
		}
	}
}

// sampleSections is a file with two top-level sections that each hold an
// "Examples" heading, the second under a nested "Details":
//
//	# Intro           S1
//	## Examples       S1.1
//	# Usage           S2
//	## Details        S2.1
//	### Examples      S2.1.1
func sampleSections() []wikilink.Section {
	return []wikilink.Section{
		{ID: "n#S1", Parent: "n", Key: "intro"},
		{ID: "n#S1.1", Parent: "n#S1", Key: "examples"},
		{ID: "n#S2", Parent: "n", Key: "usage"},
		{ID: "n#S2.1", Parent: "n#S2", Key: "details"},
		{ID: "n#S2.1.1", Parent: "n#S2.1", Key: "examples"},
	}
}

func TestMatchHeading(test *testing.T) {
	for _, tc := range []struct {
		chain  string
		wantID string
	}{
		{"Intro", "n#S1"},
		{"usage", "n#S2"},
		// A heading several sections share resolves to the first.
		{"Examples", "n#S1.1"},
		// A chain looks for each heading below the one before it, at any depth.
		{"Usage#Examples", "n#S2.1.1"},
		{"Usage#Details#Examples", "n#S2.1.1"},
		{"Intro#Examples", "n#S1.1"},
		{"Usage##Examples", "n#S2.1.1"},
		{"Intro#Details", ""},
		{"Details#Usage", ""},
		{"Missing", ""},
		{"!!!", ""},
		{"", ""},
	} {
		gotID, found := wikilink.MatchHeading(tc.chain, sampleSections())

		if gotID != tc.wantID || found != (tc.wantID != "") {
			test.Errorf("MatchHeading(%q) = %q, %v; want %q", tc.chain, gotID, found, tc.wantID)
		}
	}
}

// fakeLookup answers files from names and anchors from fixed tables.
type fakeLookup struct {
	*wikilink.Names

	subUnits map[string]bool
	sections map[string][]wikilink.Section
}

func (lookup fakeLookup) Exists(id string) (bool, error) {
	if _, isFile := lookup.Path(id); isFile {
		return true, nil
	}

	return lookup.subUnits[id], nil
}

func (lookup fakeLookup) Sections(fileID string) ([]wikilink.Section, error) {
	return lookup.sections[fileID], nil
}

func TestResolveTarget(test *testing.T) {
	lookup := fakeLookup{
		Names: newNames(wikilink.CaseInsensitive, "notes/auth-rfc.md", "a/dup.md", "b/dup.md", "inbox/today.md"),
		subUnits: map[string]bool{
			"notes/auth-rfc#S1":    true,
			"notes/auth-rfc#S1.1":  true,
			"notes/auth-rfc#S1P1":  true,
			"inbox/today#S1":       true,
			"notes/auth-rfc#S2":    true,
			"notes/auth-rfc#S2.1":  true,
			"notes/auth-rfc#S2P1":  true,
			"notes/auth-rfc#S1.1B": true,
		},
		sections: map[string][]wikilink.Section{
			"notes/auth-rfc": {
				{ID: "notes/auth-rfc#S1", Parent: "notes/auth-rfc", Key: "overview"},
				{ID: "notes/auth-rfc#S1.1", Parent: "notes/auth-rfc#S1", Key: "s2"},
				{ID: "notes/auth-rfc#S2", Parent: "notes/auth-rfc", Key: "open questions"},
				{ID: "notes/auth-rfc#S2.1", Parent: "notes/auth-rfc#S2", Key: "tokens"},
			},
			"inbox/today": {
				{ID: "inbox/today#S1", Parent: "inbox/today", Key: "agenda"},
			},
		},
	}
	source := wikilink.Source{ID: "inbox/today", Path: "inbox/today.md"}

	for _, tc := range []struct {
		target string
		want   wikilink.Target
	}{
		{"auth-rfc", wikilink.Target{ID: "notes/auth-rfc"}},
		{"notes/auth-rfc", wikilink.Target{ID: "notes/auth-rfc"}},
		{"auth-rfc#S1P1", wikilink.Target{ID: "notes/auth-rfc#S1P1"}},
		{"auth-rfc#Open Questions", wikilink.Target{ID: "notes/auth-rfc#S2"}},
		{"notes/auth-rfc#open-questions", wikilink.Target{ID: "notes/auth-rfc#S2"}},
		{"../notes/auth-rfc#Open questions#Tokens", wikilink.Target{ID: "notes/auth-rfc#S2.1"}},
		// An existing structural address wins over a heading with that text.
		{"auth-rfc#S2", wikilink.Target{ID: "notes/auth-rfc#S2"}},
		// A heading reads as text when no sub-unit has the address.
		{"auth-rfc#s2", wikilink.Target{ID: "notes/auth-rfc#S1.1"}},
		{"auth-rfc#Renamed heading", wikilink.Target{ID: "notes/auth-rfc", MissingFragment: true}},
		{"auth-rfc#S9", wikilink.Target{ID: "notes/auth-rfc", MissingFragment: true}},
		{"auth-rfc#^block-id", wikilink.Target{ID: "notes/auth-rfc", MissingFragment: true}},
		{"auth-rfc#", wikilink.Target{ID: "notes/auth-rfc"}},
		// No path part names the linking file.
		{"#Agenda", wikilink.Target{ID: "inbox/today#S1"}},
		{"#Nope", wikilink.Target{ID: "inbox/today", MissingFragment: true}},
		{"dup#Overview", wikilink.Target{Candidates: []string{"a/dup", "b/dup"}}},
		{"gone#Overview", wikilink.Target{}},
		{"notes/gone#Overview", wikilink.Target{}},
		{"notes/gone", wikilink.Target{}},
	} {
		got, resolveErr := wikilink.ResolveTarget(tc.target, source, lookup)

		if resolveErr != nil {
			test.Fatalf("ResolveTarget(%q): %v", tc.target, resolveErr)
		}

		if !reflect.DeepEqual(got, tc.want) {
			test.Errorf("ResolveTarget(%q) = %+v, want %+v", tc.target, got, tc.want)
		}
	}

	if got, _ := wikilink.ResolveTarget("#Agenda", wikilink.Source{Path: "inbox/today.md"}, lookup); got.ID != "" {
		test.Errorf("a self link with no source id resolved to %q", got.ID)
	}
}
