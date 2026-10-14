package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/germanamz/tusk/internal/workspace"
	"github.com/spf13/cobra"
)

// workspaceFlag is the persistent root flag that points tusk at a vault.
const workspaceFlag = "workspace"

// workspaceEnv is the environment equivalent of --workspace, for MCP clients
// whose config has env but neither args nor a working directory.
const workspaceEnv = "TUSK_WORKSPACE"

// startDir returns the absolute directory a command runs as if started in:
// --workspace, else TUSK_WORKSPACE, else the process's working directory. Like
// git -C, the walk up to tusk.toml starts there, init creates the vault there,
// and a relative path argument resolves against it.
func startDir(cmd *cobra.Command) (string, error) {
	dir, setting := workspaceSetting(cmd)

	if dir == "" {
		return os.Getwd()
	}

	expanded, expandErr := expandHome(dir)

	if expandErr != nil {
		return "", fmt.Errorf("%s %s: %w", setting, dir, expandErr)
	}

	absolute, absErr := filepath.Abs(expanded)

	if absErr != nil {
		return "", fmt.Errorf("%s %s: %w", setting, dir, absErr)
	}

	stat, statErr := os.Stat(absolute)

	if errors.Is(statErr, fs.ErrNotExist) {
		return "", fmt.Errorf("%s %s: no such directory", setting, dir)
	}

	if statErr != nil {
		return "", fmt.Errorf("%s %s: %w", setting, dir, statErr)
	}

	if !stat.IsDir() {
		return "", fmt.Errorf("%s %s: not a directory", setting, dir)
	}

	return absolute, nil
}

// workspaceSetting returns the directory --workspace or TUSK_WORKSPACE names,
// with the setting it came from for error messages. Both empty means neither
// is set and the command starts from the working directory.
func workspaceSetting(cmd *cobra.Command) (string, string) {
	if dir, _ := cmd.Flags().GetString(workspaceFlag); dir != "" {
		return dir, "--" + workspaceFlag
	}

	if dir := os.Getenv(workspaceEnv); dir != "" {
		return dir, workspaceEnv
	}

	return "", ""
}

// expandHome expands a leading ~ to the user's home directory. MCP clients
// pass args and env to the server without a shell, so "~/notes" in a JSON
// config would otherwise arrive as a literal ~.
func expandHome(dir string) (string, error) {
	if dir != "~" && !strings.HasPrefix(dir, "~/") && !strings.HasPrefix(dir, "~"+string(filepath.Separator)) {
		return dir, nil
	}

	home, homeErr := os.UserHomeDir()

	if homeErr != nil {
		return "", homeErr
	}

	return filepath.Join(home, dir[1:]), nil
}

// findWorkspace locates the workspace enclosing the command's start
// directory. When there is none, the error says what to try next, or which
// setting chose the directory when the walk didn't start from the working
// directory.
func findWorkspace(cmd *cobra.Command) (*workspace.Workspace, error) {
	dir, dirErr := startDir(cmd)

	if dirErr != nil {
		return nil, dirErr
	}

	ws, findErr := workspace.Find(dir)

	if findErr == nil {
		return ws, nil
	}

	if !errors.Is(findErr, workspace.ErrNotFound) {
		return nil, fmt.Errorf("workspace: %w", findErr)
	}

	if _, setting := workspaceSetting(cmd); setting != "" {
		return nil, fmt.Errorf("%w (from %s)", findErr, setting)
	}

	return nil, fmt.Errorf("%w; run tusk init to create one, or point tusk at a vault with --workspace <dir> or %s", findErr, workspaceEnv)
}
