package e2e

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// walkUpScenario is a walk-up scenario that needs per-run setup. Each scenario
// runs across a filtered combination matrix. We don't reuse runScenarios here
// because walk-up requires dynamic temp dirs per-run and InDir mutation before
// any step executes.
type walkUpScenario struct {
	name    string
	skipDB  []string // db modes to skip ("flag"/"env")
	skipFmt []string // formats to skip ("text"/"json")
	run     func(test *testing.T, env *Env)
}

func runWalkUpScenarios(test *testing.T, scenarios []walkUpScenario) {
	test.Helper()
	if binPath == "" {
		test.Skip("binary not built")
	}
	combos := combinations(
		[]string{"flag", "env"},
		[]string{"text", "json"},
	)
	for _, scenario := range scenarios {
		for _, combo := range combos {
			dbMode, format := combo[0], combo[1]
			if slices.Contains(scenario.skipDB, dbMode) || slices.Contains(scenario.skipFmt, format) {
				continue
			}
			name := scenario.name + "/" + dbMode + "/" + format
			test.Run(name, func(test *testing.T) {
				test.Parallel()
				env := newEnv(test, binPath, dbMode, format)
				env.configDir = test.TempDir() // empty — forces a controlled global dir
				scenario.run(test, env)
			})
		}
	}
}

// resolvedTempDir returns test.TempDir() after symlink resolution so that
// comparisons against the CLI's os.Getwd() (which reports the real path on
// macOS where /var -> /private/var) succeed.
func resolvedTempDir(test *testing.T) string {
	test.Helper()
	dir := test.TempDir()
	real, err := filepath.EvalSymlinks(dir)

	if err != nil {
		test.Fatalf("evalsymlinks: %v", err)
	}

	return real
}

func TestCLI_ConfigWalkUp(test *testing.T) {
	scenarios := []walkUpScenario{
		{
			name:    "walkup_cwd_hit",
			skipFmt: []string{"json"}, // config show header is text-only
			run: func(test *testing.T, env *Env) {
				root := resolvedTempDir(test)
				local := filepath.Join(root, "tusk.toml")
				if err := os.WriteFile(local, []byte("[tui]\ncolor = false\n"), 0o644); err != nil {
					test.Fatalf("writing local: %v", err)
				}
				env.InDir(root)

				show := env.Run("config", "show")
				if show.Err != nil {
					test.Fatalf("config show failed: %v\n%s", show.Err, show.Stderr)
				}
				wantHeader := "# active: " + local
				firstLine := strings.SplitN(show.Stdout, "\n", 2)[0]
				if firstLine != wantHeader {
					test.Fatalf("first line = %q, want %q", firstLine, wantHeader)
				}
				assertContains(test, show.Stdout, "color = false")

				path := env.Run("config", "path")
				if path.Err != nil {
					test.Fatalf("config path failed: %v\n%s", path.Err, path.Stderr)
				}
				got := strings.TrimSpace(path.Stdout)
				if got != local {
					test.Fatalf("config path = %q, want %q", got, local)
				}
			},
		},
		{
			name:    "walkup_ancestor_hit",
			skipFmt: []string{"json"},
			run: func(test *testing.T, env *Env) {
				root := resolvedTempDir(test)
				local := filepath.Join(root, "tusk.toml")
				if err := os.WriteFile(local, []byte("# local\n"), 0o644); err != nil {
					test.Fatalf("writing local: %v", err)
				}
				child := filepath.Join(root, "a", "b", "c")
				if err := os.MkdirAll(child, 0o755); err != nil {
					test.Fatalf("mkdir: %v", err)
				}
				env.InDir(child)

				result := env.Run("config", "path")
				if result.Err != nil {
					test.Fatalf("config path failed: %v\n%s", result.Err, result.Stderr)
				}
				got := strings.TrimSpace(result.Stdout)
				if got != local {
					test.Fatalf("config path = %q, want %q", got, local)
				}
			},
		},
		{
			name: "walkup_no_global_autocreate_when_local_present",
			run: func(test *testing.T, env *Env) {
				root := resolvedTempDir(test)
				if err := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte("# local\n"), 0o644); err != nil {
					test.Fatalf("writing local: %v", err)
				}
				env.InDir(root)

				// configDir was set to an empty temp dir by runWalkUpScenarios.
				listRes := env.Run("task", "list")
				if listRes.Err != nil {
					test.Fatalf("list failed: %v\n%s", listRes.Err, listRes.Stderr)
				}

				globalCfg := filepath.Join(env.configDir, "config.toml")
				if _, err := os.Stat(globalCfg); !os.IsNotExist(err) {
					test.Fatalf("global config.toml should not exist after walk-up hit; stat err = %v", err)
				}
			},
		},
		{
			name:    "walkup_explicit_config_flag_overrides",
			skipFmt: []string{"json"},
			run: func(test *testing.T, env *Env) {
				root := resolvedTempDir(test)
				if err := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte("[tui]\ncolor = false\n"), 0o644); err != nil {
					test.Fatalf("writing local: %v", err)
				}
				other := filepath.Join(test.TempDir(), "custom.toml")
				if err := os.WriteFile(other, []byte("[tui]\ncolor = true\n"), 0o644); err != nil {
					test.Fatalf("writing other: %v", err)
				}
				env.InDir(root)

				result := env.Run("--config", other, "config", "show")
				if result.Err != nil {
					test.Fatalf("config show failed: %v\n%s", result.Err, result.Stderr)
				}
				firstLine := strings.SplitN(result.Stdout, "\n", 2)[0]
				want := "# active: " + other
				if firstLine != want {
					test.Fatalf("first line = %q, want %q", firstLine, want)
				}
			},
		},
		{
			name: "config_set_local_writes_to_walkup_file",
			run: func(test *testing.T, env *Env) {
				root := resolvedTempDir(test)
				local := filepath.Join(root, "tusk.toml")
				if err := os.WriteFile(local, []byte("[tui]\ncolor = true\n"), 0o644); err != nil {
					test.Fatalf("writing local: %v", err)
				}
				env.InDir(root)

				setResult := env.Run("config", "set", "tui.color", "false")
				if setResult.Err != nil {
					test.Fatalf("config set failed: %v\n%s", setResult.Err, setResult.Stderr)
				}

				data, readErr := os.ReadFile(local)

				if readErr != nil {
					test.Fatalf("reading local: %v", readErr)
				}

				assertContains(test, string(data), "color = false")

				globalCfg := filepath.Join(env.configDir, "config.toml")
				if _, statErr := os.Stat(globalCfg); !os.IsNotExist(statErr) {
					test.Fatalf("global config.toml should not exist after local set; stat err = %v", statErr)
				}
			},
		},
		{
			name: "config_set_global_flag_writes_to_global_even_with_local_present",
			run: func(test *testing.T, env *Env) {
				root := resolvedTempDir(test)
				local := filepath.Join(root, "tusk.toml")
				localContent := []byte("[tui]\ncolor = true\n")
				if err := os.WriteFile(local, localContent, 0o644); err != nil {
					test.Fatalf("writing local: %v", err)
				}
				env.InDir(root)

				setResult := env.Run("config", "set", "--global", "tui.color", "false")
				if setResult.Err != nil {
					test.Fatalf("config set --global failed: %v\n%s", setResult.Err, setResult.Stderr)
				}

				globalCfg := filepath.Join(env.configDir, "config.toml")
				globalData, globalReadErr := os.ReadFile(globalCfg)

				if globalReadErr != nil {
					test.Fatalf("reading global: %v", globalReadErr)
				}

				assertContains(test, string(globalData), "color = false")

				localData, localReadErr := os.ReadFile(local)

				if localReadErr != nil {
					test.Fatalf("reading local: %v", localReadErr)
				}

				if string(localData) != string(localContent) {
					test.Fatalf("local tusk.toml unexpectedly modified:\n%s", localData)
				}
			},
		},
		{
			name: "config_set_no_local_falls_back_to_global",
			run: func(test *testing.T, env *Env) {
				root := resolvedTempDir(test)
				env.InDir(root)

				setResult := env.Run("config", "set", "tui.color", "false")
				if setResult.Err != nil {
					test.Fatalf("config set failed: %v\n%s", setResult.Err, setResult.Stderr)
				}

				if _, statErr := os.Stat(filepath.Join(root, "tusk.toml")); !os.IsNotExist(statErr) {
					test.Fatalf("local tusk.toml should not have been created; stat err = %v", statErr)
				}

				globalCfg := filepath.Join(env.configDir, "config.toml")
				data, readErr := os.ReadFile(globalCfg)

				if readErr != nil {
					test.Fatalf("reading global: %v", readErr)
				}

				assertContains(test, string(data), "color = false")
			},
		},
		{
			name:    "config_init_local_creates_file",
			skipFmt: []string{"json"},
			run: func(test *testing.T, env *Env) {
				root := resolvedTempDir(test)
				env.InDir(root)

				initResult := env.Run("config", "init", "--local")
				if initResult.Err != nil {
					test.Fatalf("config init --local failed: %v\n%s", initResult.Err, initResult.Stderr)
				}

				local := filepath.Join(root, "tusk.toml")
				if _, statErr := os.Stat(local); statErr != nil {
					test.Fatalf("expected local tusk.toml: %v", statErr)
				}

				validate := env.Run("config", "validate")
				if validate.Err != nil {
					test.Fatalf("config validate failed: %v\n%s", validate.Err, validate.Stderr)
				}

				show := env.Run("config", "show")
				if show.Err != nil {
					test.Fatalf("config show failed: %v\n%s", show.Err, show.Stderr)
				}
				wantHeader := "# active: " + local
				firstLine := strings.SplitN(show.Stdout, "\n", 2)[0]
				if firstLine != wantHeader {
					test.Fatalf("first line = %q, want %q", firstLine, wantHeader)
				}
			},
		},
		{
			name: "config_init_local_refuses_overwrite",
			run: func(test *testing.T, env *Env) {
				root := resolvedTempDir(test)
				local := filepath.Join(root, "tusk.toml")
				original := []byte("[tui]\ncolor = false\n")
				if err := os.WriteFile(local, original, 0o644); err != nil {
					test.Fatalf("writing local: %v", err)
				}
				env.InDir(root)

				result := env.Run("config", "init", "--local")
				if result.Err == nil {
					test.Fatalf("expected error, got none. stdout:\n%s", result.Stdout)
				}
				assertStderrContains(test, result, "file exists")

				got, readErr := os.ReadFile(local)

				if readErr != nil {
					test.Fatalf("reading local: %v", readErr)
				}

				if string(got) != string(original) {
					test.Fatalf("local tusk.toml unexpectedly overwritten:\n%s", got)
				}
			},
		},
		{
			name:    "walkup_tusk_config_env_overrides",
			skipFmt: []string{"json"},
			run: func(test *testing.T, env *Env) {
				root := resolvedTempDir(test)
				if err := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte("[tui]\ncolor = false\n"), 0o644); err != nil {
					test.Fatalf("writing local: %v", err)
				}
				other := filepath.Join(test.TempDir(), "custom.toml")
				if err := os.WriteFile(other, []byte("[tui]\ncolor = true\n"), 0o644); err != nil {
					test.Fatalf("writing other: %v", err)
				}
				env.InDir(root)
				env.WithEnv("TUSK_CONFIG", other)

				result := env.Run("config", "show")
				if result.Err != nil {
					test.Fatalf("config show failed: %v\n%s", result.Err, result.Stderr)
				}
				firstLine := strings.SplitN(result.Stdout, "\n", 2)[0]
				want := "# active: " + other
				if firstLine != want {
					test.Fatalf("first line = %q, want %q", firstLine, want)
				}
			},
		},
	}
	runWalkUpScenarios(test, scenarios)
}

// TestCLI_ConfigWalkUp_StoragePathRelative runs outside the matrix because
// --db would bypass storage.path, so the "flag" db mode cannot exercise the
// walk-up + relative storage interaction.
func TestCLI_ConfigWalkUp_StoragePathRelative(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	root := resolvedTempDir(test)
	if err := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte("[storage]\npath = \"./tusk.db\"\n"), 0o644); err != nil {
		test.Fatalf("writing local: %v", err)
	}
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		test.Fatalf("mkdir: %v", err)
	}

	env := newEnv(test, binPath, "env", "text")
	env.dbPath = "" // do not pre-create; we want storage.path to decide
	env.configDir = test.TempDir()
	env.WithoutDBArg()

	env.InDir(sub)
	addRes := env.Run("task", "create", "walkup-foo")
	if addRes.Err != nil {
		test.Fatalf("add failed: %v\nstderr: %s", addRes.Err, addRes.Stderr)
	}

	env.InDir(root)
	listRes := env.Run("task", "list")
	if listRes.Err != nil {
		test.Fatalf("list failed: %v\nstderr: %s", listRes.Err, listRes.Stderr)
	}
	assertContains(test, listRes.Stdout, "walkup-foo")

	// Sanity: the DB file lives next to tusk.toml.
	if _, err := os.Stat(filepath.Join(root, "tusk.db")); err != nil {
		test.Fatalf("expected tusk.db next to tusk.toml: %v", err)
	}
}
