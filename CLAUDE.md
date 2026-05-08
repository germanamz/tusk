# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Tusk v1 is a local-first agent brain: a markdown vault with a smart, schema-validated, semantically-indexed graph layered on top. Files (markdown + manifest TOML) are the source of truth; git is the history; tusk is the indexer + retrieval engine.

The v1 rebuild is in progress. Until v1 features ship, the authoritative reference for architecture and behavior is the design spec.

## Spec

- **Design spec:** `docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md` — read this first.
- **Plans:** `docs/superpowers/plans/2026-05-05-tusk-v1-*.md` — sequenced implementation plans, one per subsystem.

## Commands (during v1 build-out)

```bash
make build          # build ./bin/tusk (Plans 1–6 complete: full CLI + MCP server)
make test           # run unit tests
make test-race      # tests with race detector
make vet            # go vet
make lint           # golangci-lint run ./...
make fmt            # gofmt across the tree
```

## Style

See `STYLE.md` for the codebase naming and spacing conventions (rules 1–4 are linter-enforced).

## Commits

Conventional commits with scope: `feat(cli):`, `fix(index):`, `docs(spec):`, `chore(cleanup):`, etc.

## v0 references

- `v0.14.0` — last v0 release tag
- `v0-archive` — long-lived branch holding v0 sources for emergency patches
- `v0-final` — tag on `main` marking the cleanup commit that retires v0 documentation
