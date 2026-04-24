FROM mcr.microsoft.com/devcontainers/base:ubuntu-24.04

ARG GO_VERSION=1.26.1

RUN apt-get update && apt-get install -y \
        curl \
        git \
        build-essential \
        ca-certificates \
        fd-find \
        ripgrep \
        tinyproxy \
        iptables \
        sudo \
        gnupg \
        unzip \
        libtree-sitter-dev \
    && ln -sf "$(command -v fdfind)" /usr/local/bin/fd \
    && rm -rf /var/lib/apt/lists/*

# GitHub CLI from the official repo. Needed so claude can open PRs without
# SSH (port 22 isn't reachable through tinyproxy).
RUN install -m 0755 -d /etc/apt/keyrings \
    && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
        -o /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
        > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update \
    && apt-get install -y gh \
    && rm -rf /var/lib/apt/lists/*

# Rewrite SSH GitHub URLs to HTTPS so existing `origin` remotes work through
# the egress proxy, and wire `gh` in as git's credential helper so `git push`
# picks up the token after `gh auth login`.
RUN git config --system url."https://github.com/".insteadOf "git@github.com:" \
    && git config --system --add url."https://github.com/".insteadOf "ssh://git@github.com/" \
    && git config --system credential."https://github.com".helper "!gh auth git-credential" \
    && git config --system credential."https://gist.github.com".helper "!gh auth git-credential"

# Neovim (upstream stable, multi-arch). The apt package on Ubuntu 24.04 lags
# behind what most modern configs (lazy.nvim, treesitter, etc.) require.
RUN ARCH=$(dpkg --print-architecture) \
    && case "$ARCH" in \
         amd64) NVIM_ARCH=x86_64 ;; \
         arm64) NVIM_ARCH=arm64 ;; \
         *) echo "unsupported arch for nvim: $ARCH" >&2; exit 1 ;; \
       esac \
    && curl -fsSL "https://github.com/neovim/neovim/releases/latest/download/nvim-linux-${NVIM_ARCH}.tar.gz" \
        | tar -C /opt -xz \
    && ln -sf "/opt/nvim-linux-${NVIM_ARCH}/bin/nvim" /usr/local/bin/nvim

# Zellij - terminal multiplexer, installed system-wide so both vscode (agent)
# and dev (admin) users pick it up via PATH. Config lives at /etc/zellij/ and
# is advertised via ENV ZELLIJ_CONFIG_DIR below.
RUN ARCH=$(dpkg --print-architecture) \
    && case "$ARCH" in \
         amd64) ZJ_ARCH=x86_64 ;; \
         arm64) ZJ_ARCH=aarch64 ;; \
         *) echo "unsupported arch for zellij: $ARCH" >&2; exit 1 ;; \
       esac \
    && curl -fsSL "https://github.com/zellij-org/zellij/releases/latest/download/zellij-${ZJ_ARCH}-unknown-linux-musl.tar.gz" \
        | tar -C /usr/local/bin -xz zellij \
    && chmod 0755 /usr/local/bin/zellij

# Go SDK (multi-arch)
RUN ARCH=$(dpkg --print-architecture) \
    && curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz" | tar -C /usr/local -xz

# Node.js 20 (for claude-code CLI) — copied from the official image to sidestep
# the "invalid signature" apt flake on ports.ubuntu.com inside Docker Desktop's
# build VM. node:20-slim is Debian bookworm (glibc 2.36); Node binaries built
# against older glibc run fine on Ubuntu 24.04 (glibc 2.39).
COPY --from=node:20-slim /usr/local/bin/node /usr/local/bin/node
COPY --from=node:20-slim /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -s /usr/local/lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
    && ln -s /usr/local/lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx

ENV PATH=/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
ENV ZELLIJ_CONFIG_DIR=/etc/zellij

# Revoke the base image's vscode user's sudo access. Keeping the default
# username avoids breakage in the base image's scripts that assume
# /home/vscode; the sandbox guarantees come from the firewall + sudo
# revocation, not the username.
RUN rm -f /etc/sudoers.d/vscode

# Unrestricted operator user (the human)
RUN useradd -m -s /bin/bash -G sudo dev \
    && echo "dev ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/dev \
    && chmod 0440 /etc/sudoers.d/dev

# Shared group so vscode and dev can both work in /workspaces
RUN groupadd developers \
    && usermod -aG developers vscode \
    && usermod -aG developers dev

# Pre-install Go tooling system-wide. Build-time has unrestricted internet,
# so this is the right moment to do it.
RUN GOPATH=/tmp/gopath GOBIN=/usr/local/bin go install \
        golang.org/x/tools/gopls@latest \
    && GOPATH=/tmp/gopath GOBIN=/usr/local/bin go install \
        github.com/go-delve/delve/cmd/dlv@latest \
    && GOPATH=/tmp/gopath GOBIN=/usr/local/bin go install \
        github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest \
    && GOPATH=/tmp/gopath GOBIN=/usr/local/bin go install \
        github.com/evilmartians/lefthook@latest \
    && GOPATH=/tmp/gopath GOBIN=/usr/local/bin go install \
        github.com/siderolabs/conform/cmd/conform@latest \
    && rm -rf /tmp/gopath

# Pre-install Claude Code CLI and tree-sitter CLI globally
RUN npm install -g @anthropic-ai/claude-code tree-sitter-cli \
    && rm -rf /root/.npm

# Clone Neovim config into the image so it's writable (lazy.nvim manages a
# lockfile that the previous read-only host mount couldn't accept).
RUN runuser -u vscode -- git clone --depth=1 \
        https://github.com/germanamz/simple-nvim \
        /home/vscode/.config/nvim

# Sandbox configuration + entrypoint
COPY tinyproxy.conf /etc/tinyproxy/tinyproxy.conf
COPY tinyproxy-filter /etc/tinyproxy/filter
COPY init-firewall.sh /usr/local/bin/init-firewall.sh
COPY entrypoint.sh /usr/local/bin/devcontainer-entrypoint.sh
RUN chmod 0755 /usr/local/bin/init-firewall.sh /usr/local/bin/devcontainer-entrypoint.sh

# Shared Zellij config + layouts (ZELLIJ_CONFIG_DIR is set above).
COPY zellij-config.kdl /etc/zellij/config.kdl
COPY zellij-layouts/ /etc/zellij/layouts/
RUN chmod 0644 /etc/zellij/config.kdl /etc/zellij/layouts/*.kdl

# vscode's interactive defaults
RUN echo 'umask 002' >> /home/vscode/.bashrc \
    && echo 'export PATH=$PATH:$HOME/go/bin' >> /home/vscode/.bashrc

# Pre-create the persistent-volume mount points so Docker seeds each empty
# named volume with vscode ownership on first mount. Without this they mount
# as root:root and the unprivileged vscode user cannot write to them.
# .claude — Claude Code credentials and settings.
# .gocache — Go build + module cache (off the root overlay to avoid fill-up).
# .config/gh — gh auth token, so `gh auth login` persists across rebuilds.
RUN mkdir -p \
        /home/vscode/.claude \
        /home/vscode/.gocache/build \
        /home/vscode/.gocache/mod \
        /home/vscode/.config/gh \
    && chown -R vscode:vscode \
        /home/vscode/.claude \
        /home/vscode/.gocache \
        /home/vscode/.config/gh

# Stay root for PID 1 so the entrypoint can apply iptables; actual user
# attachment happens via `docker exec -u vscode` (set by remoteUser).
ENTRYPOINT ["/usr/local/bin/devcontainer-entrypoint.sh"]
CMD ["sleep", "infinity"]
