package mcp_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/internal/manifest"
	"github.com/germanamz/tusk/internal/mcp"
)

// bootRuntimeWithAlias is like bootRuntime but writes a tusk.toml that
// declares a single [alias.<name>] block and wires an introspector covering
// the read verbs the alias targets.
func bootRuntimeWithAlias(test *testing.T, aliasBlock string) *mcp.Runtime {
	test.Helper()

	root := test.TempDir()
	body := "[workspace]\nname = \"x\"\n\n" + aliasBlock

	if writeErr := os.WriteFile(filepath.Join(root, "tusk.toml"), []byte(body), 0o644); writeErr != nil {
		test.Fatalf("write: %v", writeErr)
	}

	introspect := manifest.VerbIntrospector(func(verb string) ([]manifest.FlagSpec, bool) {
		// Permissive introspector for the test fixtures.
		return []manifest.FlagSpec{
			{Name: "filter", Kind: "string"},
			{Name: "sort", Kind: "string"},
			{Name: "take", Kind: "int"},
			{Name: "skip", Kind: "int"},
			{Name: "include", Kind: "stringSlice"},
			{Name: "fields", Kind: "stringSlice"},
			{Name: "semantic", Kind: "string"},
			{Name: "from", Kind: "string"},
			{Name: "to", Kind: "string"},
			{Name: "type", Kind: "string"},
			{Name: "no-migrate", Kind: "bool"},
		}, true
	})

	rt, openErr := mcp.Open(root, mcp.WithAliasIntrospector(introspect))

	if openErr != nil {
		test.Fatalf("Open: %v", openErr)
	}

	return rt
}

func TestTool_Run_StatusAlias(test *testing.T) {
	rt := bootRuntimeWithAlias(test, `[alias.snap]
command = "status"
`)
	defer rt.Close()

	srv := mcp.NewServer(rt)
	body, callErr := callTool(test, srv, "tusk_run", map[string]any{"alias": "snap"})

	if callErr != nil {
		test.Fatalf("tusk_run: %v", callErr)
	}

	if body["alias"] != "snap" {
		test.Errorf("alias = %v, want snap", body["alias"])
	}

	if body["command"] != "status" {
		test.Errorf("command = %v, want status", body["command"])
	}

	if body["kind"] != "status" {
		test.Errorf("kind = %v, want status", body["kind"])
	}

	result, ok := body["result"].(map[string]any)

	if !ok {
		test.Fatalf("result type = %T, want map[string]any", body["result"])
	}

	if _, hasNodesByType := result["nodes_by_type"]; !hasNodesByType {
		test.Errorf("result missing nodes_by_type: %v", result)
	}
}

func TestTool_Run_UnknownAlias(test *testing.T) {
	rt := bootRuntimeWithAlias(test, "")
	defer rt.Close()

	srv := mcp.NewServer(rt)
	_, callErr := callTool(test, srv, "tusk_run", map[string]any{"alias": "nope"})

	if callErr == nil {
		test.Fatalf("expected error, got nil")
	}
}

func TestTool_Doctor_SurfacesAliasErrors(test *testing.T) {
	rt := bootRuntimeWithAlias(test, `[alias.bad]
command = "no-such-verb"
`)
	defer rt.Close()

	srv := mcp.NewServer(rt)
	body, callErr := callTool(test, srv, "tusk_doctor", map[string]any{})

	if callErr != nil {
		test.Fatalf("tusk_doctor: %v", callErr)
	}

	aliasErrors, ok := body["alias_errors"].([]any)

	if !ok {
		test.Fatalf("alias_errors missing or wrong type: %T %v", body["alias_errors"], body)
	}

	if len(aliasErrors) != 1 {
		test.Fatalf("alias_errors len = %d, want 1: %v", len(aliasErrors), aliasErrors)
	}

	first, _ := aliasErrors[0].(map[string]any)

	if first["name"] != "bad" {
		test.Errorf("alias_errors[0].name = %v, want bad", first["name"])
	}
}

// TestTool_Run_DoctorAlias_IncludesAliasErrors asserts that dispatching a
// doctor alias via tusk_run surfaces alias_errors inside the envelope's
// result block — parity with the direct tusk_doctor tool response.
func TestTool_Run_DoctorAlias_IncludesAliasErrors(test *testing.T) {
	rt := bootRuntimeWithAlias(test, `[alias.health]
command = "doctor"

[alias.bad]
command = "no-such-verb"
`)
	defer rt.Close()

	srv := mcp.NewServer(rt)
	body, callErr := callTool(test, srv, "tusk_run", map[string]any{"alias": "health"})

	if callErr != nil {
		test.Fatalf("tusk_run health: %v", callErr)
	}

	if body["kind"] != "doctor" {
		test.Fatalf("kind = %v, want doctor", body["kind"])
	}

	result, ok := body["result"].(map[string]any)

	if !ok {
		test.Fatalf("result type = %T, want map[string]any", body["result"])
	}

	aliasErrors, hasField := result["alias_errors"].([]any)

	if !hasField {
		test.Fatalf("result missing alias_errors: %v", result)
	}

	if len(aliasErrors) != 1 {
		test.Fatalf("alias_errors len = %d, want 1: %v", len(aliasErrors), aliasErrors)
	}

	first, _ := aliasErrors[0].(map[string]any)

	if first["name"] != "bad" {
		test.Errorf("alias_errors[0].name = %v, want bad", first["name"])
	}
}
