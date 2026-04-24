package e2e

import (
	"testing"
)

// assertOrderField asserts that a parsed task JSON's "order" field equals want.
// The harness emits `order` only when non-nil, so a missing field is a failure here.
func assertOrderField(t *testing.T, parsed any, want float64) {
	t.Helper()
	m, ok := parsed.(map[string]any)
	if !ok {
		t.Fatalf("assertOrderField: expected map, got %T", parsed)
	}
	got, present := m["order"]
	if !present {
		t.Fatalf("assertOrderField: order field missing in %v", m)
	}
	f, ok := got.(float64)
	if !ok {
		t.Fatalf("assertOrderField: expected float64, got %T (%v)", got, got)
	}
	if f != want {
		t.Fatalf("assertOrderField: got %v, want %v", f, want)
	}
}

// assertOrderAbsent asserts the "order" field is absent or explicitly nil.
func assertOrderAbsent(t *testing.T, parsed any) {
	t.Helper()
	m, ok := parsed.(map[string]any)
	if !ok {
		t.Fatalf("assertOrderAbsent: expected map, got %T", parsed)
	}
	if v, present := m["order"]; present && v != nil {
		t.Fatalf("assertOrderAbsent: expected absent/nil, got %v", v)
	}
}

func TestSiblingOrdering(t *testing.T) {
	scenarios := []Scenario{
		{
			// Phase 1: NextOrder auto-assigns dense integers to new siblings.
			Name: "auto_order_on_create",
			Steps: []Step{
				{Args: []string{"task", "create", "Parent"}},                        // 0
				{Args: []string{"task", "create", "Child A", "parent=$0.short_id"}}, // 1
				{Args: []string{"task", "create", "Child B", "parent=$0.short_id"}}, // 2
				{Args: []string{"task", "create", "Child C", "parent=$0.short_id"}}, // 3
				{
					Args: []string{"task", "get", "$1.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						assertOrderField(t, parsed, 1.0)
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Order:")
					},
				},
				{
					Args: []string{"task", "get", "$2.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						assertOrderField(t, parsed, 2.0)
					},
				},
				{
					Args: []string{"task", "get", "$3.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						assertOrderField(t, parsed, 3.0)
					},
				},
			},
		},
		{
			// Phase 1: tree view defaults to (order ASC NULLS LAST, created_at ASC).
			Name: "tree_default_sort_is_order",
			Steps: []Step{
				{Args: []string{"task", "create", "Parent"}},                        // 0
				{Args: []string{"task", "create", "Child A", "parent=$0.short_id"}}, // 1
				{Args: []string{"task", "create", "Child B", "parent=$0.short_id"}}, // 2
				{Args: []string{"task", "create", "Child C", "parent=$0.short_id"}}, // 3
				{
					Args: []string{"task", "tree", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 root, got %d", len(arr))
						}
						root := arr[0].(map[string]any)
						children := root["children"].([]any)
						if len(children) != 3 {
							t.Fatalf("expected 3 children, got %d", len(children))
						}
						titles := []string{
							children[0].(map[string]any)["title"].(string),
							children[1].(map[string]any)["title"].(string),
							children[2].(map[string]any)["title"].(string),
						}
						want := []string{"Child A", "Child B", "Child C"}
						for i := range want {
							if titles[i] != want[i] {
								t.Fatalf("tree position %d: got %q want %q", i, titles[i], want[i])
							}
						}
					},
				},
			},
		},
		{
			// Phase 2/3: `move --before` reorders within a sibling group.
			Name: "move_before_reorders_siblings",
			Steps: []Step{
				{Args: []string{"task", "create", "Parent"}},                        // 0
				{Args: []string{"task", "create", "Child A", "parent=$0.short_id"}}, // 1
				{Args: []string{"task", "create", "Child B", "parent=$0.short_id"}}, // 2
				{Args: []string{"task", "create", "Child C", "parent=$0.short_id"}}, // 3
				// Move C before B → expected sibling sequence: A, C, B.
				{Args: []string{"task", "move", "$3.short_id", "--before", "$2.short_id"}}, // 4
				{
					Args: []string{"task", "tree", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						root := arr[0].(map[string]any)
						children := root["children"].([]any)
						if len(children) != 3 {
							t.Fatalf("expected 3 children, got %d", len(children))
						}
						got := []string{
							children[0].(map[string]any)["title"].(string),
							children[1].(map[string]any)["title"].(string),
							children[2].(map[string]any)["title"].(string),
						}
						want := []string{"Child A", "Child C", "Child B"}
						for i := range want {
							if got[i] != want[i] {
								t.Fatalf("post-move position %d: got %q want %q", i, got[i], want[i])
							}
						}
					},
				},
				// The moved task's order should now be strictly between A (1.0) and B (2.0).
				{
					Args: []string{"task", "get", "$3.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						v, ok := m["order"].(float64)
						if !ok {
							t.Fatalf("expected numeric order, got %v", m["order"])
						}
						if !(v > 1.0 && v < 2.0) {
							t.Fatalf("expected midpoint in (1.0, 2.0), got %v", v)
						}
					},
				},
			},
		},
		{
			// Phase 2/3: `move --first` jumps the task to the head of the group.
			Name: "move_first_jumps_to_head",
			Steps: []Step{
				{Args: []string{"task", "create", "Parent"}},                        // 0
				{Args: []string{"task", "create", "Child A", "parent=$0.short_id"}}, // 1
				{Args: []string{"task", "create", "Child B", "parent=$0.short_id"}}, // 2
				{Args: []string{"task", "create", "Child C", "parent=$0.short_id"}}, // 3
				{Args: []string{"task", "move", "$2.short_id", "--first"}},          // 4
				{
					Args: []string{"task", "tree", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						root := arr[0].(map[string]any)
						children := root["children"].([]any)
						titles := []string{
							children[0].(map[string]any)["title"].(string),
							children[1].(map[string]any)["title"].(string),
							children[2].(map[string]any)["title"].(string),
						}
						want := []string{"Child B", "Child A", "Child C"}
						for i := range want {
							if titles[i] != want[i] {
								t.Fatalf("position %d: got %q want %q", i, titles[i], want[i])
							}
						}
					},
				},
				// Moved task's order should be less than the current head's previous order.
				{
					Args: []string{"task", "get", "$2.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						v, ok := m["order"].(float64)
						if !ok {
							t.Fatalf("expected numeric order, got %v", m["order"])
						}
						if v >= 1.0 {
							t.Fatalf("expected order < 1.0 after --first, got %v", v)
						}
					},
				},
			},
		},
		{
			// Phase 2/3: `move --after <target-in-other-parent>` reparents atomically.
			Name: "move_across_parents_reparents",
			Steps: []Step{
				{Args: []string{"task", "create", "Parent A"}},                        // 0
				{Args: []string{"task", "create", "Parent B"}},                        // 1
				{Args: []string{"task", "create", "Mover", "parent=$0.short_id"}},     // 2
				{Args: []string{"task", "create", "B's first", "parent=$1.short_id"}}, // 3
				// Move Mover --after B's-first → should land under Parent B.
				{Args: []string{"task", "move", "$2.short_id", "--after", "$3.short_id"}}, // 4
				{
					Args: []string{"task", "get", "$2.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						// parent_id should now match Parent B's UUID (fetched via B's first)
						// Simplest check: parent_id is present and non-empty.
						if m["parent_id"] == nil || m["parent_id"] == "" {
							t.Fatal("expected parent_id to be set after cross-parent move")
						}
					},
				},
				{
					Args: []string{"task", "tree", "$1.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						root := arr[0].(map[string]any)
						children := root["children"].([]any)
						if len(children) != 2 {
							t.Fatalf("expected 2 children under Parent B, got %d", len(children))
						}
						// After-move means Mover sits after B's first.
						titles := []string{
							children[0].(map[string]any)["title"].(string),
							children[1].(map[string]any)["title"].(string),
						}
						want := []string{"B's first", "Mover"}
						for i := range want {
							if titles[i] != want[i] {
								t.Fatalf("position %d under B: got %q want %q", i, titles[i], want[i])
							}
						}
					},
				},
			},
		},
		{
			// Phase 1/3: `modify order=` (empty value) clears order to NULL; the
			// sibling sort policy puts NULL orders at the end of the group.
			Name: "modify_order_clear_sinks_to_end",
			Steps: []Step{
				{Args: []string{"task", "create", "Parent"}},                        // 0
				{Args: []string{"task", "create", "Child A", "parent=$0.short_id"}}, // 1
				{Args: []string{"task", "create", "Child B", "parent=$0.short_id"}}, // 2
				{Args: []string{"task", "create", "Child C", "parent=$0.short_id"}}, // 3
				// Clear A's order — it should sink to the tail of the group.
				{Args: []string{"task", "modify", "$1.short_id", "order="}}, // 4
				{
					Args: []string{"task", "get", "$1.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						assertOrderAbsent(t, parsed)
					},
				},
				{
					Args: []string{"task", "tree", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						root := arr[0].(map[string]any)
						children := root["children"].([]any)
						if len(children) != 3 {
							t.Fatalf("expected 3 children, got %d", len(children))
						}
						// NULLS LAST → A is last.
						titles := []string{
							children[0].(map[string]any)["title"].(string),
							children[1].(map[string]any)["title"].(string),
							children[2].(map[string]any)["title"].(string),
						}
						want := []string{"Child B", "Child C", "Child A"}
						for i := range want {
							if titles[i] != want[i] {
								t.Fatalf("position %d: got %q want %q", i, titles[i], want[i])
							}
						}
					},
				},
			},
		},
		{
			// Phase 2/3: `move --resequence <parent>` rewrites a sibling group
			// to dense integers. Seed with orders that all differ from the
			// target dense sequence (1.0, 2.0, 3.0) so every row gets rewritten.
			// Resequence skips rows that already sit on their target integer.
			Name: "resequence_rewrites_dense_integers",
			Steps: []Step{
				{Args: []string{"task", "create", "Parent"}},                                    // 0
				{Args: []string{"task", "create", "First", "parent=$0.short_id", "order=0.5"}},  // 1
				{Args: []string{"task", "create", "Second", "parent=$0.short_id", "order=1.5"}}, // 2
				{Args: []string{"task", "create", "Third", "parent=$0.short_id", "order=2.5"}},  // 3
				{
					Args: []string{"task", "move", "--resequence", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m, ok := parsed.(map[string]any)
						if !ok {
							t.Fatalf("expected resequence response map, got %T", parsed)
						}
						if m["rewritten"] != float64(3) {
							t.Fatalf("expected rewritten=3, got %v", m["rewritten"])
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "resequenced 3 tasks under parent")
					},
				},
				{
					Args: []string{"task", "get", "$1.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						assertOrderField(t, parsed, 1.0)
					},
				},
				{
					Args: []string{"task", "get", "$2.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						assertOrderField(t, parsed, 2.0)
					},
				},
				{
					Args: []string{"task", "get", "$3.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						assertOrderField(t, parsed, 3.0)
					},
				},
			},
		},
		{
			// Phase 2/3: moving a parent under one of its descendants is rejected.
			Name: "move_rejects_cycle",
			Steps: []Step{
				{Args: []string{"task", "create", "Parent"}},                           // 0
				{Args: []string{"task", "create", "Child", "parent=$0.short_id"}},      // 1
				{Args: []string{"task", "create", "Grandchild", "parent=$1.short_id"}}, // 2
				// Try to move Parent --after Grandchild (would create cycle).
				{
					Args:    []string{"task", "move", "$0.short_id", "--after", "$2.short_id"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "cycle")
					},
				},
			},
		},
		{
			// Phase 2/3: moving between neighbors whose gap is below float64
			// resolution surfaces ErrOrderGapExhausted with the parent hint.
			Name: "move_rejects_on_float_underflow",
			Steps: []Step{
				{Args: []string{"task", "create", "Parent"}},                                                 // 0
				{Args: []string{"task", "create", "Low", "parent=$0.short_id", "order=1.0"}},                 // 1
				{Args: []string{"task", "create", "High", "parent=$0.short_id", "order=1.0000000000000002"}}, // 2
				{Args: []string{"task", "create", "Incoming", "parent=$0.short_id"}},                         // 3
				{
					Args:    []string{"task", "move", "$3.short_id", "--before", "$2.short_id"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "no float64 midpoint")
						assertStderrContains(t, r, "parent")
					},
				},
			},
		},
		{
			// Phase 3: filter grammar supports `order=<a>..<b>` inclusive range.
			Name: "list_filter_by_order_range",
			Steps: []Step{
				{Args: []string{"task", "create", "Parent"}},                              // 0
				{Args: []string{"task", "create", "K1", "parent=$0.short_id", "order=1"}}, // 1
				{Args: []string{"task", "create", "K2", "parent=$0.short_id", "order=2"}}, // 2
				{Args: []string{"task", "create", "K3", "parent=$0.short_id", "order=3"}}, // 3
				{Args: []string{"task", "create", "K4", "parent=$0.short_id", "order=4"}}, // 4
				{Args: []string{"task", "create", "K5", "parent=$0.short_id", "order=5"}}, // 5
				{
					Args: []string{"task", "list", "parent=$0.short_id", "order=2..4"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 3 {
							t.Fatalf("expected 3 tasks in range, got %d", len(arr))
						}
						titles := map[string]bool{}
						for _, x := range arr {
							titles[x.(map[string]any)["title"].(string)] = true
						}
						for _, want := range []string{"K2", "K3", "K4"} {
							if !titles[want] {
								t.Fatalf("expected %s in filtered set, got %v", want, titles)
							}
						}
						for _, unwanted := range []string{"K1", "K5"} {
							if titles[unwanted] {
								t.Fatalf("did not expect %s in filtered set, got %v", unwanted, titles)
							}
						}
					},
				},
			},
		},
		{
			// Phase 3: the `--sort` override is wired on `tusk task tree` and
			// works in both full-tree and subtree form. Subtree mode also
			// routes through TaskService.List so Urgency is populated on
			// descendants — without that, --sort urgency would be a no-op.
			Name: "tree_sort_urgency_override",
			Steps: []Step{
				{Args: []string{"task", "create", "Parent"}},                                        // 0
				{Args: []string{"task", "create", "Low prio", "parent=$0.short_id", "priority=1"}},  // 1
				{Args: []string{"task", "create", "Mid prio", "parent=$0.short_id", "priority=2"}},  // 2
				{Args: []string{"task", "create", "High prio", "parent=$0.short_id", "priority=3"}}, // 3
				{
					// Default sort is the repo's order-based sibling sort:
					// creation order wins, so Low, Mid, High.
					Args: []string{"task", "tree", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("subtree: expected 1 root, got %d", len(arr))
						}
						root := arr[0].(map[string]any)
						children := root["children"].([]any)
						if len(children) != 3 {
							t.Fatalf("default subtree: expected 3 children, got %d", len(children))
						}
						titles := []string{
							children[0].(map[string]any)["title"].(string),
							children[1].(map[string]any)["title"].(string),
							children[2].(map[string]any)["title"].(string),
						}
						want := []string{"Low prio", "Mid prio", "High prio"}
						for i := range want {
							if titles[i] != want[i] {
								t.Fatalf("default sort position %d: got %q want %q", i, titles[i], want[i])
							}
						}
					},
				},
				{
					// --sort urgency reorders: priority_weight is positive so
					// higher priority yields higher urgency; High should lead.
					Args: []string{"task", "tree", "$0.short_id", "--sort", "urgency"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("urgency subtree: expected 1 root, got %d", len(arr))
						}
						root := arr[0].(map[string]any)
						children := root["children"].([]any)
						if len(children) != 3 {
							t.Fatalf("urgency subtree: expected 3 children, got %d", len(children))
						}
						firstTitle := children[0].(map[string]any)["title"].(string)
						if firstTitle != "High prio" {
							titles := []string{
								firstTitle,
								children[1].(map[string]any)["title"].(string),
								children[2].(map[string]any)["title"].(string),
							}
							t.Fatalf("urgency sort: expected High prio first, got %v", titles)
						}
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}
