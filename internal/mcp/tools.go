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

// toolResultJSON marshals payload as indented JSON and wraps it in an MCP tool result.
func toolResultJSON(payload any) (*mcp.CallToolResult, error) {
	body, marshalErr := json.MarshalIndent(payload, "", "  ")

	if marshalErr != nil {
		return nil, marshalErr
	}

	return mcp.NewToolResultText(string(body)), nil
}

// toolError translates a domain error into an MCP tool-result error.
// context is optional extra info for not-found errors (e.g., "task abc123").
// *domain.TaxonomyError surfaces through taxonomyErrorResult so clients can
// branch on the structured `taxonomy_violation` payload.
func toolError(err error, context string) *mcp.CallToolResult {
	var taxonomyErr *domain.TaxonomyError
	if errors.As(err, &taxonomyErr) {
		return taxonomyErrorResult(taxonomyErr)
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

func toUrgencyWeightsJSON(weights domain.ResolvedUrgencyWeights) *urgencyWeightsJSON {
	return &urgencyWeightsJSON{
		PriorityWeight:    weights.PriorityWeight,
		DueWeight:         weights.DueWeight,
		AgeWeight:         weights.AgeWeight,
		ActiveWeight:      weights.ActiveWeight,
		BlockingWeight:    weights.BlockingWeight,
		BlockedWeight:     weights.BlockedWeight,
		TagsWeight:        weights.TagsWeight,
		ProjectWeight:     weights.ProjectWeight,
		AnnotationsWeight: weights.AnnotationsWeight,
		WaitingWeight:     weights.WaitingWeight,
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

func (cache *projectNameCache) name(id uuid.UUID) string {
	if cache == nil || cache.svc == nil {
		return id.String()
	}
	if cached, ok := cache.cache[id]; ok {
		return cached
	}
	project, lookupErr := cache.svc.GetByID(cache.ctx, id)

	if lookupErr != nil {
		cache.cache[id] = id.String()
		return id.String()
	}

	cache.cache[id] = project.Name
	return project.Name
}

// projectNames returns a new per-invocation project-name cache.
func (server *Server) projectNames(ctx context.Context) *projectNameCache {
	return newProjectNameCache(ctx, server.projectSvc)
}

func toTaskResponse(task *domain.Task, tags []*domain.Tag, projectNames *projectNameCache) taskResponse {
	resp := taskResponse{
		ID:          task.ID.String(),
		ShortID:     task.ShortID,
		Title:       task.Title,
		Description: task.Description,
		Status:      task.Status,
		Priority:    task.Priority,
		Version:     task.Version,
		CreatedAt:   task.CreatedAt.Format(time.RFC3339),
		ModifiedAt:  task.ModifiedAt.Format(time.RFC3339),
		Urgency:     task.Urgency,
	}
	if task.ParentID != nil {
		parentStr := task.ParentID.String()
		resp.ParentID = &parentStr
	}
	resp.ProjectID = projectNames.name(task.ProjectID)
	if task.DueAt != nil {
		dueStr := task.DueAt.Format(time.RFC3339)
		resp.DueAt = &dueStr
	}
	if task.WaitUntil != nil {
		waitStr := task.WaitUntil.Format(time.RFC3339)
		resp.WaitUntil = &waitStr
	}
	resp.RecurrenceRule = task.RecurrenceRule
	resp.Level = task.Level
	resp.Order = task.Order
	resp.UDA = task.UDA
	resp.Tags = make([]string, len(tags))
	for index, tg := range tags {
		resp.Tags[index] = tg.Name
	}
	if task.ClaimedBy != nil {
		resp.ClaimedBy = task.ClaimedBy
	}
	if task.ClaimedAt != nil {
		claimedStr := task.ClaimedAt.Format(time.RFC3339)
		resp.ClaimedAt = &claimedStr
	}
	if task.UrgencyOverrides != nil {
		resp.UrgencyOverrides = task.UrgencyOverrides
	}
	if task.EffectiveWeights != nil {
		resp.EffectiveUrgencyWeights = toUrgencyWeightsJSON(*task.EffectiveWeights)
	}
	return resp
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
func (server *Server) handleTaskCreate(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := server.checkBlocked("tusk_task_create", request); result != nil {
		return result, nil
	}
	title, titleErr := request.RequireString("title")

	if titleErr != nil {
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
	if priority, err := request.RequireFloat("priority"); err == nil {
		task.Priority = int(priority)
	}

	// Optional: level — empty string on create is rejected so the client has
	// to make an explicit choice rather than silently sending a no-op.
	if raw, ok := request.GetArguments()["level"]; ok && raw != nil {
		level, ok := raw.(string)
		if !ok {
			return mcp.NewToolResultError("\"level\" must be a string"), nil
		}
		if level == "" {
			return mcp.NewToolResultError("level on create requires a value; use modify to clear"), nil
		}
		task.Level = &level
	}

	// Optional: project (by name)
	if projectName, err := request.RequireString("project"); err == nil {
		resolved, resolveErr := server.taskSvc.ResolveProjectName(ctx, projectName)

		if resolveErr != nil {
			return toolError(resolveErr, "project "+projectName), nil
		}

		task.ProjectID = resolved
	}

	// Optional: parent (by short_id)
	if parentShortID, err := request.RequireString("parent"); err == nil {
		parent, lookupErr := server.taskSvc.GetByShortID(ctx, parentShortID)

		if lookupErr != nil {
			return toolError(lookupErr, "parent task "+parentShortID), nil
		}

		task.ParentID = &parent.ID
	}

	// Optional: due (ISO 8601)
	if dueStr, err := request.RequireString("due"); err == nil {
		dueTime, parseErr := time.Parse(time.RFC3339, dueStr)

		if parseErr != nil {
			return mcp.NewToolResultError("invalid due date format, expected ISO 8601 (RFC3339)"), nil
		}

		task.DueAt = &dueTime
	}

	// Optional: wait_until (ISO 8601)
	if waitStr, err := request.RequireString("wait_until"); err == nil {
		waitTime, parseErr := time.Parse(time.RFC3339, waitStr)

		if parseErr != nil {
			return mcp.NewToolResultError("invalid wait_until format, expected ISO 8601 (RFC3339)"), nil
		}

		task.WaitUntil = &waitTime
	}

	// Optional: uda
	if udaMap, errResult := extractUDA(request); errResult != nil {
		return errResult, nil
	} else if udaMap != nil {
		task.UDA = udaMap
	}

	if createErr := server.taskSvc.Create(ctx, task); createErr != nil {
		return toolError(createErr, ""), nil
	}

	// Assign tags if provided
	tags := request.GetStringSlice("tags", nil)
	if len(tags) > 0 {
		if tagErr := server.tagSvc.AssignToTask(ctx, task.ID, tags); tagErr != nil {
			return toolError(tagErr, ""), nil
		}
	}

	// Fetch tags for response
	taskTags, tagsErr := server.tagSvc.GetTaskTags(ctx, task.ID)

	if tagsErr != nil {
		return nil, tagsErr
	}

	return toolResultJSON(toTaskResponse(task, taskTags, server.projectNames(ctx)))
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
func (server *Server) buildTaskGetResponse(ctx context.Context, shortID string) (*taskGetResponse, error) {
	task, taskErr := server.taskSvc.GetByShortID(ctx, shortID)

	if taskErr != nil {
		return nil, taskErr
	}

	tags, tagsErr := server.tagSvc.GetTaskTags(ctx, task.ID)

	if tagsErr != nil {
		return nil, tagsErr
	}

	annotations, annotationsErr := server.taskSvc.GetAnnotations(ctx, shortID)

	if annotationsErr != nil {
		return nil, annotationsErr
	}

	rels, relsErr := server.relationSvc.GetByTask(ctx, shortID)

	if relsErr != nil {
		return nil, relsErr
	}

	resp := &taskGetResponse{
		taskResponse: toTaskResponse(task, tags, server.projectNames(ctx)),
		Annotations:  make([]annotationResponse, len(annotations)),
		Relations:    make([]relationResponse, 0, len(rels)),
	}

	for index, annotation := range annotations {
		resp.Annotations[index] = annotationResponse{
			ID:        annotation.ID.String(),
			TaskID:    annotation.TaskID.String(),
			Body:      annotation.Body,
			CreatedAt: annotation.CreatedAt.Format(time.RFC3339),
		}
	}

	for _, rel := range rels {
		relResp := relationResponse{
			ID:           rel.ID.String(),
			SourceID:     rel.SourceID.String(),
			TargetID:     rel.TargetID.String(),
			RelationType: rel.RelationType,
			CreatedAt:    rel.CreatedAt.Format(time.RFC3339),
		}
		if rel.TargetID == task.ID {
			switch rel.RelationType {
			case "blocks":
				relResp.DirectionLabel = "blocked_by"
			case "relates_to":
				relResp.DirectionLabel = "related_to"
			case "duplicates":
				relResp.DirectionLabel = "duplicated_by"
			}
			if other, lookupErr := server.taskSvc.GetByID(ctx, rel.SourceID); lookupErr == nil {
				relResp.RelatedShortID = other.ShortID
				relResp.RelatedTitle = other.Title
			}
		} else {
			relResp.DirectionLabel = rel.RelationType
			if other, lookupErr := server.taskSvc.GetByID(ctx, rel.TargetID); lookupErr == nil {
				relResp.RelatedShortID = other.ShortID
				relResp.RelatedTitle = other.Title
			}
		}
		resp.Relations = append(resp.Relations, relResp)
	}

	return resp, nil
}

// updatePlayerLiveness updates last_seen_at for a player if the player_id is
// provided and valid, and returns ctx wrapped with the acting player so
// downstream service calls can attribute events.
func (server *Server) updatePlayerLiveness(ctx context.Context, request mcp.CallToolRequest) context.Context {
	playerID := request.GetString("player_id", "")
	if playerID == "" {
		return ctx
	}
	if server.playerSvc != nil {
		server.playerSvc.UpdateLastSeen(ctx, playerID) //nolint:errcheck
	}
	return service.WithActor(ctx, playerID)
}

// handleTaskGet handles the tusk_task_get tool. Returns the full task with
// tags, relations, and annotations.
func (server *Server) handleTaskGet(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx = server.updatePlayerLiveness(ctx, request)
	shortID, shortIDErr := request.RequireString("short_id")

	if shortIDErr != nil {
		return mcp.NewToolResultError("short_id is required"), nil
	}

	resp, respErr := server.buildTaskGetResponse(ctx, shortID)

	if respErr != nil {
		return toolError(respErr, "task "+shortID), nil
	}

	return toolResultJSON(resp)
}

// handleTaskNext returns the highest-urgency actionable task.
func (server *Server) handleTaskNext(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	task, taskErr := server.taskSvc.Next(ctx)

	if taskErr != nil {
		return toolError(taskErr, "next task"), nil
	}

	resp, respErr := server.buildTaskGetResponse(ctx, task.ShortID)

	if respErr != nil {
		return nil, respErr
	}

	return toolResultJSON(resp)
}

// buildTaskFilter constructs a domain.TaskFilter from a request's
// structured filter params. It accepts a SUPERSET of the param keys any
// individual tool registration exposes — each tool's MCP registration
// (in server.go) controls which params are reachable. Unset / unknown
// params are silently no-ops.
//
// On invalid input (malformed date, unresolvable project / parent /
// root short_id) it returns a non-nil *mcp.CallToolResult that the
// caller must surface as the tool's reply.
func (server *Server) buildTaskFilter(ctx context.Context, request mcp.CallToolRequest) (*domain.TaskFilter, *mcp.CallToolResult) {
	tf := &domain.TaskFilter{}

	// Optional: status (string array)
	if statuses := request.GetStringSlice("status", nil); len(statuses) > 0 {
		tf.Statuses = statuses
	}

	// Optional: priority range
	if pMin, err := request.RequireFloat("priority_min"); err == nil {
		minVal := int(pMin)
		tf.PriorityMin = &minVal
	}
	if pMax, err := request.RequireFloat("priority_max"); err == nil {
		maxVal := int(pMax)
		tf.PriorityMax = &maxVal
	}

	// Optional: project (by name)
	if projectName, err := request.RequireString("project"); err == nil {
		resolved, resolveErr := server.taskSvc.ResolveProjectName(ctx, projectName)

		if resolveErr != nil {
			return nil, toolError(resolveErr, "project "+projectName)
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
		dueAfter, parseErr := time.Parse(time.RFC3339, after)

		if parseErr != nil {
			return nil, mcp.NewToolResultError("invalid due_after format, expected ISO 8601 (RFC3339)")
		}

		tf.DueAfter = &dueAfter
	}
	if before, err := request.RequireString("due_before"); err == nil {
		dueBefore, parseErr := time.Parse(time.RFC3339, before)

		if parseErr != nil {
			return nil, mcp.NewToolResultError("invalid due_before format, expected ISO 8601 (RFC3339)")
		}

		tf.DueBefore = &dueBefore
	}

	// Optional: parent (direct children)
	if parentShortID, err := request.RequireString("parent"); err == nil {
		parent, lookupErr := server.taskSvc.GetByShortID(ctx, parentShortID)

		if lookupErr != nil {
			return nil, toolError(lookupErr, "parent task "+parentShortID)
		}

		tf.ParentID = &parent.ID
	}

	// Optional: root (all descendants)
	if rootShortID, err := request.RequireString("root"); err == nil {
		root, lookupErr := server.taskSvc.GetByShortID(ctx, rootShortID)

		if lookupErr != nil {
			return nil, toolError(lookupErr, "root task "+rootShortID)
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

	// Optional: level — single-value param maps to TaskFilter.Levels.
	// Reachable only by tools that register the "level" parameter.
	if level, err := request.RequireString("level"); err == nil && level != "" {
		tf.Levels = []string{level}
	}

	return tf, nil
}

// handleTaskList handles the tusk_task_list tool.
func (server *Server) handleTaskList(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx = server.updatePlayerLiveness(ctx, request)
	// If a filter string is provided, use ParseExpr for full boolean support
	if filterStr, err := request.RequireString("filter"); err == nil {
		expr, parseErrs := filter.ParseExpr(filterStr)

		if len(parseErrs) > 0 {
			return mcp.NewToolResultError("filter parse error: " + filter.FormatErrors(parseErrs)), nil
		}

		var filterExpr domain.FilterExpr
		if expr != nil {
			resolver := server.newResolver(ctx)
			var resolveErrs []error
			filterExpr, resolveErrs = resolver.ResolveExpr(ctx, expr)
			if len(resolveErrs) > 0 {
				return mcp.NewToolResultError(resolveErrs[0].Error()), nil
			}
		}

		tasks, listErr := server.taskSvc.List(ctx, filterExpr)

		if listErr != nil {
			return nil, listErr
		}

		taskIDs := make([]uuid.UUID, len(tasks))
		for index, task := range tasks {
			taskIDs[index] = task.ID
		}
		tagsByTask, tagsErr := server.tagSvc.GetTaskTagsBatch(ctx, taskIDs)

		if tagsErr != nil {
			return nil, tagsErr
		}

		names := server.projectNames(ctx)
		results := make([]taskResponse, len(tasks))
		for index, task := range tasks {
			results[index] = toTaskResponse(task, tagsByTask[task.ID], names)
		}

		return toolResultJSON(results)
	}

	tf, errResult := server.buildTaskFilter(ctx, request)
	if errResult != nil {
		return errResult, nil
	}

	tasks, listErr := server.taskSvc.List(ctx, &domain.TermFilter{TaskFilter: *tf})

	if listErr != nil {
		return nil, listErr
	}

	// Batch-fetch tags for all tasks
	taskIDs := make([]uuid.UUID, len(tasks))
	for index, task := range tasks {
		taskIDs[index] = task.ID
	}
	tagsByTask, tagsErr := server.tagSvc.GetTaskTagsBatch(ctx, taskIDs)

	if tagsErr != nil {
		return nil, tagsErr
	}

	names := server.projectNames(ctx)
	results := make([]taskResponse, len(tasks))
	for index, task := range tasks {
		results[index] = toTaskResponse(task, tagsByTask[task.ID], names)
	}

	return toolResultJSON(results)
}

// rollupResponse is the JSON-rendered shape of a domain.Rollup. Mirrors
// the CLI envelope (internal/tui/summary.go).
type rollupResponse struct {
	Done         int                  `json:"done"`
	Total        int                  `json:"total"`
	Percent      float64              `json:"percent"`
	StatusCounts []domain.StatusCount `json:"status_counts"`
}

// summaryBlockResponse pairs a task with its descendant rollup.
type summaryBlockResponse struct {
	Task   taskResponse   `json:"task"`
	Rollup rollupResponse `json:"rollup"`
}

// summaryResponse is the wire envelope returned by tusk_task_summary.
type summaryResponse struct {
	Mode   string                 `json:"mode"`
	Blocks []summaryBlockResponse `json:"blocks"`
	Totals *rollupResponse        `json:"totals,omitempty"`
}

// toRollupResponse renders a domain.Rollup with status_counts forced to
// an empty slice when nil. JSON consumers (especially TypeScript clients)
// routinely choke on null arrays, so we always emit [].
func toRollupResponse(rollup domain.Rollup) rollupResponse {
	counts := rollup.StatusCounts
	if counts == nil {
		counts = []domain.StatusCount{}
	}
	return rollupResponse{
		Done:         rollup.Done,
		Total:        rollup.Total,
		Percent:      rollup.Percent,
		StatusCounts: counts,
	}
}

// computeMCPTotals sums rollup counts across blocks, mirroring the CLI's
// computeTotals (internal/tui/summary.go). Always returns a non-nil
// pointer so the envelope's "totals" field is consistently present in
// filter and roots modes. StatusCounts in the totals follow first-seen
// order across blocks; same merging rule as domain.AggregateRollup.
func computeMCPTotals(blocks []*domain.SummaryBlock) *domain.Rollup {
	counts := make(map[string]int)
	order := make([]string, 0)
	seen := make(map[string]bool)

	var done, total int
	for _, block := range blocks {
		if block == nil {
			continue
		}
		done += block.Rollup.Done
		total += block.Rollup.Total
		for _, statusCount := range block.Rollup.StatusCounts {
			if !seen[statusCount.Name] {
				seen[statusCount.Name] = true
				order = append(order, statusCount.Name)
			}
			counts[statusCount.Name] += statusCount.Count
		}
	}

	statusCounts := make([]domain.StatusCount, 0, len(order))
	for _, name := range order {
		statusCounts = append(statusCounts, domain.StatusCount{Name: name, Count: counts[name]})
	}

	var percent float64
	if total > 0 {
		percent = float64(done) / float64(total)
	}

	return &domain.Rollup{
		Done:         done,
		Total:        total,
		Percent:      percent,
		StatusCounts: statusCounts,
	}
}

// isEmptyTaskFilter reports whether tf has no fields set. Used to
// distinguish "filter mode with empty filter" from "roots mode" (no
// filter at all → blocks = root tasks). Reads only the fields
// buildTaskFilter populates.
func isEmptyTaskFilter(tf *domain.TaskFilter) bool {
	if tf == nil {
		return true
	}
	return tf.ProjectID == nil &&
		tf.ParentID == nil &&
		tf.RootID == nil &&
		len(tf.Statuses) == 0 &&
		len(tf.Levels) == 0 &&
		len(tf.Tags) == 0 &&
		len(tf.ExcludeTags) == 0 &&
		tf.PriorityMin == nil &&
		tf.PriorityMax == nil &&
		tf.DueAfter == nil &&
		tf.DueBefore == nil &&
		tf.TitleContains == nil &&
		tf.DescriptionContains == nil
}

// summaryToolResult builds the MCP JSON envelope for a summary response.
// The envelope shape mirrors the CLI's `{mode, blocks[], totals?}`. Each
// block's task is rendered through toTaskResponse so MCP consumers see
// the same task fields tusk_task_list, tusk_task_get, etc. emit. Tags
// are batch-fetched in one GetTaskTagsBatch call to avoid N+1.
func (server *Server) summaryToolResult(
	ctx context.Context,
	mode string,
	blocks []*domain.SummaryBlock,
	totals *domain.Rollup,
) (*mcp.CallToolResult, error) {
	taskIDs := make([]uuid.UUID, 0, len(blocks))
	for _, block := range blocks {
		if block == nil || block.Task == nil {
			continue
		}
		taskIDs = append(taskIDs, block.Task.ID)
	}
	tagsByTask, tagsErr := server.tagSvc.GetTaskTagsBatch(ctx, taskIDs)

	if tagsErr != nil {
		return nil, tagsErr
	}

	names := server.projectNames(ctx)

	out := summaryResponse{
		Mode:   mode,
		Blocks: make([]summaryBlockResponse, 0, len(blocks)),
	}
	for _, block := range blocks {
		if block == nil || block.Task == nil {
			continue
		}
		out.Blocks = append(out.Blocks, summaryBlockResponse{
			Task:   toTaskResponse(block.Task, tagsByTask[block.Task.ID], names),
			Rollup: toRollupResponse(block.Rollup),
		})
	}
	if totals != nil {
		totalsResp := toRollupResponse(*totals)
		out.Totals = &totalsResp
	}
	return toolResultJSON(out)
}

// handleTaskSummary handles the tusk_task_summary tool. Precedence:
// short_id (single mode) > filter string > structured params > no
// args (roots mode).
func (server *Server) handleTaskSummary(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx = server.updatePlayerLiveness(ctx, request)

	// Single-id mode: short_id wins over everything.
	if shortID, err := request.RequireString("short_id"); err == nil && shortID != "" {
		if request.GetBool("full", false) {
			return mcp.NewToolResultError("full is not valid in single-id mode"), nil
		}
		task, taskErr := server.taskSvc.GetByShortID(ctx, shortID)

		if taskErr != nil {
			return toolError(taskErr, "task "+shortID), nil
		}

		block, blockErr := server.taskSvc.SummarizeSubtree(ctx, task.ID)

		if blockErr != nil {
			return nil, blockErr
		}

		return server.summaryToolResult(ctx, "single", []*domain.SummaryBlock{block}, nil)
	}

	full := request.GetBool("full", false)

	// Filter-string mode: parse + resolve, ignore structured params.
	if filterStr, err := request.RequireString("filter"); err == nil && filterStr != "" {
		expr, parseErrs := filter.ParseExpr(filterStr)
		if len(parseErrs) > 0 {
			return mcp.NewToolResultError("filter parse error: " + filter.FormatErrors(parseErrs)), nil
		}
		var filterExpr domain.FilterExpr
		if expr != nil {
			resolver := server.newResolver(ctx)
			var resolveErrs []error
			filterExpr, resolveErrs = resolver.ResolveExpr(ctx, expr)

			if len(resolveErrs) > 0 {
				return mcp.NewToolResultError(resolveErrs[0].Error()), nil
			}
		}
		blocks, blocksErr := server.taskSvc.SummarizeBlocks(ctx, filterExpr, full)

		if blocksErr != nil {
			return nil, blocksErr
		}

		return server.summaryToolResult(ctx, "filter", blocks, computeMCPTotals(blocks))
	}

	// Structured-params mode (or no params at all → roots).
	tf, errResult := server.buildTaskFilter(ctx, request)
	if errResult != nil {
		return errResult, nil
	}
	var filterExpr domain.FilterExpr
	mode := "roots"
	if !isEmptyTaskFilter(tf) {
		filterExpr = &domain.TermFilter{TaskFilter: *tf}
		mode = "filter"
	}
	blocks, blocksErr := server.taskSvc.SummarizeBlocks(ctx, filterExpr, full)

	if blocksErr != nil {
		return nil, blocksErr
	}

	return server.summaryToolResult(ctx, mode, blocks, computeMCPTotals(blocks))
}

// handleTaskModify handles the tusk_task_modify tool.
func (server *Server) handleTaskModify(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := server.checkBlocked("tusk_task_modify", request); result != nil {
		return result, nil
	}
	shortID, shortIDErr := request.RequireString("short_id")

	if shortIDErr != nil {
		return mcp.NewToolResultError("short_id is required"), nil
	}

	version, versionErr := request.RequireFloat("version")

	if versionErr != nil {
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
		level, ok := raw.(string)
		if !ok {
			return mcp.NewToolResultError("\"level\" must be a string"), nil
		}
		if level == "" {
			var nilStr *string
			upd.Level = &nilStr
		} else {
			levelPtr := &level
			upd.Level = &levelPtr
		}
	}
	if priority, err := request.RequireFloat("priority"); err == nil {
		priorityVal := int(priority)
		upd.Priority = &priorityVal
	}

	// Optional: project (by name)
	if projectName, err := request.RequireString("project"); err == nil {
		resolved, resolveErr := server.taskSvc.ResolveProjectName(ctx, projectName)

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
			parent, lookupErr := server.taskSvc.GetByShortID(ctx, parentShortID)

			if lookupErr != nil {
				return toolError(lookupErr, "parent task "+parentShortID), nil
			}

			pid := parent.ID
			parentPtr := &pid
			upd.ParentID = &parentPtr
		}
	}

	// Optional: due (ISO 8601, empty string clears)
	if dueStr, err := request.RequireString("due"); err == nil {
		if dueStr == "" {
			var nilTime *time.Time
			upd.DueAt = &nilTime
		} else {
			dueTime, parseErr := time.Parse(time.RFC3339, dueStr)

			if parseErr != nil {
				return mcp.NewToolResultError("invalid due date format, expected ISO 8601 (RFC3339)"), nil
			}

			dp := &dueTime
			upd.DueAt = &dp
		}
	}

	// Optional: wait_until (ISO 8601, empty string clears)
	if waitStr, err := request.RequireString("wait_until"); err == nil {
		if waitStr == "" {
			var nilTime *time.Time
			upd.WaitUntil = &nilTime
		} else {
			waitTime, parseErr := time.Parse(time.RFC3339, waitStr)

			if parseErr != nil {
				return mcp.NewToolResultError("invalid wait_until format, expected ISO 8601 (RFC3339)"), nil
			}

			wp := &waitTime
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
		boolVal, ok := raw.(bool)
		if !ok {
			return mcp.NewToolResultError(fmt.Sprintf("urgency_overrides_clear: must be a boolean, got %T", raw)), nil
		}
		clearAll = boolVal
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
			switch numValue := value.(type) {
			case float64:
				patch.Set[key] = numValue
			case float32:
				patch.Set[key] = float64(numValue)
			case int:
				patch.Set[key] = float64(numValue)
			case int64:
				patch.Set[key] = float64(numValue)
			default:
				return mcp.NewToolResultError(fmt.Sprintf("urgency_overrides: unexpected numeric type %T for key %q", numValue, key)), nil
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

	updated, updateErr := server.taskSvc.Update(ctx, upd)

	if updateErr != nil {
		return toolError(updateErr, "task "+shortID), nil
	}

	// Handle tag changes
	addTags := request.GetStringSlice("add_tags", nil)
	if len(addTags) > 0 {
		if addTagsErr := server.tagSvc.AssignToTask(ctx, updated.ID, addTags); addTagsErr != nil {
			return toolError(addTagsErr, ""), nil
		}
	}
	removeTags := request.GetStringSlice("remove_tags", nil)
	if len(removeTags) > 0 {
		if removeTagsErr := server.tagSvc.RemoveFromTask(ctx, updated.ID, removeTags); removeTagsErr != nil {
			return toolError(removeTagsErr, ""), nil
		}
	}

	// Fetch tags for response
	taskTags, tagsErr := server.tagSvc.GetTaskTags(ctx, updated.ID)

	if tagsErr != nil {
		return nil, tagsErr
	}

	return toolResultJSON(toTaskResponse(updated, taskTags, server.projectNames(ctx)))
}

// handleTaskTransition is a shared helper for start/done/delete handlers.
func (server *Server) handleTaskTransition(ctx context.Context, request mcp.CallToolRequest, transition func(context.Context, string, int) (*domain.Task, error)) (*mcp.CallToolResult, error) {
	shortID, shortIDErr := request.RequireString("short_id")

	if shortIDErr != nil {
		return mcp.NewToolResultError("short_id is required"), nil
	}

	version, versionErr := request.RequireFloat("version")

	if versionErr != nil {
		return mcp.NewToolResultError("version is required"), nil
	}

	updated, transitionErr := transition(ctx, shortID, int(version))

	if transitionErr != nil {
		return toolError(transitionErr, "task "+shortID), nil
	}

	tags, tagsErr := server.tagSvc.GetTaskTags(ctx, updated.ID)

	if tagsErr != nil {
		return nil, tagsErr
	}

	return toolResultJSON(toTaskResponse(updated, tags, server.projectNames(ctx)))
}

// handleTaskStart handles the tusk_task_start tool.
func (server *Server) handleTaskStart(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := server.checkBlocked("tusk_task_start", request); result != nil {
		return result, nil
	}
	shortID, shortIDErr := request.RequireString("short_id")

	if shortIDErr != nil {
		return mcp.NewToolResultError("short_id is required"), nil
	}

	version, versionErr := request.RequireFloat("version")

	if versionErr != nil {
		return mcp.NewToolResultError("version is required"), nil
	}

	playerID := request.GetString("player_id", "")
	ctx = service.WithActor(ctx, playerID)

	// Auto-register player as agent if provided
	if playerID != "" && server.playerSvc != nil {
		if _, regErr := server.playerSvc.Register(ctx, playerID, "agent"); regErr != nil {
			if !errors.Is(regErr, domain.ErrConflict) {
				return toolError(regErr, "auto-registering player"), nil
			}
		}
	}

	updated, updateErr := server.taskSvc.Start(ctx, shortID, int(version), playerID)

	if updateErr != nil {
		return toolError(updateErr, "task "+shortID), nil
	}

	tags, tagsErr := server.tagSvc.GetTaskTags(ctx, updated.ID)

	if tagsErr != nil {
		return nil, tagsErr
	}

	return toolResultJSON(toTaskResponse(updated, tags, server.projectNames(ctx)))
}

// handleTaskDone handles the tusk_task_done tool.
func (server *Server) handleTaskDone(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := server.checkBlocked("tusk_task_done", request); result != nil {
		return result, nil
	}
	return server.handleTaskTransition(ctx, request, server.taskSvc.Complete)
}

// handleTaskDelete handles the tusk_task_delete tool.
func (server *Server) handleTaskDelete(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := server.checkBlocked("tusk_task_delete", request); result != nil {
		return result, nil
	}
	return server.handleTaskTransition(ctx, request, server.taskSvc.Delete)
}

// handleTaskAnnotate handles the tusk_task_annotate tool.
func (server *Server) handleTaskAnnotate(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := server.checkBlocked("tusk_task_annotate", request); result != nil {
		return result, nil
	}
	shortID, shortIDErr := request.RequireString("short_id")

	if shortIDErr != nil {
		return mcp.NewToolResultError("short_id is required"), nil
	}

	body, bodyErr := request.RequireString("body")

	if bodyErr != nil {
		return mcp.NewToolResultError("body is required"), nil
	}

	annotation, annotateErr := server.taskSvc.Annotate(ctx, shortID, body)

	if annotateErr != nil {
		return toolError(annotateErr, "task "+shortID), nil
	}

	return toolResultJSON(annotationResponse{
		ID:        annotation.ID.String(),
		TaskID:    annotation.TaskID.String(),
		Body:      annotation.Body,
		CreatedAt: annotation.CreatedAt.Format(time.RFC3339),
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
	resp := treeNodeResponse{
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
		parentStr := task.ParentID.String()
		resp.ParentID = &parentStr
	}
	resp.ProjectID = projectNames.name(task.ProjectID)
	if task.DueAt != nil {
		dueStr := task.DueAt.Format(time.RFC3339)
		resp.DueAt = &dueStr
	}
	if task.WaitUntil != nil {
		waitStr := task.WaitUntil.Format(time.RFC3339)
		resp.WaitUntil = &waitStr
	}
	resp.RecurrenceRule = task.RecurrenceRule
	if task.UrgencyOverrides != nil {
		resp.UrgencyOverrides = task.UrgencyOverrides
	}
	if task.EffectiveWeights != nil {
		resp.EffectiveUrgencyWeights = toUrgencyWeightsJSON(*task.EffectiveWeights)
	}
	return resp
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
	for _, task := range tasks {
		treeNode := &node{resp: toTreeNodeResponse(task, projectNames)}
		byID[task.ID] = treeNode
	}

	var roots []*node
	for _, task := range tasks {
		treeNode := byID[task.ID]
		if rootID != nil && task.ID == *rootID {
			roots = append(roots, treeNode)
			continue
		}
		if task.ParentID != nil {
			if parent, ok := byID[*task.ParentID]; ok {
				parent.children = append(parent.children, treeNode)
				continue
			}
		}
		if rootID == nil {
			roots = append(roots, treeNode)
		}
	}

	var flatten func(treeNode *node) treeNodeResponse
	flatten = func(treeNode *node) treeNodeResponse {
		resp := treeNode.resp
		resp.Children = make([]treeNodeResponse, len(treeNode.children))
		for index, child := range treeNode.children {
			resp.Children[index] = flatten(child)
		}
		return resp
	}

	result := make([]treeNodeResponse, len(roots))
	for index, root := range roots {
		result[index] = flatten(root)
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
func (server *Server) handleTaskLink(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := server.checkBlocked("tusk_task_link", request); result != nil {
		return result, nil
	}
	source, sourceErr := request.RequireString("source")

	if sourceErr != nil {
		return mcp.NewToolResultError("source is required"), nil
	}

	target, targetErr := request.RequireString("target")

	if targetErr != nil {
		return mcp.NewToolResultError("target is required"), nil
	}

	relType, relTypeErr := request.RequireString("type")

	if relTypeErr != nil {
		return mcp.NewToolResultError("type is required"), nil
	}

	rel, relErr := server.relationSvc.Add(ctx, source, target, relType)

	if relErr != nil {
		return toolError(relErr, ""), nil
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
func (server *Server) handleTaskUnlink(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := server.checkBlocked("tusk_task_unlink", request); result != nil {
		return result, nil
	}
	source, sourceErr := request.RequireString("source")

	if sourceErr != nil {
		return mcp.NewToolResultError("source is required"), nil
	}

	target, targetErr := request.RequireString("target")

	if targetErr != nil {
		return mcp.NewToolResultError("target is required"), nil
	}

	relType, relTypeErr := request.RequireString("type")

	if relTypeErr != nil {
		return mcp.NewToolResultError("type is required"), nil
	}

	if removeErr := server.relationSvc.Remove(ctx, source, target, relType); removeErr != nil {
		return toolError(removeErr, ""), nil
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
	Description       string                    `json:"description"`
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

func (server *Server) toProjectResponse(project *domain.Project, workflowName string) projectResponse {
	effective, source := server.projectSvc.EffectiveTaxonomy(project)
	ranks := [][]string(effective)
	if ranks == nil {
		ranks = [][]string{}
	}
	return projectResponse{
		ID:          project.Name,
		Workflow:    workflowName,
		Description: project.Description,
		Settings:    projectSettingsToResponse(project.Settings),
		EffectiveTaxonomy: effectiveTaxonomyResponse{
			Ranks:  ranks,
			Source: taxonomySourceName(source),
		},
	}
}

// handleProjectList handles the tusk_project_list tool.
func (server *Server) handleProjectList(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projects, projectsErr := server.projectSvc.List(ctx)

	if projectsErr != nil {
		return nil, projectsErr
	}

	workflows, workflowsErr := server.workflowSvc.List(ctx)

	if workflowsErr != nil {
		return nil, workflowsErr
	}

	wfNames := make(map[uuid.UUID]string, len(workflows))
	for _, workflow := range workflows {
		wfNames[workflow.ID] = workflow.Name
	}

	results := make([]projectResponse, len(projects))
	for index, project := range projects {
		results[index] = server.toProjectResponse(project, wfNames[project.WorkflowID])
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
func (server *Server) handleWorkflowList(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	workflows, workflowsErr := server.workflowSvc.List(ctx)

	if workflowsErr != nil {
		return nil, workflowsErr
	}

	results := make([]workflowListResponse, len(workflows))
	for index, workflow := range workflows {
		_, projectIDs, workflowProjectsErr := server.workflowSvc.GetWorkflowWithProjects(ctx, workflow.Name)

		if workflowProjectsErr != nil {
			return nil, workflowProjectsErr
		}

		transitions := make([]transitionResponse, len(workflow.Transitions))
		for jj, transition := range workflow.Transitions {
			transitions[jj] = transitionResponse{From: transition.FromStatus, To: transition.ToStatus}
		}

		if projectIDs == nil {
			projectIDs = []string{}
		}
		names := workflow.StatusNames()
		statuses := make([]statusResponse, len(names))
		for jj, name := range names {
			statusConf := workflow.Statuses[name]
			roles := make([]string, len(statusConf.Roles))
			for kk, role := range statusConf.Roles {
				roles[kk] = string(role)
			}
			statuses[jj] = statusResponse{Name: name, Roles: roles}
		}
		results[index] = workflowListResponse{
			Name:        workflow.Name,
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

func toPlayerResponse(player *domain.Player) playerResponse {
	return playerResponse{
		ID:           player.ID,
		Type:         player.Type,
		RegisteredAt: player.RegisteredAt.Format(time.RFC3339),
		LastSeenAt:   player.LastSeenAt.Format(time.RFC3339),
	}
}

// handlePlayerRegister handles the tusk_player_register tool.
func (server *Server) handlePlayerRegister(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := server.checkBlocked("tusk_player_register", request); result != nil {
		return result, nil
	}
	playerID, playerIDErr := request.RequireString("player_id")

	if playerIDErr != nil {
		return mcp.NewToolResultError("player_id is required"), nil
	}

	ctx = service.WithActor(ctx, playerID)

	player, registerErr := server.playerSvc.Register(ctx, playerID, "agent")

	if registerErr != nil {
		return toolError(registerErr, "player "+playerID), nil
	}

	return toolResultJSON(toPlayerResponse(player))
}

// handleTaskClaim handles the tusk_task_claim tool.
func (server *Server) handleTaskClaim(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := server.checkBlocked("tusk_task_claim", request); result != nil {
		return result, nil
	}
	shortID, shortIDErr := request.RequireString("short_id")

	if shortIDErr != nil {
		return mcp.NewToolResultError("short_id is required"), nil
	}

	playerID, playerIDErr := request.RequireString("player_id")

	if playerIDErr != nil {
		return mcp.NewToolResultError("player_id is required"), nil
	}

	ctx = service.WithActor(ctx, playerID)
	version, versionErr := request.RequireFloat("version")

	if versionErr != nil {
		return mcp.NewToolResultError("version is required"), nil
	}

	updated, claimErr := server.taskSvc.Claim(ctx, shortID, playerID, int(version))

	if claimErr != nil {
		return toolError(claimErr, "task "+shortID), nil
	}

	tags, tagsErr := server.tagSvc.GetTaskTags(ctx, updated.ID)

	if tagsErr != nil {
		return nil, tagsErr
	}

	return toolResultJSON(toTaskResponse(updated, tags, server.projectNames(ctx)))
}

// handleTaskRelease handles the tusk_task_release tool.
func (server *Server) handleTaskRelease(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := server.checkBlocked("tusk_task_release", request); result != nil {
		return result, nil
	}
	shortID, shortIDErr := request.RequireString("short_id")

	if shortIDErr != nil {
		return mcp.NewToolResultError("short_id is required"), nil
	}

	playerID, playerIDErr := request.RequireString("player_id")

	if playerIDErr != nil {
		return mcp.NewToolResultError("player_id is required"), nil
	}

	ctx = service.WithActor(ctx, playerID)
	version, versionErr := request.RequireFloat("version")

	if versionErr != nil {
		return mcp.NewToolResultError("version is required"), nil
	}

	updated, releaseErr := server.taskSvc.Release(ctx, shortID, playerID, int(version))

	if releaseErr != nil {
		return toolError(releaseErr, "task "+shortID), nil
	}

	tags, tagsErr := server.tagSvc.GetTaskTags(ctx, updated.ID)

	if tagsErr != nil {
		return nil, tagsErr
	}

	return toolResultJSON(toTaskResponse(updated, tags, server.projectNames(ctx)))
}

// handleTaskAvailable handles the tusk_task_available tool.
func (server *Server) handleTaskAvailable(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx = server.updatePlayerLiveness(ctx, request)

	playerID, playerIDErr := request.RequireString("player_id")

	if playerIDErr != nil {
		return mcp.NewToolResultError("player_id is required"), nil
	}

	ctx = service.WithActor(ctx, playerID)

	// Auto-register player as agent
	if server.playerSvc != nil {
		if _, regErr := server.playerSvc.Register(ctx, playerID, "agent"); regErr != nil {
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
			resolver := server.newResolver(ctx)
			var resolveErrs []error
			filterExpr, resolveErrs = resolver.ResolveExpr(ctx, expr)
			if len(resolveErrs) > 0 {
				return mcp.NewToolResultError(resolveErrs[0].Error()), nil
			}
		}
	}

	tasks, tasksErr := server.taskSvc.Available(ctx, filterExpr)

	if tasksErr != nil {
		return nil, tasksErr
	}

	taskIDs := make([]uuid.UUID, len(tasks))
	for index, task := range tasks {
		taskIDs[index] = task.ID
	}
	tagsByTask, tagsErr := server.tagSvc.GetTaskTagsBatch(ctx, taskIDs)

	if tagsErr != nil {
		return nil, tagsErr
	}

	names := server.projectNames(ctx)
	results := make([]taskResponse, len(tasks))
	for index, task := range tasks {
		results[index] = toTaskResponse(task, tagsByTask[task.ID], names)
	}

	return toolResultJSON(results)
}

// handleTaskPop handles the tusk_task_pop tool.
func (server *Server) handleTaskPop(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := server.checkBlocked("tusk_task_pop", request); result != nil {
		return result, nil
	}
	playerID, playerIDErr := request.RequireString("player_id")

	if playerIDErr != nil {
		return mcp.NewToolResultError("player_id is required"), nil
	}

	ctx = service.WithActor(ctx, playerID)

	// Auto-register player as agent
	if server.playerSvc != nil {
		if _, regErr := server.playerSvc.Register(ctx, playerID, "agent"); regErr != nil {
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
			resolver := server.newResolver(ctx)
			var resolveErrs []error
			filterExpr, resolveErrs = resolver.ResolveExpr(ctx, expr)
			if len(resolveErrs) > 0 {
				return mcp.NewToolResultError(resolveErrs[0].Error()), nil
			}
		}
	}

	task, popErr := server.taskSvc.Pop(ctx, playerID, filterExpr)

	if popErr != nil {
		if errors.Is(popErr, domain.ErrNoAvailableTasks) {
			return mcp.NewToolResultText("No available tasks matching the given filters"), nil
		}
		return toolError(popErr, "pop"), nil
	}

	tags, tagsErr := server.tagSvc.GetTaskTags(ctx, task.ID)

	if tagsErr != nil {
		return nil, tagsErr
	}

	return toolResultJSON(toTaskResponse(task, tags, server.projectNames(ctx)))
}

// handleTaskTree handles the tusk_task_tree tool.
func (server *Server) handleTaskTree(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx = server.updatePlayerLiveness(ctx, request)
	var tasks []*domain.Task
	var rootID *uuid.UUID

	if shortID, err := request.RequireString("short_id"); err == nil {
		// Subtree mode
		root, lookupErr := server.taskSvc.GetByShortID(ctx, shortID)

		if lookupErr != nil {
			return toolError(lookupErr, "task "+shortID), nil
		}

		descendants, descendantsErr := server.taskSvc.GetDescendants(ctx, root.ID)

		if descendantsErr != nil {
			return nil, descendantsErr
		}

		tasks = append([]*domain.Task{root}, descendants...)
		rootID = &root.ID
	} else {
		// Full tree mode
		taskFilter := domain.TaskFilter{
			Statuses: []string{"pending", "active", "completed"},
		}
		// Check include_deleted flag
		if request.GetBool("include_deleted", false) {
			taskFilter = domain.TaskFilter{}
		}
		var listErr error
		tasks, listErr = server.taskSvc.List(ctx, &domain.TermFilter{TaskFilter: taskFilter})

		if listErr != nil {
			return nil, listErr
		}
	}

	tree := buildTreeResponse(tasks, rootID, server.projectNames(ctx))
	return toolResultJSON(tree)
}
