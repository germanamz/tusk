package manifest_test

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/wikilink"
)

func TestLinkCase_DefaultsToInsensitive(test *testing.T) {
	loaded := loadTOMLFromString(test, `[workspace]
name = "x"
`)

	if got := loaded.LinkCase(); got != wikilink.CaseInsensitive {
		test.Errorf("LinkCase() = %q, want %q", got, wikilink.CaseInsensitive)
	}
}

func TestLinkCase_NilManifestDefaultsToInsensitive(test *testing.T) {
	var loaded *manifest.Manifest

	if got := loaded.LinkCase(); got != wikilink.CaseInsensitive {
		test.Errorf("LinkCase() = %q, want %q", got, wikilink.CaseInsensitive)
	}
}

func TestLinkCase_ParsesEachCase(test *testing.T) {
	for _, linkCase := range []wikilink.Case{wikilink.CaseInsensitive, wikilink.CaseSensitive} {
		loaded := loadTOMLFromString(test, `[workspace]
name = "x"
link-case = "`+string(linkCase)+`"
`)

		if got := loaded.LinkCase(); got != linkCase {
			test.Errorf("LinkCase() = %q, want %q", got, linkCase)
		}
	}
}

func TestLinkCase_RejectsUnknownValue(test *testing.T) {
	_, loadErr := loadTOMLString(test, `[workspace]
name = "x"
link-case = "auto"
`)

	if loadErr == nil {
		test.Fatal("Load accepted link-case = \"auto\", want an error")
	}

	if !strings.Contains(loadErr.Error(), "workspace.link-case") {
		test.Errorf("error = %q, want it to name workspace.link-case", loadErr)
	}
}
