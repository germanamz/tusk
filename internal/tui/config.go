package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/germanamz/tusk/config"
	toml "github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// buildConfigCmd creates the `tusk config` command group.
func (a *App) buildConfigCmd() *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
	}

	configCmd.AddCommand(
		&cobra.Command{
			Use:   "show",
			Short: "Display current effective configuration",
			Args:  cobra.NoArgs,
			RunE:  a.runConfigShow,
		},
		&cobra.Command{
			Use:   "path",
			Short: "Print resolved config file path",
			Args:  cobra.NoArgs,
			RunE:  a.runConfigPath,
		},
		&cobra.Command{
			Use:   "init",
			Short: "Create config file with defaults if none exists",
			Args:  cobra.NoArgs,
			RunE:  a.runConfigInit,
		},
		&cobra.Command{
			Use:   "get <key>",
			Short: "Get a specific config value by dot-path key",
			Args:  cobra.ExactArgs(1),
			RunE:  a.runConfigGet,
		},
		&cobra.Command{
			Use:   "validate",
			Short: "Validate config file for errors",
			Args:  cobra.NoArgs,
			RunE:  a.runConfigValidate,
		},
		&cobra.Command{
			Use:   "edit",
			Short: "Open config file in $EDITOR",
			Args:  cobra.NoArgs,
			RunE:  a.runConfigEdit,
		},
		&cobra.Command{
			Use:   "set <key> <value>",
			Short: "Set a config value and write to file",
			Args:  cobra.ExactArgs(2),
			RunE:  a.runConfigSet,
		},
	)

	return configCmd
}

func (a *App) runConfigShow(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(a.loadOpts...)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	out := cmd.OutOrStdout()

	if a.format != "json" {
		header := "# active: "
		if cfg.Sources.File != "" {
			header += cfg.Sources.File
		} else {
			header += "defaults only"
		}
		if _, err := fmt.Fprintln(out, header); err != nil {
			return err
		}
	}

	_, err = out.Write(data)
	return err
}

func (a *App) runConfigPath(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(a.loadOpts...)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if cfg.Sources.File != "" {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), cfg.Sources.File)
		return err
	}

	path, err := config.ConfigFilePath(a.loadOpts...)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), path); err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.ErrOrStderr(), "(not yet created)")
	return err
}

func (a *App) runConfigInit(cmd *cobra.Command, args []string) error {
	path, err := config.ConfigFilePath(a.loadOpts...)
	if err != nil {
		return err
	}

	if _, err := os.Stat(path); err == nil {
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Config file already exists: %s\n", path)
		return err
	}

	// Create directory and write defaults.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	// Load embedded defaults and write them.
	cfg, err := config.Load(a.loadOpts...)
	if err != nil {
		return fmt.Errorf("loading defaults: %w", err)
	}
	if err := config.WriteConfig(cfg, path); err != nil {
		return err
	}

	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n", path)
	return err
}

func (a *App) runConfigGet(cmd *cobra.Command, args []string) error {
	key := args[0]

	if !config.IsValidKey(key) {
		return fmt.Errorf("unknown config key: %q", key)
	}

	// Build a Viper instance with the same config as Load() to get dot-path resolution.
	v, err := a.buildConfigViper()
	if err != nil {
		return err
	}

	val := v.Get(key)
	if val == nil {
		return fmt.Errorf("unknown config key: %q", key)
	}

	// Determine output format.
	switch v := val.(type) {
	case string, bool, int, int64, float64:
		if a.format == "json" {
			enc := json.NewEncoder(cmd.OutOrStdout())
			return enc.Encode(val)
		}
		_, err := fmt.Fprintln(cmd.OutOrStdout(), v)
		return err
	default:
		// Complex value — always JSON.
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(val)
	}
}

// buildConfigViper creates a Viper instance mirroring the Load() setup for dot-path access.
func (a *App) buildConfigViper() (*viper.Viper, error) {
	cfg, err := config.Load(a.loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	data, err := toml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling config: %w", err)
	}

	v := viper.New()
	v.SetConfigType("toml")
	if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("reading config into viper: %w", err)
	}

	return v, nil
}

func (a *App) runConfigValidate(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(a.loadOpts...)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if cfg.Sources.File == "" {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "no user config — defaults only")
		return err
	}

	fileCfg, err := config.LoadFile(cfg.Sources.File)
	if err != nil {
		return err
	}
	if err := fileCfg.Validate(); err != nil {
		return err
	}

	_, err = fmt.Fprintln(cmd.OutOrStdout(), "Config valid")
	return err
}

func (a *App) runConfigEdit(cmd *cobra.Command, args []string) error {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		return fmt.Errorf("$EDITOR is not set")
	}

	cfg, err := config.Load(a.loadOpts...)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	path := cfg.Sources.File
	if path == "" {
		initPath, err := config.ConfigFilePath(a.loadOpts...)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(initPath), 0o755); err != nil {
			return fmt.Errorf("creating config directory: %w", err)
		}
		if err := config.WriteConfig(cfg, initPath); err != nil {
			return err
		}
		path = initPath
	}

	c := exec.Command(editor, path)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func (a *App) runConfigSet(cmd *cobra.Command, args []string) error {
	key := args[0]
	value := args[1]

	if !config.IsValidKey(key) {
		return fmt.Errorf("unknown config key: %q", key)
	}

	// Resolve config file path.
	path, err := config.ConfigFilePath(a.loadOpts...)
	if err != nil {
		return err
	}

	// Reject if no config file exists.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("no config file found; run \"tusk config init\" to create one")
	}

	// Load the file contents (no defaults, no env).
	fileCfg, err := config.LoadFile(path)
	if err != nil {
		return err
	}

	// Marshal to TOML, load into Viper for dot-path Set().
	data, err := toml.Marshal(fileCfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	v := viper.New()
	v.SetConfigType("toml")
	if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
		return fmt.Errorf("reading config into viper: %w", err)
	}

	// Determine if this is a slice field and parse accordingly.
	var parsedValue any
	if config.IsSliceKey(key) {
		parsedValue = strings.Split(value, ",")
	} else {
		parsedValue = value
	}

	v.Set(key, parsedValue)

	// Unmarshal back to Config.
	var newCfg config.Config
	if err := v.Unmarshal(&newCfg); err != nil {
		return fmt.Errorf("applying config change: %w", err)
	}

	// Validate before writing.
	if err := newCfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	return config.WriteConfig(&newCfg, path)
}
