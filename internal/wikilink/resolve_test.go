package wikilink_test

import (
	"reflect"
	"testing"

	"github.com/germanamz/tusk/internal/wikilink"
)

func TestParseCase(test *testing.T) {
	cases := map[string]wikilink.Case{
		"":            wikilink.CaseInsensitive,
		"insensitive": wikilink.CaseInsensitive,
		"sensitive":   wikilink.CaseSensitive,
	}

	for value, want := range cases {
		got, parseErr := wikilink.ParseCase(value)

		if parseErr != nil || got != want {
			test.Errorf("ParseCase(%q) = %q, %v; want %q", value, got, parseErr, want)
		}
	}

	if _, parseErr := wikilink.ParseCase("Sensitive"); parseErr == nil {
		test.Error("ParseCase accepted a value outside insensitive | sensitive")
	}
}

func TestIsName(test *testing.T) {
	for target, want := range map[string]bool{
		"auth-rfc":          true,
		"Auth RFC":          true,
		"auth-rfc#S1":       true,
		"auth-rfc#a/b":      true, // a slash in the fragment is not a folder
		"page.html":         true,
		"notes/auth-rfc":    false,
		"./auth-rfc":        false,
		"../notes/auth-rfc": false,
		"#S1":               false,
		"":                  false,
		".":                 false,
		"..":                false,
	} {
		if got := wikilink.IsName(target); got != want {
			test.Errorf("IsName(%q) = %v, want %v", target, got, want)
		}
	}
}

func TestNormalize(test *testing.T) {
	for _, tc := range []struct {
		target, source, want string
	}{
		{"auth-rfc", "inbox/today.md", "auth-rfc"},
		{"notes/auth-rfc", "inbox/today.md", "notes/auth-rfc"},
		{"./auth-rfc", "inbox/today.md", "inbox/auth-rfc"},
		{"./sub/x#S1", "inbox/today.md", "inbox/sub/x#S1"},
		{"../decisions/x", "inbox/today.md", "decisions/x"},
		{"../decisions/x", "inbox/deep/today.md", "inbox/decisions/x"},
		{"./x", "today.md", "x"},
		// Climbing out of the workspace resolves to nothing, so the link
		// keeps its text and dangles.
		{"../x", "today.md", "../x"},
		{"../../x", "inbox/today.md", "../../x"},
	} {
		if got := wikilink.Normalize(tc.target, tc.source); got != tc.want {
			test.Errorf("Normalize(%q, %q) = %q, want %q", tc.target, tc.source, got, tc.want)
		}
	}
}

func TestRelativeTo(test *testing.T) {
	for _, tc := range []struct {
		source, id, want string
	}{
		{"inbox/today.md", "inbox/auth-rfc", "./auth-rfc"},
		{"inbox/today.md", "inbox/sub/x", "./sub/x"},
		{"inbox/today.md", "decisions/x", "../decisions/x"},
		{"inbox/deep/today.md", "decisions/x", "../../decisions/x"},
		{"today.md", "x", "./x"},
		{"today.md", "notes/x", "./notes/x"},
	} {
		if got := wikilink.RelativeTo(tc.source, tc.id); got != tc.want {
			test.Errorf("RelativeTo(%q, %q) = %q, want %q", tc.source, tc.id, got, tc.want)
		}

		// Round trip: the rendered link resolves back to the id.
		if back := wikilink.Normalize(wikilink.RelativeTo(tc.source, tc.id), tc.source); back != tc.id {
			test.Errorf("Normalize(RelativeTo(%q, %q)) = %q", tc.source, tc.id, back)
		}
	}
}

func TestFileName(test *testing.T) {
	for relPath, want := range map[string]string{
		"notes/auth-rfc.md":   "auth-rfc",
		"notes/Auth RFC.md":   "Auth RFC",
		"notes/v1.2.md":       "v1.2",
		"docs/page.html":      "page",
		"notes/component.mdx": "component",
		"root.md":             "root",
	} {
		if got := wikilink.FileName(relPath); got != want {
			test.Errorf("FileName(%q) = %q, want %q", relPath, got, want)
		}
	}
}

func newNames(linkCase wikilink.Case, paths ...string) *wikilink.Names {
	names := wikilink.NewNames(linkCase)

	for _, relPath := range paths {
		id := relPath[:len(relPath)-len(".md")]
		names.Add(id, relPath)
	}

	return names
}

func TestNamesResolveName(test *testing.T) {
	names := newNames(wikilink.CaseInsensitive,
		"notes/auth-rfc.md",
		"notes/Auth RFC.md",
		"a/dup.md",
		"b/dup.md",
		"root.md",
		"deep/root.md",
	)

	for _, tc := range []struct {
		name       string
		wantID     string
		candidates []string
	}{
		{"auth-rfc", "notes/auth-rfc", nil},
		{"AUTH-RFC", "notes/auth-rfc", nil},
		{"auth rfc", "notes/Auth RFC", nil},
		{"dup", "", []string{"a/dup", "b/dup"}},
		{"missing", "", nil},
		// The exact id wins over a same-named file deeper in the tree.
		{"root", "root", nil},
	} {
		gotID, gotCandidates, resolveErr := names.ResolveName(tc.name)

		if resolveErr != nil {
			test.Fatalf("ResolveName(%q): %v", tc.name, resolveErr)
		}

		if gotID != tc.wantID || !reflect.DeepEqual(gotCandidates, tc.candidates) {
			test.Errorf("ResolveName(%q) = %q, %v; want %q, %v", tc.name, gotID, gotCandidates, tc.wantID, tc.candidates)
		}
	}
}

func TestNamesResolveName_CaseSensitive(test *testing.T) {
	names := newNames(wikilink.CaseSensitive, "notes/Auth RFC.md", "other/auth rfc.md")

	if id, _, _ := names.ResolveName("Auth RFC"); id != "notes/Auth RFC" {
		test.Errorf("exact-case name resolved to %q", id)
	}

	if id, _, _ := names.ResolveName("AUTH RFC"); id != "" {
		test.Errorf("case-sensitive lookup matched another case: %q", id)
	}
}

func TestNamesRemoveAndClone(test *testing.T) {
	names := newNames(wikilink.CaseInsensitive, "a/dup.md", "b/dup.md")
	after := names.Clone()
	after.Remove("b/dup")

	if id, _, _ := after.ResolveName("dup"); id != "a/dup" {
		test.Errorf("after removing b/dup, dup resolved to %q", id)
	}

	if id, candidates, _ := names.ResolveName("dup"); id != "" || len(candidates) != 2 {
		test.Errorf("Clone shared state with the original: %q %v", id, candidates)
	}
}

func TestResolve(test *testing.T) {
	names := newNames(wikilink.CaseInsensitive, "notes/auth-rfc.md", "a/dup.md", "b/dup.md")

	for _, tc := range []struct {
		target, source, wantID string
		candidates             []string
	}{
		{"auth-rfc", "inbox/today.md", "notes/auth-rfc", nil},
		{"auth-rfc#S1", "inbox/today.md", "notes/auth-rfc#S1", nil},
		{"dup", "inbox/today.md", "", []string{"a/dup", "b/dup"}},
		{"../notes/auth-rfc", "inbox/today.md", "notes/auth-rfc", nil},
		{"../../x", "inbox/today.md", "", nil},
		// A folder path is an id as written; whether a node has it is the
		// caller's question.
		{"notes/gone", "inbox/today.md", "notes/gone", nil},
	} {
		gotID, gotCandidates, resolveErr := wikilink.Resolve(tc.target, tc.source, names)

		if resolveErr != nil {
			test.Fatalf("Resolve(%q): %v", tc.target, resolveErr)
		}

		if gotID != tc.wantID || !reflect.DeepEqual(gotCandidates, tc.candidates) {
			test.Errorf("Resolve(%q, %q) = %q, %v; want %q, %v", tc.target, tc.source, gotID, gotCandidates, tc.wantID, tc.candidates)
		}
	}
}
