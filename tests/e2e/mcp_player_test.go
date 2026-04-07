package e2e

import (
	"testing"
)

func TestMCPPlayerRegister(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	result := env.callTool("tusk_player_register", map[string]any{
		"player_id": "mcp-agent-1",
	})
	if result["id"] != "mcp-agent-1" {
		t.Fatalf("expected id 'mcp-agent-1', got %v", result["id"])
	}
	if result["type"] != "agent" {
		t.Fatalf("expected type 'agent', got %v", result["type"])
	}
	if result["registered_at"] == nil {
		t.Fatal("expected registered_at to be set")
	}

	// Duplicate registration should fail
	errMsg := env.callToolExpectError("tusk_player_register", map[string]any{
		"player_id": "mcp-agent-1",
	})
	if errMsg == "" {
		t.Fatal("expected error on duplicate registration")
	}
}

func TestMCPTaskClaim(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	// Register player and create task
	env.callTool("tusk_player_register", map[string]any{"player_id": "claimer-1"})
	created := env.callTool("tusk_task_create", map[string]any{"title": "MCP claim test"})
	shortID := created["short_id"].(string)
	version := created["version"].(float64)

	// Claim the task
	claimed := env.callTool("tusk_task_claim", map[string]any{
		"short_id":  shortID,
		"player_id": "claimer-1",
		"version":   version,
	})
	if claimed["claimed_by"] != "claimer-1" {
		t.Fatalf("expected claimed_by 'claimer-1', got %v", claimed["claimed_by"])
	}
	if claimed["claimed_at"] == nil {
		t.Fatal("expected claimed_at to be set")
	}
}

func TestMCPTaskRelease(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	env.callTool("tusk_player_register", map[string]any{"player_id": "releaser-1"})
	created := env.callTool("tusk_task_create", map[string]any{"title": "MCP release test"})
	shortID := created["short_id"].(string)
	version := created["version"].(float64)

	// Claim then release
	claimed := env.callTool("tusk_task_claim", map[string]any{
		"short_id":  shortID,
		"player_id": "releaser-1",
		"version":   version,
	})
	claimedVersion := claimed["version"].(float64)

	released := env.callTool("tusk_task_release", map[string]any{
		"short_id":  shortID,
		"player_id": "releaser-1",
		"version":   claimedVersion,
	})
	if released["claimed_by"] != nil {
		t.Fatalf("expected claimed_by nil after release, got %v", released["claimed_by"])
	}
	if released["claimed_at"] != nil {
		t.Fatalf("expected claimed_at nil after release, got %v", released["claimed_at"])
	}
}

func TestMCPTaskStartWithPlayer(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	// Create task — do NOT pre-register player (auto-register should handle it)
	created := env.callTool("tusk_task_create", map[string]any{"title": "MCP start auto-claim"})
	shortID := created["short_id"].(string)
	version := created["version"].(float64)

	// Start with player_id — should auto-register as agent and auto-claim
	started := env.callTool("tusk_task_start", map[string]any{
		"short_id":  shortID,
		"version":   version,
		"player_id": "auto-agent",
	})
	if started["status"] != "active" {
		t.Fatalf("expected status 'active', got %v", started["status"])
	}
	if started["claimed_by"] != "auto-agent" {
		t.Fatalf("expected claimed_by 'auto-agent', got %v", started["claimed_by"])
	}
}

func TestMCPTaskClaimAlreadyClaimed(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	env.callTool("tusk_player_register", map[string]any{"player_id": "first"})
	env.callTool("tusk_player_register", map[string]any{"player_id": "second"})
	created := env.callTool("tusk_task_create", map[string]any{"title": "MCP contested"})
	shortID := created["short_id"].(string)
	version := created["version"].(float64)

	// First player claims
	claimed := env.callTool("tusk_task_claim", map[string]any{
		"short_id":  shortID,
		"player_id": "first",
		"version":   version,
	})
	claimedVersion := claimed["version"].(float64)

	// Second player tries to claim — should fail
	errMsg := env.callToolExpectError("tusk_task_claim", map[string]any{
		"short_id":  shortID,
		"player_id": "second",
		"version":   claimedVersion,
	})
	if errMsg == "" {
		t.Fatal("expected error when second player claims")
	}
}

func TestMCPReadToolLiveness(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	// Register player
	env.callTool("tusk_player_register", map[string]any{"player_id": "liveness-agent"})
	created := env.callTool("tusk_task_create", map[string]any{"title": "Liveness test"})
	shortID := created["short_id"].(string)

	// Call tusk_task_get with player_id — should not error
	fetched := env.callTool("tusk_task_get", map[string]any{
		"short_id":  shortID,
		"player_id": "liveness-agent",
	})
	if fetched["title"] != "Liveness test" {
		t.Fatalf("expected title 'Liveness test', got %v", fetched["title"])
	}
}
