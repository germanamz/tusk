package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

const rulesToolManifest = `[workspace]
name = "rules"

[node-types.note]
properties = [
  { name = "domain", type = "enum", values = ["product", "engineering"] },
]

[rule.domain-matches-directory]
description = "a product note lives under docs/product/"
filter      = "type=note AND domain=product AND NOT path=docs/product/**"
`

// rulesToolServer writes the rules manifest and two notes, one breaking the
// rule, and returns a server over the indexed workspace.
func rulesToolServer(test *testing.T) (*Server, string) {
	test.Helper()

	root := setupServerWorkspace(test)
	rewriteManifest(test, root, rulesToolManifest)

	for relPath, content := range map[string]string{
		"docs/product/recording.md": "---\ntype: note\ndomain: product\n---\n\n# Recording\n",
		"docs/eng/pricing.md":       "---\ntype: note\ndomain: product\n---\n\n# Pricing\n",
	} {
		abs := filepath.Join(root, relPath)

		if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
			test.Fatalf("mkdir: %v", mkErr)
		}

		if writeErr := os.WriteFile(abs, []byte(content), 0o644); writeErr != nil {
			test.Fatalf("write %s: %v", relPath, writeErr)
		}
	}

	return newServerForRoot(test, root), root
}

func TestDoctorTool_ReportsRuleViolations(test *testing.T) {
	srv, _ := rulesToolServer(test)

	result, callErr := srv.HandleToolCall(context.Background(), mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{Name: "tusk_doctor", Arguments: map[string]any{}},
	})

	if callErr != nil {
		test.Fatalf("tusk_doctor: %v", callErr)
	}

	var response struct {
		ErrorCount int `json:"error_count"`
		Issues     []struct {
			Kind     string `json:"kind"`
			Severity string `json:"severity"`
			NodeID   string `json:"node_id"`
			Message  string `json:"message"`
		} `json:"issues"`
	}

	if parseErr := json.Unmarshal([]byte(textOf(result)), &response); parseErr != nil {
		test.Fatalf("parse response: %v (%s)", parseErr, textOf(result))
	}

	var ruleIssues []string

	for _, issue := range response.Issues {
		if strings.HasPrefix(issue.Kind, "rule") {
			ruleIssues = append(ruleIssues, issue.Severity+" "+issue.Kind+" "+issue.NodeID+": "+issue.Message)
		}
	}

	want := "error rule:domain-matches-directory docs/eng/pricing: a product note lives under docs/product/"

	if len(ruleIssues) != 1 || ruleIssues[0] != want {
		test.Fatalf("rule issues = %v, want [%s]", ruleIssues, want)
	}

	if response.ErrorCount == 0 {
		test.Errorf("error_count = 0, want the rule violation counted")
	}
}

func TestReloadTool_ListsInvalidRulesAsWarnings(test *testing.T) {
	srv, root := rulesToolServer(test)

	rewriteManifest(test, root, strings.Replace(rulesToolManifest, "type=note AND", "type=notee AND", 1))

	result, callErr := reloadToolHandler(context.Background(), mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{Name: "tusk_reload", Arguments: map[string]any{}},
	}, srv)

	if callErr != nil {
		test.Fatalf("reload tool: %v", callErr)
	}

	var response struct {
		ValidationErrors []string `json:"validation_errors"`
		Warnings         []string `json:"warnings"`
	}

	if parseErr := json.Unmarshal([]byte(textOf(result)), &response); parseErr != nil {
		test.Fatalf("parse response: %v (%s)", parseErr, textOf(result))
	}

	if len(response.ValidationErrors) > 0 {
		test.Fatalf("validation_errors = %v, want an invalid rule to be non-blocking", response.ValidationErrors)
	}

	want := `rule "domain-matches-directory": filter: node type "notee" not declared in manifest at column 1 (did you mean "note"?)`

	found := false

	for _, warning := range response.Warnings {
		if warning == want {
			found = true
		}
	}

	if !found {
		test.Fatalf("warnings = %v, want %q", response.Warnings, want)
	}
}
