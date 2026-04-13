# Phase 1 — MCP Config Plumbing & Hot-Reload Infrastructure

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the MCP server the plumbing it needs to (a) resolve the active
config file path using the same walk-up discovery rules as the CLI and (b)
hot-reload workflow, project, and urgency state after the upcoming phases
mutate the config file. No new MCP tools are added in this phase. The binary
builds, `tusk config ...` / `tusk workflow ...` / `tusk project ...` CLI
commands still work, and `tusk mcp serve` still starts with exactly the
pre-existing tool set.

**Architecture:** Two in-memory repositories (`inmem.WorkflowRepository`,
`inmem.ProjectRepository`) and one service (`service.UrgencyEngine`) gain a
`Reload` method that atomically replaces their internal state while holding an
`sync.RWMutex`. A new `reloadConfig(ctx)` helper on `mcp.Server` calls
`config.Load` with the server's stored `loadOpts` and fans that reload out to
the three components. The server constructor gains a `loadOpts []config.Option`
parameter, threaded from `tui.App.New`. The helper is added but never called
in this phase — its first caller lands in phase 2.

**Tech Stack:** Go 1.23+, existing `config`, `inmem`, `service`, `internal/mcp`
packages. No new third-party dependencies.

**Prerequisites:** None beyond the current `main` branch.

## Inherits From

This is phase 1; it operates on the codebase at the tip of `main` with the
v0.9 *Local Config Discovery* initiative already complete. Relevant current
state:

- `tui.App` holds `loadOpts []config.Option` (`internal/tui/app.go:39`) which
  represents the walk-up / explicit-file / search-path configuration used by
  every CLI config mutation today.
- `tuskmcp.New(taskSvc, tagSvc, relationSvc, projectSvc, workflowSvc,
  playerSvc, version, mcpCfg)` is called from `internal/tui/app.go:106` inside
  the `mcp serve` RunE.
- `inmem.NewWorkflowRepository` and `inmem.NewProjectRepository` are
  constructed once in `cmd/tusk/main.go:75-76` from `cfg.Workflows` /
  `cfg.Projects` and are read-only.
- `service.NewUrgencyEngine(defaults UrgencyWeights)` stores `defaults` in a
  bare struct field (`service/urgency.go:60-67`) with no synchronization; it
  is called from `Score()` on every task ranking request.

## User-Visible Behavior (must still work)

- `tusk config show|path|init|set|get|validate|edit` unchanged.
- `tusk workflow list|info|create|modify|delete` unchanged.
- `tusk project list|create|modify|delete` unchanged.
- `tusk mcp serve` starts and exposes exactly the same tool list as before
  (the existing `tusk_task_*`, `tusk_relation_*`, `tusk_project_list`,
  `tusk_workflow_list`, `tusk_player_register` tools — no more, no fewer).
- All existing unit tests in `./config`, `./inmem`, `./service`, and
  `./internal/mcp` continue to pass, including with `-race`.

## Tasks

### Task 1: Thread-safe Reload on `inmem.WorkflowRepository`

**Files:**
- Modify: `inmem/workflow.go`
- Test: `inmem/workflow_test.go`

- [ ] **Step 1: Write the failing test**

Add a test to `inmem/workflow_test.go` that constructs a repo with one
workflow, calls `List`, asserts one entry, calls `Reload` with a different
config map (two workflows), then calls `List` again and asserts two entries
with the new names. Also verifies `GetByName` reflects the new set.

```go
func TestWorkflowRepository_Reload(t *testing.T) {
	repo := inmem.NewWorkflowRepository(map[string]config.WorkflowConfig{
		"alpha": {
			Statuses: map[string]config.StatusConfig{
				"pending": {Roles: []string{"initial"}},
				"active":  {Roles: []string{"start"}},
				"done":    {Roles: []string{"terminal", "done"}},
				"deleted": {Roles: []string{"terminal", "delete"}},
			},
			Transitions: []config.WorkflowTransitionConfig{{From: "pending", To: "active"}},
		},
	})

	got, err := repo.List(context.Background())
	if err != nil || len(got) != 1 || got[0].Name != "alpha" {
		t.Fatalf("pre-reload List: got %+v err=%v", got, err)
	}

	repo.Reload(map[string]config.WorkflowConfig{
		"beta": {
			Statuses: map[string]config.StatusConfig{
				"pending": {Roles: []string{"initial"}},
				"active":  {Roles: []string{"start"}},
				"done":    {Roles: []string{"terminal", "done"}},
				"deleted": {Roles: []string{"terminal", "delete"}},
			},
			Transitions: []config.WorkflowTransitionConfig{{From: "pending", To: "active"}},
		},
		"gamma": {
			Statuses: map[string]config.StatusConfig{
				"pending": {Roles: []string{"initial"}},
				"active":  {Roles: []string{"start"}},
				"done":    {Roles: []string{"terminal", "done"}},
				"deleted": {Roles: []string{"terminal", "delete"}},
			},
			Transitions: []config.WorkflowTransitionConfig{{From: "pending", To: "active"}},
		},
	})

	got, err = repo.List(context.Background())
	if err != nil || len(got) != 2 {
		t.Fatalf("post-reload List: got %+v err=%v", got, err)
	}
	names := []string{got[0].Name, got[1].Name}
	if names[0] != "beta" || names[1] != "gamma" {
		t.Fatalf("post-reload names: got %v, want [beta gamma]", names)
	}
	if _, err := repo.GetByName(context.Background(), "alpha"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("alpha should be gone after Reload, got err=%v", err)
	}
}
```

Add imports `"errors"` and `"github.com/germanamz/tusk/domain"` at the top of
the file if not already present.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./inmem -run TestWorkflowRepository_Reload -v`
Expected: FAIL — `repo.Reload undefined`.

- [ ] **Step 3: Add the `Reload` method and mutex-guard all access**

Edit `inmem/workflow.go` to introduce an `sync.RWMutex` and wrap every
`workflows` access:

```go
package inmem

import (
	"context"
	"sort"
	"sync"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
)

var _ repository.WorkflowRepository = (*WorkflowRepository)(nil)

type WorkflowRepository struct {
	mu        sync.RWMutex
	workflows map[string]*domain.Workflow
}

func NewWorkflowRepository(cfgWorkflows map[string]config.WorkflowConfig) *WorkflowRepository {
	return &WorkflowRepository{workflows: buildWorkflowMap(cfgWorkflows)}
}

// Reload atomically replaces the workflow set. Safe for concurrent readers.
func (r *WorkflowRepository) Reload(cfgWorkflows map[string]config.WorkflowConfig) {
	next := buildWorkflowMap(cfgWorkflows)
	r.mu.Lock()
	r.workflows = next
	r.mu.Unlock()
}

func buildWorkflowMap(cfgWorkflows map[string]config.WorkflowConfig) map[string]*domain.Workflow {
	workflows := make(map[string]*domain.Workflow, len(cfgWorkflows))
	for name, cfg := range cfgWorkflows {
		wf := &domain.Workflow{
			Name:        name,
			Statuses:    make(map[string]domain.StatusConfig, len(cfg.Statuses)),
			Transitions: make([]domain.WorkflowTransition, len(cfg.Transitions)),
		}
		for statusName, sc := range cfg.Statuses {
			roles := make([]domain.StatusRole, len(sc.Roles))
			for i, r := range sc.Roles {
				roles[i] = domain.StatusRole(r)
			}
			wf.Statuses[statusName] = domain.StatusConfig{Roles: roles}
		}
		for i, t := range cfg.Transitions {
			wf.Transitions[i] = domain.WorkflowTransition{
				FromStatus: t.From,
				ToStatus:   t.To,
			}
		}
		workflows[name] = wf
	}
	return workflows
}

func (r *WorkflowRepository) GetByName(_ context.Context, name string) (*domain.Workflow, error) {
	r.mu.RLock()
	wf, ok := r.workflows[name]
	r.mu.RUnlock()
	if !ok {
		return nil, domain.ErrNotFound
	}
	return copyWorkflow(wf), nil
}

func (r *WorkflowRepository) List(_ context.Context) ([]*domain.Workflow, error) {
	r.mu.RLock()
	result := make([]*domain.Workflow, 0, len(r.workflows))
	for _, wf := range r.workflows {
		result = append(result, copyWorkflow(wf))
	}
	r.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}
```

Keep `copyWorkflow` unchanged.

- [ ] **Step 4: Verify tests pass, including race**

Run: `go test ./inmem -run TestWorkflowRepository -v -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add inmem/workflow.go inmem/workflow_test.go
git commit -m "feat(inmem): hot-reload workflow repository"
```

### Task 2: Thread-safe Reload on `inmem.ProjectRepository`

**Files:**
- Modify: `inmem/project.go`
- Test: `inmem/project_test.go`

- [ ] **Step 1: Write the failing test**

Add a test mirroring Task 1's structure but for projects. The repo starts
with one project (`"alpha"` → workflow `"kanban"`); after `Reload` it has two
(`"beta"`, `"gamma"`), and `GetByID(alpha)` returns `domain.ErrNotFound`.

```go
func TestProjectRepository_Reload(t *testing.T) {
	repo := inmem.NewProjectRepository(map[string]config.ProjectConfig{
		"alpha": {Workflow: "kanban"},
	})

	got, err := repo.List(context.Background())
	if err != nil || len(got) != 1 || got[0].ID != "alpha" {
		t.Fatalf("pre-reload List: got %+v err=%v", got, err)
	}

	repo.Reload(map[string]config.ProjectConfig{
		"beta":  {Workflow: "kanban"},
		"gamma": {Workflow: "kanban"},
	})

	got, err = repo.List(context.Background())
	if err != nil || len(got) != 2 {
		t.Fatalf("post-reload List: got %+v err=%v", got, err)
	}
	if got[0].ID != "beta" || got[1].ID != "gamma" {
		t.Fatalf("post-reload IDs: got [%s %s], want [beta gamma]", got[0].ID, got[1].ID)
	}
	if _, err := repo.GetByID(context.Background(), "alpha"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("alpha should be gone after Reload, got err=%v", err)
	}
}
```

Add imports `"errors"` and `"github.com/germanamz/tusk/domain"` if not
present.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./inmem -run TestProjectRepository_Reload -v`
Expected: FAIL — `repo.Reload undefined`.

- [ ] **Step 3: Add `Reload` and guard every access with an `sync.RWMutex`**

Edit `inmem/project.go` analogously to Task 1:

```go
package inmem

import (
	"context"
	"sort"
	"sync"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
)

var _ repository.ProjectRepository = (*ProjectRepository)(nil)

type ProjectRepository struct {
	mu       sync.RWMutex
	projects map[string]*domain.Project
}

func NewProjectRepository(cfgProjects map[string]config.ProjectConfig) *ProjectRepository {
	return &ProjectRepository{projects: buildProjectMap(cfgProjects)}
}

// Reload atomically replaces the project set. Safe for concurrent readers.
func (r *ProjectRepository) Reload(cfgProjects map[string]config.ProjectConfig) {
	next := buildProjectMap(cfgProjects)
	r.mu.Lock()
	r.projects = next
	r.mu.Unlock()
}

func buildProjectMap(cfgProjects map[string]config.ProjectConfig) map[string]*domain.Project {
	projects := make(map[string]*domain.Project, len(cfgProjects))
	for id, cfg := range cfgProjects {
		p := &domain.Project{ID: id, Workflow: cfg.Workflow}
		if cfg.Settings.AutoCompleteParent != nil {
			p.Settings.AutoCompleteParent = &domain.AutoCompleteConfig{
				TriggerStatus: cfg.Settings.AutoCompleteParent.TriggerStatus,
				TargetStatus:  cfg.Settings.AutoCompleteParent.TargetStatus,
			}
		}
		if cfg.Settings.AutoRevertParent != nil {
			p.Settings.AutoRevertParent = &domain.AutoRevertConfig{
				TriggerStatus: cfg.Settings.AutoRevertParent.TriggerStatus,
				TargetStatus:  cfg.Settings.AutoRevertParent.TargetStatus,
			}
		}
		if cfg.Settings.Urgency != nil {
			p.Settings.Urgency = &domain.UrgencyOverrides{
				PriorityWeight:    cfg.Settings.Urgency.PriorityWeight,
				DueWeight:         cfg.Settings.Urgency.DueWeight,
				AgeWeight:         cfg.Settings.Urgency.AgeWeight,
				ActiveWeight:      cfg.Settings.Urgency.ActiveWeight,
				BlockingWeight:    cfg.Settings.Urgency.BlockingWeight,
				BlockedWeight:     cfg.Settings.Urgency.BlockedWeight,
				TagsWeight:        cfg.Settings.Urgency.TagsWeight,
				ProjectWeight:     cfg.Settings.Urgency.ProjectWeight,
				AnnotationsWeight: cfg.Settings.Urgency.AnnotationsWeight,
				WaitingWeight:     cfg.Settings.Urgency.WaitingWeight,
			}
		}
		projects[id] = p
	}
	return projects
}

func (r *ProjectRepository) GetByID(_ context.Context, id string) (*domain.Project, error) {
	r.mu.RLock()
	p, ok := r.projects[id]
	r.mu.RUnlock()
	if !ok {
		return nil, domain.ErrNotFound
	}
	return copyProject(p), nil
}

func (r *ProjectRepository) List(_ context.Context) ([]*domain.Project, error) {
	r.mu.RLock()
	result := make([]*domain.Project, 0, len(r.projects))
	for _, p := range r.projects {
		result = append(result, copyProject(p))
	}
	r.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result, nil
}
```

Keep `copyProject` unchanged.

- [ ] **Step 4: Verify tests pass, including race**

Run: `go test ./inmem -v -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add inmem/project.go inmem/project_test.go
git commit -m "feat(inmem): hot-reload project repository"
```

### Task 3: `Reload` on `service.UrgencyEngine`

**Files:**
- Modify: `service/urgency.go`
- Test: `service/urgency_test.go`

- [ ] **Step 1: Write the failing test**

Append to `service/urgency_test.go`:

```go
func TestUrgencyEngine_Reload(t *testing.T) {
	e := service.NewUrgencyEngine(service.UrgencyWeights{Priority: 10})
	task := &domain.Task{Priority: 4}
	ctx := service.ScoringContext{}

	before := e.Score(task, ctx)
	if before == 0 {
		t.Fatalf("expected non-zero score before reload")
	}

	e.Reload(service.UrgencyWeights{Priority: 0})
	after := e.Score(task, ctx)
	if after != 0 {
		t.Fatalf("expected zero score after reload, got %v", after)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./service -run TestUrgencyEngine_Reload -v`
Expected: FAIL — `e.Reload undefined`.

- [ ] **Step 3: Guard `defaults` with an RWMutex and add `Reload`**

Edit `service/urgency.go`. Add `sync` to imports.

```go
type UrgencyEngine struct {
	mu       sync.RWMutex
	defaults UrgencyWeights
}

func NewUrgencyEngine(defaults UrgencyWeights) *UrgencyEngine {
	return &UrgencyEngine{defaults: defaults}
}

// Reload atomically replaces the default weights used when a task's project
// has no per-project overrides. Safe for concurrent readers.
func (e *UrgencyEngine) Reload(defaults UrgencyWeights) {
	e.mu.Lock()
	e.defaults = defaults
	e.mu.Unlock()
}
```

Inside `weightsFor` (or wherever `e.defaults` is currently read), take the
read lock before the return path that uses `e.defaults`:

```go
func (e *UrgencyEngine) weightsFor(projectID string, ctx ScoringContext) UrgencyWeights {
	if pw, ok := ctx.ProjectWeights[projectID]; ok {
		return *pw
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.defaults
}
```

Do not introduce any other behavior changes.

- [ ] **Step 4: Verify tests pass with race detector**

Run: `go test ./service -run TestUrgency -v -race`
Expected: PASS, including the new `TestUrgencyEngine_Reload`.

- [ ] **Step 5: Commit**

```bash
git add service/urgency.go service/urgency_test.go
git commit -m "feat(service): hot-reload urgency engine defaults"
```

### Task 4: MCP server gains `loadOpts`, `projectRepo`, `workflowRepo`, `urgencyEngine`, and `reloadConfig`

**Files:**
- Modify: `internal/mcp/server.go`
- Modify: `internal/tui/app.go:98-113` (the `mcp serve` RunE)
- Modify: `cmd/tusk/main.go:75-130` (inject repo + engine handles into `tui.New`)
- Modify: `internal/tui/app.go:24-68` (App struct + `tui.New` signature)
- Test: `internal/mcp/server_test.go`

- [ ] **Step 1: Extend the `mcp.Server` struct with the reload dependencies**

Edit `internal/mcp/server.go`. Add imports `"github.com/germanamz/tusk/inmem"`
and — only if not already imported — `"github.com/germanamz/tusk/service"`.

```go
type Server struct {
	taskSvc        *service.TaskService
	tagSvc         *service.TagService
	relationSvc    *service.RelationService
	projectSvc     *service.ProjectService
	workflowSvc    *service.WorkflowService
	playerSvc      *service.PlayerService
	workflowRepo   *inmem.WorkflowRepository
	projectRepo    *inmem.ProjectRepository
	urgencyEngine  *service.UrgencyEngine
	server         *server.MCPServer
	cfg            config.MCPConfig
	loadOpts       []config.Option
	toolGroups     map[string]string
	resourceGroups map[string]string
}
```

Extend `New` to accept the new dependencies. Keep every existing tool
registration intact. The new parameters must be the last positional arguments
so that reading the call site stays linear:

```go
func New(
	taskSvc *service.TaskService,
	tagSvc *service.TagService,
	relationSvc *service.RelationService,
	projectSvc *service.ProjectService,
	workflowSvc *service.WorkflowService,
	playerSvc *service.PlayerService,
	workflowRepo *inmem.WorkflowRepository,
	projectRepo *inmem.ProjectRepository,
	urgencyEngine *service.UrgencyEngine,
	version string,
	cfg config.MCPConfig,
	loadOpts []config.Option,
) (*Server, error) {
	s := &Server{
		taskSvc:        taskSvc,
		tagSvc:         tagSvc,
		relationSvc:    relationSvc,
		projectSvc:     projectSvc,
		workflowSvc:    workflowSvc,
		playerSvc:      playerSvc,
		workflowRepo:   workflowRepo,
		projectRepo:    projectRepo,
		urgencyEngine:  urgencyEngine,
		cfg:            cfg,
		loadOpts:       loadOpts,
		toolGroups:     make(map[string]string),
		resourceGroups: make(map[string]string),
	}
	// ... existing body unchanged ...
}
```

- [ ] **Step 2: Add the `reloadConfig` helper**

At the bottom of `internal/mcp/server.go`, before `const serverInstructions`,
add:

```go
// reloadConfig re-reads the active config file via the stored loadOpts and
// hot-reloads the workflow repository, project repository, and urgency engine
// with the fresh data. It does NOT rebuild the MCP server, reopen the
// database, or reconfigure transports — those require a process restart.
//
// Safe to call from any MCP tool handler after a successful config mutation.
// Returns an error when Load fails; callers should surface the error back to
// the caller without applying partial state (Load is a full parse with
// validation, so there is no partial state to apply).
func (s *Server) reloadConfig(ctx context.Context) error {
	cfg, err := config.Load(s.loadOpts...)
	if err != nil {
		return fmt.Errorf("reloading config: %w", err)
	}
	s.workflowRepo.Reload(cfg.Workflows)
	s.projectRepo.Reload(cfg.Projects)
	s.urgencyEngine.Reload(service.UrgencyWeights{
		Priority:    cfg.Urgency.PriorityWeight,
		Due:         cfg.Urgency.DueWeight,
		Age:         cfg.Urgency.AgeWeight,
		Active:      cfg.Urgency.ActiveWeight,
		Blocking:    cfg.Urgency.BlockingWeight,
		Blocked:     cfg.Urgency.BlockedWeight,
		Tags:        cfg.Urgency.TagsWeight,
		Project:     cfg.Urgency.ProjectWeight,
		Annotations: cfg.Urgency.AnnotationsWeight,
		Waiting:     cfg.Urgency.WaitingWeight,
	})
	_ = ctx // ctx reserved for future per-call tracing
	return nil
}
```

If the field names on `service.UrgencyWeights` differ from the list above,
open `service/urgency.go` and use the exact field names declared there — do
not invent new names. The `config.UrgencyConfig → service.UrgencyWeights`
mapping is the same one `cmd/tusk/main.go` already uses when constructing
the engine at startup; copy that mapping verbatim rather than guessing.

- [ ] **Step 3: Update `tui.App` and `tui.New` to accept and hold the handles**

Edit `internal/tui/app.go`.

Extend the `App` struct:

```go
type App struct {
	taskSvc       *service.TaskService
	tagSvc        *service.TagService
	relationSvc   *service.RelationService
	projectSvc    *service.ProjectService
	workflowSvc   *service.WorkflowService
	playerSvc     *service.PlayerService
	workflowRepo  *inmem.WorkflowRepository
	projectRepo   *inmem.ProjectRepository
	urgencyEngine *service.UrgencyEngine
	playerID      string
	resolver      *filter.Resolver
	root          *cobra.Command
	format        string
	noColor       bool
	version       VersionInfo
	tuiCfg        config.TUIConfig
	mcpCfg        config.MCPConfig
	loadOpts      []config.Option
}
```

Add `"github.com/germanamz/tusk/inmem"` to the imports.

Extend `New`:

```go
func New(
	taskSvc *service.TaskService,
	tagSvc *service.TagService,
	relationSvc *service.RelationService,
	projectSvc *service.ProjectService,
	workflowSvc *service.WorkflowService,
	playerSvc *service.PlayerService,
	workflowRepo *inmem.WorkflowRepository,
	projectRepo *inmem.ProjectRepository,
	urgencyEngine *service.UrgencyEngine,
	vi VersionInfo,
	tuiCfg config.TUIConfig,
	mcpCfg config.MCPConfig,
	loadOpts []config.Option,
) *App {
	a := &App{
		// ... existing assignments ...
		workflowRepo:  workflowRepo,
		projectRepo:   projectRepo,
		urgencyEngine: urgencyEngine,
	}
	// ... rest unchanged ...
}
```

Update the `tuskmcp.New` call inside the `mcp serve` RunE so it passes the
new args:

```go
mcpServer, err := tuskmcp.New(
	taskSvc, tagSvc, relationSvc, projectSvc,
	a.workflowSvc, a.playerSvc,
	a.workflowRepo, a.projectRepo, a.urgencyEngine,
	vi.Version, a.mcpCfg, a.loadOpts,
)
```

- [ ] **Step 4: Update `cmd/tusk/main.go` to pass the new handles into `tui.New`**

Locate the call to `tui.New` (currently around `cmd/tusk/main.go:130`). Pass
the existing `projectRepo`, `workflowRepo`, and `urgencyEngine` local
variables (they are already constructed above that line). If any of those
identifiers are named differently in the current file, use the current
names — do not rename them in this phase. The final call should be:

```go
app := tui.New(
	taskSvc, tagSvc, relationSvc, projectSvc, workflowSvc, playerSvc,
	workflowRepo, projectRepo, urgencyEngine,
	tui.VersionInfo{Version: version, Commit: commit, Date: date},
	cfg.TUI, cfg.MCP, loadOpts,
)
```

If `loadOpts` is not yet a local variable here, find where the rest of the
code already constructs it (every walk-up-aware call in the file uses the
same builder) and reuse that slice.

- [ ] **Step 5: Add a smoke test for the new MCP constructor signature and reload helper**

Append to `internal/mcp/server_test.go`:

```go
func TestServer_ReloadConfig_SmokeTest(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "tusk.toml")
	if err := os.WriteFile(configPath, minimalConfigTOML(), 0o644); err != nil {
		t.Fatalf("writing seed config: %v", err)
	}

	workflowRepo := inmem.NewWorkflowRepository(map[string]config.WorkflowConfig{})
	projectRepo := inmem.NewProjectRepository(map[string]config.ProjectConfig{})
	urgencyEngine := service.NewUrgencyEngine(service.UrgencyWeights{})

	srv, err := mcp.New(
		nil, nil, nil, nil, nil, nil,
		workflowRepo, projectRepo, urgencyEngine,
		"test", config.MCPConfig{},
		[]config.Option{config.WithExplicitFile(configPath)},
	)
	if err != nil {
		t.Fatalf("mcp.New: %v", err)
	}

	if err := srv.ReloadConfigForTest(context.Background()); err != nil {
		t.Fatalf("reloadConfig: %v", err)
	}

	wfs, err := workflowRepo.List(context.Background())
	if err != nil || len(wfs) == 0 {
		t.Fatalf("post-reload workflows: got %+v err=%v", wfs, err)
	}
}
```

`minimalConfigTOML` returns a byte slice containing the embedded default
config; copy it verbatim from `config/default.toml` via `os.ReadFile` at
test start if that is simpler than inlining. The helper
`ReloadConfigForTest` is a thin exported wrapper you will add next to
`reloadConfig`:

```go
// ReloadConfigForTest exposes reloadConfig for internal tests.
func (s *Server) ReloadConfigForTest(ctx context.Context) error {
	return s.reloadConfig(ctx)
}
```

- [ ] **Step 6: Run all affected tests with race**

Run:

```bash
go build ./...
go test ./inmem ./service ./internal/mcp ./internal/tui -race
```

Expected: all PASS. Treat any existing test needing a signature update as
part of this task and fix it inline.

- [ ] **Step 7: Commit**

```bash
git add internal/mcp/server.go internal/mcp/server_test.go \
        internal/tui/app.go cmd/tusk/main.go
git commit -m "feat(mcp): plumb loadOpts and hot-reload helper into server"
```

## Changes Introduced

- **Modified files:**
  - `inmem/workflow.go` — `Reload` method, `sync.RWMutex` guarding `workflows`,
    internal `buildWorkflowMap` factored out.
  - `inmem/project.go` — same pattern as `workflow.go`.
  - `service/urgency.go` — `Reload` method and `sync.RWMutex` guarding
    `defaults`. `weightsFor` now takes the read lock before returning defaults.
  - `internal/mcp/server.go` — `Server` gains `workflowRepo`, `projectRepo`,
    `urgencyEngine`, and `loadOpts` fields. `New` signature extended with
    matching parameters. Unexported `reloadConfig(ctx)` helper added plus an
    exported `ReloadConfigForTest` shim.
  - `internal/tui/app.go` — `App` struct and `New` signature extended with
    `workflowRepo`, `projectRepo`, `urgencyEngine`. `mcp serve` RunE threads
    the new handles into `tuskmcp.New`.
  - `cmd/tusk/main.go` — call to `tui.New` passes the new handles and
    `loadOpts`.

- **New test files:** none — tests extend existing files.

- **Schema migrations:** none.

- **Environment variables:** none new.

- **Dependencies:** none added.

- **Bridge code:** `Server.reloadConfig` / `ReloadConfigForTest` is added but
  has no production caller. First caller: **Phase 2**, `handleConfigSet`.
  Second and third callers: **Phase 3** (workflow mutation handlers) and
  **Phase 4** (project mutation handlers). Not scheduled for removal — it is
  permanent infrastructure.
