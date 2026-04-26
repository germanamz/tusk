package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

// summaryFixture creates a small task tree and drives one descendant to
// completion. Returns the root short_id and child short_ids in order.
//
//	Root
//	├── Child A (pending)
//	├── Child B (active)
//	└── Child C (completed)
func summaryFixture(t *testing.T, env *mcpEnv) (rootSID string, childSIDs []string) {
	t.Helper()
	root := env.callTool("tusk_task_create", map[string]any{"title": "Summary root"})
	rootSID = root["short_id"].(string)

	a := env.callTool("tusk_task_create", map[string]any{"title": "Child A", "parent": rootSID})
	b := env.callTool("tusk_task_create", map[string]any{"title": "Child B", "parent": rootSID})
	c := env.callTool("tusk_task_create", map[string]any{"title": "Child C", "parent": rootSID})

	bv := b["version"].(float64)
	bStarted := env.callTool("tusk_task_start", map[string]any{
		"short_id": b["short_id"].(string),
		"version":  bv,
	})
	_ = bStarted

	cv := c["version"].(float64)
	cStarted := env.callTool("tusk_task_start", map[string]any{
		"short_id": c["short_id"].(string),
		"version":  cv,
	})
	cv2 := cStarted["version"].(float64)
	env.callTool("tusk_task_done", map[string]any{
		"short_id": c["short_id"].(string),
		"version":  cv2,
	})

	return rootSID, []string{
		a["short_id"].(string),
		b["short_id"].(string),
		c["short_id"].(string),
	}
}

// callSummary runs tusk_task_summary and returns the parsed envelope.
func callSummary(t *testing.T, env *mcpEnv, args map[string]any) map[string]any {
	t.Helper()
	return env.callTool("tusk_task_summary", args)
}

// TestMCPSummary_SingleMode covers single-subtree summaries (mode=single,
// totals omitted) and the full=true rejection in single-id mode.
func TestMCPSummary_SingleMode(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	rootSID, _ := summaryFixture(t, env)

	resp := callSummary(t, env, map[string]any{"short_id": rootSID})
	if resp["mode"] != "single" {
		t.Fatalf("expected mode 'single', got %v", resp["mode"])
	}
	blocks := resp["blocks"].([]any)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	block := blocks[0].(map[string]any)
	task := block["task"].(map[string]any)
	if task["short_id"] != rootSID {
		t.Fatalf("expected block task short_id %q, got %v", rootSID, task["short_id"])
	}
	roll := block["rollup"].(map[string]any)
	if roll["done"].(float64) != 1 {
		t.Fatalf("expected done=1, got %v", roll["done"])
	}
	if roll["total"].(float64) != 3 {
		t.Fatalf("expected total=3, got %v", roll["total"])
	}
	if _, ok := resp["totals"]; ok {
		t.Fatalf("expected totals to be absent in single mode, got: %v", resp["totals"])
	}

	// full=true is rejected in single mode.
	errMsg := env.callToolExpectError("tusk_task_summary", map[string]any{
		"short_id": rootSID,
		"full":     true,
	})
	if !strings.Contains(errMsg, "single-id mode") {
		t.Fatalf("expected 'single-id mode' error, got: %s", errMsg)
	}
}

// TestMCPSummary_FilterStringMode exercises the boolean filter expression
// path and verifies that, when the filter string is present, structured
// filter params are ignored.
func TestMCPSummary_FilterStringMode(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	taxonomyTOML := "[taxonomy]\nlevels = [[\"initiative\"], [\"story\"], [\"task\"]]\n"
	env := newMCPEnvWithConfig(t, binPath, taxonomyTOML)

	// Roadmap (initiative)
	//   Story 1 (story)
	//     Task 1.1 (task)
	//   Story 2 (story)
	roadmap := env.callTool("tusk_task_create", map[string]any{"title": "Roadmap", "level": "initiative"})
	roadmapSID := roadmap["short_id"].(string)
	story1 := env.callTool("tusk_task_create", map[string]any{
		"title":  "Story 1",
		"level":  "story",
		"parent": roadmapSID,
	})
	story1SID := story1["short_id"].(string)
	env.callTool("tusk_task_create", map[string]any{
		"title":  "Task 1.1",
		"level":  "task",
		"parent": story1SID,
	})
	env.callTool("tusk_task_create", map[string]any{
		"title":  "Story 2",
		"level":  "story",
		"parent": roadmapSID,
	})

	// Filter-string mode: structured params (level: "task") must be
	// ignored when filter string is set.
	resp := callSummary(t, env, map[string]any{
		"filter": "level=story",
		"level":  "task",
	})
	if resp["mode"] != "filter" {
		t.Fatalf("expected mode 'filter', got %v", resp["mode"])
	}
	blocks := resp["blocks"].([]any)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 story blocks (filter wins over structured level=task), got %d", len(blocks))
	}
	for _, b := range blocks {
		task := b.(map[string]any)["task"].(map[string]any)
		if task["level"] != "story" {
			t.Fatalf("expected story-level block, got level=%v title=%v", task["level"], task["title"])
		}
	}
	if _, ok := resp["totals"]; !ok {
		t.Fatalf("expected totals populated in filter mode, got nil")
	}
}

// TestMCPSummary_StructuredParamsMode exercises the structured-params
// path: passing only `level: "initiative"` (no filter, no short_id)
// should produce filter-mode blocks of initiative-level tasks.
func TestMCPSummary_StructuredParamsMode(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	taxonomyTOML := "[taxonomy]\nlevels = [[\"initiative\"], [\"story\"], [\"task\"]]\n"
	env := newMCPEnvWithConfig(t, binPath, taxonomyTOML)

	// Two initiatives plus an unrelated story under one of them.
	init1 := env.callTool("tusk_task_create", map[string]any{"title": "Init A", "level": "initiative"})
	env.callTool("tusk_task_create", map[string]any{"title": "Init B", "level": "initiative"})
	env.callTool("tusk_task_create", map[string]any{
		"title":  "Story under A",
		"level":  "story",
		"parent": init1["short_id"].(string),
	})

	resp := callSummary(t, env, map[string]any{"level": "initiative"})
	if resp["mode"] != "filter" {
		t.Fatalf("expected mode 'filter' (any populated structured filter), got %v", resp["mode"])
	}
	blocks := resp["blocks"].([]any)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 initiative blocks, got %d", len(blocks))
	}
	for _, b := range blocks {
		task := b.(map[string]any)["task"].(map[string]any)
		if task["level"] != "initiative" {
			t.Fatalf("expected initiative-level block, got level=%v", task["level"])
		}
	}
	if _, ok := resp["totals"]; !ok {
		t.Fatalf("expected totals populated in structured-params mode, got nil")
	}
}

// TestMCPSummary_RootsMode verifies that calling with no filter / short_id
// summarizes root tasks (mode=roots) and returns one block per root.
func TestMCPSummary_RootsMode(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	// Two distinct roots; the second has a child.
	env.callTool("tusk_task_create", map[string]any{"title": "Root A"})
	rootB := env.callTool("tusk_task_create", map[string]any{"title": "Root B"})
	env.callTool("tusk_task_create", map[string]any{
		"title":  "B child",
		"parent": rootB["short_id"].(string),
	})

	resp := callSummary(t, env, nil)
	if resp["mode"] != "roots" {
		t.Fatalf("expected mode 'roots', got %v", resp["mode"])
	}
	blocks := resp["blocks"].([]any)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 root blocks, got %d", len(blocks))
	}
	totals := resp["totals"].(map[string]any)
	if totals["total"].(float64) != 1 {
		t.Fatalf("expected total=1 (one descendant across both roots), got %v", totals["total"])
	}
}

// TestMCPSummary_FilterParseError surfaces filter parse errors as a tool
// error (isError=true), not a JSON-RPC protocol error.
func TestMCPSummary_FilterParseError(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	errMsg := env.callToolExpectError("tusk_task_summary", map[string]any{
		"filter": "level=story AND (",
	})
	if !strings.Contains(errMsg, "filter parse error") {
		t.Fatalf("expected 'filter parse error' in error, got: %s", errMsg)
	}
}

// TestMCPSummary_EmptyResult verifies that a filter matching nothing
// returns mode=filter with empty blocks and a zero-rollup totals block
// whose status_counts is `[]`, not `null`.
func TestMCPSummary_EmptyResult(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	taxonomyTOML := "[taxonomy]\nlevels = [[\"initiative\"], [\"story\"], [\"task\"]]\n"
	env := newMCPEnvWithConfig(t, binPath, taxonomyTOML)

	env.callTool("tusk_task_create", map[string]any{"title": "Anything", "level": "initiative"})

	rawText := env.callToolRaw("tusk_task_summary", map[string]any{"level": "nonexistent"})
	var resp map[string]any
	if err := json.Unmarshal([]byte(rawText), &resp); err != nil {
		t.Fatalf("parsing summary result: %v\nraw: %s", err, rawText)
	}
	if resp["mode"] != "filter" {
		t.Fatalf("expected mode 'filter', got %v", resp["mode"])
	}
	blocks := resp["blocks"].([]any)
	if len(blocks) != 0 {
		t.Fatalf("expected empty blocks, got %d", len(blocks))
	}
	totals, ok := resp["totals"].(map[string]any)
	if !ok {
		t.Fatalf("expected totals to be present, got: %v", resp["totals"])
	}
	if totals["total"].(float64) != 0 {
		t.Fatalf("expected total=0, got %v", totals["total"])
	}
	// Critical: status_counts must serialize as [] not null.
	if !strings.Contains(rawText, `"status_counts": []`) {
		t.Fatalf("expected status_counts to be empty array `[]`, raw output:\n%s", rawText)
	}
	sc, ok := totals["status_counts"].([]any)
	if !ok {
		t.Fatalf("expected status_counts to decode as array, got %T", totals["status_counts"])
	}
	if len(sc) != 0 {
		t.Fatalf("expected empty status_counts, got %v", sc)
	}
}

// TestMCPSummary_CustomWorkflow exercises rollup against a custom
// workflow where the `done` role lives on `shipped`. Mirrors Phase 2's
// custom-workflow case but via the MCP surface. Workspace-shaping tools
// (workflow_create, project_modify) are disabled by default in
// config/default.toml — opt them back in for this test.
func TestMCPSummary_CustomWorkflow(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	// Override workspace-shaping defaults: enable workflow_create /
	// project_modify, and clear the default blocked_fields so the
	// project_modify "workflow" field is writable.
	env := newMCPEnvWithConfig(t, binPath,
		"[mcp]\n"+
			"disabled_tools = []\n"+
			"[mcp.blocked_fields]\n"+
			"tusk_project_modify = []\n"+
			"tusk_project_delete = []\n",
	)

	// Create a scrum-style workflow.
	env.callTool("tusk_workflow_create", map[string]any{
		"name": "scrum",
		"statuses": []map[string]any{
			{"name": "backlog", "roles": []string{"initial"}},
			{"name": "in_progress", "roles": []string{"start", "highlight"}},
			{"name": "shipped", "roles": []string{"terminal", "done", "dim"}},
			{"name": "wontfix", "roles": []string{"terminal", "delete", "dim"}},
		},
		"transitions": []map[string]any{
			{"from": "backlog", "to": "in_progress"},
			{"from": "in_progress", "to": "shipped"},
			{"from": "in_progress", "to": "wontfix"},
			{"from": "backlog", "to": "wontfix"},
		},
	})

	// Rebind the default project to scrum. The seeded default project's
	// version is 1 in a freshly migrated DB (see migrations/004 +
	// service.Project.Create). project_list does not expose version, so
	// we rely on this invariant rather than re-fetching.
	env.callTool("tusk_project_modify", map[string]any{
		"name":     "default",
		"version":  1,
		"workflow": "scrum",
	})

	// Build root + 2 children, ship one of them.
	root := env.callTool("tusk_task_create", map[string]any{"title": "Custom rollup root"})
	rootSID := root["short_id"].(string)
	a := env.callTool("tusk_task_create", map[string]any{"title": "A", "parent": rootSID})
	b := env.callTool("tusk_task_create", map[string]any{"title": "B", "parent": rootSID})

	aStarted := env.callTool("tusk_task_start", map[string]any{
		"short_id": a["short_id"].(string),
		"version":  a["version"].(float64),
	})
	_ = aStarted

	bStarted := env.callTool("tusk_task_start", map[string]any{
		"short_id": b["short_id"].(string),
		"version":  b["version"].(float64),
	})
	env.callTool("tusk_task_done", map[string]any{
		"short_id": b["short_id"].(string),
		"version":  bStarted["version"].(float64),
	})

	resp := callSummary(t, env, map[string]any{"short_id": rootSID})
	blocks := resp["blocks"].([]any)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	roll := blocks[0].(map[string]any)["rollup"].(map[string]any)
	if roll["done"].(float64) != 1 {
		t.Fatalf("expected done=1 (shipped task), got %v", roll["done"])
	}
	if roll["total"].(float64) != 2 {
		t.Fatalf("expected total=2 (in_progress + shipped, wontfix excluded), got %v", roll["total"])
	}
	counts := roll["status_counts"].([]any)
	foundShipped, foundInProgress, foundBacklog := false, false, false
	for _, c := range counts {
		m := c.(map[string]any)
		switch m["name"].(string) {
		case "shipped":
			foundShipped = true
			if m["count"].(float64) != 1 {
				t.Fatalf("expected shipped: 1, got %v", m["count"])
			}
		case "in_progress":
			foundInProgress = true
			if m["count"].(float64) != 1 {
				t.Fatalf("expected in_progress: 1, got %v", m["count"])
			}
		case "backlog":
			foundBacklog = true
			if m["count"].(float64) != 0 {
				t.Fatalf("expected backlog: 0, got %v", m["count"])
			}
		case "wontfix":
			t.Fatalf("wontfix bucket must be excluded by delete role, got: %v", m)
		}
	}
	if !foundShipped || !foundInProgress || !foundBacklog {
		t.Fatalf("expected scrum status buckets present, got: %v", counts)
	}
}

// TestMCPSummary_ToolListed verifies the new tool appears in tools/list.
func TestMCPSummary_ToolListed(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	resp := env.send("tools/list", nil)
	if resp.Error != nil {
		t.Fatalf("tools/list error: %s", resp.Error)
	}
	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("parsing tools/list: %v", err)
	}
	for _, tool := range result.Tools {
		if tool.Name == "tusk_task_summary" {
			return
		}
	}
	t.Fatal("tusk_task_summary not found in tools/list")
}

// TestMCPSummary_TaskListUnchanged is a regression check for Task 4.1's
// buildTaskFilter extraction: the structured-param filter path on
// tusk_task_list must still produce the expected output.
func TestMCPSummary_TaskListUnchanged(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	created := env.callTool("tusk_task_create", map[string]any{
		"title": "Filterable",
		"tags":  []string{"alpha"},
	})
	shortID := created["short_id"].(string)

	// Filter by tag — exercises the structured-params path.
	listRaw := env.callToolRaw("tusk_task_list", map[string]any{
		"tags": []string{"alpha"},
	})
	var listed []map[string]any
	if err := json.Unmarshal([]byte(listRaw), &listed); err != nil {
		t.Fatalf("parsing list: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected 1 task with tag alpha, got %d", len(listed))
	}
	if listed[0]["short_id"] != shortID {
		t.Fatalf("expected short_id %q, got %v", shortID, listed[0]["short_id"])
	}
	tags, _ := listed[0]["tags"].([]any)
	if len(tags) != 1 || tags[0] != "alpha" {
		t.Fatalf("expected tags [alpha], got %v", tags)
	}

	// Filter by status — second structured-params path.
	pendingRaw := env.callToolRaw("tusk_task_list", map[string]any{
		"status": []string{"pending"},
	})
	var pending []map[string]any
	if err := json.Unmarshal([]byte(pendingRaw), &pending); err != nil {
		t.Fatalf("parsing pending list: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending task, got %d", len(pending))
	}
}
