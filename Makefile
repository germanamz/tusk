BINARY_NAME := tusk
BUILD_DIR := bin
GO := go
GOFLAGS := -v

.PHONY: all build clean test test-race test-e2e vet lint run install setup-hooks devcontainer-up devcontainer-shell devcontainer-shell-ops devcontainer-down

DEVCONTAINER_CID = docker ps --filter "label=devcontainer.local_folder=$(CURDIR)" -q
DEVCONTAINER_WORKDIR := /workspaces/$(notdir $(CURDIR))

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

devcontainer-up:
	devcontainer up --workspace-folder $(CURDIR) --build-no-cache

devcontainer-shell:
	@cid=$$($(DEVCONTAINER_CID)); \
	test -n "$$cid" || { echo "dev container not running. Start it with: make devcontainer-up"; exit 1; }; \
	docker exec -u claude -w $(DEVCONTAINER_WORKDIR) -it "$$cid" bash

devcontainer-shell-ops:
	@cid=$$($(DEVCONTAINER_CID)); \
	test -n "$$cid" || { echo "dev container not running. Start it with: make devcontainer-up"; exit 1; }; \
	docker exec -u dev -w $(DEVCONTAINER_WORKDIR) -it "$$cid" bash

devcontainer-down:
	@cid=$$(docker ps -a --filter "label=devcontainer.local_folder=$(CURDIR)" -q); \
	if [ -n "$$cid" ]; then docker rm -f $$cid; else echo "no dev container to remove"; fi
