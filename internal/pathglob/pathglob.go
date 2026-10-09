// Package pathglob translates the glob patterns a filter writes against `path`
// and `id` into regular expressions. The pattern language follows the wildcard
// rules gitignore documents, anchored to the whole value: `*` matches any run
// of characters except `/`, `?` matches exactly one character other than `/`,
// and a segment that is exactly `**` crosses directories.
//
// The output is a plain regex string rather than a matcher so the database
// engine evaluates it natively. It uses only syntax that SQLite's Go-backed
// REGEXP, Postgres `~`, MySQL REGEXP_LIKE and DuckDB all read the same way:
// `^…$` anchors, the `[^/]` class, the `([^/]*/)*` directory group, and
// backslash-escaped metacharacters. Nothing in it is RE2-only, and it never
// uses `.`, so engines that differ on whether `.` matches a newline agree.
package pathglob

import (
	"regexp"
	"strings"
)

// doubleStar is the segment that crosses directories.
const doubleStar = "**"

// IsPattern reports whether value holds a wildcard (`*` or `?`).
func IsPattern(value string) bool {
	return strings.ContainsAny(value, "*?")
}

// ToRegexp translates pattern into an anchored regular expression.
//
//   - `*` (and `**` inside a segment) matches any run of characters except `/`.
//   - `?` matches exactly one character other than `/`.
//   - A leading `**/` matches zero or more directories, so `**/x` finds `x` at
//     any depth, including the root.
//   - A middle `/**/` matches zero or more directories: `a/**/b` matches `a/b`,
//     `a/x/b` and `a/x/y/b`.
//   - A trailing `/**` matches everything inside the directory but not the
//     directory itself.
//   - A pattern that is exactly `**` matches everything.
//
// Every other character is literal.
func ToRegexp(pattern string) string {
	segments := collapseDoubleStars(strings.Split(pattern, "/"))
	last := len(segments) - 1

	var builder strings.Builder

	builder.WriteString("^")

	needSeparator := false

	for index, segment := range segments {
		if segment != doubleStar {
			if needSeparator {
				builder.WriteString("/")
			}

			builder.WriteString(translateSegment(segment))

			needSeparator = true

			continue
		}

		switch {
		case last == 0:
			builder.WriteString("([^/]*/)*[^/]*")
		case index == last:
			builder.WriteString("/([^/]*/)*[^/]+")
		case index == 0:
			builder.WriteString("([^/]*/)*")
		default:
			builder.WriteString("/([^/]*/)*")
		}

		needSeparator = false
	}

	builder.WriteString("$")

	return builder.String()
}

// collapseDoubleStars folds runs of consecutive `**` segments into one, since
// `a/**/**/b` names the same set of paths as `a/**/b`.
func collapseDoubleStars(segments []string) []string {
	collapsed := make([]string, 0, len(segments))

	for _, segment := range segments {
		if segment == doubleStar && len(collapsed) > 0 && collapsed[len(collapsed)-1] == doubleStar {
			continue
		}

		collapsed = append(collapsed, segment)
	}

	return collapsed
}

// translateSegment translates one `/`-free segment. A run of `*` becomes a
// single `[^/]*`, so `**` sharing a segment with other characters acts as `*`.
// Literal runs go through regexp.QuoteMeta, whose escape set is exactly the
// POSIX ERE metacharacters.
func translateSegment(segment string) string {
	var builder strings.Builder

	literalStart := 0

	for index := 0; index < len(segment); index++ {
		switch segment[index] {
		case '*':
			builder.WriteString(regexp.QuoteMeta(segment[literalStart:index]))

			if index == 0 || segment[index-1] != '*' {
				builder.WriteString("[^/]*")
			}

			literalStart = index + 1
		case '?':
			builder.WriteString(regexp.QuoteMeta(segment[literalStart:index]))
			builder.WriteString("[^/]")

			literalStart = index + 1
		}
	}

	builder.WriteString(regexp.QuoteMeta(segment[literalStart:]))

	return builder.String()
}
