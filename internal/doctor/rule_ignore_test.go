package doctor_test

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/doctor"
	"github.com/germanamz/tusk/internal/index"
)

// A node lists the rules it is an accepted exception to under tusk-ignore
// (#852). Its violations are dropped, and an entry that ignores nothing is a
// rule-ignore-unused warning, so stale exceptions don't pile up.
func TestRun_TuskIgnoreExemptsANodeFromARule(test *testing.T) {
	store, _ := index.Open(filepath.Join(test.TempDir(), "index.db"))
	defer store.Close()

	nodes := index.NewNodeRepo(store)

	for _, row := range []index.NodeRow{
		// Breaks both rules, exempt from one of them.
		{ID: "retired", Type: "note", Path: "retired.md", Title: "R", PropertiesJSON: `{"domain":"product","tusk-ignore":["product-only"]}`, LastChecksum: "x"},
		// Breaks no rule, so its exemption is stale.
		{ID: "fixed", Type: "note", Path: "docs/product/fixed.md", Title: "F", PropertiesJSON: `{"domain":"engineering","tusk-ignore":"product-only"}`, LastChecksum: "x"},
		// Names a rule tusk.toml does not declare, and an invalid rule.
		{ID: "ghost", Type: "note", Path: "docs/product/ghost.md", Title: "G", PropertiesJSON: `{"domain":"engineering","tusk-ignore":["no-such-rule","broken"]}`, LastChecksum: "x"},
		// Not a rule name or a list of them.
		{ID: "odd", Type: "note", Path: "docs/product/odd.md", Title: "O", PropertiesJSON: `{"domain":"engineering","tusk-ignore":5}`, LastChecksum: "x"},
		// The file's exemption covers its sections, which carry no frontmatter.
		{ID: "long", Type: "note", Path: "docs/product/long.md", Title: "L", PropertiesJSON: `{"domain":"engineering","tusk-ignore":["no-sections"]}`, LastChecksum: "x"},
		{ID: "plain", Type: "note", Path: "plain.md", Title: "P", PropertiesJSON: `{"domain":"engineering"}`, LastChecksum: "x"},
	} {
		if upsertErr := nodes.Upsert(row); upsertErr != nil {
			test.Fatalf("upsert %s: %v", row.ID, upsertErr)
		}
	}

	sections := []index.NodeRow{
		{ID: "long#S1", Type: "section", Path: "docs/product/long.md", Title: "One", PropertiesJSON: `{}`, ParentID: sql.NullString{String: "long", Valid: true}},
		{ID: "plain#S1", Type: "section", Path: "plain.md", Title: "One", PropertiesJSON: `{}`, ParentID: sql.NullString{String: "plain", Valid: true}},
	}

	if upsertErr := nodes.BulkUpsert(sections, ""); upsertErr != nil {
		test.Fatalf("upsert sections: %v", upsertErr)
	}

	loaded := loadGraphExpansionManifest(test, rulesManifestHeader+`
[rule.product-only]
filter = "type=note AND domain=product"

[rule.outside-product]
filter   = "type=note AND NOT path=docs/product/**"
severity = "warning"

[rule.no-sections]
filter   = "type=section"
severity = "advice"

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

	var got []string

	for _, issue := range report.Issues {
		if strings.HasPrefix(issue.Kind, doctor.RuleKindPrefix) || issue.Kind == doctor.IssueRuleIgnoreUnused {
			got = append(got, issue.Severity+" "+issue.Kind+" "+issue.NodeID+": "+issue.Message)
		}
	}

	// "broken" on ghost is not reported: a rule that doesn't compile can't
	// say whether the node breaks it, and rule-invalid already flags it.
	want := []string{
		`warning rule:outside-product plain: matches filter "type=note AND NOT path=docs/product/**"`,
		`warning rule:outside-product retired: matches filter "type=note AND NOT path=docs/product/**"`,
		`warning rule-ignore-unused fixed: tusk-ignore lists "product-only", but this node no longer breaks that rule; remove the entry`,
		`warning rule-ignore-unused ghost: tusk-ignore lists "no-such-rule", but tusk.toml declares no [rule.no-such-rule]`,
		`warning rule-ignore-unused odd: tusk-ignore takes a rule name or a list of rule names; 5 ignores nothing`,
		`advice rule:no-sections plain#S1: matches filter "type=section"`,
	}

	if !reflect.DeepEqual(got, want) {
		test.Fatalf("issues =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
