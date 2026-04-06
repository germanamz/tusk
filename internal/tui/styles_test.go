package tui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/config"
)

func TestStyledPriority_NoColor(t *testing.T) {
	r := NewRenderer(&bytes.Buffer{}, "text", false)
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
	r := NewRenderer(&bytes.Buffer{}, "text", true)
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
	r := NewRenderer(&bytes.Buffer{}, "text", false)
	got := r.styledHeader("Title")
	if got != "Title" {
		t.Errorf("styledHeader(\"Title\") = %q, want \"Title\"", got)
	}
}

func TestStyledHeader_WithColor(t *testing.T) {
	r := NewRenderer(&bytes.Buffer{}, "text", true)
	got := r.styledHeader("Title")
	if !strings.Contains(got, "Title") {
		t.Errorf("styledHeader should contain \"Title\", got %q", got)
	}
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("styledHeader should contain ANSI codes, got %q", got)
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
