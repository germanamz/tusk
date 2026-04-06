package tui

import (
	"io"

	"charm.land/lipgloss/v2"
)

// Styles holds precomputed lipgloss styles for colored terminal output.
type Styles struct {
	// Priority maps priority int (0-4) to a foreground color style.
	Priority [5]lipgloss.Style
	// Dim is applied to rows whose status is in the workflow's dim_statuses.
	Dim lipgloss.Style
	// Header is used for table column headers (bold).
	Header lipgloss.Style
}

// Renderer encapsulates output formatting and styling for CLI commands.
type Renderer struct {
	w      io.Writer
	format string // "text" or "json"
	color  bool
	styles *Styles // nil when color=false
}

// NewRenderer creates a Renderer. When color is true, styles are initialized.
func NewRenderer(w io.Writer, format string, color bool) *Renderer {
	r := &Renderer{
		w:      w,
		format: format,
		color:  color,
	}
	if color {
		r.styles = newStyles()
	}
	return r
}

// newStyles initializes the default color styles.
func newStyles() *Styles {
	return &Styles{
		Priority: [5]lipgloss.Style{
			lipgloss.NewStyle().Foreground(lipgloss.Color("#666666")), // 0: none
			lipgloss.NewStyle().Foreground(lipgloss.Color("#4488ff")), // 1: low
			lipgloss.NewStyle().Foreground(lipgloss.Color("#ffcc00")), // 2: medium
			lipgloss.NewStyle().Foreground(lipgloss.Color("#ff8800")), // 3: high
			lipgloss.NewStyle().Foreground(lipgloss.Color("#ff4444")), // 4: urgent
		},
		Dim:    lipgloss.NewStyle().Faint(true),
		Header: lipgloss.NewStyle().Bold(true),
	}
}
