package e2e

import (
	"strings"
	"testing"
)

// TestSummaryCLI exercises `tusk task summary` across single-id, filter,
// roots, and error scenarios. The harness runs each scenario in 4
// permutations (DB-config × output-format), so each is written once.
func TestSummaryCLI(test *testing.T) {
	scenarios := []Scenario{
		{
			Name: "single_leaf_zero_rollup",
			Steps: []Step{
				{Args: []string{"task", "create", "Lonely leaf"}},
				{
					Args: []string{"task", "summary", "$0.short_id"},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Lonely leaf")
						assertContains(test, output, "0/0 done, –%")
						// Single mode never prints a TOTALS section.
						assertNotContains(test, output, "TOTALS")
					},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						assertEqual(test, mapped["mode"], "single")
						blocks := mapped["blocks"].([]any)
						if len(blocks) != 1 {
							test.Fatalf("expected 1 block, got %d", len(blocks))
						}
						if _, ok := mapped["totals"]; ok {
							test.Fatalf("single mode must omit totals; got: %v", mapped["totals"])
						}
						roll := blocks[0].(map[string]any)["rollup"].(map[string]any)
						assertEqual(test, roll["total"], float64(0))
					},
				},
			},
		},
		{
			Name: "single_branch_with_descendants",
			Steps: []Step{
				{Args: []string{"task", "create", "Root"}},
				{Args: []string{"task", "create", "Child A", "parent=$0.short_id"}},
				{Args: []string{"task", "create", "Child B", "parent=$0.short_id"}},
				// Drive Child B all the way to completed via the workflow.
				{Args: []string{"task", "start", "$2.short_id"}},
				{Args: []string{"task", "done", "$2.short_id"}},
				{
					Args: []string{"task", "summary", "$0.short_id"},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Root")
						assertContains(test, output, "1/2 done, 50%")
						assertContains(test, output, "completed: 1")
						assertNotContains(test, output, "TOTALS")
					},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						assertEqual(test, mapped["mode"], "single")
						blocks := mapped["blocks"].([]any)
						if len(blocks) != 1 {
							test.Fatalf("expected 1 block, got %d", len(blocks))
						}
						roll := blocks[0].(map[string]any)["rollup"].(map[string]any)
						assertEqual(test, roll["done"], float64(1))
						assertEqual(test, roll["total"], float64(2))
					},
				},
			},
		},
		{
			Name: "roots_workspace_wide",
			Steps: []Step{
				// Tree A: root with 2 children, 1 completed.
				{Args: []string{"task", "create", "Root A"}},
				{Args: []string{"task", "create", "A1", "parent=$0.short_id"}},
				{Args: []string{"task", "create", "A2", "parent=$0.short_id"}},
				{Args: []string{"task", "start", "$2.short_id"}},
				{Args: []string{"task", "done", "$2.short_id"}},
				// Tree B: root with 1 child.
				{Args: []string{"task", "create", "Root B"}},
				{Args: []string{"task", "create", "B1", "parent=$5.short_id"}},
				{
					Args: []string{"task", "summary"},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Root A")
						assertContains(test, output, "Root B")
						assertContains(test, output, "TOTALS")
						// 1 done out of 3 total descendants: 1/3 = 33%
						assertContains(test, output, "1/3 done, 33%")
					},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						assertEqual(test, mapped["mode"], "roots")
						blocks := mapped["blocks"].([]any)
						if len(blocks) != 2 {
							test.Fatalf("expected 2 root blocks, got %d", len(blocks))
						}
						totals := mapped["totals"].(map[string]any)
						assertEqual(test, totals["done"], float64(1))
						assertEqual(test, totals["total"], float64(3))
					},
				},
			},
		},
		{
			Name: "filter_level_default_restricts_descendants",
			Steps: []Step{
				{Args: []string{"config", "set", "taxonomy.levels", "milestone:story:task"}},
				// Story tree:
				//   Story 1
				//     Task 1.1
				//     Task 1.2  (completed)
				//   Story 2
				//     Task 2.1
				{Args: []string{"task", "create", "Roadmap", "level=milestone"}},
				{Args: []string{"task", "create", "Story 1", "level=story", "parent=$1.short_id"}},
				{Args: []string{"task", "create", "Story 2", "level=story", "parent=$1.short_id"}},
				{Args: []string{"task", "create", "Task 1.1", "level=task", "parent=$2.short_id"}},
				{Args: []string{"task", "create", "Task 1.2", "level=task", "parent=$2.short_id"}},
				{Args: []string{"task", "create", "Task 2.1", "level=task", "parent=$3.short_id"}},
				// Drive Task 1.2 to completed.
				{Args: []string{"task", "start", "$5.short_id"}},
				{Args: []string{"task", "done", "$5.short_id"}},
				{
					// Default mode: filter is level=story → blocks are stories,
					// AND descendants are restricted to level=story too. Stories
					// have no story-level descendants, so each rollup is empty.
					Args: []string{"task", "summary", "level=story"},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Story 1")
						assertContains(test, output, "Story 2")
						// Both rollups should be 0/0 because no descendant is a story.
						count := strings.Count(output, "0/0 done, –%")
						if count < 2 {
							test.Fatalf("expected at least 2 zero rollups (one per story block), got %d in:\n%s", count, output)
						}
					},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						assertEqual(test, mapped["mode"], "filter")
						blocks := mapped["blocks"].([]any)
						if len(blocks) != 2 {
							test.Fatalf("expected 2 story blocks, got %d", len(blocks))
						}
						for _, block := range blocks {
							roll := block.(map[string]any)["rollup"].(map[string]any)
							if roll["total"].(float64) != 0 {
								test.Fatalf("default-mode story rollup must be 0 (filter restricts descendants), got: %v", roll)
							}
						}
						totals := mapped["totals"].(map[string]any)
						assertEqual(test, totals["total"], float64(0))
					},
				},
			},
		},
		{
			Name: "filter_level_full_counts_subtree",
			Steps: []Step{
				{Args: []string{"config", "set", "taxonomy.levels", "milestone:story:task"}},
				{Args: []string{"task", "create", "Roadmap", "level=milestone"}},
				{Args: []string{"task", "create", "Story 1", "level=story", "parent=$1.short_id"}},
				{Args: []string{"task", "create", "Task 1.1", "level=task", "parent=$2.short_id"}},
				{Args: []string{"task", "create", "Task 1.2", "level=task", "parent=$2.short_id"}},
				// Drive Task 1.2 → completed.
				{Args: []string{"task", "start", "$4.short_id"}},
				{Args: []string{"task", "done", "$4.short_id"}},
				{
					// --full: filter only selects blocks; full subtree counts.
					Args: []string{"task", "summary", "--full", "level=story"},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Story 1")
						// Story 1 has 2 task-level descendants, 1 completed.
						assertContains(test, output, "1/2 done, 50%")
					},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						blocks := mapped["blocks"].([]any)
						if len(blocks) != 1 {
							test.Fatalf("expected 1 story block, got %d", len(blocks))
						}
						roll := blocks[0].(map[string]any)["rollup"].(map[string]any)
						assertEqual(test, roll["done"], float64(1))
						assertEqual(test, roll["total"], float64(2))
					},
				},
			},
		},
		{
			Name: "full_with_short_id_is_usage_error",
			Steps: []Step{
				{Args: []string{"task", "create", "Some task"}},
				{
					Args:    []string{"task", "summary", "--full", "$0.short_id"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertStderrContains(test, result, "single-id mode")
					},
				},
			},
		},
		{
			Name: "full_with_no_args_is_usage_error",
			Steps: []Step{
				{
					Args:    []string{"task", "summary", "--full"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertStderrContains(test, result, "without a filter")
					},
				},
			},
		},
		{
			Name: "filter_no_match_is_empty_envelope",
			Steps: []Step{
				{Args: []string{"task", "create", "Anything"}},
				{
					Args: []string{"task", "summary", "status=nonexistent"},
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						// Command must succeed; renderer prints to stderr in
						// text mode and emits an empty envelope in JSON mode.
						if result.Err != nil {
							test.Fatalf("expected zero exit, got err=%v stderr=%s", result.Err, result.Stderr)
						}
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						// stdout is empty; "No tasks matched." goes to stderr.
						if strings.TrimSpace(output) != "" {
							test.Fatalf("expected empty stdout, got %q", output)
						}
					},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						assertEqual(test, mapped["mode"], "filter")
						blocks := mapped["blocks"].([]any)
						if len(blocks) != 0 {
							test.Fatalf("expected empty blocks, got %d", len(blocks))
						}
						totals := mapped["totals"].(map[string]any)
						assertEqual(test, totals["total"], float64(0))
						statusCounts := totals["status_counts"].([]any)
						if len(statusCounts) != 0 {
							test.Fatalf("expected empty status_counts, got %v", statusCounts)
						}
					},
				},
			},
		},
	}
	runScenarios(test, binPath, scenarios)
}
