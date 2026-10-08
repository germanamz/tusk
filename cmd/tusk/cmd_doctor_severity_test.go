package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/doctor"
)

// writeDoctorWorkspace writes a manifest declaring `note` plus a wikilink
// `references` edge type, then each file, and reindexes.
func writeDoctorWorkspace(test *testing.T, files map[string]string) string {
	test.Helper()

	root := test.TempDir()

	manifestBody := `[workspace]
name = "doctor-severity"

[node-types.note]
properties = []

[edge-types.references]
from        = ["*"]
to          = ["*"]
cardinality = "many-to-many"
wikilinks   = true
`

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(manifestBody), 0o644); writeErr != nil {
		test.Fatalf("write manifest: %v", writeErr)
	}

	for relPath, content := range files {
		abs := filepath.Join(root, relPath)

		if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
			test.Fatalf("mkdir: %v", mkErr)
		}

		if writeErr := os.WriteFile(abs, []byte(content), 0o644); writeErr != nil {
			test.Fatalf("write %s: %v", relPath, writeErr)
		}
	}

	if _, stderr, ok := runCLISplit(root, "reindex"); !ok {
		test.Fatalf("reindex failed: %s", stderr)
	}

	return root
}

// TestDoctor_IssueRepro is the #759 repro minus the embedder: the dangling
// link reads as one error located at the file, the broken file is reported
// with its decode error, and the exit is non-zero.
func TestDoctor_IssueRepro(test *testing.T) {
	root := writeDoctorWorkspace(test, map[string]string{
		"docs/dangling.md": "---\ntype: note\n---\n\n# Dangling\n\nSee [[docs/nowhere]].\n",
		"docs/broken.md":   "---\ntype: [unclosed\n---\n\n# Broken\n",
	})

	stdout, stderr, ok := runCLISplit(root, "doctor")

	if ok {
		test.Errorf("exit 0, want non-zero with errors present")
	}

	out := stdout.String()

	if !strings.HasPrefix(out, "doctor: 2 errors, 0 warnings, 0 advice\n") {
		test.Errorf("stdout should open with the summary line; got %q", out)
	}

	if got := strings.Count(out, "[dangling-edge]"); got != 1 {
		test.Errorf("dangling-edge lines = %d, want 1; got %q", got, out)
	}

	if !strings.Contains(out, `  error    [dangling-edge] docs/dangling: edge "references" -> "docs/nowhere" (target missing); also in #S1, #S1P1`) {
		test.Errorf("missing the grouped dangling line; got %q", out)
	}

	if !strings.Contains(out, "  error    [skipped-file] docs/broken.md: node: decode frontmatter docs/broken.md:") {
		test.Errorf("missing the skipped-file line; got %q", out)
	}

	if !strings.Contains(stderr.String(), "doctor: 2 errors (--fail-on=error)") {
		test.Errorf("stderr = %q, want the fail-on summary", stderr.String())
	}
}

func TestDoctor_FailOnNeverExitsZeroWithErrors(test *testing.T) {
	root := writeDoctorWorkspace(test, map[string]string{
		"docs/broken.md": "---\ntype: [unclosed\n---\n\n# Broken\n",
	})

	stdout, stderr, ok := runCLISplit(root, "doctor", "--fail-on=never")

	if !ok {
		test.Errorf("exit non-zero with --fail-on=never; stderr %q", stderr.String())
	}

	if !strings.Contains(stdout.String(), "[skipped-file]") {
		test.Errorf("--fail-on=never must still print the report; got %q", stdout.String())
	}
}

// TestDoctor_FailOnWarning pins that a warning alone passes the default check
// but fails --fail-on=warning.
func TestDoctor_FailOnWarning(test *testing.T) {
	root := writeDoctorWorkspace(test, map[string]string{
		"docs/typo.md": "---\ntype: notee\n---\n\n# Typo\n",
	})

	stdout, stderr, ok := runCLISplit(root, "doctor")

	if !ok {
		test.Errorf("default exit non-zero with only a warning; stderr %q", stderr.String())
	}

	if !strings.Contains(stdout.String(), "  warning  [undeclared-type] type \"notee\"") ||
		!strings.Contains(stdout.String(), "; in docs/typo") {
		test.Errorf("missing the undeclared-type warning; got %q", stdout.String())
	}

	_, stderr, ok = runCLISplit(root, "doctor", "--fail-on=warning")

	if ok {
		test.Errorf("exit 0 with --fail-on=warning and a warning present")
	}

	if !strings.Contains(stderr.String(), "doctor: 0 errors, 1 warning (--fail-on=warning)") {
		test.Errorf("stderr = %q, want the warning fail-on summary", stderr.String())
	}
}

func TestDoctor_InvalidFailOnIsUsageError(test *testing.T) {
	root := writeDoctorWorkspace(test, nil)

	stdout, stderr, ok := runCLISplit(root, "doctor", "--fail-on=sometimes")

	if ok {
		test.Fatalf("exit 0 with an invalid --fail-on value")
	}

	if !strings.Contains(stderr.String(), `invalid --fail-on "sometimes"`) {
		test.Errorf("stderr = %q, want the usage error", stderr.String())
	}

	if stdout.Len() != 0 {
		test.Errorf("doctor ran despite the usage error; stdout %q", stdout.String())
	}
}

func TestDescribeDoctorIssue(test *testing.T) {
	for name, testCase := range map[string]struct {
		issue doctor.Issue
		want  string
	}{
		"plain": {
			issue: doctor.Issue{NodeID: "docs/a", Message: "bad"},
			want:  "docs/a: bad",
		},
		"no node id": {
			issue: doctor.Issue{Message: "stored embeddings drifted"},
			want:  "stored embeddings drifted",
		},
		"sub-unit locations shorten to addresses": {
			issue: doctor.Issue{NodeID: "docs/a", Message: "bad", Locations: []string{"docs/a#S1", "docs/a#S1P1"}},
			want:  "docs/a: bad; also in #S1, #S1P1",
		},
		"locations without a node id": {
			issue: doctor.Issue{Message: "type \"x\" undeclared", Locations: []string{"a", "b"}},
			want:  "type \"x\" undeclared; in a, b",
		},
		"long location lists are capped": {
			issue: doctor.Issue{Message: "m", Locations: []string{"a", "b", "c", "d", "e", "f", "g"}},
			want:  "m; in a, b, c, d, e, and 2 more",
		},
	} {
		if got := describeDoctorIssue(testCase.issue); got != testCase.want {
			test.Errorf("%s: got %q, want %q", name, got, testCase.want)
		}
	}
}
