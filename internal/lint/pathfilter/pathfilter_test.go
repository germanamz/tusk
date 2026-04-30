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
		{"github.com/germanamz/tusk/repository", true},
		{"github.com/germanamz/tusk/sqlite", true},
		{"github.com/germanamz/tusk/cmd", true},
		{"github.com/germanamz/tusk/tests/e2e", true},
		{"github.com/germanamz/tusk/config", true},
		// In-scope: root package (client.go lives here).
		{"github.com/germanamz/tusk", true},
		// In-scope: sub-packages of excluded packages.
		{"github.com/germanamz/tusk/config/subpkg", true},

		// In-scope: external test packages (Go's `package foo_test`
		// convention). pkgPath ends with `_test`; trimmed before match.
		{"github.com/germanamz/tusk/sqlite_test", true},
		{"github.com/germanamz/tusk/repository_test", true},

		// Out-of-scope: service/, internal/tui/, internal/mcp/, internal/portability/,
		// filter/, domain/, syntax/ exclusions were removed; lint now covers them.
		{"github.com/germanamz/tusk/service", false},
		{"github.com/germanamz/tusk/service/foo", false},
		{"github.com/germanamz/tusk/service_test", false},
		{"github.com/germanamz/tusk/internal/tui", false},
		{"github.com/germanamz/tusk/internal/tui/render", false},
		{"github.com/germanamz/tusk/internal/tui_test", false},
		{"github.com/germanamz/tusk/internal/mcp", false},
		{"github.com/germanamz/tusk/internal/mcp_test", false},
		{"github.com/germanamz/tusk/internal/portability", false},
		{"github.com/germanamz/tusk/internal/portability_test", false},
		{"github.com/germanamz/tusk/filter", false},
		{"github.com/germanamz/tusk/filter/sub", false},
		{"github.com/germanamz/tusk/filter_test", false},
		{"github.com/germanamz/tusk/domain", false},
		{"github.com/germanamz/tusk/domain_test", false},
		{"github.com/germanamz/tusk/syntax", false},
		{"github.com/germanamz/tusk/syntax_test", false},

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
