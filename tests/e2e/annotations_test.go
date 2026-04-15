package e2e

import "testing"

func TestAnnotations(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "annotate_then_info",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Annotate target"},
				},
				{
					Args: []string{"annotate", "$0.short_id", "This is a note"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						// annotate returns the task object
						m := parsed.(map[string]any)
						assertEqual(t, m["title"], "Annotate target")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Annotated task")
					},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						annotations := m["annotations"].([]any)
						if len(annotations) != 1 {
							t.Fatalf("expected 1 annotation, got %d", len(annotations))
						}
						ann := annotations[0].(map[string]any)
						assertEqual(t, ann["body"], "This is a note")
						if ann["id"] == nil || ann["id"] == "" {
							t.Fatal("expected annotation id to be set")
						}
						if ann["created_at"] == nil || ann["created_at"] == "" {
							t.Fatal("expected annotation created_at to be set")
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Annotations:")
						assertContains(t, output, "This is a note")
					},
				},
			},
		},
		{
			Name: "multiple_annotations",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Multi note task"},
				},
				{
					Args: []string{"annotate", "$0.short_id", "First note"},
				},
				{
					Args: []string{"annotate", "$0.short_id", "Second note"},
				},
				{
					Args: []string{"annotate", "$0.short_id", "Third note"},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						annotations := m["annotations"].([]any)
						if len(annotations) != 3 {
							t.Fatalf("expected 3 annotations, got %d", len(annotations))
						}
						bodies := make([]string, len(annotations))
						for i, a := range annotations {
							bodies[i] = a.(map[string]any)["body"].(string)
						}
						// All three should be present
						found := map[string]bool{}
						for _, b := range bodies {
							found[b] = true
						}
						for _, want := range []string{"First note", "Second note", "Third note"} {
							if !found[want] {
								t.Fatalf("missing annotation %q in %v", want, bodies)
							}
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Annotations:")
						assertContains(t, output, "First note")
						assertContains(t, output, "Second note")
						assertContains(t, output, "Third note")
					},
				},
			},
		},
		{
			Name: "annotate_nonexistent_task",
			Steps: []Step{
				{
					Args:    []string{"annotate", "nonexist", "A note"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "not found")
					},
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}
