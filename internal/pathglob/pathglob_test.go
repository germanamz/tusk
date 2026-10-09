package pathglob

import (
	"regexp"
	"testing"
)

func TestIsPattern(test *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"docs/product/recording.md", false},
		{"docs/product/*", true},
		{"docs/?.md", true},
		{"**", true},
		{"", false},
		{"notes/a.b:c-d_e", false},
	}

	for _, testCase := range cases {
		if got := IsPattern(testCase.value); got != testCase.want {
			test.Errorf("IsPattern(%q) = %v, want %v", testCase.value, got, testCase.want)
		}
	}
}

func TestToRegexpMatching(test *testing.T) {
	cases := []struct {
		pattern string
		value   string
		want    bool
	}{
		// * stays inside one segment and the match is anchored to the whole value.
		{"docs/product/*", "docs/product/recording.md", true},
		{"docs/product/*", "docs/product/sub/deep.md", false},
		{"docs/product/*", "other/docs/product/x.md", false},
		{"docs/product/*", "docs/product", false},
		{"*.md", "readme.md", true},
		{"*.md", "docs/readme.md", false},
		{"docs/*/index.md", "docs/guide/index.md", true},
		{"docs/*/index.md", "docs/guide/deep/index.md", false},
		{"docs/*/index.md", "docs/index.md", false},
		{"docs/a*b.md", "docs/ab.md", true},
		{"docs/a*b.md", "docs/axyzb.md", true},
		{"docs/a*b.md", "docs/ax/yb.md", false},

		// ? is exactly one character other than /.
		{"docs/?.md", "docs/a.md", true},
		{"docs/?.md", "docs/ab.md", false},
		{"docs/?.md", "docs/.md", false},
		{"docs?a.md", "docs/a.md", false},
		{"notes/caf?.md", "notes/café.md", true},

		// A leading **/ matches zero or more directories, including the root.
		{"**/index.md", "index.md", true},
		{"**/index.md", "docs/index.md", true},
		{"**/index.md", "docs/guide/index.md", true},
		{"**/index.md", "docs/myindex.md", false},

		// A middle /**/ matches zero or more directories.
		{"docs/**/index.md", "docs/index.md", true},
		{"docs/**/index.md", "docs/guide/index.md", true},
		{"docs/**/index.md", "docs/guide/deep/index.md", true},
		{"docs/**/index.md", "other/index.md", false},
		{"docs/**/a*b.md", "docs/x/ab.md", true},
		{"docs/**/a*b.md", "docs/xa/yb.md", false},

		// A trailing /** matches everything inside, but not the directory itself.
		{"docs/product/**", "docs/product/recording.md", true},
		{"docs/product/**", "docs/product/sub/deep.md", true},
		{"docs/product/**", "docs/product", false},
		{"docs/product/**", "docs/productivity/x.md", false},

		// A bare ** matches everything.
		{"**", "readme.md", true},
		{"**", "docs/guide/index.md", true},

		// Repeated ** segments behave like one.
		{"docs/**/**/index.md", "docs/index.md", true},
		{"docs/**/**/index.md", "docs/a/b/index.md", true},

		// ** sharing a segment with other characters acts as a single *.
		{"docs/a**.md", "docs/abc.md", true},
		{"docs/a**.md", "docs/a/b.md", false},

		// Every other character is literal, regex metacharacters included.
		{"docs/v1.0/*", "docs/v1x0/a.md", false},
		{"docs/v1.0/*", "docs/v1.0/a.md", true},
		{"notes/a+b/*", "notes/a+b/x.md", true},
		{"notes/a+b/*", "notes/aab/x.md", false},
		{"notes/(draft)/*", "notes/(draft)/x.md", true},
		{"notes/[draft]/*", "notes/[draft]/x.md", true},
		{"notes/[draft]/*", "notes/d/x.md", false},
		{"notes/^a$|b/*", "notes/^a$|b/x.md", true},
		{"notes/{a}/*", "notes/{a}/x.md", true},
		{`notes/a\b/*`, `notes/a\b/x.md`, true},
	}

	for _, testCase := range cases {
		expression := ToRegexp(testCase.pattern)
		compiled, compileErr := regexp.Compile(expression)

		if compileErr != nil {
			test.Errorf("ToRegexp(%q) = %q does not compile: %v", testCase.pattern, expression, compileErr)

			continue
		}

		if got := compiled.MatchString(testCase.value); got != testCase.want {
			test.Errorf("pattern %q (regex %q) against %q = %v, want %v", testCase.pattern, expression, testCase.value, got, testCase.want)
		}
	}
}

// portableRegexp accepts only the constructs every mainstream regex engine
// (SQLite's Go-backed REGEXP, Postgres ~, MySQL REGEXP_LIKE, DuckDB) reads the
// same way: anchors, the [^/] class with an optional * or + quantifier, the
// ([^/]*/)* directory group, a backslash before a non-alphanumeric character,
// and plain characters that are not metacharacters. A translation that reached
// for RE2-only syntax ((?:, \d, (?s), ...) or an unescaped metacharacter fails.
var portableRegexp = regexp.MustCompile(`^\^(?:\[\^/\][*+]?|\(\[\^/\]\*/\)\*|\\[^A-Za-z0-9]|[^\\.+*?()|\[\]{}^$])*\$$`)

func TestToRegexpStaysPortable(test *testing.T) {
	patterns := []string{
		"docs/product/*",
		"docs/?.md",
		"**",
		"**/index.md",
		"docs/**/index.md",
		"docs/product/**",
		"docs/a**b.md",
		"notes/^a$|b+(c)[d]{e}.f/*",
		`notes/a\b/*`,
		"notes/café/*",
	}

	for _, pattern := range patterns {
		if expression := ToRegexp(pattern); !portableRegexp.MatchString(expression) {
			test.Errorf("ToRegexp(%q) = %q uses syntax outside the portable subset", pattern, expression)
		}
	}
}
