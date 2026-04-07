# Phase 3: MCP Player Tools & E2E Tests

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose player registration, claiming, and release as MCP tools. Add `player_id` opt-in parameter to MCP read tools for liveness tracking. Write comprehensive E2E tests for both CLI and MCP player workflows.

**Architecture:** MCP server gains `playerSvc` dependency. Three new tools (`tusk_player_register`, `tusk_task_claim`, `tusk_task_release`), one modified tool (`tusk_task_start`), and optional `player_id` on read tools. E2E tests cover full CLI and MCP player scenarios.

**Tech Stack:** Go, MCP (github.com/mark3labs/mcp-go), SQLite

**Prerequisites:** Phase 1 and Phase 2 must be completed.

**Design Spec:** `docs/superpowers/specs/2026-04-06-player-entity-registration-design.md`

---

## Inherits From

**Phase 1** added:
- `domain.Player` struct, `domain.ErrTaskClaimed`, `ClaimedBy`/`ClaimedAt` on Task/TaskUpdate
- `repository.PlayerRepository` interface, `sqlite.PlayerRepo`
- Migration 002 with `players` table and task claim columns

**Phase 2** added:
- `service.PlayerService` — Register, GetByID, UpdateLastSeen, List
- `TaskService.Claim`, `TaskService.Release`, modified `TaskService.Start` (accepts optional playerID)
- `tui.App` accepts `playerSvc` and `playerID`; new CLI commands: `tusk player register`, `tusk claim`, `tusk release`
- `--player` global flag parsed in main.go
- `ClaimedBy`/`ClaimedAt` in task JSON and text output
- `claimed_by:<id>` and `unclaimed:true` filters
- `taskJSON` and `toTaskJSON` include `ClaimedBy`/`ClaimedAt`
- MCP `taskResponse` struct does NOT yet include `ClaimedBy`/`ClaimedAt` (added in this phase)
- MCP `handleTaskStart` uses a closure wrapper to adapt the 4-arg `Start` to the 3-arg `handleTaskTransition` helper (bridge code — replaced with dedicated handler in this phase, Task 3)

---

### Task 1: Add `playerSvc` to MCP Server and update task response

**Files:**
- Modify: `internal/mcp/server.go` — add `playerSvc` field and constructor parameter
- Modify: `internal/mcp/tools.go` — add `ClaimedBy`/`ClaimedAt` to `taskResponse` and `toTaskResponse`
- Modify: `internal/tui/app.go` — pass `playerSvc` to MCP server in mcp serve command

- [ ] **Step 1: Add `playerSvc` to MCP Server struct**

In `internal/mcp/server.go`, add the field to the struct (after `workflowSvc` at line 20):

```go
type Server struct {
	taskSvc        *service.TaskService
	tagSvc         *service.TagService
	relationSvc    *service.RelationService
	projectSvc     *service.ProjectService
	workflowSvc    *service.WorkflowService
	playerSvc      *service.PlayerService
	server         *server.MCPServer
	cfg            config.MCPConfig
	toolGroups     map[string]string
	resourceGroups map[string]string
}
```

Update the `New` constructor signature:

```go
func New(
	taskSvc *service.TaskService,
	tagSvc *service.TagService,
	relationSvc *service.RelationService,
	projectSvc *service.ProjectService,
	workflowSvc *service.WorkflowService,
	playerSvc *service.PlayerService,
	version string,
	cfg config.MCPConfig,
) (*Server, error) {
```

Wire `playerSvc` in the struct initialization:

```go
s := &Server{
	taskSvc:        taskSvc,
	tagSvc:         tagSvc,
	relationSvc:    relationSvc,
	projectSvc:     projectSvc,
	workflowSvc:    workflowSvc,
	playerSvc:      playerSvc,
	cfg:            cfg,
	toolGroups:     make(map[string]string),
	resourceGroups: make(map[string]string),
}
```

- [ ] **Step 2: Update ALL callers of `tuskmcp.New`**

Search for all callers:

```bash
grep -rn "tuskmcp.New\|mcp.New(" --include="*.go"
```

The main caller is in `internal/tui/app.go` in the MCP serve command RunE. Update it to pass `a.playerSvc`:

```go
mcpServer, err := tuskmcp.New(taskSvc, tagSvc, relationSvc, projectSvc, a.workflowSvc, a.playerSvc, vi.Version, a.mcpCfg)
```

Also check `tests/e2e/` for any MCP test helpers that create an MCP server — update those too.

- [ ] **Step 3: Add `ClaimedBy`/`ClaimedAt` to `taskResponse` in `internal/mcp/tools.go`**

Add to the `taskResponse` struct (after `Urgency` at line 47):

```go
ClaimedBy *string `json:"claimed_by,omitempty"`
ClaimedAt *string `json:"claimed_at,omitempty"`
```

Update `toTaskResponse` to populate them (after the existing field mappings):

```go
if t.ClaimedBy != nil {
	r.ClaimedBy = t.ClaimedBy
}
if t.ClaimedAt != nil {
	s := t.ClaimedAt.Format(time.RFC3339)
	r.ClaimedAt = &s
}
```

- [ ] **Step 4: Add `player` group to `validateConfig`**

In `internal/mcp/server.go`, update the validation maps:

Add `"tusk_player_register"`, `"tusk_task_claim"`, `"tusk_task_release"` to `validToolNames`.

Add `"player": true` to `validToolGroups`.

- [ ] **Step 5: Build and verify**

```bash
go build ./...
```

Expected: compiles cleanly.

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/server.go internal/mcp/tools.go internal/tui/app.go
git commit -m "feat(mcp): add playerSvc to MCP Server, claim fields in task responses"
```

---

### Task 2: MCP player registration and claim tools

**Files:**
- Modify: `internal/mcp/server.go` — register new tools in `registerTools()`
- Modify: `internal/mcp/tools.go` — add handler functions

- [ ] **Step 1: Register new tools in `registerTools()`**

In `internal/mcp/server.go`, add these tool registrations at the end of `registerTools()` (before the closing brace):

```go
s.addTool("player",
	mcp.NewTool("tusk_player_register",
		mcp.WithDescription("Register a new player (agent). Player type is always 'agent' for MCP."),
		mcp.WithString("player_id",
			mcp.Required(),
			mcp.Description("Unique player identifier"),
		),
	),
	s.handlePlayerRegister,
)

s.addTool("task",
	mcp.NewTool("tusk_task_claim",
		mcp.WithDescription("Claim a task for a player. Returns error if already claimed by another player."),
		mcp.WithString("short_id",
			mcp.Required(),
			mcp.Description("Task short ID"),
		),
		mcp.WithString("player_id",
			mcp.Required(),
			mcp.Description("Player ID claiming the task"),
		),
		mcp.WithNumber("version",
			mcp.Required(),
			mcp.Description("Current task version (for optimistic locking)"),
		),
	),
	s.handleTaskClaim,
)

s.addTool("task",
	mcp.NewTool("tusk_task_release",
		mcp.WithDescription("Release a task claim. Only the current claimant can release."),
		mcp.WithString("short_id",
			mcp.Required(),
			mcp.Description("Task short ID"),
		),
		mcp.WithString("player_id",
			mcp.Required(),
			mcp.Description("Player ID releasing the claim"),
		),
		mcp.WithNumber("version",
			mcp.Required(),
			mcp.Description("Current task version (for optimistic locking)"),
		),
	),
	s.handleTaskRelease,
)
```

- [ ] **Step 2: Implement handler functions in `internal/mcp/tools.go`**

```go
// playerResponse is the JSON structure returned by player tools.
type playerResponse struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	RegisteredAt string `json:"registered_at"`
	LastSeenAt   string `json:"last_seen_at"`
}

func toPlayerResponse(p *domain.Player) playerResponse {
	return playerResponse{
		ID:           p.ID,
		Type:         p.Type,
		RegisteredAt: p.RegisteredAt.Format(time.RFC3339),
		LastSeenAt:   p.LastSeenAt.Format(time.RFC3339),
	}
}

// handlePlayerRegister handles the tusk_player_register tool.
func (s *Server) handlePlayerRegister(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	playerID, err := request.RequireString("player_id")
	if err != nil {
		return mcp.NewToolResultError("player_id is required"), nil
	}

	player, err := s.playerSvc.Register(ctx, playerID, "agent")
	if err != nil {
		return toolError(err, "player "+playerID), nil
	}

	return toolResultJSON(toPlayerResponse(player))
}

// handleTaskClaim handles the tusk_task_claim tool.
func (s *Server) handleTaskClaim(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	shortID, err := request.RequireString("short_id")
	if err != nil {
		return mcp.NewToolResultError("short_id is required"), nil
	}
	playerID, err := request.RequireString("player_id")
	if err != nil {
		return mcp.NewToolResultError("player_id is required"), nil
	}
	version, err := request.RequireFloat("version")
	if err != nil {
		return mcp.NewToolResultError("version is required"), nil
	}

	updated, err := s.taskSvc.Claim(ctx, shortID, playerID, int(version))
	if err != nil {
		return toolError(err, "task "+shortID), nil
	}

	tags, err := s.tagSvc.GetTaskTags(ctx, updated.ID)
	if err != nil {
		return nil, err
	}

	return toolResultJSON(toTaskResponse(updated, tags))
}

// handleTaskRelease handles the tusk_task_release tool.
func (s *Server) handleTaskRelease(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	shortID, err := request.RequireString("short_id")
	if err != nil {
		return mcp.NewToolResultError("short_id is required"), nil
	}
	playerID, err := request.RequireString("player_id")
	if err != nil {
		return mcp.NewToolResultError("player_id is required"), nil
	}
	version, err := request.RequireFloat("version")
	if err != nil {
		return mcp.NewToolResultError("version is required"), nil
	}

	updated, err := s.taskSvc.Release(ctx, shortID, playerID, int(version))
	if err != nil {
		return toolError(err, "task "+shortID), nil
	}

	tags, err := s.tagSvc.GetTaskTags(ctx, updated.ID)
	if err != nil {
		return nil, err
	}

	return toolResultJSON(toTaskResponse(updated, tags))
}
```

- [ ] **Step 3: Build and verify**

```bash
go build ./...
```

Expected: compiles cleanly.

- [ ] **Step 4: Commit**

```bash
git add internal/mcp/server.go internal/mcp/tools.go
git commit -m "feat(mcp): add tusk_player_register, tusk_task_claim, tusk_task_release tools"
```

---

### Task 3: Add optional `player_id` to MCP `tusk_task_start` and read tools

**Files:**
- Modify: `internal/mcp/server.go` — update tool definitions for `tusk_task_start`, `tusk_task_list`, `tusk_task_get`, `tusk_task_tree`
- Modify: `internal/mcp/tools.go` — update handlers

- [ ] **Step 1: Add optional `player_id` parameter to `tusk_task_start` tool definition**

In `internal/mcp/server.go`, find the `tusk_task_start` registration (around line 312) and add the parameter:

```go
s.addTool("task",
	mcp.NewTool("tusk_task_start",
		mcp.WithDescription("Transition a task to active status. If player_id is provided, auto-claims the task."),
		mcp.WithString("short_id",
			mcp.Required(),
			mcp.Description("Task short ID"),
		),
		mcp.WithNumber("version",
			mcp.Required(),
			mcp.Description("Current task version (for optimistic locking)"),
		),
		mcp.WithString("player_id",
			mcp.Description("Player ID — auto-registers as agent, auto-claims the task"),
		),
	),
	s.handleTaskStart,
)
```

- [ ] **Step 2: Add optional `player_id` to `tusk_task_list`, `tusk_task_get`, `tusk_task_tree` definitions**

For each of these tool registrations, add:

```go
mcp.WithString("player_id",
	mcp.Description("Player ID — updates last_seen_at if provided (no auto-register)"),
),
```

- [ ] **Step 3: Update `handleTaskStart` handler**

The existing `handleTaskStart` (from Phase 2) uses a closure wrapper around `handleTaskTransition` to adapt the 4-arg `Start` signature. Replace it with a dedicated handler that passes the actual `player_id` and handles auto-registration:

```go
func (s *Server) handleTaskStart(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	shortID, err := request.RequireString("short_id")
	if err != nil {
		return mcp.NewToolResultError("short_id is required"), nil
	}
	version, err := request.RequireFloat("version")
	if err != nil {
		return mcp.NewToolResultError("version is required"), nil
	}

	playerID, _ := request.GetString("player_id", "")

	// Auto-register player as agent if provided
	if playerID != "" && s.playerSvc != nil {
		if _, regErr := s.playerSvc.Register(ctx, playerID, "agent"); regErr != nil {
			if !errors.Is(regErr, domain.ErrConflict) {
				return toolError(regErr, "auto-registering player"), nil
			}
		}
	}

	updated, err := s.taskSvc.Start(ctx, shortID, int(version), playerID)
	if err != nil {
		return toolError(err, "task "+shortID), nil
	}

	tags, err := s.tagSvc.GetTaskTags(ctx, updated.ID)
	if err != nil {
		return nil, err
	}

	return toolResultJSON(toTaskResponse(updated, tags))
}
```

**NOTE:** This replaces the Phase 2 closure wrapper (bridge code removal). `handleTaskDone` and `handleTaskDelete` still use `handleTaskTransition` — their signatures haven't changed.

- [ ] **Step 4: Add `player_id` liveness tracking to read handlers**

Create a helper function:

```go
// updatePlayerLiveness updates last_seen_at for a player if the player_id is provided and valid.
func (s *Server) updatePlayerLiveness(ctx context.Context, request mcp.CallToolRequest) {
	playerID, _ := request.GetString("player_id", "")
	if playerID != "" && s.playerSvc != nil {
		s.playerSvc.UpdateLastSeen(ctx, playerID) //nolint:errcheck
	}
}
```

Add `s.updatePlayerLiveness(ctx, request)` as the first line in each of these handlers:
- `handleTaskList`
- `handleTaskGet`
- `handleTaskTree`

- [ ] **Step 5: Build and verify**

```bash
go build ./...
```

Expected: compiles cleanly.

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/server.go internal/mcp/tools.go
git commit -m "feat(mcp): add player_id to tusk_task_start and opt-in liveness on read tools"
```

---

### Task 4: CLI E2E tests for player workflows

**Files:**
- Create: `tests/e2e/player_test.go`

- [ ] **Step 1: Write E2E scenarios in `tests/e2e/player_test.go`**

Follow the existing harness pattern in `tests/e2e/`. Each scenario runs across 4 modes (flag/env x text/json). The harness `Run` method handles `--db`/`TUSK_DB` and `--format` injection. For `--player`, pass it as part of `Args`.

```go
package e2e

import (
	"testing"
)

func TestPlayerRegistration(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "register_player",
			Steps: []Step{
				{
					Args: []string{"player", "register", "test-agent", "--type", "agent"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["id"], "test-agent")
						assertEqual(t, m["type"], "agent")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Registered")
						assertContains(t, output, "test-agent")
					},
				},
			},
		},
		{
			Name: "register_player_default_type",
			Steps: []Step{
				{
					Args: []string{"player", "register", "default-agent"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["type"], "agent")
					},
				},
			},
		},
		{
			Name: "register_player_human",
			Steps: []Step{
				{
					Args: []string{"player", "register", "german", "--type", "human"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["type"], "human")
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}

func TestClaimRelease(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "claim_and_release",
			Steps: []Step{
				{Args: []string{"player", "register", "agent-1"}},
				{Args: []string{"add", "Claimable task"}},
				{
					Args: []string{"claim", "$1.short_id", "--player", "agent-1"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["claimed_by"], "agent-1")
						if m["claimed_at"] == nil {
							t.Error("expected claimed_at to be set")
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Claimed")
					},
				},
				{
					Args: []string{"release", "$1.short_id", "--player", "agent-1"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["claimed_by"] != nil {
							t.Errorf("expected claimed_by to be nil after release, got %v", m["claimed_by"])
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Released")
					},
				},
			},
		},
		{
			Name: "claim_already_claimed",
			Steps: []Step{
				{Args: []string{"player", "register", "agent-1"}},
				{Args: []string{"player", "register", "agent-2"}},
				{Args: []string{"add", "Contested task"}},
				{Args: []string{"claim", "$2.short_id", "--player", "agent-1"}},
				{
					Args:    []string{"claim", "$2.short_id", "--player", "agent-2"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertContains(t, r.Stderr, "already claimed")
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}

func TestStartAutoClaim(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "start_auto_claims",
			Steps: []Step{
				{Args: []string{"add", "Auto-claim task"}},
				{
					Args: []string{"start", "$0.short_id", "--player", "agent-auto"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["status"], "active")
						assertEqual(t, m["claimed_by"], "agent-auto")
					},
				},
			},
		},
		{
			Name: "start_without_player_no_claim",
			Steps: []Step{
				{Args: []string{"add", "No-claim task"}},
				{
					Args: []string{"start", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["status"], "active")
						if m["claimed_by"] != nil {
							t.Errorf("expected no claim, got %v", m["claimed_by"])
						}
					},
				},
			},
		},
		{
			Name: "start_claimed_by_other_fails",
			Steps: []Step{
				{Args: []string{"player", "register", "agent-1"}},
				{Args: []string{"player", "register", "agent-2"}},
				{Args: []string{"add", "Guarded task"}},
				{Args: []string{"claim", "$2.short_id", "--player", "agent-1"}},
				{
					Args:    []string{"start", "$2.short_id", "--player", "agent-2"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertContains(t, r.Stderr, "already claimed")
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}

func TestClaimVisibleInInfo(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "info_shows_claim",
			Steps: []Step{
				{Args: []string{"add", "Visible claim task"}},
				{Args: []string{"claim", "$0.short_id", "--player", "agent-vis"}},
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["claimed_by"], "agent-vis")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Claimed By:")
						assertContains(t, output, "agent-vis")
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}

func TestClaimPreservedAfterDone(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "done_preserves_claim",
			Steps: []Step{
				{Args: []string{"add", "Finish task"}},
				{Args: []string{"start", "$0.short_id", "--player", "agent-done"}},
				{
					Args: []string{"done", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["status"], "completed")
						assertEqual(t, m["claimed_by"], "agent-done")
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}

func TestClaimFilters(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "filter_claimed_by",
			Steps: []Step{
				{Args: []string{"add", "Task A"}},
				{Args: []string{"add", "Task B"}},
				{Args: []string{"claim", "$0.short_id", "--player", "agent-filter"}},
				{
					Args: []string{"list", "claimed_by:agent-filter"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						items := parsed.([]any)
						if len(items) != 1 {
							t.Fatalf("expected 1 task, got %d", len(items))
						}
						m := items[0].(map[string]any)
						assertEqual(t, m["claimed_by"], "agent-filter")
					},
				},
			},
		},
		{
			Name: "filter_unclaimed",
			Steps: []Step{
				{Args: []string{"add", "Unclaimed task"}},
				{Args: []string{"add", "Claimed task"}},
				{Args: []string{"claim", "$1.short_id", "--player", "agent-unc"}},
				{
					Args: []string{"list", "unclaimed:true"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						items := parsed.([]any)
						if len(items) != 1 {
							t.Fatalf("expected 1 unclaimed task, got %d", len(items))
						}
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}
```

- [ ] **Step 2: Run E2E tests**

```bash
go test -v ./tests/e2e/ -run "TestPlayerRegistration|TestClaimRelease|TestStartAutoClaim|TestClaimVisibleInInfo|TestClaimPreservedAfterDone|TestClaimFilters"
```

Expected: all tests pass.

- [ ] **Step 3: Commit**

```bash
git add tests/e2e/player_test.go
git commit -m "test(e2e): add CLI E2E tests for player registration, claiming, and filters"
```

---

### Task 5: MCP E2E tests for player workflows

**Files:**
- Create: `tests/e2e/mcp_player_test.go`

Uses the existing `mcpEnv` harness from `tests/e2e/mcp_test.go` — `newMCPEnv(t, binPath)` starts a `tusk mcp serve` subprocess, `env.callTool(name, args)` sends a JSON-RPC `tools/call` and returns parsed JSON, `env.callToolExpectError(name, args)` expects an `isError=true` result and returns the error text.

- [ ] **Step 1: Write MCP E2E tests in `tests/e2e/mcp_player_test.go`**

```go
package e2e

import (
	"testing"
)

func TestMCPPlayerRegister(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	result := env.callTool("tusk_player_register", map[string]any{
		"player_id": "mcp-agent-1",
	})
	if result["id"] != "mcp-agent-1" {
		t.Fatalf("expected id 'mcp-agent-1', got %v", result["id"])
	}
	if result["type"] != "agent" {
		t.Fatalf("expected type 'agent', got %v", result["type"])
	}
	if result["registered_at"] == nil {
		t.Fatal("expected registered_at to be set")
	}

	// Duplicate registration should fail
	errMsg := env.callToolExpectError("tusk_player_register", map[string]any{
		"player_id": "mcp-agent-1",
	})
	if errMsg == "" {
		t.Fatal("expected error on duplicate registration")
	}
}

func TestMCPTaskClaim(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	// Register player and create task
	env.callTool("tusk_player_register", map[string]any{"player_id": "claimer-1"})
	created := env.callTool("tusk_task_create", map[string]any{"title": "MCP claim test"})
	shortID := created["short_id"].(string)
	version := created["version"].(float64)

	// Claim the task
	claimed := env.callTool("tusk_task_claim", map[string]any{
		"short_id":  shortID,
		"player_id": "claimer-1",
		"version":   version,
	})
	if claimed["claimed_by"] != "claimer-1" {
		t.Fatalf("expected claimed_by 'claimer-1', got %v", claimed["claimed_by"])
	}
	if claimed["claimed_at"] == nil {
		t.Fatal("expected claimed_at to be set")
	}
}

func TestMCPTaskRelease(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	env.callTool("tusk_player_register", map[string]any{"player_id": "releaser-1"})
	created := env.callTool("tusk_task_create", map[string]any{"title": "MCP release test"})
	shortID := created["short_id"].(string)
	version := created["version"].(float64)

	// Claim then release
	claimed := env.callTool("tusk_task_claim", map[string]any{
		"short_id":  shortID,
		"player_id": "releaser-1",
		"version":   version,
	})
	claimedVersion := claimed["version"].(float64)

	released := env.callTool("tusk_task_release", map[string]any{
		"short_id":  shortID,
		"player_id": "releaser-1",
		"version":   claimedVersion,
	})
	if released["claimed_by"] != nil {
		t.Fatalf("expected claimed_by nil after release, got %v", released["claimed_by"])
	}
	if released["claimed_at"] != nil {
		t.Fatalf("expected claimed_at nil after release, got %v", released["claimed_at"])
	}
}

func TestMCPTaskStartWithPlayer(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	// Create task — do NOT pre-register player (auto-register should handle it)
	created := env.callTool("tusk_task_create", map[string]any{"title": "MCP start auto-claim"})
	shortID := created["short_id"].(string)
	version := created["version"].(float64)

	// Start with player_id — should auto-register as agent and auto-claim
	started := env.callTool("tusk_task_start", map[string]any{
		"short_id":  shortID,
		"version":   version,
		"player_id": "auto-agent",
	})
	if started["status"] != "active" {
		t.Fatalf("expected status 'active', got %v", started["status"])
	}
	if started["claimed_by"] != "auto-agent" {
		t.Fatalf("expected claimed_by 'auto-agent', got %v", started["claimed_by"])
	}
}

func TestMCPTaskClaimAlreadyClaimed(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	env.callTool("tusk_player_register", map[string]any{"player_id": "first"})
	env.callTool("tusk_player_register", map[string]any{"player_id": "second"})
	created := env.callTool("tusk_task_create", map[string]any{"title": "MCP contested"})
	shortID := created["short_id"].(string)
	version := created["version"].(float64)

	// First player claims
	claimed := env.callTool("tusk_task_claim", map[string]any{
		"short_id":  shortID,
		"player_id": "first",
		"version":   version,
	})
	claimedVersion := claimed["version"].(float64)

	// Second player tries to claim — should fail
	errMsg := env.callToolExpectError("tusk_task_claim", map[string]any{
		"short_id":  shortID,
		"player_id": "second",
		"version":   claimedVersion,
	})
	if errMsg == "" {
		t.Fatal("expected error when second player claims")
	}
}

func TestMCPReadToolLiveness(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	// Register player
	env.callTool("tusk_player_register", map[string]any{"player_id": "liveness-agent"})
	created := env.callTool("tusk_task_create", map[string]any{"title": "Liveness test"})
	shortID := created["short_id"].(string)

	// Call tusk_task_get with player_id — should not error
	fetched := env.callTool("tusk_task_get", map[string]any{
		"short_id":  shortID,
		"player_id": "liveness-agent",
	})
	if fetched["title"] != "Liveness test" {
		t.Fatalf("expected title 'Liveness test', got %v", fetched["title"])
	}
}
```

- [ ] **Step 2: Run MCP E2E tests**

```bash
go test -v ./tests/e2e/ -run "TestMCPPlayer|TestMCPTaskClaim|TestMCPTaskRelease|TestMCPTaskStartWithPlayer|TestMCPReadToolLiveness"
```

Expected: all 6 tests pass.

- [ ] **Step 3: Commit**

```bash
git add tests/e2e/mcp_player_test.go
git commit -m "test(e2e): add MCP E2E tests for player registration and claiming"
```

---

### Task 6: Run full test suite and verify

**Files:** None (verification only)

- [ ] **Step 1: Run full test suite**

```bash
make test
```

Expected: all tests pass.

- [ ] **Step 2: Run with race detector**

```bash
make test-race
```

Expected: no race conditions detected.

- [ ] **Step 3: Run linter**

```bash
make lint
```

Expected: no lint errors.

- [ ] **Step 4: Run vet**

```bash
make vet
```

Expected: no vet warnings.

---

## Changes Introduced

**New files:**
- `tests/e2e/player_test.go` — CLI E2E tests for player workflows
- `tests/e2e/mcp_player_test.go` — MCP E2E tests for player workflows

**Modified files:**
- `internal/mcp/server.go` — added `playerSvc` field, updated constructor, registered 3 new tools, added `player` group to validation
- `internal/mcp/tools.go` — added `playerResponse`, `toPlayerResponse`, `handlePlayerRegister`, `handleTaskClaim`, `handleTaskRelease`, updated `handleTaskStart`, added `updatePlayerLiveness` helper, added `ClaimedBy`/`ClaimedAt` to `taskResponse`
- `internal/tui/app.go` — updated MCP server creation to pass `playerSvc`

**New dependencies:** None.

**Bridge code:** None — this is the final phase.

**User-visible behavior preserved:**
- All existing CLI commands work identically
- All existing MCP tools work identically (new optional parameters don't break existing callers)
- All existing E2E tests pass

**New user-visible behavior:**
- `tusk_player_register` MCP tool — registers a player as agent
- `tusk_task_claim` MCP tool — claims a task
- `tusk_task_release` MCP tool — releases a claim
- `tusk_task_start` accepts optional `player_id` for auto-claim
- `tusk_task_list`, `tusk_task_get`, `tusk_task_tree` accept optional `player_id` for liveness tracking
- All task tool responses include `claimed_by` and `claimed_at` when present
