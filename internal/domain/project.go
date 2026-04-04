package domain

// Project is a config-driven container for tasks. Projects are defined in
// config.toml and loaded into memory at startup. They are immutable at runtime.
type Project struct {
	ID       string          // Human-readable identifier, e.g. "default", "backend". The config key.
	Workflow string          // Name of the workflow for tasks in this project, e.g. "kanban".
	Settings ProjectSettings // Automation config (auto-complete/revert parent propagation).
}
