package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

// TestHarness_IsolatesFromAncestorTuskToml verifies that the default
// Env.Run path (no InDir override) keeps cmd.Dir out of any ancestor
// chain that contains a tusk.toml. The threat is a harness regression
// that would inherit the test process's CWD — walk-up from there
// would hit /workspaces/tusk/tusk.toml (committed in this PR) and
// every test in the suite would inherit the workspace taxonomy.
//
// This test cannot run with test.Parallel() — it mutates the test
// process's CWD. Go's test runner runs sequential tests before any
// parallel cohort, so the test.Cleanup-restored CWD is in place by the
// time other tests run.
func TestHarness_IsolatesFromAncestorTuskToml(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	// Setup: seedRoot/tusk.toml + seedRoot/child/.
	seedRoot := test.TempDir()
	seedTOML := filepath.Join(seedRoot, "tusk.toml")
	seedContent := []byte(`[taxonomy]
levels = [["milestone"], ["initiative"], ["story"], ["task", "spike"]]
`)
	if err := os.WriteFile(seedTOML, seedContent, 0o644); err != nil {
		test.Fatalf("writing seed tusk.toml: %v", err)
	}
	childDir := filepath.Join(seedRoot, "child")
	if err := os.MkdirAll(childDir, 0o755); err != nil {
		test.Fatalf("mkdir child: %v", err)
	}

	// Part 1 — sanity. With InDir(childDir), walk-up resolves
	// seedRoot/tusk.toml. A level-less create against the default
	// project must be rejected, proving the seed is operative.
	sanity := newEnv(test, binPath, "flag", "text")
	sanity.InDir(childDir)
	sanityResult := sanity.Run("task", "create", "should-fail")
	if sanityResult.Err == nil {
		test.Fatalf("sanity check failed: walk-up did not reach seed tusk.toml from %s. stdout: %s",
			childDir, sanityResult.Stdout)
	}

	// Part 2 — isolation. Mutate test-process CWD into childDir so
	// that any regression that inherits CWD would walk up into
	// seedRoot. The working harness defaults workDir = test.TempDir(),
	// so cmd.Dir lives in a separate ancestor chain regardless.
	saved, cwdErr := os.Getwd()

	if cwdErr != nil {
		test.Fatalf("os.Getwd: %v", cwdErr)
	}

	if err := os.Chdir(childDir); err != nil {
		test.Fatalf("chdir to %s: %v", childDir, err)
	}
	test.Cleanup(func() {
		if err := os.Chdir(saved); err != nil {
			test.Logf("warning: failed to restore CWD to %s: %v", saved, err)
		}
	})

	isolated := newEnv(test, binPath, "flag", "text")
	isolatedResult := isolated.Run("task", "create", "should-succeed")
	if isolatedResult.Err != nil {
		test.Fatalf("isolation broken: harness leaked walk-up via test-process CWD.\nstderr: %s\nstdout: %s",
			isolatedResult.Stderr, isolatedResult.Stdout)
	}
}
