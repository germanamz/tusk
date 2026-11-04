package subunit_test

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"testing"

	"github.com/germanamz/tusk/internal/subunit"
)

func TestNewLeafContext_Label(test *testing.T) {
	file := subunit.ContextFile{Title: "ledger-sync", Path: "services/ledger-sync.md"}

	cases := []struct {
		name     string
		mode     string
		file     subunit.ContextFile
		headings []string
		want     string
	}{
		{name: "none has no label", mode: "none", file: file, headings: []string{"Async"}, want: ""},
		{name: "empty mode behaves as none", mode: "", file: file, want: ""},
		{name: "title ignores headings", mode: "title", file: file, headings: []string{"Async"}, want: "ledger-sync"},
		{name: "untitled file goes by its file name", mode: "title", file: subunit.ContextFile{Path: "services/ledger-sync.md"}, want: "ledger-sync"},
		{name: "path joins the headings outermost first", mode: "path", file: file, headings: []string{"Runtime", "Async"}, want: "ledger-sync > Runtime > Async"},
		{name: "path drops an H1 that repeats the title", mode: "path", file: file, headings: []string{"Ledger-Sync", "Async"}, want: "ledger-sync > Async"},
		{name: "path skips blank headings", mode: "path", file: file, headings: []string{" ", "Async"}, want: "ledger-sync > Async"},
		{name: "path at the document root is the title", mode: "path", file: file, want: "ledger-sync"},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			if got := subunit.NewLeafContext(testCase.mode, nil, testCase.file, testCase.headings).Label; got != testCase.want {
				test.Errorf("Label = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestNewLeafContext_PropertyLines(test *testing.T) {
	file := subunit.ContextFile{
		Title: "ledger-sync",
		Type:  "service",
		PropertiesJSON: `{"aliases":["ls","ledger",["nested"],""],"tier":1,"ratio":0.5,"public":false,` +
			`"owner":"  payments\n team ","empty":"","none":null,"meta":{"a":1}}`,
	}

	names := []string{"owner", "aliases", "tier", "ratio", "public", "type", "empty", "none", "meta", "absent"}
	want := []string{"owner: payments team", "aliases: ls, ledger", "tier: 1", "ratio: 0.5", "public: false", "type: service"}

	if got := subunit.NewLeafContext("none", names, file, nil).Lines; !slices.Equal(got, want) {
		test.Errorf("Lines = %q, want %q", got, want)
	}

	if got := subunit.NewLeafContext("title", names, subunit.ContextFile{PropertiesJSON: "not json"}, nil).Lines; len(got) != 0 {
		test.Errorf("undecodable properties rendered %q, want no lines", got)
	}
}

func TestLeafContext_Header(test *testing.T) {
	leaf := subunit.LeafContext{Label: "ledger-sync > Async", Lines: []string{"aliases: ls"}}

	if got := string(leaf.Header(false)); got != "ledger-sync > Async\naliases: ls\n" {
		test.Errorf("Header(false) = %q", got)
	}

	if got := string(leaf.Header(true)); got != "aliases: ls\n" {
		test.Errorf("Header(true) = %q, want the label left to {title}", got)
	}

	if got := (subunit.LeafContext{Label: "x"}).Header(true); got != nil {
		test.Errorf("a label-only context in {title} has header %q, want nil", got)
	}

	if !(subunit.LeafContext{}).Empty() || leaf.Empty() {
		test.Error("Empty must hold exactly when there is no label and no lines")
	}
}

// TestLeafContext_ContentHash: an empty context hashes the payload alone, the
// hash a leaf had before #862, and any context changes it.
func TestLeafContext_ContentHash(test *testing.T) {
	sum := sha256.Sum256([]byte("leaf text"))

	if got := (subunit.LeafContext{}).ContentHash("leaf text"); got != hex.EncodeToString(sum[:]) {
		test.Errorf("empty context hash = %s, want sha256 of the payload", got)
	}

	titled := sha256.Sum256([]byte("ledger-sync\nleaf text"))

	if got := (subunit.LeafContext{Label: "ledger-sync"}).ContentHash("leaf text"); got != hex.EncodeToString(titled[:]) {
		test.Errorf("titled hash = %s, want sha256 of the header and payload", got)
	}
}
