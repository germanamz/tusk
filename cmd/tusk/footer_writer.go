package main

import (
	"io"
	"sync"
)

// eraseLine returns the cursor to column 0 and clears to end of line.
const eraseLine = "\r\033[K"

// footerWriter multiplexes a sticky one-line status footer and interleaved log
// lines onto a single terminal stream. `tusk graph`'s interactive footer redraws
// in place with a carriage-return + erase-line and leaves the cursor mid-line
// (no trailing newline); an asynchronous slog line written to the same terminal
// would otherwise glue onto the footer text ("…[q] quittime=2026-…"). Routing
// both the footer and the verbose background logs through this writer makes a log
// write erase the footer, print its line, and redraw the footer, with every write
// serialized under one mutex so the two can never interleave mid-sequence.
//
// Until activated it is a transparent passthrough: Write forwards bytes unchanged
// and setFooter is a no-op. A non-interactive run (CI, a pipe, or a raw-mode
// setup that failed) never activates it, so background logging behaves exactly as
// an unwrapped stderr logger.
type footerWriter struct {
	mu     sync.Mutex
	out    io.Writer
	footer string
	active bool
}

// newFooterWriter wraps out. The writer starts passive; call activate once an
// interactive terminal is confirmed.
func newFooterWriter(out io.Writer) *footerWriter {
	return &footerWriter{out: out}
}

// activate turns on sticky-footer coordination.
func (writer *footerWriter) activate() {
	writer.mu.Lock()
	writer.active = true
	writer.mu.Unlock()
}

// setFooter replaces the footer text and redraws it in place. A no-op while
// passive (there is no sticky footer to coordinate).
func (writer *footerWriter) setFooter(text string) {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	if !writer.active {
		return
	}

	writer.footer = text
	writer.drawLocked()
}

// Write is the io.Writer the verbose slog handler (and the HTTP error log) target.
// While passive it forwards p unchanged. Once active it erases the footer, writes
// the already-newline-terminated log line, then redraws the footer beneath it, so
// the log line and the footer never glue together. The returned count reflects
// bytes written from p — the erase/redraw control bytes are not counted — so the
// io.Writer contract holds for the caller.
func (writer *footerWriter) Write(payload []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	if !writer.active {
		return writer.out.Write(payload)
	}

	_, _ = io.WriteString(writer.out, eraseLine)
	written, writeErr := writer.out.Write(payload)
	writer.drawLocked()

	return written, writeErr
}

// finish disables coordination and moves the cursor to a fresh line below the
// final footer so the shell prompt starts cleanly. Called once when the console
// loop returns. It writes a carriage-return + newline rather than a bare newline
// because it runs while the terminal is still in raw mode (ONLCR disabled), where
// a lone "\n" drops a row without returning to column 0 — leaving the next shell
// prompt indented to the end of the footer.
func (writer *footerWriter) finish() {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	if !writer.active {
		return
	}

	writer.active = false
	_, _ = io.WriteString(writer.out, "\r\n")
}

// drawLocked writes the footer in place (erase current line, then the footer with
// no trailing newline). The caller must hold the mutex.
func (writer *footerWriter) drawLocked() {
	_, _ = io.WriteString(writer.out, eraseLine)
	_, _ = io.WriteString(writer.out, writer.footer)
}
