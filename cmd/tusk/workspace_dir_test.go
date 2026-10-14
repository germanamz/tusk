package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The tests below pin #794: --workspace (-C) and TUSK_WORKSPACE point tusk at
// a vault without relying on the working directory, the flag wins over the
// variable and the variable over the working directory, and a missing vault
// says where tusk looked.

func TestWorkspaceFlag_RunsInTheNamedVault(test *testing.T) {
	root := setupTempWorkspace(test)
	outside := test.TempDir()

	for _, args := range [][]string{
		{"--workspace", root, "status"},
		{"-C", root, "status"},
		{"status", "--workspace", root},
	} {
		if _, stderr, ok := runCLISplit(outside, args...); !ok {
			test.Errorf("tusk %s from outside the vault failed:\n%s", strings.Join(args, " "), stderr)
		}
	}
}

func TestWorkspaceEnv_RunsInTheNamedVault(test *testing.T) {
	root := setupTempWorkspace(test)

	test.Setenv(workspaceEnv, root)

	if _, stderr, ok := runCLISplit(test.TempDir(), "status"); !ok {
		test.Fatalf("tusk status with %s set failed:\n%s", workspaceEnv, stderr)
	}
}

func TestWorkspaceFlag_WinsOverEnvAndEnvOverWorkingDirectory(test *testing.T) {
	root := setupTempWorkspace(test)
	bare := test.TempDir()

	test.Setenv(workspaceEnv, bare)

	if _, stderr, ok := runCLISplit(bare, "--workspace", root, "status"); !ok {
		test.Fatalf("--workspace did not win over %s:\n%s", workspaceEnv, stderr)
	}

	_, stderr, ok := runCLISplit(root, "status")

	if ok {
		test.Fatalf("%s naming a folder with no vault should win over a working directory inside one", workspaceEnv)
	}

	want := "no tusk.toml in " + bare + " or any parent directory (from TUSK_WORKSPACE)\n"

	if stderr.String() != want {
		test.Errorf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestWorkspaceNotFound_NamesTheDirectoryAndSuggestsTheFlag(test *testing.T) {
	bare := test.TempDir()
	want := "no tusk.toml in <WS> or any parent directory; run tusk init to create one, or point tusk at a vault with --workspace <dir> or TUSK_WORKSPACE\n"

	for _, verb := range []string{"status", "mcp", "reindex", "pack"} {
		args := []string{verb}

		if verb == "pack" {
			args = []string{"pack", "add", "vault"}
		}

		_, stderr, ok := runCLISplit(bare, args...)

		if ok {
			test.Fatalf("tusk %s outside a vault succeeded", verb)
		}

		if got := scrubWorkspace(stderr.String(), bare); got != want {
			test.Errorf("tusk %s: stderr = %q, want %q", verb, got, want)
		}
	}

	_, stderr, _ := runCLISplit(test.TempDir(), "--workspace", bare, "mcp")

	if want := "no tusk.toml in " + bare + " or any parent directory (from --workspace)\n"; stderr.String() != want {
		test.Errorf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestWorkspaceFlag_RejectsAMissingDirectoryOrAFile(test *testing.T) {
	bare := test.TempDir()
	missing := filepath.Join(bare, "missing")
	file := filepath.Join(bare, "notes.md")

	if writeErr := os.WriteFile(file, []byte("# notes\n"), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	for dir, want := range map[string]string{
		missing: "--workspace " + missing + ": no such directory\n",
		file:    "--workspace " + file + ": not a directory\n",
	} {
		_, stderr, ok := runCLISplit(bare, "--workspace", dir, "status")

		if ok {
			test.Fatalf("--workspace %s succeeded", dir)
		}

		if stderr.String() != want {
			test.Errorf("stderr = %q, want %q", stderr.String(), want)
		}
	}
}

func TestWorkspaceFlag_ExpandsALeadingTilde(test *testing.T) {
	root := setupTempWorkspace(test)

	test.Setenv("HOME", filepath.Dir(root))
	test.Setenv("USERPROFILE", filepath.Dir(root))

	tilde := "~/" + filepath.Base(root)

	if _, stderr, ok := runCLISplit(test.TempDir(), "--workspace", tilde, "status"); !ok {
		test.Fatalf("--workspace %s failed:\n%s", tilde, stderr)
	}

	test.Setenv(workspaceEnv, tilde)

	if _, stderr, ok := runCLISplit(test.TempDir(), "status"); !ok {
		test.Fatalf("%s=%s failed:\n%s", workspaceEnv, tilde, stderr)
	}
}

func TestExpandHome(test *testing.T) {
	home := test.TempDir()

	test.Setenv("HOME", home)
	test.Setenv("USERPROFILE", home)

	for input, want := range map[string]string{
		"~":          home,
		"~/notes":    filepath.Join(home, "notes"),
		"~/a/b":      filepath.Join(home, "a", "b"),
		"~bob/notes": "~bob/notes",
		"/abs/~":     "/abs/~",
		"notes":      "notes",
	} {
		got, expandErr := expandHome(input)

		if expandErr != nil {
			test.Fatalf("expandHome(%q): %v", input, expandErr)
		}

		if got != want {
			test.Errorf("expandHome(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestInit_WorkspaceFlagCreatesTheVaultThere(test *testing.T) {
	test.Setenv("TUSK_EMBED_WORKERS", "1")

	target := test.TempDir()
	outside := test.TempDir()

	stdout, stderr, ok := runCLISplit(outside, "init", "--name", "notes", "--workspace", target)

	if !ok {
		test.Fatalf("init --workspace failed:\n%s", stderr)
	}

	if want := "Initialized Tusk workspace at " + target + "\n"; stdout.String() != want {
		test.Errorf("stdout = %q, want %q", stdout.String(), want)
	}

	if _, statErr := os.Stat(filepath.Join(target, "tusk.toml")); statErr != nil {
		test.Errorf("tusk.toml not created in the --workspace directory: %v", statErr)
	}

	if _, statErr := os.Stat(filepath.Join(outside, "tusk.toml")); statErr == nil {
		test.Errorf("tusk.toml created in the working directory instead")
	}
}

func TestPackAdd_WalksUpFromASubfolder(test *testing.T) {
	root := setupTempWorkspace(test)
	sub := filepath.Join(root, "notes", "deep")

	if mkErr := os.MkdirAll(sub, 0o755); mkErr != nil {
		test.Fatalf("mkdir: %v", mkErr)
	}

	if _, stderr, ok := runCLISplit(sub, "pack", "add", "vault"); !ok {
		test.Fatalf("pack add from a subfolder failed:\n%s", stderr)
	}

	body, readErr := os.ReadFile(filepath.Join(root, "tusk.toml"))

	if readErr != nil {
		test.Fatalf("read tusk.toml: %v", readErr)
	}

	if !strings.Contains(string(body), "[node-types.note]") {
		test.Errorf("vault pack not merged into the root tusk.toml:\n%s", body)
	}

	if _, statErr := os.Stat(filepath.Join(sub, "tusk.toml")); statErr == nil {
		test.Errorf("pack add wrote a tusk.toml into the subfolder")
	}
}
