package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPTaskAvailable(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	env := NewMCPEnv(test, binPath)

	// Create 3 tasks with different priorities
	env.callTool("tusk_task_create", map[string]any{"title": "avail-low", "priority": 1})
	env.callTool("tusk_task_create", map[string]any{"title": "avail-med", "priority": 2})
	highTask := env.callTool("tusk_task_create", map[string]any{"title": "avail-high", "priority": 3})

	// Claim one task so it is no longer available
	env.callTool("tusk_player_register", map[string]any{"player_id": "claimer-avail"})
	env.callTool("tusk_task_claim", map[string]any{
		"short_id":  highTask["short_id"].(string),
		"player_id": "claimer-avail",
		"version":   highTask["version"].(float64),
	})

	// Query available tasks (auto-registers avail-agent)
	raw := env.callToolRaw("tusk_task_available", map[string]any{"player_id": "avail-agent"})
	var items []any
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		test.Fatalf("parsing available results: %v", err)
	}
	if len(items) != 2 {
		test.Fatalf("expected 2 available tasks, got %d", len(items))
	}
}

func TestMCPTaskAvailableBlocked(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	env := NewMCPEnv(test, binPath)

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
		test.Fatalf("parsing available results: %v", err)
	}
	if len(items) != 1 {
		test.Fatalf("expected 1 available task, got %d", len(items))
	}

	task := items[0].(map[string]any)
	if task["title"] != "blocker-task" {
		test.Fatalf("expected available task to be 'blocker-task', got %v", task["title"])
	}
}

func TestMCPTaskPop(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	env := NewMCPEnv(test, binPath)

	// Create two tasks with different priorities
	lowTask := env.callTool("tusk_task_create", map[string]any{"title": "pop-low", "priority": 1})
	highTask := env.callTool("tusk_task_create", map[string]any{"title": "pop-high", "priority": 3})

	// Pop should return the highest-urgency task
	popped := env.callTool("tusk_task_pop", map[string]any{"player_id": "pop-agent"})

	if popped["short_id"] != highTask["short_id"].(string) {
		test.Fatalf("expected popped task to be high-priority (%s), got %v (low=%s)",
			highTask["short_id"], popped["short_id"], lowTask["short_id"])
	}
	if popped["status"] != "active" {
		test.Fatalf("expected status 'active', got %v", popped["status"])
	}
	if popped["claimed_by"] != "pop-agent" {
		test.Fatalf("expected claimed_by 'pop-agent', got %v", popped["claimed_by"])
	}
}

func TestMCPTaskPopEmpty(test *testing.T) {
	if binPath == "" {
		test.Skip("binary not built")
	}
	env := NewMCPEnv(test, binPath)

	// No tasks exist — pop should return informational text, not an error
	raw := env.callToolRaw("tusk_task_pop", map[string]any{"player_id": "empty-agent"})
	if !strings.Contains(raw, "No available tasks matching the given filters") {
		test.Fatalf("expected 'No available tasks' message, got: %s", raw)
	}
}
