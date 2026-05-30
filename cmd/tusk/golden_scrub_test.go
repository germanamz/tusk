package main

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// The golden suite (Initiative 1 of the codebase-simplification effort) pins the
// observable CLI surface byte-for-byte so the Initiative 2 refactors can be
// validated against a frozen behavioral baseline. This file holds the shared
// harness: the determinism scrubber, the workspace builder, and a
// dependency-free diff used to report golden mismatches.

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
