package subunit_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/subunit"
)

func TestFirstHeading(test *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{"atx", "# Title\n\nbody\n", "Title"},
		{"setext", "Title\n=====\n\nbody\n", "Title"},
		{"skips deeper levels", "## Sub\n\n# Top\n", "Top"},
		{"skips fenced code", "```\n# not\n```\n\n# Real\n", "Real"},
		{"skips a blockquote", "> # quoted\n\n# Real\n", "Real"},
		{"skips an empty heading", "#\n\n# Real\n", "Real"},
		{"inline markup is flattened", "# The *auth* `RFC`\n", "The auth RFC"},
		{"crlf", "# Windows\r\n\r\nbody\r\n", "Windows"},
		{"none", "Just prose.\n", ""},
	}

	for _, tc := range cases {
		test.Run(tc.name, func(test *testing.T) {
			if got := subunit.FirstHeading([]byte(tc.source)); got != tc.want {
				test.Errorf("FirstHeading = %q, want %q", got, tc.want)
			}
		})
	}
}
