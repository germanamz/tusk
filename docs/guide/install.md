# Installing and updating tusk

## Prerequisites

- (Optional, for semantic search) [Ollama](https://ollama.com) running locally with an embedding model, e.g. `ollama pull nomic-embed-text`. Everything except semantic and hybrid queries works without it.

## Homebrew (macOS and Linux)

```bash
brew install germanamz/tap/tusk
```

Installs the prebuilt binary along with its man pages and bash, zsh and fish completions. The release binaries are cosign-signed but not notarized by Apple, so on macOS the cask clears the quarantine flag during install to keep Gatekeeper from blocking it.

## One-liner (prebuilt binary)

```bash
curl -fsSL https://raw.githubusercontent.com/germanamz/tusk/main/install.sh | sh
```

Detects your OS/arch, downloads the latest GitHub release, drops the `tusk` binary into `~/.local/bin` (override with `INSTALL_DIR=/usr/local/bin`), and installs its man pages into `~/.local/share/man` (override with `MAN_DIR`; `man tusk` works once that dir is on your `MANPATH`). Pin a specific release with `TUSK_VERSION=v1.1.0`. Prebuilt archives ship for darwin/linux/windows on amd64 + arm64.

## From source

Requires Go 1.26+.

```bash
git clone https://github.com/germanamz/tusk
cd tusk
make build
# binary at ./bin/tusk — move it onto your PATH
install bin/tusk /usr/local/bin/tusk
```

Or, without cloning:

```bash
go install github.com/germanamz/tusk/cmd/tusk@latest
```

Verify:

```bash
tusk --version
```

## Updating

If you installed a prebuilt binary, `tusk update` replaces it in place. Your workspace and `.tusk/` index are untouched.

```bash
tusk update              # latest release
tusk update v1.2.0       # pin a version (or roll back to one)
tusk update --check      # report what's available, change nothing
```

The archive is verified against the release checksums before anything is extracted, and the previous binary is kept aside until the replacement is in place, so a failed swap rolls back rather than leaving you with no binary. Man pages are refreshed alongside it.

A binary managed by Homebrew, installed with `go install`, or built from source is owned by that tool, so `tusk update` refuses it and prints the right command instead (`--force` overrides):

```bash
# Homebrew
brew upgrade tusk

# From source
cd tusk && git pull && make build && install bin/tusk /usr/local/bin/tusk

# go install
go install github.com/germanamz/tusk/cmd/tusk@latest
```

Re-running the install one-liner also still works, and remains the way to bootstrap a machine that has no `tusk` yet:

```bash
curl -fsSL https://raw.githubusercontent.com/germanamz/tusk/main/install.sh | sh
curl -fsSL https://raw.githubusercontent.com/germanamz/tusk/main/install.sh | TUSK_VERSION=v1.2.0 sh
```

After a major upgrade, run `tusk reindex` to pick up any indexer changes, then `tusk doctor` to confirm the workspace is healthy.
