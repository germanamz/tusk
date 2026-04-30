// tests/e2e/main_test.go
package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// binPath is the path to the compiled tusk binary, set in TestMain.
var binPath string

func TestMain(runner *testing.M) {
	// Build the tusk binary into a temp directory.
	tmpDir, createErr := os.MkdirTemp("", "tusk-e2e-bin-*")

	if createErr != nil {
		panic("creating temp dir for binary: " + createErr.Error())
	}

	defer os.RemoveAll(tmpDir)

	binPath = filepath.Join(tmpDir, "tusk")
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/tusk")
	// Build from the project root. The test runs from tests/e2e/, so go up two levels.
	cmd.Dir = filepath.Join(mustGetwd(), "..", "..")
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if buildErr := cmd.Run(); buildErr != nil {
		panic("building tusk binary: " + buildErr.Error())
	}

	os.Exit(runner.Run())
}

func mustGetwd() string {
	wd, err := os.Getwd()

	if err != nil {
		panic("getwd: " + err.Error())
	}

	return wd
}
