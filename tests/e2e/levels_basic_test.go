package e2e

import (
	"strings"
	"testing"
)

// TestLevelsBasic seeds a workspace taxonomy, creates tasks at each rank,
// and verifies that the level surfaces in `tusk task get` and `tusk task tree`.
func TestLevelsBasic(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "levels_render_on_get_and_tree",
			Steps: []Step{
				{
					Args: []string{"config", "set", "taxonomy.levels", "milestone:story:(task,spike)"},
				},
				{
					Args: []string{"task", "create", "Roadmap Q1", "level=milestone"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["level"], "milestone")
					},
				},
				{
					Args: []string{"task", "create", "Login story", "level=story", "parent=$1.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["level"], "story")
					},
				},
				{
					Args: []string{"task", "create", "Spike", "level=spike", "parent=$2.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["level"], "spike")
					},
				},
				{
					Args: []string{"task", "get", "$2.short_id"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						if !strings.Contains(output, "Level:") {
							t.Fatalf("expected Level: line, got:\n%s", output)
						}
						if !strings.Contains(output, "story") {
							t.Fatalf("expected 'story' level, got:\n%s", output)
						}
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["level"], "story")
					},
				},
				{
					Args: []string{"task", "tree"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						for _, level := range []string{"[milestone]", "[story]", "[spike]"} {
							if !strings.Contains(output, level) {
								t.Fatalf("expected %q in tree output:\n%s", level, output)
							}
						}
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}
