package config

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/germanamz/tusk/domain"
	"github.com/spf13/viper"
)

//go:embed default.toml
var defaultConfig []byte

// WorkflowTransitionConfig defines an allowed status transition.
type WorkflowTransitionConfig struct {
	From string `mapstructure:"from" toml:"from" json:"from"`
	To   string `mapstructure:"to"   toml:"to"   json:"to"`
}

// StatusConfig defines a single status within a workflow.
type StatusConfig struct {
	Roles []string `mapstructure:"roles" toml:"roles" json:"roles"`
}

// Valid status roles.
const (
	RoleInitial   = "initial"   // default status for new tasks
	RoleStart     = "start"     // target for tusk start / tusk pop
	RoleTerminal  = "terminal"  // task is finished; excluded from available/pop
	RoleDone      = "done"      // target for tusk done
	RoleDelete    = "delete"    // target for tusk delete
	RoleHighlight = "highlight" // emphasized in terminal output
	RoleDim       = "dim"       // deemphasized in terminal output
)

// WorkflowConfig defines a named workflow with its statuses and transitions.
type WorkflowConfig struct {
	Statuses    map[string]StatusConfig    `mapstructure:"statuses"    toml:"statuses"    json:"statuses"`
	Transitions []WorkflowTransitionConfig `mapstructure:"transitions" toml:"transitions" json:"transitions"`
}

// AutoCompleteParentConfig controls automatic parent completion.
type AutoCompleteParentConfig struct {
	TriggerStatus string `mapstructure:"trigger_status" toml:"trigger_status" json:"trigger_status"`
	TargetStatus  string `mapstructure:"target_status"  toml:"target_status"  json:"target_status"`
}

// AutoRevertParentConfig controls automatic parent revert.
type AutoRevertParentConfig struct {
	TriggerStatus string `mapstructure:"trigger_status" toml:"trigger_status" json:"trigger_status"`
	TargetStatus  string `mapstructure:"target_status"  toml:"target_status"  json:"target_status"`
}

// ProjectUrgencyConfig holds per-project urgency weight overrides.
// Nil fields inherit from the global [urgency] config.
type ProjectUrgencyConfig struct {
	PriorityWeight    *float64 `mapstructure:"priority_weight"    toml:"priority_weight,omitempty"    json:"priority_weight,omitempty"`
	DueWeight         *float64 `mapstructure:"due_weight"         toml:"due_weight,omitempty"         json:"due_weight,omitempty"`
	AgeWeight         *float64 `mapstructure:"age_weight"         toml:"age_weight,omitempty"         json:"age_weight,omitempty"`
	ActiveWeight      *float64 `mapstructure:"active_weight"      toml:"active_weight,omitempty"      json:"active_weight,omitempty"`
	BlockingWeight    *float64 `mapstructure:"blocking_weight"    toml:"blocking_weight,omitempty"    json:"blocking_weight,omitempty"`
	BlockedWeight     *float64 `mapstructure:"blocked_weight"     toml:"blocked_weight,omitempty"     json:"blocked_weight,omitempty"`
	TagsWeight        *float64 `mapstructure:"tags_weight"        toml:"tags_weight,omitempty"        json:"tags_weight,omitempty"`
	ProjectWeight     *float64 `mapstructure:"project_weight"     toml:"project_weight,omitempty"     json:"project_weight,omitempty"`
	AnnotationsWeight *float64 `mapstructure:"annotations_weight" toml:"annotations_weight,omitempty" json:"annotations_weight,omitempty"`
	WaitingWeight     *float64 `mapstructure:"waiting_weight"     toml:"waiting_weight,omitempty"     json:"waiting_weight,omitempty"`
}

// ProjectSettingsConfig holds per-project automation settings.
type ProjectSettingsConfig struct {
	AutoCompleteParent *AutoCompleteParentConfig `mapstructure:"auto_complete_parent" toml:"auto_complete_parent,omitempty" json:"auto_complete_parent,omitempty"`
	AutoRevertParent   *AutoRevertParentConfig   `mapstructure:"auto_revert_parent"   toml:"auto_revert_parent,omitempty"   json:"auto_revert_parent,omitempty"`
	Urgency            *ProjectUrgencyConfig     `mapstructure:"urgency"              toml:"urgency,omitempty"              json:"urgency,omitempty"`
}

// ProjectConfig defines a named project with its workflow assignment and settings.
type ProjectConfig struct {
	Workflow string                `mapstructure:"workflow" toml:"workflow" json:"workflow"`
	Settings ProjectSettingsConfig `mapstructure:"settings" toml:"settings" json:"settings"`
}

// ConfigSources records how the effective Config was assembled.
// It is populated by Load and is not persisted to disk.
type ConfigSources struct {
	// File is the active config file path, or "" when no user file was found.
	File string `mapstructure:"-" toml:"-" json:"-"`
}

// Config is the top-level Tusk configuration.
type Config struct {
	Storage   StorageConfig             `mapstructure:"storage"   toml:"storage"   json:"storage"`
	Urgency   UrgencyConfig             `mapstructure:"urgency"   toml:"urgency"   json:"urgency"`
	TUI       TUIConfig                 `mapstructure:"tui"       toml:"tui"       json:"tui"`
	MCP       MCPConfig                 `mapstructure:"mcp"       toml:"mcp"       json:"mcp"`
	Workflows map[string]WorkflowConfig `mapstructure:"workflows" toml:"workflows" json:"workflows"`
	Projects  map[string]ProjectConfig  `mapstructure:"projects"  toml:"projects"  json:"projects"`

	// Sources records where the effective config came from. Populated by Load.
	// Skipped by both mapstructure and TOML encoding so it never appears in
	// files or round-trips through Viper.
	Sources ConfigSources `mapstructure:"-" toml:"-" json:"-"`
}

// StorageConfig configures the database backend.
type StorageConfig struct {
	Backend  string         `mapstructure:"backend"  toml:"backend"  json:"backend"`
	Path     string         `mapstructure:"path"     toml:"path"     json:"path"`
	Postgres PostgresConfig `mapstructure:"postgres" toml:"postgres" json:"postgres"`
}

// PostgresConfig holds PostgreSQL connection settings (future use).
type PostgresConfig struct {
	DSN string `mapstructure:"dsn" toml:"dsn" json:"dsn"`
}

// UrgencyConfig holds weights for the urgency scoring algorithm.
type UrgencyConfig struct {
	PriorityWeight    float64 `mapstructure:"priority_weight"    toml:"priority_weight"    json:"priority_weight"`
	DueWeight         float64 `mapstructure:"due_weight"         toml:"due_weight"         json:"due_weight"`
	AgeWeight         float64 `mapstructure:"age_weight"         toml:"age_weight"         json:"age_weight"`
	ActiveWeight      float64 `mapstructure:"active_weight"      toml:"active_weight"      json:"active_weight"`
	BlockingWeight    float64 `mapstructure:"blocking_weight"    toml:"blocking_weight"    json:"blocking_weight"`
	BlockedWeight     float64 `mapstructure:"blocked_weight"     toml:"blocked_weight"     json:"blocked_weight"`
	TagsWeight        float64 `mapstructure:"tags_weight"        toml:"tags_weight"        json:"tags_weight"`
	ProjectWeight     float64 `mapstructure:"project_weight"     toml:"project_weight"     json:"project_weight"`
	AnnotationsWeight float64 `mapstructure:"annotations_weight" toml:"annotations_weight" json:"annotations_weight"`
	WaitingWeight     float64 `mapstructure:"waiting_weight"     toml:"waiting_weight"     json:"waiting_weight"`
}

// MCPConfig controls which tools and resources the MCP server exposes.
type MCPConfig struct {
	DisabledToolGroups     []string `mapstructure:"disabled_tool_groups"     toml:"disabled_tool_groups"     json:"disabled_tool_groups"`
	DisabledTools          []string `mapstructure:"disabled_tools"           toml:"disabled_tools"           json:"disabled_tools"`
	DisabledResourceGroups []string `mapstructure:"disabled_resource_groups" toml:"disabled_resource_groups" json:"disabled_resource_groups"`
	DisabledResources      []string `mapstructure:"disabled_resources"       toml:"disabled_resources"       json:"disabled_resources"`
}

// TUIConfig controls CLI output formatting.
type TUIConfig struct {
	DateFormat  string `mapstructure:"date_format"  toml:"date_format"  json:"date_format"`
	Color       bool   `mapstructure:"color"        toml:"color"        json:"color"`
	TreeIndent  int    `mapstructure:"tree_indent"  toml:"tree_indent"  json:"tree_indent"`
	DefaultSort string `mapstructure:"default_sort" toml:"default_sort" json:"default_sort"`
}

// Option configures the Load function.
type Option func(o *loadOptions)

type loadOptions struct {
	searchPath   string
	explicitFile string
	startDir     string
}

// WithSearchPath overrides the global config directory used to locate
// config.toml. Used in tests to point at a temp directory instead of
// ~/.config/tusk/. It does not affect the explicit-file path.
func WithSearchPath(path string) Option {
	return func(o *loadOptions) {
		o.searchPath = path
	}
}

// WithExplicitFile points Load/ConfigFilePath at a specific config file.
// If the file does not exist, Load returns a hard error; the global
// search path is bypassed entirely.
func WithExplicitFile(path string) Option {
	return func(o *loadOptions) {
		o.explicitFile = path
	}
}

// WithStartDir sets the directory used for walk-up discovery of a local
// tusk.toml. Only meaningful when no explicit file is configured.
func WithStartDir(path string) Option {
	return func(o *loadOptions) {
		o.startDir = path
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
//  2. Config file resolved by ResolveConfigFile
//  3. Hardcoded defaults embedded in the binary
//
// When WithExplicitFile is set the file must exist — a missing file is a
// hard error. Otherwise Load walks up from WithStartDir looking for a
// tusk.toml, then falls back to the global directory (WithSearchPath >
// TUSK_CONFIG_DIR > ~/.config/tusk). The global config.toml is auto-created
// only when walk-up finds nothing — running tusk inside a project with its
// own tusk.toml never spawns a global file. If the home directory cannot be
// resolved and no search path is provided, Load proceeds with embedded
// defaults only.
func Load(opts ...Option) (*Config, error) {
	v := viper.New()
	v.SetConfigType("toml")

	if err := v.ReadConfig(bytes.NewReader(defaultConfig)); err != nil {
		return nil, fmt.Errorf("reading embedded defaults: %w", err)
	}

	var lo loadOptions
	for _, opt := range opts {
		opt(&lo)
	}

	globalDir := resolveGlobalDir(lo.searchPath)

	filePath, err := ResolveConfigFile(lo.startDir, lo.explicitFile, globalDir)
	if err != nil {
		return nil, err
	}

	// When the resolver finds nothing — no explicit file, no walk-up hit,
	// no pre-existing global config.toml — create the global file so a
	// fresh install gets a seeded config. A walk-up hit suppresses this.
	if filePath == "" && lo.explicitFile == "" && globalDir != "" {
		if err := ensureConfigFile(globalDir); err != nil {
			return nil, err
		}
		filePath = filepath.Join(globalDir, "config.toml")
	}

	if filePath != "" {
		v.SetConfigFile(filePath)
		if err := v.MergeInConfig(); err != nil {
			return nil, fmt.Errorf("reading config file: %w", err)
		}
	}

	v.SetEnvPrefix("TUSK")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	cfg.Sources.File = filePath

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

// resolveGlobalDir mirrors the legacy search-path precedence:
// WithSearchPath option > TUSK_CONFIG_DIR env > ~/.config/tusk.
// Returns "" when the home directory cannot be determined and no
// explicit override was provided.
func resolveGlobalDir(searchPath string) string {
	if searchPath != "" {
		return searchPath
	}
	if envDir := os.Getenv("TUSK_CONFIG_DIR"); envDir != "" {
		return envDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "tusk")
}

// Validate checks cross-references between config sections.
func (c *Config) Validate() error {
	for name, wfCfg := range c.Workflows {
		wf, err := WorkflowFromConfig(name, wfCfg)
		if err != nil {
			return fmt.Errorf("workflow %q: %w", name, err)
		}
		if err := domain.ValidateWorkflow(wf); err != nil {
			return err
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
