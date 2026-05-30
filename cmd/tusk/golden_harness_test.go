package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The golden suite (Initiative 1 of the codebase-simplification effort) pins the
// observable CLI surface byte-for-byte so the Initiative 2 refactors are
// validated against a frozen baseline. This file is the shared harness — the
// determinism scrubber, the deterministic workspace builder, the table runner,
// and a dependency-free diff. The cases themselves live in per-area files
// (golden_node_test.go, golden_edge_test.go, golden_query_test.go, ...), each a
// thin TestGoldenCLI_<Area> that calls runGoldenCLICases.

var (
	reReindexTimestamp = regexp.MustCompile(`last reindex \(unix ns\): \d+`)
	reLastReindexAt    = regexp.MustCompile(`"last_reindex_at":\s*"?[^",}]*"?`)
	rePackAddDate      = regexp.MustCompile(`on \d{4}-\d{2}-\d{2}`)
	reMillis           = regexp.MustCompile(`\b\d+ms\b`)
	reVersionToken     = regexp.MustCompile(`tusk version \S+`)
)

// scrub replaces the non-deterministic substrings in command output with fixed
// placeholders so a golden literal stays stable across runs and machines.
// wsRoot (the random per-test TempDir) collapses to <WS>; the remaining rules
// neutralize the reindex timestamp, the MCP last_reindex_at field, the pack-add
// date line, slog millisecond tokens, and the build version. See the
// determinism plan (spec §3.3). Passing wsRoot == "" skips the path rewrite.
func scrub(text, wsRoot string) string {
	if wsRoot != "" {
		text = strings.ReplaceAll(text, wsRoot, "<WS>")
	}

	text = reReindexTimestamp.ReplaceAllString(text, "last reindex (unix ns): <TS>")
	text = reLastReindexAt.ReplaceAllString(text, `"last_reindex_at":"<TS>"`)
	text = rePackAddDate.ReplaceAllString(text, "on <DATE>")
	text = reMillis.ReplaceAllString(text, "<MS>")
	text = reVersionToken.ReplaceAllString(text, "tusk version <VERSION>")

	return text
}

// goldenWorkspace builds a deterministic workspace for a golden case: it pins
// the embed-worker count and lease TTL via env knobs (so output never depends
// on NumCPU or wall-clock leases) and then runs `tusk init`. An empty
// manifestBody keeps the default init manifest; a non-empty body overwrites
// tusk.toml. Returns the workspace root; the process cwd is left inside it
// (restored by initWorkspace's cleanup).
func goldenWorkspace(test *testing.T, manifestBody string) string {
	test.Helper()

	test.Setenv("TUSK_EMBED_WORKERS", "1")
	test.Setenv("TUSK_LEASE_TTL_SECONDS", "3600")

	if manifestBody == "" {
		return initWorkspace(test)
	}

	return initWorkspaceWithManifest(test, manifestBody)
}

// goldenCLICase is one row of a CLI golden table. wantStdout (and the optional
// wantStderr) are inline literals compared byte-for-byte after scrubbing. There
// is deliberately no -update flag (spec §3.1): a regression must never be
// auto-blessed, so expected output is edited by hand from the diff.
type goldenCLICase struct {
	name               string
	manifest           string // tusk.toml body; "" uses the default `init` manifest
	noInit             bool   // run in a bare (un-initialized) dir — for `tusk init` itself
	setup              func(test *testing.T, root string)
	args               []string
	argsFunc           func(root string) []string // computes args from the root (e.g. file:// paths); takes precedence over args
	wantStdout         string
	wantStderr         string // exact stderr after scrubbing; checked only when non-empty
	wantStderrContains string // substring stderr must contain after scrubbing — for messages with a library-owned tail we must not pin exactly
	wantErr            bool   // command is expected to exit non-zero
}

// runGoldenCLICases drives each case through the in-process fast tier
// (newRootCmd + buffer capture via runCLISplit) — the tier the Initiative 2
// refactors validate against on every commit — and asserts byte-stable stdout
// (and stderr, when specified) plus the exit disposition.
func runGoldenCLICases(test *testing.T, cases []goldenCLICase) {
	test.Helper()

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			root := testCase.workspace(test)

			if testCase.setup != nil {
				testCase.setup(test, root)
			}

			args := testCase.args

			if testCase.argsFunc != nil {
				args = testCase.argsFunc(root)
			}

			stdout, stderr, ok := runCLISplit(root, args...)

			if ok == testCase.wantErr {
				test.Fatalf("exit ok = %v, wantErr = %v\nstderr:\n%s", ok, testCase.wantErr, stderr.String())
			}

			if diff := goldenDiff(scrubWorkspace(testCase.wantStdout, root), scrubWorkspace(stdout.String(), root)); diff != "" {
				test.Errorf("stdout %s", diff)
			}

			if testCase.wantStderr != "" {
				if diff := goldenDiff(scrubWorkspace(testCase.wantStderr, root), scrubWorkspace(stderr.String(), root)); diff != "" {
					test.Errorf("stderr %s", diff)
				}
			}

			if testCase.wantStderrContains != "" {
				gotStderr := scrubWorkspace(stderr.String(), root)

				if want := scrubWorkspace(testCase.wantStderrContains, root); !strings.Contains(gotStderr, want) {
					test.Errorf("stderr %q does not contain %q", gotStderr, want)
				}
			}
		})
	}
}

// workspace builds the case's root: a bare temp dir when the case exercises
// `tusk init` itself, otherwise an initialized workspace.
func (testCase goldenCLICase) workspace(test *testing.T) string {
	test.Helper()

	if testCase.noInit {
		return bareWorkspace(test)
	}

	return goldenWorkspace(test, testCase.manifest)
}

// bareWorkspace returns an un-initialized temp dir with the determinism env
// knobs pinned — for cases whose command (`tusk init`) must run against a
// workspace that does not yet exist.
func bareWorkspace(test *testing.T) string {
	test.Helper()

	test.Setenv("TUSK_EMBED_WORKERS", "1")
	test.Setenv("TUSK_LEASE_TTL_SECONDS", "3600")

	return test.TempDir()
}

// scrubWorkspace runs scrub but first collapses the symlink-resolved form of
// root to <WS>. On macOS test.TempDir() lives under a /var -> /private/var
// symlink, so a command that prints os.Getwd() (e.g. `init`) emits the resolved
// path, which would not match the TempDir root that scrub knows about.
func scrubWorkspace(text, root string) string {
	if realRoot, evalErr := filepath.EvalSymlinks(root); evalErr == nil && realRoot != root {
		text = strings.ReplaceAll(text, realRoot, "<WS>")
	}

	return scrub(text, root)
}

// writeFile writes content to root/relPath verbatim WITHOUT reindexing — for
// cases whose command (e.g. reindex) is the thing under test and must observe
// the un-indexed file itself.
func writeFile(test *testing.T, root, relPath, content string) {
	test.Helper()

	fullPath := filepath.Join(root, relPath)

	if mkErr := os.MkdirAll(filepath.Dir(fullPath), 0o755); mkErr != nil {
		test.Fatalf("mkdir for %s: %v", relPath, mkErr)
	}

	if writeErr := os.WriteFile(fullPath, []byte(content), 0o644); writeErr != nil {
		test.Fatalf("write %s: %v", relPath, writeErr)
	}
}

// removeFile deletes root/relPath — for cases exercising removal/tombstone paths
// (e.g. reindex after a node is deleted on disk).
func removeFile(test *testing.T, root, relPath string) {
	test.Helper()

	if rmErr := os.Remove(filepath.Join(root, relPath)); rmErr != nil {
		test.Fatalf("remove %s: %v", relPath, rmErr)
	}
}

// reindexWorkspace runs `tusk reindex` and fails the test if it errors — for
// setups that write several fixture files and then bring the index up to date
// in one pass.
func reindexWorkspace(test *testing.T, root string) {
	test.Helper()

	_, stderr, ok := runCLISplit(root, "reindex")

	if !ok {
		test.Fatalf("reindex: %s", stderr.String())
	}
}

// writeGoldenNode writes content to root/relPath verbatim, then reindexes so the
// node row exists for read commands. Writing the file directly (rather than via
// `node create`) lets a golden case own the exact bytes it asserts on.
func writeGoldenNode(test *testing.T, root, relPath, content string) {
	test.Helper()

	writeFile(test, root, relPath, content)
	reindexWorkspace(test, root)
}

// goldenDiff returns a human-readable description of the first per-line
// differences between want and got, or "" when they are byte-identical. It is a
// dependency-free stand-in for cmp.Diff — sufficient for golden assertions and
// keeps the test tree free of a direct go-cmp dependency.
func goldenDiff(want, got string) string {
	if want == got {
		return ""
	}

	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")

	maxLines := max(len(wantLines), len(gotLines))

	var builder strings.Builder

	builder.WriteString("golden mismatch (-want +got):\n")

	for idx := range maxLines {
		wantLine := ""

		if idx < len(wantLines) {
			wantLine = wantLines[idx]
		}

		gotLine := ""

		if idx < len(gotLines) {
			gotLine = gotLines[idx]
		}

		if wantLine == gotLine {
			continue
		}

		fmt.Fprintf(&builder, "  line %d:\n    -want: %q\n    +got:  %q\n", idx+1, wantLine, gotLine)
	}

	return builder.String()
}
