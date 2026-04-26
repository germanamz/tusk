// Copyright 2025 German Meza
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/germanamz/tusk/internal/portability"
	"github.com/germanamz/tusk/service"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// ErrImportFailed signals that `tusk import` rejected the dump. The renderer
// has already written every issue to stderr; main suppresses the redundant
// "Error: ..." wrapper line and exits non-zero.
var ErrImportFailed = errors.New("import failed")

// buildImportCmd builds the `tusk import` subcommand.
func (a *App) buildImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import --input <path> [--replace] [--truncate] [--dry-run]",
		Short: "Import a JSON dump into the workspace",
		Long: `Import a JSON workspace dump produced by 'tusk export'. By default
fails on any collision (an entity in the dump whose ID already exists
in the workspace). Use --replace to overwrite collisions row-by-row,
or --replace --truncate to wipe every entity table before applying
the dump (wipe-and-restore mode).

Import preserves IDs, timestamps, and version numbers exactly so a
backup round-trips losslessly. Per-entity events are not emitted —
one workspace_imported event records the import.

Validation runs in a single pass before any writes, collecting every
issue (schema, FK, taxonomy, blocks-cycle, workflow well-formedness,
collision) so you see the full picture in one round-trip.`,
		Example: `  # Import from a file
  tusk import --input /tmp/ws.json

  # Restore over the existing workspace
  tusk import --input /tmp/ws.json --replace

  # Wipe and rehydrate from the dump
  tusk import --input /tmp/ws.json --replace --truncate

  # Validate without writing
  tusk import --input /tmp/ws.json --dry-run

  # Read from stdin
  cat ws.json | tusk import --input -`,
		RunE: a.runImport,
	}
	cmd.Flags().StringP("input", "i", "", `path to read from; "-" for stdin`)
	cmd.Flags().Bool("replace", false, "row-level upsert on collision")
	cmd.Flags().Bool("truncate", false, "wipe every entity table before applying; requires --replace")
	cmd.Flags().Bool("dry-run", false, "validate and report counts; no writes")
	_ = cmd.MarkFlagRequired("input")
	return cmd
}

func (a *App) runImport(cmd *cobra.Command, _ []string) error {
	if a.portabilitySvc == nil {
		return fmt.Errorf("portability service not configured")
	}
	input, err := cmd.Flags().GetString("input")
	if err != nil {
		return err
	}
	replace, err := cmd.Flags().GetBool("replace")
	if err != nil {
		return err
	}
	truncate, err := cmd.Flags().GetBool("truncate")
	if err != nil {
		return err
	}
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return err
	}
	if truncate && !replace {
		return fmt.Errorf("--truncate requires --replace")
	}

	reader, closer, err := a.openImportSource(cmd, input)
	if err != nil {
		return err
	}
	defer closer()

	ws, err := portability.Decode(reader)
	if err != nil {
		var ie *portability.ImportError
		if errors.As(err, &ie) {
			a.renderImportError(cmd.ErrOrStderr(), ie)
			return ErrImportFailed
		}
		return err
	}

	report, err := a.portabilitySvc.Import(cmd.Context(), ws, service.ImportOptions{
		Replace:  replace,
		Truncate: truncate,
		DryRun:   dryRun,
	})
	if err != nil {
		var ie *portability.ImportError
		if errors.As(err, &ie) {
			a.renderImportError(cmd.ErrOrStderr(), ie)
			return ErrImportFailed
		}
		return err
	}
	return a.renderImportReport(cmd.OutOrStdout(), report, dryRun)
}

// openImportSource opens the dump source — a file path or "-" for stdin.
// The returned closer is always safe to call.
func (a *App) openImportSource(cmd *cobra.Command, input string) (io.Reader, func(), error) {
	if input != "-" {
		f, err := os.Open(input)
		if err != nil {
			return nil, func() {}, fmt.Errorf("opening %s: %w", input, err)
		}
		return f, func() { _ = f.Close() }, nil
	}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		return nil, func() {}, fmt.Errorf("--input -: stdin is a terminal; pipe a file or omit the flag")
	}
	return cmd.InOrStdin(), func() {}, nil
}

// importReportJSON mirrors service.ImportReport with snake_case field names.
type importReportJSON struct {
	Workflows   int    `json:"workflows"`
	Projects    int    `json:"projects"`
	Players     int    `json:"players"`
	Tags        int    `json:"tags"`
	Tasks       int    `json:"tasks"`
	Relations   int    `json:"relations"`
	Annotations int    `json:"annotations"`
	Notes       int    `json:"notes"`
	Events      int    `json:"events"`
	Replaced    int    `json:"replaced"`
	Truncated   bool   `json:"truncated"`
	DryRun      bool   `json:"dry_run"`
	EventID     string `json:"event_id,omitempty"`
}

func (a *App) renderImportReport(w io.Writer, r *service.ImportReport, dryRun bool) error {
	if a.format == "json" {
		payload := importReportJSON{
			Workflows:   r.Workflows,
			Projects:    r.Projects,
			Players:     r.Players,
			Tags:        r.Tags,
			Tasks:       r.Tasks,
			Relations:   r.Relations,
			Annotations: r.Annotations,
			Notes:       r.Notes,
			Events:      r.Events,
			Replaced:    r.Replaced,
			Truncated:   r.Truncated,
			DryRun:      dryRun,
		}
		if r.EventID.String() != "00000000-0000-0000-0000-000000000000" {
			payload.EventID = r.EventID.String()
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}

	if dryRun {
		if _, err := fmt.Fprintln(w, "dry-run: no writes performed"); err != nil {
			return err
		}
	}
	if r.Truncated {
		if _, err := fmt.Fprintln(w, "truncated: yes"); err != nil {
			return err
		}
	}
	kinds := []struct {
		name  string
		count int
	}{
		{"workflows", r.Workflows},
		{"projects", r.Projects},
		{"players", r.Players},
		{"tags", r.Tags},
		{"tasks", r.Tasks},
		{"relations", r.Relations},
		{"annotations", r.Annotations},
		{"notes", r.Notes},
		{"events", r.Events},
	}
	for _, k := range kinds {
		if _, err := fmt.Fprintf(w, "%-12s %d\n", k.name+":", k.count); err != nil {
			return err
		}
	}
	if r.Replaced > 0 {
		if _, err := fmt.Fprintf(w, "%-12s %d\n", "replaced:", r.Replaced); err != nil {
			return err
		}
	}
	if r.EventID.String() != "00000000-0000-0000-0000-000000000000" {
		if _, err := fmt.Fprintf(w, "%-12s %s\n", "event:", r.EventID.String()); err != nil {
			return err
		}
	}
	return nil
}

// importIssueJSON mirrors portability.ImportIssue for the JSON renderer.
// We re-marshal so the output carries snake_case keys and a stable shape
// even if the codec adds fields later.
type importIssueJSON struct {
	Kind        string `json:"kind"`
	EntityKind  string `json:"entity_kind,omitempty"`
	EntityID    string `json:"entity_id,omitempty"`
	JSONPointer string `json:"json_pointer,omitempty"`
	Message     string `json:"message"`
}

type importErrorJSON struct {
	Issues []importIssueJSON `json:"issues"`
}

func (a *App) renderImportError(w io.Writer, e *portability.ImportError) {
	issues := make([]portability.ImportIssue, len(e.Issues))
	copy(issues, e.Issues)
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Kind != issues[j].Kind {
			return issues[i].Kind < issues[j].Kind
		}
		if issues[i].EntityKind != issues[j].EntityKind {
			return issues[i].EntityKind < issues[j].EntityKind
		}
		return issues[i].EntityID < issues[j].EntityID
	})

	if a.format == "json" {
		payload := importErrorJSON{Issues: make([]importIssueJSON, len(issues))}
		for i, iss := range issues {
			payload.Issues[i] = importIssueJSON{
				Kind:        iss.Kind,
				EntityKind:  iss.EntityKind,
				EntityID:    iss.EntityID,
				JSONPointer: iss.JSONPointer,
				Message:     iss.Message,
			}
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(payload)
		return
	}

	for _, iss := range issues {
		var prefix string
		switch {
		case iss.EntityKind != "" && iss.EntityID != "":
			prefix = fmt.Sprintf("[%s] %s %s: ", iss.Kind, iss.EntityKind, iss.EntityID)
		case iss.EntityKind != "":
			prefix = fmt.Sprintf("[%s] %s: ", iss.Kind, iss.EntityKind)
		case iss.JSONPointer != "":
			prefix = fmt.Sprintf("[%s] %s: ", iss.Kind, iss.JSONPointer)
		default:
			prefix = fmt.Sprintf("[%s] ", iss.Kind)
		}
		_, _ = fmt.Fprintf(w, "%s%s\n", prefix, iss.Message)
	}
}
