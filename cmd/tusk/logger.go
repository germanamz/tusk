package main

import (
	"io"
	"log/slog"
)

// newLogger builds the stderr logger used by long-running commands.
// Default level is Warn; verbose drops it to Debug.
func newLogger(out io.Writer, verbose bool) *slog.Logger {
	level := slog.LevelWarn

	if verbose {
		level = slog.LevelDebug
	}

	handler := slog.NewTextHandler(out, &slog.HandlerOptions{Level: level})

	return slog.New(handler)
}
