package tui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/germanamz/tusk/domain"
)

func TestRunGet_RendersLevelWhenTaxonomyActive(t *testing.T) {
	app, tasks, _ := testAppWithTaxonomy(t, [][]string{{"milestone"}, {"story"}})
	level := "milestone"
	task := seedViolatingTask(t, tasks, "77777777", "with-level", &level)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "get", task.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("task get: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Level:") {
		t.Fatalf("expected Level: in output, got:\n%s", out)
	}
	if !strings.Contains(out, "milestone") {
		t.Fatalf("expected milestone value in output, got:\n%s", out)
	}
}

func TestRunGet_OmitsLevelWhenNoTaxonomy(t *testing.T) {
	app, taskSvc := testApp(t)

	task := &domain.Task{Title: "no-taxonomy"}
	if err := taskSvc.Create(t.Context(), task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "get", task.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("task get: %v", err)
	}
	if strings.Contains(buf.String(), "Level:") {
		t.Fatalf("expected no Level: line when taxonomy empty, got:\n%s", buf.String())
	}
}

func TestRunGet_LevelDashWhenUnset(t *testing.T) {
	app, tasks, _ := testAppWithTaxonomy(t, [][]string{{"milestone"}})
	// Seeded task has no level.
	task := seedViolatingTask(t, tasks, "88888888", "unset-level", nil)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "get", task.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("task get: %v", err)
	}
	if !strings.Contains(buf.String(), "Level:") || !strings.Contains(buf.String(), "—") {
		t.Fatalf("expected Level: — for unset level, got:\n%s", buf.String())
	}
}

func TestRunTree_AppendsLevelSuffixWhenTaxonomyActive(t *testing.T) {
	app, tasks, _ := testAppWithTaxonomy(t, [][]string{{"milestone"}})
	level := "milestone"
	seedViolatingTask(t, tasks, "aa000000", "tree-task", &level)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "tree"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("task tree: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "[milestone]") {
		t.Fatalf("expected [milestone] suffix in tree output, got:\n%s", out)
	}
}

func TestRunTree_NoLevelSuffixWhenTaxonomyEmpty(t *testing.T) {
	app, taskSvc := testApp(t)

	task := &domain.Task{Title: "no-tax tree"}
	if err := taskSvc.Create(t.Context(), task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "tree"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("task tree: %v", err)
	}
	if strings.Contains(buf.String(), "[—]") {
		t.Fatalf("expected no level suffix when taxonomy empty, got:\n%s", buf.String())
	}
	// Ensure the task short ID did make it into the tree output, so the test
	// isn't passing because the tree is empty.
	if !strings.Contains(buf.String(), task.ShortID) {
		t.Fatalf("expected short ID in tree output, got:\n%s", buf.String())
	}
}
