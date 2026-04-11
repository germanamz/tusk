package e2e

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLI_WithConfigFile(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	// Create a temp directory for the custom DB.
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "custom.db")

	// Set up a fake HOME with a config file pointing to the custom DB path.
	homeDir := t.TempDir()
	tuskConfigDir := filepath.Join(homeDir, ".config", "tusk")
	if err := os.MkdirAll(tuskConfigDir, 0o755); err != nil {
		t.Fatalf("creating config dir: %v", err)
	}
	configContent := []byte("[storage]\npath = \"" + dbPath + "\"\n")
	if err := os.WriteFile(filepath.Join(tuskConfigDir, "config.toml"), configContent, 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	// Run tusk add (no --db flag, no TUSK_DB) with HOME overridden.
	cmd := exec.Command(binPath, "add", "Config test task")
	cmd.Env = envWithHome(homeDir)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("tusk add failed: %v\nstderr: %s", err, stderr.String())
	}

	// Verify the DB file was created at the custom path.
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatalf("expected DB at %s but it doesn't exist", dbPath)
	}
}

func TestMCP_DisabledTools(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	// Create config that disables the relation tool group.
	homeDir := t.TempDir()
	tuskConfigDir := filepath.Join(homeDir, ".config", "tusk")
	if err := os.MkdirAll(tuskConfigDir, 0o755); err != nil {
		t.Fatalf("creating config dir: %v", err)
	}
	configContent := []byte("[mcp]\ndisabled_tool_groups = [\"relation\"]\n")
	if err := os.WriteFile(filepath.Join(tuskConfigDir, "config.toml"), configContent, 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	// Start MCP server with this config.
	env := newMCPEnvWithHome(t, binPath, homeDir)

	// List tools.
	resp := env.send("tools/list", nil)
	if resp.Error != nil {
		t.Fatalf("tools/list error: %s", resp.Error)
	}

	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("parsing tools/list: %v", err)
	}

	for _, tool := range result.Tools {
		if tool.Name == "tusk_relation_add" || tool.Name == "tusk_relation_remove" {
			t.Errorf("tool %s should be disabled but was listed", tool.Name)
		}
	}

	// Verify non-disabled tools are still present.
	foundTaskCreate := false
	for _, tool := range result.Tools {
		if tool.Name == "tusk_task_create" {
			foundTaskCreate = true
		}
	}
	if !foundTaskCreate {
		t.Error("tusk_task_create should be listed but was not found")
	}
}

func TestCLI_ConfigInit(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	homeDir := t.TempDir()
	configPath := filepath.Join(homeDir, ".config", "tusk", "config.toml")

	// Run config init: startup auto-creates the file via ensureConfigFile in config.Load,
	// so config init will always report "already exists" and succeed.
	cmd := exec.Command(binPath, "config", "init")
	cmd.Env = envWithHome(homeDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("config init failed: %v\noutput: %s", err, out)
	}

	// Verify file exists (created by startup or by config init itself).
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("config file was not created")
	}

	// Second run: should also succeed and report already exists.
	cmd = exec.Command(binPath, "config", "init")
	cmd.Env = envWithHome(homeDir)
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("config init (second run) failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "already exists") {
		t.Errorf("expected 'already exists' in output, got: %s", out)
	}
}

func TestCLI_ConfigPath(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	homeDir := t.TempDir()
	cmd := exec.Command(binPath, "config", "path")
	cmd.Env = envWithHome(homeDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("config path failed: %v\noutput: %s", err, out)
	}
	expected := filepath.Join(homeDir, ".config", "tusk", "config.toml")
	if strings.TrimSpace(string(out)) != expected {
		t.Errorf("got %q, want %q", strings.TrimSpace(string(out)), expected)
	}
}

func TestCLI_ConfigShow(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	homeDir := t.TempDir()
	// Config init first to create the file.
	initCmd := exec.Command(binPath, "config", "init")
	initCmd.Env = envWithHome(homeDir)
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("config init failed: %v\noutput: %s", err, out)
	}

	cmd := exec.Command(binPath, "config", "show")
	cmd.Env = envWithHome(homeDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("config show failed: %v\noutput: %s", err, out)
	}

	output := string(out)
	// Should contain key TOML sections.
	if !strings.Contains(output, "[storage]") {
		t.Error("config show output missing [storage] section")
	}
	if !strings.Contains(output, "[urgency]") {
		t.Error("config show output missing [urgency] section")
	}
	if !strings.Contains(output, "[tui]") {
		t.Error("config show output missing [tui] section")
	}
}

// envWithHome builds an env slice with HOME overridden and TUSK_DB removed.
func envWithHome(home string) []string {
	var env []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "HOME=") || strings.HasPrefix(e, "TUSK_DB=") {
			continue
		}
		env = append(env, e)
	}
	return append(env, "HOME="+home)
}

// newMCPEnvWithHome starts an MCP server with a custom HOME directory.
func newMCPEnvWithHome(t *testing.T, binPath, home string) *mcpEnv {
	t.Helper()
	tmpFile, err := os.CreateTemp(t.TempDir(), "tusk-mcp-e2e-*.db")
	if err != nil {
		t.Fatalf("creating temp db: %v", err)
	}
	_ = tmpFile.Close()

	cmd := exec.Command(binPath, "--db", tmpFile.Name(), "mcp", "serve")
	cmd.Env = envWithHome(home)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("starting mcp server: %v", err)
	}

	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	})

	e := &mcpEnv{
		t:      t,
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		nextID: 1,
	}

	e.initialize()
	return e
}
