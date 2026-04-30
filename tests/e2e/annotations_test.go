package e2e

import "testing"

func TestAnnotations(test *testing.T) {
	scenarios := []Scenario{
		{
			Name: "annotate_then_info",
			Steps: []Step{
				{
					Args: []string{"task", "create", "Annotate target"},
				},
				{
					Args: []string{"task", "annotate", "$0.short_id", "This is a note"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						// annotate returns the task object
						mapped := parsed.(map[string]any)
						assertEqual(test, mapped["title"], "Annotate target")
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Annotated task")
					},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						annotations := mapped["annotations"].([]any)
						if len(annotations) != 1 {
							test.Fatalf("expected 1 annotation, got %d", len(annotations))
						}
						annotation := annotations[0].(map[string]any)
						assertEqual(test, annotation["body"], "This is a note")
						if annotation["id"] == nil || annotation["id"] == "" {
							test.Fatal("expected annotation id to be set")
						}
						if annotation["created_at"] == nil || annotation["created_at"] == "" {
							test.Fatal("expected annotation created_at to be set")
						}
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Annotations:")
						assertContains(test, output, "This is a note")
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
					Args: []string{"task", "annotate", "$0.short_id", "First note"},
				},
				{
					Args: []string{"task", "annotate", "$0.short_id", "Second note"},
				},
				{
					Args: []string{"task", "annotate", "$0.short_id", "Third note"},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					AssertJSON: func(test *testing.T, parsed any) {
						test.Helper()
						mapped := parsed.(map[string]any)
						annotations := mapped["annotations"].([]any)
						if len(annotations) != 3 {
							test.Fatalf("expected 3 annotations, got %d", len(annotations))
						}
						bodies := make([]string, len(annotations))
						for index, annotation := range annotations {
							bodies[index] = annotation.(map[string]any)["body"].(string)
						}
						// All three should be present
						found := map[string]bool{}
						for _, body := range bodies {
							found[body] = true
						}
						for _, want := range []string{"First note", "Second note", "Third note"} {
							if !found[want] {
								test.Fatalf("missing annotation %q in %v", want, bodies)
							}
						}
					},
					AssertText: func(test *testing.T, output string) {
						test.Helper()
						assertContains(test, output, "Annotations:")
						assertContains(test, output, "First note")
						assertContains(test, output, "Second note")
						assertContains(test, output, "Third note")
					},
				},
			},
		},
		{
			Name: "annotate_nonexistent_task",
			Steps: []Step{
				{
					Args:    []string{"task", "annotate", "nonexist", "A note"},
					WantErr: true,
					Assert: func(test *testing.T, result Result) {
						test.Helper()
						assertStderrContains(test, result, "not found")
					},
				},
			},
		},
	}

	runScenarios(test, binPath, scenarios)
}
