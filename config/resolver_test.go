package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveConfigFile(test *testing.T) {
	// Build a temp globalDir that contains a real config.toml.
	populatedGlobal := test.TempDir()
	if err := os.WriteFile(filepath.Join(populatedGlobal, "config.toml"), []byte("# test\n"), 0o644); err != nil {
		test.Fatalf("writing global config: %v", err)
	}

	// Build an empty globalDir with no config.toml.
	emptyGlobal := test.TempDir()

	// Build an explicit file that exists.
	existingExplicit := filepath.Join(test.TempDir(), "custom.toml")
	if err := os.WriteFile(existingExplicit, []byte("# custom\n"), 0o644); err != nil {
		test.Fatalf("writing explicit file: %v", err)
	}

	// A path that definitely does not exist.
	missingExplicit := filepath.Join(test.TempDir(), "nope.toml")

	cases := []struct {
		name         string
		startDir     string
		explicitFile string
		globalDir    string
		wantPath     string // "" means expect empty-string return
		wantErrSub   string // non-empty means expect an error containing this substring
	}{
		{
			name:         "explicit file exists",
			explicitFile: existingExplicit,
			globalDir:    populatedGlobal,
			wantPath:     existingExplicit,
		},
		{
			name:         "explicit file missing is hard error",
			explicitFile: missingExplicit,
			globalDir:    populatedGlobal,
			wantErrSub:   "config file not found",
		},
		{
			name:      "global file exists",
			globalDir: populatedGlobal,
			wantPath:  filepath.Join(populatedGlobal, "config.toml"),
		},
		{
			name:      "global file missing returns empty",
			globalDir: emptyGlobal,
			wantPath:  "",
		},
		{
			name:     "no global dir and no explicit file returns empty",
			wantPath: "",
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			got, err := ResolveConfigFile(testCase.startDir, testCase.explicitFile, testCase.globalDir)
			if testCase.wantErrSub != "" {
				if err == nil {
					test.Fatalf("want error containing %q, got nil (path=%q)", testCase.wantErrSub, got)
				}
				if !strings.Contains(err.Error(), testCase.wantErrSub) {
					test.Fatalf("error %q does not contain %q", err.Error(), testCase.wantErrSub)
				}
				return
			}
			if err != nil {
				test.Fatalf("unexpected error: %v", err)
			}
			if got != testCase.wantPath {
				test.Fatalf("got %q, want %q", got, testCase.wantPath)
			}
		})
	}
}

func TestResolveConfigFileWalkUp(test *testing.T) {
	writeTusk := func(test *testing.T, dir string) string {
		test.Helper()
		filePath := filepath.Join(dir, "tusk.toml")
		if err := os.WriteFile(filePath, []byte("# local\n"), 0o644); err != nil {
			test.Fatalf("writing %s: %v", filePath, err)
		}
		return filePath
	}
	writeGlobal := func(test *testing.T, dir string) string {
		test.Helper()
		filePath := filepath.Join(dir, "config.toml")
		if err := os.WriteFile(filePath, []byte("# global\n"), 0o644); err != nil {
			test.Fatalf("writing %s: %v", filePath, err)
		}
		return filePath
	}

	test.Run("walkup_cwd_hit", func(test *testing.T) {
		start := test.TempDir()
		want := writeTusk(test, start)
		got, err := ResolveConfigFile(start, "", "")

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		if got != want {
			test.Fatalf("got %q, want %q", got, want)
		}
	})

	test.Run("walkup_ancestor_hit", func(test *testing.T) {
		root := test.TempDir()
		want := writeTusk(test, root)
		start := filepath.Join(root, "a", "b")
		if err := os.MkdirAll(start, 0o755); err != nil {
			test.Fatalf("mkdir: %v", err)
		}
		got, err := ResolveConfigFile(start, "", "")

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		if got != want {
			test.Fatalf("got %q, want %q", got, want)
		}
	})

	test.Run("walkup_root_stop", func(test *testing.T) {
		start := test.TempDir()
		got, err := ResolveConfigFile(start, "", "")

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		if got != "" {
			test.Fatalf("got %q, want empty", got)
		}
	})

	test.Run("walkup_walks_over_global", func(test *testing.T) {
		root := test.TempDir()
		localWant := writeTusk(test, root)
		globalDir := test.TempDir()
		writeGlobal(test, globalDir)
		start := filepath.Join(root, "child")
		if err := os.MkdirAll(start, 0o755); err != nil {
			test.Fatalf("mkdir: %v", err)
		}
		got, err := ResolveConfigFile(start, "", globalDir)

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		if got != localWant {
			test.Fatalf("got %q, want %q", got, localWant)
		}
	})

	test.Run("walkup_skipped_when_explicit", func(test *testing.T) {
		start := test.TempDir()
		writeTusk(test, start)
		explicit := filepath.Join(test.TempDir(), "custom.toml")
		if err := os.WriteFile(explicit, []byte("# custom\n"), 0o644); err != nil {
			test.Fatalf("writing explicit: %v", err)
		}
		got, err := ResolveConfigFile(start, explicit, "")

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		if got != explicit {
			test.Fatalf("got %q, want %q", got, explicit)
		}
	})
}
