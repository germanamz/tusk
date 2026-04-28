package e2e

import (
	"encoding/json"
	"os"
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
	env := newEnv(t, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()
	r := env.Run("task", "create", "Config test task")
	if r.Err != nil {
		t.Fatalf("tusk add failed: %v\nstderr: %s", r.Err, r.Stderr)
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

	// Create config that disables the task_relations tool group.
	homeDir := t.TempDir()
	tuskConfigDir := filepath.Join(homeDir, ".config", "tusk")
	if err := os.MkdirAll(tuskConfigDir, 0o755); err != nil {
		t.Fatalf("creating config dir: %v", err)
	}
	configContent := []byte("[mcp]\ndisabled_tool_groups = [\"task_relations\"]\n")
	if err := os.WriteFile(filepath.Join(tuskConfigDir, "config.toml"), configContent, 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	// Start MCP server with this config.
	env := NewMCPEnv(t, binPath).WithHome(homeDir)

	// List tools.
	resp := env.Send("tools/list", nil)
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
		if tool.Name == "tusk_task_link" || tool.Name == "tusk_task_unlink" {
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

	env := newEnv(t, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()

	// Run config init: startup auto-creates the file via ensureConfigFile in config.Load,
	// so config init will always report "already exists" and succeed.
	r := env.Run("config", "init")
	if r.Err != nil {
		t.Fatalf("config init failed: %v\nstdout: %s\nstderr: %s", r.Err, r.Stdout, r.Stderr)
	}

	// Verify file exists (created by startup or by config init itself).
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("config file was not created")
	}

	// Second run: should also succeed and report already exists.
	r = env.Run("config", "init")
	if r.Err != nil {
		t.Fatalf("config init (second run) failed: %v\nstdout: %s\nstderr: %s", r.Err, r.Stdout, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "already exists") {
		t.Errorf("expected 'already exists' in output, got: %s", r.Stdout)
	}
}

func TestCLI_ConfigPath(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	homeDir := t.TempDir()
	env := newEnv(t, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()

	r := env.Run("config", "path")
	if r.Err != nil {
		t.Fatalf("config path failed: %v\nstdout: %s\nstderr: %s", r.Err, r.Stdout, r.Stderr)
	}
	expected := filepath.Join(homeDir, ".config", "tusk", "config.toml")
	if strings.TrimSpace(r.Stdout) != expected {
		t.Errorf("got %q, want %q", strings.TrimSpace(r.Stdout), expected)
	}
}

func TestCLI_ConfigShow(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	homeDir := t.TempDir()
	env := newEnv(t, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()

	// Config init first to create the file.
	if r := env.Run("config", "init"); r.Err != nil {
		t.Fatalf("config init failed: %v\nstdout: %s\nstderr: %s", r.Err, r.Stdout, r.Stderr)
	}

	r := env.Run("config", "show")
	if r.Err != nil {
		t.Fatalf("config show failed: %v\nstdout: %s\nstderr: %s", r.Err, r.Stdout, r.Stderr)
	}

	// Should contain key TOML sections.
	if !strings.Contains(r.Stdout, "[storage]") {
		t.Error("config show output missing [storage] section")
	}
	if !strings.Contains(r.Stdout, "[urgency]") {
		t.Error("config show output missing [urgency] section")
	}
	if !strings.Contains(r.Stdout, "[tui]") {
		t.Error("config show output missing [tui] section")
	}
}

func TestCLI_ConfigGet(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	homeDir := t.TempDir()
	env := newEnv(t, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()

	// Init config first.
	if r := env.Run("config", "init"); r.Err != nil {
		t.Fatalf("config init: %v\nstdout: %s\nstderr: %s", r.Err, r.Stdout, r.Stderr)
	}

	t.Run("scalar_bool", func(t *testing.T) {
		r := env.Run("config", "get", "tui.color")
		if r.Err != nil {
			t.Fatalf("config get: %v\nstdout: %s\nstderr: %s", r.Err, r.Stdout, r.Stderr)
		}
		if strings.TrimSpace(r.Stdout) != "true" {
			t.Errorf("got %q, want %q", strings.TrimSpace(r.Stdout), "true")
		}
	})

	t.Run("scalar_float", func(t *testing.T) {
		r := env.Run("config", "get", "urgency.due_weight")
		if r.Err != nil {
			t.Fatalf("config get: %v\nstdout: %s\nstderr: %s", r.Err, r.Stdout, r.Stderr)
		}
		if strings.TrimSpace(r.Stdout) != "12" {
			t.Errorf("got %q, want %q", strings.TrimSpace(r.Stdout), "12")
		}
	})

	t.Run("complex_value", func(t *testing.T) {
		// `config get workflows.kanban.statuses` hydrates the workflow row
		// from the database and returns a JSON object keyed by status name.
		r := env.Run("config", "get", "workflows.kanban.statuses")
		if r.Err != nil {
			t.Fatalf("config get: %v\nstdout: %s\nstderr: %s", r.Err, r.Stdout, r.Stderr)
		}
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(r.Stdout), &obj); err != nil {
			t.Fatalf("expected JSON object, got: %s", r.Stdout)
		}
		if len(obj) != 4 {
			t.Errorf("expected 4 statuses, got %d", len(obj))
		}
	})

	t.Run("unknown_key", func(t *testing.T) {
		r := env.Run("config", "get", "nonexistent.key")
		if r.Err == nil {
			t.Fatal("expected error for unknown key")
		}
		if !strings.Contains(r.Stderr, "unknown config key") {
			t.Errorf("expected 'unknown config key' error, got stdout=%q stderr=%q", r.Stdout, r.Stderr)
		}
	})
}

func TestCLI_ConfigSet(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	homeDir := t.TempDir()
	env := newEnv(t, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()

	// Init config first.
	if r := env.Run("config", "init"); r.Err != nil {
		t.Fatalf("config init: %v\nstdout: %s\nstderr: %s", r.Err, r.Stdout, r.Stderr)
	}

	t.Run("set_scalar", func(t *testing.T) {
		if r := env.Run("config", "set", "tui.color", "false"); r.Err != nil {
			t.Fatalf("config set: %v\nstdout: %s\nstderr: %s", r.Err, r.Stdout, r.Stderr)
		}

		// Verify the change persisted.
		r := env.Run("config", "get", "tui.color")
		if r.Err != nil {
			t.Fatalf("config get: %v\nstdout: %s\nstderr: %s", r.Err, r.Stdout, r.Stderr)
		}
		if strings.TrimSpace(r.Stdout) != "false" {
			t.Errorf("got %q, want %q", strings.TrimSpace(r.Stdout), "false")
		}
	})

	t.Run("set_list", func(t *testing.T) {
		if r := env.Run("config", "set", "mcp.disabled_tools", "tusk_task_delete,tusk_task_tree"); r.Err != nil {
			t.Fatalf("config set: %v\nstdout: %s\nstderr: %s", r.Err, r.Stdout, r.Stderr)
		}

		// Verify.
		r := env.Run("config", "get", "mcp.disabled_tools")
		if r.Err != nil {
			t.Fatalf("config get: %v\nstdout: %s\nstderr: %s", r.Err, r.Stdout, r.Stderr)
		}
		var arr []string
		if err := json.Unmarshal([]byte(r.Stdout), &arr); err != nil {
			t.Fatalf("expected JSON array, got: %s", r.Stdout)
		}
		if len(arr) != 2 || arr[0] != "tusk_task_delete" || arr[1] != "tusk_task_tree" {
			t.Errorf("unexpected disabled_tools: %v", arr)
		}
	})

	t.Run("reject_unknown_key", func(t *testing.T) {
		r := env.Run("config", "set", "nonexistent.key", "value")
		if r.Err == nil {
			t.Fatal("expected error for unknown key")
		}
		if !strings.Contains(r.Stderr, "unknown config key") {
			t.Errorf("expected 'unknown config key' error, got stdout=%q stderr=%q", r.Stdout, r.Stderr)
		}
	})

	t.Run("reject_projects_write", func(t *testing.T) {
		r := env.Run("config", "set", "projects.default.workflow", "kanban")
		if r.Err == nil {
			t.Fatal("expected error for projects.* write")
		}
		if !strings.Contains(r.Stderr, "projects.* is managed by the database") {
			t.Errorf("expected projects.* rejection, got stdout=%q stderr=%q", r.Stdout, r.Stderr)
		}
	})

	t.Run("reject_workflows_write", func(t *testing.T) {
		r := env.Run("config", "set", "workflows.kanban.statuses.pending.roles", "initial")
		if r.Err == nil {
			t.Fatal("expected error for workflows.* write")
		}
		if !strings.Contains(r.Stderr, "workflows.* is managed by the database") {
			t.Errorf("expected workflows.* rejection, got stdout=%q stderr=%q", r.Stdout, r.Stderr)
		}
	})

	t.Run("works_with_auto_created_config", func(t *testing.T) {
		// Even with a fresh HOME (no pre-existing config), config set works
		// because Load() in main.go auto-creates the file via ensureConfigFile().
		freshHome := t.TempDir()
		freshEnv := newEnv(t, binPath, "flag", "text")
		freshEnv.WithHome(freshHome)
		freshEnv.WithoutDBArg()

		if r := freshEnv.Run("config", "set", "tui.color", "false"); r.Err != nil {
			t.Fatalf("config set with fresh home: %v\nstdout: %s\nstderr: %s", r.Err, r.Stdout, r.Stderr)
		}

		// Verify it persisted.
		r := freshEnv.Run("config", "get", "tui.color")
		if r.Err != nil {
			t.Fatalf("config get: %v\nstdout: %s\nstderr: %s", r.Err, r.Stdout, r.Stderr)
		}
		if strings.TrimSpace(r.Stdout) != "false" {
			t.Errorf("got %q, want %q", strings.TrimSpace(r.Stdout), "false")
		}
	})
}

func TestCLI_ConfigSet_BlockedFields(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	homeDir := t.TempDir()
	env := newEnv(t, binPath, "flag", "json")
	env.WithHome(homeDir)
	env.WithoutDBArg()

	if r := env.Run("config", "init"); r.Err != nil {
		t.Fatalf("config init: %v\nstdout: %s\nstderr: %s", r.Err, r.Stdout, r.Stderr)
	}

	if r := env.Run("config", "set", "mcp.blocked_fields.tusk_task_modify", "priority"); r.Err != nil {
		t.Fatalf("config set mcp.blocked_fields.tusk_task_modify: %v\nstdout: %s\nstderr: %s", r.Err, r.Stdout, r.Stderr)
	}

	r := env.Run("config", "show")
	if r.Err != nil {
		t.Fatalf("config show --format json: %v\nstdout: %s\nstderr: %s", r.Err, r.Stdout, r.Stderr)
	}

	var parsed struct {
		MCP struct {
			BlockedFields map[string][]string `json:"blocked_fields"`
		} `json:"mcp"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &parsed); err != nil {
		t.Fatalf("parsing config show JSON: %v\n%s", err, r.Stdout)
	}
	got, ok := parsed.MCP.BlockedFields["tusk_task_modify"]
	if !ok {
		t.Fatalf("blocked_fields missing tusk_task_modify entry: %+v", parsed.MCP.BlockedFields)
	}
	if len(got) != 1 || got[0] != "priority" {
		t.Errorf("tusk_task_modify blocked fields = %v, want [priority]", got)
	}
}

func TestCLI_ConfigValidate(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	t.Run("valid_config", func(t *testing.T) {
		homeDir := t.TempDir()
		env := newEnv(t, binPath, "flag", "text")
		env.WithHome(homeDir)
		env.WithoutDBArg()

		// Init config (writes valid defaults).
		if r := env.Run("config", "init"); r.Err != nil {
			t.Fatalf("config init: %v\nstdout: %s\nstderr: %s", r.Err, r.Stdout, r.Stderr)
		}

		r := env.Run("config", "validate")
		if r.Err != nil {
			t.Fatalf("config validate: %v\nstdout: %s\nstderr: %s", r.Err, r.Stdout, r.Stderr)
		}
		if !strings.Contains(r.Stdout, "Config valid") {
			t.Errorf("expected 'Config valid', got: %s", r.Stdout)
		}
	})

	t.Run("legacy_sections_rejected", func(t *testing.T) {
		// Post-phase-2: [projects.*] and [workflows.*] sections in the TOML
		// config are hard errors. Using a stale config file must fail at
		// Load time with a clear migration message.
		homeDir := t.TempDir()
		tuskConfigDir := filepath.Join(homeDir, ".config", "tusk")
		if err := os.MkdirAll(tuskConfigDir, 0o755); err != nil {
			t.Fatal(err)
		}
		legacy := []byte("[projects.default]\nworkflow = \"kanban\"\n")
		if err := os.WriteFile(filepath.Join(tuskConfigDir, "config.toml"), legacy, 0o644); err != nil {
			t.Fatal(err)
		}

		env := newEnv(t, binPath, "flag", "text")
		env.WithHome(homeDir)
		env.WithoutDBArg()

		r := env.Run("config", "validate")
		if r.Err == nil {
			t.Fatal("expected error for legacy [projects.*] section")
		}
		if !strings.Contains(r.Stderr, "managed in the database") {
			t.Errorf("expected legacy-section error, got stdout=%q stderr=%q", r.Stdout, r.Stderr)
		}
	})
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

	env := newEnv(t, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()

	r := env.Run("--config", configFile, "task", "create", "Explicit config task")
	if r.Err != nil {
		t.Fatalf("tusk --config ... add failed: %v\nstderr: %s", r.Err, r.Stderr)
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

	env := newEnv(t, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()
	env.WithEnv("TUSK_CONFIG", configFile)

	r := env.Run("task", "create", "Env config task")
	if r.Err != nil {
		t.Fatalf("tusk add with TUSK_CONFIG failed: %v\nstderr: %s", r.Err, r.Stderr)
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
	env := newEnv(t, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()
	env.WithEnv("TUSK_CONFIG", envFile)

	r := env.Run("--config", flagFile, "task", "create", "Precedence task")
	if r.Err != nil {
		t.Fatalf("tusk failed: %v\nstderr: %s", r.Err, r.Stderr)
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

	env := newEnv(t, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()

	r := env.Run("--config", missing, "task", "list")
	if r.Err == nil {
		t.Fatal("expected tusk to exit non-zero when --config points at a missing file")
	}
	if !strings.Contains(r.Stderr, "config file not found") {
		t.Fatalf("expected 'config file not found' in stderr, got: %s", r.Stderr)
	}
}

func TestCLI_ConfigValidate_ExplicitFile(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	// Post-phase-2 the config file holds globals only — projects and
	// workflows live in the database. A minimal valid file just needs a
	// [storage] section (the remaining keys pick up embedded defaults).
	dir := t.TempDir()
	configFile := filepath.Join(dir, "custom.toml")
	minimal := []byte(`[storage]
backend = "sqlite"
path = "/tmp/validate-explicit.db"
`)
	if err := os.WriteFile(configFile, minimal, 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	env := newEnv(t, binPath, "flag", "text")
	env.WithHome(t.TempDir())
	env.WithoutDBArg()

	r := env.Run("--config", configFile, "config", "validate")
	if r.Err != nil {
		t.Fatalf("config validate failed: %v\nstdout: %s\nstderr: %s", r.Err, r.Stdout, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "Config valid") {
		t.Fatalf("expected 'Config valid' in output, got: %s", r.Stdout)
	}
}

func TestCLI_ConfigPath_ExistingGlobal(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	homeDir := t.TempDir()
	env := newEnv(t, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()

	if r := env.Run("config", "init"); r.Err != nil {
		t.Fatalf("config init failed: %v\nstdout: %s\nstderr: %s", r.Err, r.Stdout, r.Stderr)
	}

	r := env.Run("config", "path")
	if r.Err != nil {
		t.Fatalf("config path failed: %v\nstdout: %s\nstderr: %s", r.Err, r.Stdout, r.Stderr)
	}
	got := strings.TrimSpace(r.Stdout)
	want := filepath.Join(homeDir, ".config", "tusk", "config.toml")
	if got != want {
		t.Fatalf("config path = %q, want %q", got, want)
	}
}

func TestCLI_ConfigPath_ExplicitFile(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	dir := t.TempDir()
	configFile := filepath.Join(dir, "custom.toml")
	if err := os.WriteFile(configFile, []byte("# custom\n"), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	homeDir := t.TempDir()

	env := newEnv(t, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()

	r := env.Run("--config", configFile, "config", "path")
	if r.Err != nil {
		t.Fatalf("config path failed: %v\nstdout: %s\nstderr: %s", r.Err, r.Stdout, r.Stderr)
	}
	got := strings.TrimSpace(r.Stdout)
	if got != configFile {
		t.Fatalf("config path = %q, want %q", got, configFile)
	}
}

func TestCLI_ConfigShowHeader_ExplicitFile(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	dir := t.TempDir()
	configFile := filepath.Join(dir, "custom.toml")
	if err := os.WriteFile(configFile, []byte("[storage]\npath = \"/tmp/header.db\"\n"), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	homeDir := t.TempDir()

	env := newEnv(t, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()

	r := env.Run("--config", configFile, "config", "show")
	if r.Err != nil {
		t.Fatalf("tusk config show failed: %v\nstdout: %s\nstderr: %s", r.Err, r.Stdout, r.Stderr)
	}
	firstLine := strings.SplitN(r.Stdout, "\n", 2)[0]
	wantPrefix := "# active: " + configFile
	if firstLine != wantPrefix {
		t.Fatalf("first line = %q, want %q", firstLine, wantPrefix)
	}
}

func TestCLI_ConfigShowHeader_GlobalFile(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	homeDir := t.TempDir()
	env := newEnv(t, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()

	r := env.Run("config", "show")
	if r.Err != nil {
		t.Fatalf("tusk config show failed: %v\nstdout: %s\nstderr: %s", r.Err, r.Stdout, r.Stderr)
	}

	wantPath := filepath.Join(homeDir, ".config", "tusk", "config.toml")
	firstLine := strings.SplitN(r.Stdout, "\n", 2)[0]
	wantPrefix := "# active: " + wantPath
	if firstLine != wantPrefix {
		t.Fatalf("first line = %q, want %q", firstLine, wantPrefix)
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
	env := newEnv(t, binPath, "flag", "text")
	env.WithHome(homeDir)
	env.WithoutDBArg()
	env.WithEnv("TUSK_STORAGE_PATH", envDB)

	r := env.Run("--config", configFile, "task", "create", "Overlay task")
	if r.Err != nil {
		t.Fatalf("tusk failed: %v\nstderr: %s", r.Err, r.Stderr)
	}

	if _, err := os.Stat(envDB); os.IsNotExist(err) {
		t.Fatalf("expected DB at env-override path %s", envDB)
	}
	if _, err := os.Stat(fileDB); err == nil {
		t.Fatalf("unexpected DB at file path %s — env should have overridden it", fileDB)
	}
}
