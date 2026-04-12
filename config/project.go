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
