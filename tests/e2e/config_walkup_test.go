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
	run     func(t *testing.T, env *Env)
}

func runWalkUpScenarios(t *testing.T, scenarios []walkUpScenario) {
	t.Helper()
	if binPath == "" {
		t.Skip("binary not built")
	}
	combos := combinations(
		[]string{"flag", "env"},
		[]string{"text", "json"},
	)
	for _, sc := range scenarios {
		for _, combo := range combos {
			dbMode, format := combo[0], combo[1]
			if slices.Contains(sc.skipDB, dbMode) || slices.Contains(sc.skipFmt, format) {
				continue
			}
			name := sc.name + "/" + dbMode + "/" + format
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				env := newEnv(t, binPath, dbMode, format)
				env.configDir = t.TempDir() // empty — forces a controlled global dir
				sc.run(t, env)
			})
		}
	}
}

// resolvedTempDir returns t.TempDir() after symlink resolution so that
// comparisons against the CLI's os.Getwd() (which reports the real path on
// macOS where /var -> /private/var) succeed.
func resolvedTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("evalsymlinks: %v", err)
	}
	return real
}

func TestCLI_ConfigWalkUp(t *testing.T) {
	scenarios := []walkUpScenario{
		{
			name:    "walkup_cwd_hit",
			skipFmt: []string{"json"}, // config show header is text-only
			run: func(t *testing.T, env *Env) {
				root := resolvedTempDir(t)
				local := filepath.Join(root, "tusk.toml")
				if err := os.WriteFile(local, []byte("[tui]\ncolor = false\n"), 0o644); err != nil {
					t.Fatalf("writing local: %v", err)
				}
				env.InDir(root)

				show := env.Run("config", "show")
				if show.Err != nil {
					t.Fatalf("config show failed: %v\n%s", show.Err, show.Stderr)
				}
				wantHeader := "# active: " + local
				firstLine := strings.SplitN(show.Stdout, "\n", 2)[0]
				if firstLine != wantHeader {
					t.Fatalf("first line = %q, want %q", firstLine, wantHeader)
				}
				assertContains(t, show.Stdout, "color = false")

				path := env.Run("config", "path")
				if path.Err != nil {
					t.Fatalf("config path failed: %v\n%s", path.Err, path.Stderr)
				}
				got := strings.TrimSpace(path.Stdout)
				if got != local {
					t.Fatalf("config path = %q, want %q", got, local)
				}
			},
		},
		{
			name:    "walkup_ancestor_hit",
			skipFmt: []string{"json"},
			run: func(t *testing.T, env *Env) {
				root := resolvedTempDir(t)
				local := filepath.Join(root, "tusk.toml")
				if err := os.WriteFile(local, []byte("# local\n"), 0o644); err != nil {
					t.Fatalf("writing local: %v", err)
				}
				child := filepath.Join(root, "a", "b", "c")
				if err := os.MkdirAll(child, 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				env.InDir(child)

				r := env.Run("config", "path")
				if r.Err != nil {
					t.Fatalf("config path failed: %v\n%s", r.Err, r.Stderr)
				}
				got := strings.TrimSpace(r.Stdout)
				if got != local {
					t.Fatalf("config path = %q, want %q", got, local)
				}
			},
		},
		{
			name: "walkup_no_global_autocreate_when_local_present",
			run: func(t *testing.T, env *Env) {
				root := resolvedTempDir(t)
				if err := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte("# local\n"), 0o644); err != nil {
					t.Fatalf("writing local: %v", err)
				}
				env.InDir(root)

				// configDir was set to an empty temp dir by runWalkUpScenarios.
				listRes := env.Run("list")
				if listRes.Err != nil {
					t.Fatalf("list failed: %v\n%s", listRes.Err, listRes.Stderr)
				}

				globalCfg := filepath.Join(env.configDir, "config.toml")
				if _, err := os.Stat(globalCfg); !os.IsNotExist(err) {
					t.Fatalf("global config.toml should not exist after walk-up hit; stat err = %v", err)
				}
			},
		},
		{
			name:    "walkup_explicit_config_flag_overrides",
			skipFmt: []string{"json"},
			run: func(t *testing.T, env *Env) {
				root := resolvedTempDir(t)
				if err := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte("[tui]\ncolor = false\n"), 0o644); err != nil {
					t.Fatalf("writing local: %v", err)
				}
				other := filepath.Join(t.TempDir(), "custom.toml")
				if err := os.WriteFile(other, []byte("[tui]\ncolor = true\n"), 0o644); err != nil {
					t.Fatalf("writing other: %v", err)
				}
				env.InDir(root)

				r := env.Run("--config", other, "config", "show")
				if r.Err != nil {
					t.Fatalf("config show failed: %v\n%s", r.Err, r.Stderr)
				}
				firstLine := strings.SplitN(r.Stdout, "\n", 2)[0]
				want := "# active: " + other
				if firstLine != want {
					t.Fatalf("first line = %q, want %q", firstLine, want)
				}
			},
		},
		{
			name: "config_set_local_writes_to_walkup_file",
			run: func(t *testing.T, env *Env) {
				root := resolvedTempDir(t)
				local := filepath.Join(root, "tusk.toml")
				if err := os.WriteFile(local, []byte("[tui]\ncolor = true\n"), 0o644); err != nil {
					t.Fatalf("writing local: %v", err)
				}
				env.InDir(root)

				r := env.Run("config", "set", "tui.color", "false")
				if r.Err != nil {
					t.Fatalf("config set failed: %v\n%s", r.Err, r.Stderr)
				}

				data, err := os.ReadFile(local)
				if err != nil {
					t.Fatalf("reading local: %v", err)
				}
				assertContains(t, string(data), "color = false")

				globalCfg := filepath.Join(env.configDir, "config.toml")
				if _, err := os.Stat(globalCfg); !os.IsNotExist(err) {
					t.Fatalf("global config.toml should not exist after local set; stat err = %v", err)
				}
			},
		},
		{
			name: "config_set_global_flag_writes_to_global_even_with_local_present",
			run: func(t *testing.T, env *Env) {
				root := resolvedTempDir(t)
				local := filepath.Join(root, "tusk.toml")
				localContent := []byte("[tui]\ncolor = true\n")
				if err := os.WriteFile(local, localContent, 0o644); err != nil {
					t.Fatalf("writing local: %v", err)
				}
				env.InDir(root)

				r := env.Run("config", "set", "--global", "tui.color", "false")
				if r.Err != nil {
					t.Fatalf("config set --global failed: %v\n%s", r.Err, r.Stderr)
				}

				globalCfg := filepath.Join(env.configDir, "config.toml")
				data, err := os.ReadFile(globalCfg)
				if err != nil {
					t.Fatalf("reading global: %v", err)
				}
				assertContains(t, string(data), "color = false")

				gotLocal, err := os.ReadFile(local)
				if err != nil {
					t.Fatalf("reading local: %v", err)
				}
				if string(gotLocal) != string(localContent) {
					t.Fatalf("local tusk.toml unexpectedly modified:\n%s", gotLocal)
				}
			},
		},
		{
			name: "config_set_no_local_falls_back_to_global",
			run: func(t *testing.T, env *Env) {
				root := resolvedTempDir(t)
				env.InDir(root)

				r := env.Run("config", "set", "tui.color", "false")
				if r.Err != nil {
					t.Fatalf("config set failed: %v\n%s", r.Err, r.Stderr)
				}

				if _, err := os.Stat(filepath.Join(root, "tusk.toml")); !os.IsNotExist(err) {
					t.Fatalf("local tusk.toml should not have been created; stat err = %v", err)
				}

				globalCfg := filepath.Join(env.configDir, "config.toml")
				data, err := os.ReadFile(globalCfg)
				if err != nil {
					t.Fatalf("reading global: %v", err)
				}
				assertContains(t, string(data), "color = false")
			},
		},
		{
			name:    "config_init_local_creates_file",
			skipFmt: []string{"json"},
			run: func(t *testing.T, env *Env) {
				root := resolvedTempDir(t)
				env.InDir(root)

				r := env.Run("config", "init", "--local")
				if r.Err != nil {
					t.Fatalf("config init --local failed: %v\n%s", r.Err, r.Stderr)
				}

				local := filepath.Join(root, "tusk.toml")
				if _, err := os.Stat(local); err != nil {
					t.Fatalf("expected local tusk.toml: %v", err)
				}

				validate := env.Run("config", "validate")
				if validate.Err != nil {
					t.Fatalf("config validate failed: %v\n%s", validate.Err, validate.Stderr)
				}

				show := env.Run("config", "show")
				if show.Err != nil {
					t.Fatalf("config show failed: %v\n%s", show.Err, show.Stderr)
				}
				wantHeader := "# active: " + local
				firstLine := strings.SplitN(show.Stdout, "\n", 2)[0]
				if firstLine != wantHeader {
					t.Fatalf("first line = %q, want %q", firstLine, wantHeader)
				}
			},
		},
		{
			name: "config_init_local_refuses_overwrite",
			run: func(t *testing.T, env *Env) {
				root := resolvedTempDir(t)
				local := filepath.Join(root, "tusk.toml")
				original := []byte("[tui]\ncolor = false\n")
				if err := os.WriteFile(local, original, 0o644); err != nil {
					t.Fatalf("writing local: %v", err)
				}
				env.InDir(root)

				r := env.Run("config", "init", "--local")
				if r.Err == nil {
					t.Fatalf("expected error, got none. stdout:\n%s", r.Stdout)
				}
				assertStderrContains(t, r, "file exists")

				got, err := os.ReadFile(local)
				if err != nil {
					t.Fatalf("reading local: %v", err)
				}
				if string(got) != string(original) {
					t.Fatalf("local tusk.toml unexpectedly overwritten:\n%s", got)
				}
			},
		},
		{
			name:    "walkup_tusk_config_env_overrides",
			skipFmt: []string{"json"},
			run: func(t *testing.T, env *Env) {
				root := resolvedTempDir(t)
				if err := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte("[tui]\ncolor = false\n"), 0o644); err != nil {
					t.Fatalf("writing local: %v", err)
				}
				other := filepath.Join(t.TempDir(), "custom.toml")
				if err := os.WriteFile(other, []byte("[tui]\ncolor = true\n"), 0o644); err != nil {
					t.Fatalf("writing other: %v", err)
				}
				env.InDir(root)
				env.WithEnv("TUSK_CONFIG", other)

				r := env.Run("config", "show")
				if r.Err != nil {
					t.Fatalf("config show failed: %v\n%s", r.Err, r.Stderr)
				}
				firstLine := strings.SplitN(r.Stdout, "\n", 2)[0]
				want := "# active: " + other
				if firstLine != want {
					t.Fatalf("first line = %q, want %q", firstLine, want)
				}
			},
		},
	}
	runWalkUpScenarios(t, scenarios)
}

// TestCLI_ConfigWalkUp_StoragePathRelative runs outside the matrix because
// --db would bypass storage.path, so the "flag" db mode cannot exercise the
// walk-up + relative storage interaction.
func TestCLI_ConfigWalkUp_StoragePathRelative(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	root := resolvedTempDir(t)
	if err := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte("[storage]\npath = \"./tusk.db\"\n"), 0o644); err != nil {
		t.Fatalf("writing local: %v", err)
	}
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	env := newEnv(t, binPath, "env", "text")
	env.dbPath = "" // do not pre-create; we want storage.path to decide
	env.configDir = t.TempDir()

	// Disable TUSK_DB injection by switching dbMode to a sentinel the harness
	// won't recognize. The simpler path: clear dbMode.
	env.dbMode = ""

	env.InDir(sub)
	addRes := env.Run("add", "walkup-foo")
	if addRes.Err != nil {
		t.Fatalf("add failed: %v\nstderr: %s", addRes.Err, addRes.Stderr)
	}

	env.InDir(root)
	listRes := env.Run("list")
	if listRes.Err != nil {
		t.Fatalf("list failed: %v\nstderr: %s", listRes.Err, listRes.Stderr)
	}
	assertContains(t, listRes.Stdout, "walkup-foo")

	// Sanity: the DB file lives next to tusk.toml.
	if _, err := os.Stat(filepath.Join(root, "tusk.db")); err != nil {
		t.Fatalf("expected tusk.db next to tusk.toml: %v", err)
	}
}
