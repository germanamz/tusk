# Project Management Phase 4: E2E Tests + Harness Cleanup

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add E2E tests for all project CLI commands, and remove the `SetDefaultProjectSettings` harness helper in favor of the new `tusk project modify` command.

**Architecture:** New E2E test file follows the existing `Scenario`/`Step` pattern from `tests/e2e/harness.go`. The propagation tests are updated to use CLI commands instead of direct database manipulation. The `SetDefaultProjectSettings` helper is removed.

**Tech Stack:** Go, E2E test harness (`tests/e2e`)

**Prerequisite:** Phase 3 must be complete (CLI commands working, `tusk project {list,create,modify}` operational).

---

### Task 1: Add E2E Tests for Project Commands

**Files:**
- Create: `tests/e2e/project_test.go`

- [ ] **Step 1: Create the project E2E test file**

Create `tests/e2e/project_test.go`:

```go
package e2e

import (
	"strings"
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
						assertContains(t, output, "_default")
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) < 1 {
							t.Fatal("expected at least 1 project (_default)")
						}
						found := false
						for _, item := range arr {
							m := item.(map[string]any)
							if m["name"] == "_default" {
								found = true
								break
							}
						}
						if !found {
							t.Fatal("expected _default project in list")
						}
					},
				},
			},
		},
		{
			Name: "project_create_and_list",
			Steps: []Step{
				// Step 0: Create a project
				{
					Args: []string{"project", "create", "myproject", "-d", "My test project"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Created project myproject")
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["name"], "myproject")
						assertEqual(t, m["description"], "My test project")
						assertEqual(t, m["default_workflow"], "default")
						assertEqual(t, m["version"], float64(1))
					},
				},
				// Step 1: List should include the new project
				{
					Args: []string{"project", "list"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "myproject")
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						// _default + myproject = 2
						if len(arr) != 2 {
							t.Fatalf("expected 2 projects, got %d", len(arr))
						}
					},
				},
			},
		},
		{
			Name: "project_modify_description",
			Steps: []Step{
				// Step 0: Create
				{Args: []string{"project", "create", "descproj", "-d", "Old desc"}},
				// Step 1: Modify description
				{
					Args: []string{"project", "modify", "descproj", "-d", "New desc"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Modified project descproj")
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["description"], "New desc")
						assertEqual(t, m["version"], float64(2))
					},
				},
			},
		},
		{
			Name: "project_modify_settings_set",
			Steps: []Step{
				// Step 0: Create
				{Args: []string{"project", "create", "settingsproj"}},
				// Step 1: Set auto-complete settings
				{
					Args: []string{
						"project", "modify", "settingsproj",
						"--set", "auto_complete_parent.trigger_status=completed",
						"--set", "auto_complete_parent.target_status=completed",
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						settings := m["settings"].(map[string]any)
						ac := settings["auto_complete_parent"].(map[string]any)
						assertEqual(t, ac["trigger_status"], "completed")
						assertEqual(t, ac["target_status"], "completed")
					},
				},
			},
		},
		{
			Name: "project_modify_settings_unset",
			Steps: []Step{
				// Step 0: Create
				{Args: []string{"project", "create", "unsetproj"}},
				// Step 1: Set auto-complete
				{Args: []string{
					"project", "modify", "unsetproj",
					"--set", "auto_complete_parent.trigger_status=completed",
					"--set", "auto_complete_parent.target_status=completed",
				}},
				// Step 2: Unset auto-complete
				{
					Args: []string{
						"project", "modify", "unsetproj",
						"--unset", "auto_complete_parent",
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						settings := m["settings"].(map[string]any)
						if settings["auto_complete_parent"] != nil {
							t.Fatal("expected auto_complete_parent to be nil after unset")
						}
					},
				},
			},
		},
		{
			Name: "project_create_duplicate",
			Steps: []Step{
				{Args: []string{"project", "create", "dupproj"}},
				{
					Args:    []string{"project", "create", "dupproj"},
					WantErr: true,
				},
			},
		},
		{
			Name: "project_modify_not_found",
			Steps: []Step{
				{
					Args:    []string{"project", "modify", "nonexistent", "-d", "nope"},
					WantErr: true,
				},
			},
		},
		{
			Name: "project_modify_no_flags",
			Steps: []Step{
				{
					Args:    []string{"project", "modify", "_default"},
					WantErr: true,
				},
			},
		},
		{
			Name: "project_modify_invalid_set_format",
			Steps: []Step{
				{
					Args:    []string{"project", "modify", "_default", "--set", "noequals"},
					WantErr: true,
				},
			},
		},
		{
			Name: "project_modify_unknown_set_path",
			Steps: []Step{
				{
					Args:    []string{"project", "modify", "_default", "--set", "unknown.path=value"},
					WantErr: true,
				},
			},
		},
		{
			Name: "project_create_description_in_list",
			Steps: []Step{
				{Args: []string{"project", "create", "listedproj", "-d", "Visible description"}},
				{
					Args: []string{"project", "list"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "listedproj")
						assertContains(t, output, "Visible description")
					},
				},
			},
		},
		{
			Name: "project_settings_summary_in_list",
			Steps: []Step{
				{Args: []string{"project", "create", "summaryproj"}},
				{Args: []string{
					"project", "modify", "summaryproj",
					"--set", "auto_complete_parent.trigger_status=completed",
					"--set", "auto_complete_parent.target_status=completed",
				}},
				{
					Args: []string{"project", "list"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						// Find the line for summaryproj
						lines := strings.Split(output, "\n")
						found := false
						for _, line := range lines {
							if strings.Contains(line, "summaryproj") {
								found = true
								assertContains(t, line, "auto-complete:on")
								break
							}
						}
						if !found {
							t.Fatal("summaryproj not found in list output")
						}
					},
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}
```

- [ ] **Step 2: Run the E2E tests**

```bash
go test -v ./tests/e2e -run TestProjectCommands -count=1
```

Expected: All scenarios pass across all 4 combinations (flag/env × text/json).

If any tests fail, diagnose and fix before continuing. Common issues:
- JSON output structure might differ from expected — check actual output with `-v` flag
- Text output format might not match expected strings — adjust assertions

- [ ] **Step 3: Commit**

```bash
git add tests/e2e/project_test.go
git commit -m "test(e2e): add project management E2E tests

Cover project list, create, modify (description + settings),
duplicate create error, not-found error, no-flags error,
invalid set format, and unknown set path."
```

---

### Task 2: Update Propagation Tests to Use CLI

**Files:**
- Modify: `tests/e2e/propagation_test.go`

This task replaces `env.SetDefaultProjectSettings(...)` calls with `tusk project modify _default --set ...` CLI steps.

- [ ] **Step 1: Update runPropagationScenarios helper**

In `tests/e2e/propagation_test.go`, the `runPropagationScenarios` function (around line 57-90) currently calls `env.SetDefaultProjectSettings(settings)`. Replace the direct DB manipulation with a CLI step that runs before each scenario's steps.

Replace the entire `runPropagationScenarios` function with:

```go
// runPropagationScenarios runs scenarios after configuring project settings via CLI.
// JSON-only since assertions use AssertJSON.
func runPropagationScenarios(t *testing.T, scenarios []Scenario, setupSteps []Step) {
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

				// Run setup steps to configure project settings
				for i, step := range setupSteps {
					r := env.Run(step.Args...)
					if r.Err != nil {
						t.Fatalf("setup step %d: %v\nstderr: %s", i, r.Err, r.Stderr)
					}
				}
				// Clear results so scenario step references ($0, $1) start fresh
				env.results = nil

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
```

- [ ] **Step 2: Replace settings string constants with setup step slices**

At the top of `propagation_test.go`, replace the settings JSON string constants with Step slices.

Remove:

```go
// autoCompleteSettings is the JSON to enable auto-complete on the default project.
const autoCompleteSettings = `{"auto_complete_parent":{"trigger_status":"completed","target_status":"completed"}}`

// bothPropagationSettings enables both auto-complete and auto-revert.
// Note: default workflow only allows completed -> pending (not completed -> active),
// so the revert target must be "pending".
const bothPropagationSettings = `{"auto_complete_parent":{"trigger_status":"completed","target_status":"completed"},"auto_revert_parent":{"trigger_status":"completed","target_status":"pending"}}`
```

Replace with:

```go
// autoCompleteSetup configures auto-complete on the _default project via CLI.
var autoCompleteSetup = []Step{
	{Args: []string{
		"project", "modify", "_default",
		"--set", "auto_complete_parent.trigger_status=completed",
		"--set", "auto_complete_parent.target_status=completed",
	}},
}

// bothPropagationSetup enables both auto-complete and auto-revert on _default.
// Note: default workflow only allows completed -> pending (not completed -> active),
// so the revert target must be "pending".
var bothPropagationSetup = []Step{
	{Args: []string{
		"project", "modify", "_default",
		"--set", "auto_complete_parent.trigger_status=completed",
		"--set", "auto_complete_parent.target_status=completed",
		"--set", "auto_revert_parent.trigger_status=completed",
		"--set", "auto_revert_parent.target_status=pending",
	}},
}
```

- [ ] **Step 3: Update all callers of runPropagationScenarios**

There are 4 callers. Update each one:

1. **TestPropagation_AutoComplete** (around line 92-193) — change:
```go
runPropagationScenarios(t, scenarios, autoCompleteSettings)
```
to:
```go
runPropagationScenarios(t, scenarios, autoCompleteSetup)
```

2. **TestPropagation_Recursive** (around line 196-242) — change:
```go
runPropagationScenarios(t, scenarios, autoCompleteSettings)
```
to:
```go
runPropagationScenarios(t, scenarios, autoCompleteSetup)
```

3. **TestPropagation_AutoRevert** (around line 244-342) — change:
```go
runPropagationScenarios(t, scenarios, bothPropagationSettings)
```
to:
```go
runPropagationScenarios(t, scenarios, bothPropagationSetup)
```

4. **TestPropagation_CustomTargetStatus** (around line 344-394) — this test has its own inline `customSettings` string. Replace the whole settings definition and caller.

Change:
```go
customSettings := `{"auto_complete_parent":{"trigger_status":"completed","target_status":"completed"},"auto_revert_parent":{"trigger_status":"completed","target_status":"pending"}}`
```

to:

```go
customSetup := []Step{
	{Args: []string{
		"project", "modify", "_default",
		"--set", "auto_complete_parent.trigger_status=completed",
		"--set", "auto_complete_parent.target_status=completed",
		"--set", "auto_revert_parent.trigger_status=completed",
		"--set", "auto_revert_parent.target_status=pending",
	}},
}
```

And change:
```go
runPropagationScenarios(t, scenarios, customSettings)
```
to:
```go
runPropagationScenarios(t, scenarios, customSetup)
```

- [ ] **Step 4: Run propagation tests**

```bash
go test -v ./tests/e2e -run TestPropagation -count=1
```

Expected: All propagation tests pass. The setup steps configure settings via CLI before each scenario runs.

- [ ] **Step 5: Commit**

```bash
git add tests/e2e/propagation_test.go
git commit -m "refactor(e2e): use CLI commands for propagation test settings

Replace direct DB manipulation with tusk project modify --set
commands. Settings are now configured through the same code path
users will use."
```

---

### Task 3: Remove SetDefaultProjectSettings Helper

**Files:**
- Modify: `tests/e2e/harness.go`

- [ ] **Step 1: Remove the SetDefaultProjectSettings method**

In `tests/e2e/harness.go`, delete the entire `SetDefaultProjectSettings` method (lines 97-123):

```go
// SetDefaultProjectSettings updates the _default project's settings JSON
// directly in the database. This is needed because there is no CLI command
// to modify project settings yet. It runs a dummy CLI command first to
// ensure the database and tables are initialized.
func (e *Env) SetDefaultProjectSettings(settingsJSON string) {
	e.t.Helper()

	// Run a no-op command to initialize the database (migrations, default project).
	// We don't store the result in env.results so $N references are unaffected.
	initArgs := []string{"--db", e.dbPath, "list"}
	cmd := exec.Command(e.binPath, initArgs...)
	if err := cmd.Run(); err != nil {
		e.t.Fatalf("initializing db for settings: %v", err)
	}

	db, err := sql.Open("sqlite", e.dbPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		e.t.Fatalf("opening db for settings: %v", err)
	}
	defer db.Close()

	defaultProjectID := "00000000-0000-0000-0000-000000000000"
	_, err = db.Exec(`UPDATE projects SET settings = ? WHERE id = ?`, settingsJSON, defaultProjectID)
	if err != nil {
		e.t.Fatalf("setting project settings: %v", err)
	}
}
```

- [ ] **Step 2: Remove unused imports from harness.go**

After removing `SetDefaultProjectSettings`, the `"database/sql"` import is no longer used. Remove it from the import block.

Check if any other function in harness.go uses `"database/sql"`. Looking at the file, only `SetDefaultProjectSettings` used `sql.Open`. Remove the import:

```go
"database/sql"
```

Also check if `_ "modernc.org/sqlite"` is still needed. This was the driver import for `sql.Open("sqlite", ...)`. Since we removed the only `sql.Open` call, this import is no longer needed either. Remove:

```go
_ "modernc.org/sqlite"
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./tests/e2e/...
```

Expected: Success. If there are compile errors about unused imports, remove them.

- [ ] **Step 4: Run the full E2E test suite**

```bash
go test -v ./tests/e2e/... -count=1
```

Expected: All tests pass — both the new project tests and the updated propagation tests.

- [ ] **Step 5: Run the complete test suite**

```bash
make test
```

Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
git add tests/e2e/harness.go
git commit -m "refactor(e2e): remove SetDefaultProjectSettings helper

No longer needed now that tusk project modify --set provides
the same capability through the CLI."
```
