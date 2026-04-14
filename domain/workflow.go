package domain

import (
	"sort"
	"time"

	"github.com/google/uuid"
)

// StatusRole is a named behavior attached to a workflow status.
type StatusRole string

const (
	RoleInitial   StatusRole = "initial"
	RoleStart     StatusRole = "start"
	RoleTerminal  StatusRole = "terminal"
	RoleDone      StatusRole = "done"
	RoleDelete    StatusRole = "delete"
	RoleHighlight StatusRole = "highlight"
	RoleDim       StatusRole = "dim"
)

// StatusConfig holds the roles for a single workflow status.
type StatusConfig struct {
	Roles []StatusRole
}

// HasRole returns true if this status has the given role.
func (sc StatusConfig) HasRole(role StatusRole) bool {
	for _, r := range sc.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// Workflow is a named set of statuses and allowed transitions.
// Workflows are persisted in the workspace database and carry the same
// version + audit fields as every other mutable entity.
type Workflow struct {
	ID          uuid.UUID
	Name        string
	Statuses    map[string]StatusConfig
	Transitions []WorkflowTransition
	Version     int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// StatusNames returns the status names as a sorted slice.
func (w *Workflow) StatusNames() []string {
	names := make([]string, 0, len(w.Statuses))
	for name := range w.Statuses {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// StatusByRole returns the name of the status with the given role.
// Returns ("", false) if no status has the role.
func (w *Workflow) StatusByRole(role StatusRole) (string, bool) {
	for name, sc := range w.Statuses {
		if sc.HasRole(role) {
			return name, true
		}
	}
	return "", false
}

// NonTerminalStatuses returns all status names that do not have the terminal role, sorted.
func (w *Workflow) NonTerminalStatuses() []string {
	var names []string
	for name, sc := range w.Statuses {
		if !sc.HasRole(RoleTerminal) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// WorkflowTransition defines an allowed status change within a workflow.
type WorkflowTransition struct {
	FromStatus string
	ToStatus   string
}
