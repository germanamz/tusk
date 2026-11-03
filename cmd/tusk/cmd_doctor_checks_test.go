package main

import (
	"strings"
	"testing"
)

// checksManifest declares one project check that fails with a finding in the
// `path:line: message` shape.
const checksManifest = `[workspace]
name = "checks"

[node-types.note]
description = "n"

[check.lint]
command = "echo 'notes/a.md:3: owner missing'; exit 1"
`

// Project checks run only under --checks (#852): plain `tusk doctor` stays a
// read-only check, says what it skipped, and exits 0.
func TestDoctor_RunsProjectChecksOnlyWithTheFlag(test *testing.T) {
	root := writeRulesWorkspace(test, checksManifest, map[string]string{
		"notes/a.md": "---\ntype: note\n---\n\n# A\n",
	})

	stdout, stderr, ok := runCLISplit(root, "doctor")

	if !ok {
		test.Fatalf("doctor without --checks failed: %s\n%s", stderr, stdout)
	}

	if strings.Contains(stdout.String(), "[check:lint]") {
		test.Fatalf("doctor ran a check without --checks:\n%s", stdout)
	}

	if !strings.Contains(stdout.String(), "project checks: 1 not run (pass --checks to run them)") {
		test.Errorf("doctor did not say it skipped the check:\n%s", stdout)
	}

	stdout, _, ok = runCLISplit(root, "doctor", "--checks")

	if ok {
		test.Fatalf("doctor --checks exited 0 with a failing error-level check:\n%s", stdout)
	}

	if !strings.Contains(stdout.String(), "error    [check:lint] notes/a.md:3: owner missing") {
		test.Errorf("doctor --checks output missing the finding:\n%s", stdout)
	}

	if strings.Contains(stdout.String(), "project checks:") {
		test.Errorf("doctor --checks still says checks were skipped:\n%s", stdout)
	}
}

// --checks runs shell commands from tusk.toml, so it is CLI-only: an alias
// can't set it, since `tusk run` and MCP `tusk_run` would then run them.
func TestDoctor_ChecksFlagIsNotAnAliasArg(test *testing.T) {
	root := writeRulesWorkspace(test, checksManifest+`
[alias.full-doctor]
command = "doctor"
args    = { checks = true }
`, map[string]string{"notes/a.md": "---\ntype: note\n---\n\n# A\n"})

	stdout, _, _ := runCLISplit(root, "doctor")

	if !strings.Contains(stdout.String(), "[alias-invalid] full-doctor") || !strings.Contains(stdout.String(), `"checks"`) {
		test.Fatalf("doctor did not reject the checks alias arg:\n%s", stdout)
	}
}

// Reload lists the keys tusk does not read with the other manifest warnings.
func TestReload_ListsUnknownManifestKeys(test *testing.T) {
	root := writeRulesWorkspace(test, checksManifest+`
[mystery.lint]
command = "python3 lint.py"
`, nil)

	stdout, stderr, ok := runCLISplit(root, "reload")

	if !ok {
		test.Fatalf("reload failed: %s", stderr)
	}

	if !strings.Contains(stdout.String(), "tusk.toml: mystery: tusk does not read this table") {
		test.Errorf("reload warnings missing the unknown table:\n%s", stdout)
	}
}
