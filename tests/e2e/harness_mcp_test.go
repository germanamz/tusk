// tests/e2e/harness_mcp_test.go
package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// MCPEnv manages a `tusk mcp serve` subprocess for E2E testing.
//
// Construction is lazy: NewMCPEnv only allocates temp paths and stores
// configuration. The subprocess is spawned on the first Send call so
// chainable mutators (WithHome, WithEnv) can finish wiring the env
// before the binary launches. Tests can also seed configDir with a
// custom config.toml before the first Send.
type MCPEnv struct {
	t         *testing.T
	binPath   string
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    *bufio.Reader
	nextID    int
	homeDir   string
	extraEnv  []string
	started   bool
	dbPath    string
	configDir string
}

// NewMCPEnv allocates a fresh temp DB path and config directory under
// t.TempDir() and returns a not-yet-started MCPEnv. The subprocess is
// started lazily on the first Send call.
func NewMCPEnv(t *testing.T, binPath string) *MCPEnv {
	t.Helper()
	tmpFile, err := os.CreateTemp(t.TempDir(), "tusk-mcp-e2e-*.db")
	if err != nil {
		t.Fatalf("creating temp db: %v", err)
	}
	_ = tmpFile.Close()

	return &MCPEnv{
		t:         t,
		binPath:   binPath,
		nextID:    1,
		dbPath:    tmpFile.Name(),
		configDir: t.TempDir(),
	}
}

// WithHome overrides HOME (and USERPROFILE on Windows) for the
// subprocess. Must be called before the first Send. When set, MCPEnv
// skips injecting TUSK_CONFIG_DIR so tusk's config resolver falls
// through to <homeDir>/.config/tusk — the path tests typically seed
// with a config.toml under WithHome.
func (e *MCPEnv) WithHome(dir string) *MCPEnv {
	if e.started {
		e.t.Fatalf("MCPEnv.WithHome: subprocess already started")
	}
	e.homeDir = dir
	return e
}

// WithEnv appends a KEY=VALUE pair to the subprocess environment. Must
// be called before the first Send.
func (e *MCPEnv) WithEnv(key, value string) *MCPEnv {
	if e.started {
		e.t.Fatalf("MCPEnv.WithEnv: subprocess already started")
	}
	e.extraEnv = append(e.extraEnv, key+"="+value)
	return e
}

// WithConfigFile writes content as config.toml inside MCPEnv's
// configDir. Must be called before the first Send so the seeded file
// is in place when the subprocess reads its config.
func (e *MCPEnv) WithConfigFile(content string) *MCPEnv {
	if e.started {
		e.t.Fatalf("MCPEnv.WithConfigFile: subprocess already started")
	}
	path := filepath.Join(e.configDir, "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		e.t.Fatalf("writing config.toml: %v", err)
	}
	return e
}

// Send sends a JSON-RPC request and reads the response. The first call
// lazy-starts the subprocess, registers cleanup, and performs the MCP
// initialize handshake before forwarding the requested method.
func (e *MCPEnv) Send(method string, params any) jsonRPCResponse {
	e.t.Helper()
	if !e.started {
		e.start()
	}
	return e.rawSend(method, params)
}

// start spawns the `tusk mcp serve` subprocess with the configured
// flags and env. The subprocess inherits cmd.Dir from newCmd
// (per-call t.TempDir()) so tusk's walk-up config resolver never
// reaches an ancestor's tusk.toml.
func (e *MCPEnv) start() {
	e.t.Helper()

	cmd := newCmd(e.t, e.binPath, "--db", e.dbPath, "mcp", "serve")
	if e.homeDir != "" {
		cmd.Env = append(cmd.Env, "HOME="+e.homeDir, "USERPROFILE="+e.homeDir)
	} else {
		// Shield from the developer's real ~/.config/tusk by pointing
		// the subprocess at an isolated empty config dir. When WithHome
		// is set, the test wants HOME-based resolution, so skip the
		// override (TUSK_CONFIG_DIR shadows HOME-based lookup).
		cmd.Env = append(cmd.Env, "TUSK_CONFIG_DIR="+e.configDir)
	}
	cmd.Env = append(cmd.Env, e.extraEnv...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		e.t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		e.t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		e.t.Fatalf("starting mcp server: %v", err)
	}

	e.cmd = cmd
	e.stdin = stdin
	e.stdout = bufio.NewReader(stdout)
	e.started = true

	e.t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	})

	e.initialize()
}

// rawSend writes a JSON-RPC request and reads one response line. It
// assumes the subprocess is already running.
func (e *MCPEnv) rawSend(method string, params any) jsonRPCResponse {
	e.t.Helper()

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      e.nextID,
		Method:  method,
		Params:  params,
	}
	e.nextID++

	b, err := json.Marshal(req)
	if err != nil {
		e.t.Fatalf("marshaling request: %v", err)
	}

	if _, err := fmt.Fprintf(e.stdin, "%s\n", b); err != nil {
		e.t.Fatalf("writing request: %v", err)
	}

	line, err := e.stdout.ReadString('\n')
	if err != nil {
		e.t.Fatalf("reading response: %v", err)
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		e.t.Fatalf("unmarshaling response: %v\nraw: %s", err, line)
	}

	return resp
}

// initialize performs the MCP initialize handshake. Called once during
// lazy-start; tests do not invoke it directly.
func (e *MCPEnv) initialize() {
	e.t.Helper()
	resp := e.rawSend("initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "tusk-e2e-test",
			"version": "1.0.0",
		},
	})
	if resp.Error != nil {
		e.t.Fatalf("initialize failed: %s", resp.Error)
	}

	notif := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	b, _ := json.Marshal(notif)
	if _, err := fmt.Fprintf(e.stdin, "%s\n", b); err != nil {
		e.t.Fatalf("writing initialized notification: %v", err)
	}
}

// callTool sends a tools/call request and returns the parsed JSON
// content as a map. Fails the test on protocol error or isError=true.
func (e *MCPEnv) callTool(name string, args map[string]any) map[string]any {
	e.t.Helper()
	resp := e.Send("tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if resp.Error != nil {
		e.t.Fatalf("tool %s returned error: %s", name, resp.Error)
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		e.t.Fatalf("parsing tool result: %v", err)
	}
	if result.IsError {
		e.t.Fatalf("tool %s returned isError=true: %s", name, result.Content[0].Text)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &parsed); err != nil {
		e.t.Fatalf("parsing tool JSON content: %v\nraw: %s", err, result.Content[0].Text)
	}
	return parsed
}

// callToolRaw sends a tools/call request and returns the raw text
// content. Fails the test on protocol error.
func (e *MCPEnv) callToolRaw(name string, args map[string]any) string {
	e.t.Helper()
	resp := e.Send("tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if resp.Error != nil {
		e.t.Fatalf("tool %s returned error: %s", name, resp.Error)
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		e.t.Fatalf("parsing tool result: %v", err)
	}
	return result.Content[0].Text
}

// callToolExpectError sends a tools/call request and expects an
// isError=true result. Returns the error text.
func (e *MCPEnv) callToolExpectError(name string, args map[string]any) string {
	e.t.Helper()
	resp := e.Send("tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if resp.Error != nil {
		e.t.Fatalf("tool %s returned protocol error (expected tool error): %s", name, resp.Error)
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		e.t.Fatalf("parsing tool result: %v", err)
	}
	if !result.IsError {
		e.t.Fatalf("expected isError=true, got false. content: %s", result.Content[0].Text)
	}
	return result.Content[0].Text
}
