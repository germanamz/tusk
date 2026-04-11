package config

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

//go:embed default.toml
var defaultConfig []byte

// WorkflowTransitionConfig defines an allowed status transition.
type WorkflowTransitionConfig struct {
	From string `mapstructure:"from" toml:"from"`
	To   string `mapstructure:"to"   toml:"to"`
}

// WorkflowConfig defines a named workflow with its statuses and transitions.
type WorkflowConfig struct {
	Statuses          []string                   `mapstructure:"statuses"           toml:"statuses"`
	Transitions       []WorkflowTransitionConfig `mapstructure:"transitions"        toml:"transitions"`
	HighlightStatuses []string                   `mapstructure:"highlight_statuses" toml:"highlight_statuses"`
	DimStatuses       []string                   `mapstructure:"dim_statuses"       toml:"dim_statuses"`
}

// AutoCompleteParentConfig controls automatic parent completion.
type AutoCompleteParentConfig struct {
	TriggerStatus string `mapstructure:"trigger_status" toml:"trigger_status"`
	TargetStatus  string `mapstructure:"target_status"  toml:"target_status"`
}

// AutoRevertParentConfig controls automatic parent revert.
type AutoRevertParentConfig struct {
	TriggerStatus string `mapstructure:"trigger_status" toml:"trigger_status"`
	TargetStatus  string `mapstructure:"target_status"  toml:"target_status"`
}

// ProjectUrgencyConfig holds per-project urgency weight overrides.
// Nil fields inherit from the global [urgency] config.
type ProjectUrgencyConfig struct {
	PriorityWeight    *float64 `mapstructure:"priority_weight"    toml:"priority_weight,omitempty"`
	DueWeight         *float64 `mapstructure:"due_weight"         toml:"due_weight,omitempty"`
	AgeWeight         *float64 `mapstructure:"age_weight"         toml:"age_weight,omitempty"`
	ActiveWeight      *float64 `mapstructure:"active_weight"      toml:"active_weight,omitempty"`
	BlockingWeight    *float64 `mapstructure:"blocking_weight"    toml:"blocking_weight,omitempty"`
	BlockedWeight     *float64 `mapstructure:"blocked_weight"     toml:"blocked_weight,omitempty"`
	TagsWeight        *float64 `mapstructure:"tags_weight"        toml:"tags_weight,omitempty"`
	ProjectWeight     *float64 `mapstructure:"project_weight"     toml:"project_weight,omitempty"`
	AnnotationsWeight *float64 `mapstructure:"annotations_weight" toml:"annotations_weight,omitempty"`
	WaitingWeight     *float64 `mapstructure:"waiting_weight"     toml:"waiting_weight,omitempty"`
}

// ProjectSettingsConfig holds per-project automation settings.
type ProjectSettingsConfig struct {
	AutoCompleteParent *AutoCompleteParentConfig `mapstructure:"auto_complete_parent" toml:"auto_complete_parent,omitempty"`
	AutoRevertParent   *AutoRevertParentConfig   `mapstructure:"auto_revert_parent"   toml:"auto_revert_parent,omitempty"`
	Urgency            *ProjectUrgencyConfig     `mapstructure:"urgency"              toml:"urgency,omitempty"`
}

// ProjectConfig defines a named project with its workflow assignment and settings.
type ProjectConfig struct {
	Workflow string                `mapstructure:"workflow" toml:"workflow"`
	Settings ProjectSettingsConfig `mapstructure:"settings" toml:"settings"`
}

// Config is the top-level Tusk configuration.
type Config struct {
	Storage   StorageConfig             `mapstructure:"storage"   toml:"storage"`
	Urgency   UrgencyConfig             `mapstructure:"urgency"   toml:"urgency"`
	TUI       TUIConfig                 `mapstructure:"tui"       toml:"tui"`
	MCP       MCPConfig                 `mapstructure:"mcp"       toml:"mcp"`
	Workflows map[string]WorkflowConfig `mapstructure:"workflows" toml:"workflows"`
	Projects  map[string]ProjectConfig  `mapstructure:"projects"  toml:"projects"`
}

// StorageConfig configures the database backend.
type StorageConfig struct {
	Backend  string         `mapstructure:"backend"  toml:"backend"`
	Path     string         `mapstructure:"path"     toml:"path"`
	Postgres PostgresConfig `mapstructure:"postgres" toml:"postgres"`
}

// PostgresConfig holds PostgreSQL connection settings (future use).
type PostgresConfig struct {
	DSN string `mapstructure:"dsn" toml:"dsn"`
}

// UrgencyConfig holds weights for the urgency scoring algorithm.
type UrgencyConfig struct {
	PriorityWeight    float64 `mapstructure:"priority_weight"    toml:"priority_weight"`
	DueWeight         float64 `mapstructure:"due_weight"         toml:"due_weight"`
	AgeWeight         float64 `mapstructure:"age_weight"         toml:"age_weight"`
	ActiveWeight      float64 `mapstructure:"active_weight"      toml:"active_weight"`
	BlockingWeight    float64 `mapstructure:"blocking_weight"    toml:"blocking_weight"`
	BlockedWeight     float64 `mapstructure:"blocked_weight"     toml:"blocked_weight"`
	TagsWeight        float64 `mapstructure:"tags_weight"        toml:"tags_weight"`
	ProjectWeight     float64 `mapstructure:"project_weight"     toml:"project_weight"`
	AnnotationsWeight float64 `mapstructure:"annotations_weight" toml:"annotations_weight"`
	WaitingWeight     float64 `mapstructure:"waiting_weight"     toml:"waiting_weight"`
}

// MCPConfig controls which tools and resources the MCP server exposes.
type MCPConfig struct {
	DisabledToolGroups     []string `mapstructure:"disabled_tool_groups"     toml:"disabled_tool_groups"`
	DisabledTools          []string `mapstructure:"disabled_tools"           toml:"disabled_tools"`
	DisabledResourceGroups []string `mapstructure:"disabled_resource_groups" toml:"disabled_resource_groups"`
	DisabledResources      []string `mapstructure:"disabled_resources"       toml:"disabled_resources"`
}

// TUIConfig controls CLI output formatting.
type TUIConfig struct {
	DateFormat  string `mapstructure:"date_format"  toml:"date_format"`
	Color       bool   `mapstructure:"color"        toml:"color"`
	TreeIndent  int    `mapstructure:"tree_indent"  toml:"tree_indent"`
	DefaultSort string `mapstructure:"default_sort" toml:"default_sort"`
}

// Option configures the Load function.
type Option func(o *loadOptions)

type loadOptions struct {
	searchPath string
}

// WithSearchPath overrides the config file search path.
// Used in tests to point at a temp directory instead of ~/.config/tusk/.
func WithSearchPath(path string) Option {
	return func(o *loadOptions) {
		o.searchPath = path
	}
}

// ensureConfigFile creates the config file with default content if it doesn't exist.
func ensureConfigFile(searchPath string) error {
	configPath := filepath.Join(searchPath, "config.toml")
	if _, err := os.Stat(configPath); err == nil {
		return nil // file already exists
	}
	if err := os.MkdirAll(searchPath, 0o755); err != nil {
		return fmt.Errorf("creating config directory %s: %w", searchPath, err)
	}
	if err := os.WriteFile(configPath, defaultConfig, 0o644); err != nil {
		return fmt.Errorf("writing default config: %w", err)
	}
	return nil
}

// Load reads configuration from file, environment, and defaults.
//
// Precedence (highest to lowest):
//  1. TUSK_* environment variables
//  2. Config file (~/.config/tusk/config.toml)
//  3. Hardcoded defaults
//
// If no config file is found, defaults are used without error.
func Load(opts ...Option) (*Config, error) {
	v := viper.New()
	v.SetConfigType("toml")

	// Load embedded default.toml as the base configuration.
	if err := v.ReadConfig(bytes.NewReader(defaultConfig)); err != nil {
		return nil, fmt.Errorf("reading embedded defaults: %w", err)
	}

	// Apply options.
	var lo loadOptions
	for _, opt := range opts {
		opt(&lo)
	}

	// Use custom search path if provided, otherwise default to ~/.config/tusk/.
	var searchPath string
	if lo.searchPath != "" {
		searchPath = lo.searchPath
	} else if envDir := os.Getenv("TUSK_CONFIG_DIR"); envDir != "" {
		searchPath = envDir
	} else {
		home, err := os.UserHomeDir()
		if err == nil {
			searchPath = filepath.Join(home, ".config", "tusk")
		}
	}

	if searchPath != "" {
		v.SetConfigName("config")
		v.AddConfigPath(searchPath)
		if err := ensureConfigFile(searchPath); err != nil {
			return nil, err
		}

		// Merge user config on top of embedded defaults.
		if err := v.MergeInConfig(); err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
				return nil, fmt.Errorf("reading config file: %w", err)
			}
		}
	}

	// Environment variables: TUSK_STORAGE_PATH, TUSK_TUI_COLOR, etc.
	v.SetEnvPrefix("TUSK")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Validate cross-references
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

// Validate checks cross-references between config sections.
func (c *Config) Validate() error {
	for name, wf := range c.Workflows {
		statusSet := make(map[string]bool, len(wf.Statuses))
		for _, s := range wf.Statuses {
			statusSet[s] = true
		}
		for _, s := range wf.HighlightStatuses {
			if !statusSet[s] {
				return fmt.Errorf("workflow %q: highlight_statuses references unknown status %q", name, s)
			}
		}
		dimSet := make(map[string]bool, len(wf.DimStatuses))
		for _, s := range wf.DimStatuses {
			if !statusSet[s] {
				return fmt.Errorf("workflow %q: dim_statuses references unknown status %q", name, s)
			}
			dimSet[s] = true
		}
		for _, s := range wf.HighlightStatuses {
			if dimSet[s] {
				return fmt.Errorf("workflow %q: status %q cannot be in both highlight_statuses and dim_statuses", name, s)
			}
		}
	}
	for id, proj := range c.Projects {
		if _, ok := c.Workflows[proj.Workflow]; !ok {
			return fmt.Errorf("project %q references unknown workflow %q", id, proj.Workflow)
		}
	}
	return nil
}

// ExpandPath replaces a leading ~ with the user's home directory.
// Returns the path unchanged if it doesn't start with ~.
func ExpandPath(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}
