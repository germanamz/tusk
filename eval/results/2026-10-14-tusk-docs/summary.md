# Agent retrieval benchmark

- Corpus: 34 markdown files, 149 sections (docs/packages)
- Questions: 40, 3 repetitions per arm
- Agent model: claude-sonnet-5-5; judge: haiku
- tusk v1.0.0-dev (1ab211a, modified); Claude Code 2.1.299; 4 concurrent runs; 2026-10-14

## Per arm

Each figure is the median over questions of that question's median run, with the 25th to 75th percentile in brackets.

| Arm | Input tokens | Tool calls | Tool output | Time | Cost | Section cited | Judged correct | Runs (failed) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| grep | 17,108 [14,182–21,332] | 3 [2–4] | 7.6 KB [4.6 KB–9.7 KB] | 7.7 s [6.3 s–10.4 s] | $0.024 [$0.018–$0.032] | 91% | 98% | 120 (0) |
| tusk | 20,809 [12,354–23,341] | 2 [1–2] | 10.4 KB [9.1 KB–14.6 KB] | 7.7 s [5.7 s–8.6 s] | $0.033 [$0.027–$0.039] | 100% | 100% | 120 (0) |

## tusk vs grep, paired by question

A ratio below 1 means tusk used less than grep on the same question.

| Metric | Median ratio (tusk / grep) | 95% CI | Questions where tusk used less |
| --- | --- | --- | --- |
| Input tokens | 1.11 | 0.91–1.41 | 16 of 40 |
| Tool output | 1.41 | 1.18–2.11 | 8 of 40 |
| Time | 0.99 | 0.84–1.10 | 22 of 40 |
| Cost | 1.37 | 1.26–1.64 | 7 of 40 |

| Metric | Median difference (tusk − grep) | 95% CI | Questions where tusk used fewer |
| --- | --- | --- | --- |
| Tool calls | -1 | -1 to +0 | 25 of 40 |

| Accuracy | grep | tusk | Difference | 95% CI |
| --- | --- | --- | --- | --- |
| Section cited | 91% | 100% | +9 pp | +2 pp to +17 pp |
| Judged correct | 98% | 100% | +2 pp | +0 pp to +7 pp |

### By question kind

| Kind | Questions | Arm | Input tokens | Tool calls | Section cited | Judged correct |
| --- | --- | --- | --- | --- | --- | --- |
| lexical | 20 | grep | 15,143 | 2.5 | 93% | 97% |
| lexical | 20 | tusk | 16,194 | 1.5 | 100% | 100% |
| paraphrase | 20 | grep | 17,756 | 3 | 88% | 98% |
| paraphrase | 20 | tusk | 21,737 | 2 | 100% | 100% |

### Where tusk lost

24 of 40 questions: tusk read more input tokens than grep, or was judged correct less often.

| Question | Kind | Input tokens (grep → tusk) | Tool calls (grep → tusk) | Judged correct (grep / tusk) |
| --- | --- | --- | --- | --- |
| `address-table-cell` What does the sub-unit address `T1R0C2` refer to? | lexical | 19,462 → 21,099 | 2 → 2 | 100% / 100% |
| `asset-route-symlink` How does the `/api/read/asset` route stop a symlink from exposing files under `.tusk` or `.git`? | lexical | 17,505 → 24,204 | 3 → 2 | 100% / 100% |
| `clones-same-mentions` Two people clone the same vault on different machines with different files checked out. Will indexing record the same file mentions for both? | paraphrase | 15,077 → 22,662 | 2 → 2 | 100% / 100% |
| `fresh-clone-health` Right after cloning a repo, before anything has been indexed, what does the health check report? | paraphrase | 16,986 → 23,054 | 3 → 2 | 100% / 100% |
| `identifier-not-path` Is an inline code span like `ExtractWikilinks` recorded as a path ref? | lexical | 15,827 → 24,201 | 2 → 2 | 100% / 100% |
| `incoming-links-once` When several parts of a single page all reference the same note, does the reader's incoming-links list show that page once or once per part? | paraphrase | 15,397 → 37,667 | 2 → 3 | 100% / 100% |
| `link-case-reprocess` Why does changing `[workspace] link-case` force a full re-process of the vault? | lexical | 21,218 → 58,304 | 4 → 6 | 100% / 100% |
| `lone-carriage-return-lines` A file uses bare carriage returns as line breaks, like old Mac files. With default settings, will tusk's reported line numbers match what most editors show? | paraphrase | 12,344 → 25,933 | 2 → 2 | 100% / 100% |
| `manifest-epoch-format` What is the on-disk format of `.tusk/manifest-epoch`, and can it be changed? | lexical | 14,296 → 21,052 | 2 → 2 | 100% / 100% |
| `meaning-based-positions` When the 3D view arranges notes by meaning instead of by links, how are their positions computed? | paraphrase | 17,230 → 20,641 | 2 → 2 | 100% / 100% |
| `names-path-directory` Does `names-path=server/ledger/core.go` also match a page that only names the `server/ledger` directory? | lexical | 21,674 → 22,363 | 2 → 3 | 100% / 100% |
| `plugin-check-ci` Why doesn't `make claude-plugin-check` run in CI? | lexical | 7,622 → 11,533 | 1 → 1 | 100% / 100% |
| `query-takes-lock` Does `tusk query` take the workspace lock? | lexical | 13,654 → 19,509 | 3 → 2 | 100% / 100% |
| `rebuild-without-ollama` What happens when you rebuild the index while Ollama isn't running? | paraphrase | 41,758 → 47,681 | 5 → 4 | 100% / 100% |
| `reload-parse-error` What happens if `tusk_reload` hits a TOML parse error in tusk.toml? | lexical | 13,782 → 21,311 | 3 → 2 | 100% / 100% |
| `same-paragraph-twice` If two notes contain exactly the same paragraph, does tusk call the embedding model for it twice? | paraphrase | 20,368 → 21,073 | 5 → 1 | 100% / 100% |
| `second-condition-after-traversal` In a filter that follows links out to other notes and then adds a second condition after it, does the second condition apply to the linked notes or to the starting notes? | paraphrase | 18,281 → 33,033 | 4 → 3 | 100% / 100% |
| `short-claim-window` What goes wrong if the time a worker may hold a claimed job is set very short, like one second? | paraphrase | 13,838 → 20,933 | 2 → 2 | 100% / 100% |
| `sibling-notices-rebuild` When one tusk process deletes and rebuilds the index database, how do other tusk servers already running on the same vault find out? | paraphrase | 20,340 → 20,405 | 3 → 2 | 67% / 100% |
| `slow-browser-tab` What happens to live updates for a browser tab that reads too slowly and falls behind? | paraphrase | 15,515 → 39,834 | 2 → 4 | 100% / 100% |
| `update-install-method` How does `tusk update` tell a `go install` binary apart from a local `make build`, given both report a -dev version? | lexical | 12,795 → 20,613 | 2 → 2 | 100% / 100% |
| `updater-any-string` What could go wrong if the self-updater accepted any string as the release to install? | paraphrase | 14,512 → 20,869 | 2 → 2 | 100% / 100% |
| `vault-pack-offline` Can `tusk init` add the built-in vault pack without network access? | lexical | 17,234 → 20,749 | 3 → 2 | 100% / 100% |
| `wipe-fails-partway` If wiping and recreating the search database fails partway through, will other running servers switch over to the half-finished copy? | paraphrase | 21,741 → 25,666 | 2 → 3 | 100% / 100% |

## Per question

| Question | Kind | Arm | Input tokens | Tool calls | Tool output | Time | Section cited | Judged correct | Failed |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `address-table-cell` | lexical | grep | 19,462 | 2 | 9.5 KB | 7.1 s | 100% | 100% | 0 |
| `address-table-cell` | lexical | tusk | 21,099 | 2 | 10.3 KB | 7.0 s | 100% | 100% | 0 |
| `alias-doctor-exit` | lexical | grep | 17,627 | 2 | 7.7 KB | 7.3 s | 33% | 100% | 0 |
| `alias-doctor-exit` | lexical | tusk | 12,247 | 1 | 8.4 KB | 4.2 s | 100% | 100% | 0 |
| `asset-route-symlink` | lexical | grep | 17,505 | 3 | 3.0 KB | 8.2 s | 100% | 100% | 0 |
| `asset-route-symlink` | lexical | tusk | 24,204 | 2 | 17.6 KB | 6.8 s | 100% | 100% | 0 |
| `clones-same-mentions` | paraphrase | grep | 15,077 | 2 | 9.4 KB | 8.5 s | 100% | 100% | 0 |
| `clones-same-mentions` | paraphrase | tusk | 22,662 | 2 | 14.0 KB | 8.4 s | 100% | 100% | 0 |
| `csp-inline-style` | lexical | grep | 12,771 | 3 | 1.6 KB | 4.9 s | 100% | 100% | 0 |
| `csp-inline-style` | lexical | tusk | 12,328 | 1 | 9.0 KB | 6.3 s | 100% | 100% | 0 |
| `filter-literal-asterisk` | paraphrase | grep | 13,447 | 3 | 4.1 KB | 6.4 s | 100% | 100% | 0 |
| `filter-literal-asterisk` | paraphrase | tusk | 11,650 | 1 | 7.5 KB | 4.8 s | 100% | 100% | 0 |
| `fresh-clone-health` | paraphrase | grep | 16,986 | 3 | 12.9 KB | 7.4 s | 100% | 100% | 0 |
| `fresh-clone-health` | paraphrase | tusk | 23,054 | 2 | 16.7 KB | 8.9 s | 100% | 100% | 0 |
| `hand-edited-state-skip` | paraphrase | grep | 25,370 | 3 | 23.6 KB | 9.6 s | 100% | 100% | 0 |
| `hand-edited-state-skip` | paraphrase | tusk | 22,401 | 1 | 11.5 KB | 8.6 s | 100% | 100% | 0 |
| `hierarchy-default-twice` | lexical | grep | 13,325 | 2 | 2.5 KB | 5.4 s | 100% | 100% | 0 |
| `hierarchy-default-twice` | lexical | tusk | 12,210 | 1 | 9.1 KB | 4.6 s | 100% | 100% | 0 |
| `identifier-not-path` | lexical | grep | 15,827 | 2 | 7.5 KB | 9.3 s | 100% | 100% | 0 |
| `identifier-not-path` | lexical | tusk | 24,201 | 2 | 18.8 KB | 9.1 s | 100% | 100% | 0 |
| `ignore-negation` | lexical | grep | 14,442 | 2 | 4.3 KB | 5.5 s | 100% | 100% | 0 |
| `ignore-negation` | lexical | tusk | 12,203 | 1 | 8.5 KB | 6.8 s | 100% | 100% | 0 |
| `incoming-links-once` | paraphrase | grep | 15,397 | 2 | 9.7 KB | 6.0 s | 100% | 100% | 0 |
| `incoming-links-once` | paraphrase | tusk | 37,667 | 3 | 19.7 KB | 9.8 s | 100% | 100% | 0 |
| `link-case-reprocess` | lexical | grep | 21,218 | 4 | 6.2 KB | 8.6 s | 100% | 100% | 0 |
| `link-case-reprocess` | lexical | tusk | 58,304 | 6 | 33.5 KB | 15.8 s | 100% | 100% | 0 |
| `lone-carriage-return-lines` | paraphrase | grep | 12,344 | 2 | 1.8 KB | 5.4 s | 100% | 100% | 0 |
| `lone-carriage-return-lines` | paraphrase | tusk | 25,933 | 2 | 16.1 KB | 8.8 s | 100% | 100% | 0 |
| `manifest-epoch-format` | lexical | grep | 14,296 | 2 | 3.6 KB | 5.7 s | 100% | 100% | 0 |
| `manifest-epoch-format` | lexical | tusk | 21,052 | 2 | 11.5 KB | 7.6 s | 100% | 100% | 0 |
| `meaning-based-positions` | paraphrase | grep | 17,230 | 2 | 8.9 KB | 6.8 s | 100% | 100% | 0 |
| `meaning-based-positions` | paraphrase | tusk | 20,641 | 2 | 10.6 KB | 8.3 s | 100% | 100% | 0 |
| `move-note-name-links` | paraphrase | grep | 45,032 | 6 | 20.7 KB | 13.8 s | 100% | 100% | 0 |
| `move-note-name-links` | paraphrase | tusk | 12,650 | 1 | 10.5 KB | 5.5 s | 100% | 100% | 0 |
| `names-path-directory` | lexical | grep | 21,674 | 2 | 13.8 KB | 9.0 s | 100% | 100% | 0 |
| `names-path-directory` | lexical | tusk | 22,363 | 3 | 11.6 KB | 9.6 s | 100% | 100% | 0 |
| `path-missing-severity` | lexical | grep | 35,883 | 4 | 10.3 KB | 14.6 s | 67% | 33% | 0 |
| `path-missing-severity` | lexical | tusk | 12,111 | 1 | 8.8 KB | 4.3 s | 100% | 100% | 0 |
| `plugin-check-ci` | lexical | grep | 7,622 | 1 | 0.6 KB | 3.8 s | 100% | 100% | 0 |
| `plugin-check-ci` | lexical | tusk | 11,533 | 1 | 7.1 KB | 4.0 s | 100% | 100% | 0 |
| `query-takes-lock` | lexical | grep | 13,654 | 3 | 5.0 KB | 4.4 s | 67% | 100% | 0 |
| `query-takes-lock` | lexical | tusk | 19,509 | 2 | 7.7 KB | 6.7 s | 100% | 100% | 0 |
| `rebuild-without-ollama` | paraphrase | grep | 41,758 | 5 | 23.3 KB | 11.8 s | 33% | 100% | 0 |
| `rebuild-without-ollama` | paraphrase | tusk | 47,681 | 4 | 24.7 KB | 12.6 s | 100% | 100% | 0 |
| `reload-parse-error` | lexical | grep | 13,782 | 3 | 4.6 KB | 6.4 s | 100% | 100% | 0 |
| `reload-parse-error` | lexical | tusk | 21,311 | 2 | 11.3 KB | 7.9 s | 100% | 100% | 0 |
| `renamed-heading-ids` | paraphrase | grep | 14,313 | 2 | 7.0 KB | 6.4 s | 100% | 100% | 0 |
| `renamed-heading-ids` | paraphrase | tusk | 12,413 | 1 | 9.9 KB | 6.5 s | 100% | 100% | 0 |
| `repeat-edit-reminder` | paraphrase | grep | 26,435 | 4 | 9.8 KB | 12.1 s | 100% | 100% | 0 |
| `repeat-edit-reminder` | paraphrase | tusk | 12,363 | 1 | 9.6 KB | 5.8 s | 100% | 100% | 0 |
| `same-paragraph-twice` | paraphrase | grep | 20,368 | 5 | 8.8 KB | 11.8 s | 100% | 100% | 0 |
| `same-paragraph-twice` | paraphrase | tusk | 21,073 | 1 | 9.5 KB | 8.0 s | 100% | 100% | 0 |
| `schema-migrate-in-place` | lexical | grep | 27,881 | 5 | 7.9 KB | 10.6 s | 100% | 100% | 0 |
| `schema-migrate-in-place` | lexical | tusk | 12,880 | 1 | 10.9 KB | 10.5 s | 100% | 100% | 0 |
| `second-condition-after-traversal` | paraphrase | grep | 18,281 | 4 | 5.6 KB | 10.4 s | 100% | 100% | 0 |
| `second-condition-after-traversal` | paraphrase | tusk | 33,033 | 3 | 22.6 KB | 8.3 s | 100% | 100% | 0 |
| `session-summary-concurrency` | paraphrase | grep | 34,203 | 6 | 9.7 KB | 11.2 s | 100% | 100% | 0 |
| `session-summary-concurrency` | paraphrase | tusk | 11,883 | 1 | 8.2 KB | 5.3 s | 100% | 100% | 0 |
| `short-claim-window` | paraphrase | grep | 13,838 | 2 | 4.9 KB | 7.5 s | 100% | 100% | 0 |
| `short-claim-window` | paraphrase | tusk | 20,933 | 2 | 10.7 KB | 8.4 s | 100% | 100% | 0 |
| `sibling-notices-rebuild` | paraphrase | grep | 20,340 | 3 | 14.0 KB | 8.3 s | 0% | 67% | 0 |
| `sibling-notices-rebuild` | paraphrase | tusk | 20,405 | 2 | 10.2 KB | 6.5 s | 100% | 100% | 0 |
| `slow-browser-tab` | paraphrase | grep | 15,515 | 2 | 6.0 KB | 6.9 s | 33% | 100% | 0 |
| `slow-browser-tab` | paraphrase | tusk | 39,834 | 4 | 23.2 KB | 11.6 s | 100% | 100% | 0 |
| `swap-wait-forever` | paraphrase | grep | 31,425 | 4 | 14.3 KB | 11.3 s | 100% | 100% | 0 |
| `swap-wait-forever` | paraphrase | tusk | 24,654 | 2 | 16.7 KB | 10.3 s | 100% | 100% | 0 |
| `unquoted-frontmatter-wikilink` | lexical | grep | 21,075 | 5 | 8.3 KB | 11.6 s | 100% | 100% | 0 |
| `unquoted-frontmatter-wikilink` | lexical | tusk | 12,655 | 1 | 10.0 KB | 4.3 s | 100% | 100% | 0 |
| `update-cosign` | lexical | grep | 14,459 | 3 | 7.2 KB | 5.4 s | 100% | 100% | 0 |
| `update-cosign` | lexical | tusk | 12,230 | 1 | 8.9 KB | 4.5 s | 100% | 100% | 0 |
| `update-install-method` | lexical | grep | 12,795 | 2 | 3.4 KB | 4.8 s | 100% | 100% | 0 |
| `update-install-method` | lexical | tusk | 20,613 | 2 | 9.8 KB | 6.9 s | 100% | 100% | 0 |
| `updater-any-string` | paraphrase | grep | 14,512 | 2 | 7.4 KB | 8.0 s | 100% | 100% | 0 |
| `updater-any-string` | paraphrase | tusk | 20,869 | 2 | 10.2 KB | 8.0 s | 100% | 100% | 0 |
| `vault-pack-offline` | lexical | grep | 17,234 | 3 | 8.4 KB | 6.9 s | 100% | 100% | 0 |
| `vault-pack-offline` | lexical | tusk | 20,749 | 2 | 9.6 KB | 7.8 s | 100% | 100% | 0 |
| `wikilink-in-code-fence` | lexical | grep | 12,853 | 2 | 2.2 KB | 7.8 s | 100% | 100% | 0 |
| `wikilink-in-code-fence` | lexical | tusk | 11,547 | 1 | 7.2 KB | 4.9 s | 100% | 100% | 0 |
| `wipe-fails-partway` | paraphrase | grep | 21,741 | 2 | 14.2 KB | 10.5 s | 100% | 100% | 0 |
| `wipe-fails-partway` | paraphrase | tusk | 25,666 | 3 | 11.3 KB | 8.3 s | 100% | 100% | 0 |

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

