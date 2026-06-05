# STYLE.md — Tusk Naming and Spacing Convention

**Status:** enforced repository-wide.

This document is the canonical reference for the Tusk codebase naming and
spacing convention. Rules 1–4 are linter-enforced; the style guide that
follows is advisory review-time guidance. The enforcement summary at the end
maps each rule to the tool that checks it.

---

## Rules

### Rule 1 — No single-character identifiers anywhere

Locals, range variables, function and method parameters, receivers, loop
indices, generic type parameters, **package names, and file names** all
require at least two characters. There are no exemptions, including
`*testing.T`, `*testing.B`, `testing.TB`, short generic type parameters,
and the analysistest stdlib convention of single-letter testdata packages
(`package a`, `a.go`) — those use `package fixtures` and `cases.go` (or
similarly named ≥ 2-character files) instead.

**Rationale:** Single-character names force readers to hold an implicit
mapping in working memory. A two-character minimum is enough to make the name
a recognizable word fragment.

```go
// before
for _, t := range tasks {
    if t.WaitUntil != nil { ... }
}

// after
for _, task := range tasks {
    if task.WaitUntil != nil { ... }
}
```

```go
// before
func (s *TaskService) Create(ctx context.Context, t *domain.Task) error

// after
func (service *TaskService) Create(ctx context.Context, task *domain.Task) error
```

---

### Rule 2 — Blank lines around `if err != nil` guards

An error-producing assignment must be separated from its `if err != nil` guard
by a blank line, and the guard's closing brace must be separated from the next
statement by another blank line.

**Rationale:** Dense unspaced blocks make identifiers visually easy to lose.
Blank lines create visual rhythm that lets reviewers scan error paths without
re-reading each statement.

```go
// before
project, err := projectRepo.GetByID(ctx, id)
if err != nil {
    return nil, fmt.Errorf("looking up project: %w", err)
}
return project.ID, nil

// after
project, err := projectRepo.GetByID(ctx, id)

if err != nil {
    return nil, fmt.Errorf("looking up project: %w", err)
}

return project.ID, nil
```

---

### Rule 3 — Named errors on `err` shadow

When two or more `err` variables are declared at the same lexical scope,
**every** instance — including the first — must use a typed name
(`<noun>Err`). A function that produces exactly one `err` keeps it named
`err`; this rule fires only on shadow.

**Rationale:** Sequential `err :=` shadows hide the failure mode at the
variable; named errors document it. Renaming **every** instance rather than
leaving the first as `err` keeps the block visually uniform — readers do not
have to track which slot is which. `blockingErr` in a guard immediately
identifies what failed; generic `err` in a multi-error block tells the reader
nothing.

```go
// before — service/task.go:325–339
blockingCounts, err := bundle.Relations.CountBlockingByTasks(ctx, taskIDs)
if err != nil {
    return nil, fmt.Errorf("loading blocking counts: %w", err)
}
blockedByCounts, err := bundle.Relations.CountBlockedByTasks(ctx, taskIDs)
if err != nil {
    return nil, fmt.Errorf("loading blocked-by counts: %w", err)
}
annotationCounts, err := bundle.Annotations.CountByTasks(ctx, taskIDs)
if err != nil {
    return nil, fmt.Errorf("loading annotation counts: %w", err)
}

// after
blockingCounts, blockingErr := bundle.Relations.CountBlockingByTasks(ctx, taskIDs)

if blockingErr != nil {
    return nil, fmt.Errorf("loading blocking counts: %w", blockingErr)
}

blockedByCounts, blockedByErr := bundle.Relations.CountBlockedByTasks(ctx, taskIDs)

if blockedByErr != nil {
    return nil, fmt.Errorf("loading blocked-by counts: %w", blockedByErr)
}

annotationCounts, annotationErr := bundle.Annotations.CountByTasks(ctx, taskIDs)

if annotationErr != nil {
    return nil, fmt.Errorf("loading annotation counts: %w", annotationErr)
}
```

---

### Rule 4 — Standardized test-handle parameter names

Functions and methods that accept a testing handle must use the name from the
table below. Deviation fails the build.

| Type          | Required name |
|---------------|---------------|
| `*testing.T`  | `test`        |
| `*testing.B`  | `bench`       |
| `testing.TB`  | `harness`     |

**Rationale:** A uniform name means every test file reads the same way.
Reviewers can grep for `test.Helper()`, `bench.ResetTimer()`, and
`harness.Fatal()` across the entire codebase without ambiguity.

```go
// before
func TestRunCreate(t *testing.T) { ... }

// after
func TestRunCreate(test *testing.T) { ... }
```

---

## Style guide (advisory)

The linter does not enforce these. They are review-time guidance for cases
where the right name depends on context.

**Receivers** use a role word matching the type: `*TaskService` → `service`,
`*Renderer` → `renderer`, `*App` → `app`. When two service types appear in the
same scope, qualify: `taskService` / `projectService`. Receiver names are
consistent within a file, never per-method.

**Generic type parameters** use role-named identifiers: `Element`, `Item`,
`Key`, `Value`, `Result`. Never single letters — rule 1 already enforces ≥2
characters; the role-naming is an additional taste guideline.

**Loop indices** use `index` for the outermost loop, `subindex` for nested
loops, or contextual names when the domain provides them (`row` / `col`,
`phase` / `step`).

**Errors in single-error functions** stay `err`. Rule 3 fires only when
shadow occurs; introducing typed names in a function that produces a single
error is unnecessary and unwanted.

---

## Enforcement summary

| Rule | Description                                   | Enforced by                |
|------|-----------------------------------------------|----------------------------|
| 1    | No single-character identifiers               | `varnamelen` (golangci-lint) |
| 2    | Blank lines around `if err != nil` guards     | `tusk-lint -blankline`     |
| 3    | Named errors on `err` shadow                  | `tusk-lint -namederr`      |
| 4    | Standardized test-handle parameter names      | `tusk-lint -testhandle`    |

`varnamelen` checks identifier-kind names (locals, parameters, receivers,
range variables, loop indices, generic type parameters) but does not
check package or file names. Those parts of rule 1 are review-enforced.

Run `make lint` to execute both `golangci-lint` (rule 1) and `tusk-lint`
(rules 2–4) in one step.

**Lock-in:** no per-package exclusions in `.golangci.yml`, no
`// nolint:varnamelen` directives anywhere in the Go source tree; both are
guarded by `make lint-style-locked`, which runs as a prerequisite of
`make lint` in CI.
