package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCommands_ManifestLoadErrorNamesManifestOnce runs each command that loads
// tusk.toml up front against an invalid one: the error must say "manifest: "
// once, as manifest.Load already starts with it (#869).
func TestCommands_ManifestLoadErrorNamesManifestOnce(test *testing.T) {
	commands := [][]string{
		{"reindex"},
		{"reload"},
		{"reset", "--yes"},
	}

	for _, args := range commands {
		test.Run(args[0], func(test *testing.T) {
			root := test.TempDir()

			if mkErr := os.MkdirAll(filepath.Join(root, ".tusk"), 0o755); mkErr != nil {
				test.Fatalf("mkdir .tusk: %v", mkErr)
			}

			body := "[workspace]\nname = \"probe\"\nlink-case = \"bogus\"\n"

			if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(body), 0o644); writeErr != nil {
				test.Fatalf("write tusk.toml: %v", writeErr)
			}

			chdir(test, root)

			out, runErr := runCLI(args...)

			if runErr == nil {
				test.Fatalf("%v succeeded on an invalid manifest (out=%s)", args, out)
			}

			if count := strings.Count(runErr.Error(), "manifest: "); count != 1 {
				test.Errorf("%v error = %q, want %q once (got %d)", args, runErr.Error(), "manifest: ", count)
			}
		})
	}
}
