package tui

import (
	"fmt"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/spf13/cobra"
)

// buildTagCmd creates the `tusk tag` command group with its subcommands.
func (a *App) buildTagCmd() *cobra.Command {
	tagCmd := &cobra.Command{
		Use:   "tag",
		Short: "Manage tags",
	}

	// tusk tag list
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all tags",
		Args:  cobra.NoArgs,
		RunE:  a.runTagList,
	}
	listCmd.Flags().String("color", "", `filter by color: "any", "none", or a hex value`)
	listCmd.Flags().Bool("usage", false, "show task count per tag")
	tagCmd.AddCommand(listCmd)

	// tusk tag create <name>
	createCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new tag",
		Args:  cobra.ExactArgs(1),
		RunE:  a.runTagCreate,
	}
	createCmd.Flags().String("color", "", "tag color as hex (e.g. #ff0000)")
	tagCmd.AddCommand(createCmd)

	// tusk tag modify <name>
	modifyCmd := &cobra.Command{
		Use:   "modify <name>",
		Short: "Modify a tag",
		Args:  cobra.ExactArgs(1),
		RunE:  a.runTagModify,
	}
	modifyCmd.Flags().String("color", "", `tag color as hex, or "" to clear`)
	tagCmd.AddCommand(modifyCmd)

	// tusk tag delete <name>
	tagCmd.AddCommand(&cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a tag (must not be assigned to any tasks)",
		Args:  cobra.ExactArgs(1),
		RunE:  a.runTagDelete,
	})

	// tusk tag rename <old> <new>
	tagCmd.AddCommand(&cobra.Command{
		Use:   "rename <old> <new>",
		Short: "Rename a tag",
		Args:  cobra.ExactArgs(2),
		RunE:  a.runTagRename,
	})

	return tagCmd
}

func (a *App) runTagList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	showUsage, _ := cmd.Flags().GetBool("usage")

	tags, err := a.tagSvc.ListWithUsage(ctx)
	if err != nil {
		return err
	}

	// Apply --color filter if provided
	if cmd.Flags().Changed("color") {
		colorFilter, _ := cmd.Flags().GetString("color")
		tags = filterTagsByColor(tags, colorFilter)
	}

	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), a.buildDimStatuses())
	return r.renderTagList(tags, showUsage)
}

// filterTagsByColor filters tags based on the color flag value.
// "any" = tags with a color set, "none" = tags without a color,
// anything else = exact color match.
func filterTagsByColor(tags []domain.TagWithUsage, filter string) []domain.TagWithUsage {
	result := make([]domain.TagWithUsage, 0, len(tags))
	for _, tw := range tags {
		switch filter {
		case "any":
			if tw.Tag.Color != nil {
				result = append(result, tw)
			}
		case "none":
			if tw.Tag.Color == nil {
				result = append(result, tw)
			}
		default:
			if tw.Tag.Color != nil && *tw.Tag.Color == filter {
				result = append(result, tw)
			}
		}
	}
	return result
}

func (a *App) runTagCreate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]

	var color *string
	if cmd.Flags().Changed("color") {
		c, _ := cmd.Flags().GetString("color")
		color = &c
	}

	tag, err := a.tagSvc.Create(ctx, name, color)
	if err != nil {
		return err
	}
	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), a.buildDimStatuses())
	return r.renderTagResult("Created", tag)
}

func (a *App) runTagModify(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]

	if !cmd.Flags().Changed("color") {
		return fmt.Errorf("at least one flag must be specified (--color)")
	}

	colorStr, _ := cmd.Flags().GetString("color")
	var color *string
	if colorStr != "" {
		color = &colorStr
	}
	// If colorStr is empty string, color stays nil — this clears the color

	tag, err := a.tagSvc.Modify(ctx, name, color)
	if err != nil {
		return err
	}
	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), a.buildDimStatuses())
	return r.renderTagResult("Modified", tag)
}

func (a *App) runTagDelete(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]

	tag, err := a.tagSvc.Delete(ctx, name)
	if err != nil {
		return err
	}
	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), a.buildDimStatuses())
	return r.renderTagResult("Deleted", tag)
}

func (a *App) runTagRename(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	oldName, newName := args[0], args[1]

	tag, err := a.tagSvc.Rename(ctx, oldName, newName)
	if err != nil {
		return err
	}

	if a.format == "json" {
		r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), a.buildDimStatuses())
		return r.renderTagResult("Renamed", tag)
	}
	_, fmtErr := fmt.Fprintf(cmd.OutOrStdout(), "Renamed tag %s to %s\n", oldName, newName)
	return fmtErr
}
