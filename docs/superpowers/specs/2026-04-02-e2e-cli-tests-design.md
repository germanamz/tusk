# Automated End-to-End CLI Tests

## Purpose

Regression test suite that exercises the compiled `tusk` binary as a black box. Every scenario runs the real binary via `exec.Command` against a temp SQLite database, asserting on stdout/stderr/exit code. No in-process execution — tests validate the same path users take.

## Execution Matrix

Every scenario runs **4 times** via a cartesian product:

| Axis       | Values         |
|------------|----------------|
| DB mode    | `--db` flag, `TUSK_DB` env var |
| Format     | `--format text`, `--format json` |

A generic `combinations()` function produces the cartesian product of any number of string slices. Adding a new axis (e.g. WAL mode) requires no structural changes.

Each combination gets its own temp DB file — fully independent and parallelizable.

## File Layout

```
tests/
└── e2e/
    ├── harness.go                # Env, Result, combinations(), runScenarios(), assertion helpers
    ├── main_test.go              # TestMain — builds binary once, stores path in package var
    ├── task_lifecycle_test.go    # add → list → info → modify → start → done → delete
    ├── filtering_test.go         # list with status, project, priority, tags, combined filters
    ├── tags_test.go              # tag assignment, +/-tag, multi-tag, filtering by tag
    ├── annotations_test.go       # annotate, verify in info, multiple annotations
    ├── error_handling_test.go    # not found, invalid transitions, bad args, invalid filters
    └── output_format_test.go    # text columns/symbols, JSON structure/keys, empty outputs
```

- `harness.go` is a non-test file with shared helpers for the `e2e` package
- `main_test.go` owns `TestMain` which runs `go build -o <tmpdir>/tusk ./cmd/tusk` once
- Each `*_test.go` defines scenarios as `[]Scenario` and calls `runScenarios(t, scenarios)`
- Package: `e2e_test` — external test package, no internal imports

## Test Harness

### Env

```go
type Env struct {
    t       *testing.T
    binPath string   // built once in TestMain
    dbPath  string   // temp file per test
    dbMode  string   // "flag" | "env"
    format  string   // "text" | "json"
}
```

`Env.Run(args ...string) Result` builds the full command:
- **flag mode**: prepends `--db <dbPath>` and appends `--format <format>` to args
- **env mode**: sets `TUSK_DB=<dbPath>` in the subprocess environment, appends `--format <format>`

### Result

```go
type Result struct {
    Stdout string
    Stderr string
    Err    error
}
```

### Scenario and Step

```go
type Step struct {
    Args       []string                        // CLI args (db/format injected by Env)
    WantErr    bool                            // expect non-zero exit code
    Assert     func(t *testing.T, r Result)    // format-agnostic assertions
    AssertJSON func(t *testing.T, parsed any)  // runs only for json format
    AssertText func(t *testing.T, output string) // runs only for text format
}

type Scenario struct {
    Name  string
    Steps []Step
}
```

### Step Execution Flow

1. Expand `$N.field` references in args (see below)
2. Execute command via `Env.Run()`
3. Store result for future reference
4. Check exit code against `WantErr`
5. Run `Assert` if set
6. If JSON format and `AssertJSON` set: unmarshal stdout, call it
7. If text format and `AssertText` set: call with raw stdout

### Inter-Step References

Steps often need values from earlier steps (e.g. `short_id` of a created task). Args support `$N.field` syntax where `N` is a zero-based step index.

Resolution strategy depends on current format:
- **JSON**: parse stored stdout as JSON, extract field by key
- **Text**: extract `short_id` via regex on known output patterns (e.g. "Created task XXXXXXXX")

### runScenarios

```go
func runScenarios(t *testing.T, scenarios []Scenario) {
    combos := combinations(
        []string{"flag", "env"},
        []string{"text", "json"},
    )
    for _, sc := range scenarios {
        for _, combo := range combos {
            dbMode, format := combo[0], combo[1]
            name := sc.Name + "/" + dbMode + "/" + format
            t.Run(name, func(t *testing.T) {
                env := newEnv(t, binPath, dbMode, format)
                for i, step := range sc.Steps {
                    step.Run(t, env, i)
                }
            })
        }
    }
}
```

### Assertion Helpers

Standard library only — no external test libraries.

```go
func assertEqual(t *testing.T, got, want any)
func assertContains(t *testing.T, got, substr string)
func assertNotContains(t *testing.T, got, substr string)
func assertMatches(t *testing.T, got, pattern string)        // regex
func jsonField(parsed any, path string) any                   // e.g. "title", "tags.0.name"
func jsonArray(parsed any) []any
func assertExitOK(t *testing.T, r Result)
func assertExitErr(t *testing.T, r Result)
func assertStderrContains(t *testing.T, r Result, substr string)
```

## Scenario Coverage

### task_lifecycle_test.go

- Create a task — verify fields (title, status=pending, priority, short_id)
- Create → start → verify active
- Create → start → done → verify completed
- Create → delete → verify deleted
- Create → start → back to pending → verify pending
- Completed → reopen (completed → pending)
- Create with project assignment
- Create multiple tasks → list shows all

### filtering_test.go

- `status:active` — only active tasks shown
- `status:pending,active` — both statuses shown
- `+tag` — only matching tasks
- `-tag` — excludes matching tasks
- `priority:3` — exact match
- `priority:2..4` — range
- `project:name` — scoped to project
- Combined filters — `status:active +api project:backend`
- No results — empty output, no error

### tags_test.go

- Create task with `+tag1 +tag2` — tags assigned
- Modify to add tag — `modify <id> +newtag`
- Modify to remove tag — `modify <id> -oldtag`
- Tags appear in list output
- Tags appear in info output
- Filter by tag after assignment

### annotations_test.go

- Annotate a task → verify in info output
- Multiple annotations on same task — all shown
- Annotate nonexistent task — error

### error_handling_test.go

- Info/modify/start/done/delete with nonexistent short_id — "not found" error
- Invalid status transition (pending → done) — "transition not allowed" error
- Start/done/delete already in target state — appropriate error
- No args to `add` — usage error
- Invalid filter field — error message

### output_format_test.go

- Text list has correct column headers
- Priority symbols render correctly (L/M/H/U)
- JSON output has snake_case keys
- JSON list returns array
- JSON info returns object with all expected fields
- Empty list — text shows nothing, JSON shows `[]`

## Build Integration

`TestMain` in `main_test.go`:
1. `go build -o <tmpdir>/tusk ./cmd/tusk` (requires `CGO_ENABLED=1`)
2. Store binary path in package-level var
3. Call `os.Exit(m.Run())`

The existing `make test` target (`go test -v ./...`) will pick up these tests automatically.
