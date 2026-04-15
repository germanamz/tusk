package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPTaskAvailable(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	// Create 3 tasks with different priorities
	env.callTool("tusk_task_create", map[string]any{"title": "avail-low", "priority": 1})
	env.callTool("tusk_task_create", map[string]any{"title": "avail-med", "priority": 2})
	t3 := env.callTool("tusk_task_create", map[string]any{"title": "avail-high", "priority": 3})

	// Claim one task so it is no longer available
	env.callTool("tusk_player_register", map[string]any{"player_id": "claimer-avail"})
	env.callTool("tusk_task_claim", map[string]any{
		"short_id":  t3["short_id"].(string),
		"player_id": "claimer-avail",
		"version":   t3["version"].(float64),
	})

	// Query available tasks (auto-registers avail-agent)
	raw := env.callToolRaw("tusk_task_available", map[string]any{"player_id": "avail-agent"})
	var items []any
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		t.Fatalf("parsing available results: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 available tasks, got %d", len(items))
	}
}

func TestMCPTaskAvailableBlocked(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	// Create task A and task B
	taskA := env.callTool("tusk_task_create", map[string]any{"title": "blocker-task"})
	taskB := env.callTool("tusk_task_create", map[string]any{"title": "blocked-task"})

	// A blocks B
	env.callTool("tusk_task_link", map[string]any{
		"source": taskA["short_id"].(string),
		"target": taskB["short_id"].(string),
		"type":   "blocks",
	})

	// Query available tasks — B should be excluded because it is blocked
	raw := env.callToolRaw("tusk_task_available", map[string]any{"player_id": "block-agent"})
	var items []any
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		t.Fatalf("parsing available results: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 available task, got %d", len(items))
	}

	task := items[0].(map[string]any)
	if task["title"] != "blocker-task" {
		t.Fatalf("expected available task to be 'blocker-task', got %v", task["title"])
	}
}

func TestMCPTaskPop(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	// Create two tasks with different priorities
	low := env.callTool("tusk_task_create", map[string]any{"title": "pop-low", "priority": 1})
	high := env.callTool("tusk_task_create", map[string]any{"title": "pop-high", "priority": 3})

	// Pop should return the highest-urgency task
	popped := env.callTool("tusk_task_pop", map[string]any{"player_id": "pop-agent"})

	if popped["short_id"] != high["short_id"].(string) {
		t.Fatalf("expected popped task to be high-priority (%s), got %v (low=%s)",
			high["short_id"], popped["short_id"], low["short_id"])
	}
	if popped["status"] != "active" {
		t.Fatalf("expected status 'active', got %v", popped["status"])
	}
	if popped["claimed_by"] != "pop-agent" {
		t.Fatalf("expected claimed_by 'pop-agent', got %v", popped["claimed_by"])
	}
}

func TestMCPTaskPopEmpty(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	// No tasks exist — pop should return informational text, not an error
	raw := env.callToolRaw("tusk_task_pop", map[string]any{"player_id": "empty-agent"})
	if !strings.Contains(raw, "No available tasks matching the given filters") {
		t.Fatalf("expected 'No available tasks' message, got: %s", raw)
	}
}
