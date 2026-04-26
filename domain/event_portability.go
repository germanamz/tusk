package domain

import (
	"time"
)

const (
	// EventWorkspaceImported is emitted exactly once per `tusk import`
	// invocation, inside the apply transaction, recording what the import
	// did at a workspace-wide level. Per-entity events are not emitted —
	// the dump's existing event log already captures per-entity history.
	EventWorkspaceImported EventType = "workspace_imported"

	// EntityWorkspace identifies workspace-scoped events that don't belong
	// to a single task or relation. The event's EntityID for an
	// EventWorkspaceImported is the empty string.
	EntityWorkspace EntityKind = "workspace"
)

// WorkspaceImportedPayload describes a completed import operation.
// Counts are keyed by entity kind ("tasks", "projects", …) and report the
// number of rows inserted or updated by the import, including under
// --replace. Replaced and Truncated reflect the ImportOptions used.
type WorkspaceImportedPayload struct {
	Kind          EventType      `json:"kind"`
	SchemaVersion int            `json:"schema_version"`
	SourceTuskVer string         `json:"source_tusk_version"`
	ExportedAt    time.Time      `json:"exported_at"`
	Replace       bool           `json:"replace"`
	Truncate      bool           `json:"truncate"`
	Counts        map[string]int `json:"counts"`
}

// EventKind satisfies the EventPayload interface.
func (WorkspaceImportedPayload) EventKind() EventType { return EventWorkspaceImported }
