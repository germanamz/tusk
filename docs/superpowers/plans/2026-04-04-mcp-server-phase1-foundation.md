# MCP Server Phase 1: Foundation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Set up the MCP server skeleton — dependency, server struct, error mapping, CLI wiring — so subsequent phases can add tools incrementally.

**Architecture:** A new `Server` struct in `internal/mcp/` mirrors the `tui.App` pattern. It holds the same service dependencies and registers tools/resources on a `mcp-go` server. The CLI gets a `tusk mcp serve` command that starts the stdio transport.

**Tech Stack:** Go, `github.com/mark3labs/mcp-go` (MCP SDK), Cobra (CLI)

**Design Spec:** `docs/superpowers/specs/2026-04-04-mcp-server-core-design.md`

---

### Task 1: Add mcp-go dependency and create error mapping

**Files:**
- Modify: `go.mod` (add dependency)
- Create: `internal/mcp/errors.go`
- Create: `internal/mcp/errors_test.go`

- [ ] **Step 1: Add the mcp-go dependency**

Run:
```bash
go get github.com/mark3labs/mcp-go@latest
```

Expected: `go.mod` and `go.sum` updated with the new dependency.

- [ ] **Step 2: Write failing tests for error mapping**

Create `internal/mcp/errors_test.go`:

```go
package mcp

import (
	"fmt"
	"testing"

	"github.com/germanamz/tusk/internal/domain"
)

func TestMapError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		context string
		want    string
	}{
		{
			name:    "ErrNotFound",
			err:     domain.ErrNotFound,
			context: "task abc12345",
			want:    "not found: task abc12345",
		},
		{
			name:    "wrapped ErrNotFound",
			err:     fmt.Errorf("lookup: %w", domain.ErrNotFound),
			context: "task abc12345",
			want:    "not found: task abc12345",
		},
		{
			name: "ErrConflict",
			err:  domain.ErrConflict,
			want: "version conflict: task was modified, re-fetch and retry",
		},
		{
			name: "ErrInvalidTransition",
			err:  domain.ErrInvalidTransition,
			want: "invalid status transition",
		},
		{
			name: "ErrCyclicBlock",
			err:  domain.ErrCyclicBlock,
			want: "would create a dependency cycle",
		},
		{
			name: "ErrCyclicParent",
			err:  domain.ErrCyclicParent,
			want: "would create a parent-child cycle",
		},
		{
			name: "ErrDuplicateRelation",
			err:  domain.ErrDuplicateRelation,
			want: "relation already exists",
		},
		{
			name:    "ErrSourceNotFound",
			err:     domain.ErrSourceNotFound,
			context: "task src123",
			want:    "source task not found",
		},
		{
			name:    "ErrTargetNotFound",
			err:     domain.ErrTargetNotFound,
			context: "task tgt456",
			want:    "target task not found",
		},
		{
			name: "unknown error",
			err:  fmt.Errorf("db connection lost"),
			want: "internal error: db connection lost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapError(tt.err, tt.context)
			if got != tt.want {
				t.Errorf("mapError() = %q, want %q", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run:
```bash
go test -v ./internal/mcp/ -run TestMapError
```
Expected: Compilation error — `mapError` undefined.

- [ ] **Step 4: Implement mapError**

Create `internal/mcp/errors.go`:

```go
package mcp

import (
	"errors"
	"fmt"

	"github.com/germanamz/tusk/internal/domain"
)

// mapError translates domain sentinel errors into user-facing MCP error strings.
// The context parameter adds specificity (e.g., "task abc12345") for not-found errors.
func mapError(err error, context string) string {
	switch {
	case errors.Is(err, domain.ErrSourceNotFound):
		return "source task not found"
	case errors.Is(err, domain.ErrTargetNotFound):
		return "target task not found"
	case errors.Is(err, domain.ErrNotFound):
		if context != "" {
			return fmt.Sprintf("not found: %s", context)
		}
		return "not found"
	case errors.Is(err, domain.ErrConflict):
		return "version conflict: task was modified, re-fetch and retry"
	case errors.Is(err, domain.ErrInvalidTransition):
		return "invalid status transition"
	case errors.Is(err, domain.ErrCyclicBlock):
		return "would create a dependency cycle"
	case errors.Is(err, domain.ErrCyclicParent):
		return "would create a parent-child cycle"
	case errors.Is(err, domain.ErrDuplicateRelation):
		return "relation already exists"
	default:
		return fmt.Sprintf("internal error: %s", err.Error())
	}
}
```

**Important:** `ErrSourceNotFound` and `ErrTargetNotFound` must be checked BEFORE `ErrNotFound` because they wrap `ErrNotFound` (see `internal/domain/errors.go`). If `ErrNotFound` is checked first, those specific errors would match the generic case instead.

- [ ] **Step 5: Run tests to verify they pass**

Run:
```bash
go test -v ./internal/mcp/ -run TestMapError
```
Expected: All 10 tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/errors.go internal/mcp/errors_test.go go.mod go.sum
git commit -m "feat(mcp): add error mapping and mcp-go dependency"
```

---

### Task 2: Create Server struct with New() and Serve()

**Files:**
- Modify: `internal/mcp/server.go` (replace placeholder comment)
- Create: `internal/mcp/server_test.go`

- [ ] **Step 1: Write failing test for server creation**

Create `internal/mcp/server_test.go`:

```go
package mcp

import (
	"testing"
)

func TestNewServer(t *testing.T) {
	// New should not panic with nil services (used for testing tool registration).
	s := New(nil, nil, nil, nil)
	if s == nil {
		t.Fatal("New() returned nil")
	}
	if s.server == nil {
		t.Fatal("New() did not initialize internal MCP server")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:
```bash
go test -v ./internal/mcp/ -run TestNewServer
```
Expected: Compilation error — `New` undefined.

- [ ] **Step 3: Implement Server struct and New()**

Replace the contents of `internal/mcp/server.go` with:

```go
package mcp

import (
	"github.com/germanamz/tusk/internal/service"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Server wraps an MCP server that exposes tusk capabilities as tools and resources.
type Server struct {
	taskSvc     *service.TaskService
	tagSvc      *service.TagService
	relationSvc *service.RelationService
	projectSvc  *service.ProjectService
	server      *server.MCPServer
}

// New creates a new MCP Server and registers all tools and resources.
func New(
	taskSvc *service.TaskService,
	tagSvc *service.TagService,
	relationSvc *service.RelationService,
	projectSvc *service.ProjectService,
) *Server {
	s := &Server{
		taskSvc:     taskSvc,
		tagSvc:      tagSvc,
		relationSvc: relationSvc,
		projectSvc:  projectSvc,
	}

	s.server = server.NewMCPServer(
		"tusk",
		"0.3.0",
		server.WithToolCapabilities(false),
		server.WithResourceCapabilities(false, false),
		server.WithRecovery(),
		server.WithInstructions(serverInstructions),
	)

	return s
}

// Serve starts the MCP server using stdio transport.
// This blocks until the transport is closed (e.g., stdin EOF).
func (s *Server) Serve() error {
	return server.ServeStdio(s.server)
}

const serverInstructions = `Tusk is a task management system. You can create, list, modify, and transition tasks through workflow statuses. Tasks support parent-child hierarchy, typed relations (blocks, relates_to, duplicates), tags, annotations, and projects. All mutation tools require a version parameter for optimistic locking — fetch the task first to get the current version.`
```

- [ ] **Step 4: Run test to verify it passes**

Run:
```bash
go test -v ./internal/mcp/ -run TestNewServer
```
Expected: PASS.

- [ ] **Step 5: Run full test suite to check for regressions**

Run:
```bash
make test
```
Expected: All tests pass. No regressions.

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/server.go internal/mcp/server_test.go
git commit -m "feat(mcp): add Server struct with New() and Serve()"
```

---

### Task 3: Wire `tusk mcp serve` CLI command

**Files:**
- Modify: `internal/tui/app.go` (add `mcp serve` command)
- Modify: `internal/tui/app.go` (add import for `internal/mcp`)

This task adds the Cobra command. Since `tusk mcp serve` starts a blocking stdio server, unit testing the command registration is sufficient — E2E tests come in Phase 4.

- [ ] **Step 1: Add the `mcp serve` command to the Cobra tree**

In `internal/tui/app.go`, add the import for the MCP package at the top of the import block:

```go
tuskmcp "github.com/germanamz/tusk/internal/mcp"
```

Then, in the `New()` function, after the line `a.root.AddCommand(a.buildTagCmd())`, add:

```go
mcpCmd := &cobra.Command{
	Use:   "mcp",
	Short: "MCP server commands",
}
mcpCmd.AddCommand(&cobra.Command{
	Use:   "serve",
	Short: "Start MCP server with stdio transport",
	RunE: func(cmd *cobra.Command, args []string) error {
		mcpServer := tuskmcp.New(taskSvc, tagSvc, relationSvc, projectSvc)
		return mcpServer.Serve()
	},
})
a.root.AddCommand(mcpCmd)
```

- [ ] **Step 2: Verify the project compiles**

Run:
```bash
make build
```
Expected: Binary compiles successfully to `bin/tusk`.

- [ ] **Step 3: Verify the command is registered**

Run:
```bash
./bin/tusk mcp --help
```
Expected: Output shows `serve` as an available subcommand under `mcp`.

Run:
```bash
./bin/tusk mcp serve --help
```
Expected: Output shows "Start MCP server with stdio transport".

- [ ] **Step 4: Run full test suite**

Run:
```bash
make test
```
Expected: All tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/app.go
git commit -m "feat(tui): add tusk mcp serve command"
```
