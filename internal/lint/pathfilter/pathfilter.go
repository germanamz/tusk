// Package pathfilter provides a helper for the custom lint analyzers to
// short-circuit on packages that are excluded from per-package lint rules
// during the v0.14 naming-convention sweep. Each entry in the excluded slice
// mirrors a corresponding per-package exclusion in .golangci.yml and will be
// removed as its sweep phase completes; any residual entries are removed in
// Phase 8.
package pathfilter

import (
	"regexp"
	"strings"
)

var excluded = []*regexp.Regexp{
	// config/ omitted from the v0.14 spec enumeration; tracked as
	// follow-up sweep (tusk task 06fa0f50) and removed by that task.
	regexp.MustCompile(`^github\.com/germanamz/tusk/config(/|$)`),
}

// Excluded reports whether pkgPath matches any of the per-package exclusion
// regexes. Analyzers call this at the top of their Run function and
// short-circuit with no diagnostics when it returns true.
//
// External test packages — Go's `package foo_test` convention — produce a
// pkgPath ending in `_test` (e.g. `github.com/germanamz/tusk/sqlite_test`).
// The regexes target the production package boundary, so the trailing
// `_test` suffix is stripped before matching: a test file in `sqlite_test`
// is governed by the same exclusion as the `sqlite` production package.
func Excluded(pkgPath string) bool {
	pkgPath = strings.TrimSuffix(pkgPath, "_test")
	for _, re := range excluded {
		if re.MatchString(pkgPath) {
			return true
		}
	}
	return false
}
