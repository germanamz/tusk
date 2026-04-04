package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Config is the top-level Tusk configuration.
type Config struct {
	Storage StorageConfig `mapstructure:"storage"`
	Urgency UrgencyConfig `mapstructure:"urgency"`
	TUI     TUIConfig     `mapstructure:"tui"`
	MCP     MCPConfig     `mapstructure:"mcp"`
}

// StorageConfig configures the database backend.
type StorageConfig struct {
	Backend  string         `mapstructure:"backend"`
	Path     string         `mapstructure:"path"`
	Postgres PostgresConfig `mapstructure:"postgres"`
}

// PostgresConfig holds PostgreSQL connection settings (future use).
type PostgresConfig struct {
	DSN string `mapstructure:"dsn"`
}

// UrgencyConfig holds weights for the urgency scoring algorithm.
type UrgencyConfig struct {
	PriorityWeight float64 `mapstructure:"priority_weight"`
	DueWeight      float64 `mapstructure:"due_weight"`
	AgeWeight      float64 `mapstructure:"age_weight"`
	BlockingWeight float64 `mapstructure:"blocking_weight"`
	BlockedWeight  float64 `mapstructure:"blocked_weight"`
}

// MCPConfig controls which tools and resources the MCP server exposes.
type MCPConfig struct {
	DisabledToolGroups     []string `mapstructure:"disabled_tool_groups"`
	DisabledTools          []string `mapstructure:"disabled_tools"`
	DisabledResourceGroups []string `mapstructure:"disabled_resource_groups"`
	DisabledResources      []string `mapstructure:"disabled_resources"`
}

// TUIConfig controls CLI output formatting.
type TUIConfig struct {
	DateFormat  string `mapstructure:"date_format"`
	Color       bool   `mapstructure:"color"`
	TreeIndent  int    `mapstructure:"tree_indent"`
	DefaultSort string `mapstructure:"default_sort"`
}

// Option configures the Load function for testing.
type Option func(v *viper.Viper)

// WithSearchPath overrides the config file search path.
// Used in tests to point at a temp directory.
func WithSearchPath(path string) Option {
	return func(v *viper.Viper) {
		v.AddConfigPath(path)
	}
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

	// Hardcoded defaults
	v.SetDefault("storage.backend", "sqlite")
	v.SetDefault("storage.path", "~/.local/share/tusk/tusk.db")
	v.SetDefault("storage.postgres.dsn", "")

	v.SetDefault("urgency.priority_weight", 6.0)
	v.SetDefault("urgency.due_weight", 12.0)
	v.SetDefault("urgency.age_weight", 2.0)
	v.SetDefault("urgency.blocking_weight", 8.0)
	v.SetDefault("urgency.blocked_weight", -5.0)

	v.SetDefault("tui.date_format", "2006-01-02")
	v.SetDefault("tui.color", true)
	v.SetDefault("tui.tree_indent", 2)
	v.SetDefault("tui.default_sort", "urgency")

	// MCP defaults — empty slices are the zero value, no SetDefault needed.

	// Config file
	v.SetConfigName("config")
	v.SetConfigType("toml")

	// Default search path: ~/.config/tusk/
	hasCustomPath := len(opts) > 0
	if !hasCustomPath {
		home, err := os.UserHomeDir()
		if err == nil {
			v.AddConfigPath(filepath.Join(home, ".config", "tusk"))
		}
	}

	// Apply options (e.g., WithSearchPath for tests)
	for _, opt := range opts {
		opt(v)
	}

	// Environment variables: TUSK_STORAGE_PATH, TUSK_TUI_COLOR, etc.
	v.SetEnvPrefix("TUSK")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Read config file (ignore "not found" — config is optional)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config file: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return &cfg, nil
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
