package domain

// Project is a config-driven container for tasks. Projects are defined in
// config.toml and loaded into memory at startup. They are immutable at runtime.
type Project struct {
	ID       string          // Human-readable identifier from config key (e.g. "default", "backend")
	Workflow string          // Name of the workflow for this project (e.g. "kanban")
	Settings ProjectSettings // Automation settings (auto-complete/revert parent)
}
