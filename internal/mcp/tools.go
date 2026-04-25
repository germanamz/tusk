package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/filter"
	"github.com/germanamz/tusk/service"
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
// *domain.TaxonomyError surfaces through taxonomyErrorResult so clients can
// branch on the structured `taxonomy_violation` payload.
func toolError(err error, context string) *mcp.CallToolResult {
	var te *domain.TaxonomyError
	if errors.As(err, &te) {
		return taxonomyErrorResult(te)
	}
	return mcp.NewToolResultError(mapError(err, context))
}

// taskResponse is the JSON structure returned by task tools.
type taskResponse struct {
	ID                      string                   `json:"id"`
	ShortID                 string                   `json:"short_id"`
	ParentID                *string                  `json:"parent_id,omitempty"`
	ProjectID               string                   `json:"project_id"`
	Title                   string                   `json:"title"`
	Description             string                   `json:"description"`
	Level                   *string                  `json:"level,omitempty"`
	Status                  string                   `json:"status"`
	Priority                int                      `json:"priority"`
	Order                   *float64                 `json:"order,omitempty"`
	Version                 int                      `json:"version"`
	Tags                    []string                 `json:"tags"`
	DueAt                   *string                  `json:"due_at,omitempty"`
	WaitUntil               *string                  `json:"wait_until,omitempty"`
	RecurrenceRule          *string                  `json:"recurrence_rule,omitempty"`
	UDA                     map[string]any           `json:"uda,omitempty"`
	CreatedAt               string                   `json:"created_at"`
	ModifiedAt              string                   `json:"modified_at"`
	Urgency                 float64                  `json:"urgency"`
	UrgencyOverrides        *domain.UrgencyOverrides `json:"urgency_overrides,omitempty"`
	EffectiveUrgencyWeights *urgencyWeightsJSON      `json:"effective_urgency_weights,omitempty"`
	ClaimedBy               *string                  `json:"claimed_by,omitempty"`
	ClaimedAt               *string                  `json:"claimed_at,omitempty"`
}

// urgencyWeightsJSON serializes a fully-resolved 10-weight urgency table.
// Populated from domain.ResolvedUrgencyWeights when the chain contributed
// non-default values; absent otherwise so default-only tasks stay terse.
type urgencyWeightsJSON struct {
	PriorityWeight    float64 `json:"priority_weight"`
	DueWeight         float64 `json:"due_weight"`
	AgeWeight         float64 `json:"age_weight"`
	ActiveWeight      float64 `json:"active_weight"`
	BlockingWeight    float64 `json:"blocking_weight"`
	BlockedWeight     float64 `json:"blocked_weight"`
	TagsWeight        float64 `json:"tags_weight"`
	ProjectWeight     float64 `json:"project_weight"`
	AnnotationsWeight float64 `json:"annotations_weight"`
	WaitingWeight     float64 `json:"waiting_weight"`
}

func toUrgencyWeightsJSON(w domain.ResolvedUrgencyWeights) *urgencyWeightsJSON {
	return &urgencyWeightsJSON{
		PriorityWeight:    w.PriorityWeight,
		DueWeight:         w.DueWeight,
		AgeWeight:         w.AgeWeight,
		ActiveWeight:      w.ActiveWeight,
		BlockingWeight:    w.BlockingWeight,
		BlockedWeight:     w.BlockedWeight,
		TagsWeight:        w.TagsWeight,
		ProjectWeight:     w.ProjectWeight,
		AnnotationsWeight: w.AnnotationsWeight,
		WaitingWeight:     w.WaitingWeight,
	}
}

// projectNameCache resolves project UUIDs to names within one MCP handler
// invocation, so a list response avoids N+1 lookups.
type projectNameCache struct {
	ctx   context.Context
	svc   projectByIDLookup
	cache map[uuid.UUID]string
}

// projectByIDLookup is the subset of ProjectService used by projectNameCache.
type projectByIDLookup interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error)
}

func newProjectNameCache(ctx context.Context, svc projectByIDLookup) *projectNameCache {
	return &projectNameCache{ctx: ctx, svc: svc, cache: map[uuid.UUID]string{}}
}

func (c *projectNameCache) name(id uuid.UUID) string {
	if c == nil || c.svc == nil {
		return id.String()
	}
	if n, ok := c.cache[id]; ok {
		return n
	}
	p, err := c.svc.GetByID(c.ctx, id)
	if err != nil {
		c.cache[id] = id.String()
		return id.String()
	}
	c.cache[id] = p.Name
	return p.Name
}

// projectNames returns a new per-invocation project-name cache.
func (s *Server) projectNames(ctx context.Context) *projectNameCache {
	return newProjectNameCache(ctx, s.projectSvc)
}

func toTaskResponse(t *domain.Task, tags []*domain.Tag, projectNames *projectNameCache) taskResponse {
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
		Urgency:     t.Urgency,
	}
	if t.ParentID != nil {
		s := t.ParentID.String()
		r.ParentID = &s
	}
	r.ProjectID = projectNames.name(t.ProjectID)
	if t.DueAt != nil {
		s := t.DueAt.Format(time.RFC3339)
		r.DueAt = &s
	}
	if t.WaitUntil != nil {
		s := t.WaitUntil.Format(time.RFC3339)
		r.WaitUntil = &s
	}
	r.RecurrenceRule = t.RecurrenceRule
	r.Level = t.Level
	r.Order = t.Order
	r.UDA = t.UDA
	r.Tags = make([]string, len(tags))
	for i, tg := range tags {
		r.Tags[i] = tg.Name
	}
	if t.ClaimedBy != nil {
		r.ClaimedBy = t.ClaimedBy
	}
	if t.ClaimedAt != nil {
		s := t.ClaimedAt.Format(time.RFC3339)
		r.ClaimedAt = &s
	}
	if t.UrgencyOverrides != nil {
		r.UrgencyOverrides = t.UrgencyOverrides
	}
	if t.EffectiveWeights != nil {
		r.EffectiveUrgencyWeights = toUrgencyWeightsJSON(*t.EffectiveWeights)
	}
	return r
}

// extractUDA extracts and validates the "uda" object parameter from the request.
// Returns nil if the parameter is not present. Returns an MCP error result if
// the parameter is present but invalid (wrong type, invalid keys, non-string values).
func extractUDA(request mcp.CallToolRequest) (map[string]any, *mcp.CallToolResult) {
	args := request.GetArguments()
	raw, ok := args["uda"]
	if !ok || raw == nil {
		return nil, nil
	}
	udaMap, ok := raw.(map[string]any)
	if !ok {
		return nil, mcp.NewToolResultError("\"uda\" must be an object")
	}
	if err := domain.ValidateUDA(udaMap); err != nil {
		return nil, mcp.NewToolResultError(err.Error())
	}
	return udaMap, nil
}

// handleTaskCreate handles the tusk_task_create tool.
func (s *Server) handleTaskCreate(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := s.checkBlocked("tusk_task_create", request); result != nil {
		return result, nil
	}
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

	// Optional: level — empty string on create is rejected so the client has
	// to make an explicit choice rather than silently sending a no-op.
	if raw, ok := request.GetArguments()["level"]; ok && raw != nil {
		lvl, ok := raw.(string)
		if !ok {
			return mcp.NewToolResultError("\"level\" must be a string"), nil
		}
		if lvl == "" {
			return mcp.NewToolResultError("level on create requires a value; use modify to clear"), nil
		}
		task.Level = &lvl
	}

	// Optional: project (by name)
	if projectName, err := request.RequireString("project"); err == nil {
		resolved, resolveErr := s.taskSvc.ResolveProjectName(ctx, projectName)
		if resolveErr != nil {
			return toolError(resolveErr, "project "+projectName), nil
		}
		task.ProjectID = resolved
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

	// Optional: uda
	if udaMap, errResult := extractUDA(request); errResult != nil {
		return errResult, nil
	} else if udaMap != nil {
		task.UDA = udaMap
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

	return toolResultJSON(toTaskResponse(task, taskTags, s.projectNames(ctx)))
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
		taskResponse: toTaskResponse(task, tags, s.projectNames(ctx)),
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

// updatePlayerLiveness updates last_seen_at for a player if the player_id is
// provided and valid, and returns ctx wrapped with the acting player so
// downstream service calls can attribute events.
func (s *Server) updatePlayerLiveness(ctx context.Context, request mcp.CallToolRequest) context.Context {
	playerID := request.GetString("player_id", "")
	if playerID == "" {
		return ctx
	}
	if s.playerSvc != nil {
		s.playerSvc.UpdateLastSeen(ctx, playerID) //nolint:errcheck
	}
	return service.WithActor(ctx, playerID)
}

// handleTaskGet handles the tusk_task_get tool. Returns the full task with
// tags, relations, and annotations.
func (s *Server) handleTaskGet(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx = s.updatePlayerLiveness(ctx, request)
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

// handleTaskNext returns the highest-urgency actionable task.
func (s *Server) handleTaskNext(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	task, err := s.taskSvc.Next(ctx)
	if err != nil {
		return toolError(err, "next task"), nil
	}

	resp, err := s.buildTaskGetResponse(ctx, task.ShortID)
	if err != nil {
		return nil, err
	}
	return toolResultJSON(resp)
}

// handleTaskList handles the tusk_task_list tool.
func (s *Server) handleTaskList(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx = s.updatePlayerLiveness(ctx, request)
	// If a filter string is provided, use ParseExpr for full boolean support
	if filterStr, err := request.RequireString("filter"); err == nil {
		expr, parseErrs := filter.ParseExpr(filterStr)
		if len(parseErrs) > 0 {
			return mcp.NewToolResultError("filter parse error: " + filter.FormatErrors(parseErrs)), nil
		}

		var filterExpr domain.FilterExpr
		if expr != nil {
			resolver := s.newResolver(ctx)
			var resolveErrs []error
			filterExpr, resolveErrs = resolver.ResolveExpr(ctx, expr)
			if len(resolveErrs) > 0 {
				return mcp.NewToolResultError(resolveErrs[0].Error()), nil
			}
		}

		tasks, err := s.taskSvc.List(ctx, filterExpr)
		if err != nil {
			return nil, err
		}

		taskIDs := make([]uuid.UUID, len(tasks))
		for i, t := range tasks {
			taskIDs[i] = t.ID
		}
		tagsByTask, err := s.tagSvc.GetTaskTagsBatch(ctx, taskIDs)
		if err != nil {
			return nil, err
		}

		names := s.projectNames(ctx)
		results := make([]taskResponse, len(tasks))
		for i, t := range tasks {
			results[i] = toTaskResponse(t, tagsByTask[t.ID], names)
		}

		return toolResultJSON(results)
	}

	tf := domain.TaskFilter{}

	// Optional: status (string array)
	if statuses := request.GetStringSlice("status", nil); len(statuses) > 0 {
		tf.Statuses = statuses
	}

	// Optional: priority range
	if pMin, err := request.RequireFloat("priority_min"); err == nil {
		v := int(pMin)
		tf.PriorityMin = &v
	}
	if pMax, err := request.RequireFloat("priority_max"); err == nil {
		v := int(pMax)
		tf.PriorityMax = &v
	}

	// Optional: project (by name)
	if projectName, err := request.RequireString("project"); err == nil {
		resolved, resolveErr := s.taskSvc.ResolveProjectName(ctx, projectName)
		if resolveErr != nil {
			return toolError(resolveErr, "project "+projectName), nil
		}
		tf.ProjectID = &resolved
	}

	// Optional: tags include/exclude
	if tags := request.GetStringSlice("tags", nil); len(tags) > 0 {
		tf.Tags = tags
	}
	if exTags := request.GetStringSlice("exclude_tags", nil); len(exTags) > 0 {
		tf.ExcludeTags = exTags
	}

	// Optional: due date range
	if after, err := request.RequireString("due_after"); err == nil {
		d, parseErr := time.Parse(time.RFC3339, after)
		if parseErr != nil {
			return mcp.NewToolResultError("invalid due_after format, expected ISO 8601 (RFC3339)"), nil
		}
		tf.DueAfter = &d
	}
	if before, err := request.RequireString("due_before"); err == nil {
		d, parseErr := time.Parse(time.RFC3339, before)
		if parseErr != nil {
			return mcp.NewToolResultError("invalid due_before format, expected ISO 8601 (RFC3339)"), nil
		}
		tf.DueBefore = &d
	}

	// Optional: parent (direct children)
	if parentShortID, err := request.RequireString("parent"); err == nil {
		parent, lookupErr := s.taskSvc.GetByShortID(ctx, parentShortID)
		if lookupErr != nil {
			return toolError(lookupErr, "parent task "+parentShortID), nil
		}
		tf.ParentID = &parent.ID
	}

	// Optional: root (all descendants)
	if rootShortID, err := request.RequireString("root"); err == nil {
		root, lookupErr := s.taskSvc.GetByShortID(ctx, rootShortID)
		if lookupErr != nil {
			return toolError(lookupErr, "root task "+rootShortID), nil
		}
		tf.RootID = &root.ID
	}

	// Optional: title substring
	if title, err := request.RequireString("title"); err == nil {
		tf.TitleContains = &title
	}

	// Optional: description substring
	if desc, err := request.RequireString("description"); err == nil {
		tf.DescriptionContains = &desc
	}

	tasks, err := s.taskSvc.List(ctx, &domain.TermFilter{TaskFilter: tf})
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

	names := s.projectNames(ctx)
	results := make([]taskResponse, len(tasks))
	for i, t := range tasks {
		results[i] = toTaskResponse(t, tagsByTask[t.ID], names)
	}

	return toolResultJSON(results)
}

// handleTaskModify handles the tusk_task_modify tool.
func (s *Server) handleTaskModify(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := s.checkBlocked("tusk_task_modify", request); result != nil {
		return result, nil
	}
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
	// Optional: level — empty string clears, non-empty sets.
	if raw, ok := request.GetArguments()["level"]; ok && raw != nil {
		lvl, ok := raw.(string)
		if !ok {
			return mcp.NewToolResultError("\"level\" must be a string"), nil
		}
		if lvl == "" {
			var nilStr *string
			upd.Level = &nilStr
		} else {
			lp := &lvl
			upd.Level = &lp
		}
	}
	if p, err := request.RequireFloat("priority"); err == nil {
		v := int(p)
		upd.Priority = &v
	}

	// Optional: project (by name)
	if projectName, err := request.RequireString("project"); err == nil {
		resolved, resolveErr := s.taskSvc.ResolveProjectName(ctx, projectName)
		if resolveErr != nil {
			return toolError(resolveErr, "project "+projectName), nil
		}
		upd.ProjectID = &resolved
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

	// Optional: uda (merge semantics — empty string value removes key)
	if udaMap, errResult := extractUDA(request); errResult != nil {
		return errResult, nil
	} else if udaMap != nil {
		upd.UDA = &udaMap
	}

	// Optional: urgency_overrides (RFC 7396 merge patch) and
	// urgency_overrides_clear (one-shot reset before re-patching).
	args := request.GetArguments()
	rawPatch, rawPatchPresent := args["urgency_overrides"]
	if rawPatchPresent && rawPatch == nil {
		return mcp.NewToolResultError("urgency_overrides: null is not supported; use urgency_overrides_clear: true to drop all overrides"), nil
	}

	var clearAll bool
	if raw, ok := args["urgency_overrides_clear"]; ok && raw != nil {
		v, ok := raw.(bool)
		if !ok {
			return mcp.NewToolResultError(fmt.Sprintf("urgency_overrides_clear: must be a boolean, got %T", raw)), nil
		}
		clearAll = v
	}

	var patch *domain.UrgencyOverridesPatch
	if rawPatch != nil {
		rawMap, ok := rawPatch.(map[string]any)
		if !ok {
			return mcp.NewToolResultError(fmt.Sprintf("urgency_overrides: must be an object, got %T", rawPatch)), nil
		}
		if err := domain.ValidateUrgencyOverridesPatch(rawMap); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		patch = &domain.UrgencyOverridesPatch{
			Clear: make(map[string]bool),
			Set:   make(map[string]float64),
		}
		for key, value := range rawMap {
			if value == nil {
				patch.Clear[key] = true
				continue
			}
			switch v := value.(type) {
			case float64:
				patch.Set[key] = v
			case float32:
				patch.Set[key] = float64(v)
			case int:
				patch.Set[key] = float64(v)
			case int64:
				patch.Set[key] = float64(v)
			default:
				return mcp.NewToolResultError(fmt.Sprintf("urgency_overrides: unexpected numeric type %T for key %q", v, key)), nil
			}
		}
	}

	if clearAll {
		if patch == nil {
			patch = &domain.UrgencyOverridesPatch{
				Clear: make(map[string]bool),
				Set:   make(map[string]float64),
			}
		}
		patch.ClearAll = true
	}

	if patch != nil && !patch.ClearAll && len(patch.Clear) == 0 && len(patch.Set) == 0 {
		patch = nil
	}
	if patch != nil {
		upd.UrgencyMergePatch = patch
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

	return toolResultJSON(toTaskResponse(updated, taskTags, s.projectNames(ctx)))
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

	return toolResultJSON(toTaskResponse(updated, tags, s.projectNames(ctx)))
}

// handleTaskStart handles the tusk_task_start tool.
func (s *Server) handleTaskStart(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := s.checkBlocked("tusk_task_start", request); result != nil {
		return result, nil
	}
	shortID, err := request.RequireString("short_id")
	if err != nil {
		return mcp.NewToolResultError("short_id is required"), nil
	}
	version, err := request.RequireFloat("version")
	if err != nil {
		return mcp.NewToolResultError("version is required"), nil
	}

	playerID := request.GetString("player_id", "")
	ctx = service.WithActor(ctx, playerID)

	// Auto-register player as agent if provided
	if playerID != "" && s.playerSvc != nil {
		if _, regErr := s.playerSvc.Register(ctx, playerID, "agent"); regErr != nil {
			if !errors.Is(regErr, domain.ErrConflict) {
				return toolError(regErr, "auto-registering player"), nil
			}
		}
	}

	updated, err := s.taskSvc.Start(ctx, shortID, int(version), playerID)
	if err != nil {
		return toolError(err, "task "+shortID), nil
	}

	tags, err := s.tagSvc.GetTaskTags(ctx, updated.ID)
	if err != nil {
		return nil, err
	}

	return toolResultJSON(toTaskResponse(updated, tags, s.projectNames(ctx)))
}

// handleTaskDone handles the tusk_task_done tool.
func (s *Server) handleTaskDone(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := s.checkBlocked("tusk_task_done", request); result != nil {
		return result, nil
	}
	return s.handleTaskTransition(ctx, request, s.taskSvc.Complete)
}

// handleTaskDelete handles the tusk_task_delete tool.
func (s *Server) handleTaskDelete(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := s.checkBlocked("tusk_task_delete", request); result != nil {
		return result, nil
	}
	return s.handleTaskTransition(ctx, request, s.taskSvc.Delete)
}

// handleTaskAnnotate handles the tusk_task_annotate tool.
func (s *Server) handleTaskAnnotate(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := s.checkBlocked("tusk_task_annotate", request); result != nil {
		return result, nil
	}
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
	ID                      string                   `json:"id"`
	ShortID                 string                   `json:"short_id"`
	ParentID                *string                  `json:"parent_id,omitempty"`
	ProjectID               string                   `json:"project_id"`
	Title                   string                   `json:"title"`
	Description             string                   `json:"description"`
	Status                  string                   `json:"status"`
	Priority                int                      `json:"priority"`
	Version                 int                      `json:"version"`
	DueAt                   *string                  `json:"due_at,omitempty"`
	WaitUntil               *string                  `json:"wait_until,omitempty"`
	RecurrenceRule          *string                  `json:"recurrence_rule,omitempty"`
	CreatedAt               string                   `json:"created_at"`
	ModifiedAt              string                   `json:"modified_at"`
	UrgencyOverrides        *domain.UrgencyOverrides `json:"urgency_overrides,omitempty"`
	EffectiveUrgencyWeights *urgencyWeightsJSON      `json:"effective_urgency_weights,omitempty"`
	Children                []treeNodeResponse       `json:"children"`
}

func toTreeNodeResponse(task *domain.Task, projectNames *projectNameCache) treeNodeResponse {
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
	r.ProjectID = projectNames.name(task.ProjectID)
	if task.DueAt != nil {
		s := task.DueAt.Format(time.RFC3339)
		r.DueAt = &s
	}
	if task.WaitUntil != nil {
		s := task.WaitUntil.Format(time.RFC3339)
		r.WaitUntil = &s
	}
	r.RecurrenceRule = task.RecurrenceRule
	if task.UrgencyOverrides != nil {
		r.UrgencyOverrides = task.UrgencyOverrides
	}
	if task.EffectiveWeights != nil {
		r.EffectiveUrgencyWeights = toUrgencyWeightsJSON(*task.EffectiveWeights)
	}
	return r
}

// buildTreeResponse constructs a nested tree from a flat task list.
// If rootID is non-nil, only that task is the root. Otherwise, tasks without
// a parent (or whose parent is not in the set) become roots.
func buildTreeResponse(tasks []*domain.Task, rootID *uuid.UUID, projectNames *projectNameCache) []treeNodeResponse {
	type node struct {
		resp     treeNodeResponse
		children []*node
	}

	byID := make(map[uuid.UUID]*node, len(tasks))
	for _, t := range tasks {
		n := &node{resp: toTreeNodeResponse(t, projectNames)}
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

// handleTaskLink handles the tusk_task_link tool.
func (s *Server) handleTaskLink(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := s.checkBlocked("tusk_task_link", request); result != nil {
		return result, nil
	}
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

// handleTaskUnlink handles the tusk_task_unlink tool.
func (s *Server) handleTaskUnlink(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := s.checkBlocked("tusk_task_unlink", request); result != nil {
		return result, nil
	}
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

// taxonomyPayload mirrors the `{"ranks": [...]}` shape used by MCP taxonomy
// inputs and outputs. A pointer to it carries tristate semantics: nil =
// omitted (inherit workspace default), &{Ranks:[]} = explicit opt-out,
// &{Ranks: populated} = project-specific override.
type taxonomyPayload struct {
	Ranks [][]string `json:"ranks"`
}

// effectiveTaxonomyResponse is the per-project derived taxonomy reported in
// project responses. Source is one of "workspace_default", "project_override",
// or "none".
type effectiveTaxonomyResponse struct {
	Ranks  [][]string `json:"ranks"`
	Source string     `json:"source"`
}

// projectSettingsResponse mirrors domain.ProjectSettings but wraps the
// taxonomy override in a {"ranks": [...]} envelope to match MCP input shape.
type projectSettingsResponse struct {
	AutoCompleteParent *domain.AutoCompleteConfig `json:"auto_complete_parent,omitempty"`
	AutoRevertParent   *domain.AutoRevertConfig   `json:"auto_revert_parent,omitempty"`
	Urgency            *domain.UrgencyOverrides   `json:"urgency,omitempty"`
	NoteWindowSize     *int                       `json:"note_window_size,omitempty"`
	Taxonomy           *taxonomyPayload           `json:"taxonomy,omitempty"`
}

// projectResponse is the JSON structure returned by project tools.
type projectResponse struct {
	ID                string                    `json:"id"`
	Workflow          string                    `json:"workflow"`
	Settings          projectSettingsResponse   `json:"settings"`
	EffectiveTaxonomy effectiveTaxonomyResponse `json:"effective_taxonomy"`
}

func taxonomySourceName(src service.TaxonomySource) string {
	switch src {
	case service.TaxonomySourceProjectOverride:
		return "project_override"
	case service.TaxonomySourceWorkspace:
		return "workspace_default"
	default:
		return "none"
	}
}

func projectSettingsToResponse(settings domain.ProjectSettings) projectSettingsResponse {
	out := projectSettingsResponse{
		AutoCompleteParent: settings.AutoCompleteParent,
		AutoRevertParent:   settings.AutoRevertParent,
		Urgency:            settings.Urgency,
		NoteWindowSize:     settings.NoteWindowSize,
	}
	if settings.Taxonomy != nil {
		ranks := [][]string(*settings.Taxonomy)
		if ranks == nil {
			ranks = [][]string{}
		}
		out.Taxonomy = &taxonomyPayload{Ranks: ranks}
	}
	return out
}

func (s *Server) toProjectResponse(p *domain.Project, workflowName string) projectResponse {
	effective, source := s.projectSvc.EffectiveTaxonomy(p)
	ranks := [][]string(effective)
	if ranks == nil {
		ranks = [][]string{}
	}
	return projectResponse{
		ID:       p.Name,
		Workflow: workflowName,
		Settings: projectSettingsToResponse(p.Settings),
		EffectiveTaxonomy: effectiveTaxonomyResponse{
			Ranks:  ranks,
			Source: taxonomySourceName(source),
		},
	}
}

// handleProjectList handles the tusk_project_list tool.
func (s *Server) handleProjectList(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projects, err := s.projectSvc.List(ctx)
	if err != nil {
		return nil, err
	}

	workflows, err := s.workflowSvc.List(ctx)
	if err != nil {
		return nil, err
	}
	wfNames := make(map[uuid.UUID]string, len(workflows))
	for _, wf := range workflows {
		wfNames[wf.ID] = wf.Name
	}

	results := make([]projectResponse, len(projects))
	for i, p := range projects {
		results[i] = s.toProjectResponse(p, wfNames[p.WorkflowID])
	}

	return toolResultJSON(results)
}

// statusResponse is the JSON structure for a workflow status.
type statusResponse struct {
	Name  string   `json:"name"`
	Roles []string `json:"roles,omitempty"`
}

// workflowListResponse is the JSON structure returned by the workflow list tool.
type workflowListResponse struct {
	Name        string               `json:"name"`
	Statuses    []statusResponse     `json:"statuses"`
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
		names := wf.StatusNames()
		statuses := make([]statusResponse, len(names))
		for j, name := range names {
			sc := wf.Statuses[name]
			roles := make([]string, len(sc.Roles))
			for k, r := range sc.Roles {
				roles[k] = string(r)
			}
			statuses[j] = statusResponse{Name: name, Roles: roles}
		}
		results[i] = workflowListResponse{
			Name:        wf.Name,
			Statuses:    statuses,
			Transitions: transitions,
			Projects:    projectIDs,
		}
	}

	return toolResultJSON(results)
}

// playerResponse is the JSON structure returned by player tools.
type playerResponse struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	RegisteredAt string `json:"registered_at"`
	LastSeenAt   string `json:"last_seen_at"`
}

func toPlayerResponse(p *domain.Player) playerResponse {
	return playerResponse{
		ID:           p.ID,
		Type:         p.Type,
		RegisteredAt: p.RegisteredAt.Format(time.RFC3339),
		LastSeenAt:   p.LastSeenAt.Format(time.RFC3339),
	}
}

// handlePlayerRegister handles the tusk_player_register tool.
func (s *Server) handlePlayerRegister(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := s.checkBlocked("tusk_player_register", request); result != nil {
		return result, nil
	}
	playerID, err := request.RequireString("player_id")
	if err != nil {
		return mcp.NewToolResultError("player_id is required"), nil
	}
	ctx = service.WithActor(ctx, playerID)

	player, err := s.playerSvc.Register(ctx, playerID, "agent")
	if err != nil {
		return toolError(err, "player "+playerID), nil
	}

	return toolResultJSON(toPlayerResponse(player))
}

// handleTaskClaim handles the tusk_task_claim tool.
func (s *Server) handleTaskClaim(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := s.checkBlocked("tusk_task_claim", request); result != nil {
		return result, nil
	}
	shortID, err := request.RequireString("short_id")
	if err != nil {
		return mcp.NewToolResultError("short_id is required"), nil
	}
	playerID, err := request.RequireString("player_id")
	if err != nil {
		return mcp.NewToolResultError("player_id is required"), nil
	}
	ctx = service.WithActor(ctx, playerID)
	version, err := request.RequireFloat("version")
	if err != nil {
		return mcp.NewToolResultError("version is required"), nil
	}

	updated, err := s.taskSvc.Claim(ctx, shortID, playerID, int(version))
	if err != nil {
		return toolError(err, "task "+shortID), nil
	}

	tags, err := s.tagSvc.GetTaskTags(ctx, updated.ID)
	if err != nil {
		return nil, err
	}

	return toolResultJSON(toTaskResponse(updated, tags, s.projectNames(ctx)))
}

// handleTaskRelease handles the tusk_task_release tool.
func (s *Server) handleTaskRelease(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := s.checkBlocked("tusk_task_release", request); result != nil {
		return result, nil
	}
	shortID, err := request.RequireString("short_id")
	if err != nil {
		return mcp.NewToolResultError("short_id is required"), nil
	}
	playerID, err := request.RequireString("player_id")
	if err != nil {
		return mcp.NewToolResultError("player_id is required"), nil
	}
	ctx = service.WithActor(ctx, playerID)
	version, err := request.RequireFloat("version")
	if err != nil {
		return mcp.NewToolResultError("version is required"), nil
	}

	updated, err := s.taskSvc.Release(ctx, shortID, playerID, int(version))
	if err != nil {
		return toolError(err, "task "+shortID), nil
	}

	tags, err := s.tagSvc.GetTaskTags(ctx, updated.ID)
	if err != nil {
		return nil, err
	}

	return toolResultJSON(toTaskResponse(updated, tags, s.projectNames(ctx)))
}

// handleTaskAvailable handles the tusk_task_available tool.
func (s *Server) handleTaskAvailable(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx = s.updatePlayerLiveness(ctx, request)

	playerID, err := request.RequireString("player_id")
	if err != nil {
		return mcp.NewToolResultError("player_id is required"), nil
	}
	ctx = service.WithActor(ctx, playerID)

	// Auto-register player as agent
	if s.playerSvc != nil {
		if _, regErr := s.playerSvc.Register(ctx, playerID, "agent"); regErr != nil {
			if !errors.Is(regErr, domain.ErrConflict) {
				return toolError(regErr, "auto-registering player"), nil
			}
		}
	}

	var filterExpr domain.FilterExpr

	if filterStr, err := request.RequireString("filter"); err == nil {
		expr, parseErrs := filter.ParseExpr(filterStr)
		if len(parseErrs) > 0 {
			return mcp.NewToolResultError("filter parse error: " + filter.FormatErrors(parseErrs)), nil
		}

		if expr != nil {
			resolver := s.newResolver(ctx)
			var resolveErrs []error
			filterExpr, resolveErrs = resolver.ResolveExpr(ctx, expr)
			if len(resolveErrs) > 0 {
				return mcp.NewToolResultError(resolveErrs[0].Error()), nil
			}
		}
	}

	tasks, err := s.taskSvc.Available(ctx, filterExpr)
	if err != nil {
		return nil, err
	}

	taskIDs := make([]uuid.UUID, len(tasks))
	for i, t := range tasks {
		taskIDs[i] = t.ID
	}
	tagsByTask, err := s.tagSvc.GetTaskTagsBatch(ctx, taskIDs)
	if err != nil {
		return nil, err
	}

	names := s.projectNames(ctx)
	results := make([]taskResponse, len(tasks))
	for i, t := range tasks {
		results[i] = toTaskResponse(t, tagsByTask[t.ID], names)
	}

	return toolResultJSON(results)
}

// handleTaskPop handles the tusk_task_pop tool.
func (s *Server) handleTaskPop(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := s.checkBlocked("tusk_task_pop", request); result != nil {
		return result, nil
	}
	playerID, err := request.RequireString("player_id")
	if err != nil {
		return mcp.NewToolResultError("player_id is required"), nil
	}
	ctx = service.WithActor(ctx, playerID)

	// Auto-register player as agent
	if s.playerSvc != nil {
		if _, regErr := s.playerSvc.Register(ctx, playerID, "agent"); regErr != nil {
			if !errors.Is(regErr, domain.ErrConflict) {
				return toolError(regErr, "auto-registering player"), nil
			}
		}
	}

	// Parse optional filter
	var filterExpr domain.FilterExpr
	if filterStr, err := request.RequireString("filter"); err == nil {
		expr, parseErrs := filter.ParseExpr(filterStr)
		if len(parseErrs) > 0 {
			return mcp.NewToolResultError("filter parse error: " + filter.FormatErrors(parseErrs)), nil
		}
		if expr != nil {
			resolver := s.newResolver(ctx)
			var resolveErrs []error
			filterExpr, resolveErrs = resolver.ResolveExpr(ctx, expr)
			if len(resolveErrs) > 0 {
				return mcp.NewToolResultError(resolveErrs[0].Error()), nil
			}
		}
	}

	task, err := s.taskSvc.Pop(ctx, playerID, filterExpr)
	if err != nil {
		if errors.Is(err, domain.ErrNoAvailableTasks) {
			return mcp.NewToolResultText("No available tasks matching the given filters"), nil
		}
		return toolError(err, "pop"), nil
	}

	tags, err := s.tagSvc.GetTaskTags(ctx, task.ID)
	if err != nil {
		return nil, err
	}

	return toolResultJSON(toTaskResponse(task, tags, s.projectNames(ctx)))
}

// handleTaskTree handles the tusk_task_tree tool.
func (s *Server) handleTaskTree(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx = s.updatePlayerLiveness(ctx, request)
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
		tasks, listErr = s.taskSvc.List(ctx, &domain.TermFilter{TaskFilter: filter})
		if listErr != nil {
			return nil, listErr
		}
	}

	tree := buildTreeResponse(tasks, rootID, s.projectNames(ctx))
	return toolResultJSON(tree)
}
