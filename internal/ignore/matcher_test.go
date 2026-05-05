package ignore_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/ignore"
)

func TestMatcher_RespectsRootGitignore(test *testing.T) {
	root := test.TempDir()

	if writeErr := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("build/\n*.tmp\n"), 0o644); writeErr != nil {
		test.Fatalf("write gitignore: %v", writeErr)
	}

	matcher, newErr := ignore.NewMatcher(root, nil)

	if newErr != nil {
		test.Fatalf("NewMatcher: %v", newErr)
	}

	cases := []struct {
		path    string
		isDir   bool
		ignored bool
	}{
		{"build", true, true},
		{"foo.tmp", false, true},
		{"keep.md", false, false},
		{"build/output.bin", false, true},
	}

	for _, tc := range cases {
		got := matcher.Matches(tc.path, tc.isDir)

		if got != tc.ignored {
			test.Errorf("Matches(%q, %v) = %v, want %v", tc.path, tc.isDir, got, tc.ignored)
		}
	}
}

func TestMatcher_AlwaysIgnoresTuskAndGit(test *testing.T) {
	matcher, newErr := ignore.NewMatcher(test.TempDir(), nil)

	if newErr != nil {
		test.Fatalf("NewMatcher: %v", newErr)
	}

	for _, path := range []string{".tusk", ".tusk/index.db", ".git", ".git/HEAD"} {
		isDir := path == ".tusk" || path == ".git"

		if !matcher.Matches(path, isDir) {
			test.Errorf("Matches(%q) should be true (built-in ignore)", path)
		}
	}
}

func TestMatcher_AppliesWorkspaceIgnore(test *testing.T) {
	matcher, newErr := ignore.NewMatcher(test.TempDir(), []string{"vendor/", "*.cache"})

	if newErr != nil {
		test.Fatalf("NewMatcher: %v", newErr)
	}

	if !matcher.Matches("vendor", true) {
		test.Errorf("vendor/ should be ignored")
	}

	if !matcher.Matches("foo.cache", false) {
		test.Errorf("*.cache should be ignored")
	}

	if matcher.Matches("foo.md", false) {
		test.Errorf("foo.md should not be ignored")
	}
}

func TestMatcher_NoGitignoreNoWorkspaceIgnoreOnlyBuiltins(test *testing.T) {
	matcher, newErr := ignore.NewMatcher(test.TempDir(), nil)

	if newErr != nil {
		test.Fatalf("NewMatcher: %v", newErr)
	}

	if matcher.Matches("anything.md", false) {
		test.Errorf("anything.md should not be ignored when only built-ins active")
	}
}
