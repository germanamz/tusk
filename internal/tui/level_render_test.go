package tui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/germanamz/tusk/domain"
)

func TestRunGet_RendersLevelWhenTaxonomyActive(test *testing.T) {
	app, tasks, _ := testAppWithTaxonomy(test, [][]string{{"milestone"}, {"story"}})
	level := "milestone"
	task := seedViolatingTask(test, tasks, "77777777", "with-level", &level)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "get", task.ShortID})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("task get: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Level:") {
		test.Fatalf("expected Level: in output, got:\n%s", out)
	}
	if !strings.Contains(out, "milestone") {
		test.Fatalf("expected milestone value in output, got:\n%s", out)
	}
}

func TestRunGet_OmitsLevelWhenNoTaxonomy(test *testing.T) {
	app, taskSvc := testApp(test)

	task := &domain.Task{Title: "no-taxonomy"}
	if err := taskSvc.Create(test.Context(), task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "get", task.ShortID})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("task get: %v", err)
	}
	if strings.Contains(buf.String(), "Level:") {
		test.Fatalf("expected no Level: line when taxonomy empty, got:\n%s", buf.String())
	}
}

func TestRunGet_LevelDashWhenUnset(test *testing.T) {
	app, tasks, _ := testAppWithTaxonomy(test, [][]string{{"milestone"}})
	// Seeded task has no level.
	task := seedViolatingTask(test, tasks, "88888888", "unset-level", nil)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "get", task.ShortID})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("task get: %v", err)
	}
	if !strings.Contains(buf.String(), "Level:") || !strings.Contains(buf.String(), "—") {
		test.Fatalf("expected Level: — for unset level, got:\n%s", buf.String())
	}
}

func TestRunTree_AppendsLevelSuffixWhenTaxonomyActive(test *testing.T) {
	app, tasks, _ := testAppWithTaxonomy(test, [][]string{{"milestone"}})
	level := "milestone"
	seedViolatingTask(test, tasks, "aa000000", "tree-task", &level)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "tree"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("task tree: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "[milestone]") {
		test.Fatalf("expected [milestone] suffix in tree output, got:\n%s", out)
	}
}

func TestRunTree_NoLevelSuffixWhenTaxonomyEmpty(test *testing.T) {
	app, taskSvc := testApp(test)

	task := &domain.Task{Title: "no-tax tree"}
	if err := taskSvc.Create(test.Context(), task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "tree"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("task tree: %v", err)
	}
	if strings.Contains(buf.String(), "[—]") {
		test.Fatalf("expected no level suffix when taxonomy empty, got:\n%s", buf.String())
	}
	// Ensure the task short ID did make it into the tree output, so the test
	// isn't passing because the tree is empty.
	if !strings.Contains(buf.String(), task.ShortID) {
		test.Fatalf("expected short ID in tree output, got:\n%s", buf.String())
	}
}
