# Agent retrieval benchmark

- Corpus: 34 markdown files, 151 sections (docs/packages)
- Questions: 40, 3 repetitions per arm
- Agent model: claude-sonnet-5-5; judge: haiku
- tusk v1.0.0-dev (9e6e5b3); Claude Code 2.1.300; 4 concurrent runs; 2026-10-15

## Per arm

Each figure is the median over questions of that question's median run, with the 25th to 75th percentile in brackets.

| Arm | Input tokens | Tool calls | Tool output | Time | Cost | Section cited | Judged correct | Runs (failed) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| grep | 18,012 [14,406–21,943] | 3 [2–4] | 7.6 KB [4.6 KB–10.2 KB] | 7.3 s [6.3 s–9.0 s] | $0.025 [$0.018–$0.031] | 95% | 99% | 120 (0) |
| tusk | 21,156 [13,173–23,505] | 2 [1–2] | 10.9 KB [9.4 KB–13.7 KB] | 6.7 s [5.4 s–8.4 s] | $0.033 [$0.029–$0.037] | 100% | 100% | 120 (0) |

## tusk vs grep, paired by question

A ratio below 1 means tusk used less than grep on the same question.

| Metric | Median ratio (tusk / grep) | 95% CI | Questions where tusk used less |
| --- | --- | --- | --- |
| Input tokens | 1.15 | 1.01–1.39 | 13 of 40 |
| Tool output | 1.41 | 1.19–2.33 | 5 of 40 |
| Time | 0.99 | 0.82–1.09 | 20 of 40 |
| Cost | 1.36 | 1.24–1.72 | 7 of 40 |

| Metric | Median difference (tusk − grep) | 95% CI | Questions where tusk used fewer |
| --- | --- | --- | --- |
| Tool calls | -1 | -1 to -0.5 | 26 of 40 |

| Accuracy | grep | tusk | Difference | 95% CI |
| --- | --- | --- | --- | --- |
| Section cited | 95% | 100% | +5 pp | +1 pp to +10 pp |
| Judged correct | 99% | 100% | +1 pp | +0 pp to +2 pp |

### By question kind

| Kind | Questions | Arm | Input tokens | Tool calls | Section cited | Judged correct |
| --- | --- | --- | --- | --- | --- | --- |
| lexical | 20 | grep | 15,614 | 2 | 98% | 98% |
| lexical | 20 | tusk | 15,366 | 1 | 100% | 100% |
| paraphrase | 20 | grep | 19,806 | 3 | 92% | 100% |
| paraphrase | 20 | tusk | 22,299 | 2 | 100% | 100% |

### Where tusk lost

27 of 40 questions: tusk read more input tokens than grep, or was judged correct less often.

| Question | Kind | Input tokens (grep → tusk) | Tool calls (grep → tusk) | Judged correct (grep / tusk) |
| --- | --- | --- | --- | --- |
| `asset-route-symlink` How does the `/api/read/asset` route stop a symlink from exposing files under `.tusk` or `.git`? | lexical | 18,150 → 24,911 | 3 → 2 | 100% / 100% |
| `clones-same-mentions` Two people clone the same vault on different machines with different files checked out. Will indexing record the same file mentions for both? | paraphrase | 15,103 → 23,138 | 3 → 2 | 100% / 100% |
| `csp-inline-style` Why does the web app's CSP allow `'unsafe-inline'` styles but never inline scripts? | lexical | 12,756 → 12,778 | 3 → 1 | 100% / 100% |
| `fresh-clone-health` Right after cloning a repo, before anything has been indexed, what does the health check report? | paraphrase | 17,241 → 28,852 | 3 → 2 | 100% / 100% |
| `identifier-not-path` Is an inline code span like `ExtractWikilinks` recorded as a path ref? | lexical | 16,633 → 20,949 | 2 → 2 | 100% / 100% |
| `ignore-negation` Does `[workspace] ignore` in tusk.toml support gitignore-style `!` negation? | lexical | 14,442 → 14,746 | 2 → 1 | 100% / 100% |
| `incoming-links-once` When several parts of a single page all reference the same note, does the reader's incoming-links list show that page once or once per part? | paraphrase | 15,457 → 24,460 | 2 → 2 | 100% / 100% |
| `link-case-reprocess` Why does changing `[workspace] link-case` force a full re-process of the vault? | lexical | 21,338 → 39,653 | 4 → 3 | 67% / 100% |
| `lone-carriage-return-lines` A file uses bare carriage returns as line breaks, like old Mac files. With default settings, will tusk's reported line numbers match what most editors show? | paraphrase | 12,523 → 21,460 | 2 → 2 | 100% / 100% |
| `manifest-epoch-format` What is the on-disk format of `.tusk/manifest-epoch`, and can it be changed? | lexical | 14,296 → 21,770 | 2 → 2 | 100% / 100% |
| `meaning-based-positions` When the 3D view arranges notes by meaning instead of by links, how are their positions computed? | paraphrase | 17,222 → 21,274 | 2 → 2 | 100% / 100% |
| `names-path-directory` Does `names-path=server/ledger/core.go` also match a page that only names the `server/ledger` directory? | lexical | 21,668 → 23,009 | 2 → 3 | 100% / 100% |
| `plugin-check-ci` Why doesn't `make claude-plugin-check` run in CI? | lexical | 7,622 → 11,981 | 1 → 1 | 100% / 100% |
| `query-takes-lock` Does `tusk query` take the workspace lock? | lexical | 14,104 → 15,987 | 2 → 1 | 100% / 100% |
| `rebuild-without-ollama` What happens when you rebuild the index while Ollama isn't running? | paraphrase | 36,452 → 51,053 | 6 → 4 | 100% / 100% |
| `reload-parse-error` What happens if `tusk_reload` hits a TOML parse error in tusk.toml? | lexical | 9,420 → 21,841 | 1 → 2 | 100% / 100% |
| `repeat-edit-reminder` If an agent edits the same source file ten times in one session, does tusk add the list of docs that mention that file after every edit? | paraphrase | 19,995 → 26,027 | 3 → 2 | 100% / 100% |
| `second-condition-after-traversal` In a filter that follows links out to other notes and then adds a second condition after it, does the second condition apply to the linked notes or to the starting notes? | paraphrase | 19,557 → 40,329 | 4 → 3 | 100% / 100% |
| `session-summary-concurrency` When a Claude Code session starts, the vault summary can include several named saved queries. Are those run one after another or concurrently? | paraphrase | 19,412 → 21,267 | 4 → 2 | 100% / 100% |
| `short-claim-window` What goes wrong if the time a worker may hold a claimed job is set very short, like one second? | paraphrase | 13,523 → 21,154 | 2 → 2 | 100% / 100% |
| `slow-browser-tab` What happens to live updates for a browser tab that reads too slowly and falls behind? | paraphrase | 22,768 → 42,507 | 3 → 4 | 100% / 100% |
| `swap-wait-forever` Inside one MCP server process, could two operations that swap out the open database end up waiting on each other forever? | paraphrase | 22,895 → 25,670 | 3 → 2 | 100% / 100% |
| `update-install-method` How does `tusk update` tell a `go install` binary apart from a local `make build`, given both report a -dev version? | lexical | 12,877 → 20,357 | 2 → 2 | 100% / 100% |
| `updater-any-string` What could go wrong if the self-updater accepted any string as the release to install? | paraphrase | 14,649 → 21,159 | 2 → 2 | 100% / 100% |
| `vault-pack-offline` Can `tusk init` add the built-in vault pack without network access? | lexical | 17,355 → 21,729 | 3 → 2 | 100% / 100% |
| `wikilink-in-code-fence` Does a `[[wikilink]]` written inside a fenced code block create an edge? | lexical | 12,921 → 14,740 | 2 → 1 | 100% / 100% |
| `wipe-fails-partway` If wiping and recreating the search database fails partway through, will other running servers switch over to the half-finished copy? | paraphrase | 19,908 → 23,270 | 2 → 2 | 100% / 100% |

## Per question

| Question | Kind | Arm | Input tokens | Tool calls | Tool output | Time | Section cited | Judged correct | Failed |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `address-table-cell` | lexical | grep | 26,933 | 4 | 6.3 KB | 9.5 s | 100% | 100% | 0 |
| `address-table-cell` | lexical | tusk | 20,600 | 2 | 8.8 KB | 6.8 s | 100% | 100% | 0 |
| `alias-doctor-exit` | lexical | grep | 17,874 | 2 | 8.1 KB | 6.6 s | 67% | 100% | 0 |
| `alias-doctor-exit` | lexical | tusk | 12,674 | 1 | 8.6 KB | 4.1 s | 100% | 100% | 0 |
| `asset-route-symlink` | lexical | grep | 18,150 | 3 | 3.8 KB | 11.5 s | 100% | 100% | 0 |
| `asset-route-symlink` | lexical | tusk | 24,911 | 2 | 17.6 KB | 9.2 s | 100% | 100% | 0 |
| `clones-same-mentions` | paraphrase | grep | 15,103 | 3 | 9.4 KB | 7.3 s | 100% | 100% | 0 |
| `clones-same-mentions` | paraphrase | tusk | 23,138 | 2 | 13.6 KB | 8.2 s | 100% | 100% | 0 |
| `csp-inline-style` | lexical | grep | 12,756 | 3 | 1.6 KB | 5.9 s | 100% | 100% | 0 |
| `csp-inline-style` | lexical | tusk | 12,778 | 1 | 9.0 KB | 5.4 s | 100% | 100% | 0 |
| `filter-literal-asterisk` | paraphrase | grep | 19,703 | 4 | 5.1 KB | 6.7 s | 100% | 100% | 0 |
| `filter-literal-asterisk` | paraphrase | tusk | 12,549 | 1 | 8.9 KB | 4.2 s | 100% | 100% | 0 |
| `fresh-clone-health` | paraphrase | grep | 17,241 | 3 | 13.6 KB | 6.9 s | 100% | 100% | 0 |
| `fresh-clone-health` | paraphrase | tusk | 28,852 | 2 | 23.0 KB | 8.9 s | 100% | 100% | 0 |
| `hand-edited-state-skip` | paraphrase | grep | 27,921 | 3 | 11.8 KB | 8.0 s | 100% | 100% | 0 |
| `hand-edited-state-skip` | paraphrase | tusk | 24,210 | 2 | 13.7 KB | 6.6 s | 100% | 100% | 0 |
| `hierarchy-default-twice` | lexical | grep | 13,325 | 2 | 2.5 KB | 7.7 s | 100% | 100% | 0 |
| `hierarchy-default-twice` | lexical | tusk | 12,637 | 1 | 9.1 KB | 3.9 s | 100% | 100% | 0 |
| `identifier-not-path` | lexical | grep | 16,633 | 2 | 8.6 KB | 6.2 s | 100% | 100% | 0 |
| `identifier-not-path` | lexical | tusk | 20,949 | 2 | 9.3 KB | 6.5 s | 100% | 100% | 0 |
| `ignore-negation` | lexical | grep | 14,442 | 2 | 4.3 KB | 10.2 s | 100% | 100% | 0 |
| `ignore-negation` | lexical | tusk | 14,746 | 1 | 13.8 KB | 4.7 s | 100% | 100% | 0 |
| `incoming-links-once` | paraphrase | grep | 15,457 | 2 | 9.8 KB | 5.7 s | 100% | 100% | 0 |
| `incoming-links-once` | paraphrase | tusk | 24,460 | 2 | 13.6 KB | 6.7 s | 100% | 100% | 0 |
| `link-case-reprocess` | lexical | grep | 21,338 | 4 | 6.2 KB | 8.9 s | 100% | 67% | 0 |
| `link-case-reprocess` | lexical | tusk | 39,653 | 3 | 24.8 KB | 10.7 s | 100% | 100% | 0 |
| `lone-carriage-return-lines` | paraphrase | grep | 12,523 | 2 | 2.1 KB | 5.8 s | 100% | 100% | 0 |
| `lone-carriage-return-lines` | paraphrase | tusk | 21,460 | 2 | 9.5 KB | 7.7 s | 100% | 100% | 0 |
| `manifest-epoch-format` | lexical | grep | 14,296 | 2 | 3.6 KB | 5.4 s | 100% | 100% | 0 |
| `manifest-epoch-format` | lexical | tusk | 21,770 | 2 | 11.5 KB | 7.7 s | 100% | 100% | 0 |
| `meaning-based-positions` | paraphrase | grep | 17,222 | 2 | 8.9 KB | 7.3 s | 100% | 100% | 0 |
| `meaning-based-positions` | paraphrase | tusk | 21,274 | 2 | 10.5 KB | 7.4 s | 100% | 100% | 0 |
| `move-note-name-links` | paraphrase | grep | 49,318 | 8 | 19.9 KB | 14.1 s | 67% | 100% | 0 |
| `move-note-name-links` | paraphrase | tusk | 13,194 | 1 | 10.8 KB | 7.5 s | 100% | 100% | 0 |
| `names-path-directory` | lexical | grep | 21,668 | 2 | 13.8 KB | 6.3 s | 100% | 100% | 0 |
| `names-path-directory` | lexical | tusk | 23,009 | 3 | 12.5 KB | 8.8 s | 100% | 100% | 0 |
| `path-missing-severity` | lexical | grep | 24,320 | 4 | 11.3 KB | 9.2 s | 100% | 100% | 0 |
| `path-missing-severity` | lexical | tusk | 12,562 | 1 | 8.8 KB | 4.3 s | 100% | 100% | 0 |
| `plugin-check-ci` | lexical | grep | 7,622 | 1 | 0.6 KB | 3.6 s | 100% | 100% | 0 |
| `plugin-check-ci` | lexical | tusk | 11,981 | 1 | 7.1 KB | 3.6 s | 100% | 100% | 0 |
| `query-takes-lock` | lexical | grep | 14,104 | 2 | 3.8 KB | 6.0 s | 100% | 100% | 0 |
| `query-takes-lock` | lexical | tusk | 15,987 | 1 | 15.0 KB | 6.3 s | 100% | 100% | 0 |
| `rebuild-without-ollama` | paraphrase | grep | 36,452 | 6 | 21.4 KB | 11.8 s | 67% | 100% | 0 |
| `rebuild-without-ollama` | paraphrase | tusk | 51,053 | 4 | 27.7 KB | 14.1 s | 100% | 100% | 0 |
| `reload-parse-error` | lexical | grep | 9,420 | 1 | 4.8 KB | 4.7 s | 100% | 100% | 0 |
| `reload-parse-error` | lexical | tusk | 21,841 | 2 | 10.9 KB | 7.0 s | 100% | 100% | 0 |
| `renamed-heading-ids` | paraphrase | grep | 22,777 | 2 | 12.3 KB | 8.2 s | 100% | 100% | 0 |
| `renamed-heading-ids` | paraphrase | tusk | 20,326 | 2 | 12.3 KB | 9.3 s | 100% | 100% | 0 |
| `repeat-edit-reminder` | paraphrase | grep | 19,995 | 3 | 7.6 KB | 9.9 s | 100% | 100% | 0 |
| `repeat-edit-reminder` | paraphrase | tusk | 26,027 | 2 | 18.4 KB | 10.1 s | 100% | 100% | 0 |
| `same-paragraph-twice` | paraphrase | grep | 21,011 | 4 | 8.2 KB | 9.7 s | 100% | 100% | 0 |
| `same-paragraph-twice` | paraphrase | tusk | 13,018 | 1 | 9.6 KB | 5.2 s | 100% | 100% | 0 |
| `schema-migrate-in-place` | lexical | grep | 28,838 | 5 | 8.3 KB | 8.0 s | 100% | 100% | 0 |
| `schema-migrate-in-place` | lexical | tusk | 13,251 | 1 | 10.9 KB | 4.8 s | 100% | 100% | 0 |
| `second-condition-after-traversal` | paraphrase | grep | 19,557 | 4 | 7.1 KB | 8.4 s | 100% | 100% | 0 |
| `second-condition-after-traversal` | paraphrase | tusk | 40,329 | 3 | 22.8 KB | 9.9 s | 100% | 100% | 0 |
| `session-summary-concurrency` | paraphrase | grep | 19,412 | 4 | 5.7 KB | 8.4 s | 100% | 100% | 0 |
| `session-summary-concurrency` | paraphrase | tusk | 21,267 | 2 | 9.5 KB | 6.5 s | 100% | 100% | 0 |
| `short-claim-window` | paraphrase | grep | 13,523 | 2 | 4.9 KB | 7.1 s | 100% | 100% | 0 |
| `short-claim-window` | paraphrase | tusk | 21,154 | 2 | 10.0 KB | 6.4 s | 100% | 100% | 0 |
| `sibling-notices-rebuild` | paraphrase | grep | 20,748 | 3 | 14.4 KB | 7.4 s | 33% | 100% | 0 |
| `sibling-notices-rebuild` | paraphrase | tusk | 12,819 | 1 | 9.5 KB | 6.4 s | 100% | 100% | 0 |
| `slow-browser-tab` | paraphrase | grep | 22,768 | 3 | 6.7 KB | 6.4 s | 67% | 100% | 0 |
| `slow-browser-tab` | paraphrase | tusk | 42,507 | 4 | 26.1 KB | 9.8 s | 100% | 100% | 0 |
| `swap-wait-forever` | paraphrase | grep | 22,895 | 3 | 16.0 KB | 10.9 s | 100% | 100% | 0 |
| `swap-wait-forever` | paraphrase | tusk | 25,670 | 2 | 14.4 KB | 8.8 s | 100% | 100% | 0 |
| `unquoted-frontmatter-wikilink` | lexical | grep | 24,776 | 4 | 9.6 KB | 10.4 s | 100% | 100% | 0 |
| `unquoted-frontmatter-wikilink` | lexical | tusk | 13,109 | 1 | 10.2 KB | 4.4 s | 100% | 100% | 0 |
| `update-cosign` | lexical | grep | 14,596 | 3 | 7.4 KB | 5.6 s | 100% | 100% | 0 |
| `update-cosign` | lexical | tusk | 12,681 | 1 | 8.9 KB | 6.5 s | 100% | 100% | 0 |
| `update-install-method` | lexical | grep | 12,877 | 2 | 3.5 KB | 6.2 s | 100% | 100% | 0 |
| `update-install-method` | lexical | tusk | 20,357 | 2 | 8.3 KB | 8.3 s | 100% | 100% | 0 |
| `updater-any-string` | paraphrase | grep | 14,649 | 2 | 7.6 KB | 7.2 s | 100% | 100% | 0 |
| `updater-any-string` | paraphrase | tusk | 21,159 | 2 | 9.8 KB | 7.6 s | 100% | 100% | 0 |
| `vault-pack-offline` | lexical | grep | 17,355 | 3 | 8.7 KB | 7.6 s | 100% | 100% | 0 |
| `vault-pack-offline` | lexical | tusk | 21,729 | 2 | 10.1 KB | 6.4 s | 100% | 100% | 0 |
| `wikilink-in-code-fence` | lexical | grep | 12,921 | 2 | 2.2 KB | 7.5 s | 100% | 100% | 0 |
| `wikilink-in-code-fence` | lexical | tusk | 14,740 | 1 | 13.7 KB | 5.0 s | 100% | 100% | 0 |
| `wipe-fails-partway` | paraphrase | grep | 19,908 | 2 | 11.7 KB | 6.9 s | 100% | 100% | 0 |
| `wipe-fails-partway` | paraphrase | tusk | 23,270 | 2 | 12.9 KB | 6.7 s | 100% | 100% | 0 |

## Charts

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="charts/tokens-tusk-vs-grep-dark.svg">
  <img alt="Input tokens per question, tusk against grep, on a log scale" src="charts/tokens-tusk-vs-grep-light.svg">
</picture>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="charts/per-arm-dark.svg">
  <img alt="Spread of input tokens, tool calls, tool output and time per question, by arm" src="charts/per-arm-light.svg">
</picture>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="charts/accuracy-dark.svg">
  <img alt="Share of runs that cited the right section and that the judge marked correct, by arm and question kind" src="charts/accuracy-light.svg">
</picture>

