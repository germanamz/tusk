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

func TestCLI_ConfigGet(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	homeDir := t.TempDir()
	// Init config first.
	initCmd := exec.Command(binPath, "config", "init")
	initCmd.Env = envWithHome(homeDir)
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("config init: %v\n%s", err, out)
	}

	t.Run("scalar_bool", func(t *testing.T) {
		cmd := exec.Command(binPath, "config", "get", "tui.color")
		cmd.Env = envWithHome(homeDir)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("config get: %v\n%s", err, out)
		}
		if strings.TrimSpace(string(out)) != "true" {
			t.Errorf("got %q, want %q", strings.TrimSpace(string(out)), "true")
		}
	})

	t.Run("scalar_float", func(t *testing.T) {
		cmd := exec.Command(binPath, "config", "get", "urgency.due_weight")
		cmd.Env = envWithHome(homeDir)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("config get: %v\n%s", err, out)
		}
		if strings.TrimSpace(string(out)) != "12" {
			t.Errorf("got %q, want %q", strings.TrimSpace(string(out)), "12")
		}
	})

	t.Run("complex_value", func(t *testing.T) {
		cmd := exec.Command(binPath, "config", "get", "workflows.kanban.statuses")
		cmd.Env = envWithHome(homeDir)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("config get: %v\n%s", err, out)
		}
		// Should be a JSON object (map[string]StatusConfig).
		var obj map[string]interface{}
		if err := json.Unmarshal(out, &obj); err != nil {
			t.Fatalf("expected JSON object, got: %s", out)
		}
		if len(obj) != 4 {
			t.Errorf("expected 4 statuses, got %d", len(obj))
		}
	})

	t.Run("unknown_key", func(t *testing.T) {
		cmd := exec.Command(binPath, "config", "get", "nonexistent.key")
		cmd.Env = envWithHome(homeDir)
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatal("expected error for unknown key")
		}
		if !strings.Contains(string(out), "unknown config key") {
			t.Errorf("expected 'unknown config key' error, got: %s", out)
		}
	})
}

func TestCLI_ConfigSet(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	homeDir := t.TempDir()
	env := envWithHome(homeDir)

	// Init config first.
	initCmd := exec.Command(binPath, "config", "init")
	initCmd.Env = env
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("config init: %v\n%s", err, out)
	}

	t.Run("set_scalar", func(t *testing.T) {
		cmd := exec.Command(binPath, "config", "set", "tui.color", "false")
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("config set: %v\n%s", err, out)
		}

		// Verify the change persisted.
		getCmd := exec.Command(binPath, "config", "get", "tui.color")
		getCmd.Env = env
		out, err := getCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("config get: %v\n%s", err, out)
		}
		if strings.TrimSpace(string(out)) != "false" {
			t.Errorf("got %q, want %q", strings.TrimSpace(string(out)), "false")
		}
	})

	t.Run("set_list", func(t *testing.T) {
		cmd := exec.Command(binPath, "config", "set", "mcp.disabled_tools", "tusk_task_delete,tusk_task_tree")
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("config set: %v\n%s", err, out)
		}

		// Verify.
		getCmd := exec.Command(binPath, "config", "get", "mcp.disabled_tools")
		getCmd.Env = env
		out, err := getCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("config get: %v\n%s", err, out)
		}
		var arr []string
		if err := json.Unmarshal(out, &arr); err != nil {
			t.Fatalf("expected JSON array, got: %s", out)
		}
		if len(arr) != 2 || arr[0] != "tusk_task_delete" || arr[1] != "tusk_task_tree" {
			t.Errorf("unexpected disabled_tools: %v", arr)
		}
	})

	t.Run("reject_unknown_key", func(t *testing.T) {
		cmd := exec.Command(binPath, "config", "set", "nonexistent.key", "value")
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatal("expected error for unknown key")
		}
		if !strings.Contains(string(out), "unknown config key") {
			t.Errorf("expected 'unknown config key' error, got: %s", out)
		}
	})

	t.Run("reject_invalid_config", func(t *testing.T) {
		cmd := exec.Command(binPath, "config", "set", "projects.default.workflow", "nonexistent_workflow")
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatal("expected error for invalid config")
		}
		if !strings.Contains(string(out), "unknown workflow") {
			t.Errorf("expected workflow validation error, got: %s", out)
		}
	})

	t.Run("works_with_auto_created_config", func(t *testing.T) {
		// Even with a fresh HOME (no pre-existing config), config set works
		// because Load() in main.go auto-creates the file via ensureConfigFile().
		freshHome := t.TempDir()
		cmd := exec.Command(binPath, "config", "set", "tui.color", "false")
		cmd.Env = envWithHome(freshHome)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("config set with fresh home: %v\n%s", err, out)
		}

		// Verify it persisted.
		getCmd := exec.Command(binPath, "config", "get", "tui.color")
		getCmd.Env = envWithHome(freshHome)
		out, err := getCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("config get: %v\n%s", err, out)
		}
		if strings.TrimSpace(string(out)) != "false" {
			t.Errorf("got %q, want %q", strings.TrimSpace(string(out)), "false")
		}
	})
}

func TestCLI_ConfigValidate(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	t.Run("valid_config", func(t *testing.T) {
		homeDir := t.TempDir()
		// Init config (writes valid defaults).
		initCmd := exec.Command(binPath, "config", "init")
		initCmd.Env = envWithHome(homeDir)
		if out, err := initCmd.CombinedOutput(); err != nil {
			t.Fatalf("config init: %v\n%s", err, out)
		}

		cmd := exec.Command(binPath, "config", "validate")
		cmd.Env = envWithHome(homeDir)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("config validate: %v\n%s", err, out)
		}
		if !strings.Contains(string(out), "Config valid") {
			t.Errorf("expected 'Config valid', got: %s", out)
		}
	})

	t.Run("invalid_config", func(t *testing.T) {
		homeDir := t.TempDir()
		tuskConfigDir := filepath.Join(homeDir, ".config", "tusk")
		if err := os.MkdirAll(tuskConfigDir, 0o755); err != nil {
			t.Fatal(err)
		}
		// Write config with project referencing nonexistent workflow.
		badConfig := []byte("[projects.default]\nworkflow = \"nonexistent\"\n")
		if err := os.WriteFile(filepath.Join(tuskConfigDir, "config.toml"), badConfig, 0o644); err != nil {
			t.Fatal(err)
		}

		cmd := exec.Command(binPath, "config", "validate")
		cmd.Env = envWithHome(homeDir)
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatal("expected error for invalid config")
		}
		if !strings.Contains(string(out), "unknown workflow") {
			t.Errorf("expected workflow validation error, got: %s", out)
		}
	})
}

// envWithHome builds an env slice with HOME overridden and TUSK_DB removed.
func envWithHome(home string) []string {
	var env []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "HOME=") || strings.HasPrefix(e, "TUSK_DB=") || strings.HasPrefix(e, "TUSK_CONFIG=") {
			continue
		}
		env = append(env, e)
	}
	return append(env, "HOME="+home)
}

func TestCLI_ExplicitConfigFlag(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	// Build a custom config file in a directory that is NOT the HOME config dir.
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "explicit.db")
	configDir := t.TempDir()
	configFile := filepath.Join(configDir, "tusk-custom.toml")
	configContent := []byte("[storage]\npath = \"" + dbPath + "\"\n")
	if err := os.WriteFile(configFile, configContent, 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	// Use a fresh HOME so there is no fallback config to confuse the test.
	homeDir := t.TempDir()

	cmd := exec.Command(binPath, "--config", configFile, "add", "Explicit config task")
	cmd.Env = envWithHome(homeDir)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("tusk --config ... add failed: %v\nstderr: %s", err, stderr.String())
	}

	// The custom DB should exist at the path the explicit config pointed to.
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatalf("expected DB at %s from explicit config, but it does not exist", dbPath)
	}
}

func TestCLI_TuskConfigEnv(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "env.db")
	configDir := t.TempDir()
	configFile := filepath.Join(configDir, "tusk-env.toml")
	configContent := []byte("[storage]\npath = \"" + dbPath + "\"\n")
	if err := os.WriteFile(configFile, configContent, 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	homeDir := t.TempDir()

	cmd := exec.Command(binPath, "add", "Env config task")
	env := envWithHome(homeDir)
	env = append(env, "TUSK_CONFIG="+configFile)
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("tusk add with TUSK_CONFIG failed: %v\nstderr: %s", err, stderr.String())
	}

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatalf("expected DB at %s from TUSK_CONFIG, but it does not exist", dbPath)
	}
}

func TestCLI_FlagBeatsEnv(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	// Two config files pointing at two different DBs. --config should win.
	dir := t.TempDir()
	flagDB := filepath.Join(dir, "flag.db")
	envDB := filepath.Join(dir, "env.db")

	flagFile := filepath.Join(dir, "flag.toml")
	envFile := filepath.Join(dir, "env.toml")
	if err := os.WriteFile(flagFile, []byte("[storage]\npath = \""+flagDB+"\"\n"), 0o644); err != nil {
		t.Fatalf("writing flag config: %v", err)
	}
	if err := os.WriteFile(envFile, []byte("[storage]\npath = \""+envDB+"\"\n"), 0o644); err != nil {
		t.Fatalf("writing env config: %v", err)
	}

	homeDir := t.TempDir()
	cmd := exec.Command(binPath, "--config", flagFile, "add", "Precedence task")
	env := envWithHome(homeDir)
	env = append(env, "TUSK_CONFIG="+envFile)
	cmd.Env = env
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("tusk failed: %v\nstderr: %s", err, stderr.String())
	}

	if _, err := os.Stat(flagDB); os.IsNotExist(err) {
		t.Fatalf("expected DB at flag path %s, but it does not exist", flagDB)
	}
	if _, err := os.Stat(envDB); err == nil {
		t.Fatalf("unexpected DB at env path %s — flag should have won", envDB)
	}
}

func TestCLI_MissingExplicitConfigIsHardError(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	missing := filepath.Join(t.TempDir(), "does-not-exist.toml")
	homeDir := t.TempDir()

	cmd := exec.Command(binPath, "--config", missing, "list")
	cmd.Env = envWithHome(homeDir)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected tusk to exit non-zero when --config points at a missing file")
	}
	if !strings.Contains(stderr.String(), "config file not found") {
		t.Fatalf("expected 'config file not found' in stderr, got: %s", stderr.String())
	}
}

func TestCLI_TuskEnvOverlaysExplicitConfig(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	// Config file says one DB path; TUSK_STORAGE_PATH env overrides it.
	dir := t.TempDir()
	fileDB := filepath.Join(dir, "from-file.db")
	envDB := filepath.Join(dir, "from-env.db")
	configFile := filepath.Join(dir, "custom.toml")
	if err := os.WriteFile(configFile, []byte("[storage]\npath = \""+fileDB+"\"\n"), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	homeDir := t.TempDir()
	cmd := exec.Command(binPath, "--config", configFile, "add", "Overlay task")
	env := envWithHome(homeDir)
	env = append(env, "TUSK_STORAGE_PATH="+envDB)
	cmd.Env = env
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("tusk failed: %v\nstderr: %s", err, stderr.String())
	}

	if _, err := os.Stat(envDB); os.IsNotExist(err) {
		t.Fatalf("expected DB at env-override path %s", envDB)
	}
	if _, err := os.Stat(fileDB); err == nil {
		t.Fatalf("unexpected DB at file path %s — env should have overridden it", fileDB)
	}
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
