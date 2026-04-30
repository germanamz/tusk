package mcp

// toolFields enumerates every declared input parameter for each MCP tool
// that accepts writes. Used by validateConfig to reject unknown entries
// in mcp.blocked_fields at startup, and by checkBlocked at runtime
// (introduced in phase 3).
//
// This registry is hand-maintained. Adding, renaming, or removing a
// tool parameter in registerTools (server.go) requires a matching edit
// here. Keep entries in the same order handlers appear in registerTools
// for easy cross-reference.
var toolFields = map[string]map[string]struct{}{
	"tusk_task_create": setOf(
		"title", "description", "priority", "project", "parent",
		"tags", "due", "wait_until", "uda",
	),
	"tusk_task_modify": setOf(
		"short_id", "version", "title", "description", "priority",
		"project", "parent", "due", "wait_until", "uda",
		"add_tags", "remove_tags",
		"urgency_overrides", "urgency_overrides_clear",
	),
	"tusk_task_move":       setOf("task_id", "position", "target_id", "parent_id", "version", "player_id"),
	"tusk_task_resequence": setOf("parent_id", "player_id"),
	"tusk_task_start":      setOf("short_id", "version", "player_id"),
	"tusk_task_done":       setOf("short_id", "version"),
	"tusk_task_delete":     setOf("short_id", "version"),
	"tusk_task_annotate":   setOf("short_id", "body"),
	"tusk_task_link":       setOf("source", "target", "type"),
	"tusk_task_unlink":     setOf("source", "target", "type"),
	"tusk_task_claim":      setOf("short_id", "player_id", "version"),
	"tusk_task_release":    setOf("short_id", "player_id", "version"),
	"tusk_task_pop":        setOf("player_id", "filter"),

	"tusk_project_create": setOf("name", "workflow", "urgency", "auto_complete", "auto_revert"),
	"tusk_project_modify": setOf("name", "version", "workflow", "urgency_set", "urgency_delta", "auto_complete", "auto_revert", "taxonomy"),
	"tusk_project_delete": setOf("name", "version", "force"),

	"tusk_workflow_create": setOf("name", "statuses", "transitions"),
	"tusk_workflow_modify": setOf("name", "version", "add_statuses", "set_statuses", "remove_statuses", "add_transitions", "remove_transitions"),
	"tusk_workflow_delete": setOf("name", "version"),

	"tusk_player_register": setOf("player_id"),

	"tusk_note_add":     setOf("player_id", "body", "project", "task", "metadata"),
	"tusk_note_archive": setOf("player_id", "id"),

	"tusk_config_set": setOf("key", "value"),
}

func setOf(keys ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		result[key] = struct{}{}
	}
	return result
}
