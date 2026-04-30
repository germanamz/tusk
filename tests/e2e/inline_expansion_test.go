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
	run     func(test *testing.T, env *Env, dir string)
}

func runInlineScenarios(test *testing.T, scenarios []inlineScenario) {
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
			if slices.Contains(scenario.skipFmt, format) {
				continue
			}
			name := scenario.name + "/" + dbMode + "/" + format
			test.Run(name, func(test *testing.T) {
				test.Parallel()
				env := newEnv(test, binPath, dbMode, format)
				dir := test.TempDir()
				env.InDir(dir)
				scenario.run(test, env, dir)
			})
		}
	}
}

func writeFixture(test *testing.T, dir, name, content string) {
	test.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		test.Fatalf("writing fixture %s: %v", name, err)
	}
}

func TestCLI_InlineExpansion(test *testing.T) {
	scenarios := []inlineScenario{
		{
			name: "description_whole_file",
			run: func(test *testing.T, env *Env, dir string) {
				writeFixture(test, dir, "spec.md", "## Spec body\n\nDetails here.")
				createResult := env.Run("task", "create", "Spec task", "description=@./spec.md")
				if createResult.Err != nil {
					test.Fatalf("create: %v\n%s", createResult.Err, createResult.Stderr)
				}
				getResult := env.Run("task", "get", "$0.short_id")
				if getResult.Err != nil {
					test.Fatalf("get: %v\n%s", getResult.Err, getResult.Stderr)
				}
				assertContains(test, getResult.Stdout, "Spec body")
				assertContains(test, getResult.Stdout, "Details here.")
			},
		},
		{
			name: "description_midstring_expansion",
			run: func(test *testing.T, env *Env, dir string) {
				writeFixture(test, dir, "notes.md", "the relevant notes")
				createResult := env.Run("task", "create", "Mid task", `description="see @./notes.md for details"`)
				if createResult.Err != nil {
					test.Fatalf("create: %v\n%s", createResult.Err, createResult.Stderr)
				}
				getResult := env.Run("task", "get", "$0.short_id")
				if getResult.Err != nil {
					test.Fatalf("get: %v\n%s", getResult.Err, getResult.Stderr)
				}
				assertContains(test, getResult.Stdout, "see the relevant notes for details")
			},
		},
		{
			name: "description_word_internal_at_preserved",
			run: func(test *testing.T, env *Env, _ string) {
				createResult := env.Run("task", "create", "Email task", `description="email@example.com"`)
				if createResult.Err != nil {
					test.Fatalf("create: %v\n%s", createResult.Err, createResult.Stderr)
				}
				getResult := env.Run("task", "get", "$0.short_id")
				if getResult.Err != nil {
					test.Fatalf("get: %v\n%s", getResult.Err, getResult.Stderr)
				}
				assertContains(test, getResult.Stdout, "email@example.com")
			},
		},
		{
			name: "description_at_escape",
			run: func(test *testing.T, env *Env, _ string) {
				createResult := env.Run("task", "create", "Escape task", `description="@@literal"`)
				if createResult.Err != nil {
					test.Fatalf("create: %v\n%s", createResult.Err, createResult.Stderr)
				}
				getResult := env.Run("task", "get", "$0.short_id")
				if getResult.Err != nil {
					test.Fatalf("get: %v\n%s", getResult.Err, getResult.Stderr)
				}
				assertContains(test, getResult.Stdout, "@literal")
				assertNotContains(test, getResult.Stdout, "@@literal")
			},
		},
		{
			name: "modify_description_from_file",
			run: func(test *testing.T, env *Env, dir string) {
				writeFixture(test, dir, "new.md", "updated content")
				createResult := env.Run("task", "create", "Replace target")
				if createResult.Err != nil {
					test.Fatalf("create: %v\n%s", createResult.Err, createResult.Stderr)
				}
				modifyResult := env.Run("task", "modify", "$0.short_id", "description=@./new.md")
				if modifyResult.Err != nil {
					test.Fatalf("modify: %v\n%s", modifyResult.Err, modifyResult.Stderr)
				}
				getResult := env.Run("task", "get", "$0.short_id")
				if getResult.Err != nil {
					test.Fatalf("get: %v\n%s", getResult.Err, getResult.Stderr)
				}
				assertContains(test, getResult.Stdout, "updated content")
			},
		},
		{
			name: "modify_title_from_file",
			run: func(test *testing.T, env *Env, dir string) {
				writeFixture(test, dir, "title.txt", "New Title From File")
				createResult := env.Run("task", "create", "Original title")
				if createResult.Err != nil {
					test.Fatalf("create: %v\n%s", createResult.Err, createResult.Stderr)
				}
				modifyResult := env.Run("task", "modify", "$0.short_id", "title=@./title.txt")
				if modifyResult.Err != nil {
					test.Fatalf("modify: %v\n%s", modifyResult.Err, modifyResult.Stderr)
				}
				getResult := env.Run("task", "get", "$0.short_id")
				if getResult.Err != nil {
					test.Fatalf("get: %v\n%s", getResult.Err, getResult.Stderr)
				}
				assertContains(test, getResult.Stdout, "New Title From File")
			},
		},
		{
			name: "create_title_and_description_from_files",
			run: func(test *testing.T, env *Env, dir string) {
				writeFixture(test, dir, "title.txt", "Loaded Title")
				writeFixture(test, dir, "body.md", "Loaded body content")
				createResult := env.Run("task", "create", "title=@./title.txt", "description=@./body.md")
				if createResult.Err != nil {
					test.Fatalf("create: %v\n%s", createResult.Err, createResult.Stderr)
				}
				getResult := env.Run("task", "get", "$0.short_id")
				if getResult.Err != nil {
					test.Fatalf("get: %v\n%s", getResult.Err, getResult.Stderr)
				}
				assertContains(test, getResult.Stdout, "Loaded Title")
				assertContains(test, getResult.Stdout, "Loaded body content")
			},
		},
		{
			name: "annotate_positional_file",
			run: func(test *testing.T, env *Env, dir string) {
				writeFixture(test, dir, "note.md", "annotation loaded from file")
				createResult := env.Run("task", "create", "Annotate target")
				if createResult.Err != nil {
					test.Fatalf("create: %v\n%s", createResult.Err, createResult.Stderr)
				}
				annotateResult := env.Run("task", "annotate", "$0.short_id", "@./note.md")
				if annotateResult.Err != nil {
					test.Fatalf("annotate: %v\n%s", annotateResult.Err, annotateResult.Stderr)
				}
				getResult := env.Run("task", "get", "$0.short_id")
				if getResult.Err != nil {
					test.Fatalf("get: %v\n%s", getResult.Err, getResult.Stderr)
				}
				assertContains(test, getResult.Stdout, "annotation loaded from file")
			},
		},
		{
			name: "stale_short_flag_rejected",
			run: func(test *testing.T, env *Env, _ string) {
				result := env.Run("task", "create", "t", "-d", "body")
				if result.Err == nil {
					test.Fatalf("expected error, stdout: %s", result.Stdout)
				}
				if !strings.Contains(result.Stderr, "unknown shorthand flag") &&
					!strings.Contains(result.Stderr, "unknown shorthand") {
					test.Fatalf("expected unknown shorthand flag error, got stderr:\n%s", result.Stderr)
				}
			},
		},
		{
			name: "stale_long_flag_rejected",
			run: func(test *testing.T, env *Env, _ string) {
				result := env.Run("task", "create", "t", "--description", "body")
				if result.Err == nil {
					test.Fatalf("expected error, stdout: %s", result.Stdout)
				}
				if !strings.Contains(result.Stderr, "unknown flag") {
					test.Fatalf("expected unknown flag error, got stderr:\n%s", result.Stderr)
				}
			},
		},
		{
			name: "binary_file_rejected",
			run: func(test *testing.T, env *Env, dir string) {
				path := filepath.Join(dir, "binary.bin")
				if err := os.WriteFile(path, []byte("hello\x00world"), 0o644); err != nil {
					test.Fatalf("writing binary fixture: %v", err)
				}
				result := env.Run("task", "create", "Bin task", "description=@./binary.bin")
				if result.Err == nil {
					test.Fatalf("expected error, stdout: %s", result.Stdout)
				}
				if !strings.Contains(result.Stderr, "binary file") {
					test.Fatalf("expected binary file error, got stderr:\n%s", result.Stderr)
				}
			},
		},
		{
			name: "file_over_size_cap",
			run: func(test *testing.T, env *Env, dir string) {
				// Override the default 1 MB cap with a tiny 100-byte cap via
				// a workspace tusk.toml at the scenario root. The binary's
				// walk-up config loader picks it up because InDir sets the
				// CLI's working directory to `dir`.
				cfg := "[inline]\nmax_expansion_size = 100\n"
				writeFixture(test, dir, "tusk.toml", cfg)
				big := strings.Repeat("x", 200)
				writeFixture(test, dir, "big.txt", big)

				result := env.Run("task", "create", "Big task", "description=@./big.txt")
				if result.Err == nil {
					test.Fatalf("expected error, stdout: %s", result.Stdout)
				}
				if !strings.Contains(result.Stderr, "exceeds") || !strings.Contains(result.Stderr, "limit") {
					test.Fatalf("expected size-cap error with 'exceeds' and 'limit', got stderr:\n%s", result.Stderr)
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
			run: func(test *testing.T, env *Env, dir string) {
				writeFixture(test, dir, "my file.txt", "spaced content")
				createResult := env.Run("task", "create", "Spaced task",
					`description="prefix @\"./my file.txt\" suffix"`)
				if createResult.Err != nil {
					test.Fatalf("create: %v\n%s", createResult.Err, createResult.Stderr)
				}
				getResult := env.Run("task", "get", "$0.short_id")
				if getResult.Err != nil {
					test.Fatalf("get: %v\n%s", getResult.Err, getResult.Stderr)
				}
				assertContains(test, getResult.Stdout, "prefix spaced content suffix")
			},
		},
	}

	runInlineScenarios(test, scenarios)
}

// TestCLI_InlineExpansion_Stdin exercises the `@-` stdin path for both the
// annotate positional body and `description=@-` on create. Each scenario
// runs across the (dbMode, format) matrix via runScenarios.
func TestCLI_InlineExpansion_Stdin(test *testing.T) {
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
					Assert: func(test *testing.T, result Result) {
						assertContains(test, result.Stdout, "piped note body")
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
					Assert: func(test *testing.T, result Result) {
						assertContains(test, result.Stdout, "piped description body")
					},
				},
			},
		},
	}
	runScenarios(test, binPath, scenarios)
}
