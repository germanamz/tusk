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
func (a *App) buildTaskMoveCmd() *cobra.Command {
	f := &moveFlags{}
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
		PreRunE: a.validateMoveFlags(f),
		RunE:    a.runMove(f),
	}
	cmd.Flags().StringVar(&f.before, "before", "", "place task immediately before target short_id")
	cmd.Flags().StringVar(&f.after, "after", "", "place task immediately after target short_id")
	cmd.Flags().BoolVar(&f.first, "first", false, "place task at the head of its (optionally re-homed) sibling group")
	cmd.Flags().BoolVar(&f.last, "last", false, "place task at the tail of its (optionally re-homed) sibling group")
	cmd.Flags().StringVar(&f.parent, "parent", "", "new parent short_id (or \"root\"); only valid with --first or --last")
	cmd.Flags().StringVar(&f.resequence, "resequence", "", "rewrite the entire sibling group under this parent short_id (or \"root\") to dense integer orders")
	cmd.Flags().IntVar(&f.version, "version", 0, "expected task version (optimistic lock); fetched automatically when omitted")
	return cmd
}

// validateMoveFlags enforces the mutual-exclusion rules described in the
// command's long help. Positional arg is required for every mode except
// --resequence, which takes its parent through the flag.
func (a *App) validateMoveFlags(f *moveFlags) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		positions := 0
		if f.before != "" {
			positions++
		}
		if f.after != "" {
			positions++
		}
		if f.first {
			positions++
		}
		if f.last {
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

		if f.parent != "" && !f.first && !f.last {
			return fmt.Errorf("--parent is only valid with --first or --last")
		}

		if resequenceSet {
			if len(args) > 0 {
				return fmt.Errorf("--resequence takes its parent through the flag; do not pass a positional short_id")
			}
			if f.parent != "" {
				return fmt.Errorf("--parent cannot be combined with --resequence")
			}
			if f.version != 0 {
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
func (a *App) runMove(f *moveFlags) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		if cmd.Flags().Changed("resequence") {
			return a.runResequence(cmd, f.resequence)
		}

		shortID := args[0]
		current, err := a.taskSvc.GetByShortID(ctx, shortID)
		if err != nil {
			return fmt.Errorf("%s", formatError(err, shortID))
		}

		version := f.version
		if version == 0 {
			version = current.Version
		}

		req := service.MoveRequest{
			TaskID:  current.ID,
			Version: version,
		}
		if a.playerID != "" {
			p := a.playerID
			req.ActorID = &p
		}

		switch {
		case f.before != "":
			target, err := a.taskSvc.GetByShortID(ctx, f.before)
			if err != nil {
				return fmt.Errorf("%s", formatError(err, f.before))
			}
			req.Position = service.MovePositionBefore
			tid := target.ID
			req.TargetID = &tid
		case f.after != "":
			target, err := a.taskSvc.GetByShortID(ctx, f.after)
			if err != nil {
				return fmt.Errorf("%s", formatError(err, f.after))
			}
			req.Position = service.MovePositionAfter
			tid := target.ID
			req.TargetID = &tid
		case f.first:
			req.Position = service.MovePositionFirst
			if err := a.setMoveParent(cmd, f, &req); err != nil {
				return err
			}
		case f.last:
			req.Position = service.MovePositionLast
			if err := a.setMoveParent(cmd, f, &req); err != nil {
				return err
			}
		}

		updated, err := a.taskSvc.Move(ctx, req)
		if err != nil {
			return formatMoveError(err, shortID)
		}
		r := a.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
		return r.renderMutationResult("Moved", updated, nil)
	}
}

// setMoveParent translates the --parent flag (including the "root" sentinel)
// into a tristate **uuid.UUID on the MoveRequest. Only invoked in first/last
// branches where --parent is meaningful.
func (a *App) setMoveParent(cmd *cobra.Command, f *moveFlags, req *service.MoveRequest) error {
	if !cmd.Flags().Changed("parent") {
		return nil
	}
	if f.parent == "root" {
		var nilUUID *uuid.UUID
		req.ParentID = &nilUUID
		return nil
	}
	parent, err := a.taskSvc.GetByShortID(cmd.Context(), f.parent)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, f.parent))
	}
	pid := parent.ID
	pp := &pid
	req.ParentID = &pp
	return nil
}

// runResequence handles the --resequence branch. Resolves the parent short_id
// ("root" → nil) and calls TaskService.Resequence.
func (a *App) runResequence(cmd *cobra.Command, parentRef string) error {
	ctx := cmd.Context()

	var parentID *uuid.UUID
	parentLabel := "root"
	if parentRef != "root" {
		parent, err := a.taskSvc.GetByShortID(ctx, parentRef)
		if err != nil {
			return fmt.Errorf("%s", formatError(err, parentRef))
		}
		pid := parent.ID
		parentID = &pid
		parentLabel = parent.ShortID
	}

	var actor *string
	if a.playerID != "" {
		p := a.playerID
		actor = &p
	}

	rewritten, err := a.taskSvc.Resequence(ctx, parentID, actor)
	if err != nil {
		return formatMoveError(err, parentRef)
	}

	if a.format == "json" {
		type resp struct {
			Rewritten int     `json:"rewritten"`
			ParentID  *string `json:"parent_id"`
		}
		r := resp{Rewritten: rewritten}
		if parentID != nil {
			s := parentID.String()
			r.ParentID = &s
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "resequenced %d tasks under parent %s\n", rewritten, parentLabel)
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
