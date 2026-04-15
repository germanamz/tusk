package config

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/spf13/viper"
)

//go:embed default.toml
var defaultConfig []byte

// ConfigSources records how the effective Config was assembled.
// It is populated by Load and is not persisted to disk.
type ConfigSources struct {
	// File is the active config file path, or "" when no user file was found.
	File string `mapstructure:"-" toml:"-" json:"-"`
}

// Config is the top-level Tusk configuration.
type Config struct {
	Storage StorageConfig `mapstructure:"storage" toml:"storage" json:"storage"`
	Urgency UrgencyConfig `mapstructure:"urgency" toml:"urgency" json:"urgency"`
	TUI     TUIConfig     `mapstructure:"tui"     toml:"tui"     json:"tui"`
	MCP     MCPConfig     `mapstructure:"mcp"     toml:"mcp"     json:"mcp"`

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

// checkLegacySections rejects config files that still carry the removed
// [projects.*] / [workflows.*] sections. Projects and workflows are now
// managed exclusively in the database; the TOML schema no longer accepts
// them.
func checkLegacySections(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("reading config file: %w", err)
	}
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		// Defer to Viper's parse error path for malformed files.
		return nil
	}
	for _, section := range []string{"projects", "workflows"} {
		v, ok := raw[section]
		if !ok {
			continue
		}
		if _, isMap := v.(map[string]any); !isMap {
			continue
		}
		return fmt.Errorf(
			"config file %s contains [%s.*] sections — projects and workflows are now managed in the database. "+
				"Remove the section(s) from the file and recreate the equivalent entries via `tusk project` / `tusk workflow`",
			filePath, section,
		)
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
		if err := checkLegacySections(filePath); err != nil {
			return nil, err
		}
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
// Currently a no-op — all historical cross-section checks were tied to
// the removed [projects.*] / [workflows.*] schema. Kept in place so
// callers do not need to change and future globals validation has a
// natural home.
func (c *Config) Validate() error {
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
