# Config-based Projects — Revised Phase 3: E2E Tests & Cleanup

**Goal:** Update E2E tests for config-driven projects, update the test harness to support custom config files, and verify everything works end-to-end.

**Outcome:** All unit tests, E2E tests, vet, and lint pass. The migration from UUID-based DB projects to config-driven string-keyed projects is complete.

**Prerequisite:** Phase 2 (full type migration) must be complete and `go build ./...` must succeed.

**Design spec:** `docs/superpowers/specs/2026-04-04-config-based-projects-design.md`

---

## What changed for E2E tests

1. `tusk project create` and `tusk project modify` no longer exist
2. `tusk project list` now shows `ID`, `WORKFLOW`, `SETTINGS` columns (not `NAME`, `DESCRIPTION`)
3. The default project is now called `default` (not `_default`)
4. Project JSON format changed: `{id, workflow, settings}` instead of `{id, name, description, default_workflow, settings, version, created_at}`
5. Propagation tests that used `project modify _default --set ...` now need a custom config file instead

---

## Task 1: Update E2E test harness to support custom config

The harness (`tests/e2e/harness.go`) currently creates a temp DB file but doesn't manage config. For propagation tests, we need to write a custom `config.toml` with project settings.

### Step 1: Add config support to `Env`

In `tests/e2e/harness.go`, add a `configDir` field to the `Env` struct:

Find:
```go
type Env struct {
	t       *testing.T
	binPath string   // path to compiled tusk binary
	dbPath  string   // path to temp SQLite file
	dbMode  string   // "flag" or "env"
	format  string   // "text" or "json"
	results []Result // stored results for inter-step references
}
```
Replace with:
```go
type Env struct {
	t         *testing.T
	binPath   string   // path to compiled tusk binary
	dbPath    string   // path to temp SQLite file
	configDir string   // path to temp config directory (optional)
	dbMode    string   // "flag" or "env"
	format    string   // "text" or "json"
	results   []Result // stored results for inter-step references
}
```

### Step 2: Add WithConfig option to Env

Add a function to write a config file and set the config path. Add this after `newEnv`:

```go
// withConfig writes a config.toml to a temp directory and sets TUSK_CONFIG_DIR
// on future commands so the tusk binary uses it.
func (e *Env) withConfig(configContent string) {
	e.t.Helper()
	dir := e.t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		e.t.Fatalf("writing test config: %v", err)
	}
	e.configDir = dir
}
```

You'll need to add `"path/filepath"` to the imports if not already present.

### Step 3: Pass config dir to tusk commands

In the `Run` method, after the TUSK_DB env var block, add config dir support.

Find (in the `Run` method):
```go
	// Set TUSK_DB env var if using env mode
	if e.dbMode == "env" {
		cmd.Env = append(os.Environ(), "TUSK_DB="+e.dbPath)
	}
```
Replace with:
```go
	// Set environment variables
	env := os.Environ()
	if e.dbMode == "env" {
		env = append(env, "TUSK_DB="+e.dbPath)
	}
	if e.configDir != "" {
		env = append(env, "TUSK_CONFIG_DIR="+e.configDir)
	}
	cmd.Env = env
```

**IMPORTANT:** This requires the tusk binary to respect `TUSK_CONFIG_DIR`. Check if `config.Load()` supports this. If not, you need to add it:

In `internal/config/config.go`, in the `Load()` function, update the search path resolution to check `TUSK_CONFIG_DIR`:

Find:
```go
	var searchPath string
	if lo.searchPath != "" {
		searchPath = lo.searchPath
	} else {
		home, err := os.UserHomeDir()
		if err == nil {
			searchPath = filepath.Join(home, ".config", "tusk")
		}
	}
```
Replace with:
```go
	var searchPath string
	if lo.searchPath != "" {
		searchPath = lo.searchPath
	} else if envDir := os.Getenv("TUSK_CONFIG_DIR"); envDir != "" {
		searchPath = envDir
	} else {
		home, err := os.UserHomeDir()
		if err == nil {
			searchPath = filepath.Join(home, ".config", "tusk")
		}
	}
```

---

## Task 2: Rewrite project E2E tests

### Step 1: Replace `tests/e2e/project_test.go`

The old tests tested `project create`, `project modify`, duplicate names, etc. All of those are gone. Replace the ENTIRE file with tests for the new behavior:

```go
package e2e

import (
	"testing"
)

func TestProjectCommands(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "project_list_default",
			Steps: []Step{
				{
					Args: []string{"project", "list"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "default")
						assertContains(t, output, "kanban")
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) < 1 {
							t.Fatal("expected at least 1 project")
						}
						found := false
						for _, item := range arr {
							m := item.(map[string]any)
							if m["id"] == "default" {
								found = true
								if m["workflow"] != "kanban" {
									t.Errorf("expected workflow 'kanban', got %v", m["workflow"])
								}
								break
							}
						}
						if !found {
							t.Fatal("expected 'default' project in list")
						}
					},
				},
			},
		},
		{
			Name: "project_create_removed",
			Steps: []Step{
				{
					Args:    []string{"project", "create", "myproject"},
					WantErr: true,
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}
```

---

## Task 3: Rewrite propagation E2E tests

Propagation tests previously configured settings via `project modify _default --set ...`. Now they need a custom config file.

### Step 1: Replace `tests/e2e/propagation_test.go`

Replace the ENTIRE file with:

```go
package e2e

import (
	"encoding/json"
	"testing"
)

// autoCompleteConfig is a config.toml that enables auto-complete on the default project.
const autoCompleteConfig = `
[workflows.kanban]
statuses = ["pending", "active", "completed", "deleted"]
transitions = [
  { from = "pending",   to = "active" },
  { from = "pending",   to = "deleted" },
  { from = "active",    to = "completed" },
  { from = "active",    to = "pending" },
  { from = "active",    to = "deleted" },
  { from = "completed", to = "pending" },
]

[projects.default]
workflow = "kanban"

[projects.default.settings.auto_complete_parent]
trigger_status = "completed"
target_status = "completed"
`

// bothPropagationConfig enables both auto-complete and auto-revert on the default project.
const bothPropagationConfig = `
[workflows.kanban]
statuses = ["pending", "active", "completed", "deleted"]
transitions = [
  { from = "pending",   to = "active" },
  { from = "pending",   to = "deleted" },
  { from = "active",    to = "completed" },
  { from = "active",    to = "pending" },
  { from = "active",    to = "deleted" },
  { from = "completed", to = "pending" },
]

[projects.default]
workflow = "kanban"

[projects.default.settings.auto_complete_parent]
trigger_status = "completed"
target_status = "completed"

[projects.default.settings.auto_revert_parent]
trigger_status = "completed"
target_status = "pending"
`

func TestPropagation_Disabled(t *testing.T) {
	// Propagation is disabled by default — completing all children should NOT
	// auto-complete the parent.
	scenarios := []Scenario{
		{
			Name: "propagation_disabled_by_default",
			Steps: []Step{
				{Args: []string{"add", "Parent task"}},
				{Args: []string{"start", "$0.short_id"}},
				{Args: []string{"add", "Child task", "parent:$0.short_id"}},
				{Args: []string{"start", "$2.short_id"}},
				{Args: []string{"done", "$2.short_id"}},
				{
					Args: []string{"info", "$0.short_id"},
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertContains(t, r.Stdout, "active")
						assertNotContains(t, r.Stdout, "completed")
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "active" {
							t.Fatalf("expected parent status 'active', got %v", m["status"])
						}
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}

// runPropagationScenarios runs scenarios with a custom config file.
// JSON-only since assertions use AssertJSON.
func runPropagationScenarios(t *testing.T, scenarios []Scenario, configContent string) {
	t.Helper()
	combos := combinations(
		[]string{"flag", "env"},
		[]string{"json"},
	)
	for _, sc := range scenarios {
		for _, combo := range combos {
			dbMode := combo[0]
			name := sc.Name + "/" + dbMode + "/json"
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				env := newEnv(t, binPath, dbMode, "json")
				env.withConfig(configContent)

				for i, step := range sc.Steps {
					r := env.Run(step.Args...)
					if step.WantErr && r.Err == nil {
						t.Fatalf("step %d: expected error, got none. stdout:\n%s", i, r.Stdout)
					}
					if !step.WantErr && r.Err != nil {
						t.Fatalf("step %d: unexpected error: %v\nstderr: %s\nstdout: %s", i, r.Err, r.Stderr, r.Stdout)
					}
					if step.AssertJSON != nil {
						var parsed any
						if err := json.Unmarshal([]byte(r.Stdout), &parsed); err != nil {
							t.Fatalf("step %d: failed to parse JSON: %v\nraw:\n%s", i, err, r.Stdout)
						}
						step.AssertJSON(t, parsed)
					}
				}
			})
		}
	}
}

func TestPropagation_AutoComplete(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "auto_complete_all_children_done",
			Steps: []Step{
				{Args: []string{"add", "Parent task"}},
				{Args: []string{"start", "$0.short_id"}},
				{Args: []string{"add", "Child one", "parent:$0.short_id"}},
				{Args: []string{"add", "Child two", "parent:$0.short_id"}},
				{Args: []string{"start", "$2.short_id"}},
				{Args: []string{"done", "$2.short_id"}},
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "active" {
							t.Fatalf("expected parent still 'active', got %v", m["status"])
						}
					},
				},
				{Args: []string{"start", "$3.short_id"}},
				{Args: []string{"done", "$3.short_id"}},
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "completed" {
							t.Fatalf("expected parent 'completed' (auto-complete), got %v", m["status"])
						}
					},
				},
			},
		},
		{
			Name: "auto_complete_deleted_child_ignored",
			Steps: []Step{
				{Args: []string{"add", "Parent"}},
				{Args: []string{"start", "$0.short_id"}},
				{Args: []string{"add", "Child 1", "parent:$0.short_id"}},
				{Args: []string{"add", "Child 2", "parent:$0.short_id"}},
				{Args: []string{"delete", "$3.short_id"}},
				{Args: []string{"start", "$2.short_id"}},
				{Args: []string{"done", "$2.short_id"}},
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "completed" {
							t.Fatalf("expected parent 'completed', got %v", m["status"])
						}
					},
				},
			},
		},
		{
			Name: "auto_complete_workflow_guard",
			Steps: []Step{
				{Args: []string{"add", "Parent pending"}},
				{Args: []string{"add", "Child", "parent:$0.short_id"}},
				{Args: []string{"start", "$1.short_id"}},
				{Args: []string{"done", "$1.short_id"}},
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "pending" {
							t.Fatalf("expected parent still 'pending' (workflow guard), got %v", m["status"])
						}
					},
				},
			},
		},
	}

	runPropagationScenarios(t, scenarios, autoCompleteConfig)
}

func TestPropagation_Recursive(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "auto_complete_recursive",
			Steps: []Step{
				{Args: []string{"add", "Grandparent"}},
				{Args: []string{"start", "$0.short_id"}},
				{Args: []string{"add", "Parent", "parent:$0.short_id"}},
				{Args: []string{"start", "$2.short_id"}},
				{Args: []string{"add", "Child", "parent:$2.short_id"}},
				{Args: []string{"start", "$4.short_id"}},
				{Args: []string{"done", "$4.short_id"}},
				{
					Args: []string{"info", "$2.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "completed" {
							t.Fatalf("expected parent 'completed', got %v", m["status"])
						}
					},
				},
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "completed" {
							t.Fatalf("expected grandparent 'completed', got %v", m["status"])
						}
					},
				},
			},
		},
	}

	runPropagationScenarios(t, scenarios, autoCompleteConfig)
}

func TestPropagation_AutoRevert(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "auto_revert_child_reopened",
			Steps: []Step{
				{Args: []string{"add", "Parent"}},
				{Args: []string{"start", "$0.short_id"}},
				{Args: []string{"add", "Child", "parent:$0.short_id"}},
				{Args: []string{"start", "$2.short_id"}},
				{Args: []string{"done", "$2.short_id"}},
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "completed" {
							t.Fatalf("expected parent 'completed', got %v", m["status"])
						}
					},
				},
				{Args: []string{"modify", "$2.short_id", "status:pending"}},
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "pending" {
							t.Fatalf("expected parent 'pending' after revert, got %v", m["status"])
						}
					},
				},
			},
		},
		{
			Name: "auto_revert_recursive",
			Steps: []Step{
				{Args: []string{"add", "Grandparent"}},
				{Args: []string{"start", "$0.short_id"}},
				{Args: []string{"add", "Parent", "parent:$0.short_id"}},
				{Args: []string{"start", "$2.short_id"}},
				{Args: []string{"add", "Child", "parent:$2.short_id"}},
				{Args: []string{"start", "$4.short_id"}},
				{Args: []string{"done", "$4.short_id"}},
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "completed" {
							t.Fatalf("expected grandparent 'completed', got %v", m["status"])
						}
					},
				},
				{Args: []string{"modify", "$4.short_id", "status:pending"}},
				{
					Args: []string{"info", "$2.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "pending" {
							t.Fatalf("expected parent 'pending' after revert, got %v", m["status"])
						}
					},
				},
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "pending" {
							t.Fatalf("expected grandparent 'pending' after revert, got %v", m["status"])
						}
					},
				},
			},
		},
	}

	runPropagationScenarios(t, scenarios, bothPropagationConfig)
}
```

---

## Task 4: Update other E2E tests that reference project fields

### Step 1: Search for ProjectID references in E2E tests

Search all files in `tests/e2e/` for:
- `project_id` in JSON assertions (the value is now a string like `"default"`, not a UUID)
- `_default` (old project name — now `default`)
- `project create` or `project modify` (commands that no longer exist)

Fix any that reference the old format.

### Step 2: Check if any test creates tasks with `project:` filter

If any test does `add "task" project:_default`, update to `project:default`.

---

## Task 5: Update unit tests

### Step 1: Check and fix service tests

Run: `go test ./internal/service/...`

Common fixes needed:
- Tests that create `domain.Task` with `ProjectID: &someUUID` → change to `ProjectID: "default"`
- Tests that create `domain.TaskUpdate` with `ProjectID: &ppUUID` → change to `ProjectID: &someString`
- Tests that call `ProjectService.Create()` or `GetByName()` → remove or rewrite (these methods no longer exist)
- Tests that use `TaskTxProvider` with the old 3-argument callback → update to 1-argument callback

### Step 2: Check and fix SQLite tests

Run: `go test ./internal/sqlite/...`

Common fixes:
- Tests that use `ProjectRepo` → remove (no more SQLite project repo)
- Tests that reference the `projects` table in SQL → update or remove
- Tests that insert tasks with UUID project IDs → use string project IDs

### Step 3: Check and fix MCP tests

Run: `go test ./internal/mcp/...`

The `handlers_test.go` creates services with `NewProjectService(projectRepo)`. If it uses a SQLite project repo, switch to `inmem.NewProjectRepository(...)`.

### Step 4: Check and fix filter tests

Run: `go test ./internal/filter/...`

Tests that construct a `Resolver` with `NewResolver(projectLookup, taskLookup)` → change to `NewResolver(taskLookup)`.

---

## Task 6: Final verification

### Step 1: Run ALL tests

```bash
make test
```

Expected: ALL PASS.

### Step 2: Run with race detector

```bash
make test-race
```

Expected: ALL PASS.

### Step 3: Run E2E tests

```bash
make test-e2e
```

Expected: ALL PASS.

### Step 4: Run vet and lint

```bash
make vet
make lint
```

Expected: ALL PASS.

### Step 5: Manual smoke test

Build and run manually:

```bash
make build
./bin/tusk project list
./bin/tusk add "Test task"
./bin/tusk list
./bin/tusk info $(./bin/tusk list --format json | jq -r '.[0].short_id')
```

Verify:
- `project list` shows `default` with `kanban` workflow
- Tasks are created with `project_id: "default"`
- The config file is auto-created at `~/.config/tusk/config.toml`

---

## Commits

```bash
git add tests/e2e/ internal/config/config.go
git commit -m "test(e2e): update tests for config-driven projects"

# If service/sqlite/mcp/filter tests needed fixes:
git add internal/service/ internal/sqlite/ internal/mcp/ internal/filter/
git commit -m "test: fix unit tests for config-driven project types"
```
