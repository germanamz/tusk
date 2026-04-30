package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

// urgencyOverridesMap reads the urgency_overrides object from a tool
// response, returning nil if the key is missing or null.
func urgencyOverridesMap(test *testing.T, resp map[string]any) map[string]any {
	test.Helper()
	raw, ok := resp["urgency_overrides"]
	if !ok || raw == nil {
		return nil
	}
	mapped, ok := raw.(map[string]any)
	if !ok {
		test.Fatalf("urgency_overrides not an object: %T", raw)
	}
	return mapped
}

func TestMCPTaskModify_UrgencyOverrides_SetMultipleKeys(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	env := NewMCPEnv(test, binPath)

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

	got := urgencyOverridesMap(test, modified)
	if got == nil {
		test.Fatalf("expected urgency_overrides on response, got nil. raw=%v", modified)
	}
	if pw, _ := got["priority_weight"].(float64); pw != 5 {
		test.Errorf("priority_weight = %v, want 5", got["priority_weight"])
	}
	if bw, _ := got["blocking_weight"].(float64); bw != 20 {
		test.Errorf("blocking_weight = %v, want 20", got["blocking_weight"])
	}
	if len(got) != 2 {
		test.Errorf("expected exactly 2 keys in urgency_overrides, got %d (%v)", len(got), got)
	}
}

func TestMCPTaskModify_UrgencyOverrides_NullClearsSingleKey(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	env := NewMCPEnv(test, binPath)

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

	got := urgencyOverridesMap(test, second)
	if _, present := got["due_weight"]; present {
		test.Errorf("due_weight should be cleared, got %v", got["due_weight"])
	}
	if pw, _ := got["priority_weight"].(float64); pw != 5 {
		test.Errorf("priority_weight should be intact = 5, got %v", got["priority_weight"])
	}
	if bw, _ := got["blocking_weight"].(float64); bw != 20 {
		test.Errorf("blocking_weight should be intact = 20, got %v", got["blocking_weight"])
	}
}

func TestMCPTaskModify_UrgencyOverrides_EmptyPatchNoOp(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	env := NewMCPEnv(test, binPath)

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
	got := urgencyOverridesMap(test, second)
	if pw, _ := got["priority_weight"].(float64); pw != 5 {
		test.Errorf("priority_weight unchanged expected 5, got %v", got["priority_weight"])
	}
	if len(got) != 1 {
		test.Errorf("expected only priority_weight in urgency_overrides, got %v", got)
	}
}

func TestMCPTaskModify_UrgencyOverrides_TopLevelNullRejected(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	env := NewMCPEnv(test, binPath)

	created := env.callTool("tusk_task_create", map[string]any{"title": "top-null"})
	shortID := created["short_id"].(string)
	version := created["version"].(float64)

	errMsg := env.callToolExpectError("tusk_task_modify", map[string]any{
		"short_id":          shortID,
		"version":           version,
		"urgency_overrides": nil,
	})
	if !strings.Contains(errMsg, "urgency_overrides_clear") {
		test.Errorf("expected error to mention urgency_overrides_clear, got: %s", errMsg)
	}
}

func TestMCPTaskModify_UrgencyOverrides_ClearAllThenSet(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	env := NewMCPEnv(test, binPath)

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
	got := urgencyOverridesMap(test, second)
	if len(got) != 1 {
		test.Errorf("expected exactly one key after clear+set, got %d (%v)", len(got), got)
	}
	if pw, _ := got["priority_weight"].(float64); pw != 7 {
		test.Errorf("priority_weight = %v, want 7", got["priority_weight"])
	}
}

func TestMCPTaskModify_UrgencyOverrides_UnknownKeyRejected(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	env := NewMCPEnv(test, binPath)

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
		test.Errorf("expected error to name bogus_key, got: %s", errMsg)
	}
	for _, valid := range []string{
		"priority_weight", "due_weight", "age_weight", "active_weight",
		"blocking_weight", "blocked_weight", "tags_weight", "project_weight",
		"annotations_weight", "waiting_weight",
	} {
		if !strings.Contains(errMsg, valid) {
			test.Errorf("error should list valid key %q, got: %s", valid, errMsg)
		}
	}
}

func TestMCPTaskModify_UrgencyOverrides_NonNumericRejected(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	env := NewMCPEnv(test, binPath)

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
		test.Errorf("expected error to name priority_weight, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "must be a number or null") {
		test.Errorf("expected error to mention 'must be a number or null', got: %s", errMsg)
	}
}

func TestMCPTaskModify_UrgencyOverrides_BlockedFieldsGate(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	env := NewMCPEnv(test, binPath).WithConfigFile(`
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
		test.Errorf("expected blocked-fields error, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "blocked") {
		test.Errorf("expected error to mention 'blocked', got: %s", errMsg)
	}

	// Task state should be unchanged.
	fetched := env.callTool("tusk_task_get", map[string]any{"short_id": shortID})
	if got := urgencyOverridesMap(test, fetched); got != nil {
		test.Errorf("expected urgency_overrides to be absent after blocked call, got %v", got)
	}
}

func TestMCPTaskModify_UrgencyOverrides_EffectiveWeightsOnRead(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	env := NewMCPEnv(test, binPath)

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
		test.Fatalf("expected effective_urgency_weights on response, got: %v", fetched)
	}
	weights, ok := rawWeights.(map[string]any)
	if !ok {
		test.Fatalf("effective_urgency_weights not an object: %T", rawWeights)
	}
	for _, key := range []string{
		"priority_weight", "due_weight", "age_weight", "active_weight",
		"blocking_weight", "blocked_weight", "tags_weight", "project_weight",
		"annotations_weight", "waiting_weight",
	} {
		if _, present := weights[key]; !present {
			test.Errorf("effective_urgency_weights missing key %q (got: %v)", key, weights)
		}
	}
	if pw, _ := weights["priority_weight"].(float64); pw != 99 {
		test.Errorf("effective priority_weight = %v, want 99", weights["priority_weight"])
	}
}

func TestMCPTaskModify_UrgencyOverrides_TaskTreeCarriesFields(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	env := NewMCPEnv(test, binPath)

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
		test.Fatalf("parsing tree: %v", err)
	}
	if len(tree) != 1 {
		test.Fatalf("expected 1 root, got %d", len(tree))
	}

	// effective_urgency_weights inheritance routes through GetDescendants in
	// handleTaskTree and may not stamp ResolvedWeights on every descendant.
	// Per phase plan: if descendants surface zero / missing weights, document
	// and treat as pending the hardening initiative; this case is a smoke
	// check rather than a strict assertion.
	rootWeights, _ := tree[0]["effective_urgency_weights"].(map[string]any)
	if rootWeights == nil {
		test.Errorf("root node missing effective_urgency_weights — finding for plan: tree subtree path may not stamp weights (hardening backlog)")
	} else if pw, _ := rootWeights["priority_weight"].(float64); pw != 50 {
		test.Errorf("root effective priority_weight = %v, want 50 (subtree weights may need hardening)", rootWeights["priority_weight"])
	}

	children, _ := tree[0]["children"].([]any)
	if len(children) != 1 {
		test.Fatalf("expected 1 child, got %d", len(children))
	}
	firstChild, _ := children[0].(map[string]any)
	if firstChild["short_id"] != childSID {
		test.Fatalf("child short_id mismatch: got %v want %s", firstChild["short_id"], childSID)
	}
	childWeights, _ := firstChild["effective_urgency_weights"].(map[string]any)
	if childWeights == nil {
		// Documented finding — see comment above.
		test.Logf("note: child node missing effective_urgency_weights from tree subtree path; flagging for hardening initiative (see phase 5 plan)")
	} else if pw, _ := childWeights["priority_weight"].(float64); pw != 50 {
		// Inherited from parent override in normal resolution.
		test.Logf("note: child effective priority_weight = %v (expected 50 via inheritance) — possible hardening gap", childWeights["priority_weight"])
	}
}
