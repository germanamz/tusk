package tui

import "github.com/germanamz/tusk/config"

// configShowTOML is the scalar-only wrapper marshaled for `config show` text
// output. The projects/workflows TOML fragments are appended separately after
// this wrapper is rendered so the output stays deterministic and sourced from
// the database.
type configShowTOML struct {
	Storage config.StorageConfig `toml:"storage"`
	Urgency config.UrgencyConfig `toml:"urgency"`
	TUI     config.TUIConfig     `toml:"tui"`
	MCP     config.MCPConfig     `toml:"mcp"`
	Inline  config.InlineConfig  `toml:"inline"`
}

// configShowJSON is the flattened view type used by `config show --format json`.
// It replaces the serialized config.Config shape with projects/workflows drawn
// from the database via the service layer.
type configShowJSON struct {
	Storage   config.StorageConfig          `json:"storage"`
	Urgency   config.UrgencyConfig          `json:"urgency"`
	TUI       config.TUIConfig              `json:"tui"`
	MCP       config.MCPConfig              `json:"mcp"`
	Inline    config.InlineConfig           `json:"inline"`
	Projects  map[string]configProjectView  `json:"projects"`
	Workflows map[string]configWorkflowView `json:"workflows"`
}

type configProjectView struct {
	Workflow string                    `json:"workflow"`
	Settings configProjectSettingsView `json:"settings"`
}

type configProjectSettingsView struct {
	AutoCompleteParent *configAutoView    `json:"auto_complete_parent,omitempty"`
	AutoRevertParent   *configAutoView    `json:"auto_revert_parent,omitempty"`
	Urgency            *configUrgencyView `json:"urgency,omitempty"`
}

type configAutoView struct {
	TriggerStatus string `json:"trigger_status"`
	TargetStatus  string `json:"target_status"`
}

type configUrgencyView struct {
	PriorityWeight    *float64 `json:"priority_weight,omitempty"`
	DueWeight         *float64 `json:"due_weight,omitempty"`
	AgeWeight         *float64 `json:"age_weight,omitempty"`
	ActiveWeight      *float64 `json:"active_weight,omitempty"`
	BlockingWeight    *float64 `json:"blocking_weight,omitempty"`
	BlockedWeight     *float64 `json:"blocked_weight,omitempty"`
	TagsWeight        *float64 `json:"tags_weight,omitempty"`
	ProjectWeight     *float64 `json:"project_weight,omitempty"`
	AnnotationsWeight *float64 `json:"annotations_weight,omitempty"`
	WaitingWeight     *float64 `json:"waiting_weight,omitempty"`
}

type configWorkflowView struct {
	Statuses    map[string]configWorkflowStatusView `json:"statuses"`
	Transitions []configWorkflowTransitionView      `json:"transitions"`
}

type configWorkflowStatusView struct {
	Roles []string `json:"roles"`
}

type configWorkflowTransitionView struct {
	From string `json:"from"`
	To   string `json:"to"`
}
