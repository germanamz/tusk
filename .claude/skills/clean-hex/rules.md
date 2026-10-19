# Rule catalogue

The full rule set behind SKILL.md, adapted to tusk. IDs match the source report, so a rule can be cited by ID in a plan or a review. A rule is absolute unless it is tagged *(judgment)*. You may depart from a judgment rule only by writing the reason in the change description. Departing from an absolute rule needs the maintainer's explicit waiver. Rules marked **[check]** are enforced by `check_boundaries.go`, and the rest by review.

Vocabulary: **core** is a package not listed in `boundaries.txt`. A **port** is an interface the core declares for something only the outside can do (driven), or the exported core function an adapter calls (driving). An **adapter** translates between a port and one technology. The **composition root** builds the adapters and hands them to the core.

## B: boundary and dependencies

- **B1** Core imports no technology package and no adapter package. The technology list lives in `check_boundaries.go`. Pure parsing and utility libraries are allowed: goldmark, yaml.v3, BurntSushi/toml, x/net/html, go-gitignore, x/sync. **[check]**
- **B2** Driven adapters implement interfaces the core declares. Core never names a concrete adapter type (`*index.NodeRepo`, the Ollama client). **[check: import ban]**
- **B3** Driving adapters call the core directly. Neither the core nor a driven adapter imports a driving adapter. **[check]**
- **B4** Adapters don't import other adapters. Exceptions: the web family (`webapp` → `bookview`/`graphview` → `webui`), and driving adapters reading `index` repos for listings with no business rule (see W1).
- **B5** Only composition roots construct driven adapters: `cmd/tusk` (CLI commands, `runtime_helpers.go`), `mcp.Open`/`mcp.Runtime` (the MCP server and `tusk web`; it lives inside `internal/mcp`), `internal/workspace/indexopen`, and any new package given a `role root` line (an adapter-selection factory, for example). Core receives repos, embedders, clocks and config as parameters or struct fields.
- **B6** The clock, environment, file system and randomness reach core as values or interfaces (X1, X4). **[check: clock/env/fs counts]**
- **B7** Port parameter and return types are tusk types. Never `*sql.DB`, `*sql.Rows`, `*http.Request`, `mcp.CallToolRequest` or `*cobra.Command`. `bookview` and `graphview` ports return `index.NodeRow`, which is debt, not a model to copy.
- **B8** Core never reaches through a returned value into technology (`repo.DB().Query`).
- **B9** A new adapter package gets its `role` line in `boundaries.txt` in the same change. The checker fails on roles that name missing packages.
- **B10** Existing violations are frozen as `debt` lines. Never add or raise one. Lower or delete a line when your change removes the violation (the checker prints SHRINK/CLEARED). Don't refactor debt you weren't asked to touch.

## P: ports

- **P1** Name ports for their purpose in domain language, with no technology names. Go role nouns: `Files`, `Embedder`, `NodeSource`, `RefLookup`. Never `SQLiteStore` or `OllamaClient`.
- **P2** Start with one driven port per external system (SQLite index, vault files, Ollama, GitHub releases, the clock), not one per domain noun. *(judgment)*
- **P3** One conversation can be several small interfaces: `NodeSource` and `EdgeSource` are both the index. Don't build a new adapter set per entity. *(judgment)*
- **P4** Add a driven port only when the core needs something only the outside can provide (I/O, time, randomness, a third party), or when a second implementation exists now. A test fake counts as a second implementation.
- **P5** Every port and every method on it gets a doc comment covering inputs, outputs, failure cases (named sentinel errors), side effects and nil/zero semantics, without restating the name. Models: `wikilink.Files`, `bookview.RelatedSource`.
- **P6** Every port has a test actor: a hand-written fake for a driven port (`internal/bookview/fakes_test.go`), and a test calling the exported core function for a driving port.
- **P7** Declare a driven port in the core package that consumes it. Never declare it in the adapter package.
- **P8** A driving port is an exported core function or concrete type (`query.Run`, `node.Service`, `doctor.Run`). No single-implementation interface for it (D6).
- **P9** A technology-only change never changes a port. If it seems to, stop and report.

## A: adapters

- **A1** Adapters translate. They parse flags and JSON args, map types, map errors, and handle protocol details, retries and timeouts. They make no decisions on domain values: thresholds, eligibility, severity and ranking all live in the core.
- **A2** A driving adapter parses its input, calls one core operation and maps the result and errors back to its transport. The CLI command (`cmd/tusk`), the MCP tool (`internal/mcp/tools.go`), web handlers and the watcher all reach the same core function. CLI/MCP/watch parity is the invariant; parity bugs have shipped before (#681, #688).
- **A3** Timeouts, retries, auth headers, rate limits and pagination live in the driven adapter (`embed/ollama.go` is the pattern).
- **A4** Vendor DTOs (Ollama or OpenAI JSON, GitHub release JSON) stay inside the adapter and never cross the port.
- **A5** When the foreign model already matches, keep the adapter thin. *(judgment)*
- **A6** Long flat field-by-field mapping functions are exempt from the Q5 length ceiling. *(judgment)*
- **A7** Don't deduplicate two adapters by pushing their shared shape into the core.

## Q: code quality (the parts that differ from model defaults)

STYLE.md rules 1–4 always apply and override anything below. No single-character identifiers, including loop indices.

- **Q1** One term per concept: node, file node, sub-unit, edge, workspace/vault, manifest, pack, type. Grep `docs/packages/` and PRODUCT.md and reuse the existing term. No synonyms.
- **Q4** Name numeric literals other than 0, 1 and -1.
- **Q5** For new or changed functions: at most 30 logical lines, cognitive complexity 15, nesting depth 3 and 3 positional parameters (D1). Past a ceiling, write a one-line justification comment. Four or more parameters means a `Config`/`Options` struct (`reindex.Config`, `indexopen.Config`).
- **Q6** Prefer 20 lines or fewer, but never split a function only to hit a count. *(judgment)*
- **Q7** Extract a block only if it can be named without "and" or "then", can be understood without its caller, and needs at most 3 parameters. Inline a single-use helper of 2 lines or fewer whose name adds nothing. *(judgment)*
- **Q8** No boolean flag parameters on exported core functions; use two functions or an options struct.
- **Q9** A function either changes state or returns information. Idiomatic exceptions are named for it (`Create` returning the id).
- **Q12** The package doc comment states the package's single responsibility. A new package that needs "and" between two capabilities should be two packages. *(judgment)*
- **Q13** A type either guards invariants behind methods, or is plain data (`Config`, `Report`, `Issue`). No hybrids.
- **Q14** When choosing between an interface (new variants) and a switch over a closed kind (new operations), state which kind of change you expect. *(judgment)*
- **Q16** Each domain fact has one authoritative definition. Example: id↔path is `index.NodeIDForPath`/`index.FilePathForID`, never re-derived with `+ ".md"`.
- **Q17** Don't abstract look-alike code before its third occurrence, unless it is the same knowledge (Q16). *(judgment)*
- **Q18** If a shared helper would grow a mode flag or a caller-specific branch, inline it back into its callers. *(judgment)*
- **Q19** No interface, parameter, option or hook that no current use case or test exercises.
- **Q20** Implementation comments say why. No commented-out code and no change logs. Update a comment in the same change that alters the behaviour it describes.
- **Q21** gofmt, `make vet` and `make lint` stay clean. Don't reformat lines outside the change.
- **Q22** Where local convention conflicts with these rules, follow the convention and mention the conflict. The B rules always win. *(judgment)*

## E: errors (Go)

- **E1** No in-band sentinels (-1, `""`). Use `(T, error)`, or `(T, bool)` for plain absence.
- **E2** Expected domain failures are exported sentinel or typed errors (`node.ErrAlreadyExists`, `index.ErrNodeNotFound`), wrapped with `%w`, matched with `errors.Is`/`errors.As`, and listed in the doc comment.
- **E3** A bug or broken invariant is never modelled as a domain result.
- **E4** No discarded errors (`_ = err`) in production code without a comment saying why that's correct.
- **E5** Core never inspects SQLite, HTTP or `*url.Error` details. The adapter translates first.
- **E6** Driven adapters translate failures that carry domain meaning into core errors and keep the cause (`%w`). An unexpected failure propagates wrapped.
- **E7** Driving adapters map core errors to their transport: CLI exit code and stderr, MCP `toolError`, HTTP status. Exit codes and status codes never appear in the core.
- **E8** Absence is `(nil, nil)` or `(T, false)` with the meaning written in the doc comment (`FindByTitle`). A nil dependency is allowed only where documented (`embedder == nil` disables semantic features). *(judgment)*
- **E9** When several problems must be reported together, collect them (doctor's `[]Issue`) instead of failing on the first. *(judgment)*
- **E10** Before adding an error case, check whether the operation can be defined so the error can't happen (idempotent delete, empty result). *(judgment)*

## T: tests

- **T1** Red first. Write the test for the new behaviour against the exported core function, run it, and see it fail for the right reason (a compile failure counts) before writing production code (D7, Canon TDD).
- **T2** Put a test list of behaviours in the plan, including existing behaviour that must keep working. *(judgment)*
- **T3** New pure core logic is tested with no SQLite, no network and no real clock. Packages already fused with SQLite test against a temp-dir index (`test.TempDir()`); that's accepted. Never call a real Ollama: use a stub `Embedder` or `httptest`.
- **T4** Use hand-written fakes in `_test.go` files for stateful ports, and spies only for fire-and-forget calls. No mocking framework: go.mod has none, so don't add one (D14). *(judgment)*
- **T5** Don't fake types tusk doesn't own (`*sql.DB`, `*http.Client`). Put them behind a port and fake the port.
- **T6** A driven port with more than one implementation gets one contract test: a table of constructors run through the same assertions, covering the fake, the real adapter (temp SQLite, or an `httptest` server for HTTP providers) and every new implementation. Change the contract test first when the port changes.
- **T7** Test driving adapters through their real wiring: run the cobra command (`cmd/tusk/*_test.go`), call the MCP tool through the server (`internal/mcp/tools_test.go`), or hit `httptest` for web handlers. Assert translation only: flag/arg parsing, output shape, error mapping.
- **T8** When a change adds a new wiring path, add a case to the e2e/golden suites (`cmd/tusk/e2e_*`, `golden_*`). *(judgment)*
- **T9** Test at the core function. Don't create one test file per type by default. *(judgment)*
- **T10** Test names describe behaviour: `TestDoctor_StaleNode_WarnsPastCutoff`.
- **T12** A new test that passes on its first run is suspect. Show that it can fail.
- **T13** Refactor only on green, and keep refactors in separate steps from behaviour changes.
- **T14** Test doubles are goroutine-safe (atomic or mutex-guarded counters), because CI runs `-race`.

## X: cross-cutting

- **X1** Core never calls `time.Now`, `os.Getenv` or `uuid.New`. Pass `now time.Time` into the core function, or give its config struct a `Now func() time.Time` that the composition root sets. Tests pass a fixed time. **[check]**
- **X2** Logging is a `*slog.Logger` passed in, where nil means silent (the existing convention). There is no logger interface, and tests don't assert on logs (D8). *(judgment)*
- **X3** Business observations are returned data (`doctor.Issue`, reindex `Report` counters), not log lines or metrics calls.
- **X4** Config is read only in composition roots, plus the frozen `embedconfig`/`leaseconfig` env readers, and passed to the core as typed values. The manifest is loaded by the root and passed in.

## TX / V / M: transactions, validation, mapping

- **TX1** Files on disk are the source of truth, and the index can be rebuilt from them. A use case that writes both writes the file first, then the index.
- **TX2** No index transaction spans a network call. Order steps so that a crash leaves a state `reindex` heals (embed-then-swap, #684).
- **TX3** Repo methods that a use case combines with other writes must not commit on their own; pass the transaction in. *(judgment)*
- **V1** Driving adapters reject malformed input (types, required fields, ranges) before calling the core: `argval` for MCP args, cobra flag parsing for the CLI. MCP numeric args are strict (#688).
- **V2** Core `Config`/`Options` structs may validate themselves. *(judgment)*
- **V3** Rules that need current state (does the id exist, is the edge type declared) live in the core.
- **V4** The same check at the edge (for a good message) and in the core (as an invariant) is not a DRY violation.
- **M1** External contracts (MCP JSON, CLI `--json`) use dedicated wire types, not `index` row types, because agents depend on their shape (`query/semantic_json.go`).
- **M3** Mapping happens in the adapter. Don't add a mapping layer as part of an unrelated change (D3).
- **M5** Core data types don't call repos to complete themselves. The use case loads the data and passes it in.

Events: tusk has no integration events or broker. If one appears, EV rules apply: event types are plain core data, publishing goes through an outbox written in the same transaction (D12), and consumers are idempotent driving adapters.

## W: when not to apply the full pattern

- **W1** Complexity gate: a listing or filter with no business rule (`edge list --type`) may stay a thin driving adapter over `index` repos. Anything with a decision goes into a core function shared by every entry point. State which case you're in. *(judgment)*
- **W2** No single-implementation interface unless it is a driven port (P4).
- **W3** No layer whose methods only delegate.
- **W4** `eval/`, scripts and prototypes skip the hexagon. Extract a port when the second implementation or the first fake appears. *(judgment)*
- **W5** No `core/`, `ports/` or `adapters/` directory trees, inside packages or at the top level.
- **W6** Go idiom: declare interfaces where they're consumed, keep them small, return concrete types from constructors. *(judgment)*
- **W8** Layout is one package per capability under `internal/`. A new technology gets its own adapter package (for example `internal/embed/openai`), never more files in a package that already carries that technology as debt (D10). *(judgment)*

## G: agent process

- **G1** Read a port, adapter or core function before using or changing it. Don't guess APIs.
- **G2** Before editing, run the placement procedure and name the packages you'll touch.
- **G3** Keep the diff inside the packages you named. Stop and report before going further.
- **G4** Cleanup only in files the change already touches. It must preserve behaviour, be covered by tests, and stay around 10% of the diff or go in a separate commit (D16).
- **G5** Never delete, skip, weaken or re-baseline a test or assertion to get a pass. Adding tests is fine. Changing an existing test's expectation means stopping and reporting with evidence (D15). A golden-file update is fine only when the behaviour change was requested; say so.
- **G6** No special-casing test inputs or the test environment.
- **G7** Stop and report when any of these happen:
  - a technology change seems to need a core or port change;
  - a test conflicts with the request;
  - the checker fails on debt outside your scope;
  - a fix needs a weaker test;
  - a new driven port has no external actor;
  - it's unclear which package owns a rule;
  - a dependency can't be verified to exist.
- **G8** Add no dependency without a stated reason and a verified module version.
- **G9** No `--no-verify`, no lint suppression, and no new or raised debt line to finish.
- **G10** Don't add comments or docstrings to code you didn't change, and don't add flexibility the task doesn't need.
- **G11** Finish with the commands you ran and their pass/fail output.
- **G12** When a reviewer subagent checks your diff, act on correctness and rule violations, not on every stylistic suggestion. *(judgment)*

## Decisions (D1–D18), as settled for tusk

| # | Decision | Setting |
|---|---|---|
| D1 | Thresholds | 30 lines, cognitive 15, nesting 3, 3 params. Review-enforced (no linter yet); new or changed code only. |
| D2 | Persistence types in domain | Domain stays pure; row types live in `index`. |
| D3 | Mapping | The adapter maps; no global mapping layer. |
| D4 | Errors | `(T, error)`, sentinel or typed errors, `%w`, `errors.Is`/`errors.As`. |
| D5 | Port naming | Role nouns (`Files`, `Embedder`, `NodeSource`); `For…` allowed. |
| D6 | Driving port | Exported concrete function or type, no single-implementation interface. |
| D7 | TDD | Canon: test list, red before green, refactor on green. |
| D8 | Logging | `*slog.Logger` passed in, nil means silent; no logger port. |
| D9 | Core internals | None prescribed. |
| D10 | Layout | One package per capability; one package per new technology adapter. |
| D11 | Multi-write transactions | Files first, then index; never span a network call. |
| D12 | Events | n/a today; outbox if integration events appear. |
| D13 | Comments | Doc comments mandatory on ports and on the exported core API. |
| D14 | Doubles | Hand-written fakes for stateful ports, spies for fire-and-forget, no mock framework. |
| D15 | Existing tests | Add freely; changing an existing expectation means stop and report. |
| D16 | Cleanup budget | Touched files only, around 10% of the diff, or a separate commit. |
| D17 | Complexity gate | The agent runs it and states the result in the plan. |
| D18 | Emphasis | Plain imperatives; MUST only on the hard invariants. |
