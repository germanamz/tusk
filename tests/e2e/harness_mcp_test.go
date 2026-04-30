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
	test      *testing.T
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
// test.TempDir() and returns a not-yet-started MCPEnv. The subprocess is
// started lazily on the first Send call.
func NewMCPEnv(test *testing.T, binPath string) *MCPEnv {
	test.Helper()
	tmpFile, createErr := os.CreateTemp(test.TempDir(), "tusk-mcp-e2e-*.db")

	if createErr != nil {
		test.Fatalf("creating temp db: %v", createErr)
	}

	_ = tmpFile.Close()

	return &MCPEnv{
		test:      test,
		binPath:   binPath,
		nextID:    1,
		dbPath:    tmpFile.Name(),
		configDir: test.TempDir(),
	}
}

// WithHome overrides HOME (and USERPROFILE on Windows) for the
// subprocess. Must be called before the first Send. When set, MCPEnv
// skips injecting TUSK_CONFIG_DIR so tusk's config resolver falls
// through to <homeDir>/.config/tusk — the path tests typically seed
// with a config.toml under WithHome.
func (env *MCPEnv) WithHome(dir string) *MCPEnv {
	if env.started {
		env.test.Fatalf("MCPEnv.WithHome: subprocess already started")
	}
	env.homeDir = dir
	return env
}

// WithEnv appends a KEY=VALUE pair to the subprocess environment. Must
// be called before the first Send.
func (env *MCPEnv) WithEnv(key, value string) *MCPEnv {
	if env.started {
		env.test.Fatalf("MCPEnv.WithEnv: subprocess already started")
	}
	env.extraEnv = append(env.extraEnv, key+"="+value)
	return env
}

// WithConfigFile writes content as config.toml inside MCPEnv's
// configDir. Must be called before the first Send so the seeded file
// is in place when the subprocess reads its config.
func (env *MCPEnv) WithConfigFile(content string) *MCPEnv {
	if env.started {
		env.test.Fatalf("MCPEnv.WithConfigFile: subprocess already started")
	}
	path := filepath.Join(env.configDir, "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		env.test.Fatalf("writing config.toml: %v", err)
	}
	return env
}

// Send sends a JSON-RPC request and reads the response. The first call
// lazy-starts the subprocess, registers cleanup, and performs the MCP
// initialize handshake before forwarding the requested method.
func (env *MCPEnv) Send(method string, params any) jsonRPCResponse {
	env.test.Helper()
	if !env.started {
		env.start()
	}
	return env.rawSend(method, params)
}

// start spawns the `tusk mcp serve` subprocess with the configured
// flags and env. The subprocess inherits cmd.Dir from newCmd
// (per-call test.TempDir()) so tusk's walk-up config resolver never
// reaches an ancestor's tusk.toml.
func (env *MCPEnv) start() {
	env.test.Helper()

	cmd := newCmd(env.test, env.binPath, "--db", env.dbPath, "mcp", "serve")
	if env.homeDir != "" {
		cmd.Env = append(cmd.Env, "HOME="+env.homeDir, "USERPROFILE="+env.homeDir)
	} else {
		// Shield from the developer's real ~/.config/tusk by pointing
		// the subprocess at an isolated empty config dir. When WithHome
		// is set, the test wants HOME-based resolution, so skip the
		// override (TUSK_CONFIG_DIR shadows HOME-based lookup).
		cmd.Env = append(cmd.Env, "TUSK_CONFIG_DIR="+env.configDir)
	}
	cmd.Env = append(cmd.Env, env.extraEnv...)

	stdinPipe, stdinErr := cmd.StdinPipe()

	if stdinErr != nil {
		env.test.Fatalf("stdin pipe: %v", stdinErr)
	}

	stdoutPipe, stdoutErr := cmd.StdoutPipe()

	if stdoutErr != nil {
		env.test.Fatalf("stdout pipe: %v", stdoutErr)
	}

	cmd.Stderr = os.Stderr

	if startErr := cmd.Start(); startErr != nil {
		env.test.Fatalf("starting mcp server: %v", startErr)
	}

	env.cmd = cmd
	env.stdin = stdinPipe
	env.stdout = bufio.NewReader(stdoutPipe)
	env.started = true

	env.test.Cleanup(func() {
		_ = stdinPipe.Close()
		_ = cmd.Wait()
	})

	env.initialize()
}

// rawSend writes a JSON-RPC request and reads one response line. It
// assumes the subprocess is already running.
func (env *MCPEnv) rawSend(method string, params any) jsonRPCResponse {
	env.test.Helper()

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      env.nextID,
		Method:  method,
		Params:  params,
	}
	env.nextID++

	encoded, marshalErr := json.Marshal(req)

	if marshalErr != nil {
		env.test.Fatalf("marshaling request: %v", marshalErr)
	}

	if _, writeErr := fmt.Fprintf(env.stdin, "%s\n", encoded); writeErr != nil {
		env.test.Fatalf("writing request: %v", writeErr)
	}

	line, readErr := env.stdout.ReadString('\n')

	if readErr != nil {
		env.test.Fatalf("reading response: %v", readErr)
	}

	var resp jsonRPCResponse
	if unmarshalErr := json.Unmarshal([]byte(line), &resp); unmarshalErr != nil {
		env.test.Fatalf("unmarshaling response: %v\nraw: %s", unmarshalErr, line)
	}

	return resp
}

// initialize performs the MCP initialize handshake. Called once during
// lazy-start; tests do not invoke it directly.
func (env *MCPEnv) initialize() {
	env.test.Helper()
	resp := env.rawSend("initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "tusk-e2e-test",
			"version": "1.0.0",
		},
	})
	if resp.Error != nil {
		env.test.Fatalf("initialize failed: %s", resp.Error)
	}

	notif := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	encoded, _ := json.Marshal(notif)
	if _, err := fmt.Fprintf(env.stdin, "%s\n", encoded); err != nil {
		env.test.Fatalf("writing initialized notification: %v", err)
	}
}

// callTool sends a tools/call request and returns the parsed JSON
// content as a map. Fails the test on protocol error or isError=true.
func (env *MCPEnv) callTool(name string, args map[string]any) map[string]any {
	env.test.Helper()
	resp := env.Send("tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if resp.Error != nil {
		env.test.Fatalf("tool %s returned error: %s", name, resp.Error)
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		env.test.Fatalf("parsing tool result: %v", err)
	}
	if result.IsError {
		env.test.Fatalf("tool %s returned isError=true: %s", name, result.Content[0].Text)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &parsed); err != nil {
		env.test.Fatalf("parsing tool JSON content: %v\nraw: %s", err, result.Content[0].Text)
	}
	return parsed
}

// callToolRaw sends a tools/call request and returns the raw text
// content. Fails the test on protocol error.
func (env *MCPEnv) callToolRaw(name string, args map[string]any) string {
	env.test.Helper()
	resp := env.Send("tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if resp.Error != nil {
		env.test.Fatalf("tool %s returned error: %s", name, resp.Error)
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		env.test.Fatalf("parsing tool result: %v", err)
	}
	return result.Content[0].Text
}

// callToolExpectError sends a tools/call request and expects an
// isError=true result. Returns the error text.
func (env *MCPEnv) callToolExpectError(name string, args map[string]any) string {
	env.test.Helper()
	resp := env.Send("tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if resp.Error != nil {
		env.test.Fatalf("tool %s returned protocol error (expected tool error): %s", name, resp.Error)
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		env.test.Fatalf("parsing tool result: %v", err)
	}
	if !result.IsError {
		env.test.Fatalf("expected isError=true, got false. content: %s", result.Content[0].Text)
	}
	return result.Content[0].Text
}
