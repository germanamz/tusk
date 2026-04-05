package mcp

import (
	"context"
	"encoding/json"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
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
	ProjectID      string   `json:"project_id"`
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
	r.ProjectID = t.ProjectID
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

	// Optional: project (by ID)
	if projectID, err := request.RequireString("project"); err == nil {
		task.ProjectID = projectID
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

// buildTaskGetResponse builds a full taskGetResponse with tags, annotations,
// and resolved relations. Shared by handleTaskGet and handleTaskResource.
func (s *Server) buildTaskGetResponse(ctx context.Context, shortID string) (*taskGetResponse, error) {
	task, err := s.taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		return nil, err
	}

	tags, err := s.tagSvc.GetTaskTags(ctx, task.ID)
	if err != nil {
		return nil, err
	}

	annotations, err := s.taskSvc.GetAnnotations(ctx, shortID)
	if err != nil {
		return nil, err
	}

	rels, err := s.relationSvc.GetByTask(ctx, shortID)
	if err != nil {
		return nil, err
	}

	resp := &taskGetResponse{
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

	return resp, nil
}

// handleTaskGet handles the tusk_task_get tool. Returns the full task with
// tags, relations, and annotations.
func (s *Server) handleTaskGet(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	shortID, err := request.RequireString("short_id")
	if err != nil {
		return mcp.NewToolResultError("short_id is required"), nil
	}

	resp, err := s.buildTaskGetResponse(ctx, shortID)
	if err != nil {
		return toolError(err, "task "+shortID), nil
	}

	return toolResultJSON(resp)
}

// handleTaskList handles the tusk_task_list tool.
func (s *Server) handleTaskList(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	filter := domain.TaskFilter{}

	// Optional: status (string array)
	if statuses := request.GetStringSlice("status", nil); len(statuses) > 0 {
		filter.Statuses = statuses
	}

	// Optional: priority range
	if pMin, err := request.RequireFloat("priority_min"); err == nil {
		v := int(pMin)
		filter.PriorityMin = &v
	}
	if pMax, err := request.RequireFloat("priority_max"); err == nil {
		v := int(pMax)
		filter.PriorityMax = &v
	}

	// Optional: project (by ID)
	if projectID, err := request.RequireString("project"); err == nil {
		filter.ProjectID = &projectID
	}

	// Optional: tags include/exclude
	if tags := request.GetStringSlice("tags", nil); len(tags) > 0 {
		filter.Tags = tags
	}
	if exTags := request.GetStringSlice("exclude_tags", nil); len(exTags) > 0 {
		filter.ExcludeTags = exTags
	}

	// Optional: due date range
	if after, err := request.RequireString("due_after"); err == nil {
		d, parseErr := time.Parse(time.RFC3339, after)
		if parseErr != nil {
			return mcp.NewToolResultError("invalid due_after format, expected ISO 8601 (RFC3339)"), nil
		}
		filter.DueAfter = &d
	}
	if before, err := request.RequireString("due_before"); err == nil {
		d, parseErr := time.Parse(time.RFC3339, before)
		if parseErr != nil {
			return mcp.NewToolResultError("invalid due_before format, expected ISO 8601 (RFC3339)"), nil
		}
		filter.DueBefore = &d
	}

	// Optional: parent (direct children)
	if parentShortID, err := request.RequireString("parent"); err == nil {
		parent, lookupErr := s.taskSvc.GetByShortID(ctx, parentShortID)
		if lookupErr != nil {
			return toolError(lookupErr, "parent task "+parentShortID), nil
		}
		filter.ParentID = &parent.ID
	}

	// Optional: root (all descendants)
	if rootShortID, err := request.RequireString("root"); err == nil {
		root, lookupErr := s.taskSvc.GetByShortID(ctx, rootShortID)
		if lookupErr != nil {
			return toolError(lookupErr, "root task "+rootShortID), nil
		}
		filter.RootID = &root.ID
	}

	tasks, err := s.taskSvc.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Batch-fetch tags for all tasks
	taskIDs := make([]uuid.UUID, len(tasks))
	for i, t := range tasks {
		taskIDs[i] = t.ID
	}
	tagsByTask, err := s.tagSvc.GetTaskTagsBatch(ctx, taskIDs)
	if err != nil {
		return nil, err
	}

	results := make([]taskResponse, len(tasks))
	for i, t := range tasks {
		results[i] = toTaskResponse(t, tagsByTask[t.ID])
	}

	return toolResultJSON(results)
}

// handleTaskModify handles the tusk_task_modify tool.
func (s *Server) handleTaskModify(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	shortID, err := request.RequireString("short_id")
	if err != nil {
		return mcp.NewToolResultError("short_id is required"), nil
	}

	version, err := request.RequireFloat("version")
	if err != nil {
		return mcp.NewToolResultError("version is required"), nil
	}

	upd := domain.TaskUpdate{
		ShortID: shortID,
		Version: int(version),
	}

	// Optional fields
	if title, err := request.RequireString("title"); err == nil {
		upd.Title = &title
	}
	if desc, err := request.RequireString("description"); err == nil {
		if desc == "" {
			var nilStr *string
			upd.Description = &nilStr
		} else {
			dp := &desc
			upd.Description = &dp
		}
	}
	if p, err := request.RequireFloat("priority"); err == nil {
		v := int(p)
		upd.Priority = &v
	}

	// Optional: project (by ID)
	if projectID, err := request.RequireString("project"); err == nil {
		upd.ProjectID = &projectID
	}

	// Optional: parent (by short_id, empty string clears parent)
	if parentShortID, err := request.RequireString("parent"); err == nil {
		if parentShortID == "" {
			var nilUUID *uuid.UUID
			upd.ParentID = &nilUUID
		} else {
			parent, lookupErr := s.taskSvc.GetByShortID(ctx, parentShortID)
			if lookupErr != nil {
				return toolError(lookupErr, "parent task "+parentShortID), nil
			}
			pid := parent.ID
			pp := &pid
			upd.ParentID = &pp
		}
	}

	// Optional: due (ISO 8601, empty string clears)
	if dueStr, err := request.RequireString("due"); err == nil {
		if dueStr == "" {
			var nilTime *time.Time
			upd.DueAt = &nilTime
		} else {
			d, parseErr := time.Parse(time.RFC3339, dueStr)
			if parseErr != nil {
				return mcp.NewToolResultError("invalid due date format, expected ISO 8601 (RFC3339)"), nil
			}
			dp := &d
			upd.DueAt = &dp
		}
	}

	// Optional: wait_until (ISO 8601, empty string clears)
	if waitStr, err := request.RequireString("wait_until"); err == nil {
		if waitStr == "" {
			var nilTime *time.Time
			upd.WaitUntil = &nilTime
		} else {
			w, parseErr := time.Parse(time.RFC3339, waitStr)
			if parseErr != nil {
				return mcp.NewToolResultError("invalid wait_until format, expected ISO 8601 (RFC3339)"), nil
			}
			wp := &w
			upd.WaitUntil = &wp
		}
	}

	updated, err := s.taskSvc.Update(ctx, upd)
	if err != nil {
		return toolError(err, "task "+shortID), nil
	}

	// Handle tag changes
	addTags := request.GetStringSlice("add_tags", nil)
	if len(addTags) > 0 {
		if err := s.tagSvc.AssignToTask(ctx, updated.ID, addTags); err != nil {
			return toolError(err, ""), nil
		}
	}
	removeTags := request.GetStringSlice("remove_tags", nil)
	if len(removeTags) > 0 {
		if err := s.tagSvc.RemoveFromTask(ctx, updated.ID, removeTags); err != nil {
			return toolError(err, ""), nil
		}
	}

	// Fetch tags for response
	taskTags, err := s.tagSvc.GetTaskTags(ctx, updated.ID)
	if err != nil {
		return nil, err
	}

	return toolResultJSON(toTaskResponse(updated, taskTags))
}

// handleTaskTransition is a shared helper for start/done/delete handlers.
func (s *Server) handleTaskTransition(ctx context.Context, request mcp.CallToolRequest, transition func(context.Context, string, int) (*domain.Task, error)) (*mcp.CallToolResult, error) {
	shortID, err := request.RequireString("short_id")
	if err != nil {
		return mcp.NewToolResultError("short_id is required"), nil
	}
	version, err := request.RequireFloat("version")
	if err != nil {
		return mcp.NewToolResultError("version is required"), nil
	}

	updated, err := transition(ctx, shortID, int(version))
	if err != nil {
		return toolError(err, "task "+shortID), nil
	}

	tags, err := s.tagSvc.GetTaskTags(ctx, updated.ID)
	if err != nil {
		return nil, err
	}

	return toolResultJSON(toTaskResponse(updated, tags))
}

// handleTaskStart handles the tusk_task_start tool.
func (s *Server) handleTaskStart(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.handleTaskTransition(ctx, request, s.taskSvc.Start)
}

// handleTaskDone handles the tusk_task_done tool.
func (s *Server) handleTaskDone(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.handleTaskTransition(ctx, request, s.taskSvc.Complete)
}

// handleTaskDelete handles the tusk_task_delete tool.
func (s *Server) handleTaskDelete(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.handleTaskTransition(ctx, request, s.taskSvc.Delete)
}

// handleTaskAnnotate handles the tusk_task_annotate tool.
func (s *Server) handleTaskAnnotate(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	shortID, err := request.RequireString("short_id")
	if err != nil {
		return mcp.NewToolResultError("short_id is required"), nil
	}
	body, err := request.RequireString("body")
	if err != nil {
		return mcp.NewToolResultError("body is required"), nil
	}

	ann, err := s.taskSvc.Annotate(ctx, shortID, body)
	if err != nil {
		return toolError(err, "task "+shortID), nil
	}

	return toolResultJSON(annotationResponse{
		ID:        ann.ID.String(),
		TaskID:    ann.TaskID.String(),
		Body:      ann.Body,
		CreatedAt: ann.CreatedAt.Format(time.RFC3339),
	})
}

// treeNodeResponse is the nested JSON structure for the tree tool.
type treeNodeResponse struct {
	ID             string             `json:"id"`
	ShortID        string             `json:"short_id"`
	ParentID       *string            `json:"parent_id,omitempty"`
	ProjectID      string             `json:"project_id"`
	Title          string             `json:"title"`
	Description    string             `json:"description"`
	Status         string             `json:"status"`
	Priority       int                `json:"priority"`
	Version        int                `json:"version"`
	DueAt          *string            `json:"due_at,omitempty"`
	WaitUntil      *string            `json:"wait_until,omitempty"`
	RecurrenceRule *string            `json:"recurrence_rule,omitempty"`
	CreatedAt      string             `json:"created_at"`
	ModifiedAt     string             `json:"modified_at"`
	Children       []treeNodeResponse `json:"children"`
}

func toTreeNodeResponse(task *domain.Task) treeNodeResponse {
	r := treeNodeResponse{
		ID:          task.ID.String(),
		ShortID:     task.ShortID,
		Title:       task.Title,
		Description: task.Description,
		Status:      task.Status,
		Priority:    task.Priority,
		Version:     task.Version,
		CreatedAt:   task.CreatedAt.Format(time.RFC3339),
		ModifiedAt:  task.ModifiedAt.Format(time.RFC3339),
		Children:    []treeNodeResponse{},
	}
	if task.ParentID != nil {
		s := task.ParentID.String()
		r.ParentID = &s
	}
	r.ProjectID = task.ProjectID
	if task.DueAt != nil {
		s := task.DueAt.Format(time.RFC3339)
		r.DueAt = &s
	}
	if task.WaitUntil != nil {
		s := task.WaitUntil.Format(time.RFC3339)
		r.WaitUntil = &s
	}
	r.RecurrenceRule = task.RecurrenceRule
	return r
}

// buildTreeResponse constructs a nested tree from a flat task list.
// If rootID is non-nil, only that task is the root. Otherwise, tasks without
// a parent (or whose parent is not in the set) become roots.
func buildTreeResponse(tasks []*domain.Task, rootID *uuid.UUID) []treeNodeResponse {
	type node struct {
		resp     treeNodeResponse
		children []*node
	}

	byID := make(map[uuid.UUID]*node, len(tasks))
	for _, t := range tasks {
		n := &node{resp: toTreeNodeResponse(t)}
		byID[t.ID] = n
	}

	var roots []*node
	for _, t := range tasks {
		n := byID[t.ID]
		if rootID != nil && t.ID == *rootID {
			roots = append(roots, n)
			continue
		}
		if t.ParentID != nil {
			if parent, ok := byID[*t.ParentID]; ok {
				parent.children = append(parent.children, n)
				continue
			}
		}
		if rootID == nil {
			roots = append(roots, n)
		}
	}

	var flatten func(n *node) treeNodeResponse
	flatten = func(n *node) treeNodeResponse {
		r := n.resp
		r.Children = make([]treeNodeResponse, len(n.children))
		for i, child := range n.children {
			r.Children[i] = flatten(child)
		}
		return r
	}

	result := make([]treeNodeResponse, len(roots))
	for i, root := range roots {
		result[i] = flatten(root)
	}
	return result
}

// relationAddResponse is the JSON structure returned by the relation add tool.
type relationAddResponse struct {
	ID           string `json:"id"`
	SourceID     string `json:"source_id"`
	TargetID     string `json:"target_id"`
	RelationType string `json:"relation_type"`
	CreatedAt    string `json:"created_at"`
}

// handleRelationAdd handles the tusk_relation_add tool.
func (s *Server) handleRelationAdd(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	source, err := request.RequireString("source")
	if err != nil {
		return mcp.NewToolResultError("source is required"), nil
	}
	target, err := request.RequireString("target")
	if err != nil {
		return mcp.NewToolResultError("target is required"), nil
	}
	relType, err := request.RequireString("type")
	if err != nil {
		return mcp.NewToolResultError("type is required"), nil
	}

	rel, err := s.relationSvc.Add(ctx, source, target, relType)
	if err != nil {
		return toolError(err, ""), nil
	}

	return toolResultJSON(relationAddResponse{
		ID:           rel.ID.String(),
		SourceID:     rel.SourceID.String(),
		TargetID:     rel.TargetID.String(),
		RelationType: rel.RelationType,
		CreatedAt:    rel.CreatedAt.Format(time.RFC3339),
	})
}

// handleRelationRemove handles the tusk_relation_remove tool.
func (s *Server) handleRelationRemove(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	source, err := request.RequireString("source")
	if err != nil {
		return mcp.NewToolResultError("source is required"), nil
	}
	target, err := request.RequireString("target")
	if err != nil {
		return mcp.NewToolResultError("target is required"), nil
	}
	relType, err := request.RequireString("type")
	if err != nil {
		return mcp.NewToolResultError("type is required"), nil
	}

	if err := s.relationSvc.Remove(ctx, source, target, relType); err != nil {
		return toolError(err, ""), nil
	}

	return mcp.NewToolResultText("relation removed"), nil
}

// projectResponse is the JSON structure returned by project tools.
type projectResponse struct {
	ID       string                 `json:"id"`
	Workflow string                 `json:"workflow"`
	Settings domain.ProjectSettings `json:"settings"`
}

func toProjectResponse(p *domain.Project) projectResponse {
	return projectResponse{
		ID:       p.ID,
		Workflow: p.Workflow,
		Settings: p.Settings,
	}
}

// handleProjectList handles the tusk_project_list tool.
func (s *Server) handleProjectList(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projects, err := s.projectSvc.List(ctx)
	if err != nil {
		return nil, err
	}

	results := make([]projectResponse, len(projects))
	for i, p := range projects {
		results[i] = toProjectResponse(p)
	}

	return toolResultJSON(results)
}

// workflowListResponse is the JSON structure returned by the workflow list tool.
type workflowListResponse struct {
	Name        string               `json:"name"`
	Statuses    []string             `json:"statuses"`
	Transitions []transitionResponse `json:"transitions"`
	Projects    []string             `json:"projects"`
}

// handleWorkflowList handles the tusk_workflow_list tool.
func (s *Server) handleWorkflowList(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	workflows, err := s.workflowSvc.List(ctx)
	if err != nil {
		return nil, err
	}

	results := make([]workflowListResponse, len(workflows))
	for i, wf := range workflows {
		_, projectIDs, err := s.workflowSvc.GetWorkflowWithProjects(ctx, wf.Name)
		if err != nil {
			return nil, err
		}

		transitions := make([]transitionResponse, len(wf.Transitions))
		for j, t := range wf.Transitions {
			transitions[j] = transitionResponse{From: t.FromStatus, To: t.ToStatus}
		}

		if projectIDs == nil {
			projectIDs = []string{}
		}
		results[i] = workflowListResponse{
			Name:        wf.Name,
			Statuses:    wf.Statuses,
			Transitions: transitions,
			Projects:    projectIDs,
		}
	}

	return toolResultJSON(results)
}

// handleTaskTree handles the tusk_task_tree tool.
func (s *Server) handleTaskTree(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var tasks []*domain.Task
	var rootID *uuid.UUID

	if shortID, err := request.RequireString("short_id"); err == nil {
		// Subtree mode
		root, lookupErr := s.taskSvc.GetByShortID(ctx, shortID)
		if lookupErr != nil {
			return toolError(lookupErr, "task "+shortID), nil
		}
		descendants, err := s.taskSvc.GetDescendants(ctx, root.ID)
		if err != nil {
			return nil, err
		}
		tasks = append([]*domain.Task{root}, descendants...)
		rootID = &root.ID
	} else {
		// Full tree mode
		filter := domain.TaskFilter{
			Statuses: []string{"pending", "active", "completed"},
		}
		// Check include_deleted flag
		if request.GetBool("include_deleted", false) {
			filter = domain.TaskFilter{}
		}
		var listErr error
		tasks, listErr = s.taskSvc.List(ctx, filter)
		if listErr != nil {
			return nil, listErr
		}
	}

	tree := buildTreeResponse(tasks, rootID)
	return toolResultJSON(tree)
}
