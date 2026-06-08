---
title: tusk reload
---

## tusk reload

Hot-reload the manifest (tusk.toml) without restarting the daemon

### Synopsis

Validate and reload the manifest (tusk.toml) while any running daemon
processes converge to the new schema. Unlike "tusk reset" (which drops the
index), reload re-reads and validates tusk.toml in place, atomically swaps
the in-memory schema, and optionally triggers a reindex to re-validate
already-indexed content against the new schema.

A reload is non-interactive and non-destructive: the file watcher continues
to ignore tusk.toml (reload is explicit-only), and all running daemons
converge via the manifest-epoch sentinel (no need to restart).

By default no local reindex runs — a running daemon converges via the
manifest-epoch sentinel and owns the reindex. Pass --reindex to run a
synchronous reindex pass in this process (for the no-daemon scenario).

Validation matches boot semantics: a TOML parse/structural error or
behavior-engine build failure aborts the reload (exit non-zero, no epoch
bump); dangling aliases and invalid [context] entries are dropped and
reported as warnings while the swap still proceeds.

```
tusk reload [flags]
```

### Examples

```
  # Reload the manifest; daemon handles reindex if running
  tusk reload

  # Reload and synchronously reindex (for no-daemon scenarios)
  tusk reload --reindex
```

### Options

```
  -h, --help      help for reload
      --reindex   synchronously reindex after reloading the manifest
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk](tusk.md)	 - Local-first memory for agents: index a markdown vault into a graph

