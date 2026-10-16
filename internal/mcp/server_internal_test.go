package mcp

import (
	"bytes"
	"encoding/json"
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
		"vault at /vaults/notes (",
		"schema /vaults/notes/tusk.toml,",
		"SQLite /vaults/notes/.tusk/index.db)",
	} {
		if !strings.Contains(instructions, want) {
			test.Errorf("instructions missing %q:\n%s", want, instructions)
		}
	}

	if strings.Contains(instructions, "./") || strings.Contains(instructions, "%!") {
		test.Errorf("instructions still name ./ or carry a format error:\n%s", instructions)
	}
}

// TestPerTurnBudget pins #815: every turn of an agent connected to tusk mcp
// carries the server instructions and the definitions of the tools it may
// call, and tusk_query is the one every reader has. Raise a budget on
// purpose, with a benchmark run behind it, not as a side effect of one more
// sentence.
func TestPerTurnBudget(test *testing.T) {
	const instructionsBudget = 360
	const queryToolBudget = 1850

	runtime := &Runtime{
		Root:         "/vaults/notes",
		ManifestPath: "/vaults/notes/tusk.toml",
		IndexPath:    "/vaults/notes/.tusk/index.db",
	}

	if size := len(serverInstructions(runtime)); size > instructionsBudget {
		test.Errorf("server instructions are %d bytes, over the %d budget", size, instructionsBudget)
	}

	queryTool := NewServer(runtime).mcp.GetTool("tusk_query")

	if queryTool == nil {
		test.Fatal("tusk_query is not registered")
	}

	schema, marshalErr := json.Marshal(queryTool.Tool.InputSchema)

	if marshalErr != nil {
		test.Fatalf("marshal tusk_query schema: %v", marshalErr)
	}

	if size := len(queryTool.Tool.Description) + len(schema); size > queryToolBudget {
		test.Errorf("tusk_query description and schema are %d bytes, over the %d budget", size, queryToolBudget)
	}
}
