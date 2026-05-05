BINARY_NAME := tusk
BUILD_DIR := bin
GO := go
GOFLAGS := -v

.PHONY: all build clean test test-race vet lint fmt help

all: build

# v1 build target — populated as cmd/tusk lands in Plan 1b
build:
	@mkdir -p $(BUILD_DIR)
	@echo "v1 build target: cmd/tusk not yet implemented (Plan 1b)"

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

help:
	@echo "v1 Make targets:"
	@echo "  build      — compile the tusk binary (stub until Plan 1b)"
	@echo "  test       — run unit tests"
	@echo "  test-race  — run unit tests with race detector"
	@echo "  vet        — run go vet"
	@echo "  lint       — run golangci-lint"
	@echo "  fmt        — run gofmt across the tree"
	@echo "  clean      — remove build artifacts"
