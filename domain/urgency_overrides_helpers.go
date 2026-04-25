package domain

// UrgencyOverrideFieldPtr returns a pointer to the **float64 field on
// UrgencyOverrides matching the given snake_case key. Returns nil for
// unknown keys. Both the CLI parser and the service write path use this
// to translate key strings into concrete field pointers.
func UrgencyOverrideFieldPtr(o *UrgencyOverrides, key string) **float64 {
	switch key {
	case "priority_weight":
		return &o.PriorityWeight
	case "due_weight":
		return &o.DueWeight
	case "age_weight":
		return &o.AgeWeight
	case "active_weight":
		return &o.ActiveWeight
	case "blocking_weight":
		return &o.BlockingWeight
	case "blocked_weight":
		return &o.BlockedWeight
	case "tags_weight":
		return &o.TagsWeight
	case "project_weight":
		return &o.ProjectWeight
	case "annotations_weight":
		return &o.AnnotationsWeight
	case "waiting_weight":
		return &o.WaitingWeight
	}
	return nil
}
