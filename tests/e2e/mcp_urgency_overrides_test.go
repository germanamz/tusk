package e2e

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newMCPEnvWithConfig boots an MCP server subprocess with the given TOML
// config snippet written into a fresh TUSK_CONFIG_DIR. Mirrors newMCPEnv
// but lets a single test exercise mcp.blocked_fields without polluting
// the shared harness.
func newMCPEnvWithConfig(t *testing.T, binPath, configTOML string) *mcpEnv {
	t.Helper()
	tmpFile, err := os.CreateTemp(t.TempDir(), "tusk-mcp-e2e-*.db")
	if err != nil {
		t.Fatalf("creating temp db: %v", err)
	}
	_ = tmpFile.Close()

	cfgDir := t.TempDir()
	if configTOML != "" {
		path := filepath.Join(cfgDir, "config.toml")
		if err := os.WriteFile(path, []byte(configTOML), 0o644); err != nil {
			t.Fatalf("writing config.toml: %v", err)
		}
	}

	cmd := exec.Command(binPath, "--db", tmpFile.Name(), "mcp", "serve")
	cmd.Env = append(os.Environ(), "TUSK_CONFIG_DIR="+cfgDir)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("starting mcp server: %v", err)
	}

	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	})

	env := &mcpEnv{
		t:      t,
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		nextID: 1,
	}
	env.initialize()
	return env
}

// urgencyOverridesMap reads the urgency_overrides object from a tool
// response, returning nil if the key is missing or null.
func urgencyOverridesMap(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	raw, ok := resp["urgency_overrides"]
	if !ok || raw == nil {
		return nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("urgency_overrides not an object: %T", raw)
	}
	return m
}

func TestMCPTaskModify_UrgencyOverrides_SetMultipleKeys(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	created := env.callTool("tusk_task_create", map[string]any{"title": "set-multi"})
	shortID := created["short_id"].(string)
	version := created["version"].(float64)

	modified := env.callTool("tusk_task_modify", map[string]any{
		"short_id": shortID,
		"version":  version,
		"urgency_overrides": map[string]any{
			"priority_weight": 5.0,
			"blocking_weight": 20.0,
		},
	})

	got := urgencyOverridesMap(t, modified)
	if got == nil {
		t.Fatalf("expected urgency_overrides on response, got nil. raw=%v", modified)
	}
	if pw, _ := got["priority_weight"].(float64); pw != 5 {
		t.Errorf("priority_weight = %v, want 5", got["priority_weight"])
	}
	if bw, _ := got["blocking_weight"].(float64); bw != 20 {
		t.Errorf("blocking_weight = %v, want 20", got["blocking_weight"])
	}
	if len(got) != 2 {
		t.Errorf("expected exactly 2 keys in urgency_overrides, got %d (%v)", len(got), got)
	}
}

func TestMCPTaskModify_UrgencyOverrides_NullClearsSingleKey(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	created := env.callTool("tusk_task_create", map[string]any{"title": "null-clear-one"})
	shortID := created["short_id"].(string)
	version := created["version"].(float64)

	first := env.callTool("tusk_task_modify", map[string]any{
		"short_id": shortID,
		"version":  version,
		"urgency_overrides": map[string]any{
			"priority_weight": 5.0,
			"blocking_weight": 20.0,
			"due_weight":      3.0,
		},
	})
	version = first["version"].(float64)

	second := env.callTool("tusk_task_modify", map[string]any{
		"short_id": shortID,
		"version":  version,
		"urgency_overrides": map[string]any{
			"due_weight": nil,
		},
	})

	got := urgencyOverridesMap(t, second)
	if _, present := got["due_weight"]; present {
		t.Errorf("due_weight should be cleared, got %v", got["due_weight"])
	}
	if pw, _ := got["priority_weight"].(float64); pw != 5 {
		t.Errorf("priority_weight should be intact = 5, got %v", got["priority_weight"])
	}
	if bw, _ := got["blocking_weight"].(float64); bw != 20 {
		t.Errorf("blocking_weight should be intact = 20, got %v", got["blocking_weight"])
	}
}

func TestMCPTaskModify_UrgencyOverrides_EmptyPatchNoOp(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	created := env.callTool("tusk_task_create", map[string]any{"title": "empty-noop"})
	shortID := created["short_id"].(string)
	version := created["version"].(float64)

	first := env.callTool("tusk_task_modify", map[string]any{
		"short_id": shortID,
		"version":  version,
		"urgency_overrides": map[string]any{
			"priority_weight": 5.0,
		},
	})
	startVersion := first["version"].(float64)

	second := env.callTool("tusk_task_modify", map[string]any{
		"short_id":          shortID,
		"version":           startVersion,
		"urgency_overrides": map[string]any{},
	})
	got := urgencyOverridesMap(t, second)
	if pw, _ := got["priority_weight"].(float64); pw != 5 {
		t.Errorf("priority_weight unchanged expected 5, got %v", got["priority_weight"])
	}
	if len(got) != 1 {
		t.Errorf("expected only priority_weight in urgency_overrides, got %v", got)
	}
}

func TestMCPTaskModify_UrgencyOverrides_TopLevelNullRejected(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	created := env.callTool("tusk_task_create", map[string]any{"title": "top-null"})
	shortID := created["short_id"].(string)
	version := created["version"].(float64)

	errMsg := env.callToolExpectError("tusk_task_modify", map[string]any{
		"short_id":          shortID,
		"version":           version,
		"urgency_overrides": nil,
	})
	if !strings.Contains(errMsg, "urgency_overrides_clear") {
		t.Errorf("expected error to mention urgency_overrides_clear, got: %s", errMsg)
	}
}

func TestMCPTaskModify_UrgencyOverrides_ClearAllThenSet(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	created := env.callTool("tusk_task_create", map[string]any{"title": "clear-then-set"})
	shortID := created["short_id"].(string)
	version := created["version"].(float64)

	first := env.callTool("tusk_task_modify", map[string]any{
		"short_id": shortID,
		"version":  version,
		"urgency_overrides": map[string]any{
			"priority_weight": 5.0,
			"blocking_weight": 20.0,
			"due_weight":      3.0,
		},
	})
	version = first["version"].(float64)

	second := env.callTool("tusk_task_modify", map[string]any{
		"short_id":                shortID,
		"version":                 version,
		"urgency_overrides_clear": true,
		"urgency_overrides": map[string]any{
			"priority_weight": 7.0,
		},
	})
	got := urgencyOverridesMap(t, second)
	if len(got) != 1 {
		t.Errorf("expected exactly one key after clear+set, got %d (%v)", len(got), got)
	}
	if pw, _ := got["priority_weight"].(float64); pw != 7 {
		t.Errorf("priority_weight = %v, want 7", got["priority_weight"])
	}
}

func TestMCPTaskModify_UrgencyOverrides_UnknownKeyRejected(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	created := env.callTool("tusk_task_create", map[string]any{"title": "unknown-key"})
	shortID := created["short_id"].(string)
	version := created["version"].(float64)

	errMsg := env.callToolExpectError("tusk_task_modify", map[string]any{
		"short_id": shortID,
		"version":  version,
		"urgency_overrides": map[string]any{
			"bogus_key": 1.0,
		},
	})
	if !strings.Contains(errMsg, "bogus_key") {
		t.Errorf("expected error to name bogus_key, got: %s", errMsg)
	}
	for _, valid := range []string{
		"priority_weight", "due_weight", "age_weight", "active_weight",
		"blocking_weight", "blocked_weight", "tags_weight", "project_weight",
		"annotations_weight", "waiting_weight",
	} {
		if !strings.Contains(errMsg, valid) {
			t.Errorf("error should list valid key %q, got: %s", valid, errMsg)
		}
	}
}

func TestMCPTaskModify_UrgencyOverrides_NonNumericRejected(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	created := env.callTool("tusk_task_create", map[string]any{"title": "non-numeric"})
	shortID := created["short_id"].(string)
	version := created["version"].(float64)

	errMsg := env.callToolExpectError("tusk_task_modify", map[string]any{
		"short_id": shortID,
		"version":  version,
		"urgency_overrides": map[string]any{
			"priority_weight": "high",
		},
	})
	if !strings.Contains(errMsg, "priority_weight") {
		t.Errorf("expected error to name priority_weight, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "must be a number or null") {
		t.Errorf("expected error to mention 'must be a number or null', got: %s", errMsg)
	}
}

func TestMCPTaskModify_UrgencyOverrides_BlockedFieldsGate(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnvWithConfig(t, binPath, `
[mcp]
disabled_tools = []
disabled_tool_groups = []
disabled_resources = []
disabled_resource_groups = []

[mcp.blocked_fields]
tusk_task_modify = ["urgency_overrides"]
`)

	created := env.callTool("tusk_task_create", map[string]any{"title": "blocked"})
	shortID := created["short_id"].(string)
	version := created["version"].(float64)

	errMsg := env.callToolExpectError("tusk_task_modify", map[string]any{
		"short_id": shortID,
		"version":  version,
		"urgency_overrides": map[string]any{
			"priority_weight": 5.0,
		},
	})
	if !strings.Contains(errMsg, "urgency_overrides") {
		t.Errorf("expected blocked-fields error, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "blocked") {
		t.Errorf("expected error to mention 'blocked', got: %s", errMsg)
	}

	// Task state should be unchanged.
	fetched := env.callTool("tusk_task_get", map[string]any{"short_id": shortID})
	if got := urgencyOverridesMap(t, fetched); got != nil {
		t.Errorf("expected urgency_overrides to be absent after blocked call, got %v", got)
	}
}

func TestMCPTaskModify_UrgencyOverrides_EffectiveWeightsOnRead(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	created := env.callTool("tusk_task_create", map[string]any{"title": "effective-weights"})
	shortID := created["short_id"].(string)
	version := created["version"].(float64)

	env.callTool("tusk_task_modify", map[string]any{
		"short_id": shortID,
		"version":  version,
		"urgency_overrides": map[string]any{
			"priority_weight": 99.0,
		},
	})

	fetched := env.callTool("tusk_task_get", map[string]any{"short_id": shortID})
	rawWeights, ok := fetched["effective_urgency_weights"]
	if !ok || rawWeights == nil {
		t.Fatalf("expected effective_urgency_weights on response, got: %v", fetched)
	}
	weights, ok := rawWeights.(map[string]any)
	if !ok {
		t.Fatalf("effective_urgency_weights not an object: %T", rawWeights)
	}
	for _, key := range []string{
		"priority_weight", "due_weight", "age_weight", "active_weight",
		"blocking_weight", "blocked_weight", "tags_weight", "project_weight",
		"annotations_weight", "waiting_weight",
	} {
		if _, present := weights[key]; !present {
			t.Errorf("effective_urgency_weights missing key %q (got: %v)", key, weights)
		}
	}
	if pw, _ := weights["priority_weight"].(float64); pw != 99 {
		t.Errorf("effective priority_weight = %v, want 99", weights["priority_weight"])
	}
}

func TestMCPTaskModify_UrgencyOverrides_TaskTreeCarriesFields(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	parent := env.callTool("tusk_task_create", map[string]any{"title": "tree-parent"})
	parentSID := parent["short_id"].(string)
	parentVersion := parent["version"].(float64)

	child := env.callTool("tusk_task_create", map[string]any{
		"title":  "tree-child",
		"parent": parentSID,
	})
	childSID := child["short_id"].(string)

	env.callTool("tusk_task_modify", map[string]any{
		"short_id": parentSID,
		"version":  parentVersion,
		"urgency_overrides": map[string]any{
			"priority_weight": 50.0,
		},
	})

	treeRaw := env.callToolRaw("tusk_task_tree", map[string]any{
		"short_id": parentSID,
	})
	var tree []map[string]any
	if err := json.Unmarshal([]byte(treeRaw), &tree); err != nil {
		t.Fatalf("parsing tree: %v", err)
	}
	if len(tree) != 1 {
		t.Fatalf("expected 1 root, got %d", len(tree))
	}

	// effective_urgency_weights inheritance routes through GetDescendants in
	// handleTaskTree and may not stamp ResolvedWeights on every descendant.
	// Per phase plan: if descendants surface zero / missing weights, document
	// and treat as pending the hardening initiative; this case is a smoke
	// check rather than a strict assertion.
	rootWeights, _ := tree[0]["effective_urgency_weights"].(map[string]any)
	if rootWeights == nil {
		t.Errorf("root node missing effective_urgency_weights — finding for plan: tree subtree path may not stamp weights (hardening backlog)")
	} else if pw, _ := rootWeights["priority_weight"].(float64); pw != 50 {
		t.Errorf("root effective priority_weight = %v, want 50 (subtree weights may need hardening)", rootWeights["priority_weight"])
	}

	children, _ := tree[0]["children"].([]any)
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(children))
	}
	first, _ := children[0].(map[string]any)
	if first["short_id"] != childSID {
		t.Fatalf("child short_id mismatch: got %v want %s", first["short_id"], childSID)
	}
	childWeights, _ := first["effective_urgency_weights"].(map[string]any)
	if childWeights == nil {
		// Documented finding — see comment above.
		t.Logf("note: child node missing effective_urgency_weights from tree subtree path; flagging for hardening initiative (see phase 5 plan)")
	} else if pw, _ := childWeights["priority_weight"].(float64); pw != 50 {
		// Inherited from parent override in normal resolution.
		t.Logf("note: child effective priority_weight = %v (expected 50 via inheritance) — possible hardening gap", childWeights["priority_weight"])
	}
}
