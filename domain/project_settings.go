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

// UrgencyOverrides holds per-project urgency weight overrides.
// Nil fields inherit from global defaults.
type UrgencyOverrides struct {
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

// ProjectSettings holds per-project configuration stored as JSON in the
// projects table. Nil fields mean the feature is disabled (the default).
type ProjectSettings struct {
	AutoCompleteParent *AutoCompleteConfig `json:"auto_complete_parent,omitempty"`
	AutoRevertParent   *AutoRevertConfig   `json:"auto_revert_parent,omitempty"`
	Urgency            *UrgencyOverrides   `json:"urgency,omitempty"`
}
