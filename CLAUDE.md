# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Tusk v1 is a local-first agent brain: a markdown vault with a smart, schema-validated, semantically-indexed graph layered on top. Files (markdown + manifest TOML) are the source of truth; git is the history; tusk is the indexer + retrieval engine.

The v1 rebuild has shipped the full CLI and MCP server. Product vision and design principles live in `PRODUCT.md`; usage and the working surface live in `README.md` and `docs/cli/`; per-package architecture and behavior detail lives under `docs/packages/`.

## Reference

- **Product & design principles:** `PRODUCT.md` — what Tusk is and why; read this first.
- **Usage:** `README.md` and `docs/cli/` — install, quickstart, and per-command CLI/MCP reference.
- **Package docs:** `docs/packages/*.md` — one file per `internal/*` package, describing public surface and intent.

## Commands

```bash
make build          # build ./bin/tusk
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

- `v0.14.0` — last v0 release tag; preserves all v0 sources. Branch from it (`git checkout -b v0-hotfix v0.14.0`) if an emergency v0 patch is ever needed.
- `v0-final` — tag on `main` marking the cleanup commit that retires v0 documentation
