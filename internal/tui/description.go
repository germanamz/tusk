package tui

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// readDescription resolves a --description flag value to its content.
// If value starts with "@", it reads from a file path (or stdin for "@-").
// Otherwise, the value is returned as-is.
// stdin should be an *os.File (e.g. os.Stdin) for production use, or an
// *os.File from os.Pipe() for tests. The TTY check uses term.IsTerminal
// on the file descriptor.
// NOTE: When bubbletea is introduced in v0.6, the TTY detection strategy
// may need to change since bubbletea manages its own terminal state.
func readDescription(value string, stdin *os.File) (string, error) {
	if !strings.HasPrefix(value, "@") {
		return value, nil
	}

	path := value[1:]

	if path == "-" {
		if stdin == nil || term.IsTerminal(int(stdin.Fd())) {
			return "", fmt.Errorf("stdin is a terminal, not a pipe")
		}
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("reading description from stdin: %w", err)
		}
		return string(data), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read description file: %w", err)
	}
	return string(data), nil
}
