package main

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
)

func TestFooterWriter_PassiveIsPassthrough(test *testing.T) {
	var buf bytes.Buffer

	writer := newFooterWriter(&buf)

	writer.setFooter("footer") // no-op before activate
	_, _ = io.WriteString(writer, "log line\n")

	if got := buf.String(); got != "log line\n" {
		test.Errorf("passive footerWriter wrote %q, want a clean passthrough %q", got, "log line\n")
	}
}

// TestFooterWriter_ActiveNeverGluesLogOntoFooter pins the defect-2 fix: a log
// line written while a footer is displayed must never be concatenated onto the
// footer text. The writer erases the footer, prints the line, and redraws.
func TestFooterWriter_ActiveNeverGluesLogOntoFooter(test *testing.T) {
	var buf bytes.Buffer

	writer := newFooterWriter(&buf)
	writer.activate()
	writer.setFooter("FOOTER")

	_, _ = io.WriteString(writer, "logged\n")

	got := buf.String()

	if !strings.Contains(got, "logged\n") {
		test.Errorf("output %q missing the log line", got)
	}

	// The bug is "FOOTER" immediately followed by log text. An erase-line must
	// sit between them, so the glued substring must never appear.
	if strings.Contains(got, "FOOTERlogged") {
		test.Errorf("log line glued onto footer: %q", got)
	}

	// After a log line the footer is redrawn beneath it (erase + text), so the
	// footer stays visible at the bottom.
	if !strings.HasSuffix(got, eraseLine+"FOOTER") {
		test.Errorf("footer not redrawn after the log line: %q", got)
	}
}

func TestFooterWriter_SetFooterRedrawsInPlace(test *testing.T) {
	var buf bytes.Buffer

	writer := newFooterWriter(&buf)
	writer.activate()

	writer.setFooter("first")
	writer.setFooter("second")

	got := buf.String()

	// Each redraw is prefixed by an erase-line, and the latest footer wins.
	if strings.Count(got, eraseLine) != 2 {
		test.Errorf("expected two in-place redraws (erase-line prefixes), got %q", got)
	}

	if !strings.HasSuffix(got, "second") {
		test.Errorf("latest footer should be %q, got %q", "second", got)
	}
}

func TestFooterWriter_FinishEndsBelowFooter(test *testing.T) {
	var buf bytes.Buffer

	writer := newFooterWriter(&buf)
	writer.activate()
	writer.setFooter("done")
	writer.finish()

	// Raw-mode safe: carriage-return + newline so the cursor lands at column 0 of
	// a fresh line, not indented to the end of the footer.
	if !strings.HasSuffix(buf.String(), "done\r\n") {
		test.Errorf("finish should end with a raw-mode-safe CRLF below the footer, got %q", buf.String())
	}

	// After finish the writer is passive again: further writes pass through.
	buf.Reset()
	_, _ = io.WriteString(writer, "after\n")

	if got := buf.String(); got != "after\n" {
		test.Errorf("post-finish write should pass through, got %q", got)
	}
}

// TestFooterWriter_SlogLinesNeverGlueOntoFooter is the defect-2 integration
// check: a slog logger built over the coordinated writer (exactly how serveGraph
// wires the background logger) must never glue a log record onto the footer text.
// This exercises the real slog handler, which emits one Write per record.
func TestFooterWriter_SlogLinesNeverGlueOntoFooter(test *testing.T) {
	var buf bytes.Buffer

	footer := newFooterWriter(&buf)
	logger := newLogger(footer, true) // verbose: Debug level

	footer.activate()
	footer.setFooter("synced · 2 nodes · 2 edges · 0 clients   [space] open  [q] quit")

	logger.Info("reindex walk complete", "indexed", 0, "duration_ms", 1)

	got := buf.String()

	if !strings.Contains(got, "reindex walk complete") {
		test.Fatalf("log record missing from output: %q", got)
	}

	// The regression: the footer's trailing "[q] quit" concatenated with the log
	// record's leading "time=" ("…[q] quittime=2026-…"). An erase-line must sit
	// between them.
	if strings.Contains(got, "quittime=") {
		test.Errorf("log record glued onto footer text: %q", got)
	}
}

// TestFooterWriter_ConcurrentWritesAndFooter pins goroutine safety: background
// log writes and console footer updates run on different goroutines. CI runs
// go test -race.
func TestFooterWriter_ConcurrentWritesAndFooter(test *testing.T) {
	writer := newFooterWriter(io.Discard)
	writer.activate()

	var waitGroup sync.WaitGroup

	waitGroup.Add(2)

	go func() {
		defer waitGroup.Done()

		for iter := 0; iter < 500; iter++ {
			_, _ = io.WriteString(writer, "background log\n")
		}
	}()

	go func() {
		defer waitGroup.Done()

		for iter := 0; iter < 500; iter++ {
			writer.setFooter("tick")
		}
	}()

	waitGroup.Wait()
}
