package e2e

import (
	"encoding/json"
	"math"
	"testing"
)

// TestTreeRollup exercises `tusk task tree --rollup` across the basic
// mixed-status, deletion, custom-workflow, empty-subtree, and JSON-envelope
// stability cases enumerated in the Phase 2 plan.
func TestTreeRollup(test *testing.T) {
	scenarios := []Scenario{
		{
			Name: "rollup_mixed_statuses",
			Steps: []Step{
				// Step 0: root
				{Args: []string{"task", "create", "Rollup root"}},
				// Step 1: child A (stays pending)
				{Args: []string{"task", "create", "Child A pending", "parent=$0.short_id"}},
				// Step 2: child B
				{Args: []string{"task", "create", "Child B active", "parent=$0.short_id"}},
				// Step 3: start B → active
				{Args: []string{"task", "start", "$2.short_id"}},
				// Step 4: child C
				{Args: []string{"task", "create", "Child C completed", "parent=$0.short_id"}},
				// Step 5: start C
				{Args: []string{"task", "start", "$4.short_id"}},
				// Step 6: complete C
				{Args: []string{"task", "done", "$4.short_id"}},
				// Step 7: tree --rollup
				{
					Args: []string{"task", "tree", "--rollup"},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Rollup root")
						assertContains(test, output, "[1/3 done, 33%]")
						assertContains(test, output, "(pending: 1, active: 1, completed: 1)")
						// Children render unchanged: still visible, still no
						// rollup suffix on the leaf lines themselves.
						assertContains(test, output, "Child A pending")
						assertContains(test, output, "Child B active")
						assertContains(test, output, "Child C completed")
					},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						if len(arr) != 1 {
							test.Fatalf("expected 1 root, got %d", len(arr))
						}
						root := arr[0].(map[string]any)
						roll := requireRollup(test, root)
						assertJSONNumber(test, roll, "done", 1)
						assertJSONNumber(test, roll, "total", 3)
						pct, ok := roll["percent"].(float64)
						if !ok {
							test.Fatalf("expected percent to be float64, got %T", roll["percent"])
						}
						if math.Abs(pct-1.0/3.0) > 1e-6 {
							test.Fatalf("expected percent ~= 0.3333, got %v", pct)
						}
						counts := roll["status_counts"].([]any)
						if len(counts) != 3 {
							test.Fatalf("expected 3 status_counts, got %d (%v)", len(counts), counts)
						}
						assertStatusCount(test, counts, "pending", 1)
						assertStatusCount(test, counts, "active", 1)
						assertStatusCount(test, counts, "completed", 1)

						// Every child is a leaf with a zero rollup.
						children := root["children"].([]any)
						if len(children) != 3 {
							test.Fatalf("expected 3 children, got %d", len(children))
						}
						for _, child := range children {
							childRoll := requireRollup(test, child.(map[string]any))
							assertJSONNumber(test, childRoll, "done", 0)
							assertJSONNumber(test, childRoll, "total", 0)
							assertJSONNumber(test, childRoll, "percent", 0)
							sc := childRoll["status_counts"]
							if sc == nil {
								test.Fatalf("expected status_counts to be [], got nil")
							}
							scArr, ok := sc.([]any)
							if !ok || len(scArr) != 0 {
								test.Fatalf("expected status_counts to be empty array, got %#v", sc)
							}
						}
					},
				},
			},
		},
		{
			Name: "rollup_with_deletion",
			Steps: []Step{
				// Step 0: root
				{Args: []string{"task", "create", "Del rollup root"}},
				// Step 1: child A (will be deleted)
				{Args: []string{"task", "create", "Child A toDelete", "parent=$0.short_id"}},
				// Step 2: child B
				{Args: []string{"task", "create", "Child B keep active", "parent=$0.short_id"}},
				// Step 3: start B
				{Args: []string{"task", "start", "$2.short_id"}},
				// Step 4: child C
				{Args: []string{"task", "create", "Child C keep done", "parent=$0.short_id"}},
				// Step 5: start C
				{Args: []string{"task", "start", "$4.short_id"}},
				// Step 6: complete C
				{Args: []string{"task", "done", "$4.short_id"}},
				// Step 7: delete A
				{Args: []string{"task", "delete", "$1.short_id"}},
				// Step 8: tree --rollup (no --all): deleted child must NOT
				// render in text; rollup excludes it from totals.
				{
					Args: []string{"task", "tree", "--rollup"},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Del rollup root")
						assertContains(test, output, "[1/2 done, 50%]")
						assertContains(test, output, "Child B keep active")
						assertContains(test, output, "Child C keep done")
						assertNotContains(test, output, "Child A toDelete")
					},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						if len(arr) != 1 {
							test.Fatalf("expected 1 root, got %d", len(arr))
						}
						root := arr[0].(map[string]any)
						roll := requireRollup(test, root)
						assertJSONNumber(test, roll, "done", 1)
						assertJSONNumber(test, roll, "total", 2)
						// Deleted child filtered out of children too.
						children := root["children"].([]any)
						if len(children) != 2 {
							test.Fatalf("expected 2 visible children (deleted hidden), got %d", len(children))
						}
						for _, child := range children {
							title := child.(map[string]any)["title"]
							if title == "Child A toDelete" {
								test.Fatalf("deleted child rendered without --all")
							}
						}
					},
				},
				// Step 9: tree --rollup --all: deleted child renders, but
				// rollup numbers stay the same (delete-role excluded).
				{
					Args: []string{"task", "tree", "--rollup", "--all"},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "[1/2 done, 50%]")
						assertContains(test, output, "Child A toDelete")
						assertContains(test, output, "Child B keep active")
						assertContains(test, output, "Child C keep done")
					},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						root := arr[0].(map[string]any)
						roll := requireRollup(test, root)
						assertJSONNumber(test, roll, "done", 1)
						assertJSONNumber(test, roll, "total", 2)
						children := root["children"].([]any)
						if len(children) != 3 {
							test.Fatalf("expected 3 children with --all, got %d", len(children))
						}
					},
				},
			},
		},
		{
			Name: "rollup_empty_subtree",
			Steps: []Step{
				// Step 0: a leaf (no children)
				{Args: []string{"task", "create", "Lonely leaf"}},
				// Step 1: tree --rollup <leaf>: no rollup suffix in text;
				// JSON leaf still has zero rollup.
				{
					Args: []string{"task", "tree", "--rollup", "$0.short_id"},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Lonely leaf")
						assertNotContains(test, output, "done,")
					},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						if len(arr) != 1 {
							test.Fatalf("expected 1 root, got %d", len(arr))
						}
						root := arr[0].(map[string]any)
						roll := requireRollup(test, root)
						assertJSONNumber(test, roll, "done", 0)
						assertJSONNumber(test, roll, "total", 0)
						assertJSONNumber(test, roll, "percent", 0)
						sc, ok := roll["status_counts"].([]any)
						if !ok || len(sc) != 0 {
							test.Fatalf("expected empty status_counts array, got %#v", roll["status_counts"])
						}
					},
				},
			},
		},
		{
			Name: "tree_no_rollup_json_envelope_stable",
			Steps: []Step{
				{Args: []string{"task", "create", "Envelope root"}},
				{Args: []string{"task", "create", "Envelope child", "parent=$0.short_id"}},
				{
					Args: []string{"task", "tree"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						arr := jsonArray(test, parsed)
						if len(arr) != 1 {
							test.Fatalf("expected 1 root, got %d", len(arr))
						}
						walkAssertNoRollup(test, arr)
					},
				},
			},
		},
	}
	runScenarios(test, binPath, scenarios)
}

// TestTreeRollupCustomWorkflow exercises the rollup against a custom workflow
// where the `done` role lives on `shipped`, validating that AggregateRollup
// honors the per-task workflow rather than hardcoding kanban status names.
// Uses the same setup pattern as TestCustomWorkflowTaskLifecycle: seed a
// workflow via the CLI, rebind the default project, then create tasks.
func TestTreeRollupCustomWorkflow(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	combos := combinations([]string{"flag", "env"}, []string{"text", "json"})
	for _, combo := range combos {
		dbMode, format := combo[0], combo[1]
		name := "rollup_custom_workflow/" + dbMode + "/" + format
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			env := newEnv(test, binPath, dbMode, format)

			result := env.Run("workflow", "create", "scrum",
				"status=backlog(initial)",
				"status=in_progress(start,highlight)",
				"status=shipped(terminal,done,dim)",
				"status=wontfix(terminal,delete,dim)",
				"transition=backlog:in_progress,in_progress:shipped,in_progress:wontfix,backlog:wontfix",
			)
			if result.Err != nil {
				test.Fatalf("workflow create scrum: %v\nstderr: %s", result.Err, result.Stderr)
			}
			result = env.Run("project", "modify", "default", "workflow=scrum")
			if result.Err != nil {
				test.Fatalf("project modify default: %v\nstderr: %s", result.Err, result.Stderr)
			}

			// 2: root
			result = env.Run("task", "create", "Custom rollup root")
			if result.Err != nil {
				test.Fatalf("create root: %v\nstderr: %s", result.Err, result.Stderr)
			}
			rootShortID := extractShortID(test, result.Stdout)

			// 3: child A
			result = env.Run("task", "create", "Custom child A", "parent="+rootShortID)
			if result.Err != nil {
				test.Fatalf("create A: %v\nstderr: %s", result.Err, result.Stderr)
			}
			aShortID := extractShortID(test, result.Stdout)

			// 4: child B
			result = env.Run("task", "create", "Custom child B", "parent="+rootShortID)
			if result.Err != nil {
				test.Fatalf("create B: %v\nstderr: %s", result.Err, result.Stderr)
			}
			bShortID := extractShortID(test, result.Stdout)

			// 5: start A → in_progress
			result = env.Run("task", "start", aShortID)
			if result.Err != nil {
				test.Fatalf("start A: %v\nstderr: %s", result.Err, result.Stderr)
			}

			// 6: start B → in_progress
			result = env.Run("task", "start", bShortID)
			if result.Err != nil {
				test.Fatalf("start B: %v\nstderr: %s", result.Err, result.Stderr)
			}

			// 7: done B → shipped (done role)
			result = env.Run("task", "done", bShortID)
			if result.Err != nil {
				test.Fatalf("done B: %v\nstderr: %s", result.Err, result.Stderr)
			}

			// 8: tree --rollup. Verify rollup honors per-task workflow:
			// shipped counts as done, breakdown lists scrum statuses in
			// workflow order (backlog, in_progress, shipped — wontfix
			// excluded by delete role).
			result = env.Run("task", "tree", "--rollup")
			if result.Err != nil {
				test.Fatalf("tree --rollup: %v\nstderr: %s", result.Err, result.Stderr)
			}
			if format == "text" {
				assertContains(test, result.Stdout, "[1/2 done, 50%]")
				assertContains(test, result.Stdout, "shipped: 1")
				assertContains(test, result.Stdout, "in_progress: 1")
				assertContains(test, result.Stdout, "backlog: 0")
			} else {
				var parsed []any
				if err := json.Unmarshal([]byte(result.Stdout), &parsed); err != nil {
					test.Fatalf("parse tree json: %v\nraw:\n%s", err, result.Stdout)
				}
				if len(parsed) == 0 {
					test.Fatalf("expected at least 1 root, got empty array")
				}
				root := parsed[0].(map[string]any)
				roll := requireRollup(test, root)
				assertJSONNumber(test, roll, "done", 1)
				assertJSONNumber(test, roll, "total", 2)
				counts := roll["status_counts"].([]any)
				assertStatusCount(test, counts, "in_progress", 1)
				assertStatusCount(test, counts, "shipped", 1)
				assertStatusCount(test, counts, "backlog", 0)
			}
		})
	}
}

// requireRollup returns the rollup map from a tree-node JSON object or fails.
func requireRollup(test *testing.T, node map[string]any) map[string]any {
	test.Helper()
	v, ok := node["rollup"]
	if !ok || v == nil {
		test.Fatalf("expected rollup field on node, got %#v", node)
	}
	mapped, ok := v.(map[string]any)
	if !ok {
		test.Fatalf("expected rollup to be object, got %T", v)
	}
	return mapped
}

// assertJSONNumber compares a numeric field in a JSON-decoded map, tolerating
// JSON-decoded floats (encoding/json always returns float64 for numbers).
func assertJSONNumber(test *testing.T, m map[string]any, key string, want float64) {
	test.Helper()
	v, ok := m[key]
	if !ok {
		test.Fatalf("missing key %q in %#v", key, m)
	}
	got, ok := v.(float64)
	if !ok {
		test.Fatalf("expected %q to be number, got %T", key, v)
	}
	if got != want {
		test.Fatalf("expected %q == %v, got %v", key, want, got)
	}
}

// assertStatusCount verifies that status_counts contains an entry with the
// given name and count. Order is not asserted here — workflow-order is
// validated implicitly by the seeded breakdown ordering.
func assertStatusCount(test *testing.T, counts []any, name string, want float64) {
	test.Helper()
	for _, entry := range counts {
		mapped := entry.(map[string]any)
		if mapped["name"] == name {
			got, ok := mapped["count"].(float64)
			if !ok {
				test.Fatalf("status %q: expected count to be number, got %T", name, mapped["count"])
			}
			if got != want {
				test.Fatalf("status %q: expected count %v, got %v", name, want, got)
			}
			return
		}
	}
	test.Fatalf("status %q not found in counts: %#v", name, counts)
}

// walkAssertNoRollup recursively checks that no node carries a `rollup` key.
// Guards the JSON envelope shape stability promise: `tusk task tree` (no
// `--rollup`) emits the same JSON it did pre-Phase-2.
func walkAssertNoRollup(test *testing.T, nodes []any) {
	test.Helper()
	for _, node := range nodes {
		mapped := node.(map[string]any)
		if _, present := mapped["rollup"]; present {
			test.Fatalf("rollup key leaked into non-rollup tree JSON: %#v", mapped)
		}
		if children, ok := mapped["children"].([]any); ok {
			walkAssertNoRollup(test, children)
		}
	}
}

// extractShortID pulls the first short ID out of a mutation-result line
// (e.g. "Created task abcdef12") or out of a JSON object's `short_id`
// field. Used by the custom-workflow scenario which runs outside the
// $N.short_id reference machinery (it builds env.Run calls directly).
func extractShortID(test *testing.T, stdout string) string {
	test.Helper()
	if matched := shortIDPattern.FindStringSubmatch(stdout); len(matched) == 2 {
		return matched[1]
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(stdout), &parsed); err == nil {
		if v, ok := parsed["short_id"].(string); ok && v != "" {
			return v
		}
	}
	test.Fatalf("could not extract short_id from output:\n%s", stdout)
	return ""
}
