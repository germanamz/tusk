package mcp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/filter"
	"github.com/germanamz/tusk/inmem"
	"github.com/germanamz/tusk/service"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Server wraps an MCP server that exposes tusk capabilities as tools and resources.
type Server struct {
	taskSvc        *service.TaskService
	tagSvc         *service.TagService
	relationSvc    *service.RelationService
	projectSvc     *service.ProjectService
	workflowSvc    *service.WorkflowService
	playerSvc      *service.PlayerService
	workflowRepo   *inmem.WorkflowRepository
	projectRepo    *inmem.ProjectRepository
	urgencyEngine  *service.UrgencyEngine
	server         *server.MCPServer
	cfg            config.MCPConfig
	loadOpts       []config.Option
	configMu       sync.Mutex        // serializes config read-modify-write + reload
	toolGroups     map[string]string // tool name → group
	resourceGroups map[string]string // resource URI template → group
}

// New creates a new MCP Server and registers all tools and resources.
// Returns an error if the config contains unknown disable list entries.
func New(
	taskSvc *service.TaskService,
	tagSvc *service.TagService,
	relationSvc *service.RelationService,
	projectSvc *service.ProjectService,
	workflowSvc *service.WorkflowService,
	playerSvc *service.PlayerService,
	workflowRepo *inmem.WorkflowRepository,
	projectRepo *inmem.ProjectRepository,
	urgencyEngine *service.UrgencyEngine,
	version string,
	cfg config.MCPConfig,
	loadOpts []config.Option,
) (*Server, error) {
	s := &Server{
		taskSvc:        taskSvc,
		tagSvc:         tagSvc,
		relationSvc:    relationSvc,
		projectSvc:     projectSvc,
		workflowSvc:    workflowSvc,
		playerSvc:      playerSvc,
		workflowRepo:   workflowRepo,
		projectRepo:    projectRepo,
		urgencyEngine:  urgencyEngine,
		cfg:            cfg,
		loadOpts:       loadOpts,
		toolGroups:     make(map[string]string),
		resourceGroups: make(map[string]string),
	}

	s.server = server.NewMCPServer(
		"tusk",
		version,
		server.WithToolCapabilities(false),
		server.WithResourceCapabilities(false, false),
		server.WithRecovery(),
		server.WithInstructions(serverInstructions),
	)

	s.registerTools()
	s.registerResources()

	if err := s.validateConfig(); err != nil {
		return nil, err
	}

	return s, nil
}

// isToolEnabled returns true if the tool should be registered based on config.
func (s *Server) isToolEnabled(name, group string) bool {
	return !containsStr(s.cfg.DisabledTools, name) &&
		!containsStr(s.cfg.DisabledToolGroups, group)
}

// isResourceEnabled returns true if the resource should be registered based on config.
func (s *Server) isResourceEnabled(uriTemplate, group string) bool {
	return !containsStr(s.cfg.DisabledResources, uriTemplate) &&
		!containsStr(s.cfg.DisabledResourceGroups, group)
}

// containsStr returns true if slice contains the given string.
func containsStr(slice []string, s string) bool {
	return slices.Contains(slice, s)
}

// validateConfig returns an error if any disable list entry does not match
// a known tool, resource, or group.
func (s *Server) validateConfig() error {
	validToolNames := map[string]bool{
		"tusk_task_create":     true,
		"tusk_task_get":        true,
		"tusk_task_list":       true,
		"tusk_task_modify":     true,
		"tusk_task_start":      true,
		"tusk_task_done":       true,
		"tusk_task_delete":     true,
		"tusk_task_annotate":   true,
		"tusk_task_tree":       true,
		"tusk_task_next":       true,
		"tusk_relation_add":    true,
		"tusk_relation_remove": true,
		"tusk_project_list":    true,
		"tusk_workflow_list":   true,
		"tusk_player_register": true,
		"tusk_task_claim":      true,
		"tusk_task_release":    true,
		"tusk_task_available":  true,
		"tusk_task_pop":        true,
		"tusk_config_show":     true,
		"tusk_config_set":      true,
		"tusk_workflow_create": true,
		"tusk_workflow_modify": true,
		"tusk_workflow_delete": true,
	}
	validToolGroups := map[string]bool{
		"task": true, "relation": true, "project": true, "workflow": true, "player": true, "config": true,
	}
	validResourceURIs := map[string]bool{
		"tusk://tasks/{short_id}":         true,
		"tusk://projects/{name}":          true,
		"tusk://projects/{name}/workflow": true,
	}
	validResourceGroups := map[string]bool{
		"task": true, "project": true, "workflow": true,
	}

	var errs []error
	for _, name := range s.cfg.DisabledTools {
		if !validToolNames[name] {
			errs = append(errs, fmt.Errorf("disabled_tools: unknown tool %q", name))
		}
	}
	for _, group := range s.cfg.DisabledToolGroups {
		if !validToolGroups[group] {
			errs = append(errs, fmt.Errorf("disabled_tool_groups: unknown group %q", group))
		}
	}
	for _, uri := range s.cfg.DisabledResources {
		if !validResourceURIs[uri] {
			errs = append(errs, fmt.Errorf("disabled_resources: unknown resource %q", uri))
		}
	}
	for _, group := range s.cfg.DisabledResourceGroups {
		if !validResourceGroups[group] {
			errs = append(errs, fmt.Errorf("disabled_resource_groups: unknown group %q", group))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid mcp config: %w", errors.Join(errs...))
	}
	return nil
}

// addTool registers a tool with the MCP server if it's enabled by config.
// It also records the tool's group in the toolGroups map.
func (s *Server) addTool(group string, tool mcp.Tool, handler server.ToolHandlerFunc) {
	name := tool.Name
	if !s.isToolEnabled(name, group) {
		return
	}
	s.toolGroups[name] = group
	s.server.AddTool(tool, handler)
}

// addResource registers a resource template if it's enabled by config.
func (s *Server) addResource(group string, tmpl mcp.ResourceTemplate, handler server.ResourceTemplateHandlerFunc) {
	uri := tmpl.URITemplate.Raw()
	if !s.isResourceEnabled(uri, group) {
		return
	}
	s.resourceGroups[uri] = group
	s.server.AddResourceTemplate(tmpl, handler)
}

// registerTools registers all MCP tool definitions and their handlers.
func (s *Server) registerTools() {
	s.addTool("task",
		mcp.NewTool("tusk_task_create",
			mcp.WithDescription("Create a new task"),
			mcp.WithString("title",
				mcp.Required(),
				mcp.Description("Task title"),
			),
			mcp.WithString("description",
				mcp.Description("Task description"),
			),
			mcp.WithNumber("priority",
				mcp.Description("Priority level: 0=none, 1=low, 2=medium, 3=high, 4=urgent"),
			),
			mcp.WithString("project",
				mcp.Description("Project name (uses default project if omitted)"),
			),
			mcp.WithString("parent",
				mcp.Description("Parent task short_id for creating subtasks"),
			),
			mcp.WithArray("tags",
				mcp.Description("Tags to assign to the task"),
				mcp.WithStringItems(),
			),
			mcp.WithString("due",
				mcp.Description("Due date in ISO 8601 / RFC3339 format"),
			),
			mcp.WithString("wait_until",
				mcp.Description("Hide task until this ISO 8601 / RFC3339 date"),
			),
			mcp.WithObject("uda",
				mcp.Description("User-defined attributes as key-value pairs (all values must be strings)"),
				mcp.AdditionalProperties(map[string]any{"type": "string"}),
			),
		),
		s.handleTaskCreate,
	)

	s.addTool("task",
		mcp.NewTool("tusk_task_get",
			mcp.WithDescription("Get a task with full details including tags, relations, and annotations"),
			mcp.WithString("short_id",
				mcp.Required(),
				mcp.Description("Task short ID (8+ hex characters)"),
			),
			mcp.WithString("player_id",
				mcp.Description("Player ID — updates last_seen_at if provided (no auto-register)"),
			),
		),
		s.handleTaskGet,
	)

	s.addTool("task",
		mcp.NewTool("tusk_task_list",
			mcp.WithDescription("List tasks with optional filters"),
			mcp.WithString("filter",
				mcp.Description("Filter expression with AND/OR/NOT/parentheses support (e.g. 'status=active OR +urgent'). When provided, other filter parameters are ignored."),
			),
			mcp.WithArray("status",
				mcp.Description("Filter by status (e.g. [\"pending\", \"active\"])"),
				mcp.WithStringItems(),
			),
			mcp.WithNumber("priority_min",
				mcp.Description("Minimum priority (0-4)"),
			),
			mcp.WithNumber("priority_max",
				mcp.Description("Maximum priority (0-4)"),
			),
			mcp.WithString("project",
				mcp.Description("Filter by project name"),
			),
			mcp.WithArray("tags",
				mcp.Description("Include tasks with these tags"),
				mcp.WithStringItems(),
			),
			mcp.WithArray("exclude_tags",
				mcp.Description("Exclude tasks with these tags"),
				mcp.WithStringItems(),
			),
			mcp.WithString("due_after",
				mcp.Description("Tasks due after this ISO 8601 date"),
			),
			mcp.WithString("due_before",
				mcp.Description("Tasks due before this ISO 8601 date"),
			),
			mcp.WithString("parent",
				mcp.Description("List direct children of this task (short_id)"),
			),
			mcp.WithString("root",
				mcp.Description("List all descendants of this task (short_id)"),
			),
			mcp.WithString("title",
				mcp.Description("Filter tasks whose title contains this substring (case-insensitive)"),
			),
			mcp.WithString("description",
				mcp.Description("Filter tasks whose description contains this substring (case-insensitive)"),
			),
			mcp.WithString("player_id",
				mcp.Description("Player ID — updates last_seen_at if provided (no auto-register)"),
			),
		),
		s.handleTaskList,
	)

	s.addTool("task",
		mcp.NewTool("tusk_task_modify",
			mcp.WithDescription("Modify task fields. Requires version for optimistic locking."),
			mcp.WithString("short_id",
				mcp.Required(),
				mcp.Description("Task short ID"),
			),
			mcp.WithNumber("version",
				mcp.Required(),
				mcp.Description("Current task version (for optimistic locking)"),
			),
			mcp.WithString("title",
				mcp.Description("New title"),
			),
			mcp.WithString("description",
				mcp.Description("New description"),
			),
			mcp.WithNumber("priority",
				mcp.Description("New priority (0-4)"),
			),
			mcp.WithString("project",
				mcp.Description("Move to project (by name)"),
			),
			mcp.WithString("parent",
				mcp.Description("Set parent task (short_id). Empty string clears parent."),
			),
			mcp.WithString("due",
				mcp.Description("Due date (ISO 8601). Empty string clears."),
			),
			mcp.WithString("wait_until",
				mcp.Description("Wait until date (ISO 8601). Empty string clears."),
			),
			mcp.WithObject("uda",
				mcp.Description("UDA key-value pairs to merge. Empty string value removes the key."),
				mcp.AdditionalProperties(map[string]any{"type": "string"}),
			),
			mcp.WithArray("add_tags",
				mcp.Description("Tags to add"),
				mcp.WithStringItems(),
			),
			mcp.WithArray("remove_tags",
				mcp.Description("Tags to remove"),
				mcp.WithStringItems(),
			),
		),
		s.handleTaskModify,
	)

	s.addTool("task",
		mcp.NewTool("tusk_task_start",
			mcp.WithDescription("Transition a task to active status. If player_id is provided, auto-claims the task."),
			mcp.WithString("short_id",
				mcp.Required(),
				mcp.Description("Task short ID"),
			),
			mcp.WithNumber("version",
				mcp.Required(),
				mcp.Description("Current task version (for optimistic locking)"),
			),
			mcp.WithString("player_id",
				mcp.Description("Player ID — auto-registers as agent, auto-claims the task"),
			),
		),
		s.handleTaskStart,
	)

	s.addTool("task",
		mcp.NewTool("tusk_task_done",
			mcp.WithDescription("Transition a task to completed status"),
			mcp.WithString("short_id",
				mcp.Required(),
				mcp.Description("Task short ID"),
			),
			mcp.WithNumber("version",
				mcp.Required(),
				mcp.Description("Current task version (for optimistic locking)"),
			),
		),
		s.handleTaskDone,
	)

	s.addTool("task",
		mcp.NewTool("tusk_task_delete",
			mcp.WithDescription("Soft-delete a task (transitions to deleted status)"),
			mcp.WithString("short_id",
				mcp.Required(),
				mcp.Description("Task short ID"),
			),
			mcp.WithNumber("version",
				mcp.Required(),
				mcp.Description("Current task version (for optimistic locking)"),
			),
		),
		s.handleTaskDelete,
	)

	s.addTool("task",
		mcp.NewTool("tusk_task_annotate",
			mcp.WithDescription("Add an annotation (note) to a task"),
			mcp.WithString("short_id",
				mcp.Required(),
				mcp.Description("Task short ID"),
			),
			mcp.WithString("body",
				mcp.Required(),
				mcp.Description("Annotation text"),
			),
		),
		s.handleTaskAnnotate,
	)

	s.addTool("relation",
		mcp.NewTool("tusk_relation_add",
			mcp.WithDescription("Create a typed relation between two tasks"),
			mcp.WithString("source",
				mcp.Required(),
				mcp.Description("Source task short_id"),
			),
			mcp.WithString("target",
				mcp.Required(),
				mcp.Description("Target task short_id"),
			),
			mcp.WithString("type",
				mcp.Required(),
				mcp.Description("Relation type"),
				mcp.Enum("blocks", "relates_to", "duplicates"),
			),
		),
		s.handleRelationAdd,
	)

	s.addTool("relation",
		mcp.NewTool("tusk_relation_remove",
			mcp.WithDescription("Remove a relation between two tasks"),
			mcp.WithString("source",
				mcp.Required(),
				mcp.Description("Source task short_id"),
			),
			mcp.WithString("target",
				mcp.Required(),
				mcp.Description("Target task short_id"),
			),
			mcp.WithString("type",
				mcp.Required(),
				mcp.Description("Relation type"),
				mcp.Enum("blocks", "relates_to", "duplicates"),
			),
		),
		s.handleRelationRemove,
	)

	s.addTool("project",
		mcp.NewTool("tusk_project_list",
			mcp.WithDescription("List all projects"),
		),
		s.handleProjectList,
	)

	s.addTool("task",
		mcp.NewTool("tusk_task_tree",
			mcp.WithDescription("Get tasks as a nested tree hierarchy"),
			mcp.WithString("short_id",
				mcp.Description("Root task short_id (omit for full tree)"),
			),
			mcp.WithBoolean("include_deleted",
				mcp.Description("Include deleted tasks in the tree"),
			),
			mcp.WithString("player_id",
				mcp.Description("Player ID — updates last_seen_at if provided (no auto-register)"),
			),
		),
		s.handleTaskTree,
	)

	s.addTool("task", mcp.NewTool(
		"tusk_task_next",
		mcp.WithDescription("Get the highest-urgency actionable task (not waiting, not blocked)"),
	), s.handleTaskNext)

	s.addTool("workflow",
		mcp.NewTool("tusk_workflow_list",
			mcp.WithDescription("List all workflows with their statuses, transitions, and referencing projects"),
		),
		s.handleWorkflowList,
	)

	s.addTool("workflow",
		mcp.NewTool("tusk_workflow_create",
			mcp.WithDescription("Create a new workflow and write it to the active config file. Fails if the name already exists."),
			mcp.WithString("name",
				mcp.Required(),
				mcp.Description("Workflow name (must be unique within the config file)"),
			),
			mcp.WithArray("statuses",
				mcp.Required(),
				mcp.Description("Ordered list of statuses. Each item is {name: string, roles: string[]}. Roles: initial, start, terminal, done, delete, highlight, dim."),
				mcp.Items(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":  map[string]any{"type": "string"},
						"roles": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					},
				}),
			),
			mcp.WithArray("transitions",
				mcp.Required(),
				mcp.Description("Allowed transitions. Each item is {from: string, to: string}."),
				mcp.Items(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"from": map[string]any{"type": "string"},
						"to":   map[string]any{"type": "string"},
					},
				}),
			),
		),
		s.handleWorkflowCreate,
	)

	s.addTool("workflow",
		mcp.NewTool("tusk_workflow_modify",
			mcp.WithDescription("Modify an existing workflow: add, remove, or update statuses and transitions."),
			mcp.WithString("name",
				mcp.Required(),
				mcp.Description("Workflow name"),
			),
			mcp.WithArray("add_statuses",
				mcp.Description("Statuses to add (must not already exist). Items: {name, roles[]}."),
				mcp.Items(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":  map[string]any{"type": "string"},
						"roles": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					},
				}),
			),
			mcp.WithArray("set_statuses",
				mcp.Description("Statuses to update in place (replaces roles). Items: {name, roles[]}."),
				mcp.Items(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":  map[string]any{"type": "string"},
						"roles": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					},
				}),
			),
			mcp.WithArray("remove_statuses",
				mcp.Description("Status names to remove. Any transitions touching these are removed too."),
				mcp.WithStringItems(),
			),
			mcp.WithArray("add_transitions",
				mcp.Description("Transitions to add. Items: {from, to}."),
				mcp.Items(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"from": map[string]any{"type": "string"},
						"to":   map[string]any{"type": "string"},
					},
				}),
			),
			mcp.WithArray("remove_transitions",
				mcp.Description("Transitions to remove. Items: {from, to}."),
				mcp.Items(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"from": map[string]any{"type": "string"},
						"to":   map[string]any{"type": "string"},
					},
				}),
			),
		),
		s.handleWorkflowModify,
	)

	s.addTool("workflow",
		mcp.NewTool("tusk_workflow_delete",
			mcp.WithDescription("Delete a workflow from the active config file. Fails if any project references it."),
			mcp.WithString("name",
				mcp.Required(),
				mcp.Description("Workflow name"),
			),
		),
		s.handleWorkflowDelete,
	)

	s.addTool("player",
		mcp.NewTool("tusk_player_register",
			mcp.WithDescription("Register a new player (agent). Player type is always 'agent' for MCP."),
			mcp.WithString("player_id",
				mcp.Required(),
				mcp.Description("Unique player identifier"),
			),
		),
		s.handlePlayerRegister,
	)

	s.addTool("task",
		mcp.NewTool("tusk_task_claim",
			mcp.WithDescription("Claim a task for a player. Returns error if already claimed by another player."),
			mcp.WithString("short_id",
				mcp.Required(),
				mcp.Description("Task short ID"),
			),
			mcp.WithString("player_id",
				mcp.Required(),
				mcp.Description("Player ID claiming the task"),
			),
			mcp.WithNumber("version",
				mcp.Required(),
				mcp.Description("Current task version (for optimistic locking)"),
			),
		),
		s.handleTaskClaim,
	)

	s.addTool("task",
		mcp.NewTool("tusk_task_release",
			mcp.WithDescription("Release a task claim. Only the current claimant can release."),
			mcp.WithString("short_id",
				mcp.Required(),
				mcp.Description("Task short ID"),
			),
			mcp.WithString("player_id",
				mcp.Required(),
				mcp.Description("Player ID releasing the claim"),
			),
			mcp.WithNumber("version",
				mcp.Required(),
				mcp.Description("Current task version (for optimistic locking)"),
			),
		),
		s.handleTaskRelease,
	)

	s.addTool("task",
		mcp.NewTool("tusk_task_available",
			mcp.WithDescription("List unclaimed, actionable, unblocked tasks sorted by urgency"),
			mcp.WithString("player_id",
				mcp.Required(),
				mcp.Description("Player ID — auto-registers as agent on first use"),
			),
			mcp.WithString("filter",
				mcp.Description("Boolean filter expression (e.g. 'project=backend AND +api')"),
			),
		),
		s.handleTaskAvailable,
	)

	s.addTool("task",
		mcp.NewTool("tusk_task_pop",
			mcp.WithDescription("Claim and start the highest-urgency available task for the given player"),
			mcp.WithString("player_id",
				mcp.Required(),
				mcp.Description("Player ID — auto-registers as agent on first use"),
			),
			mcp.WithString("filter",
				mcp.Description("Optional boolean filter to narrow candidates (e.g. 'project=backend')"),
			),
		),
		s.handleTaskPop,
	)

	s.addTool("config",
		mcp.NewTool("tusk_config_show",
			mcp.WithDescription("Return the effective Tusk configuration and the path of the active config file. Read-only."),
		),
		s.handleConfigShow,
	)

	s.addTool("config",
		mcp.NewTool("tusk_config_set",
			mcp.WithDescription("Set a scalar config value by dot-path key and hot-reload the server. Rejects storage.* keys. Changes to mcp.disabled_* take effect only after process restart."),
			mcp.WithString("key",
				mcp.Required(),
				mcp.Description("Dot-path key (e.g. urgency.due_weight, tui.color, mcp.disabled_tools)"),
			),
			mcp.WithString("value",
				mcp.Required(),
				mcp.Description("New value. For slice keys (e.g. mcp.disabled_tools), use a comma-separated list."),
			),
		),
		s.handleConfigSet,
	)
}

// newResolver builds a filter resolver seeded with the union of non-terminal
// statuses across all configured workflows. Falls back to ["pending","active"]
// if listing fails or no statuses are found.
func (s *Server) newResolver(ctx context.Context) *filter.Resolver {
	defaults := []string{"pending", "active"}
	workflows, err := s.workflowSvc.List(ctx)
	if err == nil {
		seen := make(map[string]bool)
		var collected []string
		for _, wf := range workflows {
			for _, name := range wf.NonTerminalStatuses() {
				if !seen[name] {
					seen[name] = true
					collected = append(collected, name)
				}
			}
		}
		if len(collected) > 0 {
			defaults = collected
		}
	}
	return filter.NewResolver(s.taskSvc, defaults)
}

// Serve starts the MCP server using stdio transport.
// This blocks until the transport is closed (e.g., stdin EOF).
func (s *Server) Serve() error {
	return server.ServeStdio(s.server)
}

// reloadConfig re-reads the active config file via the stored loadOpts and
// hot-reloads the workflow repository, project repository, and urgency engine
// with the fresh data. It does NOT rebuild the MCP server, reopen the
// database, or reconfigure transports — those require a process restart.
//
// Safe to call from any MCP tool handler after a successful config mutation.
// Returns an error when Load fails; callers should surface the error back to
// the caller without applying partial state (Load is a full parse with
// validation, so there is no partial state to apply).
//
// reloadConfig acquires the server-level config mutex for the duration of the
// reload so readers never observe mixed state across the three repos.
func (s *Server) reloadConfig(ctx context.Context) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	return s.reloadConfigLocked(ctx)
}

// reloadConfigLocked performs the actual hot-reload work and assumes the
// caller already holds s.configMu. Use this from code paths that have
// already acquired the lock (e.g. handleConfigSet's read-modify-write
// critical section) to avoid self-deadlock.
func (s *Server) reloadConfigLocked(ctx context.Context) error {
	cfg, err := config.Load(s.loadOpts...)
	if err != nil {
		return fmt.Errorf("reloading config: %w", err)
	}
	s.workflowRepo.Reload(cfg.Workflows)
	s.projectRepo.Reload(cfg.Projects)
	s.urgencyEngine.Reload(service.UrgencyWeights{
		Priority:    cfg.Urgency.PriorityWeight,
		Due:         cfg.Urgency.DueWeight,
		Age:         cfg.Urgency.AgeWeight,
		Active:      cfg.Urgency.ActiveWeight,
		Blocking:    cfg.Urgency.BlockingWeight,
		Blocked:     cfg.Urgency.BlockedWeight,
		Tags:        cfg.Urgency.TagsWeight,
		Project:     cfg.Urgency.ProjectWeight,
		Annotations: cfg.Urgency.AnnotationsWeight,
		Waiting:     cfg.Urgency.WaitingWeight,
	})
	_ = ctx // ctx reserved for future per-call tracing
	return nil
}

// ReloadConfigForTest exposes reloadConfig for internal tests.
func (s *Server) ReloadConfigForTest(ctx context.Context) error {
	return s.reloadConfig(ctx)
}

// WorkflowRepoForTest exposes the workflow repo handle for internal tests.
func (s *Server) WorkflowRepoForTest() *inmem.WorkflowRepository { return s.workflowRepo }

const serverInstructions = `Tusk is a task management system. You can create, list, modify, and transition tasks through workflow statuses. Tasks support parent-child hierarchy, typed relations (blocks, relates_to, duplicates), tags, annotations, and projects. All mutation tools require a version parameter for optimistic locking — fetch the task first to get the current version. You can also inspect the active configuration via tusk_config_show and modify scalar config values via tusk_config_set (storage.* keys are read-only over MCP). Workflows can be created, modified, and deleted via tusk_workflow_create, tusk_workflow_modify, and tusk_workflow_delete using structured JSON inputs (statuses and transitions as arrays of objects).`
