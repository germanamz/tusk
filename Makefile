BINARY_NAME := tusk
BUILD_DIR := bin
GO := go
GOFLAGS := -v

.PHONY: all build clean test test-race test-e2e vet lint run install setup-hooks roadmap devcontainer-up devcontainer-shell devcontainer-shell-ops devcontainer-down devcontainer-nuke

DEVCONTAINER_CID = docker ps --filter "label=devcontainer.local_folder=$(CURDIR)" -q
DEVCONTAINER_WORKDIR := /workspaces/$(notdir $(CURDIR))
DEVCONTAINER_VOLUMES := \
	tusk-devcontainer-claude-home \
	tusk-devcontainer-go-cache \
	tusk-devcontainer-gh-config

all: build

build:
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/tusk

clean:
	rm -rf $(BUILD_DIR)

test:
	$(GO) test $(GOFLAGS) ./...

test-race:
	$(GO) test $(GOFLAGS) -race ./...

test-e2e:
	$(GO) test $(GOFLAGS) ./tests/e2e/

vet:
	$(GO) vet ./...

lint:
	golangci-lint run ./...

run: build
	./$(BUILD_DIR)/$(BINARY_NAME)

install:
	$(GO) install $(GOFLAGS) ./cmd/tusk

setup-hooks:
	go install github.com/evilmartians/lefthook@latest
	go install github.com/siderolabs/conform/cmd/conform@latest
	lefthook install
	@echo "Git hooks installed via lefthook"

roadmap: build
	@./$(BUILD_DIR)/$(BINARY_NAME) task tree project=tusk-roadmap --format markdown > ROADMAP.md.tmp \
		&& mv ROADMAP.md.tmp ROADMAP.md \
		|| { rm -f ROADMAP.md.tmp; exit 1; }

devcontainer-up:
	devcontainer up --workspace-folder $(CURDIR) --build-no-cache

devcontainer-shell:
	@cid=$$($(DEVCONTAINER_CID)); \
	test -n "$$cid" || { echo "dev container not running. Start it with: make devcontainer-up"; exit 1; }; \
	docker exec -u vscode -w $(DEVCONTAINER_WORKDIR) -it "$$cid" bash

devcontainer-shell-ops:
	@cid=$$($(DEVCONTAINER_CID)); \
	test -n "$$cid" || { echo "dev container not running. Start it with: make devcontainer-up"; exit 1; }; \
	docker exec -u dev -w $(DEVCONTAINER_WORKDIR) -it "$$cid" bash

devcontainer-down:
	@cid=$$(docker ps -a --filter "label=devcontainer.local_folder=$(CURDIR)" -q); \
	if [ -n "$$cid" ]; then docker rm -f $$cid; else echo "no dev container to remove"; fi

# Nuke all local state for this project's dev container: the container (running
# or stopped), its built image, and the named volumes (~/.claude, Go cache, gh
# config). After this, `make devcontainer-up` rebuilds from scratch with fresh
# volumes — nothing from the host leaks in.
devcontainer-nuke:
	@imgs=""; \
	for c in $$(docker ps -a --filter "label=devcontainer.local_folder=$(CURDIR)" -q); do \
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
