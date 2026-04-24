package mcp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/filter"
	"github.com/germanamz/tusk/repository"
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
	noteSvc        *service.NoteService
	workflowRepo   repository.WorkflowRepository
	projectRepo    repository.ProjectRepository
	urgencyEngine  *service.UrgencyEngine
	server         *server.MCPServer
	cfgMu          sync.RWMutex
	cfg            config.MCPConfig
	loadOpts       []config.Option
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
	noteSvc *service.NoteService,
	workflowRepo repository.WorkflowRepository,
	projectRepo repository.ProjectRepository,
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
		noteSvc:        noteSvc,
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
		"tusk_task_move":       true,
		"tusk_task_resequence": true,
		"tusk_task_start":      true,
		"tusk_task_done":       true,
		"tusk_task_delete":     true,
		"tusk_task_annotate":   true,
		"tusk_task_tree":       true,
		"tusk_task_next":       true,
		"tusk_task_link":       true,
		"tusk_task_unlink":     true,
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
		"tusk_project_create":  true,
		"tusk_project_modify":  true,
		"tusk_project_delete":  true,
		"tusk_note_add":        true,
		"tusk_note_list":       true,
		"tusk_note_archive":    true,
	}
	validToolGroups := map[string]bool{
		"task": true, "task_relations": true, "project": true, "workflow": true, "player": true, "config": true, "note": true,
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

	for toolName, fields := range s.cfg.BlockedFields {
		registry, known := toolFields[toolName]
		if !known {
			errs = append(errs, fmt.Errorf("blocked_fields: unknown tool %q", toolName))
			continue
		}
		for _, field := range fields {
			if strings.Contains(field, ".") {
				errs = append(errs, fmt.Errorf("blocked_fields: dotted sub-keys not yet supported (%q on tool %q)", field, toolName))
				continue
			}
			if _, ok := registry[field]; !ok {
				errs = append(errs, fmt.Errorf("blocked_fields: tool %q has no field %q", toolName, field))
			}
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
	// Keep internal/mcp/field_registry.go in sync when adding, renaming, or
	// removing input parameters on any tool below.
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
			mcp.WithString("level",
				mcp.Description("Task taxonomy level (e.g. milestone, story, task). Required when a taxonomy is configured."),
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
			mcp.WithString("level",
				mcp.Description("New task taxonomy level. Empty string clears the level."),
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
		mcp.NewTool("tusk_task_move",
			mcp.WithDescription("Reposition a task within the sibling-order plane. Use position=before|after with target_id for pairwise placement, or position=first|last (optionally with parent_id) for extreme placement. parent_id is tristate: omit to keep the current parent, pass JSON null to move to root, or pass a short_id/UUID to re-parent."),
			mcp.WithString("task_id",
				mcp.Required(),
				mcp.Description("Task short ID or UUID"),
			),
			mcp.WithString("position",
				mcp.Required(),
				mcp.Description("Where to place the task relative to target_id / parent_id"),
				mcp.Enum("before", "after", "first", "last"),
			),
			mcp.WithString("target_id",
				mcp.Description("Sibling short_id / UUID. Required for position=before|after, forbidden for position=first|last."),
			),
			mcp.WithString("parent_id",
				mcp.Description("New parent short_id / UUID. Only valid for position=first|last. Absent keeps the current parent; JSON null re-parents to root."),
			),
			mcp.WithNumber("version",
				mcp.Required(),
				mcp.Description("Current task version (optimistic locking)"),
			),
			mcp.WithString("player_id",
				mcp.Description("Player ID — auto-registers as agent on first use"),
			),
		),
		s.handleTaskMove,
	)

	s.addTool("task",
		mcp.NewTool("tusk_task_resequence",
			mcp.WithDescription("Rewrite every sibling under the given parent to dense integer orders (1.0, 2.0, ...), preserving the current sort. Pass parent_id=null to resequence the root sibling group."),
			mcp.WithString("parent_id",
				mcp.Description("Parent short_id / UUID. JSON null resequences root-level siblings."),
			),
			mcp.WithString("player_id",
				mcp.Description("Player ID — auto-registers as agent on first use"),
			),
		),
		s.handleTaskResequence,
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

	s.addTool("task_relations",
		mcp.NewTool("tusk_task_link",
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
		s.handleTaskLink,
	)

	s.addTool("task_relations",
		mcp.NewTool("tusk_task_unlink",
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
		s.handleTaskUnlink,
	)

	s.addTool("project",
		mcp.NewTool("tusk_project_list",
			mcp.WithDescription("List all projects"),
		),
		s.handleProjectList,
	)

	s.addTool("project",
		mcp.NewTool("tusk_project_create",
			mcp.WithDescription("Create a new project bound to a workflow."),
			mcp.WithString("name",
				mcp.Required(),
				mcp.Description("Project name (unique within the config file)"),
			),
			mcp.WithString("workflow",
				mcp.Required(),
				mcp.Description("Name of an existing workflow"),
			),
			mcp.WithObject("urgency",
				mcp.Description("Per-project urgency weight overrides (e.g. {\"due_weight\": 10.0}). Keys: priority_weight, due_weight, age_weight, active_weight, blocking_weight, blocked_weight, tags_weight, project_weight, annotations_weight, waiting_weight."),
				mcp.AdditionalProperties(map[string]any{"type": "number"}),
			),
			mcp.WithObject("auto_complete",
				mcp.Description("Parent auto-complete config: {trigger_status, target_status}"),
				mcp.AdditionalProperties(map[string]any{"type": "string"}),
			),
			mcp.WithObject("auto_revert",
				mcp.Description("Parent auto-revert config: {trigger_status, target_status}"),
				mcp.AdditionalProperties(map[string]any{"type": "string"}),
			),
		),
		s.handleProjectCreate,
	)

	s.addTool("project",
		mcp.NewTool("tusk_project_modify",
			mcp.WithDescription("Modify an existing project. Only fields that are present are changed. version is required for optimistic locking."),
			mcp.WithString("name",
				mcp.Required(),
				mcp.Description("Project name"),
			),
			mcp.WithNumber("version",
				mcp.Required(),
				mcp.Description("Expected version of the project (optimistic locking)"),
			),
			mcp.WithString("workflow",
				mcp.Description("New workflow to bind"),
			),
			mcp.WithObject("urgency_set",
				mcp.Description("Absolute per-project urgency overrides — keys as in tusk_project_create.urgency"),
				mcp.AdditionalProperties(map[string]any{"type": "number"}),
			),
			mcp.WithObject("urgency_delta",
				mcp.Description("Delta to apply on top of the effective weight (positive or negative). Cannot overlap with urgency_set keys."),
				mcp.AdditionalProperties(map[string]any{"type": "number"}),
			),
			mcp.WithObject("auto_complete",
				mcp.Description("Replace parent auto-complete config"),
				mcp.AdditionalProperties(map[string]any{"type": "string"}),
			),
			mcp.WithObject("auto_revert",
				mcp.Description("Replace parent auto-revert config"),
				mcp.AdditionalProperties(map[string]any{"type": "string"}),
			),
			mcp.WithObject("taxonomy",
				mcp.Description("Project taxonomy override (tristate). Omit the field to leave unchanged. Pass null to clear the override (inherit the workspace default). Pass {\"ranks\": []} to opt the project out of level enforcement. Pass {\"ranks\": [[...], ...]} to set a project-specific taxonomy."),
				mcp.Properties(map[string]any{
					"ranks": map[string]any{
						"type":        "array",
						"description": "Ordered rank groups; each element is a peer list of level names.",
						"items": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
					},
				}),
			),
		),
		s.handleProjectModify,
	)

	s.addTool("project",
		mcp.NewTool("tusk_project_delete",
			mcp.WithDescription("Delete a project. Rejects the built-in 'default' project and any project with task references unless force=true. Under force=true, referencing tasks are reassigned to the built-in 'default' project and then the row is removed in one transaction."),
			mcp.WithString("name",
				mcp.Required(),
				mcp.Description("Project name"),
			),
			mcp.WithNumber("version",
				mcp.Required(),
				mcp.Description("Expected version of the project (optimistic locking)"),
			),
			mcp.WithBoolean("force",
				mcp.Description("Bypass the built-in-default and task-reference guards"),
			),
		),
		s.handleProjectDelete,
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
			mcp.WithDescription("Create a new workflow and persist it to the workspace database. Fails if the name already exists."),
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
			mcp.WithDescription("Modify an existing workflow: add, remove, or update statuses and transitions. Requires the current version for optimistic locking."),
			mcp.WithString("name",
				mcp.Required(),
				mcp.Description("Workflow name"),
			),
			mcp.WithNumber("version",
				mcp.Required(),
				mcp.Description("Current workflow version (fetch via tusk_workflow_list)"),
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
			mcp.WithDescription("Delete a workflow from the workspace database. Fails if any project references it. Requires the current version for optimistic locking."),
			mcp.WithString("name",
				mcp.Required(),
				mcp.Description("Workflow name"),
			),
			mcp.WithNumber("version",
				mcp.Required(),
				mcp.Description("Current workflow version (fetch via tusk_workflow_list)"),
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

	s.addTool("note",
		mcp.NewTool("tusk_note_add",
			mcp.WithDescription("Create a note in a project, optionally attached to a task."),
			mcp.WithString("player_id", mcp.Required(), mcp.Description("Player ID — auto-registers as agent. Note is attributed to this player.")),
			mcp.WithString("body", mcp.Required(), mcp.Description("Markdown note body.")),
			mcp.WithString("project", mcp.Description("Project name. Defaults to the built-in \"_default\" project.")),
			mcp.WithString("task", mcp.Description("Task short ID to attach the note to (optional — omit for a project-level note).")),
			mcp.WithObject("metadata",
				mcp.Description("Arbitrary key-value metadata (all values must be strings). Symmetric with task UDAs."),
				mcp.AdditionalProperties(map[string]any{"type": "string"}),
			),
		),
		s.handleNoteAdd,
	)

	s.addTool("note",
		mcp.NewTool("tusk_note_list",
			mcp.WithDescription("List notes in a project, newest-first, honoring the trailing window size."),
			mcp.WithString("player_id", mcp.Required(), mcp.Description("Caller player ID — auto-registers as agent. Defaults the list scope to this player's notes.")),
			mcp.WithString("project", mcp.Description("Project name. Defaults to \"_default\".")),
			mcp.WithString("task", mcp.Description("Task short ID filter.")),
			mcp.WithString("target_player_id", mcp.Description("Show notes from a specific player. Cannot combine with all_players.")),
			mcp.WithBoolean("all_players", mcp.Description("Show notes from every player. Cannot combine with target_player_id.")),
			mcp.WithNumber("window", mcp.Description("Override trailing window size (must be > 0).")),
			mcp.WithString("since", mcp.Description("Only return notes created at or after this ISO 8601 (RFC3339) timestamp.")),
			mcp.WithBoolean("include_archived", mcp.Description("Include archived notes in the result.")),
		),
		s.handleNoteList,
	)

	s.addTool("note",
		mcp.NewTool("tusk_note_archive",
			mcp.WithDescription("Archive a note. Only the note's author may archive."),
			mcp.WithString("player_id", mcp.Required(), mcp.Description("Caller player ID — must match the note's author.")),
			mcp.WithString("id", mcp.Required(), mcp.Description("Full note UUID. Short prefixes are not accepted over MCP.")),
		),
		s.handleNoteArchive,
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
	return filter.NewResolver(s.taskSvc, s.projectSvc, defaults)
}

// Serve starts the MCP server using stdio transport.
// This blocks until the transport is closed (e.g., stdin EOF).
func (s *Server) Serve() error {
	return server.ServeStdio(s.server)
}

// reloadConfig re-reads the active config file via the stored loadOpts and
// hot-reloads the urgency engine plus the in-memory MCP config snapshot
// used by checkBlocked. It does NOT rebuild the MCP server, reopen the
// database, or reconfigure transports — those require a process restart.
//
// The s.cfg swap is guarded by s.cfgMu because MCPConfig is a struct with
// maps and slices — a plain assignment would race with concurrent
// checkBlocked readers even though writers never mutate the previous
// snapshot in place. mcp.blocked_fields therefore hot-reloads, unlike
// mcp.disabled_tools / mcp.disabled_resources, which stay frozen at boot
// because tool registration happens once in New.
//
// Safe to call from any MCP tool handler after a successful config
// mutation. Returns an error when Load fails; callers should surface the
// error back to the caller without applying partial state (Load is a full
// parse with validation, so there is no partial state to apply).
//
// Concurrent project and workflow writes are serialized by the SQLite
// store's optimistic locking, not by a server-level mutex.
func (s *Server) reloadConfig(ctx context.Context) error {
	cfg, err := config.Load(s.loadOpts...)
	if err != nil {
		return fmt.Errorf("reloading config: %w", err)
	}
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
	s.cfgMu.Lock()
	s.cfg = cfg.MCP
	s.cfgMu.Unlock()
	_ = ctx // ctx reserved for future per-call tracing
	return nil
}

// ReloadConfigForTest exposes reloadConfig for internal tests.
func (s *Server) ReloadConfigForTest(ctx context.Context) error {
	return s.reloadConfig(ctx)
}

// WorkflowRepoForTest exposes the workflow repo handle for internal tests.
func (s *Server) WorkflowRepoForTest() repository.WorkflowRepository { return s.workflowRepo }

const serverInstructions = `Tusk is a task management system. You can create, list, modify, and transition tasks through workflow statuses. Tasks support parent-child hierarchy, typed relations (blocks, relates_to, duplicates), tags, annotations, and projects. All mutation tools require a version parameter for optimistic locking — fetch the task first to get the current version. You can also inspect the active configuration via tusk_config_show and modify scalar config values via tusk_config_set (storage.* keys are read-only over MCP). Workflows can be created, modified, and deleted via tusk_workflow_create, tusk_workflow_modify, and tusk_workflow_delete using structured JSON inputs. Projects can be created, modified, and deleted via tusk_project_create, tusk_project_modify, and tusk_project_delete — deletion honors the built-in-default and referencing-tasks guards (pass force=true to bypass). Notes can be created, listed, and archived via tusk_note_add, tusk_note_list, and tusk_note_archive; notes are player-scoped and append-only (archive, don't edit).`
