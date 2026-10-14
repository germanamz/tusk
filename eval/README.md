# eval

`agentbench` measures what it costs an agent to answer questions about a folder of markdown with tusk and without it. Every question runs through Claude Code in headless mode (`claude -p`) once per arm and repetition. The harness records tool calls, input tokens, tool output, time and cost, then scores whether the answer cites the section that holds it and whether a judge model finds the answer correct. The published results are in [docs/guide/benchmarks.md](../docs/guide/benchmarks.md).

## What you need

- The `claude` CLI, logged in. Runs use your normal Claude Code login; no API key is needed.
- Ollama running with the vault's embedding model (`nomic-embed-text` for this repo). The harness indexes a copy of the corpus before any agent runs and stops if a semantic query comes back empty.
- A tusk binary to test. The default is `bin/tusk`, so run `make build` first.

## Run it on tusk's own docs

```bash
make build
go run ./eval/agentbench check --tasks eval/tasks/tusk-docs.jsonl --paths docs/packages
go run ./eval/agentbench run   --tasks eval/tasks/tusk-docs.jsonl --paths docs/packages \
  --out eval/results/$(date +%F)-tusk-docs
```

`check` validates the question set and needs nothing running. `run` defaults to the `grep` and `tusk` arms, 3 repetitions, Sonnet as the agent, Haiku as the judge and 4 runs at a time. That's 240 agent runs for the 40 questions here, about 15 minutes. It writes:

| File | What it holds |
| --- | --- |
| `runs.jsonl` | one record per run: every metric, the answer, its citations and the judge's verdict |
| `meta.json` | models, versions, corpus size, the task file's hash |
| `summary.md` | the report, with the charts at the end |
| `charts/` | SVG charts, each in a light and a dark version: input tokens per question for each arm against `grep`, the spread of every cost metric by arm, and accuracy by question kind |
| `streams/` | raw `stream-json` transcripts, one per run (gitignored) |

An interrupted run resumes when you rerun the same command: runs already in `runs.jsonl` are skipped and failed ones are retried. `go run ./eval/agentbench report --out DIR` rebuilds `summary.md` and `charts/` from `runs.jsonl` without running anything.

Useful flags: `--arms grep,tusk,both` adds the arm that has both toolsets; `--only id1,id2` runs a few questions as a smoke test; `--model`, `--judge-model` (`""` skips judging), `--reps`, `--concurrency`, `--tusk`, `--max-budget` (per run, in USD).

## Run it on a private vault

Point `--vault` at the vault root (the folder with `tusk.toml`) and `--paths` at the folders to search. Keep the task file and `--out` outside this repo, and share numbers only:

```bash
go run ./eval/agentbench run --vault ~/notes --paths docs \
  --tasks ~/bench/notes-tasks.jsonl --out ~/bench/notes-$(date +%F)
go run ./eval/agentbench report --out ~/bench/notes-$(date +%F) --numbers-only
```

`--numbers-only` prints the aggregates without question text, task ids or file names. It draws no charts and writes nothing to disk.

## How a run is set up

Each run gets a fresh scratch copy of the corpus. The `grep` arm's copy has the markdown only, with no `tusk.toml` and no `.tusk/`. Both arms get the same model, the same short system prompt (answer from the docs in this directory and cite file and heading) and the same JSON schema for the answer. The arms differ only in their tools:

| Arm | Tools |
| --- | --- |
| `grep` | Grep, Glob, Read |
| `tusk` | `tusk_query` and Read |
| `both` | Grep, Glob, Read and `tusk_query` |

Claude Code runs with `--restricted` (no user, project or local settings, no code-running tools, file tools confined to the run directory) and `--strict-mcp-config`, so nothing from your own setup reaches the agent. The tusk arm's server is the binary under test, started with `TUSK_EMBED_WORKERS=0`: the copied index is already complete, so there's nothing for background workers to do. Every other tusk tool is disallowed by name, from the list `tusk mcp` reports. The harness checks each run's tool list against its arm and stops the benchmark on a mismatch.

## Writing questions

A task file is JSONL, one question per line:

```json
{"id":"rebuild-without-ollama","question":"What happens when you rebuild the index while Ollama isn't running?","kind":"paraphrase","targets":[{"file":"docs/packages/reindex.md","heading":"Notes"}]}
```

- `targets` are the sections that answer the question: a file relative to the vault root and a heading as written in it. A citation counts as a section hit when it names a target heading or one nested inside it. List every section that fully answers the question.
- `kind` is `lexical` when the question shares a distinctive word with its section, so grep can find it, and `paraphrase` when it doesn't. The report breaks results down by kind; aim for about half of each.
- Write questions by reading the files, not by searching with either arm's tools. A set picked with grep favors grep, and a set picked with tusk favors tusk.

Run `check` after every edit. It fails on a target that doesn't resolve and prints how the questions spread over kinds and files.
