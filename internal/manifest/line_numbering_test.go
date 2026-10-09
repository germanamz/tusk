package manifest_test

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/linenum"
	"github.com/germanamz/tusk/internal/manifest"
)

func TestLineNumbering_DefaultsToLF(test *testing.T) {
	loaded := loadTOMLFromString(test, `[workspace]
name = "x"
`)

	if got := loaded.LineNumbering(); got != linenum.SchemeLF {
		test.Errorf("LineNumbering() = %q, want %q", got, linenum.SchemeLF)
	}
}

func TestLineNumbering_NilManifestDefaultsToLF(test *testing.T) {
	var loaded *manifest.Manifest

	if got := loaded.LineNumbering(); got != linenum.SchemeLF {
		test.Errorf("LineNumbering() = %q, want %q", got, linenum.SchemeLF)
	}
}

func TestLineNumbering_ParsesEachScheme(test *testing.T) {
	for _, scheme := range []linenum.Scheme{linenum.SchemeLF, linenum.SchemeUniversal, linenum.SchemeUnicode} {
		loaded := loadTOMLFromString(test, `[workspace]
name = "x"
line-numbering = "`+string(scheme)+`"
`)

		if got := loaded.LineNumbering(); got != scheme {
			test.Errorf("LineNumbering() = %q, want %q", got, scheme)
		}
	}
}

func TestLineNumbering_RejectsUnknownScheme(test *testing.T) {
	_, loadErr := loadTOMLString(test, `[workspace]
name = "x"
line-numbering = "crlf"
`)

	if loadErr == nil {
		test.Fatal("Load accepted line-numbering = \"crlf\", want an error")
	}

	if !strings.Contains(loadErr.Error(), "workspace.line-numbering") {
		test.Errorf("error = %q, want it to name workspace.line-numbering", loadErr)
	}
}
