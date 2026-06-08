package mcp

import (
	"context"
	"fmt"
	"sync"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/germanamz/tusk/internal/version"
)

// serverInstructions is sent back in the MCP initialize response. Clients
// surface this to the model at session start, so it carries the load of
// telling agents that Tusk is local (not a remote service) and pointing at
// the tusk_help tool for everything else. Keep it short — the deep content
// lives in the tusk_help topics.
const serverInstructions = `Tusk is a LOCAL workspace indexer running over the current working directory.
There is no remote service. Every tool operates on:
  - markdown files under ./
  - the schema declared in ./tusk.toml
  - the SQLite index at ./.tusk/index.db

To declare new node-types or edge-types, edit ./tusk.toml directly (no MCP
tool for this), then call tusk_reindex.

Call tusk_help() for an overview + topic index, or tusk_help(topic: "<name>")
for deep dives on: workflow, node-types, edge-types, manifest, filter, query, packs.`

// Server wraps mcp-go's server with a Tusk Runtime.
type Server struct {
	mu sync.RWMutex // guards the runtime pointer; readers = handlers (whole body) + snapshotRuntime (brief), writer = swap
	//nolint:unused // becomes live in Task 5 (reopenInPlace) and Phases 6-7; serializes index-replacement ops.
	resetMu  sync.Mutex // serializes ALL index-replacement ops in-process (reset tool + sibling reopen) so the flock/write-lock acquisition orders cannot interleave into a deadlock
	runtime  *Runtime
	mcp      *server.MCPServer
	handlers map[string]server.ToolHandlerFunc
}

// NewServer builds a Server, registers every Tusk tool, and returns it. The
// caller invokes ServeStdio or ServeSSE to start a transport.
func NewServer(runtime *Runtime) *Server {
	core := server.NewMCPServer(
		"tusk",
		version.Current,
		server.WithToolCapabilities(true),
		server.WithInstructions(serverInstructions),
	)

	srv := &Server{
		runtime:  runtime,
		mcp:      core,
		handlers: map[string]server.ToolHandlerFunc{},
	}

	registerTools(srv)

	return srv
}

// snapshotRuntime returns the current runtime pointer under a brief read-lock.
// Background goroutines (drainers, watcher) call this each tick and then run
// their drain/reindex pass on the returned snapshot WITHOUT holding the lock —
// so a reset's write-lock is never blocked by a long Ollama-bound pass. If a
// concurrent swap closes the snapshot's handle mid-pass, database/sql.Close
// first lets the in-flight query finish (no panic), then subsequent queries
// error; the drainer logs and re-snapshots next tick (the index is a cache).
// Handlers do NOT use this — they hold the read-lock for their whole body (via
// the guarded register) so they never observe a closed handle.
//
//nolint:unused // becomes live in Task 3 (drainers + tusk_reindex snapshot off the lock).
func (srv *Server) snapshotRuntime() *Runtime {
	srv.mu.RLock()
	defer srv.mu.RUnlock()

	return srv.runtime
}

// register adds tool to the mcp-go server and srv.handlers, wrapping the handler
// so it holds the runtime read-lock for its entire duration. Use registerWrite
// for tools that must take the write-lock themselves (e.g. tusk_reset).
func (srv *Server) register(tool mcpgo.Tool, handler server.ToolHandlerFunc) {
	guarded := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		srv.mu.RLock()
		defer srv.mu.RUnlock()

		return handler(ctx, request)
	}

	srv.mcp.AddTool(tool, guarded)
	srv.handlers[tool.Name] = guarded
}

// registerWrite registers a handler WITHOUT the read-lock wrapper, for tools
// that acquire the write-lock internally (a read-locked handler taking the
// write-lock would deadlock) or that run a long pass off a snapshot. Used by
// tusk_reindex (snapshots, runs off the lock) and by tusk_reset in Phase 6.
//
//nolint:unused // becomes live in Task 3 (tusk_reindex re-registers through this).
func (srv *Server) registerWrite(tool mcpgo.Tool, handler server.ToolHandlerFunc) {
	srv.mcp.AddTool(tool, handler)
	srv.handlers[tool.Name] = handler
}

// HandleToolCall is exported for tests; production code goes through stdio/SSE.
// It dispatches to the registered handler for request.Params.Name. Returns an
// "unknown tool" CallToolResult error when the tool isn't registered.
func (srv *Server) HandleToolCall(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	handler, exists := srv.handlers[request.Params.Name]

	if !exists {
		return mcpgo.NewToolResultError(fmt.Sprintf("unknown tool %q", request.Params.Name)), nil
	}

	return handler(ctx, request)
}

// RegisteredToolNames returns the set of tool names currently registered on
// the server. Order is unspecified. Exposed so cross-package tests can
// cross-check the CLI/MCP wiring without duplicating tool-name lists.
func (srv *Server) RegisteredToolNames() []string {
	names := make([]string, 0, len(srv.handlers))

	for name := range srv.handlers {
		names = append(names, name)
	}

	return names
}

// ServeStdio runs the server over stdio. Blocks until stdin closes.
func (srv *Server) ServeStdio() error {
	return server.ServeStdio(srv.mcp)
}

// ServeSSE runs the server over SSE on addr (e.g. ":8765"). Blocks.
func (srv *Server) ServeSSE(addr string) error {
	sse := server.NewSSEServer(srv.mcp)

	return sse.Start(addr)
}

// RunBackground starts the embed-queue drainer, reindex drainer, and file
// watcher. All three goroutines are gated on runtime.Workers > 0: when workers
// are disabled the instance becomes a pure read-server and does not observe
// FS changes (the watcher would enqueue reindex jobs that never drain). In that
// case it emits a single startup WARN (via runtime.Logger, if set) so the
// operator knows indexing won't happen in this instance, then returns
// immediately. Blocks until ctx cancels, then returns the first non-nil error.
func (srv *Server) RunBackground(ctx context.Context) error {
	var (
		mu    sync.Mutex
		first error
	)

	record := func(err error) {
		if err == nil {
			return
		}

		mu.Lock()
		defer mu.Unlock()

		if first == nil {
			first = err
		}
	}

	var waitGroup sync.WaitGroup

	if srv.runtime.Workers > 0 {
		waitGroup.Add(3)

		go func() {
			defer waitGroup.Done()
			record(RunDrainer(ctx, DrainerConfig{Runtime: srv.runtime, Logger: srv.runtime.Logger}))
		}()

		go func() {
			defer waitGroup.Done()
			record(RunReindexDrainer(ctx, ReindexDrainerConfig{Runtime: srv.runtime, Logger: srv.runtime.Logger}))
		}()

		go func() {
			defer waitGroup.Done()
			record(RunWatcher(ctx, WatchConfig{Runtime: srv.runtime, Logger: srv.runtime.Logger}))
		}()
	} else if srv.runtime.Logger != nil {
		srv.runtime.Logger.Warn(
			"embed workers disabled; watch is also disabled in this instance. " +
				"Ensure another instance (or scheduled tusk reindex) drives indexing " +
				"for this workspace, otherwise the index will go stale.")
	}

	waitGroup.Wait()

	return first
}

// MCP exposes the underlying mcp-go server (for advanced wiring/tests).
func (srv *Server) MCP() *server.MCPServer {
	return srv.mcp
}
