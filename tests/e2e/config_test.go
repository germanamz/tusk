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
