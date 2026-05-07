package typepacks_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/typepacks"
)

func TestValidate_HappyPath(test *testing.T) {
	body, _ := os.ReadFile(filepath.Join("testdata", "sample.toml"))

	loaded, validateErr := typepacks.Validate(body)

	if validateErr != nil {
		test.Fatalf("Validate: %v", validateErr)
	}

	if _, ok := loaded.NodeTypes["task"]; !ok {
		test.Errorf("missing task type")
	}

	if _, ok := loaded.EdgeTypes["parent"]; !ok {
		test.Errorf("missing parent edge")
	}
}

func TestValidate_RejectsDisallowedSection(test *testing.T) {
	body, _ := os.ReadFile(filepath.Join("testdata", "invalid-section.toml"))

	_, validateErr := typepacks.Validate(body)

	if validateErr == nil {
		test.Fatal("expected validate error")
	}

	if !strings.Contains(validateErr.Error(), "workspace") {
		test.Errorf("err = %v", validateErr)
	}
}

func TestValidate_RejectsBadTOML(test *testing.T) {
	body, _ := os.ReadFile(filepath.Join("testdata", "bad-toml.toml"))

	_, validateErr := typepacks.Validate(body)

	if validateErr == nil {
		test.Fatal("expected validate error")
	}

	if !strings.Contains(validateErr.Error(), "TOML") && !strings.Contains(validateErr.Error(), "decode") {
		test.Errorf("err = %v", validateErr)
	}
}

func TestValidate_RejectsRefToMissingType(test *testing.T) {
	body := []byte(`
[node-types.ticket]
properties = [
    { name = "assignee", type = "ref", to = "person" },
]
`)

	_, validateErr := typepacks.Validate(body)

	if validateErr == nil || !strings.Contains(validateErr.Error(), "person") {
		test.Errorf("expected ref-to-missing-type error: %v", validateErr)
	}
}
