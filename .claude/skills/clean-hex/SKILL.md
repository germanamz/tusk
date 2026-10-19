---
name: clean-hex
description: Use when adding or changing Go code in tusk (cmd/tusk, internal/*), such as a CLI flag or command, an MCP tool or argument, an alias verb, a web endpoint, a doctor check, a new embedding, storage or network provider, or code that needs the clock, env vars, files, SQLite or HTTP, and when deciding which package a change belongs in.
---

# Hexagonal architecture in tusk

Business rules live in **core** packages that touch no technology. **Adapters** translate between the outside world and the core. **Composition roots** build the adapters and hand them to the core. Tusk is only partly there. Existing violations are frozen as debt in `boundaries.txt`. New code adds none, and `check_boundaries.go` enforces that.

## The map

| Role | Packages | Job |
|---|---|---|
| Composition roots | `cmd/tusk` (CLI commands, `runtime_helpers.go`), `mcp.Open`/`mcp.Runtime` (the MCP server and `tusk web`; it lives in `internal/mcp`), `internal/workspace/indexopen` | Build repos, embedders and HTTP clients. Read env vars and the manifest. Set the clock. Pass all of it in. |
| Driving adapters | `cmd/tusk` (cobra), `internal/mcp` (`tools.go`), `internal/aliasdispatch` (`tusk run`/`tusk_run`), `webapp`/`webui`/`bookview`/`graphview` (HTTP), `watcher` (fsnotify) | Parse and validate input, call **one** core entry point, and map the result and errors to their transport. |
| Driving ports | The verb entry points: `query.Run`, `query.ListRun`, `node.GetRun`, `node.Service`, `doctor.Run`/`RunWithMigration`, `status.Run`, `reindex.Run`, `index.EdgeListRun`* | A typed `Request`/`Config` in, a result out. |
| Driven adapters | `internal/index` (SQLite), `lock`, `selfupdate`, `claudeplugin`, `workspace`; `embed/ollama.go`* | Talk to one technology behind an interface the core declares. |
| Core | every other package | Rules, checks, ranking, parsing. |

\* Debt. `index.EdgeListRun` is a verb inside the SQLite package; extending it is fine for a pure listing (Step 0), but a business rule there belongs in core. `ollama.go` shares a package with its port. Neither is a pattern to copy.

Pure libraries are allowed in core: goldmark, yaml.v3, BurntSushi/toml, x/net/html, go-gitignore, x/sync.

## Hard invariants (checked)

Run from the repo root: `go run .claude/skills/clean-hex/check_boundaries.go`

1. Core MUST NOT import technology (`database/sql`, `net/http`, `os/exec`, cobra, mcp-go, fsnotify, flock, uuid, rand) or an adapter package.
2. Core MUST NOT call `time.Now`/`Since`/`Until`, `os.Getenv`/`LookupEnv`, or `os`/`filepath` file-system functions. That includes a zero-value fallback such as `if now.IsZero() { now = time.Now() }`.
3. A new technology MUST get its own adapter package plus a `role` line in `boundaries.txt`. Never add it as another file in a package that already carries that technology as debt.
4. A debt line in `boundaries.txt` MUST only go down. Lower or delete it when your change removes a violation. Adding or raising one needs the maintainer's explicit approval in the conversation.

## Placement: run this before editing, and write the step number in your plan

0. **No business rule** (a listing, a column filter, a display cap): extend the existing verb entry point and its adapters. No new types, interfaces or packages.
1. **A rule, check, ranking or calculation changes:** it goes in the core package that owns the capability (a doctor check goes in `internal/doctor`). Every input arrives as a `Config`/`Request` field, including `Now`, durations and settings.
2. **The core needs something from outside** (file data, mtime, the clock, the network, an env var): first check whether the index already holds it (`NodeRow.LastMtime`, `properties_json`). If not, declare a small interface in the consuming core package with a doc comment covering inputs, outputs, errors and nil semantics. Implement it in an adapter, wire it in a composition root, and add a fake in `_test.go`.
3. **A new entry point or argument:** every driving adapter parses and type-checks its own input, then calls the same core function. That means the cobra flag, the MCP arg (`argIntOptionalStrict` and `argval`, which are strict), and the alias dispatcher, because a new cobra flag automatically becomes a valid alias arg that `aliasdispatch` must read. A check every transport shares, such as a range or a grammar, has one definition: either in the core entry point or in a pure parser the adapters call, never copied into each adapter. After help-text changes, run `make docs`.
4. **A new or swapped technology** (an embedding provider, a storage engine, an external API):
   - Put it in a new adapter package (`internal/embed/openai`) that implements the existing port, which stays unchanged.
   - Do adapter selection and secrets (env vars) in a composition-root package. If the selection switch lives in a core package today (`embed.NewFromManifest`), move it into a new root package with a `role root` line. Every caller is already a composition root, so updating them is in scope.
   - Write one contract test, a table of constructors run through the same assertions: the fake, the existing adapter and the new one, all against `httptest` (no real servers).
5. **A new library:** a pure computation library is fine after you verify the module exists. Anything that does I/O or is a framework goes behind a port.
6. **Cross-cutting concerns:**
   - Clock and env: set by the caller (invariant 2).
   - Logging: a `*slog.Logger` parameter, where nil means silent.
   - Retries and timeouts: in the driven adapter.
   - Observations: returned data (`Issue`, `Report`), not log lines.
7. **Wiring only:** `cmd/tusk` or `mcp.Open`.

If a change fits none of these, or two of them in conflict, state the ambiguity and the option you chose. Stop and report instead if the choice would add a port or cross a boundary.

## The clock, done right

```go
// core: internal/doctor
type Config struct {
	StaleAfter time.Duration
	Now        time.Time // required when StaleAfter > 0
}

func checkStale(config Config) ([]Issue, error) {
	if config.StaleAfter > 0 && config.Now.IsZero() {
		return nil, errors.New("doctor: StaleAfter needs Now") // caller bug: fail fast
	}

	cutoff := config.Now.Add(-config.StaleAfter)
	// ...
}

// driving adapters (cmd/tusk, internal/mcp, internal/aliasdispatch)
cfg := doctor.Config{StaleAfter: staleAfter, Now: time.Now()}
```

Tests pass a fixed `Now`. The same shape works for env vars and secrets: the root reads them, and the core receives a typed value.

## Stop and report, don't work around

- A technology change seems to need a port or core change.
- The checker fails on debt outside your change.
- Passing the check would mean adding or raising a debt line.
- A fix needs a test deleted, skipped, loosened or re-expected.
- A new driven port has no external system behind it.
- It's unclear which package owns a rule.

## Definition of done (copy into your final message, with evidence)

- [ ] Placement step stated, and the diff touches only the packages it named.
- [ ] Red first: each new behaviour's test was run and seen failing for the right reason before the fix (quote the failure). It passes now.
- [ ] `go run .claude/skills/clean-hex/check_boundaries.go` prints `ok`, and any SHRINK/CLEARED lines are applied.
- [ ] `make test`, `make vet` and `make lint` are clean, and `go test -race` passes on the touched packages.
- [ ] No existing test or assertion was deleted, skipped or changed to match the output.
- [ ] Each new port has a doc comment and a fake. Each new adapter has a `role` line and a contract test.
- [ ] `make docs` was run if help text changed.

## Rationalizations seen without this skill

| Excuse | Reality |
|---|---|
| "It sits next to `ollama.go`" / "matches how `embedconfig` reads its env var" | That's frozen debt, not a pattern. Copying it adds debt, and the checker fails it. |
| "Passing the key in would touch 9 call sites" | Those call sites are composition roots, and wiring is their job. Touch them. |
| "Zero value falls back to `time.Now()`" | That's a clock call in core. The caller sets it, and the core rejects zero. |
| "One switch in the core factory covers every path" | Choosing an adapter is wiring, so it belongs in a root package. |
| "I'll bump the debt count, it's just a skill script" | Debt only goes down. Stop and report. |

The full rule catalogue, with IDs for citing in plans and reviews, plus the D1–D18 settings, is in `rules.md` in this directory. Read it when a placement question isn't answered here.
