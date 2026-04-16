# Contributing to Tusk

Thank you for your interest in contributing to Tusk! This document provides guidelines and information to help you get started.

## Getting Started

### Prerequisites

- Go 1.26+
- golangci-lint (for linting)
- lefthook and conform are installed automatically by `make setup-hooks`

### Setup

```bash
git clone https://github.com/germanamz/tusk.git
cd tusk
make setup-hooks
make build
make test
```

## Development Workflow

1. Fork the repository and create a feature branch from `main`.
2. Make your changes following the conventions below.
3. Run tests and linting before submitting.
4. Open a pull request against `main`.

### Running Tests

```bash
make test           # Unit + e2e tests
make test-race      # Tests with race detector
make test-e2e       # E2e tests only

# Single unit test
go test -v ./service -run TestTaskCreate

# Single e2e scenario
go test -v ./tests/e2e -run TestErrorHandling
```

### Linting

```bash
make vet
make lint
```

## Architecture

Tusk follows a layered architecture. Dependencies flow downward only:

```
Interface Layer (CLI, MCP server)
    |
Service Layer (business logic)
    |
Repository Layer (interfaces)
    |
Storage Implementations (SQLite)
```

When contributing, respect these boundaries:
- **Interface layer** (`internal/tui/`, `internal/mcp/`) translates external protocols into service calls. No business logic here.
- **Service layer** (`service/`) contains all business logic. Services accept repository interfaces via constructor injection.
- **Repository layer** (`repository/`) defines Go interfaces only.
- **Storage layer** (`sqlite/`) implements repository interfaces.

## Code Conventions

### Commits

Use conventional commits with scope:

```
feat(cli): add tree view command
fix(sqlite): handle null parent_id in query
test(e2e): add filter syntax scenarios
docs: update README with MCP examples
```

### Error Handling

- Use sentinel errors from `domain/errors.go`.
- Check errors with `errors.Is()`.
- Available sentinels: `ErrNotFound`, `ErrConflict`, `ErrCyclicBlock`, `ErrInvalidTransition`, `ErrDuplicateRelation`.

### Key Patterns

- **Optimistic locking**: every mutable entity has a `version` field. Updates must use `WHERE id = ? AND version = ?`.
- **Double-pointer updates**: `TaskUpdate` uses `**string`/`**uuid.UUID` for nullable fields (`nil` = don't change, `*nil` = set NULL, `*"value"` = set value).
- **Soft delete**: tasks transition to `deleted` status via workflow, not removed from DB.

### E2E Tests

End-to-end tests live in `tests/e2e/` and use a custom harness. Each scenario runs 4 times (2 DB config modes x 2 output formats). Reference prior step results with `$0.short_id`:

```go
scenarios := []Scenario{
    {
        Name: "create_and_get",
        Steps: []Step{
            {Args: []string{"task", "create", "My task"}},
            {Args: []string{"task", "get", "$0.short_id"}},
        },
    },
}
```

## Reporting Issues

When reporting bugs, please include:
- Steps to reproduce
- Expected vs actual behavior
- Go version and OS
- Tusk version or commit hash

## License

By contributing to Tusk, you agree that your contributions will be licensed under the Apache 2.0 License.
