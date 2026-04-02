package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/germanamz/tusk/internal/domain"
)

// formatPriority converts a priority int (0-4) to a single display character.
func formatPriority(p int) string {
	switch p {
	case 1:
		return "L"
	case 2:
		return "M"
	case 3:
		return "H"
	case 4:
		return "U"
	default:
		return "-"
	}
}

// formatAge converts a creation time to a human-readable relative age string.
func formatAge(created time.Time) string {
	d := time.Since(created)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
	}
}

// taskJSON is the JSON serialization format for a task.
// Field names use snake_case to match the domain model.
type taskJSON struct {
	ID             string         `json:"id"`
	ShortID        string         `json:"short_id"`
	ParentID       *string        `json:"parent_id,omitempty"`
	ProjectID      *string        `json:"project_id,omitempty"`
	Title          string         `json:"title"`
	Description    string         `json:"description"`
	Status         string         `json:"status"`
	Priority       int            `json:"priority"`
	Version        int            `json:"version"`
	DueAt          *string        `json:"due_at,omitempty"`
	WaitUntil      *string        `json:"wait_until,omitempty"`
	RecurrenceRule *string        `json:"recurrence_rule,omitempty"`
	UDA            map[string]any `json:"uda,omitempty"`
	CreatedAt      string         `json:"created_at"`
	ModifiedAt     string         `json:"modified_at"`
}

func toTaskJSON(t *domain.Task) taskJSON {
	tj := taskJSON{
		ID:          t.ID.String(),
		ShortID:     t.ShortID,
		Title:       t.Title,
		Description: t.Description,
		Status:      t.Status,
		Priority:    t.Priority,
		Version:     t.Version,
		UDA:         t.UDA,
		CreatedAt:   t.CreatedAt.Format(time.RFC3339),
		ModifiedAt:  t.ModifiedAt.Format(time.RFC3339),
	}
	if t.ParentID != nil {
		s := t.ParentID.String()
		tj.ParentID = &s
	}
	if t.ProjectID != nil {
		s := t.ProjectID.String()
		tj.ProjectID = &s
	}
	if t.DueAt != nil {
		s := t.DueAt.Format(time.RFC3339)
		tj.DueAt = &s
	}
	if t.WaitUntil != nil {
		s := t.WaitUntil.Format(time.RFC3339)
		tj.WaitUntil = &s
	}
	tj.RecurrenceRule = t.RecurrenceRule
	return tj
}

// renderTaskList writes a list of tasks to w in the given format.
// For "text", it renders a fixed-width table. For "json", it renders a JSON array.
// If the list is empty and format is "text", nothing is written.
func renderTaskList(w io.Writer, tasks []*domain.Task, format string) error {
	if format == "json" {
		items := make([]taskJSON, len(tasks))
		for i, t := range tasks {
			items[i] = toTaskJSON(t)
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	if len(tasks) == 0 {
		return nil
	}

	fmt.Fprintf(w, "%-8s %-9s %-4s %-5s %s\n", "ID", "Status", "Pri", "Age", "Title")
	for _, t := range tasks {
		fmt.Fprintf(w, "%-8s %-9s %-4s %-5s %s\n",
			t.ShortID,
			t.Status,
			formatPriority(t.Priority),
			formatAge(t.CreatedAt),
			t.Title,
		)
	}
	return nil
}
