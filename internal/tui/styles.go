package tui

import (
	"io"
	"strings"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
	"github.com/germanamz/tusk/internal/domain"
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
	w           io.Writer
	format      string // "text" or "json"
	color       bool
	styles      *Styles // nil when color=false
	dimStatuses map[string]bool
}

// NewRenderer creates a Renderer. When color is true, styles are initialized.
// dimStatuses is a set of status names that should be rendered faint.
func NewRenderer(w io.Writer, format string, color bool, dimStatuses map[string]bool) *Renderer {
	r := &Renderer{
		w:           w,
		format:      format,
		color:       color,
		dimStatuses: dimStatuses,
	}
	if color {
		r.styles = newStyles()
	}
	return r
}

// isDimStatus returns true if the given status should be rendered faint.
func (r *Renderer) isDimStatus(status string) bool {
	return r.styles != nil && r.dimStatuses[status]
}

// styledPriority returns the priority symbol with color applied if styles are active.
func (r *Renderer) styledPriority(priority int) string {
	sym := formatPriority(priority)
	if r.styles == nil {
		return sym
	}
	idx := priority
	if idx < 0 || idx > 4 {
		idx = 0
	}
	return r.styles.Priority[idx].Render(sym)
}

// styledHeader returns text with bold styling if styles are active.
func (r *Renderer) styledHeader(text string) string {
	if r.styles == nil {
		return text
	}
	return r.styles.Header.Render(text)
}

// styledLabel returns text with bold styling if styles are active (same as styledHeader, used for info labels).
func (r *Renderer) styledLabel(text string) string {
	if r.styles == nil {
		return text
	}
	return r.styles.Header.Render(text)
}

// paddedLabel returns a label styled and padded to a fixed visible width.
func (r *Renderer) paddedLabel(text string, width int) string {
	styled := r.styledLabel(text)
	pad := max(0, width-lipgloss.Width(styled))
	return styled + strings.Repeat(" ", pad)
}

// styledTag returns "+tagname" with the tag's hex color applied as foreground if color is enabled
// and the tag has a color set. Otherwise returns plain "+tagname".
func (r *Renderer) styledTag(tag *domain.Tag) string {
	text := "+" + tag.Name
	if r.styles == nil || tag.Color == nil {
		return text
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(*tag.Color)).Render(text)
}

// markdownStyle returns a glamour style customized for tusk's terminal output.
// Based on DarkStyleConfig with cleaner headings, subtler inline code, and
// visible code block backgrounds.
func markdownStyle() ansi.StyleConfig {
	s := styles.DarkStyleConfig

	// Headings: bold only, no markdown prefixes, backgrounds, or vibrant colors.
	noPrefix := ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: ""}}
	s.H1 = noPrefix
	s.H2 = noPrefix
	s.H3 = noPrefix
	s.H4 = noPrefix
	s.H5 = noPrefix
	s.H6 = ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Prefix: "",
			Bold:   boolPtr(false),
		},
	}

	// Inline code: bold with backtick delimiters, no color or background.
	s.Code = ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Prefix: "`",
			Suffix: "`",
			Bold:   boolPtr(true),
		},
	}

	// Code blocks: add a subtle background so they stand out.
	if s.CodeBlock.Chroma != nil {
		s.CodeBlock.Chroma.Background = ansi.StylePrimitive{
			BackgroundColor: stringPtr("#303030"),
		}
	}

	return s
}

func boolPtr(b bool) *bool       { return &b }
func stringPtr(s string) *string { return &s }

// renderMarkdown renders markdown text for terminal display using glamour.
// When color is disabled, uses NoTTY style for plain ASCII formatting.
func (r *Renderer) renderMarkdown(text string) (string, error) {
	var opts []glamour.TermRendererOption

	if r.color {
		opts = append(opts, glamour.WithStyles(markdownStyle()))
	} else {
		opts = append(opts, glamour.WithStyles(styles.NoTTYStyleConfig))
	}
	opts = append(opts, glamour.WithWordWrap(0))

	renderer, err := glamour.NewTermRenderer(opts...)
	if err != nil {
		return text, err
	}

	rendered, err := renderer.Render(text)
	if err != nil {
		return text, err
	}

	return strings.TrimRight(rendered, "\n"), nil
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
