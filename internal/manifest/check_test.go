package manifest_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
)

// [check.<name>] declares a project check doctor runs under --checks (#852).
// Like rules, a bad block never fails the load: a misspelled key lands on
// UnknownKeys and a wrong-typed value on DecodeError, both for doctor's
// check-invalid, and neither shows up as a manifest unknown key.
func TestLoad_DecodesChecksLeniently(test *testing.T) {
	manifestPath := writeAliasManifest(test, `[workspace]
name = "brain"

[check.lint]
description     = "project lint"
command         = "python3 lint.py"
severity        = "warning"
timeout-seconds = 30

[check.typo]
comand = "make lint"

[check.wrong-type]
command = ["make", "lint"]
`)

	loaded, loadErr := manifest.Load(manifestPath)

	if loadErr != nil {
		test.Fatalf("Load: %v", loadErr)
	}

	lint := manifest.Check{Description: "project lint", Command: "python3 lint.py", Severity: "warning", TimeoutSeconds: 30}

	if got := loaded.Checks["lint"]; !reflect.DeepEqual(got, lint) {
		test.Errorf("lint = %#v, want %#v", got, lint)
	}

	if got := loaded.Checks["typo"].UnknownKeys; !reflect.DeepEqual(got, []string{"comand"}) {
		test.Errorf("typo UnknownKeys = %v, want [comand]", got)
	}

	if got := loaded.Checks["wrong-type"].DecodeError; !strings.Contains(got, "check.wrong-type.command") {
		test.Errorf("wrong-type DecodeError = %q, want it to name the key", got)
	}

	if len(loaded.UnknownKeys) != 0 {
		test.Errorf("UnknownKeys = %+v, want none: check keys belong to check-invalid", loaded.UnknownKeys)
	}
}
