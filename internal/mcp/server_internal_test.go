package mcp

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// TestErrRecorder_LogsImmediatelyAndKeepsFirst pins the A1 surfacing contract:
// a background component's terminal error is logged the moment it is recorded
// (not only at process exit), and the FIRST such error is the one RunBackground
// returns. A nil error is a no-op.
func TestErrRecorder_LogsImmediatelyAndKeepsFirst(test *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	rec := &errRecorder{logger: logger}

	rec.record("embed drainer", nil)

	if buf.Len() != 0 {
		test.Errorf("nil error must not log; got %q", buf.String())
	}

	if firstErr := rec.firstErr(); firstErr != nil {
		test.Errorf("nil error must not set firstErr; got %v", firstErr)
	}

	watcherErr := errors.New("watcher died")
	rec.record("file watcher", watcherErr)

	logged := buf.String()

	if !strings.Contains(logged, "background component failed") {
		test.Errorf("error must be logged immediately; got %q", logged)
	}

	if !strings.Contains(logged, "file watcher") {
		test.Errorf("log must name the failing component; got %q", logged)
	}

	// A second error must not displace the first.
	rec.record("reindex drainer", errors.New("drainer died"))

	if firstErr := rec.firstErr(); firstErr != watcherErr {
		test.Errorf("firstErr = %v, want the first recorded error %v", firstErr, watcherErr)
	}
}

// TestErrRecorder_NilLoggerSafe confirms a nil logger silences output without
// panicking — a Workers>0 daemon started without --verbose still records the
// first error even if (defensively) no logger is wired.
func TestErrRecorder_NilLoggerSafe(test *testing.T) {
	rec := &errRecorder{logger: nil}

	bootErr := errors.New("boom")
	rec.record("index epoch watcher", bootErr)

	if firstErr := rec.firstErr(); firstErr != bootErr {
		test.Errorf("firstErr = %v, want %v", firstErr, bootErr)
	}
}

// TestServerInstructions_NameTheVault pins #794: a server started under
// --workspace runs outside the vault, so the instructions name its absolute
// paths instead of "./", which would point the agent at the wrong folder.
func TestServerInstructions_NameTheVault(test *testing.T) {
	runtime := &Runtime{
		Root:         "/vaults/notes",
		ManifestPath: "/vaults/notes/tusk.toml",
		IndexPath:    "/vaults/notes/.tusk/index.db",
	}

	instructions := serverInstructions(runtime)

	for _, want := range []string{
		"running over the vault at /vaults/notes.",
		"markdown and HTML files under /vaults/notes\n",
		"the schema declared in /vaults/notes/tusk.toml\n",
		"the SQLite index at /vaults/notes/.tusk/index.db\n",
		"edit /vaults/notes/tusk.toml in your editor",
	} {
		if !strings.Contains(instructions, want) {
			test.Errorf("instructions missing %q:\n%s", want, instructions)
		}
	}

	if strings.Contains(instructions, "./") || strings.Contains(instructions, "%!") {
		test.Errorf("instructions still name ./ or carry a format error:\n%s", instructions)
	}
}
