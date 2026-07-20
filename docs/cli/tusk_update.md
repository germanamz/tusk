---
title: tusk update
---

## tusk update

Replace the running tusk binary with a published release

### Synopsis

Replace the running tusk binary with a build from GitHub Releases.

With no argument, or with "latest", the newest published release is
installed. Pass a tag to pin a specific version; a bare "2.3.0" is
normalized to "v2.3.0". Installing an older version than the one running
is allowed and reported as a downgrade.

The archive is verified against the release checksums file before anything
is extracted, and the previous binary is kept aside until the replacement
is in place, so a failed swap rolls back rather than leaving no binary.

Man pages ship in the release archive and are installed alongside the
binary, following the same layout as install.sh: a binary in <prefix>/bin
puts its man pages in <prefix>/share/man. Set MAN_DIR to override. Man-page
installation is best-effort — a read-only directory prints a note and does
not fail an update that already succeeded.

INSTALL METHOD
  A binary managed by Homebrew, installed with "go install", or built from
  source is owned by that tool, and replacing it in place would leave the
  tool's records inconsistent. Those are refused with the correct upgrade
  command for that method; pass --force to replace the binary anyway.
  "--check" predicts the refusal rather than reporting an update that the
  real run would reject.

  --force also permits reinstalling the version already running, which
  repairs a corrupted binary in place.

  Your workspace and its .tusk/ index are never touched by an update.

EXIT CODES
  0  success, including --check whatever it found
  1  generic failure
  2  network, release-resolution, or invalid-version failure
  3  checksum verification failure
  4  install-method refusal
  5  permission or swap failure

```
tusk update [version] [flags]
```

### Examples

```
  # Update to the latest release
  tusk update

  # Pin a specific version (or roll back to one)
  tusk update v2.3.0

  # Report what an update would do, without changing anything
  tusk update --check
  tusk update --check --json

  # Replace a Homebrew- or go-install-managed binary anyway
  tusk update --force
```

### Options

```
      --check   report the available version without installing anything
      --force   replace the binary even when another tool manages it, or reinstall the current version
  -h, --help    help for update
      --json    emit machine-readable JSON
```

### Options inherited from parent commands

```
  -v, --verbose   emit debug-level logs to stderr
```

### SEE ALSO

* [tusk](tusk.md)	 - Local-first memory for agents: index a markdown + HTML vault into a graph

