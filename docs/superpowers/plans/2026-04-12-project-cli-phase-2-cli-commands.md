# Phase 2 — Project CLI Commands

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Initiative:** Project Management CLI (ROADMAP.md:543-564)
**Phase:** 2 of 4
**Prerequisites:** Phase 1 complete — the `config` package exposes `CreateProject`, `ModifyProject`, `DeleteProject`, `ResolveWeightDelta`, `UrgencyFieldPtr`, `ProjectMutation`, `TaskRefChecker`, `DefaultProjectID`, and `ProjectConfig.DBPath`.

---

## Inherits From

Phase 1 added exported CRUD helpers to the `config` package but no runtime behavior change. After Phase 1, the codebase has:

- `config.ProjectConfig` with a new `DBPath string` field (TOML `db_path`, omit-empty)
- `config.CreateProject(path, name, proj) error` — inserts a new project, validates, writes
- `config.ModifyProject(path, name, mut) error` — applies a `ProjectMutation`; urgency `Set`/`Delta` maps share a key namespace (`priority_weight`, `due_weight`, `age_weight`, `active_weight`, `blocking_weight`, `blocked_weight`, `tags_weight`, `project_weight`, `annotations_weight`, `waiting_weight`) and conflict if both name the same key
- `config.DeleteProject(path, name, hasRefs, force) error` — rejects the built-in `DefaultProjectID` and non-empty reference counts unless `force` is true
- `config.ResolveWeightDelta(globalWeight, override, delta) float64` — arithmetic base selection
- `config.UrgencyFieldPtr(u, key) **float64` — exported helper for parser reuse
- `config.ProjectMutation` struct with fields `Workflow *string`, `DBPath *string`, `AutoCompleteSet *AutoCompleteParentConfig`, `AutoRevertSet *AutoRevertParentConfig`, `UrgencySet map[string]float64`, `UrgencyDelta map[string]float64`

Phase 2 wires the CLI in front of these helpers. No other package is touched. The runtime still has a single `sqlite.Store` and every service points at it — this phase does not introduce per-project databases.

---

## Goal

Ship `tusk project create <name> [fields...]`, `tusk project modify <name> [fields...]`, and `tusk project delete <name> [--force]` as working CLI commands. The commands parse inline syntax using the existing `syntax.ParseFields` lexer (which already carries `+`/`-` modifiers on tokens), write to the config file via Phase 1 helpers, and render output through the existing `Renderer`.

---

## User-Visible Behaviors Preserved

Every pre-existing command must still work after this phase. In addition, the following new behaviors are shipped:

- `tusk project list` — unchanged (already existed)
- `tusk project create <name> workflow=<wf> [db-path=<path>] [auto-complete.trigger=<status>] [auto-complete.target=<status>] [auto-revert.trigger=<status>] [auto-revert.target=<status>] [urgency.<key>=<float>...]` — creates a new project in the effective config file
- `tusk project modify <name> [workflow=<wf>] [db-path=<path>|db-path=] [auto-complete.*] [auto-revert.*] [urgency.<key>=<float>] [+urgency.<key>=<float>] [-urgency.<key>=<float>]` — bare assignment replaces, `+` / `-` modifiers apply arithmetic deltas on urgency keys only
- `tusk project delete <name> [--force]` — rejects default project and task-referenced projects unless forced
- Storing `db_path` on a project has no runtime effect yet (Phase 3 honors it). The CLI still accepts the field and writes it to TOML.

Acceptance: `make test test-race test-e2e vet lint` must all pass.

---

## File Structure

**Create:**
- `internal/tui/project.go` — `buildProjectCmd`, `runProjectCreate`, `runProjectModify`, `runProjectDelete`, `countTasksForProject` helper
- `internal/tui/project_parse.go` — `parseProjectCreate`, `parseProjectModify`, `applyProjectField`, `urgencyCLIToConfigKey`, `parseFloatField`
- `internal/tui/project_parse_test.go` — parser unit tests
- `tests/e2e/project_crud_test.go` — e2e scenarios

**Modify:**
- `internal/tui/render.go` — add `renderProjectMutation` method alongside `renderWorkflowMutation` (currently at line 810)
- `internal/tui/app.go` — register `buildProjectCmd()` on the root command. Find the call site of `buildWorkflowCmd()` and add `buildProjectCmd()` next to it.

---

## Tasks

### Task 1: Inline-syntax parsers for create and modify

**Files:**
- Create: `internal/tui/project_parse.go`
- Create: `internal/tui/project_parse_test.go`

Decodes `[]string` args via `syntax.ParseFields` and converts the resulting tokens into either `config.ProjectConfig` or `config.ProjectMutation`. Accepted keys:

- `workflow`
- `db-path` (CLI dash form maps to `db_path` TOML form)
- `auto-complete.trigger`, `auto-complete.target`
- `auto-revert.trigger`, `auto-revert.target`
- `urgency.<weight>` — any of `priority-weight`, `due-weight`, `age-weight`, `active-weight`, `blocking-weight`, `blocked-weight`, `tags-weight`, `project-weight`, `annotations-weight`, `waiting-weight` (CLI dashes map to TOML underscores)

Create disallows all modifiers. Modify accepts `+`/`-` only on urgency keys.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/project_parse_test.go`:

```go
package tui

import "testing"

func TestParseProjectCreate_Basic(t *testing.T) {
	proj, err := parseProjectCreate([]string{"workflow=kanban", "db-path=/tmp/b.db"})
	if err != nil {
		t.Fatalf("parseProjectCreate: %v", err)
	}
	if proj.Workflow != "kanban" || proj.DBPath != "/tmp/b.db" {
		t.Fatalf("unexpected: %+v", proj)
	}
}

func TestParseProjectCreate_AutoCompleteAndUrgency(t *testing.T) {
	proj, err := parseProjectCreate([]string{
		"workflow=kanban",
		"auto-complete.trigger=completed",
		"auto-complete.target=completed",
		"urgency.blocking-weight=15",
	})
	if err != nil {
		t.Fatalf("parseProjectCreate: %v", err)
	}
	if proj.Settings.AutoCompleteParent == nil ||
		proj.Settings.AutoCompleteParent.TriggerStatus != "completed" {
		t.Fatalf("auto-complete: %+v", proj.Settings.AutoCompleteParent)
	}
	if proj.Settings.Urgency == nil ||
		proj.Settings.Urgency.BlockingWeight == nil ||
		*proj.Settings.Urgency.BlockingWeight != 15 {
		t.Fatalf("urgency: %+v", proj.Settings.Urgency)
	}
}

func TestParseProjectCreate_RejectsModifier(t *testing.T) {
	_, err := parseProjectCreate([]string{"+workflow=kanban"})
	if err == nil {
		t.Fatal("expected modifier rejection")
	}
}

func TestParseProjectCreate_UnknownField(t *testing.T) {
	_, err := parseProjectCreate([]string{"ghost=value"})
	if err == nil {
		t.Fatal("expected unknown-field error")
	}
}

func TestParseProjectModify_BareSet(t *testing.T) {
	mut, err := parseProjectModify([]string{"workflow=sprint", "urgency.blocking-weight=10"})
	if err != nil {
		t.Fatalf("parseProjectModify: %v", err)
	}
	if mut.Workflow == nil || *mut.Workflow != "sprint" {
		t.Fatalf("workflow: %+v", mut.Workflow)
	}
	if mut.UrgencySet["blocking_weight"] != 10 {
		t.Fatalf("urgency set: %+v", mut.UrgencySet)
	}
}

func TestParseProjectModify_Delta(t *testing.T) {
	mut, err := parseProjectModify([]string{"+urgency.blocking-weight=2", "-urgency.due-weight=1"})
	if err != nil {
		t.Fatalf("parseProjectModify: %v", err)
	}
	if mut.UrgencyDelta["blocking_weight"] != 2 {
		t.Fatalf("add delta: %+v", mut.UrgencyDelta)
	}
	if mut.UrgencyDelta["due_weight"] != -1 {
		t.Fatalf("sub delta: %+v", mut.UrgencyDelta)
	}
}

func TestParseProjectModify_DeltaOnNonUrgencyRejected(t *testing.T) {
	_, err := parseProjectModify([]string{"+workflow=sprint"})
	if err == nil {
		t.Fatal("expected rejection of modifier on workflow")
	}
}

func TestParseProjectModify_ClearDBPath(t *testing.T) {
	mut, err := parseProjectModify([]string{"db-path="})
	if err != nil {
		t.Fatalf("parseProjectModify: %v", err)
	}
	if mut.DBPath == nil || *mut.DBPath != "" {
		t.Fatalf("expected DBPath pointer to empty, got %+v", mut.DBPath)
	}
}
```

- [ ] **Step 2: Run and confirm failure**

```bash
go test ./internal/tui -run TestParseProject -v
```

Expected: FAIL — parsers undefined.

- [ ] **Step 3: Implement the parsers**

Create `internal/tui/project_parse.go`:

```go
package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/syntax"
)

// urgencyCLIToConfigKey maps inline-syntax weight names ("urgency.blocking-weight")
// to the TOML/struct key ("blocking_weight"). Returns the TOML key and true
// if the CLI key is a recognized urgency weight.
func urgencyCLIToConfigKey(cliKey string) (string, bool) {
	if !strings.HasPrefix(cliKey, "urgency.") {
		return "", false
	}
	rest := strings.TrimPrefix(cliKey, "urgency.")
	key := strings.ReplaceAll(rest, "-", "_")
	switch key {
	case "priority_weight", "due_weight", "age_weight", "active_weight",
		"blocking_weight", "blocked_weight", "tags_weight", "project_weight",
		"annotations_weight", "waiting_weight":
		return key, true
	}
	return "", false
}

func parseFloatField(key, value string) (float64, error) {
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("field %q: %v", key, err)
	}
	return f, nil
}

// parseProjectCreate decodes args for `tusk project create`.
// No modifiers are allowed on create.
func parseProjectCreate(args []string) (config.ProjectConfig, error) {
	input := strings.Join(args, " ")
	fs, parseErrs := syntax.ParseFields(input)
	if len(parseErrs) > 0 {
		return config.ProjectConfig{}, fmt.Errorf("parse error: %s", parseErrs[0].Message)
	}
	proj := config.ProjectConfig{}
	for _, f := range fs.Fields {
		if f.Modifier != 0 {
			return config.ProjectConfig{}, fmt.Errorf("project create does not accept modifier %q on %q", f.Modifier, f.Key)
		}
		if err := applyProjectField(&proj, f.Key, f.Value); err != nil {
			return config.ProjectConfig{}, err
		}
	}
	return proj, nil
}

// applyProjectField sets a single key=value pair on a ProjectConfig.
func applyProjectField(proj *config.ProjectConfig, key, value string) error {
	switch key {
	case "workflow":
		proj.Workflow = value
	case "db-path":
		proj.DBPath = value
	case "auto-complete.trigger":
		if proj.Settings.AutoCompleteParent == nil {
			proj.Settings.AutoCompleteParent = &config.AutoCompleteParentConfig{}
		}
		proj.Settings.AutoCompleteParent.TriggerStatus = value
	case "auto-complete.target":
		if proj.Settings.AutoCompleteParent == nil {
			proj.Settings.AutoCompleteParent = &config.AutoCompleteParentConfig{}
		}
		proj.Settings.AutoCompleteParent.TargetStatus = value
	case "auto-revert.trigger":
		if proj.Settings.AutoRevertParent == nil {
			proj.Settings.AutoRevertParent = &config.AutoRevertParentConfig{}
		}
		proj.Settings.AutoRevertParent.TriggerStatus = value
	case "auto-revert.target":
		if proj.Settings.AutoRevertParent == nil {
			proj.Settings.AutoRevertParent = &config.AutoRevertParentConfig{}
		}
		proj.Settings.AutoRevertParent.TargetStatus = value
	default:
		cfgKey, ok := urgencyCLIToConfigKey(key)
		if !ok {
			return fmt.Errorf("unknown field %q", key)
		}
		f, err := parseFloatField(key, value)
		if err != nil {
			return err
		}
		if proj.Settings.Urgency == nil {
			proj.Settings.Urgency = &config.ProjectUrgencyConfig{}
		}
		fp := config.UrgencyFieldPtr(proj.Settings.Urgency, cfgKey)
		if fp == nil {
			return fmt.Errorf("unknown urgency key %q", cfgKey)
		}
		val := f
		*fp = &val
	}
	return nil
}

// parseProjectModify decodes args for `tusk project modify`.
// Modifiers '+' and '-' are only accepted on urgency fields (arithmetic deltas).
func parseProjectModify(args []string) (config.ProjectMutation, error) {
	input := strings.Join(args, " ")
	fs, parseErrs := syntax.ParseFields(input)
	if len(parseErrs) > 0 {
		return config.ProjectMutation{}, fmt.Errorf("parse error: %s", parseErrs[0].Message)
	}
	mut := config.ProjectMutation{
		UrgencySet:   map[string]float64{},
		UrgencyDelta: map[string]float64{},
	}

	for _, f := range fs.Fields {
		if f.Modifier != 0 {
			cfgKey, ok := urgencyCLIToConfigKey(f.Key)
			if !ok {
				return config.ProjectMutation{}, fmt.Errorf("modifier %q not supported on %q (only urgency weights)", f.Modifier, f.Key)
			}
			v, err := parseFloatField(f.Key, f.Value)
			if err != nil {
				return config.ProjectMutation{}, err
			}
			switch f.Modifier {
			case '+':
				mut.UrgencyDelta[cfgKey] = v
			case '-':
				mut.UrgencyDelta[cfgKey] = -v
			default:
				return config.ProjectMutation{}, fmt.Errorf("unsupported modifier %q", f.Modifier)
			}
			continue
		}

		switch f.Key {
		case "workflow":
			v := f.Value
			mut.Workflow = &v
		case "db-path":
			v := f.Value
			mut.DBPath = &v
		case "auto-complete.trigger", "auto-complete.target":
			if mut.AutoCompleteSet == nil {
				mut.AutoCompleteSet = &config.AutoCompleteParentConfig{}
			}
			if f.Key == "auto-complete.trigger" {
				mut.AutoCompleteSet.TriggerStatus = f.Value
			} else {
				mut.AutoCompleteSet.TargetStatus = f.Value
			}
		case "auto-revert.trigger", "auto-revert.target":
			if mut.AutoRevertSet == nil {
				mut.AutoRevertSet = &config.AutoRevertParentConfig{}
			}
			if f.Key == "auto-revert.trigger" {
				mut.AutoRevertSet.TriggerStatus = f.Value
			} else {
				mut.AutoRevertSet.TargetStatus = f.Value
			}
		default:
			cfgKey, ok := urgencyCLIToConfigKey(f.Key)
			if !ok {
				return config.ProjectMutation{}, fmt.Errorf("unknown field %q", f.Key)
			}
			v, err := parseFloatField(f.Key, f.Value)
			if err != nil {
				return config.ProjectMutation{}, err
			}
			mut.UrgencySet[cfgKey] = v
		}
	}
	return mut, nil
}
```

- [ ] **Step 4: Re-run tests**

```bash
go test ./internal/tui -run TestParseProject -v
```

Expected: PASS (all eight cases).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/project_parse.go internal/tui/project_parse_test.go
git commit -m "feat(tui): add project create/modify inline-syntax parsers"
```

---

### Task 2: `renderProjectMutation` method

**Files:**
- Modify: `internal/tui/render.go` — add a new method alongside `renderWorkflowMutation` (defined at line 810 as of commit `e41f7a9`)

`renderWorkflowMutation(action string, name string) error` is the template. Mirror its structure exactly — emit plaintext like `Created project "backend"` for text output and JSON `{"action": "created", "resource": "project", "name": "backend"}` for JSON output.

- [ ] **Step 1: Open `internal/tui/render.go` and inspect `renderWorkflowMutation`**

```bash
grep -n 'renderWorkflowMutation' internal/tui/render.go
```

Expected: one definition around line 810. Read its body in full.

- [ ] **Step 2: Add a parallel method**

Copy the body of `renderWorkflowMutation` and rename to `renderProjectMutation`. Replace all occurrences of `workflow` with `project` in its output strings and JSON key names. Place the new method directly after `renderWorkflowMutation` in the same file.

- [ ] **Step 3: Build to confirm it compiles**

```bash
go build ./internal/tui/...
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/render.go
git commit -m "feat(tui): add renderProjectMutation output method"
```

---

### Task 3: `tusk project create/modify/delete` command group + wiring

**Files:**
- Create: `internal/tui/project.go`
- Modify: `internal/tui/app.go` — add `a.buildProjectCmd()` to the root command (search for `buildWorkflowCmd()` call)

The task-ref callback passed into `config.DeleteProject` uses the existing single-store `taskSvc.List` with a `project=<name>` filter — good enough for Phase 2, and still correct in Phase 4 because `List` will fan out across stores. Build the filter by joining the project name into a filter string and handing it to `filter.Parse` (the same path used by `runList` in `internal/tui/commands.go`).

- [ ] **Step 1: Confirm the wiring site**

```bash
grep -n 'buildWorkflowCmd' internal/tui cmd/tusk -r
```

Expected: one call site, typically inside `internal/tui/app.go` where the root command is assembled.

- [ ] **Step 2: Create `internal/tui/project.go`**

```go
package tui

import (
	"context"
	"fmt"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/filter"
	"github.com/spf13/cobra"
)

// buildProjectCmd creates the `tusk project` command group.
func (a *App) buildProjectCmd() *cobra.Command {
	projectCmd := &cobra.Command{
		Use:   "project",
		Short: "Manage projects",
	}

	var force bool
	deleteCmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a project from config",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runProjectDelete(cmd, args, force)
		},
	}
	deleteCmd.Flags().BoolVar(&force, "force", false, "bypass task-reference and built-in guards")

	projectCmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List all projects",
			Args:  cobra.NoArgs,
			RunE:  a.runProjectList,
		},
		&cobra.Command{
			Use:   "create <name> [fields...]",
			Short: "Create a new project",
			Args:  cobra.MinimumNArgs(1),
			RunE:  a.runProjectCreate,
		},
		&cobra.Command{
			Use:   "modify <name> [fields...]",
			Short: "Modify an existing project",
			Args:  cobra.MinimumNArgs(2),
			RunE:  a.runProjectModify,
		},
		deleteCmd,
	)
	return projectCmd
}

func (a *App) runProjectCreate(cmd *cobra.Command, args []string) error {
	name := args[0]
	proj, err := parseProjectCreate(args[1:])
	if err != nil {
		return err
	}
	path, err := config.ConfigFilePath(a.loadOpts...)
	if err != nil {
		return err
	}
	if err := config.CreateProject(path, name, proj); err != nil {
		return err
	}
	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderProjectMutation("Created", name)
}

func (a *App) runProjectModify(cmd *cobra.Command, args []string) error {
	name := args[0]
	mut, err := parseProjectModify(args[1:])
	if err != nil {
		return err
	}
	path, err := config.ConfigFilePath(a.loadOpts...)
	if err != nil {
		return err
	}
	if err := config.ModifyProject(path, name, mut); err != nil {
		return err
	}
	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderProjectMutation("Modified", name)
}

func (a *App) runProjectDelete(cmd *cobra.Command, args []string, force bool) error {
	name := args[0]
	path, err := config.ConfigFilePath(a.loadOpts...)
	if err != nil {
		return err
	}
	checker := func(projectName string) (int, error) {
		return a.countTasksForProject(cmd.Context(), projectName)
	}
	if err := config.DeleteProject(path, name, checker, force); err != nil {
		return err
	}
	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderProjectMutation("Deleted", name)
}

// countTasksForProject returns the number of tasks referencing the given
// project. Used by runProjectDelete's safety check.
func (a *App) countTasksForProject(ctx context.Context, projectName string) (int, error) {
	expr, err := filter.Parse(fmt.Sprintf("project=%s", projectName))
	if err != nil {
		return 0, fmt.Errorf("building filter: %w", err)
	}
	tasks, err := a.taskSvc.List(ctx, expr)
	if err != nil {
		return 0, err
	}
	return len(tasks), nil
}
```

**Caveat:** before committing, confirm two call-site details against the current codebase:
1. `filter.Parse` — the existing caller `runList` in `internal/tui/commands.go` already parses a filter string. Use the identical expression type that `taskSvc.List` accepts. If the function is named differently (e.g. `filter.ParseExpr`), use that name. Grep: `grep -n 'taskSvc.List' internal/tui/commands.go` to see the exact type.
2. `a.loadOpts`, `a.format`, `a.colorEnabled()` — verify these exist on the `App` struct by reading `internal/tui/workflow.go:85-103` which already uses them.

- [ ] **Step 3: Wire the command onto the root**

Open the file identified in Step 1. Find the line that calls `a.buildWorkflowCmd()` and add immediately below it:

```go
rootCmd.AddCommand(a.buildProjectCmd())
```

(Use the exact variable name that already holds the root command in that file — it may be `rootCmd`, `cmd`, or `root`.)

- [ ] **Step 4: Build and smoke-test**

```bash
make build
cp /dev/null /tmp/tusk-smoke.toml && cat > /tmp/tusk-smoke.toml <<'EOF'
[workflows.kanban.statuses.pending]
roles = ["initial"]
[workflows.kanban.statuses.active]
roles = ["start"]
[workflows.kanban.statuses.done]
roles = ["terminal", "done"]
[[workflows.kanban.transitions]]
from = "pending"
to = "active"
[[workflows.kanban.transitions]]
from = "active"
to = "done"
[projects.default]
workflow = "kanban"
EOF
TUSK_CONFIG=/tmp/tusk-smoke.toml ./bin/tusk project create backend workflow=kanban db-path=/tmp/tusk-backend.db
TUSK_CONFIG=/tmp/tusk-smoke.toml ./bin/tusk project list
TUSK_CONFIG=/tmp/tusk-smoke.toml ./bin/tusk project modify backend +urgency.blocking-weight=2
TUSK_CONFIG=/tmp/tusk-smoke.toml ./bin/tusk project delete backend
```

Expected: each command prints a success line, `list` shows `backend`, and the final `delete` removes it.

(If the env var is not `TUSK_CONFIG`, grep `config/config.go` for the real env var name — likely `TUSK_CONFIG_DIR` pointing at a directory. Adjust accordingly.)

- [ ] **Step 5: Commit**

```bash
git add internal/tui/project.go internal/tui/app.go
git commit -m "feat(tui): add tusk project command group"
```

---

### Task 4: E2E tests for project CRUD

**Files:**
- Create: `tests/e2e/project_crud_test.go`

- [ ] **Step 1: Inspect the harness**

```bash
grep -n 'type Scenario' tests/e2e/harness.go
grep -n 'type Step' tests/e2e/harness.go
grep -n 'ExpectError\|ExpectContains\|Capture' tests/e2e/harness.go
```

Expected: the field names used by existing e2e files. Use them verbatim in Step 2 — replace placeholders below with the real names if they differ.

- [ ] **Step 2: Write the scenarios**

```go
package e2e

import "testing"

func TestProjectCRUD(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "create_and_list",
			Steps: []Step{
				{Args: []string{"project", "create", "backend", "workflow=kanban"}},
				{Args: []string{"project", "list"}, ExpectContains: []string{"backend"}},
			},
		},
		{
			Name: "modify_urgency_delta",
			Steps: []Step{
				{Args: []string{"project", "create", "backend", "workflow=kanban"}},
				{Args: []string{"project", "modify", "backend", "+urgency.blocking-weight=2"}},
				{Args: []string{"config", "get", "projects.backend.settings.urgency.blocking_weight"}},
			},
		},
		{
			Name: "delete_rejects_default",
			Steps: []Step{
				{Args: []string{"project", "delete", "default"}, ExpectError: true, ExpectContains: []string{"default"}},
			},
		},
		{
			Name: "delete_rejects_when_referenced",
			Steps: []Step{
				{Args: []string{"project", "create", "backend", "workflow=kanban"}},
				{Args: []string{"add", "task in backend", "project=backend"}},
				{Args: []string{"project", "delete", "backend"}, ExpectError: true, ExpectContains: []string{"1"}},
			},
		},
		{
			Name: "delete_force_with_refs",
			Steps: []Step{
				{Args: []string{"project", "create", "backend", "workflow=kanban"}},
				{Args: []string{"add", "task in backend", "project=backend"}},
				{Args: []string{"project", "delete", "backend", "--force"}},
			},
		},
	}
	RunScenarios(t, scenarios)
}
```

- [ ] **Step 3: Run and iterate until green**

```bash
go test ./tests/e2e -run TestProjectCRUD -v
make test test-race test-e2e vet lint
```

Expected: all pass.

- [ ] **Step 4: Commit**

```bash
git add tests/e2e/project_crud_test.go
git commit -m "test(e2e): cover tusk project CRUD scenarios"
```

---

## Changes Introduced

**New files:**
- `internal/tui/project.go`
- `internal/tui/project_parse.go`
- `internal/tui/project_parse_test.go`
- `tests/e2e/project_crud_test.go`

**Modified files:**
- `internal/tui/render.go` — added `renderProjectMutation(action, name string) error` method on `*Renderer`
- `internal/tui/app.go` — root command now registers `a.buildProjectCmd()`

**New CLI surface:**
- `tusk project create <name> [fields...]`
- `tusk project modify <name> [fields...]`
- `tusk project delete <name> [--force]`

**Bridge code:** None.

**Runtime behavior:** `db_path` is accepted and written to the TOML config but has no effect on storage — all services still operate against the single default store. Phase 3 introduces the store registry that honors `db_path`.

**Behavioral guarantees for downstream phases:**
- `countTasksForProject` uses `a.taskSvc.List` with a `project=<name>` filter. Phase 4's fan-out refactor of `List` must preserve the semantics of filtering by project name so this callback keeps returning the right count.
- The parser writes urgency keys in TOML form (`blocking_weight`, etc.) into `ProjectMutation.UrgencySet` / `UrgencyDelta`. Phase 3/4 code that reads these maps can rely on the TOML-form key convention.
- The parser accepts `urgency.<key>` only — Phase 3/4 should not introduce new field names that silently alias different keys in this namespace.
