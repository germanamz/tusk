# Phase 1 — Project Config CRUD Helpers

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Initiative:** Project Management CLI (ROADMAP.md:543-564)
**Phase:** 1 of 4
**Prerequisites:** None (runs against the base codebase as of commit `e41f7a9`).

---

## Goal

Add exported helpers on the `config` package that let callers create, modify, and delete projects in a config file, including the new `db_path` field and arithmetic delta resolution for per-project urgency weights. **Pure library API — no CLI commands, no runtime behavior changes.** Ships independently as a silent extension of the public API.

---

## User-Visible Behaviors Preserved

This phase is a pure additive change. Every pre-existing behavior must still work after this phase:

- `tusk add/list/info/modify/start/done/delete/claim/pop/available/next` — unchanged
- `tusk workflow list/info/create/modify/delete` — unchanged
- `tusk config show/get/set/edit/validate` — unchanged
- `tusk project list` — unchanged
- Loading a config file that does not set `db_path` on any project — produces an equivalent `Config` struct with empty `DBPath` strings (backwards compatible)
- `config.LoadFile` / `config.WriteConfig` / `config.Validate` — unchanged semantics

Acceptance: `make test test-race test-e2e vet lint` must all pass at the end of Phase 1.

---

## File Structure

**Create:**
- `config/project.go` — `CreateProject`, `ModifyProject`, `DeleteProject`, `ProjectMutation`, `ResolveWeightDelta`, `TaskRefChecker`, `DefaultProjectID`, internal helpers `urgencyFieldPtr` and `globalWeight`
- `config/project_test.go` — unit tests for all helpers

**Modify:**
- `config/config.go` — add `DBPath` field to `ProjectConfig` (currently lines 86-89)

---

## Tasks

### Task 1: Add `DBPath` to `ProjectConfig`

**Files:**
- Modify: `config/config.go:86-89`
- Test: `config/config_test.go` (append)

- [ ] **Step 1: Write the failing test**

Append to `config/config_test.go`:

```go
func TestProjectConfig_DBPathRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `
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

[projects.backend]
workflow = "kanban"
db_path = "/data/backend.db"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if got := cfg.Projects["backend"].DBPath; got != "/data/backend.db" {
		t.Fatalf("expected backend.db_path=/data/backend.db, got %q", got)
	}
	if got := cfg.Projects["default"].DBPath; got != "" {
		t.Fatalf("expected default.db_path empty, got %q", got)
	}
}
```

- [ ] **Step 2: Run and confirm failure**

```bash
go test ./config -run TestProjectConfig_DBPathRoundTrip -v
```

Expected: FAIL — field `DBPath` not defined.

- [ ] **Step 3: Add the field**

Replace `config/config.go:86-89` with:

```go
// ProjectConfig defines a named project with its workflow assignment and settings.
type ProjectConfig struct {
	Workflow string                `mapstructure:"workflow" toml:"workflow"`
	DBPath   string                `mapstructure:"db_path"  toml:"db_path,omitempty"`
	Settings ProjectSettingsConfig `mapstructure:"settings" toml:"settings"`
}
```

- [ ] **Step 4: Re-run test + full package**

```bash
go test ./config/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add config/config.go config/config_test.go
git commit -m "feat(config): add db_path field to ProjectConfig"
```

---

### Task 2: `CreateProject` + `DeleteProject` helpers

**Files:**
- Create: `config/project.go`
- Create: `config/project_test.go`

- [ ] **Step 1: Write the failing tests**

Create `config/project_test.go`:

```go
package config

import (
	"strings"
	"testing"
)

func TestCreateProject(t *testing.T) {
	path := writeTestConfig(t, baseConfig)

	proj := ProjectConfig{Workflow: "kanban", DBPath: "/tmp/b.db"}
	if err := CreateProject(path, "backend", proj); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	got, ok := cfg.Projects["backend"]
	if !ok {
		t.Fatal("expected backend project in config")
	}
	if got.Workflow != "kanban" || got.DBPath != "/tmp/b.db" {
		t.Fatalf("unexpected project: %+v", got)
	}
	if _, ok := cfg.Projects["default"]; !ok {
		t.Fatal("expected default project preserved")
	}
}

func TestCreateProject_AlreadyExists(t *testing.T) {
	path := writeTestConfig(t, baseConfig)
	err := CreateProject(path, "default", ProjectConfig{Workflow: "kanban"})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected already-exists error, got %v", err)
	}
}

func TestCreateProject_UnknownWorkflow(t *testing.T) {
	path := writeTestConfig(t, baseConfig)
	err := CreateProject(path, "backend", ProjectConfig{Workflow: "ghost"})
	if err == nil || !strings.Contains(err.Error(), "invalid config") {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestDeleteProject_NotFound(t *testing.T) {
	path := writeTestConfig(t, baseConfig)
	err := DeleteProject(path, "ghost", func(string) (int, error) { return 0, nil }, false)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

func TestDeleteProject_RejectsDefault(t *testing.T) {
	path := writeTestConfig(t, baseConfig)
	err := DeleteProject(path, DefaultProjectID, func(string) (int, error) { return 0, nil }, false)
	if err == nil || !strings.Contains(err.Error(), "default") {
		t.Fatalf("expected default-guard error, got %v", err)
	}
}

func TestDeleteProject_ForceRemovesDefault(t *testing.T) {
	path := writeTestConfig(t, baseConfig)
	if err := DeleteProject(path, DefaultProjectID, func(string) (int, error) { return 0, nil }, true); err != nil {
		t.Fatalf("DeleteProject force: %v", err)
	}
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if _, ok := cfg.Projects[DefaultProjectID]; ok {
		t.Fatal("expected default removed")
	}
}

func TestDeleteProject_RejectsReferenced(t *testing.T) {
	path := writeTestConfig(t, baseConfig)
	if err := CreateProject(path, "backend", ProjectConfig{Workflow: "kanban"}); err != nil {
		t.Fatalf("setup: %v", err)
	}
	err := DeleteProject(path, "backend",
		func(name string) (int, error) { return 3, nil }, false)
	if err == nil || !strings.Contains(err.Error(), "3") {
		t.Fatalf("expected refs error with count, got %v", err)
	}
}

func TestDeleteProject_Force(t *testing.T) {
	path := writeTestConfig(t, baseConfig)
	if err := CreateProject(path, "backend", ProjectConfig{Workflow: "kanban"}); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := DeleteProject(path, "backend",
		func(string) (int, error) { return 3, nil }, true); err != nil {
		t.Fatalf("force delete: %v", err)
	}
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if _, ok := cfg.Projects["backend"]; ok {
		t.Fatal("expected backend removed")
	}
}
```

The test file references `writeTestConfig` and `baseConfig` — both already defined in `config/workflow_test.go` (same package). Do not redefine them.

- [ ] **Step 2: Run and confirm failure**

```bash
go test ./config -run "TestCreateProject|TestDeleteProject" -v
```

Expected: FAIL — `CreateProject`, `DeleteProject`, `DefaultProjectID` undefined.

- [ ] **Step 3: Create `config/project.go` with Create + Delete**

```go
package config

import "fmt"

// DefaultProjectID is the name of the built-in project that ships with Tusk.
const DefaultProjectID = "default"

// TaskRefChecker reports how many tasks reference a project by name.
// Passed to DeleteProject so the config package stays free of
// service/repository imports.
type TaskRefChecker func(projectName string) (int, error)

// CreateProject adds a new project to the config file.
// Returns error if the name already exists or validation fails.
func CreateProject(path string, name string, proj ProjectConfig) error {
	cfg, err := LoadFile(path)
	if err != nil {
		return err
	}
	if cfg.Projects == nil {
		cfg.Projects = make(map[string]ProjectConfig)
	}
	if _, exists := cfg.Projects[name]; exists {
		return fmt.Errorf("project %q already exists", name)
	}
	cfg.Projects[name] = proj
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	return WriteConfig(cfg, path)
}

// DeleteProject removes a project from the config file.
// Rejects the built-in default project and any project with task
// references unless force is true.
func DeleteProject(path string, name string, hasRefs TaskRefChecker, force bool) error {
	cfg, err := LoadFile(path)
	if err != nil {
		return err
	}
	if _, exists := cfg.Projects[name]; !exists {
		return fmt.Errorf("project %q: not found", name)
	}
	if name == DefaultProjectID && !force {
		return fmt.Errorf("cannot delete built-in %q project (use --force to override)", DefaultProjectID)
	}
	if hasRefs != nil {
		count, err := hasRefs(name)
		if err != nil {
			return fmt.Errorf("checking task references: %w", err)
		}
		if count > 0 && !force {
			return fmt.Errorf("project %q has %d referencing task(s) (use --force to override)", name, count)
		}
	}
	delete(cfg.Projects, name)
	return WriteConfig(cfg, path)
}
```

- [ ] **Step 4: Re-run tests**

```bash
go test ./config -run "TestCreateProject|TestDeleteProject" -v
```

Expected: PASS (all eight cases).

- [ ] **Step 5: Commit**

```bash
git add config/project.go config/project_test.go
git commit -m "feat(config): add CreateProject and DeleteProject helpers"
```

---

### Task 3: `ResolveWeightDelta` helper

Numeric urgency deltas must apply relative to the *effective* weight: the project's current override if set, otherwise the global weight.

**Files:**
- Modify: `config/project.go`
- Modify: `config/project_test.go`

- [ ] **Step 1: Write the failing test**

Append to `config/project_test.go`:

```go
func TestResolveWeightDelta(t *testing.T) {
	globalWeight := 5.0
	cases := []struct {
		name     string
		override *float64
		delta    float64
		want     float64
	}{
		{"no override, add 2", nil, 2.0, 7.0},
		{"no override, subtract 1", nil, -1.0, 4.0},
		{"override=10, add 2", floatPtr(10), 2.0, 12.0},
		{"override=10, subtract 3", floatPtr(10), -3.0, 7.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveWeightDelta(globalWeight, tc.override, tc.delta)
			if got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func floatPtr(v float64) *float64 { return &v }
```

- [ ] **Step 2: Run and confirm failure**

```bash
go test ./config -run TestResolveWeightDelta -v
```

Expected: FAIL — `ResolveWeightDelta` undefined.

- [ ] **Step 3: Implement**

Append to `config/project.go`:

```go
// ResolveWeightDelta returns the new per-project urgency weight after
// applying a delta relative to the effective current value. When
// override is nil, the delta is applied against the global weight.
func ResolveWeightDelta(globalWeight float64, override *float64, delta float64) float64 {
	base := globalWeight
	if override != nil {
		base = *override
	}
	return base + delta
}
```

- [ ] **Step 4: Re-run test**

```bash
go test ./config -run TestResolveWeightDelta -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add config/project.go config/project_test.go
git commit -m "feat(config): add ResolveWeightDelta helper"
```

---

### Task 4: `ProjectMutation` struct + internal urgency helpers

**Files:**
- Modify: `config/project.go`

This task adds the data structures needed by Task 5. It does not ship a public function yet, so there is no direct failing test — the failing tests land in Task 5. This task exists as a separate commit only because the struct and helpers are reused inside `ModifyProject` and are also referenced by Phase 2's parser (see "Changes Introduced" below).

- [ ] **Step 1: Append the struct and helpers to `config/project.go`**

```go
// ProjectMutation describes changes to apply to an existing project.
// Pointer fields: nil = don't change, non-nil = set (empty string clears db_path).
// UrgencySet and UrgencyDelta share the same key namespace. Applying both
// a set and a delta for the same key is rejected at apply time.
type ProjectMutation struct {
	Workflow        *string
	DBPath          *string
	AutoCompleteSet *AutoCompleteParentConfig
	AutoRevertSet   *AutoRevertParentConfig
	UrgencySet      map[string]float64
	UrgencyDelta    map[string]float64
}

// urgencyFieldPtr returns the **float64 slot on a ProjectUrgencyConfig for
// a given TOML/struct key. Returns nil on unknown keys. Exported as a
// package-internal helper so parsers in internal/tui can reuse it.
func urgencyFieldPtr(u *ProjectUrgencyConfig, key string) **float64 {
	switch key {
	case "priority_weight":
		return &u.PriorityWeight
	case "due_weight":
		return &u.DueWeight
	case "age_weight":
		return &u.AgeWeight
	case "active_weight":
		return &u.ActiveWeight
	case "blocking_weight":
		return &u.BlockingWeight
	case "blocked_weight":
		return &u.BlockedWeight
	case "tags_weight":
		return &u.TagsWeight
	case "project_weight":
		return &u.ProjectWeight
	case "annotations_weight":
		return &u.AnnotationsWeight
	case "waiting_weight":
		return &u.WaitingWeight
	}
	return nil
}

// UrgencyFieldPtr is the exported wrapper over urgencyFieldPtr.
// Phase 2's parser needs this from outside the config package.
func UrgencyFieldPtr(u *ProjectUrgencyConfig, key string) **float64 {
	return urgencyFieldPtr(u, key)
}

// globalWeight returns the global urgency weight for a config key.
func globalWeight(g UrgencyConfig, key string) (float64, bool) {
	switch key {
	case "priority_weight":
		return g.PriorityWeight, true
	case "due_weight":
		return g.DueWeight, true
	case "age_weight":
		return g.AgeWeight, true
	case "active_weight":
		return g.ActiveWeight, true
	case "blocking_weight":
		return g.BlockingWeight, true
	case "blocked_weight":
		return g.BlockedWeight, true
	case "tags_weight":
		return g.TagsWeight, true
	case "project_weight":
		return g.ProjectWeight, true
	case "annotations_weight":
		return g.AnnotationsWeight, true
	case "waiting_weight":
		return g.WaitingWeight, true
	}
	return 0, false
}
```

- [ ] **Step 2: Build to confirm it compiles**

```bash
go build ./config/...
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add config/project.go
git commit -m "feat(config): add ProjectMutation struct and urgency helpers"
```

---

### Task 5: `ModifyProject` applier

**Files:**
- Modify: `config/project.go`
- Modify: `config/project_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `config/project_test.go`:

```go
func TestModifyProject_SetWorkflow(t *testing.T) {
	path := writeTestConfig(t, baseConfig+`
[projects.backend]
workflow = "kanban"
`)
	mut := ProjectMutation{Workflow: strPtr("kanban")}
	if err := ModifyProject(path, "backend", mut); err != nil {
		t.Fatalf("ModifyProject: %v", err)
	}
	cfg, _ := LoadFile(path)
	if cfg.Projects["backend"].Workflow != "kanban" {
		t.Fatalf("workflow not updated: %+v", cfg.Projects["backend"])
	}
}

func TestModifyProject_SetAndClearDBPath(t *testing.T) {
	path := writeTestConfig(t, baseConfig+`
[projects.backend]
workflow = "kanban"
db_path = "/old.db"
`)
	mut := ProjectMutation{DBPath: strPtr("/new.db")}
	if err := ModifyProject(path, "backend", mut); err != nil {
		t.Fatalf("set: %v", err)
	}
	cfg, _ := LoadFile(path)
	if got := cfg.Projects["backend"].DBPath; got != "/new.db" {
		t.Fatalf("expected /new.db, got %q", got)
	}

	empty := ""
	mut = ProjectMutation{DBPath: &empty}
	if err := ModifyProject(path, "backend", mut); err != nil {
		t.Fatalf("clear: %v", err)
	}
	cfg, _ = LoadFile(path)
	if got := cfg.Projects["backend"].DBPath; got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestModifyProject_UrgencyAbsolute(t *testing.T) {
	path := writeTestConfig(t, baseConfig+`
[projects.backend]
workflow = "kanban"
`)
	mut := ProjectMutation{
		UrgencySet: map[string]float64{"blocking_weight": 15},
	}
	if err := ModifyProject(path, "backend", mut); err != nil {
		t.Fatalf("ModifyProject: %v", err)
	}
	cfg, _ := LoadFile(path)
	u := cfg.Projects["backend"].Settings.Urgency
	if u == nil || u.BlockingWeight == nil || *u.BlockingWeight != 15 {
		t.Fatalf("expected blocking_weight=15 override, got %+v", u)
	}
}

func TestModifyProject_UrgencyDeltaFromGlobal(t *testing.T) {
	path := writeTestConfig(t, baseConfig+`
[projects.backend]
workflow = "kanban"
`)
	cfg, _ := LoadFile(path)
	globalBlocking := cfg.Urgency.BlockingWeight

	mut := ProjectMutation{UrgencyDelta: map[string]float64{"blocking_weight": 2}}
	if err := ModifyProject(path, "backend", mut); err != nil {
		t.Fatalf("ModifyProject: %v", err)
	}
	cfg, _ = LoadFile(path)
	got := *cfg.Projects["backend"].Settings.Urgency.BlockingWeight
	if got != globalBlocking+2 {
		t.Fatalf("expected %v, got %v", globalBlocking+2, got)
	}
}

func TestModifyProject_UrgencyDeltaStacks(t *testing.T) {
	path := writeTestConfig(t, baseConfig+`
[projects.backend]
workflow = "kanban"
[projects.backend.settings.urgency]
blocking_weight = 10.0
`)
	mut := ProjectMutation{UrgencyDelta: map[string]float64{"blocking_weight": 2}}
	if err := ModifyProject(path, "backend", mut); err != nil {
		t.Fatalf("ModifyProject: %v", err)
	}
	cfg, _ := LoadFile(path)
	got := *cfg.Projects["backend"].Settings.Urgency.BlockingWeight
	if got != 12 {
		t.Fatalf("expected 12, got %v", got)
	}
}

func TestModifyProject_SetAndDeltaConflict(t *testing.T) {
	path := writeTestConfig(t, baseConfig+`
[projects.backend]
workflow = "kanban"
`)
	mut := ProjectMutation{
		UrgencySet:   map[string]float64{"blocking_weight": 10},
		UrgencyDelta: map[string]float64{"blocking_weight": 2},
	}
	err := ModifyProject(path, "backend", mut)
	if err == nil || !strings.Contains(err.Error(), "blocking_weight") {
		t.Fatalf("expected conflict error, got %v", err)
	}
}

func TestModifyProject_UnknownUrgencyKey(t *testing.T) {
	path := writeTestConfig(t, baseConfig+`
[projects.backend]
workflow = "kanban"
`)
	mut := ProjectMutation{UrgencySet: map[string]float64{"ghost": 1}}
	err := ModifyProject(path, "backend", mut)
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("expected unknown-key error, got %v", err)
	}
}

func strPtr(s string) *string { return &s }
```

- [ ] **Step 2: Run and confirm failure**

```bash
go test ./config -run TestModifyProject -v
```

Expected: FAIL — `ModifyProject` undefined.

- [ ] **Step 3: Implement `ModifyProject`**

Append to `config/project.go`:

```go
// ModifyProject applies a mutation to an existing project in the config file.
func ModifyProject(path string, name string, mut ProjectMutation) error {
	cfg, err := LoadFile(path)
	if err != nil {
		return err
	}
	proj, exists := cfg.Projects[name]
	if !exists {
		return fmt.Errorf("project %q: not found", name)
	}

	if mut.Workflow != nil {
		proj.Workflow = *mut.Workflow
	}
	if mut.DBPath != nil {
		proj.DBPath = *mut.DBPath
	}
	if mut.AutoCompleteSet != nil {
		ac := *mut.AutoCompleteSet
		proj.Settings.AutoCompleteParent = &ac
	}
	if mut.AutoRevertSet != nil {
		ar := *mut.AutoRevertSet
		proj.Settings.AutoRevertParent = &ar
	}

	// Reject conflicting set+delta on the same key.
	for k := range mut.UrgencySet {
		if _, dup := mut.UrgencyDelta[k]; dup {
			return fmt.Errorf("urgency key %q has both absolute and delta", k)
		}
	}

	if len(mut.UrgencySet) > 0 || len(mut.UrgencyDelta) > 0 {
		if proj.Settings.Urgency == nil {
			proj.Settings.Urgency = &ProjectUrgencyConfig{}
		}
		for k, v := range mut.UrgencySet {
			fp := urgencyFieldPtr(proj.Settings.Urgency, k)
			if fp == nil {
				return fmt.Errorf("unknown urgency key %q", k)
			}
			val := v
			*fp = &val
		}
		for k, delta := range mut.UrgencyDelta {
			fp := urgencyFieldPtr(proj.Settings.Urgency, k)
			if fp == nil {
				return fmt.Errorf("unknown urgency key %q", k)
			}
			gw, ok := globalWeight(cfg.Urgency, k)
			if !ok {
				return fmt.Errorf("unknown urgency key %q", k)
			}
			val := ResolveWeightDelta(gw, *fp, delta)
			*fp = &val
		}
	}

	cfg.Projects[name] = proj
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	return WriteConfig(cfg, path)
}
```

- [ ] **Step 4: Re-run tests**

```bash
go test ./config -run TestModifyProject -v
make test test-race vet lint
```

Expected: PASS on every target.

- [ ] **Step 5: Commit**

```bash
git add config/project.go config/project_test.go
git commit -m "feat(config): add ModifyProject with urgency absolute/delta semantics"
```

---

## Changes Introduced

**New files:**
- `config/project.go`
- `config/project_test.go`

**Modified files:**
- `config/config.go` — added `DBPath string` field to `ProjectConfig` (new TOML key `db_path` on each `[projects.<name>]` table, omit-empty)

**New exported API (`config` package):**
- `const DefaultProjectID = "default"`
- `type TaskRefChecker func(projectName string) (int, error)`
- `type ProjectMutation struct { Workflow, DBPath *string; AutoCompleteSet *AutoCompleteParentConfig; AutoRevertSet *AutoRevertParentConfig; UrgencySet, UrgencyDelta map[string]float64 }`
- `func CreateProject(path string, name string, proj ProjectConfig) error`
- `func DeleteProject(path string, name string, hasRefs TaskRefChecker, force bool) error`
- `func ModifyProject(path string, name string, mut ProjectMutation) error`
- `func ResolveWeightDelta(globalWeight float64, override *float64, delta float64) float64`
- `func UrgencyFieldPtr(u *ProjectUrgencyConfig, key string) **float64`

**New config schema:**
- `[projects.<name>].db_path` (optional string, omit-empty) — currently parsed and round-tripped but not read by the runtime. Phase 3 adds the `StoreRegistry` that honors it.

**Bridge code:** None.

**Migrations / env vars / dependencies:** None added.

**Behavioral guarantees for downstream phases:**
- Writing a `ProjectConfig` with empty `DBPath` still round-trips as omit-empty TOML — existing config files are unchanged on read/write cycles.
- `CreateProject("backend", {Workflow: "ghost"})` fails with `invalid config:` prefix — Phase 2 parser tests rely on this.
- `DeleteProject` returns the task reference count inside the error message as a bare integer — Phase 2 e2e assertions match on `"1"`/`"3"`.
