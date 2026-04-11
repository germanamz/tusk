package tui

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/germanamz/tusk/config"
	toml "github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
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
