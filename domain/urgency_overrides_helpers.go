package domain

// UrgencyOverrideFieldPtr returns a pointer to the **float64 field on
// UrgencyOverrides matching the given snake_case key. Returns nil for
// unknown keys. Both the CLI parser and the service write path use this
// to translate key strings into concrete field pointers.
func UrgencyOverrideFieldPtr(overrides *UrgencyOverrides, key string) **float64 {
	switch key {
	case "priority_weight":
		return &overrides.PriorityWeight
	case "due_weight":
		return &overrides.DueWeight
	case "age_weight":
		return &overrides.AgeWeight
	case "active_weight":
		return &overrides.ActiveWeight
	case "blocking_weight":
		return &overrides.BlockingWeight
	case "blocked_weight":
		return &overrides.BlockedWeight
	case "tags_weight":
		return &overrides.TagsWeight
	case "project_weight":
		return &overrides.ProjectWeight
	case "annotations_weight":
		return &overrides.AnnotationsWeight
	case "waiting_weight":
		return &overrides.WaitingWeight
	}
	return nil
}
