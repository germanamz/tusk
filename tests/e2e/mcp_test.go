package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// mcpEnv manages an MCP server subprocess for E2E testing.
type mcpEnv struct {
	t      *testing.T
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	nextID int
}

// newMCPEnv starts a `tusk mcp serve` subprocess with a fresh temp DB.
func newMCPEnv(t *testing.T, binPath string) *mcpEnv {
	t.Helper()
	tmpFile, err := os.CreateTemp(t.TempDir(), "tusk-mcp-e2e-*.db")
	if err != nil {
		t.Fatalf("creating temp db: %v", err)
	}
	_ = tmpFile.Close()

	cmd := exec.Command(binPath, "--db", tmpFile.Name(), "mcp", "serve")
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

	// Send initialize request
	env.initialize()

	return env
}

// jsonRPCRequest is a JSON-RPC 2.0 request.
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonRPCResponse is a JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

// send sends a JSON-RPC request and reads the response.
func (e *mcpEnv) send(method string, params any) jsonRPCResponse {
	e.t.Helper()

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      e.nextID,
		Method:  method,
		Params:  params,
	}
	e.nextID++

	b, err := json.Marshal(req)
	if err != nil {
		e.t.Fatalf("marshaling request: %v", err)
	}

	if _, err := fmt.Fprintf(e.stdin, "%s\n", b); err != nil {
		e.t.Fatalf("writing request: %v", err)
	}

	line, err := e.stdout.ReadString('\n')
	if err != nil {
		e.t.Fatalf("reading response: %v", err)
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		e.t.Fatalf("unmarshaling response: %v\nraw: %s", err, line)
	}

	return resp
}

// initialize sends the MCP initialize handshake.
func (e *mcpEnv) initialize() {
	e.t.Helper()
	resp := e.send("initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "tusk-e2e-test",
			"version": "1.0.0",
		},
	})
	if resp.Error != nil {
		e.t.Fatalf("initialize failed: %s", resp.Error)
	}

	// Send initialized notification (no response expected for notifications)
	notif := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	b, _ := json.Marshal(notif)
	if _, err := fmt.Fprintf(e.stdin, "%s\n", b); err != nil {
		e.t.Fatalf("writing initialized notification: %v", err)
	}
}

// callTool sends a tools/call request and returns the parsed result.
func (e *mcpEnv) callTool(name string, args map[string]any) map[string]any {
	e.t.Helper()
	resp := e.send("tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if resp.Error != nil {
		e.t.Fatalf("tool %s returned error: %s", name, resp.Error)
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		e.t.Fatalf("parsing tool result: %v", err)
	}
	if result.IsError {
		e.t.Fatalf("tool %s returned isError=true: %s", name, result.Content[0].Text)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &parsed); err != nil {
		e.t.Fatalf("parsing tool JSON content: %v\nraw: %s", err, result.Content[0].Text)
	}
	return parsed
}

// callToolRaw sends a tools/call request and returns the raw text content.
func (e *mcpEnv) callToolRaw(name string, args map[string]any) string {
	e.t.Helper()
	resp := e.send("tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if resp.Error != nil {
		e.t.Fatalf("tool %s returned error: %s", name, resp.Error)
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		e.t.Fatalf("parsing tool result: %v", err)
	}
	return result.Content[0].Text
}

// callToolExpectError sends a tools/call request and expects an isError=true result.
func (e *mcpEnv) callToolExpectError(name string, args map[string]any) string {
	e.t.Helper()
	resp := e.send("tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if resp.Error != nil {
		e.t.Fatalf("tool %s returned protocol error (expected tool error): %s", name, resp.Error)
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		e.t.Fatalf("parsing tool result: %v", err)
	}
	if !result.IsError {
		e.t.Fatalf("expected isError=true, got false. content: %s", result.Content[0].Text)
	}
	return result.Content[0].Text
}

func TestMCPTaskLifecycle(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	// Create a task
	created := env.callTool("tusk_task_create", map[string]any{
		"title":    "MCP test task",
		"priority": 3,
		"tags":     []string{"mcp", "test"},
	})

	shortID, ok := created["short_id"].(string)
	if !ok || shortID == "" {
		t.Fatal("created task missing short_id")
	}
	if created["title"] != "MCP test task" {
		t.Fatalf("expected title 'MCP test task', got %v", created["title"])
	}
	if created["status"] != "pending" {
		t.Fatalf("expected status 'pending', got %v", created["status"])
	}

	// Get the task (rich response)
	fetched := env.callTool("tusk_task_get", map[string]any{
		"short_id": shortID,
	})
	if fetched["title"] != "MCP test task" {
		t.Fatalf("get: expected title 'MCP test task', got %v", fetched["title"])
	}
	tags, _ := fetched["tags"].([]any)
	if len(tags) != 2 {
		t.Fatalf("get: expected 2 tags, got %d", len(tags))
	}

	// Start the task
	version := created["version"].(float64)
	started := env.callTool("tusk_task_start", map[string]any{
		"short_id": shortID,
		"version":  version,
	})
	if started["status"] != "active" {
		t.Fatalf("expected status 'active', got %v", started["status"])
	}

	// Complete the task
	version = started["version"].(float64)
	completed := env.callTool("tusk_task_done", map[string]any{
		"short_id": shortID,
		"version":  version,
	})
	if completed["status"] != "completed" {
		t.Fatalf("expected status 'completed', got %v", completed["status"])
	}

	// List tasks
	listRaw := env.callToolRaw("tusk_task_list", map[string]any{
		"status": []string{"completed"},
	})
	var listed []map[string]any
	if err := json.Unmarshal([]byte(listRaw), &listed); err != nil {
		t.Fatalf("parsing list result: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected 1 completed task, got %d", len(listed))
	}
	if listed[0]["short_id"] != shortID {
		t.Fatalf("listed task short_id mismatch")
	}
}

func TestMCPTaskModify(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	created := env.callTool("tusk_task_create", map[string]any{
		"title": "Original title",
	})
	shortID := created["short_id"].(string)
	version := created["version"].(float64)

	modified := env.callTool("tusk_task_modify", map[string]any{
		"short_id": shortID,
		"version":  version,
		"title":    "Updated title",
		"priority": 2,
		"add_tags": []string{"urgent"},
	})
	if modified["title"] != "Updated title" {
		t.Fatalf("expected 'Updated title', got %v", modified["title"])
	}
	if modified["priority"].(float64) != 2 {
		t.Fatalf("expected priority 2, got %v", modified["priority"])
	}
}

func TestMCPRelations(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	task1 := env.callTool("tusk_task_create", map[string]any{"title": "Blocker"})
	task2 := env.callTool("tusk_task_create", map[string]any{"title": "Blocked"})

	shortID1 := task1["short_id"].(string)
	shortID2 := task2["short_id"].(string)

	// Add relation
	rel := env.callTool("tusk_relation_add", map[string]any{
		"source": shortID1,
		"target": shortID2,
		"type":   "blocks",
	})
	if rel["relation_type"] != "blocks" {
		t.Fatalf("expected relation_type 'blocks', got %v", rel["relation_type"])
	}

	// Verify relation in task get
	fetched := env.callTool("tusk_task_get", map[string]any{"short_id": shortID2})
	relations, _ := fetched["relations"].([]any)
	if len(relations) != 1 {
		t.Fatalf("expected 1 relation, got %d", len(relations))
	}

	// Remove relation
	env.callToolRaw("tusk_relation_remove", map[string]any{
		"source": shortID1,
		"target": shortID2,
		"type":   "blocks",
	})

	// Verify relation removed
	fetched2 := env.callTool("tusk_task_get", map[string]any{"short_id": shortID2})
	relations2, _ := fetched2["relations"].([]any)
	if len(relations2) != 0 {
		t.Fatalf("expected 0 relations after remove, got %d", len(relations2))
	}
}

func TestMCPProjects(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	// List projects (should have default)
	listRaw := env.callToolRaw("tusk_project_list", map[string]any{})
	var projects []map[string]any
	if err := json.Unmarshal([]byte(listRaw), &projects); err != nil {
		t.Fatalf("parsing project list: %v", err)
	}
	found := false
	for _, p := range projects {
		if p["id"] == "default" {
			found = true
			if p["workflow"] != "kanban" {
				t.Fatalf("expected workflow 'kanban', got %v", p["workflow"])
			}
		}
	}
	if !found {
		t.Fatal("default project not found in list")
	}
}

func TestMCPErrorHandling(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	// Not found
	errMsg := env.callToolExpectError("tusk_task_get", map[string]any{
		"short_id": "nonexistent",
	})
	if !strings.Contains(errMsg, "not found") {
		t.Fatalf("expected 'not found' error, got: %s", errMsg)
	}

	// Version conflict
	created := env.callTool("tusk_task_create", map[string]any{"title": "Conflict test"})
	shortID := created["short_id"].(string)

	errMsg = env.callToolExpectError("tusk_task_start", map[string]any{
		"short_id": shortID,
		"version":  999,
	})
	if !strings.Contains(errMsg, "version conflict") {
		t.Fatalf("expected 'version conflict' error, got: %s", errMsg)
	}

	// Cyclic blocks
	task1 := env.callTool("tusk_task_create", map[string]any{"title": "A"})
	task2 := env.callTool("tusk_task_create", map[string]any{"title": "B"})
	sid1 := task1["short_id"].(string)
	sid2 := task2["short_id"].(string)

	env.callTool("tusk_relation_add", map[string]any{
		"source": sid1, "target": sid2, "type": "blocks",
	})
	errMsg = env.callToolExpectError("tusk_relation_add", map[string]any{
		"source": sid2, "target": sid1, "type": "blocks",
	})
	if !strings.Contains(errMsg, "cycle") {
		t.Fatalf("expected 'cycle' error, got: %s", errMsg)
	}
}

func TestMCPAnnotations(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	created := env.callTool("tusk_task_create", map[string]any{"title": "Annotate me"})
	shortID := created["short_id"].(string)

	ann := env.callTool("tusk_task_annotate", map[string]any{
		"short_id": shortID,
		"body":     "This is a note",
	})
	if ann["body"] != "This is a note" {
		t.Fatalf("expected annotation body 'This is a note', got %v", ann["body"])
	}

	// Verify annotation appears in task get
	fetched := env.callTool("tusk_task_get", map[string]any{"short_id": shortID})
	annotations, _ := fetched["annotations"].([]any)
	if len(annotations) != 1 {
		t.Fatalf("expected 1 annotation, got %d", len(annotations))
	}
}

func TestMCPTaskDelete(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	created := env.callTool("tusk_task_create", map[string]any{
		"title": "Delete me",
		"tags":  []string{"doomed"},
	})
	shortID := created["short_id"].(string)
	version := created["version"].(float64)

	// Start first (pending → active), then delete (active → deleted)
	started := env.callTool("tusk_task_start", map[string]any{
		"short_id": shortID,
		"version":  version,
	})
	version = started["version"].(float64)

	// Verify start returns tags
	startTags, _ := started["tags"].([]any)
	if len(startTags) != 1 || startTags[0] != "doomed" {
		t.Fatalf("start: expected tags [doomed], got %v", startTags)
	}

	deleted := env.callTool("tusk_task_delete", map[string]any{
		"short_id": shortID,
		"version":  version,
	})
	if deleted["status"] != "deleted" {
		t.Fatalf("expected status 'deleted', got %v", deleted["status"])
	}

	// Verify delete also returns tags
	deleteTags, _ := deleted["tags"].([]any)
	if len(deleteTags) != 1 || deleteTags[0] != "doomed" {
		t.Fatalf("delete: expected tags [doomed], got %v", deleteTags)
	}

	// Verify it no longer appears in default list
	listRaw := env.callToolRaw("tusk_task_list", map[string]any{
		"status": []string{"pending", "active"},
	})
	var listed []map[string]any
	if err := json.Unmarshal([]byte(listRaw), &listed); err != nil {
		t.Fatalf("parsing list: %v", err)
	}
	for _, task := range listed {
		if task["short_id"] == shortID {
			t.Fatal("deleted task should not appear in pending/active list")
		}
	}
}

func TestMCPResources(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	// Create a task with tags and an annotation
	created := env.callTool("tusk_task_create", map[string]any{
		"title": "Resource test task",
		"tags":  []string{"res-tag"},
	})
	shortID := created["short_id"].(string)

	env.callTool("tusk_task_annotate", map[string]any{
		"short_id": shortID,
		"body":     "resource annotation",
	})

	// Read task resource
	resp := env.send("resources/read", map[string]any{
		"uri": "tusk://tasks/" + shortID,
	})
	if resp.Error != nil {
		t.Fatalf("task resource error: %s", resp.Error)
	}
	var taskRes struct {
		Contents []struct {
			URI      string `json:"uri"`
			MIMEType string `json:"mimeType"`
			Text     string `json:"text"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(resp.Result, &taskRes); err != nil {
		t.Fatalf("parsing task resource: %v", err)
	}
	if len(taskRes.Contents) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(taskRes.Contents))
	}
	if taskRes.Contents[0].MIMEType != "application/json" {
		t.Fatalf("expected application/json, got %s", taskRes.Contents[0].MIMEType)
	}
	var taskData map[string]any
	if err := json.Unmarshal([]byte(taskRes.Contents[0].Text), &taskData); err != nil {
		t.Fatalf("parsing task resource JSON: %v", err)
	}
	if taskData["title"] != "Resource test task" {
		t.Fatalf("expected title 'Resource test task', got %v", taskData["title"])
	}
	annotations, _ := taskData["annotations"].([]any)
	if len(annotations) != 1 {
		t.Fatalf("expected 1 annotation in resource, got %d", len(annotations))
	}
	tags, _ := taskData["tags"].([]any)
	if len(tags) != 1 || tags[0] != "res-tag" {
		t.Fatalf("expected tags [res-tag], got %v", tags)
	}

	// Read project resource
	resp = env.send("resources/read", map[string]any{
		"uri": "tusk://projects/default",
	})
	if resp.Error != nil {
		t.Fatalf("project resource error: %s", resp.Error)
	}
	var projRes struct {
		Contents []struct {
			Text string `json:"text"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(resp.Result, &projRes); err != nil {
		t.Fatalf("parsing project resource: %v", err)
	}
	var projData map[string]any
	if err := json.Unmarshal([]byte(projRes.Contents[0].Text), &projData); err != nil {
		t.Fatalf("parsing project JSON: %v", err)
	}
	if projData["id"] != "default" {
		t.Fatalf("expected project id 'default', got %v", projData["id"])
	}

	// Read workflow resource
	resp = env.send("resources/read", map[string]any{
		"uri": "tusk://projects/default/workflow",
	})
	if resp.Error != nil {
		t.Fatalf("workflow resource error: %s", resp.Error)
	}
	var wfRes struct {
		Contents []struct {
			Text string `json:"text"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(resp.Result, &wfRes); err != nil {
		t.Fatalf("parsing workflow resource: %v", err)
	}
	var wfData map[string]any
	if err := json.Unmarshal([]byte(wfRes.Contents[0].Text), &wfData); err != nil {
		t.Fatalf("parsing workflow JSON: %v", err)
	}
	statuses, _ := wfData["statuses"].([]any)
	if len(statuses) == 0 {
		t.Fatal("workflow resource returned no statuses")
	}
	transitions, _ := wfData["transitions"].([]any)
	if len(transitions) == 0 {
		t.Fatal("workflow resource returned no transitions")
	}
}

func TestMCPTree(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	parent := env.callTool("tusk_task_create", map[string]any{"title": "Parent"})
	parentSID := parent["short_id"].(string)

	env.callTool("tusk_task_create", map[string]any{
		"title":  "Child 1",
		"parent": parentSID,
	})
	env.callTool("tusk_task_create", map[string]any{
		"title":  "Child 2",
		"parent": parentSID,
	})

	treeRaw := env.callToolRaw("tusk_task_tree", map[string]any{
		"short_id": parentSID,
	})
	var tree []map[string]any
	if err := json.Unmarshal([]byte(treeRaw), &tree); err != nil {
		t.Fatalf("parsing tree: %v", err)
	}
	if len(tree) != 1 {
		t.Fatalf("expected 1 root node, got %d", len(tree))
	}
	children, _ := tree[0]["children"].([]any)
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(children))
	}
}
