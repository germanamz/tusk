package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The wire tier spawns the built ./bin/tusk binary to pin what the in-process
// fast tier cannot observe: real process exit codes (notably pack add's
// os.Exit(2|3), which would kill an in-process test) and docgen's generated
// files. It is guarded by !testing.Short() so the pre-commit fast tier stays
// quick; the literal stderr/stdout substrings reuse what the fast tier froze.

var (
	wireBinOnce sync.Once
	wireBinPath string
	wireBinErr  error
)

// tuskBinary builds ./bin/tusk once and returns its path.
func tuskBinary(test *testing.T) string {
	test.Helper()

	wireBinOnce.Do(func() {
		dir, mkErr := os.MkdirTemp("", "tusk-wire-*")

		if mkErr != nil {
			wireBinErr = mkErr

			return
		}

		wireBinPath = filepath.Join(dir, "tusk")
		build := exec.Command("go", "build", "-o", wireBinPath, "./cmd/tusk")
		build.Dir = repoRoot(test)

		if out, buildErr := build.CombinedOutput(); buildErr != nil {
			wireBinErr = fmt.Errorf("build tusk: %v\n%s", buildErr, out)
		}
	})

	if wireBinErr != nil {
		test.Fatalf("tusk binary: %v", wireBinErr)
	}

	return wireBinPath
}

// runWire executes the binary in root and returns stdout, stderr, and the real
// process exit code.
func runWire(test *testing.T, root string, args ...string) (string, string, int) {
	test.Helper()

	cmd := exec.Command(tuskBinary(test), args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "TUSK_EMBED_WORKERS=1", "TUSK_LEASE_TTL_SECONDS=3600")

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	exitCode := 0

	if runErr != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](runErr); ok {
			exitCode = exitErr.ExitCode()
		} else {
			test.Fatalf("run %v: %v", args, runErr)
		}
	}

	return stdout.String(), stderr.String(), exitCode
}

// wireWorkspace returns a temp dir initialized as a workspace via the binary.
func wireWorkspace(test *testing.T) string {
	test.Helper()

	root := test.TempDir()

	if _, stderr, exit := runWire(test, root, "init", "--name", "test"); exit != 0 {
		test.Fatalf("wire init exit %d: %s", exit, stderr)
	}

	return root
}

type goldenWireCase struct {
	name               string
	setup              func(test *testing.T, root string)
	args               []string
	argsFunc           func(root string) []string
	wantExit           int
	wantStderrContains string
}

// TestGoldenWire pins the real process exit codes the fast tier cannot see.
func TestGoldenWire(test *testing.T) {
	if testing.Short() {
		test.Skip("wire tier spawns the built binary; skipped under -short")
	}

	cases := []goldenWireCase{
		{
			name:     "pack add success exits 0",
			setup:    func(test *testing.T, root string) { writeFile(test, root, "mypack.toml", goldenPackBody) },
			argsFunc: func(root string) []string { return []string{"pack", "add", "file://" + root + "/mypack.toml"} },
			wantExit: 0,
		},
		{
			// os.Exit(2|3) paths print nothing to stderr — the exit code is the
			// whole contract, which is exactly why these are wire-tier.
			name:     "pack add fetch failure exits 2",
			argsFunc: func(root string) []string { return []string{"pack", "add", "file://" + root + "/missing.toml"} },
			wantExit: 2,
		},
		{
			name:     "pack add invalid pack exits 3",
			setup:    func(test *testing.T, root string) { writeFile(test, root, "bad.toml", "not valid toml = =\n") },
			argsFunc: func(root string) []string { return []string{"pack", "add", "file://" + root + "/bad.toml"} },
			wantExit: 3,
		},
		{
			name:               "node get missing id exits 1",
			args:               []string{"node", "get", "notes/ghost"},
			wantExit:           1,
			wantStderrContains: "node not found",
		},
		{
			name:     "doctor always exits 0",
			args:     []string{"doctor"},
			wantExit: 0,
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			root := wireWorkspace(test)

			if testCase.setup != nil {
				testCase.setup(test, root)
			}

			args := testCase.args

			if testCase.argsFunc != nil {
				args = testCase.argsFunc(root)
			}

			_, stderr, exit := runWire(test, root, args...)

			if exit != testCase.wantExit {
				test.Errorf("exit = %d, want %d\nstderr:\n%s", exit, testCase.wantExit, stderr)
			}

			if testCase.wantStderrContains != "" && !strings.Contains(stderr, testCase.wantStderrContains) {
				test.Errorf("stderr %q does not contain %q", stderr, testCase.wantStderrContains)
			}
		})
	}
}

// TestGoldenWire_Docgen pins that docgen writes man pages and the markdown
// reference (exit 0, files present).
func TestGoldenWire_Docgen(test *testing.T) {
	if testing.Short() {
		test.Skip("wire tier spawns the built binary; skipped under -short")
	}

	root := wireWorkspace(test)
	manDir := filepath.Join(root, "man")
	docDir := filepath.Join(root, "docs")

	_, stderr, exit := runWire(test, root, "docgen", manDir, docDir)

	if exit != 0 {
		test.Fatalf("docgen exit %d: %s", exit, stderr)
	}

	if entries, readErr := os.ReadDir(manDir); readErr != nil || len(entries) == 0 {
		test.Errorf("man dir empty or unreadable: %v", readErr)
	}

	if _, statErr := os.Stat(filepath.Join(docDir, "README.md")); statErr != nil {
		test.Errorf("docs/README.md not generated: %v", statErr)
	}
}
