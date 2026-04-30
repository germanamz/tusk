package mcp

import (
	"context"
	"fmt"

	"github.com/germanamz/tusk/service"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
)

// handleTaskMove handles the tusk_task_move tool. See server.go for the
// declared JSON schema; this handler enforces the semantic rules that cannot
// be expressed in schema (target_id / parent_id conditional requirements and
// the parent_id tristate).
func (server *Server) handleTaskMove(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := server.checkBlocked("tusk_task_move", request); result != nil {
		return result, nil
	}
	ctx = server.updatePlayerLiveness(ctx, request)

	taskIDRaw, taskIDErr := request.RequireString("task_id")

	if taskIDErr != nil {
		return mcp.NewToolResultError("task_id is required"), nil
	}

	positionRaw, positionErr := request.RequireString("position")

	if positionErr != nil {
		return mcp.NewToolResultError("position is required"), nil
	}

	versionF, versionErr := request.RequireFloat("version")

	if versionErr != nil {
		return mcp.NewToolResultError("version is required"), nil
	}

	position, positionParseErr := parseMovePosition(positionRaw)

	if positionParseErr != nil {
		return mcp.NewToolResultError(positionParseErr.Error()), nil
	}

	args := request.GetArguments()

	// target_id — required for before/after, forbidden for first/last.
	var targetID *uuid.UUID
	if targetRaw, ok := args["target_id"]; ok && targetRaw != nil {
		targetStr, ok := targetRaw.(string)
		if !ok {
			return mcp.NewToolResultError("target_id must be a string"), nil
		}
		if targetStr == "" {
			return mcp.NewToolResultError("target_id must be a non-empty string"), nil
		}
		switch position {
		case service.MovePositionFirst, service.MovePositionLast:
			return mcp.NewToolResultError("target_id is not valid for position=first|last"), nil
		}

		target, targetErr := server.taskSvc.GetByShortID(ctx, targetStr)

		if targetErr != nil {
			return toolError(targetErr, "target task "+targetStr), nil
		}

		tid := target.ID
		targetID = &tid
	} else if position == service.MovePositionBefore || position == service.MovePositionAfter {
		return mcp.NewToolResultError("target_id is required for position=before|after"), nil
	}

	// parent_id — tristate: absent | null | string.
	var parentID **uuid.UUID
	if parentRaw, ok := args["parent_id"]; ok {
		switch position {
		case service.MovePositionBefore, service.MovePositionAfter:
			return mcp.NewToolResultError("parent_id is not valid for position=before|after; the parent is inherited from target_id"), nil
		}
		if parentRaw == nil {
			var nilUUID *uuid.UUID
			parentID = &nilUUID
		} else {
			parentStr, ok := parentRaw.(string)
			if !ok {
				return mcp.NewToolResultError("parent_id must be a string, null, or omitted"), nil
			}
			if parentStr == "" {
				return mcp.NewToolResultError("parent_id must be a non-empty string (or null to move to root)"), nil
			}

			parent, parentErr := server.taskSvc.GetByShortID(ctx, parentStr)

			if parentErr != nil {
				return toolError(parentErr, "parent task "+parentStr), nil
			}

			pid := parent.ID
			parentPtr := &pid
			parentID = &parentPtr
		}
	}

	subject, subjectErr := server.taskSvc.GetByShortID(ctx, taskIDRaw)

	if subjectErr != nil {
		return toolError(subjectErr, "task "+taskIDRaw), nil
	}

	moveReq := service.MoveRequest{
		TaskID:   subject.ID,
		Version:  int(versionF),
		Position: position,
		TargetID: targetID,
		ParentID: parentID,
	}
	if pid := request.GetString("player_id", ""); pid != "" {
		pidCopy := pid
		moveReq.ActorID = &pidCopy
	}

	moved, moveErr := server.taskSvc.Move(ctx, moveReq)

	if moveErr != nil {
		return toolError(moveErr, "task "+taskIDRaw), nil
	}

	tags, tagsErr := server.tagSvc.GetTaskTags(ctx, moved.ID)

	if tagsErr != nil {
		return nil, tagsErr
	}

	return toolResultJSON(toTaskResponse(moved, tags, server.projectNames(ctx)))
}

// handleTaskResequence handles the tusk_task_resequence tool. parent_id is
// tristate-ish here: JSON null resequences root-level siblings, a non-empty
// string resequences under that parent. The schema lists parent_id in
// required[], so the value is always present; it may simply be null.
func (server *Server) handleTaskResequence(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := server.checkBlocked("tusk_task_resequence", request); result != nil {
		return result, nil
	}
	ctx = server.updatePlayerLiveness(ctx, request)

	args := request.GetArguments()
	parentRaw, present := args["parent_id"]
	if !present {
		return mcp.NewToolResultError("parent_id is required (pass null to resequence root-level siblings)"), nil
	}

	var parentID *uuid.UUID
	var parentInput string
	if parentRaw != nil {
		parentStr, ok := parentRaw.(string)
		if !ok {
			return mcp.NewToolResultError("parent_id must be a string or null"), nil
		}
		if parentStr == "" {
			return mcp.NewToolResultError("parent_id must be a non-empty string (or null for root)"), nil
		}

		parent, parentErr := server.taskSvc.GetByShortID(ctx, parentStr)

		if parentErr != nil {
			return toolError(parentErr, "parent task "+parentStr), nil
		}

		pid := parent.ID
		parentID = &pid
		parentInput = parentStr
	}

	var actor *string
	if pid := request.GetString("player_id", ""); pid != "" {
		pidCopy := pid
		actor = &pidCopy
	}

	rewritten, resequenceErr := server.taskSvc.Resequence(ctx, parentID, actor)

	if resequenceErr != nil {
		ctxStr := "root"
		if parentInput != "" {
			ctxStr = "parent " + parentInput
		}
		return toolError(resequenceErr, ctxStr), nil
	}

	resp := resequenceResponse{Rewritten: rewritten}
	if parentID != nil {
		parentUUIDStr := parentID.String()
		resp.ParentID = &parentUUIDStr
	}
	return toolResultJSON(resp)
}

// resequenceResponse is the JSON shape returned by tusk_task_resequence.
type resequenceResponse struct {
	Rewritten int     `json:"rewritten"`
	ParentID  *string `json:"parent_id"`
}

// parseMovePosition maps the `position` enum string to its service-layer
// constant.
func parseMovePosition(raw string) (service.MovePosition, error) {
	switch raw {
	case "before":
		return service.MovePositionBefore, nil
	case "after":
		return service.MovePositionAfter, nil
	case "first":
		return service.MovePositionFirst, nil
	case "last":
		return service.MovePositionLast, nil
	}
	return 0, fmt.Errorf("invalid position %q: expected before|after|first|last", raw)
}
