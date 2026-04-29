package tui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"
)

// expandState carries expander state that must persist across multiple
// expandRefsWithState calls within a single CLI invocation. Currently only
// tracks whether stdin has been consumed, but exists so additional per-
// invocation invariants can be added without changing the call sites again.
type expandState struct {
	stdinConsumed bool
}

// expandRefs resolves `@path` inline file references inside a raw string and
// returns the fully-expanded content. See
// docs/plans/v0.11-string-field-input-unification/design.md for the full spec.
//
// Contract (summary):
//   - `@path` at a word boundary (start-of-string or preceded by space/tab)
//     expands to the contents of the referenced file.
//   - `@"quoted path"` supports paths with spaces.
//   - `@-` reads from stdin once per invocation.
//   - `@@` escapes to a literal `@`.
//   - `@` that is not at a word boundary (e.g. inside "email@example.com")
//     is passed through literally.
//   - Substituted content is NOT re-scanned for further `@` references.
func expandRefs(raw string, stdin *os.File, maxSize int64) (string, error) {
	return expandRefsWithState(raw, stdin, maxSize, &expandState{})
}

// expandRefsWithState is the stateful variant used when a single CLI
// invocation expands multiple independent strings (e.g. both `title=@-` and
// `description=@-`) and must preserve the stdin-once-per-invocation invariant
// across those calls. Callers that expand a single string should use
// expandRefs, which allocates a fresh state internally.
func expandRefsWithState(raw string, stdin *os.File, maxSize int64, state *expandState) (string, error) {
	var out strings.Builder
	out.Grow(len(raw))

	atBoundary := true

	pos := 0
	for pos < len(raw) {
		char := raw[pos]

		if char != '@' || !atBoundary {
			out.WriteByte(char)
			atBoundary = char == ' ' || char == '\t'
			pos++
			continue
		}

		// char == '@' and atBoundary == true.
		next := byte(0)
		if pos+1 < len(raw) {
			next = raw[pos+1]
		}

		// Escape: @@ → literal @.
		if next == '@' {
			out.WriteByte('@')
			atBoundary = false
			pos += 2
			continue
		}

		// Quoted path: @"...".
		if next == '"' {
			body, end, quotedErr := scanQuotedPath(raw, pos+1)

			if quotedErr != nil {
				return "", quotedErr
			}

			if body == "" {
				return "", fmt.Errorf("empty path after @")
			}

			content, contentErr := loadRef(body, stdin, maxSize, state)

			if contentErr != nil {
				return "", contentErr
			}

			out.WriteString(content)
			atBoundary = false
			pos = end
			continue
		}

		// Bare path: scan until whitespace or end-of-string.
		end := pos + 1
		for end < len(raw) && raw[end] != ' ' && raw[end] != '\t' {
			end++
		}
		path := raw[pos+1 : end]
		if path == "" {
			return "", fmt.Errorf("bare @ is not a valid reference")
		}

		content, contentErr := loadRef(path, stdin, maxSize, state)

		if contentErr != nil {
			return "", contentErr
		}

		out.WriteString(content)
		atBoundary = false
		pos = end
	}

	return out.String(), nil
}

// scanQuotedPath reads a quoted string starting at input[pos] (which must be '"').
// Returns the unescaped content (without surrounding quotes), the byte index
// immediately after the closing quote, and an error if the quote is unclosed.
// Supports \" as an escaped literal quote. Mirrors syntax/token.go:226 —
// duplicated here to avoid a cross-package import for a 15-line routine.
func scanQuotedPath(input string, pos int) (string, int, error) {
	cursor := pos + 1
	var buf []byte
	for cursor < len(input) {
		if input[cursor] == '\\' && cursor+1 < len(input) && input[cursor+1] == '"' {
			buf = append(buf, '"')
			cursor += 2
			continue
		}
		if input[cursor] == '"' {
			return string(buf), cursor + 1, nil
		}
		buf = append(buf, input[cursor])
		cursor++
	}
	return "", pos, fmt.Errorf("unclosed quoted path after @")
}

// loadRef resolves a single reference body (file path or "-" for stdin) and
// returns the content. It enforces maxSize, NUL-byte binary detection, and
// the single-stdin-read invariant across all calls that share the state.
func loadRef(path string, stdin *os.File, maxSize int64, state *expandState) (string, error) {
	if path == "-" {
		if state.stdinConsumed {
			return "", fmt.Errorf("@-: stdin referenced more than once in one invocation")
		}
		state.stdinConsumed = true
		return readStdin(stdin, maxSize)
	}
	return readFile(path, maxSize)
}

func readFile(path string, maxSize int64) (string, error) {
	resolved, resolveErr := resolvePath(path)

	if resolveErr != nil {
		return "", fmt.Errorf("@%s: %w", path, resolveErr)
	}

	info, statErr := os.Stat(resolved)

	if statErr != nil {
		if os.IsNotExist(statErr) {
			return "", fmt.Errorf("@%s: no such file", path)
		}
		return "", fmt.Errorf("@%s: %w", path, statErr)
	}

	if info.Size() > maxSize {
		return "", fmt.Errorf("@%s: file is %s, exceeds %s limit for inline expansion",
			path, humanBytes(info.Size()), humanBytes(maxSize))
	}

	data, readErr := os.ReadFile(resolved)

	if readErr != nil {
		return "", fmt.Errorf("@%s: %w", path, readErr)
	}

	if hasNUL(data) {
		return "", fmt.Errorf("@%s: appears to be a binary file; tusk descriptions and annotations must be text (binary file attachments are planned for a future release)", path)
	}
	return string(data), nil
}

func readStdin(stdin *os.File, maxSize int64) (string, error) {
	if stdin == nil {
		return "", fmt.Errorf("@-: stdin is a terminal, not a pipe")
	}
	if term.IsTerminal(int(stdin.Fd())) {
		return "", fmt.Errorf("@-: stdin is a terminal, not a pipe")
	}
	data, readErr := io.ReadAll(io.LimitReader(stdin, maxSize+1))

	if readErr != nil {
		return "", fmt.Errorf("@-: %w", readErr)
	}

	if int64(len(data)) > maxSize {
		return "", fmt.Errorf("@-: stdin is %s, exceeds %s limit for inline expansion",
			humanBytes(int64(len(data)-1)), humanBytes(maxSize))
	}
	if hasNUL(data) {
		return "", fmt.Errorf("@-: appears to be a binary stream; tusk descriptions and annotations must be text (binary file attachments are planned for a future release)")
	}
	return string(data), nil
}

// hasNUL scans the first 8 KB of data for a NUL byte. A NUL anywhere in that
// window marks the input as binary. NUL bytes past the scan window are a
// documented limitation of the heuristic.
func hasNUL(data []byte) bool {
	scanLen := len(data)
	if scanLen > 8192 {
		scanLen = 8192
	}
	for index := 0; index < scanLen; index++ {
		if data[index] == 0 {
			return true
		}
	}
	return false
}

// resolvePath expands a leading ~ to the user's home directory. Bare relative
// and absolute paths are returned unchanged — os.ReadFile resolves them against
// the current working directory already.
func resolvePath(path string) (string, error) {
	if path == "~" {
		home, homeErr := os.UserHomeDir()

		if homeErr != nil {
			return "", homeErr
		}

		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		home, homeErr := os.UserHomeDir()

		if homeErr != nil {
			return "", homeErr
		}

		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	}
	return path, nil
}

// humanBytes renders a byte count as "1.0 MB", "512.0 KB", etc. Local helper —
// no new dependency. Only used for error messages.
func humanBytes(byteCount int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case byteCount >= gb:
		return fmt.Sprintf("%.1f GB", float64(byteCount)/float64(gb))
	case byteCount >= mb:
		return fmt.Sprintf("%.1f MB", float64(byteCount)/float64(mb))
	case byteCount >= kb:
		return fmt.Sprintf("%.1f KB", float64(byteCount)/float64(kb))
	default:
		return fmt.Sprintf("%d B", byteCount)
	}
}

// expandRefs is the App-scoped wrapper that threads the configured per-reference
// size limit. Command code should prefer this over the free function so the
// limit stays consistent with config.
func (app *App) expandRefs(raw string, stdin *os.File) (string, error) {
	return expandRefs(raw, stdin, app.inlineCfg.MaxExpansionSize)
}

// expandRefsWithState is the App-scoped stateful wrapper. Use this when a
// single command invocation expands multiple strings that must share the
// stdin-once-per-invocation invariant.
func (app *App) expandRefsWithState(raw string, stdin *os.File, state *expandState) (string, error) {
	return expandRefsWithState(raw, stdin, app.inlineCfg.MaxExpansionSize, state)
}
