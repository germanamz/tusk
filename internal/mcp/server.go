package mcp

import (
	"github.com/mark3labs/mcp-go/server"
)

// Version reported by the MCP server in its initialize response.
const Version = "v1.0.0-dev"

// Server wraps mcp-go's server with a Tusk Runtime.
type Server struct {
	runtime *Runtime
	mcp     *server.MCPServer
}

// NewServer builds a Server, registers every Tusk tool, and returns it. The
// caller invokes ServeStdio or ServeSSE to start a transport.
func NewServer(runtime *Runtime) *Server {
	core := server.NewMCPServer(
		"tusk",
		Version,
		server.WithToolCapabilities(true),
	)

	srv := &Server{runtime: runtime, mcp: core}

	registerTools(srv)

	return srv
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

// MCP exposes the underlying mcp-go server (for advanced wiring/tests).
func (srv *Server) MCP() *server.MCPServer {
	return srv.mcp
}
