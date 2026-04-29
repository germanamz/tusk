package tui

import (
	"io"
	"regexp"
	"strings"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

// Styles holds precomputed lipgloss styles for colored terminal output.
type Styles struct {
	// Priority maps priority int (0-4) to a foreground color style.
	Priority [5]lipgloss.Style
	// Dim is applied to rows whose status has the dim role in its workflow.
	Dim lipgloss.Style
	// Header is used for table column headers (bold).
	Header lipgloss.Style
	// Bold is applied to inline highlight-role status segments (e.g. the
	// rollup breakdown in `tusk task tree --rollup`).
	Bold lipgloss.Style
}

// Renderer encapsulates output formatting and styling for CLI commands.
type Renderer struct {
	w                 io.Writer
	format            string // "text", "json", or "markdown" (markdown is tree-only)
	color             bool
	styles            *Styles // nil when color=false
	dimStatuses       map[string]bool
	highlightStatuses map[string]bool
	projectNames      func(uuid.UUID) string
	taxonomyForTask   func(uuid.UUID) bool
	markdown          *markdownInputs // populated by setMarkdownInputs for tree --format markdown
}

// setMarkdownInputs stashes the inputs the markdown tree renderer needs.
// A nil argument is a no-op so callers can unconditionally invoke it.
func (renderer *Renderer) setMarkdownInputs(in *markdownInputs) {
	if in == nil {
		return
	}
	renderer.markdown = in
}

// SetProjectNameResolver wires a function that resolves project UUIDs to
// their human names. Callers should pass a per-invocation cache so that
// list views avoid N+1 lookups.
func (renderer *Renderer) SetProjectNameResolver(fn func(uuid.UUID) string) {
	renderer.projectNames = fn
}

// SetTaxonomyResolver wires a function that reports whether a project has a
// non-empty effective taxonomy. Used by renderTaskInfo / renderTree to decide
// whether to show the level line / [level] suffix. nil means "always off",
// which preserves pre-Phase-5 rendering for commands that do not care.
func (renderer *Renderer) SetTaxonomyResolver(fn func(uuid.UUID) bool) {
	renderer.taxonomyForTask = fn
}

// hasTaxonomy returns true when the task's project has a non-empty effective
// taxonomy. Falls back to false when no resolver is wired.
func (renderer *Renderer) hasTaxonomy(projectID uuid.UUID) bool {
	if renderer.taxonomyForTask == nil {
		return false
	}
	return renderer.taxonomyForTask(projectID)
}

// projectName returns the display name for a project UUID, falling back to
// the stringified UUID when no resolver is wired.
func (renderer *Renderer) projectName(id uuid.UUID) string {
	if renderer.projectNames == nil {
		return id.String()
	}
	return renderer.projectNames(id)
}

// NewRenderer creates a Renderer. When color is true, styles are initialized.
// dimStatuses is a set of status names that should be rendered faint.
func NewRenderer(w io.Writer, format string, color bool, dimStatuses map[string]bool) *Renderer {
	result := &Renderer{
		w:           w,
		format:      format,
		color:       color,
		dimStatuses: dimStatuses,
	}
	if color {
		result.styles = newStyles()
	}
	return result
}

// isDimStatus returns true if the given status should be rendered faint.
func (renderer *Renderer) isDimStatus(status string) bool {
	return renderer.styles != nil && renderer.dimStatuses[status]
}

// isHighlightStatus reports whether the given status carries the highlight
// role in any configured workflow. Mirrors the dimStatuses lookup pattern;
// the set is populated by the caller (e.g. runTree in --rollup mode).
func (renderer *Renderer) isHighlightStatus(status string) bool {
	return renderer.styles != nil && renderer.highlightStatuses[status]
}

// styledPriority returns the priority symbol with color applied if styles are active.
func (renderer *Renderer) styledPriority(priority int) string {
	sym := formatPriority(priority)
	if renderer.styles == nil {
		return sym
	}
	idx := priority
	if idx < 0 || idx > 4 {
		idx = 0
	}
	return renderer.styles.Priority[idx].Render(sym)
}

// styledHeader returns text with bold styling if styles are active.
func (renderer *Renderer) styledHeader(text string) string {
	if renderer.styles == nil {
		return text
	}
	return renderer.styles.Header.Render(text)
}

// styledLabel returns text with bold styling if styles are active (used for info labels).
func (renderer *Renderer) styledLabel(text string) string {
	return renderer.styledHeader(text)
}

// paddedLabel returns a label styled and padded to a fixed visible width.
func (renderer *Renderer) paddedLabel(text string, width int) string {
	styled := renderer.styledLabel(text)
	pad := max(0, width-lipgloss.Width(styled))
	return styled + strings.Repeat(" ", pad)
}

// styledTag returns "+tagname" with the tag's hex color applied as foreground if color is enabled
// and the tag has a color set. Otherwise returns plain "+tagname".
func (renderer *Renderer) styledTag(tag *domain.Tag) string {
	text := "+" + tag.Name
	if renderer.styles == nil || tag.Color == nil {
		return text
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(*tag.Color)).Render(text)
}

// markdownStyle returns a glamour style customized for tusk's terminal output.
// Based on DarkStyleConfig with cleaner headings, subtler inline code, and
// visible code block backgrounds.
func markdownStyle() ansi.StyleConfig {
	cfg := styles.DarkStyleConfig

	// Document: use terminal default foreground instead of hardcoded light gray.
	cfg.Document.Color = nil

	// Headings: bold only, no markdown prefixes, backgrounds, or vibrant colors.
	cfg.Heading.Color = nil
	noPrefix := ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: ""}}
	cfg.H1 = noPrefix
	cfg.H2 = noPrefix
	cfg.H3 = noPrefix
	cfg.H4 = noPrefix
	cfg.H5 = noPrefix
	cfg.H6 = ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Prefix: "",
			Bold:   boolPtr(false),
		},
	}

	// Inline code: bold with backtick delimiters, no color or background.
	cfg.Code = ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Prefix: "`",
			Suffix: "`",
			Bold:   boolPtr(true),
		},
	}

	// Code blocks: closing fence delimiter. The opening ```lang is injected
	// by labelCodeBlocks since glamour strips the info string.
	cfg.CodeBlock.BlockSuffix = "```"

	return cfg
}

func boolPtr(b bool) *bool { return &b }

// codeBlockLangRe matches fenced code blocks with a language identifier.
var codeBlockLangRe = regexp.MustCompile("(?m)^(```)(\\w+)\\s*$")

// labelCodeBlocks rewrites ```lang to ```lang\n```lang so the opening
// fence with language appears as the first line inside the rendered block
// (glamour strips the info string from its output).
func labelCodeBlocks(text string) string {
	return codeBlockLangRe.ReplaceAllString(text, "${1}${2}\n${1}${2}")
}

// renderMarkdown renders markdown text for terminal display using glamour.
// When color is disabled, uses NoTTY style for plain ASCII formatting.
func (renderer *Renderer) renderMarkdown(text string) (string, error) {
	var opts []glamour.TermRendererOption

	if renderer.color {
		opts = append(opts, glamour.WithStyles(markdownStyle()))
	} else {
		opts = append(opts, glamour.WithStyles(styles.NoTTYStyleConfig))
	}
	opts = append(opts, glamour.WithWordWrap(0))

	termRenderer, termRendererErr := glamour.NewTermRenderer(opts...)

	if termRendererErr != nil {
		return text, termRendererErr
	}

	rendered, renderErr := termRenderer.Render(labelCodeBlocks(text))

	if renderErr != nil {
		return text, renderErr
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
		Bold:   lipgloss.NewStyle().Bold(true),
	}
}
