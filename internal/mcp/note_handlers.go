package mcp

import (
	"context"
	"errors"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/service"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
)

type noteResponse struct {
	ID          string         `json:"id"`
	Project     string         `json:"project"`
	PlayerID    string         `json:"player_id"`
	TaskShortID *string        `json:"task_short_id,omitempty"`
	Body        string         `json:"body"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   string         `json:"created_at"`
	ArchivedAt  *string        `json:"archived_at,omitempty"`
}

func toNoteResponse(ctx context.Context, s *Server, n *domain.Note, projectNames *projectNameCache) noteResponse {
	r := noteResponse{
		ID:        n.ID.String(),
		Project:   projectNames.name(n.ProjectID),
		PlayerID:  n.PlayerID,
		Body:      n.Body,
		Metadata:  n.Metadata,
		CreatedAt: n.CreatedAt.Format(time.RFC3339),
	}
	if n.TaskID != nil {
		var label string
		if task, err := s.taskSvc.GetByID(ctx, *n.TaskID); err == nil {
			label = task.ShortID
		} else {
			label = n.TaskID.String()
		}
		r.TaskShortID = &label
	}
	if n.ArchivedAt != nil {
		a := n.ArchivedAt.Format(time.RFC3339)
		r.ArchivedAt = &a
	}
	return r
}

func (s *Server) handleNoteAdd(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	playerID, err := request.RequireString("player_id")
	if err != nil {
		return mcp.NewToolResultError("player_id is required"), nil
	}
	body, err := request.RequireString("body")
	if err != nil {
		return mcp.NewToolResultError("body is required"), nil
	}

	if s.playerSvc != nil {
		if _, regErr := s.playerSvc.Register(ctx, playerID, "agent"); regErr != nil {
			if !errors.Is(regErr, domain.ErrConflict) {
				return toolError(regErr, "auto-registering player"), nil
			}
		}
	}

	projectName := service.DefaultProjectName
	if name, err := request.RequireString("project"); err == nil {
		projectName = name
	}
	projectID, err := s.taskSvc.ResolveProjectName(ctx, projectName)
	if err != nil {
		return toolError(err, "project "+projectName), nil
	}

	var taskID *uuid.UUID
	if taskShortID, err := request.RequireString("task"); err == nil {
		task, lookupErr := s.taskSvc.GetByShortID(ctx, taskShortID)
		if lookupErr != nil {
			return toolError(lookupErr, "task "+taskShortID), nil
		}
		taskID = &task.ID
	}

	var metadata map[string]any
	if raw, ok := request.GetArguments()["metadata"]; ok && raw != nil {
		m, ok := raw.(map[string]any)
		if !ok {
			return mcp.NewToolResultError("\"metadata\" must be an object"), nil
		}
		metadata = m
	}

	note := &domain.Note{
		ProjectID: projectID,
		PlayerID:  playerID,
		TaskID:    taskID,
		Body:      body,
		Metadata:  metadata,
	}
	if err := s.noteSvc.Create(ctx, note); err != nil {
		return toolError(err, "create note"), nil
	}

	return toolResultJSON(toNoteResponse(ctx, s, note, s.projectNames(ctx)))
}

func (s *Server) handleNoteList(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.updatePlayerLiveness(ctx, request)

	playerID, err := request.RequireString("player_id")
	if err != nil {
		return mcp.NewToolResultError("player_id is required"), nil
	}

	if s.playerSvc != nil {
		if _, regErr := s.playerSvc.Register(ctx, playerID, "agent"); regErr != nil {
			if !errors.Is(regErr, domain.ErrConflict) {
				return toolError(regErr, "auto-registering player"), nil
			}
		}
	}

	projectName := service.DefaultProjectName
	if name, err := request.RequireString("project"); err == nil {
		projectName = name
	}
	projectID, err := s.taskSvc.ResolveProjectName(ctx, projectName)
	if err != nil {
		return toolError(err, "project "+projectName), nil
	}

	var taskID *uuid.UUID
	if taskShortID, err := request.RequireString("task"); err == nil {
		task, lookupErr := s.taskSvc.GetByShortID(ctx, taskShortID)
		if lookupErr != nil {
			return toolError(lookupErr, "task "+taskShortID), nil
		}
		taskID = &task.ID
	}

	targetPlayerID := request.GetString("target_player_id", "")
	allPlayers := request.GetBool("all_players", false)
	if allPlayers && targetPlayerID != "" {
		return mcp.NewToolResultError("all_players cannot be combined with target_player_id"), nil
	}
	var targetPlayer string
	switch {
	case allPlayers:
		targetPlayer = ""
	case targetPlayerID != "":
		targetPlayer = targetPlayerID
	default:
		targetPlayer = playerID
	}

	var windowOverride *int
	if w, err := request.RequireFloat("window"); err == nil {
		if iw := int(w); iw > 0 {
			windowOverride = &iw
		}
	}

	var since *time.Time
	if sinceStr, err := request.RequireString("since"); err == nil && sinceStr != "" {
		t, parseErr := time.Parse(time.RFC3339, sinceStr)
		if parseErr != nil {
			return mcp.NewToolResultError("invalid since format, expected ISO 8601 (RFC3339)"), nil
		}
		since = &t
	}

	includeArchived := request.GetBool("include_archived", false)

	notes, err := s.noteSvc.List(ctx, service.NoteListParams{
		ProjectID:       projectID,
		PlayerID:        targetPlayer,
		CallerPlayerID:  playerID,
		TaskID:          taskID,
		Since:           since,
		IncludeArchived: includeArchived,
		WindowOverride:  windowOverride,
	})
	if err != nil {
		return toolError(err, "list notes"), nil
	}

	names := s.projectNames(ctx)
	results := make([]noteResponse, len(notes))
	for i, n := range notes {
		results[i] = toNoteResponse(ctx, s, n, names)
	}
	return toolResultJSON(results)
}

func (s *Server) handleNoteArchive(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	playerID, err := request.RequireString("player_id")
	if err != nil {
		return mcp.NewToolResultError("player_id is required"), nil
	}
	idStr, err := request.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError("id is required"), nil
	}

	if s.playerSvc != nil {
		if _, regErr := s.playerSvc.Register(ctx, playerID, "agent"); regErr != nil {
			if !errors.Is(regErr, domain.ErrConflict) {
				return toolError(regErr, "auto-registering player"), nil
			}
		}
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		return mcp.NewToolResultError("invalid note id, expected full UUID"), nil
	}

	if err := s.noteSvc.Archive(ctx, id, playerID); err != nil {
		return toolError(err, "archive note"), nil
	}

	note, err := s.noteSvc.GetByID(ctx, id)
	if err != nil {
		return toolError(err, "note "+id.String()), nil
	}

	return toolResultJSON(toNoteResponse(ctx, s, note, s.projectNames(ctx)))
}
