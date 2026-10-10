# Rules and doctor

Types and edges say what a node may contain. `tusk doctor` checks the vault against them, plus any conventions you declare as rules, and exits 1 when it finds an error. That makes it something you can run after every agent edit, or in CI.

## Health checks

```bash
tusk status     # node counts, edge count, embed-queue depth, last reindex timestamp
tusk doctor     # health check; exits 1 when an error is present
```

Every doctor finding has a severity, and the report lists errors first. An `error` means the vault or the index is wrong: a dangling link, a property that breaks its declaration, a file reindex could not parse, a pinned id left behind by a rename. A `warning` means things are correct but degraded, such as a node missing from semantic results or a type you never declared. `advice` is a hint. `tusk doctor` exits 1 when an error is present, so you (or an agent) can run it after every edit and treat a non-zero exit as "I broke something". `--fail-on=warning` fails on warnings too; `--fail-on=never` always exits 0.

`doctor` is also the place to look when:

- semantic queries seem to be missing nodes → check the embed-queue depth and last error
- a wikilink points to nothing → a `dangling-edge` error names it
- a manifest change just landed → re-validate every affected node
- a convention you declared as a `[rule.<name>]` is broken → each offending node is a `rule:<name>` issue

## Rules: conventions doctor checks

A vault usually has conventions on top of its types, like "product notes live under `docs/product/`". Write one as a filter that should match nothing:

```toml
[rule.domain-matches-directory]
description = "a product note lives under docs/product/"
filter      = "type=note AND domain=product AND NOT path=docs/product/**"

[rule.product-links-stay-in-product]
description = "a product page links only to product pages and the glossary"
filter      = "domain=product AND references-> (type=note AND NOT domain=product AND NOT id=docs/glossary)"
severity    = "warning"   # error (the default) | warning | advice
```

`tusk doctor` runs every rule and reports each matching node as a `rule:<name>` issue at the rule's severity, so a broken convention fails it like a dangling link would:

```console
$ tusk doctor
doctor: 2 errors, 0 warnings, 0 advice
  error    [dangling-edge] docs/product/refunds: edge "references" -> "docs/product/refund-policy" (target missing); also in #S1, #S1P1
  error    [rule:domain-matches-directory] docs/technical/billing: a product note lives under docs/product/
…
$ echo $?
1
```

The filter is the same grammar `tusk query` takes, so you can try it there first. Two differences: a rule matches file nodes only (sub-units share their file's `path`) unless the filter selects a sub-unit type such as `type=section`, and a rule's filter must name declared types, properties and enum values, because a typo there would match nothing and pass forever. A rule that fails those checks doesn't stop anything from loading; `tusk reload` lists it under warnings and `tusk doctor` reports it as a `rule-invalid` error.

## Running doctor in CI

`.tusk/` is gitignored, so a CI runner starts without an index, and doctor only checks what's in the index. Build it first. Two current gaps shape the recipe, both tracked in [#787](https://github.com/germanamz/tusk/issues/787):

- `tusk doctor` on a checkout that was never indexed reports nothing and exits 0.
- `tusk reindex` exits 1 when `[embeddings]` is configured and Ollama isn't reachable, which it usually isn't on a runner, even though the index itself was built.

Until #787 lands, this works on GitHub Actions:

```yaml
- name: Install tusk
  run: |
    curl -fsSL https://raw.githubusercontent.com/germanamz/tusk/main/install.sh | sh
    echo "$HOME/.local/bin" >> "$GITHUB_PATH"
- name: Build the index
  run: tusk reindex || true   # exits 1 when it can't reach Ollama to embed; the index is still built
- name: Check the vault
  run: tusk doctor
```

The `|| true` also hides a reindex that fails for another reason. Doctor reports any file reindex couldn't parse as a `skipped-file` error, so the check still fails on broken files.
