# Phase 2 — `tusk_config_show` and `tusk_config_set` MCP Tools

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let MCP clients read the effective configuration and mutate
individual scalar values, matching the semantics of `tusk config show` and
`tusk config set`. Mutations must apply to the same file the CLI would
target (walk-up discovery via `loadOpts`), validate before writing, and
hot-reload in-memory state via `reloadConfig` so subsequent task / workflow
tool calls see the new values without restarting the server.

**Architecture:** Two new handlers in `internal/mcp/config_handlers.go` reuse
the existing `config.Load`, `config.LoadFile`, `config.WriteConfig`,
`config.IsValidKey`, and `config.IsSliceKey` helpers. Both tools sit under a
new `config` tool group so operators can disable them wholesale via
`mcp.disabled_tool_groups = ["config"]`. `tusk_config_set` rejects `storage.*`
keys to avoid the undefined behavior of swapping the database path under a
live server.

**Tech Stack:** Go standard library, `spf13/viper` for dot-path set (same
dependency `internal/tui/config.go` already pulls in), `pelletier/go-toml/v2`
for round-tripping — both already in `go.mod`.

**Prerequisites:** **Phase 1** must be complete. Specifically:

- `mcp.Server.loadOpts` is populated and wired into `config.Load` via
  `reloadConfig`.
- `Server.reloadConfig(ctx)` is present and reloads workflow repo, project
  repo, and urgency engine from the active config file.
- `mcp.New` takes `workflowRepo`, `projectRepo`, `urgencyEngine`, and
  `loadOpts` arguments; `tui.App` threads them in from `cmd/tusk/main.go`.

## Inherits From

- Phase 1 plumbing (above).
- Pre-existing helpers in `config/write.go`:
  - `LoadFile(path) (*Config, error)`
  - `WriteConfig(cfg *Config, path) error`
  - `IsValidKey(key) bool`
  - `IsSliceKey(key) bool`
  - `ConfigFilePath(opts ...Option) (string, error)`
- Pre-existing CLI handler `internal/tui/config.go:355-409` (`runConfigSet`)
  which this phase mirrors step-for-step. Treat it as the reference
  implementation for how to parse, apply, validate, and write.
- The `validToolNames` / `validToolGroups` maps in
  `internal/mcp/server.go:93-115`, which must be extended for the new
  entries.

## User-Visible Behavior (must still work)

- Everything Phase 1 preserved, plus:
- `tusk_config_show` returns a JSON object with the effective config and the
  active file path; agents can now answer "what database is this pointing
  at?" without shelling out.
- `tusk_config_set` applied via MCP has the same side effects as the CLI
  `tusk config set` — writes to the walk-up hit, validates, atomic rename —
  with the extra step of hot-reloading the server.
- Attempting `tusk_config_set storage.path=/new/db.sqlite` returns an error
  and makes no changes.
- Setting `mcp.disabled_tool_groups = ["config"]` causes both new tools to
  disappear from the tool registry on the next process start, and in the
  meantime `validateConfig` does not reject the new group name.
- All existing MCP tools continue to be registered and reachable.

## Tasks

### Task 1: Register `config` tool group and tool allow-list entries

**Files:**
- Modify: `internal/mcp/server.go` (the `validateConfig` method around
  lines 92–152, and the tool registration block starting at line 176).

- [ ] **Step 1: Extend `validateConfig`**

In `validateConfig`, add the new tool names and group so disable-list entries
referencing them do not trip validation:

```go
validToolNames := map[string]bool{
	// ... existing entries, unchanged ...
	"tusk_config_show": true,
	"tusk_config_set":  true,
}
validToolGroups := map[string]bool{
	"task": true, "relation": true, "project": true,
	"workflow": true, "player": true, "config": true,
}
```

- [ ] **Step 2: Register both tools inside `registerTools`**

At the end of `registerTools`, after the `tusk_task_pop` registration, add:

```go
s.addTool("config",
	mcp.NewTool("tusk_config_show",
		mcp.WithDescription("Return the effective Tusk configuration and the path of the active config file. Read-only."),
	),
	s.handleConfigShow,
)

s.addTool("config",
	mcp.NewTool("tusk_config_set",
		mcp.WithDescription("Set a scalar config value by dot-path key and hot-reload the server. Rejects storage.* keys. Changes to mcp.disabled_* take effect only after process restart."),
		mcp.WithString("key",
			mcp.Required(),
			mcp.Description("Dot-path key (e.g. urgency.due_weight, tui.color, mcp.disabled_tools)"),
		),
		mcp.WithString("value",
			mcp.Required(),
			mcp.Description("New value. For slice keys (e.g. mcp.disabled_tools), use a comma-separated list."),
		),
	),
	s.handleConfigSet,
)
```

- [ ] **Step 3: Build the binary and confirm the tool list**

Run: `go build ./... && go vet ./internal/mcp`
Expected: no output.

Run: `go test ./internal/mcp -run TestServer -v`
Expected: PASS. The existing server tests should still pass; if any test
enumerates tool counts, update the constant to include the two new tools.

- [ ] **Step 4: Commit**

```bash
git add internal/mcp/server.go
git commit -m "feat(mcp): register tusk_config_show and tusk_config_set tool shells"
```

(The handlers are stubs that return an error at this point — that is fine.
The next task implements them. If the build fails because the handler
symbols are undefined, create empty stub functions now:

```go
func (s *Server) handleConfigShow(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError("not implemented"), nil
}
func (s *Server) handleConfigSet(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError("not implemented"), nil
}
```

Put them in a new file `internal/mcp/config_handlers.go` so the next task
can grow them without touching `tools.go`.)

### Task 2: Implement `handleConfigShow`

**Files:**
- Modify: `internal/mcp/config_handlers.go`
- Test: `internal/mcp/config_handlers_test.go` (new file)

- [ ] **Step 1: Write the failing test**

Create `internal/mcp/config_handlers_test.go` with a test that:

1. Writes a minimal `tusk.toml` to a temp dir that overrides
   `urgency.due_weight = 42.0`.
2. Constructs an `mcp.Server` with `config.WithExplicitFile` pointing at
   that file (and the other required dependencies built from in-memory
   fakes / nils where Phase-1 tests already establish the pattern).
3. Calls `s.HandleConfigShowForTest(ctx, emptyRequest)`.
4. Parses the returned JSON payload and asserts:
   - `effective.urgency.due_weight == 42.0`
   - `active_file == <path to the temp file>`

Add a `HandleConfigShowForTest` exported shim at the bottom of
`config_handlers.go` that just delegates to `handleConfigShow`. This keeps
the handler itself unexported while letting the test call it directly.

```go
func TestHandleConfigShow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	if err := os.WriteFile(path, []byte(`
[storage]
backend = "sqlite"
path = "./tusk.db"

[urgency]
priority_weight = 6.0
due_weight = 42.0
age_weight = 2.0
active_weight = 4.0
blocking_weight = 8.0
blocked_weight = 5.0
tags_weight = 1.0
project_weight = 1.0
annotations_weight = 1.0
waiting_weight = 3.0

[tui]
date_format = "2006-01-02"
color = true
tree_indent = 2
default_sort = "urgency"

[mcp]
disabled_tools = []
disabled_tool_groups = []
disabled_resources = []
disabled_resource_groups = []

[workflows.kanban]
[workflows.kanban.statuses.pending]
roles = ["initial"]
[workflows.kanban.statuses.active]
roles = ["start"]
[workflows.kanban.statuses.completed]
roles = ["terminal", "done"]
[workflows.kanban.statuses.deleted]
roles = ["terminal", "delete"]
[[workflows.kanban.transitions]]
from = "pending"
to = "active"
[[workflows.kanban.transitions]]
from = "active"
to = "completed"

[projects.default]
workflow = "kanban"
`), 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	srv := newTestServer(t, path)

	res, err := srv.HandleConfigShowForTest(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("HandleConfigShowForTest: %v", err)
	}

	var payload struct {
		ActiveFile string `json:"active_file"`
		Effective  struct {
			Urgency struct {
				DueWeight float64 `json:"due_weight"`
			} `json:"urgency"`
		} `json:"effective"`
	}
	if err := json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &payload); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if payload.ActiveFile != path {
		t.Fatalf("active_file: got %q, want %q", payload.ActiveFile, path)
	}
	if payload.Effective.Urgency.DueWeight != 42.0 {
		t.Fatalf("due_weight: got %v, want 42.0", payload.Effective.Urgency.DueWeight)
	}
}
```

`newTestServer(t, path)` is a small helper at the top of the file:

```go
func newTestServer(t *testing.T, configFile string) *mcp.Server {
	t.Helper()
	workflowRepo := inmem.NewWorkflowRepository(map[string]config.WorkflowConfig{})
	projectRepo := inmem.NewProjectRepository(map[string]config.ProjectConfig{})
	urgencyEngine := service.NewUrgencyEngine(service.UrgencyWeights{})
	srv, err := mcp.New(
		nil, nil, nil, nil, nil, nil,
		workflowRepo, projectRepo, urgencyEngine,
		"test", config.MCPConfig{},
		[]config.Option{config.WithExplicitFile(configFile)},
	)
	if err != nil {
		t.Fatalf("mcp.New: %v", err)
	}
	return srv
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/mcp -run TestHandleConfigShow -v`
Expected: FAIL — stub returns "not implemented".

- [ ] **Step 3: Implement the handler**

Replace the stub in `internal/mcp/config_handlers.go`:

```go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/germanamz/tusk/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/viper"
	toml "github.com/pelletier/go-toml/v2"
)

// configShowResponse is the JSON payload returned by tusk_config_show.
type configShowResponse struct {
	ActiveFile string          `json:"active_file"`
	Effective  json.RawMessage `json:"effective"`
}

func (s *Server) handleConfigShow(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cfg, err := config.Load(s.loadOpts...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading config: %v", err)), nil
	}

	raw, err := json.Marshal(cfg)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshaling config: %v", err)), nil
	}

	resp := configShowResponse{
		ActiveFile: cfg.Sources.File,
		Effective:  raw,
	}
	return toolResultJSON(resp)
}

// HandleConfigShowForTest exposes handleConfigShow for internal test packages.
func (s *Server) HandleConfigShowForTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.handleConfigShow(ctx, req)
}
```

- [ ] **Step 4: Verify the test passes**

Run: `go test ./internal/mcp -run TestHandleConfigShow -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/config_handlers.go internal/mcp/config_handlers_test.go
git commit -m "feat(mcp): implement tusk_config_show handler"
```

### Task 3: Implement `handleConfigSet` with `storage.*` guard

**Files:**
- Modify: `internal/mcp/config_handlers.go`
- Modify: `internal/mcp/config_handlers_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `config_handlers_test.go`:

```go
func TestHandleConfigSet_WritesAndReloads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path) // see helper below

	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"key":   "urgency.due_weight",
			"value": "99.5",
		}},
	}
	res, err := srv.HandleConfigSetForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleConfigSetForTest: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %+v", res)
	}

	// File was updated.
	loaded, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if loaded.Urgency.DueWeight != 99.5 {
		t.Fatalf("file not updated: got %v", loaded.Urgency.DueWeight)
	}
}

func TestHandleConfigSet_RejectsStorageKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"key":   "storage.path",
			"value": "/tmp/evil.db",
		}},
	}
	res, err := srv.HandleConfigSetForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleConfigSetForTest: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected error result, got success: %+v", res)
	}

	loaded, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if loaded.Storage.Path == "/tmp/evil.db" {
		t.Fatalf("storage.path was mutated despite guard")
	}
}

func TestHandleConfigSet_RejectsUnknownKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"key":   "urgency.nonsense",
			"value": "1",
		}},
	}
	res, _ := srv.HandleConfigSetForTest(context.Background(), req)
	if !res.IsError {
		t.Fatalf("expected error result for unknown key")
	}
}
```

Helper (put it in the same test file, near `newTestServer`):

```go
func writeMinimalConfig(t *testing.T, path string) {
	t.Helper()
	// Reuse the same TOML blob TestHandleConfigShow writes.
	// Extract it into a package-level const so both tests share it.
	if err := os.WriteFile(path, []byte(minimalConfigTOML), 0o644); err != nil {
		t.Fatalf("writing seed: %v", err)
	}
}
```

Define `minimalConfigTOML` as a `const` containing the same TOML used in
`TestHandleConfigShow` so both tests share one blob.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/mcp -run TestHandleConfigSet -v`
Expected: FAIL on all three — stub still returns "not implemented".

- [ ] **Step 3: Implement the handler**

In `internal/mcp/config_handlers.go`, replace the stub with:

```go
import (
	"bytes"
	"strings"
)

func (s *Server) handleConfigSet(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	key, err := req.RequireString("key")
	if err != nil {
		return mcp.NewToolResultError("key is required"), nil
	}
	value, err := req.RequireString("value")
	if err != nil {
		return mcp.NewToolResultError("value is required"), nil
	}

	if strings.HasPrefix(key, "storage.") {
		return mcp.NewToolResultError("refusing to modify storage.* keys via MCP; change the config file directly and restart the server"), nil
	}
	if !config.IsValidKey(key) {
		return mcp.NewToolResultError(fmt.Sprintf("unknown config key: %q", key)), nil
	}

	path, err := config.ConfigFilePath(s.loadOpts...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("resolving config file: %v", err)), nil
	}

	fileCfg, err := config.LoadFile(path)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading config file: %v", err)), nil
	}

	data, err := toml.Marshal(fileCfg)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshaling config: %v", err)), nil
	}

	v := viper.New()
	v.SetConfigType("toml")
	if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("reading config into viper: %v", err)), nil
	}

	var parsed any
	if config.IsSliceKey(key) {
		parsed = strings.Split(value, ",")
	} else {
		parsed = value
	}
	v.Set(key, parsed)

	var newCfg config.Config
	if err := v.Unmarshal(&newCfg); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("applying config change: %v", err)), nil
	}
	if err := newCfg.Validate(); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid config: %v", err)), nil
	}
	if err := config.WriteConfig(&newCfg, path); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("writing config: %v", err)), nil
	}

	if err := s.reloadConfig(ctx); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("reloading config: %v", err)), nil
	}

	return toolResultJSON(map[string]any{
		"ok":          true,
		"key":         key,
		"active_file": path,
	})
}

// HandleConfigSetForTest exposes handleConfigSet for internal test packages.
func (s *Server) HandleConfigSetForTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.handleConfigSet(ctx, req)
}
```

This mirrors `runConfigSet` at `internal/tui/config.go:355-409` — when in
doubt about field handling, read that function and stay consistent.

- [ ] **Step 4: Verify all three tests pass**

Run: `go test ./internal/mcp -run TestHandleConfigSet -v -race`
Expected: PASS × 3.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/config_handlers.go internal/mcp/config_handlers_test.go
git commit -m "feat(mcp): implement tusk_config_set with storage.* guard"
```

### Task 4: Update MCP server instructions string

**Files:**
- Modify: `internal/mcp/server.go` (`serverInstructions` constant at the
  bottom of the file)

- [ ] **Step 1: Extend the instructions**

Append one sentence to `serverInstructions` describing the new capability
so agents know the tools exist without enumerating them:

```go
const serverInstructions = `Tusk is a task management system. You can create, list, modify, and transition tasks through workflow statuses. Tasks support parent-child hierarchy, typed relations (blocks, relates_to, duplicates), tags, annotations, and projects. All mutation tools require a version parameter for optimistic locking — fetch the task first to get the current version. You can also inspect the active configuration via tusk_config_show and modify scalar config values via tusk_config_set (storage.* keys are read-only over MCP).`
```

- [ ] **Step 2: Verify tests still pass**

Run: `go test ./internal/mcp -v -race`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/mcp/server.go
git commit -m "docs(mcp): advertise config tools in server instructions"
```

## Changes Introduced

- **New files:**
  - `internal/mcp/config_handlers.go` — handlers for `tusk_config_show`
    and `tusk_config_set`, plus `HandleConfigShowForTest` /
    `HandleConfigSetForTest` shims.
  - `internal/mcp/config_handlers_test.go` — three tests exercising show,
    set (happy path), set (storage guard), set (unknown key) plus the
    `minimalConfigTOML` constant and `writeMinimalConfig` / `newTestServer`
    helpers.

- **Modified files:**
  - `internal/mcp/server.go` — `validateConfig` allow-list extended with
    `tusk_config_show`, `tusk_config_set`, and the `config` tool group.
    `registerTools` wires the two new tools. `serverInstructions` mentions
    them.

- **Modified interfaces:** none. `Server.reloadConfig` is now called from
  production code (the phase 1 bridge stops being dormant).

- **New environment variables / schema migrations / dependencies:** none.

- **User-visible behavior after this phase:**
  - `tusk_config_show` returns `{active_file, effective}` JSON.
  - `tusk_config_set` succeeds for valid scalar keys, refreshes the server
    state, and rejects `storage.*` and unknown keys with a clear MCP error.
  - The existing CLI `tusk config show|set` still writes to the same file
    and behaves identically.

- **Bridge code:** none introduced in this phase. Phase-1's
  `reloadConfig` helper is now in production use.
