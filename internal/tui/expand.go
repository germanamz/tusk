package tui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"
)

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
	var out strings.Builder
	out.Grow(len(raw))

	stdinConsumed := false
	atBoundary := true

	i := 0
	for i < len(raw) {
		c := raw[i]

		if c != '@' || !atBoundary {
			out.WriteByte(c)
			atBoundary = c == ' ' || c == '\t'
			i++
			continue
		}

		// c == '@' and atBoundary == true.
		next := byte(0)
		if i+1 < len(raw) {
			next = raw[i+1]
		}

		// Escape: @@ → literal @.
		if next == '@' {
			out.WriteByte('@')
			atBoundary = false
			i += 2
			continue
		}

		// Quoted path: @"...".
		if next == '"' {
			body, end, err := scanQuotedPath(raw, i+1)
			if err != nil {
				return "", err
			}
			if body == "" {
				return "", fmt.Errorf("empty path after @")
			}
			content, err := loadRef(body, stdin, maxSize, &stdinConsumed)
			if err != nil {
				return "", err
			}
			out.WriteString(content)
			atBoundary = false
			i = end
			continue
		}

		// Bare path: scan until whitespace or end-of-string.
		j := i + 1
		for j < len(raw) && raw[j] != ' ' && raw[j] != '\t' {
			j++
		}
		path := raw[i+1 : j]
		if path == "" {
			return "", fmt.Errorf("bare @ is not a valid reference")
		}

		content, err := loadRef(path, stdin, maxSize, &stdinConsumed)
		if err != nil {
			return "", err
		}
		out.WriteString(content)
		atBoundary = false
		i = j
	}

	return out.String(), nil
}

// scanQuotedPath reads a quoted string starting at input[pos] (which must be '"').
// Returns the unescaped content (without surrounding quotes), the byte index
// immediately after the closing quote, and an error if the quote is unclosed.
// Supports \" as an escaped literal quote. Mirrors syntax/token.go:226 —
// duplicated here to avoid a cross-package import for a 15-line routine.
func scanQuotedPath(input string, pos int) (string, int, error) {
	i := pos + 1
	var buf []byte
	for i < len(input) {
		if input[i] == '\\' && i+1 < len(input) && input[i+1] == '"' {
			buf = append(buf, '"')
			i += 2
			continue
		}
		if input[i] == '"' {
			return string(buf), i + 1, nil
		}
		buf = append(buf, input[i])
		i++
	}
	return "", pos, fmt.Errorf("unclosed quoted path after @")
}

// loadRef resolves a single reference body (file path or "-" for stdin) and
// returns the content. It enforces maxSize, NUL-byte binary detection, and
// the single-stdin-read invariant.
func loadRef(path string, stdin *os.File, maxSize int64, stdinConsumed *bool) (string, error) {
	if path == "-" {
		if *stdinConsumed {
			return "", fmt.Errorf("@-: stdin referenced more than once in one invocation")
		}
		*stdinConsumed = true
		return readStdin(stdin, maxSize)
	}
	return readFile(path, maxSize)
}

func readFile(path string, maxSize int64) (string, error) {
	resolved, err := resolvePath(path)
	if err != nil {
		return "", fmt.Errorf("@%s: %w", path, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("@%s: no such file", path)
		}
		return "", fmt.Errorf("@%s: %w", path, err)
	}
	if info.Size() > maxSize {
		return "", fmt.Errorf("@%s: file is %s, exceeds %s limit for inline expansion",
			path, humanBytes(info.Size()), humanBytes(maxSize))
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("@%s: %w", path, err)
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
	data, err := io.ReadAll(io.LimitReader(stdin, maxSize+1))
	if err != nil {
		return "", fmt.Errorf("@-: %w", err)
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
	n := len(data)
	if n > 8192 {
		n = 8192
	}
	for i := 0; i < n; i++ {
		if data[i] == 0 {
			return true
		}
	}
	return false
}

// resolvePath expands a leading ~ to the user's home directory. Bare relative
// and absolute paths are returned unchanged — os.ReadFile resolves them against
// the current working directory already.
func resolvePath(p string) (string, error) {
	if p == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return home, nil
	}
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, strings.TrimPrefix(p, "~/")), nil
	}
	return p, nil
}

// humanBytes renders a byte count as "1.0 MB", "512.0 KB", etc. Local helper —
// no new dependency. Only used for error messages.
func humanBytes(n int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// expandRefs is the App-scoped wrapper that threads the configured per-reference
// size limit. Command code should prefer this over the free function so the
// limit stays consistent with config. Unused in phase 1 — phase 2 wires it in.
//
//nolint:unused // wired by v0.11 phase 2
func (a *App) expandRefs(raw string, stdin *os.File) (string, error) {
	return expandRefs(raw, stdin, a.inlineCfg.MaxExpansionSize)
}
