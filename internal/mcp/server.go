package mcp

import (
	"context"
	"fmt"
	"sync"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Version reported by the MCP server in its initialize response.
const Version = "v1.0.0-dev"

// Server wraps mcp-go's server with a Tusk Runtime.
type Server struct {
	runtime  *Runtime
	mcp      *server.MCPServer
	handlers map[string]server.ToolHandlerFunc
}

// NewServer builds a Server, registers every Tusk tool, and returns it. The
// caller invokes ServeStdio or ServeSSE to start a transport.
func NewServer(runtime *Runtime) *Server {
	core := server.NewMCPServer(
		"tusk",
		Version,
		server.WithToolCapabilities(true),
	)

	srv := &Server{
		runtime:  runtime,
		mcp:      core,
		handlers: map[string]server.ToolHandlerFunc{},
	}

	registerTools(srv)

	return srv
}

// register adds tool to both the mcp-go server and srv.handlers.
func (srv *Server) register(tool mcpgo.Tool, handler server.ToolHandlerFunc) {
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

// ServeStdio runs the server over stdio. Blocks until stdin closes.
func (srv *Server) ServeStdio() error {
	return server.ServeStdio(srv.mcp)
}

// ServeSSE runs the server over SSE on addr (e.g. ":8765"). Blocks.
func (srv *Server) ServeSSE(addr string) error {
	sse := server.NewSSEServer(srv.mcp)

	return sse.Start(addr)
}

// RunBackground starts the embed-queue drainer and the file watcher. It blocks
// until ctx cancels, then returns the first non-nil error from either worker.
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

	waitGroup.Add(2)

	go func() {
		defer waitGroup.Done()
		record(RunDrainer(ctx, DrainerConfig{Runtime: srv.runtime, Logger: srv.runtime.Logger}))
	}()

	go func() {
		defer waitGroup.Done()
		record(RunWatcher(ctx, WatchConfig{Runtime: srv.runtime, Logger: srv.runtime.Logger}))
	}()

	waitGroup.Wait()

	return first
}

// MCP exposes the underlying mcp-go server (for advanced wiring/tests).
func (srv *Server) MCP() *server.MCPServer {
	return srv.mcp
}
