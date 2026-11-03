package doctor

import (
	"github.com/germanamz/tusk/internal/manifest"
)

// ManifestWarnings lists the problems in loaded that don't fail a load, worded
// for reload's warnings: invalid rules, invalid checks, then keys tusk does
// not read. Both the
// CLI and MCP reload print them. Nil without a manifest.
func ManifestWarnings(loaded *manifest.Manifest) []string {
	if loaded == nil {
		return nil
	}

	var warnings []string

	_, ruleErrs := CompileRules(loaded)

	for _, ruleErr := range ruleErrs {
		warnings = append(warnings, ruleErr.Error())
	}

	_, checkErrs := CompileChecks(loaded)

	for _, checkErr := range checkErrs {
		warnings = append(warnings, checkErr.Error())
	}

	for _, issue := range unknownKeyIssues(loaded) {
		warnings = append(warnings, "tusk.toml: "+issue.NodeID+": "+issue.Message)
	}

	return warnings
}

// unknownKeyIssues reports each key the manifest loader found no reader for.
func unknownKeyIssues(loaded *manifest.Manifest) []Issue {
	issues := make([]Issue, 0, len(loaded.UnknownKeys))

	for _, unknown := range loaded.UnknownKeys {
		what := "key"

		if unknown.Table {
			what = "table"
		}

		issues = append(issues, Issue{
			Kind:    IssueManifestUnknownKey,
			NodeID:  unknown.Key,
			Message: "tusk does not read this " + what + "; check its spelling, or upgrade tusk if a newer version added it",
		})
	}

	return issues
}
