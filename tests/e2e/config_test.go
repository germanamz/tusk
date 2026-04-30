package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLI_WithConfigFile(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	// Create a temp directory for the custom DB.
	dbDir := test.TempDir()
	dbPath := filepath.Join(dbDir, "custom.db")

	// Set up a fake HOME with a config file pointing to the custom DB path.
	homeDir := test.TempDir()
	tuskConfigDir := filepath.Join(homeDir, ".config", "tusk")
	if err := os.MkdirAll(tuskConfigDir, 0o755); err != nil {
		test.Fatalf("creating config dir: %v", err)
	}
	configContent := []byte("[storage]\npath = \"" + dbPath + "\"\n")
	if err := os.WriteFile(filepath.Join(tuskConfigDir, "config.toml"), configContent, 0o644); err != nil {
		test.Fatalf("writing config: %v", err)
	}

	// Run tusk add (no --db flag, no TUSK_DB) with HOME overridden.
	env := newEnv(test, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()
	result := env.Run("task", "create", "Config test task")
	if result.Err != nil {
		test.Fatalf("tusk add failed: %v\nstderr: %s", result.Err, result.Stderr)
	}

	// Verify the DB file was created at the custom path.
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		test.Fatalf("expected DB at %s but it doesn't exist", dbPath)
	}
}

func TestMCP_DisabledTools(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	// Create config that disables the task_relations tool group.
	homeDir := test.TempDir()
	tuskConfigDir := filepath.Join(homeDir, ".config", "tusk")
	if err := os.MkdirAll(tuskConfigDir, 0o755); err != nil {
		test.Fatalf("creating config dir: %v", err)
	}
	configContent := []byte("[mcp]\ndisabled_tool_groups = [\"task_relations\"]\n")
	if err := os.WriteFile(filepath.Join(tuskConfigDir, "config.toml"), configContent, 0o644); err != nil {
		test.Fatalf("writing config: %v", err)
	}

	// Start MCP server with this config.
	env := NewMCPEnv(test, binPath).WithHome(homeDir)

	// List tools.
	resp := env.Send("tools/list", nil)
	if resp.Error != nil {
		test.Fatalf("tools/list error: %s", resp.Error)
	}

	var toolsResult struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &toolsResult); err != nil {
		test.Fatalf("parsing tools/list: %v", err)
	}

	for _, tool := range toolsResult.Tools {
		if tool.Name == "tusk_task_link" || tool.Name == "tusk_task_unlink" {
			test.Errorf("tool %s should be disabled but was listed", tool.Name)
		}
	}

	// Verify non-disabled tools are still present.
	foundTaskCreate := false
	for _, tool := range toolsResult.Tools {
		if tool.Name == "tusk_task_create" {
			foundTaskCreate = true
		}
	}
	if !foundTaskCreate {
		test.Error("tusk_task_create should be listed but was not found")
	}
}

func TestCLI_ConfigInit(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	homeDir := test.TempDir()
	configPath := filepath.Join(homeDir, ".config", "tusk", "config.toml")

	env := newEnv(test, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()

	// Run config init: startup auto-creates the file via ensureConfigFile in config.Load,
	// so config init will always report "already exists" and succeed.
	result := env.Run("config", "init")
	if result.Err != nil {
		test.Fatalf("config init failed: %v\nstdout: %s\nstderr: %s", result.Err, result.Stdout, result.Stderr)
	}

	// Verify file exists (created by startup or by config init itself).
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		test.Fatal("config file was not created")
	}

	// Second run: should also succeed and report already exists.
	result = env.Run("config", "init")
	if result.Err != nil {
		test.Fatalf("config init (second run) failed: %v\nstdout: %s\nstderr: %s", result.Err, result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "already exists") {
		test.Errorf("expected 'already exists' in output, got: %s", result.Stdout)
	}
}

func TestCLI_ConfigPath(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	homeDir := test.TempDir()
	env := newEnv(test, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()

	result := env.Run("config", "path")
	if result.Err != nil {
		test.Fatalf("config path failed: %v\nstdout: %s\nstderr: %s", result.Err, result.Stdout, result.Stderr)
	}
	expected := filepath.Join(homeDir, ".config", "tusk", "config.toml")
	if strings.TrimSpace(result.Stdout) != expected {
		test.Errorf("got %q, want %q", strings.TrimSpace(result.Stdout), expected)
	}
}

func TestCLI_ConfigShow(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	homeDir := test.TempDir()
	env := newEnv(test, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()

	// Config init first to create the file.
	if initResult := env.Run("config", "init"); initResult.Err != nil {
		test.Fatalf("config init failed: %v\nstdout: %s\nstderr: %s", initResult.Err, initResult.Stdout, initResult.Stderr)
	}

	result := env.Run("config", "show")
	if result.Err != nil {
		test.Fatalf("config show failed: %v\nstdout: %s\nstderr: %s", result.Err, result.Stdout, result.Stderr)
	}

	// Should contain key TOML sections.
	if !strings.Contains(result.Stdout, "[storage]") {
		test.Error("config show output missing [storage] section")
	}
	if !strings.Contains(result.Stdout, "[urgency]") {
		test.Error("config show output missing [urgency] section")
	}
	if !strings.Contains(result.Stdout, "[tui]") {
		test.Error("config show output missing [tui] section")
	}
}

func TestCLI_ConfigGet(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	homeDir := test.TempDir()
	env := newEnv(test, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()

	// Init config first.
	if initResult := env.Run("config", "init"); initResult.Err != nil {
		test.Fatalf("config init: %v\nstdout: %s\nstderr: %s", initResult.Err, initResult.Stdout, initResult.Stderr)
	}

	test.Run("scalar_bool", func(test *testing.T) {
		result := env.Run("config", "get", "tui.color")
		if result.Err != nil {
			test.Fatalf("config get: %v\nstdout: %s\nstderr: %s", result.Err, result.Stdout, result.Stderr)
		}
		if strings.TrimSpace(result.Stdout) != "true" {
			test.Errorf("got %q, want %q", strings.TrimSpace(result.Stdout), "true")
		}
	})

	test.Run("scalar_float", func(test *testing.T) {
		result := env.Run("config", "get", "urgency.due_weight")
		if result.Err != nil {
			test.Fatalf("config get: %v\nstdout: %s\nstderr: %s", result.Err, result.Stdout, result.Stderr)
		}
		if strings.TrimSpace(result.Stdout) != "12" {
			test.Errorf("got %q, want %q", strings.TrimSpace(result.Stdout), "12")
		}
	})

	test.Run("complex_value", func(test *testing.T) {
		// `config get workflows.kanban.statuses` hydrates the workflow row
		// from the database and returns a JSON object keyed by status name.
		result := env.Run("config", "get", "workflows.kanban.statuses")
		if result.Err != nil {
			test.Fatalf("config get: %v\nstdout: %s\nstderr: %s", result.Err, result.Stdout, result.Stderr)
		}
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(result.Stdout), &obj); err != nil {
			test.Fatalf("expected JSON object, got: %s", result.Stdout)
		}
		if len(obj) != 4 {
			test.Errorf("expected 4 statuses, got %d", len(obj))
		}
	})

	test.Run("unknown_key", func(test *testing.T) {
		result := env.Run("config", "get", "nonexistent.key")
		if result.Err == nil {
			test.Fatal("expected error for unknown key")
		}
		if !strings.Contains(result.Stderr, "unknown config key") {
			test.Errorf("expected 'unknown config key' error, got stdout=%q stderr=%q", result.Stdout, result.Stderr)
		}
	})
}

func TestCLI_ConfigSet(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	homeDir := test.TempDir()
	env := newEnv(test, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()

	// Init config first.
	if initResult := env.Run("config", "init"); initResult.Err != nil {
		test.Fatalf("config init: %v\nstdout: %s\nstderr: %s", initResult.Err, initResult.Stdout, initResult.Stderr)
	}

	test.Run("set_scalar", func(test *testing.T) {
		if setResult := env.Run("config", "set", "tui.color", "false"); setResult.Err != nil {
			test.Fatalf("config set: %v\nstdout: %s\nstderr: %s", setResult.Err, setResult.Stdout, setResult.Stderr)
		}

		// Verify the change persisted.
		result := env.Run("config", "get", "tui.color")
		if result.Err != nil {
			test.Fatalf("config get: %v\nstdout: %s\nstderr: %s", result.Err, result.Stdout, result.Stderr)
		}
		if strings.TrimSpace(result.Stdout) != "false" {
			test.Errorf("got %q, want %q", strings.TrimSpace(result.Stdout), "false")
		}
	})

	test.Run("set_list", func(test *testing.T) {
		if setResult := env.Run("config", "set", "mcp.disabled_tools", "tusk_task_delete,tusk_task_tree"); setResult.Err != nil {
			test.Fatalf("config set: %v\nstdout: %s\nstderr: %s", setResult.Err, setResult.Stdout, setResult.Stderr)
		}

		// Verify.
		result := env.Run("config", "get", "mcp.disabled_tools")
		if result.Err != nil {
			test.Fatalf("config get: %v\nstdout: %s\nstderr: %s", result.Err, result.Stdout, result.Stderr)
		}
		var arr []string
		if err := json.Unmarshal([]byte(result.Stdout), &arr); err != nil {
			test.Fatalf("expected JSON array, got: %s", result.Stdout)
		}
		if len(arr) != 2 || arr[0] != "tusk_task_delete" || arr[1] != "tusk_task_tree" {
			test.Errorf("unexpected disabled_tools: %v", arr)
		}
	})

	test.Run("reject_unknown_key", func(test *testing.T) {
		result := env.Run("config", "set", "nonexistent.key", "value")
		if result.Err == nil {
			test.Fatal("expected error for unknown key")
		}
		if !strings.Contains(result.Stderr, "unknown config key") {
			test.Errorf("expected 'unknown config key' error, got stdout=%q stderr=%q", result.Stdout, result.Stderr)
		}
	})

	test.Run("reject_projects_write", func(test *testing.T) {
		result := env.Run("config", "set", "projects.default.workflow", "kanban")
		if result.Err == nil {
			test.Fatal("expected error for projects.* write")
		}
		if !strings.Contains(result.Stderr, "projects.* is managed by the database") {
			test.Errorf("expected projects.* rejection, got stdout=%q stderr=%q", result.Stdout, result.Stderr)
		}
	})

	test.Run("reject_workflows_write", func(test *testing.T) {
		result := env.Run("config", "set", "workflows.kanban.statuses.pending.roles", "initial")
		if result.Err == nil {
			test.Fatal("expected error for workflows.* write")
		}
		if !strings.Contains(result.Stderr, "workflows.* is managed by the database") {
			test.Errorf("expected workflows.* rejection, got stdout=%q stderr=%q", result.Stdout, result.Stderr)
		}
	})

	test.Run("works_with_auto_created_config", func(test *testing.T) {
		// Even with a fresh HOME (no pre-existing config), config set works
		// because Load() in main.go auto-creates the file via ensureConfigFile().
		freshHome := test.TempDir()
		freshEnv := newEnv(test, binPath, "flag", "text")
		freshEnv.WithHome(freshHome)
		freshEnv.WithoutDBArg()

		if setResult := freshEnv.Run("config", "set", "tui.color", "false"); setResult.Err != nil {
			test.Fatalf("config set with fresh home: %v\nstdout: %s\nstderr: %s", setResult.Err, setResult.Stdout, setResult.Stderr)
		}

		// Verify it persisted.
		result := freshEnv.Run("config", "get", "tui.color")
		if result.Err != nil {
			test.Fatalf("config get: %v\nstdout: %s\nstderr: %s", result.Err, result.Stdout, result.Stderr)
		}
		if strings.TrimSpace(result.Stdout) != "false" {
			test.Errorf("got %q, want %q", strings.TrimSpace(result.Stdout), "false")
		}
	})
}

func TestCLI_ConfigSet_BlockedFields(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	homeDir := test.TempDir()
	env := newEnv(test, binPath, "flag", "json")
	env.WithHome(homeDir)
	env.WithoutDBArg()

	if initResult := env.Run("config", "init"); initResult.Err != nil {
		test.Fatalf("config init: %v\nstdout: %s\nstderr: %s", initResult.Err, initResult.Stdout, initResult.Stderr)
	}

	if setResult := env.Run("config", "set", "mcp.blocked_fields.tusk_task_modify", "priority"); setResult.Err != nil {
		test.Fatalf("config set mcp.blocked_fields.tusk_task_modify: %v\nstdout: %s\nstderr: %s", setResult.Err, setResult.Stdout, setResult.Stderr)
	}

	result := env.Run("config", "show")
	if result.Err != nil {
		test.Fatalf("config show --format json: %v\nstdout: %s\nstderr: %s", result.Err, result.Stdout, result.Stderr)
	}

	var parsed struct {
		MCP struct {
			BlockedFields map[string][]string `json:"blocked_fields"`
		} `json:"mcp"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &parsed); err != nil {
		test.Fatalf("parsing config show JSON: %v\n%s", err, result.Stdout)
	}
	got, ok := parsed.MCP.BlockedFields["tusk_task_modify"]
	if !ok {
		test.Fatalf("blocked_fields missing tusk_task_modify entry: %+v", parsed.MCP.BlockedFields)
	}
	if len(got) != 1 || got[0] != "priority" {
		test.Errorf("tusk_task_modify blocked fields = %v, want [priority]", got)
	}
}

func TestCLI_ConfigValidate(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	test.Run("valid_config", func(test *testing.T) {
		homeDir := test.TempDir()
		env := newEnv(test, binPath, "flag", "text")
		env.WithHome(homeDir)
		env.WithoutDBArg()

		// Init config (writes valid defaults).
		if initResult := env.Run("config", "init"); initResult.Err != nil {
			test.Fatalf("config init: %v\nstdout: %s\nstderr: %s", initResult.Err, initResult.Stdout, initResult.Stderr)
		}

		result := env.Run("config", "validate")
		if result.Err != nil {
			test.Fatalf("config validate: %v\nstdout: %s\nstderr: %s", result.Err, result.Stdout, result.Stderr)
		}
		if !strings.Contains(result.Stdout, "Config valid") {
			test.Errorf("expected 'Config valid', got: %s", result.Stdout)
		}
	})

	test.Run("legacy_sections_rejected", func(test *testing.T) {
		// Post-phase-2: [projects.*] and [workflows.*] sections in the TOML
		// config are hard errors. Using a stale config file must fail at
		// Load time with a clear migration message.
		homeDir := test.TempDir()
		tuskConfigDir := filepath.Join(homeDir, ".config", "tusk")
		if err := os.MkdirAll(tuskConfigDir, 0o755); err != nil {
			test.Fatal(err)
		}
		legacy := []byte("[projects.default]\nworkflow = \"kanban\"\n")
		if err := os.WriteFile(filepath.Join(tuskConfigDir, "config.toml"), legacy, 0o644); err != nil {
			test.Fatal(err)
		}

		env := newEnv(test, binPath, "flag", "text")
		env.WithHome(homeDir)
		env.WithoutDBArg()

		result := env.Run("config", "validate")
		if result.Err == nil {
			test.Fatal("expected error for legacy [projects.*] section")
		}
		if !strings.Contains(result.Stderr, "managed in the database") {
			test.Errorf("expected legacy-section error, got stdout=%q stderr=%q", result.Stdout, result.Stderr)
		}
	})
}

func TestCLI_ExplicitConfigFlag(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	// Build a custom config file in a directory that is NOT the HOME config dir.
	dbDir := test.TempDir()
	dbPath := filepath.Join(dbDir, "explicit.db")
	configDir := test.TempDir()
	configFile := filepath.Join(configDir, "tusk-custom.toml")
	configContent := []byte("[storage]\npath = \"" + dbPath + "\"\n")
	if err := os.WriteFile(configFile, configContent, 0o644); err != nil {
		test.Fatalf("writing config: %v", err)
	}

	// Use a fresh HOME so there is no fallback config to confuse the test.
	homeDir := test.TempDir()

	env := newEnv(test, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()

	result := env.Run("--config", configFile, "task", "create", "Explicit config task")
	if result.Err != nil {
		test.Fatalf("tusk --config ... add failed: %v\nstderr: %s", result.Err, result.Stderr)
	}

	// The custom DB should exist at the path the explicit config pointed to.
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		test.Fatalf("expected DB at %s from explicit config, but it does not exist", dbPath)
	}
}

func TestCLI_TuskConfigEnv(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	dbDir := test.TempDir()
	dbPath := filepath.Join(dbDir, "env.db")
	configDir := test.TempDir()
	configFile := filepath.Join(configDir, "tusk-env.toml")
	configContent := []byte("[storage]\npath = \"" + dbPath + "\"\n")
	if err := os.WriteFile(configFile, configContent, 0o644); err != nil {
		test.Fatalf("writing config: %v", err)
	}

	homeDir := test.TempDir()

	env := newEnv(test, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()
	env.WithEnv("TUSK_CONFIG", configFile)

	result := env.Run("task", "create", "Env config task")
	if result.Err != nil {
		test.Fatalf("tusk add with TUSK_CONFIG failed: %v\nstderr: %s", result.Err, result.Stderr)
	}

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		test.Fatalf("expected DB at %s from TUSK_CONFIG, but it does not exist", dbPath)
	}
}

func TestCLI_FlagBeatsEnv(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	// Two config files pointing at two different DBs. --config should win.
	dir := test.TempDir()
	flagDB := filepath.Join(dir, "flag.db")
	envDB := filepath.Join(dir, "env.db")

	flagFile := filepath.Join(dir, "flag.toml")
	envFile := filepath.Join(dir, "env.toml")
	if err := os.WriteFile(flagFile, []byte("[storage]\npath = \""+flagDB+"\"\n"), 0o644); err != nil {
		test.Fatalf("writing flag config: %v", err)
	}
	if err := os.WriteFile(envFile, []byte("[storage]\npath = \""+envDB+"\"\n"), 0o644); err != nil {
		test.Fatalf("writing env config: %v", err)
	}

	homeDir := test.TempDir()
	env := newEnv(test, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()
	env.WithEnv("TUSK_CONFIG", envFile)

	result := env.Run("--config", flagFile, "task", "create", "Precedence task")
	if result.Err != nil {
		test.Fatalf("tusk failed: %v\nstderr: %s", result.Err, result.Stderr)
	}

	if _, err := os.Stat(flagDB); os.IsNotExist(err) {
		test.Fatalf("expected DB at flag path %s, but it does not exist", flagDB)
	}
	if _, err := os.Stat(envDB); err == nil {
		test.Fatalf("unexpected DB at env path %s — flag should have won", envDB)
	}
}

func TestCLI_MissingExplicitConfigIsHardError(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	missing := filepath.Join(test.TempDir(), "does-not-exist.toml")
	homeDir := test.TempDir()

	env := newEnv(test, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()

	result := env.Run("--config", missing, "task", "list")
	if result.Err == nil {
		test.Fatal("expected tusk to exit non-zero when --config points at a missing file")
	}
	if !strings.Contains(result.Stderr, "config file not found") {
		test.Fatalf("expected 'config file not found' in stderr, got: %s", result.Stderr)
	}
}

func TestCLI_ConfigValidate_ExplicitFile(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	// Post-phase-2 the config file holds globals only — projects and
	// workflows live in the database. A minimal valid file just needs a
	// [storage] section (the remaining keys pick up embedded defaults).
	dir := test.TempDir()
	configFile := filepath.Join(dir, "custom.toml")
	minimal := []byte(`[storage]
backend = "sqlite"
path = "/tmp/validate-explicit.db"
`)
	if err := os.WriteFile(configFile, minimal, 0o644); err != nil {
		test.Fatalf("writing config: %v", err)
	}

	env := newEnv(test, binPath, "flag", "text")
	env.WithHome(test.TempDir())
	env.WithoutDBArg()

	result := env.Run("--config", configFile, "config", "validate")
	if result.Err != nil {
		test.Fatalf("config validate failed: %v\nstdout: %s\nstderr: %s", result.Err, result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "Config valid") {
		test.Fatalf("expected 'Config valid' in output, got: %s", result.Stdout)
	}
}

func TestCLI_ConfigPath_ExistingGlobal(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	homeDir := test.TempDir()
	env := newEnv(test, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()

	if initResult := env.Run("config", "init"); initResult.Err != nil {
		test.Fatalf("config init failed: %v\nstdout: %s\nstderr: %s", initResult.Err, initResult.Stdout, initResult.Stderr)
	}

	result := env.Run("config", "path")
	if result.Err != nil {
		test.Fatalf("config path failed: %v\nstdout: %s\nstderr: %s", result.Err, result.Stdout, result.Stderr)
	}
	got := strings.TrimSpace(result.Stdout)
	want := filepath.Join(homeDir, ".config", "tusk", "config.toml")
	if got != want {
		test.Fatalf("config path = %q, want %q", got, want)
	}
}

func TestCLI_ConfigPath_ExplicitFile(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	dir := test.TempDir()
	configFile := filepath.Join(dir, "custom.toml")
	if err := os.WriteFile(configFile, []byte("# custom\n"), 0o644); err != nil {
		test.Fatalf("writing config: %v", err)
	}
	homeDir := test.TempDir()

	env := newEnv(test, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()

	result := env.Run("--config", configFile, "config", "path")
	if result.Err != nil {
		test.Fatalf("config path failed: %v\nstdout: %s\nstderr: %s", result.Err, result.Stdout, result.Stderr)
	}
	got := strings.TrimSpace(result.Stdout)
	if got != configFile {
		test.Fatalf("config path = %q, want %q", got, configFile)
	}
}

func TestCLI_ConfigShowHeader_ExplicitFile(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	dir := test.TempDir()
	configFile := filepath.Join(dir, "custom.toml")
	if err := os.WriteFile(configFile, []byte("[storage]\npath = \"/tmp/header.db\"\n"), 0o644); err != nil {
		test.Fatalf("writing config: %v", err)
	}
	homeDir := test.TempDir()

	env := newEnv(test, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()

	result := env.Run("--config", configFile, "config", "show")
	if result.Err != nil {
		test.Fatalf("tusk config show failed: %v\nstdout: %s\nstderr: %s", result.Err, result.Stdout, result.Stderr)
	}
	firstLine := strings.SplitN(result.Stdout, "\n", 2)[0]
	wantPrefix := "# active: " + configFile
	if firstLine != wantPrefix {
		test.Fatalf("first line = %q, want %q", firstLine, wantPrefix)
	}
}

func TestCLI_ConfigShowHeader_GlobalFile(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	homeDir := test.TempDir()
	env := newEnv(test, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()

	result := env.Run("config", "show")
	if result.Err != nil {
		test.Fatalf("tusk config show failed: %v\nstdout: %s\nstderr: %s", result.Err, result.Stdout, result.Stderr)
	}

	wantPath := filepath.Join(homeDir, ".config", "tusk", "config.toml")
	firstLine := strings.SplitN(result.Stdout, "\n", 2)[0]
	wantPrefix := "# active: " + wantPath
	if firstLine != wantPrefix {
		test.Fatalf("first line = %q, want %q", firstLine, wantPrefix)
	}
}

func TestCLI_TuskEnvOverlaysExplicitConfig(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	// Config file says one DB path; TUSK_STORAGE_PATH env overrides it.
	dir := test.TempDir()
	fileDB := filepath.Join(dir, "from-file.db")
	envDB := filepath.Join(dir, "from-env.db")
	configFile := filepath.Join(dir, "custom.toml")
	if err := os.WriteFile(configFile, []byte("[storage]\npath = \""+fileDB+"\"\n"), 0o644); err != nil {
		test.Fatalf("writing config: %v", err)
	}

	homeDir := test.TempDir()
	env := newEnv(test, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()
	env.WithEnv("TUSK_STORAGE_PATH", envDB)

	result := env.Run("--config", configFile, "task", "create", "Overlay task")
	if result.Err != nil {
		test.Fatalf("tusk failed: %v\nstderr: %s", result.Err, result.Stderr)
	}

	if _, err := os.Stat(envDB); os.IsNotExist(err) {
		test.Fatalf("expected DB at env-override path %s", envDB)
	}
	if _, err := os.Stat(fileDB); err == nil {
		test.Fatalf("unexpected DB at file path %s — env should have overridden it", fileDB)
	}
}
