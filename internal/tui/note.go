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
func (a *App) buildNoteCmd() *cobra.Command {
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
		RunE: a.runNoteAdd,
	}
	addCmd.Flags().String("task", "", "attach the note to a task by short ID")

	archiveCmd := &cobra.Command{
		Use:   "archive <note_id_or_prefix>",
		Short: "Archive a note",
		Long: `Archive a note by its UUID or an 8+ character UUID prefix.

Only the note's author can archive it.`,
		Args: cobra.ExactArgs(1),
		RunE: a.runNoteArchive,
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
		RunE: a.runNoteList,
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

func (a *App) runNoteAdd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	if a.playerID == "" {
		return fmt.Errorf("--player flag is required for note add")
	}
	if err := a.ensurePlayer(ctx); err != nil {
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
	if f, ok := cmd.InOrStdin().(*os.File); ok {
		stdinFile = f
	}
	state := &expandState{}

	body, err := a.expandRefsWithState(rawBody, stdinFile, state)
	if err != nil {
		return err
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("note body must not be empty")
	}

	project, err := a.projectSvc.GetByName(ctx, projectName)
	if err != nil {
		return fmt.Errorf("resolving project %q: %w", projectName, err)
	}

	var taskID *uuid.UUID
	if taskShort, _ := cmd.Flags().GetString("task"); taskShort != "" {
		task, err := a.taskSvc.GetByShortID(ctx, taskShort)
		if err != nil {
			return fmt.Errorf("resolving task %q: %w", taskShort, err)
		}
		id := task.ID
		taskID = &id
	}

	note := &domain.Note{
		ProjectID: project.ID,
		PlayerID:  a.playerID,
		TaskID:    taskID,
		Body:      body,
		Metadata:  metadata,
	}
	if err := a.noteSvc.Create(ctx, note); err != nil {
		return fmt.Errorf("creating note: %w", err)
	}

	return a.renderNoteResult(cmd, "Created", note)
}

func (a *App) runNoteArchive(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	if a.playerID == "" {
		return fmt.Errorf("--player flag is required for note archive")
	}
	if err := a.ensurePlayer(ctx); err != nil {
		return err
	}

	token := strings.ToLower(strings.TrimSpace(args[0]))
	id, err := a.resolveNoteID(ctx, token)
	if err != nil {
		return err
	}

	if err := a.noteSvc.Archive(ctx, id, a.playerID); err != nil {
		return fmt.Errorf("archiving note: %w", err)
	}

	archived, err := a.noteSvc.GetByID(ctx, id)
	if err != nil {
		return a.renderNoteArchivedBare(cmd, id)
	}
	return a.renderNoteResult(cmd, "Archived", archived)
}

// resolveNoteID accepts either a full UUID or a prefix of at least 8
// characters and returns the matching note's UUID. Prefix ambiguity is
// reported with the candidate IDs so the caller can disambiguate.
func (a *App) resolveNoteID(ctx context.Context, token string) (uuid.UUID, error) {
	if id, err := uuid.Parse(token); err == nil {
		return id, nil
	}
	if len(token) < 8 {
		return uuid.Nil, fmt.Errorf("note id prefix must be at least 8 characters, got %d", len(token))
	}
	matches, err := a.noteSvc.FindByIDPrefix(ctx, token)
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolving note id: %w", err)
	}
	switch len(matches) {
	case 0:
		return uuid.Nil, fmt.Errorf("no note matches id prefix %q", token)
	case 1:
		return matches[0].ID, nil
	default:
		ids := make([]string, 0, len(matches))
		for _, n := range matches {
			ids = append(ids, n.ID.String())
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

func toNoteJSON(n *domain.Note) noteJSON {
	j := noteJSON{
		ID:        n.ID.String(),
		ProjectID: n.ProjectID.String(),
		PlayerID:  n.PlayerID,
		Body:      n.Body,
		Metadata:  n.Metadata,
		CreatedAt: n.CreatedAt.Format(time.RFC3339),
	}
	if n.TaskID != nil {
		s := n.TaskID.String()
		j.TaskID = &s
	}
	if n.ArchivedAt != nil {
		s := n.ArchivedAt.Format(time.RFC3339)
		j.ArchivedAt = &s
	}
	return j
}

func (a *App) renderNoteResult(cmd *cobra.Command, action string, note *domain.Note) error {
	w := cmd.OutOrStdout()
	if a.format == "json" {
		enc := json.NewEncoder(w)
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
	_, err := fmt.Fprintf(w, "%s note %s%s\n", action, shortID, archivedMarker)
	return err
}

func (a *App) renderNoteArchivedBare(cmd *cobra.Command, id uuid.UUID) error {
	w := cmd.OutOrStdout()
	if a.format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"action": "archived",
			"id":     id.String(),
		})
	}
	_, err := fmt.Fprintf(w, "Archived note %s\n", id.String()[:8])
	return err
}

func (a *App) runNoteList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	if a.playerID == "" {
		return fmt.Errorf("--player flag is required for note list (caller identity)")
	}
	if err := a.ensurePlayer(ctx); err != nil {
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
	project, err := a.projectSvc.GetByName(ctx, projectName)
	if err != nil {
		return fmt.Errorf("resolving project %q: %w", projectName, err)
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

	targetPlayer := a.playerID
	if allPlayers {
		targetPlayer = ""
	} else if filterPlayer != "" {
		targetPlayer = filterPlayer
	}

	var taskID *uuid.UUID
	if taskShort, _ := cmd.Flags().GetString("task"); taskShort != "" {
		task, err := a.taskSvc.GetByShortID(ctx, taskShort)
		if err != nil {
			return fmt.Errorf("resolving task %q: %w", taskShort, err)
		}
		id := task.ID
		taskID = &id
	}

	var windowOverride *int
	if w, _ := cmd.Flags().GetInt("window"); w > 0 {
		windowOverride = &w
	} else if w < 0 {
		return fmt.Errorf("--window must be positive, got %d", w)
	}

	var since *time.Time
	if sinceStr, _ := cmd.Flags().GetString("since"); sinceStr != "" {
		d, err := filter.ParseRelativeDuration(sinceStr)
		if err != nil {
			return fmt.Errorf("parsing --since: %w", err)
		}
		t := time.Now().UTC().Add(-d)
		since = &t
	}

	includeArchived, _ := cmd.Flags().GetBool("archived")

	notes, err := a.noteSvc.List(ctx, service.NoteListParams{
		ProjectID:       project.ID,
		PlayerID:        targetPlayer,
		CallerPlayerID:  a.playerID,
		TaskID:          taskID,
		Since:           since,
		IncludeArchived: includeArchived,
		WindowOverride:  windowOverride,
	})
	if err != nil {
		return fmt.Errorf("listing notes: %w", err)
	}

	return a.renderNoteList(cmd, notes)
}

func (a *App) renderNoteList(cmd *cobra.Command, notes []*domain.Note) error {
	w := cmd.OutOrStdout()
	if a.format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		jsonItems := make([]noteJSON, 0, len(notes))
		for _, n := range notes {
			jsonItems = append(jsonItems, toNoteJSON(n))
		}
		return enc.Encode(jsonItems)
	}

	if len(notes) == 0 {
		_, err := fmt.Fprintln(w, "No notes.")
		return err
	}

	bodyRenderer := a.noteBodyRenderer()
	for i, n := range notes {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if err := a.renderNoteEntry(w, n, bodyRenderer); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) renderNoteEntry(w io.Writer, n *domain.Note, body func(string) string) error {
	shortID := n.ID.String()[:8]
	taskPart := ""
	if n.TaskID != nil {
		taskPart = fmt.Sprintf("  task=%s", n.TaskID.String()[:8])
	}
	archivedPart := ""
	if n.ArchivedAt != nil {
		archivedPart = "  [archived]"
	}
	header := fmt.Sprintf("● %s  %s  %s%s%s\n",
		shortID,
		n.PlayerID,
		n.CreatedAt.Local().Format("2006-01-02 15:04"),
		taskPart,
		archivedPart,
	)
	if _, err := fmt.Fprint(w, header); err != nil {
		return err
	}

	rendered := body(n.Body)
	for line := range strings.SplitSeq(strings.TrimRight(rendered, "\n"), "\n") {
		if _, err := fmt.Fprintf(w, "  %s\n", line); err != nil {
			return err
		}
	}

	if len(n.Metadata) > 0 {
		keys := make([]string, 0, len(n.Metadata))
		for k := range n.Metadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("meta.%s=%v", k, n.Metadata[k]))
		}
		if _, err := fmt.Fprintf(w, "  %s\n", strings.Join(parts, "  ")); err != nil {
			return err
		}
	}
	return nil
}

// noteBodyRenderer returns a function that renders a note body as styled
// markdown if color is enabled, or returns the raw body otherwise.
func (a *App) noteBodyRenderer() func(string) string {
	if !a.colorEnabled() {
		return func(s string) string { return s }
	}
	r, err := glamour.NewTermRenderer(glamour.WithWordWrap(100))
	if err != nil {
		return func(s string) string { return s }
	}
	return func(s string) string {
		out, err := r.Render(s)
		if err != nil {
			return s
		}
		return out
	}
}
