package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestContext_NoContextBlock_PrintsHint(test *testing.T) {
	root := setupTempWorkspace(test)

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("context")

	if runErr != nil {
		test.Fatalf("CLI: %v\n%s", runErr, out)
	}

	if !strings.Contains(out, "no [context] block declared") {
		test.Errorf("stdout missing 'no [context] block declared':\n%s", out)
	}
}

func TestContext_PinnedAndInclude_Compact(test *testing.T) {
	root := setupTempWorkspace(test)

	createNode(test, root, "notes/alpha.md", "note", "Alpha", "")

	appendAliasBlock(test, root, `[alias.snap]
command = "status"

[context]
pinned  = ["notes/alpha"]
include = ["snap"]
`)

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("context", "--format", "compact")

	if runErr != nil {
		test.Fatalf("CLI: %v\n%s", runErr, out)
	}

	if !strings.Contains(out, "# Pinned") {
		test.Errorf("compact output missing '# Pinned' header:\n%s", out)
	}

	if !strings.Contains(out, "notes/alpha") {
		test.Errorf("compact output missing 'notes/alpha':\n%s", out)
	}

	if !strings.Contains(out, "# Aliases / snap") {
		test.Errorf("compact output missing alias section header:\n%s", out)
	}
}

func TestContext_JSONEnvelope(test *testing.T) {
	root := setupTempWorkspace(test)

	createNode(test, root, "notes/alpha.md", "note", "Alpha", "")

	appendAliasBlock(test, root, `[alias.snap]
command = "status"

[context]
pinned  = ["notes/alpha"]
include = ["snap"]
`)

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("context", "--json")

	if runErr != nil {
		test.Fatalf("CLI: %v\n%s", runErr, out)
	}

	var envelope map[string]any

	if unmarshalErr := json.Unmarshal([]byte(out), &envelope); unmarshalErr != nil {
		test.Fatalf("Unmarshal: %v\n%s", unmarshalErr, out)
	}

	pinned, ok := envelope["pinned"].([]any)

	if !ok || len(pinned) != 1 {
		test.Fatalf("pinned shape unexpected: %v", envelope["pinned"])
	}

	aliasEnv, ok := envelope["aliases"].(map[string]any)

	if !ok {
		test.Fatalf("aliases shape unexpected: %v", envelope["aliases"])
	}

	if _, found := aliasEnv["snap"]; !found {
		test.Errorf("aliases.snap missing: %v", aliasEnv)
	}
}

func TestContext_InlineRecent_Compact(test *testing.T) {
	root := setupTempWorkspace(test)

	createNode(test, root, "notes/alpha.md", "note", "Alpha", "")

	appendAliasBlock(test, root, `[context]

[context.recent]
command = "node list"
args.filter = "type=note"
`)

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("context", "--format", "compact")

	if runErr != nil {
		test.Fatalf("CLI: %v\n%s", runErr, out)
	}

	if !strings.Contains(out, "# Recent") {
		test.Errorf("compact output missing '# Recent' header:\n%s", out)
	}

	if !strings.Contains(out, "notes/alpha") {
		test.Errorf("compact output missing 'notes/alpha':\n%s", out)
	}
}

func TestContext_ReferenceRecent_Compact(test *testing.T) {
	root := setupTempWorkspace(test)

	createNode(test, root, "notes/alpha.md", "note", "Alpha", "")

	appendAliasBlock(test, root, `[alias.recent-notes]
command = "node list"
args.filter = "type=note"

[context]
recent = "recent-notes"
`)

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("context", "--format", "compact")

	if runErr != nil {
		test.Fatalf("CLI: %v\n%s", runErr, out)
	}

	if !strings.Contains(out, "# Recent") {
		test.Errorf("compact output missing '# Recent' header:\n%s", out)
	}

	if !strings.Contains(out, "notes/alpha") {
		test.Errorf("compact output missing 'notes/alpha':\n%s", out)
	}
}

func TestContext_BothRecentForms_DoctorReports(test *testing.T) {
	root := setupTempWorkspace(test)

	appendAliasBlock(test, root, `[alias.recent-notes]
command = "node list"

[context]
recent = "recent-notes"

[context.recent]
command = "node list"
`)

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("doctor")

	if runErr != nil {
		test.Fatalf("CLI: %v\n%s", runErr, out)
	}

	if !strings.Contains(out, "context:") {
		test.Errorf("doctor stdout missing 'context:' header:\n%s", out)
	}

	if !strings.Contains(out, "both recent") {
		test.Errorf("doctor stdout missing 'both recent' message:\n%s", out)
	}
}

func TestContext_MissingPinnedID_DoctorReports(test *testing.T) {
	root := setupTempWorkspace(test)

	appendAliasBlock(test, root, `[context]
pinned = ["notes/ghost"]
`)

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("doctor")

	if runErr != nil {
		test.Fatalf("CLI: %v\n%s", runErr, out)
	}

	if !strings.Contains(out, "missing pinned") {
		test.Errorf("doctor stdout missing 'missing pinned' line:\n%s", out)
	}

	if !strings.Contains(out, "notes/ghost") {
		test.Errorf("doctor stdout missing 'notes/ghost':\n%s", out)
	}
}

func TestContext_MissingPinnedID_OmittedFromDigest(test *testing.T) {
	root := setupTempWorkspace(test)

	createNode(test, root, "notes/alpha.md", "note", "Alpha", "")

	appendAliasBlock(test, root, `[context]
pinned = ["notes/alpha", "notes/ghost"]
`)

	chdir(test, root)
	defer chdir(test, "")

	out, runErr := runCLI("context", "--json")

	if runErr != nil {
		test.Fatalf("CLI: %v\n%s", runErr, out)
	}

	var envelope map[string]any

	if unmarshalErr := json.Unmarshal([]byte(out), &envelope); unmarshalErr != nil {
		test.Fatalf("Unmarshal: %v\n%s", unmarshalErr, out)
	}

	pinned, _ := envelope["pinned"].([]any)

	if len(pinned) != 1 {
		test.Errorf("pinned len = %d, want 1 (ghost ID should be omitted): %v", len(pinned), pinned)
	}

	missing, _ := envelope["missing_pinned"].([]any)

	if len(missing) != 1 {
		test.Errorf("missing_pinned len = %d, want 1: %v", len(missing), missing)
	}
}
