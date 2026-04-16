package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/service"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// buildNoteCmd creates the `tusk note` subcommand group.
// list is added in Phase 4 — do not register it here.
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

	noteCmd.AddCommand(addCmd)
	noteCmd.AddCommand(archiveCmd)
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
