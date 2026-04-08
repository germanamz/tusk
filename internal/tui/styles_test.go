package tui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
)

func TestStyledPriority_NoColor(t *testing.T) {
	r := NewRenderer(&bytes.Buffer{}, "text", false, nil)
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
		got := r.styledPriority(tt.priority)
		if got != tt.want {
			t.Errorf("styledPriority(%d) = %q, want %q", tt.priority, got, tt.want)
		}
	}
}

func TestStyledPriority_WithColor(t *testing.T) {
	r := NewRenderer(&bytes.Buffer{}, "text", true, nil)
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
		got := r.styledPriority(tt.priority)
		// With color enabled, output should contain ANSI escape sequences
		if !strings.Contains(got, tt.wantText) {
			t.Errorf("styledPriority(%d) = %q, should contain %q", tt.priority, got, tt.wantText)
		}
		// Should contain escape character
		if !strings.Contains(got, "\x1b[") {
			t.Errorf("styledPriority(%d) = %q, should contain ANSI escape codes", tt.priority, got)
		}
	}
}

func TestStyledHeader_NoColor(t *testing.T) {
	r := NewRenderer(&bytes.Buffer{}, "text", false, nil)
	got := r.styledHeader("Title")
	if got != "Title" {
		t.Errorf("styledHeader(\"Title\") = %q, want \"Title\"", got)
	}
}

func TestStyledHeader_WithColor(t *testing.T) {
	r := NewRenderer(&bytes.Buffer{}, "text", true, nil)
	got := r.styledHeader("Title")
	if !strings.Contains(got, "Title") {
		t.Errorf("styledHeader should contain \"Title\", got %q", got)
	}
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("styledHeader should contain ANSI codes, got %q", got)
	}
}

func TestIsDimStatus(t *testing.T) {
	dim := map[string]bool{"completed": true, "deleted": true}

	t.Run("dim status with color", func(t *testing.T) {
		r := NewRenderer(&bytes.Buffer{}, "text", true, dim)
		if !r.isDimStatus("completed") {
			t.Error("expected completed to be dim")
		}
		if !r.isDimStatus("deleted") {
			t.Error("expected deleted to be dim")
		}
		if r.isDimStatus("active") {
			t.Error("expected active to not be dim")
		}
		if r.isDimStatus("pending") {
			t.Error("expected pending to not be dim")
		}
	})

	t.Run("no color disables dim", func(t *testing.T) {
		r := NewRenderer(&bytes.Buffer{}, "text", false, dim)
		if r.isDimStatus("completed") {
			t.Error("expected dim to be disabled when color is off")
		}
	})

	t.Run("nil dim map", func(t *testing.T) {
		r := NewRenderer(&bytes.Buffer{}, "text", true, nil)
		if r.isDimStatus("completed") {
			t.Error("expected false for nil map")
		}
	})
}

func TestStyledTag_NoColor(t *testing.T) {
	r := NewRenderer(&bytes.Buffer{}, "text", false, nil)
	tag := &domain.Tag{Name: "bug"}
	got := r.styledTag(tag)
	if got != "+bug" {
		t.Errorf("styledTag = %q, want \"+bug\"", got)
	}
}

func TestStyledTag_WithColorNoTagColor(t *testing.T) {
	r := NewRenderer(&bytes.Buffer{}, "text", true, nil)
	tag := &domain.Tag{Name: "bug"}
	got := r.styledTag(tag)
	// No tag color set — should return plain "+bug" without ANSI codes
	if got != "+bug" {
		t.Errorf("styledTag = %q, want \"+bug\"", got)
	}
}

func TestStyledTag_WithColorAndTagColor(t *testing.T) {
	r := NewRenderer(&bytes.Buffer{}, "text", true, nil)
	color := "#ff4444"
	tag := &domain.Tag{Name: "urgent", Color: &color}
	got := r.styledTag(tag)
	if !strings.Contains(got, "urgent") {
		t.Errorf("styledTag should contain \"urgent\", got %q", got)
	}
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("styledTag should contain ANSI codes when tag has color, got %q", got)
	}
}

func TestRenderMarkdown_WithColor(t *testing.T) {
	r := NewRenderer(&bytes.Buffer{}, "text", true, nil)
	input := "# Hello\n\nThis is **bold** text."
	got, err := r.renderMarkdown(input)
	if err != nil {
		t.Fatalf("renderMarkdown error: %v", err)
	}
	if !strings.Contains(got, "Hello") {
		t.Errorf("renderMarkdown should contain \"Hello\", got %q", got)
	}
	if !strings.Contains(got, "bold") {
		t.Errorf("renderMarkdown should contain \"bold\", got %q", got)
	}
}

func TestRenderMarkdown_NoColor(t *testing.T) {
	r := NewRenderer(&bytes.Buffer{}, "text", false, nil)
	input := "# Hello\n\nSome text."
	got, err := r.renderMarkdown(input)
	if err != nil {
		t.Fatalf("renderMarkdown error: %v", err)
	}
	if !strings.Contains(got, "Hello") {
		t.Errorf("renderMarkdown should contain \"Hello\", got %q", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("renderMarkdown with no color should not contain ANSI codes, got %q", got)
	}
}

func TestRenderMarkdown_PlainText(t *testing.T) {
	r := NewRenderer(&bytes.Buffer{}, "text", true, nil)
	input := "Just a plain description with no markdown."
	got, err := r.renderMarkdown(input)
	if err != nil {
		t.Fatalf("renderMarkdown error: %v", err)
	}
	if !strings.Contains(got, "plain description") {
		t.Errorf("renderMarkdown should contain original text, got %q", got)
	}
}

func TestColorEnabled(t *testing.T) {
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
		t.Run(tt.name, func(t *testing.T) {
			a := &App{
				noColor: tt.noColor,
				tuiCfg:  config.TUIConfig{Color: tt.cfgColor},
			}
			if tt.envSet {
				t.Setenv("NO_COLOR", "1")
			}
			if got := a.colorEnabled(); got != tt.want {
				t.Errorf("colorEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
