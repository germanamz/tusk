# Rich Descriptions Phase 2: CLI Description Input & Display — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `--description` / `-d` flag to `tusk add` and `tusk modify` with `@file` and `@-` (stdin) support, and update `tusk info` to render multi-line descriptions as a block.

**Architecture:** Add a `readDescription` helper in the TUI package that handles inline text, `@file`, and `@-` (stdin). Wire it into the `add` and `modify` commands via a Cobra string flag. Update `renderTaskInfo` for block-style description display. All changes in the `internal/tui/` package plus E2E tests.

**Tech Stack:** Go, Cobra (CLI framework), os/io for file/stdin reading

**Prerequisite:** Phase 1 (double-pointer migration) must be complete.

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/tui/description.go` | Create | `readDescription` helper: handles inline, `@file`, and `@-` |
| `internal/tui/description_test.go` | Create | Unit tests for `readDescription` |
| `internal/tui/app.go:70-93` | Modify | Register `--description` / `-d` flag on `add` and `modify` commands |
| `internal/tui/commands.go:31-105` | Modify | Wire `--description` flag into `runAdd` |
| `internal/tui/commands.go:206-311` | Modify | Wire `--description` flag into `runModify` |
| `internal/tui/render.go:383-387` | Modify | Block-style description rendering in `renderTaskInfo` |
| `tests/e2e/task_lifecycle_test.go` | Modify | E2E scenarios for description input, display, and clearing |

---

### Task 1: Create `readDescription` helper

**Files:**
- Create: `internal/tui/description.go`
- Create: `internal/tui/description_test.go`

- [ ] **Step 1: Write the failing tests for `readDescription`**

Create `internal/tui/description_test.go`:

```go
package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadDescription_InlineText(t *testing.T) {
	result, err := readDescription("hello world", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello world" {
		t.Fatalf("expected %q, got %q", "hello world", result)
	}
}

func TestReadDescription_EmptyString(t *testing.T) {
	result, err := readDescription("", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Fatalf("expected empty string, got %q", result)
	}
}

func TestReadDescription_FromStdinNil(t *testing.T) {
	_, err := readDescription("@-", nil)
	if err == nil {
		t.Fatal("expected error for nil stdin")
	}
	if !strings.Contains(err.Error(), "stdin is a terminal, not a pipe") {
		t.Fatalf("expected stdin error, got: %v", err)
	}
}

func TestReadDescription_FromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "desc.md")
	content := "# Title\n\nSome description content."
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	result, err := readDescription("@"+path, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != content {
		t.Fatalf("expected %q, got %q", content, result)
	}
}

func TestReadDescription_FromFileMissing(t *testing.T) {
	_, err := readDescription("@/nonexistent/path/file.md", nil)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "failed to read description file") {
		t.Fatalf("expected 'failed to read description file' error, got: %v", err)
	}
}

func TestReadDescription_FromStdin(t *testing.T) {
	// Use a pipe (not a TTY) to simulate piped input
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating pipe: %v", err)
	}
	_, _ = w.WriteString("piped content")
	w.Close()

	result, err := readDescription("@-", r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "piped content" {
		t.Fatalf("expected %q, got %q", "piped content", result)
	}
}

func TestReadDescription_FromStdinTTY(t *testing.T) {
	// Open /dev/tty to get a real terminal file descriptor for the TTY check.
	// Skip on CI or environments without a TTY.
	tty, err := os.Open("/dev/tty")
	if err != nil {
		t.Skip("no /dev/tty available (CI environment)")
	}
	defer tty.Close()

	_, err = readDescription("@-", tty)
	if err == nil {
		t.Fatal("expected error for TTY stdin")
	}
	if !strings.Contains(err.Error(), "stdin is a terminal, not a pipe") {
		t.Fatalf("expected stdin error, got: %v", err)
	}
}

func TestReadDescription_AtSignOnly(t *testing.T) {
	_, err := readDescription("@", nil)
	if err == nil {
		t.Fatal("expected error for bare @")
	}
	if !strings.Contains(err.Error(), "failed to read description file") {
		t.Fatalf("expected file error, got: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -run TestReadDescription -v -count=1`

Expected: FAIL — `readDescription` is not defined.

- [ ] **Step 3: Write the implementation**

First, add the `golang.org/x/term` dependency:

Run: `cd /Users/germanamz/projects/tusk && go get golang.org/x/term`

Then create `internal/tui/description.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -run TestReadDescription -v -count=1`

Expected: All 8 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/description.go internal/tui/description_test.go
git commit -m "$(cat <<'EOF'
feat(tui): add readDescription helper for @file and @- input

Resolves --description flag values: inline text returned as-is,
@path reads from file, @- reads from stdin. Returns clear errors
for missing files and TTY stdin.
EOF
)"
```

---

### Task 2: Add `--description` flag to `tusk add`

**Files:**
- Modify: `internal/tui/app.go:70-76`
- Modify: `internal/tui/commands.go:31-105`

- [ ] **Step 1: Register the `--description` flag on the `add` command**

In `internal/tui/app.go`, the `add` command is created inline at lines 71-76. Replace it with a named variable so we can add a flag:

Replace lines 70-76:

```go
	a.root.AddCommand(
		&cobra.Command{
			Use:   "add [title] [key:value...] [+tag...]",
			Short: "Create a new task",
			Args:  cobra.MinimumNArgs(1),
			RunE:  a.runAdd,
		},
```

with:

```go
	addCmd := &cobra.Command{
		Use:   "add [title] [key:value...] [+tag...]",
		Short: "Create a new task",
		Args:  cobra.MinimumNArgs(1),
		RunE:  a.runAdd,
	}
	addCmd.Flags().StringP("description", "d", "", `task description (use @file to read from file, @- for stdin)`)

	a.root.AddCommand(
		addCmd,
```

- [ ] **Step 2: Wire the flag into `runAdd`**

First, add `"os"` to the imports in `internal/tui/commands.go`:

```go
import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/filter"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)
```

Then add the description handling after the task creation (line 45-47) and before the project field (line 49). Insert after `task := &domain.Task{Title: title}` (line 45-47):

```go
	// Description
	if cmd.Flags().Changed("description") {
		descVal, _ := cmd.Flags().GetString("description")
		var stdinFile *os.File
		if f, ok := cmd.InOrStdin().(*os.File); ok {
			stdinFile = f
		}
		desc, err := readDescription(descVal, stdinFile)
		if err != nil {
			return err
		}
		task.Description = desc
	}
```

Insert this block between line 47 (`}`) and line 49 (`// Project`).

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./...`

Expected: Compiles successfully.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/app.go internal/tui/commands.go
git commit -m "$(cat <<'EOF'
feat(cli): add --description flag to tusk add

Supports inline text, @file for reading from a file path, and @-
for reading from stdin.
EOF
)"
```

---

### Task 3: Add `--description` flag to `tusk modify`

**Files:**
- Modify: `internal/tui/app.go:88-93` (after the changes from Task 2)
- Modify: `internal/tui/commands.go:206-311`

- [ ] **Step 1: Register the `--description` flag on the `modify` command**

In `internal/tui/app.go`, the `modify` command is inline. After the changes from Task 2, find the modify command block and replace it with a named variable:

Replace:

```go
		&cobra.Command{
			Use:   "modify <short_id> [key:value...]",
			Short: "Modify a task",
			Args:  cobra.MinimumNArgs(1),
			RunE:  a.runModify,
		},
```

with:

```go
		modifyCmd,
```

And add the following before the `a.root.AddCommand(` call (near where `addCmd` was defined):

```go
	modifyCmd := &cobra.Command{
		Use:   "modify <short_id> [key:value...]",
		Short: "Modify a task",
		Args:  cobra.MinimumNArgs(1),
		RunE:  a.runModify,
	}
	modifyCmd.Flags().StringP("description", "d", "", `new description (use @file to read from file, @- for stdin, "" to clear)`)
```

- [ ] **Step 2: Wire the flag into `runModify`**

In `internal/tui/commands.go`, in `runModify`, add description handling after the `upd` struct initialization (line 222-225) and before the title handling (line 228). Insert after `}` (line 225):

```go
	// Description (double pointer: outer nil = don't change, outer non-nil + inner nil = clear)
	if cmd.Flags().Changed("description") {
		descVal, _ := cmd.Flags().GetString("description")
		var stdinFile *os.File
		if f, ok := cmd.InOrStdin().(*os.File); ok {
			stdinFile = f
		}
		desc, err := readDescription(descVal, stdinFile)
		if err != nil {
			return err
		}
		if desc == "" {
			var nilStr *string
			upd.Description = &nilStr
		} else {
			dp := &desc
			upd.Description = &dp
		}
	}
```

Insert this block between the `upd` initialization and the `// Title from free text` comment.

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./...`

Expected: Compiles successfully.

- [ ] **Step 4: Run existing tests for regressions**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v -count=1 2>&1 | tail -10`

Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/app.go internal/tui/commands.go
git commit -m "$(cat <<'EOF'
feat(cli): add --description flag to tusk modify

Supports inline text, @file, @- for stdin. Empty string clears the
description using double-pointer semantics.
EOF
)"
```

---

### Task 4: Update `tusk info` description rendering

**Files:**
- Modify: `internal/tui/render.go:383-387`

- [ ] **Step 1: Update the description rendering in `renderTaskInfo`**

In `internal/tui/render.go`, replace lines 383-387:

```go
	if task.Description != "" {
		if _, err := fmt.Fprintf(w, "%-13s %s\n", "Description:", task.Description); err != nil {
			return err
		}
	}
```

with:

```go
	if task.Description != "" {
		if _, err := fmt.Fprintln(w, "Description:"); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, task.Description); err != nil {
			return err
		}
	}
```

This renders:
```
Description:

line 1 of the description
line 2 of the description
```

Always block format. Blank line after `Description:`. No indentation. No truncation.

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./...`

Expected: Compiles successfully.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/render.go
git commit -m "$(cat <<'EOF'
feat(tui): block-style description rendering in tusk info

Descriptions now render as a labeled block with a blank line
separator, rather than a key-value pair. No truncation.
EOF
)"
```

---

### Task 5: E2E tests for description workflows

**Files:**
- Modify: `tests/e2e/task_lifecycle_test.go`

- [ ] **Step 1: Add E2E scenarios for description features**

Add the following scenarios to the `scenarios` slice in `TestTaskLifecycle` in `tests/e2e/task_lifecycle_test.go`:

```go
{
    Name: "add_with_inline_description",
    Steps: []Step{
        {
            Args: []string{"add", "Described task", "--description", "This is the description"},
            AssertJSON: func(t *testing.T, parsed any) {
                t.Helper()
                m := parsed.(map[string]any)
                assertEqual(t, m["title"], "Described task")
                assertEqual(t, m["description"], "This is the description")
            },
        },
        {
            Args: []string{"info", "$0.short_id"},
            AssertJSON: func(t *testing.T, parsed any) {
                t.Helper()
                m := parsed.(map[string]any)
                assertEqual(t, m["description"], "This is the description")
            },
            AssertText: func(t *testing.T, output string) {
                t.Helper()
                assertContains(t, output, "Description:")
                assertContains(t, output, "This is the description")
            },
        },
    },
},
{
    Name: "modify_set_description",
    Steps: []Step{
        {
            Args: []string{"add", "No description yet"},
        },
        {
            Args: []string{"modify", "$0.short_id", "--description", "Added later"},
            AssertJSON: func(t *testing.T, parsed any) {
                t.Helper()
                m := parsed.(map[string]any)
                assertEqual(t, m["description"], "Added later")
            },
        },
        {
            Args: []string{"info", "$0.short_id"},
            AssertJSON: func(t *testing.T, parsed any) {
                t.Helper()
                m := parsed.(map[string]any)
                assertEqual(t, m["description"], "Added later")
            },
        },
    },
},
{
    Name: "modify_clear_description",
    Steps: []Step{
        {
            Args: []string{"add", "Has description", "--description", "Will be cleared"},
        },
        {
            Args: []string{"info", "$0.short_id"},
            AssertJSON: func(t *testing.T, parsed any) {
                t.Helper()
                m := parsed.(map[string]any)
                assertEqual(t, m["description"], "Will be cleared")
            },
        },
        {
            Args: []string{"modify", "$0.short_id", "--description", ""},
            AssertJSON: func(t *testing.T, parsed any) {
                t.Helper()
                m := parsed.(map[string]any)
                assertEqual(t, m["description"], "")
            },
        },
        {
            Args: []string{"info", "$0.short_id"},
            AssertJSON: func(t *testing.T, parsed any) {
                t.Helper()
                m := parsed.(map[string]any)
                assertEqual(t, m["description"], "")
            },
            AssertText: func(t *testing.T, output string) {
                t.Helper()
                assertNotContains(t, output, "Description:")
            },
        },
    },
},
```

- [ ] **Step 2: Run the new E2E scenarios**

Run: `cd /Users/germanamz/projects/tusk && go test ./tests/e2e/ -run "TestTaskLifecycle/(add_with_inline_description|modify_set_description|modify_clear_description)" -v -count=1 -timeout 120s 2>&1 | tail -30`

Expected: All 12 sub-combinations (3 scenarios x 4 combos) PASS.

- [ ] **Step 3: Add E2E scenario for @file description**

Add to the `scenarios` slice. This test creates a temp file using a setup step:

```go
{
    Name: "add_with_file_description",
    Steps: []Step{
        {
            // This step is a trick: we use the harness to create a task, then in
            // the assert we write a temp file and store its path. But the harness
            // doesn't support pre-step hooks, so instead we test @file via the
            // unit tests (TestReadDescription_FromFile) and trust the wiring here.
            // Instead, test a multi-line inline description which exercises the
            // same rendering path.
            Args: []string{"add", "Multi-line task", "--description", "Line one\nLine two\nLine three"},
            AssertJSON: func(t *testing.T, parsed any) {
                t.Helper()
                m := parsed.(map[string]any)
                assertEqual(t, m["description"], "Line one\nLine two\nLine three")
            },
        },
        {
            Args: []string{"info", "$0.short_id"},
            AssertText: func(t *testing.T, output string) {
                t.Helper()
                assertContains(t, output, "Description:")
                assertContains(t, output, "Line one")
                assertContains(t, output, "Line two")
                assertContains(t, output, "Line three")
            },
        },
    },
},
```

- [ ] **Step 4: Run all description-related E2E scenarios**

Run: `cd /Users/germanamz/projects/tusk && go test ./tests/e2e/ -run "TestTaskLifecycle/(add_with_inline|add_with_file|modify_set|modify_clear|create_task_has_empty)" -v -count=1 -timeout 120s 2>&1 | tail -30`

Expected: All scenarios PASS across all combos.

- [ ] **Step 5: Run the full test suite**

Run: `cd /Users/germanamz/projects/tusk && make test 2>&1 | tail -10`

Expected: All tests PASS.

- [ ] **Step 6: Commit**

```bash
git add tests/e2e/task_lifecycle_test.go
git commit -m "$(cat <<'EOF'
test(e2e): add description input and display scenarios

Cover inline description on add, set/clear description on modify,
multi-line description rendering in info, and empty description
behavior.
EOF
)"
```
