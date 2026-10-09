package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rulesManifest is the #764 example vault: notes carry an enum `domain`, and
// two rules check that product notes live under docs/product/ and link only
// to product notes and the glossary.
const rulesManifest = `[workspace]
name = "rules"

[node-types.note]
properties = [
  { name = "domain", type = "enum", values = ["product", "engineering"] },
]

[edge-types.references]
from        = ["*"]
to          = ["*"]
cardinality = "many-to-many"
wikilinks   = true

[rule.domain-matches-directory]
description = "a product note lives under docs/product/"
filter      = "type=note AND domain=product AND NOT path=docs/product/**"

[rule.product-links-stay-in-product]
description = "a product page links only to product pages and the glossary"
filter      = "domain=product AND references-> (type=note AND NOT domain=product AND NOT id=docs/glossary)"
`

func writeRulesWorkspace(test *testing.T, manifestBody string, files map[string]string) string {
	test.Helper()

	root := test.TempDir()

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(manifestBody), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	for relPath, content := range files {
		abs := filepath.Join(root, relPath)

		if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
			test.Fatalf("mkdir: %v", mkErr)
		}

		if writeErr := os.WriteFile(abs, []byte(content), 0o644); writeErr != nil {
			test.Fatalf("write %s: %v", relPath, writeErr)
		}
	}

	if _, stderr, ok := runCLISplit(root, "reindex"); !ok {
		test.Fatalf("reindex failed: %s", stderr)
	}

	return root
}

func rulesFiles() map[string]string {
	return map[string]string{
		"docs/product/recording.md": "---\ntype: note\ndomain: product\n---\n\n# Recording\n\nSee [[docs/glossary]] and [[docs/product/totals]].\n",
		"docs/product/totals.md":    "---\ntype: note\ndomain: product\n---\n\n# Totals\n\nBuilt on [[docs/eng/pipeline]].\n",
		"docs/eng/pipeline.md":      "---\ntype: note\ndomain: engineering\n---\n\n# Pipeline\n",
		"docs/eng/pricing.md":       "---\ntype: note\ndomain: product\n---\n\n# Pricing\n",
		"docs/glossary.md":          "---\ntype: note\ndomain: engineering\n---\n\n# Glossary\n",
	}
}

// TestDoctor_ReportsRuleViolations runs both #764 example rules end to end:
// each breaking node is one error under rule:<name>, and doctor exits 1.
func TestDoctor_ReportsRuleViolations(test *testing.T) {
	root := writeRulesWorkspace(test, rulesManifest, rulesFiles())

	stdout, _, ok := runCLISplit(root, "doctor")

	if ok {
		test.Errorf("exit 0, want non-zero with rule errors present")
	}

	out := stdout.String()

	for _, want := range []string{
		"  error    [rule:domain-matches-directory] docs/eng/pricing: a product note lives under docs/product/\n",
		"  error    [rule:product-links-stay-in-product] docs/product/totals: a product page links only to product pages and the glossary\n",
	} {
		if !strings.Contains(out, want) {
			test.Errorf("doctor output missing %q; got:\n%s", want, out)
		}
	}

	if got := strings.Count(out, "[rule:"); got != 2 {
		test.Errorf("rule issues = %d, want 2 (files only, no sub-units); got:\n%s", got, out)
	}
}

// A rule declared at severity "warning" passes the default --fail-on=error
// and fails --fail-on=warning, like any other warning.
func TestDoctor_RuleSeverityDrivesExit(test *testing.T) {
	manifestBody := strings.Replace(rulesManifest,
		`filter      = "type=note AND domain=product AND NOT path=docs/product/**"`,
		"filter      = \"type=note AND domain=product AND NOT path=docs/product/**\"\nseverity    = \"warning\"", 1)
	files := rulesFiles()

	// Keep only the directory rule's violation.
	files["docs/product/totals.md"] = "---\ntype: note\ndomain: product\n---\n\n# Totals\n"

	root := writeRulesWorkspace(test, manifestBody, files)

	stdout, stderr, ok := runCLISplit(root, "doctor")

	if !ok {
		test.Fatalf("default doctor failed on a warning-only report: %s\n%s", stderr, stdout)
	}

	if !strings.Contains(stdout.String(), "  warning  [rule:domain-matches-directory] docs/eng/pricing:") {
		test.Errorf("want the rule reported as a warning; got:\n%s", stdout)
	}

	if _, _, strictOK := runCLISplit(root, "doctor", "--fail-on=warning"); strictOK {
		test.Errorf("--fail-on=warning exited 0 with a warning-severity rule violation")
	}
}

// An invalid rule never fails the load: reload swaps and lists it under
// warnings, and doctor reports it as a rule-invalid error.
func TestDoctor_InvalidRuleIsReportedNotFatal(test *testing.T) {
	manifestBody := strings.Replace(rulesManifest, "domain=product AND NOT path", "domain=prodcut AND NOT path", 1)
	root := writeRulesWorkspace(test, manifestBody, map[string]string{
		"docs/product/recording.md": "---\ntype: note\ndomain: product\n---\n\n# Recording\n",
	})

	reloadOut, reloadErr, reloadOK := runCLISplit(root, "reload")

	if !reloadOK {
		test.Fatalf("reload failed on an invalid rule: %s", reloadErr)
	}

	if !strings.Contains(reloadOut.String(), `rule \"domain-matches-directory\": filter: \"prodcut\" is not a value of enum property \"domain\"`) {
		test.Errorf("reload warnings missing the invalid rule; got:\n%s", reloadOut)
	}

	stdout, _, ok := runCLISplit(root, "doctor")

	if ok {
		test.Errorf("doctor exit 0, want non-zero with an invalid rule")
	}

	if !strings.Contains(stdout.String(), `  error    [rule-invalid] domain-matches-directory: filter: "prodcut" is not a value of enum property "domain"`) {
		test.Errorf("doctor output missing the rule-invalid error; got:\n%s", stdout)
	}
}
