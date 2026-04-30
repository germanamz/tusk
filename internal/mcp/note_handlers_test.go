package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/germanamz/tusk/service"
)

// seedNotePlayers registers alice-agent and bob-agent for cross-player
// and non-author assertions.
func seedNotePlayers(test *testing.T, server *Server) {
	test.Helper()
	ctx := context.Background()

	_, err := server.playerSvc.Register(ctx, "alice-agent", "agent")

	if err != nil {
		test.Fatalf("seed alice-agent: %v", err)
	}

	_, err = server.playerSvc.Register(ctx, "bob-agent", "agent")

	if err != nil {
		test.Fatalf("seed bob-agent: %v", err)
	}
}

func TestHandleNoteAdd(test *testing.T) {
	test.Run("project-level note", func(test *testing.T) {
		server := testServer(test)
		ctx := context.Background()
		seedNotePlayers(test, server)

		result, err := server.handleNoteAdd(ctx, callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"body":      "project-level body",
		}))

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		parsed := parseToolResult(test, result)
		if parsed["body"] != "project-level body" {
			test.Fatalf("expected body 'project-level body', got %v", parsed["body"])
		}
		if parsed["player_id"] != "alice-agent" {
			test.Fatalf("expected player_id 'alice-agent', got %v", parsed["player_id"])
		}
		if _, has := parsed["task_short_id"]; has {
			test.Fatalf("expected no task_short_id for project-level note, got %v", parsed["task_short_id"])
		}
	})

	test.Run("task-level note", func(test *testing.T) {
		server := testServer(test)
		ctx := context.Background()
		seedNotePlayers(test, server)

		createRes, _ := server.handleTaskCreate(ctx, callToolRequest(map[string]any{"title": "Root"}))
		created := parseToolResult(test, createRes)
		shortID := created["short_id"].(string)

		result, err := server.handleNoteAdd(ctx, callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"body":      "task-level body",
			"task":      shortID,
		}))

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		parsed := parseToolResult(test, result)
		if parsed["task_short_id"] != shortID {
			test.Fatalf("expected task_short_id %q, got %v", shortID, parsed["task_short_id"])
		}
	})

	test.Run("missing player_id", func(test *testing.T) {
		server := testServer(test)
		result, err := server.handleNoteAdd(context.Background(), callToolRequest(map[string]any{
			"body": "body only",
		}))

		if err != nil {
			test.Fatalf("unexpected transport error: %v", err)
		}

		if msg := getToolErrorText(test, result); msg != "player_id is required" {
			test.Fatalf("expected 'player_id is required', got %q", msg)
		}
	})

	test.Run("missing body", func(test *testing.T) {
		server := testServer(test)
		result, err := server.handleNoteAdd(context.Background(), callToolRequest(map[string]any{
			"player_id": "alice-agent",
		}))

		if err != nil {
			test.Fatalf("unexpected transport error: %v", err)
		}

		if msg := getToolErrorText(test, result); msg != "body is required" {
			test.Fatalf("expected 'body is required', got %q", msg)
		}
	})

	test.Run("unknown project", func(test *testing.T) {
		server := testServer(test)
		seedNotePlayers(test, server)
		result, _ := server.handleNoteAdd(context.Background(), callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"body":      "b",
			"project":   "ghost",
		}))
		if !result.IsError {
			test.Fatal("expected error for unknown project")
		}
	})

	test.Run("unknown task short_id", func(test *testing.T) {
		server := testServer(test)
		seedNotePlayers(test, server)
		result, _ := server.handleNoteAdd(context.Background(), callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"body":      "b",
			"task":      "deadbeef",
		}))
		if !result.IsError {
			test.Fatal("expected error for unknown task short_id")
		}
	})

	test.Run("task in different project", func(test *testing.T) {
		server := testServer(test)
		ctx := context.Background()
		seedNotePlayers(test, server)

		// Seed a second project bound to the builtin workflow.
		workflow, workflowErr := server.workflowSvc.GetByName(ctx, "kanban")

		if workflowErr != nil {
			test.Fatalf("resolving kanban workflow: %v", workflowErr)
		}

		_, projectErr := server.projectSvc.Create(ctx, service.CreateProjectInput{
			Name:       "alt",
			WorkflowID: workflow.ID,
		})

		if projectErr != nil {
			test.Fatalf("creating alt project: %v", projectErr)
		}

		// Task in default project.
		createRes, _ := server.handleTaskCreate(ctx, callToolRequest(map[string]any{"title": "Default-project task"}))
		created := parseToolResult(test, createRes)
		shortID := created["short_id"].(string)

		// Pass task short_id but point project param to the alt project.
		result, _ := server.handleNoteAdd(ctx, callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"body":      "mismatch",
			"project":   "alt",
			"task":      shortID,
		}))
		if !result.IsError {
			test.Fatal("expected error when task belongs to a different project")
		}
	})

	test.Run("auto-registers new player", func(test *testing.T) {
		server := testServer(test)
		ctx := context.Background()

		result, err := server.handleNoteAdd(ctx, callToolRequest(map[string]any{
			"player_id": "fresh-agent",
			"body":      "first note",
		}))

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		if result.IsError {
			test.Fatalf("unexpected tool error: %s", getToolErrorText(test, result))
		}

		player, playerErr := server.playerSvc.GetByID(ctx, "fresh-agent")

		if playerErr != nil {
			test.Fatalf("expected player row for fresh-agent: %v", playerErr)
		}

		if player.Type != "agent" {
			test.Fatalf("expected type 'agent', got %q", player.Type)
		}
	})

	test.Run("metadata pass-through", func(test *testing.T) {
		server := testServer(test)
		ctx := context.Background()
		seedNotePlayers(test, server)

		result, err := server.handleNoteAdd(ctx, callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"body":      "with meta",
			"metadata": map[string]any{
				"topic": "auth",
				"ref":   "PR-42",
			},
		}))

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		parsed := parseToolResult(test, result)
		meta, ok := parsed["metadata"].(map[string]any)
		if !ok {
			test.Fatalf("expected metadata map, got %T", parsed["metadata"])
		}
		if meta["topic"] != "auth" || meta["ref"] != "PR-42" {
			test.Fatalf("metadata not round-tripped: %v", meta)
		}
	})
}

func TestHandleNoteArchive(test *testing.T) {
	test.Run("author archives own note", func(test *testing.T) {
		server := testServer(test)
		ctx := context.Background()
		seedNotePlayers(test, server)

		addRes, _ := server.handleNoteAdd(ctx, callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"body":      "archive me",
		}))
		created := parseToolResult(test, addRes)
		id := created["id"].(string)

		result, err := server.handleNoteArchive(ctx, callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"id":        id,
		}))

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		parsed := parseToolResult(test, result)
		if parsed["archived_at"] == nil || parsed["archived_at"] == "" {
			test.Fatalf("expected archived_at set, got %v", parsed["archived_at"])
		}
	})

	test.Run("invalid UUID", func(test *testing.T) {
		server := testServer(test)
		seedNotePlayers(test, server)
		result, _ := server.handleNoteArchive(context.Background(), callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"id":        "not-a-uuid",
		}))
		msg := getToolErrorText(test, result)
		if msg != "invalid note id, expected full UUID" {
			test.Fatalf("expected invalid-UUID error, got %q", msg)
		}
	})

	test.Run("unknown UUID", func(test *testing.T) {
		server := testServer(test)
		seedNotePlayers(test, server)
		result, _ := server.handleNoteArchive(context.Background(), callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"id":        "00000000-0000-0000-0000-000000000001",
		}))
		msg := getToolErrorText(test, result)
		if msg == "" || msg[:9] != "not found" {
			test.Fatalf("expected not-found error, got %q", msg)
		}
	})

	test.Run("non-author forbidden", func(test *testing.T) {
		server := testServer(test)
		ctx := context.Background()
		seedNotePlayers(test, server)

		addRes, _ := server.handleNoteAdd(ctx, callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"body":      "alice's note",
		}))
		created := parseToolResult(test, addRes)
		id := created["id"].(string)

		result, _ := server.handleNoteArchive(ctx, callToolRequest(map[string]any{
			"player_id": "bob-agent",
			"id":        id,
		}))
		msg := getToolErrorText(test, result)
		if len(msg) < 9 || msg[:9] != "forbidden" {
			test.Fatalf("expected forbidden error, got %q", msg)
		}
	})
}

func TestHandleNoteList(test *testing.T) {
	test.Run("default caller-only listing", func(test *testing.T) {
		server := testServer(test)
		ctx := context.Background()
		seedNotePlayers(test, server)

		server.handleNoteAdd(ctx, callToolRequest(map[string]any{"player_id": "alice-agent", "body": "alice-1"}))
		server.handleNoteAdd(ctx, callToolRequest(map[string]any{"player_id": "bob-agent", "body": "bob-1"}))

		result, err := server.handleNoteList(ctx, callToolRequest(map[string]any{"player_id": "alice-agent"}))

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		items := parseToolResultArray(test, result)
		if len(items) != 1 || items[0]["body"] != "alice-1" {
			test.Fatalf("expected only alice's note, got %v", items)
		}
	})

	test.Run("target_player_id scopes correctly", func(test *testing.T) {
		server := testServer(test)
		ctx := context.Background()
		seedNotePlayers(test, server)

		server.handleNoteAdd(ctx, callToolRequest(map[string]any{"player_id": "alice-agent", "body": "alice-1"}))
		server.handleNoteAdd(ctx, callToolRequest(map[string]any{"player_id": "bob-agent", "body": "bob-1"}))

		result, _ := server.handleNoteList(ctx, callToolRequest(map[string]any{
			"player_id":        "alice-agent",
			"target_player_id": "bob-agent",
		}))
		items := parseToolResultArray(test, result)
		if len(items) != 1 || items[0]["body"] != "bob-1" {
			test.Fatalf("expected only bob's note, got %v", items)
		}
	})

	test.Run("all_players returns every note", func(test *testing.T) {
		server := testServer(test)
		ctx := context.Background()
		seedNotePlayers(test, server)

		server.handleNoteAdd(ctx, callToolRequest(map[string]any{"player_id": "alice-agent", "body": "alice-1"}))
		server.handleNoteAdd(ctx, callToolRequest(map[string]any{"player_id": "bob-agent", "body": "bob-1"}))

		result, _ := server.handleNoteList(ctx, callToolRequest(map[string]any{
			"player_id":   "alice-agent",
			"all_players": true,
		}))
		items := parseToolResultArray(test, result)
		if len(items) != 2 {
			test.Fatalf("expected 2 notes, got %d", len(items))
		}
	})

	test.Run("all_players with target_player_id errors", func(test *testing.T) {
		server := testServer(test)
		seedNotePlayers(test, server)
		result, _ := server.handleNoteList(context.Background(), callToolRequest(map[string]any{
			"player_id":        "alice-agent",
			"all_players":      true,
			"target_player_id": "bob-agent",
		}))
		msg := getToolErrorText(test, result)
		if msg != "all_players cannot be combined with target_player_id" {
			test.Fatalf("expected mutual-exclusion error, got %q", msg)
		}
	})

	test.Run("window override respected", func(test *testing.T) {
		server := testServer(test)
		ctx := context.Background()
		seedNotePlayers(test, server)

		for range 5 {
			server.handleNoteAdd(ctx, callToolRequest(map[string]any{
				"player_id": "alice-agent",
				"body":      "note",
			}))
		}
		result, _ := server.handleNoteList(ctx, callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"window":    float64(2),
		}))
		items := parseToolResultArray(test, result)
		if len(items) != 2 {
			test.Fatalf("expected 2 notes under window=2, got %d", len(items))
		}
	})

	test.Run("since filter", func(test *testing.T) {
		server := testServer(test)
		ctx := context.Background()
		seedNotePlayers(test, server)

		server.handleNoteAdd(ctx, callToolRequest(map[string]any{"player_id": "alice-agent", "body": "old"}))
		cutoff := time.Now().UTC().Add(time.Second)
		time.Sleep(1100 * time.Millisecond)
		server.handleNoteAdd(ctx, callToolRequest(map[string]any{"player_id": "alice-agent", "body": "new"}))

		result, _ := server.handleNoteList(ctx, callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"since":     cutoff.Format(time.RFC3339),
		}))
		items := parseToolResultArray(test, result)
		if len(items) != 1 || items[0]["body"] != "new" {
			test.Fatalf("expected only 'new' note after since cutoff, got %v", items)
		}
	})

	test.Run("invalid since errors", func(test *testing.T) {
		server := testServer(test)
		seedNotePlayers(test, server)
		result, _ := server.handleNoteList(context.Background(), callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"since":     "yesterday",
		}))
		msg := getToolErrorText(test, result)
		if msg != "invalid since format, expected ISO 8601 (RFC3339)" {
			test.Fatalf("expected parse error, got %q", msg)
		}
	})

	test.Run("include_archived", func(test *testing.T) {
		server := testServer(test)
		ctx := context.Background()
		seedNotePlayers(test, server)

		addRes, _ := server.handleNoteAdd(ctx, callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"body":      "to archive",
		}))
		id := parseToolResult(test, addRes)["id"].(string)
		server.handleNoteArchive(ctx, callToolRequest(map[string]any{"player_id": "alice-agent", "id": id}))

		without, _ := server.handleNoteList(ctx, callToolRequest(map[string]any{"player_id": "alice-agent"}))
		if items := parseToolResultArray(test, without); len(items) != 0 {
			test.Fatalf("expected archived note hidden by default, got %d", len(items))
		}

		with, _ := server.handleNoteList(ctx, callToolRequest(map[string]any{
			"player_id":        "alice-agent",
			"include_archived": true,
		}))
		if items := parseToolResultArray(test, with); len(items) != 1 {
			test.Fatalf("expected archived note with include_archived, got %d", len(items))
		}
	})

	test.Run("task short_id filter", func(test *testing.T) {
		server := testServer(test)
		ctx := context.Background()
		seedNotePlayers(test, server)

		createRes, _ := server.handleTaskCreate(ctx, callToolRequest(map[string]any{"title": "T"}))
		shortID := parseToolResult(test, createRes)["short_id"].(string)

		server.handleNoteAdd(ctx, callToolRequest(map[string]any{"player_id": "alice-agent", "body": "project-level"}))
		server.handleNoteAdd(ctx, callToolRequest(map[string]any{"player_id": "alice-agent", "body": "task-level", "task": shortID}))

		result, _ := server.handleNoteList(ctx, callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"task":      shortID,
		}))
		items := parseToolResultArray(test, result)
		if len(items) != 1 || items[0]["body"] != "task-level" {
			test.Fatalf("expected only task-level note, got %v", items)
		}
	})

	test.Run("empty result returns empty array", func(test *testing.T) {
		server := testServer(test)
		seedNotePlayers(test, server)
		result, err := server.handleNoteList(context.Background(), callToolRequest(map[string]any{
			"player_id": "alice-agent",
		}))

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		items := parseToolResultArray(test, result)
		if len(items) != 0 {
			test.Fatalf("expected empty array, got %d", len(items))
		}
	})
}
