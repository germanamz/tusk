---
type: package
title: internal/pathglob — path patterns for filters
import-path: github.com/germanamz/tusk/internal/pathglob
status: stable
---

# internal/pathglob

Translates the glob patterns a filter writes against `path` and `id` (`path=docs/product/*`) into anchored regular expressions. The [filter compiler](filter.md) binds the result to a `REGEXP` comparison, and [`internal/index`](index.md) registers the function SQLite calls for that operator.

## Public surface

- `IsPattern(value string) bool` reports whether a value holds a wildcard (`*` or `?`).
- `ToRegexp(pattern string) string` translates a pattern into a regex anchored with `^…$`.

## Pattern language

The wildcards follow the rules gitignore documents, so the syntax is the one people already write in `[workspace] ignore`. The match is always anchored to the whole value, though: `path` is workspace-relative, so a pattern names files from the vault root.

| Pattern | Matches |
|---|---|
| `*` | any run of characters except `/`, so it stays inside one segment |
| `?` | exactly one character other than `/` |
| `**/x` | `x` at any depth, including the root |
| `a/**/b` | `a/b`, `a/x/b`, `a/x/y/b` and so on |
| `a/**` | everything inside `a`, but not `a` itself |
| `**` | everything |

A `**` that shares a segment with other characters (`a**b`) acts as a single `*`, the way git treats it. Every other character is literal, regex metacharacters included. There are no character classes, because `[` is a real filename character in vaults (see #683).

The filter grammar decides when a value is a pattern. Only a bare value can be one, so quoting is the escape: `path="odd/file*.md"` compares literally.

## Why `internal/ignore` doesn't do this

`internal/ignore` wraps `go-gitignore`, which implements gitignore matching rather than plain globbing. Its patterns aren't anchored unless they start with `/`, a matched directory pulls in its whole subtree, `?` is a literal question mark, and regex metacharacters such as `+` pass through unescaped. That suits the reindex walk, but `path=docs/product/*` would return the whole subtree plus any `other/docs/product/...` path. So filters get their own small translator that implements the documented wildcard rules exactly.

## Portability

The output is a plain regex string rather than a Go matcher, so the database engine evaluates it natively. It uses only syntax that SQLite's Go-backed `REGEXP`, Postgres `~`, MySQL `REGEXP_LIKE` and DuckDB read the same way: `^…$` anchors, the `[^/]` class, the `([^/]*/)*` directory group, and metacharacters escaped with `regexp.QuoteMeta`, whose escape set is the POSIX ERE metacharacters. It never emits RE2-only syntax such as `(?:` and never uses `.`, so engines that disagree about `.` and newlines still agree. A test guards that subset.
