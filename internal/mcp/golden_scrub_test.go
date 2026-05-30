package mcp_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/mcp"
)

// This file is the MCP-side counterpart of cmd/tusk/golden_scrub_test.go. The
// golden suite pins the MCP wire surface byte-for-byte so Initiative 2 refactors
// validate against a frozen baseline. It holds the shared harness: the
// determinism scrubber, the runtime builder, and a dependency-free diff.

var (
	reReindexTimestamp = regexp.MustCompile(`last reindex \(unix ns\): \d+`)
	reLastReindexAt    = regexp.MustCompile(`"last_reindex_at":\s*"?[^",}]*"?`)
	rePackAddDate      = regexp.MustCompile(`on \d{4}-\d{2}-\d{2}`)
	reMillis           = regexp.MustCompile(`\b\d+ms\b`)
)

// scrub neutralizes non-deterministic substrings in tool output so a golden
// literal stays stable across runs. See the determinism plan (spec §3.3).
// Passing wsRoot == "" skips the workspace-path rewrite.
func scrub(text, wsRoot string) string {
	if wsRoot != "" {
		text = strings.ReplaceAll(text, wsRoot, "<WS>")
	}

	text = reReindexTimestamp.ReplaceAllString(text, "last reindex (unix ns): <TS>")
	text = reLastReindexAt.ReplaceAllString(text, `"last_reindex_at":"<TS>"`)
	text = rePackAddDate.ReplaceAllString(text, "on <DATE>")
	text = reMillis.ReplaceAllString(text, "<MS>")

	return text
}

// goldenRuntime builds a deterministic MCP runtime for a golden case. It pins
// the embed-worker count and lease TTL via env knobs, writes manifestBody to
// tusk.toml (an empty body uses a minimal default), and opens the runtime. The
// runtime is closed automatically on test cleanup.
func goldenRuntime(test *testing.T, manifestBody string) *mcp.Runtime {
	test.Helper()

	test.Setenv("TUSK_EMBED_WORKERS", "1")
	test.Setenv("TUSK_LEASE_TTL_SECONDS", "3600")

	root := test.TempDir()

	if manifestBody == "" {
		manifestBody = "[workspace]\nname = \"test\"\n"
	}

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(manifestBody), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	rt, openErr := mcp.Open(root)

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	test.Cleanup(func() { _ = rt.Close() })

	return rt
}

// goldenDiff returns a human-readable description of the first per-line
// differences between want and got, or "" when they are byte-identical. It is a
// dependency-free stand-in for cmp.Diff.
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
