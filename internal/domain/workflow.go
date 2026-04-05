package domain

// Workflow is a named set of statuses and allowed transitions.
// Workflows are config-driven in-memory entities identified by Name.
type Workflow struct {
	Name        string
	Statuses    []string
	Transitions []WorkflowTransition
}

// WorkflowTransition defines an allowed status change within a workflow.
type WorkflowTransition struct {
	FromStatus string
	ToStatus   string
}
