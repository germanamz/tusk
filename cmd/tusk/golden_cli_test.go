package main

import (
	"os"
	"path/filepath"
	"testing"
)

// goldenCLICase is one row of the CLI golden table. wantStdout is an inline
// literal compared byte-for-byte (after scrubbing) against the command's
// stdout. There is deliberately no -update flag (spec §3.1): a regression must
// never be auto-blessed, so expected output is edited by hand from the diff.
type goldenCLICase struct {
	name       string
	manifest   string // tusk.toml body; "" uses the default `init` manifest
	setup      func(test *testing.T, root string)
	args       []string
	wantStdout string
	wantErr    bool // command is expected to exit non-zero
}

// TestGoldenCLI pins the byte-stable stdout of CLI flows through the in-process
// fast tier (newRootCmd + buffer capture via runCLISplit), the tier the
// Initiative 2 refactors validate against on every commit.
func TestGoldenCLI(test *testing.T) {
	cases := []goldenCLICase{
		{
			// The highest-risk trap on the CLI side: `node get` with no shape
			// flags must echo the markdown file verbatim, byte-for-byte. The
			// fixture is written directly (not via `node create`) so the test
			// owns the exact on-disk bytes the passthrough is asserted against.
			name: "node get raw passthrough echoes the file verbatim",
			setup: func(test *testing.T, root string) {
				writeGoldenNode(test, root, "notes/hello.md", goldenHelloFile)
			},
			args:       []string{"node", "get", "notes/hello"},
			wantStdout: goldenHelloFile,
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			root := goldenWorkspace(test, testCase.manifest)

			if testCase.setup != nil {
				testCase.setup(test, root)
			}

			stdout, stderr, ok := runCLISplit(root, testCase.args...)

			if ok == testCase.wantErr {
				test.Fatalf("exit ok = %v, wantErr = %v\nstderr:\n%s", ok, testCase.wantErr, stderr.String())
			}

			got := scrub(stdout.String(), root)
			want := scrub(testCase.wantStdout, root)

			if diff := goldenDiff(want, got); diff != "" {
				test.Errorf("%s", diff)
			}
		})
	}
}

// goldenHelloFile is the exact on-disk content of the node-get fixture. Because
// `node get` (no flags) reads the file and prints it verbatim, this literal is
// simultaneously the fixture and the expected stdout.
const goldenHelloFile = "---\ntype: note\ntitle: Hello\n---\n\n# Hello\n\nFirst body line.\n"

// writeGoldenNode writes content to root/relPath verbatim, then reindexes so the
// node row exists for read commands. Writing the file directly (rather than via
// `node create`) lets a golden case own the exact bytes it asserts on.
func writeGoldenNode(test *testing.T, root, relPath, content string) {
	test.Helper()

	fullPath := filepath.Join(root, relPath)

	if mkErr := os.MkdirAll(filepath.Dir(fullPath), 0o755); mkErr != nil {
		test.Fatalf("mkdir for %s: %v", relPath, mkErr)
	}

	if writeErr := os.WriteFile(fullPath, []byte(content), 0o644); writeErr != nil {
		test.Fatalf("write %s: %v", relPath, writeErr)
	}

	_, stderr, ok := runCLISplit(root, "reindex")

	if !ok {
		test.Fatalf("reindex after writing %s: %s", relPath, stderr.String())
	}
}
