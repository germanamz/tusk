package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"charm.land/glamour/v2"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/filter"
	"github.com/germanamz/tusk/service"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// buildNoteCmd creates the `tusk note` subcommand group.
func (app *App) buildNoteCmd() *cobra.Command {
	noteCmd := &cobra.Command{
		Use:   "note",
		Short: "Player notebook — create, archive, and list notes",
	}

	addCmd := &cobra.Command{
		Use:   "add <body> [project=<name>] [meta.key=value...]",
		Short: "Create a note",
		Long: `Create a note in a project, optionally scoped to a task.

The body may be literal text, an @./path reference, or @- for stdin
(same conventions as task create/modify). Project defaults to the
built-in "_default" project when project=<name> is not supplied.

Arbitrary metadata is namespaced under meta. — e.g., meta.topic=auth.
Bare key=value tokens that are not reserved (project=, task=) are
rejected to surface typos.`,
		Args: cobra.MinimumNArgs(1),
		RunE: app.runNoteAdd,
	}
	addCmd.Flags().String("task", "", "attach the note to a task by short ID")

	archiveCmd := &cobra.Command{
		Use:   "archive <note_id_or_prefix>",
		Short: "Archive a note",
		Long: `Archive a note by its UUID or an 8+ character UUID prefix.

Only the note's author can archive it.`,
		Args: cobra.ExactArgs(1),
		RunE: app.runNoteArchive,
	}

	listCmd := &cobra.Command{
		Use:   "list [project=<name>]",
		Short: "List notes within the trailing window",
		Long: `List notes in a project, newest first, respecting the trailing window.

By default shows the caller's own notes only. --all-players shows every
player's notes within the same window; use one of the filter forms below
to scope to a specific player. --task <short_id> restricts to notes
attached to that task.

Filter by player using either:
  --filter-player <id>     flag form
  player=<id>              inline token (symmetric with project=<name>)

Use --all-players to show every player's notes; it cannot be combined
with either filter form above.

The window size is resolved through the chain:
  --window flag → player DB setting → project config → global config → 20`,
		Args: cobra.ArbitraryArgs,
		RunE: app.runNoteList,
	}
	listCmd.Flags().String("task", "", "show notes attached to a task by short ID")
	listCmd.Flags().Bool("all-players", false, "show notes from every player")
	listCmd.Flags().String("filter-player", "", "show notes from a specific player (inline alias: player=<id>)")
	listCmd.Flags().Int("window", 0, "override trailing window size (>0 to override)")
	listCmd.Flags().String("since", "", "only show notes newer than this relative duration (e.g. 7d, 24h)")
	listCmd.Flags().Bool("archived", false, "include archived notes")

	noteCmd.AddCommand(addCmd)
	noteCmd.AddCommand(archiveCmd)
	noteCmd.AddCommand(listCmd)
	return noteCmd
}

func (app *App) runNoteAdd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	if app.playerID == "" {
		return fmt.Errorf("--player flag is required for note add")
	}
	if err := app.ensurePlayer(ctx); err != nil {
		return err
	}

	rawBody := args[0]

	projectName := service.DefaultProjectName
	metadata := map[string]any{}
	for _, tok := range args[1:] {
		if tok == "" {
			continue
		}
		key, value, hasEq := strings.Cut(tok, "=")
		if !hasEq {
			return fmt.Errorf("unexpected token %q on note add (expected key=value)", tok)
		}
		switch {
		case key == "project":
			if value == "" {
				return fmt.Errorf("project value must not be empty")
			}
			projectName = value
		case strings.HasPrefix(key, "meta."):
			metaKey := strings.TrimPrefix(key, "meta.")
			if metaKey == "" {
				return fmt.Errorf("metadata key must not be empty")
			}
			metadata[metaKey] = value
		default:
			return fmt.Errorf("unknown field %q on note add (metadata keys must be prefixed with meta.)", key)
		}
	}

	var stdinFile *os.File
	if file, ok := cmd.InOrStdin().(*os.File); ok {
		stdinFile = file
	}
	state := &expandState{}

	bodyErr := error(nil)
	body, bodyErr := app.expandRefsWithState(rawBody, stdinFile, state)

	if bodyErr != nil {
		return bodyErr
	}

	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("note body must not be empty")
	}

	project, projectErr := app.projectSvc.GetByName(ctx, projectName)

	if projectErr != nil {
		return fmt.Errorf("resolving project %q: %w", projectName, projectErr)
	}

	var taskID *uuid.UUID
	if taskShort, _ := cmd.Flags().GetString("task"); taskShort != "" {
		task, taskErr := app.taskSvc.GetByShortID(ctx, taskShort)

		if taskErr != nil {
			return fmt.Errorf("resolving task %q: %w", taskShort, taskErr)
		}

		id := task.ID
		taskID = &id
	}

	note := &domain.Note{
		ProjectID: project.ID,
		PlayerID:  app.playerID,
		TaskID:    taskID,
		Body:      body,
		Metadata:  metadata,
	}
	if createErr := app.noteSvc.Create(ctx, note); createErr != nil {
		return fmt.Errorf("creating note: %w", createErr)
	}

	return app.renderNoteResult(cmd, "Created", note)
}

func (app *App) runNoteArchive(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	if app.playerID == "" {
		return fmt.Errorf("--player flag is required for note archive")
	}
	if err := app.ensurePlayer(ctx); err != nil {
		return err
	}

	token := strings.ToLower(strings.TrimSpace(args[0]))
	id, resolveErr := app.resolveNoteID(ctx, token)

	if resolveErr != nil {
		return resolveErr
	}

	if archiveErr := app.noteSvc.Archive(ctx, id, app.playerID); archiveErr != nil {
		return fmt.Errorf("archiving note: %w", archiveErr)
	}

	archived, getErr := app.noteSvc.GetByID(ctx, id)

	if getErr != nil {
		return app.renderNoteArchivedBare(cmd, id)
	}

	return app.renderNoteResult(cmd, "Archived", archived)
}

// resolveNoteID accepts either a full UUID or a prefix of at least 8
// characters and returns the matching note's UUID. Prefix ambiguity is
// reported with the candidate IDs so the caller can disambiguate.
func (app *App) resolveNoteID(ctx context.Context, token string) (uuid.UUID, error) {
	if id, err := uuid.Parse(token); err == nil {
		return id, nil
	}
	if len(token) < 8 {
		return uuid.Nil, fmt.Errorf("note id prefix must be at least 8 characters, got %d", len(token))
	}
	matches, findErr := app.noteSvc.FindByIDPrefix(ctx, token)

	if findErr != nil {
		return uuid.Nil, fmt.Errorf("resolving note id: %w", findErr)
	}

	switch len(matches) {
	case 0:
		return uuid.Nil, fmt.Errorf("no note matches id prefix %q", token)
	case 1:
		return matches[0].ID, nil
	default:
		ids := make([]string, 0, len(matches))
		for _, match := range matches {
			ids = append(ids, match.ID.String())
		}
		return uuid.Nil, fmt.Errorf("note id prefix %q is ambiguous: matches %s", token, strings.Join(ids, ", "))
	}
}

// noteJSON is the JSON serialization format for a note.
type noteJSON struct {
	ID         string         `json:"id"`
	ProjectID  string         `json:"project_id"`
	PlayerID   string         `json:"player_id"`
	TaskID     *string        `json:"task_id,omitempty"`
	Body       string         `json:"body"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	CreatedAt  string         `json:"created_at"`
	ArchivedAt *string        `json:"archived_at,omitempty"`
}

func toNoteJSON(note *domain.Note) noteJSON {
	result := noteJSON{
		ID:        note.ID.String(),
		ProjectID: note.ProjectID.String(),
		PlayerID:  note.PlayerID,
		Body:      note.Body,
		Metadata:  note.Metadata,
		CreatedAt: note.CreatedAt.Format(time.RFC3339),
	}
	if note.TaskID != nil {
		str := note.TaskID.String()
		result.TaskID = &str
	}
	if note.ArchivedAt != nil {
		str := note.ArchivedAt.Format(time.RFC3339)
		result.ArchivedAt = &str
	}
	return result
}

func (app *App) renderNoteResult(cmd *cobra.Command, action string, note *domain.Note) error {
	writer := cmd.OutOrStdout()
	if app.format == "json" {
		enc := json.NewEncoder(writer)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"action": strings.ToLower(action),
			"note":   toNoteJSON(note),
		})
	}
	shortID := note.ID.String()[:8]
	archivedMarker := ""
	if note.ArchivedAt != nil {
		archivedMarker = " [archived]"
	}
	_, err := fmt.Fprintf(writer, "%s note %s%s\n", action, shortID, archivedMarker)
	return err
}

func (app *App) renderNoteArchivedBare(cmd *cobra.Command, id uuid.UUID) error {
	writer := cmd.OutOrStdout()
	if app.format == "json" {
		enc := json.NewEncoder(writer)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"action": "archived",
			"id":     id.String(),
		})
	}
	_, err := fmt.Fprintf(writer, "Archived note %s\n", id.String()[:8])
	return err
}

func (app *App) runNoteList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	if app.playerID == "" {
		return fmt.Errorf("--player flag is required for note list (caller identity)")
	}
	if err := app.ensurePlayer(ctx); err != nil {
		return err
	}

	projectName := service.DefaultProjectName
	inlinePlayer := ""
	haveInlinePlayer := false
	for _, tok := range args {
		if tok == "" {
			continue
		}
		key, value, hasEq := strings.Cut(tok, "=")
		if !hasEq {
			return fmt.Errorf("unexpected token %q on note list (expected key=value)", tok)
		}
		switch key {
		case "project":
			if value == "" {
				return fmt.Errorf("project value must not be empty")
			}
			projectName = value
		case "player":
			inlinePlayer = value
			haveInlinePlayer = true
		default:
			return fmt.Errorf("unknown field %q on note list", key)
		}
	}
	project, projectErr := app.projectSvc.GetByName(ctx, projectName)

	if projectErr != nil {
		return fmt.Errorf("resolving project %q: %w", projectName, projectErr)
	}

	allPlayers, _ := cmd.Flags().GetBool("all-players")
	filterPlayer, _ := cmd.Flags().GetString("filter-player")
	if haveInlinePlayer {
		if filterPlayer != "" {
			return fmt.Errorf("player filter set via both --filter-player and player=; choose one")
		}
		filterPlayer = inlinePlayer
	}
	if allPlayers && filterPlayer != "" {
		return fmt.Errorf("--all-players cannot be combined with a specific player filter")
	}

	targetPlayer := app.playerID
	if allPlayers {
		targetPlayer = ""
	} else if filterPlayer != "" {
		targetPlayer = filterPlayer
	}

	var taskID *uuid.UUID
	if taskShort, _ := cmd.Flags().GetString("task"); taskShort != "" {
		task, taskErr := app.taskSvc.GetByShortID(ctx, taskShort)

		if taskErr != nil {
			return fmt.Errorf("resolving task %q: %w", taskShort, taskErr)
		}

		id := task.ID
		taskID = &id
	}

	var windowOverride *int
	if windowVal, _ := cmd.Flags().GetInt("window"); windowVal > 0 {
		windowOverride = &windowVal
	} else if windowVal < 0 {
		return fmt.Errorf("--window must be positive, got %d", windowVal)
	}

	var since *time.Time
	if sinceStr, _ := cmd.Flags().GetString("since"); sinceStr != "" {
		duration, durationErr := filter.ParseRelativeDuration(sinceStr)

		if durationErr != nil {
			return fmt.Errorf("parsing --since: %w", durationErr)
		}

		sinceTime := time.Now().UTC().Add(-duration)
		since = &sinceTime
	}

	includeArchived, _ := cmd.Flags().GetBool("archived")

	notes, listErr := app.noteSvc.List(ctx, service.NoteListParams{
		ProjectID:       project.ID,
		PlayerID:        targetPlayer,
		CallerPlayerID:  app.playerID,
		TaskID:          taskID,
		Since:           since,
		IncludeArchived: includeArchived,
		WindowOverride:  windowOverride,
	})

	if listErr != nil {
		return fmt.Errorf("listing notes: %w", listErr)
	}

	return app.renderNoteList(cmd, notes)
}

func (app *App) renderNoteList(cmd *cobra.Command, notes []*domain.Note) error {
	writer := cmd.OutOrStdout()
	if app.format == "json" {
		enc := json.NewEncoder(writer)
		enc.SetIndent("", "  ")
		jsonItems := make([]noteJSON, 0, len(notes))
		for _, note := range notes {
			jsonItems = append(jsonItems, toNoteJSON(note))
		}
		return enc.Encode(jsonItems)
	}

	if len(notes) == 0 {
		_, err := fmt.Fprintln(writer, "No notes.")
		return err
	}

	bodyRenderer := app.noteBodyRenderer()
	for index, note := range notes {
		if index > 0 {
			if _, err := fmt.Fprintln(writer); err != nil {
				return err
			}
		}
		if err := app.renderNoteEntry(writer, note, bodyRenderer); err != nil {
			return err
		}
	}
	return nil
}

func (app *App) renderNoteEntry(writer io.Writer, note *domain.Note, body func(string) string) error {
	shortID := note.ID.String()[:8]
	taskPart := ""
	if note.TaskID != nil {
		taskPart = fmt.Sprintf("  task=%s", note.TaskID.String()[:8])
	}
	archivedPart := ""
	if note.ArchivedAt != nil {
		archivedPart = "  [archived]"
	}
	header := fmt.Sprintf("● %s  %s  %s%s%s\n",
		shortID,
		note.PlayerID,
		note.CreatedAt.Local().Format("2006-01-02 15:04"),
		taskPart,
		archivedPart,
	)
	if _, err := fmt.Fprint(writer, header); err != nil {
		return err
	}

	rendered := body(note.Body)
	for line := range strings.SplitSeq(strings.TrimRight(rendered, "\n"), "\n") {
		if _, err := fmt.Fprintf(writer, "  %s\n", line); err != nil {
			return err
		}
	}

	if len(note.Metadata) > 0 {
		keys := make([]string, 0, len(note.Metadata))
		for key := range note.Metadata {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, fmt.Sprintf("meta.%s=%v", key, note.Metadata[key]))
		}
		if _, err := fmt.Fprintf(writer, "  %s\n", strings.Join(parts, "  ")); err != nil {
			return err
		}
	}
	return nil
}

// noteBodyRenderer returns a function that renders a note body as styled
// markdown if color is enabled, or returns the raw body otherwise.
func (app *App) noteBodyRenderer() func(string) string {
	if !app.colorEnabled() {
		return func(str string) string { return str }
	}
	glamourRenderer, glamourErr := glamour.NewTermRenderer(glamour.WithWordWrap(100))

	if glamourErr != nil {
		return func(str string) string { return str }
	}

	return func(str string) string {
		out, renderErr := glamourRenderer.Render(str)

		if renderErr != nil {
			return str
		}

		return out
	}
}
