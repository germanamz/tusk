package manifest_test

import (
	"strings"
	"testing"
)

func TestLexicalWeight_AbsentIsOff(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"
`)

	if loaded.LexicalWeight != 0 {
		test.Errorf("LexicalWeight = %v, want 0", loaded.LexicalWeight)
	}
}

func TestLexicalWeight_Decodes(test *testing.T) {
	loaded := loadInlineManifest(test, `
[workspace]
name = "x"

[query]
lexical-weight = 0.5

[query.graph-expansion]
enabled = true
`)

	if loaded.LexicalWeight != 0.5 {
		test.Errorf("LexicalWeight = %v, want 0.5", loaded.LexicalWeight)
	}

	if !loaded.GraphExpansion.Enabled {
		test.Errorf("GraphExpansion.Enabled = false; [query] keys must not disturb the subtable")
	}
}

func TestLexicalWeight_AcceptsBounds(test *testing.T) {
	for _, value := range []string{"0", "0.0", "1", "1.0"} {
		loaded := loadInlineManifest(test, `
[workspace]
name = "x"

[query]
lexical-weight = `+value+`
`)

		if loaded.LexicalWeight < 0 || loaded.LexicalWeight > 1 {
			test.Errorf("lexical-weight = %s decoded as %v", value, loaded.LexicalWeight)
		}
	}
}

func TestLexicalWeight_RejectsOutOfRange(test *testing.T) {
	for _, badValue := range []string{"-0.1", "1.5", "nan", `"high"`} {
		_, loadErr := loadInlineManifestAllowError(test, `
[workspace]
name = "x"

[query]
lexical-weight = `+badValue+`
`)

		if loadErr == nil {
			test.Errorf("lexical-weight = %s: expected an error", badValue)

			continue
		}

		if !strings.Contains(loadErr.Error(), "lexical-weight") {
			test.Errorf("lexical-weight = %s: error %q does not name the key", badValue, loadErr.Error())
		}
	}
}
