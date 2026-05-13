package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewLogger_DefaultLevelIsWarn(t *testing.T) {
	var buf bytes.Buffer
	logger := newLogger(&buf, false)

	logger.Info("info-line")
	logger.Warn("warn-line")

	output := buf.String()

	if strings.Contains(output, "info-line") {
		t.Errorf("default logger should NOT emit Info; got %q", output)
	}

	if !strings.Contains(output, "warn-line") {
		t.Errorf("default logger should emit Warn; got %q", output)
	}
}

func TestNewLogger_VerboseEmitsDebug(t *testing.T) {
	var buf bytes.Buffer
	logger := newLogger(&buf, true)

	logger.Debug("debug-line")
	logger.Info("info-line")
	logger.Warn("warn-line")

	output := buf.String()

	for _, want := range []string{"debug-line", "info-line", "warn-line"} {
		if !strings.Contains(output, want) {
			t.Errorf("verbose logger should emit %q; got %q", want, output)
		}
	}
}
