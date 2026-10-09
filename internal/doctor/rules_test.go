package doctor_test

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/manifest"
)

// rulesManifestHeader declares a note type with an enum domain, the shape the
// issue's example rules check.
const rulesManifestHeader = `[workspace]
name = "brain"

[node-types.note]
properties = [
  { name = "domain", type = "enum", values = ["product", "engineering"] },
]

[edge-types.references]
from        = ["*"]
to          = ["*"]
cardinality = "many-to-many"
`

func TestCompileRules_RejectsInvalidRules(test *testing.T) {
	loaded := loadGraphExpansionManifest(test, rulesManifestHeader+`
[rule.good]
filter = "type=note AND domain=product AND NOT path=docs/product/**"

[rule.blank-filter]
filter = "   "

[rule.no-check]
description = "forgot the filter"

[rule.bad-severity]
filter   = "type=note"
severity = "fatal"

[rule.misspelled-key]
filter  = "type=note"
severty = "warning"

[rule.parse-error]
filter = "type=note AND"

[rule.undeclared-type]
filter = "type=notee"

[rule.bad-enum]
filter = "references-> domain=prodcut"

[rule.wrong-type]
filter = ["type=note"]
`)

	compiled, ruleErrs := doctor.CompileRules(loaded)

	if len(compiled) != 1 || compiled[0].Name != "good" || compiled[0].Severity != doctor.SeverityError {
		test.Fatalf("compiled = %+v, want only good at severity error", compiled)
	}

	want := map[string]string{
		"bad-enum":        `filter: "prodcut" is not a value of enum property "domain"`,
		"bad-severity":    `severity "fatal" is not one of error, warning, advice`,
		"blank-filter":    "filter is empty; it would match every node",
		"misspelled-key":  `unknown key "severty" (a rule takes description, severity, filter)`,
		"no-check":        "declares no check; set filter",
		"parse-error":     "filter: ",
		"undeclared-type": `filter: node type "notee" not declared in manifest`,
		"wrong-type":      `(last key "rule.wrong-type.filter"): incompatible types`,
	}

	got := map[string]string{}

	for _, ruleErr := range ruleErrs {
		got[ruleErr.Name] = ruleErr.Message
	}

	if len(got) != len(want) {
		test.Fatalf("rule errors = %v, want one for each of %v", ruleErrs, want)
	}

	for name, wantMessage := range want {
		if !strings.Contains(got[name], wantMessage) {
			test.Errorf("%s: message = %q, want it to contain %q", name, got[name], wantMessage)
		}
	}

	// Sorted by rule name, so reload warnings and doctor output are stable.
	for position := 1; position < len(ruleErrs); position++ {
		if ruleErrs[position-1].Name > ruleErrs[position].Name {
			test.Fatalf("rule errors not sorted by name: %v", ruleErrs)
		}
	}
}

// A vault that declares no node types is schemaless by choice: there is
// nothing to check property names against, so a rule gets query's validation.
// Edge types are still checked.
func TestCompileRules_SchemalessVaultSkipsStrictChecks(test *testing.T) {
	loaded := loadGraphExpansionManifest(test, `[workspace]
name = "brain"

[rule.ad-hoc]
filter = "domain=product AND NOT path=docs/product/**"

[rule.undeclared-edge]
filter = "mentions-> domain=product"
`)

	compiled, ruleErrs := doctor.CompileRules(loaded)

	if len(compiled) != 1 || compiled[0].Name != "ad-hoc" {
		test.Fatalf("compiled = %+v, want only ad-hoc", compiled)
	}

	if len(ruleErrs) != 1 || ruleErrs[0].Name != "undeclared-edge" || !strings.Contains(ruleErrs[0].Message, `edge type "mentions" not declared`) {
		test.Fatalf("rule errors = %v, want undeclared-edge rejected", ruleErrs)
	}
}

func TestRun_ReportsRuleViolations(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	nodes := index.NewNodeRepo(store)

	for _, row := range []index.NodeRow{
		{ID: "docs/product/a", Type: "note", Path: "docs/product/a.md", Title: "A", PropertiesJSON: `{"domain":"product"}`, LastChecksum: "x"},
		{ID: "docs/eng/b", Type: "note", Path: "docs/eng/b.md", Title: "B", PropertiesJSON: `{"domain":"product"}`, LastChecksum: "x"},
		{ID: "docs/eng/c", Type: "note", Path: "docs/eng/c.md", Title: "C", PropertiesJSON: `{"domain":"engineering"}`, LastChecksum: "x"},
	} {
		if upsertErr := nodes.Upsert(row); upsertErr != nil {
			test.Fatalf("upsert %s: %v", row.ID, upsertErr)
		}
	}

	section := index.NodeRow{
		ID: "docs/eng/b#S1", Type: "section", Path: "docs/eng/b.md", Title: "Intro", PropertiesJSON: `{}`,
		ParentID: sql.NullString{String: "docs/eng/b", Valid: true},
	}

	if upsertErr := nodes.BulkUpsert([]index.NodeRow{section}, ""); upsertErr != nil {
		test.Fatalf("upsert section: %v", upsertErr)
	}

	loaded := loadGraphExpansionManifest(test, rulesManifestHeader+`
[rule.domain-matches-directory]
description = "a product note lives under docs/product/"
filter      = "type=note AND domain=product AND NOT path=docs/product/**"

[rule.outside-product]
filter   = "NOT path=docs/product/**"
severity = "warning"

[rule.sections-outside-product]
description = "sections outside docs/product/"
filter      = "type=section AND NOT path=docs/product/**"
severity    = "advice"

[rule.broken]
filter = "type=notee"
`)

	report, runErr := doctor.Run(doctor.Config{
		Nodes:    nodes,
		Edges:    index.NewEdgeRepo(store),
		Manifest: loaded,
		DB:       store.DB(),
	})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	var got []doctor.Issue

	for _, issue := range report.Issues {
		if issue.Kind == doctor.IssueRuleInvalid || strings.HasPrefix(issue.Kind, doctor.RuleKindPrefix) {
			got = append(got, issue)
		}
	}

	want := []doctor.Issue{
		{Kind: doctor.IssueRuleInvalid, Severity: doctor.SeverityError, NodeID: "broken", Message: `filter: node type "notee" not declared in manifest at column 1 (did you mean "note"?)`},
		{Kind: "rule:domain-matches-directory", Severity: doctor.SeverityError, NodeID: "docs/eng/b", Message: "a product note lives under docs/product/"},
		// Files only: the section under docs/eng/b.md shares its path but is
		// not reported, because the filter names no sub-unit type.
		{Kind: "rule:outside-product", Severity: doctor.SeverityWarning, NodeID: "docs/eng/b", Message: `matches filter "NOT path=docs/product/**"`},
		{Kind: "rule:outside-product", Severity: doctor.SeverityWarning, NodeID: "docs/eng/c", Message: `matches filter "NOT path=docs/product/**"`},
		// Naming a sub-unit type opts the rule into sub-units.
		{Kind: "rule:sections-outside-product", Severity: doctor.SeverityAdvice, NodeID: "docs/eng/b#S1", Message: "sections outside docs/product/"},
	}

	if !reflect.DeepEqual(got, want) {
		test.Fatalf("rule issues =\n%+v\nwant\n%+v", got, want)
	}
}

// Without a database handle the rule check is a no-op, like every other check
// whose repo is absent.
func TestRun_RulesNeedADatabase(test *testing.T) {
	loaded := loadGraphExpansionManifest(test, rulesManifestHeader+`
[rule.any-note]
filter = "type=note"
`)

	report, runErr := doctor.Run(doctor.Config{Manifest: loaded})

	if runErr != nil {
		test.Fatalf("Run: %v", runErr)
	}

	for _, issue := range report.Issues {
		if strings.HasPrefix(issue.Kind, doctor.RuleKindPrefix) {
			test.Fatalf("unexpected rule issue without a database: %+v", issue)
		}
	}
}

// manifestWithRules builds a hand-made manifest for the tests that don't need
// the loader.
func manifestWithRules(rules map[string]manifest.Rule) *manifest.Manifest {
	return &manifest.Manifest{Rules: rules}
}

func TestCompileRules_NilAndEmpty(test *testing.T) {
	for name, loaded := range map[string]*manifest.Manifest{"nil": nil, "no rules": manifestWithRules(nil)} {
		compiled, ruleErrs := doctor.CompileRules(loaded)

		if len(compiled) != 0 || len(ruleErrs) != 0 {
			test.Errorf("%s: CompileRules = %v, %v, want nothing", name, compiled, ruleErrs)
		}
	}
}
