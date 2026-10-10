# AGENTS.md

Guidance for coding agents (Claude Code, Codex, Cursor and others) working in this repository. `CLAUDE.md` imports this file.

## Project

Tusk v1 is a local-first agent brain: a markdown vault with a smart, schema-validated, semantically-indexed graph layered on top. Files (markdown + manifest TOML) are the source of truth; git is the history; tusk is the indexer + retrieval engine.

The v1 rebuild has shipped the full CLI and MCP server. Product vision and design principles live in `PRODUCT.md`; usage lives in `README.md`, the guides in `docs/guide/` and the CLI reference in `docs/cli/`; per-package architecture and behavior detail lives under `docs/packages/`.

## Reference

- **Product & design principles:** `PRODUCT.md` — what Tusk is and why; read this first.
- **Usage:** `README.md` (landing page, quickstart, client setup), `docs/guide/` (workspaces, querying, rules and doctor, MCP) and `docs/cli/` (per-command CLI reference).
- **Package docs:** `docs/packages/*.md` — one file per `internal/*` package, describing public surface and intent.

## Commands

```bash
make build          # build ./bin/tusk
make test           # run unit tests
make test-race      # tests with race detector
make vet            # go vet
make lint           # golangci-lint run ./...
make fmt            # gofmt across the tree
make docs           # regenerate docs/cli/ and man/ from the cobra help text
```

After editing a command's help text in `cmd/tusk/`, run `make docs` and commit both `docs/cli/` and `man/`; a pre-push docs-drift check rejects the push otherwise.

## Style

See `STYLE.md` for the codebase naming and spacing conventions (rules 1–4 are linter-enforced).

## Commits

Conventional commits with scope: `feat(cli):`, `fix(index):`, `docs(spec):`, `chore(cleanup):`, etc.
