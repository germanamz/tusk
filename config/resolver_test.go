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
			got, err := ResolveConfigFile("", tc.explicitFile, tc.globalDir)
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
