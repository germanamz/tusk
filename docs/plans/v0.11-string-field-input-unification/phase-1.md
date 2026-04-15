# Phase 1 — Config section + expander helper

**Initiative:** String Field Input Unification (v0.11)
**Design spec:** `docs/plans/v0.11-string-field-input-unification/design.md`
**Prerequisites:** None beyond base codebase.

## Goal

Introduce the inline-reference expander as pure infrastructure: the `[inline]` config section, the `expandRefs` function, and its unit tests. No consumer command is rewired in this phase — the existing `--description` flag and `internal/tui/description.go` stay in place and continue to work. The code ships in a state where the new helper exists and is covered by tests, but is unused by any command. Phase 2 wires it in.

The split exists so the expander can land behind a full unit-test gate without colliding with the command rewire and the e2e sweep. If phase 2 is delayed, phase 1 is still mergeable.

## User-visible behaviors that must continue to work after this phase

- `tusk task create "title" -d "body"` and `--description "body"` still set the description.
- `tusk task create "title" --description @./file.md` still loads the description from a file (via `readDescription` in `description.go`, unchanged).
- `tusk task modify <id> -d "body"` still replaces the description.
- `tusk task modify <id> -d ""` still clears the description.
- `tusk task annotate <id> "body"` still annotates (no file expansion yet — that lands in phase 2).
- `config show` still loads and renders the full config. A missing `[inline]` section in a user's config file must fall through to the default (1 MB) without error.
- All existing unit and e2e tests pass.

## Tasks

### Task 1 — Add `InlineConfig` type and wire into `Config`

**File:** `config/config.go`

Add a new type alongside the existing section types:

```go
type InlineConfig struct {
    MaxExpansionSize int64 `mapstructure:"max_expansion_size"`
}
```

Add a field to the top-level `Config` struct:

```go
type Config struct {
    // ... existing fields ...
    Inline InlineConfig `mapstructure:"inline"`
}
```

In the Viper setup (wherever defaults are registered — check `config/config.go` for the existing `viper.SetDefault` or `mapstructure` default pattern; match whatever the other sections do), register the default:

- `inline.max_expansion_size = 1048576` (1 MB, int64).

In the config validation pass (wherever `[urgency]`, `[storage]`, etc. are validated — check the existing `validate` helper or equivalent), add:

- Reject `Inline.MaxExpansionSize <= 0` with an error message like: `invalid config: inline.max_expansion_size must be > 0, got <value>`.

No upper bound. A user who raises it knows what they are doing.

### Task 2 — Add `[inline]` section to `config/default.toml`

**File:** `config/default.toml`

Append a new section (order it near the other small sections like `[mcp]` or `[filter]` — do not interleave with `[urgency]` or `[storage]`):

```toml
[inline]
# Maximum byte size of a single @file expansion on inline string fields.
# Applied per reference, not per invocation — three 900 KB expansions in
# one description are allowed under the default.
max_expansion_size = 1048576
```

The file is embedded into the binary via `go:embed`; no further wiring is needed beyond the write.

Verify: `make build && ./bin/tusk config show` must render the `[inline]` section with the default value and exit cleanly.

### Task 3 — Write `internal/tui/expand.go`

**File:** `internal/tui/expand.go` (new file)

Implement the expander as specified in the design spec's "Expander" section. Summary of requirements:

**Free function signature:**

```go
func expandRefs(raw string, stdin *os.File, maxSize int64) (string, error)
```

**Method wrapper on `App`** so command code can call `a.expandRefs(raw, stdin)` without threading the size limit manually:

```go
func (a *App) expandRefs(raw string, stdin *os.File) (string, error) {
    return expandRefs(raw, stdin, a.cfg.Inline.MaxExpansionSize)
}
```

**Algorithm (single forward scan):**

1. Walk `raw` byte-by-byte. Track `atBoundary bool` — true at start; true whenever the previous emitted byte was `' '` or `'\t'`.
2. When `raw[i] == '@'` and `atBoundary`:
   - If `raw[i+1] == '@'`: emit a literal `'@'`, advance `i` by 2, set `atBoundary = false`.
   - If `raw[i+1] == '"'`: scan a quoted path via `scanQuoted` from `syntax` package (re-exported or called directly — check if it is exported; if not, duplicate the 20-line routine into `expand.go` and note it as a local helper with a comment pointing at `syntax/token.go:226` as the reference implementation). The quoted span yields the path. Resolve the file and append content to the output buffer.
   - Otherwise: scan a bare path — advance `j` from `i+1` until `raw[j]` is `' '`, `'\t'`, or end-of-string. `path := raw[i+1:j]`. Resolve and append.
   - If the resolved path is exactly `-` (bare, unquoted), read from stdin instead of a file.
3. Any other byte: append to output buffer, update `atBoundary` based on whether the byte is `' '` or `'\t'`.

**File read:**

1. `info, err := os.Stat(path)`; on `os.IsNotExist(err)` return `fmt.Errorf("@%s: no such file", path)`. Wrap other errors with the same prefix.
2. If `info.Size() > maxSize`, return `fmt.Errorf("@%s: file is %s, exceeds %s limit for inline expansion", path, humanBytes(info.Size()), humanBytes(maxSize))`. Use a small internal `humanBytes(int64) string` helper that emits `"1.0 MB"` style — write it inline in `expand.go`, no new dependency.
3. `data, err := os.ReadFile(path)`; wrap errors with the `@%s:` prefix.
4. NUL-byte scan on `data[:min(8192, len(data))]`. If any NUL byte, return `fmt.Errorf("@%s: appears to be a binary file; tusk descriptions and annotations must be text. binary file attachments are planned for a future release.", path)`.
5. Append `string(data)` to the output buffer.

**Stdin read:**

1. If `stdin == nil || term.IsTerminal(int(stdin.Fd()))`, return `fmt.Errorf("@-: stdin is a terminal, not a pipe")`. This preserves the `readDescription` TTY guard.
2. `data, err := io.ReadAll(io.LimitReader(stdin, maxSize+1))`; wrap errors.
3. If `int64(len(data)) > maxSize`, return an over-cap error (stdin size reported as `len(data)` minus 1 to avoid misreporting).
4. NUL-byte scan on first 8 KB of `data`.
5. Append `string(data)` to the buffer.
6. Track stdin consumption with a local `stdinConsumed bool`; a second `@-` in the same call returns `fmt.Errorf("@-: stdin referenced more than once in one invocation")`.

**Edge cases the algorithm must reject with errors:**

- `"foo @ bar"` → `bare @ is not a valid reference`.
- `"@"` alone at end of string → same.
- `"@\""` (unclosed quoted path) → `unclosed quoted path after @`.
- `@""` (empty quoted path) → `empty path after @`.

**Edge cases the algorithm must handle silently (no expansion, literal passthrough):**

- `"email@example.com"` → `@` is word-internal; `atBoundary` is false when the `@` byte is visited because the previous byte `'l'` is not whitespace. No expansion, emit `'@'` literally.
- `"user@host"` → same.

**Relative paths:** Resolve via `os.ReadFile(path)` directly. Go's `os.ReadFile` already handles CWD-relative paths; absolute paths and paths with `~` prefix must be expanded first. Add a small helper `resolvePath(p string) (string, error)`:

```go
func resolvePath(p string) (string, error) {
    if strings.HasPrefix(p, "~/") || p == "~" {
        home, err := os.UserHomeDir()
        if err != nil {
            return "", err
        }
        return filepath.Join(home, strings.TrimPrefix(p, "~")), nil
    }
    return p, nil
}
```

**Imports to add:**

```go
import (
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strings"

    "golang.org/x/term"
)
```

**Re-scanning rule:** Substituted content is NOT re-scanned for `@` references. The outer scan resumes at the byte immediately after the consumed reference, not inside the substituted content. This must be enforced by writing the algorithm as an append-to-output-buffer pattern rather than a string-replace-and-restart pattern.

**Do not delete `internal/tui/description.go` in this phase.** It stays in place and is still the thing the commands call. Deletion happens in phase 2.

### Task 4 — Write `internal/tui/expand_test.go`

**File:** `internal/tui/expand_test.go` (new file)

Unit tests covering every semantic branch. Each test uses `t.TempDir()` to create isolated files, and `os.Pipe()` pairs for stdin cases.

Required cases:

1. **Whole-value file load:** input `"@./foo.txt"`, file contains `"hello"`, expect output `"hello"`.
2. **Mid-string expansion:** input `"prefix @./foo.txt suffix"`, expect `"prefix hello suffix"`.
3. **Multiple references in one input:** `"a @./one.txt b @./two.txt"` — both resolved.
4. **Quoted path with space:** `@"./my file.txt"` — file named `my file.txt` resolved.
5. **`@@` escape:** input `"@@mention please"`, expect `"@mention please"` (literal `@` emitted, rest passed through).
6. **`@@` mid-string:** input `"hi @@literal bye"`, expect `"hi @literal bye"`.
7. **Word-internal `@`:** input `"email@example.com"`, expect unchanged. No file read attempted.
8. **Bare `@` alone:** input `"foo @ bar"` → error `bare @ is not a valid reference`.
9. **Bare `@` at end:** input `"trailing @"` → same error.
10. **Empty quoted path:** input `@""` → error `empty path after @`.
11. **Unclosed quoted path:** input `@"./baz` → error `unclosed quoted path after @`.
12. **Missing file:** input `"@./nope.txt"` → error message contains `no such file` and the path.
13. **Binary file rejection:** file containing a NUL byte in the first 8 KB → error message contains `binary file` and the path.
14. **Binary file with NUL after 8 KB:** file where the NUL byte is at offset 10000 → expansion succeeds (NUL past the scan window is ignored by design). This is a documented limitation of the NUL-byte heuristic; the test locks it in.
15. **Over size cap:** file larger than `maxSize` → error message contains both sizes. Pass `maxSize = 100` and a 200-byte file.
16. **Stdin happy path:** `@-`, pipe has `"from stdin"`, expect `"from stdin"`. Use `os.Pipe()` and write via a goroutine.
17. **Stdin mid-string:** `"prefix @- suffix"` with piped content `"X"`, expect `"prefix X suffix"`.
18. **Stdin referenced twice:** `"@- @-"` → error `stdin referenced more than once`.
19. **Stdin is TTY:** pass `nil` for `stdin` → error `stdin is a terminal, not a pipe`. Skip the actual TTY detection on `nil` by checking nil first in the expander.
20. **Substituted content not re-scanned:** file content is `"@./other.txt"`, `other.txt` also exists with content `"nested"`. Expansion of `"@./first.txt"` yields the literal string `"@./other.txt"`, not `"nested"`.
21. **Relative path resolution:** change to `t.TempDir()` via `t.Chdir()` (Go 1.24) or `os.Chdir` + cleanup, place a file there, reference as `@./x.txt`, expect it to resolve against the CWD.
22. **Home-relative path:** set `HOME` env to a temp dir via `t.Setenv`, place a file at `$HOME/tusk-test.txt`, reference as `@~/tusk-test.txt`, expect it to resolve.
23. **Empty input:** input `""`, expect `""` and no error.
24. **No `@` at all:** input `"plain text no refs"`, expect unchanged.

Every test asserts both the returned string and the error (nil or containing the expected substring). Do not assert exact error strings — use `strings.Contains` on the substring markers listed in each case. This keeps the tests resilient to minor wording tweaks while still locking in the error taxonomy.

Fixture files live in `t.TempDir()` per test — no shared `testdata/` additions.

## User-visible behaviors to preserve at phase completion

- `make build` succeeds.
- `make test` passes — all existing tests (including `TestReadDescription`) still run untouched.
- `make lint` passes — no unused imports, no unused package-level funcs (Go allows the latter but `golangci-lint`'s `unused` linter does not flag package-level funcs unless `-E deadcode` is enabled; confirm the lint config does not fail on `expandRefs` being uncalled).
- `tusk config show` renders the new `[inline]` section with `max_expansion_size = 1048576`.
- Every old `--description @file` flow still works because `description.go` is untouched.

## Changes Introduced

**New files:**

- `internal/tui/expand.go` — the expander implementation and the `App.expandRefs` method wrapper.
- `internal/tui/expand_test.go` — unit tests covering every branch of the algorithm.

**Modified files:**

- `config/config.go` — new `InlineConfig` type, `Config.Inline` field, default registration, validation clause.
- `config/default.toml` — new `[inline]` section with `max_expansion_size = 1048576`.

**New interfaces:**

- `func expandRefs(raw string, stdin *os.File, maxSize int64) (string, error)` — package-private free function in `internal/tui`.
- `func (a *App) expandRefs(raw string, stdin *os.File) (string, error)` — thin method wrapper.

**New config keys:** `inline.max_expansion_size` (int64, default `1048576`).

**Schema migrations:** None.

**Environment variables:** None new. Existing `TUSK_INLINE_MAX_EXPANSION_SIZE` override is automatic via Viper's `TUSK_` prefix — document it in phase 3 when `docs/configuration.md` is updated.

**Dependencies:** None new. `golang.org/x/term` is already in `go.mod` (used by `description.go`).

**Bridge code:** None. The expander is unused by any command in this phase, which is the intended state. Phase 2 introduces the wiring and deletes the old `description.go` path.
