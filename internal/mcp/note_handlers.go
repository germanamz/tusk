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

func toNoteResponse(ctx context.Context, server *Server, note *domain.Note, projectNames *projectNameCache) noteResponse {
	response := noteResponse{
		ID:        note.ID.String(),
		Project:   projectNames.name(note.ProjectID),
		PlayerID:  note.PlayerID,
		Body:      note.Body,
		Metadata:  note.Metadata,
		CreatedAt: note.CreatedAt.Format(time.RFC3339),
	}
	if note.TaskID != nil {
		var label string
		if task, lookupErr := server.taskSvc.GetByID(ctx, *note.TaskID); lookupErr == nil {
			label = task.ShortID
		} else {
			label = note.TaskID.String()
		}
		response.TaskShortID = &label
	}
	if note.ArchivedAt != nil {
		archivedAt := note.ArchivedAt.Format(time.RFC3339)
		response.ArchivedAt = &archivedAt
	}
	return response
}

func (server *Server) handleNoteAdd(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := server.checkBlocked("tusk_note_add", request); result != nil {
		return result, nil
	}

	playerID, playerIDErr := request.RequireString("player_id")

	if playerIDErr != nil {
		return mcp.NewToolResultError("player_id is required"), nil
	}

	ctx = service.WithActor(ctx, playerID)

	body, bodyErr := request.RequireString("body")

	if bodyErr != nil {
		return mcp.NewToolResultError("body is required"), nil
	}

	if server.playerSvc != nil {
		if _, regErr := server.playerSvc.Register(ctx, playerID, "agent"); regErr != nil {
			if !errors.Is(regErr, domain.ErrConflict) {
				return toolError(regErr, "auto-registering player"), nil
			}
		}
	}

	projectName := service.DefaultProjectName
	if name, projectNameErr := request.RequireString("project"); projectNameErr == nil {
		projectName = name
	}

	projectID, projectErr := server.taskSvc.ResolveProjectName(ctx, projectName)

	if projectErr != nil {
		return toolError(projectErr, "project "+projectName), nil
	}

	var taskID *uuid.UUID
	if taskShortID, taskShortIDErr := request.RequireString("task"); taskShortIDErr == nil {
		task, lookupErr := server.taskSvc.GetByShortID(ctx, taskShortID)

		if lookupErr != nil {
			return toolError(lookupErr, "task "+taskShortID), nil
		}

		taskID = &task.ID
	}

	var metadata map[string]any
	if raw, ok := request.GetArguments()["metadata"]; ok && raw != nil {
		metaMap, ok := raw.(map[string]any)
		if !ok {
			return mcp.NewToolResultError("\"metadata\" must be an object"), nil
		}
		metadata = metaMap
	}

	note := &domain.Note{
		ProjectID: projectID,
		PlayerID:  playerID,
		TaskID:    taskID,
		Body:      body,
		Metadata:  metadata,
	}

	createErr := server.noteSvc.Create(ctx, note)

	if createErr != nil {
		return toolError(createErr, "create note"), nil
	}

	return toolResultJSON(toNoteResponse(ctx, server, note, server.projectNames(ctx)))
}

func (server *Server) handleNoteList(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx = server.updatePlayerLiveness(ctx, request)

	playerID, playerIDErr := request.RequireString("player_id")

	if playerIDErr != nil {
		return mcp.NewToolResultError("player_id is required"), nil
	}

	if server.playerSvc != nil {
		if _, regErr := server.playerSvc.Register(ctx, playerID, "agent"); regErr != nil {
			if !errors.Is(regErr, domain.ErrConflict) {
				return toolError(regErr, "auto-registering player"), nil
			}
		}
	}

	projectName := service.DefaultProjectName
	if name, projectNameErr := request.RequireString("project"); projectNameErr == nil {
		projectName = name
	}

	projectID, projectErr := server.taskSvc.ResolveProjectName(ctx, projectName)

	if projectErr != nil {
		return toolError(projectErr, "project "+projectName), nil
	}

	var taskID *uuid.UUID
	if taskShortID, taskShortIDErr := request.RequireString("task"); taskShortIDErr == nil {
		task, lookupErr := server.taskSvc.GetByShortID(ctx, taskShortID)

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
	if windowF, windowErr := request.RequireFloat("window"); windowErr == nil {
		if iw := int(windowF); iw > 0 {
			windowOverride = &iw
		}
	}

	var since *time.Time
	if sinceStr, sinceErr := request.RequireString("since"); sinceErr == nil && sinceStr != "" {
		parsed, parseErr := time.Parse(time.RFC3339, sinceStr)

		if parseErr != nil {
			return mcp.NewToolResultError("invalid since format, expected ISO 8601 (RFC3339)"), nil
		}

		since = &parsed
	}

	includeArchived := request.GetBool("include_archived", false)

	notes, listErr := server.noteSvc.List(ctx, service.NoteListParams{
		ProjectID:       projectID,
		PlayerID:        targetPlayer,
		CallerPlayerID:  playerID,
		TaskID:          taskID,
		Since:           since,
		IncludeArchived: includeArchived,
		WindowOverride:  windowOverride,
	})

	if listErr != nil {
		return toolError(listErr, "list notes"), nil
	}

	names := server.projectNames(ctx)
	results := make([]noteResponse, len(notes))
	for idx, note := range notes {
		results[idx] = toNoteResponse(ctx, server, note, names)
	}
	return toolResultJSON(results)
}

func (server *Server) handleNoteArchive(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := server.checkBlocked("tusk_note_archive", request); result != nil {
		return result, nil
	}

	playerID, playerIDErr := request.RequireString("player_id")

	if playerIDErr != nil {
		return mcp.NewToolResultError("player_id is required"), nil
	}

	ctx = service.WithActor(ctx, playerID)

	idStr, idStrErr := request.RequireString("id")

	if idStrErr != nil {
		return mcp.NewToolResultError("id is required"), nil
	}

	if server.playerSvc != nil {
		if _, regErr := server.playerSvc.Register(ctx, playerID, "agent"); regErr != nil {
			if !errors.Is(regErr, domain.ErrConflict) {
				return toolError(regErr, "auto-registering player"), nil
			}
		}
	}

	id, parseErr := uuid.Parse(idStr)

	if parseErr != nil {
		return mcp.NewToolResultError("invalid note id, expected full UUID"), nil
	}

	archiveErr := server.noteSvc.Archive(ctx, id, playerID)

	if archiveErr != nil {
		return toolError(archiveErr, "archive note"), nil
	}

	note, getErr := server.noteSvc.GetByID(ctx, id)

	if getErr != nil {
		return toolError(getErr, "note "+id.String()), nil
	}

	return toolResultJSON(toNoteResponse(ctx, server, note, server.projectNames(ctx)))
}
