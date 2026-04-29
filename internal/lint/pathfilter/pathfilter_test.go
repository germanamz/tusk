package pathfilter_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/lint/pathfilter"
)

func TestExcluded(test *testing.T) {
	cases := []struct {
		pkgPath  string
		excluded bool
	}{
		// In-scope: exact package matches.
		{"github.com/germanamz/tusk/service", true},
		{"github.com/germanamz/tusk/internal/tui", true},
		{"github.com/germanamz/tusk/internal/mcp", true},
		{"github.com/germanamz/tusk/internal/portability", true},
		{"github.com/germanamz/tusk/filter", true},
		{"github.com/germanamz/tusk/domain", true},
		{"github.com/germanamz/tusk/syntax", true},
		{"github.com/germanamz/tusk/repository", true},
		{"github.com/germanamz/tusk/sqlite", true},
		{"github.com/germanamz/tusk/cmd", true},
		{"github.com/germanamz/tusk/tests/e2e", true},
		{"github.com/germanamz/tusk/config", true},
		// In-scope: root package (client.go lives here).
		{"github.com/germanamz/tusk", true},
		// In-scope: sub-packages of excluded packages.
		{"github.com/germanamz/tusk/service/foo", true},
		{"github.com/germanamz/tusk/internal/tui/render", true},
		{"github.com/germanamz/tusk/config/subpkg", true},

		// Out-of-scope: lint packages themselves are not excluded.
		{"github.com/germanamz/tusk/internal/lint/blankline", false},
		{"github.com/germanamz/tusk/internal/lint/namederr", false},
		{"github.com/germanamz/tusk/internal/lint/pathfilter", false},

		// Out-of-scope: different module.
		{"github.com/germanamz/other/service", false},

		// Out-of-scope: analysistest testdata packages use single-letter paths.
		{"a", false},

		// Boundary: "service2/" must NOT match the service/ regex.
		{"github.com/germanamz/tusk/service2", false},
		{"github.com/germanamz/tusk/service2/sub", false},

		// Boundary: partial prefix without the module path.
		{"service", false},
		{"tusk/service", false},
	}

	for _, tc := range cases {
		got := pathfilter.Excluded(tc.pkgPath)
		if got != tc.excluded {
			test.Errorf("Excluded(%q) = %v, want %v", tc.pkgPath, got, tc.excluded)
		}
	}
}
