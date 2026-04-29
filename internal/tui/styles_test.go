package tui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
)

func TestStyledPriority_NoColor(test *testing.T) {
	renderer := NewRenderer(&bytes.Buffer{}, "text", false, nil)
	tests := []struct {
		priority int
		want     string
	}{
		{0, "-"},
		{1, "L"},
		{2, "M"},
		{3, "H"},
		{4, "U"},
	}
	for _, tt := range tests {
		got := renderer.styledPriority(tt.priority)
		if got != tt.want {
			test.Errorf("styledPriority(%d) = %q, want %q", tt.priority, got, tt.want)
		}
	}
}

func TestStyledPriority_WithColor(test *testing.T) {
	renderer := NewRenderer(&bytes.Buffer{}, "text", true, nil)
	tests := []struct {
		priority int
		wantText string // the visible text inside the ANSI codes
	}{
		{0, "-"},
		{1, "L"},
		{2, "M"},
		{3, "H"},
		{4, "U"},
	}
	for _, tt := range tests {
		got := renderer.styledPriority(tt.priority)
		// With color enabled, output should contain ANSI escape sequences
		if !strings.Contains(got, tt.wantText) {
			test.Errorf("styledPriority(%d) = %q, should contain %q", tt.priority, got, tt.wantText)
		}
		// Should contain escape character
		if !strings.Contains(got, "\x1b[") {
			test.Errorf("styledPriority(%d) = %q, should contain ANSI escape codes", tt.priority, got)
		}
	}
}

func TestStyledHeader_NoColor(test *testing.T) {
	renderer := NewRenderer(&bytes.Buffer{}, "text", false, nil)
	got := renderer.styledHeader("Title")
	if got != "Title" {
		test.Errorf("styledHeader(\"Title\") = %q, want \"Title\"", got)
	}
}

func TestStyledHeader_WithColor(test *testing.T) {
	renderer := NewRenderer(&bytes.Buffer{}, "text", true, nil)
	got := renderer.styledHeader("Title")
	if !strings.Contains(got, "Title") {
		test.Errorf("styledHeader should contain \"Title\", got %q", got)
	}
	if !strings.Contains(got, "\x1b[") {
		test.Errorf("styledHeader should contain ANSI codes, got %q", got)
	}
}

func TestIsDimStatus(test *testing.T) {
	dim := map[string]bool{"completed": true, "deleted": true}

	test.Run("dim status with color", func(test *testing.T) {
		renderer := NewRenderer(&bytes.Buffer{}, "text", true, dim)
		if !renderer.isDimStatus("completed") {
			test.Error("expected completed to be dim")
		}
		if !renderer.isDimStatus("deleted") {
			test.Error("expected deleted to be dim")
		}
		if renderer.isDimStatus("active") {
			test.Error("expected active to not be dim")
		}
		if renderer.isDimStatus("pending") {
			test.Error("expected pending to not be dim")
		}
	})

	test.Run("no color disables dim", func(test *testing.T) {
		renderer := NewRenderer(&bytes.Buffer{}, "text", false, dim)
		if renderer.isDimStatus("completed") {
			test.Error("expected dim to be disabled when color is off")
		}
	})

	test.Run("nil dim map", func(test *testing.T) {
		renderer := NewRenderer(&bytes.Buffer{}, "text", true, nil)
		if renderer.isDimStatus("completed") {
			test.Error("expected false for nil map")
		}
	})
}

func TestStyledTag_NoColor(test *testing.T) {
	renderer := NewRenderer(&bytes.Buffer{}, "text", false, nil)
	tag := &domain.Tag{Name: "bug"}
	got := renderer.styledTag(tag)
	if got != "+bug" {
		test.Errorf("styledTag = %q, want \"+bug\"", got)
	}
}

func TestStyledTag_WithColorNoTagColor(test *testing.T) {
	renderer := NewRenderer(&bytes.Buffer{}, "text", true, nil)
	tag := &domain.Tag{Name: "bug"}
	got := renderer.styledTag(tag)
	// No tag color set — should return plain "+bug" without ANSI codes
	if got != "+bug" {
		test.Errorf("styledTag = %q, want \"+bug\"", got)
	}
}

func TestStyledTag_WithColorAndTagColor(test *testing.T) {
	renderer := NewRenderer(&bytes.Buffer{}, "text", true, nil)
	color := "#ff4444"
	tag := &domain.Tag{Name: "urgent", Color: &color}
	got := renderer.styledTag(tag)
	if !strings.Contains(got, "urgent") {
		test.Errorf("styledTag should contain \"urgent\", got %q", got)
	}
	if !strings.Contains(got, "\x1b[") {
		test.Errorf("styledTag should contain ANSI codes when tag has color, got %q", got)
	}
}

func TestRenderMarkdown_WithColor(test *testing.T) {
	renderer := NewRenderer(&bytes.Buffer{}, "text", true, nil)
	input := "# Hello\n\nThis is **bold** text."
	got, err := renderer.renderMarkdown(input)

	if err != nil {
		test.Fatalf("renderMarkdown error: %v", err)
	}

	if !strings.Contains(got, "Hello") {
		test.Errorf("renderMarkdown should contain \"Hello\", got %q", got)
	}
	if !strings.Contains(got, "bold") {
		test.Errorf("renderMarkdown should contain \"bold\", got %q", got)
	}
}

func TestRenderMarkdown_NoColor(test *testing.T) {
	renderer := NewRenderer(&bytes.Buffer{}, "text", false, nil)
	input := "# Hello\n\nSome text."
	got, err := renderer.renderMarkdown(input)

	if err != nil {
		test.Fatalf("renderMarkdown error: %v", err)
	}

	if !strings.Contains(got, "Hello") {
		test.Errorf("renderMarkdown should contain \"Hello\", got %q", got)
	}
	if strings.Contains(got, "\x1b[") {
		test.Errorf("renderMarkdown with no color should not contain ANSI codes, got %q", got)
	}
}

func TestRenderMarkdown_PlainText(test *testing.T) {
	renderer := NewRenderer(&bytes.Buffer{}, "text", true, nil)
	input := "Just a plain description with no markdown."
	got, err := renderer.renderMarkdown(input)

	if err != nil {
		test.Fatalf("renderMarkdown error: %v", err)
	}

	if !strings.Contains(got, "plain description") {
		test.Errorf("renderMarkdown should contain original text, got %q", got)
	}
}

func TestColorEnabled(test *testing.T) {
	tests := []struct {
		name     string
		noColor  bool
		envSet   bool
		cfgColor bool
		want     bool
	}{
		{"defaults to config true", false, false, true, true},
		{"defaults to config false", false, false, false, false},
		{"no-color flag overrides config", true, false, true, false},
		{"NO_COLOR env overrides config", false, true, true, false},
		{"flag takes precedence over env", true, true, true, false},
	}
	for _, tt := range tests {
		test.Run(tt.name, func(test *testing.T) {
			app := &App{
				noColor: tt.noColor,
				tuiCfg:  config.TUIConfig{Color: tt.cfgColor},
			}
			if tt.envSet {
				test.Setenv("NO_COLOR", "1")
			}
			if got := app.colorEnabled(); got != tt.want {
				test.Errorf("colorEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
