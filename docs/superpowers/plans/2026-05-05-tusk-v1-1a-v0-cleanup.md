---
type: plan
title: Plan 1a
status: shipped
shipped-at: "2026-05-05"
implements:
  - Tusk v1 Rebuild
---

# Tusk v1 — Plan 1a: v0 Cleanup + v1 Branch Setup

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Transition the repository from v0.x maintenance to v1 development. Land a cleanup PR on `main` that removes v0-only artifacts; tag `v0-final`; cut a `v0-archive` branch from `v0.14.0`; open the `v1` branch with obsolete source removed and a v1 skeleton (Makefile stubs, placeholder PRODUCT.md, trimmed CLAUDE.md, release-please rebased for v1).

**Architecture:** Two phases. **Phase 1** lands cleanup on `main` from the existing `chore/roadmap-cleanup` branch (which already carries the v1 spec). **Phase 2** opens the long-lived `v1` branch from post-cleanup `main`, removes obsolete v0 source files, installs a minimal v1 skeleton, and opens a draft integration PR. End state: subsequent v1 plans target the `v1` branch.

**Tech Stack:** git, GitHub CLI (`gh`), GNU Make, Go modules, release-please.

**Spec reference:** `docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md` §3.3.

---

## File Structure

### Phase 1 — cleanup on `chore/roadmap-cleanup` (merges to `main`)

**Removed:**
- `docs/status/` (v0 status reports)
- `docs/releases/` (v0 release notes)
- `docs/retrospectives/` (v0 retrospectives)
- `docs/configuration.md` (v0 configuration doc)
- `docs/programmatic-usage.md` (v0 programmatic-usage doc)
- `ROADMAP.md` (v0 roadmap)
- `CHANGELOG.md` (only if present at cleanup time — release-please-managed)

**Modified:**
- `README.md` — banner at the top noting v1 rebuild and pointing at `v0.14.0` / `v0-archive` / spec

**Preserved unchanged:**
- All v0 source (`cmd/`, `domain/`, `service/`, `sqlite/`, etc.) — v0 still compiles and runs from `main`'s tip until v1 ships
- `LICENSE`, `CODE_OF_CONDUCT.md`, `CONTRIBUTING.md`, `STYLE.md`
- `.github/`, `.devcontainer/`, `lefthook.yml`, `commitlint.config.mjs`
- `release-please-config.json` (rebased in Phase 2 on `v1`, not on `main`)
- `docs/superpowers/` (specs and plans)

### Phase 2 — `v1` branch initialization (off post-cleanup `main`)

**Removed (v0 source):**
- `cmd/`, `domain/`, `service/`, `repository/`, `sqlite/`, `filter/`, `config/`, `internal/`, `migrations/`, `tests/`, `syntax/`, `assets/`, `dist/`, `bin/`
- `client.go`, `client_test.go`
- `install.sh` (v0 installer; v1 installer rebuilt later)
- `tusk.toml` at root (v0 workspace config; v1 reuses the filename for the new manifest format, written fresh by `tusk init` in Plan 1b)
- `go.sum` (regenerated as v1 adds dependencies)
- `.data/` (v0 dev DB; gitignored going forward)

**Modified:**
- `Makefile` — replace v0 targets with v1 skeleton (`build`, `test`, `lint`, `vet`, `fmt`, `clean` — all stubs that exit 0)
- `PRODUCT.md` — replace with v1 placeholder pointing at the spec
- `CLAUDE.md` — trim v0 architecture notes; point at the v1 spec for context
- `README.md` — replace v0 README with v1 banner pointing at the spec, `v0.14.0` tag, and `v0-archive` branch
- `go.mod` — strip v0 dependencies, keep module path + Go version
- `.gitignore` — add `.tusk/`, ensure `.data/` and `bin/` and `dist/` covered
- `release-please-config.json` — rebase `bootstrap-sha` to the first v1 commit; reset version tracking for v1.0.0

**Preserved unchanged:**
- `LICENSE`, `CODE_OF_CONDUCT.md`, `CONTRIBUTING.md`, `STYLE.md`
- `.github/`, `.devcontainer/`, `lefthook.yml`, `commitlint.config.mjs`
- `docs/superpowers/` (specs and plans)

---

## Phase 1 — Cleanup PR

### Task 1: Verify pre-flight state

**Files:** none (read-only)

- [ ] **Step 1: Confirm `v0.14.0` tag exists locally and on remote**

```bash
git tag -l v0.14.0
git ls-remote --tags origin v0.14.0
```

Expected: both commands print `v0.14.0`. If the remote line is empty, run `git fetch --tags origin` first.

- [ ] **Step 2: Confirm current branch is `chore/roadmap-cleanup` and has the v1 spec committed**

```bash
git rev-parse --abbrev-ref HEAD
git log --oneline -5
ls docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md
```

Expected: branch is `chore/roadmap-cleanup`; recent log includes `docs(spec): add tusk v1 rebuild design`; spec file exists.

- [ ] **Step 3: Confirm working tree is clean except for known unstaged changes**

```bash
git status --short
```

Expected: only the pre-existing `M .data/tusk.db`, `.devcontainer/devcontainer.json`, `.devcontainer/init-firewall.sh`, `.gitignore` (or empty). If anything else appears unstaged, stop and ask the user.

---

### Task 2: Remove v0-only docs subdirectories

**Files:**
- Remove: `docs/status/`, `docs/releases/`, `docs/retrospectives/`

- [ ] **Step 1: Remove the three subdirectories**

```bash
git rm -r docs/status docs/releases docs/retrospectives
```

- [ ] **Step 2: Verify removal**

```bash
ls docs/status docs/releases docs/retrospectives 2>&1
```

Expected: three "No such file or directory" errors.

```bash
git status --short docs/
```

Expected: deletions staged for the three directories.

---

### Task 3: Remove v0 standalone docs files

**Files:**
- Remove: `docs/configuration.md`, `docs/programmatic-usage.md`

- [ ] **Step 1: Remove the two files**

```bash
git rm docs/configuration.md docs/programmatic-usage.md
```

- [ ] **Step 2: Verify**

```bash
ls docs/configuration.md docs/programmatic-usage.md 2>&1
```

Expected: two "No such file or directory" errors.

---

### Task 4: Remove ROADMAP.md and CHANGELOG.md (if present)

**Files:**
- Remove: `ROADMAP.md`, `CHANGELOG.md` (if present)

- [ ] **Step 1: Remove ROADMAP.md**

```bash
git rm ROADMAP.md
```

- [ ] **Step 2: Remove CHANGELOG.md if it exists**

```bash
test -f CHANGELOG.md && git rm CHANGELOG.md || echo "no CHANGELOG.md to remove"
```

- [ ] **Step 3: Verify**

```bash
ls ROADMAP.md CHANGELOG.md 2>&1
```

Expected: "No such file or directory" for both.

---

### Task 5: Verify no remaining files reference removed paths

**Files:** none (read-only scan)

- [ ] **Step 1: Search for references to removed docs**

```bash
grep -rn -E "(docs/status|docs/releases|docs/retrospectives|docs/configuration|docs/programmatic-usage|ROADMAP\.md|CHANGELOG\.md)" \
  --include="*.md" --include="*.go" --include="*.toml" --include="*.json" --include="*.yml" --include="*.yaml" \
  --exclude-dir=.git --exclude-dir=.tusk --exclude-dir=node_modules --exclude-dir=docs/superpowers \
  . 2>/dev/null | grep -v "^docs/superpowers/specs/" || echo "no remaining references"
```

Expected: `no remaining references`. (References inside `docs/superpowers/` specs are exempt — those are historical descriptions, not active links.)

If references remain in active code or docs (e.g., `README.md` linking to `ROADMAP.md`), record them and update or remove in Task 6.

---

### Task 6: Update README.md with v1-rebuild banner

**Files:**
- Modify: `README.md` (insert banner block at top)

- [ ] **Step 1: Insert the banner immediately after the centered banner image / nav block**

Open `README.md` and, immediately before the existing `---` separator near the top, insert:

```markdown
> ⚠️ **v1 rebuild in progress.** This README documents Tusk **v0.x**. The v0 line ended at [`v0.14.0`](https://github.com/germanamz/tusk/releases/tag/v0.14.0). Active development now targets **Tusk v1** — a local-first agent brain. See the [v1 design spec](docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md). v0 sources remain available on the [`v0-archive`](https://github.com/germanamz/tusk/tree/v0-archive) branch.

```

(One blank line after the banner block, so the existing `---` stays visually separated.)

- [ ] **Step 2: If Task 5 found references to removed paths in README.md, remove or rewrite those lines**

Use `Edit` on each affected line. Re-run the Task 5 grep afterwards to confirm zero remaining references in `README.md`.

- [ ] **Step 3: Render-check**

```bash
head -20 README.md
```

Expected: banner block visible after the nav row, before the first `---`.

---

### Task 7: Commit cleanup

**Files:** none new

- [ ] **Step 1: Confirm what is staged**

The `git rm` calls in Tasks 2–4 already staged the deletions; Task 6 staged the README modification implicitly by editing in place (run `git add README.md` if the README change isn't yet staged).

```bash
git add README.md
git status --short
```

Expected: `D ` entries for the removed v0 docs/ subdirectories, `D docs/configuration.md`, `D docs/programmatic-usage.md`, `D ROADMAP.md`, optionally `D CHANGELOG.md`; `M README.md`. Nothing else.

- [ ] **Step 2: Commit**

```bash
git commit -m "$(cat <<'EOF'
chore(cleanup): remove v0.x history artifacts ahead of v1 rebuild

Strip v0-only documentation (status reports, release notes,
retrospectives, configuration.md, programmatic-usage.md, ROADMAP.md)
from main. README gains a v1-rebuild banner pointing at the design
spec and v0 archive locations.

v0 sources remain reachable at the v0.14.0 tag and (after this PR
merges) the v0-archive branch.
EOF
)"
```

- [ ] **Step 3: Verify commit landed**

```bash
git log --oneline -3
```

Expected: top commit is the cleanup commit.

---

### Task 8: Push branch and open PR

**Files:** none

- [ ] **Step 1: Push the branch**

```bash
git push -u origin chore/roadmap-cleanup
```

Expected: push succeeds. If the branch is already pushed, this is a fast-forward update.

- [ ] **Step 2: Open the PR**

```bash
gh pr create --title "chore(cleanup): retire v0.x history, prepare for v1 rebuild" --body "$(cat <<'EOF'
## Summary

- Add v1 design spec at `docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md`.
- Remove v0-only documentation: `docs/status/`, `docs/releases/`, `docs/retrospectives/`, `docs/configuration.md`, `docs/programmatic-usage.md`, `ROADMAP.md`.
- Remove `CHANGELOG.md` if present (will be regenerated by release-please for v1).
- Add v1-rebuild banner to README.

## Why

The v0 line ends at `v0.14.0`. v1 is a ground-up rewrite (see the linked spec). This PR retires v0 documentation that doesn't carry forward; v0 sources remain available at the `v0.14.0` tag and on the upcoming `v0-archive` branch.

## Test plan

- [ ] CI green
- [ ] `git log` on `main` after merge shows the cleanup commit
- [ ] `docs/status/`, `docs/releases/`, `docs/retrospectives/` gone
- [ ] README banner renders correctly on GitHub
- [ ] No remaining references to removed paths in active code/docs (verified locally)
EOF
)"
```

Expected: PR URL printed.

- [ ] **Step 4: Wait for review and merge**

User merges the PR via GitHub. (Agent does not self-merge; this is a maintainer step.)

After merge, return to local main and pull:

```bash
git checkout main
git pull origin main
git log --oneline -3
```

Expected: top commit is the cleanup commit (or the squash-merge equivalent), now on `main`.

---

## Phase 2 — Tags and Branches

### Task 9: Tag `v0-final` on the cleanup commit

**Files:** none

- [ ] **Step 1: Tag main's tip**

```bash
git checkout main
git pull origin main
git tag -a v0-final -m "End of v0.x line on main; v1 rebuild begins after this tag."
```

- [ ] **Step 2: Push the tag**

```bash
git push origin v0-final
```

- [ ] **Step 3: Verify**

```bash
git tag -l v0-final
git ls-remote --tags origin v0-final
```

Expected: `v0-final` printed in both. Local and remote agree.

---

### Task 10: Create `v0-archive` branch from `v0.14.0`

**Files:** none

- [ ] **Step 1: Create the branch**

```bash
git branch v0-archive v0.14.0
```

- [ ] **Step 2: Push the branch**

```bash
git push -u origin v0-archive
```

- [ ] **Step 3: Verify**

```bash
git branch --list v0-archive
git ls-remote --heads origin v0-archive
```

Expected: branch listed locally and remotely.

---

## Phase 3 — `v1` Branch Initialization

### Task 11: Create `v1` branch from post-cleanup `main`

**Files:** none

- [ ] **Step 1: Branch off main**

```bash
git checkout main
git pull origin main
git checkout -b v1
git log --oneline -3
```

Expected: now on `v1`, top commit is the cleanup commit.

---

### Task 12: Remove obsolete v0 source

**Files:**
- Remove: `cmd/`, `domain/`, `service/`, `repository/`, `sqlite/`, `filter/`, `config/`, `internal/`, `migrations/`, `tests/`, `syntax/`, `assets/`, `dist/`, `bin/`, `client.go`, `client_test.go`, `install.sh`, `tusk.toml`, `go.sum`, `.data/`

- [ ] **Step 1: Remove top-level Go source directories**

```bash
git rm -r cmd domain service repository sqlite filter config internal migrations tests syntax
```

- [ ] **Step 2: Remove non-source v0 directories**

```bash
git rm -rf assets dist bin 2>/dev/null || true
```

(Some of these may be gitignored or already missing; the `|| true` keeps the step idempotent.)

- [ ] **Step 3: Remove top-level v0 files**

```bash
git rm client.go client_test.go install.sh tusk.toml go.sum
```

- [ ] **Step 4: Remove the v0 dev database directory**

```bash
git rm -rf .data 2>/dev/null || true
```

- [ ] **Step 5: Verify**

```bash
git status --short | head -50
```

Expected: many `D ` (deletion) entries staged. No staged additions yet.

```bash
ls cmd domain service 2>&1
```

Expected: three "No such file or directory" errors.

---

### Task 13: Reset `go.mod` to v1 baseline

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Read current go.mod to get module path and Go version**

```bash
head -5 go.mod
```

Note the module path (e.g., `github.com/germanamz/tusk`) and the `go` directive (e.g., `go 1.23`).

- [ ] **Step 2: Replace go.mod with a minimal v1 baseline**

Write the file with exactly:

```
module github.com/germanamz/tusk

go 1.23
```

(Substitute the actual module path and Go version from Step 1.)

Use the `Write` tool to overwrite `go.mod`.

- [ ] **Step 3: Verify**

```bash
cat go.mod
go mod tidy
```

Expected: `go.mod` shows only the module + go directive. `go mod tidy` exits 0 (no dependencies to resolve since no source imports anything yet).

---

### Task 14: Replace `Makefile` with v1 skeleton

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Overwrite Makefile with the v1 skeleton**

Use `Write` to replace `Makefile` with:

```makefile
BINARY_NAME := tusk
BUILD_DIR := bin
GO := go
GOFLAGS := -v

.PHONY: all build clean test test-race vet lint fmt help

all: build

# v1 build target — populated as cmd/tusk lands in Plan 1b
build:
	@mkdir -p $(BUILD_DIR)
	@echo "v1 build target: cmd/tusk not yet implemented (Plan 1b)"

clean:
	rm -rf $(BUILD_DIR)

# v1 test target — populated as tests land
test:
	$(GO) test $(GOFLAGS) ./...

test-race:
	$(GO) test $(GOFLAGS) -race ./...

vet:
	$(GO) vet ./...

lint:
	@command -v golangci-lint >/dev/null || { echo "golangci-lint not installed"; exit 0; }
	golangci-lint run ./...

fmt:
	$(GO) fmt ./...

help:
	@echo "v1 Make targets:"
	@echo "  build      — compile the tusk binary (stub until Plan 1b)"
	@echo "  test       — run unit tests"
	@echo "  test-race  — run unit tests with race detector"
	@echo "  vet        — run go vet"
	@echo "  lint       — run golangci-lint"
	@echo "  fmt        — run gofmt across the tree"
	@echo "  clean      — remove build artifacts"
```

- [ ] **Step 2: Verify all targets execute**

```bash
make help
make build
make test
make vet
make fmt
make clean
```

Expected: all exit 0. `make build` prints the stub message. `make test` and `make vet` run on an empty Go tree (zero packages), exit 0.

---

### Task 15: Replace `PRODUCT.md` with v1 placeholder

**Files:**
- Modify: `PRODUCT.md`

- [ ] **Step 1: Overwrite PRODUCT.md**

Use `Write` to replace `PRODUCT.md` with:

```markdown
# Tusk v1 — Product

> 🚧 **v1 rebuild in progress.** This document is a placeholder. The full product description is regenerated as v1 features ship.
>
> The authoritative reference for v1's shape is the design spec at [`docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md`](docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md).
>
> For v0.x product information, check out the [`v0.14.0`](https://github.com/germanamz/tusk/releases/tag/v0.14.0) tag.
```

- [ ] **Step 2: Verify**

```bash
head -10 PRODUCT.md
```

Expected: placeholder block visible.

---

### Task 16: Trim `CLAUDE.md` for v1 scope

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Read existing CLAUDE.md**

```bash
wc -l CLAUDE.md
head -30 CLAUDE.md
```

Note the structure. CLAUDE.md currently contains v0-specific architecture notes (services, repositories, sqlite layer, filter parser, etc.).

- [ ] **Step 2: Replace CLAUDE.md with a v1 stub**

Use `Write` to replace `CLAUDE.md` with:

```markdown
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
make build          # build target — stub until cmd/tusk lands in Plan 1b
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
```

- [ ] **Step 3: Verify**

```bash
head -20 CLAUDE.md
wc -l CLAUDE.md
```

Expected: stub renders correctly; line count is small (~40 lines).

---

### Task 17: Replace `README.md` with v1 stub

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Overwrite README.md**

Use `Write` to replace `README.md` with:

```markdown
# Tusk v1

**A local-first agent brain.** A markdown vault with a smart, schema-validated, semantically-indexed graph layered on top.

> 🚧 **v1 rebuild in progress.** This README is a placeholder. Full documentation lands as v1 features ship.

## Status

- **Design spec:** [`docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md`](docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md)
- **Plans:** [`docs/superpowers/plans/`](docs/superpowers/plans/) — sequenced implementation plans.
- **v0 sources:** preserved at the [`v0.14.0`](https://github.com/germanamz/tusk/releases/tag/v0.14.0) tag and on the [`v0-archive`](https://github.com/germanamz/tusk/tree/v0-archive) branch.

## License

[Apache 2.0](LICENSE)
```

- [ ] **Step 2: Verify**

```bash
cat README.md
```

Expected: stub renders cleanly.

---

### Task 18: Update `.gitignore` for v1

**Files:**
- Modify: `.gitignore`

- [ ] **Step 1: Read current .gitignore**

```bash
cat .gitignore
```

- [ ] **Step 2: Ensure required entries are present**

The v1 `.gitignore` must include (in addition to whatever is already there):

```
# v1 local index
.tusk/

# Go build artifacts
bin/
dist/

# v0 dev DB (preserved here in case)
.data/
```

Use `Edit` to append any missing entries. If the file already contains all of them, no-op.

- [ ] **Step 3: Verify**

```bash
grep -E '^\.tusk/|^bin/|^dist/|^\.data/' .gitignore
```

Expected: all four patterns match.

---

### Task 19: Rebase `release-please-config.json` for v1.0.0

**Files:**
- Modify: `release-please-config.json`

- [ ] **Step 1: Read current config**

```bash
cat release-please-config.json
```

- [ ] **Step 2: Rewrite with v1 settings**

Use `Write` to replace `release-please-config.json` with:

```json
{
  "$schema": "https://raw.githubusercontent.com/googleapis/release-please/main/schemas/config.json",
  "release-type": "go",
  "include-component-in-tag": false,
  "separate-pull-requests": false,
  "bootstrap-sha": "TASK_19_PLACEHOLDER_v1_INITIAL_SHA",
  "initial-version": "1.0.0",
  "packages": {
    ".": {
      "package-name": "tusk",
      "changelog-path": "CHANGELOG.md",
      "pull-request-title-pattern": "chore(release): release ${version}"
    }
  }
}
```

- [ ] **Step 3: Verify (placeholder is intentional — replaced after the v1-init commit lands)**

```bash
grep -F TASK_19_PLACEHOLDER release-please-config.json
```

Expected: the placeholder line is present. We replace it in Task 22 once we have the SHA of the v1 initial commit.

---

### Task 20: Verify Make targets all execute on the v1 tree

**Files:** none (verification only)

- [ ] **Step 1: Run each target**

```bash
make help
make build
make test
make vet
make fmt
```

Expected: all exit 0. `make build` prints the stub. `make test` and `make vet` walk an empty Go tree (`./...` resolves to nothing) and exit cleanly.

- [ ] **Step 2: Confirm `go build ./...` is clean**

```bash
go build ./...
```

Expected: exit 0, no output. (No packages to build yet.)

---

### Task 21: Stage and commit the v1 initial state

**Files:** all changes from Tasks 12-20

- [ ] **Step 1: Confirm staged state**

```bash
git status --short
```

Expected: a long list of `D ` deletions (from Task 12) and `M ` modifications for `Makefile`, `PRODUCT.md`, `CLAUDE.md`, `README.md`, `.gitignore`, `release-please-config.json`, `go.mod`. No unexpected entries.

- [ ] **Step 2: Stage everything**

```bash
git add -A
git status --short
```

- [ ] **Step 3: Commit**

```bash
git commit -m "$(cat <<'EOF'
feat(v1)!: begin v1 rebuild from clean slate

Remove v0 source (cmd/, domain/, service/, repository/, sqlite/,
filter/, config/, internal/, migrations/, tests/, syntax/, etc.) and
v0 root files (client.go, install.sh, tusk.toml, go.sum, .data/).

Install a v1 skeleton:
  - Makefile with stub targets (build, test, vet, lint, fmt, clean)
  - go.mod reset to module path + go directive only
  - PRODUCT.md, CLAUDE.md, README.md replaced with v1 placeholders
    pointing at the design spec
  - release-please-config.json rebased for v1.0.0 (bootstrap-sha
    placeholder; replaced post-commit)
  - .gitignore updated with .tusk/, bin/, dist/, .data/

BREAKING CHANGE: this commit retires every v0.x interface. v0 sources
remain at the v0.14.0 tag and the v0-archive branch.

Refs: docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md
EOF
)"
```

- [ ] **Step 4: Capture the commit SHA**

```bash
V1_INIT_SHA=$(git rev-parse HEAD)
echo "v1 initial commit: $V1_INIT_SHA"
```

Note this SHA — Task 22 needs it.

---

### Task 22: Replace the release-please bootstrap-sha placeholder

**Files:**
- Modify: `release-please-config.json`

- [ ] **Step 1: Replace the placeholder with the captured SHA**

```bash
V1_INIT_SHA=$(git rev-parse HEAD)
sed -i "s/TASK_19_PLACEHOLDER_v1_INITIAL_SHA/${V1_INIT_SHA}/" release-please-config.json
```

(On macOS BSD sed: `sed -i '' "s/.../.../" release-please-config.json`.)

- [ ] **Step 2: Verify**

```bash
grep -F "$V1_INIT_SHA" release-please-config.json
grep -F TASK_19_PLACEHOLDER release-please-config.json || echo "placeholder removed"
```

Expected: SHA present; placeholder gone.

- [ ] **Step 3: Commit the bootstrap-sha update**

```bash
git add release-please-config.json
git commit -m "chore(release): pin release-please bootstrap-sha to v1 initial commit"
```

- [ ] **Step 4: Verify log**

```bash
git log --oneline -3
```

Expected: top two commits are the bootstrap-sha pin and the v1 init commit.

---

### Task 23: Push `v1` branch and open draft integration PR

**Files:** none

- [ ] **Step 1: Push the branch**

```bash
git push -u origin v1
```

Expected: push succeeds.

- [ ] **Step 2: Open the draft PR**

```bash
gh pr create --draft --base main --head v1 --title "v1 rebuild: long-lived integration branch" --body "$(cat <<'EOF'
## Summary

This is the long-lived integration branch for the **Tusk v1 rebuild**. Subsequent v1 plans (1b, 2, 3, ...) target this branch. It merges to `main` only when v1.0.0 is ready to ship.

## What's in the initial commit

- v0 source removed (`cmd/`, `domain/`, `service/`, `repository/`, `sqlite/`, `filter/`, `config/`, `internal/`, `migrations/`, `tests/`, `syntax/`, `client.go`, `install.sh`, `tusk.toml`, `go.sum`, `.data/`).
- v1 skeleton installed: Makefile stubs, reset `go.mod`, placeholder `PRODUCT.md`/`CLAUDE.md`/`README.md`, `.gitignore` updated, `release-please-config.json` rebased for v1.0.0.

## v0 preservation

- `v0.14.0` tag — last v0 release.
- `v0-archive` branch — emergency v0 patch target.
- `v0-final` tag — cleanup commit on `main` retiring v0 documentation.

## Spec

[`docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md`](docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md)

## Plans landing on this branch

- [x] Plan 1a — v0 cleanup + v1 branch setup *(this PR)*
- [ ] Plan 1b — first node lifecycle (`tusk init`, `node create`/`get`/`list`, `reindex`)
- [ ] Plan 2 — edges + relationships
- [ ] Plan 3 — watcher + ignore patterns + locks
- [ ] Plan 4 — filter grammar (lexer/AST/SQL compilation)
- [ ] Plan 5 — semantic retrieval (ollama)
- [ ] Plan 6 — MCP server
- [ ] Plan 7 — behavior + type packs
- [ ] Plan 8 — doctor + polish

## Test plan

- [ ] CI passes `make test`, `make vet`, `make lint` on each subsequent commit
- [ ] No commits land on `v1` until each plan's PR has been reviewed
EOF
)"
```

Expected: PR URL printed.

- [ ] **Step 3: Verify**

```bash
gh pr view --json url,state,isDraft
```

Expected: state OPEN, isDraft true, URL points at the new PR.

---

## Closing

After Task 23, the v1 integration target is live. Plan 1b begins next; its PR targets `v1`.

---

## Self-Review

After writing the plan, the implementer (or planning agent) should run a final read-through:

- [ ] **Spec coverage.** Does this plan cover every bullet in spec §3.3? (v0.14.0 preserved ✓, cleanup PR ✓, `v0-final` tag ✓, `v0-archive` branch ✓, v1 branch with `git rm` of obsolete source ✓, README banner ✓, no automatic data migration ✓.)
- [ ] **Placeholder scan.** Search the plan for `TBD`, `TODO`, vague phrasing. The `TASK_19_PLACEHOLDER` is intentional and gets replaced in Task 22. Otherwise should be clean.
- [ ] **Verification commands.** Each task has at least one verifiable expectation; no task ends with "looks good" without a runnable check.
- [ ] **Git operations safety.** No `--force`, no destructive commands without explicit user gates. The PR-merge step (Task 8) is gated on user review. Tag pushes and branch creation are routine.
