package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/google/uuid"
)

// DefaultProjectName is the config-level name of the built-in default project.
// Via ProjectRepository.GetByName it resolves to domain.DefaultProjectUUID.
// The SQLite projects table stores the row as "_default"; the config file
// ships it as "default". Both map to uuid.Nil.
const DefaultProjectName = "default"

// TaskService implements task business logic including validation,
// workflow enforcement, and optimistic locking. It routes per-task
// operations to the SQLite store that owns the task's project via a
// BundleResolver.
type TaskService struct {
	resolve     BundleResolver
	projects    ProjectLister
	projectRepo repository.ProjectRepository
	projectSvc  *ProjectService
	workflowSvc *WorkflowService
	engine      *UrgencyEngine
}

// NewTaskService creates a new TaskService wired to the given resolver,
// project lister, project repo, project service, workflow service, and
// optional urgency engine. projectSvc is installed here so Phase 3 can
// consult ProjectService.EffectiveTaxonomy for level validation; it may
// be nil in tests that do not exercise the taxonomy path.
func NewTaskService(
	resolve BundleResolver,
	projects ProjectLister,
	projectRepo repository.ProjectRepository,
	projectSvc *ProjectService,
	workflowSvc *WorkflowService,
	engine *UrgencyEngine,
) *TaskService {
	return &TaskService{
		resolve:     resolve,
		projects:    projects,
		projectRepo: projectRepo,
		projectSvc:  projectSvc,
		workflowSvc: workflowSvc,
		engine:      engine,
	}
}

// defaultProjectID returns the stored UUID of the built-in default project.
// Used by entry points that need the default project but did not receive
// a specific project from the caller. The SQLite seed row and the config
// helper both write it under uuid.Nil, so we look it up by ID to avoid the
// name drift between "default" (config) and "_default" (legacy SQL seed).
func (s *TaskService) defaultProjectID(ctx context.Context) (uuid.UUID, error) {
	p, err := s.projectRepo.GetByID(ctx, domain.DefaultProjectUUID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("looking up default project: %w", err)
	}
	return p.ID, nil
}

// workflowName resolves the project's workflow UUID to its name via the
// workflow service. Centralizes the lookup so callers that previously read
// project.Workflow (a compat string field removed in Phase 4) share one path.
func (s *TaskService) workflowName(ctx context.Context, project *domain.Project) (string, error) {
	wf, err := s.workflowSvc.GetByID(ctx, project.WorkflowID)
	if err != nil {
		return "", fmt.Errorf("looking up workflow %v: %w", project.WorkflowID, err)
	}
	return wf.Name, nil
}

// ResolveProjectName looks up a project by name and returns its UUID.
// CLI and MCP callers use this to translate user-entered project names
// into the typed Task.ProjectID value before calling Create.
// Returns domain.ErrNotFound if the project does not exist.
func (s *TaskService) ResolveProjectName(ctx context.Context, name string) (uuid.UUID, error) {
	if name == "" {
		return s.defaultProjectID(ctx)
	}
	p, err := s.projectRepo.GetByName(ctx, name)
	if err != nil {
		return uuid.Nil, err
	}
	return p.ID, nil
}

// defaultBundle resolves the bundle backing the default project. Player
// records and tag definitions are global resources kept in the default
// store, so operations involving them go through this bundle regardless
// of which project a task lives in.
func (s *TaskService) defaultBundle(ctx context.Context) (*RepoBundle, error) {
	defID, err := s.defaultProjectID(ctx)
	if err != nil {
		return nil, err
	}
	return s.resolve(ctx, defID)
}

func (s *TaskService) bundleForShortID(ctx context.Context, shortID string) (*RepoBundle, *domain.Task, error) {
	defID, err := s.defaultProjectID(ctx)
	if err != nil {
		return nil, nil, err
	}
	bundle, err := s.resolve(ctx, defID)
	if err != nil {
		return nil, nil, err
	}
	task, err := bundle.Tasks.GetByShortID(ctx, shortID)
	if err != nil {
		return nil, nil, err
	}
	return bundle, task, nil
}

func (s *TaskService) bundleForID(ctx context.Context, id uuid.UUID) (*RepoBundle, *domain.Task, error) {
	defID, err := s.defaultProjectID(ctx)
	if err != nil {
		return nil, nil, err
	}
	bundle, err := s.resolve(ctx, defID)
	if err != nil {
		return nil, nil, err
	}
	task, err := bundle.Tasks.GetByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	return bundle, task, nil
}

// Create validates and persists a new task. It populates the task's ID,
// ShortID, Version, timestamps, and default ProjectID before saving.
func (s *TaskService) Create(ctx context.Context, task *domain.Task) error {
	if strings.TrimSpace(task.Title) == "" {
		return fmt.Errorf("title must not be empty")
	}
	if task.Priority < 0 || task.Priority > 4 {
		return fmt.Errorf("priority must be between 0 and 4")
	}

	if task.ProjectID == uuid.Nil {
		defID, err := s.defaultProjectID(ctx)
		if err != nil {
			return err
		}
		task.ProjectID = defID
	}

	project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("project not found: %w", err)
		}
		return fmt.Errorf("looking up project: %w", err)
	}

	wfName, err := s.workflowName(ctx, project)
	if err != nil {
		return err
	}

	bundle, err := s.resolve(ctx, task.ProjectID)
	if err != nil {
		return fmt.Errorf("resolving project store: %w", err)
	}

	if task.ParentID != nil {
		_, err := bundle.Tasks.GetByID(ctx, *task.ParentID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return fmt.Errorf("parent task not found: %w", err)
			}
			return fmt.Errorf("looking up parent task: %w", err)
		}
	}

	if err := s.validateTaxonomy(ctx, bundle, task); err != nil {
		return err
	}

	if task.Status == "" {
		initialStatus, err := s.workflowSvc.GetStatusByRole(ctx, wfName, domain.RoleInitial)
		if err != nil {
			return fmt.Errorf("resolving initial status: %w", err)
		}
		task.Status = initialStatus
	}
	statuses, err := s.workflowSvc.GetStatuses(ctx, wfName)
	if err != nil {
		return fmt.Errorf("loading workflow statuses: %w", err)
	}
	validStatus := false
	for _, st := range statuses {
		if st == task.Status {
			validStatus = true
			break
		}
	}
	if !validStatus {
		return fmt.Errorf("status %q is not valid for workflow %q", task.Status, wfName)
	}

	task.ID = uuid.New()
	shortID, err := s.generateShortID(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("generating short ID: %w", err)
	}
	task.ShortID = shortID

	if task.Order == nil {
		next, err := bundle.Tasks.NextOrder(ctx, task.ParentID)
		if err != nil {
			return fmt.Errorf("computing default order: %w", err)
		}
		task.Order = &next
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	task.Version = 1
	task.CreatedAt = now
	task.ModifiedAt = now
	if task.UDA != nil {
		if err := domain.ValidateUDA(task.UDA); err != nil {
			return err
		}
	} else {
		task.UDA = map[string]any{}
	}

	actor := ActorFromContext(ctx)
	return bundle.WriteTx.WithTx(ctx, func(tx WriteTx) error {
		if err := tx.Tasks().Create(ctx, task); err != nil {
			return err
		}
		return tx.Events().Record(ctx, domain.NewTaskCreatedEvent(task, actor))
	})
}

// GetByShortID retrieves a task by its short ID, searching every known
// project store.
func (s *TaskService) GetByShortID(ctx context.Context, shortID string) (*domain.Task, error) {
	_, task, err := s.bundleForShortID(ctx, shortID)
	if err != nil {
		return task, err
	}
	if err := s.stampEffectiveWeights(ctx, task); err != nil {
		return task, err
	}
	return task, nil
}

// GetByID retrieves a task by its full UUID, searching every known
// project store.
func (s *TaskService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	_, task, err := s.bundleForID(ctx, id)
	if err != nil {
		return task, err
	}
	if err := s.stampEffectiveWeights(ctx, task); err != nil {
		return task, err
	}
	return task, nil
}

// stampEffectiveWeights populates task.EffectiveWeights if the task's chain
// contributes any non-default value. Safe to call on any loaded task; a nil
// engine leaves the field unchanged.
func (s *TaskService) stampEffectiveWeights(ctx context.Context, task *domain.Task) error {
	if s.engine == nil || task == nil {
		return nil
	}
	w, has, err := s.ResolveEffectiveWeights(ctx, task.ID)
	if err != nil {
		return err
	}
	if has {
		rw := w.Resolved()
		task.EffectiveWeights = &rw
	}
	return nil
}

// List returns tasks matching the given filter, scored and sorted by
// urgency.
func (s *TaskService) List(ctx context.Context, filter domain.FilterExpr) ([]*domain.Task, error) {
	defID, err := s.defaultProjectID(ctx)
	if err != nil {
		return nil, err
	}
	bundle, err := s.resolve(ctx, defID)
	if err != nil {
		return nil, err
	}
	return s.listInBundle(ctx, bundle, filter)
}

// listInBundle runs the filter against a single bundle and scores the
// resulting tasks using that bundle's own relation, annotation, and tag
// repos.
func (s *TaskService) listInBundle(ctx context.Context, bundle *RepoBundle, filter domain.FilterExpr) ([]*domain.Task, error) {
	tasks, err := bundle.Tasks.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 || s.engine == nil {
		return tasks, nil
	}

	taskIDs := make([]uuid.UUID, len(tasks))
	for i, t := range tasks {
		taskIDs[i] = t.ID
	}

	blockingCounts, err := bundle.Relations.CountBlockingByTasks(ctx, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("loading blocking counts: %w", err)
	}
	blockedByCounts, err := bundle.Relations.CountBlockedByTasks(ctx, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("loading blocked-by counts: %w", err)
	}
	annotationCounts, err := bundle.Annotations.CountByTasks(ctx, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("loading annotation counts: %w", err)
	}
	tagsByTask, err := bundle.Tags.GetTaskTagsBatch(ctx, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("loading tag counts: %w", err)
	}
	tagCounts := make(map[uuid.UUID]int, len(tagsByTask))
	for id, tags := range tagsByTask {
		tagCounts[id] = len(tags)
	}

	projectWeights := s.buildProjectWeights(ctx, tasks)
	effective, err := s.buildEffectiveWeights(ctx, bundle, tasks, projectWeights)
	if err != nil {
		return nil, err
	}

	sctx := ScoringContext{
		BlockingCount:    blockingCounts,
		BlockedByCount:   blockedByCounts,
		AnnotationCount:  annotationCounts,
		TagCount:         tagCounts,
		ProjectWeights:   projectWeights,
		EffectiveWeights: effective,
	}
	s.engine.ScoreAndSort(tasks, sctx)
	for _, t := range tasks {
		if w, ok := effective[t.ID]; ok {
			rw := w.Resolved()
			t.EffectiveWeights = &rw
		}
	}
	return tasks, nil
}

// Next returns the highest-urgency actionable task. Actionable means:
// non-terminal status, not waiting, not blocked. Returns
// domain.ErrNotFound if no actionable task exists.
func (s *TaskService) Next(ctx context.Context) (*domain.Task, error) {
	nonTerminal, err := s.collectNonTerminalStatuses(ctx)
	if err != nil {
		return nil, err
	}
	defID, err := s.defaultProjectID(ctx)
	if err != nil {
		return nil, err
	}
	bundle, err := s.resolve(ctx, defID)
	if err != nil {
		return nil, err
	}
	filter := &domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: nonTerminal}}
	tasks, err := s.listInBundle(ctx, bundle, filter)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	for _, t := range tasks {
		if t.WaitUntil != nil && t.WaitUntil.After(now) {
			continue
		}
		blockedBy, err := bundle.Relations.CountBlockedByTasks(ctx, []uuid.UUID{t.ID})
		if err != nil {
			return nil, fmt.Errorf("checking blocked status: %w", err)
		}
		if blockedBy[t.ID] > 0 {
			continue
		}
		return t, nil
	}
	return nil, domain.ErrNotFound
}

// collectNonTerminalStatuses returns the union of non-terminal status
// names across all configured workflows. Used by Available and Next to
// find actionable tasks regardless of which workflow defines the status.
func (s *TaskService) collectNonTerminalStatuses(ctx context.Context) ([]string, error) {
	workflows, err := s.workflowSvc.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing workflows: %w", err)
	}
	seen := make(map[string]bool)
	var result []string
	for _, wf := range workflows {
		for _, name := range wf.NonTerminalStatuses() {
			if !seen[name] {
				seen[name] = true
				result = append(result, name)
			}
		}
	}
	return result, nil
}

// buildProjectWeights constructs per-project merged urgency weights for
// all distinct projects found in the task list.
func (s *TaskService) buildProjectWeights(ctx context.Context, tasks []*domain.Task) map[uuid.UUID]*UrgencyWeights {
	if s.engine == nil {
		return nil
	}
	seen := make(map[uuid.UUID]bool)
	for _, t := range tasks {
		seen[t.ProjectID] = true
	}
	weights := make(map[uuid.UUID]*UrgencyWeights, len(seen))
	for projectID := range seen {
		project, err := s.projectRepo.GetByID(ctx, projectID)
		if err != nil {
			continue
		}
		if project.Settings.Urgency == nil {
			continue
		}
		merged := MergeWeights(s.engine.defaults, project.Settings.Urgency)
		weights[projectID] = &merged
	}
	return weights
}

// buildEffectiveWeights resolves the project + ancestor + self override chain
// for each task, returning a map keyed by task ID. The result holds an entry
// only for tasks whose chain contributes at least one non-default value;
// callers fall through to ProjectWeights / engine defaults via weightsFor for
// tasks that are absent from the result.
//
// Fast path: if every task has no overrides and no parent, returns (nil, nil)
// without round-tripping the ancestor CTE.
func (s *TaskService) buildEffectiveWeights(
	ctx context.Context,
	bundle *RepoBundle,
	tasks []*domain.Task,
	projectWeights map[uuid.UUID]*UrgencyWeights,
) (map[uuid.UUID]*UrgencyWeights, error) {
	if s.engine == nil || len(tasks) == 0 {
		return nil, nil
	}

	needsWalk := false
	for _, t := range tasks {
		if t.UrgencyOverrides != nil || t.ParentID != nil {
			needsWalk = true
			break
		}
	}
	if !needsWalk {
		return nil, nil
	}

	taskIDs := make([]uuid.UUID, len(tasks))
	for i, t := range tasks {
		taskIDs[i] = t.ID
	}
	rows, err := bundle.Tasks.GetAncestorOverrides(ctx, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("loading ancestor overrides: %w", err)
	}

	parentByID := make(map[uuid.UUID]*uuid.UUID, len(rows))
	overridesByID := make(map[uuid.UUID]*domain.UrgencyOverrides, len(rows))
	projectByID := make(map[uuid.UUID]uuid.UUID, len(rows))
	for _, row := range rows {
		parentByID[row.TaskID] = row.ParentID
		overridesByID[row.TaskID] = row.Overrides
		projectByID[row.TaskID] = row.ProjectID
	}

	out := make(map[uuid.UUID]*UrgencyWeights, len(tasks))
	for _, t := range tasks {
		var chain []uuid.UUID
		current := t.ID
		for depth := 0; depth < maxParentDepth; depth++ {
			chain = append(chain, current)
			parent, ok := parentByID[current]
			if !ok || parent == nil {
				break
			}
			current = *parent
		}
		// Reverse to root → self.
		for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
			chain[i], chain[j] = chain[j], chain[i]
		}

		var merged UrgencyWeights
		if pw, ok := projectWeights[t.ProjectID]; ok {
			merged = *pw
		} else {
			merged = s.engine.Defaults()
		}

		contributed := false
		for _, id := range chain {
			if ov := overridesByID[id]; ov != nil {
				merged = MergeWeights(merged, ov)
				contributed = true
			}
		}
		if contributed {
			cp := merged
			out[t.ID] = &cp
		}
	}

	return out, nil
}

// resolveEffectiveWeightsFromTask resolves the urgency-weight chain using the
// provided in-memory task for self state, so callers that have just mutated
// task.UrgencyOverrides get the consistent answer. Ancestors are read fresh
// from the repo via GetAncestorOverrides.
//
// Unlike TaskService.ResolveEffectiveWeights, this does not re-read the task
// from the DB — callers inside a write transaction need the post-patch
// in-memory state as the self contribution.
func (s *TaskService) resolveEffectiveWeightsFromTask(
	ctx context.Context,
	bundle *RepoBundle,
	task *domain.Task,
) (UrgencyWeights, error) {
	if s.engine == nil {
		return UrgencyWeights{}, fmt.Errorf("urgency engine not configured")
	}

	projectWeights := s.buildProjectWeights(ctx, []*domain.Task{task})
	var merged UrgencyWeights
	if pw, ok := projectWeights[task.ProjectID]; ok {
		merged = *pw
	} else {
		merged = s.engine.Defaults()
	}

	parentByID := make(map[uuid.UUID]*uuid.UUID)
	overridesByID := make(map[uuid.UUID]*domain.UrgencyOverrides)
	if task.ParentID != nil {
		rows, err := bundle.Tasks.GetAncestorOverrides(ctx, []uuid.UUID{task.ID})
		if err != nil {
			return UrgencyWeights{}, fmt.Errorf("loading ancestor overrides: %w", err)
		}
		for _, row := range rows {
			parentByID[row.TaskID] = row.ParentID
			overridesByID[row.TaskID] = row.Overrides
		}
	}
	// The caller owns the authoritative self state — substitute in the
	// in-memory parent + overrides regardless of what the repo returned.
	parentByID[task.ID] = task.ParentID
	overridesByID[task.ID] = task.UrgencyOverrides

	var chain []uuid.UUID
	current := task.ID
	for depth := 0; depth < maxParentDepth; depth++ {
		chain = append(chain, current)
		parent, ok := parentByID[current]
		if !ok || parent == nil {
			break
		}
		current = *parent
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}

	for _, id := range chain {
		if ov := overridesByID[id]; ov != nil {
			merged = MergeWeights(merged, ov)
		}
	}
	return merged, nil
}

// ResolveEffectiveWeights returns the fully-resolved urgency weights for a
// single task, walking the project + ancestor + self override chain. The
// second return is true when any node in the chain contributed a non-default
// value (drives Phase 4's render-or-omit decision for effective_urgency_weights).
func (s *TaskService) ResolveEffectiveWeights(ctx context.Context, taskID uuid.UUID) (UrgencyWeights, bool, error) {
	if s.engine == nil {
		return UrgencyWeights{}, false, fmt.Errorf("urgency engine not configured")
	}
	bundle, task, err := s.bundleForID(ctx, taskID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return UrgencyWeights{}, false, err
		}
		return UrgencyWeights{}, false, fmt.Errorf("resolving bundle: %w", err)
	}

	projectWeights := s.buildProjectWeights(ctx, []*domain.Task{task})
	effective, err := s.buildEffectiveWeights(ctx, bundle, []*domain.Task{task}, projectWeights)
	if err != nil {
		return UrgencyWeights{}, false, err
	}
	if w, ok := effective[task.ID]; ok {
		return *w, true, nil
	}
	if pw, ok := projectWeights[task.ProjectID]; ok {
		return *pw, false, nil
	}
	return s.engine.Defaults(), false, nil
}

// GetChildren returns the direct children of a task. The parent task
// must exist in some known project store.
func (s *TaskService) GetChildren(ctx context.Context, parentID uuid.UUID) ([]*domain.Task, error) {
	bundle, _, err := s.bundleForID(ctx, parentID)
	if err != nil {
		return nil, err
	}
	return bundle.Tasks.GetChildren(ctx, parentID)
}

// GetDescendants returns all descendants of a task (recursive).
func (s *TaskService) GetDescendants(ctx context.Context, rootID uuid.UUID) ([]*domain.Task, error) {
	bundle, _, err := s.bundleForID(ctx, rootID)
	if err != nil {
		return nil, err
	}
	return bundle.Tasks.GetDescendants(ctx, rootID)
}

// SummarizeSubtree rolls up a single task's strict descendants. The
// root task itself is NOT counted — its own status answers a different
// question. All non-delete-role descendants count toward Total; those
// whose status carries the `done` role count toward Done.
//
// Returns domain.ErrNotFound if rootID does not exist.
func (s *TaskService) SummarizeSubtree(ctx context.Context, rootID uuid.UUID) (*domain.SummaryBlock, error) {
	bundle, root, err := s.bundleForID(ctx, rootID)
	if err != nil {
		return nil, err
	}
	descendants, err := bundle.Tasks.GetDescendants(ctx, rootID)
	if err != nil {
		return nil, fmt.Errorf("loading descendants: %w", err)
	}
	wfFor, err := s.workflowsByProject(ctx, descendants, root)
	if err != nil {
		return nil, err
	}
	rollup := domain.AggregateRollup(descendants, wfFor)
	return &domain.SummaryBlock{Task: root, Rollup: rollup}, nil
}

// SummarizeBlocks selects block tasks via blockFilter and rolls up
// each one's strict descendants. blockFilter == nil means "blocks are
// root tasks (parent_id IS NULL)". When full == false, blockFilter
// ALSO restricts which descendants are counted under each block; when
// full == true, blockFilter only selects blocks and the full subtree
// under each block is counted. Blocks are returned in urgency-desc
// order with short_id ascending as tiebreaker.
func (s *TaskService) SummarizeBlocks(
	ctx context.Context,
	blockFilter domain.FilterExpr,
	full bool,
) ([]*domain.SummaryBlock, error) {
	defID, err := s.defaultProjectID(ctx)
	if err != nil {
		return nil, err
	}
	bundle, err := s.resolve(ctx, defID)
	if err != nil {
		return nil, err
	}

	var blocks []*domain.Task
	if blockFilter == nil {
		all, err := s.listInBundle(ctx, bundle, nil)
		if err != nil {
			return nil, fmt.Errorf("listing root tasks: %w", err)
		}
		for _, t := range all {
			if t.ParentID == nil {
				blocks = append(blocks, t)
			}
		}
	} else {
		blocks, err = s.listInBundle(ctx, bundle, blockFilter)
		if err != nil {
			return nil, fmt.Errorf("listing blocks: %w", err)
		}
	}

	if len(blocks) == 0 {
		return []*domain.SummaryBlock{}, nil
	}

	out := make([]*domain.SummaryBlock, 0, len(blocks))
	for _, block := range blocks {
		descendants, err := bundle.Tasks.GetDescendants(ctx, block.ID)
		if err != nil {
			return nil, fmt.Errorf("loading descendants for block %s: %w", block.ShortID, err)
		}
		if !full && blockFilter != nil {
			filtered := descendants[:0]
			for _, d := range descendants {
				if domain.EvalFilter(blockFilter, d) {
					filtered = append(filtered, d)
				}
			}
			descendants = filtered
		}
		wfFor, err := s.workflowsByProject(ctx, descendants, block)
		if err != nil {
			return nil, err
		}
		rollup := domain.AggregateRollup(descendants, wfFor)
		out = append(out, &domain.SummaryBlock{Task: block, Rollup: rollup})
	}

	sort.SliceStable(out, func(i, j int) bool {
		ui, uj := out[i].Task.Urgency, out[j].Task.Urgency
		if ui != uj {
			return ui > uj
		}
		return out[i].Task.ShortID < out[j].Task.ShortID
	})
	return out, nil
}

// workflowsByProject preloads each distinct project's workflow once and
// returns a closure suitable as the workflowFor callback to
// domain.AggregateRollup. The seedTask is included in the project set so
// the seed-workflow lookup remains stable when the descendant slice is
// empty.
func (s *TaskService) workflowsByProject(
	ctx context.Context,
	descendants []*domain.Task,
	seed *domain.Task,
) (func(*domain.Task) *domain.Workflow, error) {
	projectIDs := make(map[uuid.UUID]struct{})
	if seed != nil {
		projectIDs[seed.ProjectID] = struct{}{}
	}
	for _, t := range descendants {
		if t == nil {
			continue
		}
		projectIDs[t.ProjectID] = struct{}{}
	}
	cache := make(map[uuid.UUID]*domain.Workflow, len(projectIDs))
	for projectID := range projectIDs {
		project, err := s.projectRepo.GetByID(ctx, projectID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				cache[projectID] = nil
				continue
			}
			return nil, fmt.Errorf("looking up project %v: %w", projectID, err)
		}
		wf, err := s.workflowSvc.GetByID(ctx, project.WorkflowID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				cache[projectID] = nil
				continue
			}
			return nil, fmt.Errorf("looking up workflow %v: %w", project.WorkflowID, err)
		}
		cache[projectID] = wf
	}
	return func(t *domain.Task) *domain.Workflow {
		if t == nil {
			return nil
		}
		return cache[t.ProjectID]
	}, nil
}

// Update applies a partial update to a task. It validates the patched
// state, enforces workflow transitions, and uses optimistic locking.
func (s *TaskService) Update(ctx context.Context, upd domain.TaskUpdate) (*domain.Task, error) {
	bundle, task, err := s.bundleForShortID(ctx, upd.ShortID)
	if err != nil {
		return nil, err
	}

	if task.Version != upd.Version {
		return nil, domain.ErrConflict
	}

	if upd.UrgencyOverrides != nil && (upd.UrgencyMergePatch != nil || len(upd.UrgencyDelta) > 0) {
		return nil, fmt.Errorf("urgency_overrides full-replace is mutually exclusive with merge patch and delta; got both in one update")
	}

	oldStatus := task.Status
	snapshot := snapshotTask(task)

	if upd.Title != nil {
		task.Title = *upd.Title
	}
	if upd.Description != nil {
		if *upd.Description == nil {
			task.Description = ""
		} else {
			task.Description = **upd.Description
		}
	}
	if upd.Level != nil {
		if *upd.Level == nil {
			task.Level = nil
		} else {
			val := **upd.Level
			task.Level = &val
		}
	}
	if upd.Order != nil {
		if *upd.Order == nil {
			task.Order = nil
		} else {
			val := **upd.Order
			task.Order = &val
		}
	}
	if upd.Status != nil {
		task.Status = *upd.Status
	}
	if upd.Priority != nil {
		task.Priority = *upd.Priority
	}
	if upd.ParentID != nil {
		task.ParentID = *upd.ParentID
	}
	if upd.ProjectID != nil {
		task.ProjectID = *upd.ProjectID
	}
	if upd.DueAt != nil {
		task.DueAt = *upd.DueAt
	}
	if upd.WaitUntil != nil {
		task.WaitUntil = *upd.WaitUntil
	}
	if upd.RecurrenceRule != nil {
		task.RecurrenceRule = *upd.RecurrenceRule
	}
	if upd.ClaimedBy != nil {
		task.ClaimedBy = *upd.ClaimedBy
	}
	if upd.ClaimedAt != nil {
		task.ClaimedAt = *upd.ClaimedAt
	}
	if upd.UDA != nil {
		if err := domain.ValidateUDA(*upd.UDA); err != nil {
			return nil, err
		}
		if task.UDA == nil {
			task.UDA = map[string]any{}
		}
		for k, v := range *upd.UDA {
			if v == "" {
				delete(task.UDA, k)
			} else {
				task.UDA[k] = v
			}
		}
	}

	if err := s.applyUrgencyOverridesUpdate(ctx, bundle, task, upd); err != nil {
		return nil, err
	}

	if strings.TrimSpace(task.Title) == "" {
		return nil, fmt.Errorf("title must not be empty")
	}
	if task.Priority < 0 || task.Priority > 4 {
		return nil, fmt.Errorf("priority must be between 0 and 4")
	}

	if upd.ParentID != nil && task.ParentID != nil {
		if *task.ParentID == task.ID {
			return nil, fmt.Errorf("task cannot be its own parent")
		}
		_, err := bundle.Tasks.GetByID(ctx, *task.ParentID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return nil, fmt.Errorf("parent task not found: %w", err)
			}
			return nil, fmt.Errorf("looking up parent task: %w", err)
		}
		if err := s.detectParentCycle(ctx, bundle.Tasks, task.ID, *task.ParentID); err != nil {
			return nil, err
		}
	}

	if upd.ProjectID != nil {
		_, err := s.projectRepo.GetByID(ctx, task.ProjectID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return nil, fmt.Errorf("project not found: %w", err)
			}
			return nil, fmt.Errorf("looking up project: %w", err)
		}
	}

	if upd.Level != nil || upd.ParentID != nil || upd.ProjectID != nil {
		if err := s.validateTaxonomy(ctx, bundle, task); err != nil {
			return nil, err
		}
	}

	statusChanged := task.Status != oldStatus
	var wfName string
	if statusChanged {
		project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("looking up project for workflow: %w", err)
		}
		wfName, err = s.workflowName(ctx, project)
		if err != nil {
			return nil, err
		}
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, wfName, oldStatus, task.Status)
		if err != nil {
			return nil, fmt.Errorf("checking transition: %w", err)
		}
		if !allowed {
			return nil, fmt.Errorf("transition %q → %q not allowed: %w", oldStatus, task.Status, domain.ErrInvalidTransition)
		}
	}

	task.ModifiedAt = time.Now().UTC().Truncate(time.Millisecond)

	actor := ActorFromContext(ctx)
	var result *domain.Task
	err = bundle.WriteTx.WithTx(ctx, func(tx WriteTx) error {
		updated, err := s.applyValidatedUpdate(ctx, tx, task, oldStatus)
		if err != nil {
			return err
		}
		result = updated

		if statusChanged {
			roles, err := s.workflowSvc.GetStatusRoles(ctx, wfName, updated.Status)
			if err != nil {
				return fmt.Errorf("loading roles for status: %w", err)
			}
			evt := domain.NewStatusChangedEvent(updated, oldStatus, updated.Status, roles, "user", actor)
			if err := tx.Events().Record(ctx, evt); err != nil {
				return fmt.Errorf("recording status_changed event: %w", err)
			}
		}
		if changes := diffTaskFields(snapshot, updated); len(changes) > 0 {
			evt := domain.NewTaskModifiedEvent(updated, changes, actor)
			if err := tx.Events().Record(ctx, evt); err != nil {
				return fmt.Errorf("recording task_modified event: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Start transitions a task from its current status to its workflow's
// start-role status. If playerID is non-empty, it auto-claims the task
// for the player. Emits exactly one task_started event.
func (s *TaskService) Start(ctx context.Context, shortID string, version int, playerID string) (*domain.Task, error) {
	bundle, task, err := s.bundleForShortID(ctx, shortID)
	if err != nil {
		return nil, err
	}
	if task.Version != version {
		return nil, domain.ErrConflict
	}

	project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("loading project %v: %w", task.ProjectID, err)
	}
	wfName, err := s.workflowName(ctx, project)
	if err != nil {
		return nil, err
	}
	startStatus, err := s.workflowSvc.GetStatusByRole(ctx, wfName, domain.RoleStart)
	if err != nil {
		return nil, fmt.Errorf("resolving start status: %w", err)
	}

	oldStatus := task.Status
	if task.Status != startStatus {
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, wfName, task.Status, startStatus)
		if err != nil {
			return nil, fmt.Errorf("checking transition: %w", err)
		}
		if !allowed {
			return nil, fmt.Errorf("transition %q → %q not allowed: %w", task.Status, startStatus, domain.ErrInvalidTransition)
		}
	}

	var players repository.PlayerRepository
	autoClaimed := false

	if playerID != "" {
		players, err = s.playerRepo(ctx)
		if err != nil {
			return nil, err
		}
		if players != nil {
			if _, err := players.GetByID(ctx, playerID); err != nil {
				return nil, fmt.Errorf("player %q: %w", playerID, err)
			}
			if task.ClaimedBy != nil && *task.ClaimedBy != playerID {
				return nil, domain.ErrTaskClaimed
			}
			if task.ClaimedBy == nil {
				now := time.Now().UTC().Truncate(time.Millisecond)
				claimedBy := playerID
				task.ClaimedBy = &claimedBy
				task.ClaimedAt = &now
				autoClaimed = true
			}
		}
	}

	_ = bundle
	task.Status = startStatus
	task.ModifiedAt = time.Now().UTC().Truncate(time.Millisecond)

	actor := ActorFromContext(ctx)
	var result *domain.Task
	err = bundle.WriteTx.WithTx(ctx, func(tx WriteTx) error {
		updated, err := s.applyValidatedUpdate(ctx, tx, task, oldStatus)
		if err != nil {
			return err
		}
		result = updated
		return tx.Events().Record(ctx, domain.NewTaskStartedEvent(updated, oldStatus, autoClaimed, actor))
	})
	if err != nil {
		return nil, err
	}

	if players != nil && playerID != "" {
		players.UpdateLastSeen(ctx, playerID) //nolint:errcheck
	}
	return result, nil
}

// playerRepo returns the default bundle's PlayerRepository. Players are
// a global resource stored only in the default store. Returns nil if the
// default bundle has no player repo wired (minimal-dependency usage in
// tests).
func (s *TaskService) playerRepo(ctx context.Context) (repository.PlayerRepository, error) {
	bundle, err := s.defaultBundle(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolving default bundle for players: %w", err)
	}
	return bundle.Players, nil
}

// Claim assigns a task to a player. Returns ErrTaskClaimed if claimed
// by another player. Re-claiming by the same player is idempotent.
func (s *TaskService) Claim(ctx context.Context, shortID, playerID string, version int) (*domain.Task, error) {
	players, err := s.playerRepo(ctx)
	if err != nil {
		return nil, err
	}
	if players == nil {
		return nil, fmt.Errorf("player support not configured")
	}

	if _, err := players.GetByID(ctx, playerID); err != nil {
		return nil, fmt.Errorf("player %q: %w", playerID, err)
	}

	bundle, task, err := s.bundleForShortID(ctx, shortID)
	if err != nil {
		return nil, err
	}
	if task.Version != version {
		return nil, domain.ErrConflict
	}
	if task.ClaimedBy != nil && *task.ClaimedBy != playerID {
		return nil, domain.ErrTaskClaimed
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	claimedBy := playerID
	task.ClaimedBy = &claimedBy
	task.ClaimedAt = &now
	task.ModifiedAt = now

	actor := ActorFromContext(ctx)
	oldStatus := task.Status
	var result *domain.Task
	err = bundle.WriteTx.WithTx(ctx, func(tx WriteTx) error {
		updated, err := s.applyValidatedUpdate(ctx, tx, task, oldStatus)
		if err != nil {
			return err
		}
		result = updated
		return tx.Events().Record(ctx, domain.NewTaskClaimedEvent(updated, playerID, actor))
	})
	if err != nil {
		return nil, err
	}

	players.UpdateLastSeen(ctx, playerID) //nolint:errcheck
	return result, nil
}

// Release clears a task's claim. Only the current claimant can release.
func (s *TaskService) Release(ctx context.Context, shortID, playerID string, version int) (*domain.Task, error) {
	players, err := s.playerRepo(ctx)
	if err != nil {
		return nil, err
	}
	if players == nil {
		return nil, fmt.Errorf("player support not configured")
	}

	bundle, task, err := s.bundleForShortID(ctx, shortID)
	if err != nil {
		return nil, err
	}
	if task.Version != version {
		return nil, domain.ErrConflict
	}
	if task.ClaimedBy == nil {
		return nil, fmt.Errorf("task is not claimed")
	}
	if *task.ClaimedBy != playerID {
		return nil, fmt.Errorf("task is claimed by a different player")
	}

	task.ClaimedBy = nil
	task.ClaimedAt = nil
	task.ModifiedAt = time.Now().UTC().Truncate(time.Millisecond)

	actor := ActorFromContext(ctx)
	oldStatus := task.Status
	var result *domain.Task
	err = bundle.WriteTx.WithTx(ctx, func(tx WriteTx) error {
		updated, err := s.applyValidatedUpdate(ctx, tx, task, oldStatus)
		if err != nil {
			return err
		}
		result = updated
		return tx.Events().Record(ctx, domain.NewTaskReleasedEvent(updated, playerID, actor))
	})
	if err != nil {
		return nil, err
	}

	players.UpdateLastSeen(ctx, playerID) //nolint:errcheck
	return result, nil
}

// Complete transitions a task to its workflow's done-role status.
func (s *TaskService) Complete(ctx context.Context, shortID string, version int) (*domain.Task, error) {
	bundle, task, err := s.bundleForShortID(ctx, shortID)
	if err != nil {
		return nil, err
	}
	if task.Version != version {
		return nil, domain.ErrConflict
	}
	project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("loading project %v: %w", task.ProjectID, err)
	}
	wfName, err := s.workflowName(ctx, project)
	if err != nil {
		return nil, err
	}
	doneStatus, err := s.workflowSvc.GetStatusByRole(ctx, wfName, domain.RoleDone)
	if err != nil {
		return nil, fmt.Errorf("resolving done status: %w", err)
	}
	oldStatus := task.Status
	statusChanged := oldStatus != doneStatus
	if statusChanged {
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, wfName, oldStatus, doneStatus)
		if err != nil {
			return nil, fmt.Errorf("checking transition: %w", err)
		}
		if !allowed {
			return nil, fmt.Errorf("transition %q → %q not allowed: %w", oldStatus, doneStatus, domain.ErrInvalidTransition)
		}
	}

	task.Status = doneStatus
	task.ModifiedAt = time.Now().UTC().Truncate(time.Millisecond)

	actor := ActorFromContext(ctx)
	var result *domain.Task
	err = bundle.WriteTx.WithTx(ctx, func(tx WriteTx) error {
		updated, err := s.applyValidatedUpdate(ctx, tx, task, oldStatus)
		if err != nil {
			return err
		}
		result = updated
		if !statusChanged {
			return nil
		}
		return tx.Events().Record(ctx, domain.NewTaskCompletedEvent(updated, oldStatus, actor))
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Delete soft-deletes a task by transitioning to its workflow's
// delete-role status.
func (s *TaskService) Delete(ctx context.Context, shortID string, version int) (*domain.Task, error) {
	bundle, task, err := s.bundleForShortID(ctx, shortID)
	if err != nil {
		return nil, err
	}
	if task.Version != version {
		return nil, domain.ErrConflict
	}
	project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("loading project %v: %w", task.ProjectID, err)
	}
	wfName, err := s.workflowName(ctx, project)
	if err != nil {
		return nil, err
	}
	deleteStatus, err := s.workflowSvc.GetStatusByRole(ctx, wfName, domain.RoleDelete)
	if err != nil {
		return nil, fmt.Errorf("resolving delete status: %w", err)
	}
	oldStatus := task.Status
	statusChanged := oldStatus != deleteStatus
	if statusChanged {
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, wfName, oldStatus, deleteStatus)
		if err != nil {
			return nil, fmt.Errorf("checking transition: %w", err)
		}
		if !allowed {
			return nil, fmt.Errorf("transition %q → %q not allowed: %w", oldStatus, deleteStatus, domain.ErrInvalidTransition)
		}
	}

	task.Status = deleteStatus
	task.ModifiedAt = time.Now().UTC().Truncate(time.Millisecond)

	actor := ActorFromContext(ctx)
	var result *domain.Task
	err = bundle.WriteTx.WithTx(ctx, func(tx WriteTx) error {
		updated, err := s.applyValidatedUpdate(ctx, tx, task, oldStatus)
		if err != nil {
			return err
		}
		result = updated
		if !statusChanged {
			return nil
		}
		return tx.Events().Record(ctx, domain.NewTaskDeletedEvent(updated, oldStatus, actor))
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Available returns unclaimed, actionable, unblocked tasks sorted by
// urgency.
func (s *TaskService) Available(ctx context.Context, filter domain.FilterExpr) ([]*domain.Task, error) {
	nonTerminal, err := s.collectNonTerminalStatuses(ctx)
	if err != nil {
		return nil, err
	}
	baseFilter := &domain.AndFilter{
		Children: []domain.FilterExpr{
			&domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: nonTerminal}},
			&domain.TermFilter{TaskFilter: domain.TaskFilter{Unclaimed: ptr(true)}},
		},
	}
	if filter != nil {
		baseFilter.Children = append(baseFilter.Children, filter)
	}

	defID, err := s.defaultProjectID(ctx)
	if err != nil {
		return nil, err
	}
	bundle, err := s.resolve(ctx, defID)
	if err != nil {
		return nil, err
	}
	return s.availableInBundle(ctx, bundle, baseFilter)
}

// availableInBundle runs the availability filter against a single
// bundle, removing blocked and waiting tasks.
func (s *TaskService) availableInBundle(ctx context.Context, bundle *RepoBundle, filter domain.FilterExpr) ([]*domain.Task, error) {
	tasks, err := s.listInBundle(ctx, bundle, filter)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return tasks, nil
	}

	taskIDs := make([]uuid.UUID, len(tasks))
	for i, t := range tasks {
		taskIDs[i] = t.ID
	}

	blockedCounts, err := bundle.Relations.CountBlockedByIncompleteTasks(ctx, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("checking blocked status: %w", err)
	}

	now := time.Now()
	result := make([]*domain.Task, 0, len(tasks))
	for _, t := range tasks {
		if blockedCounts[t.ID] > 0 {
			continue
		}
		if t.WaitUntil != nil && t.WaitUntil.After(now) {
			continue
		}
		result = append(result, t)
	}
	return result, nil
}

// Pop claims and starts the highest-urgency available task for the
// given player atomically, emitting exactly one task_popped event.
// Retries on claim-conflict and optimistic-lock errors. Returns
// domain.ErrNoAvailableTasks if nothing can be claimed.
func (s *TaskService) Pop(ctx context.Context, playerID string, filter domain.FilterExpr) (*domain.Task, error) {
	available, err := s.Available(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(available) == 0 {
		return nil, domain.ErrNoAvailableTasks
	}

	players, err := s.playerRepo(ctx)
	if err != nil {
		return nil, err
	}
	if players == nil {
		return nil, fmt.Errorf("player support not configured")
	}
	if _, err := players.GetByID(ctx, playerID); err != nil {
		return nil, fmt.Errorf("player %q: %w", playerID, err)
	}

	actor := ActorFromContext(ctx)
	for _, candidate := range available {
		bundle, task, err := s.bundleForShortID(ctx, candidate.ShortID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				continue
			}
			return nil, err
		}

		project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("loading project: %w", err)
		}
		wfName, err := s.workflowName(ctx, project)
		if err != nil {
			return nil, err
		}
		startStatus, err := s.workflowSvc.GetStatusByRole(ctx, wfName, domain.RoleStart)
		if err != nil {
			return nil, fmt.Errorf("resolving start status: %w", err)
		}

		if task.Status != startStatus {
			allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, wfName, task.Status, startStatus)
			if err != nil {
				return nil, fmt.Errorf("checking transition: %w", err)
			}
			if !allowed {
				continue
			}
		}

		if task.ClaimedBy != nil {
			continue
		}

		now := time.Now().UTC().Truncate(time.Millisecond)
		claimedBy := playerID
		task.ClaimedBy = &claimedBy
		task.ClaimedAt = &now
		oldStatus := task.Status
		task.Status = startStatus
		task.ModifiedAt = now

		var result *domain.Task
		err = bundle.WriteTx.WithTx(ctx, func(tx WriteTx) error {
			updated, err := s.applyValidatedUpdate(ctx, tx, task, oldStatus)
			if err != nil {
				return err
			}
			result = updated
			return tx.Events().Record(ctx, domain.NewTaskPoppedEvent(updated, playerID, oldStatus, actor))
		})
		if err != nil {
			if errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrTaskClaimed) {
				continue
			}
			return nil, err
		}

		players.UpdateLastSeen(ctx, playerID) //nolint:errcheck
		return result, nil
	}

	return nil, domain.ErrNoAvailableTasks
}

// applyValidatedUpdate writes an already-validated task through tx.Tasks(),
// then runs the auto-complete and auto-revert cascades. It does not emit
// events for the primary task — callers emit the action-specific event.
// The cascades themselves emit status_changed events.
//
// task must carry the post-update field values and be version-matched;
// oldStatus is the status before the update so the cascades can decide
// whether to fire.
func (s *TaskService) applyValidatedUpdate(
	ctx context.Context,
	tx WriteTx,
	task *domain.Task,
	oldStatus string,
) (*domain.Task, error) {
	tr := tx.Tasks()
	if err := tr.Update(ctx, task); err != nil {
		return nil, err
	}
	updated, err := tr.GetByID(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	if updated.Status != oldStatus {
		if err := s.checkAutoComplete(ctx, updated, tx); err != nil {
			return nil, err
		}
		if err := s.checkAutoRevert(ctx, updated, oldStatus, tx); err != nil {
			return nil, err
		}
	}
	return updated, nil
}

// taskSnapshot captures the fields that participate in task_modified diffs
// before an Update mutates the working *domain.Task in place.
type taskSnapshot struct {
	Title          string
	Description    string
	Level          *string
	Priority       int
	Order          *float64
	ParentID       *uuid.UUID
	ProjectID      uuid.UUID
	DueAt          *time.Time
	WaitUntil      *time.Time
	RecurrenceRule *string
	ClaimedBy      *string
	ClaimedAt      *time.Time
	UDA            map[string]any
}

// snapshotTask captures the current field values of a task for later diffing
// against the mutated task.
func snapshotTask(t *domain.Task) taskSnapshot {
	uda := make(map[string]any, len(t.UDA))
	for k, v := range t.UDA {
		uda[k] = v
	}
	return taskSnapshot{
		Title:          t.Title,
		Description:    t.Description,
		Level:          t.Level,
		Priority:       t.Priority,
		Order:          t.Order,
		ParentID:       t.ParentID,
		ProjectID:      t.ProjectID,
		DueAt:          t.DueAt,
		WaitUntil:      t.WaitUntil,
		RecurrenceRule: t.RecurrenceRule,
		ClaimedBy:      t.ClaimedBy,
		ClaimedAt:      t.ClaimedAt,
		UDA:            uda,
	}
}

// diffTaskFields returns a map of JSON-field-name to FieldChange for the
// non-status fields that differ between orig and updated. Status is excluded
// intentionally — status changes flow through status_changed events, not
// task_modified.
func diffTaskFields(orig taskSnapshot, updated *domain.Task) map[string]domain.FieldChange {
	changes := make(map[string]domain.FieldChange)
	if orig.Title != updated.Title {
		changes["title"] = domain.FieldChange{From: orig.Title, To: updated.Title}
	}
	if orig.Description != updated.Description {
		changes["description"] = domain.FieldChange{From: orig.Description, To: updated.Description}
	}
	if !stringPtrEqual(orig.Level, updated.Level) {
		changes["level"] = domain.FieldChange{From: stringPtrValue(orig.Level), To: stringPtrValue(updated.Level)}
	}
	if orig.Priority != updated.Priority {
		changes["priority"] = domain.FieldChange{From: orig.Priority, To: updated.Priority}
	}
	if !float64PtrEqual(orig.Order, updated.Order) {
		changes["order"] = domain.FieldChange{From: float64PtrValue(orig.Order), To: float64PtrValue(updated.Order)}
	}
	if !uuidPtrEqual(orig.ParentID, updated.ParentID) {
		changes["parent_id"] = domain.FieldChange{From: uuidPtrValue(orig.ParentID), To: uuidPtrValue(updated.ParentID)}
	}
	if orig.ProjectID != updated.ProjectID {
		changes["project_id"] = domain.FieldChange{From: orig.ProjectID.String(), To: updated.ProjectID.String()}
	}
	if !timePtrEqual(orig.DueAt, updated.DueAt) {
		changes["due_at"] = domain.FieldChange{From: timePtrValue(orig.DueAt), To: timePtrValue(updated.DueAt)}
	}
	if !timePtrEqual(orig.WaitUntil, updated.WaitUntil) {
		changes["wait_until"] = domain.FieldChange{From: timePtrValue(orig.WaitUntil), To: timePtrValue(updated.WaitUntil)}
	}
	if !stringPtrEqual(orig.RecurrenceRule, updated.RecurrenceRule) {
		changes["recurrence_rule"] = domain.FieldChange{From: stringPtrValue(orig.RecurrenceRule), To: stringPtrValue(updated.RecurrenceRule)}
	}
	if !stringPtrEqual(orig.ClaimedBy, updated.ClaimedBy) {
		changes["claimed_by"] = domain.FieldChange{From: stringPtrValue(orig.ClaimedBy), To: stringPtrValue(updated.ClaimedBy)}
	}
	if !timePtrEqual(orig.ClaimedAt, updated.ClaimedAt) {
		changes["claimed_at"] = domain.FieldChange{From: timePtrValue(orig.ClaimedAt), To: timePtrValue(updated.ClaimedAt)}
	}
	if !reflect.DeepEqual(orig.UDA, updated.UDA) {
		changes["uda"] = domain.FieldChange{From: orig.UDA, To: updated.UDA}
	}
	if len(changes) == 0 {
		return nil
	}
	return changes
}

func uuidPtrEqual(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func uuidPtrValue(p *uuid.UUID) any {
	if p == nil {
		return nil
	}
	return p.String()
}

func stringPtrEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func float64PtrEqual(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func stringPtrValue(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func timePtrEqual(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

func timePtrValue(p *time.Time) any {
	if p == nil {
		return nil
	}
	return *p
}

// validateTaxonomy loads the effective taxonomy for task.ProjectID and runs
// TaxonomyValidator against task. Returns nil when the projectSvc is not
// wired or the taxonomy is empty (levels disabled). When the task has a
// parent, parent.Level is loaded from bundle.Tasks and passed to the
// validator so rank-compatibility can be checked.
func (s *TaskService) validateTaxonomy(ctx context.Context, bundle *RepoBundle, task *domain.Task) error {
	if s.projectSvc == nil {
		return nil
	}
	project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
	if err != nil {
		return err
	}
	tx, _ := s.projectSvc.EffectiveTaxonomy(project)
	if tx.IsEmpty() {
		return nil
	}

	var parentLevel *string
	if task.ParentID != nil {
		parent, err := bundle.Tasks.GetByID(ctx, *task.ParentID)
		if err != nil {
			return err
		}
		var lvl string
		if parent.Level != nil {
			lvl = *parent.Level
		}
		parentLevel = &lvl
	}

	return domain.TaxonomyValidator{}.Check(
		domain.ValidationContext{Taxonomy: tx, ParentLevel: parentLevel},
		task,
	)
}

// maxParentDepth guards cycle detection and auto-complete propagation
// against corrupted data causing infinite loops.
const maxParentDepth = 100

// detectParentCycle walks up the ancestor chain from proposedParentID.
// If it encounters taskID, the proposed parent relationship would
// create a cycle.
func (s *TaskService) detectParentCycle(ctx context.Context, tasks repository.TaskRepository, taskID, proposedParentID uuid.UUID) error {
	current := proposedParentID
	for depth := 0; depth < maxParentDepth; depth++ {
		if current == taskID {
			return domain.ErrCyclicParent
		}
		parent, err := tasks.GetByID(ctx, current)
		if err != nil {
			return fmt.Errorf("checking parent cycle: %w", err)
		}
		if parent.ParentID == nil {
			return nil
		}
		current = *parent.ParentID
	}
	return fmt.Errorf("parent chain exceeds maximum depth (%d)", maxParentDepth)
}

// generateShortID derives a short ID from the task's UUID. It starts
// with 8 hex characters and extends if a collision is detected in any
// known project store (short IDs are globally unique).
func (s *TaskService) generateShortID(ctx context.Context, id uuid.UUID) (string, error) {
	hex := strings.ReplaceAll(id.String(), "-", "")
	for length := 8; length <= len(hex); length++ {
		candidate := hex[:length]
		_, _, err := s.bundleForShortID(ctx, candidate)
		if errors.Is(err, domain.ErrNotFound) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("could not generate unique short ID")
}

// Annotate adds a timestamped note to a task.
func (s *TaskService) Annotate(ctx context.Context, taskShortID string, body string) (*domain.Annotation, error) {
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("annotation body must not be empty")
	}

	bundle, task, err := s.bundleForShortID(ctx, taskShortID)
	if err != nil {
		return nil, err
	}

	ann := &domain.Annotation{
		ID:        uuid.New(),
		TaskID:    task.ID,
		Body:      body,
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}

	if err := bundle.Annotations.Create(ctx, ann); err != nil {
		return nil, err
	}
	return ann, nil
}

// GetAnnotations returns all annotations for a task, identified by
// short ID.
func (s *TaskService) GetAnnotations(ctx context.Context, taskShortID string) ([]*domain.Annotation, error) {
	bundle, task, err := s.bundleForShortID(ctx, taskShortID)
	if err != nil {
		return nil, err
	}
	return bundle.Annotations.GetByTask(ctx, task.ID)
}

// DeleteAnnotation removes an annotation by its ID. Fan-out: every
// project store is asked to delete; returns domain.ErrNotFound if no
// store held the row.
func (s *TaskService) DeleteAnnotation(ctx context.Context, annotationID uuid.UUID) error {
	ids, err := s.projects(ctx)
	if err != nil {
		return err
	}
	deleted := false
	for _, pid := range ids {
		bundle, err := s.resolve(ctx, pid)
		if err != nil {
			return err
		}
		err = bundle.Annotations.Delete(ctx, annotationID)
		if err == nil {
			deleted = true
			continue
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
	}
	if !deleted {
		return domain.ErrNotFound
	}
	return nil
}

// checkAutoComplete checks whether completing a task should trigger
// automatic completion of its parent. If the task has a parent, all
// non-deleted siblings are at the trigger status, and the workflow
// allows the transition, the parent is auto-completed. Each cascaded
// parent transition emits a status_changed event with source="auto_complete".
func (s *TaskService) checkAutoComplete(
	ctx context.Context,
	task *domain.Task,
	tx WriteTx,
) error {
	tr := tx.Tasks()
	actor := ActorFromContext(ctx)
	current := task
	for depth := 0; depth < maxParentDepth; depth++ {
		if current.ParentID == nil {
			return nil
		}

		parent, err := tr.GetByID(ctx, *current.ParentID)
		if err != nil {
			return fmt.Errorf("loading parent for propagation: %w", err)
		}

		project, err := s.projectRepo.GetByID(ctx, parent.ProjectID)
		if err != nil {
			return fmt.Errorf("loading project for propagation: %w", err)
		}

		cfg := project.Settings.AutoCompleteParent
		if cfg == nil {
			return nil
		}

		if current.Status != cfg.TriggerStatus {
			return nil
		}

		wfName, err := s.workflowName(ctx, project)
		if err != nil {
			return err
		}

		children, err := tr.GetChildren(ctx, parent.ID)
		if err != nil {
			return fmt.Errorf("loading siblings for propagation: %w", err)
		}

		deleteStatus, err := s.workflowSvc.GetDeleteStatus(ctx, wfName)
		if err != nil {
			return fmt.Errorf("resolving delete status for propagation: %w", err)
		}

		allReady := true
		for _, child := range children {
			if child.Status == deleteStatus {
				continue
			}
			if child.Status != cfg.TriggerStatus {
				allReady = false
				break
			}
		}
		if !allReady {
			return nil
		}

		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, wfName, parent.Status, cfg.TargetStatus)
		if err != nil {
			return fmt.Errorf("checking propagation transition: %w", err)
		}
		if !allowed {
			return nil
		}

		prevParentStatus := parent.Status
		parent.Status = cfg.TargetStatus
		parent.ModifiedAt = time.Now().UTC().Truncate(time.Millisecond)
		if err := tr.Update(ctx, parent); err != nil {
			return fmt.Errorf("auto-completing parent: %w", err)
		}

		current, err = tr.GetByID(ctx, parent.ID)
		if err != nil {
			return fmt.Errorf("re-reading parent after propagation: %w", err)
		}

		roles, err := s.workflowSvc.GetStatusRoles(ctx, wfName, current.Status)
		if err != nil {
			return fmt.Errorf("loading roles for auto-complete status: %w", err)
		}
		evt := domain.NewStatusChangedEvent(current, prevParentStatus, current.Status, roles, "auto_complete", actor)
		if err := tx.Events().Record(ctx, evt); err != nil {
			return fmt.Errorf("recording auto_complete event: %w", err)
		}
	}
	return fmt.Errorf("auto-complete propagation exceeded maximum depth (%d)", maxParentDepth)
}

// checkAutoRevert checks whether a task moving away from the trigger
// status should revert its parent. Each cascaded parent transition emits a
// status_changed event with source="auto_revert".
func (s *TaskService) checkAutoRevert(
	ctx context.Context,
	task *domain.Task,
	oldStatus string,
	tx WriteTx,
) error {
	tr := tx.Tasks()
	actor := ActorFromContext(ctx)
	current := task
	currentOldStatus := oldStatus
	for depth := 0; depth < maxParentDepth; depth++ {
		if current.ParentID == nil {
			return nil
		}

		parent, err := tr.GetByID(ctx, *current.ParentID)
		if err != nil {
			return fmt.Errorf("loading parent for revert: %w", err)
		}

		project, err := s.projectRepo.GetByID(ctx, parent.ProjectID)
		if err != nil {
			return fmt.Errorf("loading project for revert: %w", err)
		}

		revertCfg := project.Settings.AutoRevertParent
		if revertCfg == nil {
			return nil
		}

		if currentOldStatus != revertCfg.TriggerStatus {
			return nil
		}
		if current.Status == revertCfg.TriggerStatus {
			return nil
		}

		completeCfg := project.Settings.AutoCompleteParent
		if completeCfg == nil {
			return nil
		}
		if parent.Status != completeCfg.TargetStatus {
			return nil
		}

		wfName, err := s.workflowName(ctx, project)
		if err != nil {
			return err
		}

		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, wfName, parent.Status, revertCfg.TargetStatus)
		if err != nil {
			return fmt.Errorf("checking revert transition: %w", err)
		}
		if !allowed {
			return nil
		}

		prevParentStatus := parent.Status
		parent.Status = revertCfg.TargetStatus
		parent.ModifiedAt = time.Now().UTC().Truncate(time.Millisecond)
		if err := tr.Update(ctx, parent); err != nil {
			return fmt.Errorf("reverting parent: %w", err)
		}

		current, err = tr.GetByID(ctx, parent.ID)
		if err != nil {
			return fmt.Errorf("re-reading parent after revert: %w", err)
		}

		roles, err := s.workflowSvc.GetStatusRoles(ctx, wfName, current.Status)
		if err != nil {
			return fmt.Errorf("loading roles for auto-revert status: %w", err)
		}
		evt := domain.NewStatusChangedEvent(current, prevParentStatus, current.Status, roles, "auto_revert", actor)
		if err := tx.Events().Record(ctx, evt); err != nil {
			return fmt.Errorf("recording auto_revert event: %w", err)
		}
		currentOldStatus = prevParentStatus
	}
	return fmt.Errorf("auto-revert propagation exceeded maximum depth (%d)", maxParentDepth)
}

// ptr is a generic helper that returns a pointer to the given value.
func ptr[T any](v T) *T {
	return &v
}

// LevelViolation reports a single task whose state conflicts with its
// project's effective taxonomy. TaskService.LevelCheck returns a slice of
// these so callers can render retroactive-violation reports.
type LevelViolation struct {
	Task     *domain.Task
	Taxonomy domain.Taxonomy
	Source   TaxonomySource
	Err      *domain.TaxonomyError
}

// LevelCheck walks tasks matching filter and reports each task whose state
// violates its project's effective taxonomy. Callers that want every status
// scanned (including terminal) should resolve the filter via
// filter.Resolver.ResolveExprAllStatuses so the default-status wrapper is
// skipped. LevelCheck never mutates and does not emit events.
func (s *TaskService) LevelCheck(ctx context.Context, filter domain.FilterExpr) ([]LevelViolation, error) {
	if s.projectSvc == nil {
		return nil, nil
	}

	projectIDs, err := s.projects(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing projects: %w", err)
	}

	seenBundles := make(map[*RepoBundle]struct{})
	var bundles []*RepoBundle
	for _, pid := range projectIDs {
		bundle, err := s.resolve(ctx, pid)
		if err != nil {
			return nil, fmt.Errorf("resolving bundle for project %v: %w", pid, err)
		}
		if _, ok := seenBundles[bundle]; ok {
			continue
		}
		seenBundles[bundle] = struct{}{}
		bundles = append(bundles, bundle)
	}

	type projectCtx struct {
		project  *domain.Project
		taxonomy domain.Taxonomy
		source   TaxonomySource
	}
	projectCache := make(map[uuid.UUID]*projectCtx)
	resolveProject := func(pid uuid.UUID) (*projectCtx, error) {
		if pc, ok := projectCache[pid]; ok {
			return pc, nil
		}
		p, err := s.projectRepo.GetByID(ctx, pid)
		if err != nil {
			return nil, fmt.Errorf("loading project %v: %w", pid, err)
		}
		tx, src := s.projectSvc.EffectiveTaxonomy(p)
		pc := &projectCtx{project: p, taxonomy: tx, source: src}
		projectCache[pid] = pc
		return pc, nil
	}

	var violations []LevelViolation
	for _, bundle := range bundles {
		tasks, err := bundle.Tasks.List(ctx, filter)
		if err != nil {
			return nil, fmt.Errorf("listing tasks for level-check: %w", err)
		}
		for _, task := range tasks {
			pc, err := resolveProject(task.ProjectID)
			if err != nil {
				return nil, err
			}
			if pc.taxonomy.IsEmpty() {
				continue
			}

			var parentLevel *string
			if task.ParentID != nil {
				parent, err := bundle.Tasks.GetByID(ctx, *task.ParentID)
				if err != nil {
					if errors.Is(err, domain.ErrNotFound) {
						continue
					}
					return nil, fmt.Errorf("loading parent %v: %w", *task.ParentID, err)
				}
				var lvl string
				if parent.Level != nil {
					lvl = *parent.Level
				}
				parentLevel = &lvl
			}

			checkErr := domain.TaxonomyValidator{}.Check(
				domain.ValidationContext{Taxonomy: pc.taxonomy, ParentLevel: parentLevel},
				task,
			)
			if checkErr == nil {
				continue
			}
			var te *domain.TaxonomyError
			if !errors.As(checkErr, &te) {
				continue
			}
			violations = append(violations, LevelViolation{
				Task:     task,
				Taxonomy: pc.taxonomy,
				Source:   pc.source,
				Err:      te,
			})
		}
	}

	sort.SliceStable(violations, func(i, j int) bool {
		pi := projectCache[violations[i].Task.ProjectID].project.Name
		pj := projectCache[violations[j].Task.ProjectID].project.Name
		if pi != pj {
			return pi < pj
		}
		return violations[i].Task.ShortID < violations[j].Task.ShortID
	})

	return violations, nil
}
