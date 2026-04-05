# Branch Naming Enforcement

## Convention

All branches except `main` must match:

```
^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)/[a-z0-9]+(-[a-z0-9]+)*$
```

- **Type prefix:** One of the conventional commit types defined in `.conform.yaml`
- **Description:** Kebab-case (`lowercase-words-separated-by-hyphens`)
- **Exempt:** `main`

Examples: `feat/add-filters`, `fix/null-pointer`, `ci/branch-naming`

## Enforcement Points

### 1. Local — Lefthook `pre-push` hook

Add a `pre-push` section to `lefthook.yml` with a single command that:

1. Gets the current branch name via `git rev-parse --abbrev-ref HEAD`
2. Allows `main` through
3. Validates against the regex pattern
4. Fails with a descriptive error message if the branch name doesn't match

### 2. CI — New `branch-name` job in `.github/workflows/ci.yml`

Add a new job that:

1. Extracts the PR head branch from `github.head_ref`
2. Validates it against the same regex
3. Fails the check with a descriptive error if it doesn't match

## Single Source of Truth

The types list comes from `.conform.yaml`. The CI job extracts types dynamically (same approach as the existing `pr-title` job). The local hook hardcodes the types in the regex for simplicity — they change rarely.

## Files to Modify

- `lefthook.yml` — add `pre-push` section
- `.github/workflows/ci.yml` — add `branch-name` job
