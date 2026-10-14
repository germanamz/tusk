# Benchmarks

Does an agent spend less to answer a question with `tusk_query` than with grep and read? To find out, Claude Code answered 40 questions about tusk's own package docs, three times with grep and read and three times with `tusk_query` and read, and the harness counted what each run spent.

With tusk, the agent cited the right section on every run (91% with grep) and made one tool call fewer per question (2 against 3). It also read 11% more input tokens per question and cost 37% more, and took the same time. On this corpus tusk buys accuracy, not tokens. A query result runs about 8 KB, where a grep result runs under 1 KB, so tusk comes out ahead only on questions where grep has to dig.

A second, larger corpus came out the other way on tokens. On a private vault of 79 files, tusk read 17% fewer input tokens, made two tool calls fewer and finished 28% faster, and accuracy was about the same. See [A second corpus](#a-second-corpus).

## Results

| | grep and read | `tusk_query` and read | tusk against grep, per question |
| --- | --- | --- | --- |
| Input tokens | 17,108 | 20,809 | 1.11× (0.91–1.41) |
| Tool calls | 3 | 2 | −1 (−1 to 0) |
| Tool output | 7.6 KB | 10.4 KB | 1.41× (1.18–2.11) |
| Time | 7.7 s | 7.7 s | 0.99× (0.84–1.10) |
| Cost | $0.024 | $0.033 | 1.37× (1.26–1.64) |
| Cited the right section | 91% | 100% | +9 points (+2 to +17) |
| Judged correct | 98% | 100% | +2 points (0 to +7) |

The first two columns are medians over questions, taking each question's median run. The last column compares the arms question by question: the median of tusk's ratio (or difference) to grep, with a 95% confidence interval in brackets. A 1.11× ratio whose interval runs from 0.91 to 1.41 means tusk probably reads somewhat more, and the benchmark can't rule out no difference.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="../../eval/results/2026-10-14-tusk-docs/charts/tokens-tusk-vs-grep-dark.svg">
  <img alt="Input tokens per question, tusk against grep, on a log scale: tusk reads less on 16 questions, by up to 72%, and more on 24, by up to 2.75 times" src="../../eval/results/2026-10-14-tusk-docs/charts/tokens-tusk-vs-grep-light.svg">
</picture>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="../../eval/results/2026-10-14-tusk-docs/charts/per-arm-dark.svg">
  <img alt="Spread of input tokens, tool calls, tool output and time per question, by arm" src="../../eval/results/2026-10-14-tusk-docs/charts/per-arm-light.svg">
</picture>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="../../eval/results/2026-10-14-tusk-docs/charts/accuracy-dark.svg">
  <img alt="Share of runs that cited the right section and that the judge marked correct, by arm and question kind" src="../../eval/results/2026-10-14-tusk-docs/charts/accuracy-light.svg">
</picture>

Splitting by question kind doesn't change the picture. On lexical questions, which share a distinctive word with their section, tusk read 16.2k tokens to grep's 15.1k. On paraphrase questions it read 21.7k to 17.8k. Grep cited the right section on 93% of lexical runs and 88% of paraphrase runs; tusk did on all of them.

Some of grep's citation misses are near misses: a bullet name or a bold label cited as if it were a heading, or the file's title instead of the section under it. Tusk's results name the heading of each matched section, so its citations come out exact. The judge's verdict is the better measure of the answers themselves. It marked three grep runs wrong. Two of those said no flag makes a `path-missing` finding fail CI, missing `--fail-on warning`. It marked no tusk run wrong.

## Where tusk lost

Tusk read more input tokens than grep on 24 of the 40 questions. Three things account for it.

**Every query costs about 8 KB.** A `tusk_query` result has a median size of 8.4 KB. It holds five results, each with up to three matched sections, and every section carries its id, parent id, type, heading level, line range, ordinal and score around a snippet of about 150 characters. A Grep result has a median of 0.7 KB and a Read 2.9 KB. Input tokens count the whole context once per turn, so a big result early in a run is paid for again on every turn after it. On `lone-carriage-return-lines` (2.10×), both arms took one search and one read. Tusk's query found `linenum.md` right away, but returned 8 to 15 KB, where grep's search and read came to 2 KB together. `reload-parse-error`, `update-install-method` and `plugin-check-ci` go the same way, and most of the 1.2× to 1.6× losses are this.

**The agent reads the file anyway.** The snippets point at the answer but rarely contain it, so the agent follows up with Read: 73 of the 120 tusk runs read a file, against 92 of the 120 grep runs.

**A miss costs several queries.** The largest losses come from first queries that ranked other pages above the answer:

- `link-case-reprocess`, 2.75×: "link-case change forces full re-process of vault" ranked `doctor.md` and `pathref.md` above the `reindex.md` "Notes" section that holds the answer. The agent ran up to three more queries and read all of `wikilink.md` before it got there.
- `slow-browser-tab`, 2.57×: the first query's top five didn't include `webui.md` at all, and in two of the three runs the agent read `webapp.md` or `bookview.md` before finding it.
- `incoming-links-once` (2.45×) and `second-condition-after-traversal` (1.81×): the agent re-queried with different wording before reading.

Tusk won when grep had to dig. On `move-note-name-links` (0.28×), `path-missing-severity` (0.34×) and `session-summary-concurrency` (0.35×), the question's words (move, rename, exit, doctor, queries) match lines all over the corpus. Grep ran three to seven searches and read several sections, while one query landed on the section.

So the follow-ups to try are smaller query results (fewer fields per matched section, fewer sections by default), returning the matched section's text when it's short so the agent doesn't need a Read, and better ranking for long sections that cover several topics. This benchmark is how to tell whether they work.

## A second corpus

The same harness ran with the same model and settings on a private vault of 79 markdown files and 684 sections: more than twice the files and four and a half times the sections of `docs/packages`. Its 40 questions were written the same way, half lexical and half paraphrase. The vault isn't public, so only the numbers are published.

| | grep and read | `tusk_query` and read | tusk against grep, per question |
| --- | --- | --- | --- |
| Input tokens | 20,186 | 12,446 | 0.83× (0.63–0.93) |
| Tool calls | 3 | 1 | −2 (−2 to −1) |
| Tool output | 8.2 KB | 9.7 KB | 1.18× (1.06–1.65) |
| Time | 9.2 s | 6.1 s | 0.72× (0.65–0.82) |
| Cost | $0.027 | $0.028 | 1.10× (0.90–1.18) |
| Cited the right section | 95% | 97% | +2 points (−4 to +8) |
| Judged correct | 97% | 93% | −3 points (−8 to 0) |

Tusk read fewer input tokens on 28 of the 40 questions and finished faster on 33. Most of the gap is on paraphrase questions: tusk read 12.5k tokens to grep's 26.5k, and made 1 call to grep's 4. On lexical questions it read 12.4k to 16.0k. A query still returns more than a grep does. On a bigger corpus, though, grep needs more searches and reads to land, and every extra turn pays for the whole context again.

Cost came out about even anyway, because of the prompt cache. Tusk's larger results are new text, written to the cache, and a cache write costs about 12 times a cache read. Tusk wrote a median of 5.3k tokens to the cache against grep's 4.3k, and read 11.3k from it against grep's 15.5k.

The judge marked tusk wrong a little more often, 93% against 97%, but the interval reaches zero. In both arms, most wrong verdicts were right answers with an added claim the reference contradicts.

## How it was measured

The harness is [`eval/agentbench`](../../eval/README.md), and every run's raw record is in [`eval/results/2026-10-14-tusk-docs/`](../../eval/results/2026-10-14-tusk-docs/).

**Corpus.** [`docs/packages/`](../packages/): 34 markdown files, one per internal Go package, 164 KB and 149 headed sections. Each run gets a fresh copy. The grep arm's copy is plain markdown; the tusk arm's copy also has the index, built once by `tusk reindex` before any agent ran, with `nomic-embed-text` embeddings from a local Ollama.

**Questions.** [40 questions](../../eval/tasks/tusk-docs.jsonl), each with the section or sections that answer it, spread over 28 of the 34 files. Half are *lexical*: they share a distinctive word with their section, so a grep for it lands. The other half are *paraphrase*: they describe the situation in other words ("What happens when you rebuild the index while Ollama isn't running?"). The questions were written by reading the files, not by searching with either arm's tools, so neither arm picked its own questions.

**Arms.** Both run Claude Code 2.1.299 headless (`claude -p`) with Claude Sonnet 5.5, the same short system prompt (answer from the docs in this directory, cite file and heading) and the same JSON schema for the answer. They differ only in their tools:

| Arm | Tools |
| --- | --- |
| grep | Grep, Glob, Read |
| tusk | `tusk_query` (the MCP server from this repo at commit 1ab211a) and Read |

Claude Code runs with `--restricted` and `--strict-mcp-config`, so no user settings, other MCP servers or code-running tools reach either arm, and the harness checks every run's tool list against its arm. Each question ran 3 times per arm, 240 runs in all, in shuffled order, 4 at a time.

**What's recorded.** For every run: tool calls (not counting the final structured answer), input tokens, tool output in bytes, wall time and cost as Claude Code reports it. Input tokens are everything the model read across all turns, cached or not, so they measure context size independent of caching. They include Claude Code's own system prompt and tool definitions, about 3.6k tokens per turn for the grep arm and 4.1k for the tusk arm, whose MCP tool schema and server instructions add about 500.

**Scoring.** An answer cites the right section when one of its citations names a target heading in the target file, or a heading nested inside it. A judge model (Claude Haiku 5.5) then compares the answer with the text of the target section and marks it correct or not. A failed run counts as a miss on both.

**Statistics.** Each question's figure is the median of its 3 runs. Arms are compared question by question: the tusk ÷ grep ratio for each question, then the median ratio, with a 95% confidence interval from 10,000 bootstrap resamples over questions.

## Limits

- One model and two corpora, both small. On 34 files with distinctive names and headings, grep is hard to beat. On 79 files tusk already reads less. Larger vaults are untested.
- The questions were written by Claude, reading the files, and labeled lexical or paraphrase by the same hand.
- Cost depends on prompt-cache hits, which depend on how runs overlap. Input tokens don't, which is why they're the headline.

## Run it yourself

```bash
make build
go run ./eval/agentbench run --tasks eval/tasks/tusk-docs.jsonl --paths docs/packages \
  --out eval/results/$(date +%F)-tusk-docs
```

You need the `claude` CLI logged in and Ollama running with `nomic-embed-text`. [`eval/README.md`](../../eval/README.md) covers the flags, running it on your own vault, and how to write questions.
