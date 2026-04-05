package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadDescription_InlineText(t *testing.T) {
	result, err := readDescription("hello world", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello world" {
		t.Fatalf("expected %q, got %q", "hello world", result)
	}
}

func TestReadDescription_EmptyString(t *testing.T) {
	result, err := readDescription("", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Fatalf("expected empty string, got %q", result)
	}
}

func TestReadDescription_FromStdinNil(t *testing.T) {
	_, err := readDescription("@-", nil)
	if err == nil {
		t.Fatal("expected error for nil stdin")
	}
	if !strings.Contains(err.Error(), "stdin is a terminal, not a pipe") {
		t.Fatalf("expected stdin error, got: %v", err)
	}
}

func TestReadDescription_FromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "desc.md")
	content := "# Title\n\nSome description content."
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	result, err := readDescription("@"+path, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != content {
		t.Fatalf("expected %q, got %q", content, result)
	}
}

func TestReadDescription_FromFileMissing(t *testing.T) {
	_, err := readDescription("@/nonexistent/path/file.md", nil)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "failed to read description file") {
		t.Fatalf("expected 'failed to read description file' error, got: %v", err)
	}
}

func TestReadDescription_FromStdin(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating pipe: %v", err)
	}
	_, _ = w.WriteString("piped content")
	w.Close()

	result, err := readDescription("@-", r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "piped content" {
		t.Fatalf("expected %q, got %q", "piped content", result)
	}
}

func TestReadDescription_FromStdinTTY(t *testing.T) {
	tty, err := os.Open("/dev/tty")
	if err != nil {
		t.Skip("no /dev/tty available (CI environment)")
	}
	defer tty.Close()

	_, err = readDescription("@-", tty)
	if err == nil {
		t.Fatal("expected error for TTY stdin")
	}
	if !strings.Contains(err.Error(), "stdin is a terminal, not a pipe") {
		t.Fatalf("expected stdin error, got: %v", err)
	}
}

func TestReadDescription_AtSignOnly(t *testing.T) {
	_, err := readDescription("@", nil)
	if err == nil {
		t.Fatal("expected error for bare @")
	}
	if !strings.Contains(err.Error(), "failed to read description file") {
		t.Fatalf("expected file error, got: %v", err)
	}
}
