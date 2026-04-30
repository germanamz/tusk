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
func summaryFixture(test *testing.T, env *MCPEnv) (rootSID string, childSIDs []string) {
	test.Helper()
	root := env.callTool("tusk_task_create", map[string]any{"title": "Summary root"})
	rootSID = root["short_id"].(string)

	childA := env.callTool("tusk_task_create", map[string]any{"title": "Child A", "parent": rootSID})
	childB := env.callTool("tusk_task_create", map[string]any{"title": "Child B", "parent": rootSID})
	childC := env.callTool("tusk_task_create", map[string]any{"title": "Child C", "parent": rootSID})

	bVersion := childB["version"].(float64)
	bStarted := env.callTool("tusk_task_start", map[string]any{
		"short_id": childB["short_id"].(string),
		"version":  bVersion,
	})
	_ = bStarted

	cVersion := childC["version"].(float64)
	cStarted := env.callTool("tusk_task_start", map[string]any{
		"short_id": childC["short_id"].(string),
		"version":  cVersion,
	})
	cVersion2 := cStarted["version"].(float64)
	env.callTool("tusk_task_done", map[string]any{
		"short_id": childC["short_id"].(string),
		"version":  cVersion2,
	})

	return rootSID, []string{
		childA["short_id"].(string),
		childB["short_id"].(string),
		childC["short_id"].(string),
	}
}

// callSummary runs tusk_task_summary and returns the parsed envelope.
func callSummary(test *testing.T, env *MCPEnv, args map[string]any) map[string]any {
	test.Helper()
	return env.callTool("tusk_task_summary", args)
}

// TestMCPSummary_SingleMode covers single-subtree summaries (mode=single,
// totals omitted) and the full=true rejection in single-id mode.
func TestMCPSummary_SingleMode(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	env := NewMCPEnv(test, binPath)

	rootSID, _ := summaryFixture(test, env)

	resp := callSummary(test, env, map[string]any{"short_id": rootSID})
	if resp["mode"] != "single" {
		test.Fatalf("expected mode 'single', got %v", resp["mode"])
	}
	blocks := resp["blocks"].([]any)
	if len(blocks) != 1 {
		test.Fatalf("expected 1 block, got %d", len(blocks))
	}
	block := blocks[0].(map[string]any)
	task := block["task"].(map[string]any)
	if task["short_id"] != rootSID {
		test.Fatalf("expected block task short_id %q, got %v", rootSID, task["short_id"])
	}
	roll := block["rollup"].(map[string]any)
	if roll["done"].(float64) != 1 {
		test.Fatalf("expected done=1, got %v", roll["done"])
	}
	if roll["total"].(float64) != 3 {
		test.Fatalf("expected total=3, got %v", roll["total"])
	}
	if _, ok := resp["totals"]; ok {
		test.Fatalf("expected totals to be absent in single mode, got: %v", resp["totals"])
	}

	// full=true is rejected in single mode.
	errMsg := env.callToolExpectError("tusk_task_summary", map[string]any{
		"short_id": rootSID,
		"full":     true,
	})
	if !strings.Contains(errMsg, "single-id mode") {
		test.Fatalf("expected 'single-id mode' error, got: %s", errMsg)
	}
}

// TestMCPSummary_FilterStringMode exercises the boolean filter expression
// path and verifies that, when the filter string is present, structured
// filter params are ignored.
func TestMCPSummary_FilterStringMode(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	taxonomyTOML := "[taxonomy]\nlevels = [[\"initiative\"], [\"story\"], [\"task\"]]\n"
	env := NewMCPEnv(test, binPath).WithConfigFile(taxonomyTOML)

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
	resp := callSummary(test, env, map[string]any{
		"filter": "level=story",
		"level":  "task",
	})
	if resp["mode"] != "filter" {
		test.Fatalf("expected mode 'filter', got %v", resp["mode"])
	}
	blocks := resp["blocks"].([]any)
	if len(blocks) != 2 {
		test.Fatalf("expected 2 story blocks (filter wins over structured level=task), got %d", len(blocks))
	}
	for _, block := range blocks {
		task := block.(map[string]any)["task"].(map[string]any)
		if task["level"] != "story" {
			test.Fatalf("expected story-level block, got level=%v title=%v", task["level"], task["title"])
		}
	}
	if _, ok := resp["totals"]; !ok {
		test.Fatalf("expected totals populated in filter mode, got nil")
	}
}

// TestMCPSummary_StructuredParamsMode exercises the structured-params
// path: passing only `level: "initiative"` (no filter, no short_id)
// should produce filter-mode blocks of initiative-level tasks.
func TestMCPSummary_StructuredParamsMode(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	taxonomyTOML := "[taxonomy]\nlevels = [[\"initiative\"], [\"story\"], [\"task\"]]\n"
	env := NewMCPEnv(test, binPath).WithConfigFile(taxonomyTOML)

	// Two initiatives plus an unrelated story under one of them.
	init1 := env.callTool("tusk_task_create", map[string]any{"title": "Init A", "level": "initiative"})
	env.callTool("tusk_task_create", map[string]any{"title": "Init B", "level": "initiative"})
	env.callTool("tusk_task_create", map[string]any{
		"title":  "Story under A",
		"level":  "story",
		"parent": init1["short_id"].(string),
	})

	resp := callSummary(test, env, map[string]any{"level": "initiative"})
	if resp["mode"] != "filter" {
		test.Fatalf("expected mode 'filter' (any populated structured filter), got %v", resp["mode"])
	}
	blocks := resp["blocks"].([]any)
	if len(blocks) != 2 {
		test.Fatalf("expected 2 initiative blocks, got %d", len(blocks))
	}
	for _, block := range blocks {
		task := block.(map[string]any)["task"].(map[string]any)
		if task["level"] != "initiative" {
			test.Fatalf("expected initiative-level block, got level=%v", task["level"])
		}
	}
	if _, ok := resp["totals"]; !ok {
		test.Fatalf("expected totals populated in structured-params mode, got nil")
	}
}

// TestMCPSummary_RootsMode verifies that calling with no filter / short_id
// summarizes root tasks (mode=roots) and returns one block per root.
func TestMCPSummary_RootsMode(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	env := NewMCPEnv(test, binPath)

	// Two distinct roots; the second has a child.
	env.callTool("tusk_task_create", map[string]any{"title": "Root A"})
	rootB := env.callTool("tusk_task_create", map[string]any{"title": "Root B"})
	env.callTool("tusk_task_create", map[string]any{
		"title":  "B child",
		"parent": rootB["short_id"].(string),
	})

	resp := callSummary(test, env, nil)
	if resp["mode"] != "roots" {
		test.Fatalf("expected mode 'roots', got %v", resp["mode"])
	}
	blocks := resp["blocks"].([]any)
	if len(blocks) != 2 {
		test.Fatalf("expected 2 root blocks, got %d", len(blocks))
	}
	totals := resp["totals"].(map[string]any)
	if totals["total"].(float64) != 1 {
		test.Fatalf("expected total=1 (one descendant across both roots), got %v", totals["total"])
	}
}

// TestMCPSummary_FilterParseError surfaces filter parse errors as a tool
// error (isError=true), not a JSON-RPC protocol error.
func TestMCPSummary_FilterParseError(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	env := NewMCPEnv(test, binPath)

	errMsg := env.callToolExpectError("tusk_task_summary", map[string]any{
		"filter": "level=story AND (",
	})
	if !strings.Contains(errMsg, "filter parse error") {
		test.Fatalf("expected 'filter parse error' in error, got: %s", errMsg)
	}
}

// TestMCPSummary_EmptyResult verifies that a filter matching nothing
// returns mode=filter with empty blocks and a zero-rollup totals block
// whose status_counts is `[]`, not `null`.
func TestMCPSummary_EmptyResult(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}

	taxonomyTOML := "[taxonomy]\nlevels = [[\"initiative\"], [\"story\"], [\"task\"]]\n"
	env := NewMCPEnv(test, binPath).WithConfigFile(taxonomyTOML)

	env.callTool("tusk_task_create", map[string]any{"title": "Anything", "level": "initiative"})

	rawText := env.callToolRaw("tusk_task_summary", map[string]any{"level": "nonexistent"})
	var resp map[string]any
	if err := json.Unmarshal([]byte(rawText), &resp); err != nil {
		test.Fatalf("parsing summary result: %v\nraw: %s", err, rawText)
	}
	if resp["mode"] != "filter" {
		test.Fatalf("expected mode 'filter', got %v", resp["mode"])
	}
	blocks := resp["blocks"].([]any)
	if len(blocks) != 0 {
		test.Fatalf("expected empty blocks, got %d", len(blocks))
	}
	totals, ok := resp["totals"].(map[string]any)
	if !ok {
		test.Fatalf("expected totals to be present, got: %v", resp["totals"])
	}
	if totals["total"].(float64) != 0 {
		test.Fatalf("expected total=0, got %v", totals["total"])
	}
	// Critical: status_counts must serialize as [] not null.
	if !strings.Contains(rawText, `"status_counts": []`) {
		test.Fatalf("expected status_counts to be empty array `[]`, raw output:\n%s", rawText)
	}
	statusCounts, ok := totals["status_counts"].([]any)
	if !ok {
		test.Fatalf("expected status_counts to decode as array, got %T", totals["status_counts"])
	}
	if len(statusCounts) != 0 {
		test.Fatalf("expected empty status_counts, got %v", statusCounts)
	}
}

// TestMCPSummary_CustomWorkflow exercises rollup against a custom
// workflow where the `done` role lives on `shipped`. Mirrors Phase 2's
// custom-workflow case but via the MCP surface. Workspace-shaping tools
// (workflow_create, project_modify) are disabled by default in
// config/default.toml — opt them back in for this test.
func TestMCPSummary_CustomWorkflow(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	// Override workspace-shaping defaults: enable workflow_create /
	// project_modify, and clear the default blocked_fields so the
	// project_modify "workflow" field is writable.
	env := NewMCPEnv(test, binPath).WithConfigFile(
		"[mcp]\n" +
			"disabled_tools = []\n" +
			"[mcp.blocked_fields]\n" +
			"tusk_project_modify = []\n" +
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
	taskA := env.callTool("tusk_task_create", map[string]any{"title": "A", "parent": rootSID})
	taskB := env.callTool("tusk_task_create", map[string]any{"title": "B", "parent": rootSID})

	aStarted := env.callTool("tusk_task_start", map[string]any{
		"short_id": taskA["short_id"].(string),
		"version":  taskA["version"].(float64),
	})
	_ = aStarted

	bStarted := env.callTool("tusk_task_start", map[string]any{
		"short_id": taskB["short_id"].(string),
		"version":  taskB["version"].(float64),
	})
	env.callTool("tusk_task_done", map[string]any{
		"short_id": taskB["short_id"].(string),
		"version":  bStarted["version"].(float64),
	})

	resp := callSummary(test, env, map[string]any{"short_id": rootSID})
	blocks := resp["blocks"].([]any)
	if len(blocks) != 1 {
		test.Fatalf("expected 1 block, got %d", len(blocks))
	}
	roll := blocks[0].(map[string]any)["rollup"].(map[string]any)
	if roll["done"].(float64) != 1 {
		test.Fatalf("expected done=1 (shipped task), got %v", roll["done"])
	}
	if roll["total"].(float64) != 2 {
		test.Fatalf("expected total=2 (in_progress + shipped, wontfix excluded), got %v", roll["total"])
	}
	counts := roll["status_counts"].([]any)
	foundShipped, foundInProgress, foundBacklog := false, false, false
	for _, count := range counts {
		statusMap := count.(map[string]any)
		switch statusMap["name"].(string) {
		case "shipped":
			foundShipped = true
			if statusMap["count"].(float64) != 1 {
				test.Fatalf("expected shipped: 1, got %v", statusMap["count"])
			}
		case "in_progress":
			foundInProgress = true
			if statusMap["count"].(float64) != 1 {
				test.Fatalf("expected in_progress: 1, got %v", statusMap["count"])
			}
		case "backlog":
			foundBacklog = true
			if statusMap["count"].(float64) != 0 {
				test.Fatalf("expected backlog: 0, got %v", statusMap["count"])
			}
		case "wontfix":
			test.Fatalf("wontfix bucket must be excluded by delete role, got: %v", statusMap)
		}
	}
	if !foundShipped || !foundInProgress || !foundBacklog {
		test.Fatalf("expected scrum status buckets present, got: %v", counts)
	}
}

// TestMCPSummary_ToolListed verifies the new tool appears in tools/list.
func TestMCPSummary_ToolListed(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	env := NewMCPEnv(test, binPath)

	resp := env.Send("tools/list", nil)
	if resp.Error != nil {
		test.Fatalf("tools/list error: %s", resp.Error)
	}
	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		test.Fatalf("parsing tools/list: %v", err)
	}
	for _, tool := range result.Tools {
		if tool.Name == "tusk_task_summary" {
			return
		}
	}
	test.Fatal("tusk_task_summary not found in tools/list")
}

// TestMCPSummary_TaskListUnchanged is a regression check for Task 4.1's
// buildTaskFilter extraction: the structured-param filter path on
// tusk_task_list must still produce the expected output.
func TestMCPSummary_TaskListUnchanged(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	env := NewMCPEnv(test, binPath)

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
		test.Fatalf("parsing list: %v", err)
	}
	if len(listed) != 1 {
		test.Fatalf("expected 1 task with tag alpha, got %d", len(listed))
	}
	if listed[0]["short_id"] != shortID {
		test.Fatalf("expected short_id %q, got %v", shortID, listed[0]["short_id"])
	}
	tags, _ := listed[0]["tags"].([]any)
	if len(tags) != 1 || tags[0] != "alpha" {
		test.Fatalf("expected tags [alpha], got %v", tags)
	}

	// Filter by status — second structured-params path.
	pendingRaw := env.callToolRaw("tusk_task_list", map[string]any{
		"status": []string{"pending"},
	})
	var pending []map[string]any
	if err := json.Unmarshal([]byte(pendingRaw), &pending); err != nil {
		test.Fatalf("parsing pending list: %v", err)
	}
	if len(pending) != 1 {
		test.Fatalf("expected 1 pending task, got %d", len(pending))
	}
}
