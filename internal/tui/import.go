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
func (app *App) buildImportCmd() *cobra.Command {
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
		RunE: app.runImport,
	}
	cmd.Flags().StringP("input", "i", "", `path to read from; "-" for stdin`)
	cmd.Flags().Bool("replace", false, "row-level upsert on collision")
	cmd.Flags().Bool("truncate", false, "wipe every entity table before applying; requires --replace")
	cmd.Flags().Bool("dry-run", false, "validate and report counts; no writes")
	_ = cmd.MarkFlagRequired("input")
	return cmd
}

func (app *App) runImport(cmd *cobra.Command, _ []string) error {
	if app.portabilitySvc == nil {
		return fmt.Errorf("portability service not configured")
	}
	input, inputErr := cmd.Flags().GetString("input")

	if inputErr != nil {
		return inputErr
	}

	replace, replaceErr := cmd.Flags().GetBool("replace")

	if replaceErr != nil {
		return replaceErr
	}

	truncate, truncateErr := cmd.Flags().GetBool("truncate")

	if truncateErr != nil {
		return truncateErr
	}

	dryRun, dryRunErr := cmd.Flags().GetBool("dry-run")

	if dryRunErr != nil {
		return dryRunErr
	}

	if truncate && !replace {
		return fmt.Errorf("--truncate requires --replace")
	}

	reader, closer, openErr := app.openImportSource(cmd, input)

	if openErr != nil {
		return openErr
	}

	defer closer()

	ws, decodeErr := portability.Decode(reader)

	if decodeErr != nil {
		var ie *portability.ImportError
		if errors.As(decodeErr, &ie) {
			app.renderImportError(cmd.ErrOrStderr(), ie)
			return ErrImportFailed
		}
		return decodeErr
	}

	report, importErr := app.portabilitySvc.Import(cmd.Context(), ws, service.ImportOptions{
		Replace:  replace,
		Truncate: truncate,
		DryRun:   dryRun,
	})

	if importErr != nil {
		var ie *portability.ImportError
		if errors.As(importErr, &ie) {
			app.renderImportError(cmd.ErrOrStderr(), ie)
			return ErrImportFailed
		}
		return importErr
	}

	return app.renderImportReport(cmd.OutOrStdout(), report, dryRun)
}

// openImportSource opens the dump source — a file path or "-" for stdin.
// The returned closer is always safe to call.
func (app *App) openImportSource(cmd *cobra.Command, input string) (io.Reader, func(), error) {
	if input != "-" {
		file, openErr := os.Open(input)

		if openErr != nil {
			return nil, func() {}, fmt.Errorf("opening %s: %w", input, openErr)
		}

		return file, func() { _ = file.Close() }, nil
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

func (app *App) renderImportReport(writer io.Writer, report *service.ImportReport, dryRun bool) error {
	if app.format == "json" {
		payload := importReportJSON{
			Workflows:   report.Workflows,
			Projects:    report.Projects,
			Players:     report.Players,
			Tags:        report.Tags,
			Tasks:       report.Tasks,
			Relations:   report.Relations,
			Annotations: report.Annotations,
			Notes:       report.Notes,
			Events:      report.Events,
			Replaced:    report.Replaced,
			Truncated:   report.Truncated,
			DryRun:      dryRun,
		}
		if report.EventID.String() != "00000000-0000-0000-0000-000000000000" {
			payload.EventID = report.EventID.String()
		}
		enc := json.NewEncoder(writer)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}

	if dryRun {
		if _, err := fmt.Fprintln(writer, "dry-run: no writes performed"); err != nil {
			return err
		}
	}
	if report.Truncated {
		if _, err := fmt.Fprintln(writer, "truncated: yes"); err != nil {
			return err
		}
	}
	kinds := []struct {
		name  string
		count int
	}{
		{"workflows", report.Workflows},
		{"projects", report.Projects},
		{"players", report.Players},
		{"tags", report.Tags},
		{"tasks", report.Tasks},
		{"relations", report.Relations},
		{"annotations", report.Annotations},
		{"notes", report.Notes},
		{"events", report.Events},
	}
	for _, kind := range kinds {
		if _, err := fmt.Fprintf(writer, "%-12s %d\n", kind.name+":", kind.count); err != nil {
			return err
		}
	}
	if report.Replaced > 0 {
		if _, err := fmt.Fprintf(writer, "%-12s %d\n", "replaced:", report.Replaced); err != nil {
			return err
		}
	}
	if report.EventID.String() != "00000000-0000-0000-0000-000000000000" {
		if _, err := fmt.Fprintf(writer, "%-12s %s\n", "event:", report.EventID.String()); err != nil {
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

func (app *App) renderImportError(writer io.Writer, importErr *portability.ImportError) {
	issues := make([]portability.ImportIssue, len(importErr.Issues))
	copy(issues, importErr.Issues)
	sort.SliceStable(issues, func(ii, jj int) bool {
		if issues[ii].Kind != issues[jj].Kind {
			return issues[ii].Kind < issues[jj].Kind
		}
		if issues[ii].EntityKind != issues[jj].EntityKind {
			return issues[ii].EntityKind < issues[jj].EntityKind
		}
		return issues[ii].EntityID < issues[jj].EntityID
	})

	if app.format == "json" {
		payload := importErrorJSON{Issues: make([]importIssueJSON, len(issues))}
		for index, issue := range issues {
			payload.Issues[index] = importIssueJSON{
				Kind:        issue.Kind,
				EntityKind:  issue.EntityKind,
				EntityID:    issue.EntityID,
				JSONPointer: issue.JSONPointer,
				Message:     issue.Message,
			}
		}
		enc := json.NewEncoder(writer)
		enc.SetIndent("", "  ")
		_ = enc.Encode(payload)
		return
	}

	for _, issue := range issues {
		var prefix string
		switch {
		case issue.EntityKind != "" && issue.EntityID != "":
			prefix = fmt.Sprintf("[%s] %s %s: ", issue.Kind, issue.EntityKind, issue.EntityID)
		case issue.EntityKind != "":
			prefix = fmt.Sprintf("[%s] %s: ", issue.Kind, issue.EntityKind)
		case issue.JSONPointer != "":
			prefix = fmt.Sprintf("[%s] %s: ", issue.Kind, issue.JSONPointer)
		default:
			prefix = fmt.Sprintf("[%s] ", issue.Kind)
		}
		_, _ = fmt.Fprintf(writer, "%s%s\n", prefix, issue.Message)
	}
}
