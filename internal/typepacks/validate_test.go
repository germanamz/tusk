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

func TestValidate_AcceptsRuleSection(test *testing.T) {
	body := []byte(`
[node-types.task]
properties = [{ name = "owner", type = "string" }]

[rule.task-has-an-owner]
description = "every task names its owner"
filter      = "type=task AND owner!=*"
severity    = "warning"
`)

	loaded, validateErr := typepacks.Validate(body)

	if validateErr != nil {
		test.Fatalf("Validate: %v", validateErr)
	}

	rule, declared := loaded.Rules["task-has-an-owner"]

	if !declared {
		test.Fatalf("Rules = %v, want task-has-an-owner", loaded.Rules)
	}

	if rule.Filter != "type=task AND owner!=*" || rule.Severity != "warning" {
		test.Errorf("rule = %+v", rule)
	}
}

// A check is shell text, so a pack must never be able to add one: a pack
// fetched from a URL would otherwise plant a command for `doctor --checks`.
func TestValidate_RejectsCheckSection(test *testing.T) {
	body := []byte(`
[check.lint]
command = "true"
`)

	_, validateErr := typepacks.Validate(body)

	if validateErr == nil || !strings.Contains(validateErr.Error(), `"check"`) {
		test.Errorf("expected disallowed-section error naming check, got %v", validateErr)
	}
}

func TestValidate_RejectsRuleThatDoesNotDecode(test *testing.T) {
	cases := map[string]string{
		"wrong value type": "[rule.broken]\nfilter = 3\n",
		"unknown key":      "[rule.broken]\nfliter = \"type=task\"\n",
		"not a table":      "[rule]\nbroken = \"type=task\"\n",
	}

	for name, body := range cases {
		test.Run(name, func(test *testing.T) {
			_, validateErr := typepacks.Validate([]byte(body))

			if validateErr == nil {
				test.Fatal("expected validate error")
			}

			if !strings.Contains(validateErr.Error(), `rule "broken"`) {
				test.Errorf("err = %v, want it to name rule \"broken\"", validateErr)
			}

			// cmd/tusk maps this substring to exit code 3 (validation failure).
			if !strings.Contains(validateErr.Error(), "decode pack") {
				test.Errorf("err = %v, want a decode pack error", validateErr)
			}
		})
	}
}
