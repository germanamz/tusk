package domain

// AutoCompleteConfig controls automatic parent completion when all children
// reach TriggerStatus. The parent is transitioned to TargetStatus.
type AutoCompleteConfig struct {
	TriggerStatus string `json:"trigger_status"`
	TargetStatus  string `json:"target_status"`
}

// AutoRevertConfig controls automatic parent revert when a child moves away
// from TriggerStatus. The parent is transitioned to TargetStatus.
type AutoRevertConfig struct {
	TriggerStatus string `json:"trigger_status"`
	TargetStatus  string `json:"target_status"`
}

// ProjectSettings holds per-project configuration stored as JSON in the
// projects table. Nil fields mean the feature is disabled (the default).
type ProjectSettings struct {
	AutoCompleteParent *AutoCompleteConfig `json:"auto_complete_parent,omitempty"`
	AutoRevertParent   *AutoRevertConfig   `json:"auto_revert_parent,omitempty"`
}
