package tui

import (
	"fmt"

	"github.com/germanamz/tusk/domain"
	"github.com/spf13/cobra"
)

// buildTagCmd creates the `tusk tag` command group with its subcommands.
func (app *App) buildTagCmd() *cobra.Command {
	tagCmd := &cobra.Command{
		Use:   "tag",
		Short: "Manage tags",
	}

	// tusk tag list
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all tags",
		Args:  cobra.NoArgs,
		RunE:  app.runTagList,
	}
	listCmd.Flags().String("color", "", `filter by color: "any", "none", or a hex value`)
	listCmd.Flags().Bool("usage", false, "show task count per tag")
	tagCmd.AddCommand(listCmd)

	// tusk tag create <name>
	createCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new tag",
		Args:  cobra.ExactArgs(1),
		RunE:  app.runTagCreate,
	}
	createCmd.Flags().String("color", "", "tag color as hex (e.g. #ff0000)")
	tagCmd.AddCommand(createCmd)

	// tusk tag modify <name>
	modifyCmd := &cobra.Command{
		Use:   "modify <name>",
		Short: "Modify a tag",
		Args:  cobra.ExactArgs(1),
		RunE:  app.runTagModify,
	}
	modifyCmd.Flags().String("color", "", `tag color as hex, or "" to clear`)
	tagCmd.AddCommand(modifyCmd)

	// tusk tag delete <name>
	tagCmd.AddCommand(&cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a tag (must not be assigned to any tasks)",
		Args:  cobra.ExactArgs(1),
		RunE:  app.runTagDelete,
	})

	// tusk tag rename <old> <new>
	tagCmd.AddCommand(&cobra.Command{
		Use:   "rename <old> <new>",
		Short: "Rename a tag",
		Args:  cobra.ExactArgs(2),
		RunE:  app.runTagRename,
	})

	return tagCmd
}

func (app *App) runTagList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	showUsage, _ := cmd.Flags().GetBool("usage")

	tags, err := app.tagSvc.ListWithUsage(ctx)

	if err != nil {
		return err
	}

	// Apply --color filter if provided
	if cmd.Flags().Changed("color") {
		colorFilter, _ := cmd.Flags().GetString("color")
		tags = filterTagsByColor(tags, colorFilter)
	}

	renderer := NewRenderer(cmd.OutOrStdout(), app.format, app.colorEnabled(), nil)
	return renderer.renderTagList(tags, showUsage)
}

// filterTagsByColor filters tags based on the color flag value.
// "any" = tags with a color set, "none" = tags without a color,
// anything else = exact color match.
func filterTagsByColor(tags []domain.TagWithUsage, colorFilter string) []domain.TagWithUsage {
	result := make([]domain.TagWithUsage, 0, len(tags))
	for _, tagWithUsage := range tags {
		switch colorFilter {
		case "any":
			if tagWithUsage.Tag.Color != nil {
				result = append(result, tagWithUsage)
			}
		case "none":
			if tagWithUsage.Tag.Color == nil {
				result = append(result, tagWithUsage)
			}
		default:
			if tagWithUsage.Tag.Color != nil && *tagWithUsage.Tag.Color == colorFilter {
				result = append(result, tagWithUsage)
			}
		}
	}
	return result
}

func (app *App) runTagCreate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]

	var color *string
	if cmd.Flags().Changed("color") {
		colorStr, _ := cmd.Flags().GetString("color")
		color = &colorStr
	}

	tag, err := app.tagSvc.Create(ctx, name, color)

	if err != nil {
		return err
	}

	renderer := NewRenderer(cmd.OutOrStdout(), app.format, app.colorEnabled(), nil)
	return renderer.renderTagResult("Created", tag)
}

func (app *App) runTagModify(cmd *cobra.Command, args []string) error {
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

	tag, err := app.tagSvc.Modify(ctx, name, color)

	if err != nil {
		return err
	}

	renderer := NewRenderer(cmd.OutOrStdout(), app.format, app.colorEnabled(), nil)
	return renderer.renderTagResult("Modified", tag)
}

func (app *App) runTagDelete(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]

	tag, err := app.tagSvc.Delete(ctx, name)

	if err != nil {
		return err
	}

	renderer := NewRenderer(cmd.OutOrStdout(), app.format, app.colorEnabled(), nil)
	return renderer.renderTagResult("Deleted", tag)
}

func (app *App) runTagRename(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	oldName, newName := args[0], args[1]

	tag, err := app.tagSvc.Rename(ctx, oldName, newName)

	if err != nil {
		return err
	}

	renderer := NewRenderer(cmd.OutOrStdout(), app.format, app.colorEnabled(), nil)
	return renderer.renderTagRenameResult(oldName, tag)
}
