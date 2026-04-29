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
func (service *TaskService) defaultProjectID(ctx context.Context) (uuid.UUID, error) {
	proj, projectErr := service.projectRepo.GetByID(ctx, domain.DefaultProjectUUID)

	if projectErr != nil {
		return uuid.Nil, fmt.Errorf("looking up default project: %w", projectErr)
	}

	return proj.ID, nil
}

// workflowName resolves the project's workflow UUID to its name via the
// workflow service. Centralizes the lookup so callers that previously read
// project.Workflow (a compat string field removed in Phase 4) share one path.
func (service *TaskService) workflowName(ctx context.Context, project *domain.Project) (string, error) {
	workflow, workflowErr := service.workflowSvc.GetByID(ctx, project.WorkflowID)

	if workflowErr != nil {
		return "", fmt.Errorf("looking up workflow %v: %w", project.WorkflowID, workflowErr)
	}

	return workflow.Name, nil
}

// ResolveProjectName looks up a project by name and returns its UUID.
// CLI and MCP callers use this to translate user-entered project names
// into the typed Task.ProjectID value before calling Create.
// Returns domain.ErrNotFound if the project does not exist.
func (service *TaskService) ResolveProjectName(ctx context.Context, name string) (uuid.UUID, error) {
	if name == "" {
		return service.defaultProjectID(ctx)
	}

	proj, projectErr := service.projectRepo.GetByName(ctx, name)

	if projectErr != nil {
		return uuid.Nil, projectErr
	}

	return proj.ID, nil
}

// defaultBundle resolves the bundle backing the default project. Player
// records and tag definitions are global resources kept in the default
// store, so operations involving them go through this bundle regardless
// of which project a task lives in.
func (service *TaskService) defaultBundle(ctx context.Context) (*RepoBundle, error) {
	defaultID, defaultErr := service.defaultProjectID(ctx)

	if defaultErr != nil {
		return nil, defaultErr
	}

	return service.resolve(ctx, defaultID)
}

func (service *TaskService) bundleForShortID(ctx context.Context, shortID string) (*RepoBundle, *domain.Task, error) {
	defaultID, defaultErr := service.defaultProjectID(ctx)

	if defaultErr != nil {
		return nil, nil, defaultErr
	}

	bundle, bundleErr := service.resolve(ctx, defaultID)

	if bundleErr != nil {
		return nil, nil, bundleErr
	}

	task, taskErr := bundle.Tasks.GetByShortID(ctx, shortID)

	if taskErr != nil {
		return nil, nil, taskErr
	}

	return bundle, task, nil
}

func (service *TaskService) bundleForID(ctx context.Context, id uuid.UUID) (*RepoBundle, *domain.Task, error) {
	defaultID, defaultErr := service.defaultProjectID(ctx)

	if defaultErr != nil {
		return nil, nil, defaultErr
	}

	bundle, bundleErr := service.resolve(ctx, defaultID)

	if bundleErr != nil {
		return nil, nil, bundleErr
	}

	task, taskErr := bundle.Tasks.GetByID(ctx, id)

	if taskErr != nil {
		return nil, nil, taskErr
	}

	return bundle, task, nil
}

// Create validates and persists a new task. It populates the task's ID,
// ShortID, Version, timestamps, and default ProjectID before saving.
func (service *TaskService) Create(ctx context.Context, task *domain.Task) error {
	if strings.TrimSpace(task.Title) == "" {
		return fmt.Errorf("title must not be empty")
	}
	if task.Priority < 0 || task.Priority > 4 {
		return fmt.Errorf("priority must be between 0 and 4")
	}

	if task.ProjectID == uuid.Nil {
		defaultID, defaultErr := service.defaultProjectID(ctx)

		if defaultErr != nil {
			return defaultErr
		}

		task.ProjectID = defaultID
	}

	project, projectErr := service.projectRepo.GetByID(ctx, task.ProjectID)

	if projectErr != nil {
		if errors.Is(projectErr, domain.ErrNotFound) {
			return fmt.Errorf("project not found: %w", projectErr)
		}
		return fmt.Errorf("looking up project: %w", projectErr)
	}

	workflowName, workflowErr := service.workflowName(ctx, project)

	if workflowErr != nil {
		return workflowErr
	}

	bundle, bundleErr := service.resolve(ctx, task.ProjectID)

	if bundleErr != nil {
		return fmt.Errorf("resolving project store: %w", bundleErr)
	}

	if task.ParentID != nil {
		_, parentErr := bundle.Tasks.GetByID(ctx, *task.ParentID)

		if parentErr != nil {
			if errors.Is(parentErr, domain.ErrNotFound) {
				return fmt.Errorf("parent task not found: %w", parentErr)
			}
			return fmt.Errorf("looking up parent task: %w", parentErr)
		}
	}

	if taxErr := service.validateTaxonomy(ctx, bundle, task); taxErr != nil {
		return taxErr
	}

	if task.Status == "" {
		initialStatus, initialErr := service.workflowSvc.GetStatusByRole(ctx, workflowName, domain.RoleInitial)

		if initialErr != nil {
			return fmt.Errorf("resolving initial status: %w", initialErr)
		}

		task.Status = initialStatus
	}

	statuses, statusesErr := service.workflowSvc.GetStatuses(ctx, workflowName)

	if statusesErr != nil {
		return fmt.Errorf("loading workflow statuses: %w", statusesErr)
	}

	validStatus := false
	for _, st := range statuses {
		if st == task.Status {
			validStatus = true
			break
		}
	}
	if !validStatus {
		return fmt.Errorf("status %q is not valid for workflow %q", task.Status, workflowName)
	}

	task.ID = uuid.New()
	shortID, shortIDErr := service.generateShortID(ctx, task.ID)

	if shortIDErr != nil {
		return fmt.Errorf("generating short ID: %w", shortIDErr)
	}

	task.ShortID = shortID

	if task.Order == nil {
		next, nextErr := bundle.Tasks.NextOrder(ctx, task.ParentID)

		if nextErr != nil {
			return fmt.Errorf("computing default order: %w", nextErr)
		}

		task.Order = &next
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	task.Version = 1
	task.CreatedAt = now
	task.ModifiedAt = now
	if task.UDA != nil {
		if udaErr := domain.ValidateUDA(task.UDA); udaErr != nil {
			return udaErr
		}
	} else {
		task.UDA = map[string]any{}
	}

	actor := ActorFromContext(ctx)
	return bundle.WriteTx.WithTx(ctx, func(tx WriteTx) error {
		if createErr := tx.Tasks().Create(ctx, task); createErr != nil {
			return createErr
		}
		return tx.Events().Record(ctx, domain.NewTaskCreatedEvent(task, actor))
	})
}

// GetByShortID retrieves a task by its short ID, searching every known
// project store.
func (service *TaskService) GetByShortID(ctx context.Context, shortID string) (*domain.Task, error) {
	_, task, err := service.bundleForShortID(ctx, shortID)

	if err != nil {
		return task, err
	}

	if stampErr := service.stampEffectiveWeights(ctx, task); stampErr != nil {
		return task, stampErr
	}

	return task, nil
}

// GetByID retrieves a task by its full UUID, searching every known
// project store.
func (service *TaskService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	_, task, err := service.bundleForID(ctx, id)

	if err != nil {
		return task, err
	}

	if stampErr := service.stampEffectiveWeights(ctx, task); stampErr != nil {
		return task, stampErr
	}

	return task, nil
}

// stampEffectiveWeights populates task.EffectiveWeights if the task's chain
// contributes any non-default value. Safe to call on any loaded task; a nil
// engine leaves the field unchanged.
func (service *TaskService) stampEffectiveWeights(ctx context.Context, task *domain.Task) error {
	if service.engine == nil || task == nil {
		return nil
	}
	weights, has, weightsErr := service.ResolveEffectiveWeights(ctx, task.ID)

	if weightsErr != nil {
		return weightsErr
	}

	if has {
		resolved := weights.Resolved()
		task.EffectiveWeights = &resolved
	}
	return nil
}

// List returns tasks matching the given filter, scored and sorted by
// urgency.
func (service *TaskService) List(ctx context.Context, filter domain.FilterExpr) ([]*domain.Task, error) {
	defaultID, defaultErr := service.defaultProjectID(ctx)

	if defaultErr != nil {
		return nil, defaultErr
	}

	bundle, bundleErr := service.resolve(ctx, defaultID)

	if bundleErr != nil {
		return nil, bundleErr
	}

	return service.listInBundle(ctx, bundle, filter)
}

// listInBundle runs the filter against a single bundle and scores the
// resulting tasks using that bundle's own relation, annotation, and tag
// repos.
func (service *TaskService) listInBundle(ctx context.Context, bundle *RepoBundle, filter domain.FilterExpr) ([]*domain.Task, error) {
	tasks, tasksErr := bundle.Tasks.List(ctx, filter)

	if tasksErr != nil {
		return nil, tasksErr
	}

	if len(tasks) == 0 || service.engine == nil {
		return tasks, nil
	}

	taskIDs := make([]uuid.UUID, len(tasks))
	for index, task := range tasks {
		taskIDs[index] = task.ID
	}

	blockingCounts, blockingErr := bundle.Relations.CountBlockingByTasks(ctx, taskIDs)

	if blockingErr != nil {
		return nil, fmt.Errorf("loading blocking counts: %w", blockingErr)
	}

	blockedByCounts, blockedByErr := bundle.Relations.CountBlockedByTasks(ctx, taskIDs)

	if blockedByErr != nil {
		return nil, fmt.Errorf("loading blocked-by counts: %w", blockedByErr)
	}

	annotationCounts, annotationErr := bundle.Annotations.CountByTasks(ctx, taskIDs)

	if annotationErr != nil {
		return nil, fmt.Errorf("loading annotation counts: %w", annotationErr)
	}

	tagsByTask, tagsErr := bundle.Tags.GetTaskTagsBatch(ctx, taskIDs)

	if tagsErr != nil {
		return nil, fmt.Errorf("loading tag counts: %w", tagsErr)
	}

	tagCounts := make(map[uuid.UUID]int, len(tagsByTask))
	for id, tags := range tagsByTask {
		tagCounts[id] = len(tags)
	}

	projectWeights := service.buildProjectWeights(ctx, tasks)
	effective, effectiveErr := service.buildEffectiveWeights(ctx, bundle, tasks, projectWeights)

	if effectiveErr != nil {
		return nil, effectiveErr
	}

	sctx := ScoringContext{
		BlockingCount:    blockingCounts,
		BlockedByCount:   blockedByCounts,
		AnnotationCount:  annotationCounts,
		TagCount:         tagCounts,
		ProjectWeights:   projectWeights,
		EffectiveWeights: effective,
	}
	service.engine.ScoreAndSort(tasks, sctx)
	for _, task := range tasks {
		if weights, ok := effective[task.ID]; ok {
			resolved := weights.Resolved()
			task.EffectiveWeights = &resolved
		}
	}
	return tasks, nil
}

// Next returns the highest-urgency actionable task. Actionable means:
// non-terminal status, not waiting, not blocked. Returns
// domain.ErrNotFound if no actionable task exists.
func (service *TaskService) Next(ctx context.Context) (*domain.Task, error) {
	nonTerminal, nonTerminalErr := service.collectNonTerminalStatuses(ctx)

	if nonTerminalErr != nil {
		return nil, nonTerminalErr
	}

	defaultID, defaultErr := service.defaultProjectID(ctx)

	if defaultErr != nil {
		return nil, defaultErr
	}

	bundle, bundleErr := service.resolve(ctx, defaultID)

	if bundleErr != nil {
		return nil, bundleErr
	}

	filter := &domain.TermFilter{TaskFilter: domain.TaskFilter{Statuses: nonTerminal}}
	tasks, tasksErr := service.listInBundle(ctx, bundle, filter)

	if tasksErr != nil {
		return nil, tasksErr
	}

	now := time.Now()
	for _, task := range tasks {
		if task.WaitUntil != nil && task.WaitUntil.After(now) {
			continue
		}
		blockedBy, blockedByErr := bundle.Relations.CountBlockedByTasks(ctx, []uuid.UUID{task.ID})

		if blockedByErr != nil {
			return nil, fmt.Errorf("checking blocked status: %w", blockedByErr)
		}

		if blockedBy[task.ID] > 0 {
			continue
		}
		return task, nil
	}
	return nil, domain.ErrNotFound
}

// collectNonTerminalStatuses returns the union of non-terminal status
// names across all configured workflows. Used by Available and Next to
// find actionable tasks regardless of which workflow defines the status.
func (service *TaskService) collectNonTerminalStatuses(ctx context.Context) ([]string, error) {
	workflows, workflowsErr := service.workflowSvc.List(ctx)

	if workflowsErr != nil {
		return nil, fmt.Errorf("listing workflows: %w", workflowsErr)
	}

	seen := make(map[string]bool)
	var result []string
	for _, workflow := range workflows {
		for _, name := range workflow.NonTerminalStatuses() {
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
func (service *TaskService) buildProjectWeights(ctx context.Context, tasks []*domain.Task) map[uuid.UUID]*UrgencyWeights {
	if service.engine == nil {
		return nil
	}
	seen := make(map[uuid.UUID]bool)
	for _, task := range tasks {
		seen[task.ProjectID] = true
	}
	weights := make(map[uuid.UUID]*UrgencyWeights, len(seen))
	for projectID := range seen {
		project, err := service.projectRepo.GetByID(ctx, projectID)

		if err != nil {
			continue
		}

		if project.Settings.Urgency == nil {
			continue
		}
		merged := MergeWeights(service.engine.defaults, project.Settings.Urgency)
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
func (service *TaskService) buildEffectiveWeights(
	ctx context.Context,
	bundle *RepoBundle,
	tasks []*domain.Task,
	projectWeights map[uuid.UUID]*UrgencyWeights,
) (map[uuid.UUID]*UrgencyWeights, error) {
	if service.engine == nil || len(tasks) == 0 {
		return nil, nil
	}

	needsWalk := false
	for _, task := range tasks {
		if task.UrgencyOverrides != nil || task.ParentID != nil {
			needsWalk = true
			break
		}
	}
	if !needsWalk {
		return nil, nil
	}

	taskIDs := make([]uuid.UUID, len(tasks))
	for index, task := range tasks {
		taskIDs[index] = task.ID
	}
	rows, rowsErr := bundle.Tasks.GetAncestorOverrides(ctx, taskIDs)

	if rowsErr != nil {
		return nil, fmt.Errorf("loading ancestor overrides: %w", rowsErr)
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
	for _, task := range tasks {
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
		// Reverse to root → self.
		for lo, hi := 0, len(chain)-1; lo < hi; lo, hi = lo+1, hi-1 {
			chain[lo], chain[hi] = chain[hi], chain[lo]
		}

		var merged UrgencyWeights
		if pw, ok := projectWeights[task.ProjectID]; ok {
			merged = *pw
		} else {
			merged = service.engine.Defaults()
		}

		contributed := false
		for _, id := range chain {
			if override := overridesByID[id]; override != nil {
				merged = MergeWeights(merged, override)
				contributed = true
			}
		}
		if contributed {
			clone := merged
			out[task.ID] = &clone
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
func (service *TaskService) resolveEffectiveWeightsFromTask(
	ctx context.Context,
	bundle *RepoBundle,
	task *domain.Task,
) (UrgencyWeights, error) {
	if service.engine == nil {
		return UrgencyWeights{}, fmt.Errorf("urgency engine not configured")
	}

	projectWeights := service.buildProjectWeights(ctx, []*domain.Task{task})
	var merged UrgencyWeights
	if pw, ok := projectWeights[task.ProjectID]; ok {
		merged = *pw
	} else {
		merged = service.engine.Defaults()
	}

	parentByID := make(map[uuid.UUID]*uuid.UUID)
	overridesByID := make(map[uuid.UUID]*domain.UrgencyOverrides)
	if task.ParentID != nil {
		rows, rowsErr := bundle.Tasks.GetAncestorOverrides(ctx, []uuid.UUID{task.ID})

		if rowsErr != nil {
			return UrgencyWeights{}, fmt.Errorf("loading ancestor overrides: %w", rowsErr)
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
	for lo, hi := 0, len(chain)-1; lo < hi; lo, hi = lo+1, hi-1 {
		chain[lo], chain[hi] = chain[hi], chain[lo]
	}

	for _, id := range chain {
		if override := overridesByID[id]; override != nil {
			merged = MergeWeights(merged, override)
		}
	}
	return merged, nil
}

// ResolveEffectiveWeights returns the fully-resolved urgency weights for a
// single task, walking the project + ancestor + self override chain. The
// second return is true when any node in the chain contributed a non-default
// value (drives Phase 4's render-or-omit decision for effective_urgency_weights).
func (service *TaskService) ResolveEffectiveWeights(ctx context.Context, taskID uuid.UUID) (UrgencyWeights, bool, error) {
	if service.engine == nil {
		return UrgencyWeights{}, false, fmt.Errorf("urgency engine not configured")
	}
	bundle, task, bundleErr := service.bundleForID(ctx, taskID)

	if bundleErr != nil {
		if errors.Is(bundleErr, domain.ErrNotFound) {
			return UrgencyWeights{}, false, bundleErr
		}
		return UrgencyWeights{}, false, fmt.Errorf("resolving bundle: %w", bundleErr)
	}

	projectWeights := service.buildProjectWeights(ctx, []*domain.Task{task})
	effective, effectiveErr := service.buildEffectiveWeights(ctx, bundle, []*domain.Task{task}, projectWeights)

	if effectiveErr != nil {
		return UrgencyWeights{}, false, effectiveErr
	}

	if weights, ok := effective[task.ID]; ok {
		return *weights, true, nil
	}
	if pw, ok := projectWeights[task.ProjectID]; ok {
		return *pw, false, nil
	}
	return service.engine.Defaults(), false, nil
}

// GetChildren returns the direct children of a task. The parent task
// must exist in some known project store.
func (service *TaskService) GetChildren(ctx context.Context, parentID uuid.UUID) ([]*domain.Task, error) {
	bundle, _, err := service.bundleForID(ctx, parentID)

	if err != nil {
		return nil, err
	}

	return bundle.Tasks.GetChildren(ctx, parentID)
}

// GetDescendants returns all descendants of a task (recursive).
func (service *TaskService) GetDescendants(ctx context.Context, rootID uuid.UUID) ([]*domain.Task, error) {
	bundle, _, err := service.bundleForID(ctx, rootID)

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
func (service *TaskService) SummarizeSubtree(ctx context.Context, rootID uuid.UUID) (*domain.SummaryBlock, error) {
	bundle, root, bundleErr := service.bundleForID(ctx, rootID)

	if bundleErr != nil {
		return nil, bundleErr
	}

	descendants, descendantsErr := bundle.Tasks.GetDescendants(ctx, rootID)

	if descendantsErr != nil {
		return nil, fmt.Errorf("loading descendants: %w", descendantsErr)
	}

	projectWorkflows, workflowErr := service.workflowsByProject(ctx, descendants, root)

	if workflowErr != nil {
		return nil, workflowErr
	}

	rollup := domain.AggregateRollup(descendants, projectWorkflows)
	return &domain.SummaryBlock{Task: root, Rollup: rollup}, nil
}

// SummarizeBlocks selects block tasks via blockFilter and rolls up
// each one's strict descendants. blockFilter == nil means "blocks are
// root tasks (parent_id IS NULL)". When full == false, blockFilter
// ALSO restricts which descendants are counted under each block; when
// full == true, blockFilter only selects blocks and the full subtree
// under each block is counted. Blocks are returned in urgency-desc
// order with short_id ascending as tiebreaker.
func (service *TaskService) SummarizeBlocks(
	ctx context.Context,
	blockFilter domain.FilterExpr,
	full bool,
) ([]*domain.SummaryBlock, error) {
	defaultID, defaultErr := service.defaultProjectID(ctx)

	if defaultErr != nil {
		return nil, defaultErr
	}

	bundle, bundleErr := service.resolve(ctx, defaultID)

	if bundleErr != nil {
		return nil, bundleErr
	}

	var blocks []*domain.Task
	if blockFilter == nil {
		all, allErr := service.listInBundle(ctx, bundle, nil)

		if allErr != nil {
			return nil, fmt.Errorf("listing root tasks: %w", allErr)
		}

		for _, task := range all {
			if task.ParentID == nil {
				blocks = append(blocks, task)
			}
		}
	} else {
		blocksErr := error(nil)
		blocks, blocksErr = service.listInBundle(ctx, bundle, blockFilter)

		if blocksErr != nil {
			return nil, fmt.Errorf("listing blocks: %w", blocksErr)
		}
	}

	if len(blocks) == 0 {
		return []*domain.SummaryBlock{}, nil
	}

	needTags := !full && blockFilter != nil && filterUsesTags(blockFilter)

	out := make([]*domain.SummaryBlock, 0, len(blocks))
	for _, block := range blocks {
		descendants, descendantsErr := bundle.Tasks.GetDescendants(ctx, block.ID)

		if descendantsErr != nil {
			return nil, fmt.Errorf("loading descendants for block %s: %w", block.ShortID, descendantsErr)
		}

		if !full && blockFilter != nil {
			tagsFor, tagsErr := descendantTagsLookup(ctx, bundle, descendants, needTags)

			if tagsErr != nil {
				return nil, fmt.Errorf("loading descendant tags for block %s: %w", block.ShortID, tagsErr)
			}

			filtered := descendants[:0]
			for _, desc := range descendants {
				if domain.EvalFilter(blockFilter, desc, tagsFor) {
					filtered = append(filtered, desc)
				}
			}
			descendants = filtered
		}
		projectWorkflows, workflowErr := service.workflowsByProject(ctx, descendants, block)

		if workflowErr != nil {
			return nil, workflowErr
		}

		rollup := domain.AggregateRollup(descendants, projectWorkflows)
		out = append(out, &domain.SummaryBlock{Task: block, Rollup: rollup})
	}

	sort.SliceStable(out, func(ii, jj int) bool {
		ui, uj := out[ii].Task.Urgency, out[jj].Task.Urgency
		if ui != uj {
			return ui > uj
		}
		return out[ii].Task.ShortID < out[jj].Task.ShortID
	})
	return out, nil
}

// workflowsByProject preloads each distinct project's workflow once and
// returns a closure suitable as the workflowFor callback to
// domain.AggregateRollup. The seedTask is included in the project set so
// the seed-workflow lookup remains stable when the descendant slice is
// empty.
func (service *TaskService) workflowsByProject(
	ctx context.Context,
	descendants []*domain.Task,
	seed *domain.Task,
) (func(*domain.Task) *domain.Workflow, error) {
	projectIDs := make(map[uuid.UUID]struct{})
	if seed != nil {
		projectIDs[seed.ProjectID] = struct{}{}
	}
	for _, task := range descendants {
		if task == nil {
			continue
		}
		projectIDs[task.ProjectID] = struct{}{}
	}
	cache := make(map[uuid.UUID]*domain.Workflow, len(projectIDs))
	for projectID := range projectIDs {
		project, projectErr := service.projectRepo.GetByID(ctx, projectID)

		if projectErr != nil {
			if errors.Is(projectErr, domain.ErrNotFound) {
				cache[projectID] = nil
				continue
			}
			return nil, fmt.Errorf("looking up project %v: %w", projectID, projectErr)
		}

		workflow, workflowErr := service.workflowSvc.GetByID(ctx, project.WorkflowID)

		if workflowErr != nil {
			if errors.Is(workflowErr, domain.ErrNotFound) {
				cache[projectID] = nil
				continue
			}
			return nil, fmt.Errorf("looking up workflow %v: %w", project.WorkflowID, workflowErr)
		}

		cache[projectID] = workflow
	}
	return func(task *domain.Task) *domain.Workflow {
		if task == nil {
			return nil
		}
		return cache[task.ProjectID]
	}, nil
}

// filterUsesTags reports whether expr references Tags or ExcludeTags
// anywhere in its tree. Used to skip the tag batch-fetch in
// SummarizeBlocks when descendant filtering doesn't need it.
func filterUsesTags(expr domain.FilterExpr) bool {
	switch filterExpr := expr.(type) {
	case *domain.TermFilter:
		return len(filterExpr.Tags) > 0 || len(filterExpr.ExcludeTags) > 0
	case domain.TermFilter:
		return len(filterExpr.Tags) > 0 || len(filterExpr.ExcludeTags) > 0
	case *domain.AndFilter:
		for _, child := range filterExpr.Children {
			if filterUsesTags(child) {
				return true
			}
		}
	case domain.AndFilter:
		for _, child := range filterExpr.Children {
			if filterUsesTags(child) {
				return true
			}
		}
	case *domain.OrFilter:
		for _, child := range filterExpr.Children {
			if filterUsesTags(child) {
				return true
			}
		}
	case domain.OrFilter:
		for _, child := range filterExpr.Children {
			if filterUsesTags(child) {
				return true
			}
		}
	case *domain.NotFilter:
		return filterUsesTags(filterExpr.Child)
	case domain.NotFilter:
		return filterUsesTags(filterExpr.Child)
	}
	return false
}

// descendantTagsLookup returns a closure that maps a task ID to its tag
// names. When need is false, returns a nil closure so EvalFilter falls
// through to its tag-match-all branch (saves the round-trip when no tag
// predicate is in the filter).
func descendantTagsLookup(
	ctx context.Context,
	bundle *RepoBundle,
	descendants []*domain.Task,
	need bool,
) (func(uuid.UUID) []string, error) {
	if !need || len(descendants) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(descendants))
	for _, desc := range descendants {
		if desc == nil {
			continue
		}
		ids = append(ids, desc.ID)
	}
	tagsByTask, tagsErr := bundle.Tags.GetTaskTagsBatch(ctx, ids)

	if tagsErr != nil {
		return nil, tagsErr
	}

	cache := make(map[uuid.UUID][]string, len(tagsByTask))
	for id, tags := range tagsByTask {
		names := make([]string, 0, len(tags))
		for _, tag := range tags {
			if tag != nil {
				names = append(names, tag.Name)
			}
		}
		cache[id] = names
	}
	return func(id uuid.UUID) []string {
		return cache[id]
	}, nil
}

// Update applies a partial update to a task. It validates the patched
// state, enforces workflow transitions, and uses optimistic locking.
func (service *TaskService) Update(ctx context.Context, upd domain.TaskUpdate) (*domain.Task, error) {
	bundle, task, err := service.bundleForShortID(ctx, upd.ShortID)

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
		if udaErr := domain.ValidateUDA(*upd.UDA); udaErr != nil {
			return nil, udaErr
		}
		if task.UDA == nil {
			task.UDA = map[string]any{}
		}
		for kk, vv := range *upd.UDA {
			if vv == "" {
				delete(task.UDA, kk)
			} else {
				task.UDA[kk] = vv
			}
		}
	}

	if urgencyErr := service.applyUrgencyOverridesUpdate(ctx, bundle, task, upd); urgencyErr != nil {
		return nil, urgencyErr
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
		_, parentErr := bundle.Tasks.GetByID(ctx, *task.ParentID)

		if parentErr != nil {
			if errors.Is(parentErr, domain.ErrNotFound) {
				return nil, fmt.Errorf("parent task not found: %w", parentErr)
			}
			return nil, fmt.Errorf("looking up parent task: %w", parentErr)
		}

		if cycleErr := service.detectParentCycle(ctx, bundle.Tasks, task.ID, *task.ParentID); cycleErr != nil {
			return nil, cycleErr
		}
	}

	if upd.ProjectID != nil {
		_, projectErr := service.projectRepo.GetByID(ctx, task.ProjectID)

		if projectErr != nil {
			if errors.Is(projectErr, domain.ErrNotFound) {
				return nil, fmt.Errorf("project not found: %w", projectErr)
			}
			return nil, fmt.Errorf("looking up project: %w", projectErr)
		}
	}

	if upd.Level != nil || upd.ParentID != nil || upd.ProjectID != nil {
		if taxErr := service.validateTaxonomy(ctx, bundle, task); taxErr != nil {
			return nil, taxErr
		}
	}

	statusChanged := task.Status != oldStatus
	var workflowName string
	if statusChanged {
		project, projectErr := service.projectRepo.GetByID(ctx, task.ProjectID)

		if projectErr != nil {
			return nil, fmt.Errorf("looking up project for workflow: %w", projectErr)
		}

		resolvedWorkflowName, workflowErr := service.workflowName(ctx, project)

		if workflowErr != nil {
			return nil, workflowErr
		}

		workflowName = resolvedWorkflowName
		allowed, transitionErr := service.workflowSvc.IsTransitionAllowed(ctx, workflowName, oldStatus, task.Status)

		if transitionErr != nil {
			return nil, fmt.Errorf("checking transition: %w", transitionErr)
		}

		if !allowed {
			return nil, fmt.Errorf("transition %q → %q not allowed: %w", oldStatus, task.Status, domain.ErrInvalidTransition)
		}
	}

	task.ModifiedAt = time.Now().UTC().Truncate(time.Millisecond)

	actor := ActorFromContext(ctx)
	var result *domain.Task
	err = bundle.WriteTx.WithTx(ctx, func(tx WriteTx) error {
		updated, updatedErr := service.applyValidatedUpdate(ctx, tx, task, oldStatus)

		if updatedErr != nil {
			return updatedErr
		}

		result = updated

		if statusChanged {
			roles, rolesErr := service.workflowSvc.GetStatusRoles(ctx, workflowName, updated.Status)

			if rolesErr != nil {
				return fmt.Errorf("loading roles for status: %w", rolesErr)
			}

			event := domain.NewStatusChangedEvent(updated, oldStatus, updated.Status, roles, "user", actor)
			if eventErr := tx.Events().Record(ctx, event); eventErr != nil {
				return fmt.Errorf("recording status_changed event: %w", eventErr)
			}
		}
		if changes := diffTaskFields(snapshot, updated); len(changes) > 0 {
			event := domain.NewTaskModifiedEvent(updated, changes, actor)
			if eventErr := tx.Events().Record(ctx, event); eventErr != nil {
				return fmt.Errorf("recording task_modified event: %w", eventErr)
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
func (service *TaskService) Start(ctx context.Context, shortID string, version int, playerID string) (*domain.Task, error) {
	bundle, task, bundleErr := service.bundleForShortID(ctx, shortID)

	if bundleErr != nil {
		return nil, bundleErr
	}

	if task.Version != version {
		return nil, domain.ErrConflict
	}

	project, projectErr := service.projectRepo.GetByID(ctx, task.ProjectID)

	if projectErr != nil {
		return nil, fmt.Errorf("loading project %v: %w", task.ProjectID, projectErr)
	}

	workflowName, workflowErr := service.workflowName(ctx, project)

	if workflowErr != nil {
		return nil, workflowErr
	}

	startStatus, startErr := service.workflowSvc.GetStatusByRole(ctx, workflowName, domain.RoleStart)

	if startErr != nil {
		return nil, fmt.Errorf("resolving start status: %w", startErr)
	}

	oldStatus := task.Status
	if task.Status != startStatus {
		allowed, transitionErr := service.workflowSvc.IsTransitionAllowed(ctx, workflowName, task.Status, startStatus)

		if transitionErr != nil {
			return nil, fmt.Errorf("checking transition: %w", transitionErr)
		}

		if !allowed {
			return nil, fmt.Errorf("transition %q → %q not allowed: %w", task.Status, startStatus, domain.ErrInvalidTransition)
		}
	}

	var players repository.PlayerRepository
	autoClaimed := false

	if playerID != "" {
		playersResolved, playersErr := service.playerRepo(ctx)

		if playersErr != nil {
			return nil, playersErr
		}

		players = playersResolved
		if players != nil {
			if _, playerGetErr := players.GetByID(ctx, playerID); playerGetErr != nil {
				return nil, fmt.Errorf("player %q: %w", playerID, playerGetErr)
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
	txErr := bundle.WriteTx.WithTx(ctx, func(tx WriteTx) error {
		updated, updatedErr := service.applyValidatedUpdate(ctx, tx, task, oldStatus)

		if updatedErr != nil {
			return updatedErr
		}

		result = updated
		return tx.Events().Record(ctx, domain.NewTaskStartedEvent(updated, oldStatus, autoClaimed, actor))
	})

	if txErr != nil {
		return nil, txErr
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
func (service *TaskService) playerRepo(ctx context.Context) (repository.PlayerRepository, error) {
	bundle, err := service.defaultBundle(ctx)

	if err != nil {
		return nil, fmt.Errorf("resolving default bundle for players: %w", err)
	}

	return bundle.Players, nil
}

// Claim assigns a task to a player. Returns ErrTaskClaimed if claimed
// by another player. Re-claiming by the same player is idempotent.
func (service *TaskService) Claim(ctx context.Context, shortID, playerID string, version int) (*domain.Task, error) {
	players, playersErr := service.playerRepo(ctx)

	if playersErr != nil {
		return nil, playersErr
	}

	if players == nil {
		return nil, fmt.Errorf("player support not configured")
	}

	if _, playerGetErr := players.GetByID(ctx, playerID); playerGetErr != nil {
		return nil, fmt.Errorf("player %q: %w", playerID, playerGetErr)
	}

	bundle, task, bundleErr := service.bundleForShortID(ctx, shortID)

	if bundleErr != nil {
		return nil, bundleErr
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
	txErr := bundle.WriteTx.WithTx(ctx, func(tx WriteTx) error {
		updated, updatedErr := service.applyValidatedUpdate(ctx, tx, task, oldStatus)

		if updatedErr != nil {
			return updatedErr
		}

		result = updated
		return tx.Events().Record(ctx, domain.NewTaskClaimedEvent(updated, playerID, actor))
	})

	if txErr != nil {
		return nil, txErr
	}

	players.UpdateLastSeen(ctx, playerID) //nolint:errcheck
	return result, nil
}

// Release clears a task's claim. Only the current claimant can release.
func (service *TaskService) Release(ctx context.Context, shortID, playerID string, version int) (*domain.Task, error) {
	players, playersErr := service.playerRepo(ctx)

	if playersErr != nil {
		return nil, playersErr
	}

	if players == nil {
		return nil, fmt.Errorf("player support not configured")
	}

	bundle, task, bundleErr := service.bundleForShortID(ctx, shortID)

	if bundleErr != nil {
		return nil, bundleErr
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
	txErr := bundle.WriteTx.WithTx(ctx, func(tx WriteTx) error {
		updated, updatedErr := service.applyValidatedUpdate(ctx, tx, task, oldStatus)

		if updatedErr != nil {
			return updatedErr
		}

		result = updated
		return tx.Events().Record(ctx, domain.NewTaskReleasedEvent(updated, playerID, actor))
	})

	if txErr != nil {
		return nil, txErr
	}

	players.UpdateLastSeen(ctx, playerID) //nolint:errcheck
	return result, nil
}

// Complete transitions a task to its workflow's done-role status.
func (service *TaskService) Complete(ctx context.Context, shortID string, version int) (*domain.Task, error) {
	bundle, task, bundleErr := service.bundleForShortID(ctx, shortID)

	if bundleErr != nil {
		return nil, bundleErr
	}

	if task.Version != version {
		return nil, domain.ErrConflict
	}

	project, projectErr := service.projectRepo.GetByID(ctx, task.ProjectID)

	if projectErr != nil {
		return nil, fmt.Errorf("loading project %v: %w", task.ProjectID, projectErr)
	}

	workflowName, workflowErr := service.workflowName(ctx, project)

	if workflowErr != nil {
		return nil, workflowErr
	}

	doneStatus, doneErr := service.workflowSvc.GetStatusByRole(ctx, workflowName, domain.RoleDone)

	if doneErr != nil {
		return nil, fmt.Errorf("resolving done status: %w", doneErr)
	}

	oldStatus := task.Status
	statusChanged := oldStatus != doneStatus
	if statusChanged {
		allowed, transitionErr := service.workflowSvc.IsTransitionAllowed(ctx, workflowName, oldStatus, doneStatus)

		if transitionErr != nil {
			return nil, fmt.Errorf("checking transition: %w", transitionErr)
		}

		if !allowed {
			return nil, fmt.Errorf("transition %q → %q not allowed: %w", oldStatus, doneStatus, domain.ErrInvalidTransition)
		}
	}

	task.Status = doneStatus
	task.ModifiedAt = time.Now().UTC().Truncate(time.Millisecond)

	actor := ActorFromContext(ctx)
	var result *domain.Task
	txErr := bundle.WriteTx.WithTx(ctx, func(tx WriteTx) error {
		updated, updatedErr := service.applyValidatedUpdate(ctx, tx, task, oldStatus)

		if updatedErr != nil {
			return updatedErr
		}

		result = updated
		if !statusChanged {
			return nil
		}
		return tx.Events().Record(ctx, domain.NewTaskCompletedEvent(updated, oldStatus, actor))
	})

	if txErr != nil {
		return nil, txErr
	}

	return result, nil
}

// Delete soft-deletes a task by transitioning to its workflow's
// delete-role status.
func (service *TaskService) Delete(ctx context.Context, shortID string, version int) (*domain.Task, error) {
	bundle, task, bundleErr := service.bundleForShortID(ctx, shortID)

	if bundleErr != nil {
		return nil, bundleErr
	}

	if task.Version != version {
		return nil, domain.ErrConflict
	}

	project, projectErr := service.projectRepo.GetByID(ctx, task.ProjectID)

	if projectErr != nil {
		return nil, fmt.Errorf("loading project %v: %w", task.ProjectID, projectErr)
	}

	workflowName, workflowErr := service.workflowName(ctx, project)

	if workflowErr != nil {
		return nil, workflowErr
	}

	deleteStatus, deleteErr := service.workflowSvc.GetStatusByRole(ctx, workflowName, domain.RoleDelete)

	if deleteErr != nil {
		return nil, fmt.Errorf("resolving delete status: %w", deleteErr)
	}

	oldStatus := task.Status
	statusChanged := oldStatus != deleteStatus
	if statusChanged {
		allowed, transitionErr := service.workflowSvc.IsTransitionAllowed(ctx, workflowName, oldStatus, deleteStatus)

		if transitionErr != nil {
			return nil, fmt.Errorf("checking transition: %w", transitionErr)
		}

		if !allowed {
			return nil, fmt.Errorf("transition %q → %q not allowed: %w", oldStatus, deleteStatus, domain.ErrInvalidTransition)
		}
	}

	task.Status = deleteStatus
	task.ModifiedAt = time.Now().UTC().Truncate(time.Millisecond)

	actor := ActorFromContext(ctx)
	var result *domain.Task
	txErr := bundle.WriteTx.WithTx(ctx, func(tx WriteTx) error {
		updated, updatedErr := service.applyValidatedUpdate(ctx, tx, task, oldStatus)

		if updatedErr != nil {
			return updatedErr
		}

		result = updated
		if !statusChanged {
			return nil
		}
		return tx.Events().Record(ctx, domain.NewTaskDeletedEvent(updated, oldStatus, actor))
	})

	if txErr != nil {
		return nil, txErr
	}

	return result, nil
}

// Available returns unclaimed, actionable, unblocked tasks sorted by
// urgency.
func (service *TaskService) Available(ctx context.Context, filter domain.FilterExpr) ([]*domain.Task, error) {
	nonTerminal, nonTerminalErr := service.collectNonTerminalStatuses(ctx)

	if nonTerminalErr != nil {
		return nil, nonTerminalErr
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

	defaultID, defaultErr := service.defaultProjectID(ctx)

	if defaultErr != nil {
		return nil, defaultErr
	}

	bundle, bundleErr := service.resolve(ctx, defaultID)

	if bundleErr != nil {
		return nil, bundleErr
	}

	return service.availableInBundle(ctx, bundle, baseFilter)
}

// availableInBundle runs the availability filter against a single
// bundle, removing blocked and waiting tasks.
func (service *TaskService) availableInBundle(ctx context.Context, bundle *RepoBundle, filter domain.FilterExpr) ([]*domain.Task, error) {
	tasks, tasksErr := service.listInBundle(ctx, bundle, filter)

	if tasksErr != nil {
		return nil, tasksErr
	}

	if len(tasks) == 0 {
		return tasks, nil
	}

	taskIDs := make([]uuid.UUID, len(tasks))
	for index, task := range tasks {
		taskIDs[index] = task.ID
	}

	blockedCounts, blockedErr := bundle.Relations.CountBlockedByIncompleteTasks(ctx, taskIDs)

	if blockedErr != nil {
		return nil, fmt.Errorf("checking blocked status: %w", blockedErr)
	}

	now := time.Now()
	result := make([]*domain.Task, 0, len(tasks))
	for _, task := range tasks {
		if blockedCounts[task.ID] > 0 {
			continue
		}
		if task.WaitUntil != nil && task.WaitUntil.After(now) {
			continue
		}
		result = append(result, task)
	}
	return result, nil
}

// Pop claims and starts the highest-urgency available task for the
// given player atomically, emitting exactly one task_popped event.
// Retries on claim-conflict and optimistic-lock errors. Returns
// domain.ErrNoAvailableTasks if nothing can be claimed.
func (service *TaskService) Pop(ctx context.Context, playerID string, filter domain.FilterExpr) (*domain.Task, error) {
	available, availableErr := service.Available(ctx, filter)

	if availableErr != nil {
		return nil, availableErr
	}

	if len(available) == 0 {
		return nil, domain.ErrNoAvailableTasks
	}

	players, playersErr := service.playerRepo(ctx)

	if playersErr != nil {
		return nil, playersErr
	}

	if players == nil {
		return nil, fmt.Errorf("player support not configured")
	}

	if _, playerGetErr := players.GetByID(ctx, playerID); playerGetErr != nil {
		return nil, fmt.Errorf("player %q: %w", playerID, playerGetErr)
	}

	actor := ActorFromContext(ctx)
	for _, candidate := range available {
		bundle, task, bundleErr := service.bundleForShortID(ctx, candidate.ShortID)

		if bundleErr != nil {
			if errors.Is(bundleErr, domain.ErrNotFound) {
				continue
			}
			return nil, bundleErr
		}

		project, projectErr := service.projectRepo.GetByID(ctx, task.ProjectID)

		if projectErr != nil {
			return nil, fmt.Errorf("loading project: %w", projectErr)
		}

		workflowName, workflowErr := service.workflowName(ctx, project)

		if workflowErr != nil {
			return nil, workflowErr
		}

		startStatus, startErr := service.workflowSvc.GetStatusByRole(ctx, workflowName, domain.RoleStart)

		if startErr != nil {
			return nil, fmt.Errorf("resolving start status: %w", startErr)
		}

		if task.Status != startStatus {
			allowed, transitionErr := service.workflowSvc.IsTransitionAllowed(ctx, workflowName, task.Status, startStatus)

			if transitionErr != nil {
				return nil, fmt.Errorf("checking transition: %w", transitionErr)
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
		txErr := bundle.WriteTx.WithTx(ctx, func(tx WriteTx) error {
			updated, updatedErr := service.applyValidatedUpdate(ctx, tx, task, oldStatus)

			if updatedErr != nil {
				return updatedErr
			}

			result = updated
			return tx.Events().Record(ctx, domain.NewTaskPoppedEvent(updated, playerID, oldStatus, actor))
		})

		if txErr != nil {
			if errors.Is(txErr, domain.ErrConflict) || errors.Is(txErr, domain.ErrTaskClaimed) {
				continue
			}
			return nil, txErr
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
func (service *TaskService) applyValidatedUpdate(
	ctx context.Context,
	tx WriteTx,
	task *domain.Task,
	oldStatus string,
) (*domain.Task, error) {
	tr := tx.Tasks()
	if updateErr := tr.Update(ctx, task); updateErr != nil {
		return nil, updateErr
	}
	updated, updatedErr := tr.GetByID(ctx, task.ID)

	if updatedErr != nil {
		return nil, updatedErr
	}

	if updated.Status != oldStatus {
		if autoCompleteErr := service.checkAutoComplete(ctx, updated, tx); autoCompleteErr != nil {
			return nil, autoCompleteErr
		}
		if autoRevertErr := service.checkAutoRevert(ctx, updated, oldStatus, tx); autoRevertErr != nil {
			return nil, autoRevertErr
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
func snapshotTask(task *domain.Task) taskSnapshot {
	uda := make(map[string]any, len(task.UDA))
	for kk, vv := range task.UDA {
		uda[kk] = vv
	}
	return taskSnapshot{
		Title:          task.Title,
		Description:    task.Description,
		Level:          task.Level,
		Priority:       task.Priority,
		Order:          task.Order,
		ParentID:       task.ParentID,
		ProjectID:      task.ProjectID,
		DueAt:          task.DueAt,
		WaitUntil:      task.WaitUntil,
		RecurrenceRule: task.RecurrenceRule,
		ClaimedBy:      task.ClaimedBy,
		ClaimedAt:      task.ClaimedAt,
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

func uuidPtrEqual(aa, bb *uuid.UUID) bool {
	if aa == nil || bb == nil {
		return aa == bb
	}
	return *aa == *bb
}

func uuidPtrValue(pp *uuid.UUID) any {
	if pp == nil {
		return nil
	}
	return pp.String()
}

func stringPtrEqual(aa, bb *string) bool {
	if aa == nil || bb == nil {
		return aa == bb
	}
	return *aa == *bb
}

func float64PtrEqual(aa, bb *float64) bool {
	if aa == nil || bb == nil {
		return aa == bb
	}
	return *aa == *bb
}

func stringPtrValue(pp *string) any {
	if pp == nil {
		return nil
	}
	return *pp
}

func timePtrEqual(aa, bb *time.Time) bool {
	if aa == nil || bb == nil {
		return aa == bb
	}
	return aa.Equal(*bb)
}

func timePtrValue(pp *time.Time) any {
	if pp == nil {
		return nil
	}
	return *pp
}

// validateTaxonomy loads the effective taxonomy for task.ProjectID and runs
// TaxonomyValidator against task. Returns nil when the projectSvc is not
// wired or the taxonomy is empty (levels disabled). When the task has a
// parent, parent.Level is loaded from bundle.Tasks and passed to the
// validator so rank-compatibility can be checked.
func (service *TaskService) validateTaxonomy(ctx context.Context, bundle *RepoBundle, task *domain.Task) error {
	if service.projectSvc == nil {
		return nil
	}
	project, projectErr := service.projectRepo.GetByID(ctx, task.ProjectID)

	if projectErr != nil {
		return projectErr
	}

	tx, _ := service.projectSvc.EffectiveTaxonomy(project)
	if tx.IsEmpty() {
		return nil
	}

	var parentLevel *string
	if task.ParentID != nil {
		parent, parentErr := bundle.Tasks.GetByID(ctx, *task.ParentID)

		if parentErr != nil {
			return parentErr
		}

		var level string
		if parent.Level != nil {
			level = *parent.Level
		}
		parentLevel = &level
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
func (service *TaskService) detectParentCycle(ctx context.Context, tasks repository.TaskRepository, taskID, proposedParentID uuid.UUID) error {
	current := proposedParentID
	for depth := 0; depth < maxParentDepth; depth++ {
		if current == taskID {
			return domain.ErrCyclicParent
		}
		parent, parentErr := tasks.GetByID(ctx, current)

		if parentErr != nil {
			return fmt.Errorf("checking parent cycle: %w", parentErr)
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
func (service *TaskService) generateShortID(ctx context.Context, id uuid.UUID) (string, error) {
	hex := strings.ReplaceAll(id.String(), "-", "")
	for length := 8; length <= len(hex); length++ {
		candidate := hex[:length]
		_, _, shortErr := service.bundleForShortID(ctx, candidate)
		if errors.Is(shortErr, domain.ErrNotFound) {
			return candidate, nil
		}
		if shortErr != nil {
			return "", shortErr
		}
	}
	return "", fmt.Errorf("could not generate unique short ID")
}

// Annotate adds a timestamped note to a task.
func (service *TaskService) Annotate(ctx context.Context, taskShortID string, body string) (*domain.Annotation, error) {
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("annotation body must not be empty")
	}

	bundle, task, bundleErr := service.bundleForShortID(ctx, taskShortID)

	if bundleErr != nil {
		return nil, bundleErr
	}

	annotation := &domain.Annotation{
		ID:        uuid.New(),
		TaskID:    task.ID,
		Body:      body,
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}

	if createErr := bundle.Annotations.Create(ctx, annotation); createErr != nil {
		return nil, createErr
	}

	return annotation, nil
}

// GetAnnotations returns all annotations for a task, identified by
// short ID.
func (service *TaskService) GetAnnotations(ctx context.Context, taskShortID string) ([]*domain.Annotation, error) {
	bundle, task, err := service.bundleForShortID(ctx, taskShortID)

	if err != nil {
		return nil, err
	}

	return bundle.Annotations.GetByTask(ctx, task.ID)
}

// GetAnnotationsBatch returns annotations for multiple tasks belonging to a
// single project in one query, keyed by task ID. Used by tree exporters that
// already know the project bundle and want to avoid the per-task fan-out of
// GetAnnotations. Tasks with no annotations are absent from the map.
func (service *TaskService) GetAnnotationsBatch(ctx context.Context, projectID uuid.UUID, taskIDs []uuid.UUID) (map[uuid.UUID][]*domain.Annotation, error) {
	bundle, err := service.resolve(ctx, projectID)

	if err != nil {
		return nil, fmt.Errorf("resolving bundle for project %v: %w", projectID, err)
	}

	return bundle.Annotations.GetByTasks(ctx, taskIDs)
}

// DeleteAnnotation removes an annotation by its ID. Fan-out: every
// project store is asked to delete; returns domain.ErrNotFound if no
// store held the row.
func (service *TaskService) DeleteAnnotation(ctx context.Context, annotationID uuid.UUID) error {
	ids, idsErr := service.projects(ctx)

	if idsErr != nil {
		return idsErr
	}

	deleted := false
	for _, pid := range ids {
		bundle, bundleErr := service.resolve(ctx, pid)

		if bundleErr != nil {
			return bundleErr
		}

		deleteErr := bundle.Annotations.Delete(ctx, annotationID)
		if deleteErr == nil {
			deleted = true
			continue
		}
		if !errors.Is(deleteErr, domain.ErrNotFound) {
			return deleteErr
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
func (service *TaskService) checkAutoComplete(
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

		parent, parentErr := tr.GetByID(ctx, *current.ParentID)

		if parentErr != nil {
			return fmt.Errorf("loading parent for propagation: %w", parentErr)
		}

		project, projectErr := service.projectRepo.GetByID(ctx, parent.ProjectID)

		if projectErr != nil {
			return fmt.Errorf("loading project for propagation: %w", projectErr)
		}

		cfg := project.Settings.AutoCompleteParent
		if cfg == nil {
			return nil
		}

		if current.Status != cfg.TriggerStatus {
			return nil
		}

		workflowName, workflowErr := service.workflowName(ctx, project)

		if workflowErr != nil {
			return workflowErr
		}

		children, childrenErr := tr.GetChildren(ctx, parent.ID)

		if childrenErr != nil {
			return fmt.Errorf("loading siblings for propagation: %w", childrenErr)
		}

		deleteStatus, deleteErr := service.workflowSvc.GetDeleteStatus(ctx, workflowName)

		if deleteErr != nil {
			return fmt.Errorf("resolving delete status for propagation: %w", deleteErr)
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

		allowed, transitionErr := service.workflowSvc.IsTransitionAllowed(ctx, workflowName, parent.Status, cfg.TargetStatus)

		if transitionErr != nil {
			return fmt.Errorf("checking propagation transition: %w", transitionErr)
		}

		if !allowed {
			return nil
		}

		prevParentStatus := parent.Status
		parent.Status = cfg.TargetStatus
		parent.ModifiedAt = time.Now().UTC().Truncate(time.Millisecond)
		if updateErr := tr.Update(ctx, parent); updateErr != nil {
			return fmt.Errorf("auto-completing parent: %w", updateErr)
		}

		reread, rereadErr := tr.GetByID(ctx, parent.ID)

		if rereadErr != nil {
			return fmt.Errorf("re-reading parent after propagation: %w", rereadErr)
		}

		current = reread
		roles, rolesErr := service.workflowSvc.GetStatusRoles(ctx, workflowName, current.Status)

		if rolesErr != nil {
			return fmt.Errorf("loading roles for auto-complete status: %w", rolesErr)
		}

		event := domain.NewStatusChangedEvent(current, prevParentStatus, current.Status, roles, "auto_complete", actor)
		if eventErr := tx.Events().Record(ctx, event); eventErr != nil {
			return fmt.Errorf("recording auto_complete event: %w", eventErr)
		}
	}
	return fmt.Errorf("auto-complete propagation exceeded maximum depth (%d)", maxParentDepth)
}

// checkAutoRevert checks whether a task moving away from the trigger
// status should revert its parent. Each cascaded parent transition emits a
// status_changed event with source="auto_revert".
func (service *TaskService) checkAutoRevert(
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

		parent, parentErr := tr.GetByID(ctx, *current.ParentID)

		if parentErr != nil {
			return fmt.Errorf("loading parent for revert: %w", parentErr)
		}

		project, projectErr := service.projectRepo.GetByID(ctx, parent.ProjectID)

		if projectErr != nil {
			return fmt.Errorf("loading project for revert: %w", projectErr)
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

		workflowName, workflowErr := service.workflowName(ctx, project)

		if workflowErr != nil {
			return workflowErr
		}

		allowed, transitionErr := service.workflowSvc.IsTransitionAllowed(ctx, workflowName, parent.Status, revertCfg.TargetStatus)

		if transitionErr != nil {
			return fmt.Errorf("checking revert transition: %w", transitionErr)
		}

		if !allowed {
			return nil
		}

		prevParentStatus := parent.Status
		parent.Status = revertCfg.TargetStatus
		parent.ModifiedAt = time.Now().UTC().Truncate(time.Millisecond)
		if updateErr := tr.Update(ctx, parent); updateErr != nil {
			return fmt.Errorf("reverting parent: %w", updateErr)
		}

		reread, rereadErr := tr.GetByID(ctx, parent.ID)

		if rereadErr != nil {
			return fmt.Errorf("re-reading parent after revert: %w", rereadErr)
		}

		current = reread
		roles, rolesErr := service.workflowSvc.GetStatusRoles(ctx, workflowName, current.Status)

		if rolesErr != nil {
			return fmt.Errorf("loading roles for auto-revert status: %w", rolesErr)
		}

		event := domain.NewStatusChangedEvent(current, prevParentStatus, current.Status, roles, "auto_revert", actor)
		if eventErr := tx.Events().Record(ctx, event); eventErr != nil {
			return fmt.Errorf("recording auto_revert event: %w", eventErr)
		}

		currentOldStatus = prevParentStatus
	}
	return fmt.Errorf("auto-revert propagation exceeded maximum depth (%d)", maxParentDepth)
}

// ptr is a generic helper that returns a pointer to the given value.
func ptr[T any](vv T) *T {
	return &vv
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
func (service *TaskService) LevelCheck(ctx context.Context, filter domain.FilterExpr) ([]LevelViolation, error) {
	if service.projectSvc == nil {
		return nil, nil
	}

	projectIDs, projectIDsErr := service.projects(ctx)

	if projectIDsErr != nil {
		return nil, fmt.Errorf("listing projects: %w", projectIDsErr)
	}

	seenBundles := make(map[*RepoBundle]struct{})
	var bundles []*RepoBundle
	for _, pid := range projectIDs {
		bundle, bundleErr := service.resolve(ctx, pid)

		if bundleErr != nil {
			return nil, fmt.Errorf("resolving bundle for project %v: %w", pid, bundleErr)
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
		if projectCtx, ok := projectCache[pid]; ok {
			return projectCtx, nil
		}
		proj, projErr := service.projectRepo.GetByID(ctx, pid)

		if projErr != nil {
			return nil, fmt.Errorf("loading project %v: %w", pid, projErr)
		}

		tx, src := service.projectSvc.EffectiveTaxonomy(proj)
		projectCtx := &projectCtx{project: proj, taxonomy: tx, source: src}
		projectCache[pid] = projectCtx
		return projectCtx, nil
	}

	var violations []LevelViolation
	for _, bundle := range bundles {
		tasks, tasksErr := bundle.Tasks.List(ctx, filter)

		if tasksErr != nil {
			return nil, fmt.Errorf("listing tasks for level-check: %w", tasksErr)
		}

		for _, task := range tasks {
			projectCtx, projectCtxErr := resolveProject(task.ProjectID)

			if projectCtxErr != nil {
				return nil, projectCtxErr
			}

			if projectCtx.taxonomy.IsEmpty() {
				continue
			}

			var parentLevel *string
			if task.ParentID != nil {
				parent, parentErr := bundle.Tasks.GetByID(ctx, *task.ParentID)

				if parentErr != nil {
					if errors.Is(parentErr, domain.ErrNotFound) {
						continue
					}
					return nil, fmt.Errorf("loading parent %v: %w", *task.ParentID, parentErr)
				}

				var level string
				if parent.Level != nil {
					level = *parent.Level
				}
				parentLevel = &level
			}

			checkErr := domain.TaxonomyValidator{}.Check(
				domain.ValidationContext{Taxonomy: projectCtx.taxonomy, ParentLevel: parentLevel},
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
				Taxonomy: projectCtx.taxonomy,
				Source:   projectCtx.source,
				Err:      te,
			})
		}
	}

	sort.SliceStable(violations, func(ii, jj int) bool {
		nameI := projectCache[violations[ii].Task.ProjectID].project.Name
		nameJ := projectCache[violations[jj].Task.ProjectID].project.Name
		if nameI != nameJ {
			return nameI < nameJ
		}
		return violations[ii].Task.ShortID < violations[jj].Task.ShortID
	})

	return violations, nil
}
