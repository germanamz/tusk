package tui

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/service"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// moveFlags collects the cobra flag values for `tusk task move`. Kept on the
// stack (not on App) so invocations do not leak flag state across Execute
// calls in the unified cobra test harness.
type moveFlags struct {
	before     string
	after      string
	first      bool
	last       bool
	parent     string
	resequence string
	version    int
}

// buildTaskMoveCmd wires the `tusk task move` subcommand. Kept in its own
// file because the positional shape and flag mutual-exclusion rules are
// substantially larger than every other task subcommand.
func (app *App) buildTaskMoveCmd() *cobra.Command {
	flags := &moveFlags{}
	cmd := &cobra.Command{
		Use:   "move [<short_id>]",
		Short: "Reposition a task within its sibling group or re-parent it",
		Long: `Move a task relative to a target sibling (--before/--after), to the
extremes of a sibling group (--first/--last, optionally re-parenting via
--parent), or rewrite the whole sibling group to dense integers
(--resequence <parent>).

Exactly one of --before, --after, --first, --last, or --resequence must be
set. --parent is allowed only with --first or --last; use the literal value
"root" to re-parent to the top level.`,
		Example: `  tusk task move a3f8b2c1 --before b7c9d4e2
  tusk task move a3f8b2c1 --after  b7c9d4e2
  tusk task move a3f8b2c1 --first --parent b7c9d4e2
  tusk task move a3f8b2c1 --last  --parent root
  tusk task move --resequence b7c9d4e2
  tusk task move --resequence root`,
		Args:    cobra.MaximumNArgs(1),
		PreRunE: app.validateMoveFlags(flags),
		RunE:    app.runMove(flags),
	}
	cmd.Flags().StringVar(&flags.before, "before", "", "place task immediately before target short_id")
	cmd.Flags().StringVar(&flags.after, "after", "", "place task immediately after target short_id")
	cmd.Flags().BoolVar(&flags.first, "first", false, "place task at the head of its (optionally re-homed) sibling group")
	cmd.Flags().BoolVar(&flags.last, "last", false, "place task at the tail of its (optionally re-homed) sibling group")
	cmd.Flags().StringVar(&flags.parent, "parent", "", "new parent short_id (or \"root\"); only valid with --first or --last")
	cmd.Flags().StringVar(&flags.resequence, "resequence", "", "rewrite the entire sibling group under this parent short_id (or \"root\") to dense integer orders")
	cmd.Flags().IntVar(&flags.version, "version", 0, "expected task version (optimistic lock); fetched automatically when omitted")
	return cmd
}

// validateMoveFlags enforces the mutual-exclusion rules described in the
// command's long help. Positional arg is required for every mode except
// --resequence, which takes its parent through the flag.
func (app *App) validateMoveFlags(flags *moveFlags) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		positions := 0
		if flags.before != "" {
			positions++
		}
		if flags.after != "" {
			positions++
		}
		if flags.first {
			positions++
		}
		if flags.last {
			positions++
		}
		resequenceSet := cmd.Flags().Changed("resequence")
		if resequenceSet {
			positions++
		}
		if positions == 0 {
			return fmt.Errorf("exactly one of --before, --after, --first, --last, or --resequence is required")
		}
		if positions > 1 {
			return fmt.Errorf("exactly one of --before, --after, --first, --last, or --resequence may be set")
		}

		if flags.parent != "" && !flags.first && !flags.last {
			return fmt.Errorf("--parent is only valid with --first or --last")
		}

		if resequenceSet {
			if len(args) > 0 {
				return fmt.Errorf("--resequence takes its parent through the flag; do not pass a positional short_id")
			}
			if flags.parent != "" {
				return fmt.Errorf("--parent cannot be combined with --resequence")
			}
			if flags.version != 0 {
				return fmt.Errorf("--version is not valid with --resequence")
			}
			return nil
		}

		if len(args) != 1 {
			return fmt.Errorf("move requires the task short_id as a positional argument")
		}
		return nil
	}
}

// runMove dispatches into either service.Move or service.Resequence based on
// the validated flag combination.
func (app *App) runMove(flags *moveFlags) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		if cmd.Flags().Changed("resequence") {
			return app.runResequence(cmd, flags.resequence)
		}

		shortID := args[0]
		current, getErr := app.taskSvc.GetByShortID(ctx, shortID)

		if getErr != nil {
			return fmt.Errorf("%s", formatError(getErr, shortID))
		}

		version := flags.version
		if version == 0 {
			version = current.Version
		}

		req := service.MoveRequest{
			TaskID:  current.ID,
			Version: version,
		}
		if app.playerID != "" {
			playerID := app.playerID
			req.ActorID = &playerID
		}

		switch {
		case flags.before != "":
			target, targetErr := app.taskSvc.GetByShortID(ctx, flags.before)

			if targetErr != nil {
				return fmt.Errorf("%s", formatError(targetErr, flags.before))
			}

			req.Position = service.MovePositionBefore
			tid := target.ID
			req.TargetID = &tid
		case flags.after != "":
			target, targetErr := app.taskSvc.GetByShortID(ctx, flags.after)

			if targetErr != nil {
				return fmt.Errorf("%s", formatError(targetErr, flags.after))
			}

			req.Position = service.MovePositionAfter
			tid := target.ID
			req.TargetID = &tid
		case flags.first:
			req.Position = service.MovePositionFirst
			if err := app.setMoveParent(cmd, flags, &req); err != nil {
				return err
			}
		case flags.last:
			req.Position = service.MovePositionLast
			if err := app.setMoveParent(cmd, flags, &req); err != nil {
				return err
			}
		}

		updated, moveErr := app.taskSvc.Move(ctx, req)

		if moveErr != nil {
			return formatMoveError(moveErr, shortID)
		}

		renderer := app.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
		return renderer.renderMutationResult("Moved", updated, nil)
	}
}

// setMoveParent translates the --parent flag (including the "root" sentinel)
// into a tristate **uuid.UUID on the MoveRequest. Only invoked in first/last
// branches where --parent is meaningful.
func (app *App) setMoveParent(cmd *cobra.Command, flags *moveFlags, req *service.MoveRequest) error {
	if !cmd.Flags().Changed("parent") {
		return nil
	}
	if flags.parent == "root" {
		var nilUUID *uuid.UUID
		req.ParentID = &nilUUID
		return nil
	}
	parent, parentErr := app.taskSvc.GetByShortID(cmd.Context(), flags.parent)

	if parentErr != nil {
		return fmt.Errorf("%s", formatError(parentErr, flags.parent))
	}

	pid := parent.ID
	parentPtr := &pid
	req.ParentID = &parentPtr
	return nil
}

// runResequence handles the --resequence branch. Resolves the parent short_id
// ("root" → nil) and calls TaskService.Resequence.
func (app *App) runResequence(cmd *cobra.Command, parentRef string) error {
	ctx := cmd.Context()

	var parentID *uuid.UUID
	parentLabel := "root"
	if parentRef != "root" {
		parent, parentErr := app.taskSvc.GetByShortID(ctx, parentRef)

		if parentErr != nil {
			return fmt.Errorf("%s", formatError(parentErr, parentRef))
		}

		pid := parent.ID
		parentID = &pid
		parentLabel = parent.ShortID
	}

	var actor *string
	if app.playerID != "" {
		playerID := app.playerID
		actor = &playerID
	}

	rewritten, resequenceErr := app.taskSvc.Resequence(ctx, parentID, actor)

	if resequenceErr != nil {
		return formatMoveError(resequenceErr, parentRef)
	}

	if app.format == "json" {
		type resp struct {
			Rewritten int     `json:"rewritten"`
			ParentID  *string `json:"parent_id"`
		}
		response := resp{Rewritten: rewritten}
		if parentID != nil {
			str := parentID.String()
			response.ParentID = &str
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(response)
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "resequenced %d tasks under parent %s\n", rewritten, parentLabel)
	return err
}

// formatMoveError maps domain sentinel errors that Move / Resequence can emit
// to user-facing messages, preserving the service's wrapped
// ErrOrderGapExhausted text (which carries the parent hint).
func formatMoveError(err error, shortID string) error {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return fmt.Errorf("task not found: %s", shortID)
	case errors.Is(err, domain.ErrConflict):
		return fmt.Errorf("version conflict (task changed since read; pass --version after re-reading)")
	case errors.Is(err, domain.ErrCyclicParent):
		return fmt.Errorf("cannot move: would create a parent cycle")
	case errors.Is(err, domain.ErrOrderGapExhausted):
		return fmt.Errorf("%s", err.Error())
	}
	return err
}
