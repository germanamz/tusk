package manifest_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
)

func TestLoad_DecodesRules(test *testing.T) {
	manifestPath := writeAliasManifest(test, `[workspace]
name = "brain"

[rule.domain-matches-directory]
description = "a product note lives under docs/product/"
filter      = "type=note AND domain=product AND NOT path=docs/product/**"

[rule.product-links-stay-in-product]
filter   = "domain=product"
severity = "warning"
`)

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	want := map[string]manifest.Rule{
		"domain-matches-directory": {
			Description: "a product note lives under docs/product/",
			Filter:      "type=note AND domain=product AND NOT path=docs/product/**",
		},
		"product-links-stay-in-product": {
			Filter:   "domain=product",
			Severity: "warning",
		},
	}

	if !reflect.DeepEqual(loaded.Rules, want) {
		test.Fatalf("Rules = %#v, want %#v", loaded.Rules, want)
	}
}

func TestLoad_NoRulesLeavesRulesEmpty(test *testing.T) {
	manifestPath := writeAliasManifest(test, "[workspace]\nname = \"brain\"\n")

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	if len(loaded.Rules) != 0 {
		test.Fatalf("Rules = %#v, want none", loaded.Rules)
	}
}

// A misspelled key must not fail the load (doctor reports it), and must not be
// ignored either: it lands on the rule's UnknownKeys, sorted and deduplicated.
func TestLoad_RecordsUnknownRuleKeys(test *testing.T) {
	manifestPath := writeAliasManifest(test, `[workspace]
name = "brain"

[rule.typo]
filter  = "type=note"
severty = "warning"
expect  = "empty"

[rule.typo.nested]
inner = 1

[rule.clean]
filter = "type=note"
`)

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	if got, want := loaded.Rules["typo"].UnknownKeys, []string{"expect", "nested", "severty"}; !reflect.DeepEqual(got, want) {
		test.Fatalf("typo UnknownKeys = %v, want %v", got, want)
	}

	if got := loaded.Rules["clean"].UnknownKeys; len(got) != 0 {
		test.Fatalf("clean UnknownKeys = %v, want none", got)
	}
}

func TestDeclaresUserNodeTypes(test *testing.T) {
	cases := []struct {
		name   string
		loaded *manifest.Manifest
		want   bool
	}{
		{name: "nil manifest", loaded: nil, want: false},
		{name: "no node types", loaded: &manifest.Manifest{}, want: false},
		{name: "only built-in sub-unit types", loaded: &manifest.Manifest{NodeTypes: manifest.SubdocumentNodeTypes()}, want: false},
		{name: "a user type", loaded: &manifest.Manifest{NodeTypes: map[string]manifest.NodeType{"note": {}}}, want: true},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			if got := testCase.loaded.DeclaresUserNodeTypes(); got != testCase.want {
				test.Fatalf("DeclaresUserNodeTypes() = %v, want %v", got, testCase.want)
			}
		})
	}
}

// A value of the wrong type fails only its own rule: Load succeeds, the rule
// carries the decode error (naming the key) for doctor to report, and the
// other rules decode normally.
func TestLoad_WrongTypedRuleIsRecordedNotFatal(test *testing.T) {
	manifestPath := writeAliasManifest(test, `[workspace]
name = "brain"

[rule.list-filter]
filter = ["type=note"]

[rule.number-severity]
filter   = "type=note"
severity = 3

[rule]
scalar = "type=note"

[[rule.array]]
filter = "type=note"

[rule.fine]
filter = "type=note"
`)

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	for name, wantKey := range map[string]string{
		"list-filter":     `"rule.list-filter.filter"`,
		"number-severity": `"rule.number-severity.severity"`,
		"scalar":          `"rule.scalar"`,
		"array":           `"rule.array"`,
	} {
		rule := loaded.Rules[name]

		if !strings.Contains(rule.DecodeError, wantKey) {
			test.Errorf("%s: DecodeError = %q, want it to name %s", name, rule.DecodeError, wantKey)
		}

		if len(rule.UnknownKeys) != 0 {
			test.Errorf("%s: UnknownKeys = %v, want none for a rule that failed to decode", name, rule.UnknownKeys)
		}
	}

	if fine := loaded.Rules["fine"]; fine.DecodeError != "" || fine.Filter != "type=note" {
		test.Errorf("fine = %+v, want a clean decode", fine)
	}
}
