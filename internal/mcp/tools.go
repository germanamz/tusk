package mcp

import (
	"context"
	"encoding/json"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/mark3labs/mcp-go/mcp"
)

// toolResultJSON marshals v as indented JSON and wraps it in an MCP tool result.
func toolResultJSON(v any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(string(b)), nil
}

// toolError translates a domain error into an MCP tool-result error.
// context is optional extra info for not-found errors (e.g., "task abc123").
func toolError(err error, context string) *mcp.CallToolResult {
	return mcp.NewToolResultError(mapError(err, context))
}

// taskResponse is the JSON structure returned by task tools.
type taskResponse struct {
	ID             string   `json:"id"`
	ShortID        string   `json:"short_id"`
	ParentID       *string  `json:"parent_id,omitempty"`
	ProjectID      *string  `json:"project_id,omitempty"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Status         string   `json:"status"`
	Priority       int      `json:"priority"`
	Version        int      `json:"version"`
	Tags           []string `json:"tags"`
	DueAt          *string  `json:"due_at,omitempty"`
	WaitUntil      *string  `json:"wait_until,omitempty"`
	RecurrenceRule *string  `json:"recurrence_rule,omitempty"`
	CreatedAt      string   `json:"created_at"`
	ModifiedAt     string   `json:"modified_at"`
}

func toTaskResponse(t *domain.Task, tags []*domain.Tag) taskResponse {
	r := taskResponse{
		ID:          t.ID.String(),
		ShortID:     t.ShortID,
		Title:       t.Title,
		Description: t.Description,
		Status:      t.Status,
		Priority:    t.Priority,
		Version:     t.Version,
		CreatedAt:   t.CreatedAt.Format(time.RFC3339),
		ModifiedAt:  t.ModifiedAt.Format(time.RFC3339),
	}
	if t.ParentID != nil {
		s := t.ParentID.String()
		r.ParentID = &s
	}
	if t.ProjectID != nil {
		s := t.ProjectID.String()
		r.ProjectID = &s
	}
	if t.DueAt != nil {
		s := t.DueAt.Format(time.RFC3339)
		r.DueAt = &s
	}
	if t.WaitUntil != nil {
		s := t.WaitUntil.Format(time.RFC3339)
		r.WaitUntil = &s
	}
	r.RecurrenceRule = t.RecurrenceRule
	r.Tags = make([]string, len(tags))
	for i, tg := range tags {
		r.Tags[i] = tg.Name
	}
	return r
}

// handleTaskCreate handles the tusk_task_create tool.
func (s *Server) handleTaskCreate(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	title, err := request.RequireString("title")
	if err != nil {
		return mcp.NewToolResultError("title is required"), nil
	}

	task := &domain.Task{
		Title: title,
	}

	// Optional: description
	if desc, err := request.RequireString("description"); err == nil {
		task.Description = desc
	}

	// Optional: priority
	if p, err := request.RequireFloat("priority"); err == nil {
		task.Priority = int(p)
	}

	// Optional: project (by name)
	if projectName, err := request.RequireString("project"); err == nil {
		project, lookupErr := s.projectSvc.GetByName(ctx, projectName)
		if lookupErr != nil {
			return toolError(lookupErr, "project "+projectName), nil
		}
		task.ProjectID = &project.ID
	}

	// Optional: parent (by short_id)
	if parentShortID, err := request.RequireString("parent"); err == nil {
		parent, lookupErr := s.taskSvc.GetByShortID(ctx, parentShortID)
		if lookupErr != nil {
			return toolError(lookupErr, "parent task "+parentShortID), nil
		}
		task.ParentID = &parent.ID
	}

	// Optional: due (ISO 8601)
	if dueStr, err := request.RequireString("due"); err == nil {
		d, parseErr := time.Parse(time.RFC3339, dueStr)
		if parseErr != nil {
			return mcp.NewToolResultError("invalid due date format, expected ISO 8601 (RFC3339)"), nil
		}
		task.DueAt = &d
	}

	// Optional: wait_until (ISO 8601)
	if waitStr, err := request.RequireString("wait_until"); err == nil {
		w, parseErr := time.Parse(time.RFC3339, waitStr)
		if parseErr != nil {
			return mcp.NewToolResultError("invalid wait_until format, expected ISO 8601 (RFC3339)"), nil
		}
		task.WaitUntil = &w
	}

	if err := s.taskSvc.Create(ctx, task); err != nil {
		return toolError(err, ""), nil
	}

	// Assign tags if provided
	tags := request.GetStringSlice("tags", nil)
	if len(tags) > 0 {
		if err := s.tagSvc.AssignToTask(ctx, task.ID, tags); err != nil {
			return toolError(err, ""), nil
		}
	}

	// Fetch tags for response
	taskTags, err := s.tagSvc.GetTaskTags(ctx, task.ID)
	if err != nil {
		return nil, err
	}

	return toolResultJSON(toTaskResponse(task, taskTags))
}

// annotationResponse is the JSON structure for annotations within task get.
type annotationResponse struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

// relationResponse is the JSON structure for relations within task get.
type relationResponse struct {
	ID             string `json:"id"`
	SourceID       string `json:"source_id"`
	TargetID       string `json:"target_id"`
	RelationType   string `json:"relation_type"`
	RelatedShortID string `json:"related_short_id"`
	RelatedTitle   string `json:"related_title"`
	DirectionLabel string `json:"direction_label"`
	CreatedAt      string `json:"created_at"`
}

// taskGetResponse extends taskResponse with annotations and relations.
type taskGetResponse struct {
	taskResponse
	Annotations []annotationResponse `json:"annotations"`
	Relations   []relationResponse   `json:"relations"`
}

// handleTaskGet handles the tusk_task_get tool. Returns the full task with
// tags, relations, and annotations.
func (s *Server) handleTaskGet(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	shortID, err := request.RequireString("short_id")
	if err != nil {
		return mcp.NewToolResultError("short_id is required"), nil
	}

	task, err := s.taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		return toolError(err, "task "+shortID), nil
	}

	// Fetch tags
	tags, err := s.tagSvc.GetTaskTags(ctx, task.ID)
	if err != nil {
		return nil, err
	}

	// Fetch annotations
	annotations, err := s.taskSvc.GetAnnotations(ctx, shortID)
	if err != nil {
		return nil, err
	}

	// Fetch and resolve relations
	rels, err := s.relationSvc.GetByTask(ctx, shortID)
	if err != nil {
		return nil, err
	}

	resp := taskGetResponse{
		taskResponse: toTaskResponse(task, tags),
		Annotations:  make([]annotationResponse, len(annotations)),
		Relations:    make([]relationResponse, 0, len(rels)),
	}

	for i, ann := range annotations {
		resp.Annotations[i] = annotationResponse{
			ID:        ann.ID.String(),
			TaskID:    ann.TaskID.String(),
			Body:      ann.Body,
			CreatedAt: ann.CreatedAt.Format(time.RFC3339),
		}
	}

	for _, rel := range rels {
		rr := relationResponse{
			ID:           rel.ID.String(),
			SourceID:     rel.SourceID.String(),
			TargetID:     rel.TargetID.String(),
			RelationType: rel.RelationType,
			CreatedAt:    rel.CreatedAt.Format(time.RFC3339),
		}
		if rel.TargetID == task.ID {
			switch rel.RelationType {
			case "blocks":
				rr.DirectionLabel = "blocked_by"
			case "relates_to":
				rr.DirectionLabel = "related_to"
			case "duplicates":
				rr.DirectionLabel = "duplicated_by"
			}
			if other, lookupErr := s.taskSvc.GetByID(ctx, rel.SourceID); lookupErr == nil {
				rr.RelatedShortID = other.ShortID
				rr.RelatedTitle = other.Title
			}
		} else {
			rr.DirectionLabel = rel.RelationType
			if other, lookupErr := s.taskSvc.GetByID(ctx, rel.TargetID); lookupErr == nil {
				rr.RelatedShortID = other.ShortID
				rr.RelatedTitle = other.Title
			}
		}
		resp.Relations = append(resp.Relations, rr)
	}

	return toolResultJSON(resp)
}
