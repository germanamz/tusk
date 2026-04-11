package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

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

	_, err = cmd.OutOrStdout().Write(data)
	return err
}

func (a *App) runConfigPath(cmd *cobra.Command, args []string) error {
	path, err := config.ConfigFilePath(a.loadOpts...)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), path)
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
	path, err := config.ConfigFilePath(a.loadOpts...)
	if err != nil {
		return err
	}

	cfg, err := config.LoadFile(path)
	if err != nil {
		return err
	}

	if err := cfg.Validate(); err != nil {
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

	path, err := config.ConfigFilePath(a.loadOpts...)
	if err != nil {
		return err
	}

	c := exec.Command(editor, path)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
