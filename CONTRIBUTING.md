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

If you'd rather develop in a hermetic, sandboxed environment with all tooling pre-installed, see the [Dev Container](#dev-container-sandboxed-environment) section below.

## Dev Container (Sandboxed Environment)

A `.devcontainer/` is provided that builds a hermetic Ubuntu 24.04 image with the Go SDK, Node, and all project tooling pre-installed. Outbound network access is gated by an in-container egress proxy (tinyproxy) plus iptables rules, making it safe to run AI agents like Claude Code inside.

### Prerequisites

- Docker Desktop (or another Docker engine)
- The dev container CLI: `npm install -g @devcontainers/cli`

### Starting the container

From the repository root:

```bash
make devcontainer-up
```

The first build downloads the Go SDK, Node.js, and Go tools and might take several minutes. Subsequent starts complete in well under a second — the entrypoint just applies iptables and starts the egress proxy.

### Two users with different network privileges

The container has two distinct users; pick the one that matches what you're doing.

| User | Sudo | Egress | Use for |
|---|---|---|---|
| `claude` | no | only allowlisted hosts (via tinyproxy) | day-to-day editing, running agents, `make build`, `make test` |
| `dev` | yes (NOPASSWD) | unrestricted | installing extra packages, fetching from non-allowlisted hosts, ops tasks |

```bash
# Default shell as claude (sandboxed) — IDE/devcontainer CLI attach here
devcontainer exec --workspace-folder . bash

# Shell as dev (unrestricted) via the container ID
CID=$(docker ps --filter "label=devcontainer.local_folder=$PWD" -q)
docker exec -u dev -it "$CID" bash
```

Or use the Makefile shortcuts, which handle the container ID lookup for you:

```bash
make devcontainer-shell        # exec in as claude (sandboxed)
make devcontainer-shell-ops    # exec in as dev (unrestricted)
```

### What's pre-installed

`gopls`, `dlv`, `golangci-lint`, `lefthook`, `conform`, `claude` (Claude Code CLI), `fd`, `ripgrep`, `git`, `gh`-style tooling. All on `PATH` for both users — `make build`, `make test`, and `make lint` work without further setup.

### Egress allowlist

Allowed hostnames live in `.devcontainer/tinyproxy-filter`:

- `api.anthropic.com`
- `proxy.golang.org`, `sum.golang.org`
- `github.com`, `codeload.github.com`, `*.githubusercontent.com`
- `registry.npmjs.org`, `*.npmjs.org`

Anything else is rejected by tinyproxy with a 403, and direct (non-proxied) connections are dropped at the kernel by iptables. The `dev` user is exempt from both.

To allow a new host, append a regex to `.devcontainer/tinyproxy-filter` and rebuild (see below).

### Running Claude Code

The `claude` CLI is pre-installed on `PATH`. Auth and configuration come from the host via the bind-mounted `~/.claude` directory, so a session you've signed into on the host carries over with no extra setup.

```bash
# Open a sandboxed shell and start claude in the workspace
devcontainer exec --workspace-folder . bash -lc 'cd /workspaces/tusk && claude'
```

Or, from an existing shell inside the container:

```bash
cd /workspaces/tusk
claude
```

What this means in practice:

- The agent runs as the `claude` user — it inherits the same egress posture as your shell. It can reach `api.anthropic.com` and the other allowlisted hosts; everything else returns a tinyproxy 403 or is dropped at the kernel.
- The agent has no `sudo`, so it cannot disable the firewall, restart tinyproxy, or alter the iptables rules — even if it tried.
- File edits land in `/workspaces/tusk`, which is the bind-mounted host repo, so changes show up on the host immediately.
- Local tooling (`go test`, `golangci-lint`, your locally-built `tusk` binary) is available for the agent to invoke during its workflow.
- If the agent needs a host that isn't on the allowlist, the request fails. Add it to `tinyproxy-filter` and rebuild (see [Egress allowlist](#egress-allowlist)).

If you want to run claude without the sandbox restrictions (e.g., for an interactive session that needs broader network access), exec in as `dev`:

```bash
CID=$(docker ps --filter "label=devcontainer.local_folder=$PWD" -q)
docker exec -u dev -it "$CID" bash -lc 'cd /workspaces/tusk && claude'
```

### Verifying the sandbox

```bash
CID=$(docker ps --filter "label=devcontainer.local_folder=$PWD" -q)

# claude has no sudo:
docker exec -u claude "$CID" sudo -n true            # expect failure

# Allowlisted host via proxy — succeeds:
docker exec -u claude "$CID" curl -sI https://api.anthropic.com | head -1

# Non-allowlisted host — tinyproxy 403:
docker exec -u claude "$CID" curl -sI https://example.com | head -1

# Direct connection (proxy bypassed) — iptables drops:
docker exec -u claude "$CID" curl -sI --noproxy '*' --max-time 3 https://1.1.1.1

# dev is unrestricted:
docker exec -u dev "$CID" curl -sI https://example.com | head -1
```

### Tearing down and rebuilding

```bash
# Stop and remove the current container
make devcontainer-down

# Rebuild from scratch (e.g., after editing the Dockerfile or filter list)
make devcontainer-down && devcontainer up --workspace-folder . --build-no-cache
```

### Caveats

- The workspace is bind-mounted from the host, so file ownership inside the container reflects host UIDs (not the in-container `claude`/`dev` UIDs).
- `~/.claude` is bind-mounted between host and container, so Claude Code auth/config is shared.
- The Neovim config is **cloned into the image at build time** (from `germanamz/simple-nvim`), not bind-mounted. That means lazy.nvim can manage its lockfile freely, but config edits made on the host don't appear inside the container until you change the `git clone` source in the Dockerfile and rebuild.
- Egress filter changes in `tinyproxy-filter` require a container rebuild to take effect.

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

For code style, see [STYLE.md](STYLE.md).

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

## ROADMAP.md is generated

`ROADMAP.md` is regenerated from tusk state — do not hand-edit.

The workspace tusk database lives at `.data/tusk.db` (a committed SQLite
file). It currently holds the `tusk-roadmap` project, which is rendered
into `ROADMAP.md` via `make roadmap`; over time it will also accumulate
workspace-shared notes and any other tusk state worth committing.
`tusk.toml` at the repo root sets `[storage] path = ".data/tusk.db"`,
so any `tusk` invocation from inside the checkout — locally or in CI —
resolves the committed DB via walk-up config discovery (v0.9). No
env-var setup is required, and the file in the repo — not any
developer's local DB — is the source of truth for what CI sees.

`.data/tusk.db` is a binary blob, but legibility for code review is
provided by `ROADMAP.md` itself: every PR that edits the roadmap also
regenerates `ROADMAP.md`, and the markdown diff is the human-readable
change log.

Workflow:

1. Edit the roadmap via `tusk task` commands. Examples:
   ```bash
   tusk task create "Story: my new story" level=story project=tusk-roadmap parent=<initiative-short-id>
   tusk task done <short-id>
   tusk task move <short-id> --before <target>
   ```
2. Regenerate the markdown:
   ```bash
   make roadmap
   ```
3. Commit `.data/tusk.db` and `ROADMAP.md` together with whatever code
   change motivates the roadmap edit.

The `.data/tusk.db-wal` / `.data/tusk.db-shm` sidecar files that SQLite
creates during operation are gitignored — never commit them. If your DB
has pending WAL data, run `tusk task tree --format markdown >/dev/null`
once against it (any read closes the connection cleanly and checkpoints
the WAL into the main file) before staging.

CI will fail any PR whose `ROADMAP.md` is out of sync with `.data/tusk.db`
once the gate is enabled (`TUSK_ROADMAP_CHECK_ENABLED=true` repo variable).

The `tusk-roadmap` project uses the canonical taxonomy:
`milestone:initiative:story:(task,spike)`.

## Reporting Issues

When reporting bugs, please include:
- Steps to reproduce
- Expected vs actual behavior
- Go version and OS
- Tusk version or commit hash

## License

By contributing to Tusk, you agree that your contributions will be licensed under the Apache 2.0 License.
