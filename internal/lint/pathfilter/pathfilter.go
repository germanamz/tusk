// Package pathfilter provides a helper for the custom lint analyzers to
// short-circuit on packages that are excluded from per-package lint rules
// during the v0.14 naming-convention sweep. Each entry in the excluded slice
// mirrors a corresponding per-package exclusion in .golangci.yml and will be
// removed as its sweep phase completes; any residual entries are removed in
// Phase 8.
package pathfilter

import "regexp"

var excluded = []*regexp.Regexp{
	// service/ — removed by its corresponding sweep phase.
	regexp.MustCompile(`^github\.com/germanamz/tusk/service(/|$)`),
	// internal/tui/ — removed by its corresponding sweep phase.
	regexp.MustCompile(`^github\.com/germanamz/tusk/internal/tui(/|$)`),
	// internal/mcp/ — removed by its corresponding sweep phase.
	regexp.MustCompile(`^github\.com/germanamz/tusk/internal/mcp(/|$)`),
	// internal/portability/ — removed by its corresponding sweep phase.
	regexp.MustCompile(`^github\.com/germanamz/tusk/internal/portability(/|$)`),
	// filter/ — removed by its corresponding sweep phase.
	regexp.MustCompile(`^github\.com/germanamz/tusk/filter(/|$)`),
	// domain/ — removed by its corresponding sweep phase.
	regexp.MustCompile(`^github\.com/germanamz/tusk/domain(/|$)`),
	// syntax/ — removed by its corresponding sweep phase.
	regexp.MustCompile(`^github\.com/germanamz/tusk/syntax(/|$)`),
	// repository/ — removed by its corresponding sweep phase.
	regexp.MustCompile(`^github\.com/germanamz/tusk/repository(/|$)`),
	// sqlite/ — removed by its corresponding sweep phase.
	regexp.MustCompile(`^github\.com/germanamz/tusk/sqlite(/|$)`),
	// cmd/ — removed by its corresponding sweep phase.
	regexp.MustCompile(`^github\.com/germanamz/tusk/cmd(/|$)`),
	// tests/e2e/ — removed by its corresponding sweep phase.
	regexp.MustCompile(`^github\.com/germanamz/tusk/tests/e2e(/|$)`),
	// root package (client.go) — removed by its corresponding sweep phase.
	regexp.MustCompile(`^github\.com/germanamz/tusk$`),
	// config/ omitted from the v0.14 spec enumeration; tracked as
	// follow-up sweep (tusk task 06fa0f50) and removed by that task.
	regexp.MustCompile(`^github\.com/germanamz/tusk/config(/|$)`),
}

// Excluded reports whether pkgPath matches any of the per-package exclusion
// regexes. Analyzers call this at the top of their Run function and
// short-circuit with no diagnostics when it returns true.
func Excluded(pkgPath string) bool {
	for _, re := range excluded {
		if re.MatchString(pkgPath) {
			return true
		}
	}
	return false
}
