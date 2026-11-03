---
type: package
title: internal/checkexec — project check runner
import-path: github.com/germanamz/tusk/internal/checkexec
status: stable
---

# internal/checkexec

Runs `[check.<name>]` project check commands for `tusk doctor --checks` (#852). It is the driven adapter behind `doctor.CheckRunner`, and the only package that starts a process for doctor. Only the CLI wires it, and only under `--checks`: the command is shell text from `tusk.toml`, so MCP `tusk_doctor` and doctor aliases never get a runner.

## Public surface

- `Shell` — implements `doctor.CheckRunner`. `Run(command, dir, timeout)` runs `sh -c <command>` (`cmd /C` on Windows) with `dir` as the working directory and the caller's environment, and returns the exit code and the interleaved stdout and stderr. A command that can't start (a missing directory, no shell) is an error; one that starts and fails is a result.
- `MaxOutputBytes` (4 MiB) and `TruncatedNote` — the output cap, and the line that ends output cut at it, so a command that prints without end can't exhaust memory before its timeout.

## Notes

- **Timeouts stop the whole check.** On Unix the command starts in its own process group, and the timeout kills the group, so a check's children stop with it. `WaitDelay` (2s) bounds the wait for the output pipe to close, so a child that outlives the shell and keeps the pipe open can't hold doctor. On Windows the default cancel kills the shell and `WaitDelay` covers its children.
- `shell_test.go` is the contract test: exit code and output, working directory, timeout with a background child, a start failure, and the output cap, all against the real shell (skipped on Windows, where the commands aren't POSIX).
