package tui

import (
	"errors"
	"fmt"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/spf13/cobra"
)

// formatError translates domain errors into user-friendly messages.
func formatError(err error, shortID string) string {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return fmt.Sprintf("Task not found: %s", shortID)
	case errors.Is(err, domain.ErrConflict):
		return "Version conflict - task was modified by another process"
	default:
		return err.Error()
	}
}

func (a *App) runAdd(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("not implemented")
}

func (a *App) runList(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("not implemented")
}

func (a *App) runInfo(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("not implemented")
}

func (a *App) runModify(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("not implemented")
}

func (a *App) runStart(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("not implemented")
}

func (a *App) runDone(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("not implemented")
}

func (a *App) runDelete(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("not implemented")
}

func (a *App) runAnnotate(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("not implemented")
}
