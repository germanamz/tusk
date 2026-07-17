BINARY_NAME := tusk
BUILD_DIR := bin
GO := go
GOFLAGS := -v

.PHONY: all build clean test test-race vet lint fmt help docs web web-book frontend \
        devcontainer-up devcontainer-rebuild devcontainer-shell \
        devcontainer-stop devcontainer-down devcontainer-nuke

DEVCONTAINER_CID := docker ps -a --filter "label=devcontainer.local_folder=$(CURDIR)" -q
DEVCONTAINER_VOLUMES := \
	tusk-devcontainer-claude-home \
	tusk-devcontainer-go-cache \
	tusk-devcontainer-gh-config \
	tusk-devcontainer-nvim-data \
	tusk-devcontainer-nvim-state \
	tusk-devcontainer-nvim-cache \
	tusk-devcontainer-ollama

all: build

build:
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/tusk

clean:
	rm -rf $(BUILD_DIR)

# v1 test target — populated as tests land
test:
	@pkgs=$$($(GO) list ./... 2>/dev/null); [ -z "$$pkgs" ] && echo "no packages to test" || $(GO) test $(GOFLAGS) $$pkgs

test-race:
	@pkgs=$$($(GO) list ./... 2>/dev/null); [ -z "$$pkgs" ] && echo "no packages to test" || $(GO) test $(GOFLAGS) -race $$pkgs

vet:
	@pkgs=$$($(GO) list ./... 2>/dev/null); [ -z "$$pkgs" ] && echo "no packages to vet" || $(GO) vet $$pkgs

lint:
	@command -v golangci-lint >/dev/null || { echo "golangci-lint not installed"; exit 0; }
	@pkgs=$$($(GO) list ./... 2>/dev/null); [ -z "$$pkgs" ] && echo "no packages to lint" || golangci-lint run ./...

fmt:
	@pkgs=$$($(GO) list ./... 2>/dev/null); [ -z "$$pkgs" ] && echo "no packages to fmt" || $(GO) fmt $$pkgs

web:
	cd web && pnpm install --frozen-lockfile && pnpm build

web-book:
	cd web-book && pnpm install --frozen-lockfile && pnpm build

# Aggregate target: builds every committed frontend dist. Drift guards
# (lefthook pre-push, CI's dist-drift job) call this instead of the
# individual targets so adding a third frontend only means adding it here.
frontend: web web-book

docs: build
	$(BUILD_DIR)/$(BINARY_NAME) docgen man docs/cli

help:
	@echo "v1 Make targets:"
	@echo "  build             — compile the tusk binary (stub until Plan 1b)"
	@echo "  test              — run unit tests"
	@echo "  test-race         — run unit tests with race detector"
	@echo "  vet               — run go vet"
	@echo "  lint              — run golangci-lint"
	@echo "  fmt               — run gofmt across the tree"
	@echo "  docs              — regenerate man pages and markdown CLI reference"
	@echo "  clean             — remove build artifacts"
	@echo "  web               — build the graph-view frontend into internal/graphview/dist"
	@echo "  web-book          — build the book frontend into internal/bookview/dist"
	@echo "  frontend          — build all committed frontend dists (web + web-book)"
	@echo "  devcontainer-up      — build and start the dev container (uses BuildKit cache)"
	@echo "  devcontainer-rebuild — build and start the dev container from scratch (no cache)"
	@echo "  devcontainer-shell   — open an interactive zsh inside the running dev container"
	@echo "  devcontainer-stop    — stop the dev container (preserves state)"
	@echo "  devcontainer-down    — stop and remove the dev container"
	@echo "  devcontainer-nuke    — remove container, named volumes, and image"

devcontainer-up:
	devcontainer up --workspace-folder $(CURDIR)

devcontainer-rebuild:
	devcontainer up --workspace-folder $(CURDIR) --build-no-cache

devcontainer-shell:
	@cid=$$($(DEVCONTAINER_CID)); \
	if [ -z "$$cid" ]; then echo "no dev container; run 'make devcontainer-up' first" >&2; exit 1; fi; \
	docker exec -it -u vscode -w /workspaces/tusk $$cid /bin/zsh

devcontainer-stop:
	@cid=$$($(DEVCONTAINER_CID)); \
	if [ -n "$$cid" ]; then docker stop $$cid; else echo "no dev container to stop"; fi

devcontainer-down:
	@cid=$$($(DEVCONTAINER_CID)); \
	if [ -n "$$cid" ]; then docker rm -f $$cid; else echo "no dev container to remove"; fi

# Nuke all local state for this project's dev container: the container
# (running or stopped), its built image, and the named volumes. After this,
# `make devcontainer-up` rebuilds from scratch with fresh volumes — nothing
# from the host leaks in.
devcontainer-nuke:
	@imgs=""; \
	for c in $$($(DEVCONTAINER_CID)); do \
		img=$$(docker inspect --format '{{.Image}}' $$c 2>/dev/null); \
		[ -n "$$img" ] && imgs="$$imgs $$img"; \
		echo "removing container $$c (label match)"; \
		docker rm -f $$c >/dev/null; \
	done; \
	for v in $(DEVCONTAINER_VOLUMES); do \
		for c in $$(docker ps -a -q --filter volume=$$v); do \
			img=$$(docker inspect --format '{{.Image}}' $$c 2>/dev/null); \
			[ -n "$$img" ] && imgs="$$imgs $$img"; \
			echo "removing container $$c (uses $$v)"; \
			docker rm -f $$c >/dev/null; \
		done; \
	done; \
	for v in $(DEVCONTAINER_VOLUMES); do \
		if ! docker volume inspect $$v >/dev/null 2>&1; then \
			echo "volume $$v not present"; \
		elif docker volume rm -f $$v >/dev/null 2>&1; then \
			echo "removed volume $$v"; \
		else \
			echo "FAILED to remove $$v — still referenced by:"; \
			docker ps -a --filter volume=$$v --format '  {{.ID}} {{.Names}} ({{.Status}})'; \
		fi; \
	done; \
	for img in $$(printf '%s\n' $$imgs | sort -u); do \
		[ -z "$$img" ] && continue; \
		docker rmi -f $$img >/dev/null 2>&1 && echo "removed image $$img" || echo "image $$img still in use"; \
	done
