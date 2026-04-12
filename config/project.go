package config

import "fmt"

// DefaultProjectID is the name of the built-in project that ships with Tusk.
const DefaultProjectID = "default"

// TaskRefChecker reports how many tasks reference a project by name.
// Passed to DeleteProject so the config package stays free of
// service/repository imports.
type TaskRefChecker func(projectName string) (int, error)

// CreateProject adds a new project to the config file.
// Returns error if the name already exists or validation fails.
func CreateProject(path string, name string, proj ProjectConfig) error {
	cfg, err := LoadFile(path)
	if err != nil {
		return err
	}
	if cfg.Projects == nil {
		cfg.Projects = make(map[string]ProjectConfig)
	}
	if _, exists := cfg.Projects[name]; exists {
		return fmt.Errorf("project %q already exists", name)
	}
	cfg.Projects[name] = proj
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	return WriteConfig(cfg, path)
}

// DeleteProject removes a project from the config file.
// Rejects the built-in default project and any project with task
// references unless force is true.
func DeleteProject(path string, name string, hasRefs TaskRefChecker, force bool) error {
	cfg, err := LoadFile(path)
	if err != nil {
		return err
	}
	if _, exists := cfg.Projects[name]; !exists {
		return fmt.Errorf("project %q: not found", name)
	}
	if name == DefaultProjectID && !force {
		return fmt.Errorf("cannot delete built-in %q project (use --force to override)", DefaultProjectID)
	}
	if hasRefs != nil {
		count, err := hasRefs(name)
		if err != nil {
			return fmt.Errorf("checking task references: %w", err)
		}
		if count > 0 && !force {
			return fmt.Errorf("project %q has %d referencing task(s) (use --force to override)", name, count)
		}
	}
	delete(cfg.Projects, name)
	return WriteConfig(cfg, path)
}

// ProjectMutation describes changes to apply to an existing project.
// Pointer fields: nil = don't change, non-nil = set (empty string clears db_path).
// UrgencySet and UrgencyDelta share the same key namespace. Applying both
// a set and a delta for the same key is rejected at apply time.
type ProjectMutation struct {
	Workflow        *string
	DBPath          *string
	AutoCompleteSet *AutoCompleteParentConfig
	AutoRevertSet   *AutoRevertParentConfig
	UrgencySet      map[string]float64
	UrgencyDelta    map[string]float64
}

// urgencyFieldPtr returns the **float64 slot on a ProjectUrgencyConfig for
// a given TOML/struct key. Returns nil on unknown keys.
func urgencyFieldPtr(u *ProjectUrgencyConfig, key string) **float64 {
	switch key {
	case "priority_weight":
		return &u.PriorityWeight
	case "due_weight":
		return &u.DueWeight
	case "age_weight":
		return &u.AgeWeight
	case "active_weight":
		return &u.ActiveWeight
	case "blocking_weight":
		return &u.BlockingWeight
	case "blocked_weight":
		return &u.BlockedWeight
	case "tags_weight":
		return &u.TagsWeight
	case "project_weight":
		return &u.ProjectWeight
	case "annotations_weight":
		return &u.AnnotationsWeight
	case "waiting_weight":
		return &u.WaitingWeight
	}
	return nil
}

// UrgencyFieldPtr is the exported wrapper over urgencyFieldPtr.
// Phase 2's parser needs this from outside the config package.
func UrgencyFieldPtr(u *ProjectUrgencyConfig, key string) **float64 {
	return urgencyFieldPtr(u, key)
}

// globalWeight returns the global urgency weight for a config key.
func globalWeight(g UrgencyConfig, key string) (float64, bool) {
	switch key {
	case "priority_weight":
		return g.PriorityWeight, true
	case "due_weight":
		return g.DueWeight, true
	case "age_weight":
		return g.AgeWeight, true
	case "active_weight":
		return g.ActiveWeight, true
	case "blocking_weight":
		return g.BlockingWeight, true
	case "blocked_weight":
		return g.BlockedWeight, true
	case "tags_weight":
		return g.TagsWeight, true
	case "project_weight":
		return g.ProjectWeight, true
	case "annotations_weight":
		return g.AnnotationsWeight, true
	case "waiting_weight":
		return g.WaitingWeight, true
	}
	return 0, false
}

// ResolveWeightDelta returns the new per-project urgency weight after
// applying a delta relative to the effective current value. When
// override is nil, the delta is applied against the global weight.
func ResolveWeightDelta(globalWeight float64, override *float64, delta float64) float64 {
	base := globalWeight
	if override != nil {
		base = *override
	}
	return base + delta
}

// ModifyProject applies a mutation to an existing project in the config file.
func ModifyProject(path string, name string, mut ProjectMutation) error {
	cfg, err := LoadFile(path)
	if err != nil {
		return err
	}
	proj, exists := cfg.Projects[name]
	if !exists {
		return fmt.Errorf("project %q: not found", name)
	}

	if mut.Workflow != nil {
		proj.Workflow = *mut.Workflow
	}
	if mut.DBPath != nil {
		proj.DBPath = *mut.DBPath
	}
	if mut.AutoCompleteSet != nil {
		ac := *mut.AutoCompleteSet
		proj.Settings.AutoCompleteParent = &ac
	}
	if mut.AutoRevertSet != nil {
		ar := *mut.AutoRevertSet
		proj.Settings.AutoRevertParent = &ar
	}

	for k := range mut.UrgencySet {
		if _, dup := mut.UrgencyDelta[k]; dup {
			return fmt.Errorf("urgency key %q has both absolute and delta", k)
		}
	}

	if len(mut.UrgencySet) > 0 || len(mut.UrgencyDelta) > 0 {
		if proj.Settings.Urgency == nil {
			proj.Settings.Urgency = &ProjectUrgencyConfig{}
		}
		for k, v := range mut.UrgencySet {
			fp := urgencyFieldPtr(proj.Settings.Urgency, k)
			if fp == nil {
				return fmt.Errorf("unknown urgency key %q", k)
			}
			val := v
			*fp = &val
		}
		for k, delta := range mut.UrgencyDelta {
			fp := urgencyFieldPtr(proj.Settings.Urgency, k)
			if fp == nil {
				return fmt.Errorf("unknown urgency key %q", k)
			}
			gw, ok := globalWeight(cfg.Urgency, k)
			if !ok {
				return fmt.Errorf("unknown urgency key %q", k)
			}
			val := ResolveWeightDelta(gw, *fp, delta)
			*fp = &val
		}
	}

	cfg.Projects[name] = proj
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	return WriteConfig(cfg, path)
}
