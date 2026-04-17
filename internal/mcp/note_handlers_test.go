package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/germanamz/tusk/service"
)

// seedNotePlayers registers alice-agent and bob-agent for cross-player
// and non-author assertions.
func seedNotePlayers(t *testing.T, s *Server) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.playerSvc.Register(ctx, "alice-agent", "agent"); err != nil {
		t.Fatalf("seed alice-agent: %v", err)
	}
	if _, err := s.playerSvc.Register(ctx, "bob-agent", "agent"); err != nil {
		t.Fatalf("seed bob-agent: %v", err)
	}
}

func TestHandleNoteAdd(t *testing.T) {
	t.Run("project-level note", func(t *testing.T) {
		s := testServer(t)
		ctx := context.Background()
		seedNotePlayers(t, s)

		result, err := s.handleNoteAdd(ctx, callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"body":      "project-level body",
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		parsed := parseToolResult(t, result)
		if parsed["body"] != "project-level body" {
			t.Fatalf("expected body 'project-level body', got %v", parsed["body"])
		}
		if parsed["player_id"] != "alice-agent" {
			t.Fatalf("expected player_id 'alice-agent', got %v", parsed["player_id"])
		}
		if _, has := parsed["task_short_id"]; has {
			t.Fatalf("expected no task_short_id for project-level note, got %v", parsed["task_short_id"])
		}
	})

	t.Run("task-level note", func(t *testing.T) {
		s := testServer(t)
		ctx := context.Background()
		seedNotePlayers(t, s)

		createRes, _ := s.handleTaskCreate(ctx, callToolRequest(map[string]any{"title": "Root"}))
		created := parseToolResult(t, createRes)
		shortID := created["short_id"].(string)

		result, err := s.handleNoteAdd(ctx, callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"body":      "task-level body",
			"task":      shortID,
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		parsed := parseToolResult(t, result)
		if parsed["task_short_id"] != shortID {
			t.Fatalf("expected task_short_id %q, got %v", shortID, parsed["task_short_id"])
		}
	})

	t.Run("missing player_id", func(t *testing.T) {
		s := testServer(t)
		result, err := s.handleNoteAdd(context.Background(), callToolRequest(map[string]any{
			"body": "body only",
		}))
		if err != nil {
			t.Fatalf("unexpected transport error: %v", err)
		}
		if msg := getToolErrorText(t, result); msg != "player_id is required" {
			t.Fatalf("expected 'player_id is required', got %q", msg)
		}
	})

	t.Run("missing body", func(t *testing.T) {
		s := testServer(t)
		result, err := s.handleNoteAdd(context.Background(), callToolRequest(map[string]any{
			"player_id": "alice-agent",
		}))
		if err != nil {
			t.Fatalf("unexpected transport error: %v", err)
		}
		if msg := getToolErrorText(t, result); msg != "body is required" {
			t.Fatalf("expected 'body is required', got %q", msg)
		}
	})

	t.Run("unknown project", func(t *testing.T) {
		s := testServer(t)
		seedNotePlayers(t, s)
		result, _ := s.handleNoteAdd(context.Background(), callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"body":      "b",
			"project":   "ghost",
		}))
		if !result.IsError {
			t.Fatal("expected error for unknown project")
		}
	})

	t.Run("unknown task short_id", func(t *testing.T) {
		s := testServer(t)
		seedNotePlayers(t, s)
		result, _ := s.handleNoteAdd(context.Background(), callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"body":      "b",
			"task":      "deadbeef",
		}))
		if !result.IsError {
			t.Fatal("expected error for unknown task short_id")
		}
	})

	t.Run("task in different project", func(t *testing.T) {
		s := testServer(t)
		ctx := context.Background()
		seedNotePlayers(t, s)

		// Seed a second project bound to the builtin workflow.
		wf, err := s.workflowSvc.GetByName(ctx, "kanban")
		if err != nil {
			t.Fatalf("resolving kanban workflow: %v", err)
		}
		_, err = s.projectSvc.Create(ctx, service.CreateProjectInput{
			Name:       "alt",
			WorkflowID: wf.ID,
		})
		if err != nil {
			t.Fatalf("creating alt project: %v", err)
		}

		// Task in default project.
		createRes, _ := s.handleTaskCreate(ctx, callToolRequest(map[string]any{"title": "Default-project task"}))
		created := parseToolResult(t, createRes)
		shortID := created["short_id"].(string)

		// Pass task short_id but point project param to the alt project.
		result, _ := s.handleNoteAdd(ctx, callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"body":      "mismatch",
			"project":   "alt",
			"task":      shortID,
		}))
		if !result.IsError {
			t.Fatal("expected error when task belongs to a different project")
		}
	})

	t.Run("auto-registers new player", func(t *testing.T) {
		s := testServer(t)
		ctx := context.Background()
		result, err := s.handleNoteAdd(ctx, callToolRequest(map[string]any{
			"player_id": "fresh-agent",
			"body":      "first note",
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.IsError {
			t.Fatalf("unexpected tool error: %s", getToolErrorText(t, result))
		}
		p, err := s.playerSvc.GetByID(ctx, "fresh-agent")
		if err != nil {
			t.Fatalf("expected player row for fresh-agent: %v", err)
		}
		if p.Type != "agent" {
			t.Fatalf("expected type 'agent', got %q", p.Type)
		}
	})

	t.Run("metadata pass-through", func(t *testing.T) {
		s := testServer(t)
		ctx := context.Background()
		seedNotePlayers(t, s)

		result, err := s.handleNoteAdd(ctx, callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"body":      "with meta",
			"metadata": map[string]any{
				"topic": "auth",
				"ref":   "PR-42",
			},
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		parsed := parseToolResult(t, result)
		meta, ok := parsed["metadata"].(map[string]any)
		if !ok {
			t.Fatalf("expected metadata map, got %T", parsed["metadata"])
		}
		if meta["topic"] != "auth" || meta["ref"] != "PR-42" {
			t.Fatalf("metadata not round-tripped: %v", meta)
		}
	})
}

func TestHandleNoteArchive(t *testing.T) {
	t.Run("author archives own note", func(t *testing.T) {
		s := testServer(t)
		ctx := context.Background()
		seedNotePlayers(t, s)

		addRes, _ := s.handleNoteAdd(ctx, callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"body":      "archive me",
		}))
		created := parseToolResult(t, addRes)
		id := created["id"].(string)

		result, err := s.handleNoteArchive(ctx, callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"id":        id,
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		parsed := parseToolResult(t, result)
		if parsed["archived_at"] == nil || parsed["archived_at"] == "" {
			t.Fatalf("expected archived_at set, got %v", parsed["archived_at"])
		}
	})

	t.Run("invalid UUID", func(t *testing.T) {
		s := testServer(t)
		seedNotePlayers(t, s)
		result, _ := s.handleNoteArchive(context.Background(), callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"id":        "not-a-uuid",
		}))
		msg := getToolErrorText(t, result)
		if msg != "invalid note id, expected full UUID" {
			t.Fatalf("expected invalid-UUID error, got %q", msg)
		}
	})

	t.Run("unknown UUID", func(t *testing.T) {
		s := testServer(t)
		seedNotePlayers(t, s)
		result, _ := s.handleNoteArchive(context.Background(), callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"id":        "00000000-0000-0000-0000-000000000001",
		}))
		msg := getToolErrorText(t, result)
		if msg == "" || msg[:9] != "not found" {
			t.Fatalf("expected not-found error, got %q", msg)
		}
	})

	t.Run("non-author forbidden", func(t *testing.T) {
		s := testServer(t)
		ctx := context.Background()
		seedNotePlayers(t, s)

		addRes, _ := s.handleNoteAdd(ctx, callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"body":      "alice's note",
		}))
		created := parseToolResult(t, addRes)
		id := created["id"].(string)

		result, _ := s.handleNoteArchive(ctx, callToolRequest(map[string]any{
			"player_id": "bob-agent",
			"id":        id,
		}))
		msg := getToolErrorText(t, result)
		if len(msg) < 9 || msg[:9] != "forbidden" {
			t.Fatalf("expected forbidden error, got %q", msg)
		}
	})
}

func TestHandleNoteList(t *testing.T) {
	t.Run("default caller-only listing", func(t *testing.T) {
		s := testServer(t)
		ctx := context.Background()
		seedNotePlayers(t, s)

		s.handleNoteAdd(ctx, callToolRequest(map[string]any{"player_id": "alice-agent", "body": "alice-1"}))
		s.handleNoteAdd(ctx, callToolRequest(map[string]any{"player_id": "bob-agent", "body": "bob-1"}))

		result, err := s.handleNoteList(ctx, callToolRequest(map[string]any{"player_id": "alice-agent"}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		items := parseToolResultArray(t, result)
		if len(items) != 1 || items[0]["body"] != "alice-1" {
			t.Fatalf("expected only alice's note, got %v", items)
		}
	})

	t.Run("target_player_id scopes correctly", func(t *testing.T) {
		s := testServer(t)
		ctx := context.Background()
		seedNotePlayers(t, s)

		s.handleNoteAdd(ctx, callToolRequest(map[string]any{"player_id": "alice-agent", "body": "alice-1"}))
		s.handleNoteAdd(ctx, callToolRequest(map[string]any{"player_id": "bob-agent", "body": "bob-1"}))

		result, _ := s.handleNoteList(ctx, callToolRequest(map[string]any{
			"player_id":        "alice-agent",
			"target_player_id": "bob-agent",
		}))
		items := parseToolResultArray(t, result)
		if len(items) != 1 || items[0]["body"] != "bob-1" {
			t.Fatalf("expected only bob's note, got %v", items)
		}
	})

	t.Run("all_players returns every note", func(t *testing.T) {
		s := testServer(t)
		ctx := context.Background()
		seedNotePlayers(t, s)

		s.handleNoteAdd(ctx, callToolRequest(map[string]any{"player_id": "alice-agent", "body": "alice-1"}))
		s.handleNoteAdd(ctx, callToolRequest(map[string]any{"player_id": "bob-agent", "body": "bob-1"}))

		result, _ := s.handleNoteList(ctx, callToolRequest(map[string]any{
			"player_id":   "alice-agent",
			"all_players": true,
		}))
		items := parseToolResultArray(t, result)
		if len(items) != 2 {
			t.Fatalf("expected 2 notes, got %d", len(items))
		}
	})

	t.Run("all_players with target_player_id errors", func(t *testing.T) {
		s := testServer(t)
		seedNotePlayers(t, s)
		result, _ := s.handleNoteList(context.Background(), callToolRequest(map[string]any{
			"player_id":        "alice-agent",
			"all_players":      true,
			"target_player_id": "bob-agent",
		}))
		msg := getToolErrorText(t, result)
		if msg != "all_players cannot be combined with target_player_id" {
			t.Fatalf("expected mutual-exclusion error, got %q", msg)
		}
	})

	t.Run("window override respected", func(t *testing.T) {
		s := testServer(t)
		ctx := context.Background()
		seedNotePlayers(t, s)

		for range 5 {
			s.handleNoteAdd(ctx, callToolRequest(map[string]any{
				"player_id": "alice-agent",
				"body":      "note",
			}))
		}
		result, _ := s.handleNoteList(ctx, callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"window":    float64(2),
		}))
		items := parseToolResultArray(t, result)
		if len(items) != 2 {
			t.Fatalf("expected 2 notes under window=2, got %d", len(items))
		}
	})

	t.Run("since filter", func(t *testing.T) {
		s := testServer(t)
		ctx := context.Background()
		seedNotePlayers(t, s)

		s.handleNoteAdd(ctx, callToolRequest(map[string]any{"player_id": "alice-agent", "body": "old"}))
		cutoff := time.Now().UTC().Add(time.Second)
		time.Sleep(1100 * time.Millisecond)
		s.handleNoteAdd(ctx, callToolRequest(map[string]any{"player_id": "alice-agent", "body": "new"}))

		result, _ := s.handleNoteList(ctx, callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"since":     cutoff.Format(time.RFC3339),
		}))
		items := parseToolResultArray(t, result)
		if len(items) != 1 || items[0]["body"] != "new" {
			t.Fatalf("expected only 'new' note after since cutoff, got %v", items)
		}
	})

	t.Run("invalid since errors", func(t *testing.T) {
		s := testServer(t)
		seedNotePlayers(t, s)
		result, _ := s.handleNoteList(context.Background(), callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"since":     "yesterday",
		}))
		msg := getToolErrorText(t, result)
		if msg != "invalid since format, expected ISO 8601 (RFC3339)" {
			t.Fatalf("expected parse error, got %q", msg)
		}
	})

	t.Run("include_archived", func(t *testing.T) {
		s := testServer(t)
		ctx := context.Background()
		seedNotePlayers(t, s)

		addRes, _ := s.handleNoteAdd(ctx, callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"body":      "to archive",
		}))
		id := parseToolResult(t, addRes)["id"].(string)
		s.handleNoteArchive(ctx, callToolRequest(map[string]any{"player_id": "alice-agent", "id": id}))

		without, _ := s.handleNoteList(ctx, callToolRequest(map[string]any{"player_id": "alice-agent"}))
		if items := parseToolResultArray(t, without); len(items) != 0 {
			t.Fatalf("expected archived note hidden by default, got %d", len(items))
		}

		with, _ := s.handleNoteList(ctx, callToolRequest(map[string]any{
			"player_id":        "alice-agent",
			"include_archived": true,
		}))
		if items := parseToolResultArray(t, with); len(items) != 1 {
			t.Fatalf("expected archived note with include_archived, got %d", len(items))
		}
	})

	t.Run("task short_id filter", func(t *testing.T) {
		s := testServer(t)
		ctx := context.Background()
		seedNotePlayers(t, s)

		createRes, _ := s.handleTaskCreate(ctx, callToolRequest(map[string]any{"title": "T"}))
		shortID := parseToolResult(t, createRes)["short_id"].(string)

		s.handleNoteAdd(ctx, callToolRequest(map[string]any{"player_id": "alice-agent", "body": "project-level"}))
		s.handleNoteAdd(ctx, callToolRequest(map[string]any{"player_id": "alice-agent", "body": "task-level", "task": shortID}))

		result, _ := s.handleNoteList(ctx, callToolRequest(map[string]any{
			"player_id": "alice-agent",
			"task":      shortID,
		}))
		items := parseToolResultArray(t, result)
		if len(items) != 1 || items[0]["body"] != "task-level" {
			t.Fatalf("expected only task-level note, got %v", items)
		}
	})

	t.Run("empty result returns empty array", func(t *testing.T) {
		s := testServer(t)
		seedNotePlayers(t, s)
		result, err := s.handleNoteList(context.Background(), callToolRequest(map[string]any{
			"player_id": "alice-agent",
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		items := parseToolResultArray(t, result)
		if len(items) != 0 {
			t.Fatalf("expected empty array, got %d", len(items))
		}
	})
}
