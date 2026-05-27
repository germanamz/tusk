# Task 3.2 — TTL configuration (env + manifest)

**Phase:** 3
**Spec:** `docs/superpowers/specs/2026-05-26-tusk-concurrent-instances.md`
**Plan:** `docs/superpowers/plans/2026-05-26-concurrent-instances/PLAN.md`
**Prereqs:** T3.1 landed.
**Ships as:** one PR.

## Execution Rules

1. **This task = one PR.**
2. **Build green, full Go test suite green, lint clean.**
3. **Removes bridge code:** the hardcoded `60 * time.Second` TTL in
   `internal/embed/drain.go` introduced by T3.1. This task replaces it
   with the resolved configuration value. The bridge comment goes with
   it.
4. **No schema change.**

## Goal

Make the lease TTL configurable so operators can extend it for slow
embedders or long file mutations. Resolution order (highest precedence
first):

1. `TUSK_LEASE_TTL_SECONDS` environment variable.
2. `lease.ttl_seconds` in manifest TOML.
3. Default: `60` seconds.

The same value is used everywhere a lease is taken in the codebase
(today: `EmbedQueueRepo.Drain`; later: the file_state `Claim` path in
Phase 4). One config, one value.

## Scope

### Files to modify

- `internal/manifest/` — extend the manifest schema to recognize a
  `[lease]` section with `ttl_seconds` (integer, optional). Default to
  60 if absent. Follow the same pattern used for any other manifest
  section currently in `internal/manifest/`. Add validation: if present,
  must be positive (`> 0`); 0 or negative is a manifest error.

- New file `internal/leaseconfig/leaseconfig.go` (or extend an existing
  config package if one exists — check `internal/` first; the
  implementer chooses) — a tiny resolver:
  - `Resolve(manifestTTL int) time.Duration` reads
    `TUSK_LEASE_TTL_SECONDS` from the environment, parses, and returns
    its value if valid; otherwise falls back to `manifestTTL`; otherwise
    `60 * time.Second`.
  - If the env value is set but unparseable, log a warning and fall
    back; do not error out (operators should not have a process refuse
    to start because of a malformed env var).

- `internal/embed/drain.go` — replace the hardcoded `60 * time.Second`
  TTL from T3.1's bridge with the resolved value. The drain's caller
  (somewhere in `internal/mcp/runtime.go` or `cmd/tusk/cmd_watch.go`)
  passes the manifest's value into the drain config; the drain itself
  asks `leaseconfig.Resolve` for the final value. Remove the bridge
  comment.

- `internal/mcp/runtime.go` — when constructing the embed drain config,
  read the manifest's `lease.ttl_seconds` and pass it through.

- Manifest documentation: wherever manifest keys are documented (likely
  `docs/packages/manifest.md` or similar — check `docs/packages/`),
  document the new `[lease] ttl_seconds` key with the default and the
  env override.

### Tests

- Unit tests for `leaseconfig.Resolve`:
  - Env var unset, manifest 0 → 60s default.
  - Env var unset, manifest 120 → 120s.
  - Env var "30", manifest 120 → 30s.
  - Env var "garbage" → warning, falls back to manifest then default.
  - Env var "0" or negative → warning, falls back.
- Manifest parser test for the new `[lease]` section: valid TTL, missing
  section, invalid TTL.
- Integration test in `internal/embed/` confirming the drain uses the
  resolved TTL when claiming a lease (set TTL via env in the test,
  assert the `leased_until_ns` value falls within expected bounds).

## Verification

1. `make build`, `make test`, `make vet`, `make lint`, `make fmt` all
   green.
2. `make docs` — manifest docs regenerated if applicable.
3. Manual smoke: set `TUSK_LEASE_TTL_SECONDS=5`, run the MCP server,
   confirm a `Drain` call writes `leased_until_ns ≈ now + 5s`.

## Out of Scope

- A separate TTL for `file_state` leases vs `embed_queue` leases. One
  TTL value applies to both. If we discover during Phase 4 that file
  mutations need a much shorter TTL than embedding, we'll add a second
  key — not in this task.
- Hot-reload of the manifest TTL value. The value is read at process
  start; changing it requires a restart. Document this in the manifest
  docs.
- Lease renewal / heartbeat. The design explicitly rejects heartbeats
  (spec § *Lease TTL*).

## Notes for the Implementer

- The env var is `TUSK_LEASE_TTL_SECONDS`, parsed as an integer number
  of seconds. Use `strconv.Atoi`. Use `time.Duration` only after
  parsing.
- The default of 60s is in the spec § *Lease TTL*. Do not change it as
  part of this task.
- Beware of test isolation: env vars set in one test can leak to another
  unless you use `t.Setenv`. Prefer `t.Setenv` exclusively.
- Document the hazard of setting TTL very low (e.g., 1s): an
  in-progress embed of a large node may have its lease expire while
  still working, causing a second worker to redo the same work. The
  hazard belongs in the manifest docs and the env var docs.
