---
type: package
title: internal/selfupdate — binary self-update
import-path: github.com/germanamz/tusk/internal/selfupdate
status: stable
---

# internal/selfupdate

Backs `tusk update`. Resolves a version from GitHub Releases, verifies the
downloaded archive against the release checksums, and swaps the running binary
in place with rollback on failure. Cobra-free so the whole pipeline is testable
against an `httptest` server; `cmd/tusk/cmd_update.go` is a thin adapter that
maps flags in and sentinel errors out to exit codes.

## Public surface

- `Updater{APIBase, ExecPath, CurrentVersion, GOOS, GOARCH, Force, SkipManPages}` —
  the zero value targets the real GitHub API, `os.Executable()`, and the
  ldflag-injected `version.Current`. Tests override the fields.
- `Updater.Plan(ctx, requested) (Plan, error)` — resolves a version and works
  out what applying it would do. Touches nothing. Backs `--check`.
- `Updater.Apply(ctx, plan) (Result, error)` — downloads, verifies, installs.
  A no-op when the target version is already installed.
- `Updater.Resolve(ctx, version) (Release, error)` — `latest` hits
  `/releases/latest`; anything else is a tag lookup.
- `NormalizeVersion`, `ValidateTag`, `IsDevVersion`, `CompareVersions`,
  `Direction` — version handling. `ArchiveName` / `HostArchiveName` mirror
  goreleaser's name template.
- `DetectMethod(execPath, currentVersion) Method`, `Method.UpgradeCommand()`,
  `MethodRefusal(...)` — install-method detection and its refusal error.
- `ManDirFor(targetPath)` — man-page destination, mirroring `install.sh`.
- Sentinels: `ErrNetwork`, `ErrChecksum`, `ErrInstallMethod`, `ErrPermission`,
  `ErrNoAsset`, `ErrInvalidVersion`. Matched with `errors.Is`, mapped to exit
  codes 2–5.

## Version strings are a security boundary

`ValidateTag` is not a convenience check. A version flows into the GitHub API
URL path *and* into the downloaded archive's filename, so an unvalidated value
is both a URL-injection and a path-traversal primitive:

- `tusk update 'v/../../attacker/evil/releases/latest'` would resolve against
  another repository entirely. Because that release supplies both the archive
  and its own `checksums.txt`, verification stays self-consistent and passes —
  the whole integrity chain gets repointed.
- A release reporting `tag_name: "v1.0.0/../../../home/user/.bashrc"` would
  steer the download write outside the temp directory, *before* the checksum is
  verified.

Both the user-supplied version and the server-reported tag are therefore
validated against a strict tag pattern, and the surviving value is
`url.PathEscape`d. `downloadArchive` additionally applies `filepath.Base`, so a
regression in either guard still cannot escape the working directory.

Redirects must stay on HTTPS. Release downloads legitimately redirect to a CDN
host, but a plaintext hop would let a network attacker serve the archive *and*
the checksums that "verify" it.

## Install-method detection

Only release builds carry an ldflag-injected version, so both `go install` and
a local `make build` report the `-dev` fallback. Detection therefore cannot key
off the version string alone — doing so classifies every `go install` binary as
a source build and sends the user to a git checkout they may not have.

The discriminator is the VCS stamp in the module build info, not the module
version: building a tagged repository locally yields a *pseudo-version* such as
`v1.18.1-0.20260718191123-fbe70f4e7989+dirty`, not `(devel)`, so version shape
proves nothing. A local build is stamped with `vcs.revision`; `go install`
builds from the module cache and carries no VCS settings at all.

Path signals are evaluated before the version fallback, so a Homebrew Cellar
binary is classified as Homebrew even when a formula built it from source.

## Ordering invariants

`Apply` is ordered so that the cheap refusals happen first and the running
binary is touched last:

1. Install-method refusal and the writability probe run **before** any network
   transfer, so a refused or unwritable update costs no bandwidth.
2. The SHA-256 is verified **before** extraction — an archive that fails
   verification is never unpacked.
3. Extraction completes **before** the binary is touched at all.

## The swap

The replacement is staged in the target's own directory (same filesystem, so
`os.Rename` is atomic), inheriting the current binary's permission bits with
owner-execute forced on — a deliberately restricted install must not be
silently widened to 0755 by updating it. Then:

```
rename target → target.old.<pid>   backup
rename staged → target             the swap
  on failure  → restore the backup, abort
  on success  → remove the backup (best-effort)
```

Move-aside-then-rename is used on every platform rather than branching per OS.
On Unix a plain rename over a running binary would work — the process holds the
inode — but the uniform path gives real rollback everywhere. On Windows it is
mandatory: a running `.exe` cannot be overwritten, only renamed aside.

The backup name carries the pid rather than being a fixed `.old`. Two
concurrent updates sharing one backup path would clobber each other's rollback
copy, and on Windows a backup still locked by a running process would
permanently occupy the only slot and block every future update.

`sweepLeftovers` clears abandoned backups and staging files at the start of
each swap. Every removal is advisory, and staging files are age-gated by
`stagingGrace` so a concurrent update's in-flight file is never swept.

## Notes

Verification stops at SHA-256 against `checksums.txt`. Releases also carry a
keyless cosign signature and SLSA provenance, but verifying those in-process
would pull the sigstore dependency tree into a near-stdlib CLI; they remain a
documented manual step (see `.goreleaser.yaml`). Note that this is still
strictly more verification than `install.sh` performs, which does none.

Man-page installation is best-effort by design: the binary swap has already
succeeded by the time it runs, so a read-only man directory produces a note on
`Result.ManPagesNote` rather than a failed update.

Deliberately absent from MCP and from `cliregistry`. Every graph read/write verb
has an MCP twin, but letting an agent replace the binary it is executing under
is not a capability worth exposing; the exemption is recorded in
`TestRegistry_NoOrphanCobraCommands`.
