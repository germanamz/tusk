package e2e

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// inlineScenario drives an e2e scenario that needs per-run filesystem
// fixtures (e.g. a file to reference via `description=@./spec.md`). We reuse
// the walkUpScenario shape: one run per (dbMode, format) combo, with setup
// logic inside the scenario body rather than a declarative Step list.
type inlineScenario struct {
	name    string
	skipFmt []string
	run     func(t *testing.T, env *Env, dir string)
}

func runInlineScenarios(t *testing.T, scenarios []inlineScenario) {
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
			if slices.Contains(sc.skipFmt, format) {
				continue
			}
			name := sc.name + "/" + dbMode + "/" + format
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				env := newEnv(t, binPath, dbMode, format)
				dir := t.TempDir()
				env.InDir(dir)
				sc.run(t, env, dir)
			})
		}
	}
}

func writeFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("writing fixture %s: %v", name, err)
	}
}

func TestCLI_InlineExpansion(t *testing.T) {
	scenarios := []inlineScenario{
		{
			name: "description_whole_file",
			run: func(t *testing.T, env *Env, dir string) {
				writeFixture(t, dir, "spec.md", "## Spec body\n\nDetails here.")
				r := env.Run("task", "create", "Spec task", "description=@./spec.md")
				if r.Err != nil {
					t.Fatalf("create: %v\n%s", r.Err, r.Stderr)
				}
				r2 := env.Run("task", "get", "$0.short_id")
				if r2.Err != nil {
					t.Fatalf("get: %v\n%s", r2.Err, r2.Stderr)
				}
				assertContains(t, r2.Stdout, "Spec body")
				assertContains(t, r2.Stdout, "Details here.")
			},
		},
		{
			name: "description_midstring_expansion",
			run: func(t *testing.T, env *Env, dir string) {
				writeFixture(t, dir, "notes.md", "the relevant notes")
				r := env.Run("task", "create", "Mid task", `description="see @./notes.md for details"`)
				if r.Err != nil {
					t.Fatalf("create: %v\n%s", r.Err, r.Stderr)
				}
				r2 := env.Run("task", "get", "$0.short_id")
				if r2.Err != nil {
					t.Fatalf("get: %v\n%s", r2.Err, r2.Stderr)
				}
				assertContains(t, r2.Stdout, "see the relevant notes for details")
			},
		},
		{
			name: "description_word_internal_at_preserved",
			run: func(t *testing.T, env *Env, _ string) {
				r := env.Run("task", "create", "Email task", `description="email@example.com"`)
				if r.Err != nil {
					t.Fatalf("create: %v\n%s", r.Err, r.Stderr)
				}
				r2 := env.Run("task", "get", "$0.short_id")
				if r2.Err != nil {
					t.Fatalf("get: %v\n%s", r2.Err, r2.Stderr)
				}
				assertContains(t, r2.Stdout, "email@example.com")
			},
		},
		{
			name: "description_at_escape",
			run: func(t *testing.T, env *Env, _ string) {
				r := env.Run("task", "create", "Escape task", `description="@@literal"`)
				if r.Err != nil {
					t.Fatalf("create: %v\n%s", r.Err, r.Stderr)
				}
				r2 := env.Run("task", "get", "$0.short_id")
				if r2.Err != nil {
					t.Fatalf("get: %v\n%s", r2.Err, r2.Stderr)
				}
				assertContains(t, r2.Stdout, "@literal")
				assertNotContains(t, r2.Stdout, "@@literal")
			},
		},
		{
			name: "modify_description_from_file",
			run: func(t *testing.T, env *Env, dir string) {
				writeFixture(t, dir, "new.md", "updated content")
				r := env.Run("task", "create", "Replace target")
				if r.Err != nil {
					t.Fatalf("create: %v\n%s", r.Err, r.Stderr)
				}
				rm := env.Run("task", "modify", "$0.short_id", "description=@./new.md")
				if rm.Err != nil {
					t.Fatalf("modify: %v\n%s", rm.Err, rm.Stderr)
				}
				rg := env.Run("task", "get", "$0.short_id")
				if rg.Err != nil {
					t.Fatalf("get: %v\n%s", rg.Err, rg.Stderr)
				}
				assertContains(t, rg.Stdout, "updated content")
			},
		},
		{
			name: "modify_title_from_file",
			run: func(t *testing.T, env *Env, dir string) {
				writeFixture(t, dir, "title.txt", "New Title From File")
				r := env.Run("task", "create", "Original title")
				if r.Err != nil {
					t.Fatalf("create: %v\n%s", r.Err, r.Stderr)
				}
				rm := env.Run("task", "modify", "$0.short_id", "title=@./title.txt")
				if rm.Err != nil {
					t.Fatalf("modify: %v\n%s", rm.Err, rm.Stderr)
				}
				rg := env.Run("task", "get", "$0.short_id")
				if rg.Err != nil {
					t.Fatalf("get: %v\n%s", rg.Err, rg.Stderr)
				}
				assertContains(t, rg.Stdout, "New Title From File")
			},
		},
		{
			name: "create_title_and_description_from_files",
			run: func(t *testing.T, env *Env, dir string) {
				writeFixture(t, dir, "title.txt", "Loaded Title")
				writeFixture(t, dir, "body.md", "Loaded body content")
				r := env.Run("task", "create", "title=@./title.txt", "description=@./body.md")
				if r.Err != nil {
					t.Fatalf("create: %v\n%s", r.Err, r.Stderr)
				}
				rg := env.Run("task", "get", "$0.short_id")
				if rg.Err != nil {
					t.Fatalf("get: %v\n%s", rg.Err, rg.Stderr)
				}
				assertContains(t, rg.Stdout, "Loaded Title")
				assertContains(t, rg.Stdout, "Loaded body content")
			},
		},
		{
			name: "annotate_positional_file",
			run: func(t *testing.T, env *Env, dir string) {
				writeFixture(t, dir, "note.md", "annotation loaded from file")
				r := env.Run("task", "create", "Annotate target")
				if r.Err != nil {
					t.Fatalf("create: %v\n%s", r.Err, r.Stderr)
				}
				ra := env.Run("task", "annotate", "$0.short_id", "@./note.md")
				if ra.Err != nil {
					t.Fatalf("annotate: %v\n%s", ra.Err, ra.Stderr)
				}
				rg := env.Run("task", "get", "$0.short_id")
				if rg.Err != nil {
					t.Fatalf("get: %v\n%s", rg.Err, rg.Stderr)
				}
				assertContains(t, rg.Stdout, "annotation loaded from file")
			},
		},
		{
			name: "stale_short_flag_rejected",
			run: func(t *testing.T, env *Env, _ string) {
				r := env.Run("task", "create", "t", "-d", "body")
				if r.Err == nil {
					t.Fatalf("expected error, stdout: %s", r.Stdout)
				}
				if !strings.Contains(r.Stderr, "unknown shorthand flag") &&
					!strings.Contains(r.Stderr, "unknown shorthand") {
					t.Fatalf("expected unknown shorthand flag error, got stderr:\n%s", r.Stderr)
				}
			},
		},
		{
			name: "stale_long_flag_rejected",
			run: func(t *testing.T, env *Env, _ string) {
				r := env.Run("task", "create", "t", "--description", "body")
				if r.Err == nil {
					t.Fatalf("expected error, stdout: %s", r.Stdout)
				}
				if !strings.Contains(r.Stderr, "unknown flag") {
					t.Fatalf("expected unknown flag error, got stderr:\n%s", r.Stderr)
				}
			},
		},
		{
			name: "binary_file_rejected",
			run: func(t *testing.T, env *Env, dir string) {
				path := filepath.Join(dir, "binary.bin")
				if err := os.WriteFile(path, []byte("hello\x00world"), 0o644); err != nil {
					t.Fatalf("writing binary fixture: %v", err)
				}
				r := env.Run("task", "create", "Bin task", "description=@./binary.bin")
				if r.Err == nil {
					t.Fatalf("expected error, stdout: %s", r.Stdout)
				}
				if !strings.Contains(r.Stderr, "binary file") {
					t.Fatalf("expected binary file error, got stderr:\n%s", r.Stderr)
				}
			},
		},
		{
			name: "file_over_size_cap",
			run: func(t *testing.T, env *Env, dir string) {
				// Override the default 1 MB cap with a tiny 100-byte cap via
				// a workspace tusk.toml at the scenario root. The binary's
				// walk-up config loader picks it up because InDir sets the
				// CLI's working directory to `dir`.
				cfg := "[inline]\nmax_expansion_size = 100\n"
				writeFixture(t, dir, "tusk.toml", cfg)
				big := strings.Repeat("x", 200)
				writeFixture(t, dir, "big.txt", big)

				r := env.Run("task", "create", "Big task", "description=@./big.txt")
				if r.Err == nil {
					t.Fatalf("expected error, stdout: %s", r.Stdout)
				}
				if !strings.Contains(r.Stderr, "exceeds") || !strings.Contains(r.Stderr, "limit") {
					t.Fatalf("expected size-cap error with 'exceeds' and 'limit', got stderr:\n%s", r.Stderr)
				}
			},
		},
		{
			// The quoted-path-with-space form only survives the lexer inside
			// an outer-quoted value with \" escapes — the outer quotes hide
			// the inner quotes from being stripped, and the expander then
			// sees literal @"..." on its scan. A bare `description=@"./my
			// file.txt"` does not work because the lexer strips the mid-
			// token quotes before the expander runs. The mid-string form
			// below is the documented way to reference a path with a space.
			name: "quoted_path_with_space",
			run: func(t *testing.T, env *Env, dir string) {
				writeFixture(t, dir, "my file.txt", "spaced content")
				r := env.Run("task", "create", "Spaced task",
					`description="prefix @\"./my file.txt\" suffix"`)
				if r.Err != nil {
					t.Fatalf("create: %v\n%s", r.Err, r.Stderr)
				}
				rg := env.Run("task", "get", "$0.short_id")
				if rg.Err != nil {
					t.Fatalf("get: %v\n%s", rg.Err, rg.Stderr)
				}
				assertContains(t, rg.Stdout, "prefix spaced content suffix")
			},
		},
	}

	runInlineScenarios(t, scenarios)
}

// TestCLI_InlineExpansion_Stdin exercises the `@-` stdin path for both the
// annotate positional body and `description=@-` on create. Each scenario
// runs across the (dbMode, format) matrix via runScenarios.
func TestCLI_InlineExpansion_Stdin(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "annotate_stdin",
			Steps: []Step{
				{Args: []string{"task", "create", "Stdin annotate target"}},
				{
					Stdin: "piped note body",
					Args:  []string{"task", "annotate", "$0.short_id", "@-"},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					Assert: func(t *testing.T, r Result) {
						assertContains(t, r.Stdout, "piped note body")
					},
				},
			},
		},
		{
			Name: "description_stdin",
			Steps: []Step{
				{
					Stdin: "piped description body",
					Args:  []string{"task", "create", "Stdin desc task", "description=@-"},
				},
				{
					Args: []string{"task", "get", "$0.short_id"},
					Assert: func(t *testing.T, r Result) {
						assertContains(t, r.Stdout, "piped description body")
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}
