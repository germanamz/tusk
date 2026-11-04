# Agent retrieval benchmark

- Corpus: 36 markdown files, 164 sections (docs/packages)
- Questions: 40, 3 repetitions per arm
- Agent model: claude-sonnet-5-5; judge: haiku
- tusk v1.0.0-dev; Claude Code 2.1.318; 4 concurrent runs; 2026-11-04

## Per arm

Each figure is the median over questions of that question's median run, with the 25th to 75th percentile in brackets.

| Arm | Input tokens | Tool calls | Tool output | Time | Cost | Section cited | Judged correct | Runs (failed) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| tusk | 10,852 [10,657–18,560] | 1 [1–2] | 10.4 KB [9.3 KB–11.4 KB] | 5.6 s [4.9 s–6.9 s] | $0.027 [$0.025–$0.031] | 98% | 94% | 120 (0) |

## Per question

| Question | Kind | Arm | Input tokens | Tool calls | Tool output | Time | Section cited | Judged correct | Failed |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `address-table-cell` | lexical | tusk | 10,909 | 1 | 10.4 KB | 5.2 s | 100% | 100% | 0 |
| `alias-doctor-exit` | lexical | tusk | 10,036 | 1 | 8.2 KB | 3.8 s | 100% | 100% | 0 |
| `asset-route-symlink` | lexical | tusk | 11,360 | 1 | 11.7 KB | 6.9 s | 100% | 100% | 0 |
| `clones-same-mentions` | paraphrase | tusk | 30,349 | 2 | 18.2 KB | 10.2 s | 100% | 100% | 0 |
| `csp-inline-style` | lexical | tusk | 10,194 | 1 | 8.5 KB | 5.6 s | 100% | 100% | 0 |
| `filter-literal-asterisk` | paraphrase | tusk | 14,074 | 1 | 7.9 KB | 3.8 s | 100% | 100% | 0 |
| `fresh-clone-health` | paraphrase | tusk | 25,315 | 3 | 24.7 KB | 7.6 s | 100% | 100% | 0 |
| `hand-edited-state-skip` | paraphrase | tusk | 16,768 | 1 | 7.8 KB | 6.1 s | 100% | 100% | 0 |
| `hierarchy-default-twice` | lexical | tusk | 11,090 | 1 | 10.9 KB | 4.6 s | 100% | 100% | 0 |
| `identifier-not-path` | lexical | tusk | 19,989 | 2 | 14.4 KB | 7.1 s | 100% | 100% | 0 |
| `ignore-negation` | lexical | tusk | 9,188 | 1 | 5.9 KB | 4.9 s | 100% | 67% | 0 |
| `incoming-links-once` | paraphrase | tusk | 18,533 | 1 | 11.1 KB | 5.4 s | 100% | 100% | 0 |
| `link-case-reprocess` | lexical | tusk | 10,753 | 1 | 10.6 KB | 5.0 s | 100% | 100% | 0 |
| `lone-carriage-return-lines` | paraphrase | tusk | 10,421 | 1 | 9.3 KB | 5.2 s | 100% | 100% | 0 |
| `manifest-epoch-format` | lexical | tusk | 20,307 | 2 | 14.9 KB | 5.9 s | 100% | 100% | 0 |
| `meaning-based-positions` | paraphrase | tusk | 10,593 | 1 | 9.8 KB | 5.4 s | 100% | 100% | 0 |
| `move-note-name-links` | paraphrase | tusk | 10,774 | 1 | 11.0 KB | 6.8 s | 100% | 100% | 0 |
| `names-path-directory` | lexical | tusk | 10,840 | 1 | 10.9 KB | 4.5 s | 100% | 100% | 0 |
| `path-missing-severity` | lexical | tusk | 10,808 | 1 | 10.9 KB | 5.5 s | 100% | 100% | 0 |
| `plugin-check-ci` | lexical | tusk | 9,781 | 1 | 7.7 KB | 4.0 s | 100% | 100% | 0 |
| `query-takes-lock` | lexical | tusk | 14,760 | 2 | 5.7 KB | 5.4 s | 100% | 100% | 0 |
| `rebuild-without-ollama` | paraphrase | tusk | 20,300 | 3 | 15.4 KB | 9.8 s | 0% | 0% | 0 |
| `reload-parse-error` | lexical | tusk | 10,723 | 1 | 10.1 KB | 5.4 s | 100% | 100% | 0 |
| `renamed-heading-ids` | paraphrase | tusk | 10,995 | 1 | 11.1 KB | 4.8 s | 100% | 100% | 0 |
| `repeat-edit-reminder` | paraphrase | tusk | 10,704 | 1 | 10.1 KB | 6.3 s | 100% | 100% | 0 |
| `same-paragraph-twice` | paraphrase | tusk | 10,586 | 1 | 9.8 KB | 6.8 s | 100% | 33% | 0 |
| `schema-migrate-in-place` | lexical | tusk | 10,678 | 1 | 10.3 KB | 5.8 s | 100% | 100% | 0 |
| `second-condition-after-traversal` | paraphrase | tusk | 21,857 | 2 | 18.8 KB | 7.0 s | 100% | 100% | 0 |
| `session-summary-concurrency` | paraphrase | tusk | 46,015 | 3 | 23.7 KB | 8.1 s | 100% | 100% | 0 |
| `short-claim-window` | paraphrase | tusk | 10,315 | 1 | 9.1 KB | 4.5 s | 100% | 100% | 0 |
| `sibling-notices-rebuild` | paraphrase | tusk | 18,508 | 2 | 11.2 KB | 6.7 s | 100% | 67% | 0 |
| `slow-browser-tab` | paraphrase | tusk | 10,694 | 1 | 9.8 KB | 4.6 s | 100% | 100% | 0 |
| `swap-wait-forever` | paraphrase | tusk | 10,079 | 1 | 8.2 KB | 8.3 s | 100% | 100% | 0 |
| `unquoted-frontmatter-wikilink` | lexical | tusk | 23,383 | 2 | 20.0 KB | 9.4 s | 100% | 100% | 0 |
| `update-cosign` | lexical | tusk | 10,864 | 1 | 10.5 KB | 4.4 s | 100% | 100% | 0 |
| `update-install-method` | lexical | tusk | 10,794 | 1 | 10.1 KB | 5.5 s | 100% | 100% | 0 |
| `updater-any-string` | paraphrase | tusk | 22,398 | 2 | 18.7 KB | 7.6 s | 100% | 100% | 0 |
| `vault-pack-offline` | lexical | tusk | 9,152 | 1 | 6.1 KB | 5.7 s | 100% | 100% | 0 |
| `wikilink-in-code-fence` | lexical | tusk | 10,678 | 1 | 10.2 KB | 4.9 s | 100% | 100% | 0 |
| `wipe-fails-partway` | paraphrase | tusk | 18,643 | 2 | 11.3 KB | 6.4 s | 100% | 100% | 0 |

## Charts

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="charts/per-arm-dark.svg">
  <img alt="Spread of input tokens, tool calls, tool output and time per question, by arm" src="charts/per-arm-light.svg">
</picture>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="charts/accuracy-dark.svg">
  <img alt="Share of runs that cited the right section and that the judge marked correct, by arm and question kind" src="charts/accuracy-light.svg">
</picture>

