package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveConfigFile(t *testing.T) {
	// Build a temp globalDir that contains a real config.toml.
	populatedGlobal := t.TempDir()
	if err := os.WriteFile(filepath.Join(populatedGlobal, "config.toml"), []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("writing global config: %v", err)
	}

	// Build an empty globalDir with no config.toml.
	emptyGlobal := t.TempDir()

	// Build an explicit file that exists.
	existingExplicit := filepath.Join(t.TempDir(), "custom.toml")
	if err := os.WriteFile(existingExplicit, []byte("# custom\n"), 0o644); err != nil {
		t.Fatalf("writing explicit file: %v", err)
	}

	// A path that definitely does not exist.
	missingExplicit := filepath.Join(t.TempDir(), "nope.toml")

	tests := []struct {
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

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveConfigFile(tc.startDir, tc.explicitFile, tc.globalDir)
			if tc.wantErrSub != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil (path=%q)", tc.wantErrSub, got)
				}
				if !strings.Contains(err.Error(), tc.wantErrSub) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantPath {
				t.Fatalf("got %q, want %q", got, tc.wantPath)
			}
		})
	}
}

func TestResolveConfigFileWalkUp(t *testing.T) {
	writeTusk := func(t *testing.T, dir string) string {
		t.Helper()
		p := filepath.Join(dir, "tusk.toml")
		if err := os.WriteFile(p, []byte("# local\n"), 0o644); err != nil {
			t.Fatalf("writing %s: %v", p, err)
		}
		return p
	}
	writeGlobal := func(t *testing.T, dir string) string {
		t.Helper()
		p := filepath.Join(dir, "config.toml")
		if err := os.WriteFile(p, []byte("# global\n"), 0o644); err != nil {
			t.Fatalf("writing %s: %v", p, err)
		}
		return p
	}

	t.Run("walkup_cwd_hit", func(t *testing.T) {
		start := t.TempDir()
		want := writeTusk(t, start)
		got, err := ResolveConfigFile(start, "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("walkup_ancestor_hit", func(t *testing.T) {
		root := t.TempDir()
		want := writeTusk(t, root)
		start := filepath.Join(root, "a", "b")
		if err := os.MkdirAll(start, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		got, err := ResolveConfigFile(start, "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("walkup_root_stop", func(t *testing.T) {
		start := t.TempDir()
		got, err := ResolveConfigFile(start, "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})

	t.Run("walkup_walks_over_global", func(t *testing.T) {
		root := t.TempDir()
		localWant := writeTusk(t, root)
		globalDir := t.TempDir()
		writeGlobal(t, globalDir)
		start := filepath.Join(root, "child")
		if err := os.MkdirAll(start, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		got, err := ResolveConfigFile(start, "", globalDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != localWant {
			t.Fatalf("got %q, want %q", got, localWant)
		}
	})

	t.Run("walkup_skipped_when_explicit", func(t *testing.T) {
		start := t.TempDir()
		writeTusk(t, start)
		explicit := filepath.Join(t.TempDir(), "custom.toml")
		if err := os.WriteFile(explicit, []byte("# custom\n"), 0o644); err != nil {
			t.Fatalf("writing explicit: %v", err)
		}
		got, err := ResolveConfigFile(start, explicit, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != explicit {
			t.Fatalf("got %q, want %q", got, explicit)
		}
	})
}
